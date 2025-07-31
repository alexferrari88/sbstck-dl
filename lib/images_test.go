package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test image data - a simple 1x1 PNG
var testImageData = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// createTestImageServer creates a test server that serves test images
func createTestImageServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		
		switch {
		case strings.Contains(path, "success"):
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			w.Write(testImageData)
		case strings.Contains(path, "not-found"):
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(path, "server-error"):
			w.WriteHeader(http.StatusInternalServerError)
		case strings.Contains(path, "timeout"):
			// Don't respond to simulate timeout - but add a timeout to prevent hanging
			select {
			case <-time.After(5 * time.Second):
				w.WriteHeader(http.StatusRequestTimeout)
			}
		default:
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			w.Write(testImageData)
		}
	}))
}

// createTestHTMLWithImages creates HTML content with various image structures
func createTestHTMLWithImages(baseURL string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><title>Test Post</title></head>
<body>
<h1>Test Post with Images</h1>

<!-- Simple img tag -->
<p>Here's a simple image:</p>
<img src="%s/simple-image.png" alt="Simple image" width="200" height="100">

<!-- Complex Substack-style image with srcset -->
<div class="captioned-image-container">
  <figure>
    <a class="image-link is-viewable-img image2" target="_blank" href="%s/fullsize.jpeg">
      <div class="image2-inset">
        <picture>
          <source type="image/webp" srcset="%s/w_424.webp 424w, %s/w_848.webp 848w, %s/w_1456.webp 1456w">
          <img src="%s/w_1456.jpeg" 
               srcset="%s/w_424.jpeg 424w, %s/w_848.jpeg 848w, %s/w_1456.jpeg 1456w"
               data-attrs='{"src":"%s/original.jpeg","width":1456,"height":819,"type":"image/jpeg","bytes":385174}'
               alt="Complex image" width="1456" height="819">
        </picture>
      </div>
    </a>
  </figure>
</div>

<!-- Image with data-attrs only -->
<img data-attrs='{"src":"%s/data-attrs-only.png","width":800,"height":600}' alt="Data attrs image">

<!-- Non-existent image for error testing -->
<img src="%s/not-found.png" alt="Missing image">

</body>
</html>`, 
		baseURL, baseURL, baseURL, baseURL, baseURL, baseURL, baseURL, baseURL, 
		baseURL, baseURL, baseURL, baseURL)
}

// TestNewImageDownloader tests the creation of ImageDownloader
func TestNewImageDownloader(t *testing.T) {
	t.Run("WithFetcher", func(t *testing.T) {
		fetcher := NewFetcher()
		downloader := NewImageDownloader(fetcher, "/tmp", "images", ImageQualityHigh)
		
		assert.Equal(t, fetcher, downloader.fetcher)
		assert.Equal(t, "/tmp", downloader.outputDir)
		assert.Equal(t, "images", downloader.imagesDir)
		assert.Equal(t, ImageQualityHigh, downloader.imageQuality)
	})
	
	t.Run("WithoutFetcher", func(t *testing.T) {
		downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityMedium)
		
		assert.NotNil(t, downloader.fetcher)
		assert.Equal(t, "/tmp", downloader.outputDir)
		assert.Equal(t, "images", downloader.imagesDir)
		assert.Equal(t, ImageQualityMedium, downloader.imageQuality)
	})
}

// TestGetTargetWidth tests width calculation for different quality levels
func TestGetTargetWidth(t *testing.T) {
	tests := []struct {
		quality ImageQuality
		width   int
	}{
		{ImageQualityHigh, 1456},
		{ImageQualityMedium, 848},
		{ImageQualityLow, 424},
		{ImageQuality("invalid"), 1456}, // should default to high
	}
	
	for _, test := range tests {
		t.Run(string(test.quality), func(t *testing.T) {
			downloader := NewImageDownloader(nil, "/tmp", "images", test.quality)
			width := downloader.getTargetWidth()
			assert.Equal(t, test.width, width)
		})
	}
}

// TestExtractURLFromSrcset tests srcset URL extraction
func TestExtractURLFromSrcset(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	tests := []struct {
		name       string
		srcset     string
		targetWidth int
		expected   string
	}{
		{
			name:        "ExactMatch",
			srcset:      "https://example.com/image-424.jpg 424w, https://example.com/image-848.jpg 848w, https://example.com/image-1456.jpg 1456w",
			targetWidth: 848,
			expected:    "https://example.com/image-848.jpg",
		},
		{
			name:        "ClosestHigher",
			srcset:      "https://example.com/image-424.jpg 424w, https://example.com/image-1200.jpg 1200w, https://example.com/image-1456.jpg 1456w",
			targetWidth: 800,
			expected:    "https://example.com/image-1200.jpg",
		},
		{
			name:        "ClosestLower",
			srcset:      "https://example.com/image-200.jpg 200w, https://example.com/image-400.jpg 400w",
			targetWidth: 800,
			expected:    "https://example.com/image-400.jpg",
		},
		{
			name:        "SingleEntry",
			srcset:      "https://example.com/single-image.jpg 1024w",
			targetWidth: 800,
			expected:    "https://example.com/single-image.jpg",
		},
		{
			name:        "EmptySrcset",
			srcset:      "",
			targetWidth: 800,
			expected:    "",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := downloader.extractURLFromSrcset(test.srcset, test.targetWidth)
			assert.Equal(t, test.expected, result)
		})
	}
}

// TestGenerateSafeFilename tests filename generation
func TestGenerateSafeFilename(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "SimpleURL",
			url:      "https://example.com/image.jpg",
			expected: "image.jpg",
		},
		{
			name:     "SubstackPattern",
			url:      "https://substackcdn.com/image/fetch/w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fd83a175f-d0a1-450a-931f-adf68630630e_5634x2864.jpeg",
			expected: "d83a175f-d0a1-450a-931f-adf68630630e_5634x2864.jpeg",
		},
		{
			name:     "InvalidCharacters",
			url:      "https://example.com/image:with<bad>chars.png",
			expected: "image_with_bad_chars.png",
		},
		{
			name:     "NoExtension",
			url:      "https://example.com/imagewithoutextension",
			expected: "imagewithoutextension",
		},
		{
			name:     "EmptyFilename",
			url:      "https://example.com/",
			expected: "image.jpg",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := downloader.generateSafeFilename(test.url)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, result)
		})
	}
}

// TestGetImageFormat tests image format detection
func TestGetImageFormat(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	tests := []struct {
		filename string
		format   string
	}{
		{"image.jpg", "jpeg"},
		{"image.jpeg", "jpeg"},
		{"image.png", "png"},
		{"image.webp", "webp"},
		{"image.gif", "gif"},
		{"image.JPG", "jpeg"},
		{"image.PNG", "png"},
		{"image.unknown", "unknown"},
		{"image", "unknown"},
	}
	
	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			result := downloader.getImageFormat(test.filename)
			assert.Equal(t, test.format, result)
		})
	}
}

// TestExtractDimensionsFromURL tests dimension extraction from URLs
func TestExtractDimensionsFromURL(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	tests := []struct {
		name   string
		url    string
		width  int
		height int
	}{
		{
			name:   "DimensionPattern",
			url:    "https://example.com/image_1920x1080.jpg",
			width:  1920,
			height: 1080,
		},
		{
			name:   "WidthOnlyPattern",
			url:    "https://example.com/w_1456,c_limit/image.jpg",
			width:  1456,
			height: 0,
		},
		{
			name:   "NoDimensions",
			url:    "https://example.com/image.jpg",
			width:  0,
			height: 0,
		},
		{
			name:   "SubstackPattern",
			url:    "https://substackcdn.com/image/fetch/w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fd83a175f-d0a1-450a-931f-adf68630630e_5634x2864.jpeg",
			width:  5634,
			height: 2864,
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := downloader.extractDimensionsFromURL(test.url)
			assert.Equal(t, test.width, width)
			assert.Equal(t, test.height, height)
		})
	}
}

// TestDownloadImages tests the complete image downloading workflow
func TestDownloadImages(t *testing.T) {
	// Create test server
	server := createTestImageServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "image-download-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create downloader
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	
	t.Run("SuccessfulDownload", func(t *testing.T) {
		htmlContent := createTestHTMLWithImages(server.URL)
		ctx := context.Background()
		
		result, err := downloader.DownloadImages(ctx, htmlContent, "test-post")
		require.NoError(t, err)
		
		// Check results
		assert.Greater(t, result.Success, 0, "Should have successful downloads")
		assert.Greater(t, result.Failed, 0, "Should have failed downloads (not-found image)")
		assert.Greater(t, len(result.Images), 0, "Should have image info")
		
		// Check that images directory was created
		imagesDir := filepath.Join(tempDir, "images", "test-post")
		_, err = os.Stat(imagesDir)
		assert.NoError(t, err, "Images directory should exist")
		
		// Check that some images were downloaded
		files, err := os.ReadDir(imagesDir)
		assert.NoError(t, err)
		assert.Greater(t, len(files), 0, "Should have downloaded image files")
		
		// Check that HTML was updated
		assert.NotEqual(t, htmlContent, result.UpdatedHTML, "HTML should be updated")
		assert.Contains(t, result.UpdatedHTML, "images/test-post/", "HTML should contain local image paths")
	})
	
	t.Run("NoImages", func(t *testing.T) {
		htmlContent := "<html><body><p>No images here</p></body></html>"
		ctx := context.Background()
		
		result, err := downloader.DownloadImages(ctx, htmlContent, "no-images-post")
		require.NoError(t, err)
		
		assert.Equal(t, 0, result.Success)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, len(result.Images))
		assert.Equal(t, htmlContent, result.UpdatedHTML)
	})
	
	t.Run("EmptyHTML", func(t *testing.T) {
		emptyHTML := ""
		ctx := context.Background()
		
		result, err := downloader.DownloadImages(ctx, emptyHTML, "empty-post")
		require.NoError(t, err)
		
		assert.Equal(t, 0, result.Success)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, len(result.Images))
	})
}

// TestDownloadSingleImage tests individual image downloading
func TestDownloadSingleImage(t *testing.T) {
	// Create test server
	server := createTestImageServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "single-image-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	ctx := context.Background()
	
	t.Run("SuccessfulDownload", func(t *testing.T) {
		imageURL := server.URL + "/success.png"
		imageInfo := downloader.downloadSingleImage(ctx, imageURL, tempDir)
		
		assert.True(t, imageInfo.Success)
		assert.NoError(t, imageInfo.Error)
		assert.Equal(t, imageURL, imageInfo.OriginalURL)
		assert.NotEmpty(t, imageInfo.LocalPath)
		
		// Check file exists
		_, err := os.Stat(imageInfo.LocalPath)
		assert.NoError(t, err)
		
		// Check file content
		data, err := os.ReadFile(imageInfo.LocalPath)
		assert.NoError(t, err)
		assert.Equal(t, testImageData, data)
	})
	
	t.Run("NotFound", func(t *testing.T) {
		imageURL := server.URL + "/not-found.png"
		imageInfo := downloader.downloadSingleImage(ctx, imageURL, tempDir)
		
		assert.False(t, imageInfo.Success)
		assert.Error(t, imageInfo.Error)
		assert.Equal(t, imageURL, imageInfo.OriginalURL)
	})
	
	t.Run("ServerError", func(t *testing.T) {
		imageURL := server.URL + "/server-error.png"
		imageInfo := downloader.downloadSingleImage(ctx, imageURL, tempDir)
		
		assert.False(t, imageInfo.Success)
		assert.Error(t, imageInfo.Error)
	})
}

// TestUpdateHTMLWithLocalPaths tests HTML content updating
func TestUpdateHTMLWithLocalPaths(t *testing.T) {
	downloader := NewImageDownloader(nil, "/output", "images", ImageQualityHigh)
	
	originalHTML := `<img src="https://example.com/image1.jpg" alt="Image 1">
<img src="https://example.com/image2.png" alt="Image 2">
<img src="https://example.com/image1.jpg" alt="Same image again">`
	
	urlToLocalPath := map[string]string{
		"https://example.com/image1.jpg": filepath.Join("/output", "images", "post", "image1.jpg"),
		"https://example.com/image2.png": filepath.Join("/output", "images", "post", "image2.png"),
	}
	
	updatedHTML := downloader.updateHTMLWithLocalPaths(originalHTML, urlToLocalPath)
	
	// Check that URLs were replaced
	assert.Contains(t, updatedHTML, `src="images/post/image1.jpg"`)
	assert.Contains(t, updatedHTML, `src="images/post/image2.png"`)
	assert.NotContains(t, updatedHTML, "https://example.com/")
	
	// Check that duplicate URLs were replaced
	assert.Equal(t, 2, strings.Count(updatedHTML, "images/post/image1.jpg"))
}

// Benchmark tests
func BenchmarkExtractURLFromSrcset(b *testing.B) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	srcset := "img-424.jpg 424w, img-848.jpg 848w, img-1272.jpg 1272w, img-1456.jpg 1456w"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downloader.extractURLFromSrcset(srcset, 1456)
	}
}

func BenchmarkGenerateSafeFilename(b *testing.B) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	url := "https://substackcdn.com/image/fetch/w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fd83a175f-d0a1-450a-931f-adf68630630e_5634x2864.jpeg"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downloader.generateSafeFilename(url)
	}
}

// TestWithRealSubstackHTML tests image extraction from actual Substack HTML files
func TestWithRealSubstackHTML(t *testing.T) {
	// Skip test if scraped directory doesn't exist
	scrapedDir := "../scraped/computerenhance"
	if _, err := os.Stat(scrapedDir); os.IsNotExist(err) {
		t.Skip("Scraped directory not found, skipping real HTML test")
	}
	
	// Find some sample HTML files
	files, err := os.ReadDir(scrapedDir)
	require.NoError(t, err)
	
	var htmlFiles []string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".html") && len(htmlFiles) < 3 {
			htmlFiles = append(htmlFiles, filepath.Join(scrapedDir, file.Name()))
		}
	}
	
	if len(htmlFiles) == 0 {
		t.Skip("No HTML files found in scraped directory")
	}
	
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "real-substack-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	
	for _, htmlFile := range htmlFiles {
		t.Run(filepath.Base(htmlFile), func(t *testing.T) {
			// Read the HTML file
			htmlContent, err := os.ReadFile(htmlFile)
			require.NoError(t, err)
			
			// Extract image URLs from the real HTML
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlContent)))
			require.NoError(t, err)
			
			imageURLs, err := downloader.extractImageURLs(doc)
			require.NoError(t, err)
			
			t.Logf("Found %d image URLs in %s", len(imageURLs), filepath.Base(htmlFile))
			
			// Verify we can parse the image URLs and generate filenames
			for i, imageURL := range imageURLs {
				if i >= 5 { // Limit to first 5 images for performance
					break
				}
				
				t.Logf("Image URL %d: %s", i+1, imageURL)
				
				// Test filename generation
				filename, err := downloader.generateSafeFilename(imageURL)
				assert.NoError(t, err)
				assert.NotEmpty(t, filename)
				assert.False(t, strings.Contains(filename, "<"), "Filename should not contain invalid characters")
				assert.False(t, strings.Contains(filename, ">"), "Filename should not contain invalid characters")
				
				// Test dimension extraction
				width, height := downloader.extractDimensionsFromURL(imageURL)
				t.Logf("  Dimensions: %dx%d", width, height)
				
				// Test URL parsing
				_, err = url.Parse(imageURL)
				assert.NoError(t, err, "Image URL should be valid")
			}
			
			// Test HTML update functionality (without actually downloading)
			if len(imageURLs) > 0 {
				// Create a mock mapping for URL replacement
				urlToLocalPath := make(map[string]string)
				for i, imageURL := range imageURLs {
					if i >= 3 { // Limit for performance
						break
					}
					filename, _ := downloader.generateSafeFilename(imageURL)
					localPath := filepath.Join(tempDir, "images", "test-post", filename)
					urlToLocalPath[imageURL] = localPath
				}
				
				updatedHTML := downloader.updateHTMLWithLocalPaths(string(htmlContent), urlToLocalPath)
				assert.NotEqual(t, string(htmlContent), updatedHTML, "HTML should be updated")
				
				// Verify some URLs were replaced
				for originalURL := range urlToLocalPath {
					assert.NotContains(t, updatedHTML, originalURL, "Original URL should be replaced")
				}
			}
		})
	}
}

// TestURLReplacementIssue tests that all image URLs are properly replaced in HTML
func TestURLReplacementIssue(t *testing.T) {
	// Create test server
	server := createTestImageServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "url-replacement-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create downloader
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	
	// Create HTML with mismatched URLs between src and data-attrs
	// Use server URLs so downloads will succeed
	htmlContent := fmt.Sprintf(`<div class="captioned-image-container">
  <figure>
    <a class="image-link" href="%s/fullsize.jpeg">
      <div class="image2-inset">
        <picture>
          <img src="%s/w_1456.jpeg" 
               srcset="%s/w_424.jpeg 424w, %s/w_848.jpeg 848w, %s/w_1456.jpeg 1456w"
               data-attrs='{"src":"%s/original-high-quality.jpeg","width":1456,"height":819}'
               alt="Test image" width="1456" height="819">
        </picture>
      </div>
    </a>
  </figure>
</div>

<img src="%s/simple-src.jpg" 
     data-attrs='{"src":"%s/data-attrs-src.jpg","width":800,"height":600}' 
     alt="Simple image">`, 
		server.URL, server.URL, server.URL, server.URL, server.URL, server.URL, server.URL, server.URL)
	
	t.Logf("Original HTML:\n%s", htmlContent)
	
	// Use the full DownloadImages method which should use the new logic
	ctx := context.Background()
	result, err := downloader.DownloadImages(ctx, htmlContent, "test-post")
	require.NoError(t, err)
	
	t.Logf("Download results: Success=%d, Failed=%d", result.Success, result.Failed)
	t.Logf("Updated HTML:\n%s", result.UpdatedHTML)
	
	// Verify that ALL URLs were replaced, not just the ones from data-attrs
	problemURLs := []string{
		fmt.Sprintf("%s/w_1456.jpeg", server.URL),        // src attribute
		fmt.Sprintf("%s/simple-src.jpg", server.URL),     // simple src
		fmt.Sprintf("%s/w_424.jpeg", server.URL),         // srcset URLs
		fmt.Sprintf("%s/w_848.jpeg", server.URL),
	}
	
	for _, url := range problemURLs {
		if strings.Contains(result.UpdatedHTML, url) {
			t.Errorf("URL should be replaced but still present: %s", url)
		}
	}
	
	// Verify some images were actually downloaded
	assert.Greater(t, result.Success, 0, "Should have successful downloads")
	
	// Verify local paths are present
	assert.Contains(t, result.UpdatedHTML, "images/test-post/", "Should contain local image paths")
}

// TestCommaSeparatedURLRegressionBug tests the specific bug reported in v0.6.0
// where multiple URLs for the same image (in srcset, data-attrs, etc.) would
// create comma-separated URL strings in the output instead of clean local paths.
// This is a regression test to ensure this specific pattern doesn't break again.
func TestCommaSeparatedURLRegressionBug(t *testing.T) {
	// Create a test server that serves image content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a small PNG image for any request
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		// Write minimal PNG data
		pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}
		w.Write(pngData)
	}))
	defer server.Close()

	// Create temporary directory
	tempDir := t.TempDir()
	
	fetcher := NewFetcher()
	downloader := NewImageDownloader(fetcher, tempDir, "images", ImageQualityHigh)
	
	// Create HTML that reproduces the exact bug pattern from the bug report
	// This simulates real Substack HTML where the same image appears with multiple URL variations
	// but they all represent the same actual image file and should map to the same local path
	baseImageID := "4697c31d-2502-48d2-b6c1-11e5ea97536f_2560x2174"
	
	// These represent different CDN transformations of the same base image
	// All should download the same file and map to the same local path
	originalURL := fmt.Sprintf("%s/substack-post-media.s3.amazonaws.com/public/images/%s.jpeg", server.URL, baseImageID)
	w1456URL := fmt.Sprintf("%s/substackcdn.com/image/fetch/w_1456,c_limit,f_auto,q_auto:good/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s.jpeg", server.URL, baseImageID)
	w848URL := fmt.Sprintf("%s/substackcdn.com/image/fetch/w_848,c_limit,f_auto,q_auto:good/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s.jpeg", server.URL, baseImageID)
	w424URL := fmt.Sprintf("%s/substackcdn.com/image/fetch/w_424,c_limit,f_auto,q_auto:good/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s.jpeg", server.URL, baseImageID)
	webpURL := fmt.Sprintf("%s/substackcdn.com/image/fetch/f_webp,w_1456,c_limit,q_auto:good/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s.jpeg", server.URL, baseImageID)
	
	// Create HTML that matches the structure from the bug report
	// All these URLs should map to the same local file path
	htmlContent := fmt.Sprintf(`<div class="captioned-image-container">
  <figure>
    <a class="image-link image2 is-viewable-img" target="_blank" href="%s" data-component-name="Image2ToDOM">
      <div class="image2-inset">
        <picture>
          <source type="image/webp" srcset="%s 424w, %s 848w, %s 1272w, %s 1456w" sizes="100vw">
          <img src="%s" 
               srcset="%s 424w, %s 848w, %s 1272w, %s 1456w" 
               data-attrs='{"src":"%s","srcNoWatermark":null,"fullscreen":false,"imageSize":"large","height":1236,"width":1456}'
               class="sizing-large" alt="Test Image" title="Test Image" 
               sizes="100vw" fetchpriority="high">
        </picture>
      </div>
    </a>
  </figure>
</div>`, 
		originalURL,  // href
		w424URL, w848URL, w1456URL, webpURL,  // webp srcset
		w1456URL,     // img src  
		w424URL, w848URL, w1456URL, webpURL,  // img srcset
		originalURL)  // data-attrs src
	
	t.Logf("Original HTML with potentially problematic URLs:\n%s", htmlContent)
	
	// Download images using the full pipeline
	ctx := context.Background()
	result, err := downloader.DownloadImages(ctx, htmlContent, "good-ideas")
	require.NoError(t, err)
	
	t.Logf("Download results: Success=%d, Failed=%d", result.Success, result.Failed)
	t.Logf("Updated HTML:\n%s", result.UpdatedHTML)
	
	// THE KEY REGRESSION TEST: Verify no comma-separated URL strings appear
	// This is the exact bug pattern that was reported
	commaSeparatedPatterns := []string{
		"images/good-ideas/" + baseImageID + ".jpeg,images/good-ideas/",  // Should not have comma-separated paths
		",f_webp,images/good-ideas/",  // Should not have CDN parameters mixed with local paths
		"images/good-ideas/" + baseImageID + ".jpeg,images/good-ideas/" + baseImageID + ".jpeg",  // Repeated paths
	}
	
	for _, pattern := range commaSeparatedPatterns {
		if strings.Contains(result.UpdatedHTML, pattern) {
			t.Errorf("REGRESSION BUG DETECTED: Found comma-separated URL pattern in output: %s", pattern)
			t.Errorf("This indicates the string replacement bug has returned")
		}
	}
	
	// Verify that all original URLs have been replaced with local paths
	originalURLs := []string{originalURL, w1456URL, w848URL, w424URL, webpURL}
	for _, url := range originalURLs {
		if strings.Contains(result.UpdatedHTML, url) {
			t.Errorf("Original URL should be replaced but still present: %s", url)
		}
	}
	
	// Verify clean local paths are present
	expectedLocalPath := "images/good-ideas/" + baseImageID + ".jpeg"
	if !strings.Contains(result.UpdatedHTML, expectedLocalPath) {
		t.Errorf("Expected local path not found: %s", expectedLocalPath)
	}
	
	// Verify srcset entries are clean (no commas except between entries)
	if strings.Contains(result.UpdatedHTML, `srcset="`) {
		// Extract srcset content
		srcsetStart := strings.Index(result.UpdatedHTML, `srcset="`) + 8
		srcsetEnd := strings.Index(result.UpdatedHTML[srcsetStart:], `"`)
		srcsetContent := result.UpdatedHTML[srcsetStart : srcsetStart+srcsetEnd]
		
		t.Logf("Extracted srcset: %s", srcsetContent)
		
		// Verify srcset has proper format: "path width, path width, ..." or just "path"
		// Should NOT have comma-separated paths without proper structure
		entries := strings.Split(srcsetContent, ",")
		for i, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			
			parts := strings.Fields(entry)
			if len(parts) == 0 {
				t.Errorf("Srcset entry %d is empty after trimming: %s", i, entry)
				continue
			}
			
			// First part should be a clean local path
			if !strings.HasPrefix(parts[0], "images/good-ideas/") {
				t.Errorf("Srcset entry %d doesn't have proper local path: %s", i, parts[0])
			}
			
			// If there's a second part, it should be a width descriptor
			if len(parts) >= 2 {
				if !strings.HasSuffix(parts[1], "w") {
					t.Errorf("Srcset entry %d has invalid width descriptor: %s", i, parts[1])
				}
			}
			
			// Should not have more than 2 parts
			if len(parts) > 2 {
				t.Errorf("Srcset entry %d has too many parts (should be 'path' or 'path width'): %s", i, entry)
			}
		}
	}
	
	// Verify at least one image was successfully downloaded
	assert.Greater(t, result.Success, 0, "Should have successful downloads")
	assert.Equal(t, 0, result.Failed, "Should have no failed downloads")
}

