package allocator

import (
	"reflect"
	"testing"

	"northstar/selector"
)

func TestHighVolatilityReducesSize(t *testing.T) {
	baseInput := testAllocationInput()
	lowVol := Default().Allocate(baseInput)
	highVolInput := testAllocationInput()
	highVolInput.ATR14Pct = 0.040
	highVolInput.RealizedVol20 = 0.035
	highVol := Default().Allocate(highVolInput)

	if highVol.RecommendedNotional >= lowVol.RecommendedNotional {
		t.Fatalf("expected high volatility to reduce size, low=%.2f high=%.2f", lowVol.RecommendedNotional, highVol.RecommendedNotional)
	}
}

func TestLowConfidenceReducesSize(t *testing.T) {
	high := testAllocationInput()
	high.Selection.Confidence = 0.90
	highResult := Default().Allocate(high)

	low := testAllocationInput()
	low.Selection.Confidence = 0.35
	lowResult := Default().Allocate(low)

	if lowResult.RecommendedNotional >= highResult.RecommendedNotional {
		t.Fatalf("expected low confidence to reduce size")
	}
}

func TestNoTradeSelectionReturnsZero(t *testing.T) {
	input := testAllocationInput()
	input.Selection.AllowTrading = false
	input.Selection.RecommendedRiskMode = selector.RiskModeNoTrade
	input.Selection.SelectedFamily = selector.StrategyFamilyNoTrade

	result := Default().Allocate(input)
	if result.AllowTrade {
		t.Fatalf("expected trade to be blocked")
	}
	if result.RecommendedNotional != 0 {
		t.Fatalf("expected zero notional, got %.2f", result.RecommendedNotional)
	}
}

func TestCapByMaxPositionPctWorks(t *testing.T) {
	input := testAllocationInput()
	input.Account.CurrentSymbolExposure = 18_000
	result := Default().Allocate(input)

	if !result.AllowTrade {
		t.Fatalf("expected residual capacity to allow some trade")
	}
	if result.TargetPositionPct > 0.20001 {
		t.Fatalf("expected target position pct cap, got %.4f", result.TargetPositionPct)
	}
}

func TestDeterministicOutput(t *testing.T) {
	input := testAllocationInput()
	first := Default().Allocate(input)
	second := Default().Allocate(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic allocation output")
	}
}

func testAllocationInput() Input {
	return Input{
		Symbol:            "AAPL",
		Action:            "open_long",
		EntryPrice:        100,
		CurrentPrice:      100,
		StopLoss:          96,
		IncreasesExposure: true,
		ATR14Pct:          0.012,
		RealizedVol20:     0.010,
		Account: AccountSnapshot{
			StrategyEquity:        100_000,
			AccountEquity:         100_000,
			AvailableBalance:      90_000,
			CurrentGrossExposure:  20_000,
			CurrentNetExposure:    10_000,
			CurrentSymbolExposure: 0,
			PeakStrategyEquity:    100_000,
		},
		Config: Config{
			DynamicSizing:            true,
			BaseRiskPerTradePct:      0.01,
			FallbackPositionPct:      0.10,
			MaxPositionPct:           0.20,
			MaxGrossExposurePct:      1.0,
			MaxNetExposurePct:        0.65,
			CashBufferPct:            0.95,
			DrawdownThrottleStartPct: 0.03,
			DrawdownThrottleMinScale: 0.35,
			VolatilityTargetPct:      0.015,
			VolatilityMinScale:       0.35,
			MinTradeNotional:         100,
		},
		Selection: &selector.Selection{
			SelectedFamily:      selector.StrategyFamilyMomentum,
			SelectedStrategy:    "momentum_only",
			AllowTrading:        true,
			RecommendedRiskMode: selector.RiskModeNormal,
			Confidence:          0.80,
		},
	}
}
