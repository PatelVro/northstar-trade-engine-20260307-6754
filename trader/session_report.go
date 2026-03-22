package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"northstar/execution"
	"northstar/incidents"
	"northstar/logger"
	"northstar/orders"
	"northstar/risk"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SessionCompletionStatus string

const (
	SessionCompletionCompleted SessionCompletionStatus = "completed"
	SessionCompletionDegraded  SessionCompletionStatus = "degraded"
	SessionCompletionBlocked   SessionCompletionStatus = "blocked"
	SessionCompletionPartial   SessionCompletionStatus = "partial"
)

const sessionReportVersion = 11

type SessionPortfolioRiskSnapshot struct {
	EvaluatedAt time.Time             `json:"evaluated_at"`
	Outcome     risk.Outcome          `json:"outcome"`
	Summary     string                `json:"summary"`
	Metrics     risk.PortfolioMetrics `json:"metrics"`
}

type SessionPortfolioRiskPeaks struct {
	MaxGrossExposurePct    float64 `json:"max_gross_exposure_pct"`
	MaxNetExposurePct      float64 `json:"max_net_exposure_pct"`
	MaxSectorExposurePct   float64 `json:"max_sector_exposure_pct"`
	MaxCorrelatedPositions int     `json:"max_correlated_positions"`
	MaxObservedCorrelation float64 `json:"max_observed_correlation"`
	MaxDrawdownPct         float64 `json:"max_drawdown_pct"`
}

type PaperSessionReport struct {
	ReportVersion                     int                           `json:"report_version"`
	TraderID                          string                        `json:"trader_id"`
	TraderName                        string                        `json:"trader_name"`
	Mode                              string                        `json:"mode"`
	Broker                            string                        `json:"broker"`
	StrategyMode                      string                        `json:"strategy_mode"`
	GeneratedAt                       time.Time                     `json:"generated_at"`
	SessionDate                       string                        `json:"session_date"`
	SessionStart                      time.Time                     `json:"session_start"`
	SessionEnd                        time.Time                     `json:"session_end"`
	SessionDurationSeconds            int64                         `json:"session_duration_seconds"`
	StartupReadinessStatus            ReadinessStatus               `json:"startup_readiness_status"`
	StartupReadinessMessage           string                        `json:"startup_readiness_message"`
	TradingAllowedAtStart             bool                          `json:"trading_allowed_at_start"`
	BrokerStateFinal                  string                        `json:"broker_state_final"`
	BrokerDegradedEventsCount         int                           `json:"broker_degraded_events_count"`
	BlockedCyclesCount                int                           `json:"blocked_cycles_count"`
	FinalRiskMode                     risk.SupervisorMode           `json:"final_risk_mode"`
	RiskIncidentCount                 int                           `json:"risk_incident_count"`
	CriticalRiskIncidentCount         int                           `json:"critical_risk_incident_count"`
	SupervisorRestrictedDuringSession bool                          `json:"supervisor_restricted_during_session"`
	RiskSupervisorSummary             string                        `json:"risk_supervisor_summary"`
	NotableRiskIncidents              []string                      `json:"notable_risk_incidents"`
	SessionCompletionStatus           SessionCompletionStatus       `json:"session_completion_status"`
	StrategyInitialCapital            float64                       `json:"strategy_initial_capital"`
	StartingStrategyEquity            *float64                      `json:"starting_strategy_equity"`
	EndingStrategyEquity              *float64                      `json:"ending_strategy_equity"`
	AccountCashEnd                    *float64                      `json:"account_cash_end"`
	AccountEquityEnd                  *float64                      `json:"account_equity_end"`
	RealizedPnL                       *float64                      `json:"realized_pnl"`
	UnrealizedPnLEnd                  *float64                      `json:"unrealized_pnl_end"`
	TotalPnL                          *float64                      `json:"total_pnl"`
	StrategyReturnPct                 *float64                      `json:"strategy_return_pct"`
	EndingCumulativeStrategyReturnPct *float64                      `json:"ending_cumulative_strategy_return_pct"`
	DecisionCycles                    int                           `json:"decision_cycles"`
	ActionableDecisions               int                           `json:"actionable_decisions"`
	OrderSubmitAttempts               int                           `json:"order_submit_attempts"`
	ExecutionIntentsTotal             int                           `json:"execution_intents_total"`
	ExecutionBlockedCount             int                           `json:"execution_blocked_count"`
	DuplicateSuppressedCount          int                           `json:"duplicate_suppressed_count"`
	StaleExecutionCount               int                           `json:"stale_execution_count"`
	ExecutionSubmittedCount           int                           `json:"execution_submitted_count"`
	ExecutionAcknowledgedCount        int                           `json:"execution_acknowledged_count"`
	ExecutionFilledCount              int                           `json:"execution_filled_count"`
	ExecutionRejectedCount            int                           `json:"execution_rejected_count"`
	ExecutionFailedCount              int                           `json:"execution_failed_count"`
	ShadowModeActive                  bool                          `json:"shadow_mode_active"`
	ShadowDecisionsTotal              int                           `json:"shadow_decisions_total"`
	ShadowWouldTradeCount             int                           `json:"shadow_would_trade_count"`
	ShadowBlockedCount                int                           `json:"shadow_blocked_count"`
	ShadowOpenPositionsEnd            int                           `json:"shadow_open_positions_end"`
	ShadowClosedTrades                int                           `json:"shadow_closed_trades"`
	ShadowRealizedPnL                 float64                       `json:"shadow_realized_pnl"`
	ShadowUnrealizedPnL               float64                       `json:"shadow_unrealized_pnl"`
	ShadowLastDecisionAt              string                        `json:"shadow_last_decision_at"`
	RestartRecoveryRestored           bool                          `json:"restart_recovery_restored"`
	RestartRecoveryPending            bool                          `json:"restart_recovery_pending_reconciliation"`
	RestartRecoveryBlocked            bool                          `json:"restart_recovery_blocked"`
	RestartRecoveryMessage            string                        `json:"restart_recovery_message"`
	OrdersSubmitted                   int                           `json:"orders_submitted"`
	OrdersFilled                      int                           `json:"orders_filled"`
	BuyFills                          int                           `json:"buy_fills"`
	SellFills                         int                           `json:"sell_fills"`
	PositionsOpenedCount              int                           `json:"positions_opened_count"`
	PositionsClosedCount              int                           `json:"positions_closed_count"`
	SymbolsTraded                     []string                      `json:"symbols_traded"`
	MaxConcurrentPositionsObserved    int                           `json:"max_concurrent_positions_observed"`
	MaxGrossExposureObserved          *float64                      `json:"max_gross_exposure_observed"`
	PortfolioRiskLatest               *SessionPortfolioRiskSnapshot `json:"portfolio_risk_latest,omitempty"`
	PortfolioRiskPeaks                *SessionPortfolioRiskPeaks    `json:"portfolio_risk_peaks,omitempty"`
	RiskEvaluations                   int                           `json:"risk_evaluations"`
	RiskRejectedOrders                int                           `json:"risk_rejected_orders"`
	RiskReducedOrders                 int                           `json:"risk_reduced_orders"`
	DistinctRiskMessages              []string                      `json:"distinct_risk_messages"`
	OrderReconciliationRuns           int                           `json:"order_reconciliation_runs"`
	OrderReconciliationMismatches     int                           `json:"order_reconciliation_mismatches"`
	OrderReconciliationRepairs        int                           `json:"order_reconciliation_repairs"`
	OrderReconciliationUnknownBroker  int                           `json:"order_reconciliation_unknown_broker_orders"`
	OrderReconciliationLocalMissing   int                           `json:"order_reconciliation_local_missing_at_broker"`
	OrderReconciliationFillMismatch   int                           `json:"order_reconciliation_fill_mismatches"`
	OrderReconciliationSummary        string                        `json:"order_reconciliation_summary"`
	PositionReconciliationRuns        int                           `json:"position_reconciliation_runs"`
	PositionReconciliationIncidents   int                           `json:"position_reconciliation_incidents"`
	PositionReconciliationMismatches  int                           `json:"position_reconciliation_mismatches"`
	PositionReconciliationLocalMiss   int                           `json:"position_reconciliation_local_missing_at_broker"`
	PositionReconciliationBrokerMiss  int                           `json:"position_reconciliation_broker_missing_locally"`
	PositionReconciliationSizeMiss    int                           `json:"position_reconciliation_size_mismatches"`
	PositionReconciliationPriceMiss   int                           `json:"position_reconciliation_price_mismatches"`
	PositionReconciliationStatus      string                        `json:"position_reconciliation_status"`
	PositionReconciliationSummary     string                        `json:"position_reconciliation_summary"`
	IncidentCount                     int                           `json:"incident_count"`
	CriticalIncidentCount             int                           `json:"critical_incident_count"`
	IncidentTypesSeen                 []string                      `json:"incident_types_seen"`
	UnresolvedIncidentsAtEnd          []string                      `json:"unresolved_incidents_at_end"`
	NotableIncidents                  []string                      `json:"notable_incidents"`
	SessionHadOperationalIncident     bool                          `json:"session_had_operational_incident"`
	WarningsCount                     int                           `json:"warnings_count"`
	ErrorsCount                       int                           `json:"errors_count"`
	DistinctWarningMessages           []string                      `json:"distinct_warning_messages"`
	DistinctErrorMessages             []string                      `json:"distinct_error_messages"`
	LastBlockReason                   string                        `json:"last_block_reason"`
	ReconnectAttemptsTotal            int                           `json:"reconnect_attempts_total"`
	NotableEvents                     []string                      `json:"notable_events"`
}

