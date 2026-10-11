package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discoveryExtractor() *Extractor {
	return NewExtractor(NewFetcher(WithRatePerSecond(100000), WithBurst(100),
		WithBackOffConfig(&backoff.StopBackOff{}), WithTimeout(time.Second)))
}

func discoveryPost(id int64) archivePost {
	return archivePost{ID: id, CanonicalURL: fmt.Sprintf("/p/post-%d", id), PostDate: fmt.Sprintf("2025-01-%02dT12:00:00.000Z", id)}
}

func TestDiscoverySitemapPreferred(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		filter DateFilterFunc
		want   []string
	}{
		{"absolute", `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://canonical.example/p/one</loc><lastmod>2025-01-01</lastmod></url></urlset>`, nil, []string{"https://canonical.example/p/one"}},
		{"empty", `<urlset/>`, nil, []string{}},
		{"filtered", `<urlset><url><loc>/p/one</loc><lastmod>2025-01-01</lastmod></url></urlset>`, func(string) bool { return false }, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/sitemap.xml", r.URL.Path)
				assert.Empty(t, r.URL.RawQuery)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			got, err := discoveryExtractor().GetAllPostsURLs(context.Background(), server.URL+"/archive?ignored=yes#fragment", tc.filter)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestDiscoveryMissingAndUnusableSitemap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing", 404, "missing"}, {"gone", 410, "gone"},
		{"HTML", 200, "<html><body>No sitemap</body></html>"},
		{"malformed", 200, "<urlset><url></urlset>"},
		{"missingLocation", 200, "<urlset><url/></urlset>"},
		{"index", 200, "<sitemapindex/>"},
		{"trailing", 200, "<urlset/><html/>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var apiCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sitemap.xml" {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				assert.Equal(t, "/api/v1/archive", r.URL.Path)
				apiCalls.Add(1)
				fmt.Fprint(w, "[]")
			}))
			defer server.Close()
			got, err := discoveryExtractor().GetAllPostsURLs(context.Background(), server.URL, nil)
			require.NoError(t, err)
			assert.Empty(t, got)
			assert.EqualValues(t, 1, apiCalls.Load())
		})
	}
}

func TestDiscoveryArchivePaginationAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		filter  DateFilterFunc
		wantIDs []int64
	}{
		{"all", nil, []int64{1, 2, 3, 4}},
		{"after", func(d string) bool { return d > "2025-01-03" }, []int64{3, 4}},
		{"before", func(d string) bool { return d < "2025-01-03" }, []int64{1, 2}},
		{"combined", func(d string) bool { return d > "2025-01-02" && d < "2025-01-04" }, []int64{2, 3}},
		{"none", func(string) bool { return false }, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var offsets []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sitemap.xml" {
					http.NotFound(w, r)
					return
				}
				assert.Equal(t, "/api/v1/archive", r.URL.Path)
				assert.Equal(t, "12", r.URL.Query().Get("limit"))
				assert.Equal(t, "new", r.URL.Query().Get("sort"))
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				mu.Lock()
				offsets = append(offsets, offset)
				mu.Unlock()
				pages := map[int][]archivePost{0: {discoveryPost(1), discoveryPost(2)}, 2: {discoveryPost(2), discoveryPost(3)}, 4: {discoveryPost(4)}, 5: {}}
				posts, ok := pages[offset]
				assert.True(t, ok, "unexpected offset %d", offset)
				json.NewEncoder(w).Encode(posts)
			}))
			defer server.Close()
			got, err := discoveryExtractor().GetAllPostsURLs(context.Background(), server.URL, tc.filter)
			require.NoError(t, err)
			want := make([]string, 0, len(tc.wantIDs))
			for _, id := range tc.wantIDs {
				want = append(want, server.URL+discoveryPost(id).CanonicalURL)
			}
			assert.Equal(t, want, got)
			mu.Lock()
			assert.Equal(t, []int{0, 2, 4, 5}, offsets)
			mu.Unlock()
		})
	}
}

