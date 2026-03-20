package trader

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"northstar/broker"
	"northstar/market"
	"northstar/orders"
	"strconv"
	"strings"
	"sync"
	"time"
)

type IBKRTrader struct {
	BaseURL       string
	AccountID     string
	HTTPClient    *http.Client
	Provider      *market.IBKRProvider
	orderStore    *orders.Store
	orderMu       sync.Mutex
	balanceMu     sync.RWMutex
	fallbackCash  float64
	protectMu     sync.Mutex
	protectiveOCA map[string]string
}

type IBKRBrokerSnapshot struct {
	Balance    map[string]interface{}
	Positions  []map[string]interface{}
	OpenOrders []map[string]interface{}
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
		orderStore:    orders.NewStore(),
		fallbackCash:  initialBalance,
		protectiveOCA: make(map[string]string),
	}

	go trader.reconcilerLoop()

	return trader
}

// reconcilerLoop continually polls IBKR open orders and diffs status to emit explicitly mapped state transitions.
func (t *IBKRTrader) reconcilerLoop() {
	ticker := time.NewTicker(3 * time.Second)
	for range ticker.C {
		if err := t.reconcileOrderLifecycle(); err != nil {
			continue
		}
	}
}

func (t *IBKRTrader) GetOrderReconciliationSummary() orders.Summary {
	if t.orderStore == nil {
		return orders.Summary{}
	}
	return t.orderStore.SnapshotSummary()
}

func (t *IBKRTrader) SetOrderObserver(observer orders.Observer) {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.SetObserver(observer)
}

func (t *IBKRTrader) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	if t.orderStore == nil {
		return nil
	}
	return t.orderStore.Lookup(localID, brokerOrderID)
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
		"accountCash":           cash,
		"accountEquity":         cash,
		"availableBalance":      cash,
		"grossMarketValue":      0.0,
		"unrealizedPnL":         0.0,
		"realizedPnL":           0.0,
		"totalWalletBalance":    cash,
		"totalUnrealizedProfit": 0.0,
	}
}

func (t *IBKRTrader) GetBalance() (map[string]interface{}, error) {
	bodyBytes, err := t.Provider.Client.FetchPortfolioEndpoint(t.AccountID, "summary")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IBKR account summary: %w", err)
	}
	log.Printf(" IBKR summary response bytes=%d", len(bodyBytes))

	var summary map[string]struct {
		Amount float64 `json:"amount"`
	}

	if err := json.Unmarshal(bodyBytes, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse IBKR summary: %w", err)
	}

	result := make(map[string]interface{})

	accountEquity := t.getFallbackCash()
	if val, ok := summary["netliquidation"]; ok {
		accountEquity = val.Amount
	}
	result["accountEquity"] = accountEquity

	accountCash := t.getFallbackCash()
	if val, ok := summary["totalcashvalue"]; ok {
		accountCash = val.Amount
	} else if val, ok := summary["settledcash"]; ok {
		accountCash = val.Amount
	}
	result["accountCash"] = accountCash
	t.setFallbackCash(accountCash)

	if val, ok := summary["availablefunds"]; ok {
		result["availableBalance"] = val.Amount
	} else {
		result["availableBalance"] = accountCash
	}

	if val, ok := summary["grosspositionvalue"]; ok {
		result["grossMarketValue"] = math.Abs(val.Amount)
	} else {
		result["grossMarketValue"] = 0.0
	}

	if val, ok := summary["unrealizedpnl"]; ok {
		result["unrealizedPnL"] = val.Amount
	} else {
		result["unrealizedPnL"] = 0.0
	}

	if val, ok := summary["realizedpnl"]; ok {
		result["realizedPnL"] = val.Amount
	} else {
		result["realizedPnL"] = 0.0
	}

	// Legacy keys are kept for older broker adapters, but the explicit fields above are canonical.
	result["totalWalletBalance"] = accountCash
	result["totalUnrealizedProfit"] = result["unrealizedPnL"]
	result["totalEquity"] = accountEquity

	return result, nil
}