type paperSessionTracker struct {
	report           PaperSessionReport
	currentDate      string
	startupSettled   bool
	startAccount     *AccountSummary
	lastAccount      *AccountSummary
	lastBroker       BrokerRuntimeState
	startOrderRecon  *orders.Summary
	startPosRecon    *positionReconciliationSummary
	symbols          map[string]struct{}
	warnings         map[string]struct{}
	errors           map[string]struct{}
	events           map[string]struct{}
	riskIncidents    map[string]struct{}
	incidents        map[string]struct{}
	criticalSeen     map[string]struct{}
	incidentTypes    map[string]struct{}
	notableIncidents map[string]struct{}
}

func (at *AutoTrader) paperSessionReportsEnabled() bool {
	if at.backtestMode {
		return false
	}
	return at.demoMode || strings.EqualFold(at.config.Mode, "paper") || at.shadowModeEnabled()
}

func (at *AutoTrader) currentTradingAllowed() bool {
	return at.currentTradingGateDecision(false, at.currentLatestAccountSummary()).TradingAllowed
}

func (at *AutoTrader) startPaperSessionReporting(now time.Time) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.ensurePaperSessionReportingForTime(now)
}

func (at *AutoTrader) ensurePaperSessionReportingForTime(now time.Time) {
	if !at.paperSessionReportsEnabled() {
		return
	}

	now = now.In(time.Local)
	dateKey := now.Format("2006-01-02")

	var oldTracker *paperSessionTracker
	created := false

	at.sessionReportMu.Lock()
	if at.sessionReportState == nil {
		at.sessionReportState = newPaperSessionTracker(at, now)
		oldTracker = nil
		created = true
	} else if at.sessionReportState.currentDate != dateKey {
		oldTracker = at.sessionReportState
		at.sessionReportState = newPaperSessionTracker(at, now)
		created = true
	}
	at.sessionReportMu.Unlock()

	if oldTracker != nil {
		at.writePaperSessionReport(oldTracker, "date_rollover")
	}

	if created {
		readiness := at.getReadinessSummary()
		tradingAllowed := readiness.TradingAllowed && at.currentTradingAllowed()
		shouldCaptureStart := false

		at.sessionReportMu.Lock()
		if tracker := at.sessionReportState; tracker != nil {
			if readiness.Status != "" && !isPendingReadinessSummary(readiness) {
				tracker.observeReadinessSummary(readiness, tradingAllowed)
			}
			shouldCaptureStart = tradingAllowed && tracker.startAccount == nil
		}
		at.sessionReportMu.Unlock()

		if shouldCaptureStart {
			at.capturePaperSessionStartAccountSummary()
		}
	}
}

