package lib

import (
	"fmt"
	"strings"
	"time"
)

// ToObsidianMD converts the Post's HTML body to Obsidian-optimized Markdown.
func (p *Post) ToObsidianMD(withTitle bool) (string, error) {
	return convertHTMLToObsidianMarkdown(p.BodyHTML, *p, withTitle)
}

func convertHTMLToObsidianMarkdown(html string, post Post, withTitle bool) (string, error) {
	mdContent, err := mdConverter.ConvertString(html)
	if err != nil {
		return "", err
	}
	if withTitle {
		mdContent = fmt.Sprintf("# %s\n\n%s", post.Title, mdContent)
	}
	return renderObsidianMarkdown(mdContent, post), nil
}

func transformObsidianMarkdown(content string) string {
	normalized := normalizeLineEndings(content)
	frontmatter, body, hasFrontmatter := extractFrontmatter(normalized)
	if hasFrontmatter {
		if body == "" {
			return frontmatter
		}
		return frontmatter + "\n" + transformOutsideCodeFences(body)
	}
	return transformOutsideCodeFences(normalized)
}

func renderObsidianMarkdown(content string, post Post) string {
	normalized := normalizeLineEndings(content)
	frontmatter, body, hasFrontmatter := extractFrontmatter(normalized)
	if hasFrontmatter {
		if body == "" {
			return frontmatter
		}
		return frontmatter + "\n" + transformOutsideCodeFences(body)
	}
	body = transformOutsideCodeFences(normalized)
	frontmatter = buildObsidianFrontmatter(post)
	if frontmatter == "" {
		return body
	}
	if body == "" {
		return frontmatter
	}
	return frontmatter + "\n\n" + body
}

func normalizeLineEndings(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return content
}

func extractFrontmatter(content string) (string, string, bool) {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return "", content, false
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			frontmatter := strings.Join(lines[:i+1], "\n")
			if i+1 < len(lines) {
				return frontmatter, strings.Join(lines[i+1:], "\n"), true
			}
			return frontmatter, "", true
		}
	}
	return "", content, false
}

func buildObsidianFrontmatter(post Post) string {
	title := strings.TrimSpace(post.Title)
	title = escapeYAMLString(title)
	created := earliestPublicationDate(post)
	tags := collectPostTags(post)

	var parts []string
	parts = append(parts, "---")
	parts = append(parts, fmt.Sprintf("title: \"%s\"", title))
	parts = append(parts, "tags: "+formatYAMLList(tags))
	if created != "" {
		parts = append(parts, "created: "+created)
	}
	parts = append(parts, "---")
	return strings.Join(parts, "\n")
}

func escapeYAMLString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func collectPostTags(post Post) []string {
	values := make([]string, 0)
	seen := make(map[string]struct{})
	addTag := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}

	for _, tag := range post.Tags {
		if tag.Name != "" {
			addTag(tag.Name)
			continue
		}
		addTag(tag.Slug)
	}
	for _, tag := range post.Categories {
		if tag.Name != "" {
			addTag(tag.Name)
			continue
		}
		addTag(tag.Slug)
	}

	return values
}

func formatYAMLList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "\""+escapeYAMLString(value)+"\"")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func earliestPublicationDate(post Post) string {
	candidates := []string{
		post.FirstPublishedAt,
		post.PostDate,
		post.PublishedAt,
	}
	var earliest time.Time
	found := false
	for _, candidate := range candidates {
		if parsed, ok := parseSubstackDate(candidate); ok {
			if !found || parsed.Before(earliest) {
				earliest = parsed
				found = true
			}
		}
	}
	if !found {
		return ""
	}
	return earliest.UTC().Format("2006-01-02")
}

func parseSubstackDate(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
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
