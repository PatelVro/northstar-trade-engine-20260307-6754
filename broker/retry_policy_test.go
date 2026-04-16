package broker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- IsRetryable ---

func TestIsRetryable_429(t *testing.T) {
	if !IsRetryable(http.StatusTooManyRequests, nil) {
		t.Error("expected 429 to be retryable")
	}
}

func TestIsRetryable_500(t *testing.T) {
	if !IsRetryable(http.StatusInternalServerError, nil) {
		t.Error("expected 500 to be retryable")
	}
}

func TestIsRetryable_502(t *testing.T) {
	if !IsRetryable(http.StatusBadGateway, nil) {
		t.Error("expected 502 to be retryable")
	}
}

func TestIsRetryable_503(t *testing.T) {
	if !IsRetryable(http.StatusServiceUnavailable, nil) {
		t.Error("expected 503 to be retryable")
	}
}

func TestIsRetryable_504(t *testing.T) {
	if !IsRetryable(http.StatusGatewayTimeout, nil) {
		t.Error("expected 504 to be retryable")
	}
}

func TestIsRetryable_400(t *testing.T) {
	if IsRetryable(http.StatusBadRequest, nil) {
		t.Error("expected 400 to NOT be retryable")
	}
}

func TestIsRetryable_403(t *testing.T) {
	if IsRetryable(http.StatusForbidden, nil) {
		t.Error("expected 403 to NOT be retryable")
	}
}

func TestIsRetryable_404(t *testing.T) {
	if IsRetryable(http.StatusNotFound, nil) {
		t.Error("expected 404 to NOT be retryable")
	}
}

func TestIsRetryable_200_noError(t *testing.T) {
	if IsRetryable(http.StatusOK, nil) {
		t.Error("expected 200 with nil error to NOT be retryable")
	}
}

func TestIsRetryable_transportError(t *testing.T) {
	// connection refused is a transient transport error
	err := errors.New("connection refused")
	if !IsRetryable(0, err) {
		t.Error("expected transport error (connection refused) to be retryable")
	}
}

func TestIsRetryable_timeoutError(t *testing.T) {
	err := errors.New("request timeout")
	if !IsRetryable(0, err) {
		t.Error("expected timeout error to be retryable")
	}
}

// --- RetryAfterDuration ---

func TestRetryAfterDuration_noHeader(t *testing.T) {
	resp := &http.Response{Header: make(http.Header)}
	d := RetryAfterDuration(resp)
	if d != 5*time.Second {
		t.Errorf("expected 5s default, got %s", d)
	}
}

func TestRetryAfterDuration_integerSeconds(t *testing.T) {
	resp := &http.Response{Header: make(http.Header)}
	resp.Header.Set("Retry-After", "10")
	d := RetryAfterDuration(resp)
	if d != 10*time.Second {
		t.Errorf("expected 10s, got %s", d)
	}
}

func TestRetryAfterDuration_nilResp(t *testing.T) {
	d := RetryAfterDuration(nil)
	if d != 5*time.Second {
		t.Errorf("expected 5s default for nil response, got %s", d)
	}
}

// --- DoWithRetry: basic success path ---

func TestDoWithRetry_successOnFirstAttempt(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}

	resp, err := DoWithRetry(context.Background(), EndpointClassQuote, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

// --- DoWithRetry: fn fails twice then succeeds → 2 retries ---

func TestDoWithRetry_failsThenSucceeds(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		if calls < 3 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       http.NoBody,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}

	// Use EndpointClassQuote which allows 3 attempts.
	// Override with a minimal backoff so the test is fast.
	origPolicy := DefaultRetryPolicies[EndpointClassQuote]
	DefaultRetryPolicies[EndpointClassQuote] = RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Multiplier:     2.0,
	}
	defer func() { DefaultRetryPolicies[EndpointClassQuote] = origPolicy }()

	resp, err := DoWithRetry(context.Background(), EndpointClassQuote, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on success, got %d", resp.StatusCode)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls)
	}
}

// --- DoWithRetry: fn always fails → error after MaxAttempts ---

