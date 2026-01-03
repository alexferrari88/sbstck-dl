package cmd

import (
	"fmt"
	"strings"
)

const defaultDownloadFormat = "html+md"

var supportedFormats = []string{"html", "md", "obsidian-md", "txt"}

func parseFormats(value string) ([]string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return []string{"html", "obsidian-md"}, nil
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '+' || r == ','
	})
	formats := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	hasHTML := false
	hasMD := false
	hasObsidian := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !isSupportedFormat(part) {
			return nil, fmt.Errorf("invalid format %q (use html, md, obsidian-md, txt, or combine with '+' or ',' like html+md)", part)
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		formats = append(formats, part)
		switch part {
		case "html":
			hasHTML = true
		case "md":
			hasMD = true
		case "obsidian-md":
			hasObsidian = true
		}
	}
	if len(formats) == 0 {
		return []string{"html", "obsidian-md"}, nil
	}
	if hasHTML && hasMD && !hasObsidian {
		formats = replaceFormat(formats, "md", "obsidian-md")
	}
	return formats, nil
}

func parseSingleFormat(value string) (string, error) {
	formats, err := parseFormats(value)
	if err != nil {
		return "", err
	}
	if len(formats) != 1 {
		return "", fmt.Errorf("format must be a single value (html, md, obsidian-md, or txt)")
	}
	return formats[0], nil
}

func isSupportedFormat(value string) bool {
	for _, format := range supportedFormats {
		if value == format {
			return true
		}
	}
	return false
}

func replaceFormat(formats []string, from string, to string) []string {
	out := make([]string, 0, len(formats))
	seen := make(map[string]struct{}, len(formats))
	for _, format := range formats {
		if format == from {
			format = to
		}
		if _, ok := seen[format]; ok {
			continue
		}
		seen[format] = struct{}{}
		out = append(out, format)
	}
	return out
}
