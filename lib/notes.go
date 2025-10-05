package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/k3a/html2text"
)

const notesBaseURL = "https://substack.com"

// NotesClient fetches and persists Substack notes for a user.
type NotesClient struct {
	fetcher *Fetcher
	baseURL string
}

// NewNotesClient builds a NotesClient backed by the provided fetcher.
func NewNotesClient(fetcher *Fetcher) *NotesClient {
	return &NotesClient{
		fetcher: fetcher,
		baseURL: notesBaseURL,
	}
}

// SetBaseURL overrides the Substack base URL (used in tests).
func (nc *NotesClient) SetBaseURL(base string) {
	if base != "" {
		nc.baseURL = strings.TrimRight(base, "/")
	}
}

// Note represents a Substack note extracted from the reader activity feed.
type Note struct {
	ID            string                 `json:"id"`
	Body          string                 `json:"body"`
	BodyJSON      interface{}            `json:"body_json,omitempty"`
	Context       string                 `json:"context,omitempty"`
	CreatedAt     string                 `json:"created_at"`
	AuthorName    string                 `json:"author_name"`
	AuthorHandle  string                 `json:"author_handle"`
	URL           string                 `json:"url"`
	Type          string                 `json:"type"`
	Publication   map[string]interface{} `json:"publication,omitempty"`
	ReactionCount int                    `json:"reaction_count"`
	Restacks      int                    `json:"restacks"`
}

// NotesResponse captures a paginated feed response.
type NotesResponse struct {
	Items      []ActivityItem `json:"items"`
	NextCursor string         `json:"nextCursor"`
}

// ActivityItem represents an entry in the reader feed.
type ActivityItem struct {
	Type        string                 `json:"type"`
	Comment     Comment                `json:"comment,omitempty"`
	Post        map[string]interface{} `json:"post,omitempty"`
	Context     Context                `json:"context,omitempty"`
	Publication map[string]interface{} `json:"publication,omitempty"`
}

// Comment is the raw comment structure returned by the feed.
type Comment struct {
	ID            int         `json:"id"`
	Body          string      `json:"body"`
	BodyJSON      interface{} `json:"body_json,omitempty"`
	Date          string      `json:"date"`
	Name          string      `json:"name"`
	Handle        string      `json:"handle"`
	UserID        int         `json:"user_id"`
	ReactionCount int         `json:"reaction_count"`
	Restacks      int         `json:"restacks"`
}

// Context includes metadata describing the activity item.
type Context struct {
	Type string `json:"type"`
}

// FetchAllUserActivity loads paginated activity for a given user ID.
func (nc *NotesClient) FetchAllUserActivity(ctx context.Context, userID string, maxPages int, verbose bool) ([]ActivityItem, error) {
	if maxPages <= 0 {
		maxPages = 1
	}

	base := fmt.Sprintf("%s/api/v1/reader/feed/profile/%s", nc.baseURL, userID)

	var allItems []ActivityItem
	cursor := ""

	for page := 1; page <= maxPages; page++ {
		requestURL := base
		if cursor != "" {
			requestURL = fmt.Sprintf("%s?cursor=%s", base, url.QueryEscape(cursor))
		}

		if verbose {
			fmt.Printf("Fetching notes page %d: %s\n", page, requestURL)
		}

		body, err := nc.fetcher.FetchURL(ctx, requestURL)
		if err != nil {
			return nil, fmt.Errorf("fetching notes page %d: %w", page, err)
		}

		data, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading notes page %d: %w", page, err)
		}

		var resp NotesResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("decoding notes page %d: %w", page, err)
		}

		if len(resp.Items) == 0 {
			if verbose {
				fmt.Printf("  no items on page %d\n", page)
			}
			break
		}

		allItems = append(allItems, resp.Items...)
		if verbose {
			fmt.Printf("  gathered %d items (total %d)\n", len(resp.Items), len(allItems))
		}

		cursor = resp.NextCursor
		if cursor == "" {
			if verbose {
				fmt.Printf("  cursor empty after page %d\n", page)
			}
			break
		}
	}

	return allItems, nil
}

// IsLikelyRegularComment heuristically identifies regular post comments.
func (nc *NotesClient) IsLikelyRegularComment(comment Comment, item ActivityItem) bool {
	if strings.EqualFold(item.Context.Type, "note") {
		return false
	}
	return true
}

