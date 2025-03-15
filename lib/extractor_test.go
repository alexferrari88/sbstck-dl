package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a sample Post for testing
func createSamplePost() Post {
	return Post{
		Id:               123,
		PublicationId:    456,
		Type:             "post",
		Slug:             "test-post",
		PostDate:         "2023-01-01",
		CanonicalUrl:     "https://example.substack.com/p/test-post",
		PreviousPostSlug: "previous-post",
		NextPostSlug:     "next-post",
		CoverImage:       "https://example.com/image.jpg",
		Description:      "Test description",
		WordCount:        100,
		Title:            "Test Post",
		BodyHTML:         "<p>This is a <strong>test</strong> post.</p>",
	}
}

// Helper function to create a mock HTML page with embedded JSON
func createMockSubstackHTML(post Post) string {
	// Create a wrapper and marshal it to JSON
	wrapper := PostWrapper{Post: post}
	jsonBytes, _ := json.Marshal(wrapper)

	// Escape quotes for embedding in JavaScript
	escapedJSON := strings.ReplaceAll(string(jsonBytes), `"`, `\"`)

	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
</head>
<body>
  <div class="post">Some content</div>
  <script>
    window._preloads = JSON.parse("%s")
  </script>
</body>
</html>
`, post.Title, escapedJSON)
}

// Test RawPost.ToPost
func TestRawPostToPost(t *testing.T) {
	// Create a sample post
	expectedPost := createSamplePost()

	// Create a wrapper and marshal it to JSON
	wrapper := PostWrapper{Post: expectedPost}
	jsonBytes, err := json.Marshal(wrapper)
	require.NoError(t, err)

	// Create a RawPost with the JSON string
	rawPost := RawPost{str: string(jsonBytes)}

	// Test conversion
	actualPost, err := rawPost.ToPost()
	require.NoError(t, err)

	// Verify the result
	assert.Equal(t, expectedPost, actualPost)

	// Test with invalid JSON
	invalidRawPost := RawPost{str: "invalid json"}
	_, err = invalidRawPost.ToPost()
	assert.Error(t, err)
}

// Test Post format conversions
func TestPostFormatConversions(t *testing.T) {
	post := createSamplePost()

	t.Run("ToHTML", func(t *testing.T) {
		html := post.ToHTML(true)
		assert.Contains(t, html, "<h1>Test Post</h1>")
		assert.Contains(t, html, "<p>This is a <strong>test</strong> post.</p>")

		htmlNoTitle := post.ToHTML(false)
		assert.NotContains(t, htmlNoTitle, "<h1>Test Post</h1>")
		assert.Contains(t, htmlNoTitle, "<p>This is a <strong>test</strong> post.</p>")
	})

	t.Run("ToMD", func(t *testing.T) {
		md, err := post.ToMD(true)
		require.NoError(t, err)
		assert.Contains(t, md, "# Test Post")
		assert.Contains(t, md, "This is a **test** post.")

		mdNoTitle, err := post.ToMD(false)
		require.NoError(t, err)
		assert.NotContains(t, mdNoTitle, "# Test Post")
		assert.Contains(t, mdNoTitle, "This is a **test** post.")
	})

	t.Run("ToText", func(t *testing.T) {
		text := post.ToText(true)
		assert.Contains(t, text, "Test Post")
		assert.Contains(t, text, "This is a test post.")

		textNoTitle := post.ToText(false)
		assert.NotContains(t, textNoTitle, "Test Post\n\n")
		assert.Contains(t, textNoTitle, "This is a test post.")
	})

	t.Run("ToJSON", func(t *testing.T) {
		jsonStr, err := post.ToJSON()
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"id":123`)
		assert.Contains(t, jsonStr, `"title":"Test Post"`)
	})

	t.Run("contentForFormat", func(t *testing.T) {
		// Test valid formats
		for _, format := range []string{"html", "md", "txt"} {
			content, err := post.contentForFormat(format, true)
			assert.NoError(t, err)
			assert.NotEmpty(t, content)
		}

		// Test invalid format
		_, err := post.contentForFormat("invalid", true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown format")
	})
}

