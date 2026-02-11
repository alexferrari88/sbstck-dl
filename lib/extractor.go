package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	Id               int    `json:"id"`
	PublicationId    int    `json:"publication_id"`
	Type             string `json:"type"`
	Slug             string `json:"slug"`
	PostDate         string `json:"post_date"`
	CanonicalUrl     string `json:"canonical_url"`
	PreviousPostSlug string `json:"previous_post_slug"`
	NextPostSlug     string `json:"next_post_slug"`
	CoverImage       string `json:"cover_image"`
	Description      string `json:"description"`
	Subtitle         string `json:"subtitle,omitempty"`
	WordCount        int    `json:"wordcount"`
	Title            string `json:"title"`
	BodyHTML         string `json:"body_html"`
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

	content, err := p.contentForFormat(format, true)
	if err != nil {
		return err
	}

	if addSourceURL && p.CanonicalUrl != "" {
		sourceLine := fmt.Sprintf("\n\noriginal content: %s", p.CanonicalUrl) // Add separation

		// Adjust formatting slightly for HTML
		if format == "html" {
			sourceLine = fmt.Sprintf("<p style=\"margin-top: 2em; font-size: small; color: grey;\">original content: <a href=\"%s\">%s</a></p>", p.CanonicalUrl, p.CanonicalUrl)
		}
		content += sourceLine
	}

	return os.WriteFile(path, []byte(content), 0644)
}

// WriteToFileWithImages writes the Post's content to a file with optional image downloading
func (p *Post) WriteToFileWithImages(ctx context.Context, path string, format string, addSourceURL bool, 
	downloadImages bool, imageQuality ImageQuality, imagesDir string, 
	downloadFiles bool, fileExtensions []string, filesDir string, fetcher *Fetcher) (*ImageDownloadResult, error) {
	
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	content, err := p.contentForFormat(format, true)
	if err != nil {
		return nil, err
	}

	var imageResult *ImageDownloadResult

	// Download images if requested and format supports it
	if downloadImages && (format == "html" || format == "md") {
		outputDir := filepath.Dir(path)
		imageDownloader := NewImageDownloader(fetcher, outputDir, imagesDir, imageQuality)
		
		// Only process HTML content for image downloading
		htmlContent := content
		if format == "md" {
			// For markdown, we need to work with the original HTML
			htmlContent = p.BodyHTML
		}
		
		imageResult, err = imageDownloader.DownloadImages(ctx, htmlContent, p.Slug)
		if err != nil {
			return nil, fmt.Errorf("failed to download images: %w", err)
		}

		// Update content based on format
		if format == "html" {
			content = imageResult.UpdatedHTML
			// Re-add title if needed
			if strings.HasPrefix(content, "<h1>") {
				// Title already included
			} else {
				content = fmt.Sprintf("<h1>%s</h1>\n\n%s", p.Title, imageResult.UpdatedHTML)
			}
		} else if format == "md" {
			// Convert updated HTML to markdown
			updatedContent, err := mdConverter.ConvertString(imageResult.UpdatedHTML)
			if err != nil {
				return nil, fmt.Errorf("failed to convert updated HTML to markdown: %w", err)
			}
			content = fmt.Sprintf("# %s\n\n%s", p.Title, updatedContent)
		}
	} else if downloadImages && format == "txt" {
		// For text format, we can't embed images, but we can still download them
		outputDir := filepath.Dir(path)
		imageDownloader := NewImageDownloader(fetcher, outputDir, imagesDir, imageQuality)
		
		imageResult, err = imageDownloader.DownloadImages(ctx, p.BodyHTML, p.Slug)
		if err != nil {
			return nil, fmt.Errorf("failed to download images: %w", err)
		}
		// Keep original text content since we can't embed images in text format
	}

	// Download files if requested and format supports it
	if downloadFiles && (format == "html" || format == "md") {
		outputDir := filepath.Dir(path)
		fileDownloader := NewFileDownloader(fetcher, outputDir, filesDir, fileExtensions)
		
		// Process HTML content for file downloading - use the updated HTML from images if available
		htmlContent := content
		if imageResult != nil && imageResult.UpdatedHTML != "" {
			htmlContent = imageResult.UpdatedHTML
		} else if format == "md" {
			// For markdown, we need to work with the original HTML
			htmlContent = p.BodyHTML
		}
		
		fileResult, err := fileDownloader.DownloadFiles(ctx, htmlContent, p.Slug)
		if err != nil {
			return nil, fmt.Errorf("failed to download files: %w", err)
		}

		// Update content based on format if files were processed
		if fileResult.Success > 0 || fileResult.Failed > 0 {
			if format == "html" {
				content = fileResult.UpdatedHTML
				// Re-add title if needed
				if !strings.HasPrefix(content, "<h1>") {
					content = fmt.Sprintf("<h1>%s</h1>\n\n%s", p.Title, fileResult.UpdatedHTML)
				}
			} else if format == "md" {
				// Convert updated HTML to markdown
				updatedContent, err := mdConverter.ConvertString(fileResult.UpdatedHTML)
				if err != nil {
					return nil, fmt.Errorf("failed to convert updated HTML to markdown: %w", err)
				}
				content = fmt.Sprintf("# %s\n\n%s", p.Title, updatedContent)
			}
		}
	}

	// Add source URL if requested
	if addSourceURL && p.CanonicalUrl != "" {
		sourceLine := fmt.Sprintf("\n\noriginal content: %s", p.CanonicalUrl)

		// Adjust formatting slightly for HTML
		if format == "html" {
			sourceLine = fmt.Sprintf("<p style=\"margin-top: 2em; font-size: small; color: grey;\">original content: <a href=\"%s\">%s</a></p>", p.CanonicalUrl, p.CanonicalUrl)
		}
		content += sourceLine
	}

	// Write the file
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return imageResult, err
	}

	// Return empty result if no image downloading was performed
	if imageResult == nil {
		imageResult = &ImageDownloadResult{
			Images:      []ImageInfo{},
			UpdatedHTML: content,
			Success:     0,
			Failed:      0,
		}
	}

	return imageResult, nil
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
	Post         Post
	FilePath     string
	DownloadTime time.Time
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

