// Package trader - report_export.go
// Generates human-readable Markdown and machine-readable CSV exports
// from paper session reports. These are the artifacts that accumulate
// the evidence trail for live promotion.
package trader

import (
	"fmt"
	"os"
	"strings"
)

// ExportSessionReportMarkdown generates a structured Markdown document from
// a paper session report. The caller is responsible for writing the returned
// string to disk.
func ExportSessionReportMarkdown(report *PaperSessionReport) string {
	if report == nil {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("# Session Report: %s\n\n", report.TraderName))
	b.WriteString(fmt.Sprintf("- **Trader ID:** %s\n", report.TraderID))
	b.WriteString(fmt.Sprintf("- **Date:** %s\n", report.SessionDate))
	b.WriteString(fmt.Sprintf("- **Mode:** %s\n", report.Mode))
	b.WriteString(fmt.Sprintf("- **Strategy:** %s\n", report.StrategyMode))
	b.WriteString(fmt.Sprintf("- **Broker:** %s\n", report.Broker))
	b.WriteString(fmt.Sprintf("- **Session Start:** %s\n", report.SessionStart.Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- **Session End:** %s\n", report.SessionEnd.Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- **Duration:** %ds\n", report.SessionDurationSeconds))
	b.WriteString(fmt.Sprintf("- **Completion Status:** %s\n", report.SessionCompletionStatus))
	b.WriteString("\n")

	// Summary table
	b.WriteString("## Performance Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Total Return | %s |\n", formatExportPct(report.StrategyReturnPct)))
	b.WriteString(fmt.Sprintf("| Total P&L | %s |\n", formatExportFloat(report.TotalPnL)))
	b.WriteString(fmt.Sprintf("| Realized P&L | %s |\n", formatExportFloat(report.RealizedPnL)))
	b.WriteString(fmt.Sprintf("| Unrealized P&L | %s |\n", formatExportFloat(report.UnrealizedPnLEnd)))
	b.WriteString(fmt.Sprintf("| Max Drawdown | %s |\n", formatExportDrawdown(report.PortfolioRiskPeaks)))
	b.WriteString(fmt.Sprintf("| Positions Opened | %d |\n", report.PositionsOpenedCount))
	b.WriteString(fmt.Sprintf("| Positions Closed | %d |\n", report.PositionsClosedCount))
	b.WriteString(fmt.Sprintf("| Orders Submitted | %d |\n", report.OrdersSubmitted))
	b.WriteString(fmt.Sprintf("| Orders Filled | %d |\n", report.OrdersFilled))
	b.WriteString(fmt.Sprintf("| Win Rate | %s |\n", formatExportWinRate(report)))
	b.WriteString(fmt.Sprintf("| Decision Cycles | %d |\n", report.DecisionCycles))
	b.WriteString(fmt.Sprintf("| Actionable Decisions | %d |\n", report.ActionableDecisions))
	b.WriteString("\n")

	// Execution summary
	b.WriteString("## Execution Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Execution Intents | %d |\n", report.ExecutionIntentsTotal))
	b.WriteString(fmt.Sprintf("| Submitted | %d |\n", report.ExecutionSubmittedCount))
	b.WriteString(fmt.Sprintf("| Filled | %d |\n", report.ExecutionFilledCount))
	b.WriteString(fmt.Sprintf("| Rejected | %d |\n", report.ExecutionRejectedCount))
	b.WriteString(fmt.Sprintf("| Blocked | %d |\n", report.ExecutionBlockedCount))
	b.WriteString(fmt.Sprintf("| Duplicate Suppressed | %d |\n", report.DuplicateSuppressedCount))
	b.WriteString(fmt.Sprintf("| Stale | %d |\n", report.StaleExecutionCount))
	b.WriteString(fmt.Sprintf("| Failed | %d |\n", report.ExecutionFailedCount))
	b.WriteString("\n")

	// Trade log
	b.WriteString("## Trade Log\n\n")
	if len(report.SymbolsTraded) == 0 {
		b.WriteString("No trades recorded this session.\n\n")
	} else {
		b.WriteString(fmt.Sprintf("Symbols traded: %s\n\n", strings.Join(report.SymbolsTraded, ", ")))
		b.WriteString("| Metric | Value |\n")
		b.WriteString("|--------|-------|\n")
		b.WriteString(fmt.Sprintf("| Buy Fills | %d |\n", report.BuyFills))
		b.WriteString(fmt.Sprintf("| Sell Fills | %d |\n", report.SellFills))
		b.WriteString(fmt.Sprintf("| Max Concurrent Positions | %d |\n", report.MaxConcurrentPositionsObserved))
		b.WriteString(fmt.Sprintf("| Max Gross Exposure | %s |\n", formatExportFloat(report.MaxGrossExposureObserved)))
		b.WriteString("\n")
	}

	// Risk events
	b.WriteString("## Risk Events\n\n")
	b.WriteString(fmt.Sprintf("- **Risk Mode (final):** %s\n", string(report.FinalRiskMode)))
	b.WriteString(fmt.Sprintf("- **Risk Evaluations:** %d\n", report.RiskEvaluations))
	b.WriteString(fmt.Sprintf("- **Risk Rejected Orders:** %d\n", report.RiskRejectedOrders))
	b.WriteString(fmt.Sprintf("- **Risk Reduced Orders:** %d\n", report.RiskReducedOrders))
	b.WriteString(fmt.Sprintf("- **Risk Incidents:** %d\n", report.RiskIncidentCount))
	b.WriteString(fmt.Sprintf("- **Critical Risk Incidents:** %d\n", report.CriticalRiskIncidentCount))
	b.WriteString(fmt.Sprintf("- **Supervisor Restricted:** %v\n", report.SupervisorRestrictedDuringSession))
	if len(report.NotableRiskIncidents) > 0 {
		b.WriteString("\n**Notable Risk Incidents:**\n")
		for _, incident := range report.NotableRiskIncidents {
			b.WriteString(fmt.Sprintf("- %s\n", incident))
		}
	}
	b.WriteString("\n")

	// Incidents
	if report.IncidentCount > 0 || len(report.NotableIncidents) > 0 {
		b.WriteString("## Operational Incidents\n\n")
		b.WriteString(fmt.Sprintf("- **Incident Count:** %d\n", report.IncidentCount))
		b.WriteString(fmt.Sprintf("- **Critical Incidents:** %d\n", report.CriticalIncidentCount))
		if len(report.NotableIncidents) > 0 {
			b.WriteString("\n**Notable Incidents:**\n")
			for _, incident := range report.NotableIncidents {
				b.WriteString(fmt.Sprintf("- %s\n", incident))
			}
		}
		b.WriteString("\n")
	}

	// Decision quality
	b.WriteString("## Decision Quality\n\n")
	b.WriteString(fmt.Sprintf("- **Blocked Cycles:** %d (expected: %d, unexpected: %d)\n",
		report.BlockedCyclesCount, report.ExpectedBlockedCyclesCount, report.UnexpectedBlockedCyclesCount))
	b.WriteString(fmt.Sprintf("- **Warnings:** %d\n", report.WarningsCount))
	b.WriteString(fmt.Sprintf("- **Errors:** %d\n", report.ErrorsCount))
	if report.LastBlockReason != "" {
		b.WriteString(fmt.Sprintf("- **Last Block Reason:** %s\n", report.LastBlockReason))
	}
	b.WriteString("\n")

	// Reconciliation
	if report.OrderReconciliationRuns > 0 || report.PositionReconciliationRuns > 0 {
		b.WriteString("## Reconciliation\n\n")
		b.WriteString(fmt.Sprintf("- **Order Recon Runs:** %d (mismatches: %d, repairs: %d)\n",
			report.OrderReconciliationRuns, report.OrderReconciliationMismatches, report.OrderReconciliationRepairs))
		b.WriteString(fmt.Sprintf("- **Position Recon Runs:** %d (mismatches: %d, incidents: %d)\n",
			report.PositionReconciliationRuns, report.PositionReconciliationMismatches, report.PositionReconciliationIncidents))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Generated at %s | Report version %d*\n",
		report.GeneratedAt.Format("2006-01-02 15:04:05"), report.ReportVersion))

	return b.String()
}

