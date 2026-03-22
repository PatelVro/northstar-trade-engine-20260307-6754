package trader

import (
	"math"
	"northstar/execution"
	"northstar/logger"
	"northstar/orders"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// DecisionSizingEquity — the equity figure used for position-size caps
// ---------------------------------------------------------------------------

func TestDecisionSizingEquity_PrefersStrategyEquity(t *testing.T) {
	s := AccountSummary{StrategyEquity: 80000, AccountEquity: 100000}
	if got := s.DecisionSizingEquity(); got != 80000 {
		t.Fatalf("expected 80000, got %.2f", got)
	}
}

func TestDecisionSizingEquity_FallsBackToAccountEquity(t *testing.T) {
	s := AccountSummary{StrategyEquity: 0, AccountEquity: 100000}
	if got := s.DecisionSizingEquity(); got != 100000 {
		t.Fatalf("expected 100000, got %.2f", got)
	}
}

func TestDecisionSizingEquity_UsesLowerOfTwoPositive(t *testing.T) {
	s := AccountSummary{StrategyEquity: 120000, AccountEquity: 95000}
	if got := s.DecisionSizingEquity(); got != 95000 {
		t.Fatalf("expected 95000 (lower), got %.2f", got)
	}
}

func TestDecisionSizingEquity_NegativeStrategyEquityReturnsZero(t *testing.T) {
	s := AccountSummary{StrategyEquity: -5000, AccountEquity: 0}
	if got := s.DecisionSizingEquity(); got != 0 {
		t.Fatalf("expected 0 for negative, got %.2f", got)
	}
}

func TestDecisionSizingEquity_BothZero(t *testing.T) {
	s := AccountSummary{}
	if got := s.DecisionSizingEquity(); got != 0 {
		t.Fatalf("expected 0, got %.2f", got)
	}
}

// ---------------------------------------------------------------------------
// capNotional — the pure entry-sizing cap logic
// ---------------------------------------------------------------------------

func TestCapNotional_ZeroRequestReturnsZero(t *testing.T) {
	if got := capNotional(0, 100000, 100000, 0.20, 50000); got != 0 {
		t.Fatalf("expected 0, got %.2f", got)
	}
}

func TestCapNotional_NegativeRequestReturnsZero(t *testing.T) {
	if got := capNotional(-5000, 100000, 100000, 0.20, 50000); got != 0 {
		t.Fatalf("expected 0, got %.2f", got)
	}
}

func TestCapNotional_UnderCapPassesThrough(t *testing.T) {
	// equity=100k, maxPct=20% → cap=20k, requesting 15k → passthrough
	got := capNotional(15000, 100000, 100000, 0.20, 50000)
	assertFloatEqual(t, "uncapped notional", got, 15000)
}

func TestCapNotional_OverCapGetsCapped(t *testing.T) {
	// equity=100k, maxPct=20% → cap=20k, requesting 30k → capped to 20k
	got := capNotional(30000, 100000, 100000, 0.20, 50000)
	assertFloatEqual(t, "capped notional", got, 20000)
}

func TestCapNotional_AvailableBalanceCap(t *testing.T) {
	// equity=100k, maxPct=20% → position cap=20k, but available=10k → avail cap = 9.5k
	got := capNotional(15000, 100000, 100000, 0.20, 10000)
	assertFloatEqual(t, "available-balance capped", got, 9500)
}

func TestCapNotional_AvailableCapTighterThanPositionCap(t *testing.T) {
	// equity=100k, maxPct=50% → position cap=50k, available=8k → avail cap = 7.6k
	got := capNotional(40000, 100000, 100000, 0.50, 8000)
	assertFloatEqual(t, "available-balance tighter", got, 7600)
}

func TestCapNotional_PositionCapTighterThanAvailableCap(t *testing.T) {
	// equity=100k, maxPct=10% → position cap=10k, available=50k → avail cap = 47.5k, but position cap wins
	got := capNotional(20000, 100000, 100000, 0.10, 50000)
	assertFloatEqual(t, "position cap tighter", got, 10000)
}

func TestCapNotional_ZeroMaxPositionPctUsesDefault(t *testing.T) {
	// When maxPositionPct=0, defaults to 0.20
	got := capNotional(30000, 100000, 100000, 0, 0)
	assertFloatEqual(t, "default 20% cap", got, 20000)
}

func TestCapNotional_NegativeMaxPositionPctUsesDefault(t *testing.T) {
	got := capNotional(30000, 100000, 100000, -0.10, 0)
	assertFloatEqual(t, "default 20% cap on negative", got, 20000)
}

func TestCapNotional_ZeroEquityFallsBackToInitialBalance(t *testing.T) {
	// equityCap=0, initialBalance=50k → uses 50k
	got := capNotional(20000, 0, 50000, 0.20, 0)
	assertFloatEqual(t, "fallback to initialBalance", got, 10000)
}

func TestCapNotional_BothEquityAndInitialBalanceZero(t *testing.T) {
	// No equity info at all → returns requested unchanged
	got := capNotional(30000, 0, 0, 0.20, 0)
	assertFloatEqual(t, "no equity → passthrough", got, 30000)
}

func TestCapNotional_ZeroAvailableSkipsAvailCap(t *testing.T) {
	// available=0 should not cap
	got := capNotional(15000, 100000, 100000, 0.20, 0)
	assertFloatEqual(t, "zero available → no avail cap", got, 15000)
}

func TestCapNotional_NegativeAvailableSkipsAvailCap(t *testing.T) {
	// available < 0 should not apply the 95% cap (it would be negative)
	got := capNotional(15000, 100000, 100000, 0.20, -5000)
	assertFloatEqual(t, "negative available → no avail cap", got, 15000)
}

func TestCapNotional_SmallEquitySmallRequest(t *testing.T) {
	// equity=1000, maxPct=20% → cap=200, request=150 → passthrough
	got := capNotional(150, 1000, 1000, 0.20, 500)
	assertFloatEqual(t, "small passthrough", got, 150)
}

func TestCapNotional_SmallEquityLargeRequest(t *testing.T) {
	// equity=1000, maxPct=20% → cap=200, request=500 → capped
	got := capNotional(500, 1000, 1000, 0.20, 500)
	assertFloatEqual(t, "small capped", got, 200)
}

// ---------------------------------------------------------------------------
// executionSideForAction
// ---------------------------------------------------------------------------

func TestExecutionSideForAction(t *testing.T) {
	cases := []struct {
		action string
		want   string
	}{
		{"open_long", "buy"},
		{"close_short", "buy"},
		{"open_short", "sell"},
		{"close_long", "sell"},
		{"OPEN_LONG", "buy"},
		{" Close_Long ", "sell"},
		{"hold", ""},
		{"", ""},
		{"unknown_action", ""},
	}
	for _, tc := range cases {
		got := executionSideForAction(tc.action)
		if got != tc.want {
			t.Errorf("executionSideForAction(%q) = %q, want %q", tc.action, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// executionActionIncreasesExposure
// ---------------------------------------------------------------------------

func TestExecutionActionIncreasesExposure(t *testing.T) {
	cases := []struct {
		action string
		want   bool
	}{
		{"open_long", true},
		{"open_short", true},
		{"close_long", false},
		{"close_short", false},
		{"hold", false},
		{"", false},
	}
	for _, tc := range cases {
		got := executionActionIncreasesExposure(tc.action)
		if got != tc.want {
			t.Errorf("executionActionIncreasesExposure(%q) = %v, want %v", tc.action, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// executionStatusHasImmediateFill
// ---------------------------------------------------------------------------

func TestExecutionStatusHasImmediateFill(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"filled", true},
		{"Filled", true},
		{"partially_filled", true},
		{"PARTIALLY_FILLED", true},
		{" filled ", true},
		{"submitted", false},
		{"cancelled", false},
		{"rejected", false},
		{"", false},
	}
	for _, tc := range cases {
		got := executionStatusHasImmediateFill(tc.status)
		if got != tc.want {
			t.Errorf("executionStatusHasImmediateFill(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isTrackedExecutionStatus
// ---------------------------------------------------------------------------

func TestIsTrackedExecutionStatus(t *testing.T) {
	tracked := []string{
		"blocked", "duplicate_suppressed", "submitted", "acknowledged",
		"partially_filled", "filled", "cancelled", "rejected", "stale", "failed",
	}
	for _, s := range tracked {
		if !isTrackedExecutionStatus(s) {
			t.Errorf("expected %q to be tracked", s)
		}
	}
	// Case insensitive
	if !isTrackedExecutionStatus("FILLED") {
		t.Error("expected FILLED to be tracked (case-insensitive)")
	}
	if !isTrackedExecutionStatus(" submitted ") {
		t.Error("expected trimmed 'submitted' to be tracked")
	}

	untracked := []string{"pending", "hold", "", "unknown", "processing"}
	for _, s := range untracked {
		if isTrackedExecutionStatus(s) {
			t.Errorf("expected %q to NOT be tracked", s)
		}
	}
}

// ---------------------------------------------------------------------------
// executionResultMutatesBrokerSnapshot
// ---------------------------------------------------------------------------

func TestExecutionResultMutatesBrokerSnapshot(t *testing.T) {
	mutating := []execution.Status{
		execution.StatusSubmitted, execution.StatusAcknowledged,
		execution.StatusPartiallyFilled, execution.StatusFilled,
		execution.StatusCancelled,
	}
	for _, s := range mutating {
		r := execution.Result{Status: s}
		if !executionResultMutatesBrokerSnapshot(r) {
			t.Errorf("expected %q to mutate broker snapshot", s)
		}
	}

	nonMutating := []execution.Status{
		execution.StatusPending, execution.StatusBlocked,
		execution.StatusRejected, execution.StatusFailed,
	}
	for _, s := range nonMutating {
		r := execution.Result{Status: s}
		if executionResultMutatesBrokerSnapshot(r) {
			t.Errorf("expected %q to NOT mutate broker snapshot", s)
		}
	}
}

// ---------------------------------------------------------------------------
// executionResultError
// ---------------------------------------------------------------------------

func TestExecutionResultError_SuccessStatuses(t *testing.T) {
	for _, s := range []execution.Status{
		execution.StatusSubmitted, execution.StatusAcknowledged,
		execution.StatusPartiallyFilled, execution.StatusFilled,
	} {
		r := execution.Result{Status: s}
		if err := executionResultError(r); err != nil {
			t.Errorf("expected nil error for %q, got %v", s, err)
		}
	}
}

func TestExecutionResultError_PendingReturnsError(t *testing.T) {
	r := execution.Result{Status: execution.StatusPending, Symbol: "AAPL", ActionType: "open_long"}
	err := executionResultError(r)
	if err == nil {
		t.Fatal("expected error for pending")
	}
}

func TestExecutionResultError_RejectedIncludesReason(t *testing.T) {
	r := execution.Result{Status: execution.StatusRejected, Symbol: "TSLA", ActionType: "open_short", Error: "insufficient margin"}
	err := executionResultError(r)
	if err == nil {
		t.Fatal("expected error for rejected")
	}
	if got := err.Error(); !strings.Contains(got, "insufficient margin") {
		t.Errorf("expected error to contain reason, got %q", got)
	}
}

func TestExecutionResultError_FallbackToMessage(t *testing.T) {
	r := execution.Result{Status: execution.StatusFailed, Symbol: "X", ActionType: "close_long", Message: "timeout"}
	err := executionResultError(r)
	if err == nil {
		t.Fatal("expected error for failed")
	}
	if got := err.Error(); !strings.Contains(got, "timeout") {
		t.Errorf("expected error to contain message, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// actionHasImmediatePositionEffect
// ---------------------------------------------------------------------------

func TestActionHasImmediatePositionEffect(t *testing.T) {
	cases := []struct {
		name    string
		success bool
		status  string
		want    bool
	}{
		{"filled and success", true, "filled", true},
		{"partially_filled and success", true, "partially_filled", true},
		{"filled but not success", false, "filled", false},
		{"submitted and success", true, "submitted", false},
		{"empty status", true, "", false},
	}
	for _, tc := range cases {
		action := loggerDecisionAction(tc.success, tc.status)
		got := actionHasImmediatePositionEffect(action)
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// firstOrderIssueWithAuthority
// ---------------------------------------------------------------------------

func TestFirstOrderIssueWithAuthority_Found(t *testing.T) {
	issues := []orders.Issue{
		{Type: orders.IssueUnknownBrokerOrder, Authority: orders.TruthAuthorityBrokerConfirmed, LocalID: "a"},
		{Type: orders.IssueFillMismatch, Authority: orders.TruthAuthorityReconciliationInferred, LocalID: "b"},
		{Type: orders.IssueLocalMissingAtBroker, Authority: orders.TruthAuthorityReconciliationInferred, LocalID: "c"},
	}
	got := firstOrderIssueWithAuthority(issues, orders.TruthAuthorityReconciliationInferred)
	if got == nil {
		t.Fatal("expected non-nil issue")
	}
	if got.LocalID != "b" {
		t.Errorf("expected first match (b), got %q", got.LocalID)
	}
}

func TestFirstOrderIssueWithAuthority_NotFound(t *testing.T) {
	issues := []orders.Issue{
		{Type: orders.IssueUnknownBrokerOrder, Authority: orders.TruthAuthorityBrokerConfirmed},
	}
	got := firstOrderIssueWithAuthority(issues, orders.TruthAuthorityUnresolved)
	if got != nil {
		t.Fatal("expected nil for unmatched authority")
	}
}

func TestFirstOrderIssueWithAuthority_EmptySlice(t *testing.T) {
	got := firstOrderIssueWithAuthority(nil, orders.TruthAuthorityBrokerConfirmed)
	if got != nil {
		t.Fatal("expected nil for empty slice")
	}
}

func TestFirstOrderIssueWithAuthority_ReturnedIssueIsCopy(t *testing.T) {
	issues := []orders.Issue{
		{Type: orders.IssueFillMismatch, Authority: orders.TruthAuthorityReconciliationInferred, LocalID: "original"},
	}
	got := firstOrderIssueWithAuthority(issues, orders.TruthAuthorityReconciliationInferred)
	got.LocalID = "mutated"
	if issues[0].LocalID != "original" {
		t.Fatal("modifying returned issue should not mutate original slice")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertFloatEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", name, got, want)
	}
}

func loggerDecisionAction(success bool, orderStatus string) logger.DecisionAction {
	return logger.DecisionAction{
		Success:     success,
		OrderStatus: orderStatus,
	}
}
