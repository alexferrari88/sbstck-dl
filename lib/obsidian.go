package lib

import (
	"fmt"
	"strings"
)

// ToObsidianMD converts the Post's HTML body to Obsidian-optimized Markdown.
func (p *Post) ToObsidianMD(withTitle bool) (string, error) {
	return convertHTMLToObsidianMarkdown(p.BodyHTML, p.Title, withTitle)
}

func convertHTMLToObsidianMarkdown(html, title string, withTitle bool) (string, error) {
	mdContent, err := mdConverter.ConvertString(html)
	if err != nil {
		return "", err
	}
	if withTitle {
		mdContent = fmt.Sprintf("# %s\n\n%s", title, mdContent)
	}
	return transformObsidianMarkdown(mdContent), nil
}

func transformObsidianMarkdown(content string) string {
	normalized := normalizeLineEndings(content)
	frontmatter, body := splitFrontmatter(normalized)
	return frontmatter + transformOutsideCodeFences(body)
}

func normalizeLineEndings(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}

func splitFrontmatter(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return "", content
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			frontmatter := strings.Join(lines[:i+1], "\n")
			if i+1 < len(lines) {
				return frontmatter + "\n", strings.Join(lines[i+1:], "\n")
			}
			return frontmatter, ""
		}
	}
	return "", content
}

func transformOutsideCodeFences(content string) string {
	lines := strings.Split(content, "\n")
	var out strings.Builder
	inFence := false
	fenceMarker := ""

	for i, line := range lines {
		marker, ok := fenceMarkerFromLine(line)
		if ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if fenceMatches(marker, fenceMarker) {
				inFence = false
				fenceMarker = ""
			}
			out.WriteString(line)
		} else if inFence {
			out.WriteString(line)
		} else {
			out.WriteString(transformMarkdownLinks(line))
		}

		if i < len(lines)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func fenceMarkerFromLine(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return "", false
	}
	if trimmed[0] != '`' && trimmed[0] != '~' {
		return "", false
	}
	ch := trimmed[0]
	count := 0
	for count < len(trimmed) && trimmed[count] == ch {
		count++
	}
	if count < 3 {
		return "", false
	}
	return trimmed[:count], true
}

func fenceMatches(marker, open string) bool {
	if marker == "" || open == "" {
		return false
	}
	return marker[0] == open[0] && len(marker) >= len(open)
}

func transformMarkdownLinks(line string) string {
	if !strings.Contains(line, "[") {
		return line
	}
	var out strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '!' && i+1 < len(line) && line[i+1] == '[' {
			converted, consumed := convertImageLinkAt(line[i:])
			if consumed > 0 {
				out.WriteString(converted)
				i += consumed
				continue
			}
		}
		if line[i] == '[' {
			converted, consumed := convertMarkdownLinkAt(line[i:])
			if consumed > 0 {
				out.WriteString(converted)
				i += consumed
				continue
			}
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

func convertImageLinkAt(source string) (string, int) {
	if len(source) < 2 || source[0] != '!' || source[1] != '[' {
		return "", 0
	}
	_, dest, raw, ok := parseMarkdownLink(source[1:])
	if !ok {
		return "", 0
	}
	consumed := len(raw) + 1
	if dest == "" || isExternalLink(dest) {
		return "!" + raw, consumed
	}
	path := cleanLocalPath(dest)
	return fmt.Sprintf("![[%s]]", path), consumed
}

func convertMarkdownLinkAt(source string) (string, int) {
	label, dest, raw, ok := parseMarkdownLink(source)
	if !ok {
		return "", 0
	}
	consumed := len(raw)
	if dest == "" || isExternalLink(dest) {
		return raw, consumed
	}
	path, anchor := splitAnchor(dest)
	if path == "" {
		return raw, consumed
	}
	path = cleanLocalPath(path)
	lower := strings.ToLower(path)
	if !strings.HasSuffix(lower, ".md") {
		return raw, consumed
	}
	wikiPath := path[:len(path)-3]
	if anchor != "" {
		wikiPath += "#" + anchor
	}
	if label == wikiPath {
		return "[[" + wikiPath + "]]", consumed
	}
	return "[[" + wikiPath + "|" + label + "]]", consumed
}

func parseMarkdownLink(source string) (label, dest, raw string, ok bool) {
	if source == "" || source[0] != '[' {
		return "", "", "", false
	}
	endLabel := strings.IndexByte(source, ']')
	if endLabel == -1 {
		return "", "", "", false
	}
	if endLabel+1 >= len(source) || source[endLabel+1] != '(' {
		return "", "", "", false
	}
	destStart := endLabel + 2
	destEnd := findClosingParen(source, destStart)
	if destEnd == -1 {
		return "", "", "", false
	}
	label = source[1:endLabel]
	raw = source[:destEnd+1]
	dest = parseLinkDestination(source[destStart:destEnd])
	return label, dest, raw, true
}

func findClosingParen(source string, start int) int {
	depth := 0
	for i := start; i < len(source); i++ {
		switch source[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func parseLinkDestination(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "<") {
		if idx := strings.Index(trimmed, ">"); idx != -1 {
			return strings.TrimSpace(trimmed[1:idx])
		}
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == ' ' || trimmed[i] == '\t' {
			return trimmed[:i]
		}
	}
	return trimmed
}

func splitAnchor(dest string) (string, string) {
	parts := strings.SplitN(dest, "#", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return dest, ""
}

func cleanLocalPath(path string) string {
	trimmed := strings.TrimPrefix(path, "./")
	return strings.TrimPrefix(trimmed, ".\\")
}

func isExternalLink(dest string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(dest))
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return true
	}
	if strings.HasPrefix(trimmed, "//") {
		return true
	}
	if strings.HasPrefix(trimmed, "mailto:") || strings.HasPrefix(trimmed, "tel:") || strings.HasPrefix(trimmed, "data:") {
		return true
	}
	return strings.Contains(trimmed, "://")
}
