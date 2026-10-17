package lib

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
)

type epubChapter struct {
	entry ArchiveEntry
	name  string
	doc   *html.Node
	ids   map[string]string
}

type epubImage struct {
	name, mediaType string
}

// epubContent converts archived HTML to static XHTML and packages the local
// reading resources it references. It has no downloader or network client.
type epubContent struct {
	ctx       context.Context
	dir       string
	chapters  map[string]*epubChapter
	images    map[string]epubImage
	resources []epubImage
	inputs    map[string]bool
	write     func(string, string) error
}

func prepareEPUBChapters(entries []ArchiveEntry) ([]*epubChapter, error) {
	chapters := make([]*epubChapter, 0, len(entries))
	for i, entry := range entries {
		body, err := os.ReadFile(entry.FilePath)
		if err != nil {
			return nil, fmt.Errorf("read post %s: %w", entry.FilePath, err)
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, fmt.Errorf("required post %s is empty", entry.FilePath)
		}
		doc, err := html.Parse(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("parse post %s: %w", entry.FilePath, err)
		}
		chapter := &epubChapter{entry: entry, name: fmt.Sprintf("chapter-%d.xhtml", i+1), doc: doc, ids: map[string]string{}}
		nextID := 0
		walkEPUBReadingNodes(doc, func(n *html.Node) {
			if !epubAllowedTags[n.Data] {
				return
			}
			id := ""
			for j := range n.Attr {
				a := &n.Attr[j]
				if a.Key != "id" && !(n.Data == "a" && a.Key == "name") {
					continue
				}
				original := a.Val
				if _, exists := chapter.ids[original]; exists || original == "" {
					a.Key = "" // Duplicate original IDs must not produce duplicate XML IDs.
					continue
				}
				if id == "" {
					nextID++
					id = fmt.Sprintf("id-%d", nextID)
					a.Key, a.Val = "id", id
				} else {
					a.Key = "" // The legacy name is an alias for this element's ID.
				}
				chapter.ids[original] = id
			}
		})
		chapters = append(chapters, chapter)
	}
	return chapters, nil
}

func walkEPUBReadingNodes(n *html.Node, visit func(*html.Node)) {
	switch n.Data {
	case "head", "script", "style", "noscript", "audio", "video", "iframe", "embed", "object", "source":
		return
	}
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkEPUBReadingNodes(child, visit)
	}
}

func walkEPUBNodes(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkEPUBNodes(child, visit)
	}
}

func epubAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func epubWebLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if u.Scheme == "" && u.Host != "" {
		u.Scheme = "https"
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host != "" {
			return u.String()
		}
	case "mailto", "tel":
		return u.String()
	}
	return ""
}

func (c *epubContent) localPath(raw string) (string, *url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "" || u.Host != "" || strings.Contains(u.Path, "\\") {
		return "", nil, fmt.Errorf("not an archive-relative URL: %q", raw)
	}
	return filepath.Join(c.dir, filepath.FromSlash(u.Path)), u, nil
}

func (c *epubContent) addImage(raw string) (string, error) {
	if err := c.ctx.Err(); err != nil {
		return "", err
	}
	if cached, ok := c.images[raw]; ok {
		return cached.name, nil
	}
	var data []byte
	if strings.HasPrefix(raw, "data:image/") {
		parts := strings.SplitN(raw, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return "", fmt.Errorf("required inline image must use base64 encoding")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return "", fmt.Errorf("decode required inline image: %w", err)
		}
	} else {
		path, _, err := c.localPath(raw)
		if err != nil || raw == "" {
			return "", fmt.Errorf("required image %q is not local; prepare the HTML archive with --download-images before exporting", raw)
		}
		c.inputs[path] = true
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("required image %q is missing or unreadable; prepare the HTML archive with --download-images: %w", raw, err)
		}
		rel, err := filepath.Rel(c.dir, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("required image %q is outside the HTML archive", raw)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("required image %q is not a readable regular file", raw)
		}
		if cached, ok := c.images[resolved]; ok {
			return cached.name, nil
		}
		data, err = os.ReadFile(resolved)
		if err != nil {
			return "", fmt.Errorf("read required image %q: %w", raw, err)
		}
		c.inputs[resolved] = true
		raw = resolved
	}
	config, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("required image %q is corrupt or unsupported; use a local PNG, JPEG, GIF, or WebP (including existing PNG previews): %w", raw, err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 50_000_000 {
		return "", fmt.Errorf("required image %q exceeds the 50-megapixel decoding limit", raw)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("required image %q is corrupt: %w", raw, err)
	}
	mediaType, extension := "image/"+kind, kind
	if kind == "jpeg" {
		extension = "jpg"
	}
	resource := epubImage{name: fmt.Sprintf("images/image-%d.%s", len(c.resources)+1, extension), mediaType: mediaType}
	if err := c.write("EPUB/"+resource.name, string(data)); err != nil {
		return "", err
	}
	c.images[raw] = resource
	c.resources = append(c.resources, resource)
	return resource.name, nil
}

