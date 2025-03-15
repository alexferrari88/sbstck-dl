package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

// DefaultRatePerSecond defines the default request rate per second when creating a new Fetcher.
const DefaultRatePerSecond = 2

// DefaultBurst defines the default burst size for the rate limiter.
const DefaultBurst = 5

// defaultRetryAfter specifies the default value for Retry-After header in case of too many requests.
const defaultRetryAfter = 60

// defaultMaxRetryCount defines the default maximum number of retries for a failed URL fetch.
const defaultMaxRetryCount = 10

// defaultMaxElapsedTime specifies the default maximum elapsed time for the exponential backoff.
const defaultMaxElapsedTime = 10 * time.Minute

// defaultMaxInterval defines the default maximum interval for the exponential backoff.
const defaultMaxInterval = 2 * time.Minute

// defaultClientTimeout defines the default timeout for HTTP requests.
const defaultClientTimeout = 30 * time.Second

// userAgent specifies the User-Agent header value used in HTTP requests.
const userAgent = "sbstck-dl/0.1"

// Fetcher represents a URL fetcher with rate limiting and retry mechanisms.
type Fetcher struct {
	Client      *http.Client
	RateLimiter *rate.Limiter
	BackoffCfg  backoff.BackOff
	Cookie      *http.Cookie
	MaxWorkers  int
}

// FetcherOptions holds configurable options for Fetcher.
type FetcherOptions struct {
	RatePerSecond int
	Burst         int
	ProxyURL      *url.URL
	BackOffConfig backoff.BackOff
	Cookie        *http.Cookie
	Timeout       time.Duration
	MaxWorkers    int
}

// FetcherOption defines a function that applies a specific option to FetcherOptions.
type FetcherOption func(*FetcherOptions)

// WithRatePerSecond sets the rate per second for the Fetcher.
func WithRatePerSecond(rate int) FetcherOption {
	return func(o *FetcherOptions) {
		o.RatePerSecond = rate
	}
}

// WithBurst sets the burst size for the rate limiter.
func WithBurst(burst int) FetcherOption {
	return func(o *FetcherOptions) {
		o.Burst = burst
	}
}

// WithProxyURL sets the proxy URL for the Fetcher.
func WithProxyURL(proxyURL *url.URL) FetcherOption {
	return func(o *FetcherOptions) {
		o.ProxyURL = proxyURL
	}
}

// WithBackOffConfig sets the backoff configuration for the Fetcher.
func WithBackOffConfig(b backoff.BackOff) FetcherOption {
	return func(o *FetcherOptions) {
		o.BackOffConfig = b
	}
}

// WithCookie sets the cookie for the Fetcher.
func WithCookie(cookie *http.Cookie) FetcherOption {
	return func(o *FetcherOptions) {
		if cookie != nil {
			o.Cookie = cookie
		}
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) FetcherOption {
	return func(o *FetcherOptions) {
		o.Timeout = timeout
	}
}

// WithMaxWorkers sets the maximum number of concurrent workers.
func WithMaxWorkers(workers int) FetcherOption {
	return func(o *FetcherOptions) {
		o.MaxWorkers = workers
	}
}

// FetchResult represents the result of a URL fetch operation.
type FetchResult struct {
	Url   string
	Body  io.ReadCloser
	Error error
}

// FetchError represents an error returned when encountering too many requests with a Retry-After value.
type FetchError struct {
	TooManyRequests bool
	RetryAfter      int
	StatusCode      int
}

// Error returns the error message for the FetchError.
func (e *FetchError) Error() string {
	if e.TooManyRequests {
		return fmt.Sprintf("too many requests, retry after %d seconds", e.RetryAfter)
	}
	return fmt.Sprintf("HTTP error: status code %d", e.StatusCode)
}

// NewFetcher creates a new Fetcher with the provided options.
func NewFetcher(opts ...FetcherOption) *Fetcher {
	options := FetcherOptions{
		RatePerSecond: DefaultRatePerSecond,
		Burst:         DefaultBurst,
		BackOffConfig: makeDefaultBackoff(),
		Timeout:       defaultClientTimeout,
		MaxWorkers:    10, // Default to 10 workers
	}

	for _, opt := range opts {
		opt(&options)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if options.ProxyURL != nil {
		transport.Proxy = http.ProxyURL(options.ProxyURL)
	}

	// Set sensible defaults for transport
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = options.MaxWorkers
	transport.MaxConnsPerHost = options.MaxWorkers
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second

	client := &http.Client{
		Transport: transport,
		Timeout:   options.Timeout,
	}

	return &Fetcher{
		Client:      client,
		RateLimiter: rate.NewLimiter(rate.Limit(options.RatePerSecond), options.Burst),
		BackoffCfg:  options.BackOffConfig,
		Cookie:      options.Cookie,
		MaxWorkers:  options.MaxWorkers,
	}
}

// FetchURLs concurrently fetches the specified URLs and returns a channel to receive the FetchResults.
func (f *Fetcher) FetchURLs(ctx context.Context, urls []string) <-chan FetchResult {
	// Use a smaller buffer to reduce memory footprint
	results := make(chan FetchResult, min(len(urls), f.MaxWorkers*2))

	g, ctx := errgroup.WithContext(ctx)

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, f.MaxWorkers)

	for _, u := range urls {
		u := u // Capture the variable
		g.Go(func() error {
			select {
			case sem <- struct{}{}: // Acquire semaphore
				defer func() { <-sem }() // Release semaphore
			case <-ctx.Done():
				return ctx.Err()
			}

			body, err := f.FetchURL(ctx, u)

			select {
			case results <- FetchResult{Url: u, Body: body, Error: err}:
				return nil
			case <-ctx.Done():
				// Close body if context was canceled to prevent leaks
				if body != nil {
					body.Close()
				}
				return ctx.Err()
			}
		})
	}

	// Close the results channel when all goroutines complete
	go func() {
		g.Wait()
		close(results)
	}()

	return results
}

// FetchURL fetches the specified URL with retries and rate limiting.
func (f *Fetcher) FetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	var body io.ReadCloser
	var err error
	var retryCounter int

	operation := func() error {
		if retryCounter >= defaultMaxRetryCount {
			return backoff.Permanent(fmt.Errorf("max retry count reached for URL: %s", url))
		}

		err = f.RateLimiter.Wait(ctx) // Use rate limiter
		if err != nil {
			return backoff.Permanent(err) // Context cancellation or rate limiter error
		}

		body, err = f.fetch(ctx, url)
		if err != nil {
			// If it's a fetch error that should be retried
			if fetchErr, ok := err.(*FetchError); ok && fetchErr.TooManyRequests {
				retryCounter++
				return err
			}
			// For other errors, don't retry
			return backoff.Permanent(err)
		}
		return nil
	}

	// Use backoff with notification for logging
	err = backoff.RetryNotify(
		operation,
		f.BackoffCfg,
		func(err error, d time.Duration) {
			// This could be connected to a logger
			_ = err // Avoid unused variable error
		},
	)

	return body, err
}

// fetch performs the actual HTTP GET request.
func (f *Fetcher) fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)

	// Add cookie if available
	if f.Cookie != nil {
		req.AddCookie(f.Cookie)
	}

	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}

	// Handle non-success status codes
	if res.StatusCode != http.StatusOK {
		// Always close the body for non-200 responses
		defer res.Body.Close()

		if res.StatusCode == http.StatusTooManyRequests {
			retryAfter := defaultRetryAfter
			if retryAfterStr := res.Header.Get("Retry-After"); retryAfterStr != "" {
				if seconds, err := strconv.Atoi(retryAfterStr); err == nil {
					retryAfter = seconds
				}
			}
			return nil, &FetchError{
				TooManyRequests: true,
				RetryAfter:      retryAfter,
				StatusCode:      res.StatusCode,
			}
		}

		return nil, &FetchError{
			StatusCode: res.StatusCode,
		}
	}

	return res.Body, nil
}

// makeDefaultBackoff creates the default exponential backoff configuration.
func makeDefaultBackoff() backoff.BackOff {
	backOffCfg := backoff.NewExponentialBackOff()
	backOffCfg.MaxElapsedTime = defaultMaxElapsedTime
	backOffCfg.MaxInterval = defaultMaxInterval
	backOffCfg.Multiplier = 1.5 // Reduced from 2.0 for more gradual backoff

	return backOffCfg
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
