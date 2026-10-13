package cmd

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var (
	downloadUrl    string
	format         string
	formatValues   []string
	outputFolder   string
	dryRun         bool
	addSourceURL   bool
	downloadImages bool
	imageQuality   string
	imagesDir      string
	downloadFiles  bool
	fileExtensions string
	filesDir       string
	createArchive  bool

	downloadPodcasts bool
	podcastsDir      string
	downloadVideos   bool
	videosDir        string

	downloadCmd = &cobra.Command{
		Use:   "download",
		Short: "Download individual posts or the entire public archive",
		Long:  `You can provide the url of a single post or the main url of the Substack you want to download.`,
		Args:  cobra.NoArgs,
		RunE:  runDownload,
	}
)

func init() {
	downloadCmd.Flags().StringVarP(&downloadUrl, "url", "u", "", "Specify the Substack url")
	downloadCmd.Flags().StringArrayVarP(&formatValues, "format", "f", nil, "Output formats: html, md, txt (comma-separated, space-separated, or repeated; default: html)")
	downloadCmd.Flags().StringVarP(&outputFolder, "output", "o", ".", "Specify the download directory")
	downloadCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Enable dry run")
	downloadCmd.Flags().BoolVar(&addSourceURL, "add-source-url", false, "Add the original post URL at the end of the downloaded file")
	downloadCmd.Flags().BoolVar(&downloadImages, "download-images", false, "Download images locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&imageQuality, "image-quality", "high", "Image quality to download (options: \"high\", \"medium\", \"low\")")
	downloadCmd.Flags().StringVar(&imagesDir, "images-dir", "images", "Directory name for downloaded images")
	downloadCmd.Flags().BoolVar(&downloadFiles, "download-files", false, "Download file attachments locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&fileExtensions, "file-extensions", "", "Comma-separated list of file extensions to download (e.g., 'pdf,docx,txt'). If empty, downloads all file types")
	downloadCmd.Flags().StringVar(&filesDir, "files-dir", "files", "Directory name for downloaded file attachments")
	downloadCmd.Flags().BoolVar(&createArchive, "create-archive", false, "Create an archive index page linking all downloaded posts")
	downloadCmd.Flags().BoolVar(&downloadPodcasts, "download-podcasts", false, "Download podcast episode audio (MP3) for podcast posts")
	downloadCmd.Flags().StringVar(&podcastsDir, "podcasts-dir", "podcasts", "Directory name for downloaded podcast audio")
	downloadCmd.Flags().BoolVar(&downloadVideos, "download-videos", false, "Download Substack-hosted videos (MP4) of video posts and videos embedded in posts")
	downloadCmd.Flags().StringVar(&videosDir, "videos-dir", "videos", "Directory name for downloaded videos")
	downloadCmd.MarkFlagRequired("url")
}

func convertDateTime(datetime string) string {
	// Parse the datetime string
	parsedTime, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		// Return an empty string or an error message if parsing fails
		return ""
	}

	// Format the datetime to the desired format
	formattedDateTime := fmt.Sprintf("%d%02d%02d_%02d%02d%02d",
		parsedTime.Year(), parsedTime.Month(), parsedTime.Day(),
		parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second())

	return formattedDateTime
}

func parseURL(toTest string) (*url.URL, error) {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(toTest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, err
	}

	return u, err
}

func makePath(post lib.Post, outputFolder string, format string) string {
	return fmt.Sprintf("%s/%s_%s.%s", outputFolder, convertDateTime(post.PostDate), post.Slug, format)
}

// extractSlug extracts the slug from a Substack post URL
// e.g. https://example.substack.com/p/this-is-the-post-title -> this-is-the-post-title
func extractSlug(url string) string {
	split := strings.Split(url, "/")
	return split[len(split)-1]
}

// filterExistingPosts filters out posts that already exist in the output folder.
// It looks for files whose name ends with the post slug.
func filterExistingPosts(urls []string, outputFolder string, format string) ([]string, error) {
	var filtered []string
	for _, url := range urls {
		slug := extractSlug(url)
		path := fmt.Sprintf("%s/%s_%s.%s", outputFolder, "*", slug, format)
		matches, err := filepath.Glob(path)
		if err != nil {
			return urls, err
		}
		if len(matches) == 0 {
			filtered = append(filtered, url)
		}
	}
	return filtered, nil
}

// The same success check applies to plain posts and posts with downloaded media.
func writeDownloadedPost(post lib.Post, path string) (*lib.ImageDownloadResult, error) {
	if !downloadImages && !downloadFiles {
		return nil, post.WriteToFile(path, format, addSourceURL)
	}
	var extensions []string
	if fileExtensions != "" {
		extensions = strings.Split(strings.ReplaceAll(fileExtensions, " ", ""), ",")
	}
	result, err := post.WriteToFileWithImages(ctx, path, format, addSourceURL, downloadImages, lib.ImageQuality(imageQuality), imagesDir, downloadFiles, extensions, filesDir, fetcher)
	if err == nil && verbose && result.Success > 0 {
		fmt.Printf("Downloaded %d images (%d failed) for post %s\n", result.Success, result.Failed, post.Slug)
	}
	return result, err
}

// postMedia keeps download status pending until every relevant post output has
// been published. Retained bytes can then be reused when publication is retried.
type postMedia struct {
	embedded lib.EmbeddedMedia
	outcomes []mediaOutcome
	staged   bool
}

type mediaOutcome struct {
	kind     string
	complete bool
	files    []string
}

func downloadPostMedia(manifest *lib.MediaManifest, post lib.Post, postPath string) (status postMedia, err error) {
	// Invalidate byte-only completion before repairing a missing/empty cache.
	// Otherwise a repaired file could make the old complete record look valid
	// if pending publication or its manifest save subsequently fails.
	if downloadPodcasts {
		manifest.Record(post.Slug, lib.MediaPodcast, false, nil)
	}
	if downloadVideos {
		manifest.Record(post.Slug, lib.MediaVideos, false, nil)
	}
	if saveErr := manifest.Save(); saveErr != nil {
		return status, fmt.Errorf("save pending media for %s before download: %w", post.Slug, saveErr)
	}
	var failures []error
	if downloadPodcasts {
		complete, files, src, downloadErr := downloadPostPodcast(post, postPath)
		status.embedded.Podcast = src
		status.outcomes = append(status.outcomes, mediaOutcome{lib.MediaPodcast, complete, files})
		failures = append(failures, downloadErr)
	}
	if downloadVideos {
		complete, files, srcs, downloadErr := downloadPostVideos(post, postPath)
		status.embedded.Videos = srcs
		status.outcomes = append(status.outcomes, mediaOutcome{lib.MediaVideos, complete, files})
		failures = append(failures, downloadErr)
	}
	// Persist pending output work before touching existing or new post files.
	for _, outcome := range status.outcomes {
		manifest.Record(post.Slug, outcome.kind, false, outcome.files)
	}
	if saveErr := manifest.Save(); saveErr != nil {
		failures = append(failures, fmt.Errorf("save pending media for %s: %w", post.Slug, saveErr))
	} else {
		status.staged = true
	}
	return status, errors.Join(failures...)
}

func postVideoID(post lib.Post) string {
	if post.VideoUploadId != "" {
		return post.VideoUploadId
	}
	if post.VideoUpload != nil {
		return post.VideoUpload.Id
	}
	return ""
}

// Embed through the library's failure-safe publication contract, preserving
// unrelated edits and the existing file mode.
func embedMediaInFile(path string, post lib.Post, media lib.EmbeddedMedia) error {
	return lib.EmbedMediaInFile(path, postVideoID(post), media)
}

func completePostMedia(manifest *lib.MediaManifest, post lib.Post, status postMedia) error {
	if !status.staged {
		return nil
	}
	for _, outcome := range status.outcomes {
		complete := outcome.complete
		// Without a cookie, paywalled posts can hide media entirely. A full
		// podcast is the exception, because each post has only one episode.
		if post.IsPaywalled() && idCookieVal == "" && !(outcome.kind == lib.MediaPodcast && len(outcome.files) > 0) {
			complete = false
		}
		manifest.Record(post.Slug, outcome.kind, complete, outcome.files)
	}
	if err := manifest.Save(); err != nil {
		// Keep in-memory filtering retryable as well as the previously persisted
		// pending records when atomic publication fails.
		for _, outcome := range status.outcomes {
			manifest.Record(post.Slug, outcome.kind, false, outcome.files)
		}
		return fmt.Errorf("save completed media for %s: %w", post.Slug, err)
	}
	return nil
}

// postNeedsMedia reports whether an already downloaded post must be fetched
// again because some requested media isn't complete yet.
func postNeedsMedia(manifest *lib.MediaManifest, slug string) bool {
	return (downloadPodcasts && !manifest.IsComplete(slug, lib.MediaPodcast)) ||
		(downloadVideos && !manifest.IsComplete(slug, lib.MediaVideos))
}

// downloadPostPodcast downloads the post's podcast audio next to the post file.
// The audio file shares the post file's base name. It returns whether there is
// nothing left to download, the downloaded file, and its source relative to
// the post file.
func downloadPostPodcast(post lib.Post, postPath string) (complete bool, files []string, src string, err error) {
	if post.IsPodcastUnavailable() {
		log.Printf("Warning: the podcast audio for post %s is not available (paid episode); "+
			"pass --cookie_name/--cookie_val of a paid subscription to download it\n", post.Slug)
		return false, nil, "", nil
	}
	if !post.HasPodcast() {
		return true, nil, "", nil
	}

	baseName := strings.TrimSuffix(filepath.Base(postPath), filepath.Ext(postPath))
	podcastDownloader := lib.NewPodcastDownloader(fetcher, filepath.Dir(postPath), podcastsDir)
	result, err := podcastDownloader.DownloadPodcast(ctx, post, baseName)
	if err != nil {
		return false, nil, "", fmt.Errorf("download podcast for %s: %w", post.Slug, err)
	}

	if result.Preview {
		log.Printf("Warning: only a preview of the podcast for post %s is available (paid episode); "+
			"pass --cookie_name/--cookie_val of a paid subscription to download the full episode\n", post.Slug)
	}
	if verbose {
		if result.Existed {
			fmt.Printf("Podcast already downloaded: %s\n", result.LocalPath)
		} else {
			fmt.Printf("Downloaded podcast (%.1f MB) to %s\n", float64(result.Size)/1e6, result.LocalPath)
		}
	}
	return !result.Preview, []string{result.LocalPath}, result.Source(filepath.Dir(postPath)), nil
}

// downloadPostVideos downloads the post's Substack-hosted videos next to the
// post file. The video files share the post file's base name. It returns
// whether there is nothing left to download, the downloaded files, and their
// sources relative to the post file.
func downloadPostVideos(post lib.Post, postPath string) (complete bool, files []string, srcs map[string]string, err error) {
	baseName := strings.TrimSuffix(filepath.Base(postPath), filepath.Ext(postPath))
	videoDownloader := lib.NewVideoDownloader(fetcher, filepath.Dir(postPath), videosDir)
	result, err := videoDownloader.DownloadVideos(ctx, post, baseName)
	if err != nil {
		return false, nil, nil, fmt.Errorf("download videos for %s: %w", post.Slug, err)
	}

	var failures []error
	complete = true
	for _, video := range result.Videos {
		switch {
		case video.Error != nil:
			failures = append(failures, fmt.Errorf("download video %s for %s: %w", video.UploadID, post.Slug, video.Error))
			complete = false
			continue
		case video.Preview:
			log.Printf("Warning: only a preview of video %s for post %s is available (paid video); "+
				"pass --cookie_name/--cookie_val of a paid subscription to download the full video\n", video.UploadID, post.Slug)
			complete = false
		}
		files = append(files, video.LocalPath)
		if verbose {
			if video.Existed {
				fmt.Printf("Video already downloaded: %s\n", video.LocalPath)
			} else {
				fmt.Printf("Downloaded video (%.1f MB) to %s\n", float64(video.Size)/1e6, video.LocalPath)
			}
		}
	}
	return complete, files, result.Sources(filepath.Dir(postPath)), errors.Join(failures...)
}
