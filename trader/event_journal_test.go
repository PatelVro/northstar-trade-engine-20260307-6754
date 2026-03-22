package trader

import (
	"bufio"
	"encoding/json"
	"northstar/audit"
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
	if len(events) != 2 {
		t.Fatalf("expected 2 gate journal events after dedupe, got %d", len(events))
	}
	if events[0].Type != "trading_gate_changed" || events[1].Type != "trading_gate_changed" {
		t.Fatalf("unexpected journal event types: %+v", events)
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
		isRunning: true,
	}
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
		isRunning:      true,
		startTime:      time.Now().Add(-time.Minute),
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     "status_journal_trader",
			TraderName:   "Status Journal Trader",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "momentum_only",
		}),
	}
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
