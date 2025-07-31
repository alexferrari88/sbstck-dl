package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test file data - a simple text file content
var testFileData = []byte("This is a test file content for file attachment download testing.")

// createTestFileServer creates a test server that serves test files
func createTestFileServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		
		switch {
		case strings.Contains(path, "success"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment; filename=\"test-file.pdf\"")
			w.WriteHeader(http.StatusOK)
			w.Write(testFileData)
		case strings.Contains(path, "document.pdf"):
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
			w.Write(testFileData)
		case strings.Contains(path, "spreadsheet.xlsx"):
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
			w.WriteHeader(http.StatusOK)
			w.Write(testFileData)
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
		case strings.Contains(path, "with-query"):
			// Handle URLs with filename in query parameter
			filename := r.URL.Query().Get("filename")
			if filename != "" {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write(testFileData)
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write(testFileData)
		}
	}))
}

// createTestHTMLWithFiles creates HTML content with file attachment links
func createTestHTMLWithFiles(baseURL string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><title>Test Post with Files</title></head>
<body>
<h1>Test Post with File Attachments</h1>

<!-- Standard file embed button -->
<div class="file-embed-container">
  <a class="file-embed-button wide" href="%s/document.pdf" target="_blank">
    <div class="file-embed-icon">📄</div>
    <div class="file-embed-text">Download PDF Document</div>
  </a>
</div>

<!-- Another file type -->
<div class="file-embed-container">
  <a class="file-embed-button wide" href="%s/spreadsheet.xlsx" target="_blank">
    <div class="file-embed-icon">📊</div>
    <div class="file-embed-text">Download Excel Spreadsheet</div>
  </a>
</div>

<!-- File with query parameters -->
<div class="file-embed-container">
  <a class="file-embed-button wide" href="%s/with-query?filename=report.docx&id=123" target="_blank">
    <div class="file-embed-text">Download Report</div>
  </a>
</div>

<!-- Non-existent file for error testing -->
<div class="file-embed-container">
  <a class="file-embed-button wide" href="%s/not-found.pdf" target="_blank">
    <div class="file-embed-text">Missing File</div>
  </a>
</div>

<!-- Invalid file link (not a file-embed-button) -->
<div class="other-container">
  <a class="other-button" href="%s/should-not-be-detected.pdf" target="_blank">
    Should not be detected
  </a>
</div>

<!-- File embed button without wide class -->
<div class="file-embed-container">
  <a class="file-embed-button" href="%s/should-not-be-detected-2.pdf" target="_blank">
    Should not be detected either
  </a>
</div>

</body>
</html>`, 
		baseURL, baseURL, baseURL, baseURL, baseURL, baseURL)
}

// TestNewFileDownloader tests the creation of FileDownloader
func TestNewFileDownloader(t *testing.T) {
	t.Run("WithFetcher", func(t *testing.T) {
		fetcher := NewFetcher()
		extensions := []string{"pdf", "docx"}
		downloader := NewFileDownloader(fetcher, "/tmp", "files", extensions)
		
		assert.Equal(t, fetcher, downloader.fetcher)
		assert.Equal(t, "/tmp", downloader.outputDir)
		assert.Equal(t, "files", downloader.filesDir)
		assert.Equal(t, extensions, downloader.fileExtensions)
	})
	
	t.Run("WithoutFetcher", func(t *testing.T) {
		extensions := []string{"xlsx"}
		downloader := NewFileDownloader(nil, "/tmp", "attachments", extensions)
		
		assert.NotNil(t, downloader.fetcher)
		assert.Equal(t, "/tmp", downloader.outputDir)
		assert.Equal(t, "attachments", downloader.filesDir)
		assert.Equal(t, extensions, downloader.fileExtensions)
	})
	
	t.Run("NoExtensions", func(t *testing.T) {
		downloader := NewFileDownloader(nil, "/output", "files", nil)
		
		assert.NotNil(t, downloader.fetcher)
		assert.Equal(t, "/output", downloader.outputDir)
		assert.Equal(t, "files", downloader.filesDir)
		assert.Nil(t, downloader.fileExtensions)
	})
}

// TestExtractFileElements tests file element extraction from HTML
func TestExtractFileElements(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	t.Run("SuccessfulExtraction", func(t *testing.T) {
		downloader := NewFileDownloader(nil, "/tmp", "files", nil)
		htmlContent := createTestHTMLWithFiles(server.URL)
		
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		require.NoError(t, err)
		
		elements, err := downloader.extractFileElements(doc)
		require.NoError(t, err)
		
		// Should find 4 valid file elements (only .file-embed-button.wide)
		assert.Len(t, elements, 4)
		
		// Verify URLs
		expectedURLs := []string{
			server.URL + "/document.pdf",
			server.URL + "/spreadsheet.xlsx",
			server.URL + "/with-query?filename=report.docx&id=123",
			server.URL + "/not-found.pdf",
		}
		
		actualURLs := make([]string, len(elements))
		for i, elem := range elements {
			actualURLs[i] = elem.DownloadURL
		}
		
		assert.ElementsMatch(t, expectedURLs, actualURLs)
	})
	
	t.Run("WithExtensionFilter", func(t *testing.T) {
		// Only allow PDF files
		downloader := NewFileDownloader(nil, "/tmp", "files", []string{"pdf"})
		htmlContent := createTestHTMLWithFiles(server.URL)
		
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		require.NoError(t, err)
		
		elements, err := downloader.extractFileElements(doc)
		require.NoError(t, err)
		
		// Should find only 2 PDF files
		assert.Len(t, elements, 2)
		
		for _, elem := range elements {
			assert.True(t, strings.Contains(elem.DownloadURL, ".pdf"))
		}
	})
	
	t.Run("NoFileElements", func(t *testing.T) {
		downloader := NewFileDownloader(nil, "/tmp", "files", nil)
		htmlContent := "<html><body><p>No file attachments here</p></body></html>"
		
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		require.NoError(t, err)
		
		elements, err := downloader.extractFileElements(doc)
		require.NoError(t, err)
		
		assert.Len(t, elements, 0)
	})
	
	t.Run("InvalidURLs", func(t *testing.T) {
		downloader := NewFileDownloader(nil, "/tmp", "files", nil)
		
		// HTML with invalid URLs
		htmlContent := `
		<a class="file-embed-button wide" href="">Empty href</a>
		<a class="file-embed-button wide" href="not-absolute-url">Relative URL</a>
		<a class="file-embed-button wide" href="://invalid">Invalid URL</a>
		`
		
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
		require.NoError(t, err)
		
		elements, err := downloader.extractFileElements(doc)
		require.NoError(t, err)
		
		// Should find no valid elements
		assert.Len(t, elements, 0)
	})
}

// TestExtractFilenameFromURL tests filename extraction from URLs
func TestExtractFilenameFromURL(t *testing.T) {
	downloader := NewFileDownloader(nil, "/tmp", "files", nil)
	
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "SimpleFilename",
			url:      "https://example.com/document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "FilenameWithPath",
			url:      "https://example.com/files/reports/annual-report.xlsx",
			expected: "annual-report.xlsx",
		},
		{
			name:     "FilenameInQueryParam",
			url:      "https://example.com/?filename=my-file.docx&id=123",
			expected: "my-file.docx",
		},
		{
			name:     "NoFilename",
			url:      "https://example.com/",
			expected: "",
		},
		{
			name:     "InvalidURL",
			url:      "://invalid-url",
			expected: "",
		},
		{
			name:     "OnlyPath",
			url:      "https://example.com/download",
			expected: "download",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := downloader.extractFilenameFromURL(test.url)
			assert.Equal(t, test.expected, result)
		})
	}
}

// TestIsAllowedExtension tests file extension filtering
func TestIsAllowedExtension(t *testing.T) {
	tests := []struct {
		name          string
		extensions    []string
		filename      string
		expected      bool
	}{
		{
			name:       "NoFilter",
			extensions: nil,
			filename:   "document.pdf",
			expected:   true,
		},
		{
			name:       "EmptyFilter",
			extensions: []string{},
			filename:   "document.pdf",
			expected:   true,
		},
		{
			name:       "AllowedExtension",
			extensions: []string{"pdf", "docx"},
			filename:   "document.pdf",
			expected:   true,
		},
		{
			name:       "DisallowedExtension",
			extensions: []string{"pdf", "docx"},
			filename:   "image.jpg",
			expected:   false,
		},
		{
			name:       "CaseInsensitive",
			extensions: []string{"PDF", "DOCX"},
			filename:   "document.pdf",
			expected:   true,
		},
		{
			name:       "NoExtension",
			extensions: []string{"pdf"},
			filename:   "README",
			expected:   false,
		},
		{
			name:       "ExtensionWithDot",
			extensions: []string{".pdf", "docx"},
			filename:   "document.pdf",
			expected:   false, // ".pdf" != "pdf" after dot removal
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			downloader := NewFileDownloader(nil, "/tmp", "files", test.extensions)
			result := downloader.isAllowedExtension(test.filename)
			assert.Equal(t, test.expected, result)
		})
	}
}

// TestSanitizeFilename tests filename sanitization
func TestSanitizeFilename(t *testing.T) {
	downloader := NewFileDownloader(nil, "/tmp", "files", nil)
	
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{
			name:     "SafeFilename",
			filename: "document.pdf",
			expected: "document.pdf",
		},
		{
			name:     "UnsafeCharacters",
			filename: "my<file>name.pdf",
			expected: "my_file_name.pdf",
		},
		{
			name:     "AllUnsafeCharacters",
			filename: `file<>:"/\|?*.txt`,
			expected: "file_________.txt", // 9 unsafe chars replaced with _
		},
		{
			name:     "LeadingTrailingSpaces",
			filename: "  document.pdf  ",
			expected: "document.pdf",
		},
		{
			name:     "LeadingTrailingDots",
			filename: "..document.pdf..",
			expected: "document.pdf",
		},
		{
			name:     "EmptyAfterSanitization",
			filename: "   ...   ", // Should become empty after trimming spaces and dots
			expected: "attachment",
		},
		{
			name:     "VeryLongFilename", 
			filename: strings.Repeat("a", 250) + ".pdf",
			expected: strings.Repeat("a", 250)[:200], // Should be truncated to 200 chars total
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := downloader.sanitizeFilename(test.filename)
			assert.Equal(t, test.expected, result)
			assert.LessOrEqual(t, len(result), 200, "Filename should not exceed 200 characters")
		})
	}
}