// ConvertCommentToNote maps a feed comment to a Note structure.
func (nc *NotesClient) ConvertCommentToNote(comment Comment, item ActivityItem) *Note {
	body := strings.TrimSpace(comment.Body)
	if body == "" {
		return nil
	}

	var contextText string
	if item.Post != nil {
		if title, ok := item.Post["title"].(string); ok && title != "" {
			contextText = title
		}
	}

	handle := strings.TrimSpace(comment.Handle)
	noteURL := fmt.Sprintf("%s/note/c-%d", notesBaseURL, comment.ID)
	if handle != "" {
		noteURL = fmt.Sprintf("%s/@%s/note/c-%d", notesBaseURL, handle, comment.ID)
	}

	return &Note{
		ID:            fmt.Sprintf("%d", comment.ID),
		Body:          comment.Body,
		BodyJSON:      comment.BodyJSON,
		Context:       contextText,
		CreatedAt:     comment.Date,
		AuthorName:    comment.Name,
		AuthorHandle:  handle,
		URL:           noteURL,
		Type:          "comment_note",
		Publication:   item.Publication,
		ReactionCount: comment.ReactionCount,
		Restacks:      comment.Restacks,
	}
}

// SaveNote persists a note using the requested format.
func (nc *NotesClient) SaveNote(note *Note, outputDir, format string) error {
	timestamp := sanitiseTimestamp(note.CreatedAt)
	filename := fmt.Sprintf("%s_%s.%s", timestamp, sanitiseID(note.ID), format)
	path := filepath.Join(outputDir, filename)

	var content string
	switch format {
	case "html":
		content = nc.formatNoteHTML(note)
	case "md":
		markdown, err := mdConverter.ConvertString(note.Body)
		if err != nil {
			return fmt.Errorf("converting note %s to markdown: %w", note.ID, err)
		}
		content = nc.formatNoteMarkdown(note, markdown)
	case "txt":
		text := html2text.HTML2Text(note.Body)
		content = nc.formatNoteText(note, text)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

func (nc *NotesClient) formatNoteHTML(note *Note) string {
	contextHTML := ""
	if note.Context != "" {
		contextHTML = fmt.Sprintf("<div class=\"context\"><strong>Context:</strong> %s</div>", note.Context)
	}

	publicationHTML := ""
	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok && name != "" {
			publicationHTML = fmt.Sprintf("<div class=\"publication\"><strong>Publication:</strong> %s</div>", name)
		}
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Note by %s</title>
</head>
<body>
    <div class="note">
        <div class="author">%s (@%s)</div>
        <div class="timestamp">%s</div>
        %s
        %s
        <div class="content">%s</div>
        <div class="stats">Reactions: %d | Restacks: %d</div>
        <div class="url"><a href="%s">Original Note</a></div>
    </div>
</body>
</html>`, note.AuthorName, note.AuthorName, note.AuthorHandle, note.CreatedAt, contextHTML, publicationHTML, note.Body, note.ReactionCount, note.Restacks, note.URL)
}

func (nc *NotesClient) formatNoteMarkdown(note *Note, body string) string {
	parts := []string{
		fmt.Sprintf("# Note by %s (@%s)", note.AuthorName, note.AuthorHandle),
		"",
		fmt.Sprintf("**Date:** %s  ", note.CreatedAt),
	}

	if note.Context != "" {
		parts = append(parts, fmt.Sprintf("**Context:** %s", note.Context))
	}

	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok && name != "" {
			parts = append(parts, fmt.Sprintf("**Publication:** %s", name))
		}
	}

	parts = append(parts,
		fmt.Sprintf("**URL:** %s", note.URL),
		fmt.Sprintf("**Stats:** %d reactions, %d restacks", note.ReactionCount, note.Restacks),
		"",
		body,
	)

	return strings.Join(parts, "\n") + "\n"
}

func (nc *NotesClient) formatNoteText(note *Note, body string) string {
	lines := []string{
		fmt.Sprintf("Note by %s (@%s)", note.AuthorName, note.AuthorHandle),
		fmt.Sprintf("Date: %s", note.CreatedAt),
	}

	if note.Context != "" {
		lines = append(lines, fmt.Sprintf("Context: %s", note.Context))
	}

	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok && name != "" {
			lines = append(lines, fmt.Sprintf("Publication: %s", name))
		}
	}

	lines = append(lines,
		fmt.Sprintf("URL: %s", note.URL),
		fmt.Sprintf("Stats: %d reactions, %d restacks", note.ReactionCount, note.Restacks),
		"",
		body,
		"",
	)

	return strings.Join(lines, "\n")
}

var (
	idSanitiser = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	timeLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"}
)

func sanitiseID(id string) string {
	cleaned := idSanitiser.ReplaceAllString(id, "")
	if cleaned == "" {
		return "note"
	}
	if len(cleaned) > 32 {
		cleaned = cleaned[:32]
	}
	return cleaned
}

func sanitiseTimestamp(raw string) string {
	for _, layout := range timeLayouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.Format("20060102_150405")
		}
	}
	return time.Now().Format("20060102_150405")
}