// ExportSessionReportCSV generates a CSV with one row per key session metric.
// Since session reports aggregate at the session level (not per-trade), the CSV
// captures the session-level execution and performance summary suitable for
// quantitative analysis across sessions.
func ExportSessionReportCSV(report *PaperSessionReport) string {
	if report == nil {
		return ""
	}

	var b strings.Builder

	// Header row
	b.WriteString("session_date,trader_id,mode,strategy_mode,broker,")
	b.WriteString("session_start,session_end,duration_seconds,completion_status,")
	b.WriteString("strategy_return_pct,total_pnl,realized_pnl,")
	b.WriteString("positions_opened,positions_closed,orders_submitted,orders_filled,")
	b.WriteString("buy_fills,sell_fills,")
	b.WriteString("decision_cycles,actionable_decisions,blocked_cycles,")
	b.WriteString("risk_evaluations,risk_rejected,risk_reduced,")
	b.WriteString("risk_incidents,critical_risk_incidents,incident_count,critical_incidents,")
	b.WriteString("warnings,errors\n")

	// Data row
	b.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,",
		report.SessionDate,
		csvEscape(report.TraderID),
		csvEscape(report.Mode),
		csvEscape(report.StrategyMode),
		csvEscape(report.Broker),
	))
	b.WriteString(fmt.Sprintf("%s,%s,%d,%s,",
		report.SessionStart.Format("2006-01-02T15:04:05"),
		report.SessionEnd.Format("2006-01-02T15:04:05"),
		report.SessionDurationSeconds,
		csvEscape(string(report.SessionCompletionStatus)),
	))
	b.WriteString(fmt.Sprintf("%s,%s,%s,",
		csvFloat(report.StrategyReturnPct),
		csvFloat(report.TotalPnL),
		csvFloat(report.RealizedPnL),
	))
	b.WriteString(fmt.Sprintf("%d,%d,%d,%d,",
		report.PositionsOpenedCount,
		report.PositionsClosedCount,
		report.OrdersSubmitted,
		report.OrdersFilled,
	))
	b.WriteString(fmt.Sprintf("%d,%d,",
		report.BuyFills,
		report.SellFills,
	))
	b.WriteString(fmt.Sprintf("%d,%d,%d,",
		report.DecisionCycles,
		report.ActionableDecisions,
		report.BlockedCyclesCount,
	))
	b.WriteString(fmt.Sprintf("%d,%d,%d,",
		report.RiskEvaluations,
		report.RiskRejectedOrders,
		report.RiskReducedOrders,
	))
	b.WriteString(fmt.Sprintf("%d,%d,%d,%d,",
		report.RiskIncidentCount,
		report.CriticalRiskIncidentCount,
		report.IncidentCount,
		report.CriticalIncidentCount,
	))
	b.WriteString(fmt.Sprintf("%d,%d\n",
		report.WarningsCount,
		report.ErrorsCount,
	))

	return b.String()
}

