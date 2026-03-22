package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider mapping AI model providers
type Provider string

const (
	ProviderDeepSeek Provider = "deepseek"
	ProviderQwen     Provider = "qwen"
	ProviderCustom   Provider = "custom"
)

// Client wraps AI API configuration logic
type Client struct {
	Provider   Provider
	APIKey     string
	SecretKey  string // Required for AliYun/Qwen
	BaseURL    string
	Model      string
	Timeout    time.Duration
	UseFullURL bool // Whether to use raw full URL directly (skips appending /chat/completions)
}

func New() *Client {
	// Default configuration
	var defaultClient = Client{
		Provider: ProviderDeepSeek,
		BaseURL:  "https://api.deepseek.com/v1",
		Model:    "deepseek-chat",
		Timeout:  120 * time.Second, // Increase to 120s because AI must process extensive system data
	}
	return &defaultClient
}

// SetDeepSeekAPIKey binds keys for deepseek-chat
func (cfg *Client) SetDeepSeekAPIKey(apiKey string) {
	cfg.Provider = ProviderDeepSeek
	cfg.APIKey = apiKey
	cfg.BaseURL = "https://api.deepseek.com/v1"
	cfg.Model = "deepseek-chat"
}

// SetQwenAPIKey binds dual parameter keys for Qwen API
func (cfg *Client) SetQwenAPIKey(apiKey, secretKey string) {
	cfg.Provider = ProviderQwen
	cfg.APIKey = apiKey
	cfg.SecretKey = secretKey
	cfg.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	cfg.Model = "qwen-plus" // Optional parameters: qwen-turbo, qwen-plus, qwen-max
}

// SetCustomAPI binds general generic OpenAI compatible parameters
func (cfg *Client) SetCustomAPI(apiURL, apiKey, modelName string) {
	cfg.Provider = ProviderCustom
	cfg.APIKey = apiKey

	// If URL ends with #, use it as-is (skip appending /chat/completions)
	if strings.HasSuffix(apiURL, "#") {
		cfg.BaseURL = strings.TrimSuffix(apiURL, "#")
		cfg.UseFullURL = true
	} else {
		cfg.BaseURL = apiURL
		cfg.UseFullURL = false
	}

	cfg.Model = modelName
	cfg.Timeout = 120 * time.Second
}

// SetClient replaces all fields on the receiver with values from the given Client.
func (cfg *Client) SetClient(src Client) {
	if src.Timeout == 0 {
		src.Timeout = 30 * time.Second
	}
	*cfg = src
}

// CallWithMessages invokes model executions using structured message streams (Recommended)
func (cfg *Client) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	if cfg.APIKey == "" {
		return "", fmt.Errorf("AI API key not configured: call SetDeepSeekAPIKey(), SetQwenAPIKey(), or SetCustomAPI() before use")
	}

	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			fmt.Printf("  AI API call failed, retrying (%d/%d)...\n", attempt, maxRetries)
		}

		result, err := cfg.callOnce(systemPrompt, userPrompt)
		if err == nil {
			if attempt > 1 {
				fmt.Printf(" AI API retry succeeded\n")
			}
			return result, nil
		}

		lastErr = err
		// Abort on non-network or persistent exceptions
		if !isRetryableError(err) {
			return "", err
		}

		if attempt < maxRetries {
			waitTime := time.Duration(attempt) * 2 * time.Second
			fmt.Printf(" Waiting %v before retry...\n", waitTime)
			time.Sleep(waitTime)
		}
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// callOnce performs a single AI API request without retry.
func (cfg *Client) callOnce(systemPrompt, userPrompt string) (string, error) {
	messages := []map[string]string{}

	if systemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": systemPrompt,
		})
	}

	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userPrompt,
	})

	requestBody := map[string]interface{}{
		"model":       cfg.Model,
		"messages":    messages,
		"temperature": 0.5,
		"max_tokens":  2000,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal AI request payload: %w", err)
	}

	var url string
	if cfg.UseFullURL {
		url = cfg.BaseURL
	} else {
		url = fmt.Sprintf("%s/chat/completions", cfg.BaseURL)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create AI API request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))

	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read AI API response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI API request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse AI API response JSON: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from AI model: choices array is empty")
	}

	return result.Choices[0].Message.Content, nil
}

// isRetryableError returns true for transient errors that justify a retry.
func isRetryableError(err error) bool {
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"eof",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"no such host",
		"status 429",
		"status 500",
		"status 502",
		"status 503",
		"status 504",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
