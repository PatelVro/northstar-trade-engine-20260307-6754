package trader

import (
	"fmt"
	"northstar/decision"
	"northstar/risk"
	"strings"
	"time"
)

type tradingGateDecision struct {
	Mode            risk.SupervisorMode
	TradingAllowed  bool
	EntriesAllowed  bool
	ExitsAllowed    bool
	ReduceOnly      bool
	BlockReason     string
	BlockingReasons []string
	Message         string
}

type strictLiveStatus struct {
	Required  bool
	Healthy   bool
	Message   string
	CheckedAt time.Time
}

func (at *AutoTrader) initializeRiskSupervisorState() {
	at.ensureRiskSupervisorSessionWindow(time.Now())
	state := risk.SupervisorState{
		EvaluatedAt:         time.Now(),
		Mode:                risk.SupervisorModeHalted,
		TradingAllowed:      false,
		EntriesAllowed:      false,
		ExitsAllowed:        false,
		ReduceOnly:          false,
		Summary:             "risk supervisor pending first evaluation",
		Incidents:           []risk.Incident{},
		ActiveIncidentCount: 0,
	}
	at.riskSupervisorMu.Lock()
	at.riskSupervisor = risk.NewSupervisor(risk.SupervisorConfig{
		MaxRuntimeDegradationsPerSession:    at.config.MaxRuntimeDegradationsPerSession,
		MaxReconciliationFailuresPerSession: at.config.MaxReconciliationFailuresPerSession,
		MaxOrderRejectsPerSession:           at.config.MaxOrderRejectsPerSession,
		EnableReduceOnlyOnDrawdown:          at.config.SupervisorReduceOnlyOnDrawdown,
	})
	at.riskSupervisorState = state
	at.riskSupervisorMu.Unlock()

	at.strictLiveMu.Lock()
	at.strictLiveHealthy = !at.requiresStrictLiveCheck()
	if at.requiresStrictLiveCheck() {
		at.strictLiveMessage = "strict live readiness pending first probe"
	} else {
		at.strictLiveMessage = "strict live readiness not required"
	}
	at.strictLiveLastCheckedAt = time.Now()
	at.strictLiveMu.Unlock()
}

func (at *AutoTrader) requiresStrictLiveCheck() bool {
	return at.exchange == "ibkr" && at.config.StrictLiveMode && strings.EqualFold(at.config.Mode, "live")
}

func (at *AutoTrader) ensureRiskSupervisorSessionWindow(now time.Time) {
	dayKey := now.In(time.Local).Format("2006-01-02")
	at.riskSupervisorMu.Lock()
	defer at.riskSupervisorMu.Unlock()
	if at.riskSupervisorSessionDay == dayKey {
		return
	}
	at.riskSupervisorSessionDay = dayKey
	at.riskSupervisorBrokerDegradationEvents = 0
	at.riskSupervisorReconciliationFailures = 0
	at.riskSupervisorOrderRejects = 0
}

func (at *AutoTrader) observeRiskSupervisorBrokerDegradation() {
	at.ensureRiskSupervisorSessionWindow(time.Now())
	at.riskSupervisorMu.Lock()
	at.riskSupervisorBrokerDegradationEvents++
	at.riskSupervisorMu.Unlock()
}

func (at *AutoTrader) observeRiskSupervisorReconciliationFailure() {
	at.ensureRiskSupervisorSessionWindow(time.Now())
	at.riskSupervisorMu.Lock()
	at.riskSupervisorReconciliationFailures++
	at.riskSupervisorMu.Unlock()
}

func (at *AutoTrader) observeRiskSupervisorOrderReject() {
	at.ensureRiskSupervisorSessionWindow(time.Now())
	at.riskSupervisorMu.Lock()
	at.riskSupervisorOrderRejects++
	at.riskSupervisorMu.Unlock()
}

func (at *AutoTrader) currentRiskSupervisorState() risk.SupervisorState {
	at.riskSupervisorMu.RLock()
	defer at.riskSupervisorMu.RUnlock()
	state := at.riskSupervisorState
	if state.Incidents == nil {
		state.Incidents = []risk.Incident{}
	}
	return state
}