// TestExtractImageElements tests the new image element extraction with all URLs
func TestExtractImageElements(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	htmlContent := `
	<!-- Image with all attributes -->
	<img src="https://example.com/src.jpg" 
	     srcset="https://example.com/small.jpg 400w, https://example.com/large.jpg 800w"
	     data-attrs='{"src":"https://example.com/data.jpg","width":800,"height":600}' 
	     alt="Complete image">
	
	<!-- Image with only src -->
	<img src="https://example.com/simple.jpg" alt="Simple image">
	
	<!-- Image with only data-attrs -->
	<img data-attrs='{"src":"https://example.com/data-only.jpg","width":400,"height":300}' alt="Data only">
	`
	
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	require.NoError(t, err)
	
	imageElements, err := downloader.extractImageElements(doc)
	require.NoError(t, err)
	
	// Should find 3 image elements
	assert.Len(t, imageElements, 3)
	
	// First image should have all URLs
	elem1 := imageElements[0]
	assert.Equal(t, "https://example.com/data.jpg", elem1.BestURL) // data-attrs has priority
	expectedURLs1 := []string{
		"https://example.com/data.jpg",     // from data-attrs
		"https://example.com/small.jpg",    // from srcset
		"https://example.com/large.jpg",    // from srcset
		"https://example.com/src.jpg",      // from src
	}
	assert.ElementsMatch(t, expectedURLs1, elem1.AllURLs)
	
	// Second image should have only src URL
	elem2 := imageElements[1]
	assert.Equal(t, "https://example.com/simple.jpg", elem2.BestURL)
	assert.Equal(t, []string{"https://example.com/simple.jpg"}, elem2.AllURLs)
	
	// Third image should have only data-attrs URL
	elem3 := imageElements[2]
	assert.Equal(t, "https://example.com/data-only.jpg", elem3.BestURL)
	assert.Equal(t, []string{"https://example.com/data-only.jpg"}, elem3.AllURLs)
}

