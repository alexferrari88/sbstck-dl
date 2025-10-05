package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var errCanonicalNotFound = errors.New("canonical url not found")

type aliasType int

const (
	aliasTypeUnknown aliasType = iota
	aliasTypePost
	aliasTypeNote
	aliasTypeProfile
)

// NormalizeURL attempts to resolve and canonicalize Substack URLs so callers can use any
// front-door link (e.g. substack.com/@handle/p-123) and still reach the underlying
// publication domain.
func NormalizeURL(ctx context.Context, fetcher *Fetcher, rawURL string) (string, error) {
	if rawURL == "" {
		return "", errors.New("empty url")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	normalized := rawURL
	aliasKind := aliasTypeUnknown

	if !isSubstackAliasHost(parsed.Host) && strings.Contains(parsed.Path, "/note/") {
		if noteID := noteIdentifierFromPath(parsed.Path); noteID != "" {
			normalized = fmt.Sprintf("https://substack.com/note/%s", noteID)
			parsed, _ = url.Parse(normalized)
		}
	}

	if isSubstackAliasHost(parsed.Host) && strings.HasPrefix(parsed.Path, "/@") {
		switch {
		case strings.Contains(parsed.Path, "/note/"):
			aliasKind = aliasTypeNote
		case strings.Contains(parsed.Path, "/p-"):
			aliasKind = aliasTypePost
		default:
			aliasKind = aliasTypeProfile
		}
	}

	if aliasKind != aliasTypeUnknown {
		if canonical, err := canonicalizeSubstackAlias(ctx, fetcher, rawURL, aliasKind); err == nil && canonical != "" {
			normalized = canonical
		}
	} else if isSubstackAliasHost(parsed.Host) && strings.HasPrefix(parsed.Path, "/profile/") {
		if canonical, err := canonicalizeProfileURL(ctx, fetcher, rawURL); err == nil && canonical != "" {
			normalized = canonical
		}
	}

	resolved, err := fetcher.ResolveFinalURL(ctx, normalized)
	if err != nil {
		if fetchErr, ok := err.(*FetchError); ok {
			if fetchErr.StatusCode == http.StatusNotFound || fetchErr.StatusCode == http.StatusForbidden {
				return normalized, nil
			}
		}
		return "", err
	}

	return resolved, nil
}

func isSubstackAliasHost(host string) bool {
	host = strings.ToLower(host)
	return host == "substack.com" || host == "www.substack.com"
}

func canonicalizeSubstackAlias(ctx context.Context, fetcher *Fetcher, rawURL string, kind aliasType) (string, error) {
	body, err := fetcher.FetchURL(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	htmlBytes, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return "", err
	}

	if canonical, exists := doc.Find("link[rel='canonical']").Attr("href"); exists {
		canonical = strings.TrimSpace(canonical)
		if canonical != "" && !sameURL(canonical, rawURL) && kind != aliasTypeProfile {
			return canonical, nil
		}
	}

	preloads, err := parsePreloads(doc)
	if err == nil {
		switch kind {
		case aliasTypePost:
			if canonical := strings.TrimSpace(canonicalFromPreloads(preloads, false)); canonical != "" && !sameURL(canonical, rawURL) {
				return canonical, nil
			}
		case aliasTypeNote:
			if canonical := strings.TrimSpace(canonicalFromPreloads(preloads, true)); canonical != "" && !sameURL(canonical, rawURL) {
				return canonical, nil
			}
		case aliasTypeProfile:
			if base := strings.TrimSpace(canonicalDomainFromPreloads(preloads)); base != "" && !sameURL(base, rawURL) {
				return ensureTrailingSlash(base), nil
			}
		}
	}

	if kind == aliasTypeNote {
		if notePost, err := extractNoteFromPreloads(doc, rawURL); err == nil {
			canonical := strings.TrimSpace(notePost.CanonicalUrl)
			if canonical != "" && !sameURL(canonical, rawURL) {
				return canonical, nil
			}
		}
	}

	return "", errCanonicalNotFound
}

func canonicalizeProfileURL(ctx context.Context, fetcher *Fetcher, rawURL string) (string, error) {
	body, err := fetcher.FetchURL(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	htmlBytes, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlBytes))
	if err != nil {
		return "", err
	}

	if canonical, exists := doc.Find("link[rel='canonical']").Attr("href"); exists {
		canonical = strings.TrimSpace(canonical)
		if canonical != "" && strings.HasPrefix(canonical, "https://substack.com/@") {
			return canonical, nil
		}
	}

	preloads, err := parsePreloads(doc)
	if err == nil {
		if base := strings.TrimSpace(canonicalDomainFromPreloads(preloads)); base != "" {
			return ensureTrailingSlash(base), nil
		}
	}

	return "", errCanonicalNotFound
}

