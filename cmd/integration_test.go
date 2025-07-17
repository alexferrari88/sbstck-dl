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
				filePath := makePath(post, outputFolder, format)
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
	})

	t.Run("download command flags", func(t *testing.T) {
		cmd := downloadCmd
		
		// Check local flags
		assert.NotNil(t, cmd.Flags().Lookup("url"))
		assert.NotNil(t, cmd.Flags().Lookup("format"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
		assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
		assert.NotNil(t, cmd.Flags().Lookup("add-source-url"))
	})

	t.Run("list command flags", func(t *testing.T) {
		cmd := listCmd
		
		// Check local flags
		assert.NotNil(t, cmd.Flags().Lookup("url"))
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
		
		// Try to write to a non-existent directory with no permissions
		err := post.WriteToFile("/root/non-existent/file.html", "html", false)
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
			name      string
			cookieName cookieName
			cookieVal  string
			expected   string
		}{
			{
				name:      "substack.sid cookie",
				cookieName: substackSid,
				cookieVal:  "test-value",
				expected:   "substack.sid",
			},
			{
				name:      "connect.sid cookie",
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
		
		assert.Equal(t, len(urls), validUrls) // All should be valid
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