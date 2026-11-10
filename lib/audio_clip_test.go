package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func audioClipPlaceholder(id string, duration float64) string {
	return fmt.Sprintf(`<div class="native-audio-embed" data-attrs="{&quot;mediaUploadId&quot;:&quot;%s&quot;,&quot;duration&quot;:%g}"></div>`, id, duration)
}

func TestAudioClipRefs(t *testing.T) {
	post := Post{BodyHTML: audioClipPlaceholder("a-1", 12) + audioClipPlaceholder("../evil", 1) +
		audioClipPlaceholder("b-2", 30) + audioClipPlaceholder("a-1", 12) +
		`<div class="native-video-embed" data-attrs="{&quot;mediaUploadId&quot;:&quot;v-1&quot;}"></div>`}
	refs := post.audioClipRefs("base")
	require.Equal(t, []audioClipRef{{uploadID: "a-1", name: "base_audio-a-1", duration: 12}, {uploadID: "b-2", name: "base_audio-b-2", duration: 30}}, refs)
}

func TestDownloadAudioClips(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/v1/audio/upload/a-1/src":
			http.Redirect(w, r, "/cdn/a-1.mp3", http.StatusTemporaryRedirect)
		case "/cdn/a-1.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("clip bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	post := Post{BodyHTML: audioClipPlaceholder("a-1", 0) + audioClipPlaceholder("gone", 0)}
	downloader := NewAudioClipDownloader(NewFetcher(WithRatePerSecond(1000)), dir, "clips")

	result, err := downloader.DownloadAudioClips(context.Background(), post, "base", origin)
	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, result.Failed)
	data, err := os.ReadFile(filepath.Join(dir, "clips", "base_audio-a-1.mp3"))
	require.NoError(t, err)
	require.Equal(t, "clip bytes", string(data))
	require.Equal(t, map[string]string{"a-1": "clips/base_audio-a-1.mp3"}, result.Sources(dir))

	// Existing files are not requested again.
	before := requests.Load()
	result, err = downloader.DownloadAudioClips(context.Background(), Post{BodyHTML: audioClipPlaceholder("a-1", 0)}, "base", origin)
	require.NoError(t, err)
	require.True(t, result.Clips[0].Existed)
	require.Equal(t, before, requests.Load())
}

func TestEmbedAudioClips(t *testing.T) {
	body := "<p>x</p>" + audioClipPlaceholder("a-1", 0) + audioClipPlaceholder("b-2", 0)
	embedded := EmbedMedia(body, "", EmbeddedMedia{AudioClips: map[string]string{"a-1": "clips/a.mp3"}})
	require.Contains(t, embedded, `<audio data-audio-clip-id="a-1" controls preload="metadata" src="clips/a.mp3"`)
	require.Contains(t, embedded, `mediaUploadId&quot;:&quot;b-2`, "clips without a file keep their placeholder")
	// Embedding again updates the player in place.
	again := EmbedMedia(embedded, "", EmbeddedMedia{AudioClips: map[string]string{"a-1": "clips/a2.mp3"}})
	require.Contains(t, again, `src="clips/a2.mp3"`)
	require.NotContains(t, again, `src="clips/a.mp3"`)
}
