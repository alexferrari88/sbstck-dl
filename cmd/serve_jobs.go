package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
)

type jobStatus string

const (
	jobRunning  jobStatus = "running"
	jobComplete jobStatus = "completed"
	jobFailed   jobStatus = "failed"
	jobCanceled jobStatus = "canceled"
)

type jobLogEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
}

type jobPostEntry struct {
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type downloadJob struct {
	ID         string
	Status     jobStatus
	StartedAt  time.Time
	EndedAt    time.Time
	Total      int
	Downloaded int
	Skipped    int
	Failed     int
	Refreshed  int
	Retries    int
	Logs       []jobLogEntry
	Posts      []jobPostEntry
	postIndex  map[string]int
	cancel     context.CancelFunc
}

type jobManager struct {
	mu   sync.Mutex
	jobs map[string]*downloadJob
}

var uiJobs = newJobManager()

func newJobManager() *jobManager {
	return &jobManager{
		jobs: make(map[string]*downloadJob),
	}
}

type jobStartRequest struct {
	URL            string `json:"url"`
	Output         string `json:"output"`
	Format         string `json:"format"`
	AddSourceURL   bool   `json:"add_source_url"`
	CreateArchive  bool   `json:"create_archive"`
	DownloadImages bool   `json:"download_images"`
	ImageQuality   string `json:"image_quality"`
	ImagesDir      string `json:"images_dir"`
	DownloadFiles  bool   `json:"download_files"`
	FileExtensions string `json:"file_extensions"`
	FilesDir       string `json:"files_dir"`
	DryRun         bool   `json:"dry_run"`
	Force          bool   `json:"force"`
	SkipExisting   bool   `json:"skip_existing"`
	RefreshUpdated bool   `json:"refresh_updated"`
	Layout         string `json:"layout"`
	WriteMetadata  bool   `json:"write_metadata"`
	FailFast       bool   `json:"fail_fast"`
	ContinueOnErr  bool   `json:"continue_on_error"`
	After          string `json:"after"`
	Before         string `json:"before"`
	Rate           string `json:"rate"`
	MaxWorkers     string `json:"max_workers"`
	Proxy          string `json:"proxy"`
	CookieName     string `json:"cookie_name"`
	CookieVal      string `json:"cookie_val"`
	CookieValFile  string `json:"cookie_val_file"`
	CookieJar      string `json:"cookie_jar"`
	CookieKeychain string `json:"cookie_keychain"`
	NotionLabels   string `json:"notion_labels"`
	Verbose        bool   `json:"verbose"`
}

type jobStartResponse struct {
	ID string `json:"id"`
}

type jobStatusResponse struct {
	ID         string         `json:"id"`
	Status     jobStatus      `json:"status"`
	StartedAt  string         `json:"started_at,omitempty"`
	EndedAt    string         `json:"ended_at,omitempty"`
	Total      int            `json:"total"`
	Downloaded int            `json:"downloaded"`
	Skipped    int            `json:"skipped"`
	Failed     int            `json:"failed"`
	Refreshed  int            `json:"refreshed"`
	Retries    int            `json:"retries"`
	Logs       []jobLogEntry  `json:"logs"`
	Posts      []jobPostEntry `json:"posts"`
}

type jobListEntry struct {
	ID         string    `json:"id"`
	Status     jobStatus `json:"status"`
	StartedAt  string    `json:"started_at,omitempty"`
	EndedAt    string    `json:"ended_at,omitempty"`
	Total      int       `json:"total"`
	Downloaded int       `json:"downloaded"`
	Skipped    int       `json:"skipped"`
	Failed     int       `json:"failed"`
}

type jobListResponse struct {
	Jobs []jobListEntry `json:"jobs"`
}

func serveJobs(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/jobs") && r.Method == http.MethodPost {
		if !requireCSRF(w, r) {
			return
		}
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/jobs")
	path = strings.TrimPrefix(path, "/")

	if r.Method == http.MethodPost && path == "start" {
		serveStartJob(w, r)
		return
	}
	if r.Method == http.MethodGet && path == "" {
		serveJobList(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/cancel") {
		serveCancelJob(w, r, strings.TrimSuffix(path, "/cancel"))
		return
	}
	if r.Method == http.MethodGet && path != "" {
		serveJobStatus(w, r, path)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func serveStartJob(w http.ResponseWriter, r *http.Request) {
	var req jobStartRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}
	if _, err := parseURL(strings.TrimSpace(req.URL)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "URL must be a valid http(s) URL"})
		return
	}

	job, err := uiJobs.start(req)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, jobStartResponse{ID: job.ID})
}

func serveJobStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	resp, ok := uiJobs.snapshot(jobID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Job not found"})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func serveJobList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, jobListResponse{Jobs: uiJobs.list()})
}

func serveCancelJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if !uiJobs.cancel(jobID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Job not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}

func (m *jobManager) start(req jobStartRequest) (*downloadJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, job := range m.jobs {
		if job.Status == jobRunning {
			return nil, errors.New("a download job is already running")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &downloadJob{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Status:    jobRunning,
		StartedAt: time.Now(),
		postIndex: make(map[string]int),
		cancel:    cancel,
	}
	m.jobs[job.ID] = job

	go m.runJob(ctx, job, req)
	return job, nil
}

func (m *jobManager) snapshot(jobID string) (jobStatusResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[jobID]
	if job == nil {
		return jobStatusResponse{}, false
	}

	resp := jobStatusResponse{
		ID:         job.ID,
		Status:     job.Status,
		Total:      job.Total,
		Downloaded: job.Downloaded,
		Skipped:    job.Skipped,
		Failed:     job.Failed,
		Refreshed:  job.Refreshed,
		Retries:    job.Retries,
		Logs:       append([]jobLogEntry(nil), job.Logs...),
		Posts:      append([]jobPostEntry(nil), job.Posts...),
	}
	if !job.StartedAt.IsZero() {
		resp.StartedAt = job.StartedAt.Format(time.RFC3339)
	}
	if !job.EndedAt.IsZero() {
		resp.EndedAt = job.EndedAt.Format(time.RFC3339)
	}

	return resp, true
}

func (m *jobManager) list() []jobListEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	jobs := make([]jobListEntry, 0, len(m.jobs))
	for _, job := range m.jobs {
		entry := jobListEntry{
			ID:         job.ID,
			Status:     job.Status,
			Total:      job.Total,
			Downloaded: job.Downloaded,
			Skipped:    job.Skipped,
			Failed:     job.Failed,
		}
		if !job.StartedAt.IsZero() {
			entry.StartedAt = job.StartedAt.Format(time.RFC3339)
		}
		if !job.EndedAt.IsZero() {
			entry.EndedAt = job.EndedAt.Format(time.RFC3339)
		}
		jobs = append(jobs, entry)
	}

	return jobs
}

func (m *jobManager) cancel(jobID string) bool {
	m.mu.Lock()
	job := m.jobs[jobID]
	m.mu.Unlock()
	if job == nil {
		return false
	}
	if job.cancel != nil {
		job.cancel()
	}
	return true
}

func (m *jobManager) runJob(ctx context.Context, job *downloadJob, req jobStartRequest) {
	if err := applyJobConfig(req, job); err != nil {
		m.finishJob(job.ID, jobFailed, err)
		return
	}

	observer := func(ev DownloadEvent) {
		m.applyEvent(job.ID, ev)
	}

	_, err := runDownloadAllFormats(ctx, observer, false)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			m.finishJob(job.ID, jobCanceled, nil)
			return
		}
		m.finishJob(job.ID, jobFailed, err)
		return
	}
	m.finishJob(job.ID, jobComplete, nil)
}

func (m *jobManager) applyEvent(jobID string, ev DownloadEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := m.jobs[jobID]
	if job == nil {
		return
	}

	switch ev.Type {
	case DownloadEventPlan:
		job.Total = ev.Total
		job.Skipped = ev.Skipped
		job.Refreshed = ev.Refreshed
		if ev.Total > 0 {
			job.addLog(fmt.Sprintf("Planned %d downloads (skipped %d).", ev.Total, ev.Skipped))
		}
	case DownloadEventPostStart:
		job.upsertPost(ev.URL, ev.Title, ev.Path, "running", "")
		job.addLog(fmt.Sprintf("Downloading %s", ev.URL))
	case DownloadEventPostDone:
		job.Downloaded++
		job.upsertPost(ev.URL, ev.Title, ev.Path, "completed", "")
		job.addLog(fmt.Sprintf("Downloaded %s", ev.URL))
	case DownloadEventPostFailed:
		job.Failed++
		job.upsertPost(ev.URL, ev.Title, ev.Path, "failed", ev.Error)
		job.addLog(fmt.Sprintf("Failed %s: %s", ev.URL, ev.Error))
	case DownloadEventPostSkipped:
		job.Skipped++
		job.upsertPost(ev.URL, ev.Title, ev.Path, "skipped", ev.Reason)
		job.addLog(fmt.Sprintf("Skipped %s (%s)", ev.URL, ev.Reason))
	case DownloadEventRetry:
		job.Retries++
		if ev.URL != "" {
			job.addLog(fmt.Sprintf("Retry %d for %s (wait %s)", ev.RetryCount, ev.URL, ev.RetryWait))
		}
	case DownloadEventSummary:
		job.Downloaded = ev.Downloaded
		job.Skipped = ev.Skipped
		job.Failed = ev.Failed
	}
}

