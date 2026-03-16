package trader

import (
	"net/http"
	"net/http/httptest"
	"northstar/broker"
	"northstar/market"
	"northstar/orders"
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