func TestDoWithRetry_alwaysFails(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       http.NoBody,
		}, nil
	}

	origPolicy := DefaultRetryPolicies[EndpointClassPortfolio]
	DefaultRetryPolicies[EndpointClassPortfolio] = RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Multiplier:     2.0,
	}
	defer func() { DefaultRetryPolicies[EndpointClassPortfolio] = origPolicy }()

	resp, err := DoWithRetry(context.Background(), EndpointClassPortfolio, fn)
	// Should return the last 503 response (not an error) since DoWithRetry propagates
	// the final fn() result regardless.
	if err != nil {
		t.Fatalf("unexpected error: %v (expected nil, last resp returned)", err)
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected final 503 response, got %v", resp)
	}
	if calls != 3 {
		t.Errorf("expected exactly MaxAttempts=%d calls, got %d", 3, calls)
	}
}

// --- EndpointClassAuth: MaxAttempts=1, no retry even on 500 ---

func TestDoWithRetry_authNoRetryOn500(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       http.NoBody,
		}, nil
	}

	resp, err := DoWithRetry(context.Background(), EndpointClassAuth, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 response, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 call (no retry for auth), got %d", calls)
	}
}

// --- EndpointClassOrder: no retry on 4xx (non-idempotent write guard) ---

func TestDoWithRetry_orderNoRetryOn4xx(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       http.NoBody,
		}, nil
	}

	origPolicy := DefaultRetryPolicies[EndpointClassOrder]
	DefaultRetryPolicies[EndpointClassOrder] = RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Multiplier:     2.0,
	}
	defer func() { DefaultRetryPolicies[EndpointClassOrder] = origPolicy }()

	resp, _ := DoWithRetry(context.Background(), EndpointClassOrder, fn)
	if calls != 1 {
		t.Errorf("expected 1 call (order 400 should NOT retry), got %d", calls)
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 response returned as-is")
	}
}

// --- EndpointClassOrder: DOES retry on 500 ---

func TestDoWithRetry_orderRetriesOn500(t *testing.T) {
	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		if calls < 2 {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}

	origPolicy := DefaultRetryPolicies[EndpointClassOrder]
	DefaultRetryPolicies[EndpointClassOrder] = RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Multiplier:     2.0,
	}
	defer func() { DefaultRetryPolicies[EndpointClassOrder] = origPolicy }()

	resp, err := DoWithRetry(context.Background(), EndpointClassOrder, fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 failure + 1 success), got %d", calls)
	}
}

// --- Context cancellation during retry ---

func TestDoWithRetry_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	calls := 0
	fn := func() (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}, nil
	}

	_, err := DoWithRetry(ctx, EndpointClassQuote, fn)
	if err == nil {
		t.Error("expected error when context is cancelled before first attempt")
	}
	if calls != 0 {
		t.Errorf("expected 0 calls with cancelled context, got %d", calls)
	}
}

// --- DefaultRetryPolicies sanity checks ---

func TestDefaultRetryPolicies_authMaxAttempts(t *testing.T) {
	p := DefaultRetryPolicies[EndpointClassAuth]
	if p.MaxAttempts != 1 {
		t.Errorf("auth policy MaxAttempts should be 1, got %d", p.MaxAttempts)
	}
}

func TestDefaultRetryPolicies_quoteMaxAttempts(t *testing.T) {
	p := DefaultRetryPolicies[EndpointClassQuote]
	if p.MaxAttempts != 3 {
		t.Errorf("quote policy MaxAttempts should be 3, got %d", p.MaxAttempts)
	}
}

func TestDefaultRetryPolicies_portfolioMaxAttempts(t *testing.T) {
	p := DefaultRetryPolicies[EndpointClassPortfolio]
	if p.MaxAttempts != 3 {
		t.Errorf("portfolio policy MaxAttempts should be 3, got %d", p.MaxAttempts)
	}
}

func TestDefaultRetryPolicies_orderMaxAttempts(t *testing.T) {
	p := DefaultRetryPolicies[EndpointClassOrder]
	if p.MaxAttempts != 2 {
		t.Errorf("order policy MaxAttempts should be 2, got %d", p.MaxAttempts)
	}
}

// --- RetryAfterDuration with test server ---

func TestRetryAfterDuration_httpTestServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	d := RetryAfterDuration(resp)
	if d != 3*time.Second {
		t.Errorf("expected 3s from Retry-After header, got %s", d)
	}
}
