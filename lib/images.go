package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ImageQuality represents the quality level for image downloads
type ImageQuality string

const (
	ImageQualityHigh   ImageQuality = "high"   // 1456w
	ImageQualityMedium ImageQuality = "medium" // 848w
	ImageQualityLow    ImageQuality = "low"    // 424w
)

// ImageInfo contains information about a downloaded image
type ImageInfo struct {
	OriginalURL string
	LocalPath   string
	Width       int
	Height      int
	Format      string
	Success     bool
	Error       error
}

// ImageDownloader handles downloading and processing images from Substack posts
type ImageDownloader struct {
	fetcher      *Fetcher
	outputDir    string
	imagesDir    string
	imageQuality ImageQuality
}

// NewImageDownloader creates a new ImageDownloader instance
func NewImageDownloader(fetcher *Fetcher, outputDir, imagesDir string, quality ImageQuality) *ImageDownloader {
	if fetcher == nil {
		fetcher = NewFetcher()
	}
	return &ImageDownloader{
		fetcher:      fetcher,
		outputDir:    outputDir,
		imagesDir:    imagesDir,
		imageQuality: quality,
	}
}

// ImageDownloadResult contains the results of downloading images for a post
type ImageDownloadResult struct {
	Images      []ImageInfo
	UpdatedHTML string
	Success     int
	Failed      int
}

// DownloadImages downloads all images from a post's HTML content and returns updated HTML
func (id *ImageDownloader) DownloadImages(ctx context.Context, htmlContent string, postSlug string) (*ImageDownloadResult, error) {
	// Parse HTML content
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML content: %w", err)
	}

	// Extract image URLs
	imageURLs, err := id.extractImageURLs(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to extract image URLs: %w", err)
	}

	if len(imageURLs) == 0 {
		return &ImageDownloadResult{
			Images:      []ImageInfo{},
			UpdatedHTML: htmlContent,
			Success:     0,
			Failed:      0,
		}, nil
	}

	// Create images directory
	imagesPath := filepath.Join(id.outputDir, id.imagesDir, postSlug)
	if err := os.MkdirAll(imagesPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create images directory: %w", err)
	}

	// Download images
	var images []ImageInfo
	urlToLocalPath := make(map[string]string)

	for _, imageURL := range imageURLs {
		imageInfo := id.downloadSingleImage(ctx, imageURL, imagesPath)
		images = append(images, imageInfo)

		if imageInfo.Success {
			// Store mapping for URL replacement
			urlToLocalPath[imageInfo.OriginalURL] = imageInfo.LocalPath
		}
	}

	// Update HTML content with local paths
	updatedHTML := id.updateHTMLWithLocalPaths(htmlContent, urlToLocalPath)

	// Count success/failure
	success := 0
	failed := 0
	for _, img := range images {
		if img.Success {
			success++
		} else {
			failed++
		}
	}

	return &ImageDownloadResult{
		Images:      images,
		UpdatedHTML: updatedHTML,
		Success:     success,
		Failed:      failed,
	}, nil
}

// extractImageURLs extracts image URLs from HTML content
func (id *ImageDownloader) extractImageURLs(doc *goquery.Document) ([]string, error) {
	var imageURLs []string
	urlSet := make(map[string]bool) // To avoid duplicates

	// Find all img tags
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		// Get the best quality URL based on user preference
		imageURL := id.getBestImageURL(s)
		if imageURL != "" && !urlSet[imageURL] {
			imageURLs = append(imageURLs, imageURL)
			urlSet[imageURL] = true
		}
	})

	return imageURLs, nil
}

// getBestImageURL extracts the best quality image URL from an img element
func (id *ImageDownloader) getBestImageURL(imgElement *goquery.Selection) string {
	// First try to get URL from data-attrs JSON
	dataAttrs, exists := imgElement.Attr("data-attrs")
	if exists {
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(dataAttrs), &attrs); err == nil {
			if src, ok := attrs["src"].(string); ok && src != "" {
				return src
			}
		}
	}

	// Get target width based on quality preference
	targetWidth := id.getTargetWidth()

	// Try to get URL from srcset based on quality preference
	srcset, exists := imgElement.Attr("srcset")
	if exists {
		if url := id.extractURLFromSrcset(srcset, targetWidth); url != "" {
			return url
		}
	}

	// Fallback to src attribute
	src, exists := imgElement.Attr("src")
	if exists {
		return src
	}

	return ""
}

// getTargetWidth returns the target width based on image quality preference
func (id *ImageDownloader) getTargetWidth() int {
	switch id.imageQuality {
	case ImageQualityHigh:
		return 1456
	case ImageQualityMedium:
		return 848
	case ImageQualityLow:
		return 424
	default:
		return 1456
	}
}

// extractURLFromSrcset extracts the URL with the target width from a srcset attribute
func (id *ImageDownloader) extractURLFromSrcset(srcset string, targetWidth int) string {
	// Split srcset into individual entries
	entries := strings.Split(srcset, ",")
	
	var bestURL string
	var bestWidth int

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		
		// Parse "URL WIDTHw" format
		parts := strings.Split(entry, " ")
		if len(parts) >= 2 {
			url := parts[0]
			widthStr := strings.TrimSuffix(parts[1], "w")
			
			if width, err := strconv.Atoi(widthStr); err == nil {
				// Find the closest width to our target, preferring exact matches or higher
				if width == targetWidth || (bestURL == "" || 
					(width >= targetWidth && (bestWidth < targetWidth || width < bestWidth)) ||
					(width < targetWidth && bestWidth < targetWidth && width > bestWidth)) {
					bestURL = url
					bestWidth = width
				}
			}
		}
	}

	return bestURL
}

