package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
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
	doc.Find(".native-audio-embed[data-attrs]").Each(func(i int, s *goquery.Selection) {
		var attrs struct {
			MediaUploadID string  `json:"mediaUploadId"`
			Duration      float64 `json:"duration"`
		}
		if err := json.Unmarshal([]byte(s.AttrOr("data-attrs", "")), &attrs); err != nil {
			return
		}
		id := attrs.MediaUploadID
		if !uploadIDPattern.MatchString(id) || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, audioClipRef{uploadID: id, name: baseName + "_audio-" + id, duration: attrs.Duration})
	})
	return refs
}

// DownloadAudioClips downloads the post's audio clips to
// <outputDir>/<clipsDir>/ as MP3 files named after baseName.
func (ad *AudioClipDownloader) DownloadAudioClips(ctx context.Context, post Post, baseName string) (*AudioClipDownloadResult, error) {
	result := &AudioClipDownloadResult{Clips: []AudioClipInfo{}}

	refs := post.audioClipRefs(baseName)
	if len(refs) == 0 {
		return result, nil
	}

	origin, ok := publicationOrigin(post)
	if !ok {
		return nil, errors.New("post has no canonical URL to request audio clips from")
	}

	dir := filepath.Join(ad.outputDir, ad.clipsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audio clips directory: %w", err)
	}

	for _, ref := range refs {
		info := ad.downloadClip(ctx, origin.String()+"/api/v1/audio/upload/"+ref.uploadID+"/src", filepath.Join(dir, ref.name), ref.duration)
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
