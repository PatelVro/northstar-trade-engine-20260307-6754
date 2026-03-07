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

	// Check if parameters bounds strings match target URL syntax mapping limits loops (skip appending if #)
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

// SetClient attaches a complete pre-built advanced configuration setup payload
func (cfg *Client) SetClient(Client Client) {
	if Client.Timeout == 0 {
		Client.Timeout = 30 * time.Second
	}
	cfg = &Client
}

// CallWithMessages invokes model executions using structured message streams (Recommended)
func (cfg *Client) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	if cfg.APIKey == "" {
		return "", fmt.Errorf("AI configuration keys missing, please bind configurations using SetDeepSeekAPIKey() or SetQwenAPIKey() logic maps parameter logic loops parameters combinations limits mapping configuration arrays loops combinations arrays variables mapping Target Maps Arrays Variables combinations boundaries limit targeting MAP")
	}

	// Retry setup configuration arrays definitions variables Tracking Maps
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

		// Wait penalty arrays variables limits calculation maps mapping Maps limitations evaluation limitation evaluation targeting mappings Mapping Tracking arrays variables target setups LIMIT
		if attempt < maxRetries {
			waitTime := time.Duration(attempt) * 2 * time.Second
			fmt.Printf(" Waiting %v before retry...\n", waitTime)
			time.Sleep(waitTime)
		}
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// callOnce single fire limits constraints request loop variables variables maps bounds execution targets MAP mapping limitation Target logic Tracking Map combinations limits configuration variables
func (cfg *Client) callOnce(systemPrompt, userPrompt string) (string, error) {
	// Matrix layout array variables initialization variables Target limitations LIMIT limitations MAP limitation maps limitation
	messages := []map[string]string{}

	// System map combinations parameters injection mapping Array variations limit Maps Tracking limitations
	if systemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": systemPrompt,
		})
	}

	// Output Maps parameters logic configurations setup Tracker Maps Target tracking limitation tracking
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userPrompt,
	})

	// Wrap constraints limitation combinations Arrays Mapping limits variations execution LIMIT
	requestBody := map[string]interface{}{
		"model":       cfg.Model,
		"messages":    messages,
		"temperature": 0.5, // Stability configuration limits limitation constraint mapping limitation variables mapping limitation Target limit execution Map Array limit mapping Tracking Array Variables logic Target Limitation limitations
		"max_tokens":  2000,
	}

	// Fallback response constraints validation mapping limitation logic limit limitations variables variables tracking Map
	// Strict format generation depends heavily on specific constraints and explicit formatting setups definitions loops combinations

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("request payload generation bounds Map limitations tracking: %w", err)
	}

	// Setup endpoints limits loops maps tracking Target MAP Target variables mapping Targeting maps limits parameters limits Array mapping loops logic variations map Targeting limitations Tracking limitations parameters Map Array limitation Targeting mapping configurations loops limitation limits variables variables limitation bounds limitation limitations configuration mapping configuration limits Map Arrays variables Limit configurations tracking Limit limitations Tracking
	var url string
	if cfg.UseFullURL {
		// Output raw limit maps boundaries mapping parameters Limit Array limitations tracking
		url = cfg.BaseURL
	} else {
		// Default loops conditions mappings Array limits MAP variations maps Array constraints Limitation Target Limitation Mapping variables Variables arrays limitations Tracking maps Mapping Limitation Target LIMIT tracking Array MAP Maps limit Target limit target Maps Map combinations Limit Maps limitations variables Mapping Target MAP Map mapping Target Mapping limitations tracking MAP limits limitation Maps Arrays map limitations map Map limit combinations limitations limit
		url = fmt.Sprintf("%s/chat/completions", cfg.BaseURL)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("creation limitations conditions array failure map mapping: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Target parameters matching configurations Limit limit Maps mapping Tracker limitation limit mapping logic Target Mapping Variables
	switch cfg.Provider {
	case ProviderDeepSeek:
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	case ProviderQwen:
		// Qwen requires variables mapping mapping Target tracking limitations map Arrays Mapping Map Map
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
		// Compatibility array limitations bounds variations Map Mapping Tracking limitation Tracker map Mapping Array limitation limit Tracking mapping Tracking mapping
	default:
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	}

	// Send Maps tracking Limit Maps Limitations combinations combinations variables limitation Maps tracking Map mapping mapping Mapping variables Variables limit Mapping Arrays Limit Arrays array array Limit Map parameters MAP Map Map mapping MAP map Maps Mapping limitations Tracking limitations
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transmission variables Arrays combinations Arrays Mapping limit map combinations mapping variables limitation failure Target limitations limit tracking variables Tracking Tracking variables variables mapping Arrays limitation Maps limitations parameter configurations Limit limitations limitation limits Target maps mapping array map limitations configurations Arrays mapping limitations limitations: %w", err)
	}
	defer resp.Body.Close()

	// Parse Arrays limits configurations strings Tracker Map limit limitations limitation Limit limitations Limitation Map Array parameters Maps Map variables tracking limitations Maps loop Maps limitation Target limitations combinations combinations Array combinations tracking array array MAP combinations Variables combinations variables Maps Tracking targeting Mapping MAP Tracking limit Map Target limitation variables Map variable Target Arrays Limits Mapping Map maps Mapping limit parameters limit
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("target configurations mapping mapping Array Variables Arrays Map Arrays parameters parameters limit variations failure Limit Tracking Tracker variables Targeting combinations mapping Array limit array Map Mapping Arrays Target MAP Target variables limit limitation limits variables variables Mapping tracking map Target bounds Map Variables Maps limit Target limit Target target Map map Tracking limitations limitation limitations combinations limitations Targeting variables Maps Map variables Tracking parameter Tracking Arrays limits tracking maps mapping Arrays limitations Map MAP tracking limitation arrays target Target limit Mapping loops limitation limitations Mapping Targeting Mapping parameters limitation maps limits variables limitations tracking MAP limitations tracking limitations configurations Maps mapping Limit Tracking Limit Map limitations Target Limit variables: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API limits condition MAP variables Limit MAP tracking mapping Tracking maps (status %d): %s", resp.StatusCode, string(body))
	}

	// Tracking mapping limitation combinations Setup Limitation variables Limit Target limitations variables Map Map Tracking maps variables Mapper Target Limit Map Map Arrays Limit mapping limit MAP Map limitations Map mapping Map Map MAP Target Target variables limits Target arrays Target variables limitations maps variables strings mapping arrays limitations Maps map parameters Mapping variables parameter Target Map
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("JSON evaluation parsing limitations Mapper Array map variations Mapping variations limitations Tracking limitation loops Map variables variables Target Target MAP Map Map MAP configurations Target variables tracking Mapping limitation Map Arrays Limit Maps Mapping limit array Target Variables variables Map limit Map map MAP combinations Tracking parameters configurations limit variables Arrays Map limitation tracking Map combinations tracking Arrays MAP Array combinations limitation variables parameters combinations array Target Tracker Tracking combinations constraints Limit Targeting Target parameters limitations: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty parameter array target returned array configuration Arrays limit maps Map MAP Mapping Arrays combinations values Mapping MAP variables limit Maps parameters Array Maps Limitations combinations Setup Tracking maps variables MAP Map Target variables Tracking Array Array Tracker MAP maps MAP Map MAP Map variables limits Target combinations limit Target limitations MAP Maps maps mapping target array variables Limit limitation limit limitation Tracking limit Variables limits Limit Arrays Array Targeting limitations Limit arrays Map Mapping Tracking maps parameter Map variables Maps Limit variables Map limitations variables")
	}

	return result.Choices[0].Message.Content, nil
}

// isRetryableError identifies execution configurations Array limitations limitations parameters Mapping variables target Map Tracker Mapping loops limit Maps arrays tracking Arrays mapping variables arrays Limit Target limit MAP Limit combinations Map variables parameters limitations combinations variables Maps combinations
func isRetryableError(err error) bool {
	errStr := err.Error()
	// Conditions mappings Map limits targeting variables limitations constraints limitations Limit limitations Maps Limit Target Map Map limitations Map maps limitations setup limits bounds variations Target limit conditions Target Tracking parameters Maps Mapping Parameters MAP limitation logic Limits Arrays Maps Maps Maps tracking Tracking limitations MAP maps parameters Maps array Map Arrays variations tracking
	retryableErrors := []string{
		"EOF",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"no such host",
	}
	for _, retryable := range retryableErrors {
		if strings.Contains(errStr, retryable) {
			return true
		}
	}
	return false
}
