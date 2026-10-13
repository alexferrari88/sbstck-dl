package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePublication serves a minimal Substack publication: a sitemap, post pages
// with their data embedded like Substack does, and podcast audio. It counts
// requests to each post page.
type fakePublication struct {
	*httptest.Server
	mu         sync.Mutex
	posts      map[string]map[string]any // slug -> post data
	pageCounts map[string]int
}

func newFakePublication(t *testing.T) *fakePublication {
	p := &fakePublication{posts: map[string]map[string]any{}, pageCounts: map[string]int{}}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.Close)
	return p
}

func (p *fakePublication) addPost(slug string, data map[string]any) {
	data["slug"] = slug
	data["canonical_url"] = p.URL + "/p/" + slug
	data["post_date"] = "2025-01-02T03:04:05.000Z"
	data["title"] = slug
	data["body_html"] = "<p>body of " + slug + "</p>"
	p.posts[slug] = data
}

func (p *fakePublication) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/sitemap.xml":
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><urlset>`)
		for slug := range p.posts {
			fmt.Fprintf(w, "<url><loc>%s/p/%s</loc><lastmod>2025-01-02</lastmod></url>", p.URL, slug)
		}
		fmt.Fprint(w, `</urlset>`)
	case strings.HasPrefix(r.URL.Path, "/p/"):
		slug := strings.TrimPrefix(r.URL.Path, "/p/")
		p.mu.Lock()
		p.pageCounts[slug]++
		p.mu.Unlock()
		data, ok := p.posts[slug]
		if !ok {
			http.NotFound(w, r)
			return
		}
		preloads, _ := json.Marshal(map[string]any{"post": data})
		quoted, _ := json.Marshal(string(preloads))
		fmt.Fprintf(w, `<html><head><script>window._preloads = JSON.parse(%s)</script></head><body></body></html>`, quoted)
	case strings.HasPrefix(r.URL.Path, "/api/v1/audio/upload/"):
		http.Redirect(w, r, "/cdn/audio.mp3", http.StatusTemporaryRedirect)
	case r.URL.Path == "/cdn/audio.mp3":
		w.Write([]byte("fake audio"))
	case strings.HasPrefix(r.URL.Path, "/api/v1/video/upload/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/video/upload/"), "/src")
		if strings.HasSuffix(r.URL.Path, "/src") {
			http.Redirect(w, r, "/cdn/"+id+".mp4", http.StatusTemporaryRedirect)
			return
		}
		fmt.Fprintf(w, `{"id":%q}`, id)
	case strings.HasPrefix(r.URL.Path, "/cdn/") && strings.HasSuffix(r.URL.Path, ".mp4"):
		w.Write([]byte("fake video"))
	default:
		http.NotFound(w, r)
	}
}

func (p *fakePublication) pageRequests() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := p.pageCounts
	p.pageCounts = map[string]int{}
	return counts
}

// runMediaDownload runs the download command against the publication with the
// given media flags, restoring the command's global flags afterwards.
func runMediaDownload(t *testing.T, pubURL, output string, podcasts bool) {
	runMediaDownloadWith(t, pubURL, output, podcasts, false)
}

func runMediaDownloadWith(t *testing.T, pubURL, output string, podcasts, videos bool) {
	saved := []any{downloadUrl, outputFolder, format, downloadPodcasts, downloadVideos, podcastsDir, videosDir, verbose, idCookieVal, fetcher, extractor}
	savedFormats := formatValues
	t.Cleanup(func() {
		formatValues = savedFormats
		downloadUrl, outputFolder, format = saved[0].(string), saved[1].(string), saved[2].(string)
		downloadPodcasts, downloadVideos = saved[3].(bool), saved[4].(bool)
		podcastsDir, videosDir = saved[5].(string), saved[6].(string)
		verbose, idCookieVal = saved[7].(bool), saved[8].(string)
		fetcher, extractor = saved[9].(*lib.Fetcher), saved[10].(*lib.Extractor)
	})

	downloadUrl, outputFolder, format, formatValues = pubURL, output, "html", nil
	downloadPodcasts, downloadVideos = podcasts, videos
	podcastsDir, videosDir = "podcasts", "videos"
	verbose, idCookieVal = false, ""
	fetcher = lib.NewFetcher(lib.WithRatePerSecond(100))
	extractor = lib.NewExtractor(fetcher)

	require.NoError(t, runDownload(downloadCmd, nil))
}

func TestDownloadMediaForExistingPosts(t *testing.T) {
	pub := newFakePublication(t)
	pub.addPost("free-episode", map[string]any{
		"type": "podcast", "audience": "everyone",
		"podcast_url": pub.URL + "/api/v1/audio/upload/abc/src",
	})
	pub.addPost("paid-episode", map[string]any{
		"type": "podcast", "audience": "only_paid", "podcast_duration": 120.0,
	})
	pub.addPost("newsletter", map[string]any{"type": "newsletter", "audience": "everyone"})
	// Paid episode whose audio Substack serves in full: complete even without a cookie
	pub.addPost("paid-full-episode", map[string]any{
		"type": "podcast", "audience": "only_paid",
		"podcast_url": pub.URL + "/api/v1/audio/upload/def/src",
	})
	// Paid newsletter: may hide media without a cookie, so never complete
	pub.addPost("paid-newsletter", map[string]any{"type": "newsletter", "audience": "only_paid"})

	output := t.TempDir()
	postFile := filepath.Join(output, "20250102_030405_free-episode.html")
	audioFile := filepath.Join(output, "podcasts", "20250102_030405_free-episode.mp3")

	// Run 1: posts only
	runMediaDownload(t, pub.URL, output, false)
	require.FileExists(t, postFile)
	assert.NoDirExists(t, filepath.Join(output, "podcasts"))
	pub.pageRequests()

	// Run 2: media added to the existing download, without rewriting posts
	require.NoError(t, os.WriteFile(postFile, []byte("edited by user"), 0644))
	runMediaDownload(t, pub.URL, output, true)
	data, err := os.ReadFile(audioFile)
	require.NoError(t, err)
	assert.Equal(t, "fake audio", string(data))
	post, _ := os.ReadFile(postFile)
	assert.True(t, strings.HasSuffix(string(post), "\nedited by user"), "existing post files are not rewritten, only the audio is embedded")
	assert.Contains(t, string(post), `<audio data-podcast`)
	assert.Equal(t, map[string]int{
		"free-episode": 1, "paid-episode": 1, "newsletter": 1, "paid-full-episode": 1, "paid-newsletter": 1,
	}, pub.pageRequests())

	manifest, err := lib.LoadMediaManifest(output)
	require.NoError(t, err)
	assert.True(t, manifest.IsComplete("free-episode", lib.MediaPodcast))
	assert.True(t, manifest.IsComplete("newsletter", lib.MediaPodcast))
	assert.False(t, manifest.IsComplete("paid-episode", lib.MediaPodcast), "withheld without a cookie")
	assert.True(t, manifest.IsComplete("paid-full-episode", lib.MediaPodcast))
	assert.False(t, manifest.IsComplete("paid-newsletter", lib.MediaPodcast))

	// Run 3: only posts with incomplete media are fetched again
	runMediaDownload(t, pub.URL, output, true)
	assert.Equal(t, map[string]int{"paid-episode": 1, "paid-newsletter": 1}, pub.pageRequests())

	// Run 4: deleting a downloaded file makes its post be fetched again
	require.NoError(t, os.Remove(audioFile))
	runMediaDownload(t, pub.URL, output, true)
	assert.Equal(t, map[string]int{"free-episode": 1, "paid-episode": 1, "paid-newsletter": 1}, pub.pageRequests())
	assert.FileExists(t, audioFile)
}

func TestDownloadEmbedsMedia(t *testing.T) {
	const embedID = "d162532c-e6f4-486d-b168-406f015f78dd"
	pub := newFakePublication(t)
	pub.addPost("video-post", map[string]any{"type": "video", "audience": "everyone", "video_upload_id": "main-video"})
	pub.addPost("embeds", map[string]any{"type": "newsletter", "audience": "everyone"})
	pub.addPost("episode", map[string]any{
		"type": "podcast", "audience": "everyone",
		"podcast_url": pub.URL + "/api/v1/audio/upload/abc/src",
	})
	pub.posts["embeds"]["body_html"] = `<p>before</p><div class="native-video-embed" data-component-name="VideoPlaceholder" ` +
		`data-attrs="{&quot;mediaUploadId&quot;:&quot;` + embedID + `&quot;}"></div><p>after</p>`

	videoTag := func(id, file string) string {
		return `<video data-upload-id="` + id + `" controls preload="metadata" playsinline style="max-width: 100%">` +
			`<source src="videos/` + file + `" type="video/mp4"><a href="videos/` + file + `">Download video</a></video>`
	}
	audioTag := `<audio data-podcast controls preload="metadata" src="podcasts/20250102_030405_episode.mp3" style="width: 100%">` +
		`<a href="podcasts/20250102_030405_episode.mp3">Download audio</a></audio>`
	t.Run("new posts", func(t *testing.T) {
		dir := t.TempDir()
		runMediaDownloadWith(t, pub.URL, dir, true, true)

		page, err := os.ReadFile(filepath.Join(dir, "20250102_030405_episode.html"))
		require.NoError(t, err)
		assert.Equal(t, "<h1>episode</h1>\n\n"+audioTag+"\n<p>body of episode</p>", string(page))

		page, err = os.ReadFile(filepath.Join(dir, "20250102_030405_embeds.html"))
		require.NoError(t, err)
		assert.Equal(t, "<h1>embeds</h1>\n\n<p>before</p>"+videoTag(embedID, "20250102_030405_embeds_video-d162532c-e6f4-486d-b168-406f015f78dd.mp4")+"<p>after</p>", string(page))
		assert.FileExists(t, filepath.Join(dir, "videos", "20250102_030405_embeds_video-d162532c-e6f4-486d-b168-406f015f78dd.mp4"))

		page, err = os.ReadFile(filepath.Join(dir, "20250102_030405_video-post.html"))
		require.NoError(t, err)
		assert.Equal(t, "<h1>video-post</h1>\n\n"+videoTag("main-video", "20250102_030405_video-post.mp4")+"\n<p>body of video-post</p>", string(page))
	})

	t.Run("posts downloaded earlier", func(t *testing.T) {
		dir := t.TempDir()
		runMediaDownloadWith(t, pub.URL, dir, false, false)
		postFile := filepath.Join(dir, "20250102_030405_video-post.html")
		require.NoError(t, os.WriteFile(postFile, []byte("<h1>video-post</h1>\n\n<p>edited by user</p>"), 0644))

		runMediaDownloadWith(t, pub.URL, dir, true, true)
		page, err := os.ReadFile(postFile)
		require.NoError(t, err)
		assert.Equal(t, "<h1>video-post</h1>\n\n"+videoTag("main-video", "20250102_030405_video-post.mp4")+"\n<p>edited by user</p>", string(page),
			"only the video is added to existing post files")

		page, err = os.ReadFile(filepath.Join(dir, "20250102_030405_embeds.html"))
		require.NoError(t, err)
		assert.Contains(t, string(page), videoTag(embedID, "20250102_030405_embeds_video-d162532c-e6f4-486d-b168-406f015f78dd.mp4"))

		page, err = os.ReadFile(filepath.Join(dir, "20250102_030405_episode.html"))
		require.NoError(t, err)
		assert.Equal(t, "<h1>episode</h1>\n\n"+audioTag+"\n<p>body of episode</p>", string(page))
	})
}
