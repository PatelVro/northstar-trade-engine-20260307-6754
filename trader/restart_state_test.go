package trader

import (
	"encoding/json"
	"fmt"
	"northstar/alerts"
	"northstar/audit"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/orders"
	"northstar/positions"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type restartStateTestTrader struct {
	balance    map[string]interface{}
	positions  []map[string]interface{}
	orderStore *orders.Store
}

func (t *restartStateTestTrader) GetBalance() (map[string]interface{}, error) {
	if t.balance == nil {
		return map[string]interface{}{"accountEquity": 100000.0, "availableBalance": 100000.0}, nil
	}
	out := make(map[string]interface{}, len(t.balance))
	for key, value := range t.balance {
		out[key] = value
	}
	return out, nil
}

func (t *restartStateTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return clonePositionMaps(t.positions), nil
}

func (t *restartStateTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	localID := t.orderStore.RegisterSubmitted(orders.IntentEntryLong, symbol, "BUY", "long", quantity, time.Now().UTC())
	return map[string]interface{}{"status": "submitted", "localOrderId": localID, "orderId": int64(101)}, nil
}

func (t *restartStateTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	localID := t.orderStore.RegisterSubmitted(orders.IntentEntryShort, symbol, "SELL", "short", quantity, time.Now().UTC())
	return map[string]interface{}{"status": "submitted", "localOrderId": localID, "orderId": int64(102)}, nil
}

func (t *restartStateTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	localID := t.orderStore.RegisterSubmitted(orders.IntentExitLong, symbol, "SELL", "long", quantity, time.Now().UTC())
	return map[string]interface{}{"status": "submitted", "localOrderId": localID, "orderId": int64(103)}, nil
}

func (t *restartStateTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	localID := t.orderStore.RegisterSubmitted(orders.IntentExitShort, symbol, "BUY", "short", quantity, time.Now().UTC())
	return map[string]interface{}{"status": "submitted", "localOrderId": localID, "orderId": int64(104)}, nil
}

func (t *restartStateTestTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *restartStateTestTrader) GetMarketPrice(symbol string) (float64, error) { return 100, nil }
func (t *restartStateTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (t *restartStateTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (t *restartStateTestTrader) CancelAllOrders(symbol string) error { return nil }
func (t *restartStateTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}
func (t *restartStateTestTrader) SnapshotOrderStoreState() orders.StoreState {
	return t.orderStore.SnapshotState()
}
func (t *restartStateTestTrader) RestoreOrderStoreState(state orders.StoreState) error {
	return t.orderStore.RestoreState(state)
}
func (t *restartStateTestTrader) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	return t.orderStore.Lookup(localID, brokerOrderID)
}

func (t *restartStateTestTrader) GetOrderReconciliationSummary() orders.Summary {
	if t.orderStore == nil {
		return orders.Summary{}
	}
	return t.orderStore.SnapshotSummary()
}

func newRestartStateTestAutoTrader(cfg AutoTraderConfig, tr *restartStateTestTrader) *AutoTrader {
	at := &AutoTrader{
		id:               cfg.ID,
		name:             cfg.Name,
		exchange:         cfg.Exchange,
		config:           cfg,
		trader:           tr,
		executionManager: execution.NewManager(execution.Config{DedupeWindow: time.Minute, StaleAfter: 5 * time.Minute, MaxHistory: 32}),
		alertManager:     alerts.NewManager(),
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     cfg.ID,
			TraderName:   cfg.Name,
			Mode:         cfg.Mode,
			Broker:       cfg.Broker,
			StrategyMode: cfg.StrategyMode,
		}),
		initialBalance:     100000,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
		isRunning:          true,
	}
	at.executionManager.SetOrderLookup(tr)
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()
	at.initializePromotionSummary()
	at.initializeKillSwitchState()
	at.initializeDataQualityState()
	at.initializePositionReconciliationState()
	at.initializeRiskSupervisorState()
	at.initializeShadowModeState()
	at.initializeRestartRecoveryState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: time.Now()})
	return at
}

func withTempWorkingDir(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}

func TestDurableRuntimeStateRoundTripRestoresShadowPortfolio(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "shadow_restart", Name: "Shadow Restart", Mode: "shadow", Broker: "sim", Exchange: "ibkr", StrategyMode: "momentum_fallback"}
	trader := &restartStateTestTrader{orderStore: orders.NewStore()}
	at := newRestartStateTestAutoTrader(cfg, trader)
	at.setLatestAccountSummary(&AccountSummary{StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000, AccountingVersion: accountingVersion})

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL", Price: 100, Quantity: 10}
	at.observeShadowExecution(&decision.Decision{Symbol: "AAPL", Action: "open_long"}, actionRecord, execution.Intent{Symbol: "AAPL", ActionType: "open_long", Quantity: 10}, execution.Result{Status: execution.StatusFilled, AverageFillPrice: 100, FillQuantity: 10}, 100)
	at.persistDurableRuntimeState("test_roundtrip")

	restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
	restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
	restored.restoreDurableRuntimeState()

	summary := restored.currentRestartRecoverySummary()
	if !summary.Restored {
		t.Fatalf("expected durable runtime state to be restored, got %+v", summary)
	}
	if summary.TradingBlocked {
		t.Fatalf("expected shadow restore not to block trading, got %+v", summary)
	}

	positions, err := restored.GetPositions()
	if err != nil {
		t.Fatalf("expected restored shadow positions, got %v", err)
	}
	if len(positions) != 1 || positions[0]["symbol"] != "AAPL" {
		t.Fatalf("unexpected restored shadow positions: %#v", positions)
	}
}

func TestRestartRecoveryBlocksTradingUntilBrokerReconciliation(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "paper_restart", Name: "Paper Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &restartStateTestTrader{orderStore: orders.NewStore()}
	at := newRestartStateTestAutoTrader(cfg, trader)
	at.setLatestAccountSummary(&AccountSummary{StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000, AccountingVersion: accountingVersion})
	at.setLocalPositionSnapshots([]positions.Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 5, EntryPrice: 100}}, "test", time.Now().UTC())

	result := at.executionManager.Execute(execution.Intent{
		TraderID:          at.id,
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          5,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}, execution.Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}, trader)
	if result.Status != execution.StatusSubmitted {
		t.Fatalf("expected submitted execution, got %s", result.Status)
	}
	at.persistDurableRuntimeState("test_pending_reconciliation")

	restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
	restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
	restored.restoreDurableRuntimeState()

	summary := restored.currentRestartRecoverySummary()
	if !summary.PendingReconciliation || !summary.TradingBlocked {
		t.Fatalf("expected pending reconciliation to block trading after restore, got %+v", summary)
	}
	if check := restored.checkRestartRecoveryReadiness(); check.Status != ReadinessWarn {
		t.Fatalf("expected restart recovery readiness warning while pending reconciliation, got %+v", check)
	}
	gate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
	if gate.TradingAllowed {
		t.Fatalf("expected trading gate to block while recovery is pending, got %+v", gate)
	}
}

func TestRestoreDurableRuntimeStateFailsSafeOnCorruptFile(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "corrupt_restart", Name: "Corrupt Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	path := filepath.Join("output", "state", cfg.ID, "runtime_state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
	restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
	restored.restoreDurableRuntimeState()

	summary := restored.currentRestartRecoverySummary()
	if !summary.TradingBlocked || !summary.Corrupt {
		t.Fatalf("expected corrupt durable state to block trading, got %+v", summary)
	}
	if check := restored.checkRestartRecoveryReadiness(); check.Status != ReadinessFail {
		t.Fatalf("expected readiness failure for corrupt state, got %+v", check)
	}
	events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", cfg.ID, "events.jsonl"))
	if len(events) == 0 {
		t.Fatalf("expected restart recovery journal event")
	}
	last := events[len(events)-1]
	if last.Type != "restart_state_restore_failed" {
		t.Fatalf("expected restart_state_restore_failed journal event, got %q", last.Type)
	}
}

func TestDurableRuntimeStatePreservesUnresolvedOrderTruth(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "unresolved_restart", Name: "Unresolved Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &restartStateTestTrader{orderStore: orders.NewStore()}
	at := newRestartStateTestAutoTrader(cfg, trader)
	localID := trader.orderStore.RegisterSubmitted(orders.IntentEntryLong, "AAPL", "BUY", "long", 10, time.Now().Add(-time.Minute).UTC())
	state := trader.orderStore.SnapshotState()
	for idx := range state.Orders {
		if state.Orders[idx].LocalID != localID {
			continue
		}
		state.Orders[idx].Status = orders.StatusUnknown
		state.Orders[idx].TruthAuthority = orders.TruthAuthorityUnresolved
		state.Orders[idx].TruthConfidence = orders.TruthConfidenceUnresolved
		state.Orders[idx].TruthReason = "execution truth unresolved pending broker follow-up"
		state.Orders[idx].NeedsReview = true
	}
	if err := trader.orderStore.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}
	at.persistDurableRuntimeState("test_unresolved_truth")

	restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
	restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
	restored.restoreDurableRuntimeState()

	record := restoredTrader.orderStore.Lookup(localID, "")
	if record == nil {
		t.Fatalf("expected unresolved order to restore")
	}
	if record.TruthAuthority != orders.TruthAuthorityUnresolved || record.TruthConfidence != orders.TruthConfidenceUnresolved || !record.NeedsReview {
		t.Fatalf("expected unresolved truth metadata after restore, got %+v", record)
	}
}

func TestRestartStateAtomicWriteNoTempFileOnSuccess(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "atomic_write", Name: "Atomic Write", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &restartStateTestTrader{orderStore: orders.NewStore()}
	at := newRestartStateTestAutoTrader(cfg, trader)
	at.setLatestAccountSummary(&AccountSummary{StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000, AccountingVersion: accountingVersion})

	at.persistDurableRuntimeState("test_atomic_write")

	statePath := at.durableRuntimeStatePath()

	// The final state file must exist.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("expected state file to exist after persist: %v", err)
	}

	// The temp file must not persist after a successful write.
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file %s to be removed after successful write, but it exists", tmpPath)
	}

	// The written file must be valid JSON.
	var state durableRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("expected valid JSON in state file, got unmarshal error: %v", err)
	}
	if state.TraderID != cfg.ID {
		t.Fatalf("expected trader_id %q in state file, got %q", cfg.ID, state.TraderID)
	}
	if state.Version != durableRuntimeStateVersion {
		t.Fatalf("expected version %d, got %d", durableRuntimeStateVersion, state.Version)
	}
}

func TestDurableRuntimeStatePreservesInferredOrderTruthRestriction(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "inferred_restart", Name: "Inferred Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &restartStateTestTrader{orderStore: orders.NewStore()}
	at := newRestartStateTestAutoTrader(cfg, trader)
	localID := trader.orderStore.RegisterSubmitted(orders.IntentEntryLong, "AAPL", "BUY", "long", 10, time.Now().Add(-time.Minute).UTC())
	state := trader.orderStore.SnapshotState()
	for idx := range state.Orders {
		if state.Orders[idx].LocalID != localID {
			continue
		}
		state.Orders[idx].Status = orders.StatusFilled
		state.Orders[idx].TruthAuthority = orders.TruthAuthorityReconciliationInferred
		state.Orders[idx].TruthConfidence = orders.TruthConfidenceHigh
		state.Orders[idx].TruthReason = "entry order inferred from broker position evidence"
		state.Orders[idx].NeedsReview = true
	}
	state.Summary.CurrentInferredOrders = 1
	state.Summary.ConfidenceDegraded = true
	state.Summary.LastRunAt = time.Now().UTC()
	state.Summary.LastSuccessAt = state.Summary.LastRunAt
	state.Summary.LastSummary = "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0"
	state.Summary.LastIssues = []orders.Issue{{
		LocalID:     localID,
		Message:     "entry order inferred from broker position evidence",
		Authority:   orders.TruthAuthorityReconciliationInferred,
		Confidence:  orders.TruthConfidenceHigh,
		NeedsReview: true,
		Repaired:    true,
	}}
	if err := trader.orderStore.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}
	at.persistDurableRuntimeState("test_inferred_truth")

	restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
	restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
	restored.positionReconSummary = freshPositionReconSummary(time.Now())
	restored.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          1,
	}, []map[string]interface{}{})
	restored.restoreDurableRuntimeState()
	restored.resolveRestartRecoveryAfterBrokerReconciliation("test_restore")

	record := restoredTrader.orderStore.Lookup(localID, "")
	if record == nil {
		t.Fatalf("expected inferred order to restore")
	}
	if record.TruthAuthority != orders.TruthAuthorityReconciliationInferred || record.TruthConfidence != orders.TruthConfidenceHigh || !record.NeedsReview {
		t.Fatalf("expected inferred truth metadata after restore, got %+v", record)
	}
	gate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
	if !gate.TradingAllowed || gate.EntriesAllowed || !gate.ExitsAllowed || !gate.ReduceOnly {
		t.Fatalf("expected restored inferred truth to restrict entries, got %+v", gate)
	}
}
