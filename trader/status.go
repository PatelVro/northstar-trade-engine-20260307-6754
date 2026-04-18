// Package trader - status.go
// AutoTrader status accessors. GetStatus aggregates the runtime-facing view
// (broker state, readiness, execution summary, universe, risk supervisor,
// shadow mode, kill switch, news, portfolio risk) into a single map for the
// HTTP/dashboard layer. Pulled out of auto_trader.go to keep the lifecycle
// file focused on orchestration. All methods remain on *AutoTrader.
package trader

import (
	"northstar/alerts"
	"northstar/logger"
	"northstar/market"
	"time"
)

func (at *AutoTrader) GetID() string {
	return at.id
}

func (at *AutoTrader) GetName() string {
	return at.name
}

func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

func (at *AutoTrader) GetDecisionLogger() *logger.DecisionLogger {
	return at.decisionLogger
}

func (at *AutoTrader) GetStatus() map[string]interface{} {
	brokerStatus := at.brokerRuntimeStatus()
	readiness := at.getReadinessSummary()
	killSwitch := at.currentKillSwitchSummary()
	lastSessionReportPath := at.lastSessionReportPath
	lastSessionReportStatus := at.lastSessionReportStatus
	lastSessionReportAt := ""
	if !at.lastSessionReportAt.IsZero() {
		lastSessionReportAt = at.lastSessionReportAt.Format(time.RFC3339)
	}
	brokerStateSince := ""
	if !brokerStatus.Since.IsZero() {
		brokerStateSince = brokerStatus.Since.Format(time.RFC3339)
	}
	brokerLastHealthyAt := ""
	if !brokerStatus.LastHealthyAt.IsZero() {
		brokerLastHealthyAt = brokerStatus.LastHealthyAt.Format(time.RFC3339)
	}
	brokerLastReconciledAt := ""
	if !brokerStatus.LastReconciledAt.IsZero() {
		brokerLastReconciledAt = brokerStatus.LastReconciledAt.Format(time.RFC3339)
	}
	brokerNextRetryAt := ""
	if !brokerStatus.NextRetryAt.IsZero() {
		brokerNextRetryAt = brokerStatus.NextRetryAt.Format(time.RFC3339)
	}
	readinessCheckedAt := ""
	if !readiness.CheckedAt.IsZero() {
		readinessCheckedAt = readiness.CheckedAt.Format(time.RFC3339)
	}
	portfolioRisk := at.currentPortfolioRiskState()
	alertSummary := at.currentAlertsSummary()
	executionSummary := at.currentExecutionSummary()
	protectionSummary := at.currentProtectionSummary()
	brokerTruth := at.currentBrokerTruthSummary()
	brokerTradingAllowed := !at.managesIBKRBrokerRuntime() || brokerStatus.State == BrokerRuntimeHealthy
	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	riskSupervisorState := at.currentRiskSupervisorState()
	if riskSupervisorState.EvaluatedAt.IsZero() {
		riskSupervisorState = at.evaluateRiskSupervisor(at.currentLatestAccountSummary(), false)
		gate = at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	}

	aiProvider := "DeepSeek"
	if at.demoMode {
		aiProvider = "Demo"
	} else if at.aiModel == "custom" {
		aiProvider = "Custom"
	} else if at.config.UseQwen || at.aiModel == "qwen" {
		aiProvider = "Qwen"
	}
	demoLastCycleTime := ""
	if !at.demoLastCycleTime.IsZero() {
		demoLastCycleTime = at.demoLastCycleTime.Format(time.RFC3339)
	}
	lastNewsRefresh := ""
	if !at.lastNewsRefresh.IsZero() {
		lastNewsRefresh = at.lastNewsRefresh.Format(time.RFC3339)
	}
	portfolioRiskLastEvaluatedAt := ""
	portfolioRiskOutcome := ""
	portfolioRiskSummary := ""
	portfolioGrossExposurePct := 0.0
	portfolioNetExposurePct := 0.0
	portfolioLargestSector := ""
	portfolioLargestSectorPct := 0.0
	portfolioCorrelatedPositions := 0
	portfolioMaxCorrelation := 0.0
	portfolioCurrentDrawdownPct := 0.0
	var portfolioRiskMetrics interface{}
	if portfolioRisk != nil {
		portfolioRiskLastEvaluatedAt = portfolioRisk.EvaluatedAt.Format(time.RFC3339)
		portfolioRiskOutcome = string(portfolioRisk.Outcome)
		portfolioRiskSummary = portfolioRisk.Summary
		portfolioGrossExposurePct = portfolioRisk.Metrics.CurrentGrossExposurePct
		portfolioNetExposurePct = portfolioRisk.Metrics.CurrentNetExposurePct
		portfolioLargestSector = portfolioRisk.Metrics.LargestSector
		portfolioLargestSectorPct = portfolioRisk.Metrics.LargestSectorExposurePct
		portfolioCorrelatedPositions = portfolioRisk.Metrics.CorrelatedPositionCount
		portfolioMaxCorrelation = portfolioRisk.Metrics.MaxObservedCorrelation
		portfolioCurrentDrawdownPct = portfolioRisk.Metrics.CurrentDrawdownPct
		portfolioRiskMetrics = portfolioRisk.Metrics.Clone()
	}
	var recentAlerts interface{}
	if len(alertSummary.Recent) > 0 {
		recentAlerts = append([]alerts.Alert(nil), alertSummary.Recent...)
	}
	universeSummary := at.currentUniverseSummary()
	universePreview, universePreviewTruncated := previewUniverseSymbols(universeSummary.EffectiveSymbols)
	shadowSummary := at.currentShadowSummary()

	return map[string]interface{}{
		"trader_id":                 at.id,
		"trader_name":               at.name,
		"ai_model":                  at.aiModel,
		"exchange":                  at.exchange,
		"is_running":                at.isRunning,
		"start_time":                at.startTime.Format(time.RFC3339),
		"runtime_minutes":           int(time.Since(at.startTime).Minutes()),
		"broker_state":              brokerStatus.State,
		"broker_state_reason":       brokerStatus.Reason,
		"broker_last_error":         brokerStatus.LastError,
		"broker_state_since":        brokerStateSince,
		"broker_last_healthy_at":    brokerLastHealthyAt,
		"broker_last_reconciled_at": brokerLastReconciledAt,
		"broker_reconnect_attempts": brokerStatus.ReconnectAttempts,
		"broker_next_retry_at":      brokerNextRetryAt,
		"broker_recovery_active":    brokerStatus.RecoveryActive,
		"broker_trading_allowed":    brokerTradingAllowed,
		"broker_truth": map[string]interface{}{
			"available":              brokerTruth.Available,
			"required":               brokerTruth.Required,
			"broker_managed":         brokerTruth.BrokerManaged,
			"verified":               brokerTruth.Verified,
			"trading_blocked":        brokerTruth.TradingBlocked,
			"account_required":       brokerTruth.AccountRequired,
			"account_verified":       brokerTruth.AccountVerified,
			"orders_required":        brokerTruth.OrdersRequired,
			"orders_verified":        brokerTruth.OrdersVerified,
			"positions_required":     brokerTruth.PositionsRequired,
			"positions_verified":     brokerTruth.PositionsVerified,
			"market_data_required":   brokerTruth.MarketDataRequired,
			"market_data_verified":   brokerTruth.MarketDataVerified,
			"account_captured_at":    formatRFC3339(brokerTruth.AccountCapturedAt),
			"orders_checked_at":      formatRFC3339(brokerTruth.OrdersCheckedAt),
			"positions_checked_at":   formatRFC3339(brokerTruth.PositionsCheckedAt),
			"market_data_checked_at": formatRFC3339(brokerTruth.MarketDataCheckedAt),
			"message":                brokerTruth.Message,
			"blocking_reasons":       append([]string(nil), brokerTruth.BlockingReasons...),
		},
		"broker_truth_available":             brokerTruth.Available,
		"broker_truth_required":              brokerTruth.Required,
		"broker_truth_verified":              brokerTruth.Verified,
		"broker_truth_trading_blocked":       brokerTruth.TradingBlocked,
		"broker_truth_message":               brokerTruth.Message,
		"readiness_status":                   readiness.Status,
		"readiness_message":                  readiness.Message,
		"readiness_checked_at":               readinessCheckedAt,
		"readiness_trading_allowed":          readiness.TradingAllowed,
		"readiness_pass_count":               readiness.PassCount,
		"readiness_warn_count":               readiness.WarnCount,
		"readiness_fail_count":               readiness.FailCount,
		"readiness_checks":                   readiness.Checks,
		"trading_allowed":                    gate.TradingAllowed,
		"entries_allowed":                    gate.EntriesAllowed,
		"exits_allowed":                      gate.ExitsAllowed,
		"reduce_only":                        gate.ReduceOnly,
		"trading_block_reason":               gate.BlockReason,
		"blocking_reasons":                   gate.BlockingReasons,
		"risk_supervisor_mode":               riskSupervisorState.Mode,
		"risk_supervisor_summary":            riskSupervisorState.Summary,
		"risk_supervisor_active_incidents":   riskSupervisorState.ActiveIncidentCount,
		"risk_supervisor_critical_incidents": riskSupervisorState.CriticalIncidentCount,
		"risk_supervisor_incidents":          riskSupervisorState.Incidents,
		"execution": map[string]interface{}{
			"available":                  executionSummary.Available,
			"in_flight_count":            executionSummary.InFlightCount,
			"stale_count":                executionSummary.StaleCount,
			"last_execution_at":          formatRFC3339(executionSummary.LastExecutionAt),
			"last_execution_symbol":      executionSummary.LastExecutionSymbol,
			"last_execution_status":      executionSummary.LastExecutionStatus,
			"duplicate_suppressed_count": executionSummary.DuplicateSuppressedCount,
			"blocked_execution_count":    executionSummary.BlockedExecutionCount,
			"submitted_count":            executionSummary.SubmittedCount,
			"acknowledged_count":         executionSummary.AcknowledgedCount,
			"filled_count":               executionSummary.FilledCount,
			"rejected_count":             executionSummary.RejectedCount,
			"failed_count":               executionSummary.FailedCount,
		},
		"universe": map[string]interface{}{
			"available":                   universeSummary.Available,
			"instrument_type":             universeSummary.InstrumentType,
			"selection_mode":              universeSummary.SelectionMode,
			"configured_source":           universeSummary.ConfiguredSource,
			"configured_symbols_count":    len(universeSummary.ConfiguredSymbols),
			"effective_symbols_count":     len(universeSummary.EffectiveSymbols),
			"trusted_symbols_file":        universeSummary.TrustedSymbolsFile,
			"trusted_symbols_count":       universeSummary.TrustedSymbolsCount,
			"benchmark_symbols":           append([]string(nil), universeSummary.BenchmarkSymbols...),
			"manifest_path":               universeSummary.ManifestPath,
			"manifest_persisted":          universeSummary.ManifestPersisted,
			"manifest_last_error":         universeSummary.ManifestLastError,
			"last_updated_at":             formatRFC3339(universeSummary.LastUpdatedAt),
			"effective_symbols_preview":   universePreview,
			"preview_truncated":           universePreviewTruncated,
			"last_candidate_window":       append([]string(nil), universeSummary.LastCandidateWindow...),
			"last_mandatory_symbols":      append([]string(nil), universeSummary.LastMandatory...),
			"last_market_data_load_order": append([]string(nil), universeSummary.LastLoadOrder...),
			"message":                     universeSummary.Message,
		},
		"execution_available":                  executionSummary.Available,
		"execution_in_flight_count":            executionSummary.InFlightCount,
		"execution_stale_count":                executionSummary.StaleCount,
		"execution_last_execution_at":          formatRFC3339(executionSummary.LastExecutionAt),
		"execution_last_execution_symbol":      executionSummary.LastExecutionSymbol,
		"execution_last_execution_status":      executionSummary.LastExecutionStatus,
		"execution_duplicate_suppressed_count": executionSummary.DuplicateSuppressedCount,
		"execution_blocked_count":              executionSummary.BlockedExecutionCount,
		"execution_submitted_count":            executionSummary.SubmittedCount,
		"execution_acknowledged_count":         executionSummary.AcknowledgedCount,
		"execution_filled_count":               executionSummary.FilledCount,
		"execution_rejected_count":             executionSummary.RejectedCount,
		"execution_failed_count":               executionSummary.FailedCount,
		"universe_selection_mode":              universeSummary.SelectionMode,
		"universe_configured_source":           universeSummary.ConfiguredSource,
		"universe_configured_count":            len(universeSummary.ConfiguredSymbols),
		"universe_effective_count":             len(universeSummary.EffectiveSymbols),
		"universe_manifest_path":               universeSummary.ManifestPath,
		"universe_message":                     universeSummary.Message,
		"protection": map[string]interface{}{
			"available":               protectionSummary.Available,
			"pending_count":           protectionSummary.PendingCount,
			"active_protective_count": protectionSummary.ActiveProtectiveCount,
			"last_updated_at":         formatRFC3339(protectionSummary.LastUpdatedAt),
			"message":                 protectionSummary.Message,
			"pending":                 protectionSummary.Pending,
		},
		"protection_pending_count":           protectionSummary.PendingCount,
		"protection_active_protective_count": protectionSummary.ActiveProtectiveCount,
		"protection_message":                 protectionSummary.Message,
		"shadow": map[string]interface{}{
			"available":                   shadowSummary.Available,
			"active":                      shadowSummary.Active,
			"last_decision_at":            formatRFC3339(shadowSummary.LastDecisionAt),
			"last_decision_symbol":        shadowSummary.LastDecisionSymbol,
			"last_decision_action":        shadowSummary.LastDecisionAction,
			"last_decision_status":        shadowSummary.LastDecisionStatus,
			"decision_count":              shadowSummary.TotalDecisions,
			"would_trade_count":           shadowSummary.WouldTradeCount,
			"blocked_count":               shadowSummary.BlockedCount,
			"open_positions":              shadowSummary.OpenPositions,
			"closed_trades":               shadowSummary.ClosedTrades,
			"hypothetical_realized_pnl":   shadowSummary.HypotheticalRealizedPnL,
			"hypothetical_unrealized_pnl": shadowSummary.HypotheticalUnrealizedPnL,
			"last_block_reason":           shadowSummary.LastBlockReason,
		},
		"shadow_mode_active":                    shadowSummary.Active,
		"shadow_decision_count":                 shadowSummary.TotalDecisions,
		"shadow_would_trade_count":              shadowSummary.WouldTradeCount,
		"shadow_blocked_count":                  shadowSummary.BlockedCount,
		"shadow_realized_pnl":                   shadowSummary.HypotheticalRealizedPnL,
		"shadow_unrealized_pnl":                 shadowSummary.HypotheticalUnrealizedPnL,
		"kill_switch_active":                    killSwitch.Active,
		"kill_switch_source":                    killSwitch.Source,
		"kill_switch_message":                   killSwitch.Message,
		"kill_switch_file_path":                 killSwitch.FilePath,
		"kill_switch_triggered_at":              formatRFC3339(killSwitch.TriggeredAt),
		"kill_switch_last_checked_at":           formatRFC3339(killSwitch.LastCheckedAt),
		"kill_switch_last_cleared_at":           formatRFC3339(killSwitch.LastClearedAt),
		"kill_switch_orders_cancelled":          killSwitch.OrdersCancelled,
		"kill_switch_last_cancel_attempt_at":    formatRFC3339(killSwitch.LastCancelAttemptAt),
		"kill_switch_last_cancel_error":         killSwitch.LastCancelError,
		"kill_switch_activation_count":          killSwitch.ActivationCount,
		"last_session_report_path":              lastSessionReportPath,
		"last_session_report_status":            lastSessionReportStatus,
		"last_session_report_at":                lastSessionReportAt,
		"call_count":                            at.callCount,
		"initial_balance":                       at.initialBalance,
		"scan_interval":                         at.config.ScanInterval.String(),
		"max_cycles":                            at.config.MaxCycles,
		"replay_warmup_bars":                    at.config.ReplayWarmupBars,
		"stop_until":                            at.stopUntil.Format(time.RFC3339),
		"last_reset_time":                       at.lastResetTime.Format(time.RFC3339),
		"ai_provider":                           aiProvider,
		"mode":                                  at.config.Mode,
		"strategy_mode":                         at.config.StrategyMode,
		"max_gross_exposure":                    at.config.MaxGrossExposure,
		"max_position_pct":                      at.config.MaxPositionPct,
		"max_concurrent_positions":              at.config.MaxConcurrentPos,
		"risk_per_trade_pct":                    at.config.RiskPerTradePct,
		"min_factor_score":                      at.config.MinFactorScore,
		"max_pair_correlation":                  at.config.MaxPairCorrelation,
		"min_liquidity_usd":                     at.config.MinLiquidityUSD,
		"min_decision_confidence":               at.config.MinDecisionConfidence,
		"regime_risk_scaling":                   at.config.RegimeRiskScaling,
		"execution_commission_bps":              at.config.ExecutionCommissionBps,
		"execution_spread_bps":                  at.config.ExecutionSpreadBps,
		"execution_slippage_bps":                at.config.ExecutionSlippageBps,
		"execution_impact_bps":                  at.config.ExecutionImpactBps,
		"max_participation_rate":                at.config.MaxParticipationRate,
		"drawdown_throttle_start":               at.config.DrawdownThrottleStartPct,
		"drawdown_throttle_min_scale":           at.config.DrawdownThrottleMinScale,
		"max_portfolio_heat_pct":                at.config.MaxPortfolioHeatPct,
		"max_net_exposure_pct":                  at.config.MaxNetExposurePct,
		"max_sector_exposure_pct":               at.config.MaxSectorExposurePct,
		"max_correlated_positions":              at.config.MaxCorrelatedPositions,
		"loss_streak_pause_threshold":           at.config.LossStreakPauseThreshold,
		"loss_streak_pause_cycles":              at.config.LossStreakPauseCycles,
		"performance_risk_lookback":             at.config.PerformanceRiskLookback,
		"volatility_brake_target_pct":           at.config.VolatilityBrakeTargetPct,
		"volatility_brake_lookback":             at.config.VolatilityBrakeLookback,
		"volatility_brake_min_scale":            at.config.VolatilityBrakeMinScale,
		"kelly_fraction_cap":                    at.config.KellyFractionCap,
		"kelly_lookback":                        at.config.KellyLookback,
		"kelly_min_trades":                      at.config.KellyMinTrades,
		"market_stress_entry_block":             at.config.MarketStressEntryBlock,
		"market_stress_risk_min_scale":          at.config.MarketStressRiskMinScale,
		"use_news_risk":                         at.config.UseNewsRisk,
		"enable_news_in_replay":                 at.config.EnableNewsInReplay,
		"news_provider":                         at.config.NewsProvider,
		"news_lookback_minutes":                 at.config.NewsLookbackMinutes,
		"news_refresh_seconds":                  at.config.NewsRefreshSeconds,
		"news_market_impact_thresh":             at.config.NewsMarketImpactThresh,
		"news_symbol_impact_thresh":             at.config.NewsSymbolImpactThresh,
		"news_hard_block_thresh":                at.config.NewsHardBlockThresh,
		"news_max_risk_reduction":               at.config.NewsMaxRiskReduction,
		"realized_equity_vol_pct":               at.realizedEquityVolPct() * 100.0,
		"latest_market_stress":                  at.latestMarketStress,
		"latest_stress_dispersion":              at.latestStressDispersion,
		"latest_stress_correlation":             at.latestStressCorrelation,
		"latest_kelly_scale":                    at.latestKellyScale,
		"latest_news_sentiment":                 at.latestNewsSentiment,
		"latest_news_impact":                    at.latestNewsImpact,
		"latest_news_scale":                     at.latestNewsScale,
		"news_credibility_global":               at.newsCredibilityGlobal,
		"news_credibility_symbols":              len(at.newsCredibility),
		"last_news_learn_symbol":                at.lastNewsLearnSymbol,
		"last_news_learn_delta":                 at.lastNewsLearnDelta,
		"last_news_refresh":                     lastNewsRefresh,
		"news_last_error":                       at.newsLastError,
		"entry_blocked_until_cycle":             at.openEntryBlockedUntil,
		"consecutive_loss_closes":               at.consecutiveLossCloses,
		"close_pnl_ema_pct":                     at.closePnLEMA,
		"learned_symbol_count":                  len(at.symbolTradeCount),
		"is_demo_mode":                          at.demoMode,
		"demo_last_cycle_time":                  demoLastCycleTime,
		"portfolio_risk_available":              portfolioRisk != nil,
		"portfolio_risk_last_evaluated_at":      portfolioRiskLastEvaluatedAt,
		"portfolio_risk_outcome":                portfolioRiskOutcome,
		"portfolio_risk_summary":                portfolioRiskSummary,
		"portfolio_gross_exposure_pct":          portfolioGrossExposurePct,
		"portfolio_net_exposure_pct":            portfolioNetExposurePct,
		"portfolio_largest_sector":              portfolioLargestSector,
		"portfolio_largest_sector_exposure_pct": portfolioLargestSectorPct,
		"portfolio_correlated_positions":        portfolioCorrelatedPositions,
		"portfolio_max_observed_correlation":    portfolioMaxCorrelation,
		"portfolio_current_drawdown_pct":        portfolioCurrentDrawdownPct,
		"portfolio_risk_metrics":                portfolioRiskMetrics,
		"recent_alerts":                         recentAlerts,
		"alert_count":                           alertSummary.TotalCount,
		"critical_alert_count":                  alertSummary.CriticalCount,
		"warning_alert_count":                   alertSummary.WarningCount,
		"info_alert_count":                      alertSummary.InfoCount,
		"last_alert_at":                         alertSummary.LastAlertAt,
	}
}

// GetProvider returns the underlying BarsProvider.
func (at *AutoTrader) GetProvider() market.BarsProvider {
	return at.provider
}

// GetAccountInfo returns canonical broker-account and strategy-performance metrics.
func (at *AutoTrader) GetAccountInfo() (*AccountSummary, error) {
	summary, _, err := at.snapshotAccountAndPositions()
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// GetPositions returns the canonical runtime view of open broker positions.
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	_, positions, err := at.snapshotAccountAndPositions()
	if err != nil {
		return nil, err
	}
	return positions, nil
}
