package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nativeVideoEmbed(uploadID string) string {
	return fmt.Sprintf(`<div class="native-video-embed" data-component-name="VideoPlaceholder" `+
		`data-attrs="{&quot;mediaUploadId&quot;:&quot;%s&quot;,&quot;duration&quot;:null}"></div>`, uploadID)
}

func TestVideoRefs(t *testing.T) {
	const (
		embed1 = "d162532c-e6f4-486d-b168-406f015f78dd"
		embed2 = "a2517b10-9e08-4e06-996d-11ee246d373f"
	)

	t.Run("post video and embeds", func(t *testing.T) {
		post := Post{
			VideoUploadId: "post-video",
			BodyHTML: "<p>intro</p>" + nativeVideoEmbed(embed1) +
				"<p>middle</p>" + nativeVideoEmbed(embed2) +
				nativeVideoEmbed(embed1) + // duplicates are skipped
				nativeVideoEmbed("post-video") +
				`<div class="native-video-embed" data-attrs="not json"></div>` +
				`<div class="youtube-wrap"><iframe src="https://www.youtube.com/embed/x"></iframe></div>`,
		}

		refs := post.videoRefs("base")
		assert.Equal(t, []videoRef{
			{uploadID: "post-video", name: "base"},
			{uploadID: embed1, name: "base_video-d162532c"},
			{uploadID: embed2, name: "base_video-a2517b10"},
		}, refs)
	})

	t.Run("names don't depend on order", func(t *testing.T) {
		before := Post{BodyHTML: nativeVideoEmbed(embed1) + nativeVideoEmbed(embed2)}
		after := Post{BodyHTML: nativeVideoEmbed(embed2) + "<p>new</p>" + nativeVideoEmbed(embed1)}

		names := func(p Post) map[string]string {
			m := map[string]string{}
			for _, r := range p.videoRefs("base") {
				m[r.uploadID] = r.name
			}
			return m
		}
		assert.Equal(t, names(before), names(after))
	})

	t.Run("unsafe upload IDs are rejected", func(t *testing.T) {
		post := Post{
			VideoUploadId: "../../etc/passwd",
			BodyHTML: nativeVideoEmbed("../escape") + nativeVideoEmbed("a/b") +
				nativeVideoEmbed("x?y=1") + nativeVideoEmbed(""),
		}
		assert.Empty(t, post.videoRefs("base"))
	})

	t.Run("no videos", func(t *testing.T) {
		post := Post{BodyHTML: "<p>just text</p>"}
		assert.Empty(t, post.videoRefs("base"))
	})
}

func TestPostVideoJSONParsing(t *testing.T) {
	raw := `{"post":{"slug":"ep","video_upload_id":"52eeec6b","videoUpload":{"id":"52eeec6b"}}}`
	var wrapper PostWrapper
	require.NoError(t, json.Unmarshal([]byte(raw), &wrapper))
	assert.Equal(t, "52eeec6b", wrapper.Post.VideoUploadId)
}

func TestResolveRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "https://cdn.example.com/video.mp4?token=abc", http.StatusTemporaryRedirect)
		case "/direct":
			w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := NewFetcher()
	ctx := context.Background()

	got, err := fetcher.ResolveRedirect(ctx, server.URL+"/redirect")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/video.mp4?token=abc", got)

	got, err = fetcher.ResolveRedirect(ctx, server.URL+"/direct")
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/direct", got)

	_, err = fetcher.ResolveRedirect(ctx, server.URL+"/missing")
	var fetchErr *FetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, http.StatusNotFound, fetchErr.StatusCode)
}

// fakeSubstack mimics Substack's video endpoints: /api/v1/video/upload/<id>
// returns metadata, /src redirects to the full video for subscribers (cookie
// present) and to the preview otherwise, and /cdn/ serves the files.
type fakeSubstack struct {
	*httptest.Server
	mu         sync.Mutex
	cdnCookies []string
	requests   int
}

