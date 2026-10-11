package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func TestArchiveCoverCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, string(output))
	for _, format := range []string{"html", "md"} {
		for _, bodyCover := range []bool{true, false} {
			for _, initialImages := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/body_cover_%t/initial_images_%t", format, bodyCover, initialImages), func(t *testing.T) {
					var postRequests, imageRequests atomic.Int32
					var server *httptest.Server
					server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/sitemap.xml":
							fmt.Fprintf(w, "<urlset><url><loc>%s/p/fixture</loc><lastmod>2026-01-01</lastmod></url></urlset>", server.URL)
						case "/cover.jpg":
							imageRequests.Add(1)
							fmt.Fprint(w, "fixture cover bytes")
						case "/p/fixture":
							postRequests.Add(1)
							body := "<p>Fixture body</p>"
							if bodyCover {
								body += fmt.Sprintf(`<img src="%s/cover.jpg" alt="Cover body">`, server.URL)
							}
							post := lib.Post{Id: 1, Slug: "fixture", Title: "Fixture", PostDate: "2026-01-01T12:00:00Z", CoverImage: server.URL + "/cover.jpg", BodyHTML: body, CanonicalUrl: server.URL + "/p/fixture"}
							payload, _ := json.Marshal(lib.PostWrapper{Post: post})
							escaped, _ := json.Marshal(string(payload))
							fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
						default:
							http.NotFound(w, r)
						}
					}))
					server.Start()
					defer server.Close()
					dir := t.TempDir()
					run := func(images bool) {
						t.Helper()
						args := []string{"download", "--url", server.URL, "--create-archive", "--format", format, "--output", dir, "--images-dir", "assets/nested images", "--rate", "1000"}
						if images {
							args = append(args, "--download-images")
						}
						output, err := exec.Command(binary, args...).CombinedOutput()
						require.NoError(t, err, string(output))
					}
					reference := func() string {
						t.Helper()
						data, err := os.ReadFile(filepath.Join(dir, "index."+format))
						require.NoError(t, err)
						if format == "html" {
							doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
							require.NoError(t, err)
							path, exists := doc.Find(".cover-image").Attr("src")
							require.True(t, exists)
							return path
						}
						match := regexp.MustCompile(`!\[Cover Image\]\(([^)]+)\)`).FindStringSubmatch(string(data))
						require.Len(t, match, 2)
						return match[1]
					}
					localPath := func() string {
						t.Helper()
						parsed, err := url.Parse(reference())
						require.NoError(t, err)
						require.Empty(t, parsed.Scheme)
						require.True(t, strings.HasPrefix(parsed.Path, "assets/nested images/fixture/"), parsed.Path)
						require.NotContains(t, parsed.Path, "\\")
						path := filepath.Join(dir, filepath.FromSlash(parsed.Path))
						body, err := os.ReadFile(path)
						require.NoError(t, err)
						require.Equal(t, "fixture cover bytes", string(body))
						return path
					}
					run(initialImages)
					if !initialImages {
						require.Zero(t, imageRequests.Load())
						require.Equal(t, server.URL+"/cover.jpg", reference())
						run(false)
						require.Zero(t, imageRequests.Load())
						run(true) // Retained metadata, no new post body.
					}
					require.Equal(t, int32(1), imageRequests.Load(), "matching body cover should not fetch twice")
					path := localPath()
					require.Equal(t, int32(1), postRequests.Load())
					run(true)
					require.Equal(t, int32(1), imageRequests.Load(), "existing asset should be reused")
					require.Equal(t, path, localPath())
					metadata, err := os.ReadFile(filepath.Join(dir, ".sbstck-dl-archive-"+format+".json"))
					require.NoError(t, err)
					require.Contains(t, string(metadata), server.URL+"/cover.jpg")
					require.Contains(t, string(metadata), `"local_cover_image"`)
					require.NotContains(t, string(metadata), dir)
					require.NoError(t, os.Remove(path))
					run(true)
					require.Equal(t, int32(2), imageRequests.Load(), "missing asset should retry original source")
					_ = localPath()
					moved := filepath.Join(t.TempDir(), "moved")
					require.NoError(t, os.Rename(dir, moved))
					dir = moved
					run(true)
					_ = localPath()
					require.Equal(t, int32(2), imageRequests.Load(), "moving folder should not trigger fetch")
					require.Equal(t, int32(1), postRequests.Load(), "cover recovery must not fetch post body")
				})
			}
		}
	}
	t.Run("failed post write retains correct previous cover", func(t *testing.T) {
		var latest atomic.Bool
		var originalRequests atomic.Int32
		var server *httptest.Server
		server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/cover.jpg":
				originalRequests.Add(1)
				fmt.Fprint(w, "original cover bytes")
			case "/unrelated/cover.jpg":
				fmt.Fprint(w, "unrelated image bytes")
			case "/p/fixture":
				date := "2026-01-01T12:00:00Z"
				image := server.URL + "/cover.jpg"
				if latest.Load() {
					date = "2026-01-02T12:00:00Z"
					image = server.URL + "/unrelated/cover.jpg"
				}
				post := lib.Post{Slug: "fixture", Title: "Fixture", PostDate: date, CoverImage: server.URL + "/cover.jpg", BodyHTML: fmt.Sprintf(`<img src="%s">`, image)}
				payload, _ := json.Marshal(lib.PostWrapper{Post: post})
				escaped, _ := json.Marshal(string(payload))
				fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
			default:
				http.NotFound(w, r)
			}
		}))
		server.Start()
		defer server.Close()
		dir := t.TempDir()
		args := []string{"download", "--url", server.URL + "/p/fixture", "--create-archive", "--download-images", "--format", "html", "--output", dir, "--rate", "1000"}
		output, err := exec.Command(binary, args...).CombinedOutput()
		require.NoError(t, err, string(output))
		require.Equal(t, int32(1), originalRequests.Load())
		// Images download first, then a directory at the new post path makes
		// atomic post publication fail. The old post/index entry remains.
		require.NoError(t, os.Mkdir(filepath.Join(dir, "20260102_120000_fixture.html"), 0755))
		latest.Store(true)
		output, err = exec.Command(binary, args...).CombinedOutput()
		require.Error(t, err, string(output))
		require.Contains(t, string(output), "write post")
		index, err := os.ReadFile(filepath.Join(dir, "index.html"))
		require.NoError(t, err)
		require.Contains(t, string(index), "20260101_120000_fixture.html")
		require.NotContains(t, string(index), "20260102_120000_fixture.html")
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(index)))
		require.NoError(t, err)
		reference, exists := doc.Find(".cover-image").Attr("src")
		require.True(t, exists)
		require.Contains(t, reference, "/covers/")
		parsed, err := url.Parse(reference)
		require.NoError(t, err)
		cover, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(parsed.Path)))
		require.NoError(t, err)
		require.Equal(t, "original cover bytes", string(cover))
		require.Equal(t, int32(2), originalRequests.Load(), "failed post body attempts must invalidate retained cover reuse")
	})

}