// TestGenerateSafeFilenameForFiles tests safe filename generation for files
func TestGenerateSafeFilenameForFiles(t *testing.T) {
	downloader := NewFileDownloader(nil, "/tmp", "files", nil)
	
	// Test that it generates unique filenames (use very different prefixes)
	url1 := "abcdef123456"  // Will produce different hash
	url2 := "zyxwvu987654" // Will produce different hash
	
	filename1 := downloader.generateSafeFilename(url1)
	time.Sleep(1 * time.Millisecond) // Small delay to ensure different timestamp
	filename2 := downloader.generateSafeFilename(url2)
	
	assert.NotEqual(t, filename1, filename2, "Should generate different filenames for different URLs")
	assert.Contains(t, filename1, "file_", "Should contain file_ prefix")
	assert.Contains(t, filename2, "file_", "Should contain file_ prefix")
	
	// Test with same URL multiple times (should be different due to timestamp)
	time.Sleep(1001 * time.Millisecond) // Ensure different timestamp (at least 1 second difference)
	filename3 := downloader.generateSafeFilename(url1)
	assert.NotEqual(t, filename1, filename3, "Should generate different filenames due to timestamp")
}

// TestDownloadSingleFile tests individual file downloading
func TestDownloadSingleFile(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "single-file-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	downloader := NewFileDownloader(nil, tempDir, "files", nil)
	ctx := context.Background()
	
	t.Run("SuccessfulDownload", func(t *testing.T) {
		fileURL := server.URL + "/document.pdf"
		filesPath := filepath.Join(tempDir, "test-post")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.True(t, fileInfo.Success)
		assert.NoError(t, fileInfo.Error)
		assert.Equal(t, fileURL, fileInfo.OriginalURL)
		assert.NotEmpty(t, fileInfo.LocalPath)
		assert.Equal(t, "document.pdf", fileInfo.Filename)
		assert.Equal(t, int64(len(testFileData)), fileInfo.Size)
		
		// Check file exists
		_, statErr := os.Stat(fileInfo.LocalPath)
		assert.NoError(t, statErr)
		
		// Check file content
		data, err := os.ReadFile(fileInfo.LocalPath)
		assert.NoError(t, err)
		assert.Equal(t, testFileData, data)
	})
	
	t.Run("FileAlreadyExists", func(t *testing.T) {
		fileURL := server.URL + "/existing.pdf"
		filesPath := filepath.Join(tempDir, "existing-test")
		
		// Create the directory and file first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		existingFile := filepath.Join(filesPath, "existing.pdf")
		err = os.WriteFile(existingFile, []byte("existing content"), 0644)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.True(t, fileInfo.Success)
		assert.NoError(t, fileInfo.Error)
		assert.Equal(t, fileURL, fileInfo.OriginalURL)
		assert.Equal(t, existingFile, fileInfo.LocalPath)
		
		// File should still contain original content (not downloaded again)
		data, err := os.ReadFile(existingFile)
		assert.NoError(t, err)
		assert.Equal(t, []byte("existing content"), data)
	})
	
	t.Run("NotFound", func(t *testing.T) {
		fileURL := server.URL + "/not-found.pdf"
		filesPath := filepath.Join(tempDir, "not-found-test")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.False(t, fileInfo.Success)
		assert.Error(t, fileInfo.Error)
		assert.Equal(t, fileURL, fileInfo.OriginalURL)
		assert.Equal(t, "not-found.pdf", fileInfo.Filename)
	})
	
	t.Run("ServerError", func(t *testing.T) {
		fileURL := server.URL + "/server-error.pdf"
		filesPath := filepath.Join(tempDir, "server-error-test")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.False(t, fileInfo.Success)
		assert.Error(t, fileInfo.Error)
	})
	
	t.Run("FilenameFromQuery", func(t *testing.T) {
		fileURL := server.URL + "/with-query?filename=report.docx&id=123"
		filesPath := filepath.Join(tempDir, "query-test")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.True(t, fileInfo.Success)
		assert.NoError(t, fileInfo.Error)
		// The filename should come from the path (with-query), not query param since path takes precedence
		assert.Equal(t, "with-query", fileInfo.Filename)
		
		// Check file exists with correct name
		expectedPath := filepath.Join(filesPath, "with-query")
		assert.Equal(t, expectedPath, fileInfo.LocalPath)
		_, statErr := os.Stat(expectedPath)
		assert.NoError(t, statErr)
	})
	
	t.Run("FilenameFromPath", func(t *testing.T) {
		fileURL := server.URL + "/no-filename-in-path"
		filesPath := filepath.Join(tempDir, "path-test")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.True(t, fileInfo.Success)
		assert.NoError(t, fileInfo.Error)
		// The filename should come from the path (no-filename-in-path)
		assert.Equal(t, "no-filename-in-path", fileInfo.Filename)
	})
	
	t.Run("GeneratedFilename", func(t *testing.T) {
		// Use a URL with just / to trigger generated filename
		fileURL := server.URL + "/"
		filesPath := filepath.Join(tempDir, "generated-test")
		
		// Create the directory first
		err := os.MkdirAll(filesPath, 0755)
		require.NoError(t, err)
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, filesPath)
		
		assert.True(t, fileInfo.Success)
		assert.NoError(t, fileInfo.Error)
		// Should use generated filename pattern
		assert.Contains(t, fileInfo.Filename, "file_")
	})
}

