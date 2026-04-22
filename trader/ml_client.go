package trader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// MLSignalClient is a thin HTTP client for the Python ML signal sidecar.
// The sidecar lives in ml-signal-service/ and exposes POST /signal which
// takes a feature vector and returns a directional score.
//
// Design goals:
//   - Zero impact when the sidecar is unreachable. If the service is down
//     we return err and the caller (momentum_fallback.go) keeps using
//     rule-based scoring. Never block the cycle.
//   - Short timeouts. ML is an enhancement, not a critical path. A 500ms
//     deadline is plenty for local HTTP + a single tree-model inference.
//   - Consecutive-failure circuit breaker. If the sidecar is flapping,
//     skip calling it for a cooldown window to avoid adding latency to
//     every cycle.
type MLSignalClient struct {
	baseURL    string
	client     *http.Client
	// Circuit breaker state. Atomic so it's safe under concurrent use.
	consecutiveFailures int64
	nextRetryUnixMs     int64
}

const (
	mlClientDefaultURL     = "http://127.0.0.1:9091"
	mlClientTimeout        = 500 * time.Millisecond
	mlBreakerThreshold     = 3
	mlBreakerCooldownSecs  = 60
)

// NewMLSignalClient builds a client pointed at the configured sidecar URL.
// Pass empty baseURL to use the default (localhost:9091).
func NewMLSignalClient(baseURL string) *MLSignalClient {
	if baseURL == "" {
		baseURL = mlClientDefaultURL
	}
	return &MLSignalClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: mlClientTimeout,
			Transport: &http.Transport{
				// No connection reuse across cycles to keep the code simple;
				// signal requests are cheap and infrequent (per cycle, per
				// candidate). Premature optimization here costs clarity.
				DisableKeepAlives:     false,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   200 * time.Millisecond,
				ResponseHeaderTimeout: 400 * time.Millisecond,
			},
		},
	}
}

// MLSignalRequest is the wire format sent to the sidecar.
type MLSignalRequest struct {
	Symbol      string             `json:"symbol"`
	Features    map[string]float64 `json:"features"`
	TimestampMs int64              `json:"timestamp_ms,omitempty"`
}

// MLSignalResponse is the wire format returned by the sidecar.
// Mirrors the Pydantic schema in server.py.
type MLSignalResponse struct {
	Symbol        string             `json:"symbol"`
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	ModelVersion  string             `json:"model_version"`
	ActionHint    string             `json:"action_hint"`
	ServedAt      string             `json:"served_at"`
}

// Score asks the ML sidecar for a directional score on the given feature set.
// Returns the response plus an error. The caller should treat non-nil err as
// "use the rule-based fallback" — do not panic, do not halt the cycle.
//
// Circuit breaker: after `mlBreakerThreshold` consecutive failures we refuse
// to call for `mlBreakerCooldownSecs` seconds. This prevents a dead sidecar
// from adding 500ms per cycle × number of candidates of latency.
func (c *MLSignalClient) Score(symbol string, features map[string]float64, timestampMs int64) (*MLSignalResponse, error) {
	nowMs := time.Now().UnixMilli()
	nextRetry := atomic.LoadInt64(&c.nextRetryUnixMs)
	if nextRetry > nowMs {
		return nil, fmt.Errorf("ml sidecar circuit open until %d (%.0fs remaining)", nextRetry, float64(nextRetry-nowMs)/1000.0)
	}

	req := MLSignalRequest{Symbol: symbol, Features: features, TimestampMs: timestampMs}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ml request marshal: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/signal", bytes.NewReader(body))
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("ml request build: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("ml request: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		c.recordFailure()
		return nil, fmt.Errorf("ml %d: %s", resp.StatusCode, string(payload))
	}

	var out MLSignalResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("ml decode: %w", err)
	}
	c.recordSuccess()
	return &out, nil
}

// Health returns nil if the sidecar answers /health with 200 within timeout.
// Useful for startup preflight and monitoring.
func (c *MLSignalClient) Health() error {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ml health %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *MLSignalClient) recordFailure() {
	n := atomic.AddInt64(&c.consecutiveFailures, 1)
	if n >= mlBreakerThreshold {
		cooldownUntil := time.Now().Add(mlBreakerCooldownSecs * time.Second).UnixMilli()
		atomic.StoreInt64(&c.nextRetryUnixMs, cooldownUntil)
	}
}

func (c *MLSignalClient) recordSuccess() {
	atomic.StoreInt64(&c.consecutiveFailures, 0)
	atomic.StoreInt64(&c.nextRetryUnixMs, 0)
}
