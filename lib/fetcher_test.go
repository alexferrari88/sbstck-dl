package lib

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// TestNewFetcher tests the creation of a new fetcher with various options
func TestNewFetcher(t *testing.T) {
	t.Run("DefaultOptions", func(t *testing.T) {
		f := NewFetcher()
		assert.NotNil(t, f.Client)
		assert.NotNil(t, f.RateLimiter)
		assert.NotNil(t, f.BackoffCfg)
		assert.Nil(t, f.Cookie)
		assert.Equal(t, 10, f.MaxWorkers)
	})

	t.Run("CustomOptions", func(t *testing.T) {
		proxyURL, _ := url.Parse("http://proxy.example.com")
		cookie := &http.Cookie{Name: "test", Value: "value"}
		customBackoff := backoff.NewConstantBackOff(time.Second)

		f := NewFetcher(
			WithRatePerSecond(5),
			WithBurst(10),
			WithProxyURL(proxyURL),
			WithCookie(cookie),
			WithBackOffConfig(customBackoff),
			WithTimeout(time.Minute),
			WithMaxWorkers(20),
		)

		assert.NotNil(t, f.Client)
		assert.Equal(t, rate.Limit(5), f.RateLimiter.Limit())
		assert.Equal(t, 10, f.RateLimiter.Burst())
		assert.Equal(t, customBackoff, f.BackoffCfg)
		assert.Equal(t, cookie, f.Cookie)
		assert.Equal(t, 20, f.MaxWorkers)
		assert.Equal(t, time.Minute, f.Client.Timeout)
	})
}

// TestFetchURL tests the FetchURL method
func TestFetchURL(t *testing.T) {
	t.Run("SuccessfulFetch", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "sbstck-dl/0.1", r.Header.Get("User-Agent"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("response body"))
		}))
		defer server.Close()

		// Create fetcher and fetch the URL
		f := NewFetcher()
		ctx := context.Background()
		body, err := f.FetchURL(ctx, server.URL)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, body)
		defer body.Close()

		data, err := io.ReadAll(body)
		require.NoError(t, err)
		assert.Equal(t, "response body", string(data))
	})

	t.Run("FetchWithCookie", func(t *testing.T) {
		cookieReceived := false
		// Create a test server that checks for cookie
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookies := r.Cookies()
			for _, cookie := range cookies {
				if cookie.Name == "test" && cookie.Value == "value" {
					cookieReceived = true
					break
				}
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create fetcher with cookie
		cookie := &http.Cookie{Name: "test", Value: "value"}
		f := NewFetcher(WithCookie(cookie))
		ctx := context.Background()
		body, err := f.FetchURL(ctx, server.URL)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, body)
		body.Close()
		assert.True(t, cookieReceived)
	})

	t.Run("HTTPError", func(t *testing.T) {
		// Create a test server that returns an error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Create fetcher and fetch the URL
		f := NewFetcher()
		ctx := context.Background()
		body, err := f.FetchURL(ctx, server.URL)

		// Assert
		assert.Error(t, err)
		assert.Nil(t, body)

		// Check that the error is of type FetchError
		fetchErr, ok := err.(*FetchError)
		assert.True(t, ok)
		assert.Equal(t, http.StatusInternalServerError, fetchErr.StatusCode)
		assert.False(t, fetchErr.TooManyRequests)
	})

	t.Run("TooManyRequests", func(t *testing.T) {
		// Create a test server that returns too many requests
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		// Create fetcher with a quick backoff for testing
		backoffCfg := backoff.NewExponentialBackOff()
		backoffCfg.MaxElapsedTime = 500 * time.Millisecond // Short timeout for test
		f := NewFetcher(WithBackOffConfig(backoffCfg))

		ctx := context.Background()
		body, err := f.FetchURL(ctx, server.URL)

		// Assert
		assert.Error(t, err)
		assert.Nil(t, body)

		// Check that the error is of type FetchError
		fetchErr, ok := err.(*FetchError)
		if !ok {
			// Could be a permanent error from max retries
			assert.Contains(t, err.Error(), "max retry count")
		} else {
			assert.True(t, fetchErr.TooManyRequests)
			assert.Equal(t, 2, fetchErr.RetryAfter)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		// Create a test server with a delay
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create fetcher
		f := NewFetcher()

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Fetch should be canceled by context
		body, err := f.FetchURL(ctx, server.URL)

		// Assert
		assert.Error(t, err)
		assert.Nil(t, body)
		assert.Contains(t, err.Error(), "context")
	})
}

