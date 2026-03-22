package orders

import (
	"testing"
	"time"
)

func TestStoreStateRoundTripPreservesRecordsAndSummary(t *testing.T) {
	store := NewStore()
	submittedAt := time.Now().UTC().Add(-time.Minute)
	localID := store.RegisterSubmitted(IntentEntryLong, "AAPL", "BUY", "long", 10, submittedAt)
	store.MarkRejected(localID, "rejected for test", submittedAt.Add(5*time.Second))
	activeID := store.RegisterSubmitted(IntentExitLong, "MSFT", "SELL", "long", 5, submittedAt.Add(10*time.Second))

	state := store.SnapshotState()

	restored := NewStore()
	if err := restored.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	if record := restored.Lookup(localID, ""); record == nil || record.Status != StatusRejected {
		t.Fatalf("expected rejected record to round-trip, got %+v", record)
	}
	if record := restored.Lookup(activeID, ""); record == nil || record.Status != StatusSubmitted {
		t.Fatalf("expected active submitted record to round-trip, got %+v", record)
	}

	summary := restored.SnapshotSummary()
	if summary.TrackedOrders != 2 {
		t.Fatalf("expected 2 tracked orders, got %d", summary.TrackedOrders)
	}
	if summary.ActiveLocalOrders != 1 {
		t.Fatalf("expected 1 active local order, got %d", summary.ActiveLocalOrders)
	}
}

func TestStoreStateRejectsMissingLocalID(t *testing.T) {
	store := NewStore()
	err := store.RestoreState(StoreState{
		Version: storeStateVersion,
		Orders:  []Record{{Symbol: "AAPL", Status: StatusSubmitted}},
	})
	if err == nil {
		t.Fatalf("expected missing local_id error")
	}
}
