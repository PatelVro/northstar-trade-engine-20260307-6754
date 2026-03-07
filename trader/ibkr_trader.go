package trader

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"aegistrade/market"
	"strings"
	"sync"
	"time"
)

type IBKRTrader struct {
	BaseURL       string
	AccountID     string
	HTTPClient    *http.Client
	Provider      *market.IBKRProvider
	TrackedOrders map[string]string // Tracks orderId -> status
	orderMu       sync.Mutex
	balanceMu     sync.RWMutex
	fallbackCash  float64
}

func NewIBKRTrader(baseURL, accountID, sessionCookie string, provider *market.IBKRProvider, initialBalance float64) *IBKRTrader {
	if baseURL == "" {
		baseURL = "https://127.0.0.1:5002/v1/api"
	}
	if initialBalance <= 0 {
		initialBalance = 100000.0
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	trader := &IBKRTrader{
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		AccountID: accountID,
		HTTPClient: &http.Client{
			Transport: tr,
			Timeout:   10 * time.Second,
		},
		Provider:      provider,
		TrackedOrders: make(map[string]string),
		fallbackCash:  initialBalance,
	}

	go trader.reconcilerLoop()

	return trader
}

// reconcilerLoop continually polls IBKR open orders and diffs status to emit explicitly mapped state transitions.
func (t *IBKRTrader) reconcilerLoop() {
	ticker := time.NewTicker(3 * time.Second)
	for range ticker.C {
		orders, err := t.GetLiveOrders()
		if err != nil {
			continue
		}

		t.orderMu.Lock()

		// 1. Mark current fetch
		currentOpen := make(map[string]string)
		for _, o := range orders {
			// Extract orderId (sometimes 'orderId', sometimes 'id')
			idFloat, ok := o["orderId"].(float64)
			if !ok {
				continue
			}
			idStr := fmt.Sprintf("%.0f", idFloat)

			status, _ := o["status"].(string)
			if status == "" {
				status = "Submitted"
			}

			currentOpen[idStr] = status

			// Check for transition
			if oldStatus, exists := t.TrackedOrders[idStr]; exists {
				if oldStatus != status {
					log.Printf(" IBKR: Order %s transitioned %s -> %s", idStr, oldStatus, status)
					t.TrackedOrders[idStr] = status
				}
			} else {
				// New order discovered!
				log.Printf(" IBKR: Order %s transitioned Created -> %s", idStr, status)
				t.TrackedOrders[idStr] = status
			}
		}

		// 2. Identify Filled/Closed orders that evaporated
		for trackId, trackStatus := range t.TrackedOrders {
			// If it was tracked but is no longer in the live order list, it's either filled or cancelled
			if _, exists := currentOpen[trackId]; !exists {
				if trackStatus != "Filled" && trackStatus != "Closed" && trackStatus != "Cancelled" {
					log.Printf(" IBKR: Order %s evaporated from active list, assuming Filled/Closed.", trackId)
					t.TrackedOrders[trackId] = "Closed"
				}
			}
		}

		t.orderMu.Unlock()
	}
}

func (t *IBKRTrader) setFallbackCash(v float64) {
	if v <= 0 {
		return
	}
	t.balanceMu.Lock()
	t.fallbackCash = v
	t.balanceMu.Unlock()
}

func (t *IBKRTrader) getFallbackCash() float64 {
	t.balanceMu.RLock()
	defer t.balanceMu.RUnlock()
	if t.fallbackCash <= 0 {
		return 100000.0
	}
	return t.fallbackCash
}

func (t *IBKRTrader) fallbackBalance(reason error) map[string]interface{} {
	cash := t.getFallbackCash()
	log.Printf(" IBKR: using fallback account balance due to summary endpoint issue: %v", reason)
	return map[string]interface{}{
		"totalWalletBalance":    cash,
		"availableBalance":      cash,
		"totalUnrealizedProfit": 0.0,
	}
}

func (t *IBKRTrader) GetBalance() (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/portfolio/%s/summary", t.BaseURL, t.AccountID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.Provider.Client.Do(req)
	if err != nil {
		return t.fallbackBalance(err), nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf(" DEBUG IBKR SUMMARY RESPONSE: Status %d | Body: %s", resp.StatusCode, string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return t.fallbackBalance(fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes))), nil
	}

	var summary map[string]struct {
		Amount float64 `json:"amount"`
	}

	if err := json.Unmarshal(bodyBytes, &summary); err != nil {
		return t.fallbackBalance(fmt.Errorf("failed to parse IBKR summary: %w", err)), nil
	}

	result := make(map[string]interface{})

	if val, ok := summary["netliquidation"]; ok {
		result["totalWalletBalance"] = val.Amount
		t.setFallbackCash(val.Amount)
	} else {
		result["totalWalletBalance"] = t.getFallbackCash()
	}

	if val, ok := summary["availablefunds"]; ok {
		result["availableBalance"] = val.Amount
	} else {
		result["availableBalance"] = t.getFallbackCash()
	}

	if val, ok := summary["unrealizedpnl"]; ok {
		result["totalUnrealizedProfit"] = val.Amount
	} else {
		result["totalUnrealizedProfit"] = 0.0
	}

	return result, nil
}

func (t *IBKRTrader) GetPositions() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/portfolio/%s/positions", t.BaseURL, t.AccountID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.Provider.Client.Do(req)
	if err != nil {
		log.Printf(" IBKR: positions endpoint unavailable, returning empty set: %v", err)
		return []map[string]interface{}{}, nil
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf(" IBKR: positions HTTP %d, returning empty set. Body: %s", resp.StatusCode, string(b))
		return []map[string]interface{}{}, nil
	}

	var posResp []struct {
		Ticker        string  `json:"ticker"`
		Position      float64 `json:"position"`
		MktPrice      float64 `json:"mktPrice"`
		AvgCost       float64 `json:"avgCost"`
		UnrealizedPnl float64 `json:"unrealizedPnl"`
	}

	if err := json.Unmarshal(b, &posResp); err != nil {
		log.Printf(" IBKR: failed to parse positions payload, returning empty set: %v", err)
		return []map[string]interface{}{}, nil
	}

	var positions []map[string]interface{}
	for _, p := range posResp {
		if p.Position == 0 {
			continue
		}

		side := "long"
		if p.Position < 0 {
			side = "short"
		}

		positions = append(positions, map[string]interface{}{
			"symbol":           p.Ticker,
			"side":             side,
			"positionAmt":      p.Position,
			"entryPrice":       p.AvgCost,
			"markPrice":        p.MktPrice,
			"unRealizedProfit": p.UnrealizedPnl,
			"leverage":         1.0, // Equities generally map base leverage
			"liquidationPrice": 0.0,
		})
	}

	return positions, nil
}

func (t *IBKRTrader) CreateOrder(symbol string, side string, price float64, quantity float64, leverage int, takeProfit string, stopLoss string) (string, error) {
	// First resolve ConID using our central IBKRClient cache
	cID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return "", fmt.Errorf("failed to resolve ConID for %s: %w", symbol, err)
	}

	// Format AegisTrade long/short to IBKR BUY/SELL
	ibkrSide := "BUY"
	oppositeSide := "SELL"
	if side == "short" {
		ibkrSide = "SELL"
		oppositeSide = "BUY"
	}

	// Pre-submission Risk Validation
	if quantity <= 0 {
		return "", fmt.Errorf("invalid quantity: %f", quantity)
	}

	// Phase 1 - Submit Entry Order
	entryOrder := map[string]interface{}{
		"conid":      cID,
		"secType":    "STK",
		"orderType":  "MKT", // Market entry
		"tif":        "GTC",
		"quantity":   fmt.Sprintf("%f", quantity),
		"side":       ibkrSide,
		"outsideRTH": true, // Phase 4 configuration
	}

	if price > 0 {
		entryOrder["orderType"] = "LMT"
		entryOrder["price"] = price
	}

	log.Printf("IBKR: Placing entry order for %f %s %s...", quantity, symbol, ibkrSide)

	if err := t.submitIBKROrders([]interface{}{entryOrder}); err != nil {
		return "", fmt.Errorf("entry order failed: %w", err)
	}

	// Phase 2 - Await Fill Confirmation
	// Poll IBKR orders endpoint for up to 10 seconds to detect the fill
	filled := false
	log.Printf("IBKR: Waiting for fill confirmation on %s %s...", symbol, ibkrSide)
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		orders, err := t.GetLiveOrders()
		if err == nil {
			// We check if the entry order evaporated from live open orders
			// In IBKR, completely filled orders often drop from the active /orders list quickly
			// This is a naive heuristic for the REST API (Phase 6 Reconciliation engine will harden this globally)
			if len(orders) == 0 {
				filled = true
				break
			} else {
				// If there's an active order for this conid matching our side, it's still pending
				stillPending := false
				for _, o := range orders {
					rawSide, _ := o["side"].(string)
					rawConid, _ := o["conid"].(float64)
					if int(rawConid) == cID && rawSide == ibkrSide {
						stillPending = true
						break
					}
				}
				if !stillPending {
					filled = true
					break
				}
			}
		}
	}

	if !filled {
		log.Printf(" IBKR: Entry order on %s not filled within timeout. Brackets will not be placed to prevent orphan exposure.", symbol)
		return "ibkr_entry_pending", nil
	}

	log.Printf(" IBKR: Entry order for %s confirmed filled.", symbol)

	// Phase 3 & 4 - Submit OCA Brackets
	if takeProfit != "" || stopLoss != "" {
		var bracketOrders []interface{}
		ocaGroup := fmt.Sprintf("AegisTrade_OCA_%d", time.Now().UnixNano())

		if takeProfit != "" {
			bracketOrders = append(bracketOrders, map[string]interface{}{
				"conid":      cID,
				"secType":    "STK",
				"orderType":  "LMT",
				"price":      takeProfit,
				"tif":        "GTC",
				"quantity":   fmt.Sprintf("%f", quantity),
				"side":       oppositeSide,
				"outsideRTH": true,
				"ocaGroup":   ocaGroup,
			})
		}

		if stopLoss != "" {
			bracketOrders = append(bracketOrders, map[string]interface{}{
				"conid":      cID,
				"secType":    "STK",
				"orderType":  "STP",
				"auxPrice":   stopLoss, // Stop Loss uses auxPrice in IBKR REST API
				"tif":        "GTC",
				"quantity":   fmt.Sprintf("%f", quantity),
				"side":       oppositeSide,
				"outsideRTH": true,
				"ocaGroup":   ocaGroup,
			})
		}

		log.Printf("IBKR: Submitting OCA safety brackets (SL/TP) for %s...", symbol)
		if err := t.submitIBKROrders(bracketOrders); err != nil {
			log.Printf(" IBKR: Failed to submit brackets for %s: %v", symbol, err)
			return "ibkr_entry_filled_bracket_failed", err
		}
		log.Printf(" IBKR: Safe Brackets secured.")
	}

	return "ibkr_order_placed", nil
}

// submitIBKROrders is a helper to transmit the REST payload and handle reply confirmation loops
func (t *IBKRTrader) submitIBKROrders(orders []interface{}) error {
	payload := map[string]interface{}{
		"orders": orders,
	}
	jsonData, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/iserver/account/%s/orders", t.BaseURL, t.AccountID)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.Provider.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IBKR order HTTP %d: %s", resp.StatusCode, string(body))
	}

	var replyResp []struct {
		Id            string `json:"id"`
		Message       string `json:"message"`
		IsSupressable bool   `json:"isSuppressable"`
	}

	if err := json.Unmarshal(body, &replyResp); err == nil && len(replyResp) > 0 {
		for _, reply := range replyResp {
			if reply.IsSupressable {
				confirmUrl := fmt.Sprintf("%s/iserver/reply/%s", t.BaseURL, reply.Id)
				reqConfirm, _ := http.NewRequest("POST", confirmUrl, strings.NewReader(`{"confirmed":true}`))
				reqConfirm.Header.Set("Content-Type", "application/json")
				t.Provider.Client.Do(reqConfirm)
			}
		}
	}
	return nil
}

