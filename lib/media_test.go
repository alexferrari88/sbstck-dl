package lib

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mp4Box builds an MP4 box with a 32-bit size.
func mp4Box(boxType string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	box := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(box, uint32(8+len(body)))
	copy(box[4:], boxType)
	return append(box, body...)
}

// buildMP4 builds a minimal MP4 file with the given duration, with the moov
// box after the media data like non-faststart files.
func buildMP4(seconds float64, version byte) []byte {
	const timescale = 1000
	var mvhd bytes.Buffer
	mvhd.Write([]byte{version, 0, 0, 0})
	if version == 1 {
		binary.Write(&mvhd, binary.BigEndian, struct {
			Created, Modified uint64
			Timescale         uint32
			Duration          uint64
		}{0, 0, timescale, uint64(seconds * timescale)})
	} else {
		binary.Write(&mvhd, binary.BigEndian, struct {
			Created, Modified, Timescale, Duration uint32
		}{0, 0, timescale, uint32(seconds * timescale)})
	}
	mvhd.Write(make([]byte, 80))

	// A 64-bit sized mdat box exercises the large-size path
	media := bytes.Repeat([]byte{0xAB}, 4096)
	mdat := make([]byte, 16, 16+len(media))
	binary.BigEndian.PutUint32(mdat, 1)
	copy(mdat[4:], "mdat")
	binary.BigEndian.PutUint64(mdat[8:], uint64(16+len(media)))
	mdat = append(mdat, media...)

	return bytes.Join([][]byte{
		mp4Box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41")),
		mdat,
		mp4Box("moov", mp4Box("mvhd", mvhd.Bytes())),
	}, nil)
}

// mp3Header is an MPEG 1 Layer III frame header: 128 kbps, 44.1 kHz, stereo.
var mp3Header = []byte{0xFF, 0xFB, 0x90, 0x00}

const mp3FrameLen = 417 // 144 * 128000 / 44100

// buildMP3 builds a constant bitrate MP3 file of the given number of frames,
// preceded by an ID3v2 tag. If xingFrames > 0 the first frame carries a Xing
// header with that frame count.
func buildMP3(frames int, xingFrames uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 20})
	buf.Write(make([]byte, 20))
	for i := 0; i < frames; i++ {
		frame := make([]byte, mp3FrameLen)
		copy(frame, mp3Header)
		if i == 0 && xingFrames > 0 {
			copy(frame[36:], "Xing")
			binary.BigEndian.PutUint32(frame[40:], 1) // frames field present
			binary.BigEndian.PutUint32(frame[44:], xingFrames)
		}
		buf.Write(frame)
	}
	return buf.Bytes()
}

func writeTemp(t *testing.T, name string, data []byte) string {
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0644))
	return path
}