func (t *IBKRTrader) GetPositions() ([]map[string]interface{}, error) {
	b, err := t.Provider.Client.FetchPortfolioEndpoint(t.AccountID, "positions")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IBKR positions: %w", err)
	}

	var rawPositions []map[string]interface{}
	if err := json.Unmarshal(b, &rawPositions); err != nil {
		return nil, fmt.Errorf("failed to parse IBKR positions payload: %w", err)
	}

	var positions []map[string]interface{}
	for _, p := range rawPositions {
		positionAmt := toFloat(firstPresent(p["position"], p["positionAmt"], p["qty"]))
		if positionAmt == 0 {
			continue
		}

		symbol := strings.TrimSpace(toString(firstPresent(p["ticker"], p["symbol"], p["contractDesc"], p["description1"])))
		if symbol == "" {
			symbol = strings.TrimSpace(toString(p["conid"]))
		}

		side := "long"
		if positionAmt < 0 {
			side = "short"
		}

		positions = append(positions, map[string]interface{}{
			"symbol":           symbol,
			"side":             side,
			"positionAmt":      positionAmt,
			"entryPrice":       toFloat(firstPresent(p["avgCost"], p["avgPrice"], p["entryPrice"])),
			"markPrice":        toFloat(firstPresent(p["mktPrice"], p["markPrice"], p["price"])),
			"unRealizedProfit": toFloat(firstPresent(p["unrealizedPnl"], p["unRealizedProfit"], p["unrealizedProfit"])),
			"leverage":         1.0, // Equities generally map base leverage
			"liquidationPrice": 0.0,
		})
	}

	return positions, nil
}