func (at *AutoTrader) setRiskSupervisorState(state risk.SupervisorState) {
	if state.Incidents == nil {
		state.Incidents = []risk.Incident{}
	}
	at.riskSupervisorMu.Lock()
	at.riskSupervisorState = state
	at.riskSupervisorMu.Unlock()
	at.syncRiskSupervisorIncidents(state)

	if !at.paperSessionReportsEnabled() || !at.isRunning {
		return
	}
	at.sessionReportMu.Lock()
	hasTracker := at.sessionReportState != nil
	at.sessionReportMu.Unlock()
	if hasTracker {
		at.observePaperSessionRiskSupervisor(state)
	}
}

func (at *AutoTrader) setLatestAccountSummary(summary *AccountSummary) {
	at.accountSummaryMu.Lock()
	defer at.accountSummaryMu.Unlock()
	at.lastAccountSummary = cloneAccountSummary(summary)
}

func (at *AutoTrader) currentLatestAccountSummary() *AccountSummary {
	at.accountSummaryMu.RLock()
	defer at.accountSummaryMu.RUnlock()
	return cloneAccountSummary(at.lastAccountSummary)
}

func (at *AutoTrader) currentStrictLiveStatus() strictLiveStatus {
	at.strictLiveMu.RLock()
	defer at.strictLiveMu.RUnlock()
	required := at.requiresStrictLiveCheck()
	healthy := at.strictLiveHealthy
	message := at.strictLiveMessage
	checkedAt := at.strictLiveLastCheckedAt
	if required && checkedAt.IsZero() {
		healthy = true
		message = "strict live readiness has not been probed yet"
	}
	return strictLiveStatus{
		Required:  required,
		Healthy:   healthy,
		Message:   message,
		CheckedAt: checkedAt,
	}
}

func (at *AutoTrader) probeStrictLiveReadiness() {
	status := strictLiveStatus{
		Required: at.requiresStrictLiveCheck(),
		Healthy:  true,
		Message:  "strict live readiness not required",
	}
	if status.Required {
		if err := at.ensureIBKRLiveReady(); err != nil {
			status.Healthy = false
			status.Message = err.Error()
		} else {
			status.Message = "strict live readiness passed"
		}
	}
	status.CheckedAt = time.Now()

	at.strictLiveMu.Lock()
	at.strictLiveHealthy = status.Healthy
	at.strictLiveMessage = status.Message
	at.strictLiveLastCheckedAt = status.CheckedAt
	at.strictLiveMu.Unlock()
}

