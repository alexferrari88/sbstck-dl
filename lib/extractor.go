package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
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
	Id               int       `json:"id"`
	PublicationId    int       `json:"publication_id"`
	Type             string    `json:"type"`
	Slug             string    `json:"slug"`
	PostDate         string    `json:"post_date"`
	PublishedAt      string    `json:"published_at,omitempty"`
	FirstPublishedAt string    `json:"first_published_at,omitempty"`
	CanonicalUrl     string    `json:"canonical_url"`
	PreviousPostSlug string    `json:"previous_post_slug"`
	NextPostSlug     string    `json:"next_post_slug"`
	CoverImage       string    `json:"cover_image"`
	Description      string    `json:"description"`
	Subtitle         string    `json:"subtitle,omitempty"`
	WordCount        int       `json:"wordcount"`
	Title            string    `json:"title"`
	BodyHTML         string    `json:"body_html"`
	Tags             []PostTag `json:"tags,omitempty"`
	Categories       []PostTag `json:"categories,omitempty"`
}

type PostTag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (t *PostTag) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return nil
		}
		t.Name = value
		return nil
	}
	type alias PostTag
	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return nil
	}
	*t = PostTag(aux)
	return nil
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
	case "obsidian-md":
		return p.ToObsidianMD(withTitle)
	case "txt":
		return p.ToText(withTitle), nil
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

