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
		Subtitle:         "Test subtitle",
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
		for _, format := range []string{"html", "md", "obsidian-md", "txt"} {
			content, err := post.contentForFormat(format, true)
			assert.NoError(t, err)
			assert.NotEmpty(t, content)
		}

		// Test invalid format
		_, err := post.contentForFormat("invalid", true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown format")
	})

	// Test error handling for format conversions
	t.Run("ToMD error handling", func(t *testing.T) {
		// Create a post with problematic HTML for markdown conversion
		// Note: html-to-markdown library is quite robust, so we test with extremely malformed HTML
		problemPost := createSamplePost()
		problemPost.BodyHTML = "<div><p>Nested without closing</div>"

		// This should still work as the library handles most malformed HTML
		_, err := problemPost.ToMD(true)
		assert.NoError(t, err) // The library is quite tolerant
	})

	t.Run("ToJSON error handling", func(t *testing.T) {
		// Create a post that would have issues during JSON marshaling
		// This is hard to trigger with normal Post struct, but we can test the error path
		problemPost := createSamplePost()

		// Test with valid data (JSON marshaling rarely fails with valid structs)
		jsonStr, err := problemPost.ToJSON()
		assert.NoError(t, err)
		assert.NotEmpty(t, jsonStr)

		// Verify the JSON is valid
		var parsedPost Post
		err = json.Unmarshal([]byte(jsonStr), &parsedPost)
		assert.NoError(t, err)
		assert.Equal(t, problemPost.Id, parsedPost.Id)
		assert.Equal(t, problemPost.Title, parsedPost.Title)
	})
}

// Test Post.WriteToFile
func TestPostWriteToFile(t *testing.T) {
	post := createSamplePost()
	tempDir, err := os.MkdirTemp("", "post-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	formats := []string{"html", "md", "obsidian-md", "txt"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			filePath := filepath.Join(tempDir, fmt.Sprintf("test.%s", format))
			err := post.WriteToFile(filePath, format, false)
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
			case "obsidian-md":
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
		err := post.WriteToFile(filePath, "html", false)
		assert.NoError(t, err)

		// Verify directory was created
		_, err = os.Stat(newDir)
		assert.NoError(t, err)
	})

	// Test invalid format
	t.Run("invalid format", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "test.invalid")
		err := post.WriteToFile(filePath, "invalid", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown format")
	})

	// Test with addSourceURL enabled
	t.Run("with source URL", func(t *testing.T) {
		formats := []string{"html", "md", "obsidian-md", "txt"}

		for _, format := range formats {
			t.Run(format, func(t *testing.T) {
				filePath := filepath.Join(tempDir, fmt.Sprintf("test-with-source.%s", format))
				err := post.WriteToFile(filePath, format, true)
				require.NoError(t, err)

				// Read file content
				content, err := os.ReadFile(filePath)
				require.NoError(t, err)
				contentStr := string(content)

				// Check that source URL is included
				assert.Contains(t, contentStr, post.CanonicalUrl)
				assert.Contains(t, contentStr, "original content")

				// Check format-specific source URL formatting
				if format == "html" {
					assert.Contains(t, contentStr, "<a href=")
					assert.Contains(t, contentStr, "style=\"margin-top: 2em")
				} else {
					assert.Contains(t, contentStr, fmt.Sprintf("original content: %s", post.CanonicalUrl))
				}
			})
		}
	})

	// Test with addSourceURL but no canonical URL
	t.Run("with source URL but no canonical URL", func(t *testing.T) {
		postWithoutURL := createSamplePost()
		postWithoutURL.CanonicalUrl = ""

		filePath := filepath.Join(tempDir, "test-no-url.html")
		err := postWithoutURL.WriteToFile(filePath, "html", true)
		require.NoError(t, err)

		// Read file content
		content, err := os.ReadFile(filePath)
		require.NoError(t, err)
		contentStr := string(content)

		// Should not contain source URL line
		assert.NotContains(t, contentStr, "original content")
	})
}

