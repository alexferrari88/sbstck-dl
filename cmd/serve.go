package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/spf13/cobra"
)

var (
	servePort   int
	serveOpen   bool
	uiCSRFToken string
	serveCmd    = &cobra.Command{
		Use:   "serve",
		Short: "Launch a local-only web UI",
		Run: func(cmd *cobra.Command, args []string) {
			if servePort < 1 || servePort > 65535 {
				log.Fatalf("invalid --port %d (must be 1-65535)", servePort)
			}
			if uiCSRFToken == "" {
				uiCSRFToken = generateCSRFToken()
			}
			addr := fmt.Sprintf("127.0.0.1:%d", servePort)
			uiURL := fmt.Sprintf("http://%s", addr)
			server := &http.Server{
				Addr:    addr,
				Handler: serveUIHandler(),
			}

			errCh := make(chan error, 1)
			go func() {
				errCh <- server.ListenAndServe()
			}()

			fmt.Printf("UI running at %s (Ctrl+C to stop)\n", uiURL)
			if serveOpen {
				go func() {
					time.Sleep(200 * time.Millisecond)
					if err := openBrowser(uiURL); err != nil {
						log.Printf("Failed to open browser: %v\n", err)
					}
				}()
			}

			if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("UI server failed: %v", err)
			}
		},
	}
)

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8787, "Port to bind the local UI server")
	serveCmd.Flags().BoolVar(&serveOpen, "open", false, "Open the UI in your default browser")
}

func serveUIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveUIRoot)
	mux.HandleFunc("/api/test-connection", serveTestConnection)
	mux.HandleFunc("/api/preview", servePreview)
	mux.HandleFunc("/api/secret", serveSecret)
	mux.HandleFunc("/api/rerun", serveRerun)
	mux.HandleFunc("/api/jobs", serveJobs)
	mux.HandleFunc("/api/jobs/", serveJobs)
	return mux
}

func serveUIRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.ReplaceAll(serveHTML, "{{CSRF_TOKEN}}", uiCSRFToken)
	fmt.Fprint(w, page)
}

type testConnectionRequest struct {
	PublicationURL string `json:"publication_url"`
	PrivateURL     string `json:"private_url"`
	CookieName     string `json:"cookie_name"`
	CookieVal      string `json:"cookie_val"`
	CookieValFile  string `json:"cookie_val_file"`
	CookieJar      string `json:"cookie_jar"`
	CookieKeychain string `json:"cookie_keychain"`
	Proxy          string `json:"proxy"`
}

type testConnectionResponse struct {
	OK            bool     `json:"ok"`
	SitemapURL    string   `json:"sitemap_url"`
	SitemapStatus int      `json:"sitemap_status,omitempty"`
	SitemapOK     bool     `json:"sitemap_ok"`
	PrivateURL    string   `json:"private_url,omitempty"`
	PrivateStatus int      `json:"private_status,omitempty"`
	PrivateOK     bool     `json:"private_ok"`
	Errors        []string `json:"errors,omitempty"`
}

type previewRequest struct {
	PublicationURL string `json:"publication_url"`
	Output         string `json:"output"`
	Format         string `json:"format"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Force          bool   `json:"force"`
	SkipExisting   bool   `json:"skip_existing"`
	RefreshUpdated bool   `json:"refresh_updated"`
	Rate           string `json:"rate"`
	MaxWorkers     string `json:"max_workers"`
	Proxy          string `json:"proxy"`
	CookieName     string `json:"cookie_name"`
	CookieVal      string `json:"cookie_val"`
	CookieValFile  string `json:"cookie_val_file"`
	CookieJar      string `json:"cookie_jar"`
	CookieKeychain string `json:"cookie_keychain"`
}

type previewResponse struct {
	OK          bool     `json:"ok"`
	Total       int      `json:"total"`
	ToDownload  int      `json:"to_download"`
	Skipped     int      `json:"skipped"`
	Refreshed   int      `json:"refreshed,omitempty"`
	OldestDate  string   `json:"oldest_date,omitempty"`
	NewestDate  string   `json:"newest_date,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	SitemapURL  string   `json:"sitemap_url,omitempty"`
	OutputDir   string   `json:"output_dir,omitempty"`
	Format      string   `json:"format,omitempty"`
	SkipApplied bool     `json:"skip_applied"`
}

func serveTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireCSRF(w, r) {
		return
	}

	var req testConnectionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeTestResponse(w, testConnectionResponse{
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

	domainHint := ""
	if pubURL != nil {
		domainHint = pubURL.Hostname()
	}

	cookie, cookieErrors := resolveTestCookie(req, domainHint)
	errorsList = append(errorsList, cookieErrors...)

	response := testConnectionResponse{
		SitemapURL: strings.TrimSpace(req.PublicationURL),
		PrivateURL: strings.TrimSpace(req.PrivateURL),
	}

	if pubURL != nil {
		response.SitemapURL = buildSitemapURL(pubURL)
	}

	client := newTestClient(proxyURL)

	if pubURL != nil {
		status, err := fetchStatus(client, response.SitemapURL, nil)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to fetch sitemap: %v", err))
		} else {
			response.SitemapStatus = status
			response.SitemapOK = status >= 200 && status < 300
			if !response.SitemapOK {
				errorsList = append(errorsList, fmt.Sprintf("Sitemap returned status %d.", status))
			}
		}
	}

	privateURL := strings.TrimSpace(req.PrivateURL)
	if privateURL != "" {
		if _, err := parseURL(privateURL); err != nil {
			errorsList = append(errorsList, "Private post URL must be a valid http(s) URL.")
		} else if cookie == nil {
			errorsList = append(errorsList, "Cookie name/value required to test a private post URL.")
		} else {
			status, err := fetchStatus(client, privateURL, cookie)
			if err != nil {
				errorsList = append(errorsList, fmt.Sprintf("Failed to fetch private post: %v", err))
			} else {
				response.PrivateStatus = status
				response.PrivateOK = status >= 200 && status < 300
				if !response.PrivateOK {
					errorsList = append(errorsList, fmt.Sprintf("Private post returned status %d.", status))
				}
			}
		}
	}

	response.Errors = errorsList
	response.OK = len(errorsList) == 0 && response.SitemapOK && (privateURL == "" || response.PrivateOK)
	writeTestResponse(w, response)
}

func servePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !requireCSRF(w, r) {
		return
	}

	var req previewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writePreviewResponse(w, previewResponse{
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

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "html"
	}

	pubURLString := ""
	if pubURL != nil {
		pubURLString = pubURL.String()
	}
	outputDir := resolveOutputFolder(req.Output, pubURLString)

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

	rate := parsePositiveInt(req.Rate, lib.DefaultRatePerSecond)
	maxWorkers := parsePositiveInt(req.MaxWorkers, lib.DefaultMaxWorkers)

	fetcher := lib.NewFetcher(
		lib.WithRatePerSecond(rate),
		lib.WithMaxWorkers(maxWorkers),
		lib.WithProxyURL(proxyURL),
		lib.WithCookie(cookie),
	)
	extractor := lib.NewExtractor(fetcher)

	response := previewResponse{
		OutputDir: outputDir,
		Format:    format,
	}
	if pubURL != nil {
		response.SitemapURL = buildSitemapURL(pubURL)
	}

	if pubURL == nil {
		response.Errors = errorsList
		response.OK = false
		writePreviewResponse(w, response)
		return
	}

	dateFilter := makeDateFilterFunc(strings.TrimSpace(req.Before), strings.TrimSpace(req.After))
	entries, err := extractor.GetAllPostsEntries(ctx, pubURL.String(), dateFilter)
	if err != nil {
		errorsList = append(errorsList, fmt.Sprintf("Failed to fetch sitemap: %v", err))
		response.Errors = errorsList
		response.OK = false
		writePreviewResponse(w, response)
		return
	}

	response.Total = len(entries)
	oldest, newest := summarizeEntryDates(entries)
	response.OldestDate = oldest
	response.NewestDate = newest

	skipExistingMode := !req.Force
	if req.SkipExisting {
		skipExistingMode = true
	}
	response.SkipApplied = skipExistingMode

	if skipExistingMode {
		manifestPath := filepath.Join(outputDir, lib.ManifestFilename)
		manifest, err := lib.LoadManifest(manifestPath)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to read manifest: %v", err))
			manifest = lib.NewManifest()
		}
		filtered, skipped, refreshed, err := filterEntriesForDownload(entries, outputDir, format, manifest, req.RefreshUpdated)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to filter existing posts: %v", err))
		}
		response.ToDownload = len(filtered)
		response.Skipped = skipped
		response.Refreshed = refreshed
	} else {
		response.ToDownload = len(entries)
		response.Skipped = 0
		response.Refreshed = 0
	}

	response.Errors = errorsList
	response.OK = len(errorsList) == 0
	writePreviewResponse(w, response)
}