func newPaperSessionTracker(at *AutoTrader, now time.Time) *paperSessionTracker {
	orderRecon := at.currentOrderReconciliationSummary()
	positionRecon := at.currentPositionReconciliationSummary()
	tracker := &paperSessionTracker{
		report: PaperSessionReport{
			ReportVersion:            sessionReportVersion,
			TraderID:                 at.id,
			TraderName:               at.name,
			Mode:                     at.config.Mode,
			Broker:                   at.config.Broker,
			StrategyMode:             at.config.StrategyMode,
			SessionDate:              now.Format("2006-01-02"),
			SessionStart:             now,
			StrategyInitialCapital:   at.initialBalance,
			SymbolsTraded:            []string{},
			DistinctRiskMessages:     []string{},
			DistinctWarningMessages:  []string{},
			DistinctErrorMessages:    []string{},
			NotableRiskIncidents:     []string{},
			IncidentTypesSeen:        []string{},
			UnresolvedIncidentsAtEnd: []string{},
			NotableIncidents:         []string{},
			NotableEvents:            []string{},
		},
		currentDate:      now.Format("2006-01-02"),
		lastBroker:       at.brokerRuntimeStatus().State,
		startOrderRecon:  orderRecon,
		startPosRecon:    positionRecon,
		symbols:          make(map[string]struct{}),
		warnings:         make(map[string]struct{}),
		errors:           make(map[string]struct{}),
		events:           make(map[string]struct{}),
		riskIncidents:    make(map[string]struct{}),
		incidents:        make(map[string]struct{}),
		criticalSeen:     make(map[string]struct{}),
		incidentTypes:    make(map[string]struct{}),
		notableIncidents: make(map[string]struct{}),
	}
	tracker.report.BrokerStateFinal = string(tracker.lastBroker)
	return tracker
}

func (at *AutoTrader) observePaperSessionIncident(incident incidents.Incident) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	ts := incident.UpdatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	at.ensurePaperSessionReportingForTime(ts)
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.observeIncident(incident)
	}
}

func (at *AutoTrader) observeReadinessSummary(summary ReadinessSummary) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	if isPendingReadinessSummary(summary) {
		return
	}
	checkedAt := summary.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	at.ensurePaperSessionReportingForTime(checkedAt)

	shouldCaptureStart := false
	tradingAllowed := summary.TradingAllowed && at.currentTradingAllowed()

	at.sessionReportMu.Lock()
	tracker := at.sessionReportState
	if tracker != nil {
		tracker.observeReadinessSummary(summary, tradingAllowed)
		shouldCaptureStart = tradingAllowed && tracker.startAccount == nil
	}
	at.sessionReportMu.Unlock()

	if shouldCaptureStart {
		at.capturePaperSessionStartAccountSummary()
	}
}

func (t *paperSessionTracker) observeReadinessSummary(summary ReadinessSummary, tradingAllowed bool) {
	t.report.StartupReadinessStatus = summary.Status
	t.report.StartupReadinessMessage = summary.Message
	t.report.TradingAllowedAtStart = tradingAllowed
	if tradingAllowed {
		t.startupSettled = true
	}
	if !tradingAllowed && summary.Message != "" {
		t.report.LastBlockReason = summary.Message
		t.addNotableEvent("startup readiness blocked trading: " + summary.Message)
	}
	for _, check := range summary.Checks {
		switch check.Status {
		case ReadinessWarn:
			t.addWarning(fmt.Sprintf("%s: %s", check.Name, check.Message))
		case ReadinessFail:
			t.addError(fmt.Sprintf("%s: %s", check.Name, check.Message))
		}
	}
}

func (at *AutoTrader) capturePaperSessionStartAccountSummary() {
	if !at.paperSessionReportsEnabled() {
		return
	}
	if at.trader == nil {
		at.recordPaperSessionWarning("failed to capture startup account snapshot: trader is not initialized")
		return
	}
	summary, err := at.GetAccountInfo()
	if err != nil {
		at.recordPaperSessionWarning(fmt.Sprintf("failed to capture startup account snapshot: %v", err))
		return
	}

	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()

	if tracker := at.sessionReportState; tracker != nil {
		if tracker.startAccount == nil {
			tracker.startAccount = cloneAccountSummary(summary)
		}
		tracker.observeAccount(summary)
	}
}

func (at *AutoTrader) recordPaperSessionCycleStart() {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.report.DecisionCycles++
	}
}

