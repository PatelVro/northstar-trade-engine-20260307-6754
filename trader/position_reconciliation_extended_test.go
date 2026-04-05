package trader

import (
	"math"
	"northstar/alerts"
	"northstar/logger"
	"northstar/positions"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// positionSnapshotsFromBrokerMaps — broker raw data → position snapshot
// ---------------------------------------------------------------------------

func TestPositionSnapshotsFromBrokerMaps_BasicMapping(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "AAPL", "side": "long", "positionAmt": 10.0, "entryPrice": 150.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(result))
	}
	assertStr(t, "Symbol", result[0].Symbol, "AAPL")
	assertStr(t, "Side", result[0].Side, "long")
	assertF64(t, "Quantity", result[0].Quantity, 10)
	assertF64(t, "EntryPrice", result[0].EntryPrice, 150)
}

func TestPositionSnapshotsFromBrokerMaps_NegativeQtyBecomesPositive(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "TSLA", "side": "short", "positionAmt": -50.0, "entryPrice": 200.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	assertF64(t, "abs quantity", result[0].Quantity, 50)
}

func TestPositionSnapshotsFromBrokerMaps_FallbackQuantityKey(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "MSFT", "side": "long", "quantity": 25.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 snapshot via fallback qty key")
	}
	assertF64(t, "Quantity via fallback", result[0].Quantity, 25)
}

func TestPositionSnapshotsFromBrokerMaps_FallbackEntryPriceKey(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "GOOG", "side": "long", "positionAmt": 5.0, "entry_price": 170.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	assertF64(t, "EntryPrice via fallback", result[0].EntryPrice, 170)
}

func TestPositionSnapshotsFromBrokerMaps_ExplicitEmptySymbolSkipped(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "", "side": "long", "positionAmt": 10.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 0 {
		t.Fatalf("expected 0 snapshots for explicit empty symbol, got %d", len(result))
	}
}

func TestPositionSnapshotsFromBrokerMaps_ExplicitEmptySideSkipped(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "AAPL", "side": "", "positionAmt": 10.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 0 {
		t.Fatalf("expected 0 snapshots for explicit empty side, got %d", len(result))
	}
}

func TestPositionSnapshotsFromBrokerMaps_SkipsZeroQuantity(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "AAPL", "side": "long", "positionAmt": 0.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 0 {
		t.Fatalf("expected 0 snapshots for zero qty, got %d", len(result))
	}
}

func TestPositionSnapshotsFromBrokerMaps_SymbolUppercasedSideLowercased(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": " aapl ", "side": " LONG ", "positionAmt": 10.0},
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	assertStr(t, "Symbol", result[0].Symbol, "AAPL")
	assertStr(t, "Side", result[0].Side, "long")
}

func TestPositionSnapshotsFromBrokerMaps_MultiplePositions(t *testing.T) {
	raw := []map[string]interface{}{
		{"symbol": "AAPL", "side": "long", "positionAmt": 10.0},
		{"symbol": "TSLA", "side": "short", "positionAmt": -5.0},
		{"symbol": "", "side": "long", "positionAmt": 3.0}, // skipped
	}
	result := positionSnapshotsFromBrokerMaps(raw)
	if len(result) != 2 {
		t.Fatalf("expected 2 valid snapshots, got %d", len(result))
	}
}

func TestPositionSnapshotsFromBrokerMaps_EmptyInput(t *testing.T) {
	result := positionSnapshotsFromBrokerMaps(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// positionReconciliationInterval — timing bounds
// ---------------------------------------------------------------------------

func TestPositionReconciliationInterval_DefaultsTo15s(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{ScanInterval: 0}}
	got := at.positionReconciliationInterval()
	if got != 15*time.Second {
		t.Fatalf("expected 15s default, got %v", got)
	}
}

func TestPositionReconciliationInterval_HalfOfScanInterval(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{ScanInterval: 20 * time.Second}}
	got := at.positionReconciliationInterval()
	if got != 10*time.Second {
		t.Fatalf("expected 10s (half of 20s), got %v", got)
	}
}

func TestPositionReconciliationInterval_MinimumFiveSeconds(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{ScanInterval: 4 * time.Second}}
	got := at.positionReconciliationInterval()
	if got != 5*time.Second {
		t.Fatalf("expected 5s minimum, got %v", got)
	}
}

func TestPositionReconciliationInterval_Maximum30Seconds(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{ScanInterval: 120 * time.Second}}
	got := at.positionReconciliationInterval()
	if got != 30*time.Second {
		t.Fatalf("expected 30s maximum, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// applyLocalPositionOpenLocked — position accumulation + VWAP
// ---------------------------------------------------------------------------

func TestApplyLocalPositionOpen_NewPosition(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.applyLocalPositionOpenLocked("AAPL", "long", 10, 150, time.Now())

	key := positions.Key("AAPL", "long")
	snap, ok := at.localPositionSnapshots[key]
	if !ok {
		t.Fatal("expected position to exist")
	}
	assertF64(t, "Quantity", snap.Quantity, 10)
	assertF64(t, "EntryPrice", snap.EntryPrice, 150)
}

func TestApplyLocalPositionOpen_AddToExisting_VWAP(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.localPositionSnapshots[positions.Key("AAPL", "long")] = positions.Snapshot{
		Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150,
	}

	at.applyLocalPositionOpenLocked("AAPL", "long", 10, 160, time.Now())

	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertF64(t, "Quantity", snap.Quantity, 20)
	// VWAP: (150*10 + 160*10) / 20 = 155
	assertF64(t, "VWAP EntryPrice", snap.EntryPrice, 155)
}

func TestApplyLocalPositionOpen_ZeroQuantityIgnored(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.applyLocalPositionOpenLocked("AAPL", "long", 0, 150, time.Now())

	if len(at.localPositionSnapshots) != 0 {
		t.Fatal("expected no position for zero qty")
	}
}

func TestApplyLocalPositionOpen_NegativeQuantityUsesAbsValue(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.applyLocalPositionOpenLocked("AAPL", "long", -10, 150, time.Now())

	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertF64(t, "abs Quantity", snap.Quantity, 10)
}

// ---------------------------------------------------------------------------
// applyLocalPositionCloseLocked — partial/full close
// ---------------------------------------------------------------------------

func TestApplyLocalPositionClose_FullClose(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.localPositionSnapshots[positions.Key("AAPL", "long")] = positions.Snapshot{
		Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150,
	}

	at.applyLocalPositionCloseLocked("AAPL", "long", 10, time.Now())

	if _, ok := at.localPositionSnapshots[positions.Key("AAPL", "long")]; ok {
		t.Fatal("expected position to be removed on full close")
	}
}

func TestApplyLocalPositionClose_PartialClose(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.localPositionSnapshots[positions.Key("AAPL", "long")] = positions.Snapshot{
		Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150,
	}

	at.applyLocalPositionCloseLocked("AAPL", "long", 3, time.Now())

	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertF64(t, "remaining Quantity", snap.Quantity, 7)
}

func TestApplyLocalPositionClose_CloseMoreThanHeldRemovesPosition(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.localPositionSnapshots[positions.Key("AAPL", "long")] = positions.Snapshot{
		Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150,
	}

	at.applyLocalPositionCloseLocked("AAPL", "long", 15, time.Now())

	if _, ok := at.localPositionSnapshots[positions.Key("AAPL", "long")]; ok {
		t.Fatal("expected position to be removed when close qty >= held qty")
	}
}

func TestApplyLocalPositionClose_NonexistentPositionIsNoop(t *testing.T) {
	at := newMinimalReconAutoTrader()
	// Should not panic
	at.applyLocalPositionCloseLocked("AAPL", "long", 10, time.Now())
	if len(at.localPositionSnapshots) != 0 {
		t.Fatal("expected no positions")
	}
}

func TestApplyLocalPositionClose_NearFullCloseRemoves(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.localPositionSnapshots[positions.Key("AAPL", "long")] = positions.Snapshot{
		Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150,
	}

	// Close qty within 0.01 tolerance of full amount → removes
	at.applyLocalPositionCloseLocked("AAPL", "long", 9.995, time.Now())

	if _, ok := at.localPositionSnapshots[positions.Key("AAPL", "long")]; ok {
		t.Fatal("expected position removed (within tolerance of full close)")
	}
}

// ---------------------------------------------------------------------------
// updateLocalPositionStateFromActions — action filtering
// ---------------------------------------------------------------------------

func TestUpdateLocalPositionStateFromActions_AppliesFilledOpenLong(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.config.Mode = "paper"
	at.config.Broker = "ibkr"
	at.initializePositionReconciliationState()

	at.updateLocalPositionStateFromActions([]logger.DecisionAction{
		{Action: "open_long", Symbol: "AAPL", Success: true, OrderStatus: "filled", Quantity: 10, Price: 150},
	})

	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertF64(t, "Quantity", snap.Quantity, 10)
}

func TestUpdateLocalPositionStateFromActions_IgnoresUnsuccessful(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.config.Mode = "paper"
	at.config.Broker = "ibkr"
	at.initializePositionReconciliationState()

	at.updateLocalPositionStateFromActions([]logger.DecisionAction{
		{Action: "open_long", Symbol: "AAPL", Success: false, OrderStatus: "rejected", Quantity: 10},
	})

	if len(at.localPositionSnapshots) != 0 {
		t.Fatal("expected no positions from unsuccessful action")
	}
}

func TestUpdateLocalPositionStateFromActions_IgnoresPendingStatus(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.config.Mode = "paper"
	at.config.Broker = "ibkr"
	at.initializePositionReconciliationState()

	for _, status := range []string{"submitted", "accepted", "acknowledged", "pending"} {
		at.updateLocalPositionStateFromActions([]logger.DecisionAction{
			{Action: "open_long", Symbol: "AAPL", Success: true, OrderStatus: status, Quantity: 10},
		})
	}

	if len(at.localPositionSnapshots) != 0 {
		t.Fatal("expected no positions from pending-status actions")
	}
}

func TestUpdateLocalPositionStateFromActions_AppliesCloseShort(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.config.Mode = "paper"
	at.config.Broker = "ibkr"
	at.initializePositionReconciliationState()
	at.localPositionSnapshots[positions.Key("TSLA", "short")] = positions.Snapshot{
		Symbol: "TSLA", Side: "short", Quantity: 20, EntryPrice: 200,
	}

	at.updateLocalPositionStateFromActions([]logger.DecisionAction{
		{Action: "close_short", Symbol: "TSLA", Success: true, OrderStatus: "filled", Quantity: 20},
	})

	if _, ok := at.localPositionSnapshots[positions.Key("TSLA", "short")]; ok {
		t.Fatal("expected position removed after close_short")
	}
}

func TestUpdateLocalPositionStateFromActions_IgnoresEmptySymbol(t *testing.T) {
	at := newMinimalReconAutoTrader()
	at.config.Mode = "paper"
	at.config.Broker = "ibkr"
	at.initializePositionReconciliationState()

	at.updateLocalPositionStateFromActions([]logger.DecisionAction{
		{Action: "open_long", Symbol: "", Success: true, OrderStatus: "filled", Quantity: 10},
	})

	if len(at.localPositionSnapshots) != 0 {
		t.Fatal("expected no positions for empty symbol")
	}
}

// ---------------------------------------------------------------------------
// managesPositionReconciliation — mode/broker gating
// ---------------------------------------------------------------------------

func TestManagesPositionReconciliation_PaperIBKR(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{Mode: "paper", Broker: "ibkr"}, trader: &positionReconTestTrader{}}
	if !at.managesPositionReconciliation() {
		t.Fatal("expected paper+ibkr to manage position reconciliation")
	}
}

func TestManagesPositionReconciliation_DemoMode(t *testing.T) {
	at := &AutoTrader{demoMode: true, config: AutoTraderConfig{Mode: "paper", Broker: "ibkr"}, trader: &positionReconTestTrader{}}
	if at.managesPositionReconciliation() {
		t.Fatal("expected demo mode to skip reconciliation")
	}
}

func TestManagesPositionReconciliation_ReplayMode(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{Mode: "replay", Broker: "ibkr"}, trader: &positionReconTestTrader{}}
	if at.managesPositionReconciliation() {
		t.Fatal("expected replay mode to skip reconciliation")
	}
}

func TestManagesPositionReconciliation_SimBroker(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{Mode: "paper", Broker: "sim"}, trader: &positionReconTestTrader{}}
	if at.managesPositionReconciliation() {
		t.Fatal("expected sim broker to skip reconciliation")
	}
}

func TestManagesPositionReconciliation_NilTrader(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{Mode: "paper", Broker: "ibkr"}}
	if at.managesPositionReconciliation() {
		t.Fatal("expected nil trader to skip reconciliation")
	}
}

// ---------------------------------------------------------------------------
// positionStringValue
// ---------------------------------------------------------------------------

func TestPositionStringValue_String(t *testing.T) {
	assertStr(t, "string", positionStringValue("AAPL"), "AAPL")
}

func TestPositionStringValue_Nil(t *testing.T) {
	got := positionStringValue(nil)
	if got != "<nil>" {
		t.Fatalf("expected '<nil>', got %q", got)
	}
}

func TestPositionStringValue_Number(t *testing.T) {
	got := positionStringValue(42)
	if got != "42" {
		t.Fatalf("expected '42', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// errStringValue
// ---------------------------------------------------------------------------

func TestErrStringValue_Nil(t *testing.T) {
	assertStr(t, "nil error", errStringValue(nil), "")
}

func TestErrStringValue_Error(t *testing.T) {
	got := errStringValue(testError("connection timeout"))
	if got != "connection timeout" {
		t.Fatalf("expected error message, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// setLocalPositionSnapshots — normalization and filtering
// ---------------------------------------------------------------------------

func TestSetLocalPositionSnapshots_NormalizesAndKeys(t *testing.T) {
	at := newMinimalReconAutoTrader()
	snapshots := []positions.Snapshot{
		{Symbol: "  aapl  ", Side: " LONG ", Quantity: 10, EntryPrice: 150},
		{Symbol: "TSLA", Side: "short", Quantity: -5, EntryPrice: 200},
	}
	at.setLocalPositionSnapshots(snapshots, "test", time.Now())

	if len(at.localPositionSnapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(at.localPositionSnapshots))
	}
	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertStr(t, "normalized symbol", snap.Symbol, "AAPL")
	assertStr(t, "normalized side", snap.Side, "long")
}

func TestSetLocalPositionSnapshots_SkipsEmptySymbolOrSide(t *testing.T) {
	at := newMinimalReconAutoTrader()
	snapshots := []positions.Snapshot{
		{Symbol: "", Side: "long", Quantity: 10},
		{Symbol: "AAPL", Side: "", Quantity: 10},
		{Symbol: "MSFT", Side: "long", Quantity: 5},
	}
	at.setLocalPositionSnapshots(snapshots, "test", time.Now())

	if len(at.localPositionSnapshots) != 1 {
		t.Fatalf("expected 1 snapshot (skipping empty symbol/side), got %d", len(at.localPositionSnapshots))
	}
}

func TestSetLocalPositionSnapshots_SetsSourceAndTimestamp(t *testing.T) {
	at := newMinimalReconAutoTrader()
	now := time.Now()
	snapshots := []positions.Snapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 10},
	}
	at.setLocalPositionSnapshots(snapshots, "broker_recon", now)

	snap := at.localPositionSnapshots[positions.Key("AAPL", "long")]
	assertStr(t, "Source", snap.Source, "broker_recon")
	if snap.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newMinimalReconAutoTrader() *AutoTrader {
	at := &AutoTrader{
		id:                     "test",
		name:                   "Test",
		trader:                 &positionReconTestTrader{},
		alertManager:           alerts.NewManager(),
		config:                 AutoTraderConfig{ID: "test", Name: "Test", Mode: "paper", Broker: "ibkr"},
		localPositionSnapshots: make(map[string]positions.Snapshot),
	}
	at.isRunning.Store(true)
	return at
}

type testError string

func (e testError) Error() string { return string(e) }

func assertStr(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", name, got, want)
	}
}

func assertF64(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", name, got, want)
	}
}