func (e *Extractor) GetAllPostsURLs(ctx context.Context, pubUrl string, f DateFilterFunc) ([]string, error) {
	u, err := url.Parse(pubUrl)
	if err != nil {
		return nil, err
	}

	u.Path, err = url.JoinPath(u.Path, "sitemap.xml")
	if err != nil {
		return nil, err
	}

	// fetch the sitemap of the publication
	body, err := e.fetcher.FetchURL(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer body.Close()

	// Parse the XML
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, err
	}

	// Pre-allocate a reasonable size for URLs
	// This avoids multiple slice reallocations as we append
	urls := make([]string, 0, 100)

	doc.Find("url").EachWithBreak(func(i int, s *goquery.Selection) bool {
		// Check if the context has been cancelled
		select {
		case <-ctx.Done():
			return false
		default:
		}

		urlSel := s.Find("loc")
		url := urlSel.Text()
		if !strings.Contains(url, "/p/") {
			return true
		}

		// Only find lastmod if we have a filter
		if f != nil {
			lastmod := s.Find("lastmod").Text()
			if !f(lastmod) {
				return true
			}
		}

		urls = append(urls, url)
		return true
	})

	return urls, nil
}

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
	entry := ArchiveEntry{
		Post:         post,
		FilePath:     filePath,
		DownloadTime: downloadTime,
	}
	
	a.Entries = append(a.Entries, entry)
	a.sortEntries()
}