// downloadSingleImage downloads a single image and returns its info
func (id *ImageDownloader) downloadSingleImage(ctx context.Context, imageURL, imagesPath string) ImageInfo {
	imageInfo := ImageInfo{
		OriginalURL: imageURL,
		Success:     false,
	}

	// Generate safe filename
	filename, err := id.generateSafeFilename(imageURL)
	if err != nil {
		imageInfo.Error = fmt.Errorf("failed to generate filename: %w", err)
		return imageInfo
	}

	localPath := filepath.Join(imagesPath, filename)
	imageInfo.LocalPath = localPath

	// Download the image
	body, err := id.fetcher.FetchURL(ctx, imageURL)
	if err != nil {
		imageInfo.Error = fmt.Errorf("failed to fetch image: %w", err)
		return imageInfo
	}
	defer body.Close()

	// Create the local file
	file, err := os.Create(localPath)
	if err != nil {
		imageInfo.Error = fmt.Errorf("failed to create local file: %w", err)
		return imageInfo
	}
	defer file.Close()

	// Copy image data
	_, err = io.Copy(file, body)
	if err != nil {
		imageInfo.Error = fmt.Errorf("failed to write image data: %w", err)
		os.Remove(localPath) // Clean up failed file
		return imageInfo
	}

	// Extract image metadata
	imageInfo.Format = id.getImageFormat(filename)
	imageInfo.Width, imageInfo.Height = id.extractDimensionsFromURL(imageURL)

	imageInfo.Success = true
	return imageInfo
}

// generateSafeFilename generates a safe filename from an image URL
func (id *ImageDownloader) generateSafeFilename(imageURL string) (string, error) {
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return "", err
	}

	// Extract filename from URL path
	filename := filepath.Base(parsedURL.Path)
	
	// If no valid filename, generate one from URL patterns
	if filename == "" || filename == "/" || filename == "." {
		filename = "" // Reset to force fallback logic
		
		// Try to extract from the URL patterns
		if strings.Contains(imageURL, "substack") {
			// Try to extract the image ID from Substack URLs
			if match := regexp.MustCompile(`([a-f0-9-]{36})_(\d+x\d+)\.(jpeg|jpg|png|webp)`).FindStringSubmatch(imageURL); len(match) > 0 {
				filename = fmt.Sprintf("%s_%s.%s", match[1][:8], match[2], match[3])
			}
		}
		
		// If still no filename, use default
		if filename == "" {
			filename = "image.jpg"
		}
	}

	// Clean filename - remove invalid characters (but preserve structure)
	// Only replace invalid filesystem characters
	cleanedFilename := regexp.MustCompile(`[<>:"/\\|?*]`).ReplaceAllString(filename, "_")
	
	// Ensure we have a valid filename after cleaning
	if cleanedFilename == "" || cleanedFilename == "_" || cleanedFilename == "__" {
		cleanedFilename = "image.jpg"
	}
	
	// Ensure filename is not too long
	if len(cleanedFilename) > 200 {
		ext := filepath.Ext(cleanedFilename)
		name := strings.TrimSuffix(cleanedFilename, ext)
		if len(ext) < 200 {
			cleanedFilename = name[:200-len(ext)] + ext
		} else {
			cleanedFilename = "image.jpg"
		}
	}

	return cleanedFilename, nil
}

// getImageFormat determines image format from filename
func (id *ImageDownloader) getImageFormat(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	case ".webp":
		return "webp"
	case ".gif":
		return "gif"
	default:
		return "unknown"
	}
}

// extractDimensionsFromURL attempts to extract width and height from URL
func (id *ImageDownloader) extractDimensionsFromURL(imageURL string) (int, int) {
	// Look for patterns like "1456x819" or "w_1456,h_819"
	if match := regexp.MustCompile(`(\d+)x(\d+)`).FindStringSubmatch(imageURL); len(match) >= 3 {
		width, _ := strconv.Atoi(match[1])
		height, _ := strconv.Atoi(match[2])
		return width, height
	}

	if match := regexp.MustCompile(`w_(\d+)`).FindStringSubmatch(imageURL); len(match) >= 2 {
		width, _ := strconv.Atoi(match[1])
		return width, 0 // Height unknown
	}

	return 0, 0
}

// updateHTMLWithLocalPaths replaces image URLs in HTML with local paths
func (id *ImageDownloader) updateHTMLWithLocalPaths(htmlContent string, urlToLocalPath map[string]string) string {
	updatedHTML := htmlContent

	for originalURL, localPath := range urlToLocalPath {
		// Convert absolute local path to relative path from output directory
		relPath, err := filepath.Rel(id.outputDir, localPath)
		if err != nil {
			relPath = localPath // fallback to absolute path
		}

		// Always ensure forward slashes for HTML (web standard)
		// Convert any backslashes to forward slashes regardless of platform
		relPath = strings.ReplaceAll(relPath, "\\", "/")

		// Replace URL in various contexts
		updatedHTML = strings.ReplaceAll(updatedHTML, originalURL, relPath)
		
		// Also replace URL-encoded versions
		encodedURL := url.QueryEscape(originalURL)
		if encodedURL != originalURL {
			updatedHTML = strings.ReplaceAll(updatedHTML, encodedURL, relPath)
		}
	}

	return updatedHTML
}