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

// defaultRetryAfter specifies the default value for Retry-After header in case of too many requests.
const defaultRetryAfter = 60

// defaultMaxRetryCount defines the default maximum number of retries for a failed URL fetch.
const defaultMaxRetryCount = 100

// defaultMaxElapsedTime specifies the default maximum elapsed time for the exponential backoff.
const defaultMaxElapsedTime = 10 * time.Minute

// defaultMaxInterval defines the default maximum interval for the exponential backoff.
const defaultMaxInterval = 2 * time.Minute

// userAgent specifies the User-Agent header value used in HTTP requests.
const userAgent = "sbstck-dl/0.1"

// Fetcher represents a URL fetcher with rate limiting and retry mechanisms.
type Fetcher struct {
	Client      *http.Client
	RateLimiter *rate.Limiter
	BackoffCfg  backoff.BackOff
	Cookie      *http.Cookie
}

// FetcherOptions holds configurable options for Fetcher.
type FetcherOptions struct {
	RatePerSecond int
	ProxyURL      *url.URL
	BackOffConfig backoff.BackOff
	Cookie        *http.Cookie
}

// FetcherOption defines a function that applies a specific option to FetcherOptions.
type FetcherOption func(*FetcherOptions)

// WithRatePerSecond sets the rate per second for the Fetcher.
func WithRatePerSecond(rate int) FetcherOption {
	return func(o *FetcherOptions) {
		o.RatePerSecond = rate
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
}

// Error returns the error message for the FetchError, indicating the retry wait time.
func (e *FetchError) Error() string {
	return fmt.Sprintf("too many requests, retry after %d seconds", e.RetryAfter)
}

// NewFetcher creates a new Fetcher with the provided options.
// If ratePerSecond is 0, the default rate (DefaultRatePerSecond) is used.
// If b is nil, the default backoff configuration is used.
func NewFetcher(opts ...FetcherOption) *Fetcher {
	options := FetcherOptions{
		RatePerSecond: DefaultRatePerSecond,
		BackOffConfig: makeDefaultBackoff(),
	}

	for _, opt := range opts {
		opt(&options)
	}

	transport := http.DefaultTransport
	if options.ProxyURL != nil {
		transport = &http.Transport{Proxy: http.ProxyURL(options.ProxyURL)}
	}

	client := &http.Client{Transport: transport}

	return &Fetcher{
		Client:      client,
		RateLimiter: rate.NewLimiter(rate.Limit(options.RatePerSecond), 1),
		BackoffCfg:  options.BackOffConfig,
		Cookie:      options.Cookie,
	}
}

// FetchURLs concurrently fetches the specified URLs and returns a channel to receive the FetchResults.
// The returned channel will be closed once all fetch operations are completed.
func (f *Fetcher) FetchURLs(ctx context.Context, urls []string) <-chan FetchResult {
	results := make(chan FetchResult, len(urls))
	ctx, ctxCancelFn := context.WithCancel(ctx)
	defer ctxCancelFn()
	var eg errgroup.Group

	sem := make(chan struct{}, f.RateLimiter.Burst()) // worker pool

	for _, u := range urls {
		u := u // https://golang.org/doc/faq#closures_and_goroutines
		eg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := f.FetchURL(ctx, u)
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				results <- FetchResult{Url: u, Body: body, Error: err}
				return nil
			}
		})
	}

	go func() {
		eg.Wait()
		close(results)
	}()

	return results
}

// FetchURL fetches the specified URL and returns the response body as io.ReadCloser and any encountered error.
// It uses rate limiting and retry mechanisms to handle rate limits and transient failures.
func (f *Fetcher) FetchURL(ctx context.Context, url string) (io.ReadCloser, error) {

	var body io.ReadCloser
	var err error
	var retryCounter int
	var nextRetryWait time.Duration

	operation := func() error {
		if retryCounter >= defaultMaxRetryCount {
			err = fmt.Errorf("max retry count reached for URL: %s", url)
			return nil
		}
		if nextRetryWait > 0 {
			time.Sleep(nextRetryWait)
		}
		err = f.RateLimiter.Wait(ctx) // Use rate limiter
		if err != nil {
			return err // Could be a context cancellation or error in limiter
		}
		body, err = f.fetch(ctx, url)
		if err != nil {
			retryCounter++
		}
		return err
	}

	notify := func(err error, d time.Duration) {
		if respErr, ok := err.(*FetchError); ok && respErr.TooManyRequests {
			nextRetryWait = time.Duration(respErr.RetryAfter) * time.Second
			if retryCounter > 0 {
				nextRetryWait *= time.Duration(retryCounter)
			}
		}
	}

	backoff.RetryNotify(operation, f.BackoffCfg, notify)

	return body, err
}

// fetch performs the actual HTTP GET request to the specified URL and returns the response body and any encountered error.
// It checks for too many requests (status code 429) and handles it by returning a FetchError.
func (f *Fetcher) fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	// Add cookie to the request if it's not nil
	if f.Cookie != nil {
		req.AddCookie(f.Cookie)
	}

	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusTooManyRequests {
		retryAfter := defaultRetryAfter
		if retryAfterStr := res.Header.Get("Retry-After"); retryAfterStr != "" {
			retryAfter, err = strconv.Atoi(retryAfterStr)
			if err != nil {
				return nil, fmt.Errorf("invalid Retry-After header: %v", err)
			}
		}
		return nil, &FetchError{TooManyRequests: true, RetryAfter: retryAfter}
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	return res.Body, nil
}

// makeDefaultBackoff creates and returns the default exponential backoff configuration.
func makeDefaultBackoff() backoff.BackOff {
	backOffCfg := backoff.NewExponentialBackOff()
	backOffCfg.MaxElapsedTime = defaultMaxElapsedTime
	backOffCfg.MaxInterval = defaultMaxInterval
	backOffCfg.Multiplier = 2.0

	return backOffCfg
}
