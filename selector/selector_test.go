package selector

import (
	"reflect"
	"testing"
	"time"

	"northstar/regime"
)

func TestTrendRegimeSelectsMomentum(t *testing.T) {
	result := Default().Select(&regime.Result{
		Symbol:       "AAPL",
		Timestamp:    time.Now().UTC(),
		Timeframe:    "4h",
		Regime:       regime.RegimeTrend,
		TrendScore:   0.82,
		RegimeScore:  0.82,
		Confidence:   0.74,
		Explanation:  "trend regime due to strong 20-bar momentum",
		Valid:        true,
		Contributing: []regime.Contribution{{Name: "return_20_bar"}, {Name: "ema_5_vs_20_spread"}},
	})
	if result.SelectedFamily != StrategyFamilyMomentum {
		t.Fatalf("expected momentum family, got %s", result.SelectedFamily)
	}
	if result.SelectedStrategy != "momentum_only" {
		t.Fatalf("expected momentum_only, got %s", result.SelectedStrategy)
	}
	if !result.AllowTrading {
		t.Fatalf("expected trading to be allowed")
	}
}

func TestMeanReversionSelectsMultiFactorMapping(t *testing.T) {
	result := Default().Select(&regime.Result{
		Symbol:             "MSFT",
		Timestamp:          time.Now().UTC(),
		Timeframe:          "4h",
		Regime:             regime.RegimeMeanReversion,
		MeanReversionScore: 0.77,
		RegimeScore:        0.77,
		Confidence:         0.68,
		Explanation:        "mean reversion regime due to stretched z-score",
		Valid:              true,
	})
	if result.SelectedFamily != StrategyFamilyMeanReversion {
		t.Fatalf("expected mean_reversion family, got %s", result.SelectedFamily)
	}
	if result.SelectedStrategy != "multi_factor" {
		t.Fatalf("expected multi_factor mapping, got %s", result.SelectedStrategy)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected warning about current implementation mapping")
	}
}

func TestHighVolatilitySuggestsDefensiveReducedRisk(t *testing.T) {
	result := Default().Select(&regime.Result{
		Symbol:          "NVDA",
		Timestamp:       time.Now().UTC(),
		Timeframe:       "4h",
		Regime:          regime.RegimeHighVolatility,
		VolatilityScore: 0.83,
		RegimeScore:     0.83,
		Confidence:      0.71,
		Explanation:     "high volatility regime due to volatility expansion",
		Valid:           true,
	})
	if result.SelectedFamily != StrategyFamilyDefensive {
		t.Fatalf("expected defensive family, got %s", result.SelectedFamily)
	}
	if result.RecommendedRiskMode != RiskModeReducedRisk {
		t.Fatalf("expected reduced_risk mode, got %s", result.RecommendedRiskMode)
	}
}

func TestUnstableReturnsNoTrade(t *testing.T) {
	result := Default().Select(&regime.Result{
		Symbol:           "QQQ",
		Timestamp:        time.Now().UTC(),
		Timeframe:        "4h",
		Regime:           regime.RegimeUnstable,
		InstabilityScore: 0.91,
		RegimeScore:      0.91,
		Confidence:       0.80,
		Explanation:      "unstable regime due to conflicting signals",
		Valid:            true,
	})
	if result.SelectedFamily != StrategyFamilyNoTrade {
		t.Fatalf("expected no_trade family, got %s", result.SelectedFamily)
	}
	if result.AllowTrading {
		t.Fatalf("expected trading to be disallowed")
	}
}

func TestInvalidRegimeUsesSafeFallback(t *testing.T) {
	result := Default().Select(&regime.Result{
		Symbol:      "SPY",
		Timeframe:   "4h",
		Regime:      regime.RegimeUnknown,
		Confidence:  0.10,
		RegimeScore: 0.12,
		Valid:       false,
		Warnings:    []string{"insufficient_history"},
	})
	if result.SelectedFamily != StrategyFamilyNoTrade {
		t.Fatalf("expected no_trade for invalid regime, got %s", result.SelectedFamily)
	}
	if result.FallbackStrategy != "momentum_fallback" {
		t.Fatalf("expected safe fallback strategy, got %s", result.FallbackStrategy)
	}
}

func TestSelectorDeterministic(t *testing.T) {
	input := &regime.Result{
		Symbol:             "IWM",
		Timestamp:          time.Date(2026, 3, 16, 16, 0, 0, 0, time.UTC),
		Timeframe:          "4h",
		Regime:             regime.RegimeLowVolatility,
		LowVolatilityScore: 0.74,
		RegimeScore:        0.74,
		Confidence:         0.66,
		Explanation:        "low volatility regime due to compressed ranges",
		Valid:              true,
	}
	first := Default().Select(input)
	second := Default().Select(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic selection output")
	}
}
