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
	localID := store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, now.Add(-(missingBrokerInferenceGraceWindow + 5*time.Second)))

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
	record := store.Lookup(localID, "")
	if record == nil {
		t.Fatalf("expected repaired record")
	}
	if record.TruthAuthority != TruthAuthorityReconciliationInferred || record.TruthConfidence != TruthConfidenceHigh {
		t.Fatalf("expected inferred high-confidence truth, got authority=%s confidence=%s", record.TruthAuthority, record.TruthConfidence)
	}
	if !record.NeedsReview {
		t.Fatalf("expected inferred repair to require review")
	}
	if result.InferredOutcomes != 1 || result.UnresolvedOutcomes != 0 {
		t.Fatalf("expected inferred-only reconciliation result, got %+v", result)
	}
}

func TestReconcileKeepsFreshSubmissionPendingWhenBrokerOpenOrdersLag(t *testing.T) {
	store := NewStore()
	now := time.Now()
	localID := store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, now.Add(-2*time.Second))

	result := store.Reconcile(nil, nil, now)

	if result.LocalMissingAtBroker != 0 {
		t.Fatalf("expected no local-missing mismatch during broker grace window, got %d", result.LocalMissingAtBroker)
	}
	record := store.Lookup(localID, "")
	if record == nil {
		t.Fatalf("expected local order record to remain available")
	}
	if record.Status != StatusSubmitted {
		t.Fatalf("expected fresh missing order to remain submitted, got %s", record.Status)
	}
	if len(store.ActiveOrders()) != 1 {
		t.Fatalf("expected fresh missing order to remain active")
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
	if active[0].TruthAuthority != TruthAuthorityBrokerConfirmed || active[0].TruthConfidence != TruthConfidenceConfirmed {
		t.Fatalf("expected broker-confirmed truth after broker repair, got authority=%s confidence=%s", active[0].TruthAuthority, active[0].TruthConfidence)
	}
}

func TestReconcileLeavesMissingEntryOrderUnresolvedWithoutPositionEvidence(t *testing.T) {
	store := NewStore()
	now := time.Now()
	localID := store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, now.Add(-(missingBrokerInferenceGraceWindow + 5*time.Second)))

	result := store.Reconcile(nil, nil, now)
	record := store.Lookup(localID, "")
	if record == nil {
		t.Fatalf("expected missing record to remain tracked")
	}
	if record.Status != StatusUnknown {
		t.Fatalf("expected unresolved missing order to become unknown, got %s", record.Status)
	}
	if record.TruthAuthority != TruthAuthorityUnresolved || record.TruthConfidence != TruthConfidenceUnresolved {
		t.Fatalf("expected unresolved truth, got authority=%s confidence=%s", record.TruthAuthority, record.TruthConfidence)
	}
	if !record.NeedsReview {
		t.Fatalf("expected unresolved missing order to require review")
	}
	if result.UnresolvedOutcomes != 1 || result.Repairs != 0 || !result.TradingBlocked {
		t.Fatalf("expected unresolved broker-missing outcome to block and avoid repair, got %+v", result)
	}
	if len(store.ActiveOrders()) != 1 {
		t.Fatalf("expected unresolved order to remain active")
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
