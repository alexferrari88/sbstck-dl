package lib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArchiveCoverMatching(t *testing.T) {
	source := "https://example.test/assets/cover.jpg?version=2"
	for _, wrapped := range []string{
		"https://substackcdn.com/image/fetch/w_1456,c_limit/https%3A%2F%2Fexample.test%2Fassets%2Fcover.jpg%3Fversion%3D2",
		"https://substackcdn.com/image/fetch/w_424,c_limit/https://example.test/assets/cover.jpg%3Fversion%3D2",
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "images", "body-cover.jpg")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte("verified body image"), 0644))
		archive := NewArchive()
		postFile := filepath.Join(dir, "20260101_120000_post.html")
		archive.AddEntry(Post{Slug: "post", Title: "Post", CoverImage: source}, postFile, time.Now())
		images := map[string]*ImageDownloadResult{filepath.Clean(postFile): {Images: []ImageInfo{{OriginalURL: wrapped, LocalPath: path, Success: true}}}}
		warnings, err := archive.LocalizeCovers(context.Background(), NewImageDownloader(nil, dir, "images", ImageQualityHigh), images)
		require.NoError(t, err)
		require.Empty(t, warnings)
		require.Equal(t, "images/body-cover.jpg", archive.Entries[0].LocalCoverImage)
		require.Equal(t, source, archive.Entries[0].Post.CoverImage)
	}
	require.NotEqual(t, coverSourceIdentity(source), coverSourceIdentity("https://unrelated.test/assets/cover.jpg?version=2"))
	require.NotEqual(t, coverSourceIdentity(source), coverSourceIdentity("https://evilsubstackcdn.com/image/fetch/w_1/"+source))
}

func TestArchiveCoverSameBasenameDoesNotReuseUnrelatedAsset(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, "real cover bytes") }))
	defer server.Close()
	for _, latestSuccess := range []bool{true, false} {
		t.Run(fmt.Sprintf("latest_success_%t", latestSuccess), func(t *testing.T) {
			requests.Store(0)
			dir := t.TempDir()
			bodyPath := filepath.Join(dir, "images", "post", "cover.jpg")
			require.NoError(t, os.MkdirAll(filepath.Dir(bodyPath), 0755))
			require.NoError(t, os.WriteFile(bodyPath, []byte("unrelated body bytes"), 0644))
			source := server.URL + "/cover.jpg"
			postFile := filepath.Join(dir, "20260101_120000_post.html")
			archive := NewArchive()
			archive.AddEntry(Post{Slug: "post", Title: "Post", CoverImage: source}, postFile, time.Now())
			archive.Entries[0].LocalCoverImage = "images/post/cover.jpg" // Retained reference whose bytes were overwritten.
			// A later successful or failed attempt may replace/truncate the same
			// local filename. Retained metadata and earlier success cannot
			// certify those bytes as the cover.
			images := map[string]*ImageDownloadResult{filepath.Clean(postFile): {Images: []ImageInfo{
				{OriginalURL: source, LocalPath: bodyPath, Success: true},
				{OriginalURL: server.URL + "/unrelated/cover.jpg", LocalPath: bodyPath, Success: latestSuccess},
			}}}
			downloader := NewImageDownloader(NewFetcher(WithRatePerSecond(1000)), dir, "images", ImageQualityHigh)
			warnings, err := archive.LocalizeCovers(context.Background(), downloader, images)
			require.NoError(t, err)
			require.Empty(t, warnings)
			require.Equal(t, int32(1), requests.Load())
			require.Contains(t, archive.Entries[0].LocalCoverImage, "/covers/")
			original, err := os.ReadFile(bodyPath)
			require.NoError(t, err)
			require.Equal(t, "unrelated body bytes", string(original))
			cover, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(archive.Entries[0].LocalCoverImage)))
			require.NoError(t, err)
			require.Equal(t, "real cover bytes", string(cover))
		})
	}
}

func TestArchiveCoverCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer server.Close()
	dir := t.TempDir()
	archive := NewArchive()
	archive.AddEntry(Post{Slug: "post", Title: "Post", CoverImage: server.URL + "/cover.jpg"}, filepath.Join(dir, "20260101_120000_post.html"), time.Now())
	warnings, err := archive.LocalizeCovers(ctx, NewImageDownloader(NewFetcher(), dir, "images", ImageQualityHigh), nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, warnings)
	require.Empty(t, archive.Entries[0].LocalCoverImage)
	require.Equal(t, server.URL+"/cover.jpg", archive.Entries[0].CoverReference(dir))
}

func TestArchiveCoverManifestPortability(t *testing.T) {
	dir := t.TempDir()
	postFile := filepath.Join(dir, "20260101_120000_post.html")
	post := Post{Slug: "post", Title: "Post", CoverImage: "https://example.test/cover.jpg", BodyHTML: "<p>Body</p>"}
	require.NoError(t, post.WriteToFile(postFile, "html", false))
	cover := filepath.Join(dir, "assets", "nested", "cover.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(cover), 0755))
	require.NoError(t, os.WriteFile(cover, []byte("image"), 0644))
	archive := NewArchive()
	archive.AddEntry(post, postFile, time.Now())
	archive.Entries[0].LocalCoverImage = "assets/nested/cover.jpg"
	require.NoError(t, archive.SaveMetadata(dir, "html"))
	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(dir, moved))
	restored, err := LoadArchive(moved, "html")
	require.NoError(t, err)
	require.Equal(t, "assets/nested/cover.jpg", restored.Entries[0].CoverReference(moved))
	require.Equal(t, post.CoverImage, restored.Entries[0].Post.CoverImage)
	require.NoError(t, os.Remove(filepath.Join(moved, "assets", "nested", "cover.jpg")))
	require.Equal(t, post.CoverImage, restored.Entries[0].CoverReference(moved))
	restored.Entries[0].LocalCoverImage = "../outside.jpg"
	require.Error(t, restored.SaveMetadata(moved, "html"))
}

func TestArchiveCoverPartialTransferRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, "partial")
			return
		}
		fmt.Fprint(w, "complete cover")
	}))
	defer server.Close()
	dir := t.TempDir()
	source := server.URL + "/cover.jpg"
	downloader := NewImageDownloader(NewFetcher(WithRatePerSecond(1000)), dir, "images", ImageQualityHigh)
	failed := downloader.downloadCoverImage(context.Background(), source, "post")
	require.False(t, failed.Success)
	require.Error(t, failed.Error)
	_, err := os.Stat(failed.LocalPath)
	require.True(t, os.IsNotExist(err), "partial transfer must not publish a cacheable cover")
	retried := downloader.downloadCoverImage(context.Background(), source, "post")
	require.True(t, retried.Success)
	require.Equal(t, int32(2), requests.Load())
	body, err := os.ReadFile(retried.LocalPath)
	require.NoError(t, err)
	require.Equal(t, "complete cover", string(body))
	cached := downloader.downloadCoverImage(context.Background(), source, "post")
	require.True(t, cached.Success)
	require.Equal(t, int32(2), requests.Load())
	temp, err := filepath.Glob(filepath.Join(filepath.Dir(retried.LocalPath), ".sbstck-dl-archive-*"))
	require.NoError(t, err)
	require.Empty(t, temp)
}
