package lib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ArchivePostWrite records the outcome of one format's post write.
type ArchivePostWrite struct {
	Format       string
	Path         string
	DownloadTime time.Time
	Err          error
}

// ArchivePublicationResult separates ordinary cover warnings from publication failures.
type ArchivePublicationResult struct {
	Warnings         []error
	PublishedFormats []string
	Err              error
}

// ArchiveWorkflow owns incremental archive state for one download invocation.
// Post extraction, media preparation and post writes remain with the caller.
// The caller records completed post attempts serially, then publishes the archives.
type ArchiveWorkflow struct {
	dir        string
	formats    []string
	archives   map[string]*Archive
	bodyImages map[string]*ImageDownloadResult
	attempts   []ImageInfo
}

// LoadArchiveWorkflow restores every selected format before any download work.
func LoadArchiveWorkflow(dir string, formats []string) (*ArchiveWorkflow, error) {
	seen := make(map[string]bool)
	for _, format := range formats {
		if format != "html" && format != "md" && format != "txt" {
			return nil, fmt.Errorf("unknown archive format: %s", format)
		}
		if seen[format] {
			return nil, fmt.Errorf("duplicate archive format: %s", format)
		}
		seen[format] = true
	}
	w := &ArchiveWorkflow{
		dir:        dir,
		formats:    append([]string(nil), formats...),
		archives:   make(map[string]*Archive),
		bodyImages: make(map[string]*ImageDownloadResult),
	}
	for _, format := range w.formats {
		archive, err := LoadArchive(dir, format)
		if err != nil {
			return nil, fmt.Errorf("restore archive %s (previous index preserved): %w", format, err)
		}
		w.archives[format] = archive
	}
	return w, nil
}

// RecordPost records one media preparation event and all its write outcomes.
// Even an event with no successful writes can invalidate a retained asset's proof.
// Writes describe attempted outputs in the selected formats; nil writes records
// media evidence alone when preparation prevented writing any output.
func (w *ArchiveWorkflow) RecordPost(post Post, images *ImageDownloadResult, writes []ArchivePostWrite) {
	if images != nil {
		w.attempts = append(w.attempts, images.Images...)
		paths := make(map[string]bool)
		for _, format := range w.formats {
			for _, entry := range w.archives[format].Entries {
				if entry.Post.Slug == post.Slug {
					paths[filepath.Clean(entry.FilePath)] = true
				}
			}
		}
		for _, write := range writes {
			if w.archives[write.Format] != nil {
				paths[filepath.Clean(write.Path)] = true
			}
		}
		for path := range paths {
			if w.bodyImages[path] == nil {
				w.bodyImages[path] = &ImageDownloadResult{}
			}
			w.bodyImages[path].Images = append(w.bodyImages[path].Images, images.Images...)
		}
	}
	for _, write := range writes {
		archive := w.archives[write.Format]
		if archive == nil {
			continue
		}
		if write.Err != nil {
			info, err := os.Stat(write.Path)
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				archive.RemoveEntry(write.Path)
			}
			continue
		}
		archive.AddEntry(post, write.Path, write.DownloadTime)
		if images != nil {
			for i := range archive.Entries {
				if filepath.Clean(archive.Entries[i].FilePath) == filepath.Clean(write.Path) {
					archive.Entries[i].MediaUpdated = true
				}
			}
		}
	}
}

// Publish shares cover work across formats, then publishes each independently.
// A metadata failure prevents that format's index from being replaced.
// A nil downloader leaves cover references unchanged. Otherwise, its output
// directory must match the directory used to load this workflow.
func (w *ArchiveWorkflow) Publish(ctx context.Context, downloader *ImageDownloader) ArchivePublicationResult {
	result := ArchivePublicationResult{}
	if err := ctx.Err(); err != nil {
		result.Err = err
		return result
	}
	if len(w.formats) == 0 {
		return result
	}
	if _, err := os.Stat(w.dir); os.IsNotExist(err) {
		return result
	}
	if downloader != nil {
		combined := NewArchive()
		for _, format := range w.formats {
			combined.Entries = append(combined.Entries, w.archives[format].Entries...)
		}
		warnings, err := combined.localizeCovers(ctx, downloader, w.bodyImages, w.attempts)
		result.Warnings = warnings
		if err != nil {
			result.Err = fmt.Errorf("localize archive covers: %w", err)
			return result
		}
		offset := 0
		for _, format := range w.formats {
			n := len(w.archives[format].Entries)
			w.archives[format].Entries = combined.Entries[offset : offset+n]
			offset += n
		}
	}
	var failures []error
	for _, format := range w.formats {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		archive := w.archives[format]
		if err := archive.SaveMetadata(w.dir, format); err != nil {
			failures = append(failures, fmt.Errorf("save archive metadata %s (previous index preserved): %w", format, err))
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		var err error
		switch format {
		case "html":
			err = archive.GenerateHTML(w.dir)
		case "md":
			err = archive.GenerateMarkdown(w.dir)
		case "txt":
			err = archive.GenerateText(w.dir)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("publish archive %s (previous index preserved): %w", format, err))
		} else {
			result.PublishedFormats = append(result.PublishedFormats, format)
		}
	}
	result.Err = errors.Join(failures...)
	return result
}