func (t *IBKRTrader) CreateOrder(symbol string, side string, intent orders.Intent, positionSide string, price float64, quantity float64, leverage int, takeProfit string, stopLoss string) (map[string]interface{}, error) {
	// First resolve ConID using our central IBKRClient cache
	cID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ConID for %s: %w", symbol, err)
	}

	// Format Northstar long/short to IBKR BUY/SELL
	ibkrSide := "BUY"
	oppositeSide := "SELL"
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		ibkrSide = "SELL"
		oppositeSide = "BUY"
	}

	// Pre-submission Risk Validation
	if quantity <= 0 {
		return nil, fmt.Errorf("invalid quantity: %f", quantity)
	}
	// Stocks are submitted as whole-share quantities for safer IBKR compatibility.
	wholeQty := math.Floor(quantity)
	if wholeQty < 1 {
		return nil, fmt.Errorf("quantity too small after whole-share normalization: %.4f", quantity)
	}
	qtyValue := int64(wholeQty)

	// Phase 1 - Submit Entry Order
	entryOrder := map[string]interface{}{
		"acctId":     t.AccountID,
		"conid":      cID,
		"secType":    "STK",
		"orderType":  "MKT", // Market entry
		"tif":        "DAY",
		"quantity":   qtyValue,
		"side":       ibkrSide,
		"outsideRTH": false, // prioritize regular session routing for paper reliability
	}

	if price > 0 {
		entryOrder["orderType"] = "LMT"
		entryOrder["price"] = price
	}

	log.Printf("IBKR: Placing entry order for %d %s %s...", qtyValue, symbol, ibkrSide)

	entryLocalID := t.registerLocalOrder(intent, symbol, ibkrSide, positionSide, float64(qtyValue), time.Now())
	if err := t.submitIBKROrders([]interface{}{entryOrder}); err != nil {
		t.recordLocalOrderReject(entryLocalID, err)
		return nil, fmt.Errorf("entry order failed: %w", err)
	}
	_ = t.reconcileOrderLifecycle()
	result := map[string]interface{}{
		"status":       "submitted",
		"symbol":       symbol,
		"quantity":     float64(qtyValue),
		"leverage":     leverage,
		"localOrderId": entryLocalID,
	}

	// Phase 2 - Await Fill Confirmation
	// Poll IBKR orders endpoint for up to 10 seconds to detect the fill
	filled := false
	log.Printf("IBKR: Waiting for fill confirmation on %s %s...", symbol, ibkrSide)
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		orders, err := t.GetLiveOrders()
		if err != nil {
			return nil, fmt.Errorf("failed to confirm entry fill for %s: %w", symbol, err)
		}

		// We check if the entry order evaporated from live open orders
		// In IBKR, completely filled orders often drop from the active /orders list quickly
		// This is a naive heuristic for the REST API (Phase 6 Reconciliation engine will harden this globally)
		if len(orders) == 0 {
			filled = true
			break
		}

		// If there's an active order for this conid matching our side, it's still pending
		stillPending := false
		for _, o := range orders {
			rawSide := strings.ToUpper(strings.TrimSpace(toString(o["side"])))
			rawConid := toInt(o["conid"])
			rawStatus := strings.ToUpper(strings.TrimSpace(toString(o["status"])))
			if rawConid == cID && rawSide == ibkrSide &&
				rawStatus != "FILLED" && rawStatus != "CANCELLED" && rawStatus != "CLOSED" {
				stillPending = true
				break
			}
		}
		if !stillPending {
			filled = true
			break
		}
	}

	if !filled {
		log.Printf(" IBKR: Entry order on %s not filled within timeout. Brackets will not be placed to prevent orphan exposure.", symbol)
		result["status"] = "pending"
		return result, nil
	}

	log.Printf(" IBKR: Entry order for %s confirmed filled.", symbol)
	result["status"] = "filled"

	// Phase 3 & 4 - Submit OCA Brackets
	if takeProfit != "" || stopLoss != "" {
		var bracketOrders []interface{}
		ocaGroup := fmt.Sprintf("Northstar_OCA_%d", time.Now().UnixNano())

		if takeProfit != "" {
			tpVal, err := strconv.ParseFloat(strings.TrimSpace(takeProfit), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid takeProfit %q: %w", takeProfit, err)
			}
			bracketOrders = append(bracketOrders, map[string]interface{}{
				"acctId":     t.AccountID,
				"conid":      cID,
				"secType":    "STK",
				"orderType":  "LMT",
				"price":      tpVal,
				"tif":        "GTC",
				"quantity":   qtyValue,
				"side":       oppositeSide,
				"outsideRTH": false,
				"ocaGroup":   ocaGroup,
			})
		}

		if stopLoss != "" {
			slVal, err := strconv.ParseFloat(strings.TrimSpace(stopLoss), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid stopLoss %q: %w", stopLoss, err)
			}
			bracketOrders = append(bracketOrders, map[string]interface{}{
				"acctId":     t.AccountID,
				"conid":      cID,
				"secType":    "STK",
				"orderType":  "STP",
				"auxPrice":   slVal, // Stop Loss uses auxPrice in IBKR REST API
				"tif":        "GTC",
				"quantity":   qtyValue,
				"side":       oppositeSide,
				"outsideRTH": false,
				"ocaGroup":   ocaGroup,
			})
		}

		log.Printf("IBKR: Submitting OCA safety brackets (SL/TP) for %s...", symbol)
		bracketLocalIDs := make([]string, 0, 2)
		for _, orderType := range []struct {
			active bool
			intent orders.Intent
		}{
			{active: strings.TrimSpace(takeProfit) != "", intent: protectiveIntent(positionSide, "target")},
			{active: strings.TrimSpace(stopLoss) != "", intent: protectiveIntent(positionSide, "stop")},
		} {
			if !orderType.active {
				continue
			}
			bracketLocalIDs = append(bracketLocalIDs, t.registerLocalOrder(orderType.intent, symbol, oppositeSide, positionSide, float64(qtyValue), time.Now()))
		}
		if err := t.submitIBKROrders(bracketOrders); err != nil {
			for _, localID := range bracketLocalIDs {
				t.recordLocalOrderReject(localID, err)
			}
			log.Printf(" IBKR: Failed to submit brackets for %s: %v", symbol, err)
			result["status"] = "filled_bracket_failed"
			return result, err
		}
		_ = t.reconcileOrderLifecycle()
		log.Printf(" IBKR: Safe Brackets secured.")
	}

	return result, nil
}

