package broker

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"
)

// IBKRClient is the low-level HTTP client for interacting with the IBKR Client Portal.
type IBKRClient struct {
	BaseURL       string
	AccountID     string
	SessionCookie string
	HTTPClient    *http.Client

	conIDCache map[string]int
	cacheMutex sync.RWMutex

	AuthStatus bool
	mu         sync.RWMutex // Protect AuthStatus
}

// NewIBKRClient initializes a new core client.
func NewIBKRClient(baseURL, accountID, sessionCookie string) *IBKRClient {
	if baseURL == "" {
		baseURL = "https://127.0.0.1:5002/v1/api"
	}
	log.Printf(" IBKR Client Base URL: %s", baseURL)

	// IBKR Gateway requires cookie management to maintain the authenticated session
	// The cookie jar captures outbound responses to maintain any anonymous assignments
	// But our explicitly extracted session cookie will be forced as a raw Header directly in Do()
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatalf("Failed to initialize cookie jar: %v", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Allow local self-signed certs
	}

	client := &IBKRClient{
		BaseURL:       strings.TrimSuffix(baseURL, "/"),
		AccountID:     accountID,
		SessionCookie: sessionCookie,
		HTTPClient: &http.Client{
			Transport: tr,
			Timeout:   15 * time.Second,
			Jar:       jar,
		},
		conIDCache: make(map[string]int),
	}

	// Start background authentication monitor
	go client.monitorSession()

	return client
}

// Do executes an HTTP request with basic retry and pacing logic.
func (c *IBKRClient) Do(req *http.Request) (*http.Response, error) {
	// Standardize browser fingerprint to bypass Gateway reject blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")
	if cookie := c.getSessionCookie(); cookie != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", cookie)
	}
	// Pacing protection (simple 50ms delay, can be enhanced with token buckets if needed)
	time.Sleep(50 * time.Millisecond)

	maxRetries := 2
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = c.HTTPClient.Do(req)
		if err == nil {
			for _, cookie := range resp.Cookies() {
				if cookie.Name == "x-sess-uuid" && cookie.Value != "" {
					c.setSessionCookie(fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
				}
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				// 429 Pacing violation
				log.Printf(" IBKR: Rate limit hit. Backing off for 1 second...")
				resp.Body.Close()
				time.Sleep(1 * time.Second)
				continue
			}

			if resp.StatusCode == 401 {
				log.Printf(" IBKR session lost (401 on %s), re-running handshake...", req.URL.Path)
				resp.Body.Close()

				c.checkAuthStatus()

				if c.IsAuthenticated() && attempt < maxRetries {
					log.Printf(" IBKR session revived, retrying original request...")
					time.Sleep(1 * time.Second)
					continue
				}

				c.setAuthStatus(false)
				return nil, fmt.Errorf("status 401: unauthorized")
			}

			return resp, nil
		}

		log.Printf(" IBKR: Request failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}

	return nil, fmt.Errorf("request failed after retries: %w", err)
}

func (c *IBKRClient) doPreflight(method, endpoint string) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.BaseURL+endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	if cookie := c.getSessionCookie(); cookie != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "x-sess-uuid" && cookie.Value != "" {
			c.setSessionCookie(fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
		}
	}
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

// CheckLiveReadiness verifies account endpoints required for safe live execution.
func (c *IBKRClient) CheckLiveReadiness(accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		accountID = strings.TrimSpace(c.AccountID)
	}
	if accountID == "" {
		return fmt.Errorf("missing IBKR account ID")
	}

	bAuth, statusAuth, err := c.doPreflight("GET", "/iserver/auth/status")
	if err != nil {
		return fmt.Errorf("auth status request failed: %w", err)
	}
	if statusAuth != http.StatusOK {
		return fmt.Errorf("auth status HTTP %d", statusAuth)
	}
	var authResp struct {
		Authenticated bool `json:"authenticated"`
		Connected     bool `json:"connected"`
	}
	if err := json.Unmarshal(bAuth, &authResp); err != nil {
		return fmt.Errorf("auth status decode failed: %w", err)
	}
	if !authResp.Authenticated || !authResp.Connected {
		return fmt.Errorf("auth status not ready (authenticated=%t connected=%t)", authResp.Authenticated, authResp.Connected)
	}

	if _, statusAccounts, err := c.doPreflight("GET", "/iserver/accounts"); err != nil {
		return fmt.Errorf("iserver/accounts failed: %w", err)
	} else if statusAccounts != http.StatusOK {
		return fmt.Errorf("iserver/accounts HTTP %d", statusAccounts)
	}

	if _, statusPortfolioAccounts, err := c.doPreflight("GET", "/portfolio/accounts"); err != nil {
		return fmt.Errorf("portfolio/accounts failed: %w", err)
	} else if statusPortfolioAccounts != http.StatusOK {
		return fmt.Errorf("portfolio/accounts HTTP %d", statusPortfolioAccounts)
	}

	if _, statusMeta, err := c.doPreflight("GET", fmt.Sprintf("/portfolio/%s/meta", accountID)); err != nil {
		return fmt.Errorf("portfolio/%s/meta failed: %w", accountID, err)
	} else if statusMeta != http.StatusOK {
		return fmt.Errorf("portfolio/%s/meta HTTP %d", accountID, statusMeta)
	}

	if _, statusSummary, err := c.doPreflight("GET", fmt.Sprintf("/portfolio/%s/summary", accountID)); err != nil {
		return fmt.Errorf("portfolio/%s/summary failed: %w", accountID, err)
	} else if statusSummary != http.StatusOK {
		return fmt.Errorf("portfolio/%s/summary HTTP %d", accountID, statusSummary)
	}

	if _, statusPositions, err := c.doPreflight("GET", fmt.Sprintf("/portfolio/%s/positions", accountID)); err != nil {
		return fmt.Errorf("portfolio/%s/positions failed: %w", accountID, err)
	} else if statusPositions != http.StatusOK {
		return fmt.Errorf("portfolio/%s/positions HTTP %d", accountID, statusPositions)
	}

	if _, statusOrders, err := c.doPreflight("GET", "/iserver/account/orders"); err != nil {
		return fmt.Errorf("iserver/account/orders failed: %w", err)
	} else if statusOrders != http.StatusOK {
		return fmt.Errorf("iserver/account/orders HTTP %d", statusOrders)
	}

	return nil
}

func (c *IBKRClient) checkAuthStatus() {
	bAuth, statusAuth, err := c.doPreflight("GET", "/iserver/auth/status")
	if err != nil {
		c.setAuthStatus(false)
		return
	}
	var authResp struct {
		Authenticated bool `json:"authenticated"`
	}
	json.Unmarshal(bAuth, &authResp)

	if !authResp.Authenticated || statusAuth != 200 {
		log.Printf("Debug: IBKR auth status false/failed: %d", statusAuth)
		c.setAuthStatus(false)
		return
	}

	_, statusAccounts, err := c.doPreflight("GET", "/iserver/accounts")
	if err != nil {
		c.setAuthStatus(false)
		return
	}
	if statusAccounts != http.StatusOK {
		log.Printf("Debug: IBKR accounts check failed: %d", statusAccounts)
		c.setAuthStatus(false)
		return
	}

	bPort, statusPort, err := c.doPreflight("GET", "/portfolio/accounts")
	if err != nil {
		c.setAuthStatus(false)
		return
	}
	if statusPort == 200 {
		var accounts []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(bPort, &accounts); err == nil && len(accounts) > 0 {
			// Warm up an account-scoped endpoint; dynaccount is often rejected for demo accounts.
			_, _, _ = c.doPreflight("GET", fmt.Sprintf("/portfolio/%s/meta", accounts[0].ID))
		}
	}

	c.setAuthStatus(true)
}

// monitorSession continuously verifes the gateway auth status
func (c *IBKRClient) monitorSession() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Initial check
	c.checkAuthStatus()
	c.tickleSession()

	for range ticker.C {
		c.checkAuthStatus()
		c.tickleSession()
	}
}

// CancelAllOrders executes an emergency liquidation of pending orders natively.
func (c *IBKRClient) CancelAllOrders() {
	url := fmt.Sprintf("%s/iserver/account/%s/orders", c.BaseURL, c.AccountID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err == nil {
		log.Println(" IBKR: Transmitting Emergency OPEN ORDER Cancellation Payload...")
		resp, _ := c.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// tickleSession pings the gateway to artificially extend the auth timeout window.
func (c *IBKRClient) tickleSession() {
	url := fmt.Sprintf("%s/tickle", c.BaseURL)
	req, err := http.NewRequest("POST", url, nil)
	if err == nil {
		resp, _ := c.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}
}

func (c *IBKRClient) setAuthStatus(status bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AuthStatus = status
}

func (c *IBKRClient) setSessionCookie(cookie string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SessionCookie = strings.TrimSpace(cookie)
}

func (c *IBKRClient) getSessionCookie() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(c.SessionCookie)
}

func (c *IBKRClient) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AuthStatus
}

// ResolveContract resolves a symbol to a contract ID (conid) with caching
func (c *IBKRClient) ResolveContract(symbol string) (int, error) {
	cleanSymbol := strings.TrimSpace(strings.ToUpper(strings.TrimSuffix(strings.ToUpper(symbol), "USDT")))

	// Check cache
	c.cacheMutex.RLock()
	if conid, exists := c.conIDCache[cleanSymbol]; exists {
		c.cacheMutex.RUnlock()
		return conid, nil
	}
	c.cacheMutex.RUnlock()

	// We must resolve via the secdef endpoint
	// POST /iserver/secdef/search
	payload := map[string]interface{}{
		"symbol":  cleanSymbol,
		"name":    false, // search by symbol
		"secType": "STK",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s/iserver/secdef/search", c.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to search for contract %s: %w", cleanSymbol, err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("IBKR secdef search returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var searchResults []struct {
		Conid         int    `json:"conid"`
		Companyname   string `json:"companyName"`
		CompanyHeader string `json:"companyHeader"`
		Symbol        string `json:"symbol"`
		Description   string `json:"description"`
		Opt           string `json:"opt"`
		Exchange      string `json:"exchange"` // Should prefer SMART or NYSE/NASDAQ
	}

	if err := json.Unmarshal(bodyBytes, &searchResults); err != nil {
		return 0, fmt.Errorf("failed to decode secdef response: %w", err)
	}

	if len(searchResults) == 0 {
		return 0, fmt.Errorf("no contract found for symbol %s", cleanSymbol)
	}

	// Find best match: SMART routed on US exchange
	var bestConID int
	for _, res := range searchResults {
		// Prefer SMART routing for US Equities. If not, fallback to first STK.
		if res.Description == "STK" && (strings.Contains(res.CompanyHeader, "SMART") || strings.Contains(res.CompanyHeader, "US")) {
			bestConID = res.Conid
			break
		}
	}

	// Fallback to first if no perfect match
	if bestConID == 0 {
		bestConID = searchResults[0].Conid
		log.Printf(" IBKR: Ambiguous contract for %s. Using default conid %d (%s)", cleanSymbol, bestConID, searchResults[0].CompanyHeader)
	}

	// Update cache
	c.cacheMutex.Lock()
	c.conIDCache[cleanSymbol] = bestConID
	c.cacheMutex.Unlock()

	log.Printf("IBKR: Resolved %s to conid %d", cleanSymbol, bestConID)
	return bestConID, nil
}
