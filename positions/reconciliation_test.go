package positions

import (
	"testing"
	"time"
)

func TestCompareDetectsMissingSizeAndPriceMismatches(t *testing.T) {
	now := time.Now()
	local := []Snapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150},
		{Symbol: "MSFT", Side: "long", Quantity: 5, EntryPrice: 400},
	}
	broker := []Snapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 8, EntryPrice: 150},
		{Symbol: "NVDA", Side: "long", Quantity: 3, EntryPrice: 900},
		{Symbol: "MSFT", Side: "long", Quantity: 5, EntryPrice: 410},
	}

	result := Compare(local, broker, now)

	if result.Mismatches != 3 {
		t.Fatalf("expected 3 mismatches, got %d", result.Mismatches)
	}
	if result.SizeMismatches != 1 {
		t.Fatalf("expected 1 size mismatch, got %d", result.SizeMismatches)
	}
	if result.PriceMismatches != 1 {
		t.Fatalf("expected 1 price mismatch, got %d", result.PriceMismatches)
	}
	if result.LocalMissingAtBroker != 0 {
		t.Fatalf("expected 0 local missing at broker, got %d", result.LocalMissingAtBroker)
	}
	if result.BrokerMissingLocally != 1 {
		t.Fatalf("expected 1 broker missing locally, got %d", result.BrokerMissingLocally)
	}
	if len(result.Issues) != 3 {
		t.Fatalf("expected 3 issues, got %d", len(result.Issues))
	}
}

func TestCompareDetectsLocalMissingAtBroker(t *testing.T) {
	result := Compare(
		[]Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150}},
		nil,
		time.Now(),
	)

	if result.LocalMissingAtBroker != 1 {
		t.Fatalf("expected local missing at broker count 1, got %d", result.LocalMissingAtBroker)
	}
	if result.Mismatches != 1 {
		t.Fatalf("expected one mismatch, got %d", result.Mismatches)
	}
}

func TestCompareAllowsSmallPriceNoise(t *testing.T) {
	result := Compare(
		[]Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150.00}},
		[]Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150.04}},
		time.Now(),
	)
	if result.Mismatches != 0 {
		t.Fatalf("expected no mismatch for small price noise, got %d", result.Mismatches)
	}
}