// TestExtractAllURLsFromSrcset tests srcset URL extraction
func TestExtractAllURLsFromSrcset(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	tests := []struct {
		name     string
		srcset   string
		expected []string
	}{
		{
			name:   "MultipleSizes",
			srcset: "https://example.com/img-400.jpg 400w, https://example.com/img-800.jpg 800w, https://example.com/img-1200.jpg 1200w",
			expected: []string{"https://example.com/img-400.jpg", "https://example.com/img-800.jpg", "https://example.com/img-1200.jpg"},
		},
		{
			name:   "SingleEntry",
			srcset: "https://example.com/single.jpg 1024w",
			expected: []string{"https://example.com/single.jpg"},
		},
		{
			name:   "ExtraSpaces",
			srcset: "  https://example.com/spaced1.jpg 400w  ,   https://example.com/spaced2.jpg 800w  ",
			expected: []string{"https://example.com/spaced1.jpg", "https://example.com/spaced2.jpg"},
		},
		{
			name:     "Empty",
			srcset:   "",
			expected: []string{},
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			urls := downloader.extractAllURLsFromSrcset(test.srcset)
			assert.Equal(t, test.expected, urls)
		})
	}
}

// TestImageURLParsing tests URL parsing with various Substack image patterns
func TestImageURLParsing(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	// Real Substack URL patterns from the analysis
	testURLs := []string{
		"https://substackcdn.com/image/fetch/f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fbucketeer-e05bbc84-baa3-437e-9518-adb32be77984.s3.amazonaws.com%2Fpublic%2Fimages%2F43e258db-6164-4e47-835f-d11f10847d9d_5616x3744.jpeg",
		"https://substackcdn.com/image/fetch/w_1456,c_limit,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2Fd83a175f-d0a1-450a-931f-adf68630630e_5634x2864.jpeg",
		"https://substack-post-media.s3.amazonaws.com/public/images/d6ad0fd8-3659-4626-b5db-f81cbcd4c779_779x305.png",
	}
	
	for i, testURL := range testURLs {
		t.Run(fmt.Sprintf("URL_%d", i+1), func(t *testing.T) {
			// Test filename generation
			filename, err := downloader.generateSafeFilename(testURL)
			assert.NoError(t, err)
			assert.NotEmpty(t, filename)
			
			// Test dimension extraction
			width, height := downloader.extractDimensionsFromURL(testURL)
			t.Logf("URL: %s", testURL)
			t.Logf("Filename: %s", filename)
			t.Logf("Dimensions: %dx%d", width, height)
			
			// URLs should be valid
			_, err = url.Parse(testURL)
			assert.NoError(t, err)
		})
	}
}

