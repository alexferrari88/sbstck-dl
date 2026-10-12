package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// previewDurationRatio is the fraction of the full duration below which a
// downloaded file is considered a truncated preview.
const previewDurationRatio = 0.9

// maxMediaRetries is how many consecutive failed attempts without progress a
// media download tolerates before giving up.
const maxMediaRetries = 5

var (
	// mediaIdleTimeout aborts and retries a download that receives no data for this long.
	mediaIdleTimeout = 60 * time.Second

	// newMediaBackoff creates the wait schedule between media download retries.
	newMediaBackoff = func() backoff.BackOff {
		b := backoff.NewExponentialBackOff()
		b.InitialInterval = 2 * time.Second
		b.MaxInterval = time.Minute
		b.MaxElapsedTime = 0 // limited by maxMediaRetries instead
		return b
	}
)

// mediaDownloader downloads large media files (podcast audio, videos). Substack
// media URLs redirect to signed CDN URLs. The redirect is resolved with the
// subscriber cookie, which decides whether Substack serves the full file or a
// free preview, and the file itself is downloaded from the CDN without it.
type mediaDownloader struct {
	fetcher *Fetcher     // talks to Substack, with cookie
	cdn     *http.Client // downloads from the CDN, without cookie
}

func newMediaDownloader(fetcher *Fetcher) *mediaDownloader {
	if fetcher == nil {
		fetcher = NewFetcher()
	}
	// The client timeout covers reading the whole body, which for a long
	// episode can take far longer. Stalled downloads are caught by
	// mediaIdleTimeout instead.
	cdn := *fetcher.Client
	cdn.Timeout = 0
	return &mediaDownloader{fetcher: fetcher, cdn: &cdn}
}

// mediaJob describes a media file to download.
type mediaJob struct {
	requestURL       string                   // URL to request the file from
	resolve          bool                     // requestURL is a Substack URL to resolve with the cookie
	basePath         string                   // local path without extension
	exts             []string                 // allowed file extensions, the first being the default
	expectedDuration float64                  // full duration in seconds, 0 if unknown
	previewHint      func(cdnURL string) bool // whether Substack's metadata says the URL serves a preview
}

// mediaResult describes a downloaded media file.
type mediaResult struct {
	LocalPath string
	Size      int64
	Preview   bool // true if only a truncated preview was available
	Existed   bool // true if the file was already present and not re-downloaded
}

// download downloads the job's file to <basePath><ext>, or to
// <basePath>_preview<ext> if it turns out to be a truncated preview, so that a
// later run with a subscriber cookie still fetches the full file. Whether it
// is a preview is decided by comparing the downloaded file's own duration with
// the expected full duration, falling back to Substack's metadata when the
// file's duration can't be read. The extension is picked from the final
// download URL or the response's Content-Type, among the job's allowed ones.
func (m *mediaDownloader) download(ctx context.Context, job mediaJob) (*mediaResult, error) {
	if path, size, ok := existingMediaFile(job.basePath, job.exts); ok {
		return &mediaResult{LocalPath: path, Size: size, Existed: true}, nil
	}

	cdnURL := job.requestURL
	if job.resolve {
		var err error
		cdnURL, err = m.fetcher.ResolveRedirect(ctx, job.requestURL)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve media URL: %w", err)
		}
	}

	hintPreview := job.previewHint != nil && job.previewHint(cdnURL)
	if hintPreview {
		if path, size, ok := existingMediaFile(job.basePath+"_preview", job.exts); ok {
			return &mediaResult{LocalPath: path, Size: size, Preview: true, Existed: true}, nil
		}
	}

	partPath := job.basePath + ".part"
	fetched, err := m.fetchToFile(ctx, cdnURL, partPath)
	if err != nil {
		return nil, err
	}

	ext := mediaExtension(job.exts, fetched.finalURL, fetched.contentType)
	preview := hintPreview
	if job.expectedDuration > 0 {
		if actual, ok := mediaDuration(partPath, ext); ok {
			preview = actual < job.expectedDuration*previewDurationRatio
		}
	}

	finalPath := job.basePath + ext
	if preview {
		finalPath = job.basePath + "_preview" + ext
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return nil, fmt.Errorf("failed to finalize file: %w", err)
	}
	os.Remove(partPath + ".src")

	return &mediaResult{LocalPath: finalPath, Size: fetched.size, Preview: preview}, nil
}