func newFakeSubstack(t *testing.T) *fakeSubstack {
	f := &fakeSubstack{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests++
		f.mu.Unlock()

		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/cdn/"):
			f.mu.Lock()
			f.cdnCookies = append(f.cdnCookies, r.Header.Get("Cookie"))
			f.mu.Unlock()
			w.Header().Set("Content-Type", "video/mp4")
			fmt.Fprintf(w, "mp4:%s", strings.TrimPrefix(path, "/cdn/"))
		case strings.HasPrefix(path, "/api/v1/video/upload/"):
			rest := strings.TrimPrefix(path, "/api/v1/video/upload/")
			id, isSrc := strings.CutSuffix(rest, "/src")
			if id == "missing" {
				http.NotFound(w, r)
				return
			}
			previewID := ""
			if strings.HasPrefix(id, "paid") {
				previewID = "preview-" + id
			}
			if !isSrc {
				json.NewEncoder(w).Encode(map[string]any{
					"id": id, "duration": 100, "mux_preview_playback_id": nullable(previewID),
				})
				return
			}
			playbackID := "full-" + id
			if _, err := r.Cookie("substack.sid"); err != nil && previewID != "" {
				playbackID = previewID
			}
			http.Redirect(w, r, f.URL+"/cdn/"+playbackID+"/high.mp4?token=t", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestDownloadVideos(t *testing.T) {
	ctx := context.Background()
	cookie := &http.Cookie{Name: "substack.sid", Value: "secret"}

	t.Run("post video and embed, with cookie", func(t *testing.T) {
		server := newFakeSubstack(t)
		outputDir := t.TempDir()
		vd := NewVideoDownloader(NewFetcher(WithCookie(cookie)), outputDir, "videos")

		post := Post{
			CanonicalUrl:  server.URL + "/p/ep",
			VideoUploadId: "paid-main",
			BodyHTML:      nativeVideoEmbed("free-embed"),
		}
		result, err := vd.DownloadVideos(ctx, post, "20250101_ep")
		require.NoError(t, err)
		assert.Equal(t, 2, result.Success)
		assert.Equal(t, 0, result.Failed)

		main := result.Videos[0]
		assert.Equal(t, "paid-main", main.UploadID)
		assert.False(t, main.Preview)
		assert.Equal(t, filepath.Join(outputDir, "videos", "20250101_ep.mp4"), main.LocalPath)
		data, err := os.ReadFile(main.LocalPath)
		require.NoError(t, err)
		assert.Equal(t, "mp4:full-paid-main/high.mp4", string(data))

		embed := result.Videos[1]
		assert.Equal(t, filepath.Join(outputDir, "videos", "20250101_ep_video-free-embed.mp4"), embed.LocalPath)
		assert.FileExists(t, embed.LocalPath)

		// The subscriber cookie must never be sent to the CDN
		assert.Equal(t, []string{"", ""}, server.cdnCookies)

		// A second run makes no requests
		server.requests = 0
		result, err = vd.DownloadVideos(ctx, post, "20250101_ep")
		require.NoError(t, err)
		assert.True(t, result.Videos[0].Existed)
		assert.True(t, result.Videos[1].Existed)
		assert.Equal(t, 0, server.requests)
	})

	t.Run("paid video without cookie saved as preview", func(t *testing.T) {
		server := newFakeSubstack(t)
		outputDir := t.TempDir()
		vd := NewVideoDownloader(nil, outputDir, "clips")

		post := Post{CanonicalUrl: server.URL + "/p/ep", VideoUploadId: "paid-main"}
		result, err := vd.DownloadVideos(ctx, post, "ep")
		require.NoError(t, err)
		require.Len(t, result.Videos, 1)

		video := result.Videos[0]
		assert.True(t, video.Preview)
		assert.Equal(t, filepath.Join(outputDir, "clips", "ep_preview.mp4"), video.LocalPath)
		data, err := os.ReadFile(video.LocalPath)
		require.NoError(t, err)
		assert.Equal(t, "mp4:preview-paid-main/high.mp4", string(data))

		// Running again with a cookie fetches the full video alongside the preview
		vd = NewVideoDownloader(NewFetcher(WithCookie(cookie)), outputDir, "clips")
		result, err = vd.DownloadVideos(ctx, post, "ep")
		require.NoError(t, err)
		assert.False(t, result.Videos[0].Preview)
		assert.False(t, result.Videos[0].Existed)
		assert.FileExists(t, filepath.Join(outputDir, "clips", "ep.mp4"))
	})

	t.Run("failed video does not stop the others", func(t *testing.T) {
		server := newFakeSubstack(t)
		outputDir := t.TempDir()
		vd := NewVideoDownloader(nil, outputDir, "videos")

		post := Post{
			CanonicalUrl: server.URL + "/p/ep",
			BodyHTML:     nativeVideoEmbed("missing") + nativeVideoEmbed("free-ok"),
		}
		result, err := vd.DownloadVideos(ctx, post, "ep")
		require.NoError(t, err)
		assert.Equal(t, 1, result.Success)
		assert.Equal(t, 1, result.Failed)
		assert.Error(t, result.Videos[0].Error)
		assert.True(t, result.Videos[1].Success)
		assert.NoFileExists(t, filepath.Join(outputDir, "videos", "ep_video-missing.mp4"))
		assert.FileExists(t, filepath.Join(outputDir, "videos", "ep_video-free-ok.mp4"))
	})

	t.Run("post without videos", func(t *testing.T) {
		outputDir := t.TempDir()
		vd := NewVideoDownloader(nil, outputDir, "videos")

		result, err := vd.DownloadVideos(ctx, Post{BodyHTML: "<p>text</p>"}, "ep")
		require.NoError(t, err)
		assert.Empty(t, result.Videos)
		assert.NoDirExists(t, filepath.Join(outputDir, "videos"))
	})
}

func TestVideoDownloadResultSources(t *testing.T) {
	dir := filepath.Join("out", "dir")
	result := &VideoDownloadResult{Videos: []VideoInfo{
		{UploadID: "a", LocalPath: filepath.Join(dir, "videos", "2025 post#1.mp4"), Success: true},
		{UploadID: "b", LocalPath: filepath.Join(dir, "videos", "failed.mp4"), Success: false},
	}}
	assert.Equal(t, map[string]string{"a": "videos/2025%20post%231.mp4"}, result.Sources(dir))
}
