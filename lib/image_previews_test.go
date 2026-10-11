package lib

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/internal/heicpreview"
	"github.com/stretchr/testify/require"
)

func heicFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/preview.heic")
	require.NoError(t, err)
	return data
}
func verifyPreview(t *testing.T, result ImageInfo, original []byte) {
	t.Helper()
	require.True(t, result.Success, "%v", result.Error)
	source, err := os.ReadFile(result.OriginalPath)
	require.NoError(t, err)
	require.Equal(t, original, source)
	preview, err := os.ReadFile(result.LocalPath)
	require.NoError(t, err)
	decoded, err := png.Decode(bytes.NewReader(preview))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 480, 640), decoded.Bounds())
	require.Equal(t, 480, result.Width)
	require.Equal(t, 640, result.Height)
	require.Equal(t, "png", result.Format)
}
func TestHEICPreviewRealFixture(t *testing.T) {
	id := NewImageDownloader(nil, t.TempDir(), "images", ImageQualityHigh)
	data := heicFixture(t)
	for _, name := range []string{"photo.heic", "photo.HEIC", "photo.heif", "photo", strings.Repeat("a", 190) + ".heic"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(id.outputDir, name)
			r := id.saveImage(context.Background(), "https://example.org/"+name, path, bytes.NewReader(data))
			verifyPreview(t, r, data)
			require.Equal(t, path, originalPreviewPath(r.LocalPath))
			require.LessOrEqual(t, len(filepath.Base(r.LocalPath)), 255)
			// Cached preview preserves permissions and its publication time.
			require.NoError(t, os.Chmod(r.LocalPath, 0600))
			info, err := os.Stat(r.LocalPath)
			require.NoError(t, err)
			cached := id.localImage(context.Background(), r.OriginalURL, path)
			verifyPreview(t, cached, data)
			after, err := os.Stat(r.LocalPath)
			require.NoError(t, err)
			require.Equal(t, info.ModTime(), after.ModTime())
			require.Equal(t, info.Mode().Perm(), after.Mode().Perm())
			require.NoError(t, os.Remove(r.LocalPath))
			verifyPreview(t, id.localImage(context.Background(), r.OriginalURL, path), data)
		})
	}
}
func TestHEICPreviewFailureAndRetry(t *testing.T) {
	id := NewImageDownloader(nil, t.TempDir(), "images", ImageQualityHigh)
	path := filepath.Join(id.outputDir, "photo.heic")
	data := heicFixture(t)
	good := id.saveImage(context.Background(), "https://example.org/photo.heic", path, bytes.NewReader(data))
	verifyPreview(t, good, data)
	old, err := os.ReadFile(good.LocalPath)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0600))
	before, err := os.Stat(path)
	require.NoError(t, err)
	bad := id.saveImage(context.Background(), good.OriginalURL, path, bytes.NewReader([]byte("corrupt HEIC")))
	require.False(t, bad.Success)
	require.Error(t, bad.Error)
	require.NotEqual(t, good.LocalPath, bad.LocalPath)
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "corrupt HEIC", string(source))
	retained, err := os.ReadFile(good.LocalPath)
	require.NoError(t, err)
	require.Equal(t, old, retained)
	_, err = os.Stat(bad.LocalPath)
	require.True(t, os.IsNotExist(err))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), info.Mode().Perm())
	verifyPreview(t, id.saveImage(context.Background(), good.OriginalURL, path, bytes.NewReader(data)), data)
}
func TestHEICPreviewPortableBytesAndCollision(t *testing.T) {
	id := NewImageDownloader(nil, t.TempDir(), "images", ImageQualityHigh)
	var regular bytes.Buffer
	require.NoError(t, png.Encode(&regular, image.NewRGBA(image.Rect(0, 0, 2, 3))))
	path := filepath.Join(id.outputDir, "photo.heic")
	result := id.saveImage(context.Background(), "https://example.org/photo.heic", path, bytes.NewReader(regular.Bytes()))
	require.True(t, result.Success)
	require.Equal(t, path, result.LocalPath)
	require.Equal(t, regular.Bytes(), mustReadPreview(t, path))
	pngPath := filepath.Join(id.outputDir, "photo.png")
	require.NoError(t, os.WriteFile(pngPath, regular.Bytes(), 0644))
	result = id.saveImage(context.Background(), result.OriginalURL, path, bytes.NewReader(heicFixture(t)))
	require.True(t, result.Success, "%v", result.Error)
	require.NotEqual(t, pngPath, result.LocalPath)
	require.Equal(t, regular.Bytes(), mustReadPreview(t, pngPath))
}
func mustReadPreview(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	require.NoError(t, e)
	return b
}
func TestHEICPreviewCancellationAndLimits(t *testing.T) {
	id := NewImageDownloader(nil, t.TempDir(), "images", ImageQualityHigh)
	path := filepath.Join(id.outputDir, "photo.heic")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := id.saveImage(ctx, "https://example.org/photo.heic", path, bytes.NewReader(heicFixture(t)))
	require.False(t, r.Success)
	require.ErrorIs(t, r.Error, context.Canceled)
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err))
	_, err = heicpreview.Decode(ctx, heicFixture(t))
	require.ErrorIs(t, err, context.Canceled)
	_, err = heicpreview.Decode(context.Background(), make([]byte, heicpreview.MaxInputBytes+1))
	require.ErrorContains(t, err, "input limit")
	// Timeout must be returned, with no completed image.
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = heicpreview.Decode(ctx, heicFixture(t))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
func TestHEICPreviewContainerDetection(t *testing.T) {
	for _, brand := range []string{"heic", "heix", "hevc", "hevx", "mif1", "msf1"} {
		header := make([]byte, 24)
		binary.BigEndian.PutUint32(header, 24)
		copy(header[4:], "ftyp")
		copy(header[8:], "zzzz")
		copy(header[16:], brand)
		require.True(t, needsHEICPreview(header, "https://example.org/no-extension", "no-extension"))
	}
	require.True(t, needsHEICPreview([]byte("bad"), "https://cdn.org/image/fetch/https%253A%252F%252Fhost%252Fimage.HEIF", "image"))
	extended := make([]byte, 32)
	binary.BigEndian.PutUint32(extended, 1)
	copy(extended[4:], "ftyp")
	binary.BigEndian.PutUint64(extended[8:], 32)
	copy(extended[16:], "heic")
	require.True(t, needsHEICPreview(extended, "https://example.org/no-extension", "no-extension"))
	avif := make([]byte, 24)
	binary.BigEndian.PutUint32(avif, 24)
	copy(avif[4:], "ftyp")
	copy(avif[8:], "mif1")
	copy(avif[16:], "avif")
	require.False(t, needsHEICPreview(avif, "https://example.org/photo.heic", "photo.heic"))
	require.False(t, needsHEICPreview([]byte("ordinary"), "https://example.org/photo.jpg", "photo.jpg"))
}
func TestHEICPreviewRetainedCoverMigration(t *testing.T) {
	dir := t.TempDir()
	id := NewImageDownloader(nil, dir, "images", ImageQualityHigh)
	path := filepath.Join(dir, "photo.heic")
	data := heicFixture(t)
	require.NoError(t, os.WriteFile(path, data, 0644))
	a := &Archive{Entries: []ArchiveEntry{{Post: Post{Slug: "fixture", Title: "Fixture", CoverImage: "https://unreachable.invalid/photo.heic"}, FilePath: filepath.Join(dir, "post.html"), LocalCoverImage: "photo.heic"}}}
	warnings, err := a.LocalizeCovers(context.Background(), id, nil)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, a.Entries[0].LocalCoverImage, ".sbstck-previews/")
	preview := filepath.Join(dir, filepath.FromSlash(a.Entries[0].LocalCoverImage))
	require.NoError(t, os.Remove(preview))
	warnings, err = a.LocalizeCovers(context.Background(), id, nil)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.FileExists(t, preview)
}