// Test Post.WriteToFile
func TestPostWriteToFile(t *testing.T) {
	post := createSamplePost()
	tempDir, err := os.MkdirTemp("", "post-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	formats := []string{"html", "md", "txt"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			filePath := filepath.Join(tempDir, fmt.Sprintf("test.%s", format))
			err := post.WriteToFile(filePath, format)
			require.NoError(t, err)

			// Verify file exists
			fileInfo, err := os.Stat(filePath)
			assert.NoError(t, err)
			assert.True(t, fileInfo.Size() > 0, "File should not be empty")

			// Read file content
			content, err := os.ReadFile(filePath)
			require.NoError(t, err)

			// Check content based on format
			switch format {
			case "html":
				assert.Contains(t, string(content), "<h1>Test Post</h1>")
				assert.Contains(t, string(content), "<p>This is a <strong>test</strong> post.</p>")
			case "md":
				assert.Contains(t, string(content), "# Test Post")
				assert.Contains(t, string(content), "This is a **test** post.")
			case "txt":
				assert.Contains(t, string(content), "Test Post")
				assert.Contains(t, string(content), "This is a test post.")
			}
		})
	}

	// Test writing to a non-existent directory
	t.Run("creating directory", func(t *testing.T) {
		newDir := filepath.Join(tempDir, "subdir", "nested")
		filePath := filepath.Join(newDir, "test.html")
		err := post.WriteToFile(filePath, "html")
		assert.NoError(t, err)

		// Verify directory was created
		_, err = os.Stat(newDir)
		assert.NoError(t, err)
	})

	// Test invalid format
	t.Run("invalid format", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "test.invalid")
		err := post.WriteToFile(filePath, "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown format")
	})
}

// Test extractJSONString function
func TestExtractJSONString(t *testing.T) {
	t.Run("validHTML", func(t *testing.T) {
		post := createSamplePost()
		html := createMockSubstackHTML(post)

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		require.NoError(t, err)

		jsonString, err := extractJSONString(doc)
		require.NoError(t, err)

		// Create a wrapper and marshal to get expected JSON
		wrapper := PostWrapper{Post: post}
		expectedJSONBytes, _ := json.Marshal(wrapper)

		// The expected JSON needs to have escaped quotes to match the actual output
		expectedJSON := strings.ReplaceAll(string(expectedJSONBytes), `"`, `\"`)
		assert.Equal(t, expectedJSON, jsonString)
	})

	t.Run("invalidHTML", func(t *testing.T) {
		// Test HTML without the required script
		invalidHTML := `<html><body><p>No script here</p></body></html>`
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(invalidHTML))
		require.NoError(t, err)

		_, err = extractJSONString(doc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to extract JSON string")
	})

	t.Run("malformedScript", func(t *testing.T) {
		// Test HTML with malformed script
		malformedHTML := `
		<html><body>
		<script>
		  window._preloads = JSON.parse("incomplete
		</script>
		</body></html>`

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(malformedHTML))
		require.NoError(t, err)

		_, err = extractJSONString(doc)
		assert.Error(t, err)
	})
}

