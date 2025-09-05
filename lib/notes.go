package lib

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown"
)

// NotesClient handles downloading Substack Notes via API
type NotesClient struct {
	fetcher *Fetcher
}

// NewNotesClient creates a new notes client
func NewNotesClient(fetcher *Fetcher) *NotesClient {
	return &NotesClient{
		fetcher: fetcher,
	}
}

// Note represents a Substack Note
type Note struct {
	ID             string                 `json:"id"`
	Body           string                 `json:"body"`
	BodyJSON       interface{}            `json:"body_json,omitempty"`
	Title          string                 `json:"title"`
	Context        string                 `json:"context"`
	CreatedAt      string                 `json:"created_at"`
	AuthorName     string                 `json:"author_name"`
	AuthorHandle   string                 `json:"author_handle"`
	URL            string                 `json:"url"`
	Type           string                 `json:"type"`
	Publication    map[string]interface{} `json:"publication,omitempty"`
	ReactionCount  int                    `json:"reaction_count"`
	Restacks       int                    `json:"restacks"`
}

// NotesResponse represents the API response structure
type NotesResponse struct {
	Items      []ActivityItem `json:"items"`
	NextCursor string         `json:"nextCursor"`
}

// ActivityItem represents an activity item from the user's feed
type ActivityItem struct {
	Type    string                 `json:"type"`
	Comment Comment                `json:"comment,omitempty"`
	Post    map[string]interface{} `json:"post,omitempty"`
	Context Context                `json:"context,omitempty"`
	Publication map[string]interface{} `json:"publication,omitempty"`
}

// Comment represents a comment/note in the activity feed
type Comment struct {
	ID            int                    `json:"id"`
	Body          string                 `json:"body"`
	BodyJSON      interface{}            `json:"body_json,omitempty"`
	Date          string                 `json:"date"`
	Name          string                 `json:"name"`
	Handle        string                 `json:"handle"`
	UserID        int                    `json:"user_id"`
	ReactionCount int                    `json:"reaction_count"`
	Restacks      int                    `json:"restacks"`
}

// Context represents the context of an activity item
type Context struct {
	Type string `json:"type"`
}

// NotesOptions contains options for downloading notes
type NotesOptions struct {
	UserID     string
	Username   string
	OutputDir  string
	Format     string
	MaxPages   int
	NotesOnly  bool
	Verbose    bool
}

// FetchAllUserActivity fetches all activity items for a user across multiple pages
func (nc *NotesClient) FetchAllUserActivity(userID string, maxPages int, verbose bool) ([]ActivityItem, error) {
	baseURL := fmt.Sprintf("https://substack.com/api/v1/reader/feed/profile/%s", userID)
	headers := map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Accept":     "application/json",
	}

	var allItems []ActivityItem
	cursor := ""
	page := 1

	for page <= maxPages {
		reqURL := baseURL
		if cursor != "" {
			reqURL += "?cursor=" + url.QueryEscape(cursor)
		}

		if verbose {
			fmt.Printf("Fetching page %d: %s\n", page, reqURL)
		}

		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := nc.fetcher.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching page %d: %w", page, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
		}

		var notesResp NotesResponse
		if err := json.NewDecoder(resp.Body).Decode(&notesResp); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		if len(notesResp.Items) == 0 {
			if verbose {
				fmt.Printf("  No items found on page %d\n", page)
			}
			break
		}

		allItems = append(allItems, notesResp.Items...)
		if verbose {
			fmt.Printf("  Found %d items on page %d (total: %d)\n", len(notesResp.Items), page, len(allItems))
		}

		cursor = notesResp.NextCursor
		if cursor == "" {
			if verbose {
				fmt.Printf("  No more pages after page %d\n", page)
			}
			break
		}

		page++
	}

	return allItems, nil
}

// IsLikelyRegularComment detects if this is a regular comment vs a note using context.type
func (nc *NotesClient) IsLikelyRegularComment(comment Comment, item ActivityItem) bool {
	// The definitive way: check context.type
	if item.Context.Type == "note" {
		return false // This is a note, not a regular comment
	}
	return true // This is a regular comment
}