func TestArchiveCoverCLIFailure(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, string(output))
	for _, format := range []string{"html", "md"} {
		t.Run(format, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/p/fixture" {
					post := lib.Post{Slug: "fixture", Title: "Fixture", PostDate: "2026-01-01T12:00:00Z", CoverImage: server.URL + "/missing.jpg", BodyHTML: "<p>Usable body</p>"}
					payload, _ := json.Marshal(lib.PostWrapper{Post: post})
					escaped, _ := json.Marshal(string(payload))
					fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
					return
				}
				http.NotFound(w, r)
			}))
			server.Start()
			defer server.Close()
			dir := t.TempDir()
			output, err := exec.Command(binary, "download", "--url", server.URL+"/p/fixture", "--create-archive", "--download-images", "--format", format, "--output", dir, "--rate", "1000").CombinedOutput()
			require.NoError(t, err, string(output))
			require.Contains(t, string(output), "Warning:")
			require.Contains(t, string(output), "using remote source")
			index, err := os.ReadFile(filepath.Join(dir, "index."+format))
			require.NoError(t, err)
			require.Contains(t, string(index), server.URL+"/missing.jpg")
			post, err := os.ReadFile(filepath.Join(dir, "20260101_120000_fixture."+format))
			require.NoError(t, err)
			require.Contains(t, string(post), "Usable body")
			metadata, err := os.ReadFile(filepath.Join(dir, ".sbstck-dl-archive-"+format+".json"))
			require.NoError(t, err)
			require.Contains(t, string(metadata), server.URL+"/missing.jpg")
			require.NotContains(t, string(metadata), "local_cover_image")
		})
	}
}
