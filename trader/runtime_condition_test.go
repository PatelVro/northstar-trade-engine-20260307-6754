package trader

import (
	"northstar/incidents"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// isMarketClosedReason
// ---------------------------------------------------------------------------

func TestIsMarketClosedReason_Matches(t *testing.T) {
	cases := []string{
		"market is closed",
		"Market Is Closed",
		"MARKET IS CLOSED",
		"market closed",
		"the market closed for today",
		"  market is closed  ",
	}
	for _, reason := range cases {
		if !isMarketClosedReason(reason) {
			t.Errorf("expected %q to match market closed", reason)
		}
	}
}

func TestIsMarketClosedReason_NoMatch(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"broker disconnected",
		"kill switch active",
		"reconciliation pending",
	}
	for _, reason := range cases {
		if isMarketClosedReason(reason) {
			t.Errorf("expected %q to NOT match market closed", reason)
		}
	}
}

// ---------------------------------------------------------------------------
// isAwaitingReconciliationReason
// ---------------------------------------------------------------------------

func TestIsAwaitingReconciliationReason_Matches(t *testing.T) {
	cases := []string{
		"reconciliation pending",
		"pending clean reconciliation",
		"pending reconciliation cycle",
		"broker truth not verified",
		"unresolved orders detected",
		"  RECONCILIATION  ",
	}
	for _, reason := range cases {
		if !isAwaitingReconciliationReason(reason) {
			t.Errorf("expected %q to match reconciliation", reason)
		}
	}
}

func TestIsAwaitingReconciliationReason_NoMatch(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"market is closed",
		"kill switch active",
		"broker disconnected",
	}
	for _, reason := range cases {
		if isAwaitingReconciliationReason(reason) {
			t.Errorf("expected %q to NOT match reconciliation", reason)
		}
	}
}

// ---------------------------------------------------------------------------
// classifyBlockedCycleReason
// ---------------------------------------------------------------------------

func TestClassifyBlockedCycleReason_MarketClosed(t *testing.T) {
	state := classifyBlockedCycleReason("market is closed for the session")
	if state.State != RuntimeConditionMarketClosed {
		t.Errorf("expected market_closed, got %s", state.State)
	}
	if state.Severity != incidents.SeverityInfo {
		t.Errorf("expected info severity, got %s", state.Severity)
	}
	if !state.ExpectedNonTradable {
		t.Error("expected ExpectedNonTradable=true")
	}
}

func TestClassifyBlockedCycleReason_Reconciliation(t *testing.T) {
	state := classifyBlockedCycleReason("pending clean reconciliation cycle")
	if state.State != RuntimeConditionAwaitingReconciliation {
		t.Errorf("expected awaiting_reconciliation, got %s", state.State)
	}
	if state.Severity != incidents.SeverityWarning {
		t.Errorf("expected warning severity, got %s", state.Severity)
	}
	if !state.AwaitingReconciliation {
		t.Error("expected AwaitingReconciliation=true")
	}
}

func TestClassifyBlockedCycleReason_UnresolvedEscalatesToCritical(t *testing.T) {
	state := classifyBlockedCycleReason("unresolved orders pending reconciliation")
	if state.State != RuntimeConditionAwaitingReconciliation {
		t.Errorf("expected awaiting_reconciliation, got %s", state.State)
	}
	if state.Severity != incidents.SeverityCritical {
		t.Errorf("expected critical severity for unresolved, got %s", state.Severity)
	}
}

func TestClassifyBlockedCycleReason_KillSwitch(t *testing.T) {
	state := classifyBlockedCycleReason("kill switch activated by operator")
	if state.State != RuntimeConditionHalted {
		t.Errorf("expected halted, got %s", state.State)
	}
	if state.Severity != incidents.SeverityCritical {
		t.Errorf("expected critical severity, got %s", state.Severity)
	}
}

func TestClassifyBlockedCycleReason_TradingHalted(t *testing.T) {
	for _, reason := range []string{"trading halted by supervisor", "the risk supervisor halted trading", "restart recovery in progress"} {
		state := classifyBlockedCycleReason(reason)
		if state.State != RuntimeConditionHalted {
			t.Errorf("reason %q: expected halted, got %s", reason, state.State)
		}
	}
}

func TestClassifyBlockedCycleReason_GenericBlocked(t *testing.T) {
	state := classifyBlockedCycleReason("insufficient margin for new trades")
	if state.State != RuntimeConditionBlocked {
		t.Errorf("expected blocked, got %s", state.State)
	}
	if state.Severity != incidents.SeverityWarning {
		t.Errorf("expected warning severity, got %s", state.Severity)
	}
}