func canonicalFromPreloads(preloads map[string]interface{}, isNote bool) string {
	if !isNote {
		if postData, ok := preloads["post"].(map[string]interface{}); ok {
			if canonical, ok := postData["canonical_url"].(string); ok && canonical != "" {
				return canonical
			}
		}
		if feedData, ok := preloads["feedData"].(map[string]interface{}); ok {
			if initial, ok := feedData["initialPost"].(map[string]interface{}); ok {
				if post, ok := initial["post"].(map[string]interface{}); ok {
					if canonical, ok := post["canonical_url"].(string); ok && canonical != "" {
						return canonical
					}
				}
			}
		}
	} else {
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
		}
	}

	return ""
}

func canonicalDomainFromPreloads(preloads map[string]interface{}) string {
	if profile, ok := preloads["profile"].(map[string]interface{}); ok {
		if primary, ok := profile["primaryPublication"].(map[string]interface{}); ok {
			if customDomain, ok := primary["custom_domain"].(string); ok && customDomain != "" {
				return normalizeDomain(customDomain)
			}
			if subdomain, ok := primary["subdomain"].(string); ok && subdomain != "" {
				return fmt.Sprintf("https://%s.substack.com", subdomain)
			}
		}
	}

	if canonical := canonicalFromPreloads(preloads, false); canonical != "" {
		if u, err := url.Parse(canonical); err == nil {
			u.Path = ""
			u.RawQuery = ""
			u.Fragment = ""
			return u.String()
		}
	}

	return ""
}

func parsePreloads(doc *goquery.Document) (map[string]interface{}, error) {
	htmlContent, err := doc.Html()
	if err != nil {
		return nil, fmt.Errorf("failed to get HTML content: %w", err)
	}

	preloadsStart := strings.Index(htmlContent, "window._preloads")
	if preloadsStart == -1 {
		return nil, errors.New("no window._preloads found")
	}

	jsonParseStart := strings.Index(htmlContent[preloadsStart:], "JSON.parse(\"")
	if jsonParseStart == -1 {
		return nil, errors.New("no JSON.parse found in window._preloads")
	}
	jsonParseStart += preloadsStart + len("JSON.parse(\"")

	jsonEnd := strings.Index(htmlContent[jsonParseStart:], "\")")
	if jsonEnd == -1 {
		return nil, errors.New("could not find end of JSON string")
	}
	jsonEnd += jsonParseStart

	jsonStr := htmlContent[jsonParseStart:jsonEnd]
	unesescapedJSON, err := jsonUnescape(jsonStr)
	if err != nil {
		return nil, fmt.Errorf("failed to unescape JSON: %w", err)
	}

	var preloadsData map[string]interface{}
	if err := json.Unmarshal([]byte(unesescapedJSON), &preloadsData); err != nil {
		return nil, fmt.Errorf("failed to parse preloads JSON: %w", err)
	}

	return preloadsData, nil
}

func normalizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		domain = "https://" + domain
	}
	return strings.TrimRight(domain, "/")
}

func ensureTrailingSlash(s string) string {
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, "/") {
		return s + "/"
	}
	return s
}

func noteIdentifierFromPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	last := segments[len(segments)-1]
	if last == "" {
		return ""
	}
	return last
}

func sameURL(a, b string) bool {
	au, errA := url.Parse(a)
	bu, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return strings.TrimRight(strings.ToLower(a), "/") == strings.TrimRight(strings.ToLower(b), "/")
	}

	if !strings.EqualFold(au.Scheme, bu.Scheme) {
		return false
	}

	if !strings.EqualFold(au.Host, bu.Host) {
		return false
	}

	if strings.TrimRight(au.Path, "/") != strings.TrimRight(bu.Path, "/") {
		return false
	}

	if au.RawQuery != bu.RawQuery {
		return false
	}

	if au.Fragment != bu.Fragment {
		return false
	}

	return true
}