// submitIBKROrders is a helper to transmit the REST payload and handle reply confirmation loops
func (t *IBKRTrader) submitIBKROrders(orders []interface{}) error {
	payload := map[string]interface{}{
		"orders": orders,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode order payload: %w", err)
	}
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
		return broker.NewIBKRHTTPError("POST", req.URL.Path, resp.StatusCode, string(body))
	}

	replies, err := parseIBKRReplyMessages(body)
	if err != nil {
		// Some successful submissions return non-JSON acks; keep flow moving but preserve visibility.
		log.Printf("IBKR: order response parse warning: %v", err)
		return nil
	}
	for _, reply := range replies {
		if strings.TrimSpace(reply.Error) != "" {
			return fmt.Errorf("ibkr order rejected: %s", reply.Error)
		}
		if hasIBKRRejectSignal(reply.Message) {
			return fmt.Errorf("ibkr order rejected: %s", reply.Message)
		}
		if !reply.IsSuppressable || strings.TrimSpace(reply.ID) == "" {
			continue
		}

		confirmURL := fmt.Sprintf("%s/iserver/reply/%s", t.BaseURL, reply.ID)
		reqConfirm, err := http.NewRequest("POST", confirmURL, strings.NewReader(`{"confirmed":true}`))
		if err != nil {
			return fmt.Errorf("failed to build IBKR confirm request: %w", err)
		}
		reqConfirm.Header.Set("Content-Type", "application/json")

		confirmResp, err := t.Provider.Client.Do(reqConfirm)
		if err != nil {
			return fmt.Errorf("ibkr confirm %s failed: %w", reply.ID, err)
		}
		confirmBody, _ := io.ReadAll(confirmResp.Body)
		confirmResp.Body.Close()
		if confirmResp.StatusCode != http.StatusOK {
			return broker.NewIBKRHTTPError("POST", reqConfirm.URL.Path, confirmResp.StatusCode, string(confirmBody))
		}
		if hasIBKRRejectSignal(string(confirmBody)) {
			return fmt.Errorf("ibkr confirm %s rejected: %s", reply.ID, string(confirmBody))
		}
	}
	return nil
}

// GetLiveOrders fetches the active pending orders from IBKR
func (t *IBKRTrader) GetLiveOrders() ([]map[string]interface{}, error) {
	endpoint := "/iserver/account/orders"
	b, statusCode, err := t.Provider.Client.DoPreflight("GET", endpoint)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, broker.NewIBKRHTTPError("GET", endpoint, statusCode, string(b))
	}

	liveOrders, err := parseLiveOrdersPayload(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse live orders: %w", err)
	}
	return liveOrders, nil
}

type ibkrReplyMessage struct {
	ID             string
	Message        string
	Error          string
	IsSuppressable bool
}

func parseIBKRReplyMessages(body []byte) ([]ibkrReplyMessage, error) {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := make([]ibkrReplyMessage, 0, 4)
	collectIBKRReplyMessages(payload, &out)
	return out, nil
}

func collectIBKRReplyMessages(v interface{}, out *[]ibkrReplyMessage) {
	switch t := v.(type) {
	case []interface{}:
		for _, item := range t {
			collectIBKRReplyMessages(item, out)
		}
	case map[string]interface{}:
		msg := strings.TrimSpace(toString(t["message"]))
		errMsg := strings.TrimSpace(toString(t["error"]))
		id := strings.TrimSpace(toString(firstPresent(t["id"], t["replyid"])))
		suppress := toBool(firstPresent(t["isSuppressable"], t["is_suppressable"]))
		if msg != "" || errMsg != "" || id != "" || suppress {
			*out = append(*out, ibkrReplyMessage{
				ID:             id,
				Message:        msg,
				Error:          errMsg,
				IsSuppressable: suppress,
			})
		}

		for _, key := range []string{"orders", "order", "messages", "replies", "data"} {
			if child, ok := t[key]; ok {
				collectIBKRReplyMessages(child, out)
			}
		}
	}
}

func parseLiveOrdersPayload(body []byte) ([]map[string]interface{}, error) {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	orders := extractOrderList(payload)
	return orders, nil
}

func extractOrderList(v interface{}) []map[string]interface{} {
	switch t := v.(type) {
	case []interface{}:
		return toOrderMaps(t)
	case map[string]interface{}:
		for _, key := range []string{"orders", "live_orders", "data"} {
			if child, ok := t[key]; ok {
				orders := extractOrderList(child)
				if len(orders) > 0 {
					return orders
				}
			}
		}
		// Sometimes the response itself is a single order object.
		if orderIDFromMap(t) != "" || toString(t["conid"]) != "" {
			return []map[string]interface{}{t}
		}
	}
	return []map[string]interface{}{}
}

func toOrderMaps(items []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

func orderIDFromMap(order map[string]interface{}) string {
	if order == nil {
		return ""
	}
	return strings.TrimSpace(toString(firstPresent(order["orderId"], order["order_id"], order["id"])))
}

func toBrokerOrders(rawOrders []map[string]interface{}, observedAt time.Time) []orders.BrokerOrder {
	out := make([]orders.BrokerOrder, 0, len(rawOrders))
	for _, order := range rawOrders {
		totalQty := toFloat(firstPresent(order["quantity"], order["qty"], order["totalQuantity"], order["size"]))
		filledQty := toFloat(firstPresent(order["filledQuantity"], order["filled_qty"], order["filled"], order["cumFillQuantity"], order["cumFill"], order["sizeFilled"]))
		remainingQty := toFloat(firstPresent(order["remainingQuantity"], order["remaining_qty"], order["remaining"], order["remainingSize"]))
		if totalQty <= 0 && filledQty > 0 {
			totalQty = filledQty + remainingQty
		}
		side := strings.ToUpper(strings.TrimSpace(toString(firstPresent(order["side"], order["order_side"]))))
		positionSide := strings.ToLower(strings.TrimSpace(toString(firstPresent(order["positionSide"], order["position_side"]))))
		rawStatus := strings.TrimSpace(toString(order["status"]))
		status := orders.NormalizeBrokerStatus(rawStatus, filledQty, totalQty, remainingQty)
		out = append(out, orders.BrokerOrder{
			OrderID:      orderIDFromMap(order),
			Symbol:       strings.ToUpper(strings.TrimSpace(toString(firstPresent(order["ticker"], order["symbol"], order["contractDesc"], order["description1"])))),
			Side:         side,
			PositionSide: positionSide,
			Status:       status,
			RawStatus:    rawStatus,
			Quantity:     totalQty,
			FilledQty:    filledQty,
			RemainingQty: remainingQty,
			AvgFillPrice: toFloat(firstPresent(order["avgFillPrice"], order["avgPrice"], order["price"])),
			ObservedAt:   observedAt,
		})
	}
	return out
}

func toOrderPositions(rawPositions []map[string]interface{}) []orders.PositionSnapshot {
	out := make([]orders.PositionSnapshot, 0, len(rawPositions))
	for _, pos := range rawPositions {
		qty := toFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"], pos["position"]))
		if qty < 0 {
			qty = -qty
		}
		out = append(out, orders.PositionSnapshot{
			Symbol:   strings.ToUpper(strings.TrimSpace(toString(pos["symbol"]))),
			Side:     strings.ToLower(strings.TrimSpace(toString(pos["side"]))),
			Quantity: qty,
		})
	}
	return out
}

func hasIBKRRejectSignal(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	for _, token := range []string{
		"reject",
		"denied",
		"insufficient",
		"invalid",
		"not allowed",
		"cannot",
		"failed",
		"error",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func firstPresent(values ...interface{}) interface{} {
	for _, v := range values {
		switch t := v.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(t) == "" {
				continue
			}
			return t
		default:
			return v
		}
	}
	return nil
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.Itoa(int(t))
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(strings.ToLower(t)))
		return err == nil && b
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, err := t.Int64()
		if err == nil {
			return int(n)
		}
		f, err := t.Float64()
		if err == nil {
			return int(f)
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int(f)
		}
	}
	return 0
}

func toFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return 0
}

func (t *IBKRTrader) SetLeverage(symbol string, leverage int) error {
	// IBKR handles margin natively via account type (RegT Margin / Portfolio Margin)
	// We do not set per-symbol leverage via this REST API.
	return nil
}

