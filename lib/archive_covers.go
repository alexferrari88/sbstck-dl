package lib

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// LocalCoverImage is stored relative to the archive folder; CoverImage remains
// the remote source so missing assets can be downloaded again.
func (entry ArchiveEntry) CoverReference(outputDir string) string {
	if entry.LocalCoverImage != "" && validLocalCoverPath(entry.LocalCoverImage) && usableImage(filepath.Join(outputDir, filepath.FromSlash(entry.LocalCoverImage))) {
		return (&url.URL{Path: entry.LocalCoverImage}).String()
	}
	return entry.Post.CoverImage
}

func validLocalCoverPath(path string) bool {
	if path == "" || strings.Contains(path, "\\") {
		return false
	}
	native := filepath.FromSlash(path)
	clean := filepath.Clean(native)
	return !filepath.IsAbs(native) && filepath.VolumeName(native) == "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func usableImage(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// Only unwrap known Substack image-fetch URLs. Comparing filenames or empty
// extracted IDs could mistake an unrelated body image for the cover.
func coverSourceIdentity(source string) string {
	parsed, err := url.Parse(source)
	if err != nil {
		return source
	}
	host := strings.ToLower(parsed.Hostname())
	if (host == "substackcdn.com" || strings.HasSuffix(host, ".substackcdn.com")) && strings.HasPrefix(parsed.Path, "/image/fetch/") {
		for _, scheme := range []string{"https://", "http://"} {
			if index := strings.Index(parsed.Path, scheme); index >= 0 {
				if original, err := url.Parse(parsed.Path[index:]); err == nil && original.Host != "" {
					parsed = original
					break
				}
			}
		}
	}
	parsed.Fragment = ""
	return parsed.String()
}

func (id *ImageDownloader) relativeCoverPath(path string) (string, error) {
	relative, err := filepath.Rel(id.outputDir, path)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if !validLocalCoverPath(relative) {
		return "", fmt.Errorf("cover image must stay within archive folder: %s", path)
	}
	return relative, nil
}

// Cover-only assets have a source-specific directory, so a cover sharing a
// basename with an unrelated body image never replaces that image.
func (id *ImageDownloader) downloadCoverImage(ctx context.Context, source, slug string) ImageInfo {
	if err := ctx.Err(); err != nil {
		return ImageInfo{OriginalURL: source, Error: err}
	}
	identity := sha256.Sum256([]byte(coverSourceIdentity(source)))
	imagesPath := filepath.Join(id.outputDir, id.imagesDir, slug, "covers", fmt.Sprintf("%x", identity[:16]))
	filename, err := id.generateSafeFilename(source)
	if err != nil {
		return ImageInfo{OriginalURL: source, Error: err}
	}
	path := filepath.Join(imagesPath, filename)
	if _, err := id.relativeCoverPath(path); err != nil {
		return ImageInfo{OriginalURL: source, Error: err}
	}
	if usableImage(path) {
		return ImageInfo{OriginalURL: source, LocalPath: path, Success: true}
	}
	if err := os.MkdirAll(imagesPath, 0755); err != nil {
		return ImageInfo{OriginalURL: source, Error: err}
	}
	result := ImageInfo{OriginalURL: source, LocalPath: path}
	body, err := id.fetcher.FetchURL(ctx, source)
	if err != nil {
		result.Error = fmt.Errorf("failed to fetch cover image: %w", err)
		return result
	}
	defer body.Close()
	err = writeFileAtomicallyWith(path, func(file *os.File) error {
		size, err := io.Copy(file, body)
		if err != nil {
			return err
		}
		if size == 0 {
			return fmt.Errorf("downloaded cover image is empty")
		}
		return ctx.Err()
	})
	if err != nil {
		result.Error = fmt.Errorf("failed to save cover image: %w", err)
		return result
	}
	result.Success = true
	result.Format = id.getImageFormat(filename)
	result.Width, result.Height = id.extractDimensionsFromURL(source)
	return result
}

// LocalizeCovers also visits retained entries when no post bodies were fetched.
// Only verified successful results from this invocation may supply body assets.
// Ordinary cover failures are warnings; cancellation stops archive publication.
func (a *Archive) LocalizeCovers(ctx context.Context, downloader *ImageDownloader, bodyImages map[string]*ImageDownloadResult) ([]error, error) {
	var warnings []error
	// A body download can replace an asset referenced by retained metadata.
	// Conflicting/failed attempts cannot certify its source, even if another
	// attempt reported success. Cover-only downloads provide a safe fallback.
	type bodyProof struct {
		source  string
		invalid bool
	}
	proofs := make(map[string]bodyProof)
	for _, result := range bodyImages {
		if result == nil {
			continue
		}
		for _, image := range result.Images {
			if image.LocalPath == "" {
				continue
			}
			path := filepath.Clean(image.LocalPath)
			source := coverSourceIdentity(image.OriginalURL)
			old, exists := proofs[path]
			proofs[path] = bodyProof{source: source, invalid: old.invalid || !image.Success || (exists && old.source != source)}
		}
	}
	verifiedPath := func(path, identity string) bool {
		proof, attempted := proofs[filepath.Clean(path)]
		return !attempted || (!proof.invalid && proof.source == identity)
	}
	reused := make(map[string]string)
	for i := range a.Entries {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}
		entry := &a.Entries[i]
		source := entry.Post.CoverImage
		if source == "" {
			continue
		}
		identity := coverSourceIdentity(source)
		if entry.LocalCoverImage != "" && validLocalCoverPath(entry.LocalCoverImage) && usableImage(filepath.Join(downloader.outputDir, filepath.FromSlash(entry.LocalCoverImage))) && verifiedPath(filepath.Join(downloader.outputDir, filepath.FromSlash(entry.LocalCoverImage)), identity) {
			reused[identity] = entry.LocalCoverImage
			continue
		}
		entry.LocalCoverImage = ""
		if local, exists := reused[identity]; exists {
			entry.LocalCoverImage = local
			continue
		}
		if result := bodyImages[filepath.Clean(entry.FilePath)]; result != nil {
			// A later body image may have replaced a same-basename file.
			// Only the last attempted writer can certify the bytes at a local path.
			seenPaths := make(map[string]bool)
			for j := len(result.Images) - 1; j >= 0; j-- {
				image := result.Images[j]
				path := filepath.Clean(image.LocalPath)
				if image.LocalPath == "" || seenPaths[path] {
					continue
				}
				seenPaths[path] = true
				if !image.Success {
					continue
				}
				if coverSourceIdentity(image.OriginalURL) == identity && usableImage(image.LocalPath) && verifiedPath(image.LocalPath, identity) {
					if relative, err := downloader.relativeCoverPath(image.LocalPath); err == nil {
						entry.LocalCoverImage = relative
						break
					}
				}
			}
		}
		if entry.LocalCoverImage == "" {
			result := downloader.downloadCoverImage(ctx, source, entry.Post.Slug)
			if err := ctx.Err(); err != nil {
				return warnings, err
			}
			if result.Success {
				relative, err := downloader.relativeCoverPath(result.LocalPath)
				if err != nil {
					return warnings, err
				}
				entry.LocalCoverImage = relative
			} else {
				warnings = append(warnings, fmt.Errorf("cover image for %s could not be downloaded; using remote source: %w", entry.Post.Title, result.Error))
			}
		}
		if entry.LocalCoverImage != "" {
			reused[identity] = entry.LocalCoverImage
		}
	}
	return warnings, ctx.Err()
}
