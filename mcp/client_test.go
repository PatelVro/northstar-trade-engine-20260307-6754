package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helper: build a valid OpenAI-compatible chat completion response body.
func validResponseBody(content string) string {
	resp := struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{
			{Message: struct {
				Content string `json:"content"`
			}{Content: content}},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// --- New / SetXxx ---

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.Provider != ProviderDeepSeek {
		t.Fatalf("expected default provider DeepSeek, got %s", c.Provider)
	}
	if c.Model != "deepseek-chat" {
		t.Fatalf("expected default model deepseek-chat, got %s", c.Model)
	}
	if c.Timeout != 120*time.Second {
		t.Fatalf("expected 120s timeout, got %v", c.Timeout)
	}
}

func TestSetDeepSeekAPIKey(t *testing.T) {
	c := New()
	c.SetDeepSeekAPIKey("sk-test")
	if c.APIKey != "sk-test" {
		t.Fatalf("API key not set")
	}
	if c.Provider != ProviderDeepSeek {
		t.Fatalf("provider should be deepseek")
	}
	if !strings.Contains(c.BaseURL, "deepseek") {
		t.Fatalf("base URL should contain deepseek, got %s", c.BaseURL)
	}
}

func TestSetQwenAPIKey(t *testing.T) {
	c := New()
	c.SetQwenAPIKey("ak", "sk")
	if c.APIKey != "ak" || c.SecretKey != "sk" {
		t.Fatalf("Qwen keys not set correctly")
	}
	if c.Provider != ProviderQwen {
		t.Fatalf("provider should be qwen")
	}
}

func TestSetCustomAPI(t *testing.T) {
	c := New()
	c.SetCustomAPI("http://localhost:8000/v1", "key", "model-x")
	if c.UseFullURL {
		t.Fatalf("UseFullURL should be false without # suffix")
	}
	if c.BaseURL != "http://localhost:8000/v1" {
		t.Fatalf("unexpected base URL: %s", c.BaseURL)
	}

	// With # suffix → UseFullURL=true, hash stripped
	c.SetCustomAPI("http://localhost:8000/v1/chat/completions#", "key", "model-y")
	if !c.UseFullURL {
		t.Fatalf("UseFullURL should be true with # suffix")
	}
	if strings.Contains(c.BaseURL, "#") {
		t.Fatalf("hash should be stripped from base URL")
	}
}

func TestSetClient(t *testing.T) {
	c := New()
	replacement := Client{
		Provider: ProviderCustom,
		APIKey:   "replaced-key",
		BaseURL:  "http://replaced",
		Model:    "replaced-model",
		Timeout:  99 * time.Second,
	}
	c.SetClient(replacement)
	if c.Provider != ProviderCustom {
		t.Fatalf("SetClient should update provider, got %s", c.Provider)
	}
	if c.APIKey != "replaced-key" {
		t.Fatalf("SetClient should update APIKey, got %s", c.APIKey)
	}
	if c.Model != "replaced-model" {
		t.Fatalf("SetClient should update Model, got %s", c.Model)
	}
}

func TestSetClientDefaultTimeout(t *testing.T) {
	c := New()
	c.SetClient(Client{Provider: ProviderCustom, APIKey: "k"})
	if c.Timeout != 30*time.Second {
		t.Fatalf("SetClient should default timeout to 30s when 0, got %v", c.Timeout)
	}
}

// --- CallWithMessages ---

func TestCallWithMessages_MissingAPIKey(t *testing.T) {
	c := New()
	c.APIKey = ""
	_, err := c.CallWithMessages("sys", "user")
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Fatalf("error should mention API key, got: %s", err.Error())
	}
}

func TestCallWithMessages_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("hello world")))
	}))
	defer srv.Close()

	c := &Client{
		Provider:   ProviderCustom,
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		Model:      "test-model",
		Timeout:    5 * time.Second,
		UseFullURL: true,
	}
	result, err := c.CallWithMessages("system prompt", "user prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
}

