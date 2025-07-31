package lib

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FileInfo represents information about a downloaded file attachment
type FileInfo struct {
	OriginalURL string
	LocalPath   string
	Filename    string
	Size        int64
	Success     bool
	Error       error
}

// FileDownloader handles downloading file attachments from Substack posts
type FileDownloader struct {
	fetcher        *Fetcher
	outputDir      string
	filesDir       string
	fileExtensions []string // allowed file extensions, empty means all
}

// NewFileDownloader creates a new FileDownloader instance
func NewFileDownloader(fetcher *Fetcher, outputDir, filesDir string, extensions []string) *FileDownloader {
	if fetcher == nil {
		fetcher = NewFetcher()
	}
	return &FileDownloader{
		fetcher:        fetcher,
		outputDir:      outputDir,
		filesDir:       filesDir,
		fileExtensions: extensions,
	}
}

// FileDownloadResult contains the results of downloading file attachments for a post
type FileDownloadResult struct {
	Files       []FileInfo
	UpdatedHTML string
	Success     int
	Failed      int
}

// FileElement represents a file attachment element with its download URL and local path info
type FileElement struct {
	DownloadURL string
	LocalPath   string
	Filename    string
	Success     bool
}

// DownloadFiles downloads all file attachments from a post's HTML content and returns updated HTML
func (fd *FileDownloader) DownloadFiles(ctx context.Context, htmlContent string, postSlug string) (*FileDownloadResult, error) {
	// Parse HTML content
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML content: %w", err)
	}

	// Extract file attachment elements
	fileElements, err := fd.extractFileElements(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to extract file elements: %w", err)
	}

	if len(fileElements) == 0 {
		return &FileDownloadResult{
			Files:       []FileInfo{},
			UpdatedHTML: htmlContent,
			Success:     0,
			Failed:      0,
		}, nil
	}

	// Create files directory
	filesPath := filepath.Join(fd.outputDir, fd.filesDir, postSlug)
	if err := os.MkdirAll(filesPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create files directory: %w", err)
	}

	// Download files and build URL mapping
	var files []FileInfo
	urlToLocalPath := make(map[string]string)

	for _, element := range fileElements {
		// Download the file
		fileInfo := fd.downloadSingleFile(ctx, element.DownloadURL, filesPath)
		files = append(files, fileInfo)

		if fileInfo.Success {
			urlToLocalPath[element.DownloadURL] = fileInfo.LocalPath
		}
	}

	// Update HTML content with local paths
	updatedHTML := fd.updateHTMLWithLocalPaths(htmlContent, urlToLocalPath)

	// Count success/failure
	successCount := 0
	failedCount := 0
	for _, file := range files {
		if file.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	return &FileDownloadResult{
		Files:       files,
		UpdatedHTML: updatedHTML,
		Success:     successCount,
		Failed:      failedCount,
	}, nil
}

// extractFileElements finds all file attachment elements in the HTML using the CSS selector
func (fd *FileDownloader) extractFileElements(doc *goquery.Document) ([]FileElement, error) {
	var elements []FileElement

	doc.Find(".file-embed-button.wide").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}

		// Parse and validate URL
		fileURL, err := url.Parse(href)
		if err != nil {
			return
		}

		// Make sure it's an absolute URL
		if !fileURL.IsAbs() {
			return
		}

		// Extract filename from URL
		filename := fd.extractFilenameFromURL(href)
		if filename == "" {
			// Generate filename if we can't extract one
			filename = fmt.Sprintf("attachment_%d", i+1)
		}

		// Check file extension filter if specified
		if len(fd.fileExtensions) > 0 && !fd.isAllowedExtension(filename) {
			return
		}

		elements = append(elements, FileElement{
			DownloadURL: href,
			Filename:    filename,
		})
	})

	return elements, nil
}

// extractFilenameFromURL attempts to extract a filename from a URL
func (fd *FileDownloader) extractFilenameFromURL(downloadURL string) string {
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return ""
	}

	// Try to get filename from path using URL-safe path handling
	path := parsed.Path
	if path != "" && path != "/" {
		// Use strings.LastIndex to find the last segment in a cross-platform way
		// This avoids issues with filepath.Base on different operating systems
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash >= 0 && lastSlash < len(path)-1 {
			filename := path[lastSlash+1:]
			if filename != "" && filename != "." {
				return filename
			}
		}
	}

	// Try to get filename from query parameters (common in some download links)
	if filename := parsed.Query().Get("filename"); filename != "" {
		return filename
	}

	return ""
}

// isAllowedExtension checks if a filename has an allowed extension
func (fd *FileDownloader) isAllowedExtension(filename string) bool {
	if len(fd.fileExtensions) == 0 {
		return true // Allow all if no filter specified
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" && ext[0] == '.' {
		ext = ext[1:] // Remove the dot
	}

	for _, allowedExt := range fd.fileExtensions {
		if strings.ToLower(allowedExt) == ext {
			return true
		}
	}

	return false
}