func TestClassifyBlockedCycleReason_EmptyReason(t *testing.T) {
	state := classifyBlockedCycleReason("")
	if state.State != RuntimeConditionBlocked {
		t.Errorf("expected blocked for empty, got %s", state.State)
	}
	if state.Reason != "trading blocked" {
		t.Errorf("expected default reason, got %q", state.Reason)
	}
}

func TestClassifyBlockedCycleReason_WhitespaceReason(t *testing.T) {
	state := classifyBlockedCycleReason("   ")
	if state.Reason != "trading blocked" {
		t.Errorf("expected default reason for whitespace, got %q", state.Reason)
	}
}

// ---------------------------------------------------------------------------
// marketDataIncidentSeverity
// ---------------------------------------------------------------------------

func TestMarketDataIncidentSeverity_MarketClosed(t *testing.T) {
	if got := marketDataIncidentSeverity("market is closed"); got != incidents.SeverityInfo {
		t.Errorf("expected info for market closed, got %s", got)
	}
}

func TestMarketDataIncidentSeverity_OtherReason(t *testing.T) {
	if got := marketDataIncidentSeverity("data feed timeout"); got != incidents.SeverityWarning {
		t.Errorf("expected warning for non-market-closed, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// expectedNonTradableIncident
// ---------------------------------------------------------------------------

func TestExpectedNonTradableIncident_MarketDataMarketClosed(t *testing.T) {
	incident := incidents.Incident{
		IncidentType: incidents.TypeMarketDataValidationFailed,
		Severity:     incidents.SeverityInfo,
		Summary:      "market is closed",
	}
	if !expectedNonTradableIncident(incident) {
		t.Error("expected true for market-closed market data incident")
	}
}

func TestExpectedNonTradableIncident_SymbolQualityMarketClosed(t *testing.T) {
	incident := incidents.Incident{
		IncidentType: incidents.TypeSymbolDataQualityBlocked,
		Severity:     incidents.SeverityInfo,
		Summary:      "market is closed",
	}
	if !expectedNonTradableIncident(incident) {
		t.Error("expected true for market-closed symbol quality incident")
	}
}

func TestExpectedNonTradableIncident_WarningSeverityReturnsFalse(t *testing.T) {
	incident := incidents.Incident{
		IncidentType: incidents.TypeMarketDataValidationFailed,
		Severity:     incidents.SeverityWarning,
		Summary:      "market is closed",
	}
	if expectedNonTradableIncident(incident) {
		t.Error("expected false for warning severity even with market-closed summary")
	}
}

func TestExpectedNonTradableIncident_OtherTypeReturnsFalse(t *testing.T) {
	incident := incidents.Incident{
		IncidentType: incidents.TypeKillSwitchActivated,
		Severity:     incidents.SeverityInfo,
		Summary:      "market is closed",
	}
	if expectedNonTradableIncident(incident) {
		t.Error("expected false for non-data incident type")
	}
}

func TestExpectedNonTradableIncident_NonMarketClosedReturnsFalse(t *testing.T) {
	incident := incidents.Incident{
		IncidentType: incidents.TypeMarketDataValidationFailed,
		Severity:     incidents.SeverityInfo,
		Summary:      "data feed stale for AAPL",
	}
	if expectedNonTradableIncident(incident) {
		t.Error("expected false for non-market-closed summary")
	}
}

// ---------------------------------------------------------------------------
// joinNonEmpty
// ---------------------------------------------------------------------------

func TestJoinNonEmpty_FiltersEmpty(t *testing.T) {
	got := joinNonEmpty([]string{"a", "", "  ", "b"}, "; ")
	if got != "a; b" {
		t.Errorf("expected 'a; b', got %q", got)
	}
}

func TestJoinNonEmpty_AllEmpty(t *testing.T) {
	got := joinNonEmpty([]string{"", "  ", ""}, "; ")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestJoinNonEmpty_Nil(t *testing.T) {
	got := joinNonEmpty(nil, "; ")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestJoinNonEmpty_Single(t *testing.T) {
	got := joinNonEmpty([]string{"  hello  "}, "|")
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// RuntimeConditionState constants
// ---------------------------------------------------------------------------

func TestRuntimeConditionStateConstants(t *testing.T) {
	states := []RuntimeConditionState{
		RuntimeConditionHealthy,
		RuntimeConditionDegraded,
		RuntimeConditionBlocked,
		RuntimeConditionHalted,
		RuntimeConditionAwaitingReconciliation,
		RuntimeConditionMarketClosed,
	}
	seen := map[RuntimeConditionState]bool{}
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate state: %s", s)
		}
		seen[s] = true
		if strings.TrimSpace(string(s)) == "" {
			t.Errorf("empty state constant")
		}
	}
}
