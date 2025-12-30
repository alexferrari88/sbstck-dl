package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var (
	downloadUrl    string
	format         string
	outputFolder   string
	dryRun         bool
	addSourceURL   bool
	downloadImages bool
	imageQuality   string
	imagesDir      string
	downloadFiles  bool
	fileExtensions string
	filesDir       string
	createArchive  bool
	forceDownload  bool
	skipExisting   bool
	refreshUpdated bool
	layout         string
	writeMetadata  bool
	failFast       bool
	continueOnErr  bool
	failedURLsFile = "failed-urls.txt"
	downloadCmd    = &cobra.Command{
		Use:   "download",
		Short: "Download individual posts or the entire public archive",
		Long:  `You can provide the url of a single post or the main url of the Substack you want to download.`,
		Run: func(cmd *cobra.Command, args []string) {
			if _, err := runDownload(ctx, nil, true); err != nil {
				log.Fatalln(err)
			}
		},
	}
)

func init() {
	downloadCmd.Flags().StringVarP(&downloadUrl, "url", "u", "", "Specify the Substack url")
	downloadCmd.Flags().StringVarP(&format, "format", "f", "html", "Specify the output format (options: \"html\", \"md\", \"obsidian-md\", \"txt\"")
	downloadCmd.Flags().StringVarP(&outputFolder, "output", "o", ".", "Specify the download directory")
	downloadCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Enable dry run")
	downloadCmd.Flags().BoolVar(&addSourceURL, "add-source-url", false, "Add the original post URL at the end of the downloaded file")
	downloadCmd.Flags().BoolVar(&downloadImages, "download-images", false, "Download images locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&imageQuality, "image-quality", "high", "Image quality to download (options: \"high\", \"medium\", \"low\")")
	downloadCmd.Flags().StringVar(&imagesDir, "images-dir", "images", "Directory name for downloaded images")
	downloadCmd.Flags().BoolVar(&downloadFiles, "download-files", false, "Download file attachments locally and update content to reference local files")
	downloadCmd.Flags().StringVar(&fileExtensions, "file-extensions", "", "Comma-separated list of file extensions to download (e.g., 'pdf,docx,txt'). If empty, downloads all file types")
	downloadCmd.Flags().StringVar(&filesDir, "files-dir", "files", "Directory name for downloaded file attachments")
	downloadCmd.Flags().BoolVar(&createArchive, "create-archive", false, "Create an archive index page linking all downloaded posts")
	downloadCmd.Flags().BoolVar(&forceDownload, "force", false, "Redownload posts even if they already exist")
	downloadCmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip existing posts (default for archive downloads)")
	downloadCmd.Flags().BoolVar(&refreshUpdated, "refresh-updated", false, "Refresh posts when sitemap lastmod is newer than the manifest")
	downloadCmd.Flags().StringVar(&layout, "layout", "flat", "Output layout (flat, year/month, year/slug)")
	downloadCmd.Flags().BoolVar(&writeMetadata, "write-metadata", false, "Write a JSON sidecar with post metadata")
	downloadCmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop the download on the first error")
	downloadCmd.Flags().BoolVar(&continueOnErr, "continue-on-error", false, "Continue downloading after errors (default)")
	downloadCmd.MarkFlagsMutuallyExclusive("force", "skip-existing")
	downloadCmd.MarkFlagsMutuallyExclusive("fail-fast", "continue-on-error")
	downloadCmd.MarkFlagRequired("url")
}

type downloadSummary struct {
	Downloaded int
	Skipped    int
	Failed     int
	Mode       string
}

func logSummary(summary downloadSummary) {
	if logFormat == logFormatJSON {
		logEvent("download.summary", map[string]any{
			"mode":       summary.Mode,
			"downloaded": summary.Downloaded,
			"skipped":    summary.Skipped,
			"failed":     summary.Failed,
		})
		return
	}
	fmt.Printf("Summary: downloaded=%d skipped=%d failed=%d\n", summary.Downloaded, summary.Skipped, summary.Failed)
}

func writeFailedURLs(outputFolder string, urls []string) (string, error) {
	if len(urls) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(outputFolder, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(outputFolder, failedURLsFile)
	content := strings.Join(urls, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return path, err
	}

	return path, nil
}

type postMetadata struct {
	Title        string   `json:"title"`
	Subtitle     string   `json:"subtitle,omitempty"`
	Description  string   `json:"description,omitempty"`
	Slug         string   `json:"slug"`
	CanonicalURL string   `json:"canonical_url"`
	PostDate     string   `json:"post_date"`
	DownloadedAt string   `json:"downloaded_at"`
	WordCount    int      `json:"word_count,omitempty"`
	CoverImage   string   `json:"cover_image,omitempty"`
	OutputPath   string   `json:"output_path"`
	Format       string   `json:"format"`
	LastModified string   `json:"last_modified,omitempty"`
	NotionLinks  []string `json:"notion_links"`
}

func writeMetadataSidecar(post lib.Post, outputPath string, outputFolder string, format string, downloadedAt time.Time, lastModified string) {
	relPath := outputPath
	if outputFolder != "" {
		if rel, err := filepath.Rel(outputFolder, outputPath); err == nil {
			relPath = filepath.ToSlash(rel)
		}
	}

	meta := postMetadata{
		Title:        post.Title,
		Subtitle:     post.Subtitle,
		Description:  post.Description,
		Slug:         post.Slug,
		CanonicalURL: post.CanonicalUrl,
		PostDate:     post.PostDate,
		DownloadedAt: downloadedAt.UTC().Format(time.RFC3339),
		WordCount:    post.WordCount,
		CoverImage:   post.CoverImage,
		OutputPath:   relPath,
		Format:       format,
		LastModified: lastModified,
		NotionLinks:  lib.ExtractNotionLinks(post.BodyHTML, "html"),
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		if logFormat == logFormatJSON {
			logEvent("metadata.write_failed", map[string]any{
				"path":  outputPath,
				"error": err.Error(),
			})
		} else {
			log.Printf("Error building metadata for %s: %v\n", outputPath, err)
		}
		return
	}

	path := sidecarPath(outputPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		if logFormat == logFormatJSON {
			logEvent("metadata.write_failed", map[string]any{
				"path":  path,
				"error": err.Error(),
			})
		} else {
			log.Printf("Error creating metadata directory: %v\n", err)
		}
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		if logFormat == logFormatJSON {
			logEvent("metadata.write_failed", map[string]any{
				"path":  path,
				"error": err.Error(),
			})
		} else {
			log.Printf("Error writing metadata file %s: %v\n", path, err)
		}
		return
	}

	logEvent("metadata.written", map[string]any{
		"path": path,
	})
}

func sidecarPath(outputPath string) string {
	ext := filepath.Ext(outputPath)
	if ext == "" {
		return outputPath + ".json"
	}
	return strings.TrimSuffix(outputPath, ext) + ".json"
}

func readMetadataSidecar(outputPath string) (postMetadata, bool) {
	path := sidecarPath(outputPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return postMetadata{}, false
	}
	var meta postMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return postMetadata{}, false
	}
	return meta, true
}

func normalizeLayout(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "flat"
	}
	return trimmed
}