// TestImageURLHelperFunctions tests the helper functions added for the bug fix
func TestImageURLHelperFunctions(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	t.Run("IsImageURL", func(t *testing.T) {
		tests := []struct {
			name     string
			url      string
			expected bool
		}{
			{"SubstackCDN", "https://substackcdn.com/image/fetch/w_1456/image.jpg", true},
			{"SubstackS3", "https://substack-post-media.s3.amazonaws.com/public/images/test.png", true},
			{"Bucketeer", "https://bucketeer-e05bbc84-baa3-437e-9518-adb32be77984.s3.amazonaws.com/public/images/test.jpeg", true},
			{"NotImage", "https://example.com/page.html", false},
			{"RegularImage", "https://example.com/image.jpg", false}, // Not Substack
		}
		
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				result := downloader.isImageURL(test.url)
				assert.Equal(t, test.expected, result)
			})
		}
	})
	
	t.Run("IsSameImage", func(t *testing.T) {
		baseUUID := "b0ebde87-580d-4dce-bb73-573edf9229ff"
		tests := []struct {
			name     string
			url1     string
			url2     string
			expected bool
		}{
			{
				"SameUUID",
				fmt.Sprintf("https://substackcdn.com/image/fetch/w_1456/%s_1024x1536.heic", baseUUID),
				fmt.Sprintf("https://substack-post-media.s3.amazonaws.com/public/images/%s_1024x1536.heic", baseUUID),
				true,
			},
			{
				"DifferentUUIDs",
				"https://substackcdn.com/image/fetch/w_1456/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee_800x600.jpg",
				"https://substackcdn.com/image/fetch/w_848/ffffffff-gggg-hhhh-iiii-jjjjjjjjjjjj_800x600.jpg",
				false,
			},
			{
				"NoUUIDs",
				"https://example.com/image1.jpg",
				"https://example.com/image2.jpg",
				false,
			},
		}
		
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				result := downloader.isSameImage(test.url1, test.url2)
				assert.Equal(t, test.expected, result)
			})
		}
	})
	
	t.Run("ExtractImageID", func(t *testing.T) {
		tests := []struct {
			name     string
			url      string
			expected string
		}{
			{
				"UUID",
				"https://substack-post-media.s3.amazonaws.com/public/images/b0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic",
				"b0ebde87-580d-4dce-bb73-573edf9229ff",
			},
			{
				"FilenamePattern",
				"https://example.com/path/to/myimage.jpg",
				"myimage",
			},
			{
				"NoPattern",
				"https://example.com/path/",
				"",
			},
		}
		
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				result := extractImageID(test.url)
				assert.Equal(t, test.expected, result)
			})
		}
	})
}