func TestMediaDuration(t *testing.T) {
	t.Run("mp4 version 0", func(t *testing.T) {
		d, ok := mediaDuration(writeTemp(t, "a.mp4", buildMP4(123.5, 0)), ".mp4")
		require.True(t, ok)
		assert.InDelta(t, 123.5, d, 0.001)
	})

	t.Run("mp4 version 1", func(t *testing.T) {
		d, ok := mediaDuration(writeTemp(t, "a.mp4", buildMP4(4123.886, 1)), ".mp4")
		require.True(t, ok)
		assert.InDelta(t, 4123.886, d, 0.001)
	})

	t.Run("mp3 constant bitrate", func(t *testing.T) {
		d, ok := mediaDuration(writeTemp(t, "a.mp3", buildMP3(1000, 0)), ".mp3")
		require.True(t, ok)
		assert.InDelta(t, 1000*1152/44100.0, d, 0.5)
	})

	t.Run("mp3 with Xing frame count", func(t *testing.T) {
		d, ok := mediaDuration(writeTemp(t, "a.mp3", buildMP3(10, 34453)), ".mp3")
		require.True(t, ok)
		assert.InDelta(t, 34453*1152/44100.0, d, 0.001)
	})

	t.Run("unrecognized data", func(t *testing.T) {
		_, ok := mediaDuration(writeTemp(t, "a.mp4", []byte("not a video")), ".mp4")
		assert.False(t, ok)
		_, ok = mediaDuration(writeTemp(t, "a.mp3", []byte("ID3 not audio")), ".mp3")
		assert.False(t, ok)
		_, ok = mediaDuration(writeTemp(t, "a.ogg", buildMP3(10, 0)), ".ogg")
		assert.False(t, ok)
	})
}

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		header       string
		start, total int64
		ok           bool
	}{
		{"bytes 100-199/1000", 100, 1000, true},
		{"bytes 0-99/*", 0, -1, true},
		{"bytes */1000", -1, 1000, true},
		{"items 0-1/2", 0, 0, false},
		{"bytes 100-199", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tt := range tests {
		start, total, ok := parseContentRange(tt.header)
		assert.Equal(t, tt.ok, ok, tt.header)
		if tt.ok {
			assert.Equal(t, tt.start, start, tt.header)
			assert.Equal(t, tt.total, total, tt.header)
		}
	}
}

func TestMediaSourceKey(t *testing.T) {
	first := mediaSourceKey("https://stream.mux.com/abc/high.mp4?token=secret&episode=A")
	assert.NotContains(t, first, "secret")
	assert.Equal(t, first, mediaSourceKey("https://stream.mux.com/abc/high.mp4?token=secret&episode=A"))
	assert.NotEqual(t, first, mediaSourceKey("https://stream.mux.com/abc/high.mp4?token=secret&episode=B"))
	assert.NotEqual(t, first, mediaSourceKey("https://stream.mux.com/abc/high.mp4?token=rotated&episode=A"))
}

// fastMediaRetries makes media download retries immediate for the test.
func fastMediaRetries(t *testing.T) {
	oldBackoff, oldIdle := newMediaBackoff, mediaIdleTimeout
	newMediaBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
	mediaIdleTimeout = 200 * time.Millisecond
	t.Cleanup(func() { newMediaBackoff, mediaIdleTimeout = oldBackoff, oldIdle })
}

// mediaServer serves content at /cdn/file, with /src redirecting there.
// Requests are recorded, and handle can override the response of each
// request to /cdn/file by returning true.
type mediaServer struct {
	*httptest.Server
	mu      sync.Mutex
	content []byte
	ranges  []string
	handle  func(n int, w http.ResponseWriter, r *http.Request) bool
}

func newMediaServer(t *testing.T, content []byte) *mediaServer {
	s := &mediaServer{content: content}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/src":
			http.Redirect(w, r, "/cdn/file?token=signed", http.StatusTemporaryRedirect)
		case "/cdn/file":
			w.Header().Set("ETag", `"fixture-content-v1"`)
			s.mu.Lock()
			s.ranges = append(s.ranges, r.Header.Get("Range"))
			n := len(s.ranges)
			s.mu.Unlock()
			if s.handle != nil && s.handle(n, w, r) {
				return
			}
			http.ServeContent(w, r, "file", time.Time{}, bytes.NewReader(s.content))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *mediaServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func TestMediaDownloadPreviewDetection(t *testing.T) {
	fastMediaRetries(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		served      float64 // duration of the served file
		expected    float64 // full duration according to metadata
		hint        bool
		wantPreview bool
	}{
		{"full file", 100, 100, false, false},
		{"preview missed by metadata caught by duration", 30, 100, false, true},
		{"metadata preview hint overruled by full duration", 100, 100, true, false},
		{"unknown expected duration falls back to hint", 30, 0, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newMediaServer(t, buildMP4(tt.served, 0))
			dir := t.TempDir()
			m := newMediaDownloader(nil)

			result, err := m.download(ctx, mediaJob{
				requestURL:       server.URL + "/src",
				resolve:          true,
				basePath:         filepath.Join(dir, "ep"),
				exts:             []string{".mp4"},
				expectedDuration: tt.expected,
				previewHint:      func(string) bool { return tt.hint },
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantPreview, result.Preview)

			want := filepath.Join(dir, "ep.mp4")
			if tt.wantPreview {
				want = filepath.Join(dir, "ep_preview.mp4")
			}
			assert.Equal(t, want, result.LocalPath)
			data, err := os.ReadFile(want)
			require.NoError(t, err)
			assert.Equal(t, server.content, data)

			entries, _ := os.ReadDir(dir)
			assert.Len(t, entries, 1, "no leftover .part or .src files")
		})
	}
}

func TestMediaDownloadResume(t *testing.T) {
	fastMediaRetries(t)
	ctx := context.Background()
	content := bytes.Repeat([]byte("0123456789"), 10000) // 100 KB
	half := len(content) / 2

	download := func(t *testing.T, server *mediaServer, partPath string) (int64, error) {
		m := newMediaDownloader(nil)
		fetched, err := m.fetchToFile(ctx, server.URL+"/cdn/file?token=signed", partPath)
		if err != nil {
			return 0, err
		}
		return fetched.size, nil
	}

	t.Run("connection dropped mid-download is resumed", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			if n > 1 {
				return false
			}
			// Promise the full length, send half, and drop the connection
			w.Header().Set("Content-Length", "100000")
			w.Write(content[:half])
			return true
		}
		partPath := filepath.Join(t.TempDir(), "f.part")

		size, err := download(t, server, partPath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
		assert.Equal(t, []string{"", "bytes=50000-"}, server.requests())
	})

	t.Run("stalled download is resumed", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			if n > 1 {
				return false
			}
			w.Header().Set("Content-Length", "100000")
			w.Write(content[:half])
			w.(http.Flusher).Flush()
			select { // stall until the client gives up
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return true
		}
		partPath := filepath.Join(t.TempDir(), "f.part")

		size, err := download(t, server, partPath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
		assert.Equal(t, []string{"", "bytes=50000-"}, server.requests())
	})

	t.Run("partial file from an earlier run is resumed", func(t *testing.T) {
		server := newMediaServer(t, content)
		partPath := filepath.Join(t.TempDir(), "f.part")
		require.NoError(t, os.WriteFile(partPath, content[:half], 0644))
		require.NoError(t, os.WriteFile(partPath+".src", mediaSourceFixture(server.URL+"/cdn/file?token=signed"), 0644))

		_, err := download(t, server, partPath)
		require.NoError(t, err)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
		assert.Equal(t, []string{"bytes=50000-"}, server.requests())
	})

	t.Run("partial file of a different source is discarded", func(t *testing.T) {
		server := newMediaServer(t, content)
		partPath := filepath.Join(t.TempDir(), "f.part")
		require.NoError(t, os.WriteFile(partPath, []byte("preview bytes"), 0644))
		require.NoError(t, os.WriteFile(partPath+".src", []byte("https://cdn.example.com/preview.mp4"), 0644))

		_, err := download(t, server, partPath)
		require.NoError(t, err)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
		assert.Equal(t, []string{""}, server.requests())
	})

	t.Run("partial file without source record is discarded", func(t *testing.T) {
		server := newMediaServer(t, content)
		partPath := filepath.Join(t.TempDir(), "f.part")
		require.NoError(t, os.WriteFile(partPath, []byte("junk"), 0644))

		_, err := download(t, server, partPath)
		require.NoError(t, err)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
	})

	t.Run("server ignoring range restarts cleanly", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			w.Write(content) // always 200 with the full content
			return true
		}
		partPath := filepath.Join(t.TempDir(), "f.part")
		require.NoError(t, os.WriteFile(partPath, content[:half], 0644))
		require.NoError(t, os.WriteFile(partPath+".src", mediaSourceFixture(server.URL+"/cdn/file?token=signed"), 0644))

		_, err := download(t, server, partPath)
		require.NoError(t, err)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data, "content must not be duplicated")
	})

	t.Run("already complete partial file", func(t *testing.T) {
		server := newMediaServer(t, content)
		partPath := filepath.Join(t.TempDir(), "f.part")
		require.NoError(t, os.WriteFile(partPath, content, 0644))
		require.NoError(t, os.WriteFile(partPath+".src", mediaSourceFixture(server.URL+"/cdn/file?token=signed"), 0644))

		size, err := download(t, server, partPath)
		require.NoError(t, err)
		assert.Equal(t, int64(len(content)), size)
		data, _ := os.ReadFile(partPath)
		assert.Equal(t, content, data)
	})
}