func TestObsidianMarkdownTransforms(t *testing.T) {
	t.Run("local links to wikilinks", func(t *testing.T) {
		input := "See [OtherNote](OtherNote.md) and [Label](OtherNote.md)."
		expected := "See [[OtherNote]] and [[OtherNote|Label]]."
		assert.Equal(t, expected, transformObsidianMarkdown(input))
	})

	t.Run("anchored links", func(t *testing.T) {
		input := "See [Label](OtherNote.md#section-title)."
		expected := "See [[OtherNote#section-title|Label]]."
		assert.Equal(t, expected, transformObsidianMarkdown(input))
	})

	t.Run("external links unchanged", func(t *testing.T) {
		input := "See [External](https://example.com/OtherNote.md)."
		expected := "See [External](https://example.com/OtherNote.md)."
		assert.Equal(t, expected, transformObsidianMarkdown(input))
	})

	t.Run("local images to embeds", func(t *testing.T) {
		input := "Image: ![alt](./assets/img.png) and ![ext](https://example.com/img.png)."
		expected := "Image: ![[assets/img.png]] and ![ext](https://example.com/img.png)."
		assert.Equal(t, expected, transformObsidianMarkdown(input))
	})

	t.Run("frontmatter preserved", func(t *testing.T) {
		input := "---\r\ntitle: Test\r\nlink: OtherNote.md\r\n---\r\nSee [Note](OtherNote.md)\r\n"
		expected := "---\ntitle: Test\nlink: OtherNote.md\n---\nSee [[OtherNote|Note]]\n"
		output := transformObsidianMarkdown(input)
		assert.Equal(t, expected, output)
		assert.NotContains(t, output, "\r")
	})

	t.Run("code fences preserved", func(t *testing.T) {
		input := "Before [Note](OtherNote.md)\n```\n[Note](OtherNote.md)\n```\nAfter [Note](OtherNote.md)"
		expected := "Before [[OtherNote|Note]]\n```\n[Note](OtherNote.md)\n```\nAfter [[OtherNote|Note]]"
		assert.Equal(t, expected, transformObsidianMarkdown(input))
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

	// Create sitemap XML with different dates
	sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`
	// Create ordered list of posts to ensure deterministic date assignment
	dates := []string{"2023-01-01", "2023-01-02", "2023-01-03", "2023-01-04", "2023-01-05"}
	for i := 1; i <= 5; i++ {
		post := posts[fmt.Sprintf("/p/test-post-%d", i)]
		sitemapXML += fmt.Sprintf(`  <url>
    <loc>https://example.substack.com/p/%s</loc>
    <lastmod>%s</lastmod>
  </url>
`, post.Slug, dates[i-1])
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
		// Filter for posts after 2023-01-02 (should get 3 posts: 2023-01-03, 2023-01-04, 2023-01-05)
		dateFilter := func(date string) bool {
			return date > "2023-01-02"
		}

		urls, err := extractor.GetAllPostsURLs(ctx, server.URL, dateFilter)
		require.NoError(t, err)

		// Should get 3 posts (dates 2023-01-03, 2023-01-04, 2023-01-05)
		assert.Len(t, urls, 3)

		// Verify the filtered URLs are correct
		for _, url := range urls {
			// Should contain test-post-3, test-post-4, or test-post-5
			assert.True(t, strings.Contains(url, "test-post-3") ||
				strings.Contains(url, "test-post-4") ||
				strings.Contains(url, "test-post-5"))
		}
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

func TestExtractorGetAllPostsEntries(t *testing.T) {
	server, posts := createSubstackTestServer()
	defer server.Close()

	extractor := NewExtractor(nil)
	ctx := context.Background()

	entries, err := extractor.GetAllPostsEntries(ctx, server.URL, nil)
	require.NoError(t, err)
	assert.Equal(t, len(posts), len(entries))

	entryMap := make(map[string]string)
	for _, entry := range entries {
		entryMap[entry.URL] = entry.LastMod
	}

	expectedURL := "https://example.substack.com/p/test-post-1"
	expectedLastmod := "2023-01-01"
	if got, ok := entryMap[expectedURL]; ok {
		assert.Equal(t, expectedLastmod, got)
	} else {
		t.Fatalf("missing entry for %s", expectedURL)
	}
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
		requestCount.Add(1) // Increment counter
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

		case strings.Contains(path, "timeout-post"):
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

// Test enhanced post extraction features (subtitle and cover image)
func TestEnhancedPostExtraction(t *testing.T) {
	t.Run("SubtitleExtraction", func(t *testing.T) {
		post := createSamplePost()
		post.Subtitle = "" // Clear subtitle from JSON to test HTML extraction

		// Create mock HTML with subtitle element
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <meta property="og:image" content="https://example.com/og-image.jpg">
</head>
<body>
  <div class="subtitle">   This is the subtitle from HTML   </div>
  <div class="post">Some content</div>
  <script>
    window._preloads = JSON.parse("%s")
  </script>
</body>
</html>
`, post.Title, escapeJSONForJS(post))

		// Create test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
		}))
		defer server.Close()

		extractor := NewExtractor(nil)
		ctx := context.Background()

		extractedPost, err := extractor.ExtractPost(ctx, server.URL)
		require.NoError(t, err)

		// Verify subtitle was extracted and trimmed
		assert.Equal(t, "This is the subtitle from HTML", extractedPost.Subtitle)
	})

	t.Run("CoverImageFromOGTag", func(t *testing.T) {
		post := createSamplePost()
		post.CoverImage = "" // Clear cover image from JSON to test og:image extraction

		// Create mock HTML with og:image meta tag
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <meta property="og:image" content="https://example.com/og-cover.jpg">
</head>
<body>
  <div class="post">Some content</div>
  <script>
    window._preloads = JSON.parse("%s")
  </script>
</body>
</html>
`, post.Title, escapeJSONForJS(post))

		// Create test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
		}))
		defer server.Close()

		extractor := NewExtractor(nil)
		ctx := context.Background()

		extractedPost, err := extractor.ExtractPost(ctx, server.URL)
		require.NoError(t, err)

		// Verify cover image was extracted from og:image
		assert.Equal(t, "https://example.com/og-cover.jpg", extractedPost.CoverImage)
	})

	t.Run("ExistingCoverImagePreserved", func(t *testing.T) {
		post := createSamplePost()
		post.CoverImage = "https://existing.com/image.jpg"

		// Create mock HTML with og:image meta tag (should be ignored)
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <title>%s</title>
  <meta property="og:image" content="https://example.com/og-cover.jpg">
</head>
<body>
  <div class="post">Some content</div>
  <script>
    window._preloads = JSON.parse("%s")
  </script>
</body>
</html>
`, post.Title, escapeJSONForJS(post))

		// Create test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
		}))
		defer server.Close()

		extractor := NewExtractor(nil)
		ctx := context.Background()

		extractedPost, err := extractor.ExtractPost(ctx, server.URL)
		require.NoError(t, err)

		// Verify existing cover image was preserved (not overwritten by og:image)
		assert.Equal(t, "https://existing.com/image.jpg", extractedPost.CoverImage)
	})

	t.Run("NoSubtitleOrCoverImage", func(t *testing.T) {
		post := createSamplePost()
		post.Subtitle = ""
		post.CoverImage = ""

		// Create mock HTML without subtitle or og:image
		html := fmt.Sprintf(`
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
`, post.Title, escapeJSONForJS(post))

		// Create test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
		}))
		defer server.Close()

		extractor := NewExtractor(nil)
		ctx := context.Background()

		extractedPost, err := extractor.ExtractPost(ctx, server.URL)
		require.NoError(t, err)

		// Verify empty subtitle and cover image remain empty
		assert.Empty(t, extractedPost.Subtitle)
		assert.Empty(t, extractedPost.CoverImage)
	})
}

