package trader

import (
	"net/http"
	"net/http/httptest"
	"northstar/broker"
	"northstar/market"
	"northstar/orders"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseLiveOrdersPayload_Array(t *testing.T) {
	body := []byte(`[
		{"orderId":12345,"status":"Submitted","side":"BUY","conid":265598},
		{"orderId":"54321","status":"Filled","side":"SELL","conid":"756733"}
	]`)

	orders, err := parseLiveOrdersPayload(body)
	if err != nil {
		t.Fatalf("parseLiveOrdersPayload failed: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if got := orderIDFromMap(orders[0]); got != "12345" {
		t.Fatalf("unexpected order id: %s", got)
	}
	if got := orderIDFromMap(orders[1]); got != "54321" {
		t.Fatalf("unexpected order id: %s", got)
	}
}

func TestParseLiveOrdersPayload_WrappedOrders(t *testing.T) {
	body := []byte(`{
		"orders":[
			{"id":"A1","status":"Submitted"},
			{"order_id":"B2","status":"PreSubmitted"}
		]
	}`)

	orders, err := parseLiveOrdersPayload(body)
	if err != nil {
		t.Fatalf("parseLiveOrdersPayload failed: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if got := orderIDFromMap(orders[0]); got != "A1" {
		t.Fatalf("unexpected id in order[0]: %s", got)
	}
	if got := orderIDFromMap(orders[1]); got != "B2" {
		t.Fatalf("unexpected id in order[1]: %s", got)
	}
}

func TestParseIBKRReplyMessages_Nested(t *testing.T) {
	body := []byte(`{
		"orders":[
			{"id":"r1","message":"price exceeds limit","isSuppressable":true},
			{"id":"r2","error":"insufficient buying power","isSuppressable":false}
		]
	}`)

	replies, err := parseIBKRReplyMessages(body)
	if err != nil {
		t.Fatalf("parseIBKRReplyMessages failed: %v", err)
	}
	if len(replies) < 2 {
		t.Fatalf("expected at least 2 reply messages, got %d", len(replies))
	}
	if replies[0].ID == "" {
		t.Fatalf("expected first reply to include an id")
	}
	if !hasIBKRRejectSignal(replies[1].Error) {
		t.Fatalf("expected reject signal for reply error: %q", replies[1].Error)
	}
}

func TestWholeStockQty(t *testing.T) {
	qty, err := wholeStockQty(10.9)
	if err != nil {
		t.Fatalf("wholeStockQty returned error: %v", err)
	}
	if qty != 10 {
		t.Fatalf("expected 10, got %d", qty)
	}

	if _, err := wholeStockQty(0.25); err == nil {
		t.Fatalf("expected error for sub-share quantity")
	}
}

func TestProtectiveExitSide(t *testing.T) {
	sell, err := protectiveExitSide("LONG")
	if err != nil || sell != "SELL" {
		t.Fatalf("expected LONG -> SELL, got %q (err=%v)", sell, err)
	}

	buy, err := protectiveExitSide("short")
	if err != nil || buy != "BUY" {
		t.Fatalf("expected SHORT -> BUY, got %q (err=%v)", buy, err)
	}

	if _, err := protectiveExitSide("UNKNOWN"); err == nil {
		t.Fatalf("expected error for unknown position side")
	}
}

func TestReconcileBrokerStateRefreshesBalancePositionsAndOrders(t *testing.T) {
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
			_, _ = w.Write([]byte(`["DU123456"]`))
		case "/portfolio/DU123456/summary":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"netliquidation":{"amount":101250},
				"totalcashvalue":{"amount":90000},
				"availablefunds":{"amount":85000},
				"grosspositionvalue":{"amount":11250},
				"unrealizedpnl":{"amount":1250},
				"realizedpnl":{"amount":500}
			}`))
		case "/portfolio/DU123456/positions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"ticker":"AAPL","position":10,"avgCost":150,"mktPrice":155,"unrealizedPnl":50}
			]`))
		case "/iserver/account/orders":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orders":[{"orderId":"12345","status":"Submitted","ticker":"AAPL"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &market.IBKRProvider{
		Client: &broker.IBKRClient{
			BaseURL:    server.URL,
			AccountID:  "DU123456",
			HTTPClient: server.Client(),
		},
	}

	trader := &IBKRTrader{
		BaseURL:       server.URL,
		AccountID:     "DU123456",
		Provider:      provider,
		orderStore:    orders.NewStore(),
		fallbackCash:  100000,
		protectiveOCA: make(map[string]string),
	}

	snapshot, err := trader.ReconcileBrokerState()
	if err != nil {
		t.Fatalf("ReconcileBrokerState failed: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("expected reconciliation snapshot")
	}
	if got := snapshot.Balance["accountEquity"].(float64); got != 101250 {
		t.Fatalf("unexpected account equity: %v", got)
	}
	if len(snapshot.Positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(snapshot.Positions))
	}
	if len(snapshot.OpenOrders) != 1 {
		t.Fatalf("expected 1 open order, got %d", len(snapshot.OpenOrders))
	}
	summary := trader.GetOrderReconciliationSummary()
	if summary.TrackedOrders != 1 {
		t.Fatalf("expected 1 tracked order, got %d", summary.TrackedOrders)
	}
	if summary.ActiveLocalOrders != 1 {
		t.Fatalf("expected 1 active local order, got %d", summary.ActiveLocalOrders)
	}
}

