package lib

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/language"
)

// EPUBOptions describes one offline export of a local HTML archive.
type EPUBOptions struct {
	Input    string
	Output   string
	Title    string
	Author   string
	Language string
}

// ExportEPUB publishes one complete book, preserving the source archive and any
// previous book if preparing or publishing the replacement fails.
func ExportEPUB(ctx context.Context, options EPUBOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	options.Title = strings.TrimSpace(options.Title)
	if options.Title == "" {
		return fmt.Errorf("EPUB title must not be blank")
	}
	if options.Language == "" {
		options.Language = "und"
	}
	if _, err := language.Parse(options.Language); err != nil {
		return fmt.Errorf("invalid EPUB language tag %q: %w", options.Language, err)
	}
	if !strings.EqualFold(filepath.Ext(options.Output), ".epub") {
		return fmt.Errorf("EPUB output must have an .epub extension")
	}
	info, err := os.Stat(options.Input)
	if err != nil {
		return fmt.Errorf("inspect HTML archive: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("HTML archive input must be a directory")
	}
	options.Input, err = filepath.Abs(options.Input)
	if err != nil {
		return err
	}
	options.Input, err = filepath.EvalSymlinks(options.Input)
	if err != nil {
		return err
	}
	archive, err := loadCompleteEPUBArchive(options.Input)
	if err != nil {
		return err
	}
	if len(archive.Entries) == 0 {
		return fmt.Errorf("HTML archive contains no eligible posts")
	}
	sort.Slice(archive.Entries, func(i, j int) bool {
		a, b := epubDate(archive.Entries[i].Post.PostDate), epubDate(archive.Entries[j].Post.PostDate)
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		if !a.Equal(b) {
			return a.Before(b)
		}
		return filepath.Base(archive.Entries[i].FilePath) < filepath.Base(archive.Entries[j].FilePath)
	})
	chapters, err := prepareEPUBChapters(archive.Entries)
	if err != nil {
		return err
	}
	inputs := map[string]bool{}
	files, err := os.ReadDir(options.Input)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !strings.EqualFold(filepath.Ext(file.Name()), ".epub") {
			inputs[filepath.Join(options.Input, file.Name())] = true
		}
	}
	if err := validateEPUBDestination(options.Output, inputs); err != nil {
		return err
	}
	return writeFileAtomicallyWith(options.Output, func(file *os.File) error {
		z := zip.NewWriter(file)
		write := func(name, body string) error {
			method := uint16(zip.Deflate)
			if name == "mimetype" {
				method = zip.Store
			}
			w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method})
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, body)
			return err
		}
		if err := write("mimetype", "application/epub+zip"); err != nil {
			return err
		}
		if err := write("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="EPUB/package.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`); err != nil {
			return err
		}
		content := &epubContent{ctx: ctx, dir: options.Input, chapters: map[string]*epubChapter{}, images: map[string]*epubImage{}, inputs: inputs, write: write}
		for _, chapter := range chapters {
			content.chapters[chapter.entry.FilePath] = chapter
			content.inputs[chapter.entry.FilePath] = true
		}
		if err := content.prepareImages(chapters); err != nil {
			return err
		}
		var manifest, spine, nav strings.Builder
		spine.WriteString(`<itemref idref="nav"/>`)
		for i, chapter := range chapters {
			entry := chapter.entry
			if err := ctx.Err(); err != nil {
				return err
			}
			body, err := content.render(chapter)
			if err != nil {
				return err
			}
			if !strings.Contains(body, "<h1") {
				body = "<h1>" + epubEscape(entry.Post.Title) + "</h1>" + body
			}
			if !epubDate(entry.Post.PostDate).IsZero() {
				body = "<p>Published: " + epubEscape(entry.Post.PostDate) + "</p>" + body
			}
			name := chapter.name
			if err := write("EPUB/"+name, epubXHTML(entry.Post.Title, options.Language, body)); err != nil {
				return err
			}
			fmt.Fprintf(&manifest, `<item id="chapter-%d" href="%s" media-type="application/xhtml+xml"/>`, i+1, name)
			fmt.Fprintf(&spine, `<itemref idref="chapter-%d"/>`, i+1)
			fmt.Fprintf(&nav, `<li><a href="%s">%s</a></li>`, name, epubEscape(entry.Post.Title))
		}
		for i, resource := range content.resources {
			fmt.Fprintf(&manifest, `<item id="image-%d" href="%s" media-type="%s"/>`, i+1, resource.name, resource.mediaType)
		}
		if err := write("EPUB/style.css", "body{line-height:1.4}img{max-width:100%;height:auto}pre{white-space:pre-wrap}blockquote{margin:1em}table{max-width:100%}"); err != nil {
			return err
		}
		author := ""
		if strings.TrimSpace(options.Author) != "" {
			author = "<dc:creator>" + epubEscape(options.Author) + "</dc:creator>"
		}
		packageXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="book-id">urn:sbstck-dl:%s</dc:identifier><dc:title>%s</dc:title><dc:language>%s</dc:language>%s<meta property="dcterms:modified">%s</meta></metadata><manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="style" href="style.css" media-type="text/css"/>%s</manifest><spine>%s</spine></package>`, rand.Text(), epubEscape(options.Title), epubEscape(options.Language), author, time.Now().UTC().Format("2006-01-02T15:04:05Z"), manifest.String(), spine.String())
		if err := write("EPUB/package.opf", packageXML); err != nil {
			return err
		}
		if err := write("EPUB/nav.xhtml", epubXHTML(options.Title, options.Language, `<nav epub:type="toc"><h1>Table of contents</h1><ol>`+nav.String()+`</ol></nav>`)); err != nil {
			return err
		}
		if err := z.Close(); err != nil {
			return err
		}
		if err := validateEPUBDestination(options.Output, inputs); err != nil {
			return err
		}
		return ctx.Err()
	})
}