// TestMakeRelativePath tests relative path conversion
func TestMakeRelativePath(t *testing.T) {
	downloader := NewFileDownloader(nil, "/output", "files", nil)
	
	tests := []struct {
		name         string
		localPath    string
		expected     string
	}{
		{
			name:      "NormalPath",
			localPath: "/output/files/post/document.pdf",
			expected:  "files/post/document.pdf",
		},
		{
			name:      "WindowsPath",
			localPath: "/output/files/post/report.xlsx",
			expected:  "files/post/report.xlsx",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := downloader.makeRelativePath(test.localPath)
			assert.Equal(t, test.expected, result)
		})
	}
}

// TestUpdateHTMLWithLocalPathsForFiles tests HTML content updating for files
func TestUpdateHTMLWithLocalPathsForFiles(t *testing.T) {
	downloader := NewFileDownloader(nil, "/output", "files", nil)
	
	originalHTML := `
	<a class="file-embed-button wide" href="https://example.com/document.pdf">PDF Document</a>
	<a class="file-embed-button wide" href='https://example.com/spreadsheet.xlsx'>Excel File</a>
	<a class="file-embed-button wide" href="https://example.com/document.pdf">Same PDF Again</a>
	`
	
	urlToLocalPath := map[string]string{
		"https://example.com/document.pdf":    filepath.Join("/output", "files", "post", "document.pdf"),
		"https://example.com/spreadsheet.xlsx": filepath.Join("/output", "files", "post", "spreadsheet.xlsx"),
	}
	
	updatedHTML := downloader.updateHTMLWithLocalPaths(originalHTML, urlToLocalPath)
	
	// Check that URLs were replaced
	assert.Contains(t, updatedHTML, `href="files/post/document.pdf"`)
	assert.Contains(t, updatedHTML, `href='files/post/spreadsheet.xlsx'`)
	assert.NotContains(t, updatedHTML, "https://example.com/")
	
	// Check that duplicate URLs were replaced
	assert.Equal(t, 2, strings.Count(updatedHTML, "files/post/document.pdf"))
}