func TestDiscoveryArchiveCustomDomainAndRelativeLinks(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if r.URL.Query().Get("offset") == "0" {
			posts := []archivePost{discoveryPost(1), discoveryPost(2), discoveryPost(3)}
			posts[1].CanonicalURL = "https://custom.example/p/post-2"
			posts[2].CanonicalURL = "https://canonical.substack.com/p/post-3"
			json.NewEncoder(w).Encode(posts)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	got, err := discoveryExtractor().GetAllPostsURLs(context.Background(), server.URL, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{server.URL + "/p/post-1", server.URL + "/p/post-2", server.URL + "/p/post-3"}, got)
	assert.EqualValues(t, 2, calls.Load())
}

func TestDiscoveryArchiveFailuresDoNotReturnPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		filter DateFilterFunc
	}{
		{"repeated", 200, `[{"id":1,"canonical_url":"/p/post-1"}]`, nil},
		{"repeatedFiltered", 200, `[{"id":1,"canonical_url":"/p/post-1","post_date":"2025-01-01T12:00:00Z"}]`, func(string) bool { return false }},
		{"repeatedURLWithNewID", 200, `[{"id":2,"canonical_url":"/p/post-1"}]`, nil},
		{"malformed", 200, `[{`, nil}, {"object", 200, `{}`, nil}, {"null", 200, `null`, nil},
		{"trailing", 200, `[] []`, nil}, {"HTML", 200, `<html/>`, nil},
		{"badID", 200, `[{"id":0,"canonical_url":"/p/two"}]`, nil},
		{"missingURL", 200, `[{"id":2}]`, nil},
		{"badURL", 200, `[{"id":2,"canonical_url":"javascript:/p/two"}]`, nil},
		{"hostlessURL", 200, `[{"id":2,"canonical_url":"https:/p/two"}]`, nil},
		{"traversal", 200, `[{"id":2,"canonical_url":"/p/../api"}]`, nil},
		{"missingDate", 200, `[{"id":2,"canonical_url":"/p/two"}]`, func(string) bool { return true }},
		{"badDate", 200, `[{"id":2,"canonical_url":"/p/two","post_date":"invalid"}]`, func(string) bool { return true }},
		{"unauthorized", 401, `[]`, nil}, {"forbidden", 403, `[]`, nil},
		{"rateLimited", 429, `[]`, nil}, {"serverError", 500, `[]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sitemap.xml" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("offset") == "0" {
					json.NewEncoder(w).Encode([]archivePost{discoveryPost(1)})
					return
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			got, err := discoveryExtractor().GetAllPostsURLs(context.Background(), server.URL, tc.filter)
			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestDiscoveryCancellation(t *testing.T) {
	t.Run("during sitemap filter", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			fmt.Fprint(w, `<urlset><url><loc>/p/one</loc><lastmod>2025-01-01</lastmod></url></urlset>`)
		}))
		defer server.Close()
		got, err := discoveryExtractor().GetAllPostsURLs(ctx, server.URL, func(string) bool { cancel(); return true })
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
		assert.EqualValues(t, 1, calls.Load())
	})
	t.Run("between archive pages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var apiCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sitemap.xml" {
				http.NotFound(w, r)
				return
			}
			apiCalls.Add(1)
			json.NewEncoder(w).Encode([]archivePost{discoveryPost(1)})
		}))
		defer server.Close()
		got, err := discoveryExtractor().GetAllPostsURLs(ctx, server.URL, func(string) bool { cancel(); return true })
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
		assert.EqualValues(t, 1, apiCalls.Load())
	})
	t.Run("in flight", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sitemap.xml" {
				http.NotFound(w, r)
				return
			}
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		go func() { <-started; cancel() }()
		got, err := discoveryExtractor().GetAllPostsURLs(ctx, server.URL, nil)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
	})
}

func TestDiscoveryArchiveBoundedPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		json.NewEncoder(w).Encode([]archivePost{discoveryPost(offset + 1)})
	}))
	defer server.Close()
	root, err := url.Parse(server.URL)
	require.NoError(t, err)
	got, err := discoveryExtractor().archiveURLs(context.Background(), root, nil, 2)
	assert.ErrorContains(t, err, "exceeded 2 pages")
	assert.Nil(t, got)
}

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type discoveryBody struct {
	io.Reader
	closed bool
}

func (b *discoveryBody) Close() error { b.closed = true; return nil }

func TestDiscoveryCustomOriginAndResponseCleanup(t *testing.T) {
	extractor := discoveryExtractor()
	var previous *discoveryBody
	var requests []string
	extractor.fetcher.Client.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
		if previous != nil {
			assert.True(t, previous.closed, "previous response must close before next request")
		}
		assert.Equal(t, "https", r.URL.Scheme)
		assert.Equal(t, "newsletter.example", r.URL.Host)
		requests = append(requests, r.URL.Path)
		status, response := http.StatusOK, "[]"
		if r.URL.Path == "/sitemap.xml" {
			status, response = http.StatusNotFound, "missing"
		} else if r.URL.Query().Get("offset") == "0" {
			response = `[{"id":1,"canonical_url":"https://publication.substack.com/p/alias"},{"id":2,"canonical_url":"/p/relative"}]`
		}
		previous = &discoveryBody{Reader: strings.NewReader(response)}
		return &http.Response{StatusCode: status, Body: previous, Header: make(http.Header)}, nil
	})
	got, err := extractor.GetAllPostsURLs(context.Background(), "https://newsletter.example/archive?ignored=1", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://newsletter.example/p/alias", "https://newsletter.example/p/relative"}, got)
	assert.Equal(t, []string{"/sitemap.xml", "/api/v1/archive", "/api/v1/archive"}, requests)
	assert.True(t, previous.closed)
}
