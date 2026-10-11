package lib

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArchiveRestoreFormatsAndMetadata(t *testing.T) {
	dir := t.TempDir()
	post := Post{Title: "Original title", Slug: "post", PostDate: "2026-01-02T12:00:00Z", Subtitle: "Rich subtitle", Description: "Rich description", CoverImage: "https://example.test/cover.png", BodyHTML: "<p>Body never belongs in metadata</p>"}
	for _, format := range []string{"html", "md", "txt"} {
		file := filepath.Join(dir, "20260102_120000_post."+format)
		require.NoError(t, post.WriteToFile(file, format, false))
		archive, err := LoadArchive(dir, format)
		require.NoError(t, err)
		require.Len(t, archive.Entries, 1)
		require.Equal(t, "Original title", archive.Entries[0].Post.Title)
		require.Equal(t, post.PostDate, archive.Entries[0].Post.PostDate)
		info, err := os.Stat(file)
		require.NoError(t, err)
		require.Equal(t, info.ModTime(), archive.Entries[0].DownloadTime)
		archive.AddEntry(post, file, time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC))
		require.Len(t, archive.Entries, 1)
		require.NoError(t, archive.SaveMetadata(dir, format))
		restored, err := LoadArchive(dir, format)
		require.NoError(t, err)
		require.Len(t, restored.Entries, 1)
		require.Equal(t, post.Subtitle, restored.Entries[0].Post.Subtitle)
		require.Equal(t, post.CoverImage, restored.Entries[0].Post.CoverImage)
		require.Equal(t, post.Description, restored.Entries[0].Post.Description)
		require.Equal(t, archive.Entries[0].DownloadTime, restored.Entries[0].DownloadTime)
		body, err := os.ReadFile(manifestPath(dir, format))
		require.NoError(t, err)
		require.NotContains(t, string(body), "Body never belongs")
		require.NotContains(t, string(body), dir)
	}
	// Moving the folder must keep metadata and links usable.
	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(dir, moved))
	for _, format := range []string{"html", "md", "txt"} {
		restored, err := LoadArchive(moved, format)
		require.NoError(t, err)
		require.Len(t, restored.Entries, 1)
		require.Equal(t, filepath.Join(moved, "20260102_120000_post."+format), restored.Entries[0].FilePath)
	}
}

func TestArchiveRestoreRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "20260102_120000_post.html")
	post := Post{Title: "Post", BodyHTML: "<p>Body</p>"}
	require.NoError(t, post.WriteToFile(file, "html", false))
	archive := NewArchive()
	archive.AddEntry(post, file, time.Now())
	require.NoError(t, archive.SaveMetadata(dir, "html"))
	require.NoError(t, os.WriteFile(file, nil, 0644))
	restored, err := LoadArchive(dir, "html")
	require.NoError(t, err)
	require.Empty(t, restored.Entries)
	require.NoError(t, os.Remove(file))
	restored, err = LoadArchive(dir, "html")
	require.NoError(t, err)
	require.Empty(t, restored.Entries)
	require.NoError(t, os.Mkdir(file, 0755))
	restored, err = LoadArchive(dir, "html")
	require.NoError(t, err)
	require.Empty(t, restored.Entries)
}

func TestArchiveDeterministicUpsert(t *testing.T) {
	archive := NewArchive()
	for _, name := range []string{"b", "a"} {
		archive.AddEntry(Post{Title: "Same", PostDate: "2026-01-01T00:00:00Z"}, name+".html", time.Now())
	}
	archive.AddEntry(Post{Title: "Invalid A", PostDate: "bad"}, "invalid-a.html", time.Now())
	archive.AddEntry(Post{Title: "Invalid B", PostDate: "bad"}, "invalid-b.html", time.Now())
	archive.AddEntry(Post{Title: "Older", PostDate: "2025-01-01T00:00:00Z"}, "older.html", time.Now())
	require.Equal(t, "a.html", archive.Entries[0].FilePath)
	require.Equal(t, "b.html", archive.Entries[1].FilePath)
	require.Equal(t, "Older", archive.Entries[2].Post.Title)
	require.Equal(t, "Invalid A", archive.Entries[3].Post.Title)
	archive.AddEntry(Post{Title: "Newest", PostDate: "2027-01-01T00:00:00Z"}, "a.html", time.Now())
	require.Len(t, archive.Entries, 5)
	require.Equal(t, "Newest", archive.Entries[0].Post.Title)
	archive.RemoveEntry("a.html")
	require.Len(t, archive.Entries, 4)
}

func TestArchivePublicationFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "index.html")
	require.NoError(t, os.Mkdir(destination, 0755))
	sentinel := filepath.Join(destination, "original")
	require.NoError(t, os.WriteFile(sentinel, []byte("old index"), 0644))
	require.Error(t, NewArchive().GenerateHTML(dir))
	content, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "old index", string(content))
	temporary, err := filepath.Glob(filepath.Join(dir, ".sbstck-dl-archive-*"))
	require.NoError(t, err)
	require.Empty(t, temporary)
}

func TestArchiveInvalidMetadata(t *testing.T) {
	for _, body := range []string{`{"version":2,"entries":[]}`, `{"version":1,"entries":[{"file":"../outside.html","title":"Post","download_time":"2026-01-01T00:00:00Z"}]}`, `{"version":1,"entries":[{"file":"20260101_000000_post.md","title":"Post","download_time":"2026-01-01T00:00:00Z"}]}`, `{"version":1,"entries":[{"file":"_post.html","title":"Post","download_time":"2026-01-01T00:00:00Z"},{"file":"_post.html","title":"Post","download_time":"2026-01-01T00:00:00Z"}]}`} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(manifestPath(dir, "html"), []byte(body), 0644))
			_, err := LoadArchive(dir, "html")
			require.Error(t, err)
		})
	}
}