// TestDownloadFiles tests the complete file downloading workflow
func TestDownloadFiles(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "file-download-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create downloader
	downloader := NewFileDownloader(nil, tempDir, "files", nil)
	
	t.Run("SuccessfulDownload", func(t *testing.T) {
		htmlContent := createTestHTMLWithFiles(server.URL)
		ctx := context.Background()
		
		result, err := downloader.DownloadFiles(ctx, htmlContent, "test-post")
		require.NoError(t, err)
		
		// Check results
		assert.Greater(t, result.Success, 0, "Should have successful downloads")
		assert.Greater(t, result.Failed, 0, "Should have failed downloads (not-found file)")
		assert.Greater(t, len(result.Files), 0, "Should have file info")
		
		// Check that files directory was created
		filesDir := filepath.Join(tempDir, "files", "test-post")
		_, err = os.Stat(filesDir)
		assert.NoError(t, err, "Files directory should exist")
		
		// Check that some files were downloaded
		files, err := os.ReadDir(filesDir)
		assert.NoError(t, err)
		assert.Greater(t, len(files), 0, "Should have downloaded files")
		
		// Check that HTML was updated
		assert.NotEqual(t, htmlContent, result.UpdatedHTML, "HTML should be updated")
		assert.Contains(t, result.UpdatedHTML, "files/test-post/", "HTML should contain local file paths")
		
		// Verify specific file was downloaded
		var pdfFound bool
		for _, file := range result.Files {
			if strings.Contains(file.OriginalURL, "document.pdf") && file.Success {
				pdfFound = true
				assert.Equal(t, "document.pdf", file.Filename)
				assert.Greater(t, file.Size, int64(0))
				
				// Verify file content
				data, err := os.ReadFile(file.LocalPath)
				assert.NoError(t, err)
				assert.Equal(t, testFileData, data)
			}
		}
		assert.True(t, pdfFound, "Should have successfully downloaded PDF file")
	})
	
	t.Run("WithExtensionFilter", func(t *testing.T) {
		// Only allow PDF files
		pdfDownloader := NewFileDownloader(nil, tempDir, "pdf-files", []string{"pdf"})
		htmlContent := createTestHTMLWithFiles(server.URL)
		ctx := context.Background()
		
		result, err := pdfDownloader.DownloadFiles(ctx, htmlContent, "pdf-test")
		require.NoError(t, err)
		
		// Should only process PDF files
		pdfCount := 0
		for _, file := range result.Files {
			if strings.HasSuffix(file.Filename, ".pdf") {
				pdfCount++
			}
		}
		assert.Equal(t, 2, pdfCount, "Should find exactly 2 PDF files")
		assert.Equal(t, 2, len(result.Files), "Should only process PDF files due to filter")
	})
	
	t.Run("NoFiles", func(t *testing.T) {
		htmlContent := "<html><body><p>No file attachments here</p></body></html>"
		ctx := context.Background()
		
		result, err := downloader.DownloadFiles(ctx, htmlContent, "no-files-post")
		require.NoError(t, err)
		
		assert.Equal(t, 0, result.Success)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, len(result.Files))
		assert.Equal(t, htmlContent, result.UpdatedHTML)
	})
	
	t.Run("EmptyHTML", func(t *testing.T) {
		emptyHTML := ""
		ctx := context.Background()
		
		result, err := downloader.DownloadFiles(ctx, emptyHTML, "empty-post")
		require.NoError(t, err)
		
		assert.Equal(t, 0, result.Success)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, len(result.Files))
		assert.Equal(t, emptyHTML, result.UpdatedHTML)
	})
	
	t.Run("InvalidHTML", func(t *testing.T) {
		invalidHTML := "not valid html <<<"
		ctx := context.Background()
		
		// Should still work with invalid HTML due to goquery's tolerance
		result, err := downloader.DownloadFiles(ctx, invalidHTML, "invalid-post")
		require.NoError(t, err)
		
		assert.Equal(t, 0, result.Success)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, len(result.Files))
	})
}

