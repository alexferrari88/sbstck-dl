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

// ImageElement represents an image element with all its URLs
type ImageElement struct {
	BestURL    string   // The URL to download (highest quality)
	AllURLs    []string // All URLs that should be replaced with the local path
	LocalPath  string   // Local path after download
	Success    bool     // Whether download was successful
}

// DownloadImages downloads all images from a post's HTML content and returns updated HTML
func (id *ImageDownloader) DownloadImages(ctx context.Context, htmlContent string, postSlug string) (*ImageDownloadResult, error) {
	// Parse HTML content
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML content: %w", err)
	}

	// Extract image elements with all their URLs
	imageElements, err := id.extractImageElements(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to extract image elements: %w", err)
	}

	if len(imageElements) == 0 {
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

	// Download images and build URL mapping
	var images []ImageInfo
	urlToLocalPath := make(map[string]string)

	for _, element := range imageElements {
		// Download the best quality URL
		imageInfo := id.downloadSingleImage(ctx, element.BestURL, imagesPath)
		images = append(images, imageInfo)

		if imageInfo.Success {
			// Map ALL URLs for this image element to the same local path
			for _, url := range element.AllURLs {
				urlToLocalPath[url] = imageInfo.LocalPath
			}
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

// extractImageElements extracts image elements with all their URLs from HTML content
func (id *ImageDownloader) extractImageElements(doc *goquery.Document) ([]ImageElement, error) {
	var imageElements []ImageElement
	seenBestURLs := make(map[string]bool) // To avoid duplicates based on best URL
	allURLsToCollect := make(map[string][]string) // Map from best URL to all URLs that should map to it

	// Find all img tags and collect their URLs
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		element := id.getImageElementInfo(s)
		if element.BestURL != "" && !seenBestURLs[element.BestURL] {
			allURLsToCollect[element.BestURL] = element.AllURLs
			imageElements = append(imageElements, element)
			seenBestURLs[element.BestURL] = true
		}
	})

	// Also collect URLs from <a> tags that link to images
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists && id.isImageURL(href) {
			// Find the corresponding image element to add this URL to
			for bestURL, urls := range allURLsToCollect {
				if id.isSameImage(href, bestURL) {
					// Add this href URL to the list of URLs to replace
					urlExists := false
					for _, existingURL := range urls {
						if existingURL == href {
							urlExists = true
							break
						}
					}
					if !urlExists {
						allURLsToCollect[bestURL] = append(urls, href)
						// Update the corresponding element in imageElements
						for j, elem := range imageElements {
							if elem.BestURL == bestURL {
								imageElements[j].AllURLs = allURLsToCollect[bestURL]
								break
							}
						}
					}
					break
				}
			}
		}
	})

	// Also collect URLs from <source> tags (in <picture> elements)
	doc.Find("source").Each(func(i int, s *goquery.Selection) {
		if srcset, exists := s.Attr("srcset"); exists {
			srcsetURLs := id.extractAllURLsFromSrcset(srcset)
			for _, srcsetURL := range srcsetURLs {
				if id.isImageURL(srcsetURL) {
					// Find the corresponding image element to add this URL to
					for bestURL, urls := range allURLsToCollect {
						if id.isSameImage(srcsetURL, bestURL) {
							// Add this srcset URL to the list of URLs to replace
							urlExists := false
							for _, existingURL := range urls {
								if existingURL == srcsetURL {
									urlExists = true
									break
								}
							}
							if !urlExists {
								allURLsToCollect[bestURL] = append(urls, srcsetURL)
								// Update the corresponding element in imageElements
								for j, elem := range imageElements {
									if elem.BestURL == bestURL {
										imageElements[j].AllURLs = allURLsToCollect[bestURL]
										break
									}
								}
							}
							break
						}
					}
				}
			}
		}
	})

	return imageElements, nil
}

// extractImageURLs extracts image URLs from HTML content (kept for backward compatibility with tests)
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