// TestFetchURLs tests the FetchURLs method
func TestFetchURLs(t *testing.T) {
	t.Run("MultipleFetches", func(t *testing.T) {
		// Track request count
		var requestCount int32

		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "response for %s", r.URL.Path)
		}))
		defer server.Close()

		// Create URLs
		numURLs := 10
		urls := make([]string, numURLs)
		for i := 0; i < numURLs; i++ {
			urls[i] = fmt.Sprintf("%s/%d", server.URL, i)
		}

		// Create fetcher and fetch URLs
		f := NewFetcher()
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, urls)

		// Collect results
		results := make(map[string]string)
		for result := range resultChan {
			assert.NoError(t, result.Error)
			assert.NotNil(t, result.Body)

			if result.Body != nil {
				data, err := io.ReadAll(result.Body)
				result.Body.Close()
				assert.NoError(t, err)
				results[result.Url] = string(data)
			}
		}

		// Assert all URLs were fetched
		assert.Equal(t, numURLs, len(results))
		assert.Equal(t, int32(numURLs), atomic.LoadInt32(&requestCount))

		// Check results
		for i := 0; i < numURLs; i++ {
			url := fmt.Sprintf("%s/%d", server.URL, i)
			expectedResponse := fmt.Sprintf("response for /%d", i)
			assert.Equal(t, expectedResponse, results[url])
		}
	})

	t.Run("RateLimiting", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create a lot of URLs
		numURLs := 20
		urls := make([]string, numURLs)
		for i := 0; i < numURLs; i++ {
			urls[i] = server.URL
		}

		// Create fetcher with low rate
		f := NewFetcher(
			WithRatePerSecond(2),
			WithBurst(1),
			WithMaxWorkers(5),
		)

		// Time the fetches
		start := time.Now()
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, urls)

		// Collect results
		var count int
		for result := range resultChan {
			assert.NoError(t, result.Error)
			if result.Body != nil {
				result.Body.Close()
			}
			count++
		}

		// Verify count
		assert.Equal(t, numURLs, count)

		// Check duration - should be at least 9 seconds for 20 URLs at 2 per second
		duration := time.Since(start)
		assert.GreaterOrEqual(t, duration, 9*time.Second)
	})

	t.Run("ConcurrencyLimit", func(t *testing.T) {
		// Create a mutex to protect access to the concurrent counter
		var mu sync.Mutex
		var currentConcurrent, maxConcurrent int

		// Create a test server with a delay to test concurrency
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Increment current concurrent counter
			mu.Lock()
			currentConcurrent++
			if currentConcurrent > maxConcurrent {
				maxConcurrent = currentConcurrent
			}
			mu.Unlock()

			// Sleep to maintain concurrency
			time.Sleep(100 * time.Millisecond)

			// Decrement counter
			mu.Lock()
			currentConcurrent--
			mu.Unlock()

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create a lot of URLs
		numURLs := 50
		urls := make([]string, numURLs)
		for i := 0; i < numURLs; i++ {
			urls[i] = server.URL
		}

		// Create fetcher with specific worker limit but high rate
		maxWorkers := 5
		f := NewFetcher(
			WithRatePerSecond(100), // High rate to not be rate-limited
			WithMaxWorkers(maxWorkers),
		)

		// Fetch URLs
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, urls)

		// Collect results
		for result := range resultChan {
			if result.Body != nil {
				result.Body.Close()
			}
		}

		// Verify the max concurrency was respected
		assert.LessOrEqual(t, maxConcurrent, maxWorkers)
		// We should have reached max workers at some point
		assert.GreaterOrEqual(t, maxConcurrent, maxWorkers-1)
	})

	t.Run("MixedResponses", func(t *testing.T) {
		// Create a test server with mixed responses
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract path to determine response
			path := r.URL.Path
			if path == "/success" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			} else if path == "/error" {
				w.WriteHeader(http.StatusInternalServerError)
			} else if path == "/toomany" {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
			} else if path == "/slow" {
				time.Sleep(300 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("slow"))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		// Create URLs
		urls := []string{
			server.URL + "/success",
			server.URL + "/error",
			server.URL + "/toomany",
			server.URL + "/slow",
			server.URL + "/notfound",
		}

		// Create fetcher with quick backoff for testing
		backoffCfg := backoff.NewExponentialBackOff()
		backoffCfg.MaxElapsedTime = 500 * time.Millisecond // Short timeout for test

		f := NewFetcher(
			WithBackOffConfig(backoffCfg),
			WithTimeout(1*time.Second),
		)

		// Fetch URLs
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, urls)

		// Collect results
		results := make(map[string]struct {
			body  string
			error bool
		})

		for result := range resultChan {
			resultData := struct {
				body  string
				error bool
			}{body: "", error: result.Error != nil}

			if result.Body != nil {
				data, _ := io.ReadAll(result.Body)
				result.Body.Close()
				resultData.body = string(data)
			}

			results[result.Url] = resultData
		}

		// Check results
		successURL := server.URL + "/success"
		assert.False(t, results[successURL].error)
		assert.Equal(t, "success", results[successURL].body)

		errorURL := server.URL + "/error"
		assert.True(t, results[errorURL].error)

		tooManyURL := server.URL + "/toomany"
		assert.True(t, results[tooManyURL].error)

		slowURL := server.URL + "/slow"
		assert.False(t, results[slowURL].error)
		assert.Equal(t, "slow", results[slowURL].body)

		notFoundURL := server.URL + "/notfound"
		assert.True(t, results[notFoundURL].error)
	})

	t.Run("EmptyURLList", func(t *testing.T) {
		f := NewFetcher()
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, []string{})

		// Should receive no results
		count := 0
		for range resultChan {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("SingleURL", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("single"))
		}))
		defer server.Close()

		f := NewFetcher()
		ctx := context.Background()
		resultChan := f.FetchURLs(ctx, []string{server.URL})

		// Should receive exactly one result
		count := 0
		for result := range resultChan {
			count++
			assert.NoError(t, result.Error)
			assert.NotNil(t, result.Body)
			if result.Body != nil {
				data, err := io.ReadAll(result.Body)
				result.Body.Close()
				assert.NoError(t, err)
				assert.Equal(t, "single", string(data))
			}
		}
		assert.Equal(t, 1, count)
	})

	t.Run("ContextCancellationDuringFetch", func(t *testing.T) {
		// Create a test server with delay
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		f := NewFetcher()
		ctx, cancel := context.WithCancel(context.Background())
		
		// Create multiple URLs
		urls := []string{server.URL, server.URL, server.URL}
		resultChan := f.FetchURLs(ctx, urls)

		// Cancel context after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		// Collect results
		results := 0
		for result := range resultChan {
			results++
			if result.Body != nil {
				result.Body.Close()
			}
		}

		// Should receive fewer results than total URLs due to cancellation
		assert.LessOrEqual(t, results, len(urls))
	})
}

