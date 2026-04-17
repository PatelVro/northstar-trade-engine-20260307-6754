package trader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildDailyScorecardWithZeroSessions(t *testing.T) {
	sc := BuildDailyScorecard("paper_trader", time.Date(2026, 3, 15, 0, 0, 0, 0, time.Local), nil)
	if sc == nil {
		t.Fatalf("expected non-nil scorecard for zero sessions")
	}
	if sc.SessionCount != 0 {
		t.Fatalf("expected 0 sessions, got %d", sc.SessionCount)
	}
	if sc.TraderID != "paper_trader" {
		t.Fatalf("expected trader id paper_trader, got %s", sc.TraderID)
	}
	if sc.Date != "2026-03-15" {
		t.Fatalf("expected date 2026-03-15, got %s", sc.Date)
	}
	if sc.Grade != "C" {
		t.Fatalf("expected grade C for zero sessions, got %s", sc.Grade)
	}
}

func TestBuildDailyScorecardWithMultipleSessions(t *testing.T) {
	ret1 := 1.5
	pnl1 := 1500.0
	equity1 := 100000.0
	ret2 := -0.3
	pnl2 := -300.0
	equity2 := 101500.0

	sessions := []*PaperSessionReport{
		{
			TraderID:             "paper_trader",
			StrategyReturnPct:    &ret1,
			TotalPnL:             &pnl1,
			StartingStrategyEquity: &equity1,
			PositionsOpenedCount: 3,
			PositionsClosedCount: 2,
			DecisionCycles:       50,
			BlockedCyclesCount:   2,
			RiskIncidentCount:    0,
			PortfolioRiskPeaks: &SessionPortfolioRiskPeaks{
				MaxDrawdownPct: 0.05,
			},
		},
		{
			TraderID:             "paper_trader",
			StrategyReturnPct:    &ret2,
			TotalPnL:             &pnl2,
			StartingStrategyEquity: &equity2,
			PositionsOpenedCount: 1,
			PositionsClosedCount: 3,
			DecisionCycles:       40,
			BlockedCyclesCount:   1,
			RiskIncidentCount:    0,
			PortfolioRiskPeaks: &SessionPortfolioRiskPeaks{
				MaxDrawdownPct: 0.08,
			},
		},
	}

	sc := BuildDailyScorecard("paper_trader", time.Date(2026, 3, 15, 0, 0, 0, 0, time.Local), sessions)

	if sc.SessionCount != 2 {
		t.Fatalf("expected 2 sessions, got %d", sc.SessionCount)
	}
	expectedReturn := 1.5 + (-0.3)
	if sc.TotalReturnPct < expectedReturn-0.01 || sc.TotalReturnPct > expectedReturn+0.01 {
		t.Fatalf("expected total return %.2f, got %.2f", expectedReturn, sc.TotalReturnPct)
	}
	if sc.TradesOpened != 4 {
		t.Fatalf("expected 4 trades opened, got %d", sc.TradesOpened)
	}
	if sc.TradesClosed != 5 {
		t.Fatalf("expected 5 trades closed, got %d", sc.TradesClosed)
	}
	if sc.BlockedCycles != 3 {
		t.Fatalf("expected 3 blocked cycles, got %d", sc.BlockedCycles)
	}
	// Max drawdown should be the max across sessions (0.08 -> 8%)
	if sc.MaxDrawdownPct < 7.99 || sc.MaxDrawdownPct > 8.01 {
		t.Fatalf("expected max drawdown ~8%%, got %.2f%%", sc.MaxDrawdownPct)
	}
	// Win rate: 1 of 2 sessions positive = 50%
	if sc.WinRate < 49.9 || sc.WinRate > 50.1 {
		t.Fatalf("expected win rate ~50%%, got %.1f%%", sc.WinRate)
	}
}

func TestGradeDayAssignments(t *testing.T) {
	tests := []struct {
		name     string
		card     DailyScorecard
		expected string
	}{
		{
			name:     "A grade: positive sharpe, no incidents",
			card:     DailyScorecard{TotalReturnPct: 1.5, DailySharpe: 0.8, RiskIncidents: 0},
			expected: "A",
		},
		{
			name:     "B grade: positive return with incidents",
			card:     DailyScorecard{TotalReturnPct: 0.5, DailySharpe: 0.3, RiskIncidents: 1},
			expected: "B",
		},
		{
			name:     "B grade: positive return but negative sharpe",
			card:     DailyScorecard{TotalReturnPct: 0.5, DailySharpe: -0.1, RiskIncidents: 0},
			expected: "B",
		},
		{
			name:     "C grade: flat return",
			card:     DailyScorecard{TotalReturnPct: 0.0, DailySharpe: 0.0, RiskIncidents: 0},
			expected: "C",
		},
		{
			name:     "D grade: small loss no incidents",
			card:     DailyScorecard{TotalReturnPct: -0.5, DailySharpe: -0.3, RiskIncidents: 0},
			expected: "D",
		},
		{
			name:     "F grade: large loss",
			card:     DailyScorecard{TotalReturnPct: -2.0, DailySharpe: -1.5, RiskIncidents: 0},
			expected: "F",
		},
		{
			name:     "F grade: loss with incidents",
			card:     DailyScorecard{TotalReturnPct: -0.3, DailySharpe: -0.2, RiskIncidents: 2},
			expected: "F",
		},
		{
			name:     "nil scorecard",
			card:     DailyScorecard{},
			expected: "C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GradeDay(&tt.card)
			if got != tt.expected {
				t.Fatalf("GradeDay(%s): expected %s, got %s", tt.name, tt.expected, got)
			}
		})
	}
}