func writePreviewResponse(w http.ResponseWriter, response previewResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func writeTestResponse(w http.ResponseWriter, response testConnectionResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func resolveTestCookie(req testConnectionRequest, domainHint string) (*http.Cookie, []string) {
	errorsList := make([]string, 0)
	var cn cookieName
	if strings.TrimSpace(req.CookieName) != "" {
		if err := cn.Set(strings.TrimSpace(req.CookieName)); err != nil {
			errorsList = append(errorsList, err.Error())
		}
	}

	cookieVal, err := resolveCookieValue(strings.TrimSpace(req.CookieVal), strings.TrimSpace(req.CookieValFile))
	if err != nil {
		errorsList = append(errorsList, fmt.Sprintf("Failed to read cookie value: %v", err))
	}

	if cookieVal == "" && strings.TrimSpace(req.CookieKeychain) != "" {
		value, err := secretStore.Get(strings.TrimSpace(req.CookieKeychain))
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to read cookie from keychain: %v", err))
		} else {
			cookieVal = value
		}
	}

	if cookieVal == "" && strings.TrimSpace(req.CookieJar) != "" {
		jarName, jarValue, err := readCookieFromJar(strings.TrimSpace(req.CookieJar), cn, domainHint)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("Failed to read cookie jar: %v", err))
		} else {
			if cn == "" {
				cn = jarName
			}
			cookieVal = jarValue
		}
	}

	if cn != "" && cookieVal == "" {
		errorsList = append(errorsList, "Cookie value is required when cookie name is set.")
	}
	if cn == "" && cookieVal != "" {
		errorsList = append(errorsList, "Cookie name is required when cookie value is set.")
	}

	if cn == "" || cookieVal == "" {
		return nil, errorsList
	}

	return &http.Cookie{
		Name:  cn.String(),
		Value: cookieVal,
	}, errorsList
}

func buildSitemapURL(pubURL *url.URL) string {
	normalized := *pubURL
	normalized.Path = ""
	normalized.RawQuery = ""
	normalized.Fragment = ""
	return normalized.ResolveReference(&url.URL{Path: "/sitemap.xml"}).String()
}

func newTestClient(proxyURL *url.URL) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}
}

func fetchStatus(client *http.Client, targetURL string, cookie *http.Cookie) (int, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, err
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func parsePositiveInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func summarizeEntryDates(entries []lib.SitemapEntry) (string, string) {
	var oldest time.Time
	var newest time.Time
	for _, entry := range entries {
		parsed, ok := parseDateInput(entry.LastMod)
		if !ok {
			continue
		}
		if oldest.IsZero() || parsed.Before(oldest) {
			oldest = parsed
		}
		if newest.IsZero() || parsed.After(newest) {
			newest = parsed
		}
	}
	if oldest.IsZero() && newest.IsZero() {
		return "", ""
	}
	oldestStr := oldest.Format("2006-01-02")
	newestStr := newest.Format("2006-01-02")
	return oldestStr, newestStr
}

func generateCSRFToken() string {
	seed := make([]byte, 24)
	if _, err := rand.Read(seed); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(seed)
}

func requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	if uiCSRFToken == "" {
		return true
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || token != uiCSRFToken {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	return true
}
