package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
