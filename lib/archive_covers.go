package lib

import (
	"context"
	"crypto/sha256"
	"fmt"
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
		cached := id.localImage(ctx, source, path)
		if cached.Success || ctx.Err() != nil {
			return cached
		}
		// A failed cached conversion is retried from its retained source URL.
	}
	if err := os.MkdirAll(imagesPath, 0755); err != nil {
		return ImageInfo{OriginalURL: source, Error: err}
	}
	body, err := id.fetcher.FetchURL(ctx, source)
	if err != nil {
		return ImageInfo{OriginalURL: source, LocalPath: path, Error: fmt.Errorf("failed to fetch cover image: %w", err)}
	}
	defer body.Close()
	return id.saveImage(ctx, source, path, body)
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
			source := coverSourceIdentity(image.OriginalURL)
			for _, imagePath := range []string{image.LocalPath, image.OriginalPath} {
				if imagePath == "" {
					continue
				}
				path := filepath.Clean(imagePath)
				old, exists := proofs[path]
				proofs[path] = bodyProof{source: source, invalid: old.invalid || !image.Success || (exists && old.source != source)}
			}
		}
	}
	verifiedPath := func(path, identity string) bool {
		proof, attempted := proofs[filepath.Clean(path)]
		return !attempted || (!proof.invalid && proof.source == identity)
	}
	// A successfully rewritten post must prefer this run's matching body image.
	// Old content-specific previews remain valid for posts whose write failed.
	current := make(map[int]bool)
	for i := range a.Entries {
		entry := &a.Entries[i]
		if !entry.MediaUpdated {
			continue
		}
		result := bodyImages[filepath.Clean(entry.FilePath)]
		if result == nil {
			continue
		}
		identity := coverSourceIdentity(entry.Post.CoverImage)
		for j := len(result.Images) - 1; j >= 0; j-- {
			image := result.Images[j]
			if coverSourceIdentity(image.OriginalURL) != identity {
				continue
			}
			entry.LocalCoverImage = ""
			if image.Success && usableImage(image.LocalPath) && verifiedPath(image.LocalPath, identity) {
				current[i] = true
				relative, err := downloader.relativeCoverPath(image.LocalPath)
				if err != nil {
					return warnings, err
				}
				entry.LocalCoverImage = relative
			} else if !image.Success {
				current[i] = true
				warnings = append(warnings, fmt.Errorf("cover image for %s could not be converted; using remote source: %w", entry.Post.Title, image.Error))
			}
			break
		}
	}
	// Old archives may retain HEIC originals or a now missing PNG preview.
	// Convert from local bytes before seeding reuse, without refetching posts.
	migrated := make(map[string]ImageInfo)
	for i := range a.Entries {
		entry := &a.Entries[i]
		if current[i] {
			continue
		}
		if !validLocalCoverPath(entry.LocalCoverImage) {
			continue
		}
		path := filepath.Join(downloader.outputDir, filepath.FromSlash(entry.LocalCoverImage))
		if !verifiedPath(path, coverSourceIdentity(entry.Post.CoverImage)) {
			continue
		}
		original := originalPreviewPath(path)
		if usableImage(path) && original != path {
			if _, err := cachedPreview(path); err == nil {
				original = path
			}
		}
		if !verifiedPath(original, coverSourceIdentity(entry.Post.CoverImage)) {
			continue
		}
		if !usableImage(path) && !usableImage(original) {
			continue
		}
		result, exists := migrated[path]
		if !exists {
			result = downloader.localImage(ctx, entry.Post.CoverImage, original)
			migrated[path] = result
		}
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
			entry.LocalCoverImage = ""
			warnings = append(warnings, fmt.Errorf("cover image for %s could not be converted; using remote source: %w", entry.Post.Title, result.Error))
		}
	}
	reused := make(map[string]string)
	// Seed all retained references before visiting formats: a later archive
	// may already hold a usable asset that an earlier archive is missing.
	for i, entry := range a.Entries {
		if current[i] {
			continue
		}
		path := filepath.Join(downloader.outputDir, filepath.FromSlash(entry.LocalCoverImage))
		identity := coverSourceIdentity(entry.Post.CoverImage)
		if entry.Post.CoverImage != "" && validLocalCoverPath(entry.LocalCoverImage) && usableImage(path) && verifiedPath(path, identity) {
			reused[identity] = entry.LocalCoverImage
		}
	}
	attempted := make(map[string]ImageInfo)
	for i := range a.Entries {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}
		entry := &a.Entries[i]
		if current[i] {
			continue
		}
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
			result, exists := attempted[identity]
			if !exists {
				result = downloader.downloadCoverImage(ctx, source, entry.Post.Slug)
				attempted[identity] = result
			}
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