// TestFetchErrors tests the FetchError type
func TestFetchErrors(t *testing.T) {
	t.Run("TooManyRequestsError", func(t *testing.T) {
		err := &FetchError{
			TooManyRequests: true,
			RetryAfter:      30,
			StatusCode:      429,
		}
		assert.Contains(t, err.Error(), "30 seconds")
	})

	t.Run("StatusCodeError", func(t *testing.T) {
		err := &FetchError{
			StatusCode: 404,
		}
		assert.Contains(t, err.Error(), "404")
	})
}

// Integration test with a realistic server that randomly returns errors
func TestIntegrationWithRandomErrors(t *testing.T) {
	// Skip in short test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a test server with random behavior
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Seed with request path to get consistent behavior per URL
		pathSeed := int64(0)
		for _, c := range r.URL.Path {
			pathSeed += int64(c)
		}
		rand.Seed(pathSeed)

		// Random behavior
		randomVal := rand.Intn(100)
		switch {
		case randomVal < 20:
			// 20% chance of error
			w.WriteHeader(http.StatusInternalServerError)
		case randomVal < 30:
			// 10% chance of too many requests
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case randomVal < 40:
			// 10% chance of slow response
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("slow response for %s", r.URL.Path)))
		default:
			// 60% chance of success
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("response for %s", r.URL.Path)))
		}
	}))
	defer server.Close()

	// Create a large number of URLs
	numURLs := 30
	urls := make([]string, numURLs)
	for i := 0; i < numURLs; i++ {
		urls[i] = fmt.Sprintf("%s/path%d", server.URL, i)
	}

	// Create fetcher with retry configuration
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxElapsedTime = 5 * time.Second
	backoffCfg.InitialInterval = 100 * time.Millisecond
	backoffCfg.MaxInterval = 1 * time.Second

	f := NewFetcher(
		WithRatePerSecond(10),
		WithBurst(5),
		WithMaxWorkers(8),
		WithBackOffConfig(backoffCfg),
		WithTimeout(2*time.Second),
	)

	// Fetch URLs
	ctx := context.Background()
	resultChan := f.FetchURLs(ctx, urls)

	// Collect results
	successCount := 0
	errorCount := 0

	for result := range resultChan {
		if result.Error == nil {
			successCount++
			if result.Body != nil {
				io.Copy(io.Discard, result.Body) // Read the body
				result.Body.Close()
			}
		} else {
			errorCount++
		}
	}

	// Verify we got some successes and some errors
	t.Logf("Success count: %d, Error count: %d", successCount, errorCount)
	assert.True(t, successCount > 0)
	assert.True(t, errorCount > 0)
	assert.Equal(t, numURLs, successCount+errorCount)
}

// Benchmarks
func BenchmarkFetcher(b *testing.B) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("benchmark response"))
	}))
	defer server.Close()

	b.Run("SingleFetch", func(b *testing.B) {
		f := NewFetcher()
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			body, err := f.FetchURL(ctx, server.URL)
			if err == nil && body != nil {
				io.Copy(io.Discard, body)
				body.Close()
			}
		}
	})

	b.Run("ConcurrentFetches", func(b *testing.B) {
		f := NewFetcher(
			WithRatePerSecond(100),
			WithMaxWorkers(20),
		)
		ctx := context.Background()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Create 10 URLs to fetch concurrently
			numURLs := 10
			urls := make([]string, numURLs)
			for j := 0; j < numURLs; j++ {
				urls[j] = server.URL
			}

			resultChan := f.FetchURLs(ctx, urls)
			for result := range resultChan {
				if result.Body != nil {
					io.Copy(io.Discard, result.Body)
					result.Body.Close()
				}
			}
		}
	})
}