// Create a real test server that serves mock Substack pages
func createSubstackTestServer() (*httptest.Server, map[string]Post) {
	posts := make(map[string]Post)

	// Create several sample posts
	for i := 1; i <= 5; i++ {
		post := createSamplePost()
		post.Id = i
		post.Title = fmt.Sprintf("Test Post %d", i)
		post.Slug = fmt.Sprintf("test-post-%d", i)
		post.CanonicalUrl = fmt.Sprintf("https://example.substack.com/p/test-post-%d", i)

		posts[fmt.Sprintf("/p/test-post-%d", i)] = post
	}

	// Create sitemap XML
	sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`
	for _, post := range posts {
		sitemapXML += fmt.Sprintf(`  <url>
    <loc>https://example.substack.com/p/%s</loc>
    <lastmod>%s</lastmod>
  </url>
`, post.Slug, post.PostDate)
	}
	sitemapXML += `</urlset>`

	// Create server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Handle sitemap request
		if path == "/sitemap.xml" {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(sitemapXML))
			return
		}

		// Handle post requests
		post, exists := posts[path]
		if exists {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(createMockSubstackHTML(post)))
			return
		}

		// Handle not found
		w.WriteHeader(http.StatusNotFound)
	}))

	return server, posts
}

// Test Extractor.ExtractPost
func TestExtractorExtractPost(t *testing.T) {
	// Create test server
	server, posts := createSubstackTestServer()
	defer server.Close()

	// Create extractor with default fetcher
	extractor := NewExtractor(nil)

	// Test successful extraction
	t.Run("successfulExtraction", func(t *testing.T) {
		ctx := context.Background()

		for path, expectedPost := range posts {
			postURL := server.URL + path
			extractedPost, err := extractor.ExtractPost(ctx, postURL)

			require.NoError(t, err)
			assert.Equal(t, expectedPost.Id, extractedPost.Id)
			assert.Equal(t, expectedPost.Title, extractedPost.Title)
			assert.Equal(t, expectedPost.BodyHTML, extractedPost.BodyHTML)
		}
	})

	// Test invalid URL
	t.Run("invalidURL", func(t *testing.T) {
		ctx := context.Background()
		_, err := extractor.ExtractPost(ctx, "invalid-url")
		assert.Error(t, err)
	})

	// Test not found
	t.Run("notFound", func(t *testing.T) {
		ctx := context.Background()
		_, err := extractor.ExtractPost(ctx, server.URL+"/p/non-existent")
		assert.Error(t, err)
	})

	// Test context cancellation
	t.Run("contextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := extractor.ExtractPost(ctx, server.URL+"/p/test-post-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context")
	})
}

// Test Extractor.GetAllPostsURLs
func TestExtractorGetAllPostsURLs(t *testing.T) {
	// Create test server
	server, posts := createSubstackTestServer()
	defer server.Close()

	// Create extractor
	extractor := NewExtractor(nil)
	ctx := context.Background()

	// Test without filter
	t.Run("withoutFilter", func(t *testing.T) {
		urls, err := extractor.GetAllPostsURLs(ctx, server.URL, nil)
		require.NoError(t, err)

		// Should find all post URLs
		assert.Equal(t, len(posts), len(urls))

		// Check each URL is present
		for _, post := range posts {
			found := false
			for _, url := range urls {
				if strings.Contains(url, post.Slug) {
					found = true
					break
				}
			}
			assert.True(t, found, "URL for post %s should be present", post.Slug)
		}
	})

	// Test with date filter
	t.Run("withDateFilter", func(t *testing.T) {
		// Filter for posts after 2023-01-01
		dateFilter := func(date string) bool {
			return date > "2023-01-01"
		}

		urls, err := extractor.GetAllPostsURLs(ctx, server.URL, dateFilter)
		require.NoError(t, err)

		// Our test data all has the same date, so this depends on our test data
		// In real data, this would filter based on the date
		assert.Len(t, urls, 0)
	})

	// Test with context cancellation
	t.Run("contextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := extractor.GetAllPostsURLs(ctx, server.URL, nil)
		assert.Error(t, err)
	})

	// Test with invalid URL
	t.Run("invalidURL", func(t *testing.T) {
		_, err := extractor.GetAllPostsURLs(ctx, "invalid-url", nil)
		assert.Error(t, err)
	})
}

// Test Extractor.ExtractAllPosts
func TestExtractorExtractAllPosts(t *testing.T) {
	// Create test server
	server, posts := createSubstackTestServer()
	defer server.Close()

	// Create URLs list
	urls := make([]string, 0, len(posts))
	for path := range posts {
		urls = append(urls, server.URL+path)
	}

	// Create extractor
	extractor := NewExtractor(nil)
	ctx := context.Background()

	// Test successful extraction of all posts
	t.Run("successfulExtraction", func(t *testing.T) {
		resultCh := extractor.ExtractAllPosts(ctx, urls)

		// Collect results
		results := make(map[int]Post)
		errorCount := 0

		for result := range resultCh {
			if result.Err != nil {
				errorCount++
			} else {
				results[result.Post.Id] = result.Post
			}
		}

		// Verify results
		assert.Equal(t, 0, errorCount, "There should be no errors")
		assert.Equal(t, len(posts), len(results), "All posts should be extracted")

		// Check each post
		for _, post := range posts {
			extractedPost, exists := results[post.Id]
			assert.True(t, exists, "Post with ID %d should be extracted", post.Id)
			if exists {
				assert.Equal(t, post.Title, extractedPost.Title)
				assert.Equal(t, post.BodyHTML, extractedPost.BodyHTML)
			}
		}
	})

	// Test with context cancellation
	t.Run("contextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		resultCh := extractor.ExtractAllPosts(ctx, urls)

		// Cancel after receiving first result
		var count int
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			for result := range resultCh {
				if result.Err != nil {
					continue
				}
				count++
				if count == 1 {
					cancel()
					// Add a small delay to ensure cancellation propagates
					time.Sleep(100 * time.Millisecond)
					break // Exit loop early after cancelling
				}
			}
		}()

		wg.Wait()

		// We should have received at least one result before cancellation
		assert.GreaterOrEqual(t, count, 1)
		// Don't assert that count < len(posts) since on fast machines all might complete
	})

	// Test with mixed responses (some successful, some errors)
	t.Run("mixedResponses", func(t *testing.T) {
		// Add some invalid URLs to the list
		mixedUrls := append([]string{"invalid-url", server.URL + "/p/non-existent"}, urls...)

		resultCh := extractor.ExtractAllPosts(ctx, mixedUrls)

		// Collect results
		successCount := 0
		errorCount := 0

		for result := range resultCh {
			if result.Err != nil {
				errorCount++
			} else {
				successCount++
			}
		}

		// Verify results
		assert.Equal(t, len(posts), successCount, "All valid posts should be extracted")
		assert.Equal(t, 2, errorCount, "There should be errors for invalid URLs")
	})

	// Test worker concurrency limiting
	t.Run("concurrencyLimit", func(t *testing.T) {
		// Create a large number of duplicate URLs to test concurrency
		manyUrls := make([]string, 50)
		for i := range manyUrls {
			manyUrls[i] = urls[i%len(urls)]
		}

		// Create a channel to track concurrent requests
		type accessRecord struct {
			url       string
			timestamp time.Time
		}

		accessCh := make(chan accessRecord, len(manyUrls))

		// Create a test server that records access times
		concurrentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accessCh <- accessRecord{
				url:       r.URL.Path,
				timestamp: time.Now(),
			}

			// Simulate some processing time
			time.Sleep(100 * time.Millisecond)

			// Serve the same content as the regular server
			path := r.URL.Path
			post, exists := posts[path]
			if exists {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(createMockSubstackHTML(post)))
				return
			}

			w.WriteHeader(http.StatusNotFound)
		}))
		defer concurrentServer.Close()

		// Replace URLs with concurrent server URLs
		concurrentUrls := make([]string, len(manyUrls))
		for i, u := range manyUrls {
			path := strings.TrimPrefix(u, server.URL)
			concurrentUrls[i] = concurrentServer.URL + path
		}

		// Create extractor with limited workers
		customFetcher := NewFetcher(WithMaxWorkers(10), WithRatePerSecond(100))
		concurrentExtractor := NewExtractor(customFetcher)

		// Start extraction
		resultCh := concurrentExtractor.ExtractAllPosts(ctx, concurrentUrls)

		// Collect all results to make sure extraction completes
		var results []ExtractResult
		for result := range resultCh {
			results = append(results, result)
		}

		// Close the access channel since we're done receiving
		close(accessCh)

		// Process access records to determine concurrency
		var accessRecords []accessRecord
		for record := range accessCh {
			accessRecords = append(accessRecords, record)
		}

		// Sort access records by timestamp
		maxConcurrent := 0
		activeTimes := make([]time.Time, 0)

		for _, record := range accessRecords {
			// Add this request's start time
			activeTimes = append(activeTimes, record.timestamp)

			// Expire any requests that would have completed by now
			newActiveTimes := make([]time.Time, 0)
			for _, t := range activeTimes {
				if t.Add(100 * time.Millisecond).After(record.timestamp) {
					newActiveTimes = append(newActiveTimes, t)
				}
			}
			activeTimes = newActiveTimes

			// Update max concurrent
			if len(activeTimes) > maxConcurrent {
				maxConcurrent = len(activeTimes)
			}
		}

		// Verify concurrency was limited appropriately
		// Note: This test is timing-dependent and may need adjustment
		assert.LessOrEqual(t, maxConcurrent, 15, "Concurrency should be limited")

		// Ensure all requests were processed
		assert.Equal(t, len(concurrentUrls), len(results))
	})
}

// Test error handling

func TestExtractorErrorHandling(t *testing.T) {
	// Create a server that simulates various errors
	var requestCount atomic.Int32

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get request counter
		count := requestCount.Add(1) // Using count, not requestID
		path := r.URL.Path

		// Simulate different errors based on path - order matters here!
		switch {
		case path == "/p/normal-post":
			// Return a valid post
			post := createSamplePost()
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(createMockSubstackHTML(post)))
			return

		case strings.Contains(path, "not-found"):
			w.WriteHeader(http.StatusNotFound)
			return

		case strings.Contains(path, "server-error"):
			w.WriteHeader(http.StatusInternalServerError)
			return

		case strings.Contains(path, "rate-limit"):
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return

		case strings.Contains(path, "bad-json"):
			// Return valid HTML but with malformed JSON
			html := `
			<!DOCTYPE html>
			<html>
			<head><title>Bad JSON</title></head>
			<body>
			  <script>
				window._preloads = JSON.parse("{malformed json}")
			  </script>
			</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
			return

		case strings.Contains(path, "timeout-post") || count%5 == 0: // Use count here
			// Use a long sleep to ensure timeout - longer than the client timeout
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
			return

		default:
			// Return a valid post for other paths
			post := createSamplePost()
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(createMockSubstackHTML(post)))
			return
		}
	}))
	defer errorServer.Close()

	// Create paths for different error scenarios
	paths := []string{
		"/p/normal-post",
		"/p/not-found",
		"/p/server-error",
		"/p/rate-limit",
		"/p/bad-json",
		"/p/timeout-post",
	}

	// Create URLs
	urls := make([]string, len(paths))
	for i, path := range paths {
		urls[i] = errorServer.URL + path
	}

	// Create extractor with short timeout and limited retries
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxElapsedTime = 1 * time.Second // Short timeout for tests
	backoffCfg.InitialInterval = 100 * time.Millisecond

	fetcher := NewFetcher(
		WithTimeout(500*time.Millisecond), // Make timeout shorter than the sleep for timeout test
		WithBackOffConfig(backoffCfg),
	)

	extractor := NewExtractor(fetcher)
	ctx := context.Background()

	// Test individual error cases
	t.Run("NotFound", func(t *testing.T) {
		_, err := extractor.ExtractPost(ctx, errorServer.URL+"/p/not-found")
		assert.Error(t, err)
	})

	t.Run("ServerError", func(t *testing.T) {
		_, err := extractor.ExtractPost(ctx, errorServer.URL+"/p/server-error")
		assert.Error(t, err)
	})

	t.Run("RateLimit", func(t *testing.T) {
		_, err := extractor.ExtractPost(ctx, errorServer.URL+"/p/rate-limit")
		assert.Error(t, err)
	})

	t.Run("BadJSON", func(t *testing.T) {
		_, err := extractor.ExtractPost(ctx, errorServer.URL+"/p/bad-json")
		assert.Error(t, err)
	})

	t.Run("Timeout", func(t *testing.T) {
		// Test with a URL that will cause a timeout
		_, err := extractor.ExtractPost(ctx, errorServer.URL+"/p/timeout-post")
		assert.Error(t, err)
		// The error may be a context deadline exceeded or a timeout error
	})

	// Test handling multiple URLs with mixed errors
	t.Run("MixedErrors", func(t *testing.T) {
		resultCh := extractor.ExtractAllPosts(ctx, urls)

		// Collect results
		successCount := 0
		errorCount := 0

		for result := range resultCh {
			if result.Err != nil {
				errorCount++
			} else {
				successCount++
			}
		}

		// We expect at least one success (the normal post) and several errors
		assert.GreaterOrEqual(t, successCount, 1)
		assert.GreaterOrEqual(t, errorCount, 1) // At least one error (likely timeout)
	})
}

