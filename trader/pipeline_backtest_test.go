package trader

import (
	"northstar/decision"
	"northstar/features"
	"northstar/market"
	"northstar/regime"
	"northstar/selector"
	"strings"
	"testing"
	"time"
)

func TestApplyCanonicalRuntimeStrategyDispatchBlocksNoTradeEntries(t *testing.T) {
	now := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	at := &AutoTrader{
		config: AutoTraderConfig{
			InstrumentType:        "equity",
			StrategyMode:          "momentum_only",
			DynamicPositionSizing: true,
			FallbackPositionPct:   0.10,
			RiskPerTradePct:       0.01,
			MaxPositionPct:        0.20,
			MaxGrossExposure:      1.0,
			MaxNetExposurePct:     0.65,
		},
		backtestMode:   true,
		peakEquitySeen: 100000,
	}

	ctx := &decision.Context{
		Account: decision.AccountInfo{
			AccountEquity:    100000,
			StrategyEquity:   100000,
			AvailableBalance: 100000,
		},
		MarketDataMap: map[string]*market.Data{
			"AAPL": {
				Symbol:       "AAPL",
				CurrentPrice: 100,
				Features: &features.FeatureSet{
					Vectors: map[string]*features.FeatureVector{
						"4h": {Symbol: "AAPL", Timestamp: now, Timeframe: "4h", Valid: true},
					},
				},
				Regimes: &regime.ResultSet{
					Results: map[string]*regime.Result{
						"4h": {Symbol: "AAPL", Timestamp: now, Timeframe: "4h", Regime: regime.RegimeUnstable, Confidence: 0.9, Valid: true},
					},
				},
				Selections: &selector.SelectionSet{
					Selections: map[string]*selector.Selection{
						"4h": {
							Symbol:              "AAPL",
							Timestamp:           now,
							Timeframe:           "4h",
							SelectedFamily:      selector.StrategyFamilyNoTrade,
							SelectedStrategy:    "momentum_fallback",
							AllowTrading:        false,
							RecommendedRiskMode: selector.RiskModeNoTrade,
							SelectionReason:     "unstable regime",
							Valid:               true,
						},
					},
				},
			},
		},
	}

	fullDecision := &decision.FullDecision{
		Decisions: []decision.Decision{
			{Symbol: "AAPL", Action: "open_long", PositionSizeUSD: 10000, StopLoss: 95, Confidence: 80, Reasoning: "test entry"},
		},
	}

	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
	if len(fullDecision.Decisions) != 1 {
		t.Fatalf("expected one decision, got %d", len(fullDecision.Decisions))
	}
	if got := fullDecision.Decisions[0].Action; got != "wait" {
		t.Fatalf("expected blocked decision to become wait, got %s", got)
	}
	if !strings.Contains(fullDecision.Decisions[0].Reasoning, "Canonical pipeline blocked AAPL open_long") {
		t.Fatalf("expected blocking reason to be preserved, got %q", fullDecision.Decisions[0].Reasoning)
	}
}

func TestApplyCanonicalRuntimeStrategyDispatchReducesEntrySize(t *testing.T) {
	now := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	at := &AutoTrader{
		config: AutoTraderConfig{
			InstrumentType:           "equity",
			StrategyMode:             "multi_factor",
			DynamicPositionSizing:    true,
			FallbackPositionPct:      0.10,
			RiskPerTradePct:          0.01,
			MaxPositionPct:           0.10,
			MaxGrossExposure:         1.0,
			MaxNetExposurePct:        0.65,
			DrawdownThrottleStartPct: 0.03,
			DrawdownThrottleMinScale: 0.35,
		},
		backtestMode:   true,
		peakEquitySeen: 100000,
	}

	ctx := &decision.Context{
		Account: decision.AccountInfo{
			AccountEquity:    100000,
			StrategyEquity:   100000,
			AvailableBalance: 100000,
		},
		MarketDataMap: map[string]*market.Data{
			"MSFT": {
				Symbol:       "MSFT",
				CurrentPrice: 100,
				Features: &features.FeatureSet{
					Vectors: map[string]*features.FeatureVector{
						"4h": {
							Symbol:        "MSFT",
							Timestamp:     now,
							Timeframe:     "4h",
							Valid:         true,
							ATR14Pct:      0.05,
							RealizedVol20: 0.04,
						},
					},
				},
				Regimes: &regime.ResultSet{
					Results: map[string]*regime.Result{
						"4h": {Symbol: "MSFT", Timestamp: now, Timeframe: "4h", Regime: regime.RegimeHighVolatility, Confidence: 0.8, Valid: true},
					},
				},
				Selections: &selector.SelectionSet{
					Selections: map[string]*selector.Selection{
						"4h": {
							Symbol:              "MSFT",
							Timestamp:           now,
							Timeframe:           "4h",
							SelectedFamily:      selector.StrategyFamilyDefensive,
							SelectedStrategy:    "momentum_fallback",
							Confidence:          0.45,
							AllowTrading:        true,
							RecommendedRiskMode: selector.RiskModeReducedRisk,
							SelectionReason:     "high volatility",
							Valid:               true,
						},
					},
				},
			},
		},
	}

	fullDecision := &decision.FullDecision{
		Decisions: []decision.Decision{
			{Symbol: "MSFT", Action: "open_long", PositionSizeUSD: 25000, StopLoss: 90, Confidence: 80, Reasoning: "test entry"},
		},
	}

	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
	if got := fullDecision.Decisions[0].Action; got != "open_long" {
		t.Fatalf("expected entry to remain allowed, got %s", got)
	}
	if fullDecision.Decisions[0].PositionSizeUSD <= 0 {
		t.Fatalf("expected positive position size after allocator")
	}
	if fullDecision.Decisions[0].PositionSizeUSD >= 25000 {
		t.Fatalf("expected allocator to reduce size below requested notional, got %.2f", fullDecision.Decisions[0].PositionSizeUSD)
	}
}

func TestApplyCanonicalRuntimeStrategyDispatchRunsOutsideBacktestAndShadow(t *testing.T) {
	now := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	at := &AutoTrader{
		config: AutoTraderConfig{
			Mode:                  "paper",
			InstrumentType:        "equity",
			StrategyMode:          "momentum_only",
			DynamicPositionSizing: true,
			FallbackPositionPct:   0.10,
			RiskPerTradePct:       0.01,
			MaxPositionPct:        0.20,
			MaxGrossExposure:      1.0,
			MaxNetExposurePct:     0.65,
		},
		peakEquitySeen: 100000,
	}

	ctx := &decision.Context{
		Account: decision.AccountInfo{
			AccountEquity:    100000,
			StrategyEquity:   100000,
			AvailableBalance: 100000,
		},
		MarketDataMap: map[string]*market.Data{
			"NVDA": {
				Symbol:       "NVDA",
				CurrentPrice: 100,
				Features: &features.FeatureSet{
					Vectors: map[string]*features.FeatureVector{
						"4h": {Symbol: "NVDA", Timestamp: now, Timeframe: "4h", Valid: true},
					},
				},
				Regimes: &regime.ResultSet{
					Results: map[string]*regime.Result{
						"4h": {Symbol: "NVDA", Timestamp: now, Timeframe: "4h", Regime: regime.RegimeUnstable, Confidence: 0.95, Valid: true},
					},
				},
				Selections: &selector.SelectionSet{
					Selections: map[string]*selector.Selection{
						"4h": {
							Symbol:              "NVDA",
							Timestamp:           now,
							Timeframe:           "4h",
							SelectedFamily:      selector.StrategyFamilyNoTrade,
							SelectedStrategy:    "momentum_fallback",
							AllowTrading:        false,
							RecommendedRiskMode: selector.RiskModeNoTrade,
							SelectionReason:     "unstable regime",
							Valid:               true,
						},
					},
				},
			},
		},
	}

	fullDecision := &decision.FullDecision{
		Decisions: []decision.Decision{
			{Symbol: "NVDA", Action: "open_long", PositionSizeUSD: 10000, StopLoss: 95, Confidence: 80, Reasoning: "runtime entry"},
		},
	}

	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
	if got := fullDecision.Decisions[0].Action; got != "wait" {
		t.Fatalf("expected paper runtime entry to be blocked by canonical pipeline, got %s", got)
	}
}

func TestBlockedPipelineOriginalActionSupportsCanonicalPrefix(t *testing.T) {
	d := decision.Decision{
		Action:    "wait",
		Reasoning: "Canonical pipeline blocked AAPL open_long: unstable regime",
	}
	if got := blockedPipelineOriginalAction(d); got != "open_long" {
		t.Fatalf("expected original action open_long, got %s", got)
	}
}
