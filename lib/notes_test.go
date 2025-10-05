package lib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNotesClientFetchAllUserActivity(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	handler.HandleFunc("/api/v1/reader/feed/profile/123", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("cursor") == "" {
			resp := NotesResponse{
				Items: []ActivityItem{
					{Type: "comment", Comment: Comment{ID: 1, Body: "<p>Note body</p>"}, Context: Context{Type: "note"}},
					{Type: "comment", Comment: Comment{ID: 2, Body: "<p>Regular comment</p>"}},
				},
				NextCursor: "next",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := NotesResponse{Items: []ActivityItem{}}
		json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	fetcher := NewFetcher()
	client := NewNotesClient(fetcher)
	client.SetBaseURL(server.URL)

	items, err := client.FetchAllUserActivity(context.Background(), "123", 3, false)
	if err != nil {
		t.Fatalf("FetchAllUserActivity returned error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestNotesClientConvertAndSave(t *testing.T) {
	fetcher := NewFetcher()
	client := NewNotesClient(fetcher)

	item := ActivityItem{
		Type: "comment",
		Comment: Comment{
			ID:            144807887,
			Body:          "<p>Hello note</p>",
			Date:          "2025-08-12T21:53:29.245Z",
			Name:          "The Irish Politics Newsletter",
			Handle:        "tullmcadoo",
			ReactionCount: 23,
			Restacks:      6,
		},
		Post: map[string]interface{}{"title": "Context title"},
	}

	note := client.ConvertCommentToNote(item.Comment, item)
	if note == nil {
		t.Fatal("expected a note, got nil")
	}

	if note.URL != "https://substack.com/@tullmcadoo/note/c-144807887" {
		t.Fatalf("unexpected canonical URL: %s", note.URL)
	}

	dir := t.TempDir()
	if err := client.SaveNote(note, dir, "md"); err != nil {
		t.Fatalf("SaveNote returned error: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading temp dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	content, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("reading saved note: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected markdown content, got empty file")
	}
}

func TestNotesClientIsLikelyRegularComment(t *testing.T) {
	client := NewNotesClient(NewFetcher())

	noteItem := ActivityItem{Context: Context{Type: "note"}}
	if client.IsLikelyRegularComment(Comment{}, noteItem) {
		t.Fatal("note context should not be treated as regular comment")
	}

	regular := ActivityItem{Context: Context{Type: "comment"}}
	if !client.IsLikelyRegularComment(Comment{}, regular) {
		t.Fatal("non-note context should be treated as regular comment")
	}
}
