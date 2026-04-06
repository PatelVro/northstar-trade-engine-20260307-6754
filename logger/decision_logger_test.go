package logger

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AccountSnapshot methods
// ---------------------------------------------------------------------------

func TestHasCanonicalAccounting(t *testing.T) {
	cases := []struct {
		version int
		want    bool
	}{
		{0, false},
		{1, false},
		{2, true},
		{3, true},
	}
	for _, tc := range cases {
		s := AccountSnapshot{AccountingVersion: tc.version}
		if got := s.HasCanonicalAccounting(); got != tc.want {
			t.Errorf("version %d: got %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestEffectiveAccountEquity_CanonicalReturnsAccountEquity(t *testing.T) {
	s := AccountSnapshot{AccountingVersion: 2, AccountEquity: 105000, TotalBalance: 99000}
	if got := s.EffectiveAccountEquity(); got != 105000 {
		t.Fatalf("expected 105000, got %.2f", got)
	}
}

func TestEffectiveAccountEquity_LegacyReturnsTotalBalance(t *testing.T) {
	s := AccountSnapshot{AccountingVersion: 1, AccountEquity: 105000, TotalBalance: 99000}
	if got := s.EffectiveAccountEquity(); got != 99000 {
		t.Fatalf("expected 99000, got %.2f", got)
	}
}

func TestEffectiveStrategyEquity_CanonicalWithCapital(t *testing.T) {
	s := AccountSnapshot{AccountingVersion: 2, StrategyEquity: 102000, StrategyInitialCapital: 100000}
	equity, ok := s.EffectiveStrategyEquity()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if equity != 102000 {
		t.Fatalf("expected 102000, got %.2f", equity)
	}
}

func TestEffectiveStrategyEquity_CanonicalWithoutCapital(t *testing.T) {
	s := AccountSnapshot{AccountingVersion: 2, StrategyEquity: 102000, StrategyInitialCapital: 0}
	_, ok := s.EffectiveStrategyEquity()
	if ok {
		t.Fatal("expected ok=false when no initial capital")
	}
}

func TestEffectiveStrategyEquity_Legacy(t *testing.T) {
	s := AccountSnapshot{AccountingVersion: 1, StrategyEquity: 102000}
	equity, ok := s.EffectiveStrategyEquity()
	if ok {
		t.Fatal("expected ok=false for legacy")
	}
	if equity != 0 {
		t.Fatalf("expected 0, got %.2f", equity)
	}
}

// ---------------------------------------------------------------------------
// NewDecisionLogger
// ---------------------------------------------------------------------------

func TestNewDecisionLogger_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "test_logs")
	dl := NewDecisionLogger(logDir)
	if dl == nil {
		t.Fatal("expected non-nil logger")
	}
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Fatal("expected log directory to exist")
	}
}

func TestNewDecisionLogger_DefaultDir(t *testing.T) {
	// Save and restore cwd
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	_ = os.Chdir(tmp)
	defer os.Chdir(orig)

	dl := NewDecisionLogger("")
	if dl.logDir != "decision_logs" {
		t.Fatalf("expected default 'decision_logs', got %q", dl.logDir)
	}
}

// ---------------------------------------------------------------------------
// LogDecision + GetLatestRecords round-trip
// ---------------------------------------------------------------------------

