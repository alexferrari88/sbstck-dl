package cmd

import (
	"net/url"
	"path/filepath"
	"strings"
)

func resolveOutputFolder(outputFolder string, downloadURL string) string {
	cleaned := strings.TrimSpace(outputFolder)
	if cleaned == "" {
		cleaned = "."
	}
	if filepath.Clean(cleaned) != "." {
		return cleaned
	}
	name := derivePublicationFolderName(downloadURL)
	if name == "" {
		return cleaned
	}
	return name
}

func derivePublicationFolderName(downloadURL string) string {
	trimmed := strings.TrimSpace(downloadURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")

	if host == "substack.com" {
		path := strings.Trim(parsed.Path, "/")
		if path != "" {
			parts := strings.Split(path, "/")
			if len(parts) > 0 && strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 {
				return sanitizeFolderName(parts[0][1:])
			}
		}
		return sanitizeFolderName(host)
	}

	if strings.HasSuffix(host, ".substack.com") {
		name := strings.TrimSuffix(host, ".substack.com")
		if name != "" {
			return sanitizeFolderName(name)
		}
	}

	return sanitizeFolderName(host)
}

func sanitizeFolderName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var out strings.Builder
	prevDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			out.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			out.WriteRune('-')
			prevDash = true
		}
	}
	result := strings.Trim(out.String(), "-.")
	return result
}