func (t *IBKRTrader) ClosePosition(symbol string, side string) (map[string]interface{}, error) {
	log.Printf("IBKR: Flattening position for %s", symbol)
	// Query current position to know how much to sell/buy
	positions, err := t.GetPositions()
	if err != nil {
		return nil, err
	}

	var qty float64
	targetSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	for _, p := range positions {
		posSymbol := strings.ToUpper(strings.TrimSpace(toString(p["symbol"])))
		if posSymbol == targetSymbol {
			qty = toFloat(p["positionAmt"])
			break
		}
	}

	if qty == 0 {
		return nil, fmt.Errorf("no active position found for %s to close", symbol)
	}

	closeSide := "long"
	if qty > 0 {
		closeSide = "short" // We must sell to close a long
	} else {
		qty = -qty // absolute value
	}

	intent := orders.IntentExitLong
	positionSide := "long"
	if closeSide == "long" {
		intent = orders.IntentExitShort
		positionSide = "short"
	}
	return t.CreateOrder(symbol, closeSide, intent, positionSide, 0, qty, 1, "", "")
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
		body, _ := io.ReadAll(resp.Body)
		return broker.NewIBKRHTTPError("DELETE", req.URL.Path, resp.StatusCode, string(body))
	}

	log.Printf("IBKR: Cancelled all open orders for account %s", t.AccountID)
	return nil
}

func (t *IBKRTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.ClosePosition(symbol, "long")
}

func (t *IBKRTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.ClosePosition(symbol, "short")
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
	return t.CreateOrder(symbol, "long", orders.IntentEntryLong, "long", 0, quantity, leverage, "", "")
}

func (t *IBKRTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.CreateOrder(symbol, "short", orders.IntentEntryShort, "short", 0, quantity, leverage, "", "")
}

func (t *IBKRTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	if stopPrice <= 0 || quantity <= 0 {
		return nil
	}
	cID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return fmt.Errorf("failed to resolve ConID for %s stop-loss: %w", symbol, err)
	}
	qtyValue, err := wholeStockQty(quantity)
	if err != nil {
		return fmt.Errorf("invalid stop-loss quantity for %s: %w", symbol, err)
	}
	exitSide, err := protectiveExitSide(positionSide)
	if err != nil {
		return err
	}
	ocaGroup := t.protectiveGroup(symbol, positionSide)
	order := map[string]interface{}{
		"acctId":     t.AccountID,
		"conid":      cID,
		"secType":    "STK",
		"orderType":  "STP",
		"auxPrice":   stopPrice,
		"tif":        "GTC",
		"quantity":   qtyValue,
		"side":       exitSide,
		"outsideRTH": false,
		"ocaGroup":   ocaGroup,
	}
	localID := t.registerLocalOrder(protectiveIntent(strings.ToLower(positionSide), "stop"), symbol, exitSide, strings.ToLower(positionSide), float64(qtyValue), time.Now())
	if err := t.submitIBKROrders([]interface{}{order}); err != nil {
		t.recordLocalOrderReject(localID, err)
		return fmt.Errorf("failed to submit stop-loss for %s: %w", symbol, err)
	}
	_ = t.reconcileOrderLifecycle()
	log.Printf("IBKR: Submitted stop-loss for %s at %.4f (qty=%d side=%s oca=%s)", symbol, stopPrice, qtyValue, exitSide, ocaGroup)
	return nil
}

func (t *IBKRTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	if takeProfitPrice <= 0 || quantity <= 0 {
		return nil
	}
	cID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return fmt.Errorf("failed to resolve ConID for %s take-profit: %w", symbol, err)
	}
	qtyValue, err := wholeStockQty(quantity)
	if err != nil {
		return fmt.Errorf("invalid take-profit quantity for %s: %w", symbol, err)
	}
	exitSide, err := protectiveExitSide(positionSide)
	if err != nil {
		return err
	}
	ocaGroup := t.protectiveGroup(symbol, positionSide)
	order := map[string]interface{}{
		"acctId":     t.AccountID,
		"conid":      cID,
		"secType":    "STK",
		"orderType":  "LMT",
		"price":      takeProfitPrice,
		"tif":        "GTC",
		"quantity":   qtyValue,
		"side":       exitSide,
		"outsideRTH": false,
		"ocaGroup":   ocaGroup,
	}
	localID := t.registerLocalOrder(protectiveIntent(strings.ToLower(positionSide), "target"), symbol, exitSide, strings.ToLower(positionSide), float64(qtyValue), time.Now())
	if err := t.submitIBKROrders([]interface{}{order}); err != nil {
		t.recordLocalOrderReject(localID, err)
		return fmt.Errorf("failed to submit take-profit for %s: %w", symbol, err)
	}
	_ = t.reconcileOrderLifecycle()
	log.Printf("IBKR: Submitted take-profit for %s at %.4f (qty=%d side=%s oca=%s)", symbol, takeProfitPrice, qtyValue, exitSide, ocaGroup)
	return nil
}

