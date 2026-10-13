package cmd

import (
	"context"
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
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

// One built binary exercises the real flag setup and process error status.
func TestMediaCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, "%s", output)
	run := func(t *testing.T, url, dir string, flags ...string) (string, error) {
		t.Helper()
		timeout, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		args := []string{"download", "--url", url, "--output", dir, "--rate", "1000"}
		out, err := exec.CommandContext(timeout, binary, append(args, flags...)...).CombinedOutput()
		require.NoError(t, timeout.Err(), "CLI stalled: %s", out)
		return string(out), err
	}
	read := func(t *testing.T, path string) string {
		t.Helper()
		b, e := os.ReadFile(path)
		require.NoError(t, e)
		return string(b)
	}
	complete := func(t *testing.T, dir, slug, kind string) bool {
		t.Helper()
		m, e := lib.LoadMediaManifest(dir)
		require.NoError(t, e)
		return m.IsComplete(slug, kind)
	}
	const stem = "20250102_030405_"
	t.Run("independent outputs survive API and CDN errors", func(t *testing.T) {
		for _, broken := range []string{"podcast-api", "video-api", "video-cdn"} {
			t.Run(broken, func(t *testing.T) {
				pub := newMediaCLIPublication(t)
				pub.add("good", map[string]any{"podcast_url": "/api/v1/audio/upload/good/src"})
				bad := map[string]any{"podcast_url": "/api/v1/audio/upload/bad/src"}
				if broken != "podcast-api" {
					bad = map[string]any{"type": "video", "video_upload_id": "bad"}
				}
				pub.add("bad", bad)
				switch broken {
				case "podcast-api":
					pub.setFailure("/api/v1/audio/upload/bad/src")
				case "video-api":
					pub.setFailure("/api/v1/video/upload/bad")
				case "video-cdn":
					pub.setFailure("/cdn/bad.mp4")
				}
				dir := t.TempDir()
				out, err := run(t, pub.URL, dir, "--download-podcasts", "--download-videos", "--format", "html,md,txt", "--create-archive")
				require.Error(t, err, out)
				require.Contains(t, out, "bad")
				require.Contains(t, read(t, filepath.Join(dir, stem+"good.html")), "<audio data-podcast")
				require.Equal(t, "audio bytes", read(t, filepath.Join(dir, "podcasts", stem+"good.mp3")))
				for _, format := range []string{"html", "md", "txt"} {
					require.FileExists(t, filepath.Join(dir, stem+"bad."+format))
					require.FileExists(t, filepath.Join(dir, "index."+format))
				}
				kind := lib.MediaPodcast
				if broken != "podcast-api" {
					kind = lib.MediaVideos
				}
				require.False(t, complete(t, dir, "bad", kind))
				require.True(t, complete(t, dir, "good", lib.MediaPodcast))
			})
		}
	})
	t.Run("failed existing HTML embedding retries cached bytes", func(t *testing.T) {
		for _, filename := range []string{stem + "episode.html", "19990101_010203_episode.html", "_episode.html"} {
			t.Run(filename, func(t *testing.T) {
				pub := newMediaCLIPublication(t)
				pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
				dir := t.TempDir()
				htmlPath := filepath.Join(dir, filename)
				original := "<h1>My title</h1>\n<p>user edits survive</p>"
				require.NoError(t, os.WriteFile(htmlPath, []byte(original), 0640))
				// Replace the inventoried target during media transfer to force a real
				// embedding failure, independent of privilege/atomic read-only behavior.
				pub.setAudioHook(func() {
					require.NoError(t, os.Rename(htmlPath, htmlPath+".backup"))
					require.NoError(t, os.Mkdir(htmlPath, 0755))
				})
				out, err := run(t, pub.URL, dir, "--download-podcasts")
				require.Error(t, err, out)
				require.Equal(t, original, read(t, htmlPath+".backup"))
				require.False(t, complete(t, dir, "episode", lib.MediaPodcast))
				require.Equal(t, 1, pub.count("/cdn/audio.mp3"))
				require.NoError(t, os.Remove(htmlPath))
				require.NoError(t, os.Rename(htmlPath+".backup", htmlPath))
				pub.setAudioHook(nil)
				out, err = run(t, pub.URL, dir, "--download-podcasts")
				require.NoError(t, err, out)
				repaired := read(t, htmlPath)
				require.Contains(t, repaired, "user edits survive")
				require.Contains(t, repaired, "<audio data-podcast")
				require.Equal(t, 1, pub.count("/cdn/audio.mp3"), "retry reuses completed bytes")
				info, e := os.Stat(htmlPath)
				require.NoError(t, e)
				if runtime.GOOS != "windows" {
					require.Equal(t, os.FileMode(0640), info.Mode().Perm())
				}
				require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
				out, err = run(t, pub.URL, dir, "--download-podcasts")
				require.NoError(t, err, out)
				require.Equal(t, repaired, read(t, htmlPath))
			})
		}
	})
	t.Run("read-only HTML can be atomically updated", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX file modes")
		}
		pub := newMediaCLIPublication(t)
		pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
		dir := t.TempDir()
		path := filepath.Join(dir, stem+"episode.html")
		require.NoError(t, os.WriteFile(path, []byte("<h1>User heading</h1><p>custom body</p>"), 0444))
		out, err := run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		require.Contains(t, read(t, path), "<audio data-podcast")
		require.Contains(t, read(t, path), "custom body")
		info, e := os.Stat(path)
		require.NoError(t, e)
		require.Equal(t, os.FileMode(0444), info.Mode().Perm())
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
	})
	t.Run("failed new output publication retries", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
		dir := t.TempDir()
		target := filepath.Join(dir, stem+"episode.md")
		require.NoError(t, os.Mkdir(target, 0755))
		sentinel := filepath.Join(target, "keep")
		require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0644))
		out, err := run(t, pub.URL, dir, "--download-podcasts", "--format", "html,md")
		require.Error(t, err, out)
		require.Equal(t, "keep", read(t, sentinel))
		require.FileExists(t, filepath.Join(dir, stem+"episode.html"))
		require.False(t, complete(t, dir, "episode", lib.MediaPodcast))
		require.NoError(t, os.Remove(sentinel))
		require.NoError(t, os.Remove(target))
		out, err = run(t, pub.URL, dir, "--download-podcasts", "--format", "html,md")
		require.NoError(t, err, out)
		require.Contains(t, read(t, target), "podcasts/")
		require.Equal(t, 1, pub.count("/cdn/audio.mp3"))
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
	})
	t.Run("invalid media destinations", func(t *testing.T) {
		for _, kind := range []string{"podcast", "video"} {
			for _, invalid := range []string{"empty", "directory", "symlink"} {
				t.Run(kind+"/"+invalid, func(t *testing.T) {
					pub := newMediaCLIPublication(t)
					data := map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"}
					flag := "--download-podcasts"
					subdir := "podcasts"
					ext := "mp3"
					manifestKind := lib.MediaPodcast
					if kind == "video" {
						data = map[string]any{"type": "video", "video_upload_id": "episode"}
						flag = "--download-videos"
						subdir = "videos"
						ext = "mp4"
						manifestKind = lib.MediaVideos
					}
					pub.add("episode", data)
					dir := t.TempDir()
					require.NoError(t, os.Mkdir(filepath.Join(dir, subdir), 0755))
					path := filepath.Join(dir, subdir, stem+"episode."+ext)
					switch invalid {
					case "empty":
						require.NoError(t, os.WriteFile(path, nil, 0644))
					case "directory":
						require.NoError(t, os.Mkdir(path, 0755))
						require.NoError(t, os.WriteFile(filepath.Join(path, "keep"), []byte("keep"), 0644))
					case "symlink":
						target := filepath.Join(dir, "user-file")
						require.NoError(t, os.WriteFile(target, []byte("user bytes"), 0644))
						if e := os.Symlink(target, path); e != nil {
							t.Skipf("symlinks unavailable: %v", e)
						}
					}
					// A prior complete record must not certify these invalid cache paths.
					manifest := lib.NewMediaManifest(dir)
					manifest.Record("episode", manifestKind, true, []string{path})
					require.NoError(t, manifest.Save())
					out, err := run(t, pub.URL, dir, flag)
					if invalid == "empty" {
						require.NoError(t, err, out)
						info, e := os.Stat(path)
						require.NoError(t, e)
						require.Positive(t, info.Size())
						require.True(t, complete(t, dir, "episode", manifestKind))
					} else {
						require.Error(t, err, out)
						require.False(t, complete(t, dir, "episode", manifestKind))
						if invalid == "directory" {
							require.Equal(t, "keep", read(t, filepath.Join(path, "keep")))
						} else {
							require.Equal(t, "user bytes", read(t, filepath.Join(dir, "user-file")))
							info, e := os.Lstat(path)
							require.NoError(t, e)
							require.NotZero(t, info.Mode()&os.ModeSymlink)
						}
					}
				})
			}
		}
	})
	t.Run("manifest load errors preserve state", func(t *testing.T) {
		for _, body := range []string{"broken JSON", `{"version":7,"posts":{}}`, `{"version":1,"posts":{"episode":{"podcast":{"complete":true,"files":["../outside.mp3"]}}}}`} {
			pub := newMediaCLIPublication(t)
			pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
			dir := t.TempDir()
			path := filepath.Join(dir, lib.MediaManifestFile)
			require.NoError(t, os.WriteFile(path, []byte(body), 0644))
			out, err := run(t, pub.URL, dir, "--download-podcasts")
			require.Error(t, err, out)
			require.Equal(t, body, read(t, path))
			require.Zero(t, pub.count("/cdn/audio.mp3"))
		}
	})
	t.Run("manifest save failure preserves previous durable records", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("requires non-root POSIX permissions")
		}
		pub := newMediaCLIPublication(t)
		pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
		dir := t.TempDir()
		m := lib.NewMediaManifest(dir)
		m.Record("retained", lib.MediaPodcast, false, nil)
		require.NoError(t, m.Save())
		path := filepath.Join(dir, lib.MediaManifestFile)
		original := read(t, path)
		require.NoError(t, os.Chmod(dir, 0555))
		defer os.Chmod(dir, 0755)
		out, err := run(t, pub.URL, dir, "--download-podcasts")
		require.Error(t, err, out)
		require.Contains(t, out, "save pending media")
		require.Equal(t, original, read(t, path))
		require.Zero(t, pub.count("/cdn/audio.mp3"), "failed pre-stage must prevent byte downloads")
		require.False(t, complete(t, dir, "episode", lib.MediaPodcast))
		require.NoError(t, os.Chmod(dir, 0755))
		pub.setAudioHook(nil)
		out, err = run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		require.Equal(t, 1, pub.count("/cdn/audio.mp3"))
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
	})
	t.Run("repair invalidates old completion before byte repair", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("requires non-root POSIX permissions")
		}
		pub := newMediaCLIPublication(t)
		pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
		dir := t.TempDir()
		defer os.Chmod(dir, 0755)
		htmlPath := filepath.Join(dir, stem+"episode.html")
		original := "<h1>User title</h1><p>unchanged user body</p>"
		require.NoError(t, os.WriteFile(htmlPath, []byte(original), 0644))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "podcasts"), 0755))
		audioPath := filepath.Join(dir, "podcasts", stem+"episode.mp3")
		require.NoError(t, os.WriteFile(audioPath, nil, 0644))
		m := lib.NewMediaManifest(dir)
		m.Record("episode", lib.MediaPodcast, true, []string{audioPath})
		require.NoError(t, m.Save())
		pub.setAudioHook(func() { require.NoError(t, os.Chmod(dir, 0555)) })
		out, err := run(t, pub.URL, dir, "--download-podcasts")
		require.Error(t, err, out)
		require.Equal(t, "audio bytes", read(t, audioPath))
		require.Equal(t, original, read(t, htmlPath))
		require.False(t, complete(t, dir, "episode", lib.MediaPodcast), "repair cannot resurrect the old complete:true record")
		require.NoError(t, os.Chmod(dir, 0755))
		pub.setAudioHook(nil)
		out, err = run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		require.Equal(t, 1, pub.count("/cdn/audio.mp3"))
		require.Contains(t, read(t, htmlPath), "<audio data-podcast")
		require.Contains(t, read(t, htmlPath), "unchanged user body")
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
	})
	t.Run("legacy byte-only completion repairs HTML once", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("episode", map[string]any{"podcast_url": "/api/v1/audio/upload/episode/src"})
		dir := t.TempDir()
		htmlPath := filepath.Join(dir, stem+"episode.html")
		require.NoError(t, os.WriteFile(htmlPath, []byte("<h1>User title</h1><p>legacy user edits</p>"), 0644))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "podcasts"), 0755))
		audio := filepath.Join("podcasts", stem+"episode.mp3")
		require.NoError(t, os.WriteFile(filepath.Join(dir, audio), []byte("cached legacy audio"), 0644))
		legacy := fmt.Sprintf(`{"version":1,"posts":{"episode":{"podcast":{"complete":true,"files":[%q]}}}}`, filepath.ToSlash(audio))
		require.NoError(t, os.WriteFile(filepath.Join(dir, lib.MediaManifestFile), []byte(legacy), 0644))
		out, err := run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		html := read(t, htmlPath)
		require.Contains(t, html, "<audio data-podcast")
		require.Contains(t, html, "legacy user edits")
		require.Zero(t, pub.count("/cdn/audio.mp3"))
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
		m, e := lib.LoadMediaManifest(dir)
		require.NoError(t, e)
		require.Equal(t, 2, m.Version)
		out, err = run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		require.Equal(t, html, read(t, htmlPath))
		require.Equal(t, 1, pub.count("/p/episode"))
	})
	t.Run("preview warning upgrade custom directories metadata fallback", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		data := map[string]any{"type": "podcast", "audience": "only_paid", "podcast_url": "/api/v1/audio/upload/episode/src", "podcastUpload": map[string]any{"duration": 10, "is_free_preview": true, "full_podcast_info": map[string]any{"duration": 100}}}
		pub.add("episode", data)
		pub.add("withheld", map[string]any{"type": "podcast", "audience": "only_paid", "podcast_duration": 120})
		pub.add("video", map[string]any{"type": "video", "videoUpload": map[string]any{"id": "own-video"}})
		dir := t.TempDir()
		flags := []string{"--download-podcasts", "--download-videos", "--podcasts-dir", "assets/my audio", "--videos-dir", "assets/my videos", "--format", "html,md,txt", "--create-archive"}
		out, err := run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		require.Contains(t, out, "Warning: only a preview")
		require.Contains(t, out, "Warning: the podcast audio for post withheld is not available")
		require.False(t, complete(t, dir, "withheld", lib.MediaPodcast))
		htmlPath := filepath.Join(dir, stem+"episode.html")
		require.Contains(t, read(t, htmlPath), "assets/my%20audio/"+stem+"episode_preview.mp3")
		require.False(t, complete(t, dir, "episode", lib.MediaPodcast))
		require.Contains(t, read(t, filepath.Join(dir, stem+"video.html")), `data-upload-id="own-video"`)
		require.Contains(t, read(t, filepath.Join(dir, stem+"video.md")), "assets/my%20videos/")
		data["podcastUpload"] = map[string]any{"duration": 100}
		pub.add("episode", data)
		out, err = run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		html := read(t, htmlPath)
		require.NotContains(t, html, "_preview.mp3")
		require.Equal(t, 1, strings.Count(html, "<audio data-podcast"))
		require.True(t, complete(t, dir, "episode", lib.MediaPodcast))
		out, err = run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		require.Equal(t, html, read(t, htmlPath))
		require.Equal(t, 2, pub.count("/cdn/audio.mp3"))
	})
}

