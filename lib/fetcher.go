package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
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
		RateLimiter: rate.NewLimiter(rate.Limit(ratePerSecond), 1),
	}
}

func (f *Fetcher) FetchURLs(ctx context.Context, urls []string) <-chan FetchResult {
	results := make(chan FetchResult, len(urls))

	go func() {
		var wg sync.WaitGroup
		wg.Add(len(urls))

		for _, u := range urls {
			go func(url string) {
				defer wg.Done()
				body, err := f.FetchURL(ctx, url)
				results <- FetchResult{Url: url, Body: body, Error: err}
			}(u)
		}

		wg.Wait()
		close(results)
	}()

	return results
}

func (f *Fetcher) FetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	backOffCfg := backoff.NewExponentialBackOff()
	backOffCfg.MaxElapsedTime = 1 * time.Minute

	var body io.ReadCloser

	err := backoff.Retry(func() error {
		err := f.RateLimiter.Wait(ctx) // Use rate limiter
		if err != nil {
			return err // Could be a context cancellation or error in limiter
		}
		body, err = f.fetch(ctx, url)
		if err != nil {
			if respErr, ok := err.(*FetchError); ok && respErr.TooManyRequests {
				retryAfter := respErr.RetryAfter
				if retryAfter > 0 {
					time.Sleep(time.Duration(retryAfter) * time.Second)
				}
			}
		}
		return err
	}, backOffCfg)

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
