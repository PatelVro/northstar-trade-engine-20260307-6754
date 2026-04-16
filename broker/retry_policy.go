// Package broker - retry_policy.go
// retryPolicy defines per-endpoint-class retry behaviour for IBKR HTTP calls.
// Different endpoint classes have different retry semantics:
//
//	auth      — do NOT retry (cookie-based sessions; retrying just causes re-auth storms)
//	quote     — retry up to 3x with exponential backoff (idempotent read)
//	portfolio — retry up to 3x with exponential backoff (idempotent read)
//	order     — retry only on transient network errors, NOT on 4xx responses (non-idempotent write)
//
// Retryable conditions: connection refused, timeout, 429 (with Retry-After), 500/502/503/504
// Non-retryable: 400, 401, 403, 404, any 4xx except 429
package broker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// EndpointClass categorizes IBKR HTTP endpoints by their retry semantics.
type EndpointClass int

const (
	EndpointClassAuth      EndpointClass = iota // session/auth endpoints — no retry
	EndpointClassQuote                          // market-data / quote reads — idempotent
	EndpointClassPortfolio                      // account/portfolio reads — idempotent
	EndpointClassOrder                          // order submit/cancel writes — non-idempotent
)

// RetryPolicy captures the backoff parameters for one endpoint class.
type RetryPolicy struct {
	MaxAttempts    int           // total attempts (1 = no retry)
	InitialBackoff time.Duration // backoff before second attempt
	MaxBackoff     time.Duration // cap on backoff growth
	Multiplier     float64       // backoff growth factor
}

// DefaultRetryPolicies is the canonical retry-policy matrix indexed by EndpointClass.
var DefaultRetryPolicies = map[EndpointClass]RetryPolicy{
	EndpointClassAuth: {
		MaxAttempts:    1,
		InitialBackoff: 0,
		MaxBackoff:     0,
		Multiplier:     1.0,
	},
	EndpointClassQuote: {
		MaxAttempts:    3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		Multiplier:     2.0,
	},
	EndpointClassPortfolio: {
		MaxAttempts:    3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		Multiplier:     2.0,
	},
	EndpointClassOrder: {
		MaxAttempts:    2,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     3 * time.Second,
		Multiplier:     2.0,
	},
}

// IsRetryable returns true when the given HTTP status code or transport error
// represents a transient condition that is safe to retry.
//
// Rules:
//   - nil error with non-retryable status → false
//   - transport errors (connection refused, timeout, EOF, etc.) → true
//   - HTTP 429 → true (rate-limited; caller should honour Retry-After)
//   - HTTP 5xx (500, 502, 503, 504) → true
//   - HTTP 4xx except 429 (400, 401, 403, 404, …) → false
//   - HTTP 200-399 → false
func IsRetryable(statusCode int, err error) bool {
	if err != nil {
		// Delegate to the existing transport classifier which already handles
		// net.Error timeout/temporary, connection refused, EOF, etc.
		if classifyIBKRTransport(err) == IBKRErrorTransient {
			return true
		}
		// Fall through: maybe the error carries a status code we can inspect.
	}

	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}

	if statusCode >= 500 {
		return true
	}

	return false
}

// RetryAfterDuration parses the Retry-After response header.
// It supports both integer seconds and HTTP-date formats.
// If the header is absent or unparseable, a default of 5 s is returned.
func RetryAfterDuration(resp *http.Response) time.Duration {
	const defaultDelay = 5 * time.Second

	if resp == nil {
		return defaultDelay
	}

	ra := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if ra == "" {
		return defaultDelay
	}

	// Try integer seconds first.
	if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs >= 0 {
		d := time.Duration(secs * float64(time.Second))
		if d < time.Second {
			d = time.Second
		}
		if d > 60*time.Second {
			d = 60 * time.Second
		}
		return d
	}

	// Try HTTP-date format (RFC 1123 / RFC 850 / ANSI C asctime).
	layouts := []string{
		http.TimeFormat,              // Mon, 02 Jan 2006 15:04:05 GMT
		"Monday, 02-Jan-06 15:04:05 MST", // RFC 850
		"Mon Jan _2 15:04:05 2006",   // ANSI C asctime
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ra); err == nil {
			d := time.Until(t)
			if d < time.Second {
				d = time.Second
			}
			if d > 60*time.Second {
				d = 60 * time.Second
			}
			return d
		}
	}

	return defaultDelay
}

// DoWithRetry executes fn according to the RetryPolicy for the given EndpointClass.
//
//   - Respects ctx cancellation between attempts.
//   - For 429 responses, waits the Retry-After duration before retrying.
//   - Logs each retry attempt with attempt number and backoff duration.
//   - Order endpoints (EndpointClassOrder) only retry on transport errors or 5xx/429;
//     they do NOT retry on 4xx (non-idempotent write guard).
func DoWithRetry(ctx context.Context, class EndpointClass, fn func() (*http.Response, error)) (*http.Response, error) {
	policy, ok := DefaultRetryPolicies[class]
	if !ok {
		// Unknown class — execute once with no retry.
		return fn()
	}

	var (
		resp    *http.Response
		lastErr error
	)

	backoff := policy.InitialBackoff

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Honour context cancellation before every attempt.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("retry aborted (attempt %d/%d): %w", attempt, policy.MaxAttempts, err)
		}

		resp, lastErr = fn()

		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}

		// Determine whether this outcome is retryable for this endpoint class.
		shouldRetry := false
		if lastErr != nil || statusCode != 0 {
			if class == EndpointClassOrder {
				// Orders: only retry transport errors or 5xx/429; never 4xx other than 429.
				if lastErr != nil && classifyIBKRTransport(lastErr) == IBKRErrorTransient {
					shouldRetry = true
				} else if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
					shouldRetry = true
				}
				// Explicit: do NOT retry on 4xx (except 429 handled above).
			} else {
				shouldRetry = IsRetryable(statusCode, lastErr)
			}
		}

		if !shouldRetry || attempt >= policy.MaxAttempts {
			// Either success, non-retryable failure, or exhausted attempts.
			break
		}

		// Calculate wait duration for this retry.
		wait := backoff
		if statusCode == http.StatusTooManyRequests && resp != nil {
			// Respect Retry-After header for rate-limit responses.
			raWait := RetryAfterDuration(resp)
			if raWait > wait {
				wait = raWait
			}
		}

		// Close the response body before sleeping so connections are returned.
		if resp != nil {
			resp.Body.Close()
			resp = nil
		}

		log.Printf(" IBKR retry_policy: class=%d attempt=%d/%d backoff=%s err=%v status=%d",
			class, attempt, policy.MaxAttempts, wait, lastErr, statusCode)

		// Advance backoff for next iteration.
		nextBackoff := time.Duration(float64(backoff) * policy.Multiplier)
		if nextBackoff > policy.MaxBackoff && policy.MaxBackoff > 0 {
			nextBackoff = policy.MaxBackoff
		}
		backoff = nextBackoff

		// Sleep with context awareness.
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("retry aborted during backoff (attempt %d/%d): %w", attempt, policy.MaxAttempts, ctx.Err())
		case <-time.After(wait):
		}
	}

	return resp, lastErr
}
