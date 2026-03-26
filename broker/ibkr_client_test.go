package broker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestScoreSecdefEquityCandidate_PrefersStockListings(t *testing.T) {
	futuresScore := scoreSecdefEquityCandidate(
		"BA",
		"BA",
		"ASX",
		"BARLEY FUTURES - ASX",
		"",
		[]ibkrSecdefSection{
			{SecType: "FUT", Exchange: "SNFE"},
		},
	)

	stockScore := scoreSecdefEquityCandidate(
		"BA",
		"BA",
		"NYSE",
		"BOEING CO/THE - NYSE",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "SMART;NYSE"},
		},
	)

	if stockScore <= futuresScore {
		t.Fatalf("expected stock score > futures score (stock=%d futures=%d)", stockScore, futuresScore)
	}
}

func TestScoreSecdefEquityCandidate_BonusesSmartRouting(t *testing.T) {
	plainStock := scoreSecdefEquityCandidate(
		"AAPL",
		"AAPL",
		"NASDAQ",
		"APPLE INC - NASDAQ",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "NASDAQ"},
		},
	)

	smartStock := scoreSecdefEquityCandidate(
		"AAPL",
		"AAPL",
		"NASDAQ",
		"APPLE INC - NASDAQ",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "SMART;NASDAQ"},
		},
	)

	if smartStock <= plainStock {
		t.Fatalf("expected SMART-routed stock score > plain stock score (smart=%d plain=%d)", smartStock, plainStock)
	}
}

func TestClassifyIBKRError_TransientTransport(t *testing.T) {
	err := NewIBKRTransportError("GET", "/iserver/account/orders", errors.New("connection refused"))
	if got := ClassifyIBKRError(err); got != IBKRErrorTransient {
		t.Fatalf("expected transient classification, got %s", got)
	}
	if !IsRetryableIBKRError(err) {
		t.Fatalf("expected retryable IBKR error")
	}
}

func TestClassifyIBKRError_TransientGatewayHTTP(t *testing.T) {
	err := NewIBKRHTTPError("GET", "/iserver/account/orders", 503, "gateway unavailable")
	if got := ClassifyIBKRError(err); got != IBKRErrorTransient {
		t.Fatalf("expected transient classification, got %s", got)
	}
}

func TestClassifyIBKRError_Auth(t *testing.T) {
	err := NewIBKRHTTPError("GET", "/portfolio/DU123456/summary", 403, "forbidden")
	if got := ClassifyIBKRError(err); got != IBKRErrorAuth {
		t.Fatalf("expected auth classification, got %s", got)
	}
	if !IsActionableIBKRError(err) {
		t.Fatalf("expected auth error to be operator-actionable")
	}
}

func TestClassifyIBKRError_Request(t *testing.T) {
	err := NewIBKRHTTPError("POST", "/iserver/account/orders", 400, "invalid contract")
	if got := ClassifyIBKRError(err); got != IBKRErrorRequest {
		t.Fatalf("expected request classification, got %s", got)
	}
	if IsRetryableIBKRError(err) {
		t.Fatalf("request error should not be retryable")
	}
}