// TestExtractImageElementsWithAnchorAndSourceTags tests the bug fix for collecting URLs from <a> and <source> tags
func TestExtractImageElementsWithAnchorAndSourceTags(t *testing.T) {
	downloader := NewImageDownloader(nil, "/tmp", "images", ImageQualityHigh)
	
	// This HTML pattern reproduces the exact structure from real Substack posts
	// where the same image appears in multiple places with different URLs
	baseUUID := "f35ed9ff-eb9e-4106-a443-45c963ae74cd"
	originalURL := fmt.Sprintf("https://substack-post-media.s3.amazonaws.com/public/images/%s_1208x793.png", baseUUID)
	hrefURL := fmt.Sprintf("https://substackcdn.com/image/fetch/f_auto,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s_1208x793.png", baseUUID)
	w424URL := fmt.Sprintf("https://substackcdn.com/image/fetch/w_424,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s_1208x793.png", baseUUID)
	w848URL := fmt.Sprintf("https://substackcdn.com/image/fetch/w_848,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s_1208x793.png", baseUUID)
	w1456URL := fmt.Sprintf("https://substackcdn.com/image/fetch/w_1456,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2F%s_1208x793.png", baseUUID)
	
	htmlContent := fmt.Sprintf(`
	<div class="captioned-image-container">
	  <figure>
	    <a class="image-link image2 is-viewable-img" target="_blank" href="%s" data-component-name="Image2ToDOM">
	      <div class="image2-inset">
	        <picture>
	          <source type="image/webp" srcset="%s 424w, %s 848w, %s 1456w" sizes="100vw"/>
	          <img src="%s" 
	               srcset="%s 424w, %s 848w, %s 1456w" 
	               data-attrs='{"src":"%s","width":1208,"height":793,"type":"image/png"}'
	               class="sizing-normal" alt="" 
	               sizes="100vw" fetchpriority="high"/>
	        </picture>
	      </div>
	    </a>
	  </figure>
	</div>`,
		hrefURL,                               // <a href>
		w424URL, w848URL, w1456URL,            // <source srcset>
		originalURL,                           // <img src>
		w424URL, w848URL, w1456URL,            // <img srcset>
		originalURL)                           // data-attrs src
	
	t.Logf("Test HTML:\n%s", htmlContent)
	
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	require.NoError(t, err)
	
	imageElements, err := downloader.extractImageElements(doc)
	require.NoError(t, err)
	
	// Should find exactly 1 image element (all URLs refer to the same image)
	assert.Len(t, imageElements, 1, "Should find exactly one image element")
	
	elem := imageElements[0]
	t.Logf("BestURL: %s", elem.BestURL)
	t.Logf("AllURLs: %v", elem.AllURLs)
	
	// Best URL should be from data-attrs (highest priority)
	assert.Equal(t, originalURL, elem.BestURL)
	
	// All URLs should be collected (from img src, img srcset, source srcset, a href, and data-attrs)
	expectedURLs := []string{
		originalURL,  // from data-attrs and img src
		w424URL,      // from srcsets
		w848URL,      // from srcsets
		w1456URL,     // from srcsets
		hrefURL,      // from <a href>
	}
	
	// Check that all expected URLs are present
	for _, expectedURL := range expectedURLs {
		assert.Contains(t, elem.AllURLs, expectedURL, "Should contain URL: %s", expectedURL)
	}
	
	// Should not have duplicates
	urlCounts := make(map[string]int)
	for _, url := range elem.AllURLs {
		urlCounts[url]++
	}
	for url, count := range urlCounts {
		assert.Equal(t, 1, count, "URL should appear exactly once: %s", url)
	}
}