// getImageElementInfo extracts all URLs and determines the best one for an img element
func (id *ImageDownloader) getImageElementInfo(imgElement *goquery.Selection) ImageElement {
	var allURLs []string
	urlSet := make(map[string]bool) // To avoid duplicates
	
	// Helper function to add unique URLs
	addURL := func(url string) {
		if url != "" && !urlSet[url] {
			allURLs = append(allURLs, url)
			urlSet[url] = true
		}
	}
	
	// 1. Get URL from data-attrs JSON (highest priority)
	if dataAttrs, exists := imgElement.Attr("data-attrs"); exists {
		var attrs map[string]interface{}
		if err := json.Unmarshal([]byte(dataAttrs), &attrs); err == nil {
			if src, ok := attrs["src"].(string); ok && src != "" {
				addURL(src)
			}
		}
	}
	
	// 2. Get URLs from srcset attribute
	if srcset, exists := imgElement.Attr("srcset"); exists {
		srcsetURLs := id.extractAllURLsFromSrcset(srcset)
		for _, url := range srcsetURLs {
			addURL(url)
		}
	}
	
	// 3. Get URL from src attribute
	if src, exists := imgElement.Attr("src"); exists {
		addURL(src)
	}
	
	// Determine the best URL to download
	bestURL := id.getBestImageURL(imgElement)
	
	return ImageElement{
		BestURL: bestURL,
		AllURLs: allURLs,
	}
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

// extractAllURLsFromSrcset extracts all URLs from a srcset attribute
func (id *ImageDownloader) extractAllURLsFromSrcset(srcset string) []string {
	if srcset == "" {
		return []string{} // Return empty slice instead of nil
	}
	
	var urls []string
	
	// Use the same robust parsing as updateSrcsetAttribute
	entries := id.parseSrcsetEntries(srcset)
	
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		
		// Parse "URL WIDTHw" format
		parts := strings.Fields(entry)
		if len(parts) >= 1 {
			url := parts[0]
			// Only include if it looks like a valid URL (not a fragment like "f_webp")
			if url != "" && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
				urls = append(urls, url)
			}
		}
	}
	
	if urls == nil {
		return []string{} // Ensure we never return nil
	}
	
	return urls
}

// extractURLFromSrcset extracts the URL with the target width from a srcset attribute
func (id *ImageDownloader) extractURLFromSrcset(srcset string, targetWidth int) string {
	// Use the robust parsing to handle URLs with commas
	entries := id.parseSrcsetEntries(srcset)
	
	var bestURL string
	var bestWidth int

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		
		// Parse "URL WIDTHw" format
		parts := strings.Fields(entry)
		if len(parts) >= 2 {
			url := parts[0]
			widthStr := strings.TrimSuffix(parts[1], "w")
			
			// Only process if it looks like a valid URL
			if url != "" && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
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
	// Parse HTML content
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		// Fallback to simple string replacement if parsing fails
		return id.updateHTMLWithStringReplacement(htmlContent, urlToLocalPath)
	}

	// Create URL to relative path mapping
	urlToRelPath := make(map[string]string)
	for originalURL, localPath := range urlToLocalPath {
		// Convert absolute local path to relative path from output directory
		relPath, err := filepath.Rel(id.outputDir, localPath)
		if err != nil {
			relPath = localPath // fallback to absolute path
		}
		// Always ensure forward slashes for HTML (web standard)
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		urlToRelPath[originalURL] = relPath
	}

	// Update img elements
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		// Update src attribute
		if src, exists := s.Attr("src"); exists {
			if relPath, found := urlToRelPath[src]; found {
				s.SetAttr("src", relPath)
			}
		}

		// Update srcset attribute
		if srcset, exists := s.Attr("srcset"); exists {
			updatedSrcset := id.updateSrcsetAttribute(srcset, urlToRelPath)
			s.SetAttr("srcset", updatedSrcset)
		}

		// Update data-attrs JSON
		if dataAttrs, exists := s.Attr("data-attrs"); exists {
			updatedDataAttrs := id.updateDataAttrsJSON(dataAttrs, urlToRelPath)
			s.SetAttr("data-attrs", updatedDataAttrs)
		}
	})

	// Update anchor elements with image links
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists {
			if relPath, found := urlToRelPath[href]; found {
				s.SetAttr("href", relPath)
			}
		}
	})

	// Update source elements (in picture tags)
	doc.Find("source").Each(func(i int, s *goquery.Selection) {
		if srcset, exists := s.Attr("srcset"); exists {
			updatedSrcset := id.updateSrcsetAttribute(srcset, urlToRelPath)
			s.SetAttr("srcset", updatedSrcset)
		}
	})

	// Get the updated HTML
	html, err := doc.Html()
	if err != nil {
		// Fallback to simple string replacement if HTML generation fails
		return id.updateHTMLWithStringReplacement(htmlContent, urlToLocalPath)
	}

	return html
}

