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

func TestPrimaryExecutionTruthIssuePrefersUnresolvedOverInferred(t *testing.T) {
	issues := []Issue{
		{
			LocalID:     "local-inferred",
			Message:     "execution inferred from position evidence",
			Authority:   TruthAuthorityReconciliationInferred,
			Confidence:  TruthConfidenceHigh,
			NeedsReview: true,
			Repaired:    true,
		},
		{
			LocalID:     "local-unresolved",
			Message:     "execution truth remains unresolved",
			Authority:   TruthAuthorityUnresolved,
			Confidence:  TruthConfidenceUnresolved,
			NeedsReview: true,
			Repaired:    false,
		},
	}

	primary := PrimaryExecutionTruthIssue(issues)
	if primary == nil {
		t.Fatalf("expected primary issue")
	}
	if primary.LocalID != "local-unresolved" || primary.Authority != TruthAuthorityUnresolved {
		t.Fatalf("expected unresolved issue to win, got %+v", primary)
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

func TestReconcileDoesNotReImportTerminalBrokerOrders(t *testing.T) {
	store := NewStore()
	now := time.Now()

	// First reconciliation: import an unknown broker order that is already filled
	brokerOrders := []BrokerOrder{
		{
			OrderID:    "9999",
			Symbol:     "ADBE",
			Side:       "BUY",
			Status:     StatusFilled,
			RawStatus:  "Filled",
			Quantity:   11,
			FilledQty:  11,
			ObservedAt: now,
		},
	}
	result1 := store.Reconcile(brokerOrders, nil, now)
	if result1.UnknownBrokerOrders != 1 {
		t.Fatalf("first run: expected 1 unknown broker order import, got %d", result1.UnknownBrokerOrders)
	}
	if result1.ImportedOrders != 1 {
		t.Fatalf("first run: expected 1 imported order, got %d", result1.ImportedOrders)
	}

	// Second reconciliation: same broker order still appears (IBKR keeps returning it)
	result2 := store.Reconcile(brokerOrders, nil, now.Add(3*time.Second))
	if result2.UnknownBrokerOrders != 0 {
		t.Fatalf("second run: expected 0 unknown broker orders (already imported), got %d", result2.UnknownBrokerOrders)
	}
	if result2.ImportedOrders != 0 {
		t.Fatalf("second run: expected 0 new imports, got %d", result2.ImportedOrders)
	}

	// Third reconciliation: still should not re-import
	result3 := store.Reconcile(brokerOrders, nil, now.Add(6*time.Second))
	if result3.UnknownBrokerOrders != 0 {
		t.Fatalf("third run: expected 0 unknown broker orders, got %d", result3.UnknownBrokerOrders)
	}
}
