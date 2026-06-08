// Package httputil provides shared HTTP utilities for model adapters.
package httputil

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

// MaxRetries is the default number of retry attempts for transient errors.
const MaxRetries = 3

// RetryableStatusCodes are HTTP status codes that should be retried.
var RetryableStatusCodes = map[int]bool{
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// DoWithRetry executes an HTTP request with exponential backoff retry
// for transient errors (429, 5xx). Respects Retry-After headers.
//
// The requestFn is called on each attempt to create a fresh request
// (since request bodies can only be read once).
func DoWithRetry(client *http.Client, requestFn func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		req, err := requestFn()
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < MaxRetries {
				backoff(attempt, 0)
				continue
			}
			return nil, fmt.Errorf("after %d retries: %w", MaxRetries, lastErr)
		}

		// Success or non-retryable error
		if !RetryableStatusCodes[resp.StatusCode] {
			return resp, nil
		}

		// Retryable error — read and discard body, then retry
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))

		if attempt < MaxRetries {
			retryAfter := parseRetryAfter(resp)
			backoff(attempt, retryAfter)
		}
	}

	return nil, fmt.Errorf("after %d retries: %w", MaxRetries, lastErr)
}

// backoff sleeps for exponential backoff duration.
// Base delay: 1s, 2s, 4s. Respects retryAfter if provided.
func backoff(attempt int, retryAfterSecs int) {
	delay := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	if retryAfterSecs > 0 {
		retryDelay := time.Duration(retryAfterSecs) * time.Second
		if retryDelay > delay {
			delay = retryDelay
		}
	}
	// Cap at 30 seconds
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	time.Sleep(delay)
}

// parseRetryAfter extracts the Retry-After header value in seconds.
func parseRetryAfter(resp *http.Response) int {
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0
	}
	secs, err := strconv.Atoi(header)
	if err != nil {
		return 0
	}
	return secs
}
