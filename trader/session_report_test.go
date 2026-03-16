package trader

import (
	"encoding/json"
	"northstar/execution"
	"northstar/incidents"
	"northstar/logger"
	"northstar/orders"
	"northstar/risk"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyPaperSessionAccountingUsesCanonicalSessionDeltas(t *testing.T) {
	start := &AccountSummary{
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		RealizedPnL:            250,
	}
	end := &AccountSummary{
		StrategyInitialCapital: 100000,
		StrategyEquity:         101500,
		AccountCash:            90500,
		AccountEquity:          101800,
		RealizedPnL:            1000,
		UnrealizedPnL:          800,
		StrategyReturnPct:      1.5,
	}

	report := PaperSessionReport{}
	applyPaperSessionAccounting(&report, start, end)

	if report.StartingStrategyEquity == nil || *report.StartingStrategyEquity != 100000 {
		t.Fatalf("unexpected starting strategy equity: %+v", report.StartingStrategyEquity)
	}
	if report.EndingStrategyEquity == nil || *report.EndingStrategyEquity != 101500 {
		t.Fatalf("unexpected ending strategy equity: %+v", report.EndingStrategyEquity)
	}
	if report.RealizedPnL == nil || *report.RealizedPnL != 750 {
		t.Fatalf("expected realized session pnl 750, got %+v", report.RealizedPnL)
	}
	if report.TotalPnL == nil || *report.TotalPnL != 1500 {
		t.Fatalf("expected total session pnl 1500, got %+v", report.TotalPnL)
	}
	if report.StrategyReturnPct == nil || *report.StrategyReturnPct != 1.5 {
		t.Fatalf("expected session return pct 1.5, got %+v", report.StrategyReturnPct)
	}
	if report.EndingCumulativeStrategyReturnPct == nil || *report.EndingCumulativeStrategyReturnPct != 1.5 {
		t.Fatalf("expected ending cumulative return pct 1.5, got %+v", report.EndingCumulativeStrategyReturnPct)
	}
}

func TestClassifyPaperSessionCompletionBlocked(t *testing.T) {
	report := PaperSessionReport{
		TradingAllowedAtStart: false,
		DecisionCycles:        0,
	}
	if got := classifyPaperSessionCompletion(report, false, false, true); got != SessionCompletionBlocked {
		t.Fatalf("expected blocked session status, got %s", got)
	}
}

func TestClassifyPaperSessionCompletionDegraded(t *testing.T) {
	report := PaperSessionReport{
		TradingAllowedAtStart:     true,
		DecisionCycles:            6,
		BrokerDegradedEventsCount: 1,
	}
	if got := classifyPaperSessionCompletion(report, true, true, false); got != SessionCompletionDegraded {
		t.Fatalf("expected degraded session status, got %s", got)
	}
}

func TestWritePaperSessionReportWritesExpectedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output", "session_reports", "paper_trader", "paper_trader_session_20260315_093000.json")
	report := PaperSessionReport{
		ReportVersion:           sessionReportVersion,
		TraderID:                "paper_trader",
		TraderName:              "Paper Trader",
		Mode:                    "paper",
		Broker:                  "ibkr",
		StrategyMode:            "multi_factor",
		GeneratedAt:             time.Now(),
		SessionDate:             "2026-03-15",
		SessionStart:            time.Now().Add(-time.Hour),
		SessionEnd:              time.Now(),
		SessionCompletionStatus: SessionCompletionCompleted,
	}

	if err := writePaperSessionReport(path, report); err != nil {
		t.Fatalf("writePaperSessionReport failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected session report file to exist: %v", err)
	}

	var parsed PaperSessionReport
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to parse written report json: %v", err)
	}
	if parsed.TraderID != "paper_trader" {
		t.Fatalf("unexpected trader id in written report: %s", parsed.TraderID)
	}
	if parsed.SessionCompletionStatus != SessionCompletionCompleted {
		t.Fatalf("unexpected session completion status: %s", parsed.SessionCompletionStatus)
	}
}