func TestHEICPreviewCorruptCacheAndPublicationFailure(t *testing.T) {
	id := NewImageDownloader(nil, t.TempDir(), "images", ImageQualityHigh)
	data := heicFixture(t)
	path := filepath.Join(id.outputDir, "photo.heic")
	result := id.saveImage(context.Background(), "https://example.org/photo.heic", path, bytes.NewReader(data))
	verifyPreview(t, result, data)
	preview := mustReadPreview(t, result.LocalPath)
	require.NoError(t, os.WriteFile(result.LocalPath, preview[:33], 0600))
	recovered := id.localImage(context.Background(), result.OriginalURL, path)
	verifyPreview(t, recovered, data)
	require.NoError(t, os.Remove(result.LocalPath))
	require.NoError(t, os.Mkdir(result.LocalPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(result.LocalPath, "sentinel"), []byte("preserved"), 0644))
	failed := id.saveImage(context.Background(), result.OriginalURL, path, bytes.NewReader(data))
	require.False(t, failed.Success)
	require.Error(t, failed.Error)
	require.Equal(t, data, mustReadPreview(t, path))
	require.Equal(t, []byte("preserved"), mustReadPreview(t, filepath.Join(result.LocalPath, "sentinel")))
}
func TestHEICPreviewDecoderPixelLimitAndActiveCancellation(t *testing.T) {
	data := heicFixture(t)
	offset := bytes.Index(data, []byte("ispe"))
	require.GreaterOrEqual(t, offset, 0)
	large := append([]byte{}, data...)
	binary.BigEndian.PutUint32(large[offset+8:], 10000)
	binary.BigEndian.PutUint32(large[offset+12:], 10000)
	img, err := heicpreview.Decode(context.Background(), large)
	require.ErrorContains(t, err, "50 megapixel limit")
	require.Nil(t, img)
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	img, err = heicpreview.Decode(ctx, data)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, img)
}
func TestHEICPreviewFailedCachedCoverRetries(t *testing.T) {
	data := heicFixture(t)
	var requests atomic.Int32
	var good atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if good.Load() {
			w.Write(data)
		} else {
			w.Write([]byte("corrupt HEIC"))
		}
	}))
	defer server.Close()
	id := NewImageDownloader(NewFetcher(), t.TempDir(), "images", ImageQualityHigh)
	source := server.URL + "/cover.heic"
	failed := id.downloadCoverImage(context.Background(), source, "fixture")
	require.False(t, failed.Success)
	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, []byte("corrupt HEIC"), mustReadPreview(t, failed.OriginalPath))
	good.Store(true)
	result := id.downloadCoverImage(context.Background(), source, "fixture")
	verifyPreview(t, result, data)
	require.Equal(t, int32(2), requests.Load())
	again := id.downloadCoverImage(context.Background(), source, "fixture")
	verifyPreview(t, again, data)
	require.Equal(t, int32(2), requests.Load())
}