// updateHTMLWithStringReplacement is the fallback method using simple string replacement
func (id *ImageDownloader) updateHTMLWithStringReplacement(htmlContent string, urlToLocalPath map[string]string) string {
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

// updateSrcsetAttribute updates URLs in a srcset attribute
func (id *ImageDownloader) updateSrcsetAttribute(srcset string, urlToRelPath map[string]string) string {
	if srcset == "" {
		return srcset
	}

	// Parse srcset more carefully to handle URLs with commas
	entries := id.parseSrcsetEntries(srcset)
	
	// Map to track unique local paths and their best width descriptor
	pathToEntry := make(map[string]string)
	
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Parse "URL WIDTH" format
		parts := strings.Fields(entry)
		if len(parts) >= 1 {
			url := parts[0]
			// Replace URL if we have a mapping for it
			if relPath, found := urlToRelPath[url]; found {
				// Build the new entry with local path
				var newEntry string
				if len(parts) >= 2 {
					// Has width descriptor
					newEntry = relPath + " " + parts[1]
				} else {
					// No width descriptor
					newEntry = relPath
				}
				
				// Only keep one entry per unique local path
				// If we already have an entry for this path, keep the one with width descriptor
				if existingEntry, exists := pathToEntry[relPath]; exists {
					// Prefer entries with width descriptors
					if len(parts) >= 2 && !strings.Contains(existingEntry, " ") {
						pathToEntry[relPath] = newEntry
					}
					// If both have width descriptors or both don't, keep the first one
				} else {
					pathToEntry[relPath] = newEntry
				}
			} else {
				// URL wasn't mapped, keep original entry
				pathToEntry[url] = entry
			}
		}
	}

	// Convert map back to slice, maintaining order as much as possible
	var updatedEntries []string
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		
		parts := strings.Fields(entry)
		if len(parts) >= 1 {
			url := parts[0]
			if relPath, found := urlToRelPath[url]; found {
				// Use the entry from our deduplication map
				if finalEntry, exists := pathToEntry[relPath]; exists {
					updatedEntries = append(updatedEntries, finalEntry)
					delete(pathToEntry, relPath) // Remove to avoid duplicates
				}
			} else {
				// Original URL, use as-is
				if finalEntry, exists := pathToEntry[url]; exists {
					updatedEntries = append(updatedEntries, finalEntry)
					delete(pathToEntry, url)
				}
			}
		}
	}

	return strings.Join(updatedEntries, ", ")
}

// isImageURL checks if a URL appears to be an image URL (Substack CDN or S3)
func (id *ImageDownloader) isImageURL(url string) bool {
	return strings.Contains(url, "substackcdn.com") || 
		   strings.Contains(url, "substack-post-media.s3.amazonaws.com") ||
		   strings.Contains(url, "bucketeer-") // Some Substack images use bucketeer URLs
}

// isSameImage checks if two URLs refer to the same image by comparing the core image identifier
func (id *ImageDownloader) isSameImage(url1, url2 string) bool {
	// Extract the UUID pattern from both URLs
	uuidPattern := regexp.MustCompile(`([a-f0-9-]{36})`)
	
	matches1 := uuidPattern.FindStringSubmatch(url1)
	matches2 := uuidPattern.FindStringSubmatch(url2) 
	
	if len(matches1) > 0 && len(matches2) > 0 {
		return matches1[1] == matches2[1]
	}
	
	// Fallback: if we can't find UUIDs, check if the URLs contain similar image identifiers
	// This handles cases where the URL structure might vary
	return strings.Contains(url1, extractImageID(url2)) || strings.Contains(url2, extractImageID(url1))
}

// extractImageID extracts a unique identifier from an image URL
func extractImageID(url string) string {
	// Try to extract UUID first
	if match := regexp.MustCompile(`([a-f0-9-]{36})`).FindStringSubmatch(url); len(match) > 0 {
		return match[1]
	}
	
	// Fallback to extracting a filename-like pattern
	if match := regexp.MustCompile(`/([^/]+)\.(jpeg|jpg|png|webp|heic|gif)(?:\?|$)`).FindStringSubmatch(url); len(match) > 0 {
		return match[1]
	}
	
	return ""
}

// parseSrcsetEntries carefully parses srcset entries, handling URLs that contain commas
func (id *ImageDownloader) parseSrcsetEntries(srcset string) []string {
	var entries []string
	
	// Use regex to find URLs followed by width descriptors
	// This pattern matches: (URL) (WIDTH)w where URL can contain commas
	pattern := regexp.MustCompile(`(https?://[^\s]+)\s+(\d+w)`)
	matches := pattern.FindAllStringSubmatch(srcset, -1)
	
	for _, match := range matches {
		if len(match) >= 3 {
			url := match[1]
			width := match[2]
			entries = append(entries, url+" "+width)
		}
	}
	
	// If regex parsing didn't find anything, fall back to simple comma splitting
	// but only for URLs that don't contain commas
	if len(entries) == 0 {
		parts := strings.Split(srcset, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				// Only include if it looks like a proper entry (URL + width or just URL)
				fields := strings.Fields(part)
				if len(fields) >= 1 && (strings.HasPrefix(fields[0], "http://") || strings.HasPrefix(fields[0], "https://")) {
					entries = append(entries, part)
				}
			}
		}
	}
	
	return entries
}

// updateDataAttrsJSON updates URLs in a data-attrs JSON string
func (id *ImageDownloader) updateDataAttrsJSON(dataAttrs string, urlToRelPath map[string]string) string {
	if dataAttrs == "" {
		return dataAttrs
	}

	var attrs map[string]interface{}
	if err := json.Unmarshal([]byte(dataAttrs), &attrs); err != nil {
		return dataAttrs // Return original if parsing fails
	}

	// Update src field if it exists
	if src, ok := attrs["src"].(string); ok {
		if relPath, found := urlToRelPath[src]; found {
			attrs["src"] = relPath
		}
	}

	// Marshal back to JSON
	updatedJSON, err := json.Marshal(attrs)
	if err != nil {
		return dataAttrs // Return original if marshaling fails
	}

	return string(updatedJSON)
}