func (at *AutoTrader) recordPaperSessionBlockedCycle(reason string) {
	if !at.paperSessionReportsEnabled() {
		at.emitBlockedCycleAlert(reason)
		return
	}
	at.emitBlockedCycleAlert(reason)
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.report.BlockedCyclesCount++
		if reason = strings.TrimSpace(reason); reason != "" {
			tracker.report.LastBlockReason = reason
			tracker.addWarning(reason)
		}
	}
}

func (at *AutoTrader) emitBlockedCycleAlert(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "trading blocked"
	}
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "market is closed") {
		at.alertRuntimeInfo("market_closed", "market closed: "+reason, map[string]string{
			"reason": reason,
		})
		return
	}
	at.alertTradingBlocked(reason)
}

func (at *AutoTrader) observePaperSessionDecisionRecord(record *logger.DecisionRecord) {
	if !at.paperSessionReportsEnabled() || record == nil {
		return
	}

	ts := record.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	at.ensurePaperSessionReportingForTime(ts)

	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.observeDecisionRecord(record)
	}
}

func (at *AutoTrader) observePaperSessionRiskSupervisor(state risk.SupervisorState) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	checkedAt := state.EvaluatedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	at.ensurePaperSessionReportingForTime(checkedAt)
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.observeRiskSupervisor(state)
	}
}

func (t *paperSessionTracker) observeDecisionRecord(record *logger.DecisionRecord) {
	if record.AccountState.HasCanonicalAccounting() {
		t.observeAccount(accountSummaryFromSnapshot(record.AccountState))
	}
	if !record.Success && strings.TrimSpace(record.ErrorMessage) != "" {
		t.addError(record.ErrorMessage)
	}

	for _, action := range record.Decisions {
		actionType := strings.ToLower(strings.TrimSpace(action.Action))
		if actionType == "hold" || actionType == "wait" || actionType == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(action.OrderStatus))
		trackedExecution := isTrackedExecutionStatus(status)
		if outcome := strings.TrimSpace(action.RiskOutcome); outcome != "" {
			t.report.RiskEvaluations++
			switch outcome {
			case "reject":
				t.report.RiskRejectedOrders++
			case "reduce_size":
				t.report.RiskReducedOrders++
			}
			if summary := strings.TrimSpace(action.RiskSummary); summary != "" {
				t.addRiskMessage(summary)
			}
		}
		t.report.ActionableDecisions++
		if action.Shadow != nil && action.Shadow.Active {
			t.report.ShadowModeActive = true
			t.report.ShadowDecisionsTotal++
			if action.Shadow.WouldTrade {
				t.report.ShadowWouldTradeCount++
			} else {
				t.report.ShadowBlockedCount++
			}
			if !action.Shadow.RecordedAt.IsZero() {
				t.report.ShadowLastDecisionAt = action.Shadow.RecordedAt.Format(time.RFC3339)
			}
		}
		t.report.OrderSubmitAttempts++
		if trackedExecution {
			t.report.ExecutionIntentsTotal++
			switch execution.Status(status) {
			case execution.StatusBlocked:
				t.report.ExecutionBlockedCount++
			case execution.StatusDuplicateSuppressed:
				t.report.DuplicateSuppressedCount++
			case execution.StatusStale:
				t.report.StaleExecutionCount++
			case execution.StatusSubmitted:
				t.report.ExecutionSubmittedCount++
			case execution.StatusAcknowledged:
				t.report.ExecutionSubmittedCount++
				t.report.ExecutionAcknowledgedCount++
			case execution.StatusPartiallyFilled:
				t.report.ExecutionSubmittedCount++
				t.report.ExecutionFilledCount++
			case execution.StatusFilled:
				t.report.ExecutionSubmittedCount++
				t.report.ExecutionFilledCount++
			case execution.StatusRejected:
				t.report.ExecutionRejectedCount++
			case execution.StatusCancelled, execution.StatusFailed:
				t.report.ExecutionFailedCount++
			}
		}
		if !action.Success {
			if strings.TrimSpace(action.Error) != "" {
				t.addError(action.Error)
			}
			continue
		}

		if trackedExecution {
			switch execution.Status(status) {
			case execution.StatusSubmitted:
				t.report.OrdersSubmitted++
			case execution.StatusAcknowledged:
				t.report.OrdersSubmitted++
			case execution.StatusPartiallyFilled, execution.StatusFilled:
				t.report.OrdersSubmitted++
				t.report.OrdersFilled++
			}
		} else {
			t.report.OrdersSubmitted++
			t.report.OrdersFilled++
		}
		if actionHasImmediatePositionEffect(action) {
			switch actionType {
			case "open_long", "close_short":
				t.report.BuyFills++
			case "open_short", "close_long":
				t.report.SellFills++
			}
			switch actionType {
			case "open_long", "open_short":
				t.report.PositionsOpenedCount++
			case "close_long", "close_short":
				t.report.PositionsClosedCount++
			}
		}

		if actionHasImmediatePositionEffect(action) {
			symbol := strings.ToUpper(strings.TrimSpace(action.Symbol))
			if symbol != "" {
				t.symbols[symbol] = struct{}{}
			}
		}
	}
}

