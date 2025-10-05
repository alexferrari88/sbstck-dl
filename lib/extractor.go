package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// Comment represents a Substack comment with metadata.
type Comment struct {
	Id           int         `json:"id"`
	PostId       int         `json:"post_id"`
	UserId       int         `json:"user_id"`
	Body         string      `json:"body"`
	BodyHTML     string      `json:"body_html"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	EditedAt     *time.Time  `json:"edited_at,omitempty"`
	LikesCount   int         `json:"likes_count"`
	RepliesCount int         `json:"replies_count"`
	ParentId     *int        `json:"parent_id,omitempty"`
	User         CommentUser `json:"user"`
	Replies      []Comment   `json:"replies,omitempty"`
}

// CommentUser represents the user who wrote a comment.
type CommentUser struct {
	Id         int    `json:"id"`
	Name       string `json:"name"`
	Handle     string `json:"handle"`
	PhotoURL   string `json:"photo_url"`
	IsSubstack bool   `json:"is_substack"`
	Verified   bool   `json:"verified"`
}

// Post represents a structured Substack post with various fields.
type Post struct {
	Id               int       `json:"id"`
	PublicationId    int       `json:"publication_id"`
	Type             string    `json:"type"`
	Slug             string    `json:"slug"`
	PostDate         string    `json:"post_date"`
	CanonicalUrl     string    `json:"canonical_url"`
	PreviousPostSlug string    `json:"previous_post_slug"`
	NextPostSlug     string    `json:"next_post_slug"`
	CoverImage       string    `json:"cover_image"`
	Description      string    `json:"description"`
	Subtitle         string    `json:"subtitle,omitempty"`
	WordCount        int       `json:"wordcount"`
	Title            string    `json:"title"`
	BodyHTML         string    `json:"body_html"`
	Comments         []Comment `json:"comments,omitempty"`
	CommentsCount    int       `json:"comments_count"`
}

// Static converter instance to avoid recreating it for each conversion
var mdConverter = md.NewConverter("", true, nil)

type commentsAPIResponse struct {
	Comments []commentsAPIItem `json:"comments"`
}

type commentsAPIItem struct {
	ID            int                 `json:"id"`
	Body          string              `json:"body"`
	PostID        int                 `json:"post_id"`
	UserID        int                 `json:"user_id"`
	AncestorPath  string              `json:"ancestor_path"`
	Type          string              `json:"type"`
	Date          string              `json:"date"`
	EditedAt      *string             `json:"edited_at"`
	ReactionCount int                 `json:"reaction_count"`
	Children      []commentsAPIItem   `json:"children"`
	Name          string              `json:"name"`
	PhotoURL      string              `json:"photo_url"`
	Handle        string              `json:"handle"`
	Metadata      commentsAPIMetadata `json:"metadata"`
}

type commentsAPIMetadata struct {
	IsAuthor bool `json:"is_author"`
}

type readerCommentResponse struct {
	CommentBranches []readerCommentBranch `json:"commentBranches"`
	MoreBranches    int                   `json:"moreBranches"`
	NextCursor      string                `json:"nextCursor"`
	RootComment     readerComment         `json:"rootComment"`
}

type readerCommentBranch struct {
	Comment            readerComment       `json:"comment"`
	DescendantComments []readerCommentNode `json:"descendantComments"`
}

type readerCommentNode struct {
	Type               string              `json:"type"`
	Comment            readerComment       `json:"comment"`
	DescendantComments []readerCommentNode `json:"descendantComments"`
}

type readerComment struct {
	ID                     int                       `json:"id"`
	Body                   string                    `json:"body"`
	BodyJSON               map[string]interface{}    `json:"body_json"`
	UserID                 int                       `json:"user_id"`
	Date                   string                    `json:"date"`
	EditedAt               *string                   `json:"edited_at"`
	AncestorPath           string                    `json:"ancestor_path"`
	ReactionCount          int                       `json:"reaction_count"`
	ChildrenCount          int                       `json:"children_count"`
	Name                   string                    `json:"name"`
	Handle                 string                    `json:"handle"`
	PhotoURL               string                    `json:"photo_url"`
	Attachments            []readerCommentAttachment `json:"attachments"`
	UserPrimaryPublication *readerPrimaryPublication `json:"user_primary_publication"`
}

type readerPrimaryPublication struct {
	Subdomain string `json:"subdomain"`
	Name      string `json:"name"`
}

type readerCommentAttachment struct {
	Type         string              `json:"type"`
	LinkMetadata *readerLinkMetadata `json:"linkMetadata"`
	Comment      *readerComment      `json:"comment"`
	MediaUpload  *readerMediaUpload  `json:"mediaUpload"`
}

type readerLinkMetadata struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type readerMediaUpload struct {
	MuxPlaybackID string `json:"mux_playback_id"`
}

type readerCollectedComment struct {
	Comment readerComment
}

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

// ToMDWithComments converts the Post's HTML body and comments to Markdown format.
func (p *Post) ToMDWithComments(withTitle bool) (string, error) {
	content, err := p.ToMD(withTitle)
	if err != nil {
		return "", err
	}

	if len(p.Comments) > 0 {
		content += "\n\n---\n\n## Comments\n\n"
		for i, comment := range p.Comments {
			content += fmt.Sprintf("### %s", comment.User.Name)
			if comment.User.Handle != "" {
				content += fmt.Sprintf(" (@%s)", comment.User.Handle)
			}
			if !comment.CreatedAt.IsZero() {
				content += fmt.Sprintf(" - %s", comment.CreatedAt.Format("Jan 2, 2006"))
			}
			if comment.LikesCount > 0 {
				content += fmt.Sprintf(" (%d likes)", comment.LikesCount)
			}
			content += "\n\n"

			commentBody := commentMarkdownBody(comment)
			if commentBody != "" {
				content += commentBody + "\n\n"
			}
			content += formatRepliesMarkdown(comment.Replies, 1)

			if i < len(p.Comments)-1 {
				content += "---\n\n"
			}
		}
	}

	return content, nil
}

// ToText converts the Post's HTML body to plain text format.
func (p *Post) ToText(withTitle bool) string {
	if withTitle {
		return p.Title + "\n\n" + html2text.HTML2Text(p.BodyHTML)
	}
	return html2text.HTML2Text(p.BodyHTML)
}

// ToTextWithComments converts the Post's HTML body and comments to plain text format.
func (p *Post) ToTextWithComments(withTitle bool) string {
	content := p.ToText(withTitle)

	if len(p.Comments) > 0 {
		content += "\n\n---\n\nCOMMENTS\n\n"
		for i, comment := range p.Comments {
			content += fmt.Sprintf("%s", comment.User.Name)
			if comment.User.Handle != "" {
				content += fmt.Sprintf(" (@%s)", comment.User.Handle)
			}
			if !comment.CreatedAt.IsZero() {
				content += fmt.Sprintf(" - %s", comment.CreatedAt.Format("Jan 2, 2006"))
			}
			if comment.LikesCount > 0 {
				content += fmt.Sprintf(" (%d likes)", comment.LikesCount)
			}
			content += "\n\n"

			commentBody := commentTextBody(comment)
			if commentBody != "" {
				content += commentBody + "\n\n"
			}
			content += formatRepliesText(comment.Replies, 1)

			if i < len(p.Comments)-1 {
				content += "---\n\n"
			}
		}
	}

	return content
}

// ToHTML returns the Post's HTML body as-is or with an optional title header.
func (p *Post) ToHTML(withTitle bool) string {
	if withTitle {
		return fmt.Sprintf("<h1>%s</h1>\n\n%s", p.Title, p.BodyHTML)
	}
	return p.BodyHTML
}

// ToHTMLWithComments returns the Post's HTML body with comments included.
func (p *Post) ToHTMLWithComments(withTitle bool) string {
	content := p.ToHTML(withTitle)

	if len(p.Comments) > 0 {
		content += "\n\n<hr>\n\n<h2>Comments</h2>\n\n"
		for i, comment := range p.Comments {
			content += fmt.Sprintf("<div class=\"comment\">\n")
			content += fmt.Sprintf("<h3>%s", comment.User.Name)
			if comment.User.Handle != "" {
				content += fmt.Sprintf(" <small>(@%s)</small>", comment.User.Handle)
			}
			if !comment.CreatedAt.IsZero() {
				content += fmt.Sprintf(" <small>- %s</small>", comment.CreatedAt.Format("Jan 2, 2006"))
			}
			if comment.LikesCount > 0 {
				content += fmt.Sprintf(" <small>(%d likes)</small>", comment.LikesCount)
			}
			content += "</h3>\n"

			// Use HTML body if available, otherwise plain text
			commentBody := comment.BodyHTML
			if commentBody == "" {
				commentBody = strings.ReplaceAll(comment.Body, "\n", "<br>")
			}
			content += fmt.Sprintf("<div class=\"comment-body\">%s</div>\n", commentBody)

			// Add replies
			if len(comment.Replies) > 0 {
				content += "<div class=\"replies\">\n"
				content += formatRepliesHTML(comment.Replies)
				content += "</div>\n"
			}

			content += "</div>\n"

			if i < len(p.Comments)-1 {
				content += "<hr>\n"
			}
		}
	}

	return content
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

// contentForFormatWithComments returns the content of a post with comments in the specified format.
func (p *Post) contentForFormatWithComments(format string, withTitle bool) (string, error) {
	switch format {
	case "html":
		return p.ToHTMLWithComments(withTitle), nil
	case "md":
		return p.ToMDWithComments(withTitle)
	case "txt":
		return p.ToTextWithComments(withTitle), nil
	default:
		return "", fmt.Errorf("unknown format: %s", format)
	}
}

// WriteToFile writes the Post's content to a file in the specified format (html, md, or txt).
func (p *Post) WriteToFile(path string, format string, addSourceURL bool) error {
	return p.WriteToFileWithOptions(path, format, addSourceURL, false)
}

// WriteToFileWithOptions writes the Post's content to a file with options for including comments.
func (p *Post) WriteToFileWithOptions(path string, format string, addSourceURL bool, includeComments bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var content string
	var err error

	if includeComments {
		content, err = p.contentForFormatWithComments(format, true)
	} else {
		content, err = p.contentForFormat(format, true)
	}

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
	return p.WriteToFileWithImagesAndComments(ctx, path, format, addSourceURL, downloadImages, imageQuality, imagesDir, downloadFiles, fileExtensions, filesDir, fetcher, false)
}

// WriteToFileWithImagesAndComments writes the Post's content to a file with optional image downloading and comments
func (p *Post) WriteToFileWithImagesAndComments(ctx context.Context, path string, format string, addSourceURL bool,
	downloadImages bool, imageQuality ImageQuality, imagesDir string,
	downloadFiles bool, fileExtensions []string, filesDir string, fetcher *Fetcher, includeComments bool) (*ImageDownloadResult, error) {

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	var content string
	var err error

	if includeComments {
		content, err = p.contentForFormatWithComments(format, true)
	} else {
		content, err = p.contentForFormat(format, true)
	}

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

	htmlBytes, err := io.ReadAll(body)
	if err != nil {
		return Post{}, fmt.Errorf("failed to read HTML: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
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

	if err != nil || strings.Contains(pageUrl, "/note/") || strings.EqualFold(p.Type, "note") {
		notePost, noteErr := extractNoteFromPreloads(doc, pageUrl)
		if noteErr == nil {
			p = notePost
		} else if err != nil {
			return Post{}, fmt.Errorf("failed to parse both post and note data: %w", err)
		}
	}

	if p.CanonicalUrl == "" {
		if canonical, exists := doc.Find("link[rel='canonical']").Attr("href"); exists && canonical != "" {
			p.CanonicalUrl = canonical
		} else {
			p.CanonicalUrl = pageUrl
		}
	}

	if p.Slug == "" {
		if slug, slugErr := extractSlugFromURL(p.CanonicalUrl); slugErr == nil {
			p.Slug = slug
		}
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

	// Attempt to fetch comments via Substack API
	expectedCommentsCount := p.CommentsCount
	isNoteContent := strings.Contains(pageUrl, "/note/") || strings.EqualFold(p.Type, "note")
	comments, err := e.fetchCommentsFromAPI(ctx, pageUrl, p.Id, isNoteContent)

	if err == nil && len(comments) > 0 {
		apiCount := countTotalComments(comments)

		// If API returns fewer comments than reported, attempt HTML fallback for completeness
		if expectedCommentsCount > 0 && apiCount < expectedCommentsCount {
			if htmlComments, htmlErr := extractCommentsFromHTML(htmlBytes); htmlErr == nil && len(htmlComments) > 0 {
				htmlCount := countTotalComments(htmlComments)
				if htmlCount >= apiCount {
					comments = htmlComments
					apiCount = htmlCount
					err = nil
				}
			}
		}

		// Always update the comment count to reflect what we actually retrieved
		p.CommentsCount = apiCount
	} else {
		comments, err = extractCommentsFromHTML(htmlBytes)
	}

	if err == nil && len(comments) > 0 {
		total := countTotalComments(comments)
		p.Comments = comments
		p.CommentsCount = total
	}

	return p, nil
}

// extractNoteFromPreloads extracts note data from the preloads data structure
func extractNoteFromPreloads(doc *goquery.Document, pageURL string) (Post, error) {
	var p Post

	htmlContent, err := doc.Html()
	if err != nil {
		return p, fmt.Errorf("failed to get HTML content: %w", err)
	}

	preloadsStart := strings.Index(htmlContent, "window._preloads")
	if preloadsStart == -1 {
		return p, errors.New("no window._preloads found")
	}

	jsonParseStart := strings.Index(htmlContent[preloadsStart:], "JSON.parse(\"")
	if jsonParseStart == -1 {
		return p, errors.New("no JSON.parse found in window._preloads")
	}
	jsonParseStart += preloadsStart + len("JSON.parse(\"")

	jsonEnd := strings.Index(htmlContent[jsonParseStart:], "\")")
	if jsonEnd == -1 {
		return p, errors.New("could not find end of JSON string")
	}
	jsonEnd += jsonParseStart

	jsonStr := htmlContent[jsonParseStart:jsonEnd]
	unesescapedJSON, err := jsonUnescape(jsonStr)
	if err != nil {
		return p, fmt.Errorf("failed to unescape JSON: %w", err)
	}

	var preloadsData map[string]interface{}
	if err := json.Unmarshal([]byte(unesescapedJSON), &preloadsData); err != nil {
		return p, fmt.Errorf("failed to parse preloads JSON: %w", err)
	}

	feedData, ok := preloadsData["feedData"].(map[string]interface{})
	if !ok {
		return p, errors.New("feedData not found in preloads")
	}

	feedItem, ok := feedData["feedItem"].(map[string]interface{})
	if !ok {
		return p, errors.New("feedItem not found in feedData")
	}

	if entityKey, ok := feedItem["entity_key"].(string); ok && entityKey != "" {
		p.Slug = entityKey
	}

	comment, ok := feedItem["comment"].(map[string]interface{})
	if !ok {
		return p, errors.New("comment not found in feedItem")
	}

	if id, ok := comment["id"].(float64); ok {
		p.Id = int(id)
	}

	if date, ok := comment["date"].(string); ok && date != "" {
		p.PostDate = date
	}

	if childrenCount, ok := comment["children_count"].(float64); ok {
		p.CommentsCount = int(childrenCount)
	}

	if photoURL, ok := comment["photo_url"].(string); ok && photoURL != "" {
		p.CoverImage = photoURL
	}

	bodyText := ""
	if body, ok := comment["body"].(string); ok {
		bodyText = strings.TrimSpace(body)
	}

	if bodyText != "" {
		p.BodyHTML = renderNoteBodyHTML(bodyText)
		p.WordCount = len(strings.Fields(bodyText))
		preview := truncateText(firstLine(bodyText), 140)
		p.Title = preview
		p.Description = preview
		if name, ok := comment["name"].(string); ok && name != "" {
			p.Title = truncateText(fmt.Sprintf("%s: %s", name, preview), 160)
		}
	} else if name, ok := comment["name"].(string); ok && name != "" {
		p.Title = fmt.Sprintf("%s's note", name)
	}

	if p.Title == "" {
		if p.Slug != "" {
			p.Title = fmt.Sprintf("Note %s", p.Slug)
		} else {
			p.Title = "Substack Note"
		}
	}

	if handle, ok := comment["handle"].(string); ok && handle != "" {
		p.Subtitle = "@" + handle
	}

	if attachments, ok := comment["attachments"].([]interface{}); ok {
		var fragments []string
		for _, raw := range attachments {
			att, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			typeStr, _ := att["type"].(string)
			if typeStr != "video" {
				continue
			}
			mediaUpload, ok := att["mediaUpload"].(map[string]interface{})
			if !ok {
				continue
			}
			if playbackID, ok := mediaUpload["mux_playback_id"].(string); ok && playbackID != "" {
				videoURL := fmt.Sprintf("https://stream.mux.com/%s.m3u8", playbackID)
				fragments = append(fragments, fmt.Sprintf("<p><strong>Video attachment:</strong> <a href=\"%s\">Watch</a></p>", html.EscapeString(videoURL)))
			}
		}
		if len(fragments) > 0 {
			if p.BodyHTML != "" {
				p.BodyHTML += "\n"
			}
			p.BodyHTML += strings.Join(fragments, "\n")
		}
	}

	p.Type = "note"
	p.CanonicalUrl = pageURL

	if p.Slug == "" {
		if slug, slugErr := extractSlugFromURL(pageURL); slugErr == nil {
			p.Slug = slug
		}
	}

	return p, nil
}

func extractSlugFromURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", errors.New("empty url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) == 0 {
		return "", errors.New("unable to derive slug from url")
	}
	return segments[len(segments)-1], nil
}

func renderNoteBodyHTML(body string) string {
	paragraphs := strings.Split(body, "\n\n")
	var builder strings.Builder
	for _, paragraph := range paragraphs {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed == "" {
			continue
		}
		escaped := html.EscapeString(trimmed)
		escaped = strings.ReplaceAll(escaped, "\n", "<br />")
		builder.WriteString("<p>")
		builder.WriteString(escaped)
		builder.WriteString("</p>\n")
	}
	return strings.TrimSpace(builder.String())
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if idx := strings.IndexAny(trimmed, "\r\n"); idx >= 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// extractCommentsFromHTML extracts comments from window._preloads data in HTML
func extractCommentsFromHTML(htmlBytes []byte) ([]Comment, error) {
	htmlContent := string(htmlBytes)

	// Find window._preloads data
	preloadsStart := strings.Index(htmlContent, "window._preloads")
	if preloadsStart == -1 {
		return nil, errors.New("no window._preloads found")
	}

	// Find the JSON.parse call
	jsonParseStart := strings.Index(htmlContent[preloadsStart:], "JSON.parse(\"")
	if jsonParseStart == -1 {
		return nil, errors.New("no JSON.parse found in window._preloads")
	}
	jsonParseStart += preloadsStart + len("JSON.parse(\"")

	// Find the end of the JSON string
	jsonEnd := strings.Index(htmlContent[jsonParseStart:], "\")")
	if jsonEnd == -1 {
		return nil, errors.New("could not find end of JSON string")
	}
	jsonEnd += jsonParseStart

	// Extract and unescape the JSON
	jsonStr := htmlContent[jsonParseStart:jsonEnd]
	unesescapedJSON, err := jsonUnescape(jsonStr)
	if err != nil {
		return nil, fmt.Errorf("failed to unescape JSON: %w", err)
	}

	// Parse the JSON to get the preloads data
	var preloadsData map[string]interface{}
	if err := json.Unmarshal([]byte(unesescapedJSON), &preloadsData); err != nil {
		return nil, fmt.Errorf("failed to parse preloads JSON: %w", err)
	}

	// Extract comments from the preloads data
	return extractCommentsFromPreloads(preloadsData)
}

func (e *Extractor) fetchCommentsFromAPI(ctx context.Context, pageURL string, postID int, isNote bool) ([]Comment, error) {
	comments, err := e.fetchPostCommentsFromAPI(ctx, pageURL, postID)
	if err == nil && len(comments) > 0 {
		return comments, nil
	}

	shouldTryNote := isNote
	if fetchErr, ok := err.(*FetchError); ok {
		if fetchErr.StatusCode == http.StatusNotFound || fetchErr.StatusCode == http.StatusBadRequest {
			shouldTryNote = true
		}
	}

	if shouldTryNote || (isNote && (err != nil || len(comments) == 0)) {
		noteComments, noteErr := e.fetchNoteCommentsFromAPI(ctx, pageURL, postID)
		if noteErr == nil {
			return noteComments, nil
		}

		if err != nil {
			return nil, err
		}
		return nil, noteErr
	}

	return comments, err
}

func (e *Extractor) fetchPostCommentsFromAPI(ctx context.Context, pageURL string, postID int) ([]Comment, error) {
	if postID <= 0 {
		return nil, nil
	}

	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	parsedURL.Path = fmt.Sprintf("/api/v1/post/%d/comments", postID)

	baseQuery := parsedURL.Query()
	baseQuery.Set("all_comments", "true")
	baseQuery.Set("sort", "oldest_first")
	const limit = 200
	baseQuery.Set("limit", strconv.Itoa(limit))

	seen := make(map[int]struct{})
	aggregated := make([]commentsAPIItem, 0, limit)

	offset := 0
	for page := 0; page < 50; page++ {
		q := url.Values{}
		for k, vs := range baseQuery {
			q[k] = append([]string(nil), vs...)
		}
		q.Set("offset", strconv.Itoa(offset))
		iterURL := *parsedURL
		iterURL.RawQuery = q.Encode()

		respBody, err := e.fetcher.FetchURL(ctx, iterURL.String())
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}

		data, readErr := io.ReadAll(respBody)
		respBody.Close()
		if readErr != nil {
			if page == 0 {
				return nil, readErr
			}
			break
		}

		var apiResp commentsAPIResponse
		if err := json.Unmarshal(data, &apiResp); err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}

		if len(apiResp.Comments) == 0 {
			break
		}

		newCount := 0
		for _, it := range apiResp.Comments {
			if _, ok := seen[it.ID]; ok {
				continue
			}
			seen[it.ID] = struct{}{}
			aggregated = append(aggregated, it)
			newCount++
		}

		if len(apiResp.Comments) < limit || newCount == 0 {
			break
		}

		offset += len(apiResp.Comments)
	}

	if len(aggregated) == 0 {
		return nil, nil
	}

	comments := buildCommentsFromAPI(aggregated)
	return comments, nil
}

func (e *Extractor) fetchNoteCommentsFromAPI(ctx context.Context, pageURL string, rootCommentID int) ([]Comment, error) {
	if rootCommentID <= 0 {
		return nil, fmt.Errorf("invalid note identifier")
	}

	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	parsedURL.Path = fmt.Sprintf("/api/v1/reader/comment/%d/replies", rootCommentID)
	parsedURL.RawQuery = ""

	seen := make(map[int]struct{})
	collected := make([]readerCollectedComment, 0, 32)

	cursor := ""
	for page := 0; page < 200; page++ {
		iterURL := *parsedURL
		q := iterURL.Query()
		if cursor != "" {
			q.Set("cursor", cursor)
		} else {
			q.Del("cursor")
		}
		iterURL.RawQuery = q.Encode()

		respBody, err := e.fetcher.FetchURL(ctx, iterURL.String())
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}

		data, readErr := io.ReadAll(respBody)
		respBody.Close()
		if readErr != nil {
			if page == 0 {
				return nil, readErr
			}
			break
		}

		var apiResp readerCommentResponse
		if err := json.Unmarshal(data, &apiResp); err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}

		if apiResp.RootComment.ID != 0 {
			rootCommentID = apiResp.RootComment.ID
		}

		added := 0
		for _, branch := range apiResp.CommentBranches {
			added += collectReaderComment(&collected, branch.Comment, rootCommentID, seen)
			for _, descendant := range branch.DescendantComments {
				added += collectReaderNode(&collected, descendant, rootCommentID, seen)
			}
		}

		if apiResp.MoreBranches == 0 || apiResp.NextCursor == "" || added == 0 {
			break
		}

		cursor = apiResp.NextCursor
	}

	if len(collected) == 0 {
		return nil, nil
	}

	comments := buildCommentsFromReader(collected, rootCommentID)
	return comments, nil
}

func collectReaderComment(collected *[]readerCollectedComment, item readerComment, rootID int, seen map[int]struct{}) int {
	if item.ID == 0 || item.ID == rootID {
		return 0
	}
	if _, ok := seen[item.ID]; ok {
		return 0
	}
	seen[item.ID] = struct{}{}
	*collected = append(*collected, readerCollectedComment{Comment: item})
	return 1
}

func collectReaderNode(collected *[]readerCollectedComment, node readerCommentNode, rootID int, seen map[int]struct{}) int {
	if node.Type != "comment" {
		return 0
	}
	added := collectReaderComment(collected, node.Comment, rootID, seen)
	for _, child := range node.DescendantComments {
		added += collectReaderNode(collected, child, rootID, seen)
	}
	return added
}

func buildCommentsFromReader(items []readerCollectedComment, rootID int) []Comment {
	children := make(map[int][]*Comment)

	for _, item := range items {
		comment := readerCommentToComment(item.Comment)
		parent := parentIDFromAncestorPath(item.Comment.AncestorPath)
		parentKey := rootID
		if parent != nil {
			parentKey = *parent
		}
		children[parentKey] = append(children[parentKey], comment)
	}

	var build func(parent int, nodes []*Comment) []Comment
	build = func(parent int, nodes []*Comment) []Comment {
		result := make([]Comment, 0, len(nodes))
		for _, node := range nodes {
			if parent != rootID {
				parentCopy := parent
				node.ParentId = &parentCopy
			} else {
				node.ParentId = nil
			}

			if kidList, ok := children[node.Id]; ok && len(kidList) > 0 {
				node.Replies = build(node.Id, kidList)
				node.RepliesCount = len(node.Replies)
			} else {
				node.Replies = nil
				node.RepliesCount = 0
			}

			result = append(result, *node)
		}
		return result
	}

	return build(rootID, children[rootID])
}

func readerCommentToComment(item readerComment) *Comment {
	body := strings.TrimSpace(item.Body)
	if len(item.Attachments) > 0 {
		body = augmentBodyWithAttachments(body, item.Attachments)
	}

	comment := &Comment{
		Id:         item.ID,
		UserId:     item.UserID,
		Body:       body,
		BodyHTML:   commentBodyToHTML(body),
		LikesCount: item.ReactionCount,
	}

	if item.Date != "" {
		if createdAt, err := time.Parse(time.RFC3339, item.Date); err == nil {
			comment.CreatedAt = createdAt
			comment.UpdatedAt = createdAt
		}
	}

	if item.EditedAt != nil && *item.EditedAt != "" {
		if editTime, err := time.Parse(time.RFC3339, *item.EditedAt); err == nil {
			comment.EditedAt = &editTime
			comment.UpdatedAt = editTime
		}
	}

	handle := item.Handle
	if handle == "" && item.UserPrimaryPublication != nil {
		handle = item.UserPrimaryPublication.Subdomain
	}

	comment.User = CommentUser{
		Id:       item.UserID,
		Name:     item.Name,
		Handle:   handle,
		PhotoURL: item.PhotoURL,
	}

	return comment
}

func augmentBodyWithAttachments(body string, attachments []readerCommentAttachment) string {
	if len(attachments) == 0 {
		return body
	}

	extras := make([]string, 0, len(attachments))
	for _, att := range attachments {
		switch strings.ToLower(att.Type) {
		case "link":
			if att.LinkMetadata != nil && att.LinkMetadata.URL != "" {
				extras = append(extras, att.LinkMetadata.URL)
			}
		case "video":
			if att.MediaUpload != nil && att.MediaUpload.MuxPlaybackID != "" {
				extras = append(extras, fmt.Sprintf("Video: https://stream.mux.com/%s.m3u8", att.MediaUpload.MuxPlaybackID))
			}
		case "comment":
			if att.Comment != nil {
				shared := strings.TrimSpace(att.Comment.Body)
				if shared != "" {
					if att.Comment.Name != "" {
						extras = append(extras, fmt.Sprintf("Shared from %s: %s", att.Comment.Name, shared))
					} else {
						extras = append(extras, shared)
					}
				}
			}
		}
	}

	if len(extras) == 0 {
		return body
	}

	extrasText := strings.Join(extras, "\n\n")
	if strings.TrimSpace(body) == "" {
		return extrasText
	}
	return strings.TrimSpace(body) + "\n\n" + extrasText
}

func buildCommentsFromAPI(items []commentsAPIItem) []Comment {
	comments := make([]Comment, 0, len(items))
	for _, item := range items {
		comment := Comment{
			Id:           item.ID,
			PostId:       item.PostID,
			UserId:       item.UserID,
			Body:         strings.TrimSpace(item.Body),
			BodyHTML:     commentBodyToHTML(strings.TrimSpace(item.Body)),
			LikesCount:   item.ReactionCount,
			RepliesCount: len(item.Children),
		}

		if item.Date != "" {
			if createdAt, err := time.Parse(time.RFC3339, item.Date); err == nil {
				comment.CreatedAt = createdAt
				comment.UpdatedAt = createdAt
			}
		}

		if item.EditedAt != nil && *item.EditedAt != "" {
			if editTime, err := time.Parse(time.RFC3339, *item.EditedAt); err == nil {
				comment.EditedAt = &editTime
				comment.UpdatedAt = editTime
			}
		}

		if parentID := parentIDFromAncestorPath(item.AncestorPath); parentID != nil {
			comment.ParentId = parentID
		}

		comment.User = CommentUser{
			Id:         item.UserID,
			Name:       item.Name,
			Handle:     item.Handle,
			PhotoURL:   item.PhotoURL,
			IsSubstack: item.Metadata.IsAuthor,
		}

		if len(item.Children) > 0 {
			comment.Replies = buildCommentsFromAPI(item.Children)
			comment.RepliesCount = len(comment.Replies)
		}

		comments = append(comments, comment)
	}
	return comments
}

func parentIDFromAncestorPath(path string) *int {
	path = strings.Trim(path, ".")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	last := parts[len(parts)-1]
	id, err := strconv.Atoi(last)
	if err != nil {
		return nil
	}
	return &id
}

func commentBodyToHTML(body string) string {
	if body == "" {
		return ""
	}
	paragraphs := strings.Split(body, "\n\n")
	htmlParts := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed == "" {
			continue
		}
		escaped := html.EscapeString(trimmed)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		htmlParts = append(htmlParts, fmt.Sprintf("<p>%s</p>", escaped))
	}
	return strings.Join(htmlParts, "\n")
}

func countTotalComments(comments []Comment) int {
	total := 0
	for _, comment := range comments {
		total++
		total += countTotalComments(comment.Replies)
	}
	return total
}

func commentMarkdownBody(comment Comment) string {
	body := strings.TrimSpace(comment.Body)
	if comment.BodyHTML != "" {
		if mdBody, err := mdConverter.ConvertString(comment.BodyHTML); err == nil {
			body = strings.TrimSpace(mdBody)
		}
	}
	return body
}

func commentTextBody(comment Comment) string {
	body := strings.TrimSpace(comment.Body)
	if comment.BodyHTML != "" {
		body = strings.TrimSpace(html2text.HTML2Text(comment.BodyHTML))
	}
	return body
}

func formatRepliesMarkdown(replies []Comment, depth int) string {
	if len(replies) == 0 {
		return ""
	}
	var sb strings.Builder
	prefix := strings.Repeat("> ", depth)
	for _, reply := range replies {
		sb.WriteString(prefix)
		sb.WriteString("**")
		sb.WriteString(reply.User.Name)
		sb.WriteString(" replied")
		if !reply.CreatedAt.IsZero() {
			sb.WriteString(fmt.Sprintf(": %s", reply.CreatedAt.Format("Jan 2, 2006")))
		}
		if reply.LikesCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d likes)", reply.LikesCount))
		}
		sb.WriteString("**\n")

		body := commentMarkdownBody(reply)
		if body != "" {
			quoted := prefix + strings.ReplaceAll(body, "\n", "\n"+prefix)
			sb.WriteString(quoted)
			sb.WriteString("\n\n")
		}

		sb.WriteString(formatRepliesMarkdown(reply.Replies, depth+1))
	}
	return sb.String()
}

func formatRepliesText(replies []Comment, depth int) string {
	if len(replies) == 0 {
		return ""
	}
	var sb strings.Builder
	prefix := strings.Repeat("> ", depth)
	for _, reply := range replies {
		sb.WriteString(prefix)
		sb.WriteString(reply.User.Name)
		if !reply.CreatedAt.IsZero() {
			sb.WriteString(fmt.Sprintf(" replied: %s", reply.CreatedAt.Format("Jan 2, 2006")))
		} else {
			sb.WriteString(" replied:")
		}
		if reply.LikesCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d likes)", reply.LikesCount))
		}
		sb.WriteString("\n")

		body := commentTextBody(reply)
		if body != "" {
			lines := strings.Split(body, "\n")
			for _, line := range lines {
				sb.WriteString(prefix)
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		sb.WriteString(formatRepliesText(reply.Replies, depth+1))
	}
	return sb.String()
}

func formatRepliesHTML(replies []Comment) string {
	if len(replies) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, reply := range replies {
		sb.WriteString("<blockquote class=\"reply\">\n")
		sb.WriteString(fmt.Sprintf("<p><strong>%s replied:</strong>", reply.User.Name))
		if !reply.CreatedAt.IsZero() {
			sb.WriteString(fmt.Sprintf(" %s", reply.CreatedAt.Format("Jan 2, 2006")))
		}
		if reply.LikesCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d likes)", reply.LikesCount))
		}
		sb.WriteString("</p>\n")

		// Use HTML body if available, otherwise plain text
		replyBody := strings.TrimSpace(reply.BodyHTML)
		if replyBody != "" {
			sb.WriteString(replyBody)
			sb.WriteString("\n")
		} else {
			fallback := commentBodyToHTML(reply.Body)
			if fallback != "" {
				sb.WriteString(fallback)
				sb.WriteString("\n")
			}
		}

		// Add nested replies
		if len(reply.Replies) > 0 {
			sb.WriteString("<div class=\"nested-replies\">\n")
			sb.WriteString(formatRepliesHTML(reply.Replies))
			sb.WriteString("</div>\n")
		}

		sb.WriteString("</blockquote>\n")
	}
	return sb.String()
}

// jsonUnescape unescapes a JSON string
func jsonUnescape(s string) (string, error) {
	var result string
	err := json.Unmarshal([]byte("\""+s+"\""), &result)
	return result, err
}

// extractCommentsFromPreloads extracts comments from the preloads data structure
func extractCommentsFromPreloads(preloads map[string]interface{}) ([]Comment, error) {
	var commentsData []interface{}

	// First try to get comments from post data (regular posts)
	if postData, ok := preloads["post"].(map[string]interface{}); ok {
		if comments, ok := postData["comments"].([]interface{}); ok {
			commentsData = comments
		}
	}

	// If no post comments found, try to get from feedData (notes)
	if len(commentsData) == 0 {
		if feedData, ok := preloads["feedData"].(map[string]interface{}); ok {
			if feedItem, ok := feedData["feedItem"].(map[string]interface{}); ok {
				if comment, ok := feedItem["comment"].(map[string]interface{}); ok {
					if parentComments, ok := comment["parentComments"].([]interface{}); ok {
						commentsData = parentComments
					}
				}
			}
		}
	}

	if len(commentsData) == 0 {
		return nil, fmt.Errorf("no comments found in preloads data")
	}

	var comments []Comment
	for _, commentData := range commentsData {
		commentMap, ok := commentData.(map[string]interface{})
		if !ok {
			continue
		}

		comment, err := parseComment(commentMap)
		if err != nil {
			continue // Skip invalid comments
		}

		comments = append(comments, comment)
	}

	return comments, nil
}

// parseComment parses a single comment from the comment map
func parseComment(commentMap map[string]interface{}) (Comment, error) {
	var comment Comment

	// Parse basic fields
	if id, ok := commentMap["id"].(float64); ok {
		comment.Id = int(id)
	}
	if postId, ok := commentMap["post_id"].(float64); ok {
		comment.PostId = int(postId)
	}
	if body, ok := commentMap["body"].(string); ok {
		comment.Body = body
	}
	if bodyHTML, ok := commentMap["body_html"].(string); ok {
		comment.BodyHTML = bodyHTML
	}
	if likesCount, ok := commentMap["reaction_count"].(float64); ok {
		comment.LikesCount = int(likesCount)
	}

	// Parse dates
	if dateStr, ok := commentMap["date"].(string); ok {
		if parsedDate, err := time.Parse(time.RFC3339, dateStr); err == nil {
			comment.CreatedAt = parsedDate
			comment.UpdatedAt = parsedDate
		}
	}

	// Parse user information
	if userMap, ok := commentMap["metadata"].(map[string]interface{}); ok {
		if name, ok := userMap["name"].(string); ok {
			comment.User.Name = name
		}
		if handle, ok := userMap["user_slug"].(string); ok {
			comment.User.Handle = handle
		}
		if photoURL, ok := userMap["photo_url"].(string); ok {
			comment.User.PhotoURL = photoURL
		}
	}

	// Parse children (replies)
	if children, ok := commentMap["children"].([]interface{}); ok {
		for _, childData := range children {
			if childMap, ok := childData.(map[string]interface{}); ok {
				if childComment, err := parseComment(childMap); err == nil {
					comment.Replies = append(comment.Replies, childComment)
				}
			}
		}
	}

	return comment, nil
}

type DateFilterFunc func(string) bool

func (e *Extractor) GetAllPostsURLs(ctx context.Context, pubUrl string, f DateFilterFunc, includeNotes bool) ([]string, error) {
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

	// Add notes if requested
	if includeNotes {
		noteURLs, err := e.discoverNoteURLs(ctx, pubUrl, f)
		if err != nil {
			// Silently fail on note discovery to not break existing functionality
			// Log error but don't fail the entire operation
		} else {
			urls = append(urls, noteURLs...)
		}
	}

	return urls, nil
}

// discoverNoteURLs discovers note URLs by scraping the publication's feed
func (e *Extractor) discoverNoteURLs(ctx context.Context, pubUrl string, f DateFilterFunc) ([]string, error) {
	// Parse the base publication URL
	baseURL, err := url.Parse(pubUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse publication URL: %w", err)
	}

	// Build the feed URL - we'll scrape the main page which often shows recent notes
	feedURL := *baseURL
	feedURL.Path = "/" // Use the homepage to discover notes

	// Fetch the feed page
	body, err := e.fetcher.FetchURL(ctx, feedURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed page: %w", err)
	}
	defer body.Close()

	// Read the content
	htmlBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read feed content: %w", err)
	}

	// Extract note URLs from HTML content
	return e.extractNoteURLsFromHTML(string(htmlBytes), baseURL.String(), f)
}

// extractNoteURLsFromHTML extracts note URLs from HTML content
func (e *Extractor) extractNoteURLsFromHTML(htmlContent, baseURL string, f DateFilterFunc) ([]string, error) {
	var noteURLs []string

	// Look for note URLs in the HTML using regex patterns
	// Notes typically have URLs like https://substack.com/@handle/note/c-123456789
	noteLinkRegex := `https://[^"]+/note/c-\d+`
	re, err := regexp.Compile(noteLinkRegex)
	if err != nil {
		return nil, fmt.Errorf("failed to compile note regex: %w", err)
	}

	// Find all note URLs in the HTML
	matches := re.FindAllString(htmlContent, -1)
	seen := make(map[string]bool)

	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			noteURLs = append(noteURLs, match)
		}
	}

	// Apply date filter if provided
	if f != nil {
		var filteredURLs []string
		for _, noteURL := range noteURLs {
			// Extract date from note URL if possible, otherwise include it
			// For now, we'll include all notes since we can't easily get dates without parsing
			filteredURLs = append(filteredURLs, noteURL)
		}
		return filteredURLs, nil
	}

	return noteURLs, nil
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