// existingMediaFile looks for basePath with any of the given extensions.
func existingMediaFile(basePath string, exts []string) (path string, size int64, ok bool) {
	for _, ext := range exts {
		if info, err := os.Stat(basePath + ext); err == nil {
			return basePath + ext, info.Size(), true
		}
	}
	return "", 0, false
}

// mediaContentTypes maps media types to file extensions.
var mediaContentTypes = map[string]string{
	"audio/mpeg":      ".mp3",
	"audio/mp3":       ".mp3",
	"audio/mp4":       ".m4a",
	"audio/x-m4a":     ".m4a",
	"audio/m4a":       ".m4a",
	"audio/aac":       ".aac",
	"audio/aacp":      ".aac",
	"audio/ogg":       ".ogg",
	"application/ogg": ".ogg",
	"audio/opus":      ".opus",
	"audio/wav":       ".wav",
	"audio/x-wav":     ".wav",
	"audio/wave":      ".wav",
	"audio/flac":      ".flac",
	"audio/x-flac":    ".flac",
	"video/mp4":       ".mp4",
}

// mediaExtension picks the file extension for a download: the extension of
// the final URL's path if allowed, else the one matching the Content-Type if
// allowed, else the default (the first allowed one).
func mediaExtension(allowed []string, finalURL, contentType string) string {
	isAllowed := func(ext string) bool {
		for _, a := range allowed {
			if a == ext {
				return true
			}
		}
		return false
	}

	if u, err := url.Parse(finalURL); err == nil {
		if ext := strings.ToLower(path.Ext(u.Path)); ext != "" && isAllowed(ext) {
			return ext
		}
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		if ext, ok := mediaContentTypes[mediaType]; ok && isAllowed(ext) {
			return ext
		}
	}
	return allowed[0]
}

