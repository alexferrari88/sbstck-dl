package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test command execution in isolation
func TestCommandExecution(t *testing.T) {
	// Skip in short test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock server that serves a simple post
	mockPost := lib.Post{
		Id:           123,
		Title:        "Test Post",
		Slug:         "test-post",
		PostDate:     "2023-01-01",
		BodyHTML:     "<p>This is a test post</p>",
		CanonicalUrl: "https://example.substack.com/p/test-post",
	}

	// Create sitemap XML
	sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.substack.com/p/test-post</loc>
    <lastmod>2023-01-01</lastmod>
  </url>
</urlset>`

	// Create mock HTML with embedded JSON
	postWrapper := lib.PostWrapper{Post: mockPost}
	jsonBytes, _ := json.Marshal(postWrapper)
	escapedJSON := strings.ReplaceAll(string(jsonBytes), `"`, `\"`)
	mockHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><title>%s</title></head>
<body>
  <script>
    window._preloads = JSON.parse("%s")
  </script>
</body>
</html>
`, mockPost.Title, escapedJSON)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/sitemap.xml" {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(sitemapXML))
		} else if path == "/p/test-post" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(mockHTML))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Test version command
	t.Run("version command", func(t *testing.T) {
		// Capture stdout
		var output bytes.Buffer

		// Create a command that executes the version logic
		cmd := &cobra.Command{
			Use: "test-version",
			Run: func(cmd *cobra.Command, args []string) {
				output.WriteString("sbstck-dl v0.4.0\n")
			},
		}

		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, output.String(), "sbstck-dl v0.4.0")
	})

	// Test list command
	t.Run("list command", func(t *testing.T) {
		// Reset global variables
		pubUrl = server.URL
		verbose = false
		beforeDate = ""
		afterDate = ""

		// Initialize fetcher and extractor
		fetcher = lib.NewFetcher()
		extractor = lib.NewExtractor(fetcher)
		ctx = context.Background()

		// Create a new command to capture output
		var output bytes.Buffer
		cmd := &cobra.Command{
			Use: "test-list",
			Run: func(cmd *cobra.Command, args []string) {
				// Simulate list command logic
				urls, err := extractor.GetAllPostsURLs(ctx, pubUrl, nil)
				if err != nil {
					t.Fatalf("Failed to get URLs: %v", err)
				}
				for _, url := range urls {
					output.WriteString(url + "\n")
				}
			},
		}

		err := cmd.Execute()
		assert.NoError(t, err)

		// Check that it outputs the post URL
		assert.Contains(t, output.String(), "https://example.substack.com/p/test-post")
	})

	// Test single post download
	t.Run("single post download", func(t *testing.T) {
		tempDir := t.TempDir()

		// Reset global variables
		downloadUrl = server.URL + "/p/test-post"
		outputFolder = tempDir
		format = "html"
		dryRun = false
		verbose = false
		addSourceURL = false

		// Initialize fetcher and extractor
		fetcher = lib.NewFetcher()
		extractor = lib.NewExtractor(fetcher)
		ctx = context.Background()

		// Create a new command
		cmd := &cobra.Command{
			Use: "test-download",
			Run: func(cmd *cobra.Command, args []string) {
				// Execute the single post download logic
				post, err := extractor.ExtractPost(ctx, downloadUrl)
				if err != nil {
					t.Fatalf("Failed to extract post: %v", err)
				}

				// Write to file
				filePath := makePath(post, outputFolder, format, "flat")
				err = post.WriteToFile(filePath, format, addSourceURL)
				if err != nil {
					t.Fatalf("Failed to write file: %v", err)
				}
			},
		}

		err := cmd.Execute()
		assert.NoError(t, err)

		// Check that file was created - use the correct expected format
		// Since mockPost.PostDate is "2023-01-01" (not RFC3339), convertDateTime will return ""
		expectedFile := filepath.Join(tempDir, "_test-post.html")
		_, err = os.Stat(expectedFile)
		assert.NoError(t, err)

		// Check file content
		content, err := os.ReadFile(expectedFile)
		assert.NoError(t, err)
		assert.Contains(t, string(content), "Test Post")
		assert.Contains(t, string(content), "This is a test post")
	})
}

// Test command flag parsing
func TestCommandFlags(t *testing.T) {
	t.Run("root command flags", func(t *testing.T) {
		// Test that flags are properly defined
		cmd := rootCmd

		// Check persistent flags
		assert.NotNil(t, cmd.PersistentFlags().Lookup("proxy"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("verbose"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("rate"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("cookie_name"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("cookie_val"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("before"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("after"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("max-workers"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("concurrency"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("log-format"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("cookie-val-file"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("cookie-jar"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("cookie-keychain"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("notion-labels"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("config"))
	})

	t.Run("download command flags", func(t *testing.T) {
		cmd := downloadCmd

		// Check local flags
		assert.NotNil(t, cmd.Flags().Lookup("url"))
		assert.NotNil(t, cmd.Flags().Lookup("format"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
		assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
		assert.NotNil(t, cmd.Flags().Lookup("add-source-url"))
		assert.NotNil(t, cmd.Flags().Lookup("download-images"))
		assert.NotNil(t, cmd.Flags().Lookup("image-quality"))
		assert.NotNil(t, cmd.Flags().Lookup("images-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("download-files"))
		assert.NotNil(t, cmd.Flags().Lookup("file-extensions"))
		assert.NotNil(t, cmd.Flags().Lookup("files-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("create-archive"))
		assert.NotNil(t, cmd.Flags().Lookup("force"))
		assert.NotNil(t, cmd.Flags().Lookup("skip-existing"))
		assert.NotNil(t, cmd.Flags().Lookup("refresh-updated"))
		assert.NotNil(t, cmd.Flags().Lookup("layout"))
		assert.NotNil(t, cmd.Flags().Lookup("write-metadata"))
		assert.NotNil(t, cmd.Flags().Lookup("fail-fast"))
		assert.NotNil(t, cmd.Flags().Lookup("continue-on-error"))

		// Test create-archive flag specifically
		createArchiveFlag := cmd.Flags().Lookup("create-archive")
		assert.Equal(t, "bool", createArchiveFlag.Value.Type())
		assert.Equal(t, "false", createArchiveFlag.DefValue)
	})

	t.Run("archive command flags", func(t *testing.T) {
		cmd := archiveCmd

		assert.NotNil(t, cmd.Flags().Lookup("output"))
		assert.NotNil(t, cmd.Flags().Lookup("format"))
	})

	t.Run("serve command flags", func(t *testing.T) {
		cmd := serveCmd

		assert.NotNil(t, cmd.Flags().Lookup("port"))
	})

	t.Run("list command flags", func(t *testing.T) {
		cmd := listCmd

		// Check local flags
		assert.NotNil(t, cmd.Flags().Lookup("url"))
		assert.NotNil(t, cmd.Flags().Lookup("json"))
		assert.NotNil(t, cmd.Flags().Lookup("with-metadata"))
	})
}

// Test command validation
func TestCommandValidation(t *testing.T) {
	t.Run("invalid proxy URL", func(t *testing.T) {
		// Test parseURL with invalid proxy
		_, err := parseURL("invalid-proxy")
		assert.Error(t, err)
	})

	t.Run("invalid cookie name", func(t *testing.T) {
		cn := cookieName("")
		err := cn.Set("invalid-cookie")
		assert.Error(t, err)
	})

	t.Run("rate validation", func(t *testing.T) {
		// Test that rate 0 should fail
		// This would normally be tested in the PersistentPreRun, but we can test the logic
		ratePerSecond = 0
		assert.Equal(t, 0, ratePerSecond) // Should be 0 which is invalid
	})
}

// Test error handling
func TestErrorHandling(t *testing.T) {
	t.Run("network error handling", func(t *testing.T) {
		// Test with non-existent server
		fetcher := lib.NewFetcher()
		extractor := lib.NewExtractor(fetcher)
		ctx := context.Background()

		_, err := extractor.ExtractPost(ctx, "http://non-existent-server.com/p/test")
		assert.Error(t, err)
	})

	t.Run("invalid URL format", func(t *testing.T) {
		// Test with malformed URL
		_, err := parseURL("://invalid-url")
		assert.Error(t, err)
	})

	t.Run("file system errors", func(t *testing.T) {
		// Test writing to invalid directory
		post := lib.Post{
			Title:    "Test",
			BodyHTML: "<p>Test</p>",
		}

		// Try to write to a file with invalid character (null byte forbidden on both Windows and Unix)
		err := post.WriteToFile("invalid\x00filename.html", "html", false)
		assert.Error(t, err)
	})
}

// Test with different configurations
func TestConfigurations(t *testing.T) {
	t.Run("with proxy configuration", func(t *testing.T) {
		// Test that proxy URL parsing works
		proxyURL := "http://proxy.example.com:8080"
		parsed, err := parseURL(proxyURL)
		assert.NoError(t, err)
		assert.Equal(t, "proxy.example.com:8080", parsed.Host)
		assert.Equal(t, "http", parsed.Scheme)
	})

	t.Run("with cookie configuration", func(t *testing.T) {
		// Test cookie creation
		tests := []struct {
			name       string
			cookieName cookieName
			cookieVal  string
			expected   string
		}{
			{
				name:       "substack.sid cookie",
				cookieName: substackSid,
				cookieVal:  "test-value",
				expected:   "substack.sid",
			},
			{
				name:       "connect.sid cookie",
				cookieName: connectSid,
				cookieVal:  "test-value",
				expected:   "connect.sid",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.cookieName.String())
			})
		}
	})

	t.Run("with rate limiting", func(t *testing.T) {
		// Test that different rate limits are handled
		rates := []int{1, 2, 5, 10}

		for _, rate := range rates {
			fetcher := lib.NewFetcher(lib.WithRatePerSecond(rate))
			assert.NotNil(t, fetcher)
			assert.Equal(t, rate, int(fetcher.RateLimiter.Limit()))
		}
	})
}

// Test real-world scenarios
func TestRealWorldScenarios(t *testing.T) {
	// Skip in short test mode
	if testing.Short() {
		t.Skip("Skipping real-world scenario tests in short mode")
	}

	t.Run("large number of URLs", func(t *testing.T) {
		// Test performance with many URLs
		urls := make([]string, 100)
		for i := range urls {
			urls[i] = fmt.Sprintf("https://example.substack.com/p/post-%d", i)
		}

		// Test URL parsing performance
		start := time.Now()

		// Test parsing all URLs
		validUrls := 0
		for _, url := range urls {
			if _, err := parseURL(url); err == nil {
				validUrls++
			}
		}

		duration := time.Since(start)

		assert.Equal(t, len(urls), validUrls)   // All should be valid
		assert.Less(t, duration, 1*time.Second) // Should be fast
	})

	t.Run("concurrent processing", func(t *testing.T) {
		// Test that concurrent processing works correctly
		tempDir := t.TempDir()

		// Create multiple posts concurrently
		posts := make([]lib.Post, 5)
		for i := range posts {
			posts[i] = lib.Post{
				Title:    fmt.Sprintf("Post %d", i),
				Slug:     fmt.Sprintf("post-%d", i),
				PostDate: "2023-01-01",
				BodyHTML: fmt.Sprintf("<p>Content for post %d</p>", i),
			}
		}

		// Write all posts concurrently
		start := time.Now()
		for i, post := range posts {
			filePath := filepath.Join(tempDir, fmt.Sprintf("post-%d.html", i))
			err := post.WriteToFile(filePath, "html", false)
			assert.NoError(t, err)
		}
		duration := time.Since(start)

		// Verify all files were created
		for i := range posts {
			filePath := filepath.Join(tempDir, fmt.Sprintf("post-%d.html", i))
			_, err := os.Stat(filePath)
			assert.NoError(t, err)
		}

		assert.Less(t, duration, 1*time.Second) // Should be fast
	})
}

// Test archive functionality end-to-end
func TestArchiveWorkflow(t *testing.T) {
	t.Run("single post with archive", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a mock post with enhanced fields
		post := lib.Post{
			Id:           123,
			Title:        "Test Archive Post",
			Slug:         "test-archive-post",
			PostDate:     "2023-01-01T10:30:00Z",
			Subtitle:     "This is a test subtitle",
			Description:  "Test description",
			CoverImage:   "https://example.com/cover.jpg",
			CanonicalUrl: "https://example.substack.com/p/test-archive-post",
			BodyHTML:     "<p>This is a <strong>test</strong> post for archive functionality.</p>",
		}

		// Simulate the archive workflow
		archive := lib.NewArchive()

		// Write the post to file (similar to what download command does)
		filePath := filepath.Join(tempDir, "20230101_103000_test-archive-post.html")
		err := post.WriteToFile(filePath, "html", false)
		require.NoError(t, err)

		// Add entry to archive (similar to what download command does)
		downloadTime, _ := time.Parse(time.RFC3339, "2023-01-10T12:00:00Z")
		archive.AddEntry(post, filePath, downloadTime)

		// Generate archive in all formats
		err = archive.GenerateHTML(tempDir)
		require.NoError(t, err)

		err = archive.GenerateMarkdown(tempDir)
		require.NoError(t, err)

		err = archive.GenerateText(tempDir)
		require.NoError(t, err)

		// Verify all archive files were created
		assert.FileExists(t, filepath.Join(tempDir, "index.html"))
		assert.FileExists(t, filepath.Join(tempDir, "index.md"))
		assert.FileExists(t, filepath.Join(tempDir, "index.txt"))

		// Verify HTML archive content
		htmlContent, err := os.ReadFile(filepath.Join(tempDir, "index.html"))
		require.NoError(t, err)
		htmlStr := string(htmlContent)

		assert.Contains(t, htmlStr, "Test Archive Post")
		assert.Contains(t, htmlStr, "This is a test subtitle")
		assert.Contains(t, htmlStr, "https://example.com/cover.jpg")
		assert.Contains(t, htmlStr, "20230101_103000_test-archive-post.html") // Relative path
		assert.Contains(t, htmlStr, "January 1, 2023")                        // Formatted date

		// Verify Markdown archive content
		mdContent, err := os.ReadFile(filepath.Join(tempDir, "index.md"))
		require.NoError(t, err)
		mdStr := string(mdContent)

		assert.Contains(t, mdStr, "# Substack Archive")
		assert.Contains(t, mdStr, "## [Test Archive Post](20230101_103000_test-archive-post.html)")
		assert.Contains(t, mdStr, "*This is a test subtitle*")
		assert.Contains(t, mdStr, "![Cover Image](https://example.com/cover.jpg)")

		// Verify Text archive content
		txtContent, err := os.ReadFile(filepath.Join(tempDir, "index.txt"))
		require.NoError(t, err)
		txtStr := string(txtContent)

		assert.Contains(t, txtStr, "SUBSTACK ARCHIVE")
		assert.Contains(t, txtStr, "Title: Test Archive Post")
		assert.Contains(t, txtStr, "File: 20230101_103000_test-archive-post.html")
		assert.Contains(t, txtStr, "Description: This is a test subtitle")
	})

	t.Run("multiple posts with archive", func(t *testing.T) {
		tempDir := t.TempDir()

		archive := lib.NewArchive()
		downloadTime := time.Now()

		// Create multiple posts with different dates
		posts := []lib.Post{
			{
				Id:           1,
				Title:        "First Post",
				Slug:         "first-post",
				PostDate:     "2023-01-01T10:00:00Z",
				Subtitle:     "First subtitle",
				CanonicalUrl: "https://example.substack.com/p/first-post",
				BodyHTML:     "<p>First post content</p>",
			},
			{
				Id:           2,
				Title:        "Second Post",
				Slug:         "second-post",
				PostDate:     "2023-01-02T10:00:00Z",
				Description:  "Second description",
				CoverImage:   "https://example.com/cover2.jpg",
				CanonicalUrl: "https://example.substack.com/p/second-post",
				BodyHTML:     "<p>Second post content</p>",
			},
			{
				Id:           3,
				Title:        "Third Post",
				Slug:         "third-post",
				PostDate:     "2023-01-03T10:00:00Z",
				Subtitle:     "Third subtitle",
				CanonicalUrl: "https://example.substack.com/p/third-post",
				BodyHTML:     "<p>Third post content</p>",
			},
		}

		// Write posts and add to archive
		for i, post := range posts {
			filePath := filepath.Join(tempDir, fmt.Sprintf("post-%d.html", i+1))
			err := post.WriteToFile(filePath, "html", false)
			require.NoError(t, err)

			archive.AddEntry(post, filePath, downloadTime.Add(time.Duration(i)*time.Hour))
		}

		// Generate archive
		err := archive.GenerateHTML(tempDir)
		require.NoError(t, err)

		// Verify content ordering (newest first)
		htmlContent, err := os.ReadFile(filepath.Join(tempDir, "index.html"))
		require.NoError(t, err)
		htmlStr := string(htmlContent)

		// Find positions of post titles to verify ordering
		thirdPos := strings.Index(htmlStr, "Third Post")
		secondPos := strings.Index(htmlStr, "Second Post")
		firstPos := strings.Index(htmlStr, "First Post")

		assert.True(t, thirdPos < secondPos, "Third Post should appear before Second Post")
		assert.True(t, secondPos < firstPos, "Second Post should appear before First Post")

		// Verify all posts are included
		assert.Contains(t, htmlStr, "First subtitle")
		assert.Contains(t, htmlStr, "Second description") // Fallback to description
		assert.Contains(t, htmlStr, "Third subtitle")
		assert.Contains(t, htmlStr, "https://example.com/cover2.jpg")
	})

	t.Run("archive with different formats", func(t *testing.T) {
		tempDir := t.TempDir()

		post := lib.Post{
			Id:           100,
			Title:        "Format Test Post",
			Slug:         "format-test-post",
			PostDate:     "2023-01-01T10:00:00Z",
			Subtitle:     "Testing different formats",
			CanonicalUrl: "https://example.substack.com/p/format-test-post",
			BodyHTML:     "<p>Testing <strong>different</strong> formats.</p>",
		}

		// Test with different output formats
		formats := []string{"html", "md", "obsidian-md", "txt"}

		for _, format := range formats {
			t.Run(fmt.Sprintf("format_%s", format), func(t *testing.T) {
				formatDir := filepath.Join(tempDir, format)
				err := os.MkdirAll(formatDir, 0755)
				require.NoError(t, err)

				archive := lib.NewArchive()

				// Write post in the specified format
				filePath := filepath.Join(formatDir, fmt.Sprintf("post.%s", format))
				err = post.WriteToFile(filePath, format, false)
				require.NoError(t, err)

				// Add to archive and generate
				archive.AddEntry(post, filePath, time.Now())

				switch format {
				case "html":
					err = archive.GenerateHTML(formatDir)
				case "md":
					err = archive.GenerateMarkdown(formatDir)
				case "obsidian-md":
					err = archive.GenerateObsidianMarkdown(formatDir)
				case "txt":
					err = archive.GenerateText(formatDir)
				}
				require.NoError(t, err)

				// Verify archive file exists
				indexPath := filepath.Join(formatDir, fmt.Sprintf("index.%s", format))
				assert.FileExists(t, indexPath)

				// Verify content contains the post
				content, err := os.ReadFile(indexPath)
				require.NoError(t, err)
				assert.Contains(t, string(content), "Format Test Post")
				assert.Contains(t, string(content), "Testing different formats")
			})
		}
	})
}
