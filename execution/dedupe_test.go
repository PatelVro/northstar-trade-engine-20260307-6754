package execution

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// BuildDedupeKey — deterministic key composition for duplicate detection
// ---------------------------------------------------------------------------

func TestBuildDedupeKey_BasicFields(t *testing.T) {
	intent := Intent{
		TraderID:          "paper-1",
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          10,
		OrderType:         "market",
		IncreasesExposure: true,
	}
	key := BuildDedupeKey(intent)
	parts := strings.Split(key, "|")
	if len(parts) != 12 {
		t.Fatalf("expected 12 parts in dedupe key, got %d: %q", len(parts), key)
	}
	if parts[0] != "paper-1" {
		t.Errorf("expected trader_id=paper-1, got %q", parts[0])
	}
	if parts[1] != "AAPL" {
		t.Errorf("expected symbol=AAPL, got %q", parts[1])
	}
	if parts[2] != "buy" {
		t.Errorf("expected side=buy, got %q", parts[2])
	}
	if parts[3] != "open_long" {
		t.Errorf("expected action=open_long, got %q", parts[3])
	}
}

func TestBuildDedupeKey_NormalizesCase(t *testing.T) {
	a := BuildDedupeKey(Intent{Symbol: "aapl", Side: "BUY", ActionType: "OPEN_LONG", OrderType: "MARKET", TIF: "GTC"})
	b := BuildDedupeKey(Intent{Symbol: "AAPL", Side: "buy", ActionType: "open_long", OrderType: "market", TIF: "gtc"})
	if a != b {
		t.Errorf("expected case-insensitive match:\n  a=%q\n  b=%q", a, b)
	}
}

func TestBuildDedupeKey_TrimsWhitespace(t *testing.T) {
	a := BuildDedupeKey(Intent{TraderID: " paper ", Symbol: " AAPL ", Side: " buy ", ActionType: " open_long "})
	b := BuildDedupeKey(Intent{TraderID: "paper", Symbol: "AAPL", Side: "buy", ActionType: "open_long"})
	if a != b {
		t.Errorf("expected whitespace-trimmed match:\n  a=%q\n  b=%q", a, b)
	}
}

func TestBuildDedupeKey_ZeroFloat(t *testing.T) {
	key := BuildDedupeKey(Intent{Quantity: 0, LimitPrice: 0, StopPrice: 0})
	parts := strings.Split(key, "|")
	// Quantity=parts[4], LimitPrice=parts[6], StopPrice=parts[7]
	if parts[4] != "0" {
		t.Errorf("expected quantity=0, got %q", parts[4])
	}
	if parts[6] != "0" {
		t.Errorf("expected limit_price=0, got %q", parts[6])
	}
	if parts[7] != "0" {
		t.Errorf("expected stop_price=0, got %q", parts[7])
	}
}

func TestBuildDedupeKey_NonZeroFloat(t *testing.T) {
	key := BuildDedupeKey(Intent{Quantity: 1.5})
	if !strings.Contains(key, "1.50000000") {
		t.Errorf("expected formatted float, got %q", key)
	}
}

func TestBuildDedupeKey_BoolFields(t *testing.T) {
	keyTrue := BuildDedupeKey(Intent{IncreasesExposure: true, ReduceOnly: true})
	keyFalse := BuildDedupeKey(Intent{IncreasesExposure: false, ReduceOnly: false})
	if keyTrue == keyFalse {
		t.Error("expected bool fields to differentiate keys")
	}
	if !strings.Contains(keyTrue, "1|1|") {
		t.Errorf("expected true bools as '1', got %q", keyTrue)
	}
}

func TestBuildDedupeKey_DifferentQuantitiesProduceDifferentKeys(t *testing.T) {
	a := BuildDedupeKey(Intent{Symbol: "AAPL", Quantity: 10})
	b := BuildDedupeKey(Intent{Symbol: "AAPL", Quantity: 20})
	if a == b {
		t.Error("expected different keys for different quantities")
	}
}