func isValidLayout(value string) bool {
	switch value {
	case "flat", "year/month", "year/slug":
		return true
	default:
		return false
	}
}

func entriesToURLs(entries []lib.SitemapEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}
	return urls
}

func entriesToLastMod(entries []lib.SitemapEntry) map[string]string {
	lastModByURL := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.URL == "" {
			continue
		}
		lastModByURL[entry.URL] = entry.LastMod
	}
	return lastModByURL
}

func filterEntriesForDownload(entries []lib.SitemapEntry, outputFolder string, format string, manifest *lib.Manifest, refreshUpdated bool) ([]lib.SitemapEntry, int, int, error) {
	var firstErr error
	slugIndex, err := indexExistingSlugs(outputFolder, format)
	if err != nil {
		firstErr = err
		slugIndex = map[string]struct{}{}
	}

	filtered := make([]lib.SitemapEntry, 0, len(entries))
	skipped := 0
	refreshed := 0

	for _, entry := range entries {
		if manifest != nil {
			if manifestEntry, ok := manifest.Entries[entry.URL]; ok {
				if manifestEntry.Format == "" || manifestEntry.Format == format {
					if manifestEntryExists(manifestEntry, outputFolder) {
						if refreshUpdated && isEntryUpdated(manifestEntry, entry.LastMod) {
							filtered = append(filtered, entry)
							refreshed++
							continue
						}
						skipped++
						continue
					}
				}
			}
		}

		slug := extractSlug(entry.URL)
		if _, exists := slugIndex[slug]; exists {
			skipped++
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered, skipped, refreshed, firstErr
}

func manifestEntryExists(entry lib.ManifestEntry, outputFolder string) bool {
	entryPath := filepath.FromSlash(entry.FilePath)
	if entryPath == "" {
		return false
	}
	if !filepath.IsAbs(entryPath) {
		entryPath = filepath.Join(outputFolder, entryPath)
	}
	if _, err := os.Stat(entryPath); err == nil {
		return true
	}
	return false
}

func isEntryUpdated(entry lib.ManifestEntry, lastMod string) bool {
	lastModified, ok := parseDateInput(lastMod)
	if !ok {
		return false
	}

	if entry.LastModified != "" {
		if previous, ok := parseDateInput(entry.LastModified); ok {
			return lastModified.After(previous)
		}
	}

	if entry.DownloadedAt != "" {
		if previous, ok := parseDateInput(entry.DownloadedAt); ok {
			return lastModified.After(previous)
		}
	}

	return false
}

func convertDateTime(datetime string) string {
	// Parse the datetime string
	parsedTime, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		// Return an empty string or an error message if parsing fails
		return ""
	}

	// Format the datetime to the desired format
	formattedDateTime := fmt.Sprintf("%d%02d%02d_%02d%02d%02d",
		parsedTime.Year(), parsedTime.Month(), parsedTime.Day(),
		parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second())

	return formattedDateTime
}

func postYearMonth(datetime string) (string, string) {
	parsed, ok := parseDateInput(datetime)
	if !ok {
		return "unknown", "unknown"
	}
	return fmt.Sprintf("%04d", parsed.Year()), fmt.Sprintf("%02d", parsed.Month())
}

func parseURL(toTest string) (*url.URL, error) {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(toTest)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, err
	}

	return u, err
}