func TestMediaDownloadRetries(t *testing.T) {
	fastMediaRetries(t)
	ctx := context.Background()
	content := []byte("media content")

	t.Run("server errors are retried", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			if n <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return true
			}
			return false
		}
		partPath := filepath.Join(t.TempDir(), "f.part")

		_, err := newMediaDownloader(nil).fetchToFile(ctx, server.URL+"/cdn/file", partPath)
		require.NoError(t, err)
		assert.Len(t, server.requests(), 3)
	})

	t.Run("gives up after repeated failures", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			w.WriteHeader(http.StatusBadGateway)
			return true
		}
		partPath := filepath.Join(t.TempDir(), "f.part")

		_, err := newMediaDownloader(nil).fetchToFile(ctx, server.URL+"/cdn/file", partPath)
		assert.Error(t, err)
		assert.Len(t, server.requests(), maxMediaRetries+1)
	})

	t.Run("client errors are not retried", func(t *testing.T) {
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		dir := t.TempDir()

		_, err := newMediaDownloader(nil).fetchToFile(ctx, server.URL+"/cdn/file", filepath.Join(dir, "f.part"))
		var fetchErr *FetchError
		require.ErrorAs(t, err, &fetchErr)
		assert.Equal(t, http.StatusForbidden, fetchErr.StatusCode)
		assert.Len(t, server.requests(), 1)

		entries, _ := os.ReadDir(dir)
		assert.Empty(t, entries, "no files left behind")
	})

	t.Run("cancellation stops without retrying", func(t *testing.T) {
		server := newMediaServer(t, content)
		cancelCtx, cancel := context.WithCancel(ctx)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			cancel()
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}

		_, err := newMediaDownloader(nil).fetchToFile(cancelCtx, server.URL+"/cdn/file", filepath.Join(t.TempDir(), "f.part"))
		assert.ErrorIs(t, err, context.Canceled)
		assert.Len(t, server.requests(), 1)
	})
}

