package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"northstar/orders"
)

func TestRecorderWritesAndListsRecentTrades(t *testing.T) {
	root := t.TempDir()
	recorder := NewRecorder(root, Metadata{
		TraderID:     "paper_trader",
		TraderName:   "Paper Trader",
		Mode:         "paper",
		Broker:       "ibkr",
		StrategyMode: "multi_factor",
	})

	now := time.Now().UTC()
	if err := recorder.RecordTrade(TradeRecord{
		TradeID:         "trade-1",
		Timestamp:       now,
		CycleNumber:     7,
		Symbol:          "AAPL",
		Action:          "open_long",
		Reason:          "factor breakout",
		RiskResult:      "pass",
		ExecutionResult: "filled",
		OrderLifecycle: OrderLifecycle{
			LocalOrderID:  "local-1",
			BrokerOrderID: "12345",
			Status:        "filled",
		},
	}); err != nil {
		t.Fatalf("RecordTrade failed: %v", err)
	}

	trades, err := recorder.ListRecentTrades(10)
	if err != nil {
		t.Fatalf("ListRecentTrades failed: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].TradeID != "trade-1" {
		t.Fatalf("unexpected trade id %q", trades[0].TradeID)
	}
	if trades[0].OrderLifecycle.LocalOrderID != "local-1" {
		t.Fatalf("expected local order id to persist")
	}
}

func TestRecorderLinksOrderEventsToTrade(t *testing.T) {
	root := t.TempDir()
	recorder := NewRecorder(root, Metadata{
		TraderID:     "paper_trader",
		TraderName:   "Paper Trader",
		Mode:         "paper",
		Broker:       "ibkr",
		StrategyMode: "multi_factor",
	})

	now := time.Now().UTC()
	if err := recorder.RecordTrade(TradeRecord{
		TradeID:         "trade-1",
		Timestamp:       now,
		Symbol:          "MSFT",
		Action:          "open_long",
		Reason:          "earnings continuation",
		RiskResult:      "reduce_size",
		ExecutionResult: "submitted",
		OrderLifecycle: OrderLifecycle{
			LocalOrderID:  "local-2",
			BrokerOrderID: "B-2",
			Status:        "submitted",
		},
	}); err != nil {
		t.Fatalf("RecordTrade failed: %v", err)
	}

	recorder.OnOrderEvent(orders.Event{
		EventID:        "evt-1",
		Timestamp:      now.Add(time.Second),
		Type:           orders.EventFilled,
		Message:        "broker marked order filled",
		PreviousStatus: orders.StatusSubmitted,
		CurrentStatus:  orders.StatusFilled,
		Record: orders.Record{
			LocalID:       "local-2",
			BrokerOrderID: "B-2",
			Symbol:        "MSFT",
			Status:        orders.StatusFilled,
			RequestedQty:  10,
			FilledQty:     10,
			SubmittedAt:   now,
			UpdatedAt:     now.Add(time.Second),
		},
	})

	orderDir := filepath.Join(root, "orders", "paper_trader")
	entries, err := os.ReadDir(orderDir)
	if err != nil {
		t.Fatalf("ReadDir(order audit) failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 order audit record, got %d", len(entries))
	}
}
