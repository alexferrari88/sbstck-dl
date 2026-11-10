package cmd

import (
	"context"
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
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func audioClipEmbed(id string) string {
	return fmt.Sprintf(`<div class="native-audio-embed" data-component-name="AudioPlaceholder" data-attrs="{&quot;label&quot;:null,&quot;mediaUploadId&quot;:&quot;%s&quot;,&quot;duration&quot;:416.2,&quot;downloadable&quot;:true,&quot;isEditorNode&quot;:true}"></div>`, id)
}

func TestAudioClipsCLI(t *testing.T) {
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
	complete := func(t *testing.T, dir, slug string) bool {
		t.Helper()
		m, e := lib.LoadMediaManifest(dir)
		require.NoError(t, e)
		return m.IsComplete(slug, lib.MediaAudioClips)
	}
	const stem = "20250102_030405_"
	body := "<p>Before</p>" + audioClipEmbed("clip-a") + "<p>Between</p>" + audioClipEmbed("clip-b") + audioClipEmbed("clip-a")
	clipPath := func(dir, id string) string {
		return filepath.Join(dir, "audio-clips", stem+"clips_audio-"+id+".mp3")
	}

	t.Run("downloads clips once and plays them in place", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("clips", map[string]any{"body_html": body})
		dir := t.TempDir()
		out, err := run(t, pub.URL, dir, "--download-audio-clips", "--format", "html,md")
		require.NoError(t, err, out)
		for _, id := range []string{"clip-a", "clip-b"} {
			require.Equal(t, "audio bytes", read(t, clipPath(dir, id)))
			require.Equal(t, 1, pub.count("/api/v1/audio/upload/"+id+"/src"))
		}
		post := read(t, filepath.Join(dir, stem+"clips.html"))
		require.NotContains(t, post, "native-audio-embed")
		require.Equal(t, 2, strings.Count(post, `data-audio-clip-id="clip-a"`))
		require.Contains(t, post, `src="audio-clips/`+stem+`clips_audio-clip-b.mp3"`)
		// The clips stay where they were in the text.
		require.Less(t, strings.Index(post, "Before"), strings.Index(post, "clip-a"))
		require.Less(t, strings.Index(post, "clip-a"), strings.Index(post, "Between"))
		require.Less(t, strings.Index(post, "Between"), strings.Index(post, `data-audio-clip-id="clip-b"`))
		require.Contains(t, read(t, filepath.Join(dir, stem+"clips.md")), "[Download audio](audio-clips/"+stem+"clips_audio-clip-b.mp3)")
		require.True(t, complete(t, dir, "clips"))

		// A second run fetches nothing again.
		out, err = run(t, pub.URL, dir, "--download-audio-clips", "--format", "html,md")
		require.NoError(t, err, out)
		require.Equal(t, 1, pub.count("/api/v1/audio/upload/clip-a/src"))
		require.Equal(t, 1, pub.count("/p/clips"))
	})

	t.Run("only downloads playable native clip markers", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		template := "<template>" + audioClipEmbed("template-example") + "</template>"
		span := `<span class="native-audio-embed" data-attrs='{"mediaUploadId":"span-example"}'></span>`
		pub.add("clips", map[string]any{"body_html": template + span + audioClipEmbed("clip-a")})
		dir := t.TempDir()
		out, err := run(t, pub.URL, dir, "--download-audio-clips")
		require.NoError(t, err, out)
		require.Zero(t, pub.count("/api/v1/audio/upload/template-example/src"))
		require.Zero(t, pub.count("/api/v1/audio/upload/span-example/src"))
		require.Equal(t, 1, pub.count("/api/v1/audio/upload/clip-a/src"))
		post := read(t, filepath.Join(dir, stem+"clips.html"))
		require.Contains(t, post, template+span)
		require.Equal(t, 1, strings.Count(post, "<audio "))
		require.True(t, complete(t, dir, "clips"))
	})

	t.Run("authenticates clips only at the selected publication", func(t *testing.T) {
		const resolver = "/api/v1/audio/upload/clip-a/src"
		const cookie = "substack.sid=synthetic-subscriber"
		var cdnRequests, alternateRequests atomic.Int32
		var cdnSawCookie, authenticated atomic.Bool
		cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cdnRequests.Add(1)
			if r.Header.Get("Cookie") != "" {
				cdnSawCookie.Store(true)
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			fmt.Fprint(w, "audio bytes")
		}))
		defer cdn.Close()
		alternate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			alternateRequests.Add(1)
			http.Redirect(w, r, cdn.URL+"/clip.mp3", http.StatusTemporaryRedirect)
		}))
		defer alternate.Close()
		publication := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/p/clips":
				writeMediaCLIPost(w, map[string]any{
					"slug": "clips", "title": "Clips", "post_date": "2025-01-02T03:04:05Z",
					"canonical_url": alternate.URL + "/p/clips", "body_html": audioClipEmbed("clip-a"),
				})
			case resolver:
				if r.URL.RawQuery != "" || r.Header.Get("Cookie") != cookie {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				authenticated.Store(true)
				http.Redirect(w, r, cdn.URL+"/clip.mp3", http.StatusTemporaryRedirect)
			default:
				http.NotFound(w, r)
			}
		}))
		defer publication.Close()

		dir := t.TempDir()
		out, err := run(t, publication.URL+"/p/clips?from=test#post", dir,
			"--download-audio-clips", "--cookie_name", "substack.sid", "--cookie_val", "synthetic-subscriber")
		require.NoError(t, err, out)
		require.Zero(t, alternateRequests.Load(), "canonical metadata must never select the resolver")
		require.True(t, authenticated.Load(), "the selected publication received the subscriber cookie")
		require.EqualValues(t, 1, cdnRequests.Load())
		require.False(t, cdnSawCookie.Load())
		require.Equal(t, "audio bytes", read(t, clipPath(dir, "clip-a")))
	})

	t.Run("off by default and separate from podcasts", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("clips", map[string]any{"body_html": body})
		dir := t.TempDir()
		out, err := run(t, pub.URL, dir, "--download-podcasts", "--download-videos")
		require.NoError(t, err, out)
		require.Zero(t, pub.count("/api/v1/audio/upload/clip-a/src"))
		_, err = os.Stat(filepath.Join(dir, "audio-clips"))
		require.True(t, os.IsNotExist(err))
		require.Contains(t, read(t, filepath.Join(dir, stem+"clips.html")), "native-audio-embed")
	})

	t.Run("adds clips to an earlier download", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("clips", map[string]any{"body_html": body})
		dir := t.TempDir()
		out, err := run(t, pub.URL, dir, "--download-podcasts")
		require.NoError(t, err, out)
		out, err = run(t, pub.URL, dir, "--download-podcasts", "--download-audio-clips", "--audio-clips-dir", "assets/my clips")
		require.NoError(t, err, out)
		require.Equal(t, "audio bytes", read(t, filepath.Join(dir, "assets", "my clips", stem+"clips_audio-clip-b.mp3")))
		require.Contains(t, read(t, filepath.Join(dir, stem+"clips.html")), `src="assets/my%20clips/`+stem+`clips_audio-clip-b.mp3"`)
		require.True(t, complete(t, dir, "clips"))
	})

	t.Run("backfills hidden clips in the edited saved excerpt", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, stem+"clips.html")
		backup := path + ".saved"
		var cdnRequests atomic.Int32
		var interruptOutput atomic.Bool
		pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := "http://" + r.Host
			subscriber := r.Header.Get("Cookie") == "substack.sid=synthetic-subscriber"
			switch r.URL.Path {
			case "/sitemap.xml":
				fmt.Fprintf(w, "<urlset><url><loc>%s/p/clips</loc></url></urlset>", origin)
			case "/p/clips":
				body := "<p>Public excerpt</p>"
				if subscriber {
					body += "<p>Subscriber text</p>" + audioClipEmbed("clip-b") + audioClipEmbed("clip-a")
				}
				writeMediaCLIPost(w, map[string]any{
					"slug": "clips", "title": "Paid post", "post_date": "2025-01-02T03:04:05Z",
					"canonical_url": origin + "/p/clips", "audience": "only_paid", "body_html": body,
				})
			case "/api/v1/audio/upload/clip-a/src", "/api/v1/audio/upload/clip-b/src":
				if !subscriber {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				http.Redirect(w, r, "/cdn/clip.mp3", http.StatusTemporaryRedirect)
			case "/cdn/clip.mp3":
				cdnRequests.Add(1)
				if interruptOutput.CompareAndSwap(true, false) {
					if err := os.Rename(path, backup); err != nil {
						t.Errorf("temporarily move the inventoried HTML: %v", err)
					}
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				fmt.Fprint(w, "audio bytes")
			default:
				http.NotFound(w, r)
			}
		}))
		defer pub.Close()

		out, err := run(t, pub.URL, dir, "--download-audio-clips")
		require.NoError(t, err, out)
		saved := read(t, path)
		require.Contains(t, saved, "Public excerpt")
		require.False(t, complete(t, dir, "clips"))
		require.Zero(t, cdnRequests.Load())
		const suffix = "</body></html>\n"
		classLookalike := strings.Replace(audioClipEmbed("clip-a"), `class="native-audio-embed"`, `class="example&nbsp;native-audio-embed"`, 1)
		prefix := "<!doctype html><html><body>\n" + saved + "\n<P id='local-edit'>Keep this edit.</P>\n" +
			classLookalike + "\n" +
			"<!-- " + audioClipEmbed("clip-a") + " -->\n" +
			`<script>const example = '<audio data-audio-clip-id="clip-b"></audio>';</script>` + "\n"
		original := prefix + suffix
		require.NoError(t, os.WriteFile(path, []byte(original), 0600))
		require.NoError(t, os.Chmod(path, 0600))
		info, err := os.Stat(path)
		require.NoError(t, err)
		mode := info.Mode().Perm()

		flags := []string{"--download-audio-clips", "--cookie_name", "substack.sid", "--cookie_val", "synthetic-subscriber"}
		interruptOutput.Store(true)
		out, err = run(t, pub.URL, dir, flags...)
		require.Error(t, err, out)
		require.Contains(t, out, "existing HTML for clips disappeared")
		require.Equal(t, original, read(t, backup))
		for _, id := range []string{"clip-a", "clip-b"} {
			require.Equal(t, "audio bytes", read(t, clipPath(dir, id)))
		}
		require.False(t, complete(t, dir, "clips"), "HTML publication must succeed before completion")
		require.NoError(t, os.Rename(backup, path))

		out, err = run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		post := read(t, path)
		require.True(t, strings.HasPrefix(post, prefix), "preserve the excerpt, title, and local edit byte for byte")
		require.True(t, strings.HasSuffix(post, suffix), "append the players inside the saved HTML body")
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(post))
		require.NoError(t, err)
		players := doc.Find("body audio[data-audio-clip-id]")
		require.Equal(t, 2, players.Length())
		for i, id := range []string{"clip-a", "clip-b"} {
			player := players.Eq(i)
			require.Equal(t, id, player.AttrOr("data-audio-clip-id", ""))
			require.Equal(t, "audio-clips/"+filepath.Base(clipPath(dir, id)), player.AttrOr("src", ""))
			require.Contains(t, player.Parent().Text(), "Audio clip")
		}
		require.NotContains(t, post, "Subscriber text")
		info, err = os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, mode, info.Mode().Perm())
		require.True(t, complete(t, dir, "clips"))
		require.EqualValues(t, 2, cdnRequests.Load(), "retry reuses both downloaded clips")

		require.NoError(t, os.Remove(filepath.Join(dir, lib.MediaManifestFile)))
		out, err = run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		require.Equal(t, post, read(t, path), "forced re-embedding leaves one player per ID")
		require.EqualValues(t, 2, cdnRequests.Load())
		require.True(t, complete(t, dir, "clips"))
	})

	t.Run("backfills an edited post selected by direct URL", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("clips", map[string]any{"audience": "only_paid", "body_html": "<p>Public excerpt</p>"})
		dir := t.TempDir()
		url := pub.URL + "/p/clips"
		out, err := run(t, url, dir)
		require.NoError(t, err, out)
		require.NoFileExists(t, filepath.Join(dir, lib.MediaManifestFile))
		path := filepath.Join(dir, stem+"clips.html")
		saved := read(t, path) + "\n<p>Keep this local edit.</p>"
		require.NoError(t, os.WriteFile(path, []byte(saved), 0644))

		pub.add("clips", map[string]any{"audience": "only_paid", "body_html": "<p>Subscriber text</p>" + audioClipEmbed("clip-a")})
		flags := []string{"--download-audio-clips", "--cookie_name", "substack.sid", "--cookie_val", "synthetic-subscriber", "--format", "html,md"}
		out, err = run(t, url, dir, flags...)
		require.NoError(t, err, out)
		post := read(t, path)
		require.True(t, strings.HasPrefix(post, saved), "backfill must retain the saved excerpt and edit")
		require.Equal(t, 1, strings.Count(post, `data-audio-clip-id="clip-a"`))
		require.Contains(t, post, `src="audio-clips/`+stem+`clips_audio-clip-a.mp3"`)
		require.Equal(t, "audio bytes", read(t, clipPath(dir, "clip-a")))
		require.Contains(t, read(t, filepath.Join(dir, stem+"clips.md")), "[Download audio](audio-clips/"+stem+"clips_audio-clip-a.mp3)")
		require.True(t, complete(t, dir, "clips"))

		out, err = run(t, url, dir, flags...)
		require.NoError(t, err, out)
		require.Equal(t, post, read(t, path))
		require.Equal(t, 3, pub.count("/p/clips"), "a direct URL still checks for newly visible clips")

		out, err = run(t, url, dir, "--format", "md")
		require.NoError(t, err, out)
		require.True(t, complete(t, dir, "clips"), "refreshing only Markdown leaves HTML completion intact")

		pub.add("clips", map[string]any{"audience": "only_paid", "body_html": "<p>Refreshed excerpt</p>"})
		if runtime.GOOS != "windows" && os.Geteuid() != 0 {
			t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
			require.NoError(t, os.Chmod(dir, 0555))
			out, err = run(t, url, dir)
			require.NoError(t, os.Chmod(dir, 0755))
			require.Error(t, err, out)
			require.Contains(t, out, "save pending audio clips for clips")
			require.Equal(t, post, read(t, path), "failed manifest publication must keep the existing HTML")
			require.True(t, complete(t, dir, "clips"))
		}
		out, err = run(t, url, dir)
		require.NoError(t, err, out)
		refreshed := read(t, path)
		require.Contains(t, refreshed, "Refreshed excerpt", "a direct run without clips still refreshes the post")
		require.NotContains(t, refreshed, "data-audio-clip-id")
		pending, err := lib.LoadMediaManifest(dir)
		require.NoError(t, err)

		pub.add("clips", map[string]any{"audience": "only_paid", "body_html": "<p>Subscriber text</p>" + audioClipEmbed("clip-a")})
		out, err = run(t, pub.URL, dir, flags...)
		require.NoError(t, err, out)
		repaired := read(t, path)
		require.True(t, strings.HasPrefix(repaired, refreshed), "bulk repair must retain the refreshed HTML")
		require.Equal(t, 1, strings.Count(repaired, `data-audio-clip-id="clip-a"`))
		require.Contains(t, repaired, `<figcaption>Audio clip</figcaption>`)
		require.Contains(t, repaired, `src="audio-clips/`+stem+`clips_audio-clip-a.mp3"`)
		require.False(t, pending.IsComplete("clips", lib.MediaAudioClips), "an HTML refresh makes existing clips pending")
		require.Equal(t, []string{"audio-clips/" + filepath.Base(clipPath(dir, "clip-a"))}, (*pending.Posts["clips"])[lib.MediaAudioClips].Files)
		require.Equal(t, 1, pub.count("/cdn/audio.mp3"), "bulk repair reuses the original download")
		require.True(t, complete(t, dir, "clips"))
	})

	t.Run("a failed clip keeps the post pending", func(t *testing.T) {
		pub := newMediaCLIPublication(t)
		pub.add("clips", map[string]any{"body_html": body})
		pub.setFailure("/api/v1/audio/upload/clip-b/src")
		dir := t.TempDir()
		out, err := run(t, pub.URL, dir, "--download-audio-clips")
		require.Error(t, err, out)
		require.Equal(t, "audio bytes", read(t, clipPath(dir, "clip-a")))
		require.False(t, complete(t, dir, "clips"))

		pub.setFailure("")
		out, err = run(t, pub.URL, dir, "--download-audio-clips")
		require.NoError(t, err, out)
		require.Equal(t, "audio bytes", read(t, clipPath(dir, "clip-b")))
		require.Equal(t, 1, pub.count("/api/v1/audio/upload/clip-a/src"), "downloaded clip is not fetched again")
		require.True(t, complete(t, dir, "clips"))
	})

	t.Run("help explains the flag", func(t *testing.T) {
		out, err := exec.Command(binary, "download", "--help").CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(out), "--download-audio-clips")
		require.Contains(t, string(out), "--audio-clips-dir")
	})
}