func TestHEICPreviewCurrentNonHEICCoverCollision(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write([]byte("source cover bytes")) }))
	defer server.Close()
	dir := t.TempDir()
	id := NewImageDownloader(NewFetcher(), dir, "images", ImageQualityHigh)
	path := filepath.Join(dir, "cover.jpg")
	require.NoError(t, os.WriteFile(path, []byte("unrelated last writer bytes"), 0644))
	source := server.URL + "/cover.jpg"
	postPath := filepath.Join(dir, "post.html")
	a := &Archive{Entries: []ArchiveEntry{{Post: Post{Slug: "fixture", Title: "Fixture", CoverImage: source}, FilePath: postPath, MediaUpdated: true}}}
	body := map[string]*ImageDownloadResult{postPath: {Images: []ImageInfo{{OriginalURL: source, OriginalPath: path, LocalPath: path, Success: true}, {OriginalURL: server.URL + "/other/cover.jpg", OriginalPath: path, LocalPath: path, Success: true}}}}
	warnings, err := a.LocalizeCovers(context.Background(), id, body)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, int32(1), requests.Load())
	require.Contains(t, a.Entries[0].LocalCoverImage, "/covers/")
	require.Equal(t, []byte("source cover bytes"), mustReadPreview(t, filepath.Join(dir, filepath.FromSlash(a.Entries[0].LocalCoverImage))))
	require.Equal(t, []byte("unrelated last writer bytes"), mustReadPreview(t, path))
}
