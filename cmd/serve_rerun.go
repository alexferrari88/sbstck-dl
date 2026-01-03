package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
)

type rerunRequest struct {
	PublicationURL string `json:"publication_url"`
	Output         string `json:"output"`
	Format         string `json:"format"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Rate           string `json:"rate"`
	MaxWorkers     string `json:"max_workers"`
	Proxy          string `json:"proxy"`
	CookieName     string `json:"cookie_name"`
	CookieVal      string `json:"cookie_val"`
	CookieValFile  string `json:"cookie_val_file"`
	CookieJar      string `json:"cookie_jar"`
	CookieKeychain string `json:"cookie_keychain"`
}

type rerunResponse struct {
	OK         bool     `json:"ok"`
	LastRun    string   `json:"last_run,omitempty"`
	NewPosts   int      `json:"new_posts"`
	Errors     []string `json:"errors,omitempty"`
	SitemapURL string   `json:"sitemap_url,omitempty"`
}

func serveRerun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireCSRF(w, r) {
		return
	}

	var req rerunRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, rerunResponse{
			OK:     false,
			Errors: []string{fmt.Sprintf("Invalid request payload: %v", err)},
		})
		return
	}

	errorsList := make([]string, 0)
	pubURL, err := parseURL(strings.TrimSpace(req.PublicationURL))
	if err != nil || pubURL == nil {
		errorsList = append(errorsList, "Substack URL must be a valid http(s) URL.")
	}

	var proxyURL *url.URL
	if strings.TrimSpace(req.Proxy) != "" {
		parsedProxy, err := parseURL(strings.TrimSpace(req.Proxy))
		if err != nil {
			errorsList = append(errorsList, "Proxy URL must be a valid http(s) URL.")
		} else {
			proxyURL = parsedProxy
		}
	}

	formats, formatErr := parseFormats(req.Format)
	if formatErr != nil {
		errorsList = append(errorsList, formatErr.Error())
	}

	outputDir := strings.TrimSpace(req.Output)
	if outputDir == "" {
		outputDir = "."
	}

	domainHint := ""
	if pubURL != nil {
		domainHint = pubURL.Hostname()
	}

	cookie, cookieErrors := resolveTestCookie(testConnectionRequest{
		CookieName:     req.CookieName,
		CookieVal:      req.CookieVal,
		CookieValFile:  req.CookieValFile,
		CookieJar:      req.CookieJar,
		CookieKeychain: req.CookieKeychain,
	}, domainHint)
	errorsList = append(errorsList, cookieErrors...)

	response := rerunResponse{
		OK:       false,
		NewPosts: 0,
	}
	if pubURL != nil {
		response.SitemapURL = buildSitemapURL(pubURL)
	}

	if pubURL == nil || formatErr != nil {
		response.Errors = errorsList
		response.OK = false
		writeJSON(w, http.StatusOK, response)
		return
	}

	manifestPath := filepath.Join(outputDir, lib.ManifestFilename)
	manifest, err := lib.LoadManifest(manifestPath)
	if err != nil {
		errorsList = append(errorsList, fmt.Sprintf("Failed to read manifest: %v", err))
		manifest = lib.NewManifest()
	}
	response.LastRun = manifestLastRun(manifest)

	if strings.Contains(pubURL.Path, "/p/") {
		if entry, ok := manifest.Entries[pubURL.String()]; ok && entry.DownloadedAt != "" {
			response.LastRun = entry.DownloadedAt
			response.NewPosts = 0
		} else {
			response.NewPosts = 1
		}
		response.Errors = errorsList
		response.OK = len(errorsList) == 0
		writeJSON(w, http.StatusOK, response)
		return
	}

	rate := parsePositiveInt(req.Rate, lib.DefaultRatePerSecond)
	maxWorkers := parsePositiveInt(req.MaxWorkers, lib.DefaultMaxWorkers)

	fetcher := lib.NewFetcher(
		lib.WithRatePerSecond(rate),
		lib.WithMaxWorkers(maxWorkers),
		lib.WithProxyURL(proxyURL),
		lib.WithCookie(cookie),
	)
	extractor := lib.NewExtractor(fetcher)

	dateFilter := makeDateFilterFunc(strings.TrimSpace(req.Before), strings.TrimSpace(req.After))
	entries, err := extractor.GetAllPostsEntries(ctx, pubURL.String(), dateFilter)
	if err != nil {
		errorsList = append(errorsList, fmt.Sprintf("Failed to fetch sitemap: %v", err))
		response.Errors = errorsList
		response.OK = false
		writeJSON(w, http.StatusOK, response)
		return
	}

	newPosts := make(map[string]struct{})
	for _, nextFormat := range formats {
		filtered, _, _, err := filterEntriesForDownload(entries, outputDir, nextFormat, manifest, false)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to filter existing posts for %s: %v", nextFormat, err))
		}
		for _, entry := range filtered {
			if entry.URL != "" {
				newPosts[entry.URL] = struct{}{}
			}
		}
	}
	response.NewPosts = len(newPosts)
	response.Errors = errorsList
	response.OK = len(errorsList) == 0
	writeJSON(w, http.StatusOK, response)
}

func manifestLastRun(manifest *lib.Manifest) string {
	if manifest == nil {
		return ""
	}
	if manifest.UpdatedAt != "" {
		return manifest.UpdatedAt
	}
	var latest time.Time
	for _, entry := range manifest.Entries {
		if entry.DownloadedAt == "" {
			continue
		}
		if parsed, ok := parseDateInput(entry.DownloadedAt); ok {
			if latest.IsZero() || parsed.After(latest) {
				latest = parsed
			}
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.UTC().Format(time.RFC3339)
}