func TestLogDecisionAndGetLatestRecords(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	// Log 3 records
	for i := 0; i < 3; i++ {
		record := &DecisionRecord{
			Success:     true,
			InputPrompt: "test prompt",
			Decisions: []DecisionAction{
				{Action: "open_long", Symbol: "AAPL", Success: true, Price: 150, Quantity: 10},
			},
		}
		if err := dl.LogDecision(record); err != nil {
			t.Fatalf("LogDecision %d failed: %v", i, err)
		}
		// Small sleep to ensure distinct filenames
		time.Sleep(time.Millisecond * 10)
	}

	// Retrieve all 3
	records, err := dl.GetLatestRecords(10)
	if err != nil {
		t.Fatalf("GetLatestRecords failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Verify chronological order (oldest first)
	if records[0].CycleNumber >= records[2].CycleNumber {
		t.Error("expected records in oldest-first order")
	}
}

func TestLogDecisionIncrementsCycleNumber(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	r1 := &DecisionRecord{Success: true}
	r2 := &DecisionRecord{Success: false, ErrorMessage: "test error"}

	_ = dl.LogDecision(r1)
	time.Sleep(time.Millisecond * 10)
	_ = dl.LogDecision(r2)

	if r1.CycleNumber != 1 {
		t.Errorf("expected cycle 1, got %d", r1.CycleNumber)
	}
	if r2.CycleNumber != 2 {
		t.Errorf("expected cycle 2, got %d", r2.CycleNumber)
	}
}

func TestNewDecisionLoggerContinuesCycleNumberFromExistingFiles(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("failed to create log dir: %v", err)
	}
	for _, name := range []string{
		"decision_20260326_101635_cycle1.json",
		"decision_20260326_102208_cycle2.json",
		"decision_20260326_110242_cycle7.json",
	} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to seed %s: %v", name, err)
		}
	}

	dl := NewDecisionLogger(logDir)
	record := &DecisionRecord{Success: true}
	if err := dl.LogDecision(record); err != nil {
		t.Fatalf("LogDecision failed: %v", err)
	}
	if record.CycleNumber != 8 {
		t.Fatalf("expected cycle 8 after existing files, got %d", record.CycleNumber)
	}
}

func TestLogDecisionWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	dl := NewDecisionLogger(logDir)

	record := &DecisionRecord{
		Success:     true,
		InputPrompt: "system prompt here",
		AccountState: AccountSnapshot{
			AccountingVersion: 2,
			AccountEquity:     100000,
		},
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "MSFT", Success: true, Price: 400, Quantity: 5},
		},
	}
	if err := dl.LogDecision(record); err != nil {
		t.Fatalf("LogDecision failed: %v", err)
	}

	// Read back and verify JSON is valid
	entries, _ := os.ReadDir(logDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var parsed DecisionRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("log file is not valid JSON: %v", err)
	}
	if parsed.AccountState.AccountEquity != 100000 {
		t.Errorf("expected account equity 100000, got %.2f", parsed.AccountState.AccountEquity)
	}
	if len(parsed.Decisions) != 1 || parsed.Decisions[0].Symbol != "MSFT" {
		t.Error("decision data not preserved in round-trip")
	}
}

func TestLogDecisionSanitizesNonFiniteFloats(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	dl := NewDecisionLogger(logDir)

	record := &DecisionRecord{
		Success:     true,
		InputPrompt: "nan test",
		AccountState: AccountSnapshot{
			AccountingVersion: 2,
			AccountEquity:     math.NaN(),
			MarginUsedPct:     math.Inf(1),
		},
		Pipeline: []PipelineObservation{
			{
				Symbol:           "COP",
				RegimeScore:      math.NaN(),
				RegimeConfidence: math.Inf(-1),
			},
		},
		Decisions: []DecisionAction{
			{
				Action:               "open_long",
				Symbol:               "COP",
				Success:              true,
				Price:                math.Inf(1),
				Quantity:             math.NaN(),
				DecisionPositionSize: math.NaN(),
				DecisionStopLoss:     math.Inf(-1),
				DecisionTakeProfit:   math.Inf(1),
				Pipeline: &PipelineDecision{
					DecisionAllowed:       true,
					RecommendedQuantity:   math.NaN(),
					RecommendedNotional:   math.Inf(1),
					TargetPositionPct:     math.Inf(-1),
					RiskBudgetUsed:        math.NaN(),
					AllocationAllowTrade:  true,
					AllocationReducedSize: false,
				},
			},
		},
	}

	if err := dl.LogDecision(record); err != nil {
		t.Fatalf("LogDecision failed: %v", err)
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var parsed DecisionRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("expected sanitized JSON, got unmarshal error: %v", err)
	}

	if parsed.AccountState.AccountEquity != 0 {
		t.Fatalf("expected sanitized account equity to be 0, got %.2f", parsed.AccountState.AccountEquity)
	}
	if parsed.AccountState.MarginUsedPct != 0 {
		t.Fatalf("expected sanitized margin used pct to be 0, got %.2f", parsed.AccountState.MarginUsedPct)
	}
	if len(parsed.Pipeline) != 1 || parsed.Pipeline[0].RegimeScore != 0 || parsed.Pipeline[0].RegimeConfidence != 0 {
		t.Fatalf("expected sanitized pipeline floats, got %+v", parsed.Pipeline)
	}
	if len(parsed.Decisions) != 1 {
		t.Fatalf("expected one decision action, got %d", len(parsed.Decisions))
	}
	action := parsed.Decisions[0]
	if action.Price != 0 || action.Quantity != 0 || action.DecisionPositionSize != 0 || action.DecisionStopLoss != 0 || action.DecisionTakeProfit != 0 {
		t.Fatalf("expected sanitized action floats, got %+v", action)
	}
	if action.Pipeline == nil {
		t.Fatal("expected nested pipeline decision to be preserved")
	}
	if action.Pipeline.RecommendedQuantity != 0 || action.Pipeline.RecommendedNotional != 0 || action.Pipeline.TargetPositionPct != 0 || action.Pipeline.RiskBudgetUsed != 0 {
		t.Fatalf("expected sanitized nested pipeline floats, got %+v", action.Pipeline)
	}
	if len(parsed.ExecutionLog) == 0 {
		t.Fatal("expected sanitization note in execution log")
	}
}

func TestGetLatestRecords_LimitRespected(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	for i := 0; i < 5; i++ {
		_ = dl.LogDecision(&DecisionRecord{Success: true})
		time.Sleep(time.Millisecond * 10)
	}

	records, err := dl.GetLatestRecords(2)
	if err != nil {
		t.Fatalf("GetLatestRecords failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	// Should be the last 2 (newest), returned oldest-first
	if records[0].CycleNumber != 4 || records[1].CycleNumber != 5 {
		t.Errorf("expected cycles 4,5 got %d,%d", records[0].CycleNumber, records[1].CycleNumber)
	}
}

func TestGetLatestRecords_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	records, err := dl.GetLatestRecords(10)
	if err != nil {
		t.Fatalf("GetLatestRecords failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// GetRecordByDate
// ---------------------------------------------------------------------------

func TestGetRecordByDate_FindsMatchingRecords(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	dl := NewDecisionLogger(logDir)

	// Log a record — it will get today's date in the filename
	_ = dl.LogDecision(&DecisionRecord{Success: true})

	records, err := dl.GetRecordByDate(time.Now())
	if err != nil {
		t.Fatalf("GetRecordByDate failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record for today, got %d", len(records))
	}
}

func TestGetRecordByDate_NoMatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	dl := NewDecisionLogger(logDir)

	_ = dl.LogDecision(&DecisionRecord{Success: true})

	// Query for a date in the past
	pastDate := time.Now().AddDate(-1, 0, 0)
	records, err := dl.GetRecordByDate(pastDate)
	if err != nil {
		t.Fatalf("GetRecordByDate failed: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records for past date, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// GetStatistics
// ---------------------------------------------------------------------------

func TestGetStatistics_CountsCorrectly(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	// 2 successful cycles, 1 failed
	_ = dl.LogDecision(&DecisionRecord{
		Success: true,
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "AAPL", Success: true},
			{Action: "close_long", Symbol: "MSFT", Success: true},
		},
	})
	time.Sleep(time.Millisecond * 10)
	_ = dl.LogDecision(&DecisionRecord{
		Success: true,
		Decisions: []DecisionAction{
			{Action: "open_short", Symbol: "TSLA", Success: true},
		},
	})
	time.Sleep(time.Millisecond * 10)
	_ = dl.LogDecision(&DecisionRecord{
		Success:      false,
		ErrorMessage: "API timeout",
	})
	time.Sleep(time.Millisecond * 10)

	stats, err := dl.GetStatistics()
	if err != nil {
		t.Fatalf("GetStatistics failed: %v", err)
	}
	if stats.TotalCycles != 3 {
		t.Errorf("expected 3 total cycles, got %d", stats.TotalCycles)
	}
	if stats.SuccessfulCycles != 2 {
		t.Errorf("expected 2 successful, got %d", stats.SuccessfulCycles)
	}
	if stats.FailedCycles != 1 {
		t.Errorf("expected 1 failed, got %d", stats.FailedCycles)
	}
	if stats.TotalOpenPositions != 2 {
		t.Errorf("expected 2 opens (open_long + open_short), got %d", stats.TotalOpenPositions)
	}
	if stats.TotalClosePositions != 1 {
		t.Errorf("expected 1 close, got %d", stats.TotalClosePositions)
	}
}

func TestGetStatistics_IgnoresUnsuccessfulActions(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(filepath.Join(dir, "logs"))

	_ = dl.LogDecision(&DecisionRecord{
		Success: true,
		Decisions: []DecisionAction{
			{Action: "open_long", Symbol: "AAPL", Success: false, Error: "risk blocked"},
		},
	})
	time.Sleep(time.Millisecond * 10)

	stats, err := dl.GetStatistics()
	if err != nil {
		t.Fatalf("GetStatistics failed: %v", err)
	}
	if stats.TotalOpenPositions != 0 {
		t.Errorf("expected 0 opens for unsuccessful action, got %d", stats.TotalOpenPositions)
	}
}

// ---------------------------------------------------------------------------
// CleanOldRecords
// ---------------------------------------------------------------------------

func TestCleanOldRecords_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logDir, 0755)

	// Create a file and backdate its mtime
	oldFile := filepath.Join(logDir, "decision_20240101_000000_cycle1.json")
	_ = os.WriteFile(oldFile, []byte(`{}`), 0644)
	oldTime := time.Now().AddDate(0, 0, -100)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	// Create a recent file
	newFile := filepath.Join(logDir, "decision_20260322_120000_cycle2.json")
	_ = os.WriteFile(newFile, []byte(`{}`), 0644)

	dl := &DecisionLogger{logDir: logDir}
	if err := dl.CleanOldRecords(30); err != nil {
		t.Fatalf("CleanOldRecords failed: %v", err)
	}

	// Old file should be gone, new file should remain
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old file to be deleted")
	}
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("expected new file to remain")
	}
}