func TestCheckLiveReadinessWarmsPortfolioEndpointsAndRetries(t *testing.T) {
	var sawSubaccounts int32
	var sawSubaccounts2 int32
	var summaryCalls int32
	var positionsCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/iserver/auth/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"authenticated":true,"connected":true}`))
		case r.URL.Path == "/iserver/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accounts":["DU123456"],"selectedAccount":"DU123456"}`))
		case r.URL.Path == "/portfolio/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["DU123456"]`))
		case r.URL.Path == "/portfolio/subaccounts":
			atomic.StoreInt32(&sawSubaccounts, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"DU123456"}]`))
		case r.URL.RequestURI() == "/portfolio/subaccounts2?page=0":
			atomic.StoreInt32(&sawSubaccounts2, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"DU123456"}]`))
		case r.URL.Path == "/portfolio/DU123456/meta":
			if atomic.LoadInt32(&sawSubaccounts) == 0 || atomic.LoadInt32(&sawSubaccounts2) == 0 {
				http.Error(w, "portfolio warm-up required", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"DU123456"}`))
		case r.URL.Path == "/portfolio/DU123456/summary":
			if atomic.AddInt32(&summaryCalls, 1) == 1 {
				http.Error(w, "gateway warming account summary", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"netliquidation":{"amount":101250}}`))
		case r.URL.Path == "/portfolio/DU123456/positions":
			if atomic.AddInt32(&positionsCalls, 1) == 1 {
				http.Error(w, "gateway warming positions", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/iserver/account/orders":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orders":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &IBKRClient{
		BaseURL:                 server.URL,
		AccountID:               "DU123456",
		HTTPClient:              server.Client(),
		PortfolioRequestTimeout: time.Second,
		PortfolioRetryAttempts:  3,
		PortfolioWarmTTL:        time.Second,
		conIDCache:              make(map[string]int),
	}

	if err := client.CheckLiveReadiness("DU123456"); err != nil {
		t.Fatalf("expected live readiness to succeed after warm-up and retry, got %v", err)
	}
	if atomic.LoadInt32(&sawSubaccounts) == 0 || atomic.LoadInt32(&sawSubaccounts2) == 0 {
		t.Fatalf("expected optional portfolio warm-up endpoints to be called")
	}
	if atomic.LoadInt32(&summaryCalls) < 2 {
		t.Fatalf("expected summary endpoint retry, got %d call(s)", atomic.LoadInt32(&summaryCalls))
	}
	if atomic.LoadInt32(&positionsCalls) < 2 {
		t.Fatalf("expected positions endpoint retry, got %d call(s)", atomic.LoadInt32(&positionsCalls))
	}
}

func TestFetchPortfolioEndpointFailsFastOnHungAccountEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/portfolio/accounts", "/portfolio/subaccounts", "/portfolio/subaccounts2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["DU123456"]`))
		case "/portfolio/DU123456/summary":
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"netliquidation":{"amount":101250}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &IBKRClient{
		BaseURL:                 server.URL,
		AccountID:               "DU123456",
		HTTPClient:              server.Client(),
		PortfolioRequestTimeout: 60 * time.Millisecond,
		PortfolioRetryAttempts:  2,
		PortfolioWarmTTL:        time.Second,
		conIDCache:              make(map[string]int),
	}

	start := time.Now()
	_, err := client.FetchPortfolioEndpoint("DU123456", "summary")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error for hung portfolio endpoint")
	}
	if elapsed > time.Second {
		t.Fatalf("expected fail-fast behavior, elapsed=%s", elapsed)
	}
	if got := ClassifyIBKRError(err); got != IBKRErrorTransient {
		t.Fatalf("expected transient classification for timeout, got %s", got)
	}
}

func TestCheckAuthStatus_Authenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iserver/auth/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"authenticated":true,"connected":true}`))
		case "/iserver/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accounts":["DU123456"]}`))
		case "/portfolio/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"DU123456"}]`))
		case "/portfolio/DU123456/meta":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &IBKRClient{
		BaseURL:    server.URL,
		AccountID:  "DU123456",
		HTTPClient: server.Client(),
		conIDCache: make(map[string]int),
	}

	client.checkAuthStatus()

	if !client.IsAuthenticated() {
		t.Fatalf("expected authenticated=true after successful auth status check")
	}
}

func TestCheckAuthStatus_NotAuthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iserver/auth/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"authenticated":false,"connected":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &IBKRClient{
		BaseURL:    server.URL,
		AccountID:  "DU123456",
		HTTPClient: server.Client(),
		conIDCache: make(map[string]int),
	}

	// Pre-set to true so we can confirm it gets flipped
	client.setAuthStatus(true)
	client.checkAuthStatus()

	if client.IsAuthenticated() {
		t.Fatalf("expected authenticated=false when gateway reports authenticated=false")
	}
}

func TestSessionLostDetection_401Handling(t *testing.T) {
	var authCheckCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iserver/auth/status":
			atomic.AddInt32(&authCheckCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			// Auth check also reports not authenticated (session truly dead)
			_, _ = w.Write([]byte(`{"authenticated":false,"connected":false}`))
		case "/some/endpoint":
			// Return 401 to simulate session loss
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`unauthorized`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &IBKRClient{
		BaseURL:    server.URL,
		AccountID:  "DU123456",
		HTTPClient: server.Client(),
		conIDCache: make(map[string]int),
	}
	client.setAuthStatus(true)

	req, _ := http.NewRequest("GET", server.URL+"/some/endpoint", nil)
	_, err := client.Do(req)

	if err == nil {
		t.Fatalf("expected error from 401 response")
	}

	var reqErr *IBKRRequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected IBKRRequestError, got %T: %v", err, err)
	}
	if reqErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", reqErr.StatusCode)
	}

	if client.IsAuthenticated() {
		t.Fatalf("expected AuthStatus=false after session loss via 401")
	}

	if atomic.LoadInt32(&authCheckCalls) == 0 {
		t.Fatalf("expected checkAuthStatus to be called during 401 recovery attempt")
	}
}

func TestClassifyIBKRError_401IsTransient(t *testing.T) {
	err := NewIBKRHTTPError("GET", "/some/endpoint", 401, "unauthorized")
	got := ClassifyIBKRError(err)
	if got != IBKRErrorTransient {
		t.Fatalf("expected 401 to be classified as transient, got %s", got)
	}
	if !IsRetryableIBKRError(err) {
		t.Fatalf("expected 401 error to be retryable")
	}
}