type mediaCLIPublication struct {
	*httptest.Server
	mu      sync.Mutex
	posts   map[string]map[string]any
	counts  map[string]int
	fail    string
	onAudio func()
}

func newMediaCLIPublication(t *testing.T) *mediaCLIPublication {
	p := &mediaCLIPublication{posts: map[string]map[string]any{}, counts: map[string]int{}}
	p.mu.Lock()
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	p.mu.Unlock()
	t.Cleanup(p.Close)
	return p
}
func (p *mediaCLIPublication) add(slug string, input map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data := make(map[string]any, len(input))
	for key, value := range input {
		data[key] = value
	}
	data["slug"] = slug
	data["canonical_url"] = p.URL + "/p/" + slug
	data["post_date"] = "2025-01-02T03:04:05Z"
	data["title"] = slug
	data["body_html"] = "<p>body of " + slug + "</p>"
	if data["audience"] == nil {
		data["audience"] = "everyone"
	}
	if audio, ok := data["podcast_url"].(string); ok && strings.HasPrefix(audio, "/") {
		data["podcast_url"] = p.URL + audio
	}
	p.posts[slug] = data
}
func (p *mediaCLIPublication) count(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[path]
}
func (p *mediaCLIPublication) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.counts[r.URL.Path]++
	defer p.mu.Unlock()
	if r.URL.Path == p.fail {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.URL.Path == "/sitemap.xml":
		fmt.Fprint(w, `<?xml version="1.0"?><urlset>`)
		for slug := range p.posts {
			fmt.Fprintf(w, "<url><loc>%s/p/%s</loc><lastmod>2025-01-02</lastmod></url>", p.URL, slug)
		}
		fmt.Fprint(w, `</urlset>`)
	case strings.HasPrefix(r.URL.Path, "/p/"):
		data, ok := p.posts[strings.TrimPrefix(r.URL.Path, "/p/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		preloads, _ := json.Marshal(map[string]any{"post": data})
		quoted, _ := json.Marshal(string(preloads))
		fmt.Fprintf(w, `<script>window._preloads = JSON.parse(%s)</script>`, quoted)
	case strings.HasPrefix(r.URL.Path, "/api/v1/audio/upload/"):
		http.Redirect(w, r, "/cdn/audio.mp3", http.StatusTemporaryRedirect)
	case r.URL.Path == "/cdn/audio.mp3":
		if p.onAudio != nil {
			p.onAudio()
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		fmt.Fprint(w, "audio bytes")
	case strings.HasPrefix(r.URL.Path, "/api/v1/video/upload/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/video/upload/"), "/src")
		if strings.HasSuffix(r.URL.Path, "/src") {
			http.Redirect(w, r, "/cdn/"+id+".mp4", http.StatusTemporaryRedirect)
		} else {
			fmt.Fprintf(w, `{"id":%q}`, id)
		}
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		w.Header().Set("Content-Type", "video/mp4")
		fmt.Fprint(w, "video bytes")
	default:
		http.NotFound(w, r)
	}
}

func (p *mediaCLIPublication) setFailure(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = path
}
func (p *mediaCLIPublication) setAudioHook(hook func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onAudio = hook
}