func (at *AutoTrader) evaluateRiskSupervisor(account *AccountSummary, probeStrictLive bool) risk.SupervisorState {
	if probeStrictLive {
		at.probeStrictLiveReadiness()
	}
	if account == nil {
		account = at.currentLatestAccountSummary()
	}
	if account != nil {
		at.setLatestAccountSummary(account)
	}

	at.ensureRiskSupervisorSessionWindow(time.Now())

	at.riskSupervisorMu.Lock()
	if at.riskSupervisor == nil {
		at.riskSupervisor = risk.NewSupervisor(risk.SupervisorConfig{
			MaxRuntimeDegradationsPerSession:    at.config.MaxRuntimeDegradationsPerSession,
			MaxReconciliationFailuresPerSession: at.config.MaxReconciliationFailuresPerSession,
			MaxOrderRejectsPerSession:           at.config.MaxOrderRejectsPerSession,
			EnableReduceOnlyOnDrawdown:          at.config.SupervisorReduceOnlyOnDrawdown,
		})
	}
	supervisor := at.riskSupervisor
	brokerDegradations := at.riskSupervisorBrokerDegradationEvents
	reconciliationFailures := at.riskSupervisorReconciliationFailures
	orderRejects := at.riskSupervisorOrderRejects
	at.riskSupervisorMu.Unlock()

	readiness := at.getReadinessSummary()
	promotion := at.getPromotionSummary()
	brokerStatus := at.brokerRuntimeStatus()
	positionRecon := at.currentPositionReconciliationSummary()
	portfolioRisk := at.currentPortfolioRiskState()
	killSwitch := at.currentKillSwitchSummary()
	strictLive := at.currentStrictLiveStatus()

	snapshot := risk.SupervisorSnapshot{
		Now:                         time.Now(),
		ReadinessAllowed:            readiness.TradingAllowed,
		ReadinessMessage:            readiness.Message,
		PromotionRequired:           at.requiresLivePromotion(),
		PromotionAllowed:            !at.requiresLivePromotion() || promotion.LiveTradingAllowed,
		PromotionMessage:            promotion.Message,
		BrokerManaged:               at.managesIBKRBrokerRuntime(),
		BrokerHealthy:               !at.managesIBKRBrokerRuntime() || brokerStatus.State == BrokerRuntimeHealthy,
		BrokerState:                 string(brokerStatus.State),
		BrokerReason:                brokerStatus.Reason,
		KillSwitchActive:            killSwitch.Active,
		KillSwitchMessage:           killSwitch.Message,
		StrictLiveRequired:          strictLive.Required,
		StrictLiveHealthy:           !strictLive.Required || strictLive.Healthy,
		StrictLiveMessage:           strictLive.Message,
		SessionDailyPnL:             at.dailyPnL,
		SessionDailyLossLimit:       at.currentDailyLossLimit(),
		StopTradingUntil:            at.stopUntil,
		BrokerDegradationEvents:     brokerDegradations,
		ReconciliationFailureEvents: reconciliationFailures,
		OrderRejectEvents:           orderRejects,
	}

	if positionRecon != nil {
		snapshot.PositionReconciliationManaged = positionRecon.Available
		snapshot.PositionReconciliationAllowed = positionRecon.TradingAllowed
		snapshot.PositionReconciliationStatus = string(positionRecon.Status)
		snapshot.PositionReconciliationSummary = positionRecon.Summary
	}
	if account != nil {
		snapshot.CurrentPositionCount = account.PositionCount
		if account.StrategyEquity > 0 {
			snapshot.CurrentGrossExposurePct = sanitizeFloat(account.GrossMarketValue / account.StrategyEquity)
		}
	}
	if at.config.MaxConcurrentPos > 0 {
		snapshot.MaxConcurrentPositions = at.config.MaxConcurrentPos
	}
	if at.config.MaxGrossExposure > 0 {
		snapshot.MaxGrossExposurePct = at.config.MaxGrossExposure
	}
	if at.config.MaxNetExposurePct > 0 {
		snapshot.MaxNetExposurePct = at.config.MaxNetExposurePct
	}
	if at.config.MaxSectorExposurePct > 0 {
		snapshot.MaxSectorExposurePct = at.config.MaxSectorExposurePct
	}
	if at.config.MaxCorrelatedPositions > 0 {
		snapshot.MaxCorrelatedPositions = at.config.MaxCorrelatedPositions
	}
	maxDrawdownPct := at.config.MaxDrawdown
	if maxDrawdownPct > 1 {
		maxDrawdownPct = maxDrawdownPct / 100.0
	}
	snapshot.MaxDrawdownPct = maxDrawdownPct
	if portfolioRisk != nil {
		snapshot.PortfolioRiskAvailable = true
		snapshot.PortfolioRiskSummary = portfolioRisk.Summary
		if snapshot.CurrentGrossExposurePct <= 0 {
			snapshot.CurrentGrossExposurePct = portfolioRisk.Metrics.CurrentGrossExposurePct
		}
		snapshot.CurrentNetExposurePct = portfolioRisk.Metrics.CurrentNetExposurePct
		snapshot.LargestSector = portfolioRisk.Metrics.LargestSector
		snapshot.LargestSectorExposurePct = portfolioRisk.Metrics.LargestSectorExposurePct
		snapshot.CorrelatedPositionCount = portfolioRisk.Metrics.CorrelatedPositionCount
		snapshot.CurrentDrawdownPct = portfolioRisk.Metrics.CurrentDrawdownPct
	}
	if snapshot.CurrentDrawdownPct <= 0 && at.peakEquitySeen > 0 && account != nil && account.StrategyEquity > 0 && account.StrategyEquity < at.peakEquitySeen {
		snapshot.CurrentDrawdownPct = sanitizeFloat((at.peakEquitySeen - account.StrategyEquity) / at.peakEquitySeen)
	}

	state := supervisor.Evaluate(snapshot)
	at.setRiskSupervisorState(state)
	return state
}