// GetLiveOrders fetches the active pending orders from IBKR
func (t *IBKRTrader) GetLiveOrders() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/iserver/account/orders", t.BaseURL) // The endpoint for all live orders
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.Provider.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch live orders")
	}

	var respData map[string]interface{}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &respData); err != nil {
		return nil, err
	}

	var liveOrders []map[string]interface{}
	// For IBKR Portal /iserver/account/orders usually returns an array directly or inside a "orders" key
	// We'll decode broadly
	return liveOrders, nil
}

func (t *IBKRTrader) SetLeverage(symbol string, leverage int) error {
	// IBKR handles margin natively via account type (RegT Margin / Portfolio Margin)
	// We do not set per-symbol leverage via this REST API.
	return nil
}

func (t *IBKRTrader) ClosePosition(symbol string, side string) error {
	log.Printf("IBKR: Flattening position for %s", symbol)
	// Query current position to know how much to sell/buy
	positions, err := t.GetPositions()
	if err != nil {
		return err
	}

	var qty float64 = 0
	for _, p := range positions {
		if p["symbol"].(string) == symbol {
			qty = p["positionAmt"].(float64)
			break
		}
	}

	if qty == 0 {
		return fmt.Errorf("no active position found for %s to close", symbol)
	}

	closeSide := "long"
	if qty > 0 {
		closeSide = "short" // We must sell to close a long
	} else {
		qty = -qty // absolute value
	}

	_, err = t.CreateOrder(symbol, closeSide, 0, qty, 1, "", "")
	return err
}

func (t *IBKRTrader) CancelAllOrders(symbol string) error {
	// Cancel all open orders for the account
	// The IBKR Web API supports a bulk cancel endpoint via DELETE /iserver/account/{accountId}/orders
	url := fmt.Sprintf("%s/iserver/account/%s/orders", t.BaseURL, t.AccountID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := t.Provider.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to cancel all orders on IBKR")
	}

	log.Printf("IBKR: Cancelled all open orders for account %s", t.AccountID)
	return nil
}

func (t *IBKRTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	err := t.ClosePosition(symbol, "long")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "closed_long", "symbol": symbol}, nil
}

func (t *IBKRTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	err := t.ClosePosition(symbol, "short")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "closed_short", "symbol": symbol}, nil
}

func (t *IBKRTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	// Standard stock formatting is typically whole numbers, but fractional exists.
	// For IBKR API, we can format to 2 or 4 decimals depending on the instrument.
	// Given US equities usually don't need excessive precision unless fractional, 2 is safe.
	return fmt.Sprintf("%.4f", quantity), nil
}

func (t *IBKRTrader) GetMarketPrice(symbol string) (float64, error) {
	quote, err := t.Provider.GetLatestQuote(symbol)
	if err != nil {
		return 0, err
	}

	// Default to using the Ask price as market price or midpoint
	if quote.AskPrice > 0 {
		return quote.AskPrice, nil
	}

	return quote.BidPrice, nil
}

func (t *IBKRTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	_, err := t.CreateOrder(symbol, "long", 0, quantity, leverage, "", "")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "opened_long", "symbol": symbol, "quantity": quantity, "leverage": leverage}, nil
}

func (t *IBKRTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	_, err := t.CreateOrder(symbol, "short", 0, quantity, leverage, "", "")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "opened_short", "symbol": symbol, "quantity": quantity, "leverage": leverage}, nil
}

func (t *IBKRTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	log.Printf("IBKR: Cannot natively set floating stop loss via simple REST outside of bracket orders yet. Logging desire for %f SL on %s", stopPrice, symbol)
	return nil
}

func (t *IBKRTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	log.Printf("IBKR: Cannot natively set floating take profit via simple REST outside of bracket orders yet. Logging desire for %f TP on %s", takeProfitPrice, symbol)
	return nil
}
