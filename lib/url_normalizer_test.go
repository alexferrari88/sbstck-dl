package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type hostRewriteTransport struct {
	mapping map[string]*url.URL
	base    http.RoundTripper
}

func (t *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	cloneURL := *clone.URL
	host := strings.ToLower(cloneURL.Host)

	if target, ok := t.mapping[host]; ok {
		cloneURL.Scheme = target.Scheme
		cloneURL.Host = target.Host
	} else if idx := strings.Index(host, ":"); idx != -1 {
		if target, ok := t.mapping[host[:idx]]; ok {
			cloneURL.Scheme = target.Scheme
			cloneURL.Host = target.Host
		}
	}

	clone.URL = &cloneURL
	clone.Host = cloneURL.Host

	resp, err := t.base.RoundTrip(clone)
	if err != nil {
		return nil, err
	}

	if resp.Request != nil && resp.Request.URL != nil {
		resp.Request.URL.Scheme = req.URL.Scheme
		resp.Request.URL.Host = req.URL.Host
	}

	return resp, nil
}

func newTestFetcher(server *httptest.Server, hosts ...string) *Fetcher {
	mapping := make(map[string]*url.URL)
	targetURL, _ := url.Parse(server.URL)
	for _, host := range armHosts(hosts) {
		mapping[host] = targetURL
	}

	client := server.Client()
	client.Timeout = 5 * time.Second
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &hostRewriteTransport{
		mapping: mapping,
		base:    base,
	}

	fetcher := NewFetcher()
	fetcher.Client = client
	return fetcher
}

func armHosts(hosts []string) []string {
	normalized := make([]string, len(hosts))
	for i, h := range hosts {
		normalized[i] = strings.ToLower(h)
	}
	return normalized
}

func TestNormalizeURLCanonicalizesSubstackAlias(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@writer/p-123":
			payload := `{"post":{"canonical_url":"http://www.irishpoliticsnewsletter.ie/p/slug"}}`
			fmt.Fprintf(w, `<html><head><script>window._preloads = JSON.parse(%q);</script></head><body></body></html>`, payload)
		case "/p/slug":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := newTestFetcher(server, "substack.com", "www.irishpoliticsnewsletter.ie")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	normalized, err := NormalizeURL(ctx, fetcher, "https://substack.com/@writer/p-123")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}

	if !strings.Contains(normalized, "irishpoliticsnewsletter.ie/p/slug") {
		t.Fatalf("expected canonical path, got %s", normalized)
	}
}

func TestNormalizeURLCanonicalizesProfileAlias(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@writer":
			payload := `{"profile":{"primaryPublication":{"custom_domain":"www.irishpoliticsnewsletter.ie"}}}`
			fmt.Fprintf(w, `<html><head><script>window._preloads = JSON.parse(%q);</script></head><body></body></html>`, payload)
		case "/":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := newTestFetcher(server, "substack.com", "www.irishpoliticsnewsletter.ie")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	normalized, err := NormalizeURL(ctx, fetcher, "https://substack.com/@writer")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}

	if !strings.Contains(normalized, "irishpoliticsnewsletter.ie") {
		t.Fatalf("expected canonical domain, got %s", normalized)
	}
	if !strings.HasSuffix(normalized, "/") {
		t.Fatalf("expected trailing slash, got %s", normalized)
	}
}

func TestNormalizeURLCanonicalizesProfilePrefix(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/some-user":
			payload := `{"profile":{"primaryPublication":{"custom_domain":"www.irishpoliticsnewsletter.ie"}}}`
			fmt.Fprintf(w, `<html><head><script>window._preloads = JSON.parse(%q);</script></head><body></body></html>`, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := newTestFetcher(server, "substack.com", "www.irishpoliticsnewsletter.ie")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	normalized, err := NormalizeURL(ctx, fetcher, "https://substack.com/profile/some-user")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}

	if !strings.Contains(normalized, "irishpoliticsnewsletter.ie") {
		t.Fatalf("expected canonical domain, got %s", normalized)
	}
	if !strings.HasSuffix(normalized, "/") {
		t.Fatalf("expected trailing slash, got %s", normalized)
	}
}

func TestNormalizeURLFallsBackOn404(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	fetcher := newTestFetcher(server, "substack.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := "https://substack.com/@writer/p-404"
	normalized, err := NormalizeURL(ctx, fetcher, input)
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}

	if normalized != input {
		t.Fatalf("expected %s, got %s", input, normalized)
	}
}

func TestNormalizeURLCanonicalNoteFallsBackToAlias(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/note/c-123":
			w.Header().Set("Location", "https://substack.com/profile/handle-note/note/c-123")
			w.WriteHeader(http.StatusFound)
		case "/profile/handle-note/note/c-123":
			payload := `{"feedData":{"feedItem":{"comment":{"handle":"handle-note"}}}}`
			fmt.Fprintf(w, `<html><head><script>window._preloads = JSON.parse(%q);</script></head><body></body></html>`, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := newTestFetcher(server, "substack.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	normalized, err := NormalizeURL(ctx, fetcher, "https://www.customdomain.com/note/c-123")
	if err != nil {
		t.Fatalf("NormalizeURL returned error: %v", err)
	}

	if !strings.HasSuffix(normalized, "/note/c-123") {
		t.Fatalf("expected canonical note suffix, got %s", normalized)
	}
}
