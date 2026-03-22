package trader

import (
	"encoding/json"
	"math"
	"northstar/orders"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// firstPresent
// ---------------------------------------------------------------------------

func TestFirstPresent_ReturnsFirstNonNil(t *testing.T) {
	got := firstPresent(nil, nil, "hello")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %v", got)
	}
}

func TestFirstPresent_SkipsEmptyStrings(t *testing.T) {
	got := firstPresent("", "  ", "value")
	if got != "value" {
		t.Fatalf("expected 'value', got %v", got)
	}
}

func TestFirstPresent_ReturnsNilWhenAllNil(t *testing.T) {
	got := firstPresent(nil, nil, nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestFirstPresent_ReturnsNumericZero(t *testing.T) {
	// Numeric zero is non-nil and should be returned
	got := firstPresent(nil, float64(0))
	if got != float64(0) {
		t.Fatalf("expected 0.0, got %v", got)
	}
}

func TestFirstPresent_ReturnsBoolFalse(t *testing.T) {
	got := firstPresent(nil, false)
	if got != false {
		t.Fatalf("expected false, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// toString
// ---------------------------------------------------------------------------

func TestToString_Nil(t *testing.T) {
	if got := toString(nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestToString_String(t *testing.T) {
	if got := toString("hello"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestToString_Float64(t *testing.T) {
	if got := toString(float64(150.5)); got != "150.5" {
		t.Fatalf("expected '150.5', got %q", got)
	}
}

func TestToString_Int(t *testing.T) {
	if got := toString(42); got != "42" {
		t.Fatalf("expected '42', got %q", got)
	}
}

func TestToString_JSONNumber(t *testing.T) {
	n := json.Number("12345")
	if got := toString(n); got != "12345" {
		t.Fatalf("expected '12345', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// toFloat
// ---------------------------------------------------------------------------

func TestToFloat_Float64(t *testing.T) {
	if got := toFloat(float64(3.14)); got != 3.14 {
		t.Fatalf("expected 3.14, got %v", got)
	}
}

func TestToFloat_Int(t *testing.T) {
	if got := toFloat(100); got != 100.0 {
		t.Fatalf("expected 100, got %v", got)
	}
}

func TestToFloat_JSONNumber(t *testing.T) {
	if got := toFloat(json.Number("99.5")); got != 99.5 {
		t.Fatalf("expected 99.5, got %v", got)
	}
}

func TestToFloat_String(t *testing.T) {
	if got := toFloat("42.5"); got != 42.5 {
		t.Fatalf("expected 42.5, got %v", got)
	}
}

func TestToFloat_EmptyString(t *testing.T) {
	if got := toFloat(""); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

func TestToFloat_Nil(t *testing.T) {
	if got := toFloat(nil); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// orderIDFromMap
// ---------------------------------------------------------------------------

func TestOrderIDFromMap_OrderId(t *testing.T) {
	m := map[string]interface{}{"orderId": float64(12345)}
	if got := orderIDFromMap(m); got != "12345" {
		t.Fatalf("expected '12345', got %q", got)
	}
}

func TestOrderIDFromMap_OrderIdString(t *testing.T) {
	m := map[string]interface{}{"order_id": "ABC-123"}
	if got := orderIDFromMap(m); got != "ABC-123" {
		t.Fatalf("expected 'ABC-123', got %q", got)
	}
}

func TestOrderIDFromMap_IdFallback(t *testing.T) {
	m := map[string]interface{}{"id": "fallback-id"}
	if got := orderIDFromMap(m); got != "fallback-id" {
		t.Fatalf("expected 'fallback-id', got %q", got)
	}
}

func TestOrderIDFromMap_NilMap(t *testing.T) {
	if got := orderIDFromMap(nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestOrderIDFromMap_PreferenceOrder(t *testing.T) {
	// orderId takes precedence over order_id and id
	m := map[string]interface{}{"orderId": "first", "order_id": "second", "id": "third"}
	if got := orderIDFromMap(m); got != "first" {
		t.Fatalf("expected 'first', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// hasIBKRRejectSignal
// ---------------------------------------------------------------------------

func TestHasIBKRRejectSignal(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Order rejected by exchange", true},
		{"Request denied", true},
		{"Insufficient buying power", true},
		{"Invalid order parameters", true},
		{"Order not allowed for account", true},
		{"Cannot submit at this time", true},
		{"Order failed validation", true},
		{"Error processing request", true},
		{"Order submitted successfully", false},
		{"Filled 100 shares", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		got := hasIBKRRejectSignal(tc.msg)
		if got != tc.want {
			t.Errorf("hasIBKRRejectSignal(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// toBrokerOrders — the critical parsing boundary
// ---------------------------------------------------------------------------

func TestToBrokerOrders_BasicIBKROrder(t *testing.T) {
	now := time.Now()
	raw := []map[string]interface{}{
		{
			"orderId":        float64(12345),
			"ticker":         "AAPL",
			"side":           "BUY",
			"status":         "Submitted",
			"quantity":       float64(100),
			"filledQuantity": float64(0),
			"avgFillPrice":   float64(0),
		},
	}

	result := toBrokerOrders(raw, now)
	if len(result) != 1 {
		t.Fatalf("expected 1 order, got %d", len(result))
	}

	o := result[0]
	assertEqual(t, "OrderID", o.OrderID, "12345")
	assertEqual(t, "Symbol", o.Symbol, "AAPL")
	assertEqual(t, "Side", o.Side, "BUY")
	assertEqual(t, "Status", string(o.Status), string(orders.StatusAccepted))
	assertFloat(t, "Quantity", o.Quantity, 100)
	assertFloat(t, "FilledQty", o.FilledQty, 0)
	if !o.ObservedAt.Equal(now) {
		t.Errorf("ObservedAt mismatch")
	}
}

func TestToBrokerOrders_PolymorphicQuantityKeys(t *testing.T) {
	now := time.Now()
	// Test each alternative quantity key name
	qtyKeys := []string{"quantity", "qty", "totalQuantity", "size"}
	for _, key := range qtyKeys {
		raw := []map[string]interface{}{
			{
				"orderId": "test",
				"ticker":  "MSFT",
				"side":    "SELL",
				"status":  "Filled",
				key:       float64(50),
			},
		}
		result := toBrokerOrders(raw, now)
		if len(result) != 1 {
			t.Fatalf("key %q: expected 1 order", key)
		}
		assertFloat(t, "Quantity via "+key, result[0].Quantity, 50)
	}
}

func TestToBrokerOrders_PolymorphicFilledKeys(t *testing.T) {
	now := time.Now()
	filledKeys := []string{"filledQuantity", "filled_qty", "filled", "cumFillQuantity", "cumFill", "sizeFilled"}
	for _, key := range filledKeys {
		raw := []map[string]interface{}{
			{
				"orderId":  "test",
				"ticker":   "TSLA",
				"side":     "BUY",
				"status":   "Filled",
				"quantity": float64(100),
				key:        float64(75),
			},
		}
		result := toBrokerOrders(raw, now)
		if len(result) != 1 {
			t.Fatalf("key %q: expected 1 order", key)
		}
		assertFloat(t, "FilledQty via "+key, result[0].FilledQty, 75)
	}
}

func TestToBrokerOrders_PolymorphicSymbolKeys(t *testing.T) {
	now := time.Now()
	symbolKeys := []string{"ticker", "symbol", "contractDesc", "description1"}
	for _, key := range symbolKeys {
		raw := []map[string]interface{}{
			{
				"orderId": "test",
				key:       "GOOG",
				"side":    "BUY",
				"status":  "Submitted",
			},
		}
		result := toBrokerOrders(raw, now)
		assertEqual(t, "Symbol via "+key, result[0].Symbol, "GOOG")
	}
}

func TestToBrokerOrders_TotalQtyInferredFromFilledPlusRemaining(t *testing.T) {
	now := time.Now()
	// When totalQty is 0 but filledQty > 0, totalQty should be inferred
	raw := []map[string]interface{}{
		{
			"orderId":           "test",
			"ticker":            "AAPL",
			"side":              "BUY",
			"status":            "PartiallyFilled",
			"filledQuantity":    float64(60),
			"remainingQuantity": float64(40),
		},
	}
	result := toBrokerOrders(raw, now)
	assertFloat(t, "inferred Quantity", result[0].Quantity, 100)
	assertFloat(t, "FilledQty", result[0].FilledQty, 60)
	assertFloat(t, "RemainingQty", result[0].RemainingQty, 40)
}

func TestToBrokerOrders_SideNormalization(t *testing.T) {
	now := time.Now()
	cases := []struct {
		input string
		want  string
	}{
		{"buy", "BUY"},
		{"  BUY  ", "BUY"},
		{"sell", "SELL"},
		{"SELL", "SELL"},
	}
	for _, tc := range cases {
		raw := []map[string]interface{}{
			{"orderId": "s1", "ticker": "X", "side": tc.input, "status": "Submitted"},
		}
		result := toBrokerOrders(raw, now)
		assertEqual(t, "Side from "+tc.input, result[0].Side, tc.want)
	}
}

func TestToBrokerOrders_AlternativeSideKey(t *testing.T) {
	now := time.Now()
	raw := []map[string]interface{}{
		{"orderId": "s1", "ticker": "X", "order_side": "SELL", "status": "Submitted"},
	}
	result := toBrokerOrders(raw, now)
	assertEqual(t, "Side via order_side", result[0].Side, "SELL")
}

func TestToBrokerOrders_PositionSideNormalization(t *testing.T) {
	now := time.Now()
	raw := []map[string]interface{}{
		{"orderId": "p1", "ticker": "X", "side": "BUY", "positionSide": " LONG ", "status": "Submitted"},
	}
	result := toBrokerOrders(raw, now)
	assertEqual(t, "PositionSide", result[0].PositionSide, "long")
}

func TestToBrokerOrders_StatusNormalization(t *testing.T) {
	now := time.Now()
	cases := []struct {
		rawStatus string
		filled    float64
		total     float64
		remaining float64
		want      orders.Status
	}{
		{"Rejected", 0, 100, 100, orders.StatusRejected},
		{"Cancelled", 0, 100, 100, orders.StatusCancelled},
		{"Inactive", 0, 100, 100, orders.StatusCancelled},
		{"Filled", 100, 100, 0, orders.StatusFilled},
		{"Submitted", 50, 100, 50, orders.StatusPartiallyFilled},
		{"PreSubmitted", 0, 100, 100, orders.StatusAccepted},
		{"Submitted", 0, 100, 100, orders.StatusAccepted},
		{"PendingSubmit", 0, 100, 100, orders.StatusAccepted},
		{"", 0, 0, 0, orders.StatusAccepted},
	}
	for _, tc := range cases {
		raw := []map[string]interface{}{
			{
				"orderId":           "test",
				"ticker":            "X",
				"side":              "BUY",
				"status":            tc.rawStatus,
				"quantity":          tc.total,
				"filledQuantity":    tc.filled,
				"remainingQuantity": tc.remaining,
			},
		}
		result := toBrokerOrders(raw, now)
		if result[0].Status != tc.want {
			t.Errorf("status %q (filled=%.0f, total=%.0f, rem=%.0f): got %q, want %q",
				tc.rawStatus, tc.filled, tc.total, tc.remaining, result[0].Status, tc.want)
		}
		assertEqual(t, "RawStatus for "+tc.rawStatus, result[0].RawStatus, tc.rawStatus)
	}
}

func TestToBrokerOrders_AvgFillPriceAlternativeKeys(t *testing.T) {
	now := time.Now()
	for _, key := range []string{"avgFillPrice", "avgPrice", "price"} {
		raw := []map[string]interface{}{
			{"orderId": "test", "ticker": "X", "side": "BUY", "status": "Filled",
				"quantity": float64(10), key: float64(155.50)},
		}
		result := toBrokerOrders(raw, now)
		assertFloat(t, "AvgFillPrice via "+key, result[0].AvgFillPrice, 155.50)
	}
}

func TestToBrokerOrders_EmptyInput(t *testing.T) {
	result := toBrokerOrders(nil, time.Now())
	if len(result) != 0 {
		t.Fatalf("expected 0 orders from nil input, got %d", len(result))
	}

	result = toBrokerOrders([]map[string]interface{}{}, time.Now())
	if len(result) != 0 {
		t.Fatalf("expected 0 orders from empty input, got %d", len(result))
	}
}

func TestToBrokerOrders_MultipleOrders(t *testing.T) {
	now := time.Now()
	raw := []map[string]interface{}{
		{"orderId": "1", "ticker": "AAPL", "side": "BUY", "status": "Submitted", "quantity": float64(10)},
		{"orderId": "2", "ticker": "MSFT", "side": "SELL", "status": "Filled", "quantity": float64(20), "filledQuantity": float64(20)},
		{"orderId": "3", "ticker": "GOOG", "side": "BUY", "status": "Cancelled", "quantity": float64(5)},
	}
	result := toBrokerOrders(raw, now)
	if len(result) != 3 {
		t.Fatalf("expected 3 orders, got %d", len(result))
	}
	assertEqual(t, "order[0].Symbol", result[0].Symbol, "AAPL")
	assertEqual(t, "order[1].Symbol", result[1].Symbol, "MSFT")
	assertEqual(t, "order[2].Symbol", result[2].Symbol, "GOOG")
}

// ---------------------------------------------------------------------------
// toOrderPositions — position truth boundary
// ---------------------------------------------------------------------------

func TestToOrderPositions_BasicPosition(t *testing.T) {
	raw := []map[string]interface{}{
		{
			"symbol": "AAPL",
			"side":   "long",
			"qty":    float64(100),
		},
	}
	result := toOrderPositions(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 position, got %d", len(result))
	}
	assertEqual(t, "Symbol", result[0].Symbol, "AAPL")
	assertEqual(t, "Side", result[0].Side, "long")
	assertFloat(t, "Quantity", result[0].Quantity, 100)
}

func TestToOrderPositions_NegativeQuantityBecomesPositive(t *testing.T) {
	raw := []map[string]interface{}{
		{
			"symbol":      "TSLA",
			"side":        "short",
			"positionAmt": float64(-50),
		},
	}
	result := toOrderPositions(raw)
	assertFloat(t, "abs(Quantity)", result[0].Quantity, 50)
}

func TestToOrderPositions_PolymorphicQuantityKeys(t *testing.T) {
	qtyKeys := []string{"positionAmt", "position_amt", "qty", "quantity", "position"}
	for _, key := range qtyKeys {
		raw := []map[string]interface{}{
			{"symbol": "X", "side": "long", key: float64(25)},
		}
		result := toOrderPositions(raw)
		if len(result) != 1 {
			t.Fatalf("key %q: expected 1 position", key)
		}
		assertFloat(t, "Quantity via "+key, result[0].Quantity, 25)
	}
}

func TestToOrderPositions_SymbolUppercased(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "  aapl  ", "side": "long", "qty": float64(10)},
	}
	result := toOrderPositions(raw)
	assertEqual(t, "Symbol uppercased and trimmed", result[0].Symbol, "AAPL")
}

func TestToOrderPositions_SideLowercased(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "X", "side": " LONG ", "qty": float64(10)},
	}
	result := toOrderPositions(raw)
	assertEqual(t, "Side lowercased and trimmed", result[0].Side, "long")
}

func TestToOrderPositions_EmptyInput(t *testing.T) {
	result := toOrderPositions(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

func TestToOrderPositions_MultiplePositions(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "AAPL", "side": "long", "qty": float64(100)},
		{"symbol": "TSLA", "side": "short", "position": float64(-30)},
	}
	result := toOrderPositions(raw)
	if len(result) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(result))
	}
	assertEqual(t, "pos[0].Symbol", result[0].Symbol, "AAPL")
	assertFloat(t, "pos[0].Quantity", result[0].Quantity, 100)
	assertEqual(t, "pos[1].Symbol", result[1].Symbol, "TSLA")
	assertFloat(t, "pos[1].Quantity", result[1].Quantity, 30)
}

func TestToOrderPositions_ZeroQuantity(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "FLAT", "side": "long", "qty": float64(0)},
	}
	result := toOrderPositions(raw)
	assertFloat(t, "zero qty stays zero", result[0].Quantity, 0)
}

// ---------------------------------------------------------------------------
// toBool
// ---------------------------------------------------------------------------

func TestToBool(t *testing.T) {
	cases := []struct {
		input interface{}
		want  bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"false", false},
		{"TRUE", true},
		{float64(1), true},
		{float64(0), false},
		{1, true},
		{0, false},
		{nil, false},
		{"invalid", false},
	}
	for _, tc := range cases {
		if got := toBool(tc.input); got != tc.want {
			t.Errorf("toBool(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}
