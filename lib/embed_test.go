package lib

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbedMedia(t *testing.T) {
	const (
		embed1 = "d162532c-e6f4-486d-b168-406f015f78dd"
		embed2 = "a2517b10-9e08-4e06-996d-11ee246d373f"
	)
	videos := map[string]string{
		"post-video": "videos/base.mp4",
		embed1:       "videos/base_video-d162532c.mp4",
	}

	t.Run("placeholders and post video", func(t *testing.T) {
		body := "<p>intro</p>" + nativeVideoEmbed(embed1) + "<p>middle</p>" + nativeVideoEmbed(embed2)
		got := EmbedMedia(body, "post-video", EmbeddedMedia{Videos: videos})
		assert.Equal(t, videoElement("post-video", "videos/base.mp4")+"\n<p>intro</p>"+
			videoElement(embed1, "videos/base_video-d162532c.mp4")+
			"<p>middle</p>"+nativeVideoEmbed(embed2), got, "videos not downloaded keep their placeholder")
	})

	t.Run("post video placeholder is replaced in place", func(t *testing.T) {
		body := "<p>intro</p>" + nativeVideoEmbed("post-video")
		got := EmbedMedia(body, "post-video", EmbeddedMedia{Videos: videos})
		assert.Equal(t, "<p>intro</p>"+videoElement("post-video", "videos/base.mp4"), got)
	})

	t.Run("podcast", func(t *testing.T) {
		got := EmbedMedia("<p>notes</p>", "", EmbeddedMedia{Podcast: "podcasts/base.mp3"})
		assert.Equal(t, podcastElement("podcasts/base.mp3")+"\n<p>notes</p>", got)
	})

	t.Run("embedding again updates sources", func(t *testing.T) {
		body := "<p>intro</p>" + nativeVideoEmbed(embed1)
		full := EmbeddedMedia{Podcast: "podcasts/base.mp3", Videos: videos}
		first := EmbedMedia(body, "post-video", EmbeddedMedia{
			Podcast: "podcasts/base_preview.mp3",
			Videos: map[string]string{
				"post-video": "videos/base_preview.mp4",
				embed1:       "videos/base_video-d162532c_preview.mp4",
			},
		})
		got := EmbedMedia(first, "post-video", full)
		assert.Equal(t, EmbedMedia(body, "post-video", full), got)
		assert.Equal(t, got, EmbedMedia(got, "post-video", full))
	})

	t.Run("nothing downloaded", func(t *testing.T) {
		body := "<p>intro</p>" + nativeVideoEmbed(embed1)
		assert.Equal(t, body, EmbedMedia(body, "post-video", EmbeddedMedia{}))
	})

	t.Run("sources are escaped", func(t *testing.T) {
		got := EmbedMedia(nativeVideoEmbed(embed1), "", EmbeddedMedia{Videos: map[string]string{embed1: `a"b&c.mp4`}})
		assert.Contains(t, got, `src="a&#34;b&amp;c.mp4"`)
	})
}

func TestPodcastDownloadResultSource(t *testing.T) {
	dir := filepath.Join("out", "dir")
	result := &PodcastDownloadResult{LocalPath: filepath.Join(dir, "podcasts", "2025 post#1.m4a")}
	assert.Equal(t, "podcasts/2025%20post%231.m4a", result.Source(dir))
}
