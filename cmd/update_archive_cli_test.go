package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

// Use the executable so parsing, exit codes, and offline publication are tested
// together. Fixture bodies deliberately disagree with retained metadata.
func TestUpdateArchiveCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, "build: %s", output)
	var requests atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "offline rebuild attempted network access", http.StatusBadGateway)
	}))
	defer trap.Close()
	defer func() { require.Zero(t, requests.Load(), "rebuilding must never fetch posts or covers") }()
	run := func(t *testing.T, cwd string, args ...string) ([]byte, error) {
		t.Helper()
		command := exec.Command(binary, append([]string{"update-archive-page"}, args...)...)
		command.Dir = cwd
		command.Env = os.Environ()
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
			command.Env = append(command.Env, key+"="+trap.URL)
		}
		command.Env = append(command.Env, "NO_PROXY=", "no_proxy=")
		return command.CombinedOutput()
	}
	success := func(t *testing.T, dir, format string) {
		t.Helper()
		output, err := run(t, "", "-o", dir, "-f", format)
		require.NoError(t, err, "rebuild %s: %s", format, output)
	}
	read := func(t *testing.T, dir, name string) []byte {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		return body
	}
	write := func(t *testing.T, dir, name string, body []byte) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0644))
	}
	post := func(t *testing.T, dir, name, format, title string) []byte {
		t.Helper()
		p := lib.Post{Title: title, Slug: "fixture", BodyHTML: "<p>Original body with exact bytes</p>", CanonicalUrl: trap.URL + "/post"}
		require.NoError(t, p.WriteToFile(filepath.Join(dir, name), format, true))
		return read(t, dir, name)
	}

	for _, format := range []string{"html", "md", "txt"} {
		t.Run(format, func(t *testing.T) {
			indexName := "index." + format
			manifestName := ".sbstck-dl-archive-" + format + ".json"
			t.Run("legacy discovery and membership", func(t *testing.T) {
				dir := t.TempDir()
				files := []string{"20260101_120000_old." + format, "20260102_120000_new." + format, "_undated." + format, "20269999_120000_unknown." + format}
				titles := []string{"Older legacy", "Newer legacy", "Alpha undated", "Zulu unknown"}
				bodies := map[string][]byte{}
				for i, file := range files {
					bodies[file] = post(t, dir, file, format, titles[i])
				}
				for _, name := range []string{"notes." + format, "20260103_120000_empty." + format, "20260103_120000_blank." + format, "index.other"} {
					body := []byte("Ignored noise")
					if strings.Contains(name, "empty") {
						body = nil
					}
					if strings.Contains(name, "blank") {
						body = []byte(" \n\t")
					}
					write(t, dir, name, body)
				}
				require.NoError(t, os.Mkdir(filepath.Join(dir, "20260104_120000_directory."+format), 0755))
				write(t, dir, indexName, []byte("Stale index"))
				if runtime.GOOS != "windows" {
					require.NoError(t, os.Chmod(filepath.Join(dir, indexName), 0600))
				}
				success(t, dir, format)
				index := string(read(t, dir, indexName))
				last := -1
				for _, i := range []int{1, 0, 2, 3} {
					require.Equal(t, 1, strings.Count(index, files[i]), "local post must be linked exactly once")
					position := strings.Index(index, titles[i])
					require.Greater(t, position, last, "dated posts newest first; unknown dates after known dates")
					last = position
					require.Equal(t, bodies[files[i]], read(t, dir, files[i]), "rebuild rewrote a post")
				}
				require.NotContains(t, index, "Ignored noise")
				require.NotContains(t, index, "empty.")
				require.NotContains(t, index, "blank.")
				if runtime.GOOS != "windows" {
					info, err := os.Stat(filepath.Join(dir, indexName))
					require.NoError(t, err)
					require.Equal(t, os.FileMode(0600), info.Mode().Perm())
				}
				added := "20260103_120000_added." + format
				addedBody := post(t, dir, added, format, "Added legacy")
				require.NoError(t, os.Remove(filepath.Join(dir, files[0])))
				success(t, dir, format)
				index = string(read(t, dir, indexName))
				require.NotContains(t, index, files[0])
				require.Equal(t, 1, strings.Count(index, added))
				require.Equal(t, addedBody, read(t, dir, added))
				for _, file := range append(files[1:], added) {
					require.NoError(t, os.Remove(filepath.Join(dir, file)))
				}
				success(t, dir, format)
				index = string(read(t, dir, indexName))
				for _, title := range append(titles, "Added legacy") {
					require.NotContains(t, index, title)
				}
				var metadata struct {
					Entries []json.RawMessage `json:"entries"`
				}
				require.NoError(t, json.Unmarshal(read(t, dir, manifestName), &metadata))
				require.Empty(t, metadata.Entries, "final deletion must publish an empty archive")
			})
			t.Run("portable retained metadata", func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, "original archive")
				require.NoError(t, os.Mkdir(dir, 0755))
				file := "20260101_120000_retained." + format
				body := post(t, dir, file, format, "Body title differs")
				require.NoError(t, os.Mkdir(filepath.Join(dir, "images"), 0755))
				write(t, dir, "images/cover.jpg", []byte("retained cover bytes"))
				archive := lib.NewArchive()
				downloadTime := time.Date(2020, 3, 4, 5, 6, 0, 0, time.UTC)
				p := lib.Post{Title: "Retained rich title", PostDate: "2025-09-08T12:00:00Z", Subtitle: "Retained subtitle", Description: "Retained description", CoverImage: trap.URL + "/cover.jpg"}
				archive.AddEntry(p, filepath.Join(dir, file), downloadTime)
				archive.Entries[0].LocalCoverImage = "images/cover.jpg"
				require.NoError(t, archive.SaveMetadata(dir, format))
				for _, other := range []string{"html", "md", "txt"} {
					if other != format {
						write(t, dir, "index."+other, []byte("Independent "+other+" index"))
						write(t, dir, ".sbstck-dl-archive-"+other+".json", []byte("Independent "+other+" metadata"))
					}
				}
				moved := filepath.Join(root, "moved archive with spaces")
				require.NoError(t, os.Rename(dir, moved))
				success(t, moved, format)
				index := string(read(t, moved, indexName))
				require.Contains(t, index, p.Title)
				require.NotContains(t, index, "Body title differs")
				require.Contains(t, index, "September 8, 2025")
				require.Contains(t, index, "March 4, 2020 05:06")
				require.Contains(t, index, p.Subtitle)
				if format != "txt" {
					require.Contains(t, index, "images/cover.jpg")
					require.NotContains(t, index, p.CoverImage)
				}
				require.Equal(t, body, read(t, moved, file))
				require.Equal(t, []byte("retained cover bytes"), read(t, moved, "images/cover.jpg"))
				metadata := string(read(t, moved, manifestName))
				for _, value := range []string{p.Title, p.Subtitle, p.Description, p.PostDate, downloadTime.Format(time.RFC3339), "images/cover.jpg", p.CoverImage} {
					require.Contains(t, metadata, value)
				}
				require.NotContains(t, metadata, root, "metadata must remain portable")
				for _, other := range []string{"html", "md", "txt"} {
					if other != format {
						require.Equal(t, []byte("Independent "+other+" index"), read(t, moved, "index."+other))
						require.Equal(t, []byte("Independent "+other+" metadata"), read(t, moved, ".sbstck-dl-archive-"+other+".json"))
					}
				}
			})
		})
	}

	t.Run("defaults and long flags", func(t *testing.T) {
		dir := t.TempDir()
		file := "20260101_120000_default.html"
		post(t, dir, file, "html", "Default HTML")
		output, err := run(t, dir)
		require.NoError(t, err, "%s", output)
		require.Contains(t, string(read(t, dir, "index.html")), file)
		output, err = run(t, "", "--output", dir, "--format", "html")
		require.NoError(t, err, "%s", output)
	})
	t.Run("invalid inputs preserve index", func(t *testing.T) {
		dir := t.TempDir()
		prior := []byte("Prior valid index")
		write(t, dir, "index.html", prior)
		file := "20260101_120000_legacy.html"
		post(t, dir, file, "html", "Legacy")
		failure := func(t *testing.T, args ...string) {
			t.Helper()
			output, err := run(t, "", args...)
			require.Error(t, err, "invalid input unexpectedly succeeded: %s", output)
			require.NotEmpty(t, strings.TrimSpace(string(output)), "failure needs an actionable error")
			require.Equal(t, prior, read(t, dir, "index.html"))
		}
		for _, format := range []string{"pdf", "html,md", ""} {
			failure(t, "-o", dir, "-f", format)
		}
		failure(t, "-o", dir, "extra")
		failure(t, "-o", filepath.Join(dir, "missing"))
		failure(t, "-o", filepath.Join(dir, file))
		for _, invalid := range []string{"{broken", `{"version":2,"entries":[]}`, `{"version":1,"entries":[{"file":"../escape.html","title":"Bad","download_time":"2020-01-01T00:00:00Z"}]}`, `{"version":1,"entries":[{"file":"20260101_120000_legacy.html","title":"Bad"}]}`} {
			write(t, dir, ".sbstck-dl-archive-html.json", []byte(invalid))
			failure(t, "-o", dir)
		}
	})
	t.Run("publication conflicts preserve destination", func(t *testing.T) {
		for _, destination := range []string{".sbstck-dl-archive-html.json", "index.html"} {
			t.Run(destination, func(t *testing.T) {
				dir := t.TempDir()
				post(t, dir, "20260101_120000_post.html", "html", "Post")
				prior := []byte("Prior valid index")
				if destination != "index.html" {
					write(t, dir, "index.html", prior)
				}
				require.NoError(t, os.Mkdir(filepath.Join(dir, destination), 0755))
				write(t, dir, destination+"/sentinel", prior)
				output, err := run(t, "", "-o", dir)
				require.Error(t, err, "%s", output)
				require.Equal(t, prior, read(t, dir, destination+"/sentinel"))
				if destination != "index.html" {
					require.Equal(t, prior, read(t, dir, "index.html"))
				}
			})
		}
	})
	t.Run("permission failures preserve index", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("POSIX permission checks require a non-root process")
		}
		for _, kind := range []string{"unreadable directory", "unreadable legacy post", "unwritable publication"} {
			t.Run(kind, func(t *testing.T) {
				dir := t.TempDir()
				prior := []byte("Prior valid index")
				write(t, dir, "index.html", prior)
				file := "20260101_120000_legacy.html"
				post(t, dir, file, "html", "Legacy")
				target, mode := dir, os.FileMode(0000)
				if kind == "unreadable legacy post" {
					target = filepath.Join(dir, file)
				}
				if kind == "unwritable publication" {
					mode = 0555
				}
				require.NoError(t, os.Chmod(target, mode))
				t.Cleanup(func() { _ = os.Chmod(target, 0755) })
				output, err := run(t, "", "-o", dir)
				require.Error(t, err, "%s", output)
				require.NoError(t, os.Chmod(target, 0755))
				require.Equal(t, prior, read(t, dir, "index.html"))
			})
		}
	})
}