// sortEntries sorts archive entries by publication date (newest first)
func (a *Archive) sortEntries() {
	sort.Slice(a.Entries, func(i, j int) bool {
		// Parse post dates and compare (newest first)
		dateI, errI := time.Parse(time.RFC3339, a.Entries[i].Post.PostDate)
		dateJ, errJ := time.Parse(time.RFC3339, a.Entries[j].Post.PostDate)
		
		if errI != nil || errJ != nil {
			// If parsing fails, sort by title
			return a.Entries[i].Post.Title < a.Entries[j].Post.Title
		}
		
		return dateI.After(dateJ) // newest first
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
		if entry.Post.CoverImage != "" {
			html += fmt.Sprintf(`		<img src="%s" alt="Cover" class="cover-image">
`, entry.Post.CoverImage)
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
	
	return os.WriteFile(archivePath, []byte(html), 0644)
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
		if entry.Post.CoverImage != "" {
			content += fmt.Sprintf("![Cover Image](%s)\n\n", entry.Post.CoverImage)
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
	
	return os.WriteFile(archivePath, []byte(content), 0644)
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

	return os.WriteFile(archivePath, []byte(content), 0644)
}

// HasEntry checks if an entry with the given slug already exists in the archive
func (a *Archive) HasEntry(slug string) bool {
	for _, entry := range a.Entries {
		if entry.Post.Slug == slug {
			return true
		}
	}
	return false
}

// RemoveEntry removes an entry with the given slug from the archive
func (a *Archive) RemoveEntry(slug string) {
	for i, entry := range a.Entries {
		if entry.Post.Slug == slug {
			a.Entries = append(a.Entries[:i], a.Entries[i+1:]...)
			return
		}
	}
}

// ScanExistingPosts scans the output directory for existing post files and creates archive entries
// It parses filenames in the format YYYYMMDD_HHMMSS_{slug}.{format}
func ScanExistingPosts(outputDir string, format string) (*Archive, error) {
	archive := NewArchive()

	// Pattern to match post files
	pattern := filepath.Join(outputDir, fmt.Sprintf("*_*.%s", format))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return archive, fmt.Errorf("failed to scan directory: %w", err)
	}

	for _, filePath := range matches {
		// Skip index files
		base := filepath.Base(filePath)
		if base == "index."+format {
			continue
		}

		// Parse filename to extract date and slug
		// Format: YYYYMMDD_HHMMSS_{slug}.{format}
		entry, err := parsePostFile(filePath, format)
		if err != nil {
			// Skip files that don't match the expected format
			continue
		}

		archive.Entries = append(archive.Entries, entry)
	}

	// Sort entries by publication date
	archive.sortEntries()

	return archive, nil
}

// parsePostFile parses a post file and extracts metadata to create an archive entry
func parsePostFile(filePath string, format string) (ArchiveEntry, error) {
	base := filepath.Base(filePath)
	ext := "." + format

	// Remove extension
	name := strings.TrimSuffix(base, ext)

	// Parse date and slug from filename
	// Format: YYYYMMDD_HHMMSS_{slug}
	parts := strings.SplitN(name, "_", 3)
	if len(parts) < 3 {
		return ArchiveEntry{}, fmt.Errorf("invalid filename format: %s", base)
	}

	dateStr := parts[0]
	timeStr := parts[1]
	slug := parts[2]

	// Parse date from filename
	postDate, err := time.Parse("20060102_150405", dateStr+"_"+timeStr)
	if err != nil {
		return ArchiveEntry{}, fmt.Errorf("failed to parse date from filename: %w", err)
	}

	// Read file content to extract metadata
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ArchiveEntry{}, fmt.Errorf("failed to read file: %w", err)
	}

	// Extract title, subtitle, and cover image from content
	title, subtitle, coverImage := extractMetadataFromContent(string(content), format)

	// Get file modification time as download time
	fileInfo, err := os.Stat(filePath)
	downloadTime := time.Now()
	if err == nil {
		downloadTime = fileInfo.ModTime()
	}

	entry := ArchiveEntry{
		Post: Post{
			Slug:       slug,
			Title:      title,
			Subtitle:   subtitle,
			CoverImage: coverImage,
			PostDate:   postDate.Format(time.RFC3339),
		},
		FilePath:     filePath,
		DownloadTime: downloadTime,
	}

	return entry, nil
}

// extractMetadataFromContent extracts title, subtitle, and cover image from post content
func extractMetadataFromContent(content string, format string) (title, subtitle, coverImage string) {
	switch format {
	case "html":
		return extractMetadataFromHTML(content)
	case "md":
		return extractMetadataFromMarkdown(content)
	case "txt":
		return extractMetadataFromText(content)
	}
	return "", "", ""
}

// extractMetadataFromHTML extracts metadata from HTML content
func extractMetadataFromHTML(content string) (title, subtitle, coverImage string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return "", "", ""
	}

	// Extract title from h1
	title = strings.TrimSpace(doc.Find("h1").First().Text())

	// Extract subtitle from .subtitle-container or first paragraph after title
	subtitle = strings.TrimSpace(doc.Find(".subtitle-container").First().Text())
	if subtitle == "" {
		// Try to get first paragraph as subtitle
		subtitle = strings.TrimSpace(doc.Find("h1").First().Next().Text())
	}

	// Extract cover image
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && coverImage == "" {
			// Take first image as cover
			coverImage = src
		}
	})

	return title, subtitle, coverImage
}

// extractMetadataFromMarkdown extracts metadata from Markdown content
func extractMetadataFromMarkdown(content string) (title, subtitle, coverImage string) {
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Extract title from # heading
		if strings.HasPrefix(line, "# ") && title == "" {
			title = strings.TrimPrefix(line, "# ")
			// Get next non-empty line as potential subtitle
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine != "" && !strings.HasPrefix(nextLine, "#") && !strings.HasPrefix(nextLine, "![") {
					subtitle = nextLine
					break
				}
			}
		}

		// Extract cover image from ![...](...) syntax
		if strings.HasPrefix(line, "![") && coverImage == "" {
			start := strings.Index(line, "](")
			end := strings.LastIndex(line, ")")
			if start != -1 && end != -1 && start < end {
				coverImage = line[start+2 : end]
			}
		}
	}

	return title, subtitle, coverImage
}

// extractMetadataFromText extracts metadata from plain text content
func extractMetadataFromText(content string) (title, subtitle, coverImage string) {
	lines := strings.Split(content, "\n")

	// First non-empty line is the title
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			title = line
			// Get next non-empty line as subtitle
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				nextLine := strings.TrimSpace(lines[j])
				if nextLine != "" {
					subtitle = nextLine
					break
				}
			}
			break
		}
	}

	// No cover image extraction for text format
	return title, subtitle, ""
}