// downloadSingleFile downloads a single file and returns FileInfo
func (fd *FileDownloader) downloadSingleFile(ctx context.Context, downloadURL, filesPath string) FileInfo {
	// Extract filename
	filename := fd.extractFilenameFromURL(downloadURL)
	if filename == "" {
		// Generate a safe filename based on URL
		filename = fd.generateSafeFilename(downloadURL)
	}

	// Ensure filename is safe for filesystem
	filename = fd.sanitizeFilename(filename)

	localPath := filepath.Join(filesPath, filename)

	// Check if file already exists
	if _, err := os.Stat(localPath); err == nil {
		return FileInfo{
			OriginalURL: downloadURL,
			LocalPath:   localPath,
			Filename:    filename,
			Size:        0,
			Success:     true,
			Error:       nil,
		}
	}

	// Download the file
	resp, err := fd.fetcher.FetchURL(ctx, downloadURL)
	if err != nil {
		return FileInfo{
			OriginalURL: downloadURL,
			LocalPath:   localPath,
			Filename:    filename,
			Size:        0,
			Success:     false,
			Error:       err,
		}
	}
	defer resp.Close()

	// Create the file
	file, err := os.Create(localPath)
	if err != nil {
		return FileInfo{
			OriginalURL: downloadURL,
			LocalPath:   localPath,
			Filename:    filename,
			Size:        0,
			Success:     false,
			Error:       err,
		}
	}
	defer file.Close()

	// Copy file contents
	size, err := io.Copy(file, resp)
	if err != nil {
		// Remove partially downloaded file
		os.Remove(localPath)
		return FileInfo{
			OriginalURL: downloadURL,
			LocalPath:   localPath,
			Filename:    filename,
			Size:        0,
			Success:     false,
			Error:       err,
		}
	}

	return FileInfo{
		OriginalURL: downloadURL,
		LocalPath:   localPath,
		Filename:    filename,
		Size:        size,
		Success:     true,
		Error:       nil,
	}
}

// generateSafeFilename generates a safe filename from a URL
func (fd *FileDownloader) generateSafeFilename(downloadURL string) string {
	// Use timestamp and hash of URL to create unique filename
	timestamp := time.Now().Unix()
	urlHash := fmt.Sprintf("%x", []byte(downloadURL))[:8]
	return fmt.Sprintf("file_%d_%s", timestamp, urlHash)
}

// sanitizeFilename removes or replaces unsafe characters in filenames
func (fd *FileDownloader) sanitizeFilename(filename string) string {
	// Replace unsafe characters with underscores
	unsafe := regexp.MustCompile(`[<>:"/\\|?*]`)
	safe := unsafe.ReplaceAllString(filename, "_")
	
	// Remove leading/trailing spaces and dots
	safe = strings.Trim(safe, " .")
	
	// Ensure it's not empty
	if safe == "" {
		safe = "attachment"
	}
	
	// Limit length
	if len(safe) > 200 {
		safe = safe[:200]
	}
	
	return safe
}

// updateHTMLWithLocalPaths updates the HTML content to reference local file paths
func (fd *FileDownloader) updateHTMLWithLocalPaths(htmlContent string, urlToLocalPath map[string]string) string {
	updatedHTML := htmlContent

	for originalURL, localPath := range urlToLocalPath {
		// Convert absolute local path to relative path from the post file location
		relativePath := fd.makeRelativePath(localPath)
		
		// Replace the href attribute in file-embed-button links
		oldPattern := fmt.Sprintf(`href="%s"`, regexp.QuoteMeta(originalURL))
		newPattern := fmt.Sprintf(`href="%s"`, relativePath)
		updatedHTML = regexp.MustCompile(oldPattern).ReplaceAllString(updatedHTML, newPattern)
		
		// Also handle single quotes
		oldPatternSingle := fmt.Sprintf(`href='%s'`, regexp.QuoteMeta(originalURL))
		newPatternSingle := fmt.Sprintf(`href='%s'`, relativePath)
		updatedHTML = regexp.MustCompile(oldPatternSingle).ReplaceAllString(updatedHTML, newPatternSingle)
	}

	return updatedHTML
}

// makeRelativePath converts an absolute local path to a relative path from the post location
func (fd *FileDownloader) makeRelativePath(localPath string) string {
	// Get the relative path from the output directory
	relPath, err := filepath.Rel(fd.outputDir, localPath)
	if err != nil {
		// If we can't make it relative, just use the filename
		return filepath.Base(localPath)
	}
	
	// Convert to forward slashes for web compatibility
	return filepath.ToSlash(relPath)
}