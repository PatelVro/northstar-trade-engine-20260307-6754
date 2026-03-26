package broker

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IBKRClient is the low-level HTTP client for interacting with the IBKR Client Portal.
type IBKRClient struct {
	BaseURL                 string
	AccountID               string
	SessionCookie           string
	HTTPClient              *http.Client
	PortfolioRequestTimeout time.Duration
	PortfolioRetryAttempts  int
	PortfolioWarmTTL        time.Duration

	conIDCache  map[string]int
	cacheMutex  sync.RWMutex
	logMu       sync.Mutex
	portfolioMu sync.Mutex

	AuthStatus bool
	mu         sync.RWMutex // Protect AuthStatus
	lastLogKey string
	lastLogAt  time.Time

	portfolioWarmAccount string
	portfolioWarmAt      time.Time
}

type ibkrSecdefSection struct {
	SecType  string `json:"secType"`
	Exchange string `json:"exchange"`
}

const (
	defaultPortfolioRequestTimeout = 5 * time.Second
	defaultPortfolioRetryAttempts  = 3
	defaultPortfolioWarmTTL        = 15 * time.Second
)

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
		BaseURL:                 strings.TrimSuffix(baseURL, "/"),
		AccountID:               accountID,
		SessionCookie:           sessionCookie,
		PortfolioRequestTimeout: defaultPortfolioRequestTimeout,
		PortfolioRetryAttempts:  defaultPortfolioRetryAttempts,
		PortfolioWarmTTL:        defaultPortfolioWarmTTL,
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
	var bodyBytes []byte
	var err error
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		_ = req.Body.Close()
	}

	maxRetries := 2
	var resp *http.Response

	for attempt := 0; attempt <= maxRetries; attempt++ {
		attemptReq := req.Clone(req.Context())
		attemptReq.Header = req.Header.Clone()
		attemptReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		attemptReq.Header.Set("Accept", "application/json")
		if cookie := c.getSessionCookie(); cookie != "" {
			attemptReq.Header.Set("Cookie", cookie)
		}
		if bodyBytes != nil {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			attemptReq.ContentLength = int64(len(bodyBytes))
			attemptReq.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBytes)), nil
			}
		}

		// Lightweight pacing protection before every outbound call.
		time.Sleep(50 * time.Millisecond)
		resp, err = c.HTTPClient.Do(attemptReq)
		if err == nil {
			for _, cookie := range resp.Cookies() {
				if cookie.Name == "x-sess-uuid" && cookie.Value != "" {
					c.setSessionCookie(fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
				}
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				// 429 Pacing violation
				c.logRateLimited("ibkr_429:"+req.URL.Path, " IBKR: rate limit hit on %s, backing off", req.URL.Path)
				if attempt >= maxRetries {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					return nil, NewIBKRHTTPError(req.Method, req.URL.Path, resp.StatusCode, string(body))
				}
				resp.Body.Close()
				time.Sleep(1 * time.Second)
				continue
			}

			if resp.StatusCode >= http.StatusInternalServerError {
				if attempt >= maxRetries {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					return nil, NewIBKRHTTPError(req.Method, req.URL.Path, resp.StatusCode, string(body))
				}
				c.logRateLimited(
					fmt.Sprintf("ibkr_http_%d:%s", resp.StatusCode, req.URL.Path),
					" IBKR: gateway HTTP %d on %s, retrying...",
					resp.StatusCode,
					req.URL.Path,
				)
				resp.Body.Close()
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}

			if resp.StatusCode == 401 {
				c.logRateLimited("ibkr_401:"+req.URL.Path, " IBKR session lost (401 on %s), re-running handshake...", req.URL.Path)
				resp.Body.Close()

				c.checkAuthStatus()

				if c.IsAuthenticated() && attempt < maxRetries {
					c.logRateLimited("ibkr_401_recovered:"+req.URL.Path, " IBKR session revived, retrying %s", req.URL.Path)
					time.Sleep(1 * time.Second)
					continue
				}

				c.setAuthStatus(false)
				return nil, NewIBKRHTTPError(req.Method, req.URL.Path, http.StatusUnauthorized, "unauthorized")
			}

			return resp, nil
		}

		c.logRateLimited(
			fmt.Sprintf("ibkr_transport:%s:%s", req.Method, req.URL.Path),
			" IBKR: request failed on %s (attempt %d/%d): %v",
			req.URL.Path,
			attempt+1,
			maxRetries+1,
			err,
		)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}

	return nil, NewIBKRTransportError(req.Method, req.URL.Path, fmt.Errorf("request failed after retries: %w", err))
}

