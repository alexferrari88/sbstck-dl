package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var errCanonicalNotFound = errors.New("canonical url not found")

// NormalizeURL rewrites known Substack URL aliases into canonical forms.
func NormalizeURL(ctx context.Context, fetcher *Fetcher, rawURL string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("empty url")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	hostLower := strings.ToLower(parsed.Host)
	isAliasHost := isSubstackHost(hostLower)

	// Custom-domain note URLs -> canonical Substack note URL
	if !isAliasHost {
		if noteID := noteIdentifierFromPath(parsed.Path); noteID != "" {
			return fmt.Sprintf("%s/note/%s", notesBaseURL, noteID), nil
		}
	}

	if fetcher == nil {
		return rawURL, nil
	}

	// Handle /profile/.../note/c-*
	if isAliasHost && strings.HasPrefix(parsed.Path, "/profile/") && strings.Contains(parsed.Path, "/note/") {
		if noteID := noteIdentifierFromPath(parsed.Path); noteID != "" {
			return fmt.Sprintf("%s/note/%s", notesBaseURL, noteID), nil
		}
	}

	// Handle alias URLs of the form https://substack.com/@handle/p-... or /note/...
	if isAliasHost && strings.HasPrefix(parsed.Path, "/@") {
		canonical, err := resolveCanonical(ctx, fetcher, rawURL)
		if err == nil && canonical != "" {
			return canonical, nil
		}

		// notes fallback
		if strings.Contains(parsed.Path, "/note/") {
			if noteID := noteIdentifierFromPath(parsed.Path); noteID != "" {
				return fmt.Sprintf("%s/note/%s", notesBaseURL, noteID), nil
			}
		}

		return rawURL, nil
	}

	// For other URLs, attempt to resolve via canonical link (custom domains etc.)
	canonical, err := resolveCanonical(ctx, fetcher, rawURL)
	if err == nil && canonical != "" {
		return canonical, nil
	}

	return rawURL, nil
}

func isSubstackHost(host string) bool {
	return host == "substack.com" || host == "www.substack.com"
}

func resolveCanonical(ctx context.Context, fetcher *Fetcher, pageURL string) (string, error) {
	body, err := fetcher.FetchURL(ctx, pageURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	if canonical, exists := doc.Find("link[rel='canonical']").Attr("href"); exists && canonical != "" {
		return canonical, nil
	}

	if canonical := canonicalFromPreloads(data); canonical != "" {
		return canonical, nil
	}

	return "", errCanonicalNotFound
}

func canonicalFromPreloads(htmlBytes []byte) string {
	idx := bytes.Index(htmlBytes, []byte("window._preloads"))
	if idx == -1 {
		return ""
	}
	jsonStart := bytes.Index(htmlBytes[idx:], []byte("JSON.parse(\""))
	if jsonStart == -1 {
		return ""
	}
	jsonStart += idx + len("JSON.parse(\"")
	jsonEnd := bytes.Index(htmlBytes[jsonStart:], []byte("\")"))
	if jsonEnd == -1 {
		return ""
	}
	jsonEnd += jsonStart
	encoded := htmlBytes[jsonStart:jsonEnd]

	decoded, err := jsonUnescape(encoded)
	if err != nil {
		return ""
	}

	var preloads map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &preloads); err != nil {
		return ""
	}

	if post, ok := preloads["post"].(map[string]interface{}); ok {
		if canonical, ok := post["canonical_url"].(string); ok && canonical != "" {
			return canonical
		}
	}

	if feedData, ok := preloads["feedData"].(map[string]interface{}); ok {
		if feedItem, ok := feedData["feedItem"].(map[string]interface{}); ok {
			if canonical, ok := feedItem["canonical_url"].(string); ok && canonical != "" {
				return canonical
			}
			if comment, ok := feedItem["comment"].(map[string]interface{}); ok {
				if canonical, ok := comment["canonical_url"].(string); ok && canonical != "" {
					return canonical
				}
			}
		}
		if initialPost, ok := feedData["initialPost"].(map[string]interface{}); ok {
			if post, ok := initialPost["post"].(map[string]interface{}); ok {
				if canonical, ok := post["canonical_url"].(string); ok && canonical != "" {
					return canonical
				}
			}
		}
	}

	return ""
}

func jsonUnescape(b []byte) (string, error) {
	var s string
	if err := json.Unmarshal([]byte("\""+string(b)+"\""), &s); err != nil {
		return "", err
	}
	return s, nil
}

func noteIdentifierFromPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	id := segments[len(segments)-1]
	if !strings.HasPrefix(id, "c-") {
		return ""
	}
	return id
}
