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

type Fetcher struct {
	Client      *http.Client
	RateLimiter *rate.Limiter
}

type FetchResult struct {
	Url   string
	Body  io.ReadCloser
	Error error
}

type FetchError struct {
	TooManyRequests bool
	RetryAfter      int
}

func (e *FetchError) Error() string {
	return fmt.Sprintf("too many requests, retry after %d seconds", e.RetryAfter)
}

func NewFetcher(ratePerSecond int, proxyURL *url.URL) *Fetcher {
	if ratePerSecond == 0 {
		ratePerSecond = 10
	}
	trasport := http.DefaultTransport
	if proxyURL != nil {
		trasport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}
	client := &http.Client{Transport: trasport}

	return &Fetcher{
		Client:      client,
		RateLimiter: rate.NewLimiter(rate.Limit(ratePerSecond), 1), // 1 burst means that we can send 1 request at a time (limited to ratePerSecond)
	}
}

func (f *Fetcher) FetchURLs(ctx context.Context, urls []string) <-chan FetchResult {
	results := make(chan FetchResult, len(urls))
	ctx, cancel := context.WithCancel(ctx)
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
				if err != nil {
					cancel()
				}
				return nil
			}
		})
	}

	go func() {
		eg.Wait()
		cancel()
		close(results)
	}()

	return results
}

func (f *Fetcher) FetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	backOffCfg := backoff.NewExponentialBackOff()
	backOffCfg.MaxElapsedTime = 1 * time.Minute

	var body io.ReadCloser
	var err error

	backoff.RetryNotify(func() error {
		err = f.RateLimiter.Wait(ctx) // Use rate limiter
		if err != nil {
			return err // Could be a context cancellation or error in limiter
		}
		body, err = f.fetch(ctx, url)
		return err
	}, backOffCfg, func(err error, d time.Duration) {
		if respErr, ok := err.(*FetchError); ok && respErr.TooManyRequests {
			backOffCfg.MaxInterval = time.Duration(respErr.RetryAfter) * time.Second
		}
	})

	return body, err
}

func (f *Fetcher) fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusTooManyRequests {
		retryAfter := 0
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