// TestHrefAndSourceURLReplacementRegression tests the specific bug where images were downloaded 
// but <a href> and <source srcset> URLs weren't replaced with local paths
func TestHrefAndSourceURLReplacementRegression(t *testing.T) {
	// Create test server
	server := createTestImageServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "href-source-regression-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create downloader
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	
	// Create HTML that reproduces the exact bug:
	// - Images are downloaded successfully
	// - img src and srcset are replaced correctly
	// - BUT <a href> and <source srcset> still contain original URLs
	// Using Substack-style URLs so they're detected as image URLs
	baseUUID := "123e4567-e89b-12d3-a456-426614174000"
	imageURL := server.URL + "/substack-post-media.s3.amazonaws.com/public/images/" + baseUUID + "_800x600.png"
	hrefURL := server.URL + "/substackcdn.com/image/fetch/f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2F" + baseUUID + "_1200x900.png"
	srcsetURL1 := server.URL + "/substackcdn.com/image/fetch/w_424,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2F" + baseUUID + "_800x600.png"
	srcsetURL2 := server.URL + "/substackcdn.com/image/fetch/w_848,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2F" + baseUUID + "_800x600.png"
	
	htmlContent := fmt.Sprintf(`
	<div class="captioned-image-container">
	  <figure>
	    <a class="image-link image2 is-viewable-img" target="_blank" href="%s">
	      <div class="image2-inset">
	        <picture>
	          <source type="image/webp" srcset="%s 424w, %s 848w" sizes="100vw"/>
	          <img src="%s" 
	               srcset="%s 424w, %s 848w" 
	               alt="Test image" width="800" height="600"/>
	        </picture>
	      </div>
	    </a>
	  </figure>
	</div>`,
		hrefURL,                     // <a href> - THIS was not being replaced in the bug
		srcsetURL1, srcsetURL2,      // <source srcset> - THIS was not being replaced in the bug
		imageURL,                    // <img src> - this was working
		srcsetURL1, srcsetURL2)      // <img srcset> - this was working
	
	t.Logf("Original HTML with problematic URLs:\n%s", htmlContent)
	
	// Download images using the full pipeline
	ctx := context.Background()
	result, err := downloader.DownloadImages(ctx, htmlContent, "regression-test")
	require.NoError(t, err)
	
	t.Logf("Download results: Success=%d, Failed=%d", result.Success, result.Failed)
	t.Logf("Updated HTML:\n%s", result.UpdatedHTML)
	
	// CRITICAL REGRESSION TEST: Verify ALL original URLs are replaced
	originalURLs := []string{imageURL, hrefURL, srcsetURL1, srcsetURL2}
	
	for _, originalURL := range originalURLs {
		assert.NotContains(t, result.UpdatedHTML, originalURL, 
			"REGRESSION BUG: Original URL should be replaced but still present: %s", originalURL)
	}
	
	// Verify local paths are present  
	assert.Contains(t, result.UpdatedHTML, "images/regression-test/", "Should contain local image directory path")
	
	// Verify <a href> was replaced with local path
	assert.Regexp(t, `href="images/regression-test/[^"]*"`, result.UpdatedHTML, "href should point to local path")
	
	// Verify <source srcset> was replaced with local paths
	assert.Contains(t, result.UpdatedHTML, `<source type="image/webp" srcset="images/regression-test/`, 
		"source srcset should contain local paths")
	
	// Verify some images were successfully downloaded
	assert.Greater(t, result.Success, 0, "Should have successful downloads")
	
	// Verify image files exist on disk
	imagesDir := filepath.Join(tempDir, "images", "regression-test")
	files, err := os.ReadDir(imagesDir)
	assert.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have downloaded image files to disk")
}

