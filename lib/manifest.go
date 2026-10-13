package lib

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MediaManifestFile is the name of the media manifest in the output directory.
const MediaManifestFile = ".sbstck-dl-media.json"

// Media kinds tracked in the manifest.
const (
	MediaPodcast = "podcast"
	MediaVideos  = "videos"
)

// MediaManifest records which posts' media downloads are complete, so that
// later runs don't need to fetch those posts again. It lives in the output
// directory; deleting it makes the next run check every post's media again.
type MediaManifest struct {
	path      string
	outputDir string
	Version   int                     `json:"version"`
	Posts     map[string]*MediaRecord `json:"posts"` // keyed by post slug
}

// MediaRecord holds the media status of one post, by media kind.
type MediaRecord map[string]*MediaStatus

// MediaStatus is the outcome of downloading one kind of media for a post.
type MediaStatus struct {
	// Complete is true after successful output publication, when all media was
	// downloaded in full, or the post has none. Previews, unavailable or
	// failed downloads leave it false so the post is checked again.
	Complete bool `json:"complete"`
	// Files lists the downloaded files, relative to the output directory.
	Files []string `json:"files,omitempty"`
}

// NewMediaManifest returns an empty manifest for outputDir.
func NewMediaManifest(outputDir string) *MediaManifest {
	return &MediaManifest{
		path:      filepath.Join(outputDir, MediaManifestFile),
		outputDir: outputDir,
		Version:   2,
		Posts:     map[string]*MediaRecord{},
	}
}

// LoadMediaManifest loads the manifest from outputDir, or returns an empty one
// if there is none yet.
func LoadMediaManifest(outputDir string) (*MediaManifest, error) {
	m := NewMediaManifest(outputDir)

	if err := checkMediaDestination(m.path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	m.Version = 0
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", m.path, err)
	}
	if m.Version != 1 && m.Version != 2 {
		return nil, fmt.Errorf("unsupported media manifest version %d in %s", m.Version, m.path)
	}
	if m.Posts == nil {
		m.Posts = map[string]*MediaRecord{}
	}
	for slug, record := range m.Posts {
		if record == nil {
			continue
		}
		for kind, status := range *record {
			if status == nil {
				continue
			}
			for _, file := range status.Files {
				if !validMediaManifestPath(file) {
					return nil, fmt.Errorf("invalid media path %q for %s/%s", file, slug, kind)
				}
			}
		}
	}
	// Version 1 only certified downloaded bytes, so it cannot prove the local
	// players were published. Retain files for reuse but require one output repair.
	if m.Version == 1 {
		for _, record := range m.Posts {
			if record == nil {
				continue
			}
			for _, status := range *record {
				if status != nil {
					status.Complete = false
				}
			}
		}
		m.Version = 2
	}
	return m, nil
}

func validMediaManifestPath(file string) bool {
	path := filepath.FromSlash(file)
	return path != "" && filepath.IsLocal(path) && path != "."
}

// IsComplete reports whether the given kind of media is complete for the post:
// recorded as complete, with all its files still present.
func (m *MediaManifest) IsComplete(slug, kind string) bool {
	record := m.Posts[slug]
	if record == nil {
		return false
	}
	status := (*record)[kind]
	if status == nil || !status.Complete {
		return false
	}
	for _, file := range status.Files {
		if !validMediaManifestPath(file) {
			return false
		}
		if info, err := os.Lstat(filepath.Join(m.outputDir, filepath.FromSlash(file))); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
}

// Record stores the status of a kind of media for the post. Local paths in
// files are stored relative to the output directory.
func (m *MediaManifest) Record(slug, kind string, complete bool, files []string) {
	if m.Posts == nil {
		m.Posts = map[string]*MediaRecord{}
	}
	status := &MediaStatus{Complete: complete}
	for _, file := range files {
		if rel, err := filepath.Rel(m.outputDir, file); err == nil {
			file = rel
		}
		status.Files = append(status.Files, filepath.ToSlash(file))
	}

	record := m.Posts[slug]
	if record == nil || *record == nil {
		record = &MediaRecord{}
		m.Posts[slug] = record
	}
	(*record)[kind] = status
}

// Save writes the manifest to the output directory, atomically.
func (m *MediaManifest) Save() error {
	if m.Version != 2 {
		return fmt.Errorf("unsupported media manifest version %d", m.Version)
	}
	if err := checkMediaDestination(m.path); err != nil {
		return err
	}
	for slug, record := range m.Posts {
		if record == nil {
			continue
		}
		for kind, status := range *record {
			if status == nil {
				continue
			}
			for _, path := range status.Files {
				if !validMediaManifestPath(path) {
					return fmt.Errorf("invalid media path %q for %s/%s", path, slug, kind)
				}
			}
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.outputDir, 0755); err != nil {
		return err
	}
	return writeFileAtomically(m.path, append(data, '\n'))
}
