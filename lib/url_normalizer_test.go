package lib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// rewriteTransport rewrites requests to the target test server while keeping the original path.
type rewriteTransport struct {
	target *url.URL
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func TestNormalizeURLAliasFetchCanonical(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@writer/p-123" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><link rel="canonical" href="https://custom.example.com/p/sinead-gibney" /></head><body></body></html>`))
	}))
	defer server.Close()

	srvURL, _ := url.Parse(server.URL)

	fetcher := NewFetcher()
	fetcher.Client = &http.Client{
		Transport: &rewriteTransport{target: srvURL},
		Timeout:   15 * time.Second,
	}

	normalized, err := NormalizeURL(context.Background(), fetcher, "https://substack.com/@writer/p-123")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}
	if normalized != "https://custom.example.com/p/sinead-gibney" {
		t.Fatalf("expected canonical url, got %s", normalized)
	}
}

func TestNormalizeURLProfileNoteRewrite(t *testing.T) {
	normalized, err := NormalizeURL(context.Background(), NewFetcher(), "https://substack.com/profile/109896535-the-irish-politics-newsletter/note/c-144807887")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}
	expected := "https://substack.com/note/c-144807887"
	if normalized != expected {
		t.Fatalf("expected %s, got %s", expected, normalized)
	}
}

func TestNormalizeURLCustomDomainNote(t *testing.T) {
	normalized, err := NormalizeURL(context.Background(), NewFetcher(), "https://www.irishpoliticsnewsletter.ie/note/c-144807887")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}
	expected := "https://substack.com/note/c-144807887"
	if normalized != expected {
		t.Fatalf("expected %s, got %s", expected, normalized)
	}
}

func TestNormalizeURLPassThroughWithoutFetcher(t *testing.T) {
	input := "https://substack.com/@tullmcadoo/p-163653785"
	normalized, err := NormalizeURL(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}
	if normalized != input {
		t.Fatalf("expected %s, got %s", input, normalized)
	}
}

func TestNormalizeURLEmpty(t *testing.T) {
	if _, err := NormalizeURL(context.Background(), NewFetcher(), ""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}