func makePath(post lib.Post, outputFolder string, format string, layout string) string {
	filename := fmt.Sprintf("%s_%s.%s", convertDateTime(post.PostDate), post.Slug, outputFormatExtension(format))
	year, month := postYearMonth(post.PostDate)

	switch layout {
	case "year/month":
		return filepath.Join(outputFolder, year, month, filename)
	case "year/slug":
		return filepath.Join(outputFolder, year, post.Slug, filename)
	default:
		return filepath.Join(outputFolder, filename)
	}
}

// extractSlug extracts the slug from a Substack post URL
// e.g. https://example.substack.com/p/this-is-the-post-title -> this-is-the-post-title
func extractSlug(url string) string {
	split := strings.Split(url, "/")
	return split[len(split)-1]
}

// filterExistingPosts filters out posts that already exist in the output folder.
// It prefers a manifest match by canonical URL, then falls back to slug matching.
func filterExistingPosts(urls []string, outputFolder string, format string) ([]string, error) {
	var firstErr error
	manifestPath := filepath.Join(outputFolder, lib.ManifestFilename)
	manifest, err := lib.LoadManifest(manifestPath)
	if err != nil {
		firstErr = err
		manifest = nil
	}

	slugIndex, err := indexExistingSlugs(outputFolder, format)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		slugIndex = map[string]struct{}{}
	}

	var filtered []string
	for _, url := range urls {
		if manifest != nil {
			if entry, ok := manifest.Entries[url]; ok {
				if entry.Format == "" || entry.Format == format {
					entryPath := filepath.FromSlash(entry.FilePath)
					if entryPath != "" && !filepath.IsAbs(entryPath) {
						entryPath = filepath.Join(outputFolder, entryPath)
					}
					if entryPath != "" {
						if _, statErr := os.Stat(entryPath); statErr == nil {
							continue
						}
					}
				}
			}
		}

		slug := extractSlug(url)
		if _, exists := slugIndex[slug]; !exists {
			filtered = append(filtered, url)
		}
	}
	return filtered, firstErr
}

func indexExistingSlugs(outputFolder string, format string) (map[string]struct{}, error) {
	slugIndex := make(map[string]struct{})
	ext := "." + outputFormatExtension(format)
	if _, err := os.Stat(outputFolder); err != nil {
		if os.IsNotExist(err) {
			return slugIndex, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(outputFolder, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ext) {
			return nil
		}
		base := strings.TrimSuffix(name, ext)
		slug := slugFromFilename(base)
		if slug == "" {
			return nil
		}
		slugIndex[slug] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return slugIndex, nil
}

func outputFormatExtension(format string) string {
	if strings.EqualFold(format, "obsidian-md") {
		return "obsidian.md"
	}
	return format
}

func slugFromFilename(base string) string {
	if strings.HasPrefix(base, "_") {
		if len(base) > 1 {
			return base[1:]
		}
		return ""
	}
	if len(base) > 16 && base[8] == '_' && base[15] == '_' && isDigits(base[:8]) && isDigits(base[9:15]) {
		return base[16:]
	}
	return ""
}

func isDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}
