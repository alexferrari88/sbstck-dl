package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/k3a/html2text"
)

// RawPost represents a raw Substack post in string format.
type RawPost struct {
	str string
}

// ToPost converts the RawPost to a structured Post object.
func (r *RawPost) ToPost() (Post, error) {
	var wrapper PostWrapper
	err := json.Unmarshal([]byte(r.str), &wrapper)
	if err != nil {
		return Post{}, err
	}
	return wrapper.Post, nil
}

// Post represents a structured Substack post with various fields.
type Post struct {
	Id               int          `json:"id"`
	PublicationId    int          `json:"publication_id"`
	Type             string       `json:"type"`
	Slug             string       `json:"slug"`
	PostDate         string       `json:"post_date"`
	CanonicalUrl     string       `json:"canonical_url"`
	PreviousPostSlug string       `json:"previous_post_slug"`
	NextPostSlug     string       `json:"next_post_slug"`
	CoverImage       string       `json:"cover_image"`
	Description      string       `json:"description"`
	Subtitle         string       `json:"subtitle,omitempty"`
	WordCount        int          `json:"wordcount"`
	Title            string       `json:"title"`
	BodyHTML         string       `json:"body_html"`
	PodcastUrl       string       `json:"podcast_url,omitempty"`
	VideoUpload      *VideoUpload `json:"videoUpload,omitempty"`
	PodcastUpload    *AudioUpload `json:"podcastUpload,omitempty"`
	VoiceoverUpload  *AudioUpload `json:"voiceoverUpload,omitempty"`
	Audience         string       `json:"audience,omitempty"`
	PodcastDuration  float64      `json:"podcast_duration,omitempty"`
	VideoUploadId    string       `json:"video_upload_id,omitempty"`
}

type VideoUpload struct {
	Id                   string       `json:"id"`
	Duration             float64      `json:"duration"`
	ExtractedAudio       *AudioUpload `json:"extractedAudio,omitempty"`
	MuxPreviewPlaybackID string       `json:"mux_preview_playback_id,omitempty"`
}

type AudioUpload struct {
	Id              string           `json:"id"`
	Duration        float64          `json:"duration"`
	IsFreePreview   bool             `json:"is_free_preview,omitempty"`
	FullPodcastInfo *FullPodcastInfo `json:"full_podcast_info,omitempty"`
}

// Static converter instance to avoid recreating it for each conversion
var mdConverter = md.NewConverter("", true, nil)

// ToMD converts the Post's HTML body to Markdown format.
func (p *Post) ToMD(withTitle bool) (string, error) {
	if withTitle {
		body, err := mdConverter.ConvertString(p.BodyHTML)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("# %s\n\n%s", p.Title, body), nil
	}

	return mdConverter.ConvertString(p.BodyHTML)
}

// ToText converts the Post's HTML body to plain text format.
func (p *Post) ToText(withTitle bool) string {
	if withTitle {
		return p.Title + "\n\n" + html2text.HTML2Text(p.BodyHTML)
	}
	return html2text.HTML2Text(p.BodyHTML)
}

// ToHTML returns the Post's HTML body as-is or with an optional title header.
func (p *Post) ToHTML(withTitle bool) string {
	if withTitle {
		return fmt.Sprintf("<h1>%s</h1>\n\n%s", p.Title, p.BodyHTML)
	}
	return p.BodyHTML
}

