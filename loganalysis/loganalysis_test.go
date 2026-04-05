package loganalysis

import (
	"strings"
	"testing"
	"time"

	"northstar/audit"
	"northstar/logger"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func record(cycleNum int, ts time.Time, success bool, tradable bool) *logger.DecisionRecord {
	return &logger.DecisionRecord{
		CycleNumber:   cycleNum,
		Timestamp:     ts,
		Success:       success,
		CycleTradable: tradable,
	}
}

func recordWithError(cycleNum int, ts time.Time, errMsg string) *logger.DecisionRecord {
	r := record(cycleNum, ts, false, false)
	r.ErrorMessage = errMsg
	return r
}

func recordWithDecision(cycleNum int, ts time.Time, action, symbol, status string, success bool) *logger.DecisionRecord {
	r := &logger.DecisionRecord{
		CycleNumber:   cycleNum,
		Timestamp:     ts,
		Success:       success,
		CycleTradable: true,
		Decisions: []logger.DecisionAction{
			{
				Action:      action,
				Symbol:      symbol,
				OrderStatus: status,
				Success:     success,
			},
		},
	}
	return r
}

// ---------------------------------------------------------------------------
// TestAnalyzeEmptyInputReturnsHealthy
// ---------------------------------------------------------------------------

func TestAnalyzeEmptyInputReturnsHealthy(t *testing.T) {
	report := Analyze(nil, nil)
	if report.Verdict != VerdictHealthy {
		t.Fatalf("expected Healthy verdict on empty input, got %s", report.Verdict)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings on empty input, got %d", len(report.Findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectCycleFailureRate
// ---------------------------------------------------------------------------

func TestDetectCycleFailureRateWarning(t *testing.T) {
	now := time.Now().UTC()
	// 4 failed out of 10 = 40% → warning (success=true for i>=4, false for i<4)
	records := make([]*logger.DecisionRecord, 10)
	for i := range records {
		records[i] = record(i+1, now.Add(time.Duration(i)*time.Minute), i >= 4, true)
	}
	findings := detectCycleFailureRate(records)
	if len(findings) == 0 {
		t.Fatal("expected a finding for high failure rate")
	}
	if findings[0].Severity != SeverityWarning {
		t.Fatalf("expected warning severity, got %s", findings[0].Severity)
	}
}

func TestDetectCycleFailureRateCritical(t *testing.T) {
	now := time.Now().UTC()
	// 6 failed out of 10 = 60% → critical (success=true for i>=6, false for i<6)
	records := make([]*logger.DecisionRecord, 10)
	for i := range records {
		records[i] = record(i+1, now.Add(time.Duration(i)*time.Minute), i >= 6, true)
	}
	findings := detectCycleFailureRate(records)
	if len(findings) == 0 {
		t.Fatal("expected a critical finding for very high failure rate")
	}
	if findings[0].Severity != SeverityCritical {
		t.Fatalf("expected critical severity, got %s", findings[0].Severity)
	}
}

func TestDetectCycleFailureRateBelowThreshold(t *testing.T) {
	now := time.Now().UTC()
	// 2 failed out of 10 = 20% → no finding
	records := make([]*logger.DecisionRecord, 10)
	for i := range records {
		records[i] = record(i+1, now.Add(time.Duration(i)*time.Minute), i >= 2, true)
	}
	findings := detectCycleFailureRate(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding below threshold, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectStalledCycles
// ---------------------------------------------------------------------------

func TestDetectStalledCyclesFindsGap(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		record(1, now, true, true),
		record(2, now.Add(time.Minute), true, true),
		record(3, now.Add(time.Minute+2*time.Hour), true, true), // 2-hour gap
	}
	findings := detectStalledCycles(records)
	if len(findings) == 0 {
		t.Fatal("expected a stall finding for a 2-hour gap")
	}
	ev := findings[0].Evidence
	if ev["before_cycle"] != "2" || ev["after_cycle"] != "3" {
		t.Fatalf("unexpected cycle numbers in evidence: %v", ev)
	}
}

func TestDetectStalledCyclesNoGap(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		record(1, now, true, true),
		record(2, now.Add(5*time.Minute), true, true),
		record(3, now.Add(10*time.Minute), true, true),
	}
	findings := detectStalledCycles(records)
	if len(findings) != 0 {
		t.Fatalf("expected no stall findings, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectRepeatedErrors
// ---------------------------------------------------------------------------

func TestDetectRepeatedErrorsFindsPattern(t *testing.T) {
	now := time.Now().UTC()
	msg := "upstream API timeout"
	records := []*logger.DecisionRecord{
		recordWithError(1, now, msg),
		recordWithError(2, now.Add(time.Minute), msg),
		recordWithError(3, now.Add(2*time.Minute), msg),
	}
	findings := detectRepeatedErrors(records)
	if len(findings) == 0 {
		t.Fatal("expected a repeated-error finding")
	}
	if findings[0].Evidence["count"] != "3" {
		t.Fatalf("expected count 3, got %s", findings[0].Evidence["count"])
	}
}

func TestDetectRepeatedErrorsBelowThreshold(t *testing.T) {
	now := time.Now().UTC()
	msg := "transient error"
	records := []*logger.DecisionRecord{
		recordWithError(1, now, msg),
		recordWithError(2, now.Add(time.Minute), msg),
	}
	findings := detectRepeatedErrors(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding below threshold, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectSilentNonExecution
// ---------------------------------------------------------------------------

func TestDetectSilentNonExecutionFlagsHoldLoop(t *testing.T) {
	now := time.Now().UTC()
	var records []*logger.DecisionRecord
	for i := 0; i < 5; i++ {
		r := record(i+1, now.Add(time.Duration(i)*time.Minute), true, true)
		// tradable cycle, success, zero decisions → suspicious hold loop
		records = append(records, r)
	}
	findings := detectSilentNonExecution(records)
	if len(findings) == 0 {
		t.Fatal("expected a silent-non-execution finding")
	}
	if findings[0].Severity != SeverityWarning {
		t.Fatalf("expected warning, got %s", findings[0].Severity)
	}
}

func TestDetectSilentNonExecutionSkipsExpectedNonTradable(t *testing.T) {
	now := time.Now().UTC()
	var records []*logger.DecisionRecord
	for i := 0; i < 5; i++ {
		r := record(i+1, now.Add(time.Duration(i)*time.Minute), true, true)
		r.ExpectedNonTradable = true
		records = append(records, r)
	}
	findings := detectSilentNonExecution(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding when non-tradable is expected, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectSuspiciousSuccessMessages
// ---------------------------------------------------------------------------

func TestDetectSuspiciousSuccessFindsRejectedOrders(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		recordWithDecision(1, now, "open_long", "BTCUSDT", "rejected", true),
		recordWithDecision(2, now.Add(time.Minute), "open_short", "ETHUSDT", "cancelled", true),
	}
	findings := detectSuspiciousSuccessMessages(records)
	if len(findings) == 0 {
		t.Fatal("expected a suspicious-success finding")
	}
	if findings[0].Severity != SeverityCritical {
		t.Fatalf("expected critical severity, got %s", findings[0].Severity)
	}
	if findings[0].Evidence["count"] != "2" {
		t.Fatalf("expected count 2, got %s", findings[0].Evidence["count"])
	}
}

func TestDetectSuspiciousSuccessNoFalsePositiveOnFilledStatus(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		recordWithDecision(1, now, "open_long", "BTCUSDT", "filled", true),
	}
	findings := detectSuspiciousSuccessMessages(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding for filled status, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectOrderRejectionPatterns
// ---------------------------------------------------------------------------

func TestDetectOrderRejectionPatternsFindsSymbol(t *testing.T) {
	now := time.Now().UTC()
	var records []*logger.DecisionRecord
	for i := 0; i < 4; i++ {
		r := &logger.DecisionRecord{
			CycleNumber:   i + 1,
			Timestamp:     now.Add(time.Duration(i) * time.Minute),
			CycleTradable: true,
			Decisions: []logger.DecisionAction{
				{Symbol: "XRPUSDT", OrderStatus: "rejected", Success: false},
			},
		}
		records = append(records, r)
	}
	findings := detectOrderRejectionPatterns(records)
	if len(findings) == 0 {
		t.Fatal("expected a rejection-pattern finding")
	}
	if findings[0].Evidence["symbol"] != "XRPUSDT" {
		t.Fatalf("expected symbol XRPUSDT, got %s", findings[0].Evidence["symbol"])
	}
}

// ---------------------------------------------------------------------------
// TestDetectPartialFillAnomalies
// ---------------------------------------------------------------------------

func TestDetectPartialFillAnomaliesFindsLowFills(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		{
			CycleNumber:   1,
			Timestamp:     now,
			CycleTradable: true,
			Decisions: []logger.DecisionAction{
				{
					Symbol:               "SOLUSDT",
					RiskApprovedQuantity: 100,
					Quantity:             10, // only 10% filled
				},
			},
		},
	}
	findings := detectPartialFillAnomalies(records)
	if len(findings) == 0 {
		t.Fatal("expected a partial-fill finding")
	}
}

func TestDetectPartialFillAnomaliesNoFalsePositive(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		{
			CycleNumber:   1,
			Timestamp:     now,
			CycleTradable: true,
			Decisions: []logger.DecisionAction{
				{
					Symbol:               "SOLUSDT",
					RiskApprovedQuantity: 100,
					Quantity:             90, // 90% filled → ok
				},
			},
		},
	}
	findings := detectPartialFillAnomalies(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding for 90%% fill, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectPositionInconsistencies
// ---------------------------------------------------------------------------

func TestDetectPositionInconsistenciesFindsUnknownPosition(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		{
			CycleNumber:   1,
			Timestamp:     now,
			CycleTradable: true,
			Success:       true,
			// No open actions recorded, but there is a position in the snapshot.
			Positions: []logger.PositionSnapshot{
				{Symbol: "BTCUSDT", Side: "long", PositionAmt: 0.01},
			},
		},
	}
	findings := detectPositionInconsistencies(records)
	if len(findings) == 0 {
		t.Fatal("expected a position-inconsistency finding")
	}
	if !strings.Contains(findings[0].Evidence["symbols"], "BTCUSDT") {
		t.Fatalf("expected BTCUSDT in symbols evidence, got %s", findings[0].Evidence["symbols"])
	}
}

func TestDetectPositionInconsistenciesNoFindingWhenOpened(t *testing.T) {
	now := time.Now().UTC()
	records := []*logger.DecisionRecord{
		{
			CycleNumber:   1,
			Timestamp:     now,
			CycleTradable: true,
			Success:       true,
			Decisions: []logger.DecisionAction{
				{Action: "open_long", Symbol: "BTCUSDT", Success: true},
			},
			Positions: []logger.PositionSnapshot{
				{Symbol: "BTCUSDT", Side: "long", PositionAmt: 0.01},
			},
		},
	}
	findings := detectPositionInconsistencies(records)
	if len(findings) != 0 {
		t.Fatalf("expected no finding when open action exists, got %d", len(findings))
	}
}

// ---------------------------------------------------------------------------
// TestDetectJournalAnomalies
// ---------------------------------------------------------------------------

func TestDetectJournalAnomaliesReportsCritical(t *testing.T) {
	events := []audit.JournalEvent{
		{Type: "order_reconciliation", Severity: audit.JournalSeverityCritical},
		{Type: "order_reconciliation", Severity: audit.JournalSeverityWarning},
	}
	findings := detectJournalAnomalies(events)
	hasCritical := false
	hasRecon := false
	for _, f := range findings {
		if f.Severity == SeverityCritical {
			hasCritical = true
		}
		if f.Category == CategoryReconciliation {
			hasRecon = true
		}
	}
	if !hasCritical {
		t.Fatal("expected a critical finding from journal events")
	}
	if !hasRecon {
		t.Fatal("expected a reconciliation-category finding")
	}
}

// ---------------------------------------------------------------------------
// TestOverallReport integration
// ---------------------------------------------------------------------------

func TestAnalyzeReturnsUnhealthyOnCriticalFinding(t *testing.T) {
	now := time.Now().UTC()
	// An order marked success but with rejected status → critical finding.
	records := []*logger.DecisionRecord{
		recordWithDecision(1, now, "open_long", "BTCUSDT", "rejected", true),
	}
	report := Analyze(records, nil)
	if report.Verdict != VerdictUnhealthy {
		t.Fatalf("expected Unhealthy verdict, got %s", report.Verdict)
	}
	if len(report.SuspiciousPatterns) == 0 {
		t.Fatal("expected at least one suspicious pattern")
	}
	if len(report.LikelyCauses) == 0 {
		t.Fatal("expected at least one likely cause")
	}
}

func TestAnalyzeReturnsDegradedOnWarningOnly(t *testing.T) {
	now := time.Now().UTC()
	// 4 failed out of 10 = 40% → warning only (success=true for i>=4)
	records := make([]*logger.DecisionRecord, 10)
	for i := range records {
		records[i] = record(i+1, now.Add(time.Duration(i)*time.Minute), i >= 4, true)
	}
	report := Analyze(records, nil)
	if report.Verdict != VerdictDegraded {
		t.Fatalf("expected Degraded verdict, got %s", report.Verdict)
	}
}

func TestAnalyzeReturnsHealthyOnCleanLogs(t *testing.T) {
	now := time.Now().UTC()
	var records []*logger.DecisionRecord
	for i := 0; i < 5; i++ {
		r := record(i+1, now.Add(time.Duration(i)*time.Minute), true, false)
		r.ExpectedNonTradable = true
		records = append(records, r)
	}
	report := Analyze(records, nil)
	if report.Verdict != VerdictHealthy {
		t.Fatalf("expected Healthy verdict, got %s (findings: %+v)", report.Verdict, report.Findings)
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------