func TestPaperSessionTrackerCapturesRiskOutcomes(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "paper_trader",
		name:   "Paper Trader",
		config: AutoTraderConfig{Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	record := &logger.DecisionRecord{
		Decisions: []logger.DecisionAction{
			{
				Action:      "open_long",
				Symbol:      "AAPL",
				Success:     false,
				RiskOutcome: "reject",
				RiskSummary: "risk reject: daily pnl breached",
				Error:       "risk engine rejected AAPL open_long: daily pnl breached",
			},
			{
				Action:               "open_long",
				Symbol:               "MSFT",
				Success:              true,
				RiskOutcome:          "reduce_size",
				RiskSummary:          "risk reduce_size: reduced order to symbol cap",
				RiskApprovedNotional: 10000,
			},
		},
	}

	tracker.observeDecisionRecord(record)

	if tracker.report.RiskEvaluations != 2 {
		t.Fatalf("expected 2 risk evaluations, got %d", tracker.report.RiskEvaluations)
	}
	if tracker.report.RiskRejectedOrders != 1 {
		t.Fatalf("expected 1 risk reject, got %d", tracker.report.RiskRejectedOrders)
	}
	if tracker.report.RiskReducedOrders != 1 {
		t.Fatalf("expected 1 risk reduce, got %d", tracker.report.RiskReducedOrders)
	}
	if len(tracker.report.DistinctRiskMessages) != 2 {
		t.Fatalf("expected 2 distinct risk messages, got %d", len(tracker.report.DistinctRiskMessages))
	}
}

func TestPaperSessionTrackerCapturesExecutionSummary(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "paper_trader",
		name:   "Paper Trader",
		config: AutoTraderConfig{Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	record := &logger.DecisionRecord{
		Decisions: []logger.DecisionAction{
			{
				Action:      "open_long",
				Symbol:      "AAPL",
				Success:     true,
				OrderStatus: string(execution.StatusFilled),
				Quantity:    5,
				Price:       101.5,
			},
			{
				Action:      "close_long",
				Symbol:      "MSFT",
				Success:     true,
				OrderStatus: string(execution.StatusSubmitted),
				Quantity:    3,
			},
			{
				Action:      "open_short",
				Symbol:      "NVDA",
				Success:     false,
				OrderStatus: string(execution.StatusDuplicateSuppressed),
				Error:       "execution duplicate_suppressed for NVDA open_short: duplicate execution suppressed within recent submission window",
			},
			{
				Action:      "open_long",
				Symbol:      "AMZN",
				Success:     false,
				OrderStatus: string(execution.StatusBlocked),
				Error:       "execution blocked for AMZN open_long: risk supervisor reduce_only",
			},
		},
	}

	tracker.observeDecisionRecord(record)

	if tracker.report.ExecutionIntentsTotal != 4 {
		t.Fatalf("expected 4 execution intents, got %d", tracker.report.ExecutionIntentsTotal)
	}
	if tracker.report.ExecutionBlockedCount != 1 {
		t.Fatalf("expected 1 blocked execution, got %d", tracker.report.ExecutionBlockedCount)
	}
	if tracker.report.DuplicateSuppressedCount != 1 {
		t.Fatalf("expected 1 duplicate-suppressed execution, got %d", tracker.report.DuplicateSuppressedCount)
	}
	if tracker.report.ExecutionSubmittedCount != 2 {
		t.Fatalf("expected 2 submitted executions, got %d", tracker.report.ExecutionSubmittedCount)
	}
	if tracker.report.ExecutionFilledCount != 1 {
		t.Fatalf("expected 1 filled execution, got %d", tracker.report.ExecutionFilledCount)
	}
	if tracker.report.OrdersSubmitted != 2 {
		t.Fatalf("expected 2 orders submitted, got %d", tracker.report.OrdersSubmitted)
	}
	if tracker.report.OrdersFilled != 1 {
		t.Fatalf("expected 1 order filled, got %d", tracker.report.OrdersFilled)
	}
	if tracker.report.PositionsOpenedCount != 1 {
		t.Fatalf("expected 1 opened position from immediate fills, got %d", tracker.report.PositionsOpenedCount)
	}
	if len(tracker.report.SymbolsTraded) != 0 {
		t.Fatalf("expected symbols list to finalize later from tracker map, got %+v", tracker.report.SymbolsTraded)
	}
	if _, ok := tracker.symbols["AAPL"]; !ok {
		t.Fatalf("expected filled symbol to be tracked")
	}
	if _, ok := tracker.symbols["MSFT"]; ok {
		t.Fatalf("expected submitted-only symbol not to count as traded yet")
	}
}

func TestApplyPaperSessionOrderReconciliationUsesDeltas(t *testing.T) {
	report := PaperSessionReport{}
	start := &orders.Summary{
		TotalRuns:            2,
		TotalMismatches:      1,
		TotalRepairs:         1,
		UnknownBrokerOrders:  0,
		LocalMissingAtBroker: 1,
		FillMismatches:       0,
	}
	end := &orders.Summary{
		TotalRuns:            7,
		TotalMismatches:      4,
		TotalRepairs:         4,
		UnknownBrokerOrders:  1,
		LocalMissingAtBroker: 2,
		FillMismatches:       1,
		LastSummary:          "order reconciliation repaired 3 mismatch(es)",
	}

	applyPaperSessionOrderReconciliation(&report, start, end)

	if report.OrderReconciliationRuns != 5 {
		t.Fatalf("expected 5 reconciliation runs, got %d", report.OrderReconciliationRuns)
	}
	if report.OrderReconciliationMismatches != 3 {
		t.Fatalf("expected 3 reconciliation mismatches, got %d", report.OrderReconciliationMismatches)
	}
	if report.OrderReconciliationRepairs != 3 {
		t.Fatalf("expected 3 reconciliation repairs, got %d", report.OrderReconciliationRepairs)
	}
	if report.OrderReconciliationUnknownBroker != 1 {
		t.Fatalf("expected 1 unknown broker order delta, got %d", report.OrderReconciliationUnknownBroker)
	}
	if report.OrderReconciliationLocalMissing != 1 {
		t.Fatalf("expected 1 local missing delta, got %d", report.OrderReconciliationLocalMissing)
	}
	if report.OrderReconciliationFillMismatch != 1 {
		t.Fatalf("expected 1 fill mismatch delta, got %d", report.OrderReconciliationFillMismatch)
	}
}

func TestApplyPaperSessionPositionReconciliationUsesDeltas(t *testing.T) {
	report := PaperSessionReport{}
	start := &positionReconciliationSummary{
		TotalRuns:            2,
		TotalIncidents:       1,
		TotalMismatches:      1,
		LocalMissingAtBroker: 1,
	}
	end := &positionReconciliationSummary{
		Status:               PositionReconciliationHealthy,
		Summary:              "reconciled local positions from broker truth after 2 mismatch(es)",
		TotalRuns:            6,
		TotalIncidents:       3,
		TotalMismatches:      4,
		LocalMissingAtBroker: 2,
		BrokerMissingLocally: 1,
		SizeMismatches:       1,
		PriceMismatches:      1,
	}

	applyPaperSessionPositionReconciliation(&report, start, end)

	if report.PositionReconciliationRuns != 4 {
		t.Fatalf("expected 4 reconciliation runs, got %d", report.PositionReconciliationRuns)
	}
	if report.PositionReconciliationIncidents != 2 {
		t.Fatalf("expected 2 reconciliation incidents, got %d", report.PositionReconciliationIncidents)
	}
	if report.PositionReconciliationMismatches != 3 {
		t.Fatalf("expected 3 reconciliation mismatches, got %d", report.PositionReconciliationMismatches)
	}
	if report.PositionReconciliationBrokerMiss != 1 {
		t.Fatalf("expected 1 broker-missing delta, got %d", report.PositionReconciliationBrokerMiss)
	}
	if report.PositionReconciliationStatus != string(PositionReconciliationHealthy) {
		t.Fatalf("expected final healthy status, got %q", report.PositionReconciliationStatus)
	}
}

func TestPaperSessionTrackerCapturesPortfolioRiskMetrics(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "paper_trader",
		name:   "Paper Trader",
		config: AutoTraderConfig{Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	tracker.observePortfolioRisk(&portfolioRiskState{
		EvaluatedAt: time.Now(),
		Outcome:     risk.OutcomeReject,
		Summary:     "risk reject: current drawdown breached stop",
		Metrics: risk.PortfolioMetrics{
			CurrentGrossExposurePct:  0.62,
			CurrentNetExposurePct:    0.48,
			LargestSector:            "technology",
			LargestSectorExposurePct: 0.36,
			CorrelatedPositionCount:  2,
			MaxObservedCorrelation:   0.91,
			CurrentDrawdownPct:       0.11,
			SectorExposurePct:        map[string]float64{"technology": 0.36},
		},
	})

	if tracker.report.PortfolioRiskLatest == nil {
		t.Fatalf("expected latest portfolio risk snapshot")
	}
	if tracker.report.PortfolioRiskLatest.Metrics.LargestSector != "technology" {
		t.Fatalf("expected latest largest sector to be recorded")
	}
	if tracker.report.PortfolioRiskPeaks == nil {
		t.Fatalf("expected portfolio risk peaks to be tracked")
	}
	if tracker.report.PortfolioRiskPeaks.MaxSectorExposurePct != 0.36 {
		t.Fatalf("expected max sector exposure pct 0.36, got %.2f", tracker.report.PortfolioRiskPeaks.MaxSectorExposurePct)
	}
	if tracker.report.PortfolioRiskPeaks.MaxCorrelatedPositions != 2 {
		t.Fatalf("expected max correlated positions 2, got %d", tracker.report.PortfolioRiskPeaks.MaxCorrelatedPositions)
	}
}

func TestPaperSessionTrackerCapturesRiskSupervisorState(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "paper_trader",
		name:   "Paper Trader",
		config: AutoTraderConfig{Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	tracker.observeRiskSupervisor(risk.SupervisorState{
		EvaluatedAt:           time.Now(),
		Mode:                  risk.SupervisorModeBlockNewEntries,
		TradingAllowed:        true,
		EntriesAllowed:        false,
		ExitsAllowed:          true,
		Summary:               "gross exposure 110.00% exceeds limit 100.00%",
		ActiveIncidentCount:   1,
		CriticalIncidentCount: 0,
		Incidents: []risk.Incident{
			{
				Type:         risk.IncidentMaxGrossExposureBreached,
				Severity:     risk.IncidentSeverityWarning,
				Summary:      "gross exposure 110.00% exceeds limit 100.00%",
				EnforcedMode: risk.SupervisorModeBlockNewEntries,
				Active:       true,
			},
		},
	})

	if tracker.report.FinalRiskMode != risk.SupervisorModeBlockNewEntries {
		t.Fatalf("expected final risk mode block_new_entries, got %s", tracker.report.FinalRiskMode)
	}
	if !tracker.report.SupervisorRestrictedDuringSession {
		t.Fatalf("expected supervisor restriction flag to be set")
	}
	if tracker.report.RiskIncidentCount != 1 {
		t.Fatalf("expected one active risk incident, got %d", tracker.report.RiskIncidentCount)
	}
	if len(tracker.report.NotableRiskIncidents) != 1 {
		t.Fatalf("expected one notable risk incident, got %d", len(tracker.report.NotableRiskIncidents))
	}
}

func TestPaperSessionTrackerCapturesOperationalIncidents(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "paper_trader",
		name:   "Paper Trader",
		config: AutoTraderConfig{Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	tracker.observeIncident(incidents.Incident{
		IncidentID:   "inc-001",
		IncidentType: incidents.TypeBrokerRuntimeDegraded,
		Severity:     incidents.SeverityWarning,
		Summary:      "broker runtime degraded: connection refused",
		Active:       true,
	})
	tracker.observeIncident(incidents.Incident{
		IncidentID:   "inc-002",
		IncidentType: incidents.TypeKillSwitchActivated,
		Severity:     incidents.SeverityCritical,
		Summary:      "emergency kill switch activated via file",
		Active:       true,
	})

	if tracker.report.IncidentCount != 2 {
		t.Fatalf("expected two incidents, got %d", tracker.report.IncidentCount)
	}
	if tracker.report.CriticalIncidentCount != 1 {
		t.Fatalf("expected one critical incident, got %d", tracker.report.CriticalIncidentCount)
	}
	if !tracker.report.SessionHadOperationalIncident {
		t.Fatalf("expected session operational incident flag to be set")
	}
	if len(tracker.report.NotableIncidents) != 2 {
		t.Fatalf("expected two notable incidents, got %d", len(tracker.report.NotableIncidents))
	}
	if len(sortedKeys(tracker.incidentTypes)) != 2 {
		t.Fatalf("expected two incident types to be tracked")
	}
}