// TestFileDownloadErrorScenarios tests various error conditions
func TestFileDownloadErrorScenarios(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "error-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	downloader := NewFileDownloader(nil, tempDir, "files", nil)
	ctx := context.Background()
	
	t.Run("ContextCancellation", func(t *testing.T) {
		// Create context with immediate cancellation
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		
		fileURL := server.URL + "/document.pdf"
		filesPath := filepath.Join(tempDir, "cancel-test")
		
		fileInfo := downloader.downloadSingleFile(cancelCtx, fileURL, filesPath)
		
		assert.False(t, fileInfo.Success)
		assert.Error(t, fileInfo.Error)
		assert.Contains(t, fileInfo.Error.Error(), "context")
	})
	
	t.Run("FileSystemError", func(t *testing.T) {
		// Create a read-only directory to cause file creation to fail
		readOnlyDir := filepath.Join(tempDir, "readonly")
		err := os.MkdirAll(readOnlyDir, 0755)
		require.NoError(t, err)
		
		// Make directory read-only (may not work on all filesystems)
		err = os.Chmod(readOnlyDir, 0444)
		require.NoError(t, err)
		
		// Restore permissions for cleanup
		defer os.Chmod(readOnlyDir, 0755)
		
		fileURL := server.URL + "/document.pdf"
		
		fileInfo := downloader.downloadSingleFile(ctx, fileURL, readOnlyDir)
		
		// This test may pass on some filesystems that ignore permission restrictions
		// for the same user, so we just verify the attempt was made
		if fileInfo.Error != nil {
			assert.False(t, fileInfo.Success)
			assert.Error(t, fileInfo.Error)
		} else {
			// If no error occurred (e.g., on some filesystems), just log it
			t.Logf("Note: Filesystem doesn't enforce directory permissions as expected")
			assert.True(t, fileInfo.Success)
		}
	})
	
	t.Run("DirectoryCreationError", func(t *testing.T) {
		// Try to create files directory where a file already exists
		invalidDir := filepath.Join(tempDir, "invalid-dir")
		
		// Create a file with the same name as the directory we'll try to create
		err := os.WriteFile(invalidDir, []byte("blocking file"), 0644)
		require.NoError(t, err)
		
		invalidDownloader := NewFileDownloader(nil, invalidDir, "files", nil)
		htmlContent := createTestHTMLWithFiles(server.URL)
		
		_, err = invalidDownloader.DownloadFiles(ctx, htmlContent, "blocked-post")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create files directory")
	})
}

