package trader

import (
	"encoding/json"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/orders"
	"northstar/positions"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type restartValidationSnapshot struct {
	Scenario              string `json:"scenario"`
	RestartStatus         string `json:"restart_status"`
	RuntimeState          string `json:"runtime_state"`
	RuntimeSeverity       string `json:"runtime_severity"`
	CycleTradable         bool   `json:"cycle_tradable"`
	TradingAllowed        bool   `json:"trading_allowed"`
	EntriesAllowed        bool   `json:"entries_allowed"`
	ExitsAllowed          bool   `json:"exits_allowed"`
	ReduceOnly            bool   `json:"reduce_only"`
	BrokerTruthVerified   bool   `json:"broker_truth_verified"`
	BrokerTruthBlocked    bool   `json:"broker_truth_blocked"`
	BrokerTruthRestricted bool   `json:"broker_truth_restricted"`
	JournalEventCount     int    `json:"journal_event_count"`
	LastJournalEventType  string `json:"last_journal_event_type"`
}

func logRestartValidationSnapshot(t *testing.T, scenario string, at *AutoTrader) {
	t.Helper()
	status := at.GetOperatorStatus()
	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	snapshot := restartValidationSnapshot{
		Scenario:              strings.TrimSpace(scenario),
		RestartStatus:         status.RestartRecovery.Status,
		RuntimeState:          string(status.RuntimeCondition.State),
		RuntimeSeverity:       string(status.RuntimeCondition.Severity),
		CycleTradable:         status.RuntimeCondition.CycleTradable,
		TradingAllowed:        gate.TradingAllowed,
		EntriesAllowed:        gate.EntriesAllowed,
		ExitsAllowed:          gate.ExitsAllowed,
		ReduceOnly:            gate.ReduceOnly,
		BrokerTruthVerified:   status.BrokerTruth.Verified,
		BrokerTruthBlocked:    status.BrokerTruth.TradingBlocked,
		BrokerTruthRestricted: status.BrokerTruth.EntriesRestricted,
		JournalEventCount:     status.EventJournal.EventCount,
		LastJournalEventType:  status.EventJournal.LastEventType,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Logf("restart_validation_snapshot marshal error: %v", err)
		return
	}
	t.Logf("restart_validation_snapshot=%s", data)
}

func mutateRestartOrderStoreState(t *testing.T, trader *restartStateTestTrader, mutate func(*orders.StoreState)) {
	t.Helper()
	state := trader.orderStore.SnapshotState()
	mutate(&state)
	if err := trader.orderStore.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}
}

func setRestartHarnessFreshAccountAndPositionTruth(at *AutoTrader, now time.Time, positionCount int) {
	now = now.UTC()
	at.positionReconSummary = freshPositionReconSummary(now)
	account := AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          positionCount,
	}
	at.setRuntimeAccountSnapshot(account, []map[string]interface{}{})
	at.setLatestAccountSummary(&account)
}

func setRestartHarnessBrokerTruthClean(t *testing.T, at *AutoTrader, trader *restartStateTestTrader, now time.Time, positionCount int) {
	t.Helper()
	now = now.UTC()
	mutateRestartOrderStoreState(t, trader, func(state *orders.StoreState) {
		for idx := range state.Orders {
			record := &state.Orders[idx]
			if record.Status.Terminal() {
				continue
			}
			record.Status = orders.StatusFilled
			if record.FilledQty <= 0 {
				record.FilledQty = record.RequestedQty
			}
			record.RemainingQty = 0
			if record.AvgFillPrice <= 0 {
				record.AvgFillPrice = 100
			}
			record.TruthAuthority = orders.TruthAuthorityBrokerConfirmed
			record.TruthConfidence = orders.TruthConfidenceConfirmed
			record.TruthReason = "broker reconciliation confirmed final order state"
			record.LastMessage = record.TruthReason
			record.UpdatedAt = now
			record.LastSeenAt = now
		}
		state.Summary.LastRunAt = now
		state.Summary.LastSuccessAt = now
		state.Summary.LastError = ""
		state.Summary.TotalRuns++
		state.Summary.LastSummary = "order reconciliation clean"
		state.Summary.LastIssues = nil
	})
	setRestartHarnessFreshAccountAndPositionTruth(at, now, positionCount)
}

func TestRestartInterruptionValidationHarness(t *testing.T) {
	t.Run("ControlledRestartRestoresCleanShadowState", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_shadow_clean", Name: "Harness Shadow Clean", Mode: "shadow", Broker: "sim", Exchange: "ibkr", StrategyMode: "momentum_fallback"}
		trader := &restartStateTestTrader{orderStore: orders.NewStore()}
		at := newRestartStateTestAutoTrader(cfg, trader)
		at.setLatestAccountSummary(&AccountSummary{StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000, AccountingVersion: accountingVersion})

		actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL", Price: 100, Quantity: 10}
		at.observeShadowExecution(&decision.Decision{Symbol: "AAPL", Action: "open_long"}, actionRecord, execution.Intent{Symbol: "AAPL", ActionType: "open_long", Quantity: 10}, execution.Result{Status: execution.StatusFilled, AverageFillPrice: 100, FillQuantity: 10}, 100)
		at.persistDurableRuntimeState("harness_controlled_shadow_restart")

		restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
		restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
		restored.restoreDurableRuntimeState()
		gate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if !gate.TradingAllowed || !gate.EntriesAllowed || !gate.ExitsAllowed {
			t.Fatalf("expected clean shadow restart to remain tradable, got %+v", gate)
		}
		if summary := restored.currentRestartRecoverySummary(); !summary.Restored || summary.TradingBlocked {
			t.Fatalf("expected clean shadow restart restore without block, got %+v", summary)
		}
		logRestartValidationSnapshot(t, t.Name(), restored)
	})

	t.Run("InterruptionWithoutCheckpointBlocksUntilBrokerTruthReturns", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_no_checkpoint", Name: "Harness No Checkpoint", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
		trader := &restartStateTestTrader{orderStore: orders.NewStore()}
		at := newRestartStateTestAutoTrader(cfg, trader)
		at.restoreDurableRuntimeState()

		blockedGate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if blockedGate.TradingAllowed {
			t.Fatalf("expected interruption restart without checkpoint to block until broker truth returns, got %+v", blockedGate)
		}
		if summary := at.currentRestartRecoverySummary(); summary.StatePresent {
			t.Fatalf("expected absent checkpoint summary, got %+v", summary)
		}

		setRestartHarnessBrokerTruthClean(t, at, trader, time.Now(), 0)
		clearGate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if !clearGate.TradingAllowed || !clearGate.EntriesAllowed || !clearGate.ExitsAllowed {
			t.Fatalf("expected broker truth restoration to clear interruption block, got %+v", clearGate)
		}
		logRestartValidationSnapshot(t, t.Name(), at)
	})

	t.Run("PendingReconciliationRestartStaysBlockedUntilExplicitlyCleared", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_pending_recon", Name: "Harness Pending Reconciliation", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
		trader := &restartStateTestTrader{orderStore: orders.NewStore()}
		at := newRestartStateTestAutoTrader(cfg, trader)
		at.setLatestAccountSummary(&AccountSummary{StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000, AccountingVersion: accountingVersion})
		at.setLocalPositionSnapshots([]positions.Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 5, EntryPrice: 100}}, "harness", time.Now().UTC())

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
		at.persistDurableRuntimeState("harness_pending_reconciliation")

		restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
		restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
		restored.restoreDurableRuntimeState()
		blockedGate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if blockedGate.TradingAllowed {
			t.Fatalf("expected pending reconciliation restart to block trading, got %+v", blockedGate)
		}
		if status := restored.GetOperatorStatus(); status.RuntimeCondition.State != RuntimeConditionAwaitingReconciliation {
			t.Fatalf("expected awaiting reconciliation runtime state before clear, got %+v", status.RuntimeCondition)
		}

		setRestartHarnessBrokerTruthClean(t, restored, restoredTrader, time.Now(), 1)
		restored.resolveRestartRecoveryAfterBrokerReconciliation("harness broker reconciliation cleared restored state")
		clearGate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if !clearGate.TradingAllowed || !clearGate.EntriesAllowed || !clearGate.ExitsAllowed {
			t.Fatalf("expected explicit reconciliation clear to reopen trading, got %+v", clearGate)
		}
		events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", cfg.ID, "events.jsonl"))
		if len(events) == 0 || events[len(events)-1].Type != "restart_state_reconciled" {
			t.Fatalf("expected restart_state_reconciled journal event, got %+v", events)
		}
		logRestartValidationSnapshot(t, t.Name(), restored)
	})

	t.Run("UnresolvedExecutionTruthRestoredStillHardBlocksTrading", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_unresolved_restart", Name: "Harness Unresolved Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
		trader := &restartStateTestTrader{orderStore: orders.NewStore()}
		at := newRestartStateTestAutoTrader(cfg, trader)
		localID := trader.orderStore.RegisterSubmitted(orders.IntentEntryLong, "AAPL", "BUY", "long", 10, time.Now().Add(-time.Minute).UTC())
		mutateRestartOrderStoreState(t, trader, func(state *orders.StoreState) {
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
			now := time.Now().UTC()
			state.Summary.LastRunAt = now
			state.Summary.LastSuccessAt = now
			state.Summary.LastSummary = "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=0 unresolved=1"
			state.Summary.LastIssues = []orders.Issue{{
				LocalID:     localID,
				Message:     "execution truth unresolved pending broker follow-up",
				Authority:   orders.TruthAuthorityUnresolved,
				Confidence:  orders.TruthConfidenceUnresolved,
				NeedsReview: true,
			}}
		})
		at.persistDurableRuntimeState("harness_unresolved_truth")

		restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
		restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
		restored.restoreDurableRuntimeState()
		setRestartHarnessFreshAccountAndPositionTruth(restored, time.Now(), 0)
		blockedGate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if blockedGate.TradingAllowed {
			t.Fatalf("expected restored unresolved execution truth to hard-block trading, got %+v", blockedGate)
		}
		status := restored.GetOperatorStatus()
		if status.BrokerTruth.PrimaryAuthority != string(orders.TruthAuthorityUnresolved) || status.RuntimeCondition.State != RuntimeConditionAwaitingReconciliation {
			t.Fatalf("expected unresolved restored truth to remain explicit, got broker_truth=%+v runtime=%+v", status.BrokerTruth, status.RuntimeCondition)
		}
		logRestartValidationSnapshot(t, t.Name(), restored)
	})

	t.Run("InferredExecutionTruthRestoredRestrictsEntriesUntilReviewed", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_inferred_restart", Name: "Harness Inferred Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
		trader := &restartStateTestTrader{orderStore: orders.NewStore()}
		at := newRestartStateTestAutoTrader(cfg, trader)
		localID := trader.orderStore.RegisterSubmitted(orders.IntentEntryLong, "AAPL", "BUY", "long", 10, time.Now().Add(-time.Minute).UTC())
		mutateRestartOrderStoreState(t, trader, func(state *orders.StoreState) {
			for idx := range state.Orders {
				if state.Orders[idx].LocalID != localID {
					continue
				}
				state.Orders[idx].Status = orders.StatusFilled
				state.Orders[idx].FilledQty = 10
				state.Orders[idx].RemainingQty = 0
				state.Orders[idx].AvgFillPrice = 100
				state.Orders[idx].TruthAuthority = orders.TruthAuthorityReconciliationInferred
				state.Orders[idx].TruthConfidence = orders.TruthConfidenceHigh
				state.Orders[idx].TruthReason = "entry order inferred from broker position evidence"
				state.Orders[idx].NeedsReview = true
			}
			now := time.Now().UTC()
			state.Summary.LastRunAt = now
			state.Summary.LastSuccessAt = now
			state.Summary.LastSummary = "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0"
			state.Summary.LastIssues = []orders.Issue{{
				LocalID:     localID,
				Message:     "entry order inferred from broker position evidence",
				Authority:   orders.TruthAuthorityReconciliationInferred,
				Confidence:  orders.TruthConfidenceHigh,
				NeedsReview: true,
				Repaired:    true,
			}}
		})
		at.persistDurableRuntimeState("harness_inferred_truth")

		restoredTrader := &restartStateTestTrader{orderStore: orders.NewStore()}
		restored := newRestartStateTestAutoTrader(cfg, restoredTrader)
		restored.restoreDurableRuntimeState()
		setRestartHarnessFreshAccountAndPositionTruth(restored, time.Now(), 1)
		restored.resolveRestartRecoveryAfterBrokerReconciliation("harness restored inferred truth reviewed against broker baseline")
		gate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if !gate.TradingAllowed || gate.EntriesAllowed || !gate.ExitsAllowed || !gate.ReduceOnly {
			t.Fatalf("expected restored inferred truth to restrict entries but keep exits, got %+v", gate)
		}
		status := restored.GetOperatorStatus()
		if status.BrokerTruth.PrimaryAuthority != string(orders.TruthAuthorityReconciliationInferred) || !status.BrokerTruth.EntriesRestricted {
			t.Fatalf("expected inferred restored truth to remain degraded and visible, got %+v", status.BrokerTruth)
		}
		logRestartValidationSnapshot(t, t.Name(), restored)
	})

	t.Run("CorruptCheckpointFailsClosedAndJournalsFailure", func(t *testing.T) {
		cleanup := withTempWorkingDir(t)
		defer cleanup()

		cfg := AutoTraderConfig{ID: "harness_corrupt_restart", Name: "Harness Corrupt Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
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
		gate := restored.currentTradingGateDecision(false, restored.currentLatestAccountSummary())
		if gate.TradingAllowed {
			t.Fatalf("expected corrupt checkpoint to fail closed, got %+v", gate)
		}
		events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", cfg.ID, "events.jsonl"))
		if len(events) == 0 || events[len(events)-1].Type != "restart_state_restore_failed" {
			t.Fatalf("expected restart_state_restore_failed journal event, got %+v", events)
		}
		logRestartValidationSnapshot(t, t.Name(), restored)
	})
}