// Helper function to escape JSON for embedding in JavaScript
func escapeJSONForJS(post Post) string {
	wrapper := PostWrapper{Post: post}
	jsonBytes, _ := json.Marshal(wrapper)
	return strings.ReplaceAll(string(jsonBytes), `"`, `\"`)
}

// Test Archive functionality
func TestArchive(t *testing.T) {
	t.Run("NewArchive", func(t *testing.T) {
		archive := NewArchive()
		assert.NotNil(t, archive)
		assert.NotNil(t, archive.Entries)
		assert.Len(t, archive.Entries, 0)
	})

	t.Run("AddEntry", func(t *testing.T) {
		archive := NewArchive()
		post1 := createSamplePost()
		post1.PostDate = "2023-01-01T00:00:00Z"
		post1.Title = "First Post"

		post2 := createSamplePost()
		post2.PostDate = "2023-01-02T00:00:00Z"
		post2.Title = "Second Post"

		post3 := createSamplePost()
		post3.PostDate = "2023-01-03T00:00:00Z"
		post3.Title = "Third Post"

		downloadTime := time.Now()

		// Add entries in random order
		archive.AddEntry(post2, "post2.html", downloadTime)
		archive.AddEntry(post1, "post1.html", downloadTime)
		archive.AddEntry(post3, "post3.html", downloadTime)

		// Verify entries were added and sorted by date (newest first)
		assert.Len(t, archive.Entries, 3)
		assert.Equal(t, "Third Post", archive.Entries[0].Post.Title)  // 2023-01-03 (newest)
		assert.Equal(t, "Second Post", archive.Entries[1].Post.Title) // 2023-01-02
		assert.Equal(t, "First Post", archive.Entries[2].Post.Title)  // 2023-01-01 (oldest)
	})

	t.Run("SortingWithInvalidDates", func(t *testing.T) {
		archive := NewArchive()

		post1 := createSamplePost()
		post1.PostDate = "invalid-date"
		post1.Title = "A Post"

		post2 := createSamplePost()
		post2.PostDate = "also-invalid"
		post2.Title = "B Post"

		downloadTime := time.Now()

		archive.AddEntry(post2, "post2.html", downloadTime)
		archive.AddEntry(post1, "post1.html", downloadTime)

		// Should sort by title when dates are invalid
		assert.Len(t, archive.Entries, 2)
		assert.Equal(t, "A Post", archive.Entries[0].Post.Title) // Alphabetical order
		assert.Equal(t, "B Post", archive.Entries[1].Post.Title)
	})

	t.Run("ArchiveEntryFields", func(t *testing.T) {
		archive := NewArchive()
		post := createSamplePost()
		filePath := "/path/to/post.html"
		downloadTime := time.Now()

		archive.AddEntry(post, filePath, downloadTime)

		entry := archive.Entries[0]
		assert.Equal(t, post, entry.Post)
		assert.Equal(t, filePath, entry.FilePath)
		assert.Equal(t, downloadTime, entry.DownloadTime)
	})
}

