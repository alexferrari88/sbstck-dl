package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// VideoInfo describes the outcome of downloading a single video.
type VideoInfo struct {
	UploadID  string
	LocalPath string
	Size      int64
	Preview   bool // true if only a free preview was available
	Existed   bool // true if the file was already present and not re-downloaded
	Success   bool
	Error     error
}

// VideoDownloadResult contains the results of downloading the videos of a post.
type VideoDownloadResult struct {
	Videos  []VideoInfo
	Success int
	Failed  int
}

// VideoDownloader downloads Substack-hosted videos: the video of a video post
// and native videos embedded in the post body. Third-party embeds such as
// YouTube are not downloaded.
type VideoDownloader struct {
	media     *mediaDownloader
	outputDir string
	videosDir string
}

// NewVideoDownloader creates a new VideoDownloader instance.
func NewVideoDownloader(fetcher *Fetcher, outputDir, videosDir string) *VideoDownloader {
	return &VideoDownloader{
		media:     newMediaDownloader(fetcher),
		outputDir: outputDir,
		videosDir: videosDir,
	}
}

// videoRef is a video to download together with the file name to save it under.
type videoRef struct {
	uploadID string
	name     string
}

// uploadIDPattern matches valid media upload IDs. IDs come from post data and
// end up in URLs and file names, so anything else is rejected.
var uploadIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// videoRefs lists the post's Substack-hosted videos. The post's own video is
// named <baseName>, embedded videos <baseName>_video-<ID prefix>, so their
// names stay the same if the post's videos are reordered or edited.
func (p *Post) videoRefs(baseName string) []videoRef {
	var refs []videoRef
	seen := make(map[string]bool)

	if uploadIDPattern.MatchString(p.VideoUploadId) {
		refs = append(refs, videoRef{uploadID: p.VideoUploadId, name: baseName})
		seen[p.VideoUploadId] = true
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(p.BodyHTML))
	if err != nil {
		return refs
	}
	doc.Find(".native-video-embed[data-attrs]").Each(func(i int, s *goquery.Selection) {
		var attrs struct {
			MediaUploadID string `json:"mediaUploadId"`
		}
		if err := json.Unmarshal([]byte(s.AttrOr("data-attrs", "")), &attrs); err != nil {
			return
		}
		id := attrs.MediaUploadID
		if !uploadIDPattern.MatchString(id) || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, videoRef{uploadID: id, name: baseName + "_video-" + shortUploadID(id)})
	})

	return refs
}

// shortUploadID returns the first block of a UUID upload ID (8 hex digits),
// enough to tell a post's videos apart.
func shortUploadID(id string) string {
	if i := strings.IndexByte(id, '-'); i >= 8 {
		return id[:i]
	}
	return id
}

// DownloadVideos downloads the post's videos to <outputDir>/<videosDir>/ as MP4
// files named after baseName. Free previews get a _preview suffix so that a
// later run with a subscriber cookie still fetches the full video.
func (vd *VideoDownloader) DownloadVideos(ctx context.Context, post Post, baseName string) (*VideoDownloadResult, error) {
	result := &VideoDownloadResult{Videos: []VideoInfo{}}

	refs := post.videoRefs(baseName)
	if len(refs) == 0 {
		return result, nil
	}

	origin, ok := publicationOrigin(post)
	if !ok {
		return nil, errors.New("post has no canonical URL to request videos from")
	}

	dir := filepath.Join(vd.outputDir, vd.videosDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create videos directory: %w", err)
	}

	for _, ref := range refs {
		apiURL := origin.String() + "/api/v1/video/upload/" + ref.uploadID
		info := vd.downloadVideo(ctx, apiURL, filepath.Join(dir, ref.name))
		info.UploadID = ref.uploadID
		if info.Success {
			result.Success++
		} else {
			result.Failed++
		}
		result.Videos = append(result.Videos, info)
	}

	return result, nil
}

// downloadVideo downloads one video given its upload API URL and the local
// path to save it under, without extension.
func (vd *VideoDownloader) downloadVideo(ctx context.Context, apiURL, basePath string) VideoInfo {
	info := VideoInfo{LocalPath: basePath + ".mp4"}

	// Checked before fetching metadata so re-runs make no requests
	if stat, err := os.Stat(info.LocalPath); err == nil {
		info.Size = stat.Size()
		info.Existed = true
		info.Success = true
		return info
	}

	upload, err := vd.fetchUpload(ctx, apiURL)
	if err != nil {
		info.Error = fmt.Errorf("failed to fetch video metadata: %w", err)
		return info
	}

	// /src redirects to the full video or the preview, depending on the cookie
	result, err := vd.media.download(ctx, mediaJob{
		requestURL:       apiURL + "/src",
		resolve:          true,
		basePath:         basePath,
		exts:             []string{".mp4"},
		expectedDuration: upload.Duration,
		previewHint: func(cdnURL string) bool {
			return upload.MuxPreviewPlaybackID != "" && strings.Contains(cdnURL, upload.MuxPreviewPlaybackID)
		},
	})
	if err != nil {
		info.Error = fmt.Errorf("failed to download video: %w", err)
		return info
	}

	info.LocalPath = result.LocalPath
	info.Size = result.Size
	info.Preview = result.Preview
	info.Existed = result.Existed
	info.Success = true
	return info
}

// fetchUpload fetches the metadata of a video upload.
func (vd *VideoDownloader) fetchUpload(ctx context.Context, apiURL string) (*VideoUpload, error) {
	body, err := vd.media.fetcher.FetchURL(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var upload VideoUpload
	if err := json.NewDecoder(body).Decode(&upload); err != nil {
		return nil, fmt.Errorf("failed to parse video metadata: %w", err)
	}
	return &upload, nil
}

// Sources maps the upload IDs of the downloaded videos to their file paths
// relative to dir, as URLs for HTML files in dir to reference.
func (r *VideoDownloadResult) Sources(dir string) map[string]string {
	srcs := make(map[string]string)
	for _, video := range r.Videos {
		if !video.Success {
			continue
		}
		if src, ok := relativeURL(dir, video.LocalPath); ok {
			srcs[video.UploadID] = src
		}
	}
	return srcs
}