func (t *paperSessionTracker) observePortfolioRisk(snapshot *portfolioRiskState) {
	if snapshot == nil {
		return
	}
	t.report.PortfolioRiskLatest = &SessionPortfolioRiskSnapshot{
		EvaluatedAt: snapshot.EvaluatedAt,
		Outcome:     snapshot.Outcome,
		Summary:     snapshot.Summary,
		Metrics:     snapshot.Metrics.Clone(),
	}
	if t.report.PortfolioRiskPeaks == nil {
		t.report.PortfolioRiskPeaks = &SessionPortfolioRiskPeaks{}
	}
	peaks := t.report.PortfolioRiskPeaks
	if snapshot.Metrics.CurrentGrossExposurePct > peaks.MaxGrossExposurePct {
		peaks.MaxGrossExposurePct = snapshot.Metrics.CurrentGrossExposurePct
	}
	if absPortfolioMetric(snapshot.Metrics.CurrentNetExposurePct) > peaks.MaxNetExposurePct {
		peaks.MaxNetExposurePct = absPortfolioMetric(snapshot.Metrics.CurrentNetExposurePct)
	}
	if snapshot.Metrics.LargestSectorExposurePct > peaks.MaxSectorExposurePct {
		peaks.MaxSectorExposurePct = snapshot.Metrics.LargestSectorExposurePct
	}
	if snapshot.Metrics.CorrelatedPositionCount > peaks.MaxCorrelatedPositions {
		peaks.MaxCorrelatedPositions = snapshot.Metrics.CorrelatedPositionCount
	}
	if snapshot.Metrics.MaxObservedCorrelation > peaks.MaxObservedCorrelation {
		peaks.MaxObservedCorrelation = snapshot.Metrics.MaxObservedCorrelation
	}
	if snapshot.Metrics.CurrentDrawdownPct > peaks.MaxDrawdownPct {
		peaks.MaxDrawdownPct = snapshot.Metrics.CurrentDrawdownPct
	}
}

func (t *paperSessionTracker) observeRiskSupervisor(state risk.SupervisorState) {
	t.report.FinalRiskMode = state.Mode
	t.report.RiskIncidentCount = state.ActiveIncidentCount
	t.report.CriticalRiskIncidentCount = state.CriticalIncidentCount
	t.report.RiskSupervisorSummary = strings.TrimSpace(state.Summary)
	if state.Mode != risk.SupervisorModeAllow {
		t.report.SupervisorRestrictedDuringSession = true
	}
	for _, incident := range state.Incidents {
		message := strings.TrimSpace(incident.Summary)
		if message == "" {
			message = string(incident.Type)
		}
		if _, exists := t.riskIncidents[message]; exists {
			continue
		}
		t.riskIncidents[message] = struct{}{}
		if len(t.report.NotableRiskIncidents) < 12 {
			t.report.NotableRiskIncidents = append(t.report.NotableRiskIncidents, message)
		}
	}
}

func (t *paperSessionTracker) observeIncident(incident incidents.Incident) {
	if incident.IncidentID == "" {
		return
	}
	t.report.SessionHadOperationalIncident = true
	if _, exists := t.incidents[incident.IncidentID]; !exists {
		t.incidents[incident.IncidentID] = struct{}{}
		t.report.IncidentCount++
	}
	if incident.Severity == incidents.SeverityCritical {
		if _, exists := t.criticalSeen[incident.IncidentID]; !exists {
			t.criticalSeen[incident.IncidentID] = struct{}{}
			t.report.CriticalIncidentCount++
		}
	}
	if incidentType := strings.TrimSpace(string(incident.IncidentType)); incidentType != "" {
		t.incidentTypes[incidentType] = struct{}{}
	}
	summary := strings.TrimSpace(incident.Summary)
	if summary == "" {
		summary = string(incident.IncidentType)
	}
	if _, exists := t.notableIncidents[summary]; !exists {
		t.notableIncidents[summary] = struct{}{}
		if len(t.report.NotableIncidents) < 12 {
			t.report.NotableIncidents = append(t.report.NotableIncidents, summary)
		}
	}
}

func (t *paperSessionTracker) observeAccount(summary *AccountSummary) {
	if summary == nil {
		return
	}
	t.lastAccount = cloneAccountSummary(summary)
	if summary.PositionCount > t.report.MaxConcurrentPositionsObserved {
		t.report.MaxConcurrentPositionsObserved = summary.PositionCount
	}
	if summary.StrategyEquity > 0 {
		exposure := sanitizeFloat(summary.GrossMarketValue / summary.StrategyEquity)
		if t.report.MaxGrossExposureObserved == nil || exposure > *t.report.MaxGrossExposureObserved {
			t.report.MaxGrossExposureObserved = float64Ptr(exposure)
		}
	}
}

func (at *AutoTrader) observePaperBrokerState(state BrokerRuntimeState, reason string) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.ensurePaperSessionReportingForTime(time.Now())

	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.observeBrokerState(state, reason)
	}
}

func (t *paperSessionTracker) observeBrokerState(state BrokerRuntimeState, reason string) {
	t.report.BrokerStateFinal = string(state)
	if state != t.lastBroker {
		switch state {
		case BrokerRuntimeDegraded, BrokerRuntimePaused:
			t.report.BrokerDegradedEventsCount++
			if strings.TrimSpace(reason) != "" {
				t.addWarning(reason)
			}
		case BrokerRuntimeReconnecting, BrokerRuntimeReconciling:
			if strings.TrimSpace(reason) != "" {
				t.addNotableEvent(fmt.Sprintf("broker %s: %s", state, reason))
			}
		}
		t.lastBroker = state
	}
}

func (at *AutoTrader) observePaperBrokerReconnectAttempt() {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.ensurePaperSessionReportingForTime(time.Now())
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.report.ReconnectAttemptsTotal++
	}
}

func (at *AutoTrader) recordPaperSessionWarning(message string) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.addWarning(message)
	}
}

func (at *AutoTrader) recordPaperSessionError(message string) {
	if !at.paperSessionReportsEnabled() {
		return
	}
	at.sessionReportMu.Lock()
	defer at.sessionReportMu.Unlock()
	if tracker := at.sessionReportState; tracker != nil {
		tracker.addError(message)
	}
}