func (at *AutoTrader) currentTradingGateDecision(probeStrictLive bool, account *AccountSummary) tradingGateDecision {
	state := at.evaluateRiskSupervisor(account, probeStrictLive)
	if !at.isRunning {
		return tradingGateDecision{
			Mode:            state.Mode,
			TradingAllowed:  false,
			EntriesAllowed:  false,
			ExitsAllowed:    false,
			ReduceOnly:      false,
			BlockReason:     "trader loop is not running",
			BlockingReasons: []string{"trader loop is not running"},
			Message:         "trading blocked: trader loop is not running",
		}
	}
	readiness := at.getReadinessSummary()
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
		blockReason := firstNonEmpty(strings.TrimSpace(readiness.Message), "startup readiness is blocking trading")
		if len(reasons) > 0 {
			blockReason = reasons[0]
		} else {
			reasons = []string{blockReason}
		}
		return tradingGateDecision{
			Mode:            risk.SupervisorModeHalted,
			TradingAllowed:  false,
			EntriesAllowed:  false,
			ExitsAllowed:    false,
			ReduceOnly:      false,
			BlockReason:     blockReason,
			BlockingReasons: reasons,
			Message:         fmt.Sprintf("trading blocked: %s", blockReason),
		}
	}
	restartRecovery := at.currentRestartRecoverySummary()
	if restartRecovery.TradingBlocked {
		blockReason := strings.TrimSpace(restartRecovery.Message)
		if blockReason == "" {
			blockReason = firstNonEmpty(restartRecovery.LastLoadError, restartRecovery.LastSaveError, "durable runtime state recovery is blocking trading")
		}
		return tradingGateDecision{
			Mode:            state.Mode,
			TradingAllowed:  false,
			EntriesAllowed:  false,
			ExitsAllowed:    false,
			ReduceOnly:      false,
			BlockReason:     blockReason,
			BlockingReasons: []string{blockReason},
			Message:         fmt.Sprintf("trading blocked: %s", blockReason),
		}
	}
	brokerTruth := at.currentBrokerTruthSummary()
	if brokerTruth.TradingBlocked {
		blockReason := strings.TrimSpace(brokerTruth.Message)
		if blockReason == "" {
			blockReason = "broker truth is not verified for the active mode"
		}
		reasons := append([]string(nil), brokerTruth.BlockingReasons...)
		if len(reasons) == 0 {
			reasons = []string{blockReason}
		}
		return tradingGateDecision{
			Mode:            risk.SupervisorModeHalted,
			TradingAllowed:  false,
			EntriesAllowed:  false,
			ExitsAllowed:    false,
			ReduceOnly:      false,
			BlockReason:     blockReason,
			BlockingReasons: reasons,
			Message:         fmt.Sprintf("trading blocked: %s", blockReason),
		}
	}
	if brokerTruth.EntriesRestricted {
		blockReason := strings.TrimSpace(brokerTruth.RestrictionReason)
		if blockReason == "" {
			blockReason = "broker truth confidence is degraded by reconciliation-inferred execution outcomes"
		}
		reasons := make([]string, 0, 2)
		reasons = append(reasons, blockReason)
		if reason := strings.TrimSpace(brokerTruth.PrimaryReason); reason != "" && !strings.EqualFold(reason, blockReason) {
			reasons = append(reasons, reason)
		}
		return tradingGateDecision{
			Mode:            risk.SupervisorModeReduceOnly,
			TradingAllowed:  true,
			EntriesAllowed:  false,
			ExitsAllowed:    true,
			ReduceOnly:      true,
			BlockReason:     blockReason,
			BlockingReasons: reasons,
			Message:         fmt.Sprintf("trading restricted: %s", blockReason),
		}
	}

	reasons := make([]string, 0, len(state.Incidents))
	for _, incident := range state.Incidents {
		if summary := at.formatRiskSupervisorBlockingReason(incident); summary != "" {
			reasons = append(reasons, summary)
		}
	}

	switch state.Mode {
	case risk.SupervisorModeAllow:
		return tradingGateDecision{
			Mode:            state.Mode,
			TradingAllowed:  true,
			EntriesAllowed:  true,
			ExitsAllowed:    true,
			ReduceOnly:      false,
			BlockingReasons: []string{},
			Message:         "trading allowed",
		}
	case risk.SupervisorModeReduceOnly, risk.SupervisorModeBlockNewEntries:
		blockReason := at.primaryRiskSupervisorReason(state)
		message := fmt.Sprintf("trading restricted: %s", state.Mode)
		if state.Summary != "" {
			message = fmt.Sprintf("trading restricted: %s", state.Summary)
		}
		return tradingGateDecision{
			Mode:            state.Mode,
			TradingAllowed:  true,
			EntriesAllowed:  false,
			ExitsAllowed:    true,
			ReduceOnly:      state.Mode == risk.SupervisorModeReduceOnly,
			BlockReason:     blockReason,
			BlockingReasons: reasons,
			Message:         message,
		}
	default:
		blockReason := at.primaryRiskSupervisorReason(state)
		if blockReason == "" {
			blockReason = "risk supervisor halted trading"
		}
		if len(reasons) == 0 {
			reasons = []string{blockReason}
		}
		return tradingGateDecision{
			Mode:            state.Mode,
			TradingAllowed:  false,
			EntriesAllowed:  false,
			ExitsAllowed:    false,
			ReduceOnly:      false,
			BlockReason:     blockReason,
			BlockingReasons: reasons,
			Message:         fmt.Sprintf("trading blocked: %s", blockReason),
		}
	}
}