// Test Archive page generation
func TestArchivePageGeneration(t *testing.T) {
	// Helper function to create a test archive
	setupTestArchive := func() (*Archive, string) {
		tempDir, err := os.MkdirTemp("", "archive_test")
		require.NoError(t, err)

		archive := NewArchive()

		// Create sample posts with different dates and metadata
		post1 := createSamplePost()
		post1.PostDate = "2023-01-01T10:30:00Z"
		post1.Title = "First Post"
		post1.Subtitle = "A great first post"
		post1.CoverImage = "https://example.com/cover1.jpg"

		post2 := createSamplePost()
		post2.PostDate = "2023-01-02T15:45:00Z"
		post2.Title = "Second Post"
		post2.Subtitle = "" // Empty subtitle, should fall back to description
		post2.Description = "This is the description"
		post2.CoverImage = ""

		post3 := createSamplePost()
		post3.PostDate = "2023-01-03T08:15:00Z"
		post3.Title = "Third Post"
		post3.Subtitle = ""
		post3.Description = ""
		post3.CoverImage = "https://example.com/cover3.jpg"

		downloadTime, _ := time.Parse(time.RFC3339, "2023-01-10T12:00:00Z")

		archive.AddEntry(post1, filepath.Join(tempDir, "post1.html"), downloadTime)
		archive.AddEntry(post2, filepath.Join(tempDir, "post2.html"), downloadTime.Add(time.Hour))
		archive.AddEntry(post3, filepath.Join(tempDir, "post3.html"), downloadTime.Add(2*time.Hour))

		return archive, tempDir
	}

	t.Run("GenerateHTML", func(t *testing.T) {
		archive, tempDir := setupTestArchive()
		defer os.RemoveAll(tempDir)

		err := archive.GenerateHTML(tempDir)
		require.NoError(t, err)

		// Check file was created
		indexPath := filepath.Join(tempDir, "index.html")
		assert.FileExists(t, indexPath)

		// Read and verify content
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)
		htmlContent := string(content)

		// Verify HTML structure
		assert.Contains(t, htmlContent, "<!DOCTYPE html>")
		assert.Contains(t, htmlContent, "<title>Substack Archive</title>")
		assert.Contains(t, htmlContent, "<h1>Substack Archive</h1>")

		// Verify posts are included in correct order (newest first)
		assert.Contains(t, htmlContent, "Third Post") // Should appear first (newest)
		assert.Contains(t, htmlContent, "Second Post")
		assert.Contains(t, htmlContent, "First Post")

		// Verify relative paths
		assert.Contains(t, htmlContent, "post1.html")
		assert.Contains(t, htmlContent, "post2.html")
		assert.Contains(t, htmlContent, "post3.html")

		// Verify cover images and descriptions
		assert.Contains(t, htmlContent, "https://example.com/cover1.jpg")
		assert.Contains(t, htmlContent, "https://example.com/cover3.jpg")
		assert.Contains(t, htmlContent, "A great first post")      // Subtitle
		assert.Contains(t, htmlContent, "This is the description") // Fallback description

		// Verify dates are formatted
		assert.Contains(t, htmlContent, "January 1, 2023")        // Formatted publication date
		assert.Contains(t, htmlContent, "January 10, 2023 12:00") // Formatted download date
	})

	t.Run("GenerateMarkdown", func(t *testing.T) {
		archive, tempDir := setupTestArchive()
		defer os.RemoveAll(tempDir)

		err := archive.GenerateMarkdown(tempDir)
		require.NoError(t, err)

		// Check file was created
		indexPath := filepath.Join(tempDir, "index.md")
		assert.FileExists(t, indexPath)

		// Read and verify content
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)
		mdContent := string(content)

		// Verify markdown structure
		assert.Contains(t, mdContent, "# Substack Archive\n\n")
		assert.Contains(t, mdContent, "## [Third Post](post3.html)") // Newest first
		assert.Contains(t, mdContent, "## [Second Post](post2.html)")
		assert.Contains(t, mdContent, "## [First Post](post1.html)")

		// Verify metadata format
		assert.Contains(t, mdContent, "**Published:** January 1, 2023")
		assert.Contains(t, mdContent, "**Downloaded:** January 10, 2023 12:00")

		// Verify cover image markdown syntax
		assert.Contains(t, mdContent, "![Cover Image](https://example.com/cover1.jpg)")
		assert.Contains(t, mdContent, "![Cover Image](https://example.com/cover3.jpg)")

		// Verify descriptions in italic
		assert.Contains(t, mdContent, "*A great first post*")
		assert.Contains(t, mdContent, "*This is the description*")

		// Verify separators
		assert.Contains(t, mdContent, "---")
	})

	t.Run("GenerateText", func(t *testing.T) {
		archive, tempDir := setupTestArchive()
		defer os.RemoveAll(tempDir)

		err := archive.GenerateText(tempDir)
		require.NoError(t, err)

		// Check file was created
		indexPath := filepath.Join(tempDir, "index.txt")
		assert.FileExists(t, indexPath)

		// Read and verify content
		content, err := os.ReadFile(indexPath)
		require.NoError(t, err)
		txtContent := string(content)

		// Verify text structure
		assert.Contains(t, txtContent, "SUBSTACK ARCHIVE\n================")

		// Verify post entries (newest first)
		assert.Contains(t, txtContent, "Title: Third Post")
		assert.Contains(t, txtContent, "Title: Second Post")
		assert.Contains(t, txtContent, "Title: First Post")

		// Verify file paths
		assert.Contains(t, txtContent, "File: post1.html")
		assert.Contains(t, txtContent, "File: post2.html")
		assert.Contains(t, txtContent, "File: post3.html")

		// Verify formatted dates
		assert.Contains(t, txtContent, "Published: January 1, 2023")
		assert.Contains(t, txtContent, "Downloaded: January 10, 2023 12:00")

		// Verify descriptions
		assert.Contains(t, txtContent, "Description: A great first post")
		assert.Contains(t, txtContent, "Description: This is the description")

		// Verify separators
		assert.Contains(t, txtContent, strings.Repeat("-", 50))
	})

	t.Run("EmptyArchive", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "empty_archive_test")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		archive := NewArchive()

		// Test each format with empty archive
		err = archive.GenerateHTML(tempDir)
		require.NoError(t, err)

		err = archive.GenerateMarkdown(tempDir)
		require.NoError(t, err)

		err = archive.GenerateText(tempDir)
		require.NoError(t, err)

		// Verify files exist and contain basic headers
		htmlContent, _ := os.ReadFile(filepath.Join(tempDir, "index.html"))
		assert.Contains(t, string(htmlContent), "Substack Archive")

		mdContent, _ := os.ReadFile(filepath.Join(tempDir, "index.md"))
		assert.Contains(t, string(mdContent), "# Substack Archive")

		txtContent, _ := os.ReadFile(filepath.Join(tempDir, "index.txt"))
		assert.Contains(t, string(txtContent), "SUBSTACK ARCHIVE")
	})

	t.Run("FileSystemError", func(t *testing.T) {
		archive := NewArchive()
		post := createSamplePost()
		archive.AddEntry(post, "test.html", time.Now())

		// Try to write to non-existent directory with restricted permissions
		invalidDir := "/non/existent/directory"

		err := archive.GenerateHTML(invalidDir)
		assert.Error(t, err)

		err = archive.GenerateMarkdown(invalidDir)
		assert.Error(t, err)

		err = archive.GenerateText(invalidDir)
		assert.Error(t, err)
	})
}