// ConvertCommentToNote converts a comment object to our note format
func (nc *NotesClient) ConvertCommentToNote(comment Comment, item ActivityItem) *Note {
	body := comment.Body
	if body == "" || len(strings.TrimSpace(body)) < 10 {
		return nil
	}

	// Extract post context if available
	postContext := ""
	if item.Post != nil {
		if title, ok := item.Post["title"].(string); ok && title != "" {
			postContext = fmt.Sprintf("Context: %s", title)
		}
	}

	return &Note{
		ID:            fmt.Sprintf("%d", comment.ID),
		Body:          body,
		BodyJSON:      comment.BodyJSON,
		Title:         "", // Comments don't have titles
		Context:       postContext,
		CreatedAt:     comment.Date,
		AuthorName:    comment.Name,
		AuthorHandle:  comment.Handle,
		URL:           fmt.Sprintf("https://substack.com/profile/%d/comment/%d", comment.UserID, comment.ID),
		Type:          "comment_note",
		Publication:   item.Publication,
		ReactionCount: comment.ReactionCount,
		Restacks:      comment.Restacks,
	}
}

// SaveNote saves a note to file in the specified format
func (nc *NotesClient) SaveNote(note *Note, outputDir, format string) error {
	// Create filename
	var createdAt time.Time
	if note.CreatedAt != "" {
		// Parse the date string
		dateStr := strings.ReplaceAll(strings.ReplaceAll(note.CreatedAt, "T", " "), "Z", "")
		if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
			createdAt = parsed
		} else {
			createdAt = time.Now()
		}
	} else {
		createdAt = time.Now()
	}

	timestamp := createdAt.Format("20060102_150405")
	
	// Clean ID for filename
	re := regexp.MustCompile(`[^\w\-_]`)
	cleanID := re.ReplaceAllString(note.ID, "")
	if len(cleanID) > 20 {
		cleanID = cleanID[:20]
	}
	
	filename := fmt.Sprintf("%s_%s.%s", timestamp, cleanID, format)
	filepath := filepath.Join(outputDir, filename)

	var content string
	switch format {
	case "html":
		content = nc.formatNoteHTML(note)
	case "md":
		h := html2text.NewConverter()
		h.Opt.PrettyTables = true
		mdContent := h.Convert(note.Body)
		content = nc.formatNoteMarkdown(note, mdContent)
	case "txt":
		h := html2text.NewConverter()
		h.Opt.PrettyTables = true
		h.Opt.LinkStyle = "none"
		textContent := h.Convert(note.Body)
		content = nc.formatNoteText(note, textContent)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	return os.WriteFile(filepath, []byte(content), 0644)
}

// formatNoteHTML formats a note as HTML
func (nc *NotesClient) formatNoteHTML(note *Note) string {
	contextHTML := ""
	if note.Context != "" {
		contextHTML = fmt.Sprintf("<div class='context'><strong>Context:</strong> %s</div>", note.Context)
	}
	
	pubName := ""
	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok {
			pubName = name
		}
	}
	
	pubHTML := ""
	if pubName != "" {
		pubHTML = fmt.Sprintf("<div class='publication'><strong>Publication:</strong> %s</div>", pubName)
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
        <div class="url"><a href="%s">Original Comment</a></div>
    </div>
</body>
</html>`, note.AuthorName, note.AuthorName, note.AuthorHandle, note.CreatedAt, contextHTML, pubHTML, note.Body, note.ReactionCount, note.Restacks, note.URL)
}

// formatNoteMarkdown formats a note as Markdown
func (nc *NotesClient) formatNoteMarkdown(note *Note, mdContent string) string {
	contextMD := ""
	if note.Context != "" {
		contextMD = fmt.Sprintf("**Context:** %s\n", note.Context)
	}
	
	pubName := ""
	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok {
			pubName = name
		}
	}
	
	pubMD := ""
	if pubName != "" {
		pubMD = fmt.Sprintf("**Publication:** %s\n", pubName)
	}

	return fmt.Sprintf(`# Note by %s (@%s)

**Date:** %s  
%s%s**URL:** %s
**Stats:** %d reactions, %d restacks

%s
`, note.AuthorName, note.AuthorHandle, note.CreatedAt, contextMD, pubMD, note.URL, note.ReactionCount, note.Restacks, mdContent)
}

// formatNoteText formats a note as plain text
func (nc *NotesClient) formatNoteText(note *Note, textContent string) string {
	contextTxt := ""
	if note.Context != "" {
		contextTxt = fmt.Sprintf("Context: %s\n", note.Context)
	}
	
	pubName := ""
	if note.Publication != nil {
		if name, ok := note.Publication["name"].(string); ok {
			pubName = name
		}
	}
	
	pubTxt := ""
	if pubName != "" {
		pubTxt = fmt.Sprintf("Publication: %s\n", pubName)
	}

	return fmt.Sprintf(`Note by %s (@%s)
Date: %s
%s%sURL: %s
Stats: %d reactions, %d restacks

%s
`, note.AuthorName, note.AuthorHandle, note.CreatedAt, contextTxt, pubTxt, note.URL, note.ReactionCount, note.Restacks, textContent)
}