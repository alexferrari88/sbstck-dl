package lib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPodcastData = []byte("ID3 fake mp3 audio data for podcast download testing")

func TestPostPodcastJSONParsing(t *testing.T) {
	// Trimmed-down shape of a paid episode as served to a non-subscriber
	raw := `{"post":{"slug":"ep","type":"podcast",
		"podcast_url":"https://api.substack.com/api/v1/audio/upload/abc/src",
		"podcast_duration":798.8,
		"podcastUpload":{"id":"abc","duration":798.8,"is_free_preview":true,
			"full_podcast_info":{"media_upload_id":"def","duration":3717.9}}}}`

	var wrapper PostWrapper
	require.NoError(t, json.Unmarshal([]byte(raw), &wrapper))
	post := wrapper.Post

	assert.True(t, post.HasPodcast())
	assert.Equal(t, "https://api.substack.com/api/v1/audio/upload/abc/src", post.PodcastUrl)
	assert.InDelta(t, 798.8, post.PodcastDuration, 0.01)
	require.NotNil(t, post.PodcastUpload)
	require.NotNil(t, post.PodcastUpload.FullPodcastInfo)
	assert.True(t, post.IsPodcastPreview())
}

func TestIsPodcastUnavailable(t *testing.T) {
	tests := []struct {
		name string
		post Post
		want bool
	}{
		{"paid episode withheld", Post{Type: "podcast", PodcastDuration: 186.5}, true},
		{"podcast type without duration", Post{Type: "podcast"}, true},
		{"duration without podcast type", Post{Type: "newsletter", PodcastDuration: 60}, true},
		{"available episode", Post{Type: "podcast", PodcastUrl: "https://example.com/a.mp3"}, false},
		{"plain newsletter", Post{Type: "newsletter"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.post.IsPodcastUnavailable())
		})
	}
}

func TestIsPodcastPreview(t *testing.T) {
	tests := []struct {
		name   string
		upload *PodcastUpload
		want   bool
	}{
		{"no upload metadata", nil, false},
		{"full episode", &PodcastUpload{Duration: 3000}, false},
		{
			"truncated preview",
			&PodcastUpload{Duration: 800, IsFreePreview: true, FullPodcastInfo: &FullPodcastInfo{Duration: 3700}},
			true,
		},
		{
			// Substack flags some full-length free versions as previews
			"preview flag but full length",
			&PodcastUpload{Duration: 5630.38, IsFreePreview: true, FullPodcastInfo: &FullPodcastInfo{Duration: 5630.38}},
			false,
		},
		{"preview flag without full info", &PodcastUpload{Duration: 800, IsFreePreview: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post := Post{PodcastUrl: "https://example.com/a.mp3", PodcastUpload: tt.upload}
			assert.Equal(t, tt.want, post.IsPodcastPreview())
		})
	}
}

func TestPodcastRequestURL(t *testing.T) {
	tests := []struct {
		name       string
		podcastURL string
		postURL    string
		want       string
		resolve    bool
	}{
		{
			"api.substack.com rewritten to custom domain",
			"https://api.substack.com/api/v1/audio/upload/abc/src",
			"https://www.example.com/p/episode",
			"https://www.example.com/api/v1/audio/upload/abc/src",
			true,
		},
		{
			"api.substack.com rewritten to substack subdomain",
			"https://api.substack.com/api/v1/audio/upload/abc/src",
			"https://pub.substack.com/p/episode",
			"https://pub.substack.com/api/v1/audio/upload/abc/src",
			true,
		},
		{
			"other hosts left alone and requested without cookie",
			"https://cdn.example.net/episode.mp3",
			"https://pub.substack.com/p/episode",
			"https://cdn.example.net/episode.mp3",
			false,
		},
		{
			"missing canonical url left alone",
			"https://api.substack.com/api/v1/audio/upload/abc/src",
			"",
			"https://api.substack.com/api/v1/audio/upload/abc/src",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post := Post{PodcastUrl: tt.podcastURL, CanonicalUrl: tt.postURL}
			got, resolve := podcastRequestURL(post)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.resolve, resolve)
		})
	}
}