func (t *paperSessionTracker) addWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if _, exists := t.warnings[message]; exists {
		return
	}
	t.warnings[message] = struct{}{}
	t.report.WarningsCount++
	if len(t.report.DistinctWarningMessages) < 12 {
		t.report.DistinctWarningMessages = append(t.report.DistinctWarningMessages, message)
	}
}

func (t *paperSessionTracker) addError(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if _, exists := t.errors[message]; exists {
		return
	}
	t.errors[message] = struct{}{}
	t.report.ErrorsCount++
	if len(t.report.DistinctErrorMessages) < 12 {
		t.report.DistinctErrorMessages = append(t.report.DistinctErrorMessages, message)
	}
}

func (t *paperSessionTracker) addRiskMessage(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	for _, existing := range t.report.DistinctRiskMessages {
		if existing == message {
			return
		}
	}
	if len(t.report.DistinctRiskMessages) < 12 {
		t.report.DistinctRiskMessages = append(t.report.DistinctRiskMessages, message)
	}
}

func (t *paperSessionTracker) addNotableEvent(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if _, exists := t.events[message]; exists {
		return
	}
	t.events[message] = struct{}{}
	if len(t.report.NotableEvents) < 20 {
		t.report.NotableEvents = append(t.report.NotableEvents, message)
	}
}

func (at *AutoTrader) finalizePaperSessionReport(reason string) {
	if !at.paperSessionReportsEnabled() {
		return
	}

	at.sessionReportMu.Lock()
	tracker := at.sessionReportState
	at.sessionReportState = nil
	at.sessionReportMu.Unlock()

	if tracker == nil {
		return
	}

	at.writePaperSessionReport(tracker, reason)
}

func (at *AutoTrader) writePaperSessionReport(tracker *paperSessionTracker, reason string) {
	if tracker == nil {
		return
	}

	endTime := time.Now().In(time.Local)
	report := tracker.report
	report.GeneratedAt = endTime
	report.SessionEnd = endTime
	report.SessionDurationSeconds = int64(endTime.Sub(report.SessionStart).Seconds())

	startSummary := tracker.startAccount
	endSummary := tracker.lastAccount
	endFetchErr := error(nil)

	if summary, err := at.GetAccountInfo(); err == nil {
		endSummary = cloneAccountSummary(summary)
	} else {
		endFetchErr = err
		if endSummary == nil {
			tracker.addWarning(fmt.Sprintf("failed to capture final account snapshot: %v", err))
		}
	}

	applyPaperSessionAccounting(&report, startSummary, endSummary)
	applyPaperSessionOrderReconciliation(&report, tracker.startOrderRecon, at.currentOrderReconciliationSummary())
	applyPaperSessionPositionReconciliation(&report, tracker.startPosRecon, at.currentPositionReconciliationSummary())
	if latestPortfolioRisk := at.currentPortfolioRiskState(); latestPortfolioRisk != nil {
		tracker.observePortfolioRisk(latestPortfolioRisk)
	}
	tracker.observeRiskSupervisor(at.currentRiskSupervisorState())
	report.PortfolioRiskLatest = cloneSessionPortfolioRiskSnapshot(tracker.report.PortfolioRiskLatest)
	report.PortfolioRiskPeaks = cloneSessionPortfolioRiskPeaks(tracker.report.PortfolioRiskPeaks)
	report.SymbolsTraded = sortedKeys(tracker.symbols)
	report.IncidentTypesSeen = sortedKeys(tracker.incidentTypes)
	if report.BrokerStateFinal == "" {
		report.BrokerStateFinal = string(at.brokerRuntimeStatus().State)
	}
	incidentSummary := at.currentIncidentSummary()
	report.UnresolvedIncidentsAtEnd = summarizeIncidentList(incidentSummary.OpenIncidents, 12)
	if report.IncidentCount > 0 || len(report.UnresolvedIncidentsAtEnd) > 0 {
		report.SessionHadOperationalIncident = true
	}
	report.SessionCompletionStatus = classifyPaperSessionCompletion(report, startSummary != nil, endSummary != nil, endFetchErr != nil)
	if strings.TrimSpace(reason) != "" {
		tracker.addNotableEvent("session finalized: " + reason)
	}
	report.WarningsCount = tracker.report.WarningsCount
	report.ErrorsCount = tracker.report.ErrorsCount
	report.LastBlockReason = tracker.report.LastBlockReason
	report.BrokerDegradedEventsCount = tracker.report.BrokerDegradedEventsCount
	report.BlockedCyclesCount = tracker.report.BlockedCyclesCount
	report.ReconnectAttemptsTotal = tracker.report.ReconnectAttemptsTotal
	report.RiskEvaluations = tracker.report.RiskEvaluations
	report.RiskRejectedOrders = tracker.report.RiskRejectedOrders
	report.RiskReducedOrders = tracker.report.RiskReducedOrders
	report.DistinctRiskMessages = append([]string(nil), tracker.report.DistinctRiskMessages...)
	report.FinalRiskMode = tracker.report.FinalRiskMode
	report.RiskIncidentCount = tracker.report.RiskIncidentCount
	report.CriticalRiskIncidentCount = tracker.report.CriticalRiskIncidentCount
	report.SupervisorRestrictedDuringSession = tracker.report.SupervisorRestrictedDuringSession
	report.RiskSupervisorSummary = tracker.report.RiskSupervisorSummary
	report.NotableRiskIncidents = append([]string(nil), tracker.report.NotableRiskIncidents...)
	report.IncidentCount = tracker.report.IncidentCount
	report.CriticalIncidentCount = tracker.report.CriticalIncidentCount
	report.NotableIncidents = append([]string(nil), tracker.report.NotableIncidents...)
	report.DistinctWarningMessages = append([]string(nil), tracker.report.DistinctWarningMessages...)
	report.DistinctErrorMessages = append([]string(nil), tracker.report.DistinctErrorMessages...)
	report.NotableEvents = append([]string(nil), tracker.report.NotableEvents...)
	if shadow := at.currentShadowSummary(); shadow.Available {
		report.ShadowModeActive = shadow.Active
		report.ShadowOpenPositionsEnd = shadow.OpenPositions
		report.ShadowClosedTrades = shadow.ClosedTrades
		report.ShadowRealizedPnL = shadow.HypotheticalRealizedPnL
		report.ShadowUnrealizedPnL = shadow.HypotheticalUnrealizedPnL
		if report.ShadowLastDecisionAt == "" {
			report.ShadowLastDecisionAt = formatRFC3339(shadow.LastDecisionAt)
		}
	}
	restartRecovery := at.currentRestartRecoverySummary()
	report.RestartRecoveryRestored = restartRecovery.Restored
	report.RestartRecoveryPending = restartRecovery.PendingReconciliation
	report.RestartRecoveryBlocked = restartRecovery.TradingBlocked
	report.RestartRecoveryMessage = restartRecovery.Message

	path := sessionReportPath(at.id, report.SessionStart, report.SessionDate)
	if err := writePaperSessionReport(path, report); err != nil {
		log.Printf(" [%s] Failed to write paper session report: %v", at.name, err)
		return
	}

	at.sessionReportMu.Lock()
	at.lastSessionReportPath = path
	at.lastSessionReportAt = report.GeneratedAt
	at.lastSessionReportStatus = string(report.SessionCompletionStatus)
	at.sessionReportMu.Unlock()

	log.Printf(
		" [%s] Paper session report written: %s | status=%s | cycles=%d | total_pnl=%s | positions_opened=%d | positions_closed=%d | risk_rejected=%d | risk_reduced=%d",
		at.name,
		path,
		report.SessionCompletionStatus,
		report.DecisionCycles,
		formatNullableFloat(report.TotalPnL),
		report.PositionsOpenedCount,
		report.PositionsClosedCount,
		report.RiskRejectedOrders,
		report.RiskReducedOrders,
	)
}

