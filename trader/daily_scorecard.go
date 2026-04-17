// Package trader - daily_scorecard.go
// Aggregates all paper session reports from a calendar day into a single
// daily scorecard. This provides the daily pulse check that operators
// and the promotion system need.
package trader

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DailyScorecard aggregates session-level metrics into a single daily summary
// used for operator review and automated promotion evaluation.
type DailyScorecard struct {
	TraderID         string  `json:"trader_id"`
	Date             string  `json:"date"` // YYYY-MM-DD
	SessionCount     int     `json:"session_count"`
	TotalReturnPct   float64 `json:"total_return_pct"`
	DailySharpe      float64 `json:"daily_sharpe"`
	MaxDrawdownPct   float64 `json:"max_drawdown_pct"`
	TradesOpened     int     `json:"trades_opened"`
	TradesClosed     int     `json:"trades_closed"`
	WinRate          float64 `json:"win_rate"`
	AvgHoldCycles    float64 `json:"avg_hold_cycles"`
	BlockedCycles    int     `json:"blocked_cycles"`
	RiskIncidents    int     `json:"risk_incidents"`
	RunningStreak    int     `json:"running_streak"`          // consecutive profitable days
	CumulativeReturn float64 `json:"cumulative_return_pct"`   // since first scorecard
	Grade            string  `json:"grade"`                   // A/B/C/D/F based on risk-adjusted return
}

// BuildDailyScorecard aggregates a set of paper session reports for a single
// calendar day into a DailyScorecard. The sessions slice may be empty, which
// produces a zero-value scorecard for that day.
func BuildDailyScorecard(traderID string, date time.Time, sessions []*PaperSessionReport) *DailyScorecard {
	sc := &DailyScorecard{
		TraderID:     traderID,
		Date:         date.Format("2006-01-02"),
		SessionCount: len(sessions),
	}

	if len(sessions) == 0 {
		sc.Grade = GradeDay(sc)
		return sc
	}

	totalReturnPct := 0.0
	totalPnL := 0.0
	totalStartEquity := 0.0
	maxDrawdown := 0.0
	totalOpened := 0
	totalClosed := 0
	totalBlockedCycles := 0
	totalRiskIncidents := 0
	totalDecisionCycles := 0

	returns := make([]float64, 0, len(sessions))

	for _, session := range sessions {
		if session == nil {
			continue
		}

		if session.StrategyReturnPct != nil {
			sessionReturn := *session.StrategyReturnPct
			totalReturnPct += sessionReturn
			returns = append(returns, sessionReturn)
		}

		if session.TotalPnL != nil {
			totalPnL += *session.TotalPnL
		}
		if session.StartingStrategyEquity != nil {
			totalStartEquity += *session.StartingStrategyEquity
		}

		if session.PortfolioRiskPeaks != nil {
			if session.PortfolioRiskPeaks.MaxDrawdownPct > maxDrawdown {
				maxDrawdown = session.PortfolioRiskPeaks.MaxDrawdownPct
			}
		}

		totalOpened += session.PositionsOpenedCount
		totalClosed += session.PositionsClosedCount
		totalBlockedCycles += session.BlockedCyclesCount
		totalRiskIncidents += session.RiskIncidentCount
		totalDecisionCycles += session.DecisionCycles
	}

	sc.TotalReturnPct = sanitizeFloat(totalReturnPct)
	sc.MaxDrawdownPct = sanitizeFloat(maxDrawdown * 100) // convert ratio to pct
	sc.TradesOpened = totalOpened
	sc.TradesClosed = totalClosed
	sc.BlockedCycles = totalBlockedCycles
	sc.RiskIncidents = totalRiskIncidents

	// Compute win rate as fraction of sessions with positive return
	if len(returns) > 0 {
		wins := 0
		for _, r := range returns {
			if r > 0 {
				wins++
			}
		}
		sc.WinRate = sanitizeFloat(float64(wins) / float64(len(returns)) * 100)
	}

	// Avg hold cycles: total decision cycles per opened position
	if totalOpened > 0 {
		sc.AvgHoldCycles = sanitizeFloat(float64(totalDecisionCycles) / float64(totalOpened))
	}

	// Compute daily Sharpe-like ratio: mean return / stdev of returns
	sc.DailySharpe = computeDailySharpe(returns)

	sc.Grade = GradeDay(sc)

	return sc
}

// GradeDay assigns an A/B/C/D/F grade based on risk-adjusted return metrics.
//   - A: positive Sharpe and no risk incidents
//   - B: positive return (but may have incidents or weak Sharpe)
//   - C: flat (near-zero return)
//   - D: small loss (return > -1%)
//   - F: large loss (return <= -1%) or any risk incidents with loss
func GradeDay(scorecard *DailyScorecard) string {
	if scorecard == nil {
		return "F"
	}

	hasIncidents := scorecard.RiskIncidents > 0
	ret := scorecard.TotalReturnPct

	// F: large loss or loss with incidents
	if ret <= -1.0 {
		return "F"
	}
	if ret < 0 && hasIncidents {
		return "F"
	}

	// D: small loss without incidents
	if ret < 0 {
		return "D"
	}

	// C: flat (near-zero)
	if ret >= -0.01 && ret <= 0.01 {
		return "C"
	}

	// A: positive Sharpe and no incidents
	if scorecard.DailySharpe > 0 && !hasIncidents {
		return "A"
	}

	// B: positive return but with incidents or weak Sharpe
	if ret > 0 {
		return "B"
	}

	return "C"
}

