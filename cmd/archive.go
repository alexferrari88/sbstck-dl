package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

var (
	archiveOutput string
	archiveFormat string
	archiveCmd    = &cobra.Command{
		Use:   "archive",
		Short: "Regenerate the archive index from existing downloads",
		Long:  "Generate index.{format} from existing downloaded posts without re-downloading content.",
		Run: func(cmd *cobra.Command, args []string) {
			format, err := parseSingleFormat(archiveFormat)
			if err != nil {
				log.Fatal(err)
			}

			entries, err := buildArchiveEntries(archiveOutput, format)
			if err != nil && verbose {
				fmt.Printf("Warning: %v\n", err)
			}
			if len(entries) == 0 {
				if verbose {
					fmt.Println("No posts found, exiting...")
				}
				return
			}

			archive := lib.NewArchive()
			for _, entry := range entries {
				archive.AddEntry(entry.Post, entry.Path, entry.DownloadTime)
			}

			if verbose {
				fmt.Printf("Generating archive page in %s format...\n", format)
			}

			var archiveErr error
			switch format {
			case "html":
				archiveErr = archive.GenerateHTML(archiveOutput)
			case "md":
				archiveErr = archive.GenerateMarkdown(archiveOutput)
			case "obsidian-md":
				archiveErr = archive.GenerateObsidianMarkdown(archiveOutput)
			case "txt":
				archiveErr = archive.GenerateText(archiveOutput)
			}

			if archiveErr != nil {
				log.Fatalf("Error generating archive page: %v\n", archiveErr)
			}
			if verbose {
				fmt.Printf("Archive page generated: %s/index.%s\n", archiveOutput, outputFormatExtension(format))
			}

			if err := generateNotionIndex(archiveOutput, format); err != nil {
				log.Printf("Error generating Notion links index: %v\n", err)
			} else if verbose {
				fmt.Printf("Notion links index generated: %s/%s, %s/%s\n", archiveOutput, notionLinksHTML, archiveOutput, notionLinksMD)
			}
		},
	}
)

func init() {
	archiveCmd.Flags().StringVarP(&archiveOutput, "output", "o", ".", "Specify the download directory")
	archiveCmd.Flags().StringVarP(&archiveFormat, "format", "f", "html", "Specify the output format (options: \"html\", \"md\", \"obsidian-md\", \"txt\")")
}

type archiveEntryData struct {
	Post         lib.Post
	Path         string
	DownloadTime time.Time
}

func buildArchiveEntries(outputFolder string, format string) ([]archiveEntryData, error) {
	var firstErr error
	entries := make([]archiveEntryData, 0)
	seen := make(map[string]struct{})

	manifest, manifestErr := loadManifestIfExists(outputFolder)
	if manifestErr != nil {
		firstErr = manifestErr
	}

	if manifest != nil {
		for _, entry := range manifest.Entries {
			if entry.Format != "" && entry.Format != format {
				continue
			}
			path := manifestEntryPath(entry, outputFolder)
			if path == "" {
				continue
			}
			if _, err := os.Stat(path); err != nil {
				continue
			}
			archiveEntry, err := buildEntryFromFile(path, outputFolder, format, &entry)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			entries = append(entries, archiveEntry)
			seen[path] = struct{}{}
		}
	}

	paths, scanErr := scanDownloadedFiles(outputFolder, format)
	if scanErr != nil && firstErr == nil {
		firstErr = scanErr
	}
	for _, path := range paths {
		if _, exists := seen[path]; exists {
			continue
		}
		archiveEntry, err := buildEntryFromFile(path, outputFolder, format, nil)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		entries = append(entries, archiveEntry)
	}

	return entries, firstErr
}

func loadManifestIfExists(outputFolder string) (*lib.Manifest, error) {
	manifestPath := filepath.Join(outputFolder, lib.ManifestFilename)
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return lib.LoadManifest(manifestPath)
}

func manifestEntryPath(entry lib.ManifestEntry, outputFolder string) string {
	entryPath := filepath.FromSlash(entry.FilePath)
	if entryPath == "" {
		return ""
	}
	if !filepath.IsAbs(entryPath) {
		entryPath = filepath.Join(outputFolder, entryPath)
	}
	return filepath.Clean(entryPath)
}

func scanDownloadedFiles(outputFolder string, format string) ([]string, error) {
	if _, err := os.Stat(outputFolder); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	paths := make([]string, 0)
	ext := "." + outputFormatExtension(format)
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
		if slugFromFilename(base) == "" {
			return nil
		}
		paths = append(paths, filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}

func buildEntryFromFile(path string, outputFolder string, format string, manifestEntry *lib.ManifestEntry) (archiveEntryData, error) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	meta, _ := readMetadataSidecar(path)

	slug := meta.Slug
	if slug == "" {
		slug = slugFromFilename(base)
	}
	if slug == "" {
		slug = base
	}

	title := meta.Title
	if title == "" {
		title = slug
	}

	postDate := normalizeDate(meta.PostDate)
	if postDate == "" {
		postDate = normalizeDateFromFilename(base)
	}
	if postDate == "" && manifestEntry != nil {
		postDate = normalizeDate(manifestEntry.LastModified)
	}

	downloadTime := parseDownloadedAt(meta.DownloadedAt)
	if downloadTime.IsZero() && manifestEntry != nil {
		downloadTime = parseDownloadedAt(manifestEntry.DownloadedAt)
	}
	if downloadTime.IsZero() {
		if info, err := os.Stat(path); err == nil {
			downloadTime = info.ModTime()
		}
	}
	if downloadTime.IsZero() {
		downloadTime = time.Now()
	}

	if postDate == "" {
		postDate = downloadTime.UTC().Format(time.RFC3339)
	}

	canonicalURL := meta.CanonicalURL
	if canonicalURL == "" && manifestEntry != nil {
		canonicalURL = manifestEntry.CanonicalURL
	}

	post := lib.Post{
		Title:        title,
		Subtitle:     meta.Subtitle,
		Description:  meta.Description,
		Slug:         slug,
		PostDate:     postDate,
		CanonicalUrl: canonicalURL,
		CoverImage:   meta.CoverImage,
		WordCount:    meta.WordCount,
	}

	return archiveEntryData{
		Post:         post,
		Path:         path,
		DownloadTime: downloadTime,
	}, nil
}

func normalizeDate(value string) string {
	if value == "" {
		return ""
	}
	if parsed, ok := parseDateInput(value); ok {
		return parsed.UTC().Format(time.RFC3339)
	}
	return ""
}

func normalizeDateFromFilename(base string) string {
	if len(base) < 15 || base[8] != '_' {
		return ""
	}
	prefix := base[:15]
	parsed, err := time.Parse("20060102_150405", prefix)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}

func parseDownloadedAt(value string) time.Time {
	if parsed, ok := parseDateInput(value); ok {
		return parsed
	}
	return time.Time{}
}