func sessionReportPath(traderID string, sessionStart time.Time, sessionDate string) string {
	filename := fmt.Sprintf("%s_session_%s_%s.json",
		traderID,
		strings.ReplaceAll(sessionDate, "-", ""),
		sessionStart.In(time.Local).Format("150405"),
	)
	return filepath.Join("output", "session_reports", traderID, filename)
}

func writePaperSessionReport(path string, report PaperSessionReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create session report directory: %w", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session report: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write session report: %w", err)
	}
	return nil
}

func applyPaperSessionAccounting(report *PaperSessionReport, start, end *AccountSummary) {
	if report == nil {
		return
	}
	if start != nil {
		report.StrategyInitialCapital = start.StrategyInitialCapital
		report.StartingStrategyEquity = float64Ptr(start.StrategyEquity)
	}
	if end != nil {
		if report.StrategyInitialCapital <= 0 {
			report.StrategyInitialCapital = end.StrategyInitialCapital
		}
		report.EndingStrategyEquity = float64Ptr(end.StrategyEquity)
		report.AccountCashEnd = float64Ptr(end.AccountCash)
		report.AccountEquityEnd = float64Ptr(end.AccountEquity)
		report.UnrealizedPnLEnd = float64Ptr(end.UnrealizedPnL)
		report.EndingCumulativeStrategyReturnPct = float64Ptr(end.StrategyReturnPct)
	}
	if start == nil || end == nil {
		return
	}

	realizedDelta := sanitizeFloat(end.RealizedPnL - start.RealizedPnL)
	totalDelta := sanitizeFloat(end.StrategyEquity - start.StrategyEquity)
	sessionReturnPct := 0.0
	if start.StrategyEquity != 0 {
		sessionReturnPct = sanitizeFloat((totalDelta / start.StrategyEquity) * 100.0)
	}

	report.RealizedPnL = float64Ptr(realizedDelta)
	report.TotalPnL = float64Ptr(totalDelta)
	report.StrategyReturnPct = float64Ptr(sessionReturnPct)
}

func applyPaperSessionOrderReconciliation(report *PaperSessionReport, start, end *orders.Summary) {
	if report == nil || end == nil {
		return
	}
	startRuns := 0
	startMismatches := 0
	startRepairs := 0
	startUnknown := 0
	startMissing := 0
	startFillMismatch := 0
	if start != nil {
		startRuns = start.TotalRuns
		startMismatches = start.TotalMismatches
		startRepairs = start.TotalRepairs
		startUnknown = start.UnknownBrokerOrders
		startMissing = start.LocalMissingAtBroker
		startFillMismatch = start.FillMismatches
	}
	report.OrderReconciliationRuns = maxInt(0, end.TotalRuns-startRuns)
	report.OrderReconciliationMismatches = maxInt(0, end.TotalMismatches-startMismatches)
	report.OrderReconciliationRepairs = maxInt(0, end.TotalRepairs-startRepairs)
	report.OrderReconciliationUnknownBroker = maxInt(0, end.UnknownBrokerOrders-startUnknown)
	report.OrderReconciliationLocalMissing = maxInt(0, end.LocalMissingAtBroker-startMissing)
	report.OrderReconciliationFillMismatch = maxInt(0, end.FillMismatches-startFillMismatch)
	report.OrderReconciliationSummary = strings.TrimSpace(end.LastSummary)
}