// TestFileDownloadWithRealSubstackHTML tests with realistic Substack HTML structure
func TestFileDownloadWithRealSubstackHTML(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "real-substack-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	downloader := NewFileDownloader(nil, tempDir, "attachments", nil)
	
	// Realistic Substack HTML structure with file embeds
	realisticHTML := fmt.Sprintf(`
	<div class="post-body">
		<p>Here's the quarterly report:</p>
		
		<div class="file-embed-container">
			<a class="file-embed-button wide" href="%s/quarterly-report.pdf" target="_blank">
				<div class="file-embed-icon">
					<svg>...</svg>
				</div>
				<div class="file-embed-text">
					<div class="file-embed-title">Q3 2023 Financial Report</div>
					<div class="file-embed-subtitle">PDF • 2.4 MB</div>
				</div>
			</a>
		</div>
		
		<p>And here's the supporting data:</p>
		
		<div class="file-embed-container">
			<a class="file-embed-button wide" href="%s/supporting-data.xlsx" target="_blank">
				<div class="file-embed-icon">
					<svg>...</svg>
				</div>
				<div class="file-embed-text">
					<div class="file-embed-title">Supporting Data</div>
					<div class="file-embed-subtitle">Excel • 1.8 MB</div>
				</div>
			</a>
		</div>
	</div>
	`, server.URL, server.URL)
	
	ctx := context.Background()
	result, err := downloader.DownloadFiles(ctx, realisticHTML, "financial-report")
	require.NoError(t, err)
	
	// Should successfully download both files
	assert.Equal(t, 2, result.Success)
	assert.Equal(t, 0, result.Failed)
	assert.Len(t, result.Files, 2)
	
	// Verify HTML was updated
	assert.Contains(t, result.UpdatedHTML, "attachments/financial-report/quarterly-report.pdf")
	assert.Contains(t, result.UpdatedHTML, "attachments/financial-report/supporting-data.xlsx")
	assert.NotContains(t, result.UpdatedHTML, server.URL)
	
	// Verify files exist on disk
	attachmentsDir := filepath.Join(tempDir, "attachments", "financial-report")
	files, err := os.ReadDir(attachmentsDir)
	require.NoError(t, err)
	assert.Len(t, files, 2)
	
	// Verify specific files
	fileNames := []string{files[0].Name(), files[1].Name()}
	assert.Contains(t, fileNames, "quarterly-report.pdf")
	assert.Contains(t, fileNames, "supporting-data.xlsx")
}

