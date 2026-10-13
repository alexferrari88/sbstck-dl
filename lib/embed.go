package lib

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// EmbeddedMedia lists downloaded media to embed in post HTML, as URLs relative
// to the post file.
type EmbeddedMedia struct {
	Podcast string            // podcast audio
	Videos  map[string]string // videos by upload ID
}

// videoPlaceholderPattern matches the empty elements Substack leaves in post
// HTML for native videos.
var videoPlaceholderPattern = regexp.MustCompile(`<div\b[^>]*\bnative-video-embed\b[^>]*>\s*</div>`)

// Embedded elements are matched so that embedding again updates them, e.g.
// once the full file replaces a preview.
var (
	embeddedVideoPattern   = regexp.MustCompile(`(?s)<video\b[^>]*\bdata-upload-id="([A-Za-z0-9-]+)"[^>]*>.*?</video>`)
	embeddedPodcastPattern = regexp.MustCompile(`(?s)<audio\b[^>]*\bdata-podcast\b[^>]*>.*?</audio>`)
)

// EmbedMedia embeds downloaded media in post HTML, as players of the local
// files. Native video placeholders are replaced with their videos. The post's
// own video, which usually has no placeholder, and the podcast audio are added
// at the start. Only the media elements are changed, so the rest of the HTML
// stays as it is.
func EmbedMedia(postHTML, postVideoID string, media EmbeddedMedia) string {
	embedded := make(map[string]bool)
	embed := func(match, uploadID string) string {
		src, ok := media.Videos[uploadID]
		if !ok {
			return match
		}
		embedded[uploadID] = true
		return videoElement(uploadID, src)
	}

	postHTML = embeddedVideoPattern.ReplaceAllStringFunc(postHTML, func(match string) string {
		return embed(match, embeddedVideoPattern.FindStringSubmatch(match)[1])
	})
	postHTML = videoPlaceholderPattern.ReplaceAllStringFunc(postHTML, func(match string) string {
		return embed(match, placeholderUploadID(match))
	})

	if media.Podcast != "" {
		podcastEmbedded := false
		postHTML = embeddedPodcastPattern.ReplaceAllStringFunc(postHTML, func(string) string {
			podcastEmbedded = true
			return podcastElement(media.Podcast)
		})
		if !podcastEmbedded {
			postHTML = podcastElement(media.Podcast) + "\n" + postHTML
		}
	}
	if src, ok := media.Videos[postVideoID]; ok && !embedded[postVideoID] {
		postHTML = videoElement(postVideoID, src) + "\n" + postHTML
	}
	return postHTML
}

// placeholderUploadID returns the upload ID of a native video placeholder, or
// "" if it has none.
func placeholderUploadID(placeholder string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(placeholder))
	if err != nil {
		return ""
	}
	var attrs struct {
		MediaUploadID string `json:"mediaUploadId"`
	}
	if err := json.Unmarshal([]byte(doc.Find("div").AttrOr("data-attrs", "")), &attrs); err != nil {
		return ""
	}
	return attrs.MediaUploadID
}

// videoElement returns a video element playing src, with a download link for
// browsers that can't play it.
func videoElement(uploadID, src string) string {
	src = html.EscapeString(src)
	return fmt.Sprintf(`<video data-upload-id="%s" controls preload="metadata" playsinline style="max-width: 100%%">`+
		`<source src="%s" type="video/mp4"><a href="%s">Download video</a></video>`, html.EscapeString(uploadID), src, src)
}

// podcastElement returns an audio element playing src, with a download link
// for browsers that can't play it.
func podcastElement(src string) string {
	src = html.EscapeString(src)
	return fmt.Sprintf(`<audio data-podcast controls preload="metadata" src="%s" style="width: 100%%">`+
		`<a href="%s">Download audio</a></audio>`, src, src)
}

// relativeURL returns path relative to dir as a URL for HTML files in dir to
// reference.
func relativeURL(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return "", false
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/"), true
}

// EmbedMediaInFile adds local players to an existing HTML post through atomic
// publication. Missing posts are errors so callers keep output work retryable.
func EmbedMediaInFile(path, postVideoID string, media EmbeddedMedia) error {
	return embedMediaInFileWith(path, postVideoID, media, writeFileAtomically)
}

func embedMediaInFileWith(path, postVideoID string, media EmbeddedMedia, publish func(string, []byte) error) error {
	if media.Podcast == "" && len(media.Videos) == 0 {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read HTML post %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("HTML post %s is not a nonempty regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	var title string
	if i := strings.Index(content, "</h1>"); i >= 0 {
		rest := strings.TrimLeft(content[i+len("</h1>"):], "\n")
		title, content = content[:len(content)-len(rest)], rest
	}
	updated := title + EmbedMedia(content, postVideoID, media)
	if updated == string(data) {
		return nil
	}
	return publish(path, []byte(updated))
}
