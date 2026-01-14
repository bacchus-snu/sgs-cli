// retry.go provides retry logic for transient network errors.
// It includes generic retry functions with configurable attempts and delays.
package client

import (
	"context"
	"strings"
	"time"
)

const (
	// DefaultMaxRetries is the default number of retries (1 retry = 2 total attempts)
	DefaultMaxRetries = 1
	// DefaultRetryDelay is the delay between retries
	DefaultRetryDelay = 500 * time.Millisecond
)

// RetryableFunc is a function that can be retried
type RetryableFunc[T any] func() (T, error)

// Retry executes a function with retry logic for transient errors
func Retry[T any](fn RetryableFunc[T]) (T, error) {
	return RetryWithConfig(fn, DefaultMaxRetries, DefaultRetryDelay)
}

// RetryWithConfig executes a function with configurable retry logic
func RetryWithConfig[T any](fn RetryableFunc[T], maxRetries int, delay time.Duration) (T, error) {
	var lastErr error
	var zero T

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsRetryableError(err) {
			return zero, err
		}
	}

	return zero, lastErr
}

// RetryVoid executes a void function with retry logic
func RetryVoid(fn func() error) error {
	_, err := Retry(func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// RetryWithContext executes a function with retry logic and context
func RetryWithContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var lastErr error
	var zero T

	for attempt := 0; attempt <= DefaultMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(DefaultRetryDelay):
			}
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !IsRetryableError(err) {
			return zero, err
		}
	}

	return zero, lastErr
}

// IsRetryableError checks if the error is retryable (connection reset, etc.)
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()

	retryablePatterns := []string{
		"connection reset by peer",
		"connection refused",
		"EOF",
		"no such host",
		"i/o timeout",
		"TLS handshake timeout",
		"network is unreachable",
		"temporary failure in name resolution",
		"dial tcp",
		"read tcp",
		"write tcp",
		// Credential fetching errors that may be transient
		"getting credentials",
		"exec: executable kubectl failed",
		"oidc error",
		"oidc discovery error",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}