func (c *epubContent) link(chapter *epubChapter, raw string) string {
	if web := epubWebLink(raw); web != "" {
		return web
	}
	path, u, err := c.localPath(raw)
	if err != nil {
		return ""
	}
	if u.Path == "" {
		path = chapter.entry.FilePath
	}
	c.inputs[path] = true
	if target, ok := c.chapters[path]; ok {
		link := target.name
		if u.Path == "" {
			link = ""
		}
		if id, ok := target.ids[u.Fragment]; ok {
			link += "#" + id
		}
		if link == "" {
			return chapter.name
		}
		return link
	}
	if resource, ok := c.images[path]; ok {
		return resource.name
	}
	return ""
}

func (c *epubContent) render(chapter *epubChapter) (string, error) {
	// Register images first, so links to a reading image work regardless of order.
	var imageErr error
	walkEPUBReadingNodes(chapter.doc, func(n *html.Node) {
		if imageErr == nil && n.Type == html.ElementNode && n.Data == "img" {
			_, imageErr = c.addImage(epubAttr(n, "src"))
		}
	})
	if imageErr != nil {
		return "", fmt.Errorf("post %s: %w", filepath.Base(chapter.entry.FilePath), imageErr)
	}
	var out strings.Builder
	var render func(*html.Node) error
	render = func(n *html.Node) error {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if n.Type == html.TextNode {
			out.WriteString(epubEscape(n.Data))
			return nil
		}
		if n.Type != html.ElementNode && n.Type != html.DocumentNode {
			return nil
		}
		tag := n.Data
		switch tag {
		case "head", "script", "style", "noscript", "source", "meta", "link", "input", "button":
			return nil
		case "svg":
			return fmt.Errorf("post %s contains an unsupported inline SVG; use a local PNG preview", filepath.Base(chapter.entry.FilePath))
		case "audio", "video", "iframe", "embed", "object":
			var links []string
			seen := map[string]bool{}
			walkEPUBNodes(n, func(child *html.Node) {
				for _, key := range []string{"src", "href", "data"} {
					raw := epubAttr(child, key)
					if raw == "" {
						continue
					}
					if web := epubWebLink(raw); web != "" && !seen[web] {
						seen[web] = true
						links = append(links, `<a href="`+epubEscape(web)+`">Open original media</a>`)
					} else if path, _, err := c.localPath(raw); err == nil {
						c.inputs[path] = true
					}
				}
			})
			if len(links) == 0 {
				links = append(links, "Media unavailable in this book")
			}
			out.WriteString(" <span>" + strings.Join(links, " · ") + "</span> ")
			return nil
		}
		allowed := epubAllowedTags[tag]
		if tag == "a" {
			if href := c.link(chapter, epubAttr(n, "href")); href != "" {
				out.WriteString(`<a href="` + epubEscape(href) + `"`)
			} else {
				tag = "span"
				out.WriteString("<span")
			}
		} else if allowed {
			out.WriteString("<" + tag)
		}
		if allowed {
			for _, a := range n.Attr {
				if epubAllowedAttribute(tag, a.Key, a.Val) {
					out.WriteString(" " + a.Key + `="` + epubEscape(a.Val) + `"`)
				}
			}
			if tag == "img" {
				name, err := c.addImage(epubAttr(n, "src"))
				if err != nil {
					return err
				}
				out.WriteString(` src="` + epubEscape(name) + `" alt="` + epubEscape(epubAttr(n, "alt")) + `"`)
			}
			out.WriteString(">")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := render(child); err != nil {
				return err
			}
		}
		if n.Data == "a" && tag == "span" && epubAttr(n, "href") != "" {
			out.WriteString(" (unavailable in this book)")
		}
		if allowed {
			out.WriteString("</" + tag + ">")
		}
		return nil
	}
	if err := render(chapter.doc); err != nil {
		return "", err
	}
	return out.String(), nil
}

var epubAllowedTags = func() map[string]bool {
	tags := "a abbr address article aside b bdi bdo blockquote br caption cite code col colgroup dd del details dfn div dl dt em figcaption figure footer h1 h2 h3 h4 h5 h6 header hr i img ins kbd li main mark ol p pre q rp rt ruby s samp section small span strong sub summary sup table tbody td tfoot th thead time tr u ul var wbr"
	result := map[string]bool{}
	for _, tag := range strings.Fields(tags) {
		result[tag] = true
	}
	return result
}()

func epubAllowedAttribute(tag, key, value string) bool {
	if key == "id" || key == "title" || key == "lang" || key == "dir" {
		return true
	}
	if (tag == "td" || tag == "th") && (key == "colspan" || key == "rowspan") || tag == "ol" && key == "start" || tag == "li" && key == "value" {
		_, err := strconv.Atoi(value)
		return err == nil
	}
	return false
}
