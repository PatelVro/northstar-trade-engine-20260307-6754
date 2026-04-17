package trader

import (
	"strings"
	"testing"
	"time"
)

func TestExportSessionReportMarkdownContainsExpectedSections(t *testing.T) {
	totalPnL := 1500.0
	returnPct := 1.5
	realizedPnL := 750.0
	unrealizedPnL := 800.0
	report := &PaperSessionReport{
		ReportVersion:           sessionReportVersion,
		TraderID:                "paper_trader",
		TraderName:              "Paper Trader",
		Mode:                    "paper",
		Broker:                  "ibkr",
		StrategyMode:            "multi_factor",
		SessionDate:             "2026-03-15",
		SessionStart:            time.Date(2026, 3, 15, 9, 30, 0, 0, time.Local),
		SessionEnd:              time.Date(2026, 3, 15, 16, 0, 0, 0, time.Local),
		SessionDurationSeconds:  23400,
		SessionCompletionStatus: SessionCompletionCompleted,
		TotalPnL:                &totalPnL,
		StrategyReturnPct:       &returnPct,
		RealizedPnL:             &realizedPnL,
		UnrealizedPnLEnd:        &unrealizedPnL,
		PositionsOpenedCount:    3,
		PositionsClosedCount:    2,
		OrdersSubmitted:         5,
		OrdersFilled:            4,
		DecisionCycles:          100,
		ActionableDecisions:     8,
		BuyFills:                3,
		SellFills:               2,
		SymbolsTraded:           []string{"AAPL", "MSFT", "NVDA"},
		BlockedCyclesCount:      5,
		RiskEvaluations:         10,
		RiskRejectedOrders:      1,
		RiskIncidentCount:       0,
		WarningsCount:           2,
		ErrorsCount:             1,
		NotableRiskIncidents:    []string{},
	}

	md := ExportSessionReportMarkdown(report)

	expectedSections := []string{
		"# Session Report: Paper Trader",
		"## Performance Summary",
		"## Execution Summary",
		"## Trade Log",
		"## Risk Events",
		"## Decision Quality",
	}
	for _, section := range expectedSections {
		if !strings.Contains(md, section) {
			t.Fatalf("expected Markdown to contain section %q", section)
		}
	}

	expectedContent := []string{
		"paper_trader",
		"2026-03-15",
		"paper",
		"multi_factor",
		"1.50%",
		"1500.00",
		"AAPL, MSFT, NVDA",
	}
	for _, content := range expectedContent {
		if !strings.Contains(md, content) {
			t.Fatalf("expected Markdown to contain %q", content)
		}
	}
}

func TestExportSessionReportMarkdownHandlesNilReport(t *testing.T) {
	md := ExportSessionReportMarkdown(nil)
	if md != "" {
		t.Fatalf("expected empty string for nil report, got %q", md)
	}
}

func TestExportSessionReportMarkdownHandlesNoTrades(t *testing.T) {
	report := &PaperSessionReport{
		TraderID:                "paper_trader",
		TraderName:              "Paper Trader",
		Mode:                    "paper",
		SessionDate:             "2026-03-15",
		SessionStart:            time.Date(2026, 3, 15, 9, 30, 0, 0, time.Local),
		SessionEnd:              time.Date(2026, 3, 15, 16, 0, 0, 0, time.Local),
		SessionCompletionStatus: SessionCompletionBlocked,
		SymbolsTraded:           []string{},
	}

	md := ExportSessionReportMarkdown(report)
	if !strings.Contains(md, "No trades recorded this session") {
		t.Fatalf("expected no-trades message in Markdown output")
	}
}

func TestExportSessionReportCSVHasCorrectColumns(t *testing.T) {
	totalPnL := 500.0
	returnPct := 0.5
	realizedPnL := 300.0
	report := &PaperSessionReport{
		TraderID:                "paper_trader",
		Mode:                    "paper",
		StrategyMode:            "multi_factor",
		Broker:                  "ibkr",
		SessionDate:             "2026-03-15",
		SessionStart:            time.Date(2026, 3, 15, 9, 30, 0, 0, time.Local),
		SessionEnd:              time.Date(2026, 3, 15, 16, 0, 0, 0, time.Local),
		SessionDurationSeconds:  23400,
		SessionCompletionStatus: SessionCompletionCompleted,
		TotalPnL:                &totalPnL,
		StrategyReturnPct:       &returnPct,
		RealizedPnL:             &realizedPnL,
		PositionsOpenedCount:    3,
		PositionsClosedCount:    2,
		OrdersSubmitted:         5,
		OrdersFilled:            4,
		BuyFills:                3,
		SellFills:               2,
		DecisionCycles:          100,
		ActionableDecisions:     8,
		BlockedCyclesCount:      5,
		RiskEvaluations:         10,
		RiskRejectedOrders:      1,
	}

	csv := ExportSessionReportCSV(report)

	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 data row), got %d", len(lines))
	}

	header := lines[0]
	expectedColumns := []string{
		"session_date",
		"trader_id",
		"mode",
		"strategy_mode",
		"broker",
		"session_start",
		"session_end",
		"duration_seconds",
		"completion_status",
		"strategy_return_pct",
		"total_pnl",
		"realized_pnl",
		"positions_opened",
		"positions_closed",
		"orders_submitted",
		"orders_filled",
		"buy_fills",
		"sell_fills",
		"decision_cycles",
		"actionable_decisions",
		"blocked_cycles",
		"risk_evaluations",
		"risk_rejected",
		"risk_reduced",
		"risk_incidents",
		"critical_risk_incidents",
		"incident_count",
		"critical_incidents",
		"warnings",
		"errors",
	}
	for _, col := range expectedColumns {
		if !strings.Contains(header, col) {
			t.Fatalf("expected CSV header to contain column %q", col)
		}
	}

	dataRow := lines[1]
	if !strings.Contains(dataRow, "2026-03-15") {
		t.Fatalf("expected CSV data to contain session date")
	}
	if !strings.Contains(dataRow, "paper_trader") {
		t.Fatalf("expected CSV data to contain trader id")
	}
	if !strings.Contains(dataRow, "0.5000") {
		t.Fatalf("expected CSV data to contain strategy return pct")
	}
}

func TestExportSessionReportCSVHandlesNilReport(t *testing.T) {
	csv := ExportSessionReportCSV(nil)
	if csv != "" {
		t.Fatalf("expected empty string for nil report, got %q", csv)
	}
}

func TestExportSessionReportCSVHandlesNilPnLFields(t *testing.T) {
	report := &PaperSessionReport{
		TraderID:                "paper_trader",
		Mode:                    "paper",
		StrategyMode:            "multi_factor",
		Broker:                  "ibkr",
		SessionDate:             "2026-03-15",
		SessionStart:            time.Date(2026, 3, 15, 9, 30, 0, 0, time.Local),
		SessionEnd:              time.Date(2026, 3, 15, 16, 0, 0, 0, time.Local),
		SessionCompletionStatus: SessionCompletionBlocked,
	}

	csv := ExportSessionReportCSV(report)
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines for nil-pnl report, got %d", len(lines))
	}
}
