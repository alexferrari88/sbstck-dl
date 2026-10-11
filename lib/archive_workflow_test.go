package lib_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/require"
)

func workflowManifest(dir, format string) string {
	return filepath.Join(dir, ".sbstck-dl-archive-"+format+".json")
}

func workflowRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func workflowSeed(t *testing.T, dir, format string, posts []lib.Post, covers []string) []string {
	t.Helper()
	archive := lib.NewArchive()
	paths := make([]string, len(posts))
	for i, post := range posts {
		paths[i] = filepath.Join(dir, "20260101_120000_"+post.Slug+"."+format)
		require.NoError(t, post.WriteToFile(paths[i], format, false))
		archive.AddEntry(post, paths[i], time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		if len(covers) > i {
			archive.Entries[len(archive.Entries)-1].LocalCoverImage = covers[i]
		}
	}
	require.NoError(t, archive.SaveMetadata(dir, format))
	return paths
}

func workflowDownloader(dir string) *lib.ImageDownloader {
	return lib.NewImageDownloader(lib.NewFetcher(lib.WithRatePerSecond(1000), lib.WithBackOffConfig(&backoff.StopBackOff{})), dir, "images", lib.ImageQualityHigh)
}

func TestArchiveWorkflowRestorationBarrier(t *testing.T) {
	dir := t.TempDir()
	workflowSeed(t, dir, "html", []lib.Post{{Slug: "post", Title: "Retained", BodyHTML: "<p>body</p>"}}, nil)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("retained index"), 0644))
	require.NoError(t, os.WriteFile(workflowManifest(dir, "md"), []byte("{corrupt"), 0644))
	manifest := workflowRead(t, workflowManifest(dir, "html"))
	workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md"})
	require.Error(t, err)
	require.Nil(t, workflow, "a later corrupt format must prevent a partially restored workflow")
	require.Equal(t, manifest, workflowRead(t, workflowManifest(dir, "html")))
	require.Equal(t, []byte("retained index"), workflowRead(t, filepath.Join(dir, "index.html")))
	require.Equal(t, []byte("{corrupt"), workflowRead(t, workflowManifest(dir, "md")))
}

func TestArchiveWorkflowMixedWritesPreserveRetainedMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	dir := t.TempDir()
	old := lib.Post{Slug: "post", Title: "Retained title", Subtitle: "Retained subtitle", CoverImage: server.URL + "/cover.jpg", BodyHTML: "<p>Old body</p>"}
	htmlPaths := workflowSeed(t, dir, "html", []lib.Post{old}, []string{"images/old.jpg"})
	mdPaths := workflowSeed(t, dir, "md", []lib.Post{old}, []string{"images/old.jpg"})
	txtPaths := workflowSeed(t, dir, "txt", []lib.Post{old}, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "images"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "images", "old.jpg"), []byte("retained cover"), 0644))
	workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md", "txt"})
	require.NoError(t, err)
	updated := lib.Post{Slug: "post", Title: "New title", Subtitle: "New subtitle", CoverImage: old.CoverImage, BodyHTML: "<p>New body</p>"}
	newPath := filepath.Join(dir, "20260201_120000_post.html")
	require.NoError(t, updated.WriteToFile(newPath, "html", false))
	// A failed replacement may preserve a valid old file or leave an invalid one.
	require.NoError(t, os.WriteFile(txtPaths[0], nil, 0644))
	when := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	images := &lib.ImageDownloadResult{Images: []lib.ImageInfo{{OriginalURL: old.CoverImage, LocalPath: filepath.Join(dir, "images", "new.jpg"), Error: errors.New("conversion failed")}}}
	writes := []lib.ArchivePostWrite{
		{Format: "html", Path: newPath, DownloadTime: when},
		{Format: "md", Path: mdPaths[0], DownloadTime: when, Err: errors.New("write failed")},
		{Format: "txt", Path: txtPaths[0], DownloadTime: when, Err: errors.New("write failed")},
	}
	workflow.RecordPost(updated, images, writes)
	writes[0].Path = filepath.Join(dir, "unused.html")
	result := workflow.Publish(context.Background(), workflowDownloader(dir))
	require.NoError(t, result.Err)
	require.Len(t, result.Warnings, 1, "only the successful post write should use this event's failed media")
	require.Zero(t, requests.Load(), "failed media should stay remote for the new post while retained posts keep valid old assets")
	require.Equal(t, []string{"html", "md", "txt"}, result.PublishedFormats)
	htmlArchive, err := lib.LoadArchive(dir, "html")
	require.NoError(t, err)
	require.Len(t, htmlArchive.Entries, 2)
	entries := make(map[string]lib.ArchiveEntry)
	for _, entry := range htmlArchive.Entries {
		entries[entry.FilePath] = entry
	}
	require.Equal(t, old.Title, entries[htmlPaths[0]].Post.Title)
	require.Equal(t, "images/old.jpg", entries[htmlPaths[0]].LocalCoverImage)
	require.Equal(t, updated.Title, entries[newPath].Post.Title)
	require.Empty(t, entries[newPath].LocalCoverImage)
	require.Equal(t, when, entries[newPath].DownloadTime)
	mdArchive, err := lib.LoadArchive(dir, "md")
	require.NoError(t, err)
	require.Len(t, mdArchive.Entries, 1)
	require.Equal(t, old.Title, mdArchive.Entries[0].Post.Title)
	require.Equal(t, old.Subtitle, mdArchive.Entries[0].Post.Subtitle)
	require.Equal(t, "images/old.jpg", mdArchive.Entries[0].LocalCoverImage)
	require.Contains(t, string(workflowRead(t, filepath.Join(dir, "index.md"))), old.Title)
	require.NotContains(t, string(workflowRead(t, filepath.Join(dir, "index.md"))), updated.Title)
	txtArchive, err := lib.LoadArchive(dir, "txt")
	require.NoError(t, err)
	require.Empty(t, txtArchive.Entries)
	require.NotContains(t, string(workflowRead(t, workflowManifest(dir, "txt"))), old.Title)
}