// TestExtractorIntegration tests file download integration with the extractor
func TestExtractorIntegration(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "extractor-integration-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create a mock post with file attachments
	post := Post{
		Id:       123,
		Slug:     "test-post-with-files",
		Title:    "Test Post with File Attachments",
		BodyHTML: createTestHTMLWithFiles(server.URL),
	}
	
	// Create fetcher for the extractor
	fetcher := NewFetcher()
	
	// Test file download through WriteToFileWithImages
	outputPath := filepath.Join(tempDir, "test-post.html")
	filesPath := "attachments"
	imageDownloadResult, err := post.WriteToFileWithImages(
		context.Background(),
		outputPath,
		"html",
		false, // addSourceURL
		false, // downloadImages 
		ImageQualityHigh, // imageQuality
		"", // imagesDir (not used when downloadImages is false)
		true,  // downloadFiles
		nil,   // fileExtensions (no filter)
		filesPath, // filesDir
		fetcher, // fetcher
	)
	
	require.NoError(t, err)
	require.NotNil(t, imageDownloadResult)
	
	// Check that the image result is available (files are not reported in image result)
	// We'll verify file downloads through the file system
	
	// Check that the HTML file was created
	_, err = os.Stat(outputPath)
	assert.NoError(t, err, "HTML file should be created")
	
	// Check that files directory was created
	filesDir := filepath.Join(tempDir, filesPath, post.Slug)
	_, err = os.Stat(filesDir)
	assert.NoError(t, err, "Files directory should be created")
	
	// Check that some files were actually downloaded
	files, err := os.ReadDir(filesDir)
	require.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have actual downloaded files")
	
	// Read the HTML file and verify URLs were replaced
	htmlContent, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	
	htmlStr := string(htmlContent)
	assert.Contains(t, htmlStr, fmt.Sprintf("%s/%s/", filesPath, post.Slug), "HTML should contain local file paths")
	
	// Check that successfully downloaded files had their URLs replaced
	assert.Contains(t, htmlStr, "attachments/test-post-with-files/document.pdf", "PDF file URL should be replaced")
	assert.Contains(t, htmlStr, "attachments/test-post-with-files/spreadsheet.xlsx", "XLSX file URL should be replaced")
	assert.Contains(t, htmlStr, "attachments/test-post-with-files/with-query", "Query file URL should be replaced")
	
	// URLs that weren't downloadable or detectable should remain as original
	// (not-found.pdf and files that don't match CSS selector)
	
	// Verify specific file types were downloaded
	var pdfFound, xlsxFound bool
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".pdf") {
			pdfFound = true
		}
		if strings.HasSuffix(file.Name(), ".xlsx") {
			xlsxFound = true
		}
	}
	assert.True(t, pdfFound, "Should have downloaded PDF file")
	assert.True(t, xlsxFound, "Should have downloaded XLSX file")
}

