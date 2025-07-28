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
			// Don't respond to simulate timeout
			select {}
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