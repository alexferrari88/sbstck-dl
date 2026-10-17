package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMediaCacheIntegrity(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "directory", "symlink", "partial-directory", "source-symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "episode")
			target := filepath.Join(dir, "retain")
			require.NoError(t, os.WriteFile(target, []byte("retained"), 0644))
			switch kind {
			case "empty":
				require.NoError(t, os.WriteFile(base+".mp3", nil, 0644))
			case "directory":
				require.NoError(t, os.Mkdir(base+".mp3", 0755))
			case "symlink":
				if err := os.Symlink(target, base+".mp3"); err != nil {
					t.Skip(err)
				}
			case "partial-directory":
				require.NoError(t, os.Mkdir(base+".part", 0755))
			case "source-symlink":
				if err := os.Symlink(target, base+".part.src"); err != nil {
					t.Skip(err)
				}
			}
			m := NewMediaManifest(dir)
			m.Record("episode", MediaPodcast, true, []string{base + ".mp3"})
			require.False(t, m.IsComplete("episode", MediaPodcast))
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = w.Write([]byte("usable audio")) }))
			defer server.Close()
			d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
			result, err := d.download(context.Background(), mediaJob{requestURL: server.URL, basePath: base, exts: []string{".mp3"}})
			if kind == "empty" || kind == "missing" {
				require.NoError(t, err)
				require.Equal(t, int64(12), result.Size)
				require.Equal(t, 1, requests)
				require.True(t, m.IsComplete("episode", MediaPodcast))
				_, err = d.download(context.Background(), mediaJob{requestURL: server.URL, basePath: base, exts: []string{".mp3"}})
				require.NoError(t, err)
				require.Equal(t, 1, requests)
			} else {
				require.ErrorContains(t, err, "not a regular file")
				require.Zero(t, requests)
			}
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "retained", string(data))
		})
	}
}

func TestMediaCDNDoesNotUseCookieJar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Cookie"))
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()
	f := NewFetcher(WithRatePerSecond(1000), WithBurst(100))
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	jar.SetCookies(u, []*http.Cookie{{Name: "subscriber", Value: "dummy"}})
	f.Client.Jar = jar
	_, err = newMediaDownloader(f).download(context.Background(), mediaJob{requestURL: server.URL, basePath: filepath.Join(t.TempDir(), "ep"), exts: []string{".mp3"}})
	require.NoError(t, err)
	require.Len(t, f.Client.Jar.Cookies(u), 1)
}

func TestVideoInvalidCacheRepair(t *testing.T) {
	for _, kind := range []string{"empty", "directory"} {
		t.Run(kind, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "video")
			if kind == "empty" {
				require.NoError(t, os.WriteFile(base+".mp4", nil, 0644))
			} else {
				require.NoError(t, os.Mkdir(base+".mp4", 0755))
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/upload" {
					_, _ = w.Write([]byte(`{"id":"id","duration":0}`))
					return
				}
				if r.URL.Path == "/upload/src" {
					http.Redirect(w, r, "/cdn.mp4", http.StatusFound)
					return
				}
				_, _ = w.Write([]byte("video"))
			}))
			defer server.Close()
			vd := NewVideoDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)), "", "")
			result := vd.downloadVideo(context.Background(), server.URL+"/upload", base)
			if kind == "empty" {
				require.True(t, result.Success)
				require.False(t, result.Existed)
				require.Positive(t, requests)
			} else {
				require.ErrorContains(t, result.Error, "not a regular file")
				require.False(t, result.Success)
				require.Zero(t, requests)
			}
		})
	}
}

func TestMediaEmptyResponseAndCancelledCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	base := filepath.Join(t.TempDir(), "ep")
	d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
	job := mediaJob{requestURL: server.URL, basePath: base, exts: []string{".mp3"}}
	_, err := d.download(context.Background(), job)
	require.ErrorContains(t, err, "empty")
	_, err = os.Lstat(base + ".mp3")
	require.True(t, os.IsNotExist(err))
	require.NoError(t, os.WriteFile(base+".mp3", []byte("cached"), 0644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.download(ctx, job)
	require.ErrorIs(t, err, context.Canceled)
}

func TestEmbedMediaInFileAtomicPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.html")
	original := "<!DOCTYPE html>\n<h1>User title</h1>\n\n<p>Locally edited &amp; retained.</p>\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0600))
	before, err := os.Stat(path)
	require.NoError(t, err)
	media := EmbeddedMedia{Podcast: "podcasts/ep_preview.mp3"}
	failure := errors.New("interrupted publication")
	err = embedMediaInFileWith(path, "", media, func(path string, data []byte) error {
		return writeFileAtomicallyWith(path, func(f *os.File) error { _, err := f.Write(data[:10]); require.NoError(t, err); return failure })
	})
	require.ErrorIs(t, err, failure)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, string(data))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NoError(t, EmbedMediaInFile(path, "", media))
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(first), "<h1>User title</h1>\n\n<audio")
	require.Contains(t, string(first), "<p>Locally edited &amp; retained.</p>\n")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), info.Mode().Perm())
	require.NoError(t, EmbedMediaInFile(path, "", media))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, first, second)
	media.Podcast = "podcasts/ep.mp3"
	require.NoError(t, EmbedMediaInFile(path, "", media))
	full, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(full), "_preview")
	require.Error(t, EmbedMediaInFile(filepath.Join(dir, "absent.html"), "", media))
	require.Error(t, EmbedMediaInFile(dir, "", media))
}

func TestMediaManifestMalformedAndPublication(t *testing.T) {
	for _, input := range []string{`{"version":3,"posts":{}}`, `{"posts":{}}`, `{"version":1,"posts":{"ep":{"podcast":{"complete":true,"files":["../outside.mp3"]}}}}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, MediaManifestFile)
		require.NoError(t, os.WriteFile(path, []byte(input), 0644))
		_, err := LoadMediaManifest(dir)
		require.Error(t, err)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, input, string(data))
	}
	dir := t.TempDir()
	m := NewMediaManifest(dir)
	m.Posts["ep"] = new(MediaRecord)
	m.Record("ep", MediaPodcast, true, nil)
	require.NoError(t, m.Save())
	// A pre-existing fixed temporary filename is unrelated user data.
	oldTmp := m.path + ".tmp"
	require.NoError(t, os.WriteFile(oldTmp, []byte("retained"), 0644))
	require.NoError(t, m.Save())
	data, err := os.ReadFile(oldTmp)
	require.NoError(t, err)
	require.Equal(t, "retained", string(data))
	require.NoError(t, os.Remove(m.path))
	require.NoError(t, os.Mkdir(m.path, 0755))
	require.Error(t, m.Save())
	info, err := os.Lstat(m.path)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestMediaQuerySelectedSourceRestartsPartial(t *testing.T) {
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		require.Equal(t, "B", r.URL.Query().Get("episode"))
		if r.Header.Get("Range") == "bytes=4-" {
			w.Header().Set("Content-Range", "bytes 4-9/10")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("BBBBBB"))
			return
		}
		_, _ = w.Write([]byte("BBBBBBBBBB"))
	}))
	defer server.Close()
	part := filepath.Join(t.TempDir(), "episode.part")
	require.NoError(t, os.WriteFile(part, []byte("AAAA"), 0644))
	require.NoError(t, os.WriteFile(part+".src", []byte(mediaSourceKey(server.URL+"/audio.mp3?episode=A&token=dummy-secret")), 0644))
	sourceB := server.URL + "/audio.mp3?episode=B&token=dummy-secret"
	d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
	_, err := d.fetchToFile(context.Background(), sourceB, part)
	require.NoError(t, err)
	data, err := os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, "BBBBBBBBBB", string(data))
	require.Equal(t, []string{""}, ranges)
	identity, err := os.ReadFile(part + ".src")
	require.NoError(t, err)
	require.Equal(t, mediaSourceKey(sourceB), readMediaSource(part+".src").SourceDigest)
	require.NotContains(t, string(identity), "dummy-secret")
}

func TestVideoSharedUUIDPrefixDownloadsDistinctFiles(t *testing.T) {
	ids := []string{"12345678-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "12345678-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/video/upload/") {
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/video/upload/"), "/src")
			if strings.HasSuffix(r.URL.Path, "/src") {
				http.Redirect(w, r, "/cdn/"+id+".mp4", http.StatusFound)
				return
			}
			_, _ = fmt.Fprint(w, `{"duration":0}`)
			return
		}
		_, _ = fmt.Fprint(w, r.URL.Path)
	}))
	defer server.Close()
	dir := t.TempDir()
	vd := NewVideoDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)), dir, "videos")
	post := Post{CanonicalUrl: server.URL + "/p/episode", BodyHTML: nativeVideoEmbed(ids[0]) + nativeVideoEmbed(ids[1])}
	r, err := vd.DownloadVideos(context.Background(), post, "episode")
	require.NoError(t, err)
	require.Equal(t, 2, r.Success)
	require.NotEqual(t, r.Videos[0].LocalPath, r.Videos[1].LocalPath)
	for i, video := range r.Videos {
		require.False(t, video.Existed)
		require.Equal(t, "episode_video-"+ids[i]+".mp4", filepath.Base(video.LocalPath))
		data, err := os.ReadFile(video.LocalPath)
		require.NoError(t, err)
		require.Equal(t, "/cdn/"+ids[i]+".mp4", string(data))
	}
	r, err = vd.DownloadVideos(context.Background(), post, "episode")
	require.NoError(t, err)
	require.True(t, r.Videos[0].Existed)
	require.True(t, r.Videos[1].Existed)
}

func TestMediaManifestLegacyCompletionRequiresOutputRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MediaManifestFile)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "episode.mp3"), []byte("cached audio"), 0644))
	legacy := `{"version":1,"posts":{"episode":{"podcast":{"complete":true,"files":["episode.mp3"]},"videos":{"complete":true}},"nil":null}}`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0644))
	m, err := LoadMediaManifest(dir)
	require.NoError(t, err)
	require.Equal(t, 2, m.Version)
	require.False(t, m.IsComplete("episode", MediaPodcast))
	require.False(t, m.IsComplete("episode", MediaVideos))
	require.Equal(t, []string{"episode.mp3"}, (*m.Posts["episode"])[MediaPodcast].Files)
	require.NoError(t, m.Save())
	m, err = LoadMediaManifest(dir)
	require.NoError(t, err)
	require.False(t, m.IsComplete("episode", MediaPodcast))
	m.Record("episode", MediaPodcast, true, []string{filepath.Join(dir, "episode.mp3")})
	require.NoError(t, m.Save())
	m, err = LoadMediaManifest(dir)
	require.NoError(t, err)
	require.True(t, m.IsComplete("episode", MediaPodcast))
}

func TestMediaChangedETagAfterInterruptedResponse(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	var mu sync.Mutex
	var ifRange, rangeHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if version.Load() == 1 {
			w.Header().Set("ETag", `"version-A"`)
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("AAAA"))
			return
		}
		mu.Lock()
		ifRange, rangeHeader = r.Header.Get("If-Range"), r.Header.Get("Range")
		mu.Unlock()
		w.Header().Set("ETag", `"version-B"`)
		http.ServeContent(w, r, "audio.mp3", time.Time{}, bytes.NewReader([]byte("BBBBBBBBBB")))
	}))
	defer server.Close()
	part := filepath.Join(t.TempDir(), "episode.part")
	d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := d.fetchToFile(ctx, server.URL+"/audio.mp3", part)
	require.Error(t, err)
	data, err := os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, "AAAA", string(data))
	require.Equal(t, `"version-A"`, readMediaSource(part+".src").ETag)
	version.Store(2)
	_, err = d.fetchToFile(context.Background(), server.URL+"/audio.mp3", part)
	require.NoError(t, err)
	mu.Lock()
	require.Equal(t, "bytes=4-", rangeHeader)
	require.Equal(t, `"version-A"`, ifRange)
	mu.Unlock()
	data, err = os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, "BBBBBBBBBB", string(data))
	require.Equal(t, `"version-B"`, readMediaSource(part+".src").ETag)
}

func TestMediaUnverifiablePartialsRestart(t *testing.T) {
	for _, validator := range []string{"missing", "weak", "legacy-digest", "legacy-url"} {
		t.Run(validator, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Empty(t, r.Header.Get("Range"))
				require.Empty(t, r.Header.Get("If-Range"))
				_, _ = w.Write([]byte("BBBBBBBBBB"))
			}))
			defer server.Close()
			part := filepath.Join(t.TempDir(), "ep.part")
			source := server.URL + "/audio.mp3?token=dummy-secret"
			require.NoError(t, os.WriteFile(part, []byte("AAAA"), 0644))
			metadata := mediaSource{SourceDigest: mediaSourceKey(source)}
			if validator == "weak" {
				metadata.ETag = `W/"version-A"`
			}
			record, err := json.Marshal(metadata)
			require.NoError(t, err)
			if validator == "legacy-digest" {
				record = []byte(mediaSourceKey(source))
			}
			if validator == "legacy-url" {
				record = []byte(source)
			}
			require.NoError(t, os.WriteFile(part+".src", record, 0644))
			d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
			_, err = d.fetchToFile(context.Background(), source, part)
			require.NoError(t, err)
			data, err := os.ReadFile(part)
			require.NoError(t, err)
			require.Equal(t, "BBBBBBBBBB", string(data))
			record, err = os.ReadFile(part + ".src")
			require.NoError(t, err)
			require.NotContains(t, string(record), "dummy-secret")
		})
	}
}

func TestMediaRepeatedTruncatedPrefixHasFiniteRetries(t *testing.T) {
	for _, validator := range []string{"missing", "strong-but-range-ignored"} {
		t.Run(validator, func(t *testing.T) {
			fastMediaRetries(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if validator != "missing" {
					w.Header().Set("ETag", `"same-prefix"`)
				}
				w.Header().Set("Content-Length", "10")
				_, _ = w.Write([]byte("AAAA"))
			}))
			defer server.Close()
			d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
			// A background context proves termination comes from the retry budget.
			_, err := d.fetchToFile(context.Background(), server.URL, filepath.Join(t.TempDir(), "ep.part"))
			require.Error(t, err)
			require.LessOrEqual(t, requests.Load(), int32(maxMediaRetries+2))
		})
	}
}

func TestMediaAdvancingValidatedChunksContinueBeyondRetryBudget(t *testing.T) {
	fastMediaRetries(t)
	content := []byte("abcdefghijklmnopqrst")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		offset := 0
		w.Header().Set("ETag", `"stable-content"`)
		if r.Header.Get("Range") != "" {
			_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &offset)
			require.NoError(t, err)
			require.Equal(t, `"stable-content"`, r.Header.Get("If-Range"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(content)-offset))
		if offset > 0 {
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(content[offset : offset+2])
	}))
	defer server.Close()
	part := filepath.Join(t.TempDir(), "ep.part")
	d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
	_, err := d.fetchToFile(context.Background(), server.URL, part)
	require.NoError(t, err)
	require.Greater(t, requests.Load(), int32(maxMediaRetries))
	data, err := os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestMediaFailedRestartDoesNotRelabelRetainedPrefix(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if version.Load() == 1 {
			w.Header().Set("ETag", `"version-A"`)
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("AAAA"))
			return
		}
		// Both the failed overwrite and its retry must still validate version A.
		require.Equal(t, `"version-A"`, r.Header.Get("If-Range"))
		w.Header().Set("ETag", `"version-B"`)
		http.ServeContent(w, r, "ep.mp3", time.Time{}, bytes.NewReader([]byte("BBBBBBBBBB")))
	}))
	defer server.Close()
	part := filepath.Join(t.TempDir(), "ep.part")
	d := newMediaDownloader(NewFetcher(WithRatePerSecond(1000), WithBurst(100)))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := d.fetchToFile(ctx, server.URL, part)
	require.Error(t, err)
	require.NoError(t, os.Chmod(part, 0444))
	defer os.Chmod(part, 0644)
	probe, probeErr := os.OpenFile(part, os.O_WRONLY, 0644)
	if probeErr == nil {
		probe.Close()
		t.Skip("filesystem permits writes to readonly files")
	}
	version.Store(2)
	_, err = d.fetchToFile(context.Background(), server.URL, part)
	require.Error(t, err)
	data, err := os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, "AAAA", string(data))
	require.Equal(t, `"version-A"`, readMediaSource(part+".src").ETag)
	require.NoError(t, os.Chmod(part, 0644))
	_, err = d.fetchToFile(context.Background(), server.URL, part)
	require.NoError(t, err)
	data, err = os.ReadFile(part)
	require.NoError(t, err)
	require.Equal(t, "BBBBBBBBBB", string(data))
	require.Equal(t, `"version-B"`, readMediaSource(part+".src").ETag)
}