func applyPaperSessionPositionReconciliation(report *PaperSessionReport, start, end *positionReconciliationSummary) {
	if report == nil || end == nil {
		return
	}
	startRuns := 0
	startIncidents := 0
	startMismatches := 0
	startLocalMissing := 0
	startBrokerMissing := 0
	startSizeMiss := 0
	startPriceMiss := 0
	if start != nil {
		startRuns = start.TotalRuns
		startIncidents = start.TotalIncidents
		startMismatches = start.TotalMismatches
		startLocalMissing = start.LocalMissingAtBroker
		startBrokerMissing = start.BrokerMissingLocally
		startSizeMiss = start.SizeMismatches
		startPriceMiss = start.PriceMismatches
	}
	report.PositionReconciliationRuns = maxInt(0, end.TotalRuns-startRuns)
	report.PositionReconciliationIncidents = maxInt(0, end.TotalIncidents-startIncidents)
	report.PositionReconciliationMismatches = maxInt(0, end.TotalMismatches-startMismatches)
	report.PositionReconciliationLocalMiss = maxInt(0, end.LocalMissingAtBroker-startLocalMissing)
	report.PositionReconciliationBrokerMiss = maxInt(0, end.BrokerMissingLocally-startBrokerMissing)
	report.PositionReconciliationSizeMiss = maxInt(0, end.SizeMismatches-startSizeMiss)
	report.PositionReconciliationPriceMiss = maxInt(0, end.PriceMismatches-startPriceMiss)
	report.PositionReconciliationStatus = string(end.Status)
	report.PositionReconciliationSummary = strings.TrimSpace(end.Summary)
}

func classifyPaperSessionCompletion(report PaperSessionReport, hasStart, hasEnd, missingFinalSnapshot bool) SessionCompletionStatus {
	if !report.TradingAllowedAtStart {
		return SessionCompletionBlocked
	}
	if report.DecisionCycles == 0 {
		if hasStart && hasEnd {
			return SessionCompletionPartial
		}
		return SessionCompletionBlocked
	}
	if !hasStart || !hasEnd || missingFinalSnapshot {
		return SessionCompletionPartial
	}
	if report.SupervisorRestrictedDuringSession || report.FinalRiskMode == risk.SupervisorModeReduceOnly || report.FinalRiskMode == risk.SupervisorModeBlockNewEntries || report.FinalRiskMode == risk.SupervisorModeHalted {
		return SessionCompletionDegraded
	}
	if report.PositionReconciliationIncidents > 0 {
		return SessionCompletionDegraded
	}
	if report.SessionHadOperationalIncident && report.CriticalIncidentCount > 0 {
		return SessionCompletionDegraded
	}
	if report.BrokerDegradedEventsCount > 0 || report.BlockedCyclesCount > 0 {
		return SessionCompletionDegraded
	}
	finalState := strings.ToLower(strings.TrimSpace(report.BrokerStateFinal))
	if finalState == string(BrokerRuntimeDegraded) ||
		finalState == string(BrokerRuntimePaused) ||
		finalState == string(BrokerRuntimeReconnecting) ||
		finalState == string(BrokerRuntimeReconciling) {
		return SessionCompletionDegraded
	}
	return SessionCompletionCompleted
}

func cloneAccountSummary(summary *AccountSummary) *AccountSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}

func accountSummaryFromSnapshot(snapshot logger.AccountSnapshot) *AccountSummary {
	if !snapshot.HasCanonicalAccounting() {
		return nil
	}
	return &AccountSummary{
		AccountingVersion:      snapshot.AccountingVersion,
		AccountCash:            snapshot.AccountCash,
		AvailableBalance:       snapshot.AvailableBalance,
		AccountEquity:          snapshot.AccountEquity,
		GrossMarketValue:       snapshot.GrossMarketValue,
		UnrealizedPnL:          snapshot.UnrealizedPnL,
		RealizedPnL:            snapshot.RealizedPnL,
		TotalPnL:               snapshot.TotalPnL,
		StrategyInitialCapital: snapshot.StrategyInitialCapital,
		StrategyEquity:         snapshot.StrategyEquity,
		StrategyReturnPct:      snapshot.StrategyReturnPct,
		DailyPnL:               snapshot.DailyPnL,
		PositionCount:          snapshot.PositionCount,
		MarginUsed:             snapshot.MarginUsed,
		MarginUsedPct:          snapshot.MarginUsedPct,
	}
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func summarizeIncidentList(items []incidents.Incident, limit int) []string {
	if len(items) == 0 {
		return []string{}
	}
	if limit <= 0 {
		limit = len(items)
	}
	out := make([]string, 0, minInt(limit, len(items)))
	for _, item := range items {
		label := strings.TrimSpace(item.Summary)
		if label == "" {
			label = string(item.IncidentType)
		}
		if item.Severity != "" {
			label = fmt.Sprintf("[%s] %s", item.Severity, label)
		}
		out = append(out, label)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func float64Ptr(value float64) *float64 {
	v := sanitizeFloat(value)
	return &v
}

func absPortfolioMetric(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cloneSessionPortfolioRiskSnapshot(snapshot *SessionPortfolioRiskSnapshot) *SessionPortfolioRiskSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Metrics = snapshot.Metrics.Clone()
	return &cloned
}

func cloneSessionPortfolioRiskPeaks(peaks *SessionPortfolioRiskPeaks) *SessionPortfolioRiskPeaks {
	if peaks == nil {
		return nil
	}
	cloned := *peaks
	return &cloned
}

func formatNullableFloat(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *value)
}

func isPendingReadinessSummary(summary ReadinessSummary) bool {
	return !summary.TradingAllowed && strings.EqualFold(strings.TrimSpace(summary.Message), "startup readiness pending")
}