// ---------------------------------------------------------------------------
// calculateEquityMetrics
// ---------------------------------------------------------------------------

func TestCalculateEquityMetrics_EmptyRecords(t *testing.T) {
	dl := &DecisionLogger{}
	sharpe, maxDD, growth := dl.calculateEquityMetrics(nil)
	assertNearZero(t, "sharpe", sharpe)
	assertNearZero(t, "maxDD", maxDD)
	assertNearZero(t, "growth", growth)
}

func TestCalculateEquityMetrics_SingleRecord(t *testing.T) {
	dl := &DecisionLogger{}
	records := []*DecisionRecord{
		{AccountState: AccountSnapshot{AccountingVersion: 2, AccountEquity: 100000, StrategyEquity: 100000, StrategyInitialCapital: 100000}},
	}
	sharpe, maxDD, growth := dl.calculateEquityMetrics(records)
	assertNearZero(t, "sharpe single", sharpe)
	assertNearZero(t, "maxDD single", maxDD)
	assertNearZero(t, "growth single", growth)
}

func TestCalculateEquityMetrics_SteadyGrowth(t *testing.T) {
	dl := &DecisionLogger{}
	records := make([]*DecisionRecord, 5)
	for i := range records {
		equity := 100000.0 + float64(i)*1000.0
		records[i] = &DecisionRecord{
			AccountState: AccountSnapshot{
				AccountingVersion:      2,
				AccountEquity:          equity,
				StrategyEquity:         equity,
				StrategyInitialCapital: 100000,
			},
		}
	}
	sharpe, maxDD, growth := dl.calculateEquityMetrics(records)
	// Growth: (104000 - 100000) / 100000 * 100 = 4%
	assertFloatNear(t, "growth", growth, 4.0, 0.01)
	// No drawdown in steady growth
	assertNearZero(t, "maxDD", maxDD)
	// Sharpe should be positive (consistent positive returns)
	if sharpe <= 0 {
		t.Errorf("expected positive sharpe for steady growth, got %.4f", sharpe)
	}
}