// FormatDailyScorecardMarkdown generates a human-readable Markdown summary
// of a daily scorecard for operator review.
func FormatDailyScorecardMarkdown(sc *DailyScorecard) string {
	if sc == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Daily Scorecard: %s\n\n", sc.Date))
	b.WriteString(fmt.Sprintf("**Trader:** %s | **Grade: %s**\n\n", sc.TraderID, sc.Grade))

	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Sessions | %d |\n", sc.SessionCount))
	b.WriteString(fmt.Sprintf("| Total Return | %.2f%% |\n", sc.TotalReturnPct))
	b.WriteString(fmt.Sprintf("| Daily Sharpe | %.2f |\n", sc.DailySharpe))
	b.WriteString(fmt.Sprintf("| Max Drawdown | %.2f%% |\n", sc.MaxDrawdownPct))
	b.WriteString(fmt.Sprintf("| Trades Opened | %d |\n", sc.TradesOpened))
	b.WriteString(fmt.Sprintf("| Trades Closed | %d |\n", sc.TradesClosed))
	b.WriteString(fmt.Sprintf("| Win Rate | %.1f%% |\n", sc.WinRate))
	b.WriteString(fmt.Sprintf("| Avg Hold Cycles | %.1f |\n", sc.AvgHoldCycles))
	b.WriteString(fmt.Sprintf("| Blocked Cycles | %d |\n", sc.BlockedCycles))
	b.WriteString(fmt.Sprintf("| Risk Incidents | %d |\n", sc.RiskIncidents))
	b.WriteString("\n")

	if sc.RunningStreak != 0 || sc.CumulativeReturn != 0 {
		b.WriteString("## Running Totals\n\n")
		b.WriteString(fmt.Sprintf("- **Consecutive Profitable Days:** %d\n", sc.RunningStreak))
		b.WriteString(fmt.Sprintf("- **Cumulative Return:** %.2f%%\n", sc.CumulativeReturn))
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Grade: %s*\n", gradeDescription(sc.Grade)))

	return b.String()
}

// computeDailySharpe computes a simplified Sharpe-like ratio from a slice of
// session returns. Returns 0 if there are fewer than 2 data points or if the
// standard deviation is zero.
func computeDailySharpe(returns []float64) float64 {
	n := len(returns)
	if n < 2 {
		if n == 1 && returns[0] > 0 {
			return 1.0
		}
		if n == 1 && returns[0] < 0 {
			return -1.0
		}
		return 0
	}

	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(n)

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(n - 1)
	stddev := math.Sqrt(variance)

	if stddev == 0 {
		if mean > 0 {
			return 1.0
		}
		if mean < 0 {
			return -1.0
		}
		return 0
	}

	return sanitizeFloat(mean / stddev)
}

// gradeDescription returns a human-readable explanation for each grade level.
func gradeDescription(grade string) string {
	switch grade {
	case "A":
		return "A - Positive risk-adjusted return, no incidents"
	case "B":
		return "B - Positive return"
	case "C":
		return "C - Flat"
	case "D":
		return "D - Small loss"
	case "F":
		return "F - Large loss or loss with incidents"
	default:
		return grade
	}
}

// scorecardPath returns the file path for a daily scorecard JSON file.
func scorecardPath(traderID string, date string) string {
	return filepath.Join("output", "session_reports", traderID,
		fmt.Sprintf("%s_scorecard_%s.json", traderID, strings.ReplaceAll(date, "-", "")))
}

// scorecardMarkdownPath returns the file path for a daily scorecard Markdown file.
func scorecardMarkdownPath(traderID string, date string) string {
	return filepath.Join("output", "session_reports", traderID,
		fmt.Sprintf("%s_scorecard_%s.md", traderID, strings.ReplaceAll(date, "-", "")))
}

// writeDailyScorecard writes both JSON and Markdown scorecard files to disk.
func writeDailyScorecard(sc *DailyScorecard) error {
	if sc == nil {
		return fmt.Errorf("nil scorecard")
	}

	jsonPath := scorecardPath(sc.TraderID, sc.Date)
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0755); err != nil {
		return fmt.Errorf("create scorecard directory: %w", err)
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal scorecard: %w", err)
	}
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		return fmt.Errorf("write scorecard json: %w", err)
	}

	mdPath := scorecardMarkdownPath(sc.TraderID, sc.Date)
	mdContent := FormatDailyScorecardMarkdown(sc)
	if mdContent != "" {
		if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
			return fmt.Errorf("write scorecard markdown: %w", err)
		}
	}

	return nil
}