func (c *IBKRClient) doPreflight(method, endpoint string) ([]byte, int, error) {
	return c.doPreflightWithTimeout(method, endpoint, 0)
}

func (c *IBKRClient) doPreflightWithTimeout(method, endpoint string, timeout time.Duration) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.BaseURL+endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	if cookie := c.getSessionCookie(); cookie != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, NewIBKRTransportError(method, endpoint, err)
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

// DoPreflight exposes the same authenticated preflight request path used by
// readiness checks so higher-level runtime bootstrap code can reuse the exact
// same session and cookie handling behavior.
func (c *IBKRClient) DoPreflight(method, endpoint string) ([]byte, int, error) {
	return c.doPreflight(method, endpoint)
}

func (c *IBKRClient) logRateLimited(key, format string, args ...interface{}) {
	if key == "" {
		key = format
	}

	c.logMu.Lock()
	defer c.logMu.Unlock()

	now := time.Now()
	if c.lastLogKey == key && now.Sub(c.lastLogAt) < 15*time.Second {
		return
	}
	c.lastLogKey = key
	c.lastLogAt = now
	log.Printf(format, args...)
}

func (c *IBKRClient) CheckSessionReadiness(accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		accountID = strings.TrimSpace(c.AccountID)
	}
	if accountID == "" {
		return fmt.Errorf("missing IBKR account ID")
	}

	bAuth, statusAuth, err := c.doPreflight("GET", "/iserver/auth/status")
	if err != nil {
		c.setAuthStatus(false)
		return fmt.Errorf("auth status request failed: %w", err)
	}
	if statusAuth != http.StatusOK {
		c.setAuthStatus(false)
		return NewIBKRHTTPError("GET", "/iserver/auth/status", statusAuth, string(bAuth))
	}

	var authResp struct {
		Authenticated bool `json:"authenticated"`
		Connected     bool `json:"connected"`
	}
	if err := json.Unmarshal(bAuth, &authResp); err != nil {
		c.setAuthStatus(false)
		return fmt.Errorf("auth status decode failed: %w", err)
	}
	if !authResp.Authenticated || !authResp.Connected {
		c.setAuthStatus(false)
		return NewIBKRHTTPError("GET", "/iserver/auth/status", http.StatusUnauthorized, fmt.Sprintf("auth status not ready (authenticated=%t connected=%t)", authResp.Authenticated, authResp.Connected))
	}

	if body, statusAccounts, err := c.doPreflight("GET", "/iserver/accounts"); err != nil {
		c.setAuthStatus(false)
		return fmt.Errorf("iserver/accounts failed: %w", err)
	} else if statusAccounts != http.StatusOK {
		c.setAuthStatus(false)
		return NewIBKRHTTPError("GET", "/iserver/accounts", statusAccounts, string(body))
	}

	if body, statusPortfolioAccounts, err := c.doPreflight("GET", "/portfolio/accounts"); err != nil {
		c.setAuthStatus(false)
		return fmt.Errorf("portfolio/accounts failed: %w", err)
	} else if statusPortfolioAccounts != http.StatusOK {
		c.setAuthStatus(false)
		return NewIBKRHTTPError("GET", "/portfolio/accounts", statusPortfolioAccounts, string(body))
	}

	c.setAuthStatus(true)
	return nil
}

