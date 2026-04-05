package trader

import (
	"bufio"
	"encoding/json"
	"northstar/audit"
	"northstar/orders"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalTradingGateTransitionsAreDeduped(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	at := &AutoTrader{
		id:   "journal_gate_trader",
		name: "Journal Gate Trader",
		config: AutoTraderConfig{
			ID:           "journal_gate_trader",
			Name:         "Journal Gate Trader",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		},
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     "journal_gate_trader",
			TraderName:   "Journal Gate Trader",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		}),
	}

	blocked := tradingGateDecision{
		Mode:            "halted",
		TradingAllowed:  false,
		EntriesAllowed:  false,
		ExitsAllowed:    false,
		BlockReason:     "broker truth unresolved",
		BlockingReasons: []string{"broker truth unresolved"},
		Message:         "trading blocked: broker truth unresolved",
	}
	allowed := tradingGateDecision{
		Mode:           "allow",
		TradingAllowed: true,
		EntriesAllowed: true,
		ExitsAllowed:   true,
		Message:        "trading allowed",
	}

	at.journalTradingGateDecision("test", blocked)
	at.journalTradingGateDecision("test", blocked)
	at.journalTradingGateDecision("test", allowed)

	events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", at.id, "events.jsonl"))
	gateEvents := 0
	for _, event := range events {
		if event.Type == "trading_gate_changed" {
			gateEvents++
		}
	}
	if gateEvents != 2 {
		t.Fatalf("expected 2 gate journal events after dedupe, got %d in %+v", gateEvents, events)
	}
}

func TestKillSwitchActivationWritesJournalMarker(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	traderID := "journal_kill_switch"
	t.Setenv(killSwitchEnvVarName+"_"+killSwitchEnvSuffix(traderID), "operator stop")

	at := &AutoTrader{
		id:   traderID,
		name: "Journal Kill Switch",
		config: AutoTraderConfig{
			ID:           traderID,
			Name:         "Journal Kill Switch",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		},
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     traderID,
			TraderName:   "Journal Kill Switch",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		}),
	}
	at.isRunning.Store(true)
	at.initializeKillSwitchState()

	summary := at.runKillSwitchCheck("test")
	if !summary.Active {
		t.Fatalf("expected kill switch to activate")
	}

	events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", traderID, "events.jsonl"))
	found := false
	for _, event := range events {
		if event.Type != "kill_switch_activated" {
			continue
		}
		found = true
		if event.Severity != audit.JournalSeverityCritical {
			t.Fatalf("expected critical kill switch journal severity, got %q", event.Severity)
		}
	}
	if !found {
		t.Fatalf("expected kill switch activation journal event in %+v", events)
	}
}

func TestOperatorStatusIncludesEventJournalSummary(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	at := &AutoTrader{
		id:       "status_journal_trader",
		name:     "Status Journal Trader",
		aiModel:  "deepseek",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:             "status_journal_trader",
			Name:           "Status Journal Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
			ScanInterval:   time.Minute,
		},
		initialBalance: 100000,
		startTime:      time.Now().Add(-time.Minute),
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     "status_journal_trader",
			TraderName:   "Status Journal Trader",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		}),
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now(),
		TradingAllowed: true,
		PassCount:      1,
	})
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp: time.Now().UTC(),
		Family:    "restart",
		Type:      "restart_state_restored",
		Severity:  audit.JournalSeverityInfo,
		Message:   "restored",
	})

	status := at.GetOperatorStatus()
	if !status.EventJournal.Available {
		t.Fatalf("expected event journal summary to be available")
	}
	if status.EventJournal.EventCount != 1 {
		t.Fatalf("expected event journal count 1, got %d", status.EventJournal.EventCount)
	}
	if status.EventJournal.LastEventType != "restart_state_restored" {
		t.Fatalf("expected last event type restart_state_restored, got %q", status.EventJournal.LastEventType)
	}
}

func TestJournalTradingGateTransitionsAlsoCaptureBrokerTruthState(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	now := time.Now().UTC()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:             now,
			LastSuccessAt:         now,
			LastSummary:           "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0",
			CurrentInferredOrders: 1,
			ConfidenceDegraded:    true,
			LastIssues: []orders.Issue{{
				LocalID:     "broker-local-1",
				Message:     "entry order inferred from broker position evidence",
				Authority:   orders.TruthAuthorityReconciliationInferred,
				Confidence:  orders.TruthConfidenceHigh,
				NeedsReview: true,
				Repaired:    true,
			}},
		},
	}
	at := &AutoTrader{
		id:       "journal_broker_truth",
		name:     "Journal Broker Truth",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			ID:           "journal_broker_truth",
			Name:         "Journal Broker Truth",
			Mode:         "paper",
			Broker:       "ibkr",
			StrategyMode: "momentum_only",
			ScanInterval: 5 * time.Minute,
		},
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     "journal_broker_truth",
			TraderName:   "Journal Broker Truth",
			Mode:         "paper",
			Broker:       "ibkr",
			StrategyMode: "momentum_only",
		}),
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ready", CheckedAt: now, TradingAllowed: true})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          1,
	}, []map[string]interface{}{})

	at.journalTradingGateDecision("test", at.currentTradingGateDecision(false, at.currentLatestAccountSummary()))

	events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", at.id, "events.jsonl"))
	found := false
	for _, event := range events {
		if event.Type != "broker_truth_restricted" {
			continue
		}
		found = true
		if event.TruthAuthority != string(orders.TruthAuthorityReconciliationInferred) || event.LocalOrderID != "broker-local-1" {
			t.Fatalf("expected broker truth journal to preserve inferred primary issue, got %+v", event)
		}
	}
	if !found {
		t.Fatalf("expected broker_truth_restricted journal event, got %+v", events)
	}
}

func readTraderJournalEvents(t *testing.T, path string) []audit.JournalEvent {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open journal file failed: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	events := make([]audit.JournalEvent, 0, 8)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event audit.JournalEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("Unmarshal journal event failed: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan journal file failed: %v", err)
	}
	return events
}
