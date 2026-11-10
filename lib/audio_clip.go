package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// AudioClipInfo describes the outcome of downloading a single audio clip.
type AudioClipInfo struct {
	UploadID  string
	LocalPath string
	Size      int64
	Preview   bool // true if only a free preview was available
	Existed   bool // true if the file was already present and not re-downloaded
	Success   bool
	Error     error
}

// AudioClipDownloadResult contains the results of downloading a post's audio clips.
type AudioClipDownloadResult struct {
	Clips   []AudioClipInfo
	Success int
	Failed  int
}

// AudioClipDownloader downloads audio clips embedded in the text of a post.
// These are separate from a podcast post's episode audio: Substack leaves only
// a placeholder with the clip's upload ID in the post HTML.
type AudioClipDownloader struct {
	media     *mediaDownloader
	outputDir string
	clipsDir  string
}

// NewAudioClipDownloader creates a new AudioClipDownloader instance.
func NewAudioClipDownloader(fetcher *Fetcher, outputDir, clipsDir string) *AudioClipDownloader {
	return &AudioClipDownloader{
		media:     newMediaDownloader(fetcher),
		outputDir: outputDir,
		clipsDir:  clipsDir,
	}
}

// audioClipRef is a clip to download together with the file name to save it under.
type audioClipRef struct {
	uploadID string
	name     string
	duration float64
}

type audioClipAttrs struct {
	MediaUploadID string  `json:"mediaUploadId"`
	Duration      float64 `json:"duration"`
}

func parseAudioClipAttrs(raw string) (attrs audioClipAttrs, valid bool) {
	err := json.Unmarshal([]byte(raw), &attrs)
	return attrs, err == nil && uploadIDPattern.MatchString(attrs.MediaUploadID)
}

// audioClipRefs lists the post's audio clips in document order. Clips are
// named <baseName>_audio-<ID>, so their names stay the same if the post is
// edited.
func (p *Post) audioClipRefs(baseName string) []audioClipRef {
	var refs []audioClipRef
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(p.BodyHTML))
	if err != nil {
		return refs
	}
	seen := make(map[string]bool)
	doc.Find("div.native-audio-embed[data-attrs]").Each(func(_ int, s *goquery.Selection) {
		if s.Closest("template").Length() > 0 {
			return
		}
		attrs, valid := parseAudioClipAttrs(s.AttrOr("data-attrs", ""))
		id := attrs.MediaUploadID
		if !valid || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, audioClipRef{uploadID: id, name: baseName + "_audio-" + id, duration: attrs.Duration})
	})
	return refs
}

// DownloadAudioClips downloads the post's audio clips to
// <outputDir>/<clipsDir>/ as MP3 or M4A files named after baseName.
// origin must identify the trusted publication that may receive the fetcher's
// subscriber cookie.
func (ad *AudioClipDownloader) DownloadAudioClips(ctx context.Context, post Post, baseName string, origin *url.URL) (*AudioClipDownloadResult, error) {
	result := &AudioClipDownloadResult{Clips: []AudioClipInfo{}}

	refs := post.audioClipRefs(baseName)
	if len(refs) == 0 {
		return result, nil
	}

	if origin == nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return nil, errors.New("audio clips require a trusted HTTP(S) publication origin")
	}

	dir := filepath.Join(ad.outputDir, ad.clipsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audio clips directory: %w", err)
	}

	for _, ref := range refs {
		resolver := url.URL{
			Scheme: origin.Scheme,
			Host:   origin.Host,
			Path:   "/api/v1/audio/upload/" + ref.uploadID + "/src",
		}
		info := ad.downloadClip(ctx, resolver.String(), filepath.Join(dir, ref.name), ref.duration)
		info.UploadID = ref.uploadID
		if info.Success {
			result.Success++
		} else {
			result.Failed++
		}
		result.Clips = append(result.Clips, info)
	}
	return result, nil
}

