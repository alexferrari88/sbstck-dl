package lib

import (
	"strings"
	"testing"
)

func TestBuildCommentsFromAPI(t *testing.T) {
	edited := "2025-09-26T03:34:00Z"
	apiItems := []commentsAPIItem{
		{
			ID:            1,
			Body:          "Top level\n\nParagraph two",
			PostID:        100,
			UserID:        10,
			AncestorPath:  "",
			Date:          "2025-09-26T03:23:00Z",
			ReactionCount: 2,
			Metadata:      commentsAPIMetadata{IsAuthor: false},
			Children: []commentsAPIItem{
				{
					ID:            2,
					Body:          "Reply body",
					PostID:        100,
					UserID:        20,
					AncestorPath:  "1",
					Date:          "2025-09-26T03:30:00Z",
					EditedAt:      &edited,
					ReactionCount: 1,
					Metadata:      commentsAPIMetadata{IsAuthor: true},
				},
			},
		},
	}

	comments := buildCommentsFromAPI(apiItems)
	if len(comments) != 1 {
		t.Fatalf("expected 1 top level comment got %d", len(comments))
	}

	top := comments[0]
	if top.Id != 1 {
		t.Errorf("expected id 1 got %d", top.Id)
	}
	if top.RepliesCount != 1 {
		t.Errorf("expected replies count 1 got %d", top.RepliesCount)
	}
	if top.BodyHTML != "<p>Top level</p>\n<p>Paragraph two</p>" {
		t.Errorf("unexpected body html: %q", top.BodyHTML)
	}
	if top.CreatedAt.IsZero() || top.UpdatedAt.IsZero() {
		t.Errorf("expected timestamps to be set")
	}

	if len(top.Replies) != 1 {
		t.Fatalf("expected a single reply got %d", len(top.Replies))
	}

	reply := top.Replies[0]
	if reply.ParentId == nil || *reply.ParentId != 1 {
		t.Fatalf("expected parent id 1 got %v", reply.ParentId)
	}
	if reply.EditedAt == nil {
		t.Fatalf("expected edited at to be set")
	}
	if reply.User.IsSubstack != true {
		t.Fatalf("expected reply to be marked as author")
	}
}

func TestCommentBodyToHTML(t *testing.T) {
	input := "Line one\nLine two\n\nSecond paragraph"
	want := "<p>Line one<br>Line two</p>\n<p>Second paragraph</p>"
	if got := commentBodyToHTML(input); got != want {
		t.Fatalf("unexpected html: got %q want %q", got, want)
	}

	if commentBodyToHTML("") != "" {
		t.Fatalf("expected empty string for empty input")
	}
}

func TestCountTotalComments(t *testing.T) {
	comments := []Comment{
		{Id: 1},
		{Id: 2, Replies: []Comment{{Id: 3}, {Id: 4}}},
	}
	if got := countTotalComments(comments); got != 4 {
		t.Fatalf("expected 4 comments got %d", got)
	}
}

func TestParentIDFromAncestorPath(t *testing.T) {
	if id := parentIDFromAncestorPath("123.456"); id == nil || *id != 456 {
		t.Fatalf("expected 456 got %v", id)
	}
	if parentIDFromAncestorPath("abc") != nil {
		t.Fatalf("expected nil for invalid path")
	}
}

func TestFormatRepliesMarkdown(t *testing.T) {
	replies := []Comment{
		{
			User: CommentUser{Name: "Author"},
			Body: "First reply",
			Replies: []Comment{
				{User: CommentUser{Name: "Reader"}, Body: "Nested"},
			},
		},
	}

	out := formatRepliesMarkdown(replies, 1)
	if !strings.Contains(out, "> **Author replied") {
		t.Fatalf("expected author reply line, got %q", out)
	}
	if !strings.Contains(out, "> > **Reader replied") {
		t.Fatalf("expected nested reply, got %q", out)
	}
}

func TestFormatRepliesText(t *testing.T) {
	replies := []Comment{
		{
			User: CommentUser{Name: "Author"},
			Body: "Reply body",
			Replies: []Comment{
				{User: CommentUser{Name: "Nested"}, Body: "Child"},
			},
		},
	}

	out := formatRepliesText(replies, 1)
	if !strings.Contains(out, "> Author replied:") {
		t.Fatalf("expected author reply line, got %q", out)
	}
	if !strings.Contains(out, "> > Nested replied:") {
		t.Fatalf("expected nested reply line, got %q", out)
	}
}