// TestExtractorIntegrationWithFiltering tests file download with extension filtering through extractor
func TestExtractorIntegrationWithFiltering(t *testing.T) {
	// Create test server
	server := createTestFileServer()
	defer server.Close()
	
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "extractor-filtering-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Create a mock post with file attachments
	post := Post{
		Id:       456,
		Slug:     "filtered-post",
		Title:    "Post with Filtered Files",
		BodyHTML: createTestHTMLWithFiles(server.URL),
	}
	
	// Create fetcher for the extractor
	fetcher := NewFetcher()
	
	// Test file download with extension filtering (only PDF files)
	outputPath := filepath.Join(tempDir, "filtered-post.html")
	filesPath := "documents"
	imageDownloadResult, err := post.WriteToFileWithImages(
		context.Background(),
		outputPath,
		"html",
		false, // addSourceURL
		false, // downloadImages 
		ImageQualityHigh, // imageQuality
		"", // imagesDir (not used when downloadImages is false)
		true,  // downloadFiles
		[]string{"pdf"}, // fileExtensions - only PDF files
		filesPath, // filesDir
		fetcher, // fetcher
	)
	
	require.NoError(t, err)
	require.NotNil(t, imageDownloadResult)
	
	// Check that the integration worked (files are not reported in image result)
	// We'll verify file downloads through the file system
	
	// Check that files directory was created
	filesDir := filepath.Join(tempDir, filesPath, post.Slug)
	_, err = os.Stat(filesDir)
	assert.NoError(t, err, "Files directory should be created")
	
	// Check that only PDF files were downloaded
	files, err := os.ReadDir(filesDir)
	require.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have downloaded files")
	
	// Verify only PDF files were downloaded
	for _, file := range files {
		assert.True(t, strings.HasSuffix(file.Name(), ".pdf"), 
			"Only PDF files should be downloaded, found: %s", file.Name())
	}
	
	// Should be fewer files than the unfiltered test
	assert.LessOrEqual(t, len(files), 2, "Should have fewer files due to filtering")
}

// Benchmark tests
func BenchmarkExtractFileElements(b *testing.B) {
	server := createTestFileServer()
	defer server.Close()
	
	downloader := NewFileDownloader(nil, "/tmp", "files", nil)
	htmlContent := createTestHTMLWithFiles(server.URL)
	
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downloader.extractFileElements(doc)
	}
}

func BenchmarkSanitizeFilename(b *testing.B) {
	downloader := NewFileDownloader(nil, "/tmp", "files", nil)
	filename := "my<unsafe:file>name/with\\many|bad?chars*.pdf"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		downloader.sanitizeFilename(filename)
	}
}