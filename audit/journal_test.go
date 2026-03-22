package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"northstar/orders"
)

func TestJournalAppendsEventsDurably(t *testing.T) {
	root := t.TempDir()
	journal := NewJournal(root, Metadata{
		TraderID:     "paper_trader",
		TraderName:   "Paper Trader",
		Mode:         "paper",
		Broker:       "ibkr",
		StrategyMode: "momentum_only",
	})

	when := time.Now().UTC()
	if err := journal.Append(JournalEvent{
		Timestamp: when,
		Family:    "safety",
		Type:      "trading_gate_changed",
		Severity:  JournalSeverityWarning,
		Message:   "trading blocked: broker truth unresolved",
		Payload: map[string]interface{}{
			"trading_allowed": false,
		},
	}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	summary := journal.Summary()
	if summary.EventCount != 1 {
		t.Fatalf("expected 1 journal event, got %d", summary.EventCount)
	}
	if summary.LastEventType != "trading_gate_changed" {
		t.Fatalf("unexpected last event type %q", summary.LastEventType)
	}

	raw, err := os.ReadFile(filepath.Join(root, "journal", "paper_trader", "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("expected newline-terminated append-only journal record")
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	lines := 0
	for scanner.Scan() {
		if scanner.Text() != "" {
			lines++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if lines != 1 {
		t.Fatalf("expected 1 journal line, got %d", lines)
	}
}

func TestJournalOrderAndReconciliationEventsPreserveTruthMetadata(t *testing.T) {
	root := t.TempDir()
	journal := NewJournal(root, Metadata{
		TraderID:     "paper_trader",
		TraderName:   "Paper Trader",
		Mode:         "paper",
		Broker:       "ibkr",
		StrategyMode: "momentum_only",
	})

	now := time.Now().UTC()
	journal.OnOrderEvent(orders.Event{
		EventID:        "evt-1",
		Timestamp:      now,
		Type:           orders.EventFilled,
		PreviousStatus: orders.StatusAccepted,
		CurrentStatus:  orders.StatusFilled,
		Message:        "filled from reconciliation evidence",
		Record: orders.Record{
			LocalID:         "local-1",
			BrokerOrderID:   "BRK-1",
			Symbol:          "AAPL",
			Status:          orders.StatusFilled,
			TruthAuthority:  orders.TruthAuthorityReconciliationInferred,
			TruthConfidence: orders.TruthConfidenceHigh,
			TruthReason:     "position evidence indicates fill",
			NeedsReview:     true,
		},
	})
	journal.OnReconciliation(orders.ReconciliationResult{
		RanAt:              now.Add(time.Second),
		Mismatches:         1,
		Repairs:            1,
		InferredOutcomes:   1,
		UnresolvedOutcomes: 0,
		NeedsReview:        true,
		Summary:            "order reconciliation inferred 1 execution outcome from position evidence",
		Issues: []orders.Issue{{
			Type:        orders.IssueLocalMissingAtBroker,
			LocalID:     "local-1",
			Authority:   orders.TruthAuthorityReconciliationInferred,
			Confidence:  orders.TruthConfidenceHigh,
			NeedsReview: true,
			Message:     "position evidence indicates fill",
		}},
	})

	events := readJournalEvents(t, filepath.Join(root, "journal", "paper_trader", "events.jsonl"))
	if len(events) != 2 {
		t.Fatalf("expected 2 journal events, got %d", len(events))
	}
	if events[0].TruthAuthority != string(orders.TruthAuthorityReconciliationInferred) {
		t.Fatalf("expected first event to preserve truth authority, got %q", events[0].TruthAuthority)
	}
	if events[0].TruthConfidence != string(orders.TruthConfidenceHigh) {
		t.Fatalf("expected first event to preserve truth confidence, got %q", events[0].TruthConfidence)
	}
	if !events[0].NeedsReview {
		t.Fatalf("expected first event to mark needs_review")
	}
	if events[1].Family != "reconciliation" {
		t.Fatalf("expected second event family reconciliation, got %q", events[1].Family)
	}
	if events[1].Severity != JournalSeverityWarning {
		t.Fatalf("expected reconciliation severity warning, got %q", events[1].Severity)
	}
	if events[1].TruthAuthority != string(orders.TruthAuthorityReconciliationInferred) {
		t.Fatalf("expected reconciliation event to promote truth authority, got %q", events[1].TruthAuthority)
	}
	if events[1].TruthConfidence != string(orders.TruthConfidenceHigh) {
		t.Fatalf("expected reconciliation event to promote truth confidence, got %q", events[1].TruthConfidence)
	}
	if events[1].LocalOrderID != "local-1" {
		t.Fatalf("expected reconciliation event to surface local order id, got %q", events[1].LocalOrderID)
	}
}

func TestNewJournalRestoresSummaryFromExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "journal", "paper_trader", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	events := []JournalEvent{
		{
			RecordVersion: journalRecordVersion,
			EventID:       "evt-1",
			Timestamp:     time.Now().UTC().Add(-time.Minute),
			TraderID:      "paper_trader",
			Family:        "restart",
			Type:          "restart_state_restored",
			Severity:      JournalSeverityInfo,
			Message:       "restored",
		},
		{
			RecordVersion: journalRecordVersion,
			EventID:       "evt-2",
			Timestamp:     time.Now().UTC(),
			TraderID:      "paper_trader",
			Family:        "safety",
			Type:          "kill_switch_activated",
			Severity:      JournalSeverityCritical,
			Message:       "activated",
		},
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	for _, event := range events {
		data, _ := json.Marshal(event)
		if _, err := file.Write(append(data, '\n')); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}
	file.Close()

	journal := NewJournal(root, Metadata{TraderID: "paper_trader"})
	summary := journal.Summary()
	if summary.EventCount != 2 {
		t.Fatalf("expected 2 restored events, got %d", summary.EventCount)
	}
	if summary.LastEventType != "kill_switch_activated" {
		t.Fatalf("expected last event type kill_switch_activated, got %q", summary.LastEventType)
	}
	if summary.LastSeverity != JournalSeverityCritical {
		t.Fatalf("expected last severity critical, got %q", summary.LastSeverity)
	}
}

func readJournalEvents(t *testing.T, path string) []JournalEvent {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open journal file failed: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]JournalEvent, 0, 4)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event JournalEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("Unmarshal journal line failed: %v", err)
		}
		out = append(out, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan journal file failed: %v", err)
	}
	return out
}
