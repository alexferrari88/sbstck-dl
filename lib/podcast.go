package lib

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
)

// PodcastUpload holds the subset of Substack's podcastUpload metadata needed to
// tell a full episode apart from a free preview.
type PodcastUpload struct {
	Duration        float64          `json:"duration"`
	IsFreePreview   bool             `json:"is_free_preview"`
	FullPodcastInfo *FullPodcastInfo `json:"full_podcast_info,omitempty"`
}

// FullPodcastInfo describes the full episode when the served upload is a preview.
type FullPodcastInfo struct {
	Duration float64 `json:"duration"`
}

// HasPodcast reports whether the post has podcast audio attached.
func (p *Post) HasPodcast() bool {
	return p.PodcastURL != ""
}

// IsPaywalled reports whether the post is restricted to some subscribers.
// Without access, Substack may withhold its media without any sign of it.
func (p *Post) IsPaywalled() bool {
	return p.Audience != "" && p.Audience != "everyone"
}

// IsPodcastUnavailable reports whether the post is a podcast episode whose audio
// Substack withheld. Some publications serve no preview of paid episodes at all
// to readers without access: the post then has no podcast_url.
func (p *Post) IsPodcastUnavailable() bool {
	return !p.HasPodcast() && (p.Type == "podcast" || p.PodcastDuration > 0)
}

// IsPodcastPreview reports whether the served audio is a truncated preview of a
// paid episode. Substack marks some full-length episodes as previews too (e.g.
// ad-supported free versions), so we only treat it as a preview when it is
// actually shorter than the full episode.
func (p *Post) IsPodcastPreview() bool {
	u := p.PodcastUpload
	return u != nil && u.IsFreePreview && u.FullPodcastInfo != nil &&
		u.FullPodcastInfo.Duration > u.Duration+1
}

// PodcastDownloadResult describes the outcome of downloading a post's podcast audio.
type PodcastDownloadResult struct {
	LocalPath string
	Size      int64
	Preview   bool // true if only a truncated preview was available
	Existed   bool // true if the file was already present and not re-downloaded
}

// Source returns the path of the downloaded audio relative to dir, as a URL
// for HTML files in dir to reference.
func (r *PodcastDownloadResult) Source(dir string) string {
	src, _ := relativeURL(dir, r.LocalPath)
	return src
}

// PodcastDownloader downloads podcast audio from Substack posts.
type PodcastDownloader struct {
	media       *mediaDownloader
	outputDir   string
	podcastsDir string
}

// NewPodcastDownloader creates a new PodcastDownloader instance.
func NewPodcastDownloader(fetcher *Fetcher, outputDir, podcastsDir string) *PodcastDownloader {
	return &PodcastDownloader{
		media:       newMediaDownloader(fetcher),
		outputDir:   outputDir,
		podcastsDir: podcastsDir,
	}
}

// podcastExtensions are the audio file extensions podcasts are saved with.
// Substack serves MP3; other formats come from podcasts hosted elsewhere.
var podcastExtensions = []string{".mp3", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".flac"}

// DownloadPodcast downloads the post's podcast audio to <outputDir>/<podcastsDir>/<baseName>.mp3
// (or another audio extension, depending on the format served).
// Truncated previews are saved as <baseName>_preview.mp3 so that a later run with a
// subscriber cookie still fetches the full episode.
// Returns nil and no error if the post has no podcast.
func (pd *PodcastDownloader) DownloadPodcast(ctx context.Context, post Post, baseName string) (*PodcastDownloadResult, error) {
	if !post.HasPodcast() {
		return nil, nil
	}

	dir := filepath.Join(pd.outputDir, pd.podcastsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create podcasts directory: %w", err)
	}

	requestURL, resolve := podcastRequestURL(post)
	result, err := pd.media.download(ctx, mediaJob{
		requestURL:       requestURL,
		resolve:          resolve,
		basePath:         filepath.Join(dir, baseName),
		exts:             podcastExtensions,
		expectedDuration: post.fullPodcastDuration(),
		previewHint:      func(string) bool { return post.IsPodcastPreview() },
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download podcast audio: %w", err)
	}

	return &PodcastDownloadResult{
		LocalPath: result.LocalPath,
		Size:      result.Size,
		Preview:   result.Preview,
		Existed:   result.Existed,
	}, nil
}

// fullPodcastDuration returns the duration in seconds of the full episode, as
// far as the post's metadata tells, or 0 if unknown.
func (p *Post) fullPodcastDuration() float64 {
	duration := p.PodcastDuration
	if u := p.PodcastUpload; u != nil {
		duration = math.Max(duration, u.Duration)
		if u.FullPodcastInfo != nil {
			duration = math.Max(duration, u.FullPodcastInfo.Duration)
		}
	}
	return duration
}

// podcastRequestURL returns the URL to request the audio from, and whether it
// is a Substack URL to resolve with the cookie. Substack's podcast_url points
// at api.substack.com; see publicationOrigin for why we request it from the
// publication's own domain instead. Audio hosted elsewhere (imported podcasts)
// is requested as is, and never with the cookie.
func podcastRequestURL(post Post) (string, bool) {
	audioURL, err := url.Parse(post.PodcastURL)
	origin, ok := publicationOrigin(post)
	if err != nil || !ok {
		return post.PodcastURL, false
	}
	if audioURL.Host == "api.substack.com" {
		audioURL.Scheme = origin.Scheme
		audioURL.Host = origin.Host
	}
	return audioURL.String(), audioURL.Host == origin.Host
}
