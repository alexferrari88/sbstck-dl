package lib_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func TestExportEPUBRetainedPostsMustBeComplete(t *testing.T) {
	for _, missing := range []string{"missing", "empty", "directory", "whitespace"} {
		t.Run(missing, func(t *testing.T) {
			dir := t.TempDir()
			archive := lib.NewArchive()
			first := filepath.Join(dir, "20260101_120000_first.html")
			second := filepath.Join(dir, "20260102_120000_second.html")
			for _, path := range []string{first, second} {
				p := lib.Post{Title: "Required post", BodyHTML: "<p>Keep all posts</p>"}
				require.NoError(t, p.WriteToFile(path, "html", false))
				archive.AddEntry(p, path, time.Now())
			}
			require.NoError(t, archive.SaveMetadata(dir, "html"))
			require.NoError(t, os.Remove(first))
			switch missing {
			case "empty":
				require.NoError(t, os.WriteFile(first, nil, 0644))
			case "whitespace":
				require.NoError(t, os.WriteFile(first, []byte(" \n"), 0644))
			case "directory":
				require.NoError(t, os.Mkdir(first, 0755))
			}
			book := filepath.Join(t.TempDir(), "previous.epub")
			require.NoError(t, os.WriteFile(book, []byte("previous book"), 0600))
			err := lib.ExportEPUB(context.Background(), lib.EPUBOptions{Input: dir, Output: book, Title: "Complete"})
			require.Error(t, err)
			require.Contains(t, err.Error(), filepath.Base(first))
			got, err := os.ReadFile(book)
			require.NoError(t, err)
			require.Equal(t, "previous book", string(got))
		})
	}
}

func TestExportEPUBWebPReadingImage(t *testing.T) {
	dir := t.TempDir()
	data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "photo.webp"), data, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_post.html"), []byte(`<p>A WebP image</p><img src="photo.webp" alt="A pixel">`), 0644))
	err = lib.ExportEPUB(context.Background(), lib.EPUBOptions{Input: dir, Output: filepath.Join(t.TempDir(), "webp.epub"), Title: "WebP"})
	require.NoError(t, err)
}

func TestExportEPUBNeverOverwritesInputResources(t *testing.T) {
	for _, collision := range []string{"image", "hardlink", "symlink", "parent alias"} {
		t.Run(collision, func(t *testing.T) {
			if runtime.GOOS == "windows" && collision != "image" {
				t.Skip("link creation requires platform privileges")
			}
			dir := t.TempDir()
			post := filepath.Join(dir, "_post.html")
			body := []byte("<h1>Keep this source</h1><p>Original contents</p>")
			book := filepath.Join(dir, "out.epub")
			switch collision {
			case "image", "parent alias":
				// Image content is detected from bytes, never from its filename.
				var data bytes.Buffer
				require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))))
				require.NoError(t, os.WriteFile(book, data.Bytes(), 0644))
				body = []byte(`<h1>Keep source</h1><img src="out.epub" alt="Do not replace">`)
				if collision == "parent alias" {
					alias := filepath.Join(t.TempDir(), "alias")
					require.NoError(t, os.Symlink(dir, alias))
					book = filepath.Join(alias, "out.epub")
				}
			case "hardlink":
				require.NoError(t, os.WriteFile(post, body, 0644))
				require.NoError(t, os.Link(post, book))
			case "symlink":
				require.NoError(t, os.Symlink(post, book))
			}
			require.NoError(t, os.WriteFile(post, body, 0644))
			before, err := os.ReadFile(book)
			require.NoError(t, err)
			err = lib.ExportEPUB(context.Background(), lib.EPUBOptions{Input: dir, Output: book, Title: "Keep inputs"})
			require.Error(t, err, "must reject a destination that aliases a source")
			after, err := os.ReadFile(book)
			require.NoError(t, err)
			require.Equal(t, before, after)
			afterPost, err := os.ReadFile(post)
			require.NoError(t, err)
			require.Equal(t, body, afterPost)
		})
	}
}

