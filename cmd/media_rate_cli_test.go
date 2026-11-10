package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestMediaRateCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, string(output))
	const images, posts = 12, 9
	var mu sync.Mutex
	counts := make(map[string]int)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		switch {
		case r.URL.Path == "/sitemap.xml":
			fmt.Fprint(w, "<urlset>")
			for i := 0; i < posts; i++ {
				fmt.Fprintf(w, "<url><loc>%s/p/text-%d</loc></url>", server.URL, i)
			}
			fmt.Fprint(w, "</urlset>")
		case strings.HasPrefix(r.URL.Path, "/image-"):
			fmt.Fprint(w, "image fixture bytes")
		case strings.HasPrefix(r.URL.Path, "/p/"):
			slug := strings.TrimPrefix(r.URL.Path, "/p/")
			body := "<p>Body text</p>"
			if slug == "gallery" {
				for i := 0; i < images; i++ {
					body += fmt.Sprintf(`<img src="%s/image-%d.jpg">`, server.URL, i)
				}
			}
			post := lib.Post{Slug: slug, Title: slug, PostDate: "2026-01-01T12:00:00Z", CanonicalUrl: server.URL + r.URL.Path, BodyHTML: body}
			payload, _ := json.Marshal(lib.PostWrapper{Post: post})
			escaped, _ := json.Marshal(string(payload))
			fmt.Fprintf(w, "<script>window._preloads = JSON.parse(%s)</script>", escaped)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	timed := func(t *testing.T, url string, flags ...string) time.Duration {
		t.Helper()
		args := append([]string{"download", "--url", url, "--output", t.TempDir(), "--rate", "1"}, flags...)
		start := time.Now()
		output, err := exec.Command(binary, args...).CombinedOutput()
		require.NoError(t, err, string(output))
		return time.Since(start)
	}
	imageCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		total := 0
		for path, n := range counts {
			if strings.HasPrefix(path, "/image-") {
				total += n
			}
		}
		return total
	}

	t.Run("media shares the post rate by default", func(t *testing.T) {
		before := imageCount()
		elapsed := timed(t, server.URL+"/p/gallery", "--download-images")
		require.Equal(t, images, imageCount()-before)
		// 13 requests at 1/s with a burst of 5 need about 8s.
		require.GreaterOrEqual(t, elapsed, 5*time.Second)
	})
	t.Run("media rate speeds up media only", func(t *testing.T) {
		before := imageCount()
		elapsed := timed(t, server.URL+"/p/gallery", "--download-images", "--media-rate", "1000")
		require.Equal(t, images, imageCount()-before)
		require.Less(t, elapsed, 3*time.Second)
	})
	t.Run("post pages keep the post rate", func(t *testing.T) {
		// Sitemap plus 9 posts at 1/s with a burst of 5 need about 5s.
		elapsed := timed(t, server.URL, "--media-rate", "1000")
		require.GreaterOrEqual(t, elapsed, 3*time.Second)
	})
	t.Run("negative media rate rejected", func(t *testing.T) {
		output, err := exec.Command(binary, "download", "--url", server.URL+"/p/gallery", "--output", t.TempDir(), "--media-rate", "-1").CombinedOutput()
		require.Error(t, err, string(output))
		require.Contains(t, string(output), "media-rate")
	})
}