func TestNewPodcastDownloaderDisablesClientTimeout(t *testing.T) {
	fetcher := NewFetcher(WithTimeout(5 * time.Second))
	pd := NewPodcastDownloader(fetcher, "out", "podcasts")

	assert.Equal(t, time.Duration(0), pd.media.cdn.Timeout)
	// The shared fetcher must not be modified
	assert.Equal(t, 5*time.Second, fetcher.Client.Timeout)
	assert.Same(t, fetcher, pd.media.fetcher)
}

func TestDownloadPodcast(t *testing.T) {
	var requests int
	var gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if c, err := r.Cookie("substack.sid"); err == nil {
			gotCookie = c.Value
		}
		switch r.URL.Path {
		case "/api/v1/audio/upload/abc/src":
			// Mimic Substack redirecting to the signed CDN URL
			http.Redirect(w, r, "/cdn/transcoded.mp3", http.StatusTemporaryRedirect)
		case "/cdn/transcoded.mp3":
			w.Header().Set("Content-Type", "binary/octet-stream")
			w.Write(testPodcastData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	newPost := func() Post {
		return Post{
			Slug:         "episode",
			CanonicalUrl: server.URL + "/p/episode",
			PodcastUrl:   server.URL + "/api/v1/audio/upload/abc/src",
		}
	}

	t.Run("downloads audio and follows redirect", func(t *testing.T) {
		requests = 0
		outputDir := t.TempDir()
		fetcher := NewFetcher(WithCookie(&http.Cookie{Name: "substack.sid", Value: "secret"}))
		pd := NewPodcastDownloader(fetcher, outputDir, "podcasts")

		result, err := pd.DownloadPodcast(context.Background(), newPost(), "20250101_000000_episode")
		require.NoError(t, err)
		require.NotNil(t, result)

		expectedPath := filepath.Join(outputDir, "podcasts", "20250101_000000_episode.mp3")
		assert.Equal(t, expectedPath, result.LocalPath)
		assert.Equal(t, int64(len(testPodcastData)), result.Size)
		assert.False(t, result.Preview)
		assert.False(t, result.Existed)
		assert.Equal(t, "secret", gotCookie)

		data, err := os.ReadFile(expectedPath)
		require.NoError(t, err)
		assert.Equal(t, testPodcastData, data)

		_, err = os.Stat(expectedPath + ".part")
		assert.True(t, os.IsNotExist(err), "temporary file should be gone")

		// A second run skips the download
		requests = 0
		result, err = pd.DownloadPodcast(context.Background(), newPost(), "20250101_000000_episode")
		require.NoError(t, err)
		assert.True(t, result.Existed)
		assert.Equal(t, int64(len(testPodcastData)), result.Size)
		assert.Equal(t, 0, requests)
	})

	t.Run("preview saved under separate name", func(t *testing.T) {
		outputDir := t.TempDir()
		pd := NewPodcastDownloader(nil, outputDir, "audio")

		post := newPost()
		post.PodcastUpload = &PodcastUpload{
			Duration: 800, IsFreePreview: true, FullPodcastInfo: &FullPodcastInfo{Duration: 3700},
		}

		result, err := pd.DownloadPodcast(context.Background(), post, "ep")
		require.NoError(t, err)
		assert.True(t, result.Preview)
		assert.Equal(t, filepath.Join(outputDir, "audio", "ep_preview.mp3"), result.LocalPath)
		assert.FileExists(t, result.LocalPath)
	})

	t.Run("post without podcast", func(t *testing.T) {
		outputDir := t.TempDir()
		pd := NewPodcastDownloader(nil, outputDir, "podcasts")

		result, err := pd.DownloadPodcast(context.Background(), Post{Slug: "text-only"}, "text-only")
		require.NoError(t, err)
		assert.Nil(t, result)

		_, err = os.Stat(filepath.Join(outputDir, "podcasts"))
		assert.True(t, os.IsNotExist(err), "no directory should be created")
	})

	t.Run("http error leaves no file", func(t *testing.T) {
		outputDir := t.TempDir()
		pd := NewPodcastDownloader(nil, outputDir, "podcasts")

		post := newPost()
		post.PodcastUrl = server.URL + "/missing.mp3"

		result, err := pd.DownloadPodcast(context.Background(), post, "ep")
		assert.Error(t, err)
		assert.Nil(t, result)

		entries, _ := os.ReadDir(filepath.Join(outputDir, "podcasts"))
		assert.Empty(t, entries)
	})
}