func validateEPUBDestination(output string, inputs map[string]bool) error {
	info, err := os.Lstat(output)
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("EPUB output %q must be a regular file, not a symlink or directory", output)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect EPUB output: %w", err)
	}
	parent, err := filepath.Abs(filepath.Dir(output))
	if err != nil {
		return err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("inspect EPUB output directory: %w", err)
	}
	canonical := filepath.Join(parent, filepath.Base(output))
	for input := range inputs {
		if canonical == input {
			return fmt.Errorf("EPUB output %q overlaps an archive input resource", output)
		}
		if info != nil {
			if source, err := os.Stat(input); err == nil && os.SameFile(info, source) {
				return fmt.Errorf("EPUB output %q aliases an archive input resource", output)
			}
		}
	}
	return nil
}

// Index rebuilding tolerates vanished posts; book export must account for every
// retained record before it can describe the result as complete.
func loadCompleteEPUBArchive(dir string) (*Archive, error) {
	archive, err := LoadArchive(dir, "html")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(manifestPath(dir, "html"))
	if os.IsNotExist(err) {
		return archive, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest archiveManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(archive.Entries))
	for _, entry := range archive.Entries {
		present[filepath.Base(entry.FilePath)] = true
	}
	for _, record := range manifest.Entries {
		if !present[record.File] {
			return nil, fmt.Errorf("required post %s is missing, empty, or not a regular file", record.File)
		}
	}
	return archive, nil
}

func epubDate(value string) time.Time {
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if date, err := time.Parse(layout, value); err == nil {
			return date
		}
	}
	return time.Time{}
}

func epubEscape(value string) string {
	var out strings.Builder
	xml.EscapeText(&out, []byte(value))
	return out.String()
}

func epubXHTML(title, language, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="` + epubEscape(language) + `" lang="` + epubEscape(language) + `"><head><title>` + epubEscape(title) + `</title><link rel="stylesheet" type="text/css" href="style.css"/></head><body>` + body + `</body></html>`
}