func TestBuildDedupeKey_LocalRequestKeyPreserved(t *testing.T) {
	key := BuildDedupeKey(Intent{LocalRequestKey: "req-abc-123"})
	if !strings.Contains(key, "req-abc-123") {
		t.Errorf("expected local request key in dedupe key, got %q", key)
	}
}

// ---------------------------------------------------------------------------
// formatFloatKey
// ---------------------------------------------------------------------------

func TestFormatFloatKey_Zero(t *testing.T) {
	if got := formatFloatKey(0); got != "0" {
		t.Errorf("expected '0', got %q", got)
	}
}

func TestFormatFloatKey_Integer(t *testing.T) {
	got := formatFloatKey(100)
	if got != "100.00000000" {
		t.Errorf("expected '100.00000000', got %q", got)
	}
}

func TestFormatFloatKey_Fractional(t *testing.T) {
	got := formatFloatKey(3.14159)
	if !strings.HasPrefix(got, "3.1415") {
		t.Errorf("expected '3.1415...' prefix, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// boolKey
// ---------------------------------------------------------------------------

func TestBoolKey(t *testing.T) {
	if got := boolKey(true); got != "1" {
		t.Errorf("expected '1' for true, got %q", got)
	}
	if got := boolKey(false); got != "0" {
		t.Errorf("expected '0' for false, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// classifyAction
// ---------------------------------------------------------------------------

func TestClassifyAction_Entries(t *testing.T) {
	for _, action := range []string{"open_long", "open_short", "OPEN_LONG", " open_short "} {
		class, known := classifyAction(Intent{ActionType: action})
		if !known {
			t.Errorf("expected %q to be known", action)
		}
		if class != "entry" {
			t.Errorf("expected %q to classify as entry, got %q", action, class)
		}
	}
}

func TestClassifyAction_Exits(t *testing.T) {
	for _, action := range []string{"close_long", "close_short", "CLOSE_LONG", " close_short "} {
		class, known := classifyAction(Intent{ActionType: action})
		if !known {
			t.Errorf("expected %q to be known", action)
		}
		if class != "exit" {
			t.Errorf("expected %q to classify as exit, got %q", action, class)
		}
	}
}

func TestClassifyAction_Unknown(t *testing.T) {
	for _, action := range []string{"hold", "", "unknown", "rebalance"} {
		class, known := classifyAction(Intent{ActionType: action})
		if known {
			t.Errorf("expected %q to be unknown", action)
		}
		if class != "unknown" {
			t.Errorf("expected %q to classify as unknown, got %q", action, class)
		}
	}
}

// ---------------------------------------------------------------------------
// validateIntent — pre-execution validation gate
// ---------------------------------------------------------------------------

func TestValidateIntent_ValidMarketOrder(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "open_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected valid, got %q", msg)
	}
}

func TestValidateIntent_EmptySymbol(t *testing.T) {
	intent := Intent{Symbol: "", Quantity: 10, OrderType: "market"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	msg := validateIntent(intent, gate)
	if msg == "" || !strings.Contains(msg, "symbol") {
		t.Errorf("expected symbol error, got %q", msg)
	}
}

func TestValidateIntent_WhitespaceSymbol(t *testing.T) {
	intent := Intent{Symbol: "   ", Quantity: 10, OrderType: "market"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); msg == "" {
		t.Error("expected error for whitespace-only symbol")
	}
}

func TestValidateIntent_ZeroQuantity(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 0, OrderType: "market"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); !strings.Contains(msg, "positive") {
		t.Errorf("expected positive quantity error, got %q", msg)
	}
}

func TestValidateIntent_NegativeQuantity(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: -5, OrderType: "market"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); !strings.Contains(msg, "positive") {
		t.Errorf("expected positive quantity error, got %q", msg)
	}
}

func TestValidateIntent_UnsupportedOrderType(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "trailing_stop"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); !strings.Contains(msg, "unsupported") {
		t.Errorf("expected unsupported order type error, got %q", msg)
	}
}

func TestValidateIntent_EmptyOrderTypeDefaultsToMarket(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "", ActionType: "open_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected empty order type to default to market, got %q", msg)
	}
}

