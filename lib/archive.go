package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Store only index metadata, not the full post body. Paths are relative to the
// output folder so an archive remains portable when moved.
type archiveRecord struct {
	File            string    `json:"file"`
	Title           string    `json:"title"`
	PostDate        string    `json:"post_date"`
	Subtitle        string    `json:"subtitle,omitempty"`
	Description     string    `json:"description,omitempty"`
	CoverImage      string    `json:"cover_image,omitempty"`
	LocalCoverImage string    `json:"local_cover_image,omitempty"`
	DownloadTime    time.Time `json:"download_time"`
}

type archiveManifest struct {
	Version int             `json:"version"`
	Entries []archiveRecord `json:"entries"`
}

var downloadedPostName = regexp.MustCompile(`^(\d{8}_\d{6})?_(.+)\.(html|md|txt)$`)

func manifestPath(dir, format string) string {
	return filepath.Join(dir, ".sbstck-dl-archive-"+format+".json")
}

// LoadArchive restores metadata and discovers legacy files without network
// requests. Unreadable or corrupt state is an error: never replace an index
// using an incomplete restoration.
func LoadArchive(dir, format string) (*Archive, error) {
	if format != "html" && format != "md" && format != "txt" {
		return nil, fmt.Errorf("unknown archive format: %s", format)
	}
	archive := NewArchive()
	records := make(map[string]archiveRecord)
	data, err := os.ReadFile(manifestPath(dir, format))
	if err == nil {
		var manifest archiveManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("read archive metadata %s: %w", manifestPath(dir, format), err)
		}
		if manifest.Version != 1 {
			return nil, fmt.Errorf("unsupported archive metadata version %d", manifest.Version)
		}
		for _, record := range manifest.Entries {
			if err := validateArchiveRecord(record, format); err != nil {
				return nil, err
			}
			if _, exists := records[record.File]; exists {
				return nil, fmt.Errorf("duplicate archive metadata file %q", record.File)
			}
			records[record.File] = record
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read archive metadata %s: %w", manifestPath(dir, format), err)
	}
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return archive, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read archive folder: %w", err)
	}
	for _, file := range files {
		match := downloadedPostName.FindStringSubmatch(file.Name())
		if match == nil || match[3] != format || !file.Type().IsRegular() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect archived post %s: %w", file.Name(), err)
		}
		if info.Size() == 0 {
			continue
		}
		path := filepath.Join(dir, file.Name())
		record, exists := records[file.Name()]
		if !exists {
			body, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read archived post %s: %w", file.Name(), err)
			}
			if len(bytes.TrimSpace(body)) == 0 {
				continue
			}
			record = archiveRecord{File: file.Name(), Title: legacyPostTitle(body, format), DownloadTime: info.ModTime()}
			if record.Title == "" {
				record.Title = match[2]
			}
			if date, err := time.Parse("20060102_150405", match[1]); err == nil {
				record.PostDate = date.Format(time.RFC3339)
			}
		}
		post := Post{Slug: match[2], Title: record.Title, PostDate: record.PostDate, Subtitle: record.Subtitle, Description: record.Description, CoverImage: record.CoverImage}
		archive.AddEntry(post, path, record.DownloadTime)
		for i := range archive.Entries {
			if archive.Entries[i].FilePath == path {
				archive.Entries[i].LocalCoverImage = record.LocalCoverImage
				break
			}
		}
	}
	return archive, nil
}

func legacyPostTitle(body []byte, format string) string {
	if format == "html" {
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err == nil {
			return strings.TrimSpace(doc.Find("h1").First().Text())
		}
		return ""
	}
	line := strings.SplitN(string(body), "\n", 2)[0]
	if format == "md" {
		line = strings.TrimPrefix(line, "# ")
	}
	return strings.TrimSpace(line)
}

// RemoveEntry removes a local post identity from the index.
func (a *Archive) RemoveEntry(path string) {
	for i := 0; i < len(a.Entries); {
		if filepath.Clean(a.Entries[i].FilePath) == filepath.Clean(path) {
			a.Entries = append(a.Entries[:i], a.Entries[i+1:]...)
		} else {
			i++
		}
	}
}

func validateArchiveRecord(record archiveRecord, format string) error {
	if record.DownloadTime.IsZero() {
		return fmt.Errorf("invalid archive metadata file %q: missing or zero download_time", record.File)
	}
	if strings.TrimSpace(record.Title) == "" {
		return fmt.Errorf("invalid archive metadata file %q: missing or empty title", record.File)
	}
	if record.LocalCoverImage != "" && !validLocalCoverPath(record.LocalCoverImage) {
		return fmt.Errorf("invalid local cover image path %q", record.LocalCoverImage)
	}
	match := downloadedPostName.FindStringSubmatch(record.File)
	if filepath.Base(record.File) != record.File || match == nil || match[3] != format {
		return fmt.Errorf("invalid archive metadata file %q", record.File)
	}
	return nil
}

// SaveMetadata publishes portable, format-specific metadata for future runs.
func (a *Archive) SaveMetadata(dir, format string) error {
	if format != "html" && format != "md" && format != "txt" {
		return fmt.Errorf("unknown archive format: %s", format)
	}
	manifest := archiveManifest{Version: 1, Entries: make([]archiveRecord, 0, len(a.Entries))}
	seen := make(map[string]bool)
	for _, entry := range a.Entries {
		record := archiveRecord{File: filepath.Base(entry.FilePath), Title: entry.Post.Title, PostDate: entry.Post.PostDate, Subtitle: entry.Post.Subtitle, Description: entry.Post.Description, CoverImage: entry.Post.CoverImage, LocalCoverImage: entry.LocalCoverImage, DownloadTime: entry.DownloadTime}
		if err := validateArchiveRecord(record, format); err != nil {
			return err
		}
		if seen[record.File] {
			return fmt.Errorf("duplicate archive metadata file %q", record.File)
		}
		seen[record.File] = true
		manifest.Entries = append(manifest.Entries, record)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(manifestPath(dir, format), append(data, '\n'))
}

// Write beside the destination, then rename. Failed writes or replacement leave
// the prior file intact, and temporary files are always removed.
func writeFileAtomically(path string, data []byte) error {
	return writeFileAtomicallyWith(path, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

func writeFileAtomicallyWith(path string, write func(*os.File) error) error {
	mode := os.FileMode(0644)
	existing, err := os.Lstat(path)
	if err == nil && existing.Mode().IsRegular() {
		mode = existing.Mode().Perm()
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sbstck-dl-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		err = write(file)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