func TestExportEPUBRejectsInvalidContentAndPreservesPreviousBook(t *testing.T) {
	for _, broken := range []string{"title", "language", "remote image", "missing image", "corrupt image", "corrupt metadata", "empty archive"} {
		t.Run(broken, func(t *testing.T) {
			dir := t.TempDir()
			post := filepath.Join(dir, "_post.html")
			body := "<h1>Post</h1><p>Readable content</p>"
			options := lib.EPUBOptions{Input: dir, Output: filepath.Join(t.TempDir(), "book.epub"), Title: "A book"}
			switch broken {
			case "title":
				options.Title = " \n "
			case "language":
				options.Language = "not a language tag"
			case "remote image":
				body += `<img src="https://example.invalid/image.png" alt="Remote">`
			case "missing image":
				body += `<img src="images/gone.png" alt="Gone">`
			case "corrupt image":
				body += `<img src="truncated.png" alt="Corrupt">`
				header := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137}
				require.NoError(t, os.WriteFile(filepath.Join(dir, "truncated.png"), header, 0644))
			case "corrupt metadata":
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".sbstck-dl-archive-html.json"), []byte("{broken"), 0644))
			}
			if broken != "empty archive" {
				require.NoError(t, os.WriteFile(post, []byte(body), 0644))
			}
			require.NoError(t, os.WriteFile(options.Output, []byte("previous book"), 0600))
			err := lib.ExportEPUB(context.Background(), options)
			require.Error(t, err)
			if strings.Contains(broken, "image") && broken != "corrupt image" {
				require.Contains(t, err.Error(), "--download-images")
			}
			data, err := os.ReadFile(options.Output)
			require.NoError(t, err)
			require.Equal(t, "previous book", string(data))
		})
	}
}

func TestExportEPUBCancellationPreservesBook(t *testing.T) {
	dir := t.TempDir()
	// One large text node keeps publication in progress long enough to cancel
	// after the replacement starts being prepared in the output directory.
	body := "<p>" + strings.Repeat("A long chapter. ", 1_000_000) + "</p>"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_long.html"), []byte(body), 0644))
	outputDir := t.TempDir()
	book := filepath.Join(outputDir, "book.epub")
	require.NoError(t, os.WriteFile(book, []byte("previous book"), 0600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- lib.ExportEPUB(ctx, lib.EPUBOptions{Input: dir, Output: book, Title: "Long book"})
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	waiting := true
	for waiting {
		select {
		case err := <-done:
			t.Fatalf("export finished before cancellation: %v", err)
		case <-timeout.C:
			t.Fatal("export did not start preparing a replacement")
		case <-ticker.C:
			files, err := os.ReadDir(outputDir)
			require.NoError(t, err)
			if len(files) > 1 {
				cancel()
				waiting = false
			}
		}
	}
	require.ErrorIs(t, <-done, context.Canceled)
	data, err := os.ReadFile(book)
	require.NoError(t, err)
	require.Equal(t, "previous book", string(data))
	files, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Len(t, files, 1, "no partial replacement may be left behind")
}

func TestExportEPUBFilesystemFailuresPreserveSources(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix file permissions and symlinks")
	}
	for _, broken := range []string{"unreadable post", "unreadable image", "outside image", "output directory"} {
		t.Run(broken, func(t *testing.T) {
			dir := t.TempDir()
			post := filepath.Join(dir, "_post.html")
			body := []byte("<h1>Keep source</h1>")
			var imageData bytes.Buffer
			require.NoError(t, png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
			photo := filepath.Join(dir, "photo.png")
			require.NoError(t, os.WriteFile(photo, imageData.Bytes(), 0644))
			if broken == "unreadable image" || broken == "outside image" {
				body = append(body, []byte(`<img src="photo.png" alt="Image">`)...)
			}
			require.NoError(t, os.WriteFile(post, body, 0644))
			outdir := t.TempDir()
			book := filepath.Join(outdir, "previous.epub")
			require.NoError(t, os.WriteFile(book, []byte("previous book"), 0644))
			switch broken {
			case "unreadable post":
				require.NoError(t, os.Chmod(post, 0000))
				t.Cleanup(func() { os.Chmod(post, 0644) })
			case "unreadable image":
				require.NoError(t, os.Chmod(photo, 0000))
				t.Cleanup(func() { os.Chmod(photo, 0644) })
			case "outside image":
				outside := filepath.Join(t.TempDir(), "outside.png")
				require.NoError(t, os.WriteFile(outside, imageData.Bytes(), 0644))
				require.NoError(t, os.Remove(photo))
				require.NoError(t, os.Symlink(outside, photo))
			case "output directory":
				require.NoError(t, os.Chmod(outdir, 0555))
				t.Cleanup(func() { os.Chmod(outdir, 0755) })
			}
			err := lib.ExportEPUB(context.Background(), lib.EPUBOptions{Input: dir, Output: book, Title: "Keep inputs"})
			require.Error(t, err)
			got, err := os.ReadFile(book)
			require.NoError(t, err)
			require.Equal(t, "previous book", string(got))
			if broken != "unreadable post" {
				got, err := os.ReadFile(post)
				require.NoError(t, err)
				require.Equal(t, body, got)
			}
		})
	}
}