func TestGetBalanceAndPositionsUsePortfolioWarmupPath(t *testing.T) {
	var sawSubaccounts int32
	var sawSubaccounts2 int32
	var summaryCalls int32
	var positionsCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
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
		case r.URL.Path == "/portfolio/DU123456/summary":
			if atomic.AddInt32(&summaryCalls, 1) == 1 {
				http.Error(w, "warming summary", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"netliquidation":{"amount":101250},
				"totalcashvalue":{"amount":90000},
				"availablefunds":{"amount":85000},
				"grosspositionvalue":{"amount":11250},
				"unrealizedpnl":{"amount":1250},
				"realizedpnl":{"amount":500}
			}`))
		case r.URL.Path == "/portfolio/DU123456/positions":
			if atomic.AddInt32(&positionsCalls, 1) == 1 {
				http.Error(w, "warming positions", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"ticker":"AAPL","position":10,"avgCost":150,"mktPrice":155,"unrealizedPnl":50}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := broker.NewIBKRClient(server.URL, "DU123456", "")
	client.HTTPClient = server.Client()
	provider := &market.IBKRProvider{Client: client}

	trader := &IBKRTrader{
		BaseURL:       server.URL,
		AccountID:     "DU123456",
		Provider:      provider,
		orderStore:    orders.NewStore(),
		fallbackCash:  100000,
		protectiveOCA: make(map[string]string),
	}

	balance, err := trader.GetBalance()
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if got := balance["accountEquity"].(float64); got != 101250 {
		t.Fatalf("unexpected account equity: %v", got)
	}

	positions, err := trader.GetPositions()
	if err != nil {
		t.Fatalf("GetPositions failed: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected one position, got %d", len(positions))
	}
	if atomic.LoadInt32(&sawSubaccounts) == 0 || atomic.LoadInt32(&sawSubaccounts2) == 0 {
		t.Fatalf("expected portfolio warm-up endpoints to be exercised")
	}
	if atomic.LoadInt32(&summaryCalls) < 2 {
		t.Fatalf("expected summary retry, got %d call(s)", atomic.LoadInt32(&summaryCalls))
	}
	if atomic.LoadInt32(&positionsCalls) < 2 {
		t.Fatalf("expected positions retry, got %d call(s)", atomic.LoadInt32(&positionsCalls))
	}
}

func TestCreateOrderDoesNotAssumeFillFromMissingOpenOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/iserver/secdef/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"conid":265598,"symbol":"AAPL","description":"NASDAQ","sections":[{"secType":"STK","exchange":"NASDAQ"}]}]`))
		case r.URL.Path == "/iserver/account/DU123456/orders" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/iserver/account/orders":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orders":[]}`))
		case r.URL.Path == "/portfolio/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["DU123456"]`))
		case r.URL.Path == "/portfolio/subaccounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"DU123456"}]`))
		case r.URL.RequestURI() == "/portfolio/subaccounts2?page=0":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"DU123456"}]`))
		case r.URL.Path == "/portfolio/DU123456/positions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := broker.NewIBKRClient(server.URL, "DU123456", "")
	client.HTTPClient = server.Client()
	provider := &market.IBKRProvider{Client: client}

	trader := &IBKRTrader{
		BaseURL:       server.URL,
		AccountID:     "DU123456",
		HTTPClient:    server.Client(),
		Provider:      provider,
		orderStore:    orders.NewStore(),
		fallbackCash:  100000,
		protectiveOCA: make(map[string]string),
	}

	result, err := trader.CreateOrder("AAPL", "long", orders.IntentEntryLong, "long", 0, 10, 1, "", "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if got := result["status"]; got != "submitted" {
		t.Fatalf("expected submitted status without fill inference, got %v", got)
	}
	if got := result["localOrderId"]; got == "" {
		t.Fatalf("expected local order id in result")
	}
	summary := trader.GetOrderReconciliationSummary()
	if summary.ActiveLocalOrders != 1 {
		t.Fatalf("expected submitted order to remain active while broker truth is pending, got %+v", summary)
	}
	if summary.ResolvedOrders != 0 {
		t.Fatalf("expected no resolved orders from missing open-order heuristic, got %+v", summary)
	}
}

// TestNewAutoTrader_IBKRLive_NonIBKRDataProvider_ReturnsError verifies that
// configuring Exchange="ibkr" for live trading with a non-IBKR data provider
// (e.g. "synthetic") returns a clear error rather than panicking with an unsafe
// type assertion.
func TestNewAutoTrader_IBKRLive_NonIBKRDataProvider_ReturnsError(t *testing.T) {
	nonIBKRProviders := []string{"synthetic", "csv", "alpaca"}
	for _, dp := range nonIBKRProviders {
		t.Run(dp, func(t *testing.T) {
			cfg := AutoTraderConfig{
				Exchange:       "ibkr",
				Broker:         "live",
				InstrumentType: "equity",
				DataProvider:   dp,
				InitialBalance: 100_000,
			}
			_, err := NewAutoTrader(cfg)
			if err == nil {
				t.Fatalf("expected error when data_provider=%q is used with IBKR live trading", dp)
			}
			if !strings.Contains(err.Error(), "data_provider") {
				t.Fatalf("expected error message to mention data_provider, got: %v", err)
			}
		})
	}
}