func (c *IBKRClient) FetchPortfolioEndpoint(accountID, suffix string) ([]byte, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		accountID = strings.TrimSpace(c.AccountID)
	}
	if accountID == "" {
		return nil, fmt.Errorf("missing IBKR account ID")
	}

	endpoint := fmt.Sprintf("/portfolio/%s/%s", accountID, strings.TrimPrefix(strings.TrimSpace(suffix), "/"))
	var lastErr error
	attempts := c.PortfolioRetryAttempts
	if attempts <= 0 {
		attempts = defaultPortfolioRetryAttempts
	}
	timeout := c.PortfolioRequestTimeout
	if timeout <= 0 {
		timeout = defaultPortfolioRequestTimeout
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := c.ensurePortfolioSession(accountID, attempt > 1); err != nil {
			lastErr = err
		} else {
			body, statusCode, err := c.doPreflightWithTimeout("GET", endpoint, timeout)
			if err == nil && statusCode == http.StatusOK {
				return body, nil
			}
			if err != nil {
				lastErr = fmt.Errorf("%s failed: %w", endpoint, err)
			} else {
				lastErr = NewIBKRHTTPError("GET", endpoint, statusCode, string(body))
			}
			if !IsRetryableIBKRError(lastErr) {
				return nil, lastErr
			}
		}

		if attempt < attempts {
			delay := time.Duration(attempt) * 400 * time.Millisecond
			c.logRateLimited(
				fmt.Sprintf("ibkr_portfolio_retry:%s", endpoint),
				" IBKR: retrying %s after transient portfolio bootstrap failure (%v)",
				endpoint,
				lastErr,
			)
			time.Sleep(delay)
		}
	}
	return nil, lastErr
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

	if err := c.CheckSessionReadiness(accountID); err != nil {
		return err
	}

	if _, err := c.FetchPortfolioEndpoint(accountID, "meta"); err != nil {
		return err
	}

	if _, err := c.FetchPortfolioEndpoint(accountID, "summary"); err != nil {
		return err
	}

	if _, err := c.FetchPortfolioEndpoint(accountID, "positions"); err != nil {
		return err
	}

	if body, statusOrders, err := c.doPreflight("GET", "/iserver/account/orders"); err != nil {
		return fmt.Errorf("iserver/account/orders failed: %w", err)
	} else if statusOrders != http.StatusOK {
		return NewIBKRHTTPError("GET", "/iserver/account/orders", statusOrders, string(body))
	}

	return nil
}