// TestComplexSubstackImageStructureRegression tests the full complex Substack image structure
// that was reported in the original bug, ensuring all image references are properly replaced
func TestComplexSubstackImageStructureRegression(t *testing.T) {
	// Create test server
	server := createTestImageServer()
	defer server.Close()
	
	// Create temporary directory  
	tempDir, err := os.MkdirTemp("", "complex-substack-regression-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create downloader
	downloader := NewImageDownloader(nil, tempDir, "images", ImageQualityHigh)
	
	// This is the exact HTML structure from the bug report, with server URLs
	htmlContent := fmt.Sprintf(`<div class="captioned-image-container"><figure><a class="image-link image2 is-viewable-img" target="_blank" href="%s/substackcdn.com/image/fetch/$s_!7a2j!,f_auto,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2Fb0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic" data-component-name="Image2ToDOM"><div class="image2-inset"><picture><source type="image/webp" srcset="%s/substackcdn.com/image/fetch/$s_!7a2j!,w_424,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2Fb0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic 424w, %s/substackcdn.com/image/fetch/$s_!7a2j!,w_848,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2Fb0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic 848w, %s/substackcdn.com/image/fetch/$s_!7a2j!,w_1456,c_limit,f_webp,q_auto:good,fl_progressive:steep/https%%3A%%2F%%2Fsubstack-post-media.s3.amazonaws.com%%2Fpublic%%2Fimages%%2Fb0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic 1456w" sizes="100vw"/><img src="%s/substack-post-media.s3.amazonaws.com/public/images/b0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic" width="1024" height="1536" data-attrs="{&#34;src&#34;:&#34;%s/substack-post-media.s3.amazonaws.com/public/images/b0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic&#34;,&#34;width&#34;:1024,&#34;height&#34;:1536}" class="sizing-normal" alt="" srcset="%s/substack-post-media.s3.amazonaws.com/public/images/b0ebde87-580d-4dce-bb73-573edf9229ff_1024x1536.heic 424w" sizes="100vw" fetchpriority="high"/></picture></div></a></figure></div>`,
		server.URL, server.URL, server.URL, server.URL, server.URL, server.URL, server.URL)
	
	t.Logf("Complex Substack HTML structure:\n%s", htmlContent)
	
	// Process the HTML 
	ctx := context.Background()
	result, err := downloader.DownloadImages(ctx, htmlContent, "complex-test")
	require.NoError(t, err)
	
	t.Logf("Download results: Success=%d, Failed=%d", result.Success, result.Failed)
	t.Logf("Updated HTML:\n%s", result.UpdatedHTML)
	
	// Verify NO original server URLs remain in the output
	assert.NotContains(t, result.UpdatedHTML, server.URL, 
		"REGRESSION BUG: Original server URLs should be completely replaced")
	
	// Verify local paths are present
	assert.Contains(t, result.UpdatedHTML, "images/complex-test/", "Should contain local image paths")
	
	// Verify the href was replaced
	assert.Contains(t, result.UpdatedHTML, `href="images/complex-test/`, "href should point to local path")
	
	// Verify source srcset was replaced  
	assert.Contains(t, result.UpdatedHTML, `<source type="image/webp" srcset="images/complex-test/`, 
		"source srcset should contain local paths")
	
	// Verify img src was replaced
	assert.Contains(t, result.UpdatedHTML, `src="images/complex-test/`, "img src should point to local path")
	
	// Verify img srcset was replaced
	assert.Regexp(t, `srcset="images/complex-test/[^"]+\s+424w"`, result.UpdatedHTML, 
		"img srcset should contain local paths with width descriptors")
	
	// Verify data-attrs was updated (JSON can be reordered and HTML-encoded)
	assert.Regexp(t, `&#34;src&#34;:&#34;images/complex-test/[^&]*&#34;`, result.UpdatedHTML, "data-attrs src should be updated")
	
	// Verify at least one image was successfully downloaded
	assert.Greater(t, result.Success, 0, "Should have successful downloads")
}