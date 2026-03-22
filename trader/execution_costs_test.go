package trader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutionCostModelEstimateIncludesConfiguredFriction(t *testing.T) {
	model := ExecutionCostModel{
		CommissionBps:        10,
		SpreadBps:            2,
		SlippageBps:          5,
		ImpactBps:            20,
		MaxParticipationRate: 0.15,
	}

	estimate := model.Estimate(100, 100, "long", true, 0.01, true)
	if estimate.EffectivePrice <= 100 {
		t.Fatalf("expected modeled buy fill price above reference, got %.4f", estimate.EffectivePrice)
	}
	if estimate.SpreadCostUSD <= 0 || estimate.SlippageCostUSD <= 0 || estimate.ImpactCostUSD <= 0 || estimate.CommissionUSD <= 0 {
		t.Fatalf("expected all friction components to be positive, got %+v", estimate)
	}
	if !estimate.ImpactApplied {
		t.Fatalf("expected impact to be applied when participation is known")
	}
	if estimate.TotalModeledCostUSD <= estimate.CommissionUSD {
		t.Fatalf("expected total modeled cost to exceed commission alone, got %+v", estimate)
	}
}

func TestSimTraderExecutionCostSummaryAndReplayExportIncludeModeledFriction(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	sim := NewSimTrader(100000, &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 20000})
	sim.SetExecutionCostModel(ExecutionCostModel{
		CommissionBps:        10,
		SpreadBps:            2,
		SlippageBps:          5,
		ImpactBps:            20,
		MaxParticipationRate: 0.20,
	})

	if _, err := sim.openPosition("AAPL", 100, "long"); err != nil {
		t.Fatalf("openPosition failed: %v", err)
	}
	if _, err := sim.closePosition("AAPL", 100, "long"); err != nil {
		t.Fatalf("closePosition failed: %v", err)
	}

	summary := sim.currentEvaluationCostSummary()
	if !summary.ModelApplied || !summary.AppliesToSimulatedExecution {
		t.Fatalf("expected simulated evaluation cost summary to be active, got %+v", summary)
	}
	if summary.Totals.ModeledSpreadCostUSD <= 0 || summary.Totals.ModeledSlippageCostUSD <= 0 || summary.Totals.ModeledImpactCostUSD <= 0 {
		t.Fatalf("expected modeled friction totals to be populated, got %+v", summary.Totals)
	}
	if summary.Totals.ModeledTotalCostUSD <= summary.Totals.ModeledCommissionUSD {
		t.Fatalf("expected total modeled cost to exceed commission alone, got %+v", summary.Totals)
	}

	sim.ExportSummary()

	raw, err := os.ReadFile(filepath.Join("output", "replay_summary.json"))
	if err != nil {
		t.Fatalf("expected replay summary json to exist: %v", err)
	}

	var parsed struct {
		ExecutionCostSummary string `json:"execution_cost_summary"`
		ExecutionCostModel   struct {
			SpreadBps float64 `json:"spread_bps"`
		} `json:"execution_cost_model"`
		ExecutionCostTotals struct {
			ModeledTotalCostUSD float64 `json:"modeled_total_cost_usd"`
		} `json:"execution_cost_totals"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to parse replay summary json: %v", err)
	}
	if parsed.ExecutionCostModel.SpreadBps != 2 {
		t.Fatalf("expected replay summary spread bps 2, got %.2f", parsed.ExecutionCostModel.SpreadBps)
	}
	if parsed.ExecutionCostTotals.ModeledTotalCostUSD <= 0 {
		t.Fatalf("expected replay summary modeled total cost to be positive, got %.4f", parsed.ExecutionCostTotals.ModeledTotalCostUSD)
	}
	if !strings.Contains(strings.ToLower(parsed.ExecutionCostSummary), "modeled") {
		t.Fatalf("expected replay summary to describe modeled execution costs, got %q", parsed.ExecutionCostSummary)
	}
}

func TestGetOperatorStatusExposesEvaluationCostsForShadowMode(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:       "shadow_trader",
		name:     "Shadow Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:                     "shadow_trader",
			Name:                   "Shadow Trader",
			Mode:                   "shadow",
			Broker:                 "ibkr",
			DataProvider:           "ibkr",
			InstrumentType:         "equity",
			StrategyMode:           "momentum_only",
			ScanInterval:           3 * time.Minute,
			InitialBalance:         100000,
			ExecutionCommissionBps: 10,
			ExecutionSpreadBps:     2,
			ExecutionSlippageBps:   5,
			ExecutionImpactBps:     12,
			MaxParticipationRate:   0.15,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      now.Add(-15 * time.Minute),
	}
	at.initializeShadowModeState()
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: now, TradingAllowed: true, PassCount: 3})
	at.shadowMu.Lock()
	at.shadowState.Available = true
	at.shadowState.Active = true
	at.shadowState.ModeledCommissionUSD = 12.5
	at.shadowState.ModeledSpreadCostUSD = 3.0
	at.shadowState.ModeledSlippageCostUSD = 5.5
	at.shadowState.ModeledImpactCostUSD = 0
	at.shadowState.ModeledTotalExecutionCostUSD = 21.0
	at.shadowMu.Unlock()

	status := at.GetOperatorStatus()
	if !status.EvaluationCosts.Available || !status.EvaluationCosts.ModelApplied {
		t.Fatalf("expected evaluation costs to be surfaced for shadow mode, got %+v", status.EvaluationCosts)
	}
	if !status.EvaluationCosts.AppliesToShadowHypothetical {
		t.Fatalf("expected shadow-mode evaluation costs to be marked hypothetical, got %+v", status.EvaluationCosts)
	}
	if status.EvaluationCosts.Totals.ModeledTotalCostUSD != 21.0 {
		t.Fatalf("expected modeled total cost 21, got %.2f", status.EvaluationCosts.Totals.ModeledTotalCostUSD)
	}
	if status.EvaluationModeledTotalCostUSD != 21.0 {
		t.Fatalf("expected compatibility modeled total cost 21, got %.2f", status.EvaluationModeledTotalCostUSD)
	}
}

func TestWritePaperSessionReportIncludesEvaluationCosts(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	now := time.Now()
	at := &AutoTrader{
		id:       "shadow_trader",
		name:     "Shadow Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:                     "shadow_trader",
			Name:                   "Shadow Trader",
			Mode:                   "shadow",
			Broker:                 "ibkr",
			DataProvider:           "ibkr",
			InstrumentType:         "equity",
			StrategyMode:           "momentum_only",
			ScanInterval:           3 * time.Minute,
			InitialBalance:         100000,
			ExecutionCommissionBps: 10,
			ExecutionSpreadBps:     2,
			ExecutionSlippageBps:   5,
			ExecutionImpactBps:     12,
			MaxParticipationRate:   0.15,
		},
		initialBalance: 100000,
	}
	at.initializeShadowModeState()
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: now, TradingAllowed: true, PassCount: 3})
	at.shadowMu.Lock()
	at.shadowState.Available = true
	at.shadowState.Active = true
	at.shadowState.ModeledCommissionUSD = 12.5
	at.shadowState.ModeledSpreadCostUSD = 3.0
	at.shadowState.ModeledSlippageCostUSD = 5.5
	at.shadowState.ModeledImpactCostUSD = 0
	at.shadowState.ModeledTotalExecutionCostUSD = 21.0
	at.shadowMu.Unlock()

	tracker := newPaperSessionTracker(at, now.Add(-time.Hour))
	at.writePaperSessionReport(tracker, "unit_test")

	path := sessionReportPath(at.id, tracker.report.SessionStart, tracker.report.SessionDate)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected paper session report to exist: %v", err)
	}
	var report PaperSessionReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("failed to parse paper session report: %v", err)
	}
	if !report.EvaluationCosts.Available || !report.EvaluationCosts.ModelApplied {
		t.Fatalf("expected evaluation costs in paper session report, got %+v", report.EvaluationCosts)
	}
	if report.EvaluationCosts.Totals.ModeledTotalCostUSD != 21.0 {
		t.Fatalf("expected modeled total cost 21, got %.2f", report.EvaluationCosts.Totals.ModeledTotalCostUSD)
	}
}