func TestCallWithMessages_URLConstruction(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("ok")))
	}))
	defer srv.Close()

	// UseFullURL=false → appends /chat/completions
	c := &Client{APIKey: "k", BaseURL: srv.URL + "/v1", Model: "m", Timeout: 5 * time.Second, UseFullURL: false}
	_, err := c.CallWithMessages("s", "u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/v1/chat/completions" {
		t.Fatalf("expected /v1/chat/completions, got %s", capturedPath)
	}

	// UseFullURL=true → uses BaseURL as-is
	c.BaseURL = srv.URL + "/custom/endpoint"
	c.UseFullURL = true
	_, err = c.CallWithMessages("s", "u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/custom/endpoint" {
		t.Fatalf("expected /custom/endpoint, got %s", capturedPath)
	}
}

func TestCallWithMessages_RequestPayload(t *testing.T) {
	var payload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("ok")))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "test-model", Timeout: 5 * time.Second, UseFullURL: true}
	_, _ = c.CallWithMessages("sys prompt", "user prompt")

	if payload["model"] != "test-model" {
		t.Fatalf("expected model=test-model, got %v", payload["model"])
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %v", payload["messages"])
	}
}

func TestCallWithMessages_NoSystemPrompt(t *testing.T) {
	var payload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("ok")))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, _ = c.CallWithMessages("", "user only")

	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("expected 1 message (user only) when system prompt empty, got %d", len(messages))
	}
}

func TestCallWithMessages_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error should mention empty response, got: %s", err.Error())
	}
}

func TestCallWithMessages_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not json at all`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestCallWithMessages_MissingChoicesField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-123","object":"chat.completion"}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error for missing choices field")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error should mention empty response, got: %s", err.Error())
	}
}

func TestCallWithMessages_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error should contain status code 400, got: %s", err.Error())
	}
}

func TestCallWithMessages_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("too late")))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 100 * time.Millisecond, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Retry logic ---

func TestCallWithMessages_RetriesOnTransientError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"service unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("recovered")))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	result, err := c.CallWithMessages("s", "u")
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if result != "recovered" {
		t.Fatalf("expected 'recovered', got %q", result)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestCallWithMessages_NoRetryOn400(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if attempts.Load() != 1 {
		t.Fatalf("should not retry on 400, got %d attempts", attempts.Load())
	}
}

func TestCallWithMessages_RetryOn429(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validResponseBody("ok")))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	result, err := c.CallWithMessages("s", "u")
	if err != nil {
		t.Fatalf("expected retry success after 429, got: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected 'ok', got %q", result)
	}
}

func TestCallWithMessages_RetryExhausted(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`service unavailable`))
	}))
	defer srv.Close()

	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, UseFullURL: true}
	_, err := c.CallWithMessages("s", "u")
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "3 retries") {
		t.Fatalf("expected retry exhaustion message, got: %s", err.Error())
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

// --- isRetryableError ---

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		err       string
		retryable bool
	}{
		{"EOF", true},
		{"read tcp: connection reset by peer", true},
		{"dial tcp: connection refused", true},
		{"context deadline exceeded (Client.Timeout exceeded)", true},
		{"temporary failure in name resolution", true},
		{"no such host", true},
		{"AI API request failed (status 429): rate limited", true},
		{"AI API request failed (status 500): internal server error", true},
		{"AI API request failed (status 502): bad gateway", true},
		{"AI API request failed (status 503): service unavailable", true},
		{"AI API request failed (status 504): gateway timeout", true},
		{"AI API request failed (status 400): bad request", false},
		{"AI API request failed (status 401): unauthorized", false},
		{"AI API request failed (status 403): forbidden", false},
		{"AI API request failed (status 404): not found", false},
		{"failed to parse AI API response JSON: invalid character", false},
		{"empty response from AI model: choices array is empty", false},
	}
	for _, tc := range cases {
		err := fmt.Errorf("%s", tc.err)
		got := isRetryableError(err)
		if got != tc.retryable {
			t.Errorf("isRetryableError(%q) = %v, want %v", tc.err, got, tc.retryable)
		}
	}
}