func TestArchiveWorkflowUnequalArchivesReuseLaterFormatCover(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, "unexpected download")
	}))
	defer server.Close()
	dir := t.TempDir()
	source := server.URL + "/cover.jpg"
	shared := lib.Post{Slug: "shared", Title: "Shared post", CoverImage: source, BodyHTML: "<p>body</p>"}
	workflowSeed(t, dir, "html", []lib.Post{shared}, nil)
	workflowSeed(t, dir, "md", []lib.Post{{Slug: "extra", Title: "Extra", BodyHTML: "<p>extra</p>"}, shared}, []string{"", "images/retained.jpg"})
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "images"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "images", "retained.jpg"), []byte("retained cover"), 0644))
	workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md"})
	require.NoError(t, err)
	result := workflow.Publish(context.Background(), workflowDownloader(dir))
	require.NoError(t, result.Err)
	require.Empty(t, result.Warnings)
	require.Equal(t, []string{"html", "md"}, result.PublishedFormats)
	require.Zero(t, requests.Load(), "all formats must seed retained cover reuse before localization")
	for format, count := range map[string]int{"html": 1, "md": 2} {
		archive, err := lib.LoadArchive(dir, format)
		require.NoError(t, err)
		require.Len(t, archive.Entries, count)
		for _, entry := range archive.Entries {
			if entry.Post.Slug == shared.Slug {
				require.Equal(t, "images/retained.jpg", entry.LocalCoverImage)
			}
		}
		require.Contains(t, string(workflowRead(t, filepath.Join(dir, "index."+format))), "images/retained.jpg")
	}
}

func TestArchiveWorkflowPublicationFailureIsolation(t *testing.T) {
	for _, failure := range []string{"metadata", "index"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md"})
			require.NoError(t, err)
			if failure == "metadata" {
				require.NoError(t, os.Mkdir(workflowManifest(dir, "html"), 0755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("old index"), 0644))
			} else {
				require.NoError(t, os.Mkdir(filepath.Join(dir, "index.html"), 0755))
			}
			result := workflow.Publish(context.Background(), nil)
			require.Error(t, result.Err)
			require.Empty(t, result.Warnings)
			require.Equal(t, []string{"md"}, result.PublishedFormats)
			require.FileExists(t, workflowManifest(dir, "md"))
			require.FileExists(t, filepath.Join(dir, "index.md"))
			if failure == "metadata" {
				// Failed metadata must never expose a newer index.
				require.Equal(t, []byte("old index"), workflowRead(t, filepath.Join(dir, "index.html")))
			} else {
				require.FileExists(t, workflowManifest(dir, "html"), "metadata is published before the index")
				require.DirExists(t, filepath.Join(dir, "index.html"))
			}
		})
	}
}

func TestArchiveWorkflowCanceledPublishPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	before := make(map[string][]byte)
	for _, format := range []string{"html", "md"} {
		workflowSeed(t, dir, format, []lib.Post{{Slug: "post", Title: "Retained", BodyHTML: "<p>body</p>"}}, nil)
		index := filepath.Join(dir, "index."+format)
		require.NoError(t, os.WriteFile(index, []byte("retained index "+format), 0644))
		before[index] = workflowRead(t, index)
		before[workflowManifest(dir, format)] = workflowRead(t, workflowManifest(dir, format))
	}
	workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := workflow.Publish(ctx, nil)
	require.ErrorIs(t, result.Err, context.Canceled)
	require.Empty(t, result.PublishedFormats)
	for path, data := range before {
		require.Equal(t, data, workflowRead(t, path))
	}
}

func TestArchiveWorkflowNoWritesStillInvalidatesConflictingMedia(t *testing.T) {
	for _, laterSuccess := range []bool{false, true} {
		t.Run(fmt.Sprintf("later_success_%t", laterSuccess), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				fmt.Fprint(w, "verified cover")
			}))
			defer server.Close()
			dir := t.TempDir()
			source := server.URL + "/cover.jpg"
			post := lib.Post{Slug: "post", Title: "Retained", CoverImage: source, BodyHTML: "<p>body</p>"}
			for _, format := range []string{"html", "md"} {
				workflowSeed(t, dir, format, []lib.Post{post}, []string{"images/stale.jpg"})
			}
			bodyPath := filepath.Join(dir, "images", "stale.jpg")
			require.NoError(t, os.MkdirAll(filepath.Dir(bodyPath), 0755))
			require.NoError(t, os.WriteFile(bodyPath, []byte("unrelated overwritten bytes"), 0644))
			workflow, err := lib.LoadArchiveWorkflow(dir, []string{"html", "md"})
			require.NoError(t, err)
			firstImages := &lib.ImageDownloadResult{Images: []lib.ImageInfo{
				{OriginalURL: source, LocalPath: bodyPath, Success: true},
			}}
			workflow.RecordPost(post, firstImages, nil)
			firstImages.Images[0].LocalPath = filepath.Join(dir, "unused.jpg")
			// Evidence belongs to another post and no destination write succeeded.
			// It must still invalidate an earlier success at the same asset path.
			laterImages := &lib.ImageDownloadResult{Images: []lib.ImageInfo{
				{OriginalURL: server.URL + "/unrelated.jpg", LocalPath: bodyPath, Success: laterSuccess, Error: errors.New("media failed")},
			}}
			workflow.RecordPost(lib.Post{Slug: "other"}, laterImages, nil)
			laterImages.Images[0].LocalPath = filepath.Join(dir, "unused.jpg")
			result := workflow.Publish(context.Background(), workflowDownloader(dir))
			require.NoError(t, result.Err)
			require.Empty(t, result.Warnings)
			require.Equal(t, int32(1), requests.Load(), "one shared cover fallback must serve both formats")
			for _, format := range []string{"html", "md"} {
				archive, err := lib.LoadArchive(dir, format)
				require.NoError(t, err)
				require.Len(t, archive.Entries, 1)
				cover := archive.Entries[0].LocalCoverImage
				require.Contains(t, cover, "/covers/")
				require.Equal(t, []byte("verified cover"), workflowRead(t, filepath.Join(dir, filepath.FromSlash(cover))))
				require.NotContains(t, string(workflowRead(t, filepath.Join(dir, "index."+format))), "images/stale.jpg")
			}
			require.Equal(t, []byte("unrelated overwritten bytes"), workflowRead(t, bodyPath))
		})
	}
}

func TestArchiveWorkflowMissingDirectoryIsNoOp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	formats := []string{"md", "html"}
	workflow, err := lib.LoadArchiveWorkflow(dir, formats)
	require.NoError(t, err)
	formats[0] = "unsupported" // The workflow owns its selected formats.
	result := workflow.Publish(context.Background(), nil)
	require.NoError(t, result.Err)
	require.Empty(t, result.Warnings)
	require.Empty(t, result.PublishedFormats)
	require.NoDirExists(t, dir)
	// Once the output folder exists, publication uses the original order.
	require.NoError(t, os.Mkdir(dir, 0755))
	result = workflow.Publish(context.Background(), nil)
	require.NoError(t, result.Err)
	require.Empty(t, result.Warnings)
	require.Equal(t, []string{"md", "html"}, result.PublishedFormats)
}