// ToJSON converts the Post to a JSON string.
func (p *Post) ToJSON() (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// contentForFormat returns the content of a post in the specified format.
func (p *Post) contentForFormat(format string, withTitle bool) (string, error) {
	switch format {
	case "html":
		return p.ToHTML(withTitle), nil
	case "md":
		return p.ToMD(withTitle)
	case "txt":
		return p.ToText(withTitle), nil
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

// WriteToFile writes the Post's content to a file in the specified format (html, md, or txt).
func (p *Post) WriteToFile(path string, format string, addSourceURL bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	content, err := p.fileContent(format, addSourceURL)
	if err != nil {
		return err
	}

	return writeFileAtomically(path, []byte(content))
}

// fileContent keeps source URL rendering identical across the public writers.
func (p *Post) fileContent(format string, addSourceURL bool) (string, error) {
	content, err := p.contentForFormat(format, true)
	if err != nil {
		return "", err
	}

	if addSourceURL && p.CanonicalUrl != "" {
		sourceLine := fmt.Sprintf("\n\noriginal content: %s", p.CanonicalUrl) // Add separation

		// Adjust formatting slightly for HTML
		if format == "html" {
			sourceLine = fmt.Sprintf("<p style=\"margin-top: 2em; font-size: small; color: grey;\">original content: <a href=\"%s\">%s</a></p>", p.CanonicalUrl, p.CanonicalUrl)
		}
		content += sourceLine
	}

	return content, nil
}

// PrepareMedia downloads and rewrites a canonical HTML body once for all outputs.
// Plain text continues to render the original body, without local image links.
func (p Post) PrepareMedia(ctx context.Context, outputDir string,
	downloadImages bool, imageQuality ImageQuality, imagesDir string,
	downloadFiles bool, fileExtensions []string, filesDir string, fetcher *Fetcher) (Post, *ImageDownloadResult, error) {
	prepared := p
	var images *ImageDownloadResult
	if downloadImages {
		var err error
		images, err = NewImageDownloader(fetcher, outputDir, imagesDir, imageQuality).DownloadImages(ctx, prepared.BodyHTML, p.Slug)
		if err != nil {
			return p, images, fmt.Errorf("failed to download images: %w", err)
		}
		prepared.BodyHTML = images.UpdatedHTML
	}
	if err := ctx.Err(); err != nil {
		return p, images, err
	}
	if downloadFiles {
		files, err := NewFileDownloader(fetcher, outputDir, filesDir, fileExtensions).DownloadFiles(ctx, prepared.BodyHTML, p.Slug)
		if err != nil {
			return p, images, fmt.Errorf("failed to download files: %w", err)
		}
		prepared.BodyHTML = files.UpdatedHTML
	}
	return prepared, images, ctx.Err()
}

// WriteToFileWithImages retains the single-output API over the shared media pass.
func (p *Post) WriteToFileWithImages(ctx context.Context, path string, format string, addSourceURL bool,
	downloadImages bool, imageQuality ImageQuality, imagesDir string,
	downloadFiles bool, fileExtensions []string, filesDir string, fetcher *Fetcher) (*ImageDownloadResult, error) {
	if _, err := p.contentForFormat(format, true); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	prepared, result, err := p.PrepareMedia(ctx, filepath.Dir(path), downloadImages, imageQuality, imagesDir,
		downloadFiles && format != "txt", fileExtensions, filesDir, fetcher)
	if err != nil {
		return result, err
	}
	if format == "txt" {
		prepared = *p
	}
	if err := prepared.WriteToFile(path, format, addSourceURL); err != nil {
		return result, err
	}
	if result == nil {
		content, err := prepared.fileContent(format, addSourceURL)
		if err != nil {
			return nil, err
		}
		result = &ImageDownloadResult{Images: []ImageInfo{}, UpdatedHTML: content}
	}
	return result, nil
}

// PostWrapper wraps a Post object for JSON unmarshaling.
type PostWrapper struct {
	Post Post `json:"post"`
}

// Extractor is a utility for extracting Substack posts from URLs.
type Extractor struct {
	fetcher *Fetcher
}

// ArchiveEntry represents a single entry in the archive page
type ArchiveEntry struct {
	Post            Post
	FilePath        string
	DownloadTime    time.Time
	LocalCoverImage string
	MediaUpdated    bool // Transient: body media belongs to a successfully written post this run.
}

// Archive represents a collection of posts for the archive page
type Archive struct {
	Entries []ArchiveEntry
}

// NewExtractor creates a new Extractor with the provided Fetcher.
// If the Fetcher is nil, a default Fetcher will be used.
func NewExtractor(f *Fetcher) *Extractor {
	if f == nil {
		f = NewFetcher()
	}
	return &Extractor{fetcher: f}
}

// extractJSONString finds and extracts the JSON data from script content.
// This optimized version reduces string operations.
func extractJSONString(doc *goquery.Document) (string, error) {
	var jsonString string
	var found bool

	doc.Find("script").EachWithBreak(func(i int, s *goquery.Selection) bool {
		content := s.Text()
		if strings.Contains(content, "window._preloads") && strings.Contains(content, "JSON.parse(") {
			start := strings.Index(content, "JSON.parse(\"")
			if start == -1 {
				return true
			}
			start += len("JSON.parse(\"")

			end := strings.LastIndex(content, "\")")
			if end == -1 || start >= end {
				return true
			}

			jsonString = content[start:end]
			found = true
			return false
		}
		return true
	})

	if !found {
		return "", errors.New("failed to extract JSON string")
	}

	return jsonString, nil
}

func (e *Extractor) ExtractPost(ctx context.Context, pageUrl string) (Post, error) {
	// fetch page HTML content
	body, err := e.fetcher.FetchURL(ctx, pageUrl)
	if err != nil {
		return Post{}, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer body.Close()

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return Post{}, fmt.Errorf("failed to parse HTML: %w", err)
	}

	jsonString, err := extractJSONString(doc)
	if err != nil {
		return Post{}, fmt.Errorf("failed to extract post data: %w", err)
	}

	// Unescape the JSON string directly
	var rawJSON RawPost
	err = json.Unmarshal([]byte("\""+jsonString+"\""), &rawJSON.str)
	if err != nil {
		return Post{}, fmt.Errorf("failed to unescape JSON: %w", err)
	}

	// Convert to a Go object
	p, err := rawJSON.ToPost()
	if err != nil {
		return Post{}, fmt.Errorf("failed to parse post data: %w", err)
	}

	// Extract additional metadata from HTML
	// Extract subtitle from .subtitle element
	if subtitle := doc.Find(".subtitle").First().Text(); subtitle != "" {
		p.Subtitle = strings.TrimSpace(subtitle)
	}

	// Extract cover image from og:image meta tag if not already set
	if p.CoverImage == "" {
		if ogImage, exists := doc.Find("meta[property='og:image']").Attr("content"); exists && ogImage != "" {
			p.CoverImage = ogImage
		}
	}

	return p, nil
}

type DateFilterFunc func(string) bool

type ExtractResult struct {
	Post Post
	Err  error
}

// ExtractAllPosts extracts all posts from the given URLs using a worker pool pattern
// to limit concurrency and avoid overwhelming system resources.
func (e *Extractor) ExtractAllPosts(ctx context.Context, urls []string) <-chan ExtractResult {
	resultCh := make(chan ExtractResult, len(urls))

	go func() {
		defer close(resultCh)

		// Create a channel for the URLs
		urlCh := make(chan string, len(urls))

		// Fill the URL channel
		for _, u := range urls {
			urlCh <- u
		}
		close(urlCh)

		// Limit concurrency - the number of workers is capped at 10 or the number of URLs, whichever is smaller
		workerCount := 10
		if len(urls) < workerCount {
			workerCount = len(urls)
		}

		// Create a WaitGroup to wait for all workers to finish
		var wg sync.WaitGroup
		wg.Add(workerCount)

		// Start the workers
		for i := 0; i < workerCount; i++ {
			go func() {
				defer wg.Done()

				for url := range urlCh {
					select {
					case <-ctx.Done():
						// Context cancelled, stop processing
						return
					default:
						post, err := e.ExtractPost(ctx, url)
						resultCh <- ExtractResult{Post: post, Err: err}
					}
				}
			}()
		}

		// Wait for all workers to finish
		wg.Wait()
	}()

	return resultCh
}

// NewArchive creates a new Archive instance
func NewArchive() *Archive {
	return &Archive{
		Entries: make([]ArchiveEntry, 0),
	}
}

// AddEntry adds a new entry to the archive, sorted by publication date (newest first)
func (a *Archive) AddEntry(post Post, filePath string, downloadTime time.Time) {
	if strings.TrimSpace(post.Title) == "" {
		post.Title = post.Slug
		if strings.TrimSpace(post.Title) == "" {
			post.Title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		}
	}
	localCover := ""
	for _, old := range a.Entries {
		if filepath.Clean(old.FilePath) == filepath.Clean(filePath) && old.Post.CoverImage == post.CoverImage {
			localCover = old.LocalCoverImage
			break
		}
	}
	entry := ArchiveEntry{
		Post:            post,
		FilePath:        filePath,
		DownloadTime:    downloadTime,
		LocalCoverImage: localCover,
	}

	a.RemoveEntry(filePath)
	a.Entries = append(a.Entries, entry)
	a.sortEntries()
}

// sortEntries sorts archive entries by publication date (newest first)
func (a *Archive) sortEntries() {
	sort.Slice(a.Entries, func(i, j int) bool {
		left, right := a.Entries[i], a.Entries[j]
		dateI, errI := time.Parse(time.RFC3339, left.Post.PostDate)
		dateJ, errJ := time.Parse(time.RFC3339, right.Post.PostDate)
		if (errI == nil) != (errJ == nil) {
			return errI == nil
		}
		if errI == nil && !dateI.Equal(dateJ) {
			return dateI.After(dateJ)
		}
		if left.Post.Title != right.Post.Title {
			return left.Post.Title < right.Post.Title
		}
		return left.FilePath < right.FilePath
	})
}

// GenerateHTML creates an HTML archive page
func (a *Archive) GenerateHTML(outputDir string) error {
	archivePath := filepath.Join(outputDir, "index.html")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Substack Archive</title>
	<style>
		body { font-family: Arial, sans-serif; max-width: 800px; margin: 0 auto; padding: 20px; }
		h1 { color: #333; }
		.post { margin-bottom: 30px; padding: 20px; border: 1px solid #eee; border-radius: 8px; }
		.post h2 { margin-top: 0; }
		.post h2 a { text-decoration: none; color: #ff6719; }
		.post h2 a:hover { text-decoration: underline; }
		.meta { color: #666; font-size: 14px; margin-bottom: 10px; }
		.subtitle { color: #777; font-style: italic; margin-bottom: 10px; }
		.cover-image { max-width: 200px; float: right; margin-left: 15px; }
	</style>
</head>
<body>
	<h1>Substack Archive</h1>
`

	for _, entry := range a.Entries {
		// Make file path relative from archive directory
		relPath, _ := filepath.Rel(outputDir, entry.FilePath)

		// Format publication date
		pubDate := entry.Post.PostDate
		if parsedDate, err := time.Parse(time.RFC3339, entry.Post.PostDate); err == nil {
			pubDate = parsedDate.Format("January 2, 2006")
		}

		// Format download date
		downloadDate := entry.DownloadTime.Format("January 2, 2006 15:04")

		html += `	<div class="post">
`

		// Add cover image if available
		coverImage := entry.CoverReference(outputDir)
		if coverImage != "" {
			html += fmt.Sprintf(`		<img src="%s" alt="Cover" class="cover-image">
`, coverImage)
		}

		html += fmt.Sprintf(`		<h2><a href="%s">%s</a></h2>
		<div class="meta">Published: %s | Downloaded: %s</div>
`, relPath, entry.Post.Title, pubDate, downloadDate)

		// Add subtitle/description
		description := entry.Post.Subtitle
		if description == "" {
			description = entry.Post.Description
		}
		if description != "" {
			html += fmt.Sprintf(`		<div class="subtitle">%s</div>
`, description)
		}

		html += `	</div>
`
	}

	html += `</body>
</html>`

	return writeFileAtomically(archivePath, []byte(html))
}

// GenerateMarkdown creates a Markdown archive page
func (a *Archive) GenerateMarkdown(outputDir string) error {
	archivePath := filepath.Join(outputDir, "index.md")

	content := "# Substack Archive\n\n"

	for _, entry := range a.Entries {
		// Make file path relative from archive directory
		relPath, _ := filepath.Rel(outputDir, entry.FilePath)

		// Format publication date
		pubDate := entry.Post.PostDate
		if parsedDate, err := time.Parse(time.RFC3339, entry.Post.PostDate); err == nil {
			pubDate = parsedDate.Format("January 2, 2006")
		}

		// Format download date
		downloadDate := entry.DownloadTime.Format("January 2, 2006 15:04")

		content += fmt.Sprintf("## [%s](%s)\n\n", entry.Post.Title, relPath)
		content += fmt.Sprintf("**Published:** %s | **Downloaded:** %s\n\n", pubDate, downloadDate)

		// Add cover image if available
		coverImage := entry.CoverReference(outputDir)
		if coverImage != "" {
			content += fmt.Sprintf("![Cover Image](%s)\n\n", coverImage)
		}

		// Add subtitle/description
		description := entry.Post.Subtitle
		if description == "" {
			description = entry.Post.Description
		}
		if description != "" {
			content += fmt.Sprintf("*%s*\n\n", description)
		}

		content += "---\n\n"
	}

	return writeFileAtomically(archivePath, []byte(content))
}

// GenerateText creates a plain text archive page
func (a *Archive) GenerateText(outputDir string) error {
	archivePath := filepath.Join(outputDir, "index.txt")

	content := "SUBSTACK ARCHIVE\n================\n\n"

	for _, entry := range a.Entries {
		// Make file path relative from archive directory
		relPath, _ := filepath.Rel(outputDir, entry.FilePath)

		// Format publication date
		pubDate := entry.Post.PostDate
		if parsedDate, err := time.Parse(time.RFC3339, entry.Post.PostDate); err == nil {
			pubDate = parsedDate.Format("January 2, 2006")
		}

		// Format download date
		downloadDate := entry.DownloadTime.Format("January 2, 2006 15:04")

		content += fmt.Sprintf("Title: %s\n", entry.Post.Title)
		content += fmt.Sprintf("File: %s\n", relPath)
		content += fmt.Sprintf("Published: %s\n", pubDate)
		content += fmt.Sprintf("Downloaded: %s\n", downloadDate)

		// Add subtitle/description
		description := entry.Post.Subtitle
		if description == "" {
			description = entry.Post.Description
		}
		if description != "" {
			content += fmt.Sprintf("Description: %s\n", description)
		}

		content += "\n" + strings.Repeat("-", 50) + "\n\n"
	}

	return writeFileAtomically(archivePath, []byte(content))
}
