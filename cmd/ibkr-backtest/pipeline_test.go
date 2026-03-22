package main

import (
	"encoding/json"
	"northstar/logger"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAnalyzePipelineBacktestSummarizesAttribution(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)

	records := []logger.DecisionRecord{
		{
			CycleNumber: 1,
			Timestamp:   now,
			Pipeline: []logger.PipelineObservation{
				{
					Symbol:              "AAPL",
					Timestamp:           now,
					Timeframe:           "4h",
					FeatureValid:        true,
					Regime:              "trend",
					SelectedFamily:      "momentum",
					SelectionAllowTrade: true,
				},
				{
					Symbol:              "MSFT",
					Timestamp:           now,
					Timeframe:           "4h",
					FeatureValid:        true,
					Regime:              "unstable",
					SelectedFamily:      "no_trade",
					SelectionAllowTrade: false,
				},
			},
			Decisions: []logger.DecisionAction{
				{
					Action:      "open_long",
					Symbol:      "AAPL",
					Quantity:    10,
					Success:     true,
					OrderStatus: "filled",
					Pipeline: &logger.PipelineDecision{
						PipelineObservation: logger.PipelineObservation{
							Symbol:              "AAPL",
							Timestamp:           now,
							Timeframe:           "4h",
							FeatureValid:        true,
							Regime:              "trend",
							SelectedFamily:      "momentum",
							SelectionAllowTrade: true,
						},
						DecisionAction:       "open_long",
						DecisionAllowed:      true,
						AllocationAllowTrade: true,
					},
				},
				{
					Action:      "wait",
					Symbol:      "MSFT",
					Success:     true,
					OrderStatus: "",
					Pipeline: &logger.PipelineDecision{
						PipelineObservation: logger.PipelineObservation{
							Symbol:              "MSFT",
							Timestamp:           now,
							Timeframe:           "4h",
							FeatureValid:        true,
							Regime:              "unstable",
							SelectedFamily:      "no_trade",
							SelectionAllowTrade: false,
							SelectionRiskMode:   "no_trade",
						},
						DecisionAction:       "open_long",
						DecisionAllowed:      false,
						AllocationAllowTrade: false,
						BlockingReason:       "selector blocked entry for MSFT in unstable regime",
					},
				},
			},
		},
		{
			CycleNumber: 2,
			Timestamp:   now.Add(4 * time.Hour),
			Pipeline: []logger.PipelineObservation{
				{
					Symbol:              "AAPL",
					Timestamp:           now.Add(4 * time.Hour),
					Timeframe:           "4h",
					FeatureValid:        true,
					Regime:              "trend",
					SelectedFamily:      "momentum",
					SelectionAllowTrade: true,
				},
			},
			Decisions: []logger.DecisionAction{
				{
					Action:      "close_long",
					Symbol:      "AAPL",
					Quantity:    10,
					Success:     true,
					OrderStatus: "filled",
					RealizedPnL: 150,
					Pipeline: &logger.PipelineDecision{
						PipelineObservation: logger.PipelineObservation{
							Symbol:              "AAPL",
							Timestamp:           now.Add(4 * time.Hour),
							Timeframe:           "4h",
							FeatureValid:        true,
							Regime:              "trend",
							SelectedFamily:      "momentum",
							SelectionAllowTrade: true,
						},
						DecisionAction:       "close_long",
						DecisionAllowed:      true,
						AllocationAllowTrade: true,
					},
				},
			},
		},
	}

	for _, record := range records {
		path := filepath.Join(dir, record.Timestamp.Format("20060102_150405")+filepath.Ext("decision.json"))
		if record.CycleNumber == 1 {
			path = filepath.Join(dir, "decision_20260316_100000_cycle1.json")
		} else {
			path = filepath.Join(dir, "decision_20260316_140000_cycle2.json")
		}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}

	summary, err := analyzePipelineBacktest(dir)
	if err != nil {
		t.Fatalf("analyzePipelineBacktest: %v", err)
	}

	if summary.DecisionLogCount != 2 {
		t.Fatalf("expected 2 decision logs, got %d", summary.DecisionLogCount)
	}
	if summary.ObservationCount != 3 {
		t.Fatalf("expected 3 observations, got %d", summary.ObservationCount)
	}
	if summary.AllowTradeCount != 2 || summary.NoTradeCount != 1 {
		t.Fatalf("unexpected allow/no_trade counts: %+v", summary)
	}
	if summary.EntryDecisionCount != 2 || summary.BlockedEntryCount != 1 || summary.SelectorBlockedCount != 1 {
		t.Fatalf("unexpected entry blocking counts: %+v", summary)
	}
	if summary.ClosedTradeCount != 1 || summary.TotalRealizedPnLUSD != 150 {
		t.Fatalf("unexpected trade summary: %+v", summary)
	}
	if len(summary.ByRegime) == 0 || len(summary.ByStrategyFamily) == 0 || len(summary.ByTradingRecommendation) == 0 {
		t.Fatalf("expected populated attribution buckets, got %+v", summary)
	}
}