func wholeStockQty(quantity float64) (int64, error) {
	if quantity <= 0 {
		return 0, fmt.Errorf("quantity %.4f must be positive", quantity)
	}
	whole := int64(math.Floor(quantity))
	if whole < 1 {
		return 0, fmt.Errorf("quantity %.4f rounded below 1 share", quantity)
	}
	return whole, nil
}

func protectiveExitSide(positionSide string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(positionSide)) {
	case "LONG":
		return "SELL", nil
	case "SHORT":
		return "BUY", nil
	default:
		return "", fmt.Errorf("unsupported position side %q for protective order", positionSide)
	}
}

func protectiveIntent(positionSide, kind string) orders.Intent {
	positionSide = strings.ToLower(strings.TrimSpace(positionSide))
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch {
	case positionSide == "short" && kind == "stop":
		return orders.IntentProtectiveStopShort
	case positionSide == "short" && kind == "target":
		return orders.IntentProtectiveTargetShort
	case kind == "stop":
		return orders.IntentProtectiveStopLong
	case kind == "target":
		return orders.IntentProtectiveTargetLong
	default:
		return orders.IntentUnknown
	}
}

func (t *IBKRTrader) registerLocalOrder(intent orders.Intent, symbol, side, positionSide string, qty float64, at time.Time) string {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	return t.orderStore.RegisterSubmitted(intent, symbol, side, positionSide, qty, at)
}

func (t *IBKRTrader) recordLocalOrderReject(localID string, err error) {
	if t.orderStore == nil || strings.TrimSpace(localID) == "" || err == nil {
		return
	}
	t.orderStore.MarkRejected(localID, err.Error(), time.Now())
}

func (t *IBKRTrader) protectiveGroup(symbol, positionSide string) string {
	key := strings.ToUpper(strings.TrimSpace(symbol)) + ":" + strings.ToUpper(strings.TrimSpace(positionSide))
	t.protectMu.Lock()
	defer t.protectMu.Unlock()
	if g, ok := t.protectiveOCA[key]; ok && strings.TrimSpace(g) != "" {
		return g
	}
	group := fmt.Sprintf("NS_PROTECT_%s_%d", strings.ReplaceAll(key, ":", "_"), time.Now().UnixNano())
	t.protectiveOCA[key] = group
	return group
}

func (t *IBKRTrader) reconcileOrderLifecycle() error {
	openOrders, err := t.GetLiveOrders()
	if err != nil {
		if t.orderStore != nil {
			t.orderStore.RecordReconciliationError(err, time.Now())
		}
		return err
	}
	positions, err := t.GetPositions()
	if err != nil {
		if t.orderStore != nil {
			t.orderStore.RecordReconciliationError(err, time.Now())
		}
		return err
	}
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	result := t.orderStore.Reconcile(toBrokerOrders(openOrders, time.Now()), toOrderPositions(positions), time.Now())
	if result.Mismatches > 0 {
		log.Printf(" IBKR order reconciliation: %s", result.Summary)
		for _, issue := range result.Issues {
			log.Printf(" IBKR order issue [%s]: %s", issue.Type, issue.Message)
		}
	}
	return nil
}

func (t *IBKRTrader) ReconcileBrokerState() (*IBKRBrokerSnapshot, error) {
	balance, err := t.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("account summary refresh failed: %w", err)
	}

	positions, err := t.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("positions refresh failed: %w", err)
	}

	openOrders, err := t.GetLiveOrders()
	if err != nil {
		return nil, fmt.Errorf("open orders refresh failed: %w", err)
	}

	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.Reconcile(toBrokerOrders(openOrders, time.Now()), toOrderPositions(positions), time.Now())
	log.Printf(" IBKR reconciliation refreshed account summary, %d positions, %d open orders", len(positions), len(openOrders))

	return &IBKRBrokerSnapshot{
		Balance:    balance,
		Positions:  positions,
		OpenOrders: openOrders,
	}, nil
}