// Benchmarks
func BenchmarkExtractor(b *testing.B) {
	// Create test server
	server, posts := createSubstackTestServer()
	defer server.Close()

	// Create URLs
	urls := make([]string, 0, len(posts))
	for path := range posts {
		urls = append(urls, server.URL+path)
	}

	// Create extractor
	extractor := NewExtractor(nil)
	ctx := context.Background()

	// Benchmark single post extraction
	b.Run("ExtractPost", func(b *testing.B) {
		url := urls[0]
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			post, err := extractor.ExtractPost(ctx, url)
			if err != nil {
				b.Fatal(err)
			}

			// Simple check to ensure the compiler doesn't optimize away the result
			if post.Id <= 0 {
				b.Fatal("Invalid post ID")
			}
		}
	})

	// Benchmark format conversions
	post := createSamplePost()

	b.Run("ToHTML", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			html := post.ToHTML(true)
			if len(html) == 0 {
				b.Fatal("Empty HTML")
			}
		}
	})

	b.Run("ToMD", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			md, err := post.ToMD(true)
			if err != nil {
				b.Fatal(err)
			}
			if len(md) == 0 {
				b.Fatal("Empty markdown")
			}
		}
	})

	b.Run("ToText", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			text := post.ToText(true)
			if len(text) == 0 {
				b.Fatal("Empty text")
			}
		}
	})

	// Benchmark extracting all posts
	b.Run("ExtractAllPosts", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resultCh := extractor.ExtractAllPosts(ctx, urls)

			// Consume all results
			successCount := 0
			for result := range resultCh {
				if result.Err == nil {
					successCount++
				}
			}

			if successCount != len(posts) {
				b.Fatalf("Expected %d successful extractions, got %d", len(posts), successCount)
			}
		}
	})

	// Benchmark with larger number of URLs
	b.Run("ExtractAllPostsMany", func(b *testing.B) {
		// Create many duplicate URLs to test concurrency
		manyUrls := make([]string, 50)
		for i := range manyUrls {
			manyUrls[i] = urls[i%len(urls)]
		}

		// Create extractor with optimized settings for benchmark
		optimizedFetcher := NewFetcher(
			WithMaxWorkers(20),
			WithRatePerSecond(100),
			WithBurst(50),
		)

		optimizedExtractor := NewExtractor(optimizedFetcher)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			resultCh := optimizedExtractor.ExtractAllPosts(ctx, manyUrls)

			// Consume all results
			successCount := 0
			for result := range resultCh {
				if result.Err == nil {
					successCount++
				}
			}

			if successCount < len(manyUrls)-5 { // Allow a few errors
				b.Fatalf("Too few successful extractions: %d out of %d", successCount, len(manyUrls))
			}
		}
	})
}