func (at *AutoTrader) primaryRiskSupervisorReason(state risk.SupervisorState) string {
	if len(state.Incidents) == 0 {
		return ""
	}
	incident := state.Incidents[0]
	switch incident.Type {
	case risk.IncidentStartupReadinessFailed:
		return "startup readiness failed"
	case risk.IncidentBrokerRuntimeUnhealthy:
		return "broker runtime degraded"
	case risk.IncidentLivePromotionFailed:
		return "live promotion checklist failed"
	case risk.IncidentPositionMismatchDetected:
		return "position reconciliation blocked"
	case risk.IncidentKillSwitchActive:
		return "emergency kill switch active"
	case risk.IncidentMaxDailyLossBreached:
		return "daily loss limit breached"
	case risk.IncidentExcessiveDrawdownDetected:
		return "risk supervisor reduce_only"
	case risk.IncidentRepeatedBrokerDegradation, risk.IncidentRepeatedReconciliationFailure, risk.IncidentExcessiveOrderRejects,
		risk.IncidentMaxGrossExposureBreached, risk.IncidentMaxNetExposureBreached, risk.IncidentMaxConcurrentPositionsBreached,
		risk.IncidentMaxSectorExposureBreached, risk.IncidentMaxCorrelatedPositionsBreached:
		return fmt.Sprintf("risk supervisor %s", state.Mode)
	default:
		if summary := strings.TrimSpace(incident.Summary); summary != "" {
			return summary
		}
		return fmt.Sprintf("risk supervisor %s", state.Mode)
	}
}

func (at *AutoTrader) formatRiskSupervisorBlockingReason(incident risk.Incident) string {
	summary := strings.TrimSpace(incident.Summary)
	switch incident.Type {
	case risk.IncidentStartupReadinessFailed:
		if summary == "" {
			return "startup readiness failed"
		}
		return "startup readiness failed: " + summary
	case risk.IncidentBrokerRuntimeUnhealthy:
		if summary == "" {
			return "broker runtime degraded"
		}
		return "broker runtime degraded: " + summary
	case risk.IncidentLivePromotionFailed:
		if summary == "" {
			return "live promotion checklist failed"
		}
		return "live promotion checklist failed: " + summary
	case risk.IncidentPositionMismatchDetected:
		if summary == "" {
			return "position reconciliation blocked"
		}
		return "position reconciliation blocked: " + summary
	case risk.IncidentKillSwitchActive:
		if summary == "" {
			return "emergency kill switch active"
		}
		return "emergency kill switch active: " + summary
	default:
		return summary
	}
}

func classifyDecisionRiskEffect(d *decision.Decision) string {
	if d == nil {
		return "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(d.Action)) {
	case "hold", "wait":
		return "neutral"
	case "open_long", "open_short":
		return "entry"
	case "close_long", "close_short":
		return "exit"
	default:
		return "unknown"
	}
}

func (at *AutoTrader) ensureDecisionAllowedByGate(d *decision.Decision, gate tradingGateDecision) error {
	effect := classifyDecisionRiskEffect(d)
	switch effect {
	case "neutral":
		return nil
	case "entry":
		if gate.EntriesAllowed {
			return nil
		}
		return fmt.Errorf("%s blocked %s %s: %s", gate.Mode, d.Symbol, d.Action, gate.BlockReason)
	case "exit":
		if gate.ExitsAllowed {
			return nil
		}
		return fmt.Errorf("%s blocked %s %s: %s", gate.Mode, d.Symbol, d.Action, gate.BlockReason)
	default:
		if gate.Mode != risk.SupervisorModeAllow {
			return fmt.Errorf("%s blocked uncertain action %s for %s", gate.Mode, strings.TrimSpace(d.Action), strings.TrimSpace(d.Symbol))
		}
		return nil
	}
}

func errorLooksLikeOrderReject(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "reject") || strings.Contains(message, "rejected")
}
