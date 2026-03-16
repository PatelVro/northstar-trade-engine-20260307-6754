package orders

import (
	"testing"
	"time"
)

func TestReconcileImportsUnknownBrokerOrder(t *testing.T) {
	store := NewStore()
	now := time.Now()

	result := store.Reconcile([]BrokerOrder{
		{
			OrderID:    "123",
			Symbol:     "AAPL",
			Side:       "BUY",
			Status:     StatusAccepted,
			RawStatus:  "Submitted",
			Quantity:   10,
			ObservedAt: now,
		},
	}, nil, now)

	if result.UnknownBrokerOrders != 1 {
		t.Fatalf("expected 1 unknown broker order, got %d", result.UnknownBrokerOrders)
	}
	summary := store.SnapshotSummary()
	if summary.TrackedOrders != 1 {
		t.Fatalf("expected 1 tracked order, got %d", summary.TrackedOrders)
	}
	if summary.ActiveLocalOrders != 1 {
		t.Fatalf("expected 1 active local order, got %d", summary.ActiveLocalOrders)
	}
}

func TestReconcileRepairsMissingEntryOrderToFilled(t *testing.T) {
	store := NewStore()
	now := time.Now()
	store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, now.Add(-5*time.Second))

	result := store.Reconcile(nil, []PositionSnapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 10},
	}, now)

	if result.LocalMissingAtBroker != 1 {
		t.Fatalf("expected 1 local-missing mismatch, got %d", result.LocalMissingAtBroker)
	}
	active := store.ActiveOrders()
	if len(active) != 0 {
		t.Fatalf("expected no active orders after fill repair, got %d", len(active))
	}
}

func TestReconcileRepairsFillMismatchFromBrokerTruth(t *testing.T) {
	store := NewStore()
	now := time.Now()
	localID := store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, now.Add(-5*time.Second))

	store.mu.Lock()
	store.ordersByLocal[localID].BrokerOrderID = "123"
	store.ordersByLocal[localID].Status = StatusAccepted
	store.ordersByLocal[localID].FilledQty = 0
	store.localByBroker["123"] = localID
	store.mu.Unlock()

	result := store.Reconcile([]BrokerOrder{
		{
			OrderID:      "123",
			Symbol:       "AAPL",
			Side:         "BUY",
			Status:       StatusPartiallyFilled,
			RawStatus:    "PartiallyFilled",
			Quantity:     10,
			FilledQty:    4,
			RemainingQty: 6,
			ObservedAt:   now,
		},
	}, []PositionSnapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 4},
	}, now)

	if result.FillMismatches != 1 {
		t.Fatalf("expected 1 fill mismatch, got %d", result.FillMismatches)
	}
	active := store.ActiveOrders()
	if len(active) != 1 {
		t.Fatalf("expected 1 active order, got %d", len(active))
	}
	if active[0].FilledQty != 4 {
		t.Fatalf("expected filled qty repaired to 4, got %.4f", active[0].FilledQty)
	}
}

func TestNormalizeBrokerStatus(t *testing.T) {
	if got := NormalizeBrokerStatus("Submitted", 0, 10, 10); got != StatusAccepted {
		t.Fatalf("expected accepted, got %s", got)
	}
	if got := NormalizeBrokerStatus("PartiallyFilled", 4, 10, 6); got != StatusPartiallyFilled {
		t.Fatalf("expected partially_filled, got %s", got)
	}
	if got := NormalizeBrokerStatus("Filled", 10, 10, 0); got != StatusFilled {
		t.Fatalf("expected filled, got %s", got)
	}
	if got := NormalizeBrokerStatus("Cancelled", 0, 10, 10); got != StatusCancelled {
		t.Fatalf("expected cancelled, got %s", got)
	}
	if got := NormalizeBrokerStatus("Rejected", 0, 10, 10); got != StatusRejected {
		t.Fatalf("expected rejected, got %s", got)
	}
}