func TestValidateIntent_LimitOrderRequiresLimitPrice(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "limit", LimitPrice: 0}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); !strings.Contains(msg, "limit_price") {
		t.Errorf("expected limit price error, got %q", msg)
	}
}

func TestValidateIntent_LimitOrderWithPrice(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "limit", LimitPrice: 150.50, ActionType: "open_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected valid limit order, got %q", msg)
	}
}

func TestValidateIntent_StopOrderRequiresStopPrice(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "stop", StopPrice: 0}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); !strings.Contains(msg, "stop_price") {
		t.Errorf("expected stop price error, got %q", msg)
	}
}

func TestValidateIntent_StopLimitRequiresBothPrices(t *testing.T) {
	// Missing both
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "stop_limit"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	msg := validateIntent(intent, gate)
	if msg == "" {
		t.Error("expected error for stop_limit without prices")
	}

	// With both prices
	intent.LimitPrice = 150
	intent.StopPrice = 145
	intent.ActionType = "open_long"
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected valid stop_limit with prices, got %q", msg)
	}
}

func TestValidateIntent_TradingNotAllowed(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "open_long"}
	gate := Gate{TradingAllowed: false, BlockReason: "risk halt"}
	msg := validateIntent(intent, gate)
	if msg == "" {
		t.Fatal("expected blocked message")
	}
	if !strings.Contains(msg, "risk halt") {
		t.Errorf("expected block reason in message, got %q", msg)
	}
}

func TestValidateIntent_EntriesBlocked(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "open_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: false, ExitsAllowed: true, BlockReason: "reduce only mode"}
	msg := validateIntent(intent, gate)
	if msg == "" {
		t.Fatal("expected blocked message for entry")
	}
}

func TestValidateIntent_ExitsBlocked(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "close_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: false, BlockReason: "exits blocked"}
	msg := validateIntent(intent, gate)
	if msg == "" {
		t.Fatal("expected blocked message for exit")
	}
}

func TestValidateIntent_ExitAllowedWhenOnlyEntriesBlocked(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "close_long"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: false, ExitsAllowed: true, BlockReason: "reduce only"}
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected exit to be allowed, got %q", msg)
	}
}

func TestValidateIntent_UnknownActionWithPartialGate(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "rebalance"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: false, ExitsAllowed: true}
	msg := validateIntent(intent, gate)
	if msg == "" {
		t.Fatal("expected unknown action to be blocked when entries restricted")
	}
}

func TestValidateIntent_UnknownActionWithFullGate(t *testing.T) {
	intent := Intent{Symbol: "AAPL", Quantity: 10, OrderType: "market", ActionType: "rebalance"}
	gate := Gate{TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}
	if msg := validateIntent(intent, gate); msg != "" {
		t.Errorf("expected unknown action to pass with full gate, got %q", msg)
	}
}

// ---------------------------------------------------------------------------
// blockedReason
// ---------------------------------------------------------------------------

func TestBlockedReason_WithReason(t *testing.T) {
	got := blockedReason(Gate{BlockReason: "kill switch active"})
	if got != "kill switch active" {
		t.Errorf("expected reason, got %q", got)
	}
}

func TestBlockedReason_EmptyFallback(t *testing.T) {
	got := blockedReason(Gate{})
	if got != "execution blocked by final trading gate" {
		t.Errorf("expected fallback, got %q", got)
	}
}

func TestBlockedReason_WhitespaceFallback(t *testing.T) {
	got := blockedReason(Gate{BlockReason: "   "})
	if got != "execution blocked by final trading gate" {
		t.Errorf("expected fallback for whitespace, got %q", got)
	}
}
