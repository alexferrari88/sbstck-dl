package cmd

import (
	"archive/zip"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func TestExportEPUBCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, "%s", out)
	run := func(args ...string) (string, error) {
		out, err := exec.Command(binary, append([]string{"export-epub"}, args...)...).CombinedOutput()
		return string(out), err
	}
	t.Run("invalid invocations fail without publishing", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "_post.html"), []byte("<p>A post</p>"), 0644))
		book := filepath.Join(t.TempDir(), "book.epub")
		for _, args := range [][]string{
			{"-i", dir, "-o", book},
			{"-i", dir, "-o", book, "--title", " "},
			{"-i", dir, "--title", "Book"},
			{"-i", dir, "-o", book, "--title", "Book", "extra-argument"},
			{"-i", dir, "-o", book, "--title", "Book", "--after", "2026-01-01"},
			{"-i", dir, "-o", book, "--title", "Book", "--before", "2026-01-01"},
			{"-i", dir, "-o", book, "--title", "Book", "--language", "bad language"},
		} {
			out, err := run(args...)
			require.Error(t, err, "%v: %s", args, out)
			require.NoFileExists(t, book)
		}
		out, err := run("--help")
		require.NoError(t, err)
		for _, text := range []string{"oldest first", "no network", "--input", "--output", "--title", "--author", "--language", "PDF"} {
			require.Contains(t, out, text)
		}
	})
	t.Run("EPUB option values bypass download format normalization", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "_post.html"), []byte("<p>A post</p>"), 0644))
		book := filepath.Join(t.TempDir(), "flags.epub")
		out, err := run("-i", dir, "-o", book, "--title", "-fiction", "--author", "-fwriter")
		require.NoError(t, err, "%s", out)
		files := readEPUB(t, book)
		require.Contains(t, files["EPUB/package.opf"], "<dc:title>-fiction</dc:title>")
		require.Contains(t, files["EPUB/package.opf"], "<dc:creator>-fwriter</dc:creator>")
	})
	t.Run("images and chapter links travel with the book while media remain web links or notes", func(t *testing.T) {
		var requests atomic.Int32
		trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Error(w, "export must stay offline", 500)
		}))
		defer trap.Close()
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "images"), 0755))
		photo, err := os.Create(filepath.Join(dir, "images", "photo.png"))
		require.NoError(t, err)
		im := image.NewRGBA(image.Rect(0, 0, 32, 32))
		im.Set(10, 10, color.RGBA{R: 255, A: 255})
		require.NoError(t, png.Encode(photo, im))
		require.NoError(t, photo.Close())
		first := `<h1>First</h1><p>αβ café</p><picture><source srcset="` + trap.URL + `/unused.png"><img src="images/photo.png" alt="A photo" onload="bad()"></picture><a href="20260102_120000_second%20post.html#section%20one">Next chapter</a><a href="` + trap.URL + `/source">Original</a><audio src="podcasts/episode.mp3"><img src="` + trap.URL + `/ignored-media-fallback.png"></audio><video><source src="` + trap.URL + `/video.mp4"></video><a href="files/report.pdf">Report</a><iframe src="` + trap.URL + `/embed"></iframe><script>bad()</script>`
		second := `<h1>Second</h1><h2 id="section one">A section</h2><a href="#section%20one">Same section</a><blockquote><p>A quotation</p></blockquote><a id="new-anchor" name="legacy-anchor"></a><a href="#legacy-anchor">Legacy anchor</a>`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "20260101_120000_first.html"), []byte(first), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "20260102_120000_second post.html"), []byte(second), 0644))
		book := filepath.Join(t.TempDir(), "portable.epub")
		out, err := run("-i", dir, "-o", book, "--title", "Portable")
		require.NoError(t, err, "%s", out)
		files := readEPUB(t, book)
		chapter := files["EPUB/chapter-1.xhtml"]
		require.Contains(t, chapter, "αβ café")
		require.Contains(t, chapter, `src="images/image-1.png"`)
		require.NotEmpty(t, files["EPUB/images/image-1.png"])
		require.Contains(t, chapter, `href="chapter-2.xhtml#id-1"`)
		require.Contains(t, files["EPUB/chapter-2.xhtml"], `id="id-1"`)
		require.Contains(t, files["EPUB/chapter-2.xhtml"], `href="#id-1"`)
		require.Contains(t, files["EPUB/chapter-2.xhtml"], `href="#id-2"`)
		require.Contains(t, chapter, `href="`+trap.URL+`/source"`)
		require.Contains(t, chapter, `href="`+trap.URL+`/video.mp4"`)
		require.Contains(t, chapter, `href="`+trap.URL+`/embed"`)
		require.Contains(t, chapter, "Report")
		require.Contains(t, chapter, "unavailable in this book")
		for _, forbidden := range []string{"<script", "onload", "srcset", "<audio", "<video", "<iframe", `href="files/`, `src="podcasts/`} {
			require.NotContains(t, chapter, forbidden)
		}
		require.Zero(t, requests.Load())
		require.NoError(t, os.Rename(dir, dir+"-moved"))
		t.Cleanup(func() { os.RemoveAll(dir + "-moved") })
		require.Equal(t, files, readEPUB(t, book), "book still reads after source directory is moved")
		if jar := os.Getenv("EPUBCHECK_JAR"); jar != "" {
			out, err := exec.Command("java", "-jar", jar, book, "--failonwarnings").CombinedOutput()
			require.NoError(t, err, "EPUBCheck: %s", out)
		}
	})
	t.Run("chronological chapters use retained metadata and stable unknown order", func(t *testing.T) {
		dir := t.TempDir()
		archive := lib.NewArchive()
		for _, fixture := range []struct{ file, title, date string }{
			{"20260103_120000_newest.html", "Newest", "2026-01-03T12:00:00Z"},
			{"20260102_120000_tie-b.html", "Tie B", "2026-01-02T12:00:00Z"},
			{"20260102_120000_tie-a.html", "Tie A", "2026-01-02T12:00:00Z"},
			{"20260101_120000_oldest.html", "Oldest", "2026-01-01T12:00:00Z"},
			{"_zeta.html", "Unknown Z", "nonsense"},
			{"_alpha.html", "Unknown A", ""},
		} {
			path := filepath.Join(dir, fixture.file)
			require.NoError(t, os.WriteFile(path, []byte("<p>Chapter body</p>"), 0644))
			archive.AddEntry(lib.Post{Title: fixture.title, PostDate: fixture.date}, path, time.Now())
		}
		require.NoError(t, archive.SaveMetadata(dir, "html"))
		for _, name := range []string{"index.html", "notes.html", "20260105_120000_empty.html", "20260106_120000_blank.html", "20260107_120000_other.md"} {
			body := []byte("Ignored")
			if strings.Contains(name, "empty") || strings.Contains(name, "blank") {
				body = []byte(" \n")
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0644))
		}
		book := filepath.Join(t.TempDir(), "ordered.epub")
		out, err := run("-i", dir, "-o", book, "--title", "Ordered", "--author", "A Writer", "--language", "de")
		require.NoError(t, err, "%s", out)
		files := readEPUB(t, book)
		nav := files["EPUB/nav.xhtml"]
		last := -1
		for _, title := range []string{"Oldest", "Tie A", "Tie B", "Newest", "Unknown A", "Unknown Z"} {
			position := strings.Index(nav, ">"+title+"</a>")
			require.Greater(t, position, last, "chapter order: %s", nav)
			last = position
		}
		require.NotContains(t, nav, "Ignored")
		require.Contains(t, files["EPUB/package.opf"], "<dc:creator>A Writer</dc:creator>")
		require.Contains(t, files["EPUB/package.opf"], "<dc:language>de</dc:language>")
		require.Contains(t, files["EPUB/chapter-1.xhtml"], "Oldest")
	})
	t.Run("one legacy post becomes a portable book", func(t *testing.T) {
		dir := t.TempDir()
		body := []byte(`<h1>A café &amp; a story</h1><p>Read <em>slowly</em>.</p><ul><li>First</li></ul>`)
		post := filepath.Join(dir, "20260101_120000_story.html")
		require.NoError(t, os.WriteFile(post, body, 0644))
		book := filepath.Join(t.TempDir(), "stories.epub")
		out, err := run("--input", dir, "--output", book, "--title", "Collected & selected", "--rate", "0", "--proxy", ":invalid")
		require.NoError(t, err, "%s", out)
		require.Contains(t, out, book)
		files := readEPUB(t, book)
		require.Equal(t, "application/epub+zip", files["mimetype"])
		require.Contains(t, files["EPUB/package.opf"], "Collected &amp; selected")
		require.Contains(t, files["EPUB/package.opf"], "<dc:language>und</dc:language>")
		require.NotContains(t, files["EPUB/package.opf"], "dc:creator")
		require.Contains(t, files["EPUB/nav.xhtml"], `href="chapter-1.xhtml"`)
		require.Contains(t, files["EPUB/chapter-1.xhtml"], "A café &amp; a story")
		require.Contains(t, files["EPUB/chapter-1.xhtml"], "<em>slowly</em>")
		require.Contains(t, files["EPUB/chapter-1.xhtml"], "<li>First</li>")
		after, err := os.ReadFile(post)
		require.NoError(t, err)
		require.Equal(t, body, after)
		if jar := os.Getenv("EPUBCHECK_JAR"); jar != "" {
			out, err := exec.Command("java", "-jar", jar, book, "--failonwarnings").CombinedOutput()
			require.NoError(t, err, "EPUBCheck: %s", out)
		}
	})
}

func readEPUB(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer z.Close()
	require.NotEmpty(t, z.File)
	require.Equal(t, "mimetype", z.File[0].Name)
	require.Equal(t, uint16(zip.Store), z.File[0].Method)
	files := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		files[f.Name] = string(data)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".opf") || strings.HasSuffix(f.Name, ".xhtml") {
			d := xml.NewDecoder(strings.NewReader(string(data)))
			for {
				_, err := d.Token()
				if err == io.EOF {
					break
				}
				require.NoError(t, err, "XML in %s", f.Name)
			}
		}
	}
	return files
}
