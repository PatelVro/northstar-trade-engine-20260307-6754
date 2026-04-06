package trader

import (
	"northstar/incidents"
	"northstar/orders"
	"northstar/risk"
	"strings"
	"time"
)

type RuntimeConditionState string

const (
	RuntimeConditionHealthy                RuntimeConditionState = "healthy"
	RuntimeConditionDegraded               RuntimeConditionState = "degraded"
	RuntimeConditionBlocked                RuntimeConditionState = "blocked"
	RuntimeConditionHalted                 RuntimeConditionState = "halted"
	RuntimeConditionAwaitingReconciliation RuntimeConditionState = "awaiting_reconciliation"
	RuntimeConditionMarketClosed           RuntimeConditionState = "market_closed"
)

type blockedCycleState struct {
	State                  RuntimeConditionState
	Severity               incidents.Severity
	Reason                 string
	ExpectedNonTradable    bool
	AwaitingReconciliation bool
	UpdatedAt              time.Time
}

type runtimeConditionStateView struct {
	State                  RuntimeConditionState
	Severity               incidents.Severity
	CycleTradable          bool
	ExpectedNonTradable    bool
	AwaitingReconciliation bool
	Reason                 string
	Causes                 []string
	UpdatedAt              time.Time
}

func isMarketClosedReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "market is closed") ||
		strings.Contains(lower, "market closed")
}

func isBrokerMaintenanceReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "nightly reset window") ||
		strings.Contains(lower, "maintenance window") ||
		strings.Contains(lower, "brokerage reset")
}

func isAwaitingReconciliationReason(reason string) bool {
	lower := strings.ToLower(strings.TrimSpace(reason))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "reconciliation") ||
		strings.Contains(lower, "broker truth") ||
		strings.Contains(lower, "unresolved") ||
		strings.Contains(lower, "pending clean reconciliation") ||
		strings.Contains(lower, "pending reconciliation")
}

func classifyBlockedCycleReason(reason string) blockedCycleState {
	reason = strings.TrimSpace(reason)
	state := blockedCycleState{
		State:    RuntimeConditionBlocked,
		Severity: incidents.SeverityWarning,
		Reason:   firstNonEmpty(reason, "trading blocked"),
	}

	switch {
	case isMarketClosedReason(reason):
		state.State = RuntimeConditionMarketClosed
		state.Severity = incidents.SeverityInfo
		state.ExpectedNonTradable = true
	case isBrokerMaintenanceReason(reason):
		state.State = RuntimeConditionBlocked
		state.Severity = incidents.SeverityInfo
		state.ExpectedNonTradable = true
	case isAwaitingReconciliationReason(reason):
		state.State = RuntimeConditionAwaitingReconciliation
		state.Severity = incidents.SeverityWarning
		state.AwaitingReconciliation = true
		if strings.Contains(strings.ToLower(reason), "unresolved") {
			state.Severity = incidents.SeverityCritical
		}
	case strings.Contains(strings.ToLower(reason), "kill switch"),
		strings.Contains(strings.ToLower(reason), " risk supervisor halted"),
		strings.Contains(strings.ToLower(reason), "trading halted"),
		strings.Contains(strings.ToLower(reason), "restart recovery"):
		state.State = RuntimeConditionHalted
		state.Severity = incidents.SeverityCritical
	}

	return state
}

func marketDataIncidentSeverity(summary string) incidents.Severity {
	if isMarketClosedReason(summary) {
		return incidents.SeverityInfo
	}
	return incidents.SeverityWarning
}

func expectedNonTradableIncident(incident incidents.Incident) bool {
	if incident.Severity != incidents.SeverityInfo {
		return false
	}
	switch incident.IncidentType {
	case incidents.TypeMarketDataValidationFailed, incidents.TypeSymbolDataQualityBlocked:
		return isMarketClosedReason(incident.Summary)
	default:
		return false
	}
}