func TestResolveRedirectRetries(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		n := counts[r.URL.Path]
		mu.Unlock()

		switch {
		case strings.HasPrefix(r.URL.Path, "/flaky-") && n <= 2:
			if r.URL.Path == "/flaky-429" {
				w.WriteHeader(http.StatusTooManyRequests)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case r.URL.Path == "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.Redirect(w, r, "https://cdn.example.com/file", http.StatusTemporaryRedirect)
		}
	}))
	defer server.Close()

	fetcher := NewFetcher(WithBackOffConfig(&backoff.ZeroBackOff{}))
	ctx := context.Background()

	for _, path := range []string{"/flaky-429", "/flaky-500"} {
		got, err := fetcher.ResolveRedirect(ctx, server.URL+path)
		require.NoError(t, err, path)
		assert.Equal(t, "https://cdn.example.com/file", got)
		assert.Equal(t, 3, counts[path], path)
	}

	_, err := fetcher.ResolveRedirect(ctx, server.URL+"/forbidden")
	var fetchErr *FetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, http.StatusForbidden, fetchErr.StatusCode)
	assert.Equal(t, 1, counts["/forbidden"])
}

func TestMediaExtension(t *testing.T) {
	tests := []struct {
		name, finalURL, contentType, want string
	}{
		{"substack mp3", "https://substackcdn.com/video_upload/x/transcoded.mp3?sig=1", "binary/octet-stream", ".mp3"},
		{"extension from path", "https://cdn.example.com/ep.M4A", "", ".m4a"},
		{"content type when path has none", "https://cdn.example.com/episode/123", "audio/mp4; charset=binary", ".m4a"},
		{"unknown path extension uses content type", "https://cdn.example.com/stream.php", "audio/ogg", ".ogg"},
		{"disallowed type falls back to default", "https://cdn.example.com/x", "video/mp4", ".mp3"},
		{"nothing known falls back to default", "https://cdn.example.com/x", "application/octet-stream", ".mp3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mediaExtension(podcastExtensions, tt.finalURL, tt.contentType))
		})
	}
}

func TestMediaDownloadExtensions(t *testing.T) {
	fastMediaRetries(t)
	ctx := context.Background()

	t.Run("format served decides the extension", func(t *testing.T) {
		content := buildMP4(60, 0)
		server := newMediaServer(t, content)
		server.handle = func(n int, w http.ResponseWriter, r *http.Request) bool {
			w.Header().Set("Content-Type", "audio/mp4")
			return false // ServeContent keeps the preset Content-Type
		}
		dir := t.TempDir()

		result, err := newMediaDownloader(nil).download(ctx, mediaJob{
			requestURL:       server.URL + "/cdn/file",
			basePath:         filepath.Join(dir, "ep"),
			exts:             podcastExtensions,
			expectedDuration: 120, // the served 60s file is a preview
		})
		require.NoError(t, err)
		assert.True(t, result.Preview, "duration read from the M4A container")
		assert.Equal(t, filepath.Join(dir, "ep_preview.m4a"), result.LocalPath)
	})

	t.Run("existing file with any allowed extension is kept", func(t *testing.T) {
		server := newMediaServer(t, []byte("new"))
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ep.ogg"), []byte("old"), 0644))

		result, err := newMediaDownloader(nil).download(ctx, mediaJob{
			requestURL: server.URL + "/cdn/file",
			basePath:   filepath.Join(dir, "ep"),
			exts:       podcastExtensions,
		})
		require.NoError(t, err)
		assert.True(t, result.Existed)
		assert.Equal(t, filepath.Join(dir, "ep.ogg"), result.LocalPath)
		assert.Empty(t, server.requests())
	})
}

func TestFetcherRequestBackOffIsPerRequest(t *testing.T) {
	fetcher := NewFetcher()
	a, releaseA := fetcher.requestBackOff()
	b, releaseB := fetcher.requestBackOff()
	releaseA()
	releaseB()
	assert.NotSame(t, a, b)
	assert.NotSame(t, fetcher.BackoffCfg, a)
}

func mediaSourceFixture(rawURL string) []byte {
	data, _ := json.Marshal(mediaSource{SourceDigest: mediaSourceKey(rawURL), ETag: `"fixture-content-v1"`})
	return data
}