func TestGradeDayNilScorecard(t *testing.T) {
	got := GradeDay(nil)
	if got != "F" {
		t.Fatalf("expected F for nil scorecard, got %s", got)
	}
}

func TestFormatDailyScorecardMarkdownContainsExpectedContent(t *testing.T) {
	sc := &DailyScorecard{
		TraderID:       "paper_trader",
		Date:           "2026-03-15",
		SessionCount:   3,
		TotalReturnPct: 1.2,
		DailySharpe:    0.85,
		MaxDrawdownPct: 3.5,
		TradesOpened:   5,
		TradesClosed:   4,
		WinRate:        66.7,
		AvgHoldCycles:  25.0,
		BlockedCycles:  2,
		RiskIncidents:  0,
		RunningStreak:  3,
		CumulativeReturn: 4.5,
		Grade:          "A",
	}

	md := FormatDailyScorecardMarkdown(sc)

	expectedContent := []string{
		"# Daily Scorecard: 2026-03-15",
		"paper_trader",
		"Grade: A",
		"1.20%",
		"0.85",
		"3.50%",
		"66.7%",
		"Consecutive Profitable Days",
		"Cumulative Return",
	}
	for _, content := range expectedContent {
		if !strings.Contains(md, content) {
			t.Fatalf("expected scorecard Markdown to contain %q", content)
		}
	}
}

func TestFormatDailyScorecardMarkdownHandlesNil(t *testing.T) {
	md := FormatDailyScorecardMarkdown(nil)
	if md != "" {
		t.Fatalf("expected empty string for nil scorecard, got %q", md)
	}
}

func TestWriteDailyScorecardWritesFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	sc := &DailyScorecard{
		TraderID:       "paper_trader",
		Date:           "2026-03-15",
		SessionCount:   1,
		TotalReturnPct: 0.5,
		Grade:          "B",
	}

	if err := writeDailyScorecard(sc); err != nil {
		t.Fatalf("writeDailyScorecard failed: %v", err)
	}

	jsonPath := filepath.Join("output", "session_reports", "paper_trader", "paper_trader_scorecard_20260315.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("expected scorecard JSON file to exist: %v", err)
	}

	var parsed DailyScorecard
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("failed to parse scorecard JSON: %v", err)
	}
	if parsed.TraderID != "paper_trader" {
		t.Fatalf("unexpected trader id in scorecard: %s", parsed.TraderID)
	}
	if parsed.Grade != "B" {
		t.Fatalf("unexpected grade in scorecard: %s", parsed.Grade)
	}

	mdPath := filepath.Join("output", "session_reports", "paper_trader", "paper_trader_scorecard_20260315.md")
	mdRaw, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("expected scorecard Markdown file to exist: %v", err)
	}
	if !strings.Contains(string(mdRaw), "Daily Scorecard") {
		t.Fatalf("expected Markdown file to contain scorecard heading")
	}
}

func TestComputeDailySharpeWithSingleReturn(t *testing.T) {
	if s := computeDailySharpe([]float64{1.5}); s != 1.0 {
		t.Fatalf("expected sharpe 1.0 for single positive return, got %.2f", s)
	}
	if s := computeDailySharpe([]float64{-0.5}); s != -1.0 {
		t.Fatalf("expected sharpe -1.0 for single negative return, got %.2f", s)
	}
	if s := computeDailySharpe(nil); s != 0 {
		t.Fatalf("expected sharpe 0 for nil returns, got %.2f", s)
	}
}

func TestComputeDailySharpeWithMultipleReturns(t *testing.T) {
	returns := []float64{1.0, 1.0, 1.0}
	s := computeDailySharpe(returns)
	// All same positive returns -> mean > 0, stddev = 0 -> should return 1.0
	if s != 1.0 {
		t.Fatalf("expected sharpe 1.0 for identical positive returns, got %.2f", s)
	}
}