func TestCalculateEquityMetrics_DrawdownDetected(t *testing.T) {
	dl := &DecisionLogger{}
	equities := []float64{100000, 105000, 95000, 98000}
	records := make([]*DecisionRecord, len(equities))
	for i, eq := range equities {
		records[i] = &DecisionRecord{
			AccountState: AccountSnapshot{
				AccountingVersion:      2,
				AccountEquity:          eq,
				StrategyEquity:         eq,
				StrategyInitialCapital: 100000,
			},
		}
	}
	_, maxDD, growth := dl.calculateEquityMetrics(records)
	// Max DD: peak 105000, trough 95000 → (105000-95000)/105000 * 100 ≈ 9.52%
	assertFloatNear(t, "maxDD", maxDD, 9.5238, 0.01)
	// Growth: (98000-100000)/100000 * 100 = -2%
	assertFloatNear(t, "growth", growth, -2.0, 0.01)
}

func TestCalculateEquityMetrics_AllLosses(t *testing.T) {
	dl := &DecisionLogger{}
	equities := []float64{100000, 95000, 90000}
	records := make([]*DecisionRecord, len(equities))
	for i, eq := range equities {
		records[i] = &DecisionRecord{
			AccountState: AccountSnapshot{
				AccountingVersion:      2,
				AccountEquity:          eq,
				StrategyEquity:         eq,
				StrategyInitialCapital: 100000,
			},
		}
	}
	sharpe, maxDD, growth := dl.calculateEquityMetrics(records)
	if sharpe >= 0 {
		t.Errorf("expected negative sharpe for all losses, got %.4f", sharpe)
	}
	if maxDD <= 0 {
		t.Errorf("expected positive maxDD for losses, got %.4f", maxDD)
	}
	if growth >= 0 {
		t.Errorf("expected negative growth for losses, got %.4f", growth)
	}
}

func TestCalculateEquityMetrics_ConstantEquity(t *testing.T) {
	dl := &DecisionLogger{}
	records := make([]*DecisionRecord, 4)
	for i := range records {
		records[i] = &DecisionRecord{
			AccountState: AccountSnapshot{
				AccountingVersion:      2,
				AccountEquity:          100000,
				StrategyEquity:         100000,
				StrategyInitialCapital: 100000,
			},
		}
	}
	sharpe, maxDD, growth := dl.calculateEquityMetrics(records)
	assertNearZero(t, "sharpe constant", sharpe)
	assertNearZero(t, "maxDD constant", maxDD)
	assertNearZero(t, "growth constant", growth)
}

func TestCalculateEquityMetrics_LegacyFallback(t *testing.T) {
	dl := &DecisionLogger{}
	records := []*DecisionRecord{
		{AccountState: AccountSnapshot{AccountingVersion: 1, TotalBalance: 100000}},
		{AccountState: AccountSnapshot{AccountingVersion: 1, TotalBalance: 110000}},
	}
	_, _, growth := dl.calculateEquityMetrics(records)
	assertFloatNear(t, "legacy growth", growth, 10.0, 0.01)
}

func TestCalculateEquityMetrics_SkipsZeroEquityRecords(t *testing.T) {
	dl := &DecisionLogger{}
	records := []*DecisionRecord{
		{AccountState: AccountSnapshot{AccountingVersion: 2, AccountEquity: 100000, StrategyEquity: 100000, StrategyInitialCapital: 100000}},
		{AccountState: AccountSnapshot{AccountingVersion: 2, AccountEquity: 0}}, // no equity data
		{AccountState: AccountSnapshot{AccountingVersion: 2, AccountEquity: 110000, StrategyEquity: 110000, StrategyInitialCapital: 100000}},
	}
	_, _, growth := dl.calculateEquityMetrics(records)
	// Should only use records with equity: 100000 → 110000
	assertFloatNear(t, "growth skipping zeros", growth, 10.0, 0.01)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertNearZero(t *testing.T, name string, got float64) {
	t.Helper()
	if math.Abs(got) > 0.001 {
		t.Errorf("%s: expected ~0, got %.6f", name, got)
	}
}

func assertFloatNear(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s: got %.6f, want %.6f (tolerance %.4f)", name, got, want, tolerance)
	}
}
