package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func TestMultiFormatCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, string(output))
	var mu sync.Mutex
	counts := make(map[string]int)
	var bodyCover, missingCover bool
	var postHook func()
	var server *httptest.Server
	server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		coverInBody := bodyCover
		failCover := missingCover
		hook := postHook
		mu.Unlock()
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, "<urlset><url><loc>%s/p/fixture</loc></url></urlset>", server.URL)
		case "/image.jpg", "/cover.jpg":
			if failCover && r.URL.Path == "/cover.jpg" {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, "image fixture bytes")
		case "/attachment.pdf":
			fmt.Fprint(w, "attachment fixture bytes")
		case "/p/fixture":
			if hook != nil {
				hook()
			}
			image := "/image.jpg"
			if coverInBody {
				image = "/cover.jpg"
			}
			post := lib.Post{Slug: "fixture", Title: "Fixture", PostDate: "2026-01-01T12:00:00Z", CanonicalUrl: server.URL + r.URL.Path, CoverImage: server.URL + "/cover.jpg", BodyHTML: fmt.Sprintf(`<p>Body text</p><img src="%s%s" alt="photo"><a class="file-embed-button wide" href="%s/attachment.pdf">Attachment</a>`, server.URL, image, server.URL)}
			payload, _ := json.Marshal(lib.PostWrapper{Post: post})
			escaped, _ := json.Marshal(string(payload))
			fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
		default:
			http.NotFound(w, r)
		}
	}))
	server.Start()
	defer server.Close()
	reset := func(cover bool) {
		mu.Lock()
		counts = make(map[string]int)
		bodyCover = cover
		missingCover = false
		postHook = nil
		mu.Unlock()
	}
	count := func(path string) int { mu.Lock(); defer mu.Unlock(); return counts[path] }
	run := func(dir, url string, flags ...string) ([]byte, error) {
		args := []string{"download", "--url", url, "--output", dir, "--rate", "1000"}
		args = append(args, flags...)
		return exec.Command(binary, args...).CombinedOutput()
	}
	success := func(dir, url string, flags ...string) {
		t.Helper()
		output, err := run(dir, url, flags...)
		require.NoError(t, err, string(output))
	}
	read := func(dir, file string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, err)
		return string(data)
	}
	syntaxes := [][]string{
		{"--format", "html,md,txt"}, {"--format", "html", "md", "txt"}, {"--format", "html", "--format", "md", "--format", "txt"},
		{"-f", "html,md,txt"}, {"-f", "html", "md", "txt"}, {"-fhtml", "-fmd", "-ftxt"},
		{"--format=html", "md", "txt", "--format", "HTML,md"},
	}
	for i, syntax := range syntaxes {
		for _, flags := range [][]string{nil, {"--download-images"}, {"--download-files"}, {"--download-images", "--download-files"}} {
			t.Run(fmt.Sprintf("syntax-%d/media-%v", i, flags), func(t *testing.T) {
				reset(false)
				dir := t.TempDir()
				args := append(append([]string{}, syntax...), flags...)
				args = append(args, "--add-source-url")
				success(dir, server.URL+"/p/fixture", args...)
				require.Equal(t, 1, count("/p/fixture"))
				images, files := false, false
				for _, flag := range flags {
					if flag == "--download-images" {
						images = true
					}
					if flag == "--download-files" {
						files = true
					}
				}
				expected := 0
				if images {
					expected = 1
				}
				require.Equal(t, expected, count("/image.jpg"))
				expected = 0
				if files {
					expected = 1
				}
				require.Equal(t, expected, count("/attachment.pdf"))
				require.Zero(t, count("/cover.jpg"))
				for _, format := range []string{"html", "md", "txt"} {
					body := read(dir, "20260101_120000_fixture."+format)
					require.Contains(t, body, "Body text")
					require.Contains(t, body, "original content:")
					require.Contains(t, body, server.URL+"/p/fixture")
					if format == "txt" {
						require.NotContains(t, body, "images/")
						require.NotContains(t, body, "files/")
					} else {
						if images {
							require.Contains(t, body, "images/fixture/image.jpg")
						}
						if files {
							require.Contains(t, body, "files/fixture/attachment.pdf")
						}
					}
				}
			})
		}
	}
	t.Run("default and txt-only", func(t *testing.T) {
		reset(false)
		dir := t.TempDir()
		success(dir, server.URL+"/p/fixture")
		require.Contains(t, read(dir, "20260101_120000_fixture.html"), "Body text")
		reset(false)
		dir = t.TempDir()
		success(dir, server.URL+"/p/fixture", "-f", "txt", "--download-images", "--download-files")
		require.Equal(t, 1, count("/image.jpg"))
		require.Zero(t, count("/attachment.pdf"))
		require.NotContains(t, read(dir, "20260101_120000_fixture.txt"), "images/")
	})
	t.Run("non-format values beginning with shorthand", func(t *testing.T) {
		reset(false)
		cwd := t.TempDir()
		command := exec.Command(binary, "download", "--url", server.URL+"/p/fixture", "--output", "-fmd", "--download-images", "--download-files", "--images-dir", "-fassets", "--files-dir", "-fdocs", "--rate", "1000")
		command.Dir = cwd
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		require.Contains(t, read(filepath.Join(cwd, "-fmd"), "20260101_120000_fixture.html"), "-fassets/fixture/image.jpg")
		require.Contains(t, read(filepath.Join(cwd, "-fmd"), "20260101_120000_fixture.html"), "-fdocs/fixture/attachment.pdf")
		require.Equal(t, 1, count("/p/fixture"))
		require.Equal(t, 1, count("/image.jpg"))
		require.Equal(t, 1, count("/attachment.pdf"))
	})

	t.Run("invalid before network and writes", func(t *testing.T) {
		for _, flags := range [][]string{{"--format", ""}, {"--format", "html,"}, {"--format", "html", "bogus"}, {"--format", "json"}, {"unexpected"}, {"--format", "html", "--output", "unused", "md"}, {"--format", "html", "--", "md"}, {"-f="}} {
			for _, dry := range []bool{false, true} {
				reset(false)
				dir := filepath.Join(t.TempDir(), "absent")
				args := append([]string{}, flags...)
				if dry {
					args = append(args, "--dry-run")
				}
				output, err := run(dir, server.URL+"/p/fixture", args...)
				require.Error(t, err, string(output))
				require.Zero(t, count("/p/fixture"))
				_, err = os.Stat(dir)
				require.True(t, os.IsNotExist(err))
			}
		}
	})
	for _, inBody := range []bool{true, false} {
		t.Run(fmt.Sprintf("archives-cover-in-body-%t", inBody), func(t *testing.T) {
			reset(inBody)
			dir := t.TempDir()
			flags := []string{"-f", "html,md,txt", "--create-archive", "--download-images", "--download-files", "--images-dir", "assets/nested images"}
			success(dir, server.URL, flags...)
			require.Equal(t, 1, count("/p/fixture"))
			require.Equal(t, 1, count("/cover.jpg"))
			require.Equal(t, 1, count("/attachment.pdf"))
			var cover string
			for _, format := range []string{"html", "md", "txt"} {
				metadata := read(dir, ".sbstck-dl-archive-"+format+".json")
				require.Contains(t, metadata, server.URL+"/cover.jpg")
				require.NotContains(t, metadata, dir)
				var manifest struct {
					Entries []struct {
						LocalCover string `json:"local_cover_image"`
					}
				}
				require.NoError(t, json.Unmarshal([]byte(metadata), &manifest))
				require.Len(t, manifest.Entries, 1)
				if cover == "" {
					cover = manifest.Entries[0].LocalCover
				}
				require.Equal(t, cover, manifest.Entries[0].LocalCover)
				require.Contains(t, read(dir, "index."+format), "20260101_120000_fixture."+format)
			}
			success(dir, server.URL, flags...)
			require.Equal(t, 1, count("/p/fixture"))
			require.Equal(t, 1, count("/cover.jpg"))
			require.NoError(t, os.Remove(filepath.Join(dir, filepath.FromSlash(cover))))
			success(dir, server.URL, flags...)
			require.Equal(t, 2, count("/cover.jpg"))
			require.Equal(t, 1, count("/p/fixture"))
			moved := filepath.Join(t.TempDir(), "moved")
			require.NoError(t, os.Rename(dir, moved))
			dir = moved
			success(dir, server.URL, flags...)
			require.Equal(t, 2, count("/cover.jpg"))
			before := read(dir, "index.md")
			success(dir, server.URL, append(flags, "--dry-run")...)
			require.Equal(t, before, read(dir, "index.md"))
		})
	}
	t.Run("retained later format supplies cover", func(t *testing.T) {
		reset(true)
		dir := t.TempDir()
		flags := []string{"-f", "html,md", "--create-archive", "--download-images"}
		success(dir, server.URL, flags...)
		metadata := read(dir, ".sbstck-dl-archive-html.json")
		var manifest map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(metadata), &manifest))
		for _, entry := range manifest["entries"].([]interface{}) {
			delete(entry.(map[string]interface{}), "local_cover_image")
		}
		revised, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".sbstck-dl-archive-html.json"), revised, 0644))
		success(dir, server.URL, flags...)
		require.Equal(t, 1, count("/cover.jpg"), "HTML must reuse later Markdown's valid cover without fetching")
		require.Equal(t, 1, count("/p/fixture"))
		require.Contains(t, read(dir, ".sbstck-dl-archive-html.json"), `"local_cover_image"`)
	})
	t.Run("failed covers fetch once across formats", func(t *testing.T) {
		reset(false)
		mu.Lock()
		missingCover = true
		mu.Unlock()
		dir := t.TempDir()
		output, err := run(dir, server.URL, "-f", "html,md,txt", "--create-archive", "--download-images")
		require.NoError(t, err, string(output))
		require.Contains(t, string(output), "Warning:")
		require.Equal(t, 1, count("/cover.jpg"))
		require.Equal(t, 1, count("/p/fixture"))
		for _, format := range []string{"html", "md"} {
			require.Contains(t, read(dir, "index."+format), server.URL+"/cover.jpg")
		}
	})

	t.Run("partial formats retain files and indexes", func(t *testing.T) {
		reset(false)
		dir := t.TempDir()
		success(dir, server.URL, "-f", "html", "--create-archive")
		html := read(dir, "20260101_120000_fixture.html")
		// A complete legacy version and an unrelated archived post must survive.
		require.NoError(t, os.Rename(filepath.Join(dir, "20260101_120000_fixture.html"), filepath.Join(dir, "20251201_120000_fixture.html")))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "_older.md"), []byte("# Older\n\nretained"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "20260101_120000_fixture.txt"), nil, 0644))
		success(dir, server.URL, "-f", "html,md,txt", "--create-archive")
		require.Equal(t, 2, count("/p/fixture"))
		require.Equal(t, html, read(dir, "20251201_120000_fixture.html"))
		require.Contains(t, read(dir, "index.md"), "Older")
		require.Contains(t, read(dir, "index.html"), "20251201_120000_fixture.html")
		success(dir, server.URL, "-f", "html,md,txt", "--create-archive")
		require.Equal(t, 2, count("/p/fixture"))
		// A direct post selection overwrites every requested format.
		require.NoError(t, os.WriteFile(filepath.Join(dir, "20260101_120000_fixture.md"), []byte("old"), 0644))
		success(dir, server.URL+"/p/fixture", "-f", "html,md,txt", "--create-archive")
		require.Contains(t, read(dir, "20260101_120000_fixture.md"), "Body text")
	})
	t.Run("restore all before any downloading", func(t *testing.T) {
		reset(false)
		dir := t.TempDir()
		success(dir, server.URL, "-f", "html,md", "--create-archive")
		prior := read(dir, "index.html")
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".sbstck-dl-archive-md.json"), []byte("{broken"), 0644))
		before := count("/p/fixture")
		output, err := run(dir, server.URL+"/p/fixture", "-f", "html,md", "--create-archive")
		require.Error(t, err, string(output))
		require.Contains(t, string(output), "restore archive")
		require.Equal(t, before, count("/p/fixture"))
		require.Equal(t, prior, read(dir, "index.html"))
	})
	t.Run("metadata publication failure allows other formats", func(t *testing.T) {
		reset(false)
		dir := t.TempDir()
		flags := []string{"-f", "html,md,txt", "--create-archive"}
		success(dir, server.URL, flags...)
		prior := read(dir, "index.html")
		require.NoError(t, os.Remove(filepath.Join(dir, "index.md")))
		require.NoError(t, os.Remove(filepath.Join(dir, "index.txt")))
		metadataPath := filepath.Join(dir, ".sbstck-dl-archive-html.json")
		priorMetadata := read(dir, ".sbstck-dl-archive-html.json")
		hookErrors := make(chan error, 1)
		mu.Lock()
		postHook = func() {
			err := os.Rename(metadataPath, metadataPath+".prior")
			if err == nil {
				err = os.Mkdir(metadataPath, 0755)
			}
			if err == nil {
				err = os.WriteFile(filepath.Join(metadataPath, "sentinel"), []byte(priorMetadata), 0644)
			}
			hookErrors <- err
		}
		mu.Unlock()
		output, err := run(dir, server.URL+"/p/fixture", flags...)
		require.Error(t, err, string(output))
		require.NoError(t, <-hookErrors)
		require.Contains(t, string(output), "save archive metadata html")
		require.Equal(t, prior, read(dir, "index.html"))
		require.Equal(t, priorMetadata, read(metadataPath, "sentinel"))
		require.Contains(t, read(dir, "index.md"), "Fixture")
		require.Contains(t, read(dir, "index.txt"), "Fixture")
	})

	t.Run("independent publication failures", func(t *testing.T) {
		for _, blocked := range []string{"20260101_120000_fixture.html", "index.html"} {
			reset(false)
			dir := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(dir, blocked), 0755))
			output, err := run(dir, server.URL+"/p/fixture", "-f", "html,md,txt", "--create-archive")
			require.Error(t, err, string(output))
			require.Contains(t, read(dir, "index.md"), "Fixture")
			require.Contains(t, read(dir, "index.txt"), "Fixture")
			if strings.HasPrefix(blocked, "2026") {
				require.NotContains(t, read(dir, "index.html"), "Fixture")
			}
		}
	})
}

func TestRequestedFormatsOrder(t *testing.T) {
	oldValues, oldFormat := formatValues, format
	defer func() { formatValues, format = oldValues, oldFormat }()
	formatValues = []string{" TXT, html ", "md", "HTML,txt"}
	formats, err := requestedFormats()
	require.NoError(t, err)
	require.Equal(t, []string{"txt", "html", "md"}, formats)
}
