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
	"sync/atomic"
	"testing"

	"github.com/alexferrari88/sbstck-dl/lib"
)

// Exercise the real executable, including flag parsing, extraction, skip-existing,
// and index generation. Each invocation starts with fresh process state.
func TestArchiveCLIIncremental(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	var requests atomic.Int32
	var count atomic.Int32
	count.Store(2)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			fmt.Fprint(w, "<urlset>")
			for i := 1; i <= int(count.Load()); i++ {
				fmt.Fprintf(w, "<url><loc>%s/p/post-%d</loc><lastmod>2026-01-%02d</lastmod></url>", server.URL, i, i)
			}
			fmt.Fprint(w, "</urlset>")
			return
		}
		requests.Add(1)
		var i int
		if _, err := fmt.Sscanf(r.URL.Path, "/p/post-%d", &i); err != nil {
			http.NotFound(w, r)
			return
		}
		post := lib.Post{Id: i, Title: fmt.Sprintf("Post %d", i), Slug: fmt.Sprintf("post-%d", i), PostDate: fmt.Sprintf("2026-01-%02dT12:00:00Z", i), CanonicalUrl: server.URL + r.URL.Path, BodyHTML: "<p>Fixture body</p>", Subtitle: "Retained subtitle"}
		payload, _ := json.Marshal(lib.PostWrapper{Post: post})
		escaped, _ := json.Marshal(string(payload))
		fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
	}))
	defer server.Close()
	for _, format := range []string{"html", "md", "txt"} {
		t.Run(format, func(t *testing.T) {
			count.Store(2)
			dir := t.TempDir()
			run := func(url string, extra ...string) {
				t.Helper()
				args := []string{"download", "--url", url, "--format", format, "--output", dir, "--create-archive", "--rate", "1000"}
				args = append(args, extra...)
				output, err := exec.Command(binary, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("CLI %v: %v\n%s", args, err, output)
				}
			}
			check := func(stage string, n int) {
				t.Helper()
				content, err := os.ReadFile(filepath.Join(dir, "index."+format))
				if err != nil {
					t.Fatal(err)
				}
				lastPosition := -1
				for i := n; i >= 1; i-- {
					position := strings.Index(string(content), fmt.Sprintf("Post %d", i))
					if position < lastPosition {
						t.Errorf("%s publication dates not newest first", stage)
					}
					lastPosition = position
				}
				for i := 1; i <= n; i++ {
					if strings.Count(string(content), fmt.Sprintf("Post %d", i)) != 1 {
						t.Errorf("%s index missing Post %d", stage, i)
					}
					file := fmt.Sprintf("202601%02d_120000_post-%d.%s", i, i, format)
					if strings.Count(string(content), file) != 1 {
						t.Errorf("%s missing/duplicate local link %s", stage, file)
					}
					if !strings.Contains(string(content), fmt.Sprintf("January %d, 2026", i)) {
						t.Errorf("%s missing publication date for post %d", stage, i)
					}
					if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
						t.Errorf("%s post %d file: %v", stage, i, err)
					}
				}
				t.Logf("%s: files=%d, indexed titles=%d", stage, n, strings.Count(string(content), "Post "))
			}
			run(server.URL)
			check("initial bulk", 2)
			run(server.URL + "/p/post-3")
			check("single new post", 3)
			count.Store(4)
			run(server.URL)
			check("bulk rerun", 4)
			before := requests.Load()
			run(server.URL)
			check("no new posts", 4)
			if requests.Load() != before {
				t.Error("existing posts were fetched again")
			}
			indexPath := filepath.Join(dir, "index."+format)
			metadataPath := filepath.Join(dir, ".sbstck-dl-archive-"+format+".json")
			original, _ := os.ReadFile(indexPath)
			metadata, _ := os.ReadFile(metadataPath)
			if !strings.Contains(string(metadata), "Retained subtitle") || strings.Contains(string(metadata), "Fixture body") {
				t.Error("manifest must retain index metadata without body payloads")
			}
			run(server.URL, "--dry-run")
			after, _ := os.ReadFile(indexPath)
			afterMeta, _ := os.ReadFile(metadataPath)
			if string(after) != string(original) || string(afterMeta) != string(metadata) {
				t.Error("dry run changed archive")
			}
			// Removing metadata simulates output from an older downloader.
			if err := os.Remove(metadataPath); err != nil {
				t.Fatal(err)
			}
			run(server.URL)
			check("legacy recovery", 4)
			if requests.Load() != before {
				t.Error("legacy recovery fetched existing posts")
			}
			// A corrupt manifest must fail before downloads or index replacement.
			original, _ = os.ReadFile(indexPath)
			var output []byte
			var err error
			invalidManifests := []string{
				"{broken",
				fmt.Sprintf(`{"version":1,"entries":[{"file":"20260101_120000_post-1.%s","title":"Post 1"}]}`, format),
				fmt.Sprintf(`{"version":1,"entries":[{"file":"20260101_120000_post-1.%s","title":"Post 1","download_time":"0001-01-01T00:00:00Z"}]}`, format),
				fmt.Sprintf(`{"version":1,"entries":[{"file":"20260101_120000_post-1.%s","download_time":"2026-01-01T12:00:00Z"}]}`, format),
			}
			for _, invalidManifest := range invalidManifests {
				if err := os.WriteFile(metadataPath, []byte(invalidManifest), 0644); err != nil {
					t.Fatal(err)
				}
				output, err = exec.Command(binary, "download", "--url", server.URL, "--format", format, "--output", dir, "--create-archive").CombinedOutput()
				if err == nil || !strings.Contains(string(output), "restore archive") {
					t.Errorf("corrupt manifest did not fail clearly: %v %s", err, output)
				}
				after, _ = os.ReadFile(indexPath)
				if string(after) != string(original) {
					t.Error("restore failure changed old index")
				}
			}
			// Plain/media write errors cannot add a link to an unwritten post.
			if err := os.Remove(metadataPath); err != nil {
				t.Fatal(err)
			}
			badFile := filepath.Join(dir, "20260105_120000_post-5."+format)
			if err := os.Mkdir(badFile, 0755); err != nil {
				t.Fatal(err)
			}
			for _, mediaFlag := range []string{"", "--download-images", "--download-files"} {
				args := []string{"download", "--url", server.URL + "/p/post-5", "--format", format, "--output", dir, "--create-archive", "--rate", "1000"}
				if mediaFlag != "" {
					args = append(args, mediaFlag)
				}
				output, err := exec.Command(binary, args...).CombinedOutput()
				if err == nil || !strings.Contains(string(output), "write post") {
					t.Errorf("%s write failure: %v %s", mediaFlag, err, output)
				}
				after, _ := os.ReadFile(indexPath)
				if strings.Contains(string(after), "Post 5") {
					t.Errorf("%s indexed failed post", mediaFlag)
				}
				check("write failure preserved prior entries", 4)
			}
			// An existing manifest entry whose file became invalid must vanish,
			// even when a repeated single-post write also fails.
			invalid := filepath.Join(dir, "20260101_120000_post-1."+format)
			if err := os.Remove(invalid); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(invalid, 0755); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(binary, "download", "--url", server.URL+"/p/post-1", "--format", format, "--output", dir, "--create-archive").CombinedOutput(); err == nil {
				t.Errorf("repeated single write unexpectedly passed: %s", output)
			}
			after, _ = os.ReadFile(indexPath)
			if strings.Contains(string(after), "Post 1") {
				t.Error("failed overwrite retained invalid file link")
			}
			// Atomic publication must preserve the old destination on rename failure.
			if err := os.Remove(indexPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(indexPath, 0755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(indexPath, "prior-index")
			if err := os.WriteFile(sentinel, original, 0644); err != nil {
				t.Fatal(err)
			}
			output, err = exec.Command(binary, "download", "--url", server.URL, "--format", format, "--output", dir, "--create-archive").CombinedOutput()
			if err == nil || !strings.Contains(string(output), "publish archive") {
				t.Errorf("publication failure: %v %s", err, output)
			}
			after, _ = os.ReadFile(sentinel)
			if string(after) != string(original) {
				t.Error("failed publication damaged old destination")
			}

		})
	}
	t.Run("alternating formats", func(t *testing.T) {
		dir := t.TempDir()
		count.Store(2)
		indexes := make(map[string]string)
		for _, format := range []string{"html", "md", "txt", "html", "md", "txt"} {
			output, err := exec.Command(binary, "download", "--url", server.URL, "--format", format, "--output", dir, "--create-archive", "--rate", "1000").CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v %s", format, err, output)
			}
			for other, prior := range indexes {
				body, err := os.ReadFile(filepath.Join(dir, "index."+other))
				if err != nil {
					t.Fatal(err)
				}
				if other != format && string(body) != prior {
					t.Errorf("%s changed %s index", format, other)
				}
			}
			body, err := os.ReadFile(filepath.Join(dir, "index."+format))
			if err != nil {
				t.Fatal(err)
			}
			indexes[format] = string(body)
			for i := 1; i <= 2; i++ {
				file := fmt.Sprintf("202601%02d_120000_post-%d.%s", i, i, format)
				if strings.Count(string(body), file) != 1 {
					t.Errorf("%s missing/duplicate link %s", format, file)
				}
				for _, other := range []string{"html", "md", "txt"} {
					if other != format && strings.Contains(string(body), fmt.Sprintf("post-%d.%s", i, other)) {
						t.Errorf("%s links %s post", format, other)
					}
				}
			}
		}
	})

}