func (m *jobManager) finishJob(jobID string, status jobStatus, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job := m.jobs[jobID]
	if job == nil {
		return
	}
	job.Status = status
	job.EndedAt = time.Now()
	if err != nil {
		job.addLog(fmt.Sprintf("Job finished with error: %v", err))
	} else {
		job.addLog(fmt.Sprintf("Job finished with status: %s", status))
	}
}

func (job *downloadJob) addLog(message string) {
	entry := jobLogEntry{
		Time:    time.Now().Format(time.RFC3339),
		Message: message,
	}
	job.Logs = append(job.Logs, entry)
	if len(job.Logs) > 200 {
		job.Logs = job.Logs[len(job.Logs)-200:]
	}
}

func (job *downloadJob) upsertPost(url string, title string, path string, status string, err string) {
	if url == "" {
		return
	}
	idx, ok := job.postIndex[url]
	if !ok {
		job.Posts = append(job.Posts, jobPostEntry{
			URL:    url,
			Title:  title,
			Path:   path,
			Status: status,
			Error:  err,
		})
		job.postIndex[url] = len(job.Posts) - 1
		if len(job.Posts) > 200 {
			job.Posts = job.Posts[len(job.Posts)-200:]
			job.rebuildIndex()
		}
		return
	}

	job.Posts[idx].Title = title
	job.Posts[idx].Path = path
	job.Posts[idx].Status = status
	job.Posts[idx].Error = err
}

func (job *downloadJob) rebuildIndex() {
	job.postIndex = make(map[string]int, len(job.Posts))
	for idx, post := range job.Posts {
		job.postIndex[post.URL] = idx
	}
}

func applyJobConfig(req jobStartRequest, job *downloadJob) error {
	format = strings.TrimSpace(req.Format)
	if format == "" {
		format = "html"
	}
	outputFolder = strings.TrimSpace(req.Output)
	if outputFolder == "" {
		outputFolder = "."
	}
	downloadUrl = strings.TrimSpace(req.URL)
	outputFolder = resolveOutputFolder(outputFolder, downloadUrl)
	addSourceURL = req.AddSourceURL
	createArchive = req.CreateArchive
	downloadImages = req.DownloadImages
	imageQuality = strings.TrimSpace(req.ImageQuality)
	if imageQuality == "" {
		imageQuality = "high"
	}
	imagesDir = strings.TrimSpace(req.ImagesDir)
	if imagesDir == "" {
		imagesDir = "images"
	}
	downloadFiles = req.DownloadFiles
	fileExtensions = strings.TrimSpace(req.FileExtensions)
	filesDir = strings.TrimSpace(req.FilesDir)
	if filesDir == "" {
		filesDir = "files"
	}
	dryRun = req.DryRun
	forceDownload = req.Force
	skipExisting = req.SkipExisting
	refreshUpdated = req.RefreshUpdated
	layout = strings.TrimSpace(req.Layout)
	if layout == "" {
		layout = "flat"
	}
	writeMetadata = req.WriteMetadata
	failFast = req.FailFast
	continueOnErr = req.ContinueOnErr
	afterDate = strings.TrimSpace(req.After)
	beforeDate = strings.TrimSpace(req.Before)
	verbose = req.Verbose
	logFormat = logFormatText
	notionLabelsPath = strings.TrimSpace(req.NotionLabels)

	proxyValue := strings.TrimSpace(req.Proxy)
	if proxyValue != "" {
		parsed, err := parseURL(proxyValue)
		if err != nil {
			return err
		}
		parsedProxyURL = parsed
	} else {
		parsedProxyURL = nil
	}

	domainHint := ""
	if parsedURL, err := parseURL(downloadUrl); err == nil && parsedURL != nil {
		domainHint = parsedURL.Hostname()
	}
	cookie, cookieErrors := resolveTestCookie(testConnectionRequest{
		CookieName:     req.CookieName,
		CookieVal:      req.CookieVal,
		CookieValFile:  req.CookieValFile,
		CookieJar:      req.CookieJar,
		CookieKeychain: req.CookieKeychain,
	}, domainHint)
	if len(cookieErrors) > 0 {
		return errors.New(strings.Join(cookieErrors, " | "))
	}

	ratePerSecond = parsePositiveInt(req.Rate, lib.DefaultRatePerSecond)
	maxWorkers = parsePositiveInt(req.MaxWorkers, lib.DefaultMaxWorkers)

	fetcher = lib.NewFetcher(
		lib.WithRatePerSecond(ratePerSecond),
		lib.WithMaxWorkers(maxWorkers),
		lib.WithProxyURL(parsedProxyURL),
		lib.WithCookie(cookie),
	)
	extractor = lib.NewExtractor(fetcher)

	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