// WriteToFile writes the Post's content to a file in the specified format (html, md, obsidian-md, or txt).
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
	if downloadImages && (format == "html" || format == "md" || format == "obsidian-md") {
		outputDir := filepath.Dir(path)
		imageDownloader := NewImageDownloader(fetcher, outputDir, imagesDir, imageQuality)

		// Only process HTML content for image downloading
		htmlContent := content
		if format == "md" || format == "obsidian-md" {
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
		} else if format == "obsidian-md" {
			updatedContent, err := convertHTMLToObsidianMarkdown(imageResult.UpdatedHTML, *p, true)
			if err != nil {
				return nil, fmt.Errorf("failed to convert updated HTML to Obsidian markdown: %w", err)
			}
			content = updatedContent
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
	if downloadFiles && (format == "html" || format == "md" || format == "obsidian-md") {
		outputDir := filepath.Dir(path)
		fileDownloader := NewFileDownloader(fetcher, outputDir, filesDir, fileExtensions)

		// Process HTML content for file downloading - use the updated HTML from images if available
		htmlContent := content
		if imageResult != nil && imageResult.UpdatedHTML != "" {
			htmlContent = imageResult.UpdatedHTML
		} else if format == "md" || format == "obsidian-md" {
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
			} else if format == "obsidian-md" {
				updatedContent, err := convertHTMLToObsidianMarkdown(fileResult.UpdatedHTML, *p, true)
				if err != nil {
					return nil, fmt.Errorf("failed to convert updated HTML to Obsidian markdown: %w", err)
				}
				content = updatedContent
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

type notionLinksSidecar struct {
	NotionLinks []string `json:"notion_links"`
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

type SitemapEntry struct {
	URL     string
	LastMod string
}

func (e *Extractor) GetAllPostsURLs(ctx context.Context, pubUrl string, f DateFilterFunc) ([]string, error) {
	entries, err := e.GetAllPostsEntries(ctx, pubUrl, f)
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}

	return urls, nil
}

func (e *Extractor) GetAllPostsEntries(ctx context.Context, pubUrl string, f DateFilterFunc) ([]SitemapEntry, error) {
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
	entries := make([]SitemapEntry, 0, 100)

	doc.Find("url").EachWithBreak(func(i int, s *goquery.Selection) bool {
		// Check if the context has been cancelled
		select {
		case <-ctx.Done():
			return false
		default:
		}

		urlSel := s.Find("loc")
		loc := urlSel.Text()
		if !strings.Contains(loc, "/p/") {
			return true
		}

		// Only find lastmod if we have a filter
		lastmod := s.Find("lastmod").Text()
		if f != nil {
			if !f(lastmod) {
				return true
			}
		}

		entries = append(entries, SitemapEntry{URL: loc, LastMod: lastmod})
		return true
	})

	return entries, nil
}

type ExtractResult struct {
	Url  string
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

		// Limit concurrency - the number of workers is capped at configured max or the number of URLs, whichever is smaller
		workerCount := 10
		if e.fetcher != nil && e.fetcher.MaxWorkers > 0 {
			workerCount = e.fetcher.MaxWorkers
		}
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
						resultCh <- ExtractResult{Url: url, Post: post, Err: err}
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

	type archiveHTMLEntry struct {
		Title            string
		Description      string
		CoverImage       string
		CanonicalURL     string
		RelPath          string
		PubDateDisplay   string
		PubDateISO       string
		DownloadDisplay  string
		Year             string
		WordCount        int
		EstimatedReadMin int
		NotionCount      int
		Domains          string
	}

	type archiveYearGroup struct {
		Year    string
		Entries []archiveHTMLEntry
	}

	type archiveDomainGroup struct {
		Domain string
		Count  int
	}

	type archiveHTMLPage struct {
		GeneratedAt  string
		TotalPosts   int
		YearGroups   []archiveYearGroup
		DomainGroups []archiveDomainGroup
	}

	generatedAt := time.Now().Format("January 2, 2006 15:04")
	page := archiveHTMLPage{
		GeneratedAt:  generatedAt,
		TotalPosts:   len(a.Entries),
		YearGroups:   make([]archiveYearGroup, 0),
		DomainGroups: make([]archiveDomainGroup, 0),
	}

	yearGroupIndex := make(map[string]int)
	yearOrder := make([]string, 0)
	domainCounts := make(map[string]int)

	for _, entry := range a.Entries {
		relPath, _ := filepath.Rel(outputDir, entry.FilePath)
		relPath = filepath.ToSlash(relPath)

		pubDateDisplay := entry.Post.PostDate
		pubDateISO := entry.Post.PostDate
		year := "Unknown"
		if parsedDate, err := time.Parse(time.RFC3339, entry.Post.PostDate); err == nil {
			pubDateDisplay = parsedDate.Format("January 2, 2006")
			year = fmt.Sprintf("%d", parsedDate.Year())
			pubDateISO = parsedDate.Format(time.RFC3339)
		}

		downloadDisplay := entry.DownloadTime.Format("January 2, 2006 15:04")

		description := entry.Post.Subtitle
		if description == "" {
			description = entry.Post.Description
		}

		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.FilePath)), ".")
		if format == "" {
			format = "html"
		}

		notionCount := 0
		if links, ok := readNotionLinksSidecar(entry.FilePath); ok {
			notionCount = len(links)
		}

		domains := make([]string, 0)
		if content, err := os.ReadFile(entry.FilePath); err == nil {
			domains = ExtractLinkDomains(string(content), format)
			if notionCount == 0 {
				notionCount = len(ExtractNotionLinks(string(content), format))
			}
		}
		for _, domain := range domains {
			domainCounts[domain]++
		}

		estimatedReadMin := 0
		if entry.Post.WordCount > 0 {
			estimatedReadMin = entry.Post.WordCount / 200
			if estimatedReadMin < 1 {
				estimatedReadMin = 1
			}
		}

		htmlEntry := archiveHTMLEntry{
			Title:            entry.Post.Title,
			Description:      description,
			CoverImage:       entry.Post.CoverImage,
			CanonicalURL:     entry.Post.CanonicalUrl,
			RelPath:          relPath,
			PubDateDisplay:   pubDateDisplay,
			PubDateISO:       pubDateISO,
			DownloadDisplay:  downloadDisplay,
			Year:             year,
			WordCount:        entry.Post.WordCount,
			EstimatedReadMin: estimatedReadMin,
			NotionCount:      notionCount,
			Domains:          strings.Join(domains, ","),
		}

		idx, ok := yearGroupIndex[year]
		if !ok {
			yearOrder = append(yearOrder, year)
			yearGroupIndex[year] = len(page.YearGroups)
			page.YearGroups = append(page.YearGroups, archiveYearGroup{Year: year, Entries: []archiveHTMLEntry{htmlEntry}})
			continue
		}
		page.YearGroups[idx].Entries = append(page.YearGroups[idx].Entries, htmlEntry)
	}

	for domain, count := range domainCounts {
		page.DomainGroups = append(page.DomainGroups, archiveDomainGroup{
			Domain: domain,
			Count:  count,
		})
	}
	sort.Slice(page.DomainGroups, func(i, j int) bool {
		if page.DomainGroups[i].Count != page.DomainGroups[j].Count {
			return page.DomainGroups[i].Count > page.DomainGroups[j].Count
		}
		return page.DomainGroups[i].Domain < page.DomainGroups[j].Domain
	})

	// Keep "Unknown" last when it exists; otherwise preserve the archive's publication-date order.
	if len(page.YearGroups) > 1 {
		hasUnknown := false
		for _, y := range yearOrder {
			if y == "Unknown" {
				hasUnknown = true
				break
			}
		}
		if hasUnknown {
			// Stable sort by numeric year desc, then "Unknown" last.
			sort.SliceStable(page.YearGroups, func(i, j int) bool {
				yi := page.YearGroups[i].Year
				yj := page.YearGroups[j].Year
				if yi == "Unknown" {
					return false
				}
				if yj == "Unknown" {
					return true
				}
				return yi > yj
			})
		}
	}

	const tpl = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Substack Archive</title>
	<style>
		:root {
			--bg: #ffffff;
			--fg: #111827;
			--muted: #6b7280;
			--card: #ffffff;
			--border: #e5e7eb;
			--accent: #ff6719;
			--accent-weak: rgba(255, 103, 25, 0.12);
			--shadow: 0 1px 2px rgba(0,0,0,0.06);
			--shadow-strong: 0 8px 30px rgba(0,0,0,0.10);
		}
		@media (prefers-color-scheme: dark) {
			:root {
				--bg: #0b0f19;
				--fg: #e5e7eb;
				--muted: #9ca3af;
				--card: #0f1629;
				--border: rgba(255,255,255,0.12);
				--accent-weak: rgba(255, 103, 25, 0.18);
				--shadow: 0 1px 2px rgba(0,0,0,0.35);
				--shadow-strong: 0 12px 40px rgba(0,0,0,0.50);
			}
		}
		[data-theme="light"] { color-scheme: light; }
		[data-theme="dark"] { color-scheme: dark; }

		* { box-sizing: border-box; }
		body {
			margin: 0;
			font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, "Apple Color Emoji", "Segoe UI Emoji";
			background: var(--bg);
			color: var(--fg);
		}
		a { color: inherit; }

		.container { max-width: 1100px; margin: 0 auto; padding: 18px 16px 48px; }

		header {
			position: sticky;
			top: 0;
			z-index: 10;
			background: color-mix(in srgb, var(--bg) 88%, transparent);
			backdrop-filter: blur(10px);
			border-bottom: 1px solid var(--border);
		}
		.header-inner { max-width: 1100px; margin: 0 auto; padding: 12px 16px; display: grid; gap: 10px; }

		.title-row { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
		h1 { margin: 0; font-size: 20px; letter-spacing: -0.02em; }
		.meta { color: var(--muted); font-size: 13px; }

		.controls { display: grid; grid-template-columns: 1fr auto auto auto auto auto; gap: 10px; align-items: center; }
		.controls input[type="search"],
		.controls input[type="date"] {
			width: 100%;
			padding: 10px 12px;
			border: 1px solid var(--border);
			background: var(--card);
			color: var(--fg);
			border-radius: 10px;
			box-shadow: var(--shadow);
		}
		.controls select, .controls button, .controls label {
			padding: 10px 12px;
			border: 1px solid var(--border);
			background: var(--card);
			color: var(--fg);
			border-radius: 10px;
			box-shadow: var(--shadow);
			font-size: 13px;
		}
		.controls label { display: inline-flex; gap: 8px; align-items: center; cursor: pointer; user-select: none; }
		.controls input[type="checkbox"] { transform: translateY(0.5px); }
		@media (max-width: 860px) {
			.controls { grid-template-columns: 1fr 1fr; }
			.controls input[type="search"] { grid-column: 1 / -1; }
		}

		.layout { display: grid; grid-template-columns: 190px 1fr; gap: 18px; margin-top: 18px; }
		@media (max-width: 860px) { .layout { grid-template-columns: 1fr; } }

		.year-nav {
			position: sticky;
			top: 104px;
			align-self: start;
			border: 1px solid var(--border);
			background: var(--card);
			border-radius: 12px;
			box-shadow: var(--shadow);
			padding: 10px;
		}
		.year-nav h2 { margin: 4px 8px 8px; font-size: 12px; color: var(--muted); font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; }
		.year-nav a {
			display: flex;
			justify-content: space-between;
			gap: 10px;
			padding: 8px 10px;
			border-radius: 10px;
			text-decoration: none;
		}
		.year-nav a:hover { background: var(--accent-weak); }
		.year-nav .count { color: var(--muted); font-variant-numeric: tabular-nums; }
		.year-nav .divider { height: 1px; background: var(--border); margin: 10px 6px; }
		.year-nav .domain-link.active { background: var(--accent-weak); font-weight: 600; }
		@media (max-width: 860px) { .year-nav { position: static; } }

		main { min-width: 0; }
		.year-section { margin-bottom: 26px; }
		.year-header {
			display: flex;
			align-items: baseline;
			justify-content: space-between;
			gap: 10px;
			margin: 0 0 12px;
		}
		.year-header h2 { margin: 0; font-size: 18px; letter-spacing: -0.01em; }
		.year-header .year-meta { color: var(--muted); font-size: 13px; }

		.grid { display: grid; grid-template-columns: 1fr; gap: 12px; }
		@media (min-width: 740px) { .grid { grid-template-columns: 1fr 1fr; } }

		.post-card {
			border: 1px solid var(--border);
			background: var(--card);
			border-radius: 14px;
			box-shadow: var(--shadow);
			overflow: hidden;
			display: grid;
			grid-template-columns: 128px 1fr;
			min-height: 110px;
		}
		.post-card.hidden { display: none; }
		.no-covers .post-card { grid-template-columns: 1fr; }

		.cover {
			width: 100%;
			height: 100%;
			display: block;
			object-fit: cover;
			background: color-mix(in srgb, var(--border) 30%, transparent);
		}
		.no-covers .cover-wrap { display: none; }
		.cover-wrap { border-right: 1px solid var(--border); }

		.post-body { padding: 12px 12px 12px 14px; min-width: 0; display: grid; gap: 6px; }
		.post-title { margin: 0; font-size: 15px; line-height: 1.25; letter-spacing: -0.01em; }
		.post-title a { text-decoration: none; }
		.post-title a:hover { text-decoration: underline; text-decoration-color: color-mix(in srgb, var(--accent) 65%, transparent); }
		.post-meta { color: var(--muted); font-size: 12.5px; display: flex; flex-wrap: wrap; gap: 8px 10px; }
		.post-desc { color: color-mix(in srgb, var(--fg) 82%, var(--muted)); font-size: 13px; line-height: 1.35; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
		.links { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 2px; }
		.links a {
			display: inline-flex;
			align-items: center;
			gap: 6px;
			padding: 7px 10px;
			border-radius: 10px;
			border: 1px solid var(--border);
			background: transparent;
			text-decoration: none;
			font-size: 12.5px;
		}
		.links a.primary { border-color: color-mix(in srgb, var(--accent) 45%, var(--border)); background: var(--accent-weak); }
		.links a.notion-badge { border-color: color-mix(in srgb, #0f172a 25%, var(--border)); background: color-mix(in srgb, #0f172a 12%, transparent); }
		.links a:hover { box-shadow: var(--shadow-strong); transform: translateY(-0.5px); transition: 0.12s ease; }

		.footer-note { margin-top: 22px; color: var(--muted); font-size: 12px; }
	</style>
</head>
<body>
	<header>
		<div class="header-inner">
			<div class="title-row">
				<h1>Substack Archive</h1>
				<div class="meta">
					<span id="resultCount">{{.TotalPosts}}</span> posts · Generated {{.GeneratedAt}}
				</div>
			</div>
			<div class="controls" role="region" aria-label="Archive controls">
				<input id="search" type="search" placeholder="Search title or description…" autocomplete="off" />
				<input id="filterFrom" type="date" aria-label="Filter from date" />
				<input id="filterTo" type="date" aria-label="Filter to date" />
				<select id="sort">
					<option value="newest" selected>Newest</option>
					<option value="oldest">Oldest</option>
					<option value="title">Title</option>
				</select>
				<label title="Toggle cover thumbnails">
					<input id="toggleCovers" type="checkbox" checked />
					Covers
				</label>
				<button id="toggleTheme" type="button" title="Toggle theme">Theme</button>
			</div>
			<div class="meta" id="status">Showing {{.TotalPosts}} of {{.TotalPosts}}</div>
		</div>
	</header>

	<div class="container layout">
		<nav class="year-nav" aria-label="Archive navigation">
			<h2>Years</h2>
			{{range .YearGroups}}
				<a href="#year-{{.Year}}" data-year-link="{{.Year}}">
					<span>{{.Year}}</span>
					<span class="count" data-year-count="{{.Year}}">{{len .Entries}}</span>
				</a>
			{{end}}
			{{if .DomainGroups}}
				<div class="divider" role="presentation"></div>
				<h2>Domains</h2>
				<a href="#" class="domain-link active" data-domain-filter="">All links</a>
				{{range .DomainGroups}}
					<a href="#" class="domain-link" data-domain-filter="{{.Domain}}">
						<span>{{.Domain}}</span>
						<span class="count">{{.Count}}</span>
					</a>
				{{end}}
			{{end}}
		</nav>

		<main>
			{{range .YearGroups}}
				<section class="year-section" id="year-{{.Year}}" data-year="{{.Year}}">
					<div class="year-header">
						<h2>{{.Year}}</h2>
						<div class="year-meta"><span data-year-visible="{{.Year}}">{{len .Entries}}</span> posts</div>
					</div>
					<div class="grid" data-year-grid="{{.Year}}">
						{{range .Entries}}
							<article class="post-card" data-title="{{.Title}}" data-desc="{{.Description}}" data-pubdate="{{.PubDateISO}}" data-domains="{{.Domains}}">
								<div class="cover-wrap">
									{{if .CoverImage}}<img class="cover" src="{{.CoverImage}}" alt="" loading="lazy" />{{else}}<img class="cover" src="data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=" alt="" />{{end}}
								</div>
								<div class="post-body">
									<h3 class="post-title"><a href="{{.RelPath}}">{{.Title}}</a></h3>
									<div class="post-meta">
										<span>Published: {{.PubDateDisplay}}</span>
										<span>Downloaded: {{.DownloadDisplay}}</span>
										{{if gt .EstimatedReadMin 0}}<span>Read: ~{{.EstimatedReadMin}} min</span>{{end}}
									</div>
									{{if .Description}}<div class="post-desc">{{.Description}}</div>{{end}}
									<div class="links">
										<a class="primary" href="{{.RelPath}}">Open local</a>
										{{if .CanonicalURL}}<a href="{{.CanonicalURL}}" target="_blank" rel="noreferrer">Open original</a>{{end}}
										{{if gt .NotionCount 0}}<a class="notion-badge" href="notion-links.html" title="View Notion links list">Notion {{.NotionCount}}</a>{{end}}
									</div>
								</div>
							</article>
						{{end}}
					</div>
				</section>
			{{end}}
			<div class="footer-note">Tip: use search to quickly find posts; sort by title when looking for a specific entry.</div>
		</main>
	</div>

	<script>
		(function () {
			const root = document.documentElement;
			const search = document.getElementById('search');
			const filterFrom = document.getElementById('filterFrom');
			const filterTo = document.getElementById('filterTo');
			const sort = document.getElementById('sort');
			const status = document.getElementById('status');
			const resultCount = document.getElementById('resultCount');
			const toggleCovers = document.getElementById('toggleCovers');
			const toggleTheme = document.getElementById('toggleTheme');
			const domainLinks = Array.from(document.querySelectorAll('[data-domain-filter]'));

			const yearSections = Array.from(document.querySelectorAll('.year-section'));
			const allCards = Array.from(document.querySelectorAll('.post-card'));

			function setTheme(theme) {
				root.setAttribute('data-theme', theme);
				localStorage.setItem('sbstck.theme', theme);
			}

			function toggleThemeMode() {
				const current = root.getAttribute('data-theme');
				if (current === 'dark') return setTheme('light');
				return setTheme('dark');
			}

			function setCoversEnabled(enabled) {
				if (enabled) document.body.classList.remove('no-covers');
				else document.body.classList.add('no-covers');
				toggleCovers.checked = enabled;
				localStorage.setItem('sbstck.covers', enabled ? '1' : '0');
			}

			function normalize(s) {
				return (s || '').toString().toLowerCase();
			}

			function matches(card, query) {
				if (!query) return true;
				const t = normalize(card.getAttribute('data-title'));
				const d = normalize(card.getAttribute('data-desc'));
				return t.includes(query) || d.includes(query);
			}

			let activeDomain = '';
			function matchesDomain(card) {
				if (!activeDomain) return true;
				const domains = (card.getAttribute('data-domains') || '').split(',').map(s => s.trim()).filter(Boolean);
				return domains.includes(activeDomain);
			}

			function parseDay(value) {
				if (!value) return null;
				const t = Date.parse(value + 'T00:00:00');
				return Number.isFinite(t) ? t : null;
			}

			function inDateRange(card) {
				const from = parseDay(filterFrom.value);
				const to = parseDay(filterTo.value);
				if (!from && !to) return true;
				const t = parseDate(card);
				if (!t) return false;
				if (from && t < from) return false;
				if (to && t > (to + 86399999)) return false;
				return true;
			}

			function updateCounts(visibleCount) {
				status.textContent = 'Showing ' + visibleCount + ' of ' + allCards.length;
				resultCount.textContent = visibleCount;
				yearSections.forEach(section => {
					const year = section.getAttribute('data-year');
					const cards = Array.from(section.querySelectorAll('.post-card'));
					const visible = cards.filter(c => !c.classList.contains('hidden')).length;
					const visEl = section.querySelector('[data-year-visible="' + year + '"]');
					if (visEl) visEl.textContent = visible;
					const navCount = document.querySelector('[data-year-count="' + year + '"]');
					if (navCount) navCount.textContent = visible;
					section.style.display = visible === 0 ? 'none' : '';
					const navLink = document.querySelector('[data-year-link="' + year + '"]');
					if (navLink) navLink.style.display = visible === 0 ? 'none' : '';
				});
			}

			let filterTimer = null;
			function applyFilter() {
				const q = normalize(search.value).trim();
				let visible = 0;
				allCards.forEach(card => {
					const ok = matches(card, q) && inDateRange(card) && matchesDomain(card);
					card.classList.toggle('hidden', !ok);
					if (ok) visible++;
				});
				updateCounts(visible);
			}

			function parseDate(card) {
				const iso = card.getAttribute('data-pubdate');
				const t = Date.parse(iso);
				return Number.isFinite(t) ? t : 0;
			}

			function sortSectionCards(section, mode) {
				const grid = section.querySelector('.grid');
				if (!grid) return;
				const cards = Array.from(grid.querySelectorAll('.post-card'));

				if (mode === 'title') {
					cards.sort((a, b) => normalize(a.getAttribute('data-title')).localeCompare(normalize(b.getAttribute('data-title'))));
				} else if (mode === 'oldest') {
					cards.sort((a, b) => parseDate(a) - parseDate(b));
				} else {
					cards.sort((a, b) => parseDate(b) - parseDate(a));
				}

				cards.forEach(c => grid.appendChild(c));
			}

			function sortYears(mode) {
				const container = document.querySelector('main');
				if (!container) return;

				const sections = Array.from(container.querySelectorAll('.year-section'));
				sections.sort((a, b) => {
					const ya = a.getAttribute('data-year');
					const yb = b.getAttribute('data-year');
					if (ya === 'Unknown') return 1;
					if (yb === 'Unknown') return -1;
					if (mode === 'oldest') return ya.localeCompare(yb);
					return yb.localeCompare(ya);
				});

				sections.forEach(s => container.insertBefore(s, container.querySelector('.footer-note')));
			}

			function applySort() {
				const mode = sort.value;
				sortYears(mode);
				yearSections.forEach(section => sortSectionCards(section, mode));
				applyFilter();
			}

			search.addEventListener('input', function () {
				if (filterTimer) clearTimeout(filterTimer);
				filterTimer = setTimeout(applyFilter, 60);
			});
			filterFrom.addEventListener('change', applyFilter);
			filterTo.addEventListener('change', applyFilter);
			sort.addEventListener('change', applySort);
			toggleCovers.addEventListener('change', () => setCoversEnabled(toggleCovers.checked));
			toggleTheme.addEventListener('click', toggleThemeMode);
			domainLinks.forEach(link => {
				link.addEventListener('click', function (event) {
					event.preventDefault();
					const domain = link.getAttribute('data-domain-filter') || '';
					activeDomain = domain;
					domainLinks.forEach(item => {
						const itemDomain = item.getAttribute('data-domain-filter') || '';
						item.classList.toggle('active', itemDomain === activeDomain);
					});
					applyFilter();
				});
			});

			// Restore preferences
			const savedTheme = localStorage.getItem('sbstck.theme');
			if (savedTheme === 'dark' || savedTheme === 'light') setTheme(savedTheme);
			const savedCovers = localStorage.getItem('sbstck.covers');
			if (savedCovers === '0') setCoversEnabled(false);

			applySort();
		})();
	</script>
</body>
</html>`

	t, err := template.New("archive").Parse(tpl)
	if err != nil {
		return err
	}

	var out bytes.Buffer
	if err := t.Execute(&out, page); err != nil {
		return err
	}

	return os.WriteFile(archivePath, out.Bytes(), 0644)
}

// GenerateMarkdown creates a Markdown archive page
func (a *Archive) GenerateMarkdown(outputDir string) error {
	archivePath := filepath.Join(outputDir, "index.md")
	content := a.buildMarkdownArchiveContent(outputDir)
	return os.WriteFile(archivePath, []byte(content), 0644)
}

// GenerateObsidianMarkdown creates an Obsidian-optimized Markdown archive page.
func (a *Archive) GenerateObsidianMarkdown(outputDir string) error {
	archivePath := filepath.Join(outputDir, "index.obsidian.md")
	content := a.buildMarkdownArchiveContent(outputDir)
	content = transformObsidianMarkdown(content)
	return os.WriteFile(archivePath, []byte(content), 0644)
}

func (a *Archive) buildMarkdownArchiveContent(outputDir string) string {
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

	return content
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

func readNotionLinksSidecar(outputPath string) ([]string, bool) {
	path := metadataSidecarPath(outputPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var meta notionLinksSidecar
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, false
	}
	if meta.NotionLinks == nil {
		return nil, false
	}
	return meta.NotionLinks, true
}

func metadataSidecarPath(outputPath string) string {
	ext := filepath.Ext(outputPath)
	if ext == "" {
		return outputPath + ".json"
	}
	return strings.TrimSuffix(outputPath, ext) + ".json"
}