// formatExportPct formats a nullable percentage for Markdown display.
func formatExportPct(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", *value)
}

// formatExportFloat formats a nullable float for Markdown display.
func formatExportFloat(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *value)
}

// formatExportDrawdown extracts max drawdown from portfolio risk peaks.
func formatExportDrawdown(peaks *SessionPortfolioRiskPeaks) string {
	if peaks == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", peaks.MaxDrawdownPct*100)
}

// formatExportWinRate computes a simple win rate from available session data.
// Since session reports do not track per-trade outcomes, this uses position
// close counts relative to opens as a proxy when full trade-level data is not
// available.
func formatExportWinRate(report *PaperSessionReport) string {
	if report.PositionsClosedCount == 0 {
		return "n/a"
	}
	if report.TotalPnL != nil && *report.TotalPnL > 0 {
		return "positive session"
	}
	if report.TotalPnL != nil && *report.TotalPnL < 0 {
		return "negative session"
	}
	return "flat"
}

// csvFloat formats a nullable float64 pointer for CSV output.
func csvFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%.4f", *value)
}

// csvEscape wraps a string in quotes if it contains commas or quotes.
func csvEscape(value string) string {
	if strings.ContainsAny(value, ",\"\n") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

// writeSessionReportExports writes the Markdown and CSV companion files
// alongside a JSON session report. The basePath should be the full path to
// the JSON report file (ending in .json).
func writeSessionReportExports(basePath string, report PaperSessionReport) {
	mdPath := strings.TrimSuffix(basePath, ".json") + ".md"
	csvPath := strings.TrimSuffix(basePath, ".json") + ".csv"

	mdContent := ExportSessionReportMarkdown(&report)
	if mdContent != "" {
		if err := writeExportFile(mdPath, mdContent); err != nil {
			fmt.Printf("  [%s] Failed to write Markdown session export: %v\n", report.TraderName, err)
		}
	}

	csvContent := ExportSessionReportCSV(&report)
	if csvContent != "" {
		if err := writeExportFile(csvPath, csvContent); err != nil {
			fmt.Printf("  [%s] Failed to write CSV session export: %v\n", report.TraderName, err)
		}
	}
}

// writeExportFile writes content to a file, creating directories as needed.
func writeExportFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