func TestAtomicPostReplacementFailurePreservesPriorEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20260101_120000_existing.html")
	post := Post{Title: "Original post", Slug: "existing", PostDate: "2026-01-01T12:00:00Z", Subtitle: "Retained metadata", BodyHTML: "<p>Complete original body</p>"}
	require.NoError(t, post.WriteToFile(path, "html", false))
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	archive := NewArchive()
	archive.AddEntry(post, path, time.Now())
	require.NoError(t, archive.SaveMetadata(dir, "html"))
	require.NoError(t, archive.GenerateHTML(dir))
	priorIndex, err := os.ReadFile(filepath.Join(dir, "index.html"))
	require.NoError(t, err)
	// Model a disk write that writes only part of the replacement then fails.
	require.ErrorIs(t, writeFileAtomicallyWith(path, func(file *os.File) error {
		_, err := file.Write([]byte("partial replacement"))
		require.NoError(t, err)
		return io.ErrShortWrite
	}), io.ErrShortWrite)
	remaining, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, remaining)
	restored, err := LoadArchive(dir, "html")
	require.NoError(t, err)
	require.Len(t, restored.Entries, 1)
	require.Equal(t, "Original post", restored.Entries[0].Post.Title)
	require.Equal(t, "Retained metadata", restored.Entries[0].Post.Subtitle)
	require.NoError(t, restored.GenerateHTML(dir))
	currentIndex, err := os.ReadFile(filepath.Join(dir, "index.html"))
	require.NoError(t, err)
	require.Equal(t, priorIndex, currentIndex)
	temporary, err := filepath.Glob(filepath.Join(dir, ".sbstck-dl-archive-*"))
	require.NoError(t, err)
	require.Equal(t, []string{manifestPath(dir, "html")}, temporary)
}

func TestAtomicReplacementPreservesRestrictedPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.html")
	require.NoError(t, os.WriteFile(path, []byte("private old post"), 0600))
	require.NoError(t, os.Chmod(path, 0600))
	before, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, writeFileAtomically(path, []byte("private replacement")))
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
	// Windows models only the writable bit, not Unix owner/group permissions.
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), after.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "private replacement", string(body))
}

func TestArchiveIncompleteMetadataPreservesPriorIndex(t *testing.T) {
	for _, record := range []string{
		`{"file":"20260101_120000_post.html","title":"Post"}`,
		`{"file":"20260101_120000_post.html","title":"Post","download_time":"0001-01-01T00:00:00Z"}`,
		`{"file":"20260101_120000_post.html","download_time":"2026-01-02T12:00:00Z"}`,
		`{"file":"20260101_120000_post.html","title":"  ","download_time":"2026-01-02T12:00:00Z"}`,
	} {
		t.Run(record, func(t *testing.T) {
			dir := t.TempDir()
			post := Post{Title: "Prior title", Slug: "post", PostDate: "2026-01-01T12:00:00Z", BodyHTML: "<p>Prior body</p>"}
			file := filepath.Join(dir, "20260101_120000_post.html")
			require.NoError(t, post.WriteToFile(file, "html", false))
			archive := NewArchive()
			archive.AddEntry(post, file, time.Now())
			require.NoError(t, archive.GenerateHTML(dir))
			prior, err := os.ReadFile(filepath.Join(dir, "index.html"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(manifestPath(dir, "html"), []byte(`{"version":1,"entries":[`+record+`]}`), 0644))
			_, err = LoadArchive(dir, "html")
			require.ErrorContains(t, err, "invalid archive metadata file")
			after, err := os.ReadFile(filepath.Join(dir, "index.html"))
			require.NoError(t, err)
			require.Equal(t, prior, after)
		})
	}
}

func TestArchiveInvalidSavePreservesPublishedFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "20260101_120000_post.html")
	archive := NewArchive()
	archive.AddEntry(Post{Title: "Original", Slug: "post", PostDate: "2026-01-01T12:00:00Z"}, file, time.Now())
	require.NoError(t, archive.SaveMetadata(dir, "html"))
	require.NoError(t, archive.GenerateHTML(dir))
	priorMetadata, err := os.ReadFile(manifestPath(dir, "html"))
	require.NoError(t, err)
	priorIndex, err := os.ReadFile(filepath.Join(dir, "index.html"))
	require.NoError(t, err)
	for _, invalid := range []ArchiveEntry{
		{Post: Post{Title: "Original"}, FilePath: file},
		{Post: Post{Title: " "}, FilePath: file, DownloadTime: time.Now()},
	} {
		archive.Entries = []ArchiveEntry{invalid}
		require.Error(t, archive.SaveMetadata(dir, "html"))
		metadata, err := os.ReadFile(manifestPath(dir, "html"))
		require.NoError(t, err)
		require.Equal(t, priorMetadata, metadata)
		index, err := os.ReadFile(filepath.Join(dir, "index.html"))
		require.NoError(t, err)
		require.Equal(t, priorIndex, index)
	}
	archive = NewArchive()
	archive.AddEntry(Post{Slug: "post"}, file, time.Now())
	require.Equal(t, "post", archive.Entries[0].Post.Title)
	require.NoError(t, archive.SaveMetadata(dir, "html"))
}
