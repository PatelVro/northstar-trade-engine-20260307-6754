package execution

import (
	"fmt"
	"northstar/orders"
	"testing"
	"time"
)

type testBroker struct {
	order  map[string]interface{}
	err    error
	calls  int
	lastOp string
}

func (b *testBroker) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	b.calls++
	b.lastOp = "open_long"
	return cloneOrderMap(b.order), b.err
}

func (b *testBroker) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	b.calls++
	b.lastOp = "open_short"
	return cloneOrderMap(b.order), b.err
}

func (b *testBroker) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	b.calls++
	b.lastOp = "close_long"
	return cloneOrderMap(b.order), b.err
}

func (b *testBroker) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	b.calls++
	b.lastOp = "close_short"
	return cloneOrderMap(b.order), b.err
}

type testLookup struct {
	record *orders.Record
}

func (l *testLookup) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	if l.record == nil {
		return nil
	}
	cloned := *l.record
	return &cloned
}

func TestDuplicateIntentSuppressedWithinWindow(t *testing.T) {
	manager := NewManager(Config{DedupeWindow: time.Minute, StaleAfter: 5 * time.Minute})
	broker := &testBroker{
		order: map[string]interface{}{
			"status":       "submitted",
			"localOrderId": "local-1",
			"orderId":      int64(101),
		},
	}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          10,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}
	gate := Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}

	first := manager.Execute(intent, gate, broker)
	second := manager.Execute(intent, gate, broker)

	if first.Status != StatusSubmitted {
		t.Fatalf("expected first execution to be submitted, got %s", first.Status)
	}
	if second.Status != StatusDuplicateSuppressed {
		t.Fatalf("expected duplicate suppression, got %s", second.Status)
	}
	if broker.calls != 1 {
		t.Fatalf("expected broker to be called once, got %d", broker.calls)
	}
}

func TestRestrictedModeBlocksExposureIncreasingExecution(t *testing.T) {
	manager := NewManager(Config{})
	broker := &testBroker{}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "MSFT",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          5,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}
	gate := Gate{Mode: "reduce_only", TradingAllowed: true, EntriesAllowed: false, ExitsAllowed: true, ReduceOnly: true, BlockReason: "risk supervisor reduce_only"}

	result := manager.Execute(intent, gate, broker)
	if result.Status != StatusBlocked {
		t.Fatalf("expected blocked execution, got %s", result.Status)
	}
	if broker.calls != 0 {
		t.Fatalf("expected broker not to be called, got %d", broker.calls)
	}
}

func TestExitAllowedInBlockNewEntries(t *testing.T) {
	manager := NewManager(Config{})
	broker := &testBroker{
		order: map[string]interface{}{
			"status":     "FILLED",
			"orderId":    int64(88),
			"filled_qty": 3.0,
			"price":      101.5,
		},
	}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "NVDA",
		Side:              "sell",
		ActionType:        "close_long",
		Quantity:          3,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: false,
		ReduceOnly:        true,
	}
	gate := Gate{Mode: "block_new_entries", TradingAllowed: true, EntriesAllowed: false, ExitsAllowed: true}

	result := manager.Execute(intent, gate, broker)
	if result.Status != StatusFilled {
		t.Fatalf("expected exit to be filled, got %s", result.Status)
	}
	if broker.calls != 1 {
		t.Fatalf("expected broker call, got %d", broker.calls)
	}
}

func TestBrokerRejectMapsToRejected(t *testing.T) {
	manager := NewManager(Config{})
	broker := &testBroker{err: fmt.Errorf("order rejected by broker")}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "AMD",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          4,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}
	gate := Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}

	result := manager.Execute(intent, gate, broker)
	if result.Status != StatusRejected {
		t.Fatalf("expected rejected status, got %s", result.Status)
	}
}

func TestStaleDetectionUsesOrderLookup(t *testing.T) {
	manager := NewManager(Config{DedupeWindow: time.Second, StaleAfter: time.Second, MaxHistory: 10})
	broker := &testBroker{
		order: map[string]interface{}{
			"status":       "submitted",
			"localOrderId": "local-1",
			"orderId":      int64(1001),
		},
	}
	lookup := &testLookup{}
	manager.SetOrderLookup(lookup)

	intent := Intent{
		TraderID:          "paper",
		Symbol:            "SHOP",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          2,
		OrderType:         "market",
		CreatedAt:         time.Now().Add(-2 * time.Second).UTC(),
		IncreasesExposure: true,
	}
	gate := Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}

	result := manager.Execute(intent, gate, broker)
	if result.Status != StatusSubmitted {
		t.Fatalf("expected submitted, got %s", result.Status)
	}

	summary := manager.Summary()
	if summary.StaleCount != 1 {
		t.Fatalf("expected stale count 1 without broker truth, got %d", summary.StaleCount)
	}

	lookup.record = &orders.Record{
		LocalID:       "local-1",
		BrokerOrderID: "1001",
		Status:        orders.StatusFilled,
		FilledQty:     2,
		AvgFillPrice:  99.5,
		UpdatedAt:     time.Now().UTC(),
	}
	summary = manager.Summary()
	if summary.StaleCount != 0 {
		t.Fatalf("expected stale count to clear after broker truth, got %d", summary.StaleCount)
	}
	if summary.FilledCount != 1 {
		t.Fatalf("expected filled count 1 after broker truth, got %d", summary.FilledCount)
	}
}

func TestBlockedIntentDoesNotPoisonDuplicateSuppression(t *testing.T) {
	manager := NewManager(Config{DedupeWindow: time.Minute, StaleAfter: time.Minute})
	broker := &testBroker{
		order: map[string]interface{}{
			"status":  "FILLED",
			"orderId": int64(202),
		},
	}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          10,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}

	blocked := manager.Execute(intent, Gate{Mode: "halted", TradingAllowed: false, EntriesAllowed: false, ExitsAllowed: false, BlockReason: "risk supervisor halted trading"}, broker)
	if blocked.Status != StatusBlocked {
		t.Fatalf("expected blocked status, got %s", blocked.Status)
	}

	allowed := manager.Execute(intent, Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}, broker)
	if allowed.Status != StatusFilled {
		t.Fatalf("expected later allowed intent to submit cleanly, got %s", allowed.Status)
	}
	if broker.calls != 1 {
		t.Fatalf("expected broker call after gate cleared, got %d", broker.calls)
	}
}

func TestManagerStateRoundTripPreservesDuplicateProtection(t *testing.T) {
	manager := NewManager(Config{DedupeWindow: time.Minute, StaleAfter: 5 * time.Minute, MaxHistory: 10})
	broker := &testBroker{
		order: map[string]interface{}{
			"status":       "submitted",
			"localOrderId": "local-1",
			"orderId":      int64(101),
		},
	}
	intent := Intent{
		TraderID:          "paper",
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          10,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}
	gate := Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}

	first := manager.Execute(intent, gate, broker)
	if first.Status != StatusSubmitted {
		t.Fatalf("expected submitted status before snapshot, got %s", first.Status)
	}

	state := manager.SnapshotState()
	restored := NewManager(Config{DedupeWindow: time.Minute, StaleAfter: 5 * time.Minute, MaxHistory: 10})
	if err := restored.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	second := restored.Execute(intent, gate, broker)
	if second.Status != StatusDuplicateSuppressed {
		t.Fatalf("expected duplicate suppression after restore, got %s", second.Status)
	}
	if broker.calls != 1 {
		t.Fatalf("expected broker to be called once across restore, got %d", broker.calls)
	}
}

func TestRestoreStateRejectsUnsupportedVersion(t *testing.T) {
	manager := NewManager(Config{})
	err := manager.RestoreState(ManagerState{Version: 99})
	if err == nil {
		t.Fatalf("expected unsupported version error")
	}
}

func cloneOrderMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