// permanentError marks a download error that retrying won't fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// fetchToFile downloads rawURL into partPath, retrying failed attempts and
// resuming where they stopped. A partial file left by an earlier run is
// resumed too, if it is of the same source file; the source is recorded next
// to it in partPath + ".src". On failure the partial file is kept so the next
// run can resume it.
func (m *mediaDownloader) fetchToFile(ctx context.Context, rawURL, partPath string) (*fetchedFile, error) {
	source := mediaSourceKey(rawURL)
	if prev, err := os.ReadFile(partPath + ".src"); err != nil || string(prev) != source {
		// A partial download of a different file can't be resumed
		if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		os.Remove(partPath + ".src")
	}

	b := newMediaBackoff()
	failures := 0
	for {
		fetched, progressed, err := m.fetchAttempt(ctx, rawURL, partPath, source)
		if err == nil {
			return fetched, nil
		}
		var permanent *permanentError
		if errors.As(err, &permanent) {
			return nil, permanent.err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if progressed {
			b.Reset()
			failures = 0
		}
		failures++
		if failures > maxMediaRetries {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(b.NextBackOff()):
		}
	}
}

// fetchedFile describes a completed download.
type fetchedFile struct {
	size        int64
	finalURL    string // URL the file was served from, after redirects
	contentType string
}

// fetchAttempt makes one attempt at downloading the rest of rawURL into
// partPath. It returns the completed file, or whether any data was written
// before failing. Errors not wrapped in permanentError are worth retrying.
func (m *mediaDownloader) fetchAttempt(ctx context.Context, rawURL, partPath, source string) (fetched *fetchedFile, progressed bool, err error) {
	var offset int64
	if info, err := os.Stat(partPath); err == nil {
		offset = info.Size()
	}

	if err := m.fetcher.RateLimiter.Wait(ctx); err != nil {
		return nil, false, err
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, &permanentError{err}
	}
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	res, err := m.cdn.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	total := int64(-1)
	switch {
	case res.StatusCode == http.StatusPartialContent:
		start, t, ok := parseContentRange(res.Header.Get("Content-Range"))
		if !ok || start != offset {
			os.Remove(partPath)
			return nil, false, errors.New("server returned an unexpected range, restarting download")
		}
		total = t
	case res.StatusCode == http.StatusOK:
		// No range requested, or the server ignored it: start over
		offset = 0
		total = res.ContentLength
	case res.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// Most likely the partial file is already complete
		if _, t, ok := parseContentRange(res.Header.Get("Content-Range")); ok && t == offset {
			return &fetchedFile{size: offset, finalURL: res.Request.URL.String()}, false, nil
		}
		os.Remove(partPath)
		return nil, false, errors.New("cannot resume partial download, restarting")
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return nil, false, &FetchError{StatusCode: res.StatusCode}
	default:
		return nil, false, &permanentError{&FetchError{StatusCode: res.StatusCode}}
	}

	if err := os.WriteFile(partPath+".src", []byte(source), 0644); err != nil {
		return nil, false, &permanentError{err}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partPath, flags, 0644)
	if err != nil {
		return nil, false, &permanentError{err}
	}

	idle := time.AfterFunc(mediaIdleTimeout, cancel)
	n, copyErr := copyWithProgress(file, res.Body, func() { idle.Reset(mediaIdleTimeout) })
	idle.Stop()
	closeErr := file.Close()

	offset += n
	progressed = n > 0
	if copyErr != nil {
		if attemptCtx.Err() != nil && ctx.Err() == nil {
			copyErr = fmt.Errorf("download stalled for %s", mediaIdleTimeout)
		}
		return nil, progressed, copyErr
	}
	if closeErr != nil {
		return nil, progressed, &permanentError{closeErr}
	}
	if total >= 0 && offset != total {
		return nil, progressed, fmt.Errorf("download ended early: got %d of %d bytes", offset, total)
	}
	return &fetchedFile{
		size:        offset,
		finalURL:    res.Request.URL.String(),
		contentType: res.Header.Get("Content-Type"),
	}, progressed, nil
}

// copyWithProgress copies src to dst, calling onProgress after each chunk.
// Write errors are returned as permanentError, read errors as is.
func copyWithProgress(dst io.Writer, src io.Reader, onProgress func()) (int64, error) {
	buf := make([]byte, 64<<10)
	var written int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			w, err := dst.Write(buf[:n])
			written += int64(w)
			if err != nil {
				return written, &permanentError{err}
			}
			onProgress()
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

// parseContentRange parses a "bytes start-end/total" or "bytes */total"
// Content-Range header. total is -1 if unknown ("*").
func parseContentRange(header string) (start, total int64, ok bool) {
	spec, found := strings.CutPrefix(header, "bytes ")
	if !found {
		return 0, 0, false
	}
	rangePart, totalPart, found := strings.Cut(spec, "/")
	if !found {
		return 0, 0, false
	}

	total = -1
	if totalPart != "*" {
		t, err := strconv.ParseInt(totalPart, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total = t
	}

	if rangePart == "*" {
		return -1, total, true
	}
	startPart, _, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, false
	}
	s, err := strconv.ParseInt(startPart, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return s, total, true
}

// mediaSourceKey identifies the file behind a signed CDN URL: the URL without
// its query string, which holds the signature and changes on every request.
func mediaSourceKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// publicationOrigin returns the scheme and host of the post's publication.
// Substack serves its /api/v1 endpoints on every publication's own domain, and
// requests must go there for custom-domain publications: their connect.sid
// cookie is only valid on that domain, and without it Substack serves free
// previews of paid media instead of the full files.
func publicationOrigin(post Post) (*url.URL, bool) {
	u, err := url.Parse(post.CanonicalUrl)
	if err != nil || u.Host == "" {
		return nil, false
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host}, true
}