// downloadClip downloads one clip from its source URL to basePath plus extension.
func (ad *AudioClipDownloader) downloadClip(ctx context.Context, srcURL, basePath string, duration float64) AudioClipInfo {
	info := AudioClipInfo{LocalPath: basePath + ".mp3"}
	result, err := ad.media.download(ctx, mediaJob{
		requestURL:       srcURL,
		resolve:          true,
		basePath:         basePath,
		exts:             []string{".mp3", ".m4a"},
		expectedDuration: duration,
	})
	if err != nil {
		info.Error = fmt.Errorf("failed to download audio clip: %w", err)
		return info
	}
	info.LocalPath = result.LocalPath
	info.Size = result.Size
	info.Preview = result.Preview
	info.Existed = result.Existed
	info.Success = true
	return info
}

// Sources maps the upload IDs of the downloaded clips to their file paths
// relative to dir, as URLs for HTML files in dir to reference.
func (r *AudioClipDownloadResult) Sources(dir string) map[string]string {
	srcs := make(map[string]string)
	for _, clip := range r.Clips {
		if !clip.Success {
			continue
		}
		if src, ok := relativeURL(dir, clip.LocalPath); ok {
			srcs[clip.UploadID] = src
		}
	}
	return srcs
}

// embedAudioClips uses token offsets to replace real elements while preserving
// the original bytes around them, including examples in comments and scripts.
func embedAudioClips(postHTML string, clips map[string]string) string {
	if len(clips) == 0 {
		return postHTML
	}
	tokens := html.NewTokenizer(strings.NewReader(postHTML))
	placed := make(map[string]bool)
	var output strings.Builder
	var pos, last, start, depth, templateDepth int
	var tag atom.Atom
	var id string
	bodyEnd := -1
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		offset := pos
		pos += len(tokens.Raw())
		if kind != html.StartTagToken && kind != html.EndTagToken {
			continue
		}
		token := tokens.Token()
		if token.DataAtom == atom.Template {
			if kind == html.StartTagToken {
				templateDepth++
			} else if templateDepth > 0 {
				templateDepth--
			}
			continue
		}
		if templateDepth > 0 {
			continue
		}
		if depth > 0 {
			if token.DataAtom == tag {
				if kind == html.StartTagToken {
					depth++
				} else {
					depth--
				}
				if depth == 0 {
					output.WriteString(postHTML[last:start])
					output.WriteString(audioClipElement(id, clips[id]))
					placed[id] = true
					last = pos
				}
			}
			continue
		}
		if kind == html.EndTagToken {
			if token.DataAtom == atom.Body && bodyEnd < 0 {
				bodyEnd = output.Len() + offset - last
			}
			continue
		}
		id = audioClipElementID(token)
		if _, ok := clips[id]; ok {
			start, depth, tag = offset, 1, token.DataAtom
		}
	}

	var missing []string
	for id := range clips {
		if !placed[id] {
			missing = append(missing, id)
		}
	}
	output.WriteString(postHTML[last:])
	updated := output.String()
	if len(missing) == 0 {
		return updated
	}

	if bodyEnd < 0 {
		bodyEnd = len(updated)
	}
	slices.Sort(missing)
	var fallback strings.Builder
	for _, id := range missing {
		fallback.WriteString("\n<figure><figcaption>Audio clip</figcaption>")
		fallback.WriteString(audioClipElement(id, clips[id]))
		fallback.WriteString("</figure>\n")
	}
	return updated[:bodyEnd] + fallback.String() + updated[bodyEnd:]
}

func audioClipElementID(token html.Token) string {
	switch token.DataAtom {
	case atom.Audio:
		for _, attr := range token.Attr {
			if attr.Key == "data-audio-clip-id" {
				return attr.Val
			}
		}
	case atom.Div:
		var native bool
		var raw string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "class":
				native = slices.Contains(strings.Fields(attr.Val), "native-audio-embed")
			case "data-attrs":
				raw = attr.Val
			}
		}
		if native {
			if attrs, valid := parseAudioClipAttrs(raw); valid {
				return attrs.MediaUploadID
			}
		}
	}
	return ""
}