func (at *AutoTrader) noteBlockedCycle(reason string) blockedCycleState {
	state := classifyBlockedCycleReason(reason)
	state.UpdatedAt = time.Now().UTC()
	at.blockedCycleMu.Lock()
	at.lastBlockedCycle = state
	at.blockedCycleMu.Unlock()
	return state
}

func (at *AutoTrader) clearBlockedCycle() {
	if at == nil {
		return
	}
	at.blockedCycleMu.Lock()
	at.lastBlockedCycle = blockedCycleState{}
	at.blockedCycleMu.Unlock()
}

func (at *AutoTrader) currentBlockedCycle() blockedCycleState {
	if at == nil {
		return blockedCycleState{}
	}
	at.blockedCycleMu.RLock()
	defer at.blockedCycleMu.RUnlock()
	return at.lastBlockedCycle
}

func (at *AutoTrader) currentRuntimeConditionState(
	gate tradingGateDecision,
	brokerTruth brokerTruthSummary,
	dataQuality OperatorDataQualitySummary,
	positionRecon *positionReconciliationSummary,
	orderRecon *orders.Summary,
	restart restartRecoverySummary,
	killSwitch killSwitchSummary,
	brokerStatus brokerRuntimeSnapshot,
) runtimeConditionStateView {
	readiness := at.getReadinessSummary()
	recentBlocked := at.currentBlockedCycle()
	view := runtimeConditionStateView{
		State:         RuntimeConditionHealthy,
		Severity:      incidents.SeverityInfo,
		CycleTradable: at.isRunning && gate.TradingAllowed && gate.EntriesAllowed,
		Reason:        "runtime healthy and cycle tradable",
		Causes:        []string{},
	}

	if killSwitch.Active {
		view.State = RuntimeConditionHalted
		view.Severity = incidents.SeverityCritical
		view.CycleTradable = false
		view.Reason = firstNonEmpty(strings.TrimSpace(killSwitch.Message), "kill switch active")
		view.Causes = []string{view.Reason}
		return view
	}

	if readiness.CheckedAt.After(time.Time{}) && !readiness.TradingAllowed {
		reasons := make([]string, 0, len(readiness.Checks))
		for _, check := range readiness.Checks {
			if check.Status == ReadinessPass && check.TradingAllowed {
				continue
			}
			reason := strings.TrimSpace(check.Message)
			if reason == "" {
				continue
			}
			reasons = append(reasons, reason)
		}
		if len(reasons) == 0 {
			reasons = []string{firstNonEmpty(strings.TrimSpace(readiness.Message), "startup readiness is blocking trading")}
		}
		classified := classifyBlockedCycleReason(reasons[0])
		view.State = classified.State
		view.Severity = classified.Severity
		view.CycleTradable = false
		view.ExpectedNonTradable = classified.ExpectedNonTradable
		view.AwaitingReconciliation = classified.AwaitingReconciliation
		view.Reason = reasons[0]
		view.Causes = append([]string(nil), reasons...)
		return view
	}

	if restart.TradingBlocked {
		view.State = RuntimeConditionAwaitingReconciliation
		view.Severity = incidents.SeverityCritical
		view.CycleTradable = false
		view.AwaitingReconciliation = restart.PendingReconciliation || strings.Contains(strings.ToLower(restart.Message), "reconciliation")
		view.Reason = firstNonEmpty(strings.TrimSpace(restart.Message), "restart recovery is blocking trading")
		view.Causes = []string{view.Reason}
		return view
	}

	if recentBlocked.Reason != "" && recentBlocked.ExpectedNonTradable {
		view.State = RuntimeConditionMarketClosed
		view.Severity = incidents.SeverityInfo
		view.CycleTradable = false
		view.ExpectedNonTradable = true
		view.Reason = recentBlocked.Reason
		view.Causes = []string{recentBlocked.Reason}
		view.UpdatedAt = recentBlocked.UpdatedAt
		return view
	}

	if brokerTruth.TradingBlocked {
		view.State = RuntimeConditionBlocked
		view.Severity = incidents.SeverityWarning
		view.CycleTradable = false
		view.Reason = firstNonEmpty(strings.TrimSpace(brokerTruth.Message), "broker truth is not verified for the active mode")
		view.Causes = append([]string(nil), brokerTruth.BlockingReasons...)
		if len(view.Causes) == 0 {
			view.Causes = []string{view.Reason}
		}
		if brokerTruth.UnresolvedOrderCount > 0 ||
			brokerTruth.PrimaryAuthority == orders.TruthAuthorityUnresolved ||
			(positionRecon != nil && positionRecon.Available && !positionRecon.TradingAllowed) {
			view.State = RuntimeConditionAwaitingReconciliation
			view.Severity = incidents.SeverityCritical
			view.AwaitingReconciliation = true
		}
		return view
	}

	if brokerTruth.EntriesRestricted || (orderRecon != nil && orderRecon.CurrentInferredOrders > 0) {
		view.State = RuntimeConditionAwaitingReconciliation
		view.Severity = incidents.SeverityWarning
		view.CycleTradable = false
		view.AwaitingReconciliation = true
		view.Reason = firstNonEmpty(strings.TrimSpace(brokerTruth.RestrictionReason), strings.TrimSpace(brokerTruth.Message), "broker truth confidence is degraded pending clean reconciliation")
		view.Causes = []string{view.Reason}
		if reason := strings.TrimSpace(brokerTruth.PrimaryReason); reason != "" && !containsString(view.Causes, reason) {
			view.Causes = append(view.Causes, reason)
		}
		return view
	}

	if !gate.TradingAllowed {
		view.State = RuntimeConditionBlocked
		view.Severity = incidents.SeverityWarning
		view.CycleTradable = false
		view.Reason = firstNonEmpty(strings.TrimSpace(gate.BlockReason), strings.TrimSpace(gate.Message), "trading blocked")
		view.Causes = append([]string(nil), gate.BlockingReasons...)
		if len(view.Causes) == 0 {
			view.Causes = []string{view.Reason}
		}
		if gate.Mode == risk.SupervisorModeHalted {
			view.State = RuntimeConditionHalted
			view.Severity = incidents.SeverityCritical
		}
		return view
	}

	if !gate.EntriesAllowed || brokerStatus.State != BrokerRuntimeHealthy || dataQuality.FeedDelayed {
		view.State = RuntimeConditionDegraded
		view.Severity = incidents.SeverityWarning
		view.CycleTradable = false
		reasons := []string{}
		if !gate.EntriesAllowed {
			reasons = append(reasons, firstNonEmpty(strings.TrimSpace(gate.BlockReason), strings.TrimSpace(gate.Message), "new entries are restricted"))
		}
		if brokerStatus.State != BrokerRuntimeHealthy && strings.TrimSpace(brokerStatus.Reason) != "" {
			reasons = append(reasons, brokerStatus.Reason)
		}
		if dataQuality.FeedDelayed && strings.TrimSpace(dataQuality.FeedSummary) != "" {
			reasons = append(reasons, dataQuality.FeedSummary)
		}
		view.Reason = firstNonEmpty(joinNonEmpty(reasons, "; "), "runtime degraded")
		view.Causes = append([]string(nil), reasons...)
		return view
	}

	// Per-symbol data-quality blocks are handled at the individual symbol level;
	// they degrade the cycle but do not block trading on symbols that pass validation.
	if dataQuality.BlockedSymbolsCount > 0 {
		view.State = RuntimeConditionDegraded
		view.Severity = incidents.SeverityInfo
		view.Reason = "one or more symbols are blocked by data-quality validation; tradable symbols proceed"
		view.Causes = []string{view.Reason}
	}

	return view
}

func joinNonEmpty(values []string, sep string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return strings.Join(filtered, sep)
}
