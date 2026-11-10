package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDownloadImagesInParallel(t *testing.T) {
	const images, delay = 20, 200 * time.Millisecond
	var mu sync.Mutex
	inFlight, peak := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(delay)
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write(testImageData)
		// Same-named images differ by their trailing marker.
		fmt.Fprint(w, r.URL.Path)
	}))
	defer server.Close()
	fetcher := NewFetcher(WithRatePerSecond(1000), WithBurst(1000))

	t.Run("downloads concurrently within the worker limit", func(t *testing.T) {
		var html strings.Builder
		for i := 0; i < images; i++ {
			fmt.Fprintf(&html, `<img src="%s/img-%02d.png">`, server.URL, i)
		}
		dir := t.TempDir()
		start := time.Now()
		result, err := NewImageDownloader(fetcher, dir, "images", ImageQualityHigh).DownloadImages(context.Background(), html.String(), "post")
		elapsed := time.Since(start)
		require.NoError(t, err)
		require.Equal(t, images, result.Success)
		// Sequential downloads would need images*delay = 4s.
		require.Less(t, elapsed, time.Duration(images)*delay/2)
		mu.Lock()
		require.Greater(t, peak, 1)
		require.LessOrEqual(t, peak, fetcher.MaxWorkers)
		mu.Unlock()
		// Results keep document order, and every reference is rewritten.
		for i, image := range result.Images {
			require.Equal(t, fmt.Sprintf("%s/img-%02d.png", server.URL, i), image.OriginalURL)
			require.Contains(t, result.UpdatedHTML, fmt.Sprintf("images/post/img-%02d.png", i))
		}
		require.NotContains(t, result.UpdatedHTML, server.URL)
	})

	t.Run("same filename keeps document order", func(t *testing.T) {
		html := fmt.Sprintf(`<img src="%s/a/photo.png"><img src="%s/b/photo.png">`, server.URL, server.URL)
		for i := 0; i < 10; i++ {
			dir := t.TempDir()
			result, err := NewImageDownloader(fetcher, dir, "images", ImageQualityHigh).DownloadImages(context.Background(), html, "post")
			require.NoError(t, err)
			require.Equal(t, 2, result.Success)
			data, err := os.ReadFile(filepath.Join(dir, "images", "post", "photo.png"))
			require.NoError(t, err)
			// The later image wins, as it did when downloads ran one at a time.
			require.True(t, strings.HasSuffix(string(data), "/b/photo.png"), string(data[len(testImageData):]))
		}
	})
}