func TestArchiveHTMLNotionBadges(t *testing.T) {
	tempDir := t.TempDir()

	postWithLinks := createSamplePost()
	postWithLinks.PostDate = "2023-01-01T10:30:00Z"
	postWithLinks.Title = "Notion Post"

	postWithoutLinks := createSamplePost()
	postWithoutLinks.PostDate = "2023-01-02T10:30:00Z"
	postWithoutLinks.Title = "Plain Post"

	withLinksPath := filepath.Join(tempDir, "post-with-links.html")
	withoutLinksPath := filepath.Join(tempDir, "post-without-links.html")

	withLinksContent := `<html><body>
<a href="https://www.notion.so/Workspace/Page-123?utm_source=x">Notion</a>
<a href="https://www.notion.so/Workspace/Page-123">Notion</a>
<a href="https://example.notion.site/Page-456/">Notion</a>
</body></html>`
	withoutLinksContent := `<html><body><p>No links here.</p></body></html>`

	require.NoError(t, os.WriteFile(withLinksPath, []byte(withLinksContent), 0644))
	require.NoError(t, os.WriteFile(withoutLinksPath, []byte(withoutLinksContent), 0644))

	archive := NewArchive()
	archive.AddEntry(postWithoutLinks, withoutLinksPath, time.Now())
	archive.AddEntry(postWithLinks, withLinksPath, time.Now())

	require.NoError(t, archive.GenerateHTML(tempDir))

	indexPath := filepath.Join(tempDir, "index.html")
	htmlContent, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	assert.Contains(t, string(htmlContent), "Notion 2")
	assert.Contains(t, string(htmlContent), "notion-links.html")
	assert.NotContains(t, string(htmlContent), "Notion 0")
}