func (c *IBKRClient) ensurePortfolioSession(accountID string, force bool) error {
	c.portfolioMu.Lock()
	defer c.portfolioMu.Unlock()

	warmTTL := c.PortfolioWarmTTL
	if warmTTL <= 0 {
		warmTTL = defaultPortfolioWarmTTL
	}
	timeout := c.PortfolioRequestTimeout
	if timeout <= 0 {
		timeout = defaultPortfolioRequestTimeout
	}

	if !force &&
		accountID == c.portfolioWarmAccount &&
		!c.portfolioWarmAt.IsZero() &&
		time.Since(c.portfolioWarmAt) < warmTTL {
		return nil
	}

	mandatory := []string{
		"/portfolio/accounts",
	}
	for _, endpoint := range mandatory {
		body, statusCode, err := c.doPreflightWithTimeout("GET", endpoint, timeout)
		if err != nil {
			return fmt.Errorf("%s failed: %w", endpoint, err)
		}
		if statusCode != http.StatusOK {
			return NewIBKRHTTPError("GET", endpoint, statusCode, string(body))
		}
	}

	optionalWarmups := []string{
		"/portfolio/subaccounts",
		"/portfolio/subaccounts2?page=0",
	}
	for _, endpoint := range optionalWarmups {
		body, statusCode, err := c.doPreflightWithTimeout("GET", endpoint, timeout)
		switch {
		case err != nil:
			c.logRateLimited(
				"ibkr_portfolio_warm_optional:"+endpoint,
				" IBKR: optional portfolio warm-up %s failed: %v",
				endpoint,
				err,
			)
		case statusCode == http.StatusOK:
			continue
		default:
			c.logRateLimited(
				fmt.Sprintf("ibkr_portfolio_warm_optional:%s:%d", endpoint, statusCode),
				" IBKR: optional portfolio warm-up %s returned HTTP %d: %s",
				endpoint,
				statusCode,
				strings.TrimSpace(string(body)),
			)
		}
	}

	c.portfolioWarmAccount = accountID
	c.portfolioWarmAt = time.Now()
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
		wasAuthenticated := c.IsAuthenticated()
		if wasAuthenticated {
			log.Printf(" IBKR: session LOST — authenticated=false (status %d). Re-login at https://localhost:5002 to resume.", statusAuth)
		}
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

	wasAuthenticated := c.IsAuthenticated()
	c.setAuthStatus(true)
	if !wasAuthenticated {
		log.Printf(" IBKR: session RECOVERED — authenticated=true, trading can resume")
	}
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
		Conid         interface{}         `json:"conid"`
		Companyname   string              `json:"companyName"`
		CompanyHeader string              `json:"companyHeader"`
		Symbol        string              `json:"symbol"`
		Description   string              `json:"description"`
		Opt           string              `json:"opt"`
		Exchange      string              `json:"exchange"`
		Sections      []ibkrSecdefSection `json:"sections"`
	}

	if err := json.Unmarshal(bodyBytes, &searchResults); err != nil {
		return 0, fmt.Errorf("failed to decode secdef response: %w", err)
	}

	if len(searchResults) == 0 {
		return 0, fmt.Errorf("no contract found for symbol %s", cleanSymbol)
	}

	// Find best equity contract. secdef/search can return non-equity instruments sharing the same ticker.
	var bestConID int
	bestScore := -1
	bestHeader := ""
	for _, res := range searchResults {
		conid, ok := parseConID(res.Conid)
		if !ok || conid <= 0 {
			continue
		}

		score := scoreSecdefEquityCandidate(cleanSymbol, res.Symbol, res.Description, res.CompanyHeader, res.Exchange, res.Sections)
		if score > bestScore {
			bestScore = score
			bestConID = conid
			bestHeader = res.CompanyHeader
		}
	}

	if bestConID == 0 {
		// Last-resort fallback: first parsable contract (preserves backward compatibility for edge cases).
		for _, res := range searchResults {
			conid, ok := parseConID(res.Conid)
			if ok && conid > 0 {
				bestConID = conid
				bestHeader = res.CompanyHeader
				break
			}
		}
	}
	if bestConID == 0 {
		return 0, fmt.Errorf("no parsable conid in secdef response for symbol %s", cleanSymbol)
	}

	if len(searchResults) > 1 {
		log.Printf(" IBKR: selected conid %d for %s (%s)", bestConID, cleanSymbol, bestHeader)
	}

	// Update cache
	c.cacheMutex.Lock()
	c.conIDCache[cleanSymbol] = bestConID
	c.cacheMutex.Unlock()

	log.Printf("IBKR: Resolved %s to conid %d", cleanSymbol, bestConID)
	return bestConID, nil
}

func parseConID(v interface{}) (int, bool) {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return 0, false
		}
		return int(t), true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			if f > 0 {
				return int(f), true
			}
		}
		return 0, false
	case int:
		if t <= 0 {
			return 0, false
		}
		return t, true
	case int64:
		if t <= 0 {
			return 0, false
		}
		return int(t), true
	default:
		return 0, false
	}
}

func scoreSecdefEquityCandidate(cleanSymbol, symbol, description, companyHeader, exchange string, sections []ibkrSecdefSection) int {
	score := 0
	if strings.EqualFold(strings.TrimSpace(symbol), cleanSymbol) {
		score += 15
	}

	hasSTK := false
	exchangeText := strings.ToUpper(description + " " + companyHeader + " " + exchange)
	for _, sec := range sections {
		secType := strings.ToUpper(strings.TrimSpace(sec.SecType))
		if secType == "STK" {
			hasSTK = true
		}
		exchangeText += " " + strings.ToUpper(sec.Exchange)
	}
	if hasSTK {
		score += 100
	} else {
		// Non-stock instruments using the same ticker (futures, indices, etc.) should lose strongly.
		score -= 100
	}

	if strings.Contains(exchangeText, "SMART") {
		score += 30
	}
	if strings.Contains(exchangeText, "NYSE") || strings.Contains(exchangeText, "NASDAQ") {
		score += 25
	}
	if strings.Contains(exchangeText, "TSE") || strings.Contains(exchangeText, "TSX") {
		score += 20
	}
	if strings.Contains(exchangeText, "AMEX") || strings.Contains(exchangeText, "ARCA") || strings.Contains(exchangeText, "BATS") {
		score += 10
	}

	return score
}