func TestArchiveHTMLDomainIndex(t *testing.T) {
	tempDir := t.TempDir()

	postWithLinks := createSamplePost()
	postWithLinks.PostDate = "2023-01-01T10:30:00Z"
	postWithLinks.Title = "Multi Link Post"

	postWithDocs := createSamplePost()
	postWithDocs.PostDate = "2023-01-02T10:30:00Z"
	postWithDocs.Title = "Docs Post"

	withLinksPath := filepath.Join(tempDir, "post-with-links.html")
	withDocsPath := filepath.Join(tempDir, "post-with-docs.html")

	withLinksContent := `<html><body>
<a href="https://github.com/org/repo">GitHub</a>
<a href="https://www.notion.so/Workspace/Page-123">Notion</a>
</body></html>`
	withDocsContent := `<html><body>
<a href="https://docs.google.com/spreadsheets/d/abc">Docs</a>
</body></html>`

	require.NoError(t, os.WriteFile(withLinksPath, []byte(withLinksContent), 0644))
	require.NoError(t, os.WriteFile(withDocsPath, []byte(withDocsContent), 0644))

	archive := NewArchive()
	archive.AddEntry(postWithLinks, withLinksPath, time.Now())
	archive.AddEntry(postWithDocs, withDocsPath, time.Now())

	require.NoError(t, archive.GenerateHTML(tempDir))

	indexPath := filepath.Join(tempDir, "index.html")
	htmlContent, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	content := string(htmlContent)
	assert.Contains(t, content, `data-domain-filter="github.com"`)
	assert.Contains(t, content, `data-domain-filter="docs.google.com"`)
	assert.Contains(t, content, `data-domains="github.com,notion.so"`)
	assert.Contains(t, content, "All links")
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
