package trader

import (
	"northstar/alerts"
	"northstar/execution"
	"northstar/incidents"
	"northstar/logger"
	"northstar/orders"
	"northstar/positions"
	"northstar/risk"
	"northstar/startup"
	"strings"
	"time"
)

const operatorStatusSchemaVersion = 20

type OperatorRuntimeSummary struct {
	IsRunning         bool    `json:"is_running"`
	StartTime         string  `json:"start_time"`
	RuntimeMinutes    int     `json:"runtime_minutes"`
	CallCount         int     `json:"call_count"`
	InitialBalance    float64 `json:"initial_balance"`
	ScanInterval      string  `json:"scan_interval"`
	StopUntil         string  `json:"stop_until"`
	LastResetTime     string  `json:"last_reset_time"`
	AIProvider        string  `json:"ai_provider"`
	AIModel           string  `json:"ai_model"`
	IsDemoMode        bool    `json:"is_demo_mode"`
	DemoLastCycleTime string  `json:"demo_last_cycle_time"`
}

type OperatorReadinessSummary struct {
	Status         ReadinessStatus  `json:"status"`
	Message        string           `json:"message"`
	CheckedAt      string           `json:"checked_at"`
	TradingAllowed bool             `json:"trading_allowed"`
	PassCount      int              `json:"pass_count"`
	WarnCount      int              `json:"warn_count"`
	FailCount      int              `json:"fail_count"`
	Checks         []ReadinessCheck `json:"checks"`
}

type OperatorBrokerRuntimeSummary struct {
	Managed           bool               `json:"managed"`
	State             BrokerRuntimeState `json:"state"`
	Reason            string             `json:"reason"`
	LastError         string             `json:"last_error"`
	StateSince        string             `json:"state_since"`
	LastHealthyAt     string             `json:"last_healthy_at"`
	LastReconciledAt  string             `json:"last_reconciled_at"`
	ReconnectAttempts int                `json:"reconnect_attempts"`
	NextRetryAt       string             `json:"next_retry_at"`
	RecoveryActive    bool               `json:"recovery_active"`
	TradingAllowed    bool               `json:"trading_allowed"`
}

type OperatorSessionSummary struct {
	LastSessionReportPath   string `json:"last_session_report_path"`
	LastSessionReportStatus string `json:"last_session_report_status"`
	LastSessionReportAt     string `json:"last_session_report_at"`
}

type OperatorEventJournalSummary struct {
	Available     bool   `json:"available"`
	Path          string `json:"path,omitempty"`
	EventCount    int    `json:"event_count"`
	LastEventAt   string `json:"last_event_at"`
	LastEventType string `json:"last_event_type,omitempty"`
	LastSeverity  string `json:"last_severity,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type OperatorExecutionSummary struct {
	Available                bool             `json:"available"`
	InFlightCount            int              `json:"in_flight_count"`
	StaleCount               int              `json:"stale_count"`
	LastExecutionAt          string           `json:"last_execution_at"`
	LastExecutionSymbol      string           `json:"last_execution_symbol"`
	LastExecutionStatus      execution.Status `json:"last_execution_status"`
	DuplicateSuppressedCount int              `json:"duplicate_suppressed_count"`
	BlockedExecutionCount    int              `json:"blocked_execution_count"`
	SubmittedCount           int              `json:"submitted_count"`
	AcknowledgedCount        int              `json:"acknowledged_count"`
	FilledCount              int              `json:"filled_count"`
	RejectedCount            int              `json:"rejected_count"`
	FailedCount              int              `json:"failed_count"`
}

type OperatorPendingProtectionItem struct {
	Symbol                string  `json:"symbol"`
	PositionSide          string  `json:"position_side"`
	EntryStatus           string  `json:"entry_status"`
	Status                string  `json:"status"`
	RequestedQuantity     float64 `json:"requested_quantity"`
	ConfirmedQuantity     float64 `json:"confirmed_quantity"`
	StopProtectedQuantity float64 `json:"stop_protected_quantity"`
	TargetProtectedQty    float64 `json:"target_protected_quantity"`
	StopPrice             float64 `json:"stop_price"`
	TakeProfitPrice       float64 `json:"take_profit_price"`
	Message               string  `json:"message"`
	UpdatedAt             string  `json:"updated_at"`
}

type OperatorProtectionSummary struct {
	Available             bool                            `json:"available"`
	PendingCount          int                             `json:"pending_count"`
	ActiveProtectiveCount int                             `json:"active_protective_count"`
	LastUpdatedAt         string                          `json:"last_updated_at"`
	Message               string                          `json:"message"`
	Pending               []OperatorPendingProtectionItem `json:"pending,omitempty"`
}

type OperatorUniverseSummary struct {
	Available               bool     `json:"available"`
	InstrumentType          string   `json:"instrument_type"`
	SelectionMode           string   `json:"selection_mode"`
	ConfiguredSource        string   `json:"configured_source"`
	ConfiguredSymbolsCount  int      `json:"configured_symbols_count"`
	EffectiveSymbolsCount   int      `json:"effective_symbols_count"`
	TrustedSymbolsFile      string   `json:"trusted_symbols_file,omitempty"`
	TrustedSymbolsCount     int      `json:"trusted_symbols_count"`
	BenchmarkSymbols        []string `json:"benchmark_symbols,omitempty"`
	ManifestPath            string   `json:"manifest_path,omitempty"`
	ManifestPersisted       bool     `json:"manifest_persisted"`
	ManifestLastError       string   `json:"manifest_last_error,omitempty"`
	LastUpdatedAt           string   `json:"last_updated_at"`
	EffectiveSymbolsPreview []string `json:"effective_symbols_preview,omitempty"`
	PreviewTruncated        bool     `json:"preview_truncated"`
	LastCandidateWindow     []string `json:"last_candidate_window,omitempty"`
	LastMandatorySymbols    []string `json:"last_mandatory_symbols,omitempty"`
	LastMarketDataLoadOrder []string `json:"last_market_data_load_order,omitempty"`
	Message                 string   `json:"message"`
}

type OperatorShadowModeSummary struct {
	Available                 bool                     `json:"available"`
	Active                    bool                     `json:"active"`
	LastDecisionAt            string                   `json:"last_decision_at"`
	LastDecisionSymbol        string                   `json:"last_decision_symbol"`
	LastDecisionAction        string                   `json:"last_decision_action"`
	LastDecisionStatus        string                   `json:"last_decision_status"`
	DecisionCount             int                      `json:"decision_count"`
	WouldTradeCount           int                      `json:"would_trade_count"`
	BlockedCount              int                      `json:"blocked_count"`
	OpenPositions             int                      `json:"open_positions"`
	ClosedTrades              int                      `json:"closed_trades"`
	HypotheticalRealizedPnL   float64                  `json:"hypothetical_realized_pnl"`
	HypotheticalUnrealizedPnL float64                  `json:"hypothetical_unrealized_pnl"`
	LastBlockReason           string                   `json:"last_block_reason"`
	RecentDecisions           []logger.ShadowExecution `json:"recent_decisions"`
}

type OperatorRestartRecoverySummary struct {
	Available               bool   `json:"available"`
	Status                  string `json:"status"`
	StatePath               string `json:"state_path"`
	StatePresent            bool   `json:"state_present"`
	Restored                bool   `json:"restored"`
	PendingReconciliation   bool   `json:"pending_reconciliation"`
	TradingBlocked          bool   `json:"trading_blocked"`
	Partial                 bool   `json:"partial"`
	Corrupt                 bool   `json:"corrupt"`
	Message                 string `json:"message"`
	SavedAt                 string `json:"saved_at"`
	RestoredAt              string `json:"restored_at"`
	LastPersistedAt         string `json:"last_persisted_at"`
	LastLoadError           string `json:"last_load_error"`
	LastSaveError           string `json:"last_save_error"`
	RestoredExecutionCount  int    `json:"restored_execution_count"`
	RestoredInFlightCount   int    `json:"restored_in_flight_count"`
	RestoredActiveOrders    int    `json:"restored_active_orders"`
	RestoredLocalPositions  int    `json:"restored_local_positions"`
	RestoredPendingProtect  int    `json:"restored_pending_protection"`
	RestoredShadowPositions int    `json:"restored_shadow_positions"`
}

type OperatorBrokerTruthSummary struct {
	Available            bool     `json:"available"`
	Required             bool     `json:"required"`
	BrokerManaged        bool     `json:"broker_managed"`
	Verified             bool     `json:"verified"`
	TradingBlocked       bool     `json:"trading_blocked"`
	EntriesRestricted    bool     `json:"entries_restricted"`
	ConfidenceDegraded   bool     `json:"confidence_degraded"`
	RestrictionReason    string   `json:"restriction_reason"`
	AccountRequired      bool     `json:"account_required"`
	AccountVerified      bool     `json:"account_verified"`
	OrdersRequired       bool     `json:"orders_required"`
	OrdersVerified       bool     `json:"orders_verified"`
	PositionsRequired    bool     `json:"positions_required"`
	PositionsVerified    bool     `json:"positions_verified"`
	MarketDataRequired   bool     `json:"market_data_required"`
	MarketDataVerified   bool     `json:"market_data_verified"`
	AccountCapturedAt    string   `json:"account_captured_at"`
	OrdersCheckedAt      string   `json:"orders_checked_at"`
	PositionsCheckedAt   string   `json:"positions_checked_at"`
	MarketDataCheckedAt  string   `json:"market_data_checked_at"`
	InferredOrderCount   int      `json:"inferred_order_count"`
	UnresolvedOrderCount int      `json:"unresolved_order_count"`
	PrimaryIssueLocalID  string   `json:"primary_issue_local_order_id,omitempty"`
	PrimaryIssueBrokerID string   `json:"primary_issue_broker_order_id,omitempty"`
	PrimaryAuthority     string   `json:"primary_issue_authority,omitempty"`
	PrimaryConfidence    string   `json:"primary_issue_confidence,omitempty"`
	PrimaryReason        string   `json:"primary_issue_reason,omitempty"`
	PrimaryNeedsReview   bool     `json:"primary_issue_needs_review"`
	Message              string   `json:"message"`
	BlockingReasons      []string `json:"blocking_reasons"`
}

type OperatorDeploymentValidationSummary struct {
	Required            bool   `json:"required"`
	Passed              bool   `json:"passed"`
	Fresh               bool   `json:"fresh"`
	ConfigMatches       bool   `json:"config_matches"`
	ActiveConfigFile    string `json:"active_config_file"`
	ValidatedConfigFile string `json:"validated_config_file"`
	CheckedAt           string `json:"checked_at"`
	Source              string `json:"source"`
	Message             string `json:"message"`
}

type OperatorPromotionSummary struct {
	Status             PromotionStatus  `json:"status"`
	Message            string           `json:"message"`
	CheckedAt          string           `json:"checked_at"`
	Required           bool             `json:"required"`
	LiveTradingAllowed bool             `json:"live_trading_allowed"`
	PassCount          int              `json:"pass_count"`
	WarnCount          int              `json:"warn_count"`
	FailCount          int              `json:"fail_count"`
	Checks             []PromotionCheck `json:"checks"`
}

type OperatorRiskSupervisorSummary struct {
	Mode                  risk.SupervisorMode `json:"mode"`
	TradingAllowed        bool                `json:"trading_allowed"`
	EntriesAllowed        bool                `json:"entries_allowed"`
	ExitsAllowed          bool                `json:"exits_allowed"`
	ReduceOnly            bool                `json:"reduce_only"`
	Summary               string              `json:"summary"`
	ActiveIncidentCount   int                 `json:"active_incident_count"`
	CriticalIncidentCount int                 `json:"critical_incident_count"`
	Incidents             []risk.Incident     `json:"incidents"`
}

type OperatorTradingGateSummary struct {
	Allowed         bool                `json:"allowed"`
	EntriesAllowed  bool                `json:"entries_allowed"`
	ExitsAllowed    bool                `json:"exits_allowed"`
	ReduceOnly      bool                `json:"reduce_only"`
	Mode            risk.SupervisorMode `json:"mode"`
	BlockReason     string              `json:"block_reason"`
	BlockingReasons []string            `json:"blocking_reasons"`
	Message         string              `json:"message"`
}

type OperatorOrderReconciliationSummary struct {
	Available               bool           `json:"available"`
	LastRunAt               string         `json:"last_run_at"`
	LastSuccessAt           string         `json:"last_success_at"`
	LastError               string         `json:"last_error"`
	TotalRuns               int            `json:"total_runs"`
	TotalMismatches         int            `json:"total_mismatches"`
	TotalRepairs            int            `json:"total_repairs"`
	UnknownBrokerOrders     int            `json:"unknown_broker_orders"`
	LocalMissingAtBroker    int            `json:"local_missing_at_broker"`
	FillMismatches          int            `json:"fill_mismatches"`
	ImportedOrders          int            `json:"imported_orders"`
	ResolvedOrders          int            `json:"resolved_orders"`
	TotalInferredOutcomes   int            `json:"total_inferred_outcomes"`
	TotalUnresolvedOutcomes int            `json:"total_unresolved_outcomes"`
	TrackedOrders           int            `json:"tracked_orders"`
	ActiveLocalOrders       int            `json:"active_local_orders"`
	BrokerOpenOrders        int            `json:"broker_open_orders"`
	CurrentPendingOrders    int            `json:"current_pending_orders"`
	CurrentConfirmedOrders  int            `json:"current_confirmed_orders"`
	CurrentInferredOrders   int            `json:"current_inferred_orders"`
	CurrentUnresolvedOrders int            `json:"current_unresolved_orders"`
	LastInferredAt          string         `json:"last_inferred_at"`
	LastUnresolvedAt        string         `json:"last_unresolved_at"`
	ConfidenceDegraded      bool           `json:"confidence_degraded"`
	PrimaryIssueLocalID     string         `json:"primary_issue_local_order_id,omitempty"`
	PrimaryIssueBrokerID    string         `json:"primary_issue_broker_order_id,omitempty"`
	PrimaryAuthority        string         `json:"primary_issue_authority,omitempty"`
	PrimaryConfidence       string         `json:"primary_issue_confidence,omitempty"`
	PrimaryReason           string         `json:"primary_issue_reason,omitempty"`
	PrimaryNeedsReview      bool           `json:"primary_issue_needs_review"`
	LastSummary             string         `json:"last_summary"`
	LastIssues              []orders.Issue `json:"last_issues,omitempty"`
}

type OperatorPortfolioRiskSummary struct {
	Available       bool                  `json:"available"`
	LastEvaluatedAt string                `json:"last_evaluated_at"`
	Outcome         risk.Outcome          `json:"outcome"`
	Summary         string                `json:"summary"`
	Metrics         risk.PortfolioMetrics `json:"metrics"`
}

type OperatorPositionReconciliationSummary struct {
	Available            bool                         `json:"available"`
	Status               PositionReconciliationStatus `json:"status"`
	Summary              string                       `json:"summary"`
	TradingAllowed       bool                         `json:"trading_allowed"`
	LastRunAt            string                       `json:"last_run_at"`
	LastSuccessAt        string                       `json:"last_success_at"`
	LastIncidentAt       string                       `json:"last_incident_at"`
	LastReconciledAt     string                       `json:"last_reconciled_at"`
	LastError            string                       `json:"last_error"`
	TotalRuns            int                          `json:"total_runs"`
	TotalIncidents       int                          `json:"total_incidents"`
	TotalMismatches      int                          `json:"total_mismatches"`
	LocalMissingAtBroker int                          `json:"local_missing_at_broker"`
	BrokerMissingLocally int                          `json:"broker_missing_locally"`
	SizeMismatches       int                          `json:"size_mismatches"`
	PriceMismatches      int                          `json:"price_mismatches"`
	LocalPositions       int                          `json:"local_positions"`
	BrokerPositions      int                          `json:"broker_positions"`
	LastIssues           []positions.Issue            `json:"last_issues,omitempty"`
}

type OperatorDataQualitySummaryView struct {
	Available           bool                               `json:"available"`
	LastCheckedAt       string                             `json:"last_checked_at"`
	TotalChecks         int                                `json:"total_checks"`
	TotalFailures       int                                `json:"total_failures"`
	BlockedSymbols      []OperatorDataQualityBlockedSymbol `json:"blocked_symbols"`
	BlockedSymbolsCount int                                `json:"blocked_symbols_count"`
	FeedDelayed         bool                               `json:"feed_delayed"`
	FeedSummary         string                             `json:"feed_summary"`
	FeedDetectedAt      string                             `json:"feed_detected_at"`
	FeedProbeSymbols    []string                           `json:"feed_probe_symbols"`
}

type OperatorAlertsSummary struct {
	Available     bool           `json:"available"`
	TotalCount    int            `json:"total_count"`
	CriticalCount int            `json:"critical_count"`
	WarningCount  int            `json:"warning_count"`
	InfoCount     int            `json:"info_count"`
	LastAlertAt   string         `json:"last_alert_at"`
	Recent        []alerts.Alert `json:"recent"`
}

type OperatorIncidentsSummary struct {
	Available                 bool                 `json:"available"`
	OpenCount                 int                  `json:"open_count"`
	AcknowledgedCount         int                  `json:"acknowledged_count"`
	CriticalOpenCount         int                  `json:"critical_open_count"`
	LatestIncidentAt          string               `json:"latest_incident_at"`
	LatestIncidentSummary     string               `json:"latest_incident_summary"`
	LatestIncidentSeverity    incidents.Severity   `json:"latest_incident_severity"`
	LatestIncidentRunbookHint string               `json:"latest_incident_runbook_hint"`
	LatestCriticalIncident    *incidents.Incident  `json:"latest_critical_incident,omitempty"`
	OpenIncidents             []incidents.Incident `json:"open_incidents"`
	RecentResolvedIncidents   []incidents.Incident `json:"recent_resolved_incidents"`
}

type OperatorKillSwitchSummary struct {
	Available           bool   `json:"available"`
	Active              bool   `json:"active"`
	Source              string `json:"source"`
	Message             string `json:"message"`
	FilePath            string `json:"file_path"`
	TriggeredAt         string `json:"triggered_at"`
	LastCheckedAt       string `json:"last_checked_at"`
	LastClearedAt       string `json:"last_cleared_at"`
	OrdersCancelled     bool   `json:"orders_cancelled"`
	LastCancelAttemptAt string `json:"last_cancel_attempt_at"`
	LastCancelError     string `json:"last_cancel_error"`
	ActivationCount     int    `json:"activation_count"`
}

type OperatorStatusSummary struct {
	StatusSchemaVersion    int                                   `json:"status_schema_version"`
	TraderID               string                                `json:"trader_id"`
	TraderName             string                                `json:"trader_name"`
	Mode                   string                                `json:"mode"`
	Exchange               string                                `json:"exchange"`
	Broker                 string                                `json:"broker"`
	StrategyMode           string                                `json:"strategy_mode"`
	DecisionArchitecture   string                                `json:"decision_architecture"`
	TradingAllowed         bool                                  `json:"trading_allowed"`
	EntriesAllowed         bool                                  `json:"entries_allowed"`
	ExitsAllowed           bool                                  `json:"exits_allowed"`
	ReduceOnly             bool                                  `json:"reduce_only"`
	TradingBlockReason     string                                `json:"trading_block_reason"`
	BlockingReasons        []string                              `json:"blocking_reasons"`
	OperatorMessage        string                                `json:"operator_message"`
	Readiness              OperatorReadinessSummary              `json:"readiness"`
	BrokerRuntime          OperatorBrokerRuntimeSummary          `json:"broker_runtime"`
	Promotion              OperatorPromotionSummary              `json:"promotion"`
	RiskSupervisor         OperatorRiskSupervisorSummary         `json:"risk_supervisor"`
	Execution              OperatorExecutionSummary              `json:"execution"`
	Universe               OperatorUniverseSummary               `json:"universe"`
	Protection             OperatorProtectionSummary             `json:"protection"`
	ShadowMode             OperatorShadowModeSummary             `json:"shadow_mode"`
	RestartRecovery        OperatorRestartRecoverySummary        `json:"restart_recovery"`
	BrokerTruth            OperatorBrokerTruthSummary            `json:"broker_truth"`
	DeploymentValidation   OperatorDeploymentValidationSummary   `json:"deployment_validation"`
	Session                OperatorSessionSummary                `json:"session"`
	EventJournal           OperatorEventJournalSummary           `json:"event_journal"`
	Runtime                OperatorRuntimeSummary                `json:"runtime"`
	TradingGate            OperatorTradingGateSummary            `json:"trading_gate"`
	OrderReconciliation    OperatorOrderReconciliationSummary    `json:"order_reconciliation"`
	PositionReconciliation OperatorPositionReconciliationSummary `json:"position_reconciliation"`
	DataQuality            OperatorDataQualitySummaryView        `json:"data_quality"`
	PortfolioRisk          OperatorPortfolioRiskSummary          `json:"portfolio_risk"`
	KillSwitch             OperatorKillSwitchSummary             `json:"kill_switch"`
	Alerts                 OperatorAlertsSummary                 `json:"alerts"`
	Incidents              OperatorIncidentsSummary              `json:"incidents"`

	// Compatibility fields preserved for the existing dashboard and lightweight clients.
	AIModel                         string             `json:"ai_model"`
	IsRunning                       bool               `json:"is_running"`
	StartTime                       string             `json:"start_time"`
	RuntimeMinutes                  int                `json:"runtime_minutes"`
	CallCount                       int                `json:"call_count"`
	InitialBalance                  float64            `json:"initial_balance"`
	ScanInterval                    string             `json:"scan_interval"`
	StopUntil                       string             `json:"stop_until"`
	LastResetTime                   string             `json:"last_reset_time"`
	AIProvider                      string             `json:"ai_provider"`
	IsDemoMode                      bool               `json:"is_demo_mode"`
	DemoLastCycleTime               string             `json:"demo_last_cycle_time"`
	ReadinessStatus                 ReadinessStatus    `json:"readiness_status"`
	ReadinessMessage                string             `json:"readiness_message"`
	ReadinessCheckedAt              string             `json:"readiness_checked_at"`
	ReadinessTradingAllowed         bool               `json:"readiness_trading_allowed"`
	ReadinessPassCount              int                `json:"readiness_pass_count"`
	ReadinessWarnCount              int                `json:"readiness_warn_count"`
	ReadinessFailCount              int                `json:"readiness_fail_count"`
	ReadinessChecks                 []ReadinessCheck   `json:"readiness_checks"`
	PromotionStatus                 PromotionStatus    `json:"promotion_status"`
	PromotionMessage                string             `json:"promotion_message"`
	PromotionCheckedAt              string             `json:"promotion_checked_at"`
	PromotionRequired               bool               `json:"promotion_required"`
	PromotionLiveTradingAllowed     bool               `json:"promotion_live_trading_allowed"`
	PromotionPassCount              int                `json:"promotion_pass_count"`
	PromotionWarnCount              int                `json:"promotion_warn_count"`
	PromotionFailCount              int                `json:"promotion_fail_count"`
	PromotionChecks                 []PromotionCheck   `json:"promotion_checks"`
	RiskSupervisorMode              string             `json:"risk_supervisor_mode"`
	RiskSupervisorSummary           string             `json:"risk_supervisor_summary"`
	RiskSupervisorActiveIncidents   int                `json:"risk_supervisor_active_incidents"`
	RiskSupervisorCriticalIncidents int                `json:"risk_supervisor_critical_incidents"`
	ExecutionAvailable              bool               `json:"execution_available"`
	ExecutionInFlightCount          int                `json:"execution_in_flight_count"`
	ExecutionStaleCount             int                `json:"execution_stale_count"`
	ExecutionLastAt                 string             `json:"execution_last_execution_at"`
	ExecutionLastSymbol             string             `json:"execution_last_execution_symbol"`
	ExecutionLastStatus             string             `json:"execution_last_execution_status"`
	ExecutionDuplicateSuppressed    int                `json:"execution_duplicate_suppressed_count"`
	ExecutionBlockedCount           int                `json:"execution_blocked_count"`
	ExecutionSubmittedCount         int                `json:"execution_submitted_count"`
	ExecutionAcknowledgedCount      int                `json:"execution_acknowledged_count"`
	ExecutionFilledCount            int                `json:"execution_filled_count"`
	ExecutionRejectedCount          int                `json:"execution_rejected_count"`
	ExecutionFailedCount            int                `json:"execution_failed_count"`
	UniverseAvailable               bool               `json:"universe_available"`
	UniverseSelectionMode           string             `json:"universe_selection_mode"`
	UniverseConfiguredSource        string             `json:"universe_configured_source"`
	UniverseConfiguredCount         int                `json:"universe_configured_count"`
	UniverseEffectiveCount          int                `json:"universe_effective_count"`
	UniverseManifestPath            string             `json:"universe_manifest_path"`
	UniverseMessage                 string             `json:"universe_message"`
	ProtectionPendingCount          int                `json:"protection_pending_count"`
	ProtectionActiveCount           int                `json:"protection_active_protective_count"`
	ProtectionMessage               string             `json:"protection_message"`
	ShadowModeActive                bool               `json:"shadow_mode_active"`
	ShadowDecisionCount             int                `json:"shadow_decision_count"`
	ShadowWouldTradeCount           int                `json:"shadow_would_trade_count"`
	ShadowBlockedCount              int                `json:"shadow_blocked_count"`
	ShadowRealizedPnL               float64            `json:"shadow_realized_pnl"`
	ShadowUnrealizedPnL             float64            `json:"shadow_unrealized_pnl"`
	BrokerState                     BrokerRuntimeState `json:"broker_state"`
	BrokerStateReason               string             `json:"broker_state_reason"`
	BrokerLastError                 string             `json:"broker_last_error"`
	BrokerStateSince                string             `json:"broker_state_since"`
	BrokerLastHealthyAt             string             `json:"broker_last_healthy_at"`
	BrokerLastReconciledAt          string             `json:"broker_last_reconciled_at"`
	BrokerReconnectAttempts         int                `json:"broker_reconnect_attempts"`
	BrokerNextRetryAt               string             `json:"broker_next_retry_at"`
	BrokerRecoveryActive            bool               `json:"broker_recovery_active"`
	BrokerTradingAllowed            bool               `json:"broker_trading_allowed"`
	BrokerTruthAvailable            bool               `json:"broker_truth_available"`
	BrokerTruthRequired             bool               `json:"broker_truth_required"`
	BrokerTruthVerified             bool               `json:"broker_truth_verified"`
	BrokerTruthTradingBlocked       bool               `json:"broker_truth_trading_blocked"`
	BrokerTruthEntriesRestricted    bool               `json:"broker_truth_entries_restricted"`
	BrokerTruthMessage              string             `json:"broker_truth_message"`
	BrokerTruthRestrictionReason    string             `json:"broker_truth_restriction_reason"`
	DeploymentValidationRequired    bool               `json:"deployment_validation_required"`
	DeploymentValidationPassed      bool               `json:"deployment_validation_passed"`
	DeploymentValidationFresh       bool               `json:"deployment_validation_fresh"`
	DeploymentValidationMessage     string             `json:"deployment_validation_message"`
	LastSessionReportPath           string             `json:"last_session_report_path"`
	LastSessionReportStatus         string             `json:"last_session_report_status"`
	LastSessionReportAt             string             `json:"last_session_report_at"`
	OrderReconciliationAvailable    bool               `json:"order_reconciliation_available"`
	OrderReconciliationLastRunAt    string             `json:"order_reconciliation_last_run_at"`
	OrderReconciliationLastError    string             `json:"order_reconciliation_last_error"`
	OrderReconciliationTotalRuns    int                `json:"order_reconciliation_total_runs"`
	OrderReconciliationMismatches   int                `json:"order_reconciliation_total_mismatches"`
	OrderReconciliationRepairs      int                `json:"order_reconciliation_total_repairs"`
	OrderReconciliationInferred     int                `json:"order_reconciliation_total_inferred"`
	OrderReconciliationUnresolved   int                `json:"order_reconciliation_total_unresolved"`
	OrderReconciliationDegraded     bool               `json:"order_reconciliation_confidence_degraded"`
	OrderReconciliationSummary      string             `json:"order_reconciliation_summary"`
	PositionReconciliationAvailable bool               `json:"position_reconciliation_available"`
	PositionReconciliationStatus    string             `json:"position_reconciliation_status"`
	PositionReconciliationLastRunAt string             `json:"position_reconciliation_last_run_at"`
	PositionReconciliationLastError string             `json:"position_reconciliation_last_error"`
	PositionReconciliationTotalRuns int                `json:"position_reconciliation_total_runs"`
	PositionReconciliationIncidents int                `json:"position_reconciliation_total_incidents"`
	PositionReconciliationSummary   string             `json:"position_reconciliation_summary"`
	DataQualityAvailable            bool               `json:"data_quality_available"`
	DataQualityLastCheckedAt        string             `json:"data_quality_last_checked_at"`
	DataQualityBlockedSymbols       int                `json:"data_quality_blocked_symbols"`
	DataQualityFailures             int                `json:"data_quality_total_failures"`
	DataQualityFeedDelayed          bool               `json:"data_quality_feed_delayed"`
	DataQualityFeedSummary          string             `json:"data_quality_feed_summary"`
	PortfolioRiskAvailable          bool               `json:"portfolio_risk_available"`
	PortfolioRiskLastEvaluatedAt    string             `json:"portfolio_risk_last_evaluated_at"`
	PortfolioRiskOutcome            string             `json:"portfolio_risk_outcome"`
	PortfolioRiskSummary            string             `json:"portfolio_risk_summary"`
	PortfolioGrossExposurePct       float64            `json:"portfolio_gross_exposure_pct"`
	PortfolioNetExposurePct         float64            `json:"portfolio_net_exposure_pct"`
	PortfolioLargestSector          string             `json:"portfolio_largest_sector"`
	PortfolioLargestSectorPct       float64            `json:"portfolio_largest_sector_exposure_pct"`
	PortfolioCorrelatedPositions    int                `json:"portfolio_correlated_positions"`
	PortfolioMaxCorrelation         float64            `json:"portfolio_max_observed_correlation"`
	PortfolioCurrentDrawdownPct     float64            `json:"portfolio_current_drawdown_pct"`
	KillSwitchActive                bool               `json:"kill_switch_active"`
	KillSwitchSource                string             `json:"kill_switch_source"`
	KillSwitchMessage               string             `json:"kill_switch_message"`
	KillSwitchFilePath              string             `json:"kill_switch_file_path"`
	KillSwitchTriggeredAt           string             `json:"kill_switch_triggered_at"`
	KillSwitchLastCheckedAt         string             `json:"kill_switch_last_checked_at"`
	KillSwitchOrdersCancelled       bool               `json:"kill_switch_orders_cancelled"`
	KillSwitchLastCancelError       string             `json:"kill_switch_last_cancel_error"`
	RecentAlerts                    []alerts.Alert     `json:"recent_alerts"`
	AlertCount                      int                `json:"alert_count"`
	CriticalAlertCount              int                `json:"critical_alert_count"`
	WarningAlertCount               int                `json:"warning_alert_count"`
	InfoAlertCount                  int                `json:"info_alert_count"`
	LastAlertAt                     string             `json:"last_alert_at"`
	LatestIncidentSummary           string             `json:"latest_incident_summary"`
	LatestIncidentSeverity          string             `json:"latest_incident_severity"`
	LatestIncidentRunbookHint       string             `json:"latest_incident_runbook_hint"`
}

func (at *AutoTrader) GetOperatorStatus() OperatorStatusSummary {
	brokerStatus := at.brokerRuntimeStatus()
	readiness := at.getReadinessSummary()
	promotion := at.getPromotionSummary()
	startTime := formatRFC3339(at.startTime)
	stopUntil := formatRFC3339(at.stopUntil)
	lastResetTime := formatRFC3339(at.lastResetTime)
	lastSessionReportAt := formatRFC3339(at.lastSessionReportAt)
	brokerStateSince := formatRFC3339(brokerStatus.Since)
	brokerLastHealthyAt := formatRFC3339(brokerStatus.LastHealthyAt)
	brokerLastReconciledAt := formatRFC3339(brokerStatus.LastReconciledAt)
	brokerNextRetryAt := formatRFC3339(brokerStatus.NextRetryAt)
	readinessCheckedAt := formatRFC3339(readiness.CheckedAt)
	promotionCheckedAt := formatRFC3339(promotion.CheckedAt)
	demoLastCycleTime := formatRFC3339(at.demoLastCycleTime)
	orderRecon := at.currentOrderReconciliationSummary()
	positionRecon := at.currentPositionReconciliationSummary()
	dataQuality := at.currentDataQualitySummary()
	portfolioRisk := at.currentPortfolioRiskState()
	killSwitch := at.currentKillSwitchSummary()
	alertSummary := at.currentAlertsSummary()
	incidentSummary := at.currentIncidentSummary()
	executionState := at.currentExecutionSummary()
	universeState := at.currentUniverseSummary()
	shadowState := at.currentShadowSummary()
	restartRecovery := at.currentRestartRecoverySummary()
	brokerTruth := at.currentBrokerTruthSummary()
	eventJournal := at.currentEventJournalSummary()
	deploymentValidation := startup.CurrentLiveValidationStatus("", strings.EqualFold(at.config.Mode, "live"), time.Now())
	var primaryOrderIssue *orders.Issue
	if orderRecon != nil {
		primaryOrderIssue = orders.PrimaryExecutionTruthIssue(orderRecon.LastIssues)
	}

	aiProvider := "DeepSeek"
	if at.demoMode {
		aiProvider = "Demo"
	} else if at.aiModel == "custom" {
		aiProvider = "Custom"
	} else if at.config.UseQwen || at.aiModel == "qwen" {
		aiProvider = "Qwen"
	}

	brokerManaged := at.managesIBKRBrokerRuntime()
	brokerReason := strings.TrimSpace(brokerStatus.Reason)
	if !brokerManaged && brokerReason == "" {
		brokerReason = "broker runtime gating is not required for this trader"
	}
	brokerTradingAllowed := !brokerManaged || brokerStatus.State == BrokerRuntimeHealthy
	promotionLiveTradingAllowed := !at.requiresLivePromotion() || promotion.LiveTradingAllowed
	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	riskState := at.currentRiskSupervisorState()
	if riskState.EvaluatedAt.IsZero() {
		riskState = at.evaluateRiskSupervisor(at.currentLatestAccountSummary(), false)
		gate = at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	}

	runtimeSummary := OperatorRuntimeSummary{
		IsRunning:         at.isRunning,
		StartTime:         startTime,
		RuntimeMinutes:    runtimeMinutesSince(at.startTime),
		CallCount:         at.callCount,
		InitialBalance:    at.initialBalance,
		ScanInterval:      at.config.ScanInterval.String(),
		StopUntil:         stopUntil,
		LastResetTime:     lastResetTime,
		AIProvider:        aiProvider,
		AIModel:           at.aiModel,
		IsDemoMode:        at.demoMode,
		DemoLastCycleTime: demoLastCycleTime,
	}

	readinessSummary := OperatorReadinessSummary{
		Status:         readiness.Status,
		Message:        readiness.Message,
		CheckedAt:      readinessCheckedAt,
		TradingAllowed: readiness.TradingAllowed,
		PassCount:      readiness.PassCount,
		WarnCount:      readiness.WarnCount,
		FailCount:      readiness.FailCount,
		Checks:         append([]ReadinessCheck(nil), readiness.Checks...),
	}

	promotionSummary := OperatorPromotionSummary{
		Status:             promotion.Status,
		Message:            promotion.Message,
		CheckedAt:          promotionCheckedAt,
		Required:           promotion.Required,
		LiveTradingAllowed: promotionLiveTradingAllowed,
		PassCount:          promotion.PassCount,
		WarnCount:          promotion.WarnCount,
		FailCount:          promotion.FailCount,
		Checks:             append([]PromotionCheck(nil), promotion.Checks...),
	}

	riskSupervisorSummary := OperatorRiskSupervisorSummary{
		Mode:                  riskState.Mode,
		TradingAllowed:        riskState.TradingAllowed,
		EntriesAllowed:        riskState.EntriesAllowed,
		ExitsAllowed:          riskState.ExitsAllowed,
		ReduceOnly:            riskState.ReduceOnly,
		Summary:               riskState.Summary,
		ActiveIncidentCount:   riskState.ActiveIncidentCount,
		CriticalIncidentCount: riskState.CriticalIncidentCount,
		Incidents:             append([]risk.Incident(nil), riskState.Incidents...),
	}

	executionSummary := OperatorExecutionSummary{
		Available:                executionState.Available,
		InFlightCount:            executionState.InFlightCount,
		StaleCount:               executionState.StaleCount,
		LastExecutionAt:          formatRFC3339(executionState.LastExecutionAt),
		LastExecutionSymbol:      executionState.LastExecutionSymbol,
		LastExecutionStatus:      executionState.LastExecutionStatus,
		DuplicateSuppressedCount: executionState.DuplicateSuppressedCount,
		BlockedExecutionCount:    executionState.BlockedExecutionCount,
		SubmittedCount:           executionState.SubmittedCount,
		AcknowledgedCount:        executionState.AcknowledgedCount,
		FilledCount:              executionState.FilledCount,
		RejectedCount:            executionState.RejectedCount,
		FailedCount:              executionState.FailedCount,
	}
	universePreview, universePreviewTruncated := previewUniverseSymbols(universeState.EffectiveSymbols)
	universeSummary := OperatorUniverseSummary{
		Available:               universeState.Available,
		InstrumentType:          universeState.InstrumentType,
		SelectionMode:           universeState.SelectionMode,
		ConfiguredSource:        universeState.ConfiguredSource,
		ConfiguredSymbolsCount:  len(universeState.ConfiguredSymbols),
		EffectiveSymbolsCount:   len(universeState.EffectiveSymbols),
		TrustedSymbolsFile:      universeState.TrustedSymbolsFile,
		TrustedSymbolsCount:     universeState.TrustedSymbolsCount,
		BenchmarkSymbols:        append([]string(nil), universeState.BenchmarkSymbols...),
		ManifestPath:            universeState.ManifestPath,
		ManifestPersisted:       universeState.ManifestPersisted,
		ManifestLastError:       universeState.ManifestLastError,
		LastUpdatedAt:           formatRFC3339(universeState.LastUpdatedAt),
		EffectiveSymbolsPreview: universePreview,
		PreviewTruncated:        universePreviewTruncated,
		LastCandidateWindow:     append([]string(nil), universeState.LastCandidateWindow...),
		LastMandatorySymbols:    append([]string(nil), universeState.LastMandatory...),
		LastMarketDataLoadOrder: append([]string(nil), universeState.LastLoadOrder...),
		Message:                 universeState.Message,
	}
	protectionState := at.currentProtectionSummary()
	protectionPending := make([]OperatorPendingProtectionItem, 0, len(protectionState.Pending))
	for _, pending := range protectionState.Pending {
		protectionPending = append(protectionPending, OperatorPendingProtectionItem{
			Symbol:                pending.Symbol,
			PositionSide:          pending.PositionSide,
			EntryStatus:           pending.EntryStatus,
			Status:                pending.Status,
			RequestedQuantity:     pending.RequestedQuantity,
			ConfirmedQuantity:     pending.ConfirmedQuantity,
			StopProtectedQuantity: pending.StopProtectedQuantity,
			TargetProtectedQty:    pending.TargetProtectedQty,
			StopPrice:             pending.StopPrice,
			TakeProfitPrice:       pending.TakeProfitPrice,
			Message:               pending.Message,
			UpdatedAt:             formatRFC3339(pending.UpdatedAt),
		})
	}
	protectionSummary := OperatorProtectionSummary{
		Available:             protectionState.Available,
		PendingCount:          protectionState.PendingCount,
		ActiveProtectiveCount: protectionState.ActiveProtectiveCount,
		LastUpdatedAt:         formatRFC3339(protectionState.LastUpdatedAt),
		Message:               protectionState.Message,
		Pending:               protectionPending,
	}
	shadowSummary := OperatorShadowModeSummary{
		Available:                 shadowState.Available,
		Active:                    shadowState.Active,
		LastDecisionAt:            formatRFC3339(shadowState.LastDecisionAt),
		LastDecisionSymbol:        shadowState.LastDecisionSymbol,
		LastDecisionAction:        shadowState.LastDecisionAction,
		LastDecisionStatus:        shadowState.LastDecisionStatus,
		DecisionCount:             shadowState.TotalDecisions,
		WouldTradeCount:           shadowState.WouldTradeCount,
		BlockedCount:              shadowState.BlockedCount,
		OpenPositions:             shadowState.OpenPositions,
		ClosedTrades:              shadowState.ClosedTrades,
		HypotheticalRealizedPnL:   shadowState.HypotheticalRealizedPnL,
		HypotheticalUnrealizedPnL: shadowState.HypotheticalUnrealizedPnL,
		LastBlockReason:           shadowState.LastBlockReason,
		RecentDecisions:           append([]logger.ShadowExecution(nil), shadowState.RecentDecisions...),
	}
	restartRecoverySummary := OperatorRestartRecoverySummary{
		Available:               restartRecovery.Available,
		Status:                  restartRecovery.Status,
		StatePath:               restartRecovery.StatePath,
		StatePresent:            restartRecovery.StatePresent,
		Restored:                restartRecovery.Restored,
		PendingReconciliation:   restartRecovery.PendingReconciliation,
		TradingBlocked:          restartRecovery.TradingBlocked,
		Partial:                 restartRecovery.Partial,
		Corrupt:                 restartRecovery.Corrupt,
		Message:                 restartRecovery.Message,
		SavedAt:                 formatRFC3339(restartRecovery.SavedAt),
		RestoredAt:              formatRFC3339(restartRecovery.RestoredAt),
		LastPersistedAt:         formatRFC3339(restartRecovery.LastPersistedAt),
		LastLoadError:           restartRecovery.LastLoadError,
		LastSaveError:           restartRecovery.LastSaveError,
		RestoredExecutionCount:  restartRecovery.RestoredExecutionCount,
		RestoredInFlightCount:   restartRecovery.RestoredInFlightCount,
		RestoredActiveOrders:    restartRecovery.RestoredActiveOrders,
		RestoredLocalPositions:  restartRecovery.RestoredLocalPositions,
		RestoredPendingProtect:  restartRecovery.RestoredPendingProtect,
		RestoredShadowPositions: restartRecovery.RestoredShadowPositions,
	}
	eventJournalSummary := OperatorEventJournalSummary{
		Available:     eventJournal.Available,
		Path:          eventJournal.Path,
		EventCount:    eventJournal.EventCount,
		LastEventAt:   formatRFC3339(eventJournal.LastEventAt),
		LastEventType: eventJournal.LastEventType,
		LastSeverity:  string(eventJournal.LastSeverity),
		LastError:     eventJournal.LastError,
	}
	brokerTruthSummary := OperatorBrokerTruthSummary{
		Available:            brokerTruth.Available,
		Required:             brokerTruth.Required,
		BrokerManaged:        brokerTruth.BrokerManaged,
		Verified:             brokerTruth.Verified,
		TradingBlocked:       brokerTruth.TradingBlocked,
		EntriesRestricted:    brokerTruth.EntriesRestricted,
		ConfidenceDegraded:   brokerTruth.ConfidenceDegraded,
		RestrictionReason:    brokerTruth.RestrictionReason,
		AccountRequired:      brokerTruth.AccountRequired,
		AccountVerified:      brokerTruth.AccountVerified,
		OrdersRequired:       brokerTruth.OrdersRequired,
		OrdersVerified:       brokerTruth.OrdersVerified,
		PositionsRequired:    brokerTruth.PositionsRequired,
		PositionsVerified:    brokerTruth.PositionsVerified,
		MarketDataRequired:   brokerTruth.MarketDataRequired,
		MarketDataVerified:   brokerTruth.MarketDataVerified,
		AccountCapturedAt:    formatRFC3339(brokerTruth.AccountCapturedAt),
		OrdersCheckedAt:      formatRFC3339(brokerTruth.OrdersCheckedAt),
		PositionsCheckedAt:   formatRFC3339(brokerTruth.PositionsCheckedAt),
		MarketDataCheckedAt:  formatRFC3339(brokerTruth.MarketDataCheckedAt),
		InferredOrderCount:   brokerTruth.InferredOrderCount,
		UnresolvedOrderCount: brokerTruth.UnresolvedOrderCount,
		PrimaryIssueLocalID:  brokerTruth.PrimaryIssueLocalID,
		PrimaryIssueBrokerID: brokerTruth.PrimaryIssueBrokerID,
		PrimaryAuthority:     string(brokerTruth.PrimaryAuthority),
		PrimaryConfidence:    string(brokerTruth.PrimaryConfidence),
		PrimaryReason:        brokerTruth.PrimaryReason,
		PrimaryNeedsReview:   brokerTruth.PrimaryNeedsReview,
		Message:              brokerTruth.Message,
		BlockingReasons:      append([]string(nil), brokerTruth.BlockingReasons...),
	}
	deploymentValidationSummary := OperatorDeploymentValidationSummary{
		Required:            deploymentValidation.Required,
		Passed:              deploymentValidation.Passed,
		Fresh:               deploymentValidation.Fresh,
		ConfigMatches:       deploymentValidation.ConfigMatches,
		ActiveConfigFile:    deploymentValidation.ActiveConfigFile,
		ValidatedConfigFile: deploymentValidation.ValidatedConfigFile,
		CheckedAt:           formatRFC3339(deploymentValidation.CheckedAt),
		Source:              deploymentValidation.Source,
		Message:             deploymentValidation.Message,
	}

	brokerSummary := OperatorBrokerRuntimeSummary{
		Managed:           brokerManaged,
		State:             brokerStatus.State,
		Reason:            brokerReason,
		LastError:         brokerStatus.LastError,
		StateSince:        brokerStateSince,
		LastHealthyAt:     brokerLastHealthyAt,
		LastReconciledAt:  brokerLastReconciledAt,
		ReconnectAttempts: brokerStatus.ReconnectAttempts,
		NextRetryAt:       brokerNextRetryAt,
		RecoveryActive:    brokerStatus.RecoveryActive,
		TradingAllowed:    brokerTradingAllowed,
	}

	sessionSummary := OperatorSessionSummary{
		LastSessionReportPath:   at.lastSessionReportPath,
		LastSessionReportStatus: at.lastSessionReportStatus,
		LastSessionReportAt:     lastSessionReportAt,
	}

	orderReconSummary := OperatorOrderReconciliationSummary{}
	if orderRecon != nil {
		orderReconSummary = OperatorOrderReconciliationSummary{
			Available:               true,
			LastRunAt:               formatRFC3339(orderRecon.LastRunAt),
			LastSuccessAt:           formatRFC3339(orderRecon.LastSuccessAt),
			LastError:               orderRecon.LastError,
			TotalRuns:               orderRecon.TotalRuns,
			TotalMismatches:         orderRecon.TotalMismatches,
			TotalRepairs:            orderRecon.TotalRepairs,
			UnknownBrokerOrders:     orderRecon.UnknownBrokerOrders,
			LocalMissingAtBroker:    orderRecon.LocalMissingAtBroker,
			FillMismatches:          orderRecon.FillMismatches,
			ImportedOrders:          orderRecon.ImportedOrders,
			ResolvedOrders:          orderRecon.ResolvedOrders,
			TotalInferredOutcomes:   orderRecon.TotalInferredOutcomes,
			TotalUnresolvedOutcomes: orderRecon.TotalUnresolvedOutcomes,
			TrackedOrders:           orderRecon.TrackedOrders,
			ActiveLocalOrders:       orderRecon.ActiveLocalOrders,
			BrokerOpenOrders:        orderRecon.BrokerOpenOrders,
			CurrentPendingOrders:    orderRecon.CurrentPendingOrders,
			CurrentConfirmedOrders:  orderRecon.CurrentConfirmedOrders,
			CurrentInferredOrders:   orderRecon.CurrentInferredOrders,
			CurrentUnresolvedOrders: orderRecon.CurrentUnresolvedOrders,
			LastInferredAt:          formatRFC3339(orderRecon.LastInferredAt),
			LastUnresolvedAt:        formatRFC3339(orderRecon.LastUnresolvedAt),
			ConfidenceDegraded:      orderRecon.ConfidenceDegraded,
			PrimaryIssueLocalID:     strings.TrimSpace(primaryOrderIssueField(primaryOrderIssue, "local_id")),
			PrimaryIssueBrokerID:    strings.TrimSpace(primaryOrderIssueField(primaryOrderIssue, "broker_id")),
			PrimaryAuthority:        strings.TrimSpace(primaryOrderIssueField(primaryOrderIssue, "authority")),
			PrimaryConfidence:       strings.TrimSpace(primaryOrderIssueField(primaryOrderIssue, "confidence")),
			PrimaryReason:           strings.TrimSpace(primaryOrderIssueField(primaryOrderIssue, "message")),
			PrimaryNeedsReview:      primaryOrderIssue != nil && primaryOrderIssue.NeedsReview,
			LastSummary:             orderRecon.LastSummary,
			LastIssues:              append([]orders.Issue(nil), orderRecon.LastIssues...),
		}
	}

	positionReconSummary := OperatorPositionReconciliationSummary{}
	if positionRecon != nil {
		positionReconSummary = OperatorPositionReconciliationSummary{
			Available:            positionRecon.Available,
			Status:               positionRecon.Status,
			Summary:              positionRecon.Summary,
			TradingAllowed:       positionRecon.TradingAllowed,
			LastRunAt:            formatRFC3339(positionRecon.LastRunAt),
			LastSuccessAt:        formatRFC3339(positionRecon.LastSuccessAt),
			LastIncidentAt:       formatRFC3339(positionRecon.LastIncidentAt),
			LastReconciledAt:     formatRFC3339(positionRecon.LastReconciledAt),
			LastError:            positionRecon.LastError,
			TotalRuns:            positionRecon.TotalRuns,
			TotalIncidents:       positionRecon.TotalIncidents,
			TotalMismatches:      positionRecon.TotalMismatches,
			LocalMissingAtBroker: positionRecon.LocalMissingAtBroker,
			BrokerMissingLocally: positionRecon.BrokerMissingLocally,
			SizeMismatches:       positionRecon.SizeMismatches,
			PriceMismatches:      positionRecon.PriceMismatches,
			LocalPositions:       positionRecon.LocalPositions,
			BrokerPositions:      positionRecon.BrokerPositions,
			LastIssues:           append([]positions.Issue(nil), positionRecon.LastIssues...),
		}
	}

	dataQualitySummary := OperatorDataQualitySummaryView{
		Available:           dataQuality.Available,
		LastCheckedAt:       dataQuality.LastCheckedAt,
		TotalChecks:         dataQuality.TotalChecks,
		TotalFailures:       dataQuality.TotalFailures,
		BlockedSymbols:      append([]OperatorDataQualityBlockedSymbol(nil), dataQuality.BlockedSymbols...),
		BlockedSymbolsCount: dataQuality.BlockedSymbolsCount,
		FeedDelayed:         dataQuality.FeedDelayed,
		FeedSummary:         dataQuality.FeedSummary,
		FeedDetectedAt:      dataQuality.FeedDetectedAt,
		FeedProbeSymbols:    append([]string(nil), dataQuality.FeedProbeSymbols...),
	}

	portfolioRiskSummary := OperatorPortfolioRiskSummary{}
	if portfolioRisk != nil {
		portfolioRiskSummary = OperatorPortfolioRiskSummary{
			Available:       true,
			LastEvaluatedAt: formatRFC3339(portfolioRisk.EvaluatedAt),
			Outcome:         portfolioRisk.Outcome,
			Summary:         portfolioRisk.Summary,
			Metrics:         portfolioRisk.Metrics.Clone(),
		}
	}

	killSwitchSummary := OperatorKillSwitchSummary{
		Available:           killSwitch.Available,
		Active:              killSwitch.Active,
		Source:              killSwitch.Source,
		Message:             killSwitch.Message,
		FilePath:            killSwitch.FilePath,
		TriggeredAt:         formatRFC3339(killSwitch.TriggeredAt),
		LastCheckedAt:       formatRFC3339(killSwitch.LastCheckedAt),
		LastClearedAt:       formatRFC3339(killSwitch.LastClearedAt),
		OrdersCancelled:     killSwitch.OrdersCancelled,
		LastCancelAttemptAt: formatRFC3339(killSwitch.LastCancelAttemptAt),
		LastCancelError:     killSwitch.LastCancelError,
		ActivationCount:     killSwitch.ActivationCount,
	}

	operatorAlerts := OperatorAlertsSummary{
		Available:     true,
		TotalCount:    alertSummary.TotalCount,
		CriticalCount: alertSummary.CriticalCount,
		WarningCount:  alertSummary.WarningCount,
		InfoCount:     alertSummary.InfoCount,
		LastAlertAt:   alertSummary.LastAlertAt,
		Recent:        append([]alerts.Alert(nil), alertSummary.Recent...),
	}
	operatorIncidents := OperatorIncidentsSummary{
		Available:                 true,
		OpenCount:                 incidentSummary.OpenCount,
		AcknowledgedCount:         incidentSummary.AcknowledgedCount,
		CriticalOpenCount:         incidentSummary.CriticalOpenCount,
		LatestIncidentAt:          formatRFC3339(incidentSummary.LatestIncidentAt),
		LatestIncidentSummary:     incidentSummary.LatestIncidentSummary,
		LatestIncidentSeverity:    incidentSummary.LatestIncidentSeverity,
		LatestIncidentRunbookHint: incidentSummary.LatestIncidentRunbookHint,
		OpenIncidents:             append([]incidents.Incident(nil), incidentSummary.OpenIncidents...),
		RecentResolvedIncidents:   append([]incidents.Incident(nil), incidentSummary.RecentResolvedIncidents...),
	}
	if incidentSummary.LatestCriticalIncident != nil {
		cloned := incidentSummary.LatestCriticalIncident.Clone()
		operatorIncidents.LatestCriticalIncident = &cloned
	}

	tradingGate := OperatorTradingGateSummary{
		Allowed:         gate.TradingAllowed,
		EntriesAllowed:  gate.EntriesAllowed,
		ExitsAllowed:    gate.ExitsAllowed,
		ReduceOnly:      gate.ReduceOnly,
		Mode:            gate.Mode,
		BlockReason:     gate.BlockReason,
		BlockingReasons: append([]string(nil), gate.BlockingReasons...),
		Message:         gate.Message,
	}

	return OperatorStatusSummary{
		StatusSchemaVersion:             operatorStatusSchemaVersion,
		TraderID:                        at.id,
		TraderName:                      at.name,
		Mode:                            at.config.Mode,
		Exchange:                        at.exchange,
		Broker:                          at.config.Broker,
		StrategyMode:                    at.config.StrategyMode,
		DecisionArchitecture:            at.canonicalDecisionArchitecture(),
		TradingAllowed:                  gate.TradingAllowed,
		EntriesAllowed:                  gate.EntriesAllowed,
		ExitsAllowed:                    gate.ExitsAllowed,
		ReduceOnly:                      gate.ReduceOnly,
		TradingBlockReason:              gate.BlockReason,
		BlockingReasons:                 append([]string(nil), gate.BlockingReasons...),
		OperatorMessage:                 gate.Message,
		Readiness:                       readinessSummary,
		BrokerRuntime:                   brokerSummary,
		Promotion:                       promotionSummary,
		RiskSupervisor:                  riskSupervisorSummary,
		Execution:                       executionSummary,
		Universe:                        universeSummary,
		Protection:                      protectionSummary,
		ShadowMode:                      shadowSummary,
		RestartRecovery:                 restartRecoverySummary,
		BrokerTruth:                     brokerTruthSummary,
		DeploymentValidation:            deploymentValidationSummary,
		Session:                         sessionSummary,
		EventJournal:                    eventJournalSummary,
		Runtime:                         runtimeSummary,
		TradingGate:                     tradingGate,
		OrderReconciliation:             orderReconSummary,
		PositionReconciliation:          positionReconSummary,
		DataQuality:                     dataQualitySummary,
		PortfolioRisk:                   portfolioRiskSummary,
		KillSwitch:                      killSwitchSummary,
		Alerts:                          operatorAlerts,
		Incidents:                       operatorIncidents,
		AIModel:                         at.aiModel,
		IsRunning:                       runtimeSummary.IsRunning,
		StartTime:                       runtimeSummary.StartTime,
		RuntimeMinutes:                  runtimeSummary.RuntimeMinutes,
		CallCount:                       runtimeSummary.CallCount,
		InitialBalance:                  runtimeSummary.InitialBalance,
		ScanInterval:                    runtimeSummary.ScanInterval,
		StopUntil:                       runtimeSummary.StopUntil,
		LastResetTime:                   runtimeSummary.LastResetTime,
		AIProvider:                      runtimeSummary.AIProvider,
		IsDemoMode:                      runtimeSummary.IsDemoMode,
		DemoLastCycleTime:               runtimeSummary.DemoLastCycleTime,
		ReadinessStatus:                 readinessSummary.Status,
		ReadinessMessage:                readinessSummary.Message,
		ReadinessCheckedAt:              readinessSummary.CheckedAt,
		ReadinessTradingAllowed:         readinessSummary.TradingAllowed,
		ReadinessPassCount:              readinessSummary.PassCount,
		ReadinessWarnCount:              readinessSummary.WarnCount,
		ReadinessFailCount:              readinessSummary.FailCount,
		ReadinessChecks:                 append([]ReadinessCheck(nil), readinessSummary.Checks...),
		PromotionStatus:                 promotionSummary.Status,
		PromotionMessage:                promotionSummary.Message,
		PromotionCheckedAt:              promotionSummary.CheckedAt,
		PromotionRequired:               promotionSummary.Required,
		PromotionLiveTradingAllowed:     promotionSummary.LiveTradingAllowed,
		PromotionPassCount:              promotionSummary.PassCount,
		PromotionWarnCount:              promotionSummary.WarnCount,
		PromotionFailCount:              promotionSummary.FailCount,
		PromotionChecks:                 append([]PromotionCheck(nil), promotionSummary.Checks...),
		RiskSupervisorMode:              string(riskSupervisorSummary.Mode),
		RiskSupervisorSummary:           riskSupervisorSummary.Summary,
		RiskSupervisorActiveIncidents:   riskSupervisorSummary.ActiveIncidentCount,
		RiskSupervisorCriticalIncidents: riskSupervisorSummary.CriticalIncidentCount,
		ExecutionAvailable:              executionSummary.Available,
		ExecutionInFlightCount:          executionSummary.InFlightCount,
		ExecutionStaleCount:             executionSummary.StaleCount,
		ExecutionLastAt:                 executionSummary.LastExecutionAt,
		ExecutionLastSymbol:             executionSummary.LastExecutionSymbol,
		ExecutionLastStatus:             string(executionSummary.LastExecutionStatus),
		ExecutionDuplicateSuppressed:    executionSummary.DuplicateSuppressedCount,
		ExecutionBlockedCount:           executionSummary.BlockedExecutionCount,
		ExecutionSubmittedCount:         executionSummary.SubmittedCount,
		ExecutionAcknowledgedCount:      executionSummary.AcknowledgedCount,
		ExecutionFilledCount:            executionSummary.FilledCount,
		ExecutionRejectedCount:          executionSummary.RejectedCount,
		ExecutionFailedCount:            executionSummary.FailedCount,
		UniverseAvailable:               universeSummary.Available,
		UniverseSelectionMode:           universeSummary.SelectionMode,
		UniverseConfiguredSource:        universeSummary.ConfiguredSource,
		UniverseConfiguredCount:         universeSummary.ConfiguredSymbolsCount,
		UniverseEffectiveCount:          universeSummary.EffectiveSymbolsCount,
		UniverseManifestPath:            universeSummary.ManifestPath,
		UniverseMessage:                 universeSummary.Message,
		ProtectionPendingCount:          protectionSummary.PendingCount,
		ProtectionActiveCount:           protectionSummary.ActiveProtectiveCount,
		ProtectionMessage:               protectionSummary.Message,
		ShadowModeActive:                shadowSummary.Active,
		ShadowDecisionCount:             shadowSummary.DecisionCount,
		ShadowWouldTradeCount:           shadowSummary.WouldTradeCount,
		ShadowBlockedCount:              shadowSummary.BlockedCount,
		ShadowRealizedPnL:               shadowSummary.HypotheticalRealizedPnL,
		ShadowUnrealizedPnL:             shadowSummary.HypotheticalUnrealizedPnL,
		BrokerState:                     brokerSummary.State,
		BrokerStateReason:               brokerSummary.Reason,
		BrokerLastError:                 brokerSummary.LastError,
		BrokerStateSince:                brokerSummary.StateSince,
		BrokerLastHealthyAt:             brokerSummary.LastHealthyAt,
		BrokerLastReconciledAt:          brokerSummary.LastReconciledAt,
		BrokerReconnectAttempts:         brokerSummary.ReconnectAttempts,
		BrokerNextRetryAt:               brokerSummary.NextRetryAt,
		BrokerRecoveryActive:            brokerSummary.RecoveryActive,
		BrokerTradingAllowed:            brokerSummary.TradingAllowed,
		BrokerTruthAvailable:            brokerTruthSummary.Available,
		BrokerTruthRequired:             brokerTruthSummary.Required,
		BrokerTruthVerified:             brokerTruthSummary.Verified,
		BrokerTruthTradingBlocked:       brokerTruthSummary.TradingBlocked,
		BrokerTruthEntriesRestricted:    brokerTruthSummary.EntriesRestricted,
		BrokerTruthMessage:              brokerTruthSummary.Message,
		BrokerTruthRestrictionReason:    brokerTruthSummary.RestrictionReason,
		DeploymentValidationRequired:    deploymentValidationSummary.Required,
		DeploymentValidationPassed:      deploymentValidationSummary.Passed,
		DeploymentValidationFresh:       deploymentValidationSummary.Fresh,
		DeploymentValidationMessage:     deploymentValidationSummary.Message,
		LastSessionReportPath:           sessionSummary.LastSessionReportPath,
		LastSessionReportStatus:         sessionSummary.LastSessionReportStatus,
		LastSessionReportAt:             sessionSummary.LastSessionReportAt,
		OrderReconciliationAvailable:    orderReconSummary.Available,
		OrderReconciliationLastRunAt:    orderReconSummary.LastRunAt,
		OrderReconciliationLastError:    orderReconSummary.LastError,
		OrderReconciliationTotalRuns:    orderReconSummary.TotalRuns,
		OrderReconciliationMismatches:   orderReconSummary.TotalMismatches,
		OrderReconciliationRepairs:      orderReconSummary.TotalRepairs,
		OrderReconciliationInferred:     orderReconSummary.TotalInferredOutcomes,
		OrderReconciliationUnresolved:   orderReconSummary.TotalUnresolvedOutcomes,
		OrderReconciliationDegraded:     orderReconSummary.ConfidenceDegraded,
		OrderReconciliationSummary:      orderReconSummary.LastSummary,
		PositionReconciliationAvailable: positionReconSummary.Available,
		PositionReconciliationStatus:    string(positionReconSummary.Status),
		PositionReconciliationLastRunAt: positionReconSummary.LastRunAt,
		PositionReconciliationLastError: positionReconSummary.LastError,
		PositionReconciliationTotalRuns: positionReconSummary.TotalRuns,
		PositionReconciliationIncidents: positionReconSummary.TotalIncidents,
		PositionReconciliationSummary:   positionReconSummary.Summary,
		DataQualityAvailable:            dataQualitySummary.Available,
		DataQualityLastCheckedAt:        dataQualitySummary.LastCheckedAt,
		DataQualityBlockedSymbols:       dataQualitySummary.BlockedSymbolsCount,
		DataQualityFailures:             dataQualitySummary.TotalFailures,
		DataQualityFeedDelayed:          dataQualitySummary.FeedDelayed,
		DataQualityFeedSummary:          dataQualitySummary.FeedSummary,
		PortfolioRiskAvailable:          portfolioRiskSummary.Available,
		PortfolioRiskLastEvaluatedAt:    portfolioRiskSummary.LastEvaluatedAt,
		PortfolioRiskOutcome:            string(portfolioRiskSummary.Outcome),
		PortfolioRiskSummary:            portfolioRiskSummary.Summary,
		PortfolioGrossExposurePct:       portfolioRiskSummary.Metrics.CurrentGrossExposurePct,
		PortfolioNetExposurePct:         portfolioRiskSummary.Metrics.CurrentNetExposurePct,
		PortfolioLargestSector:          portfolioRiskSummary.Metrics.LargestSector,
		PortfolioLargestSectorPct:       portfolioRiskSummary.Metrics.LargestSectorExposurePct,
		PortfolioCorrelatedPositions:    portfolioRiskSummary.Metrics.CorrelatedPositionCount,
		PortfolioMaxCorrelation:         portfolioRiskSummary.Metrics.MaxObservedCorrelation,
		PortfolioCurrentDrawdownPct:     portfolioRiskSummary.Metrics.CurrentDrawdownPct,
		KillSwitchActive:                killSwitchSummary.Active,
		KillSwitchSource:                killSwitchSummary.Source,
		KillSwitchMessage:               killSwitchSummary.Message,
		KillSwitchFilePath:              killSwitchSummary.FilePath,
		KillSwitchTriggeredAt:           killSwitchSummary.TriggeredAt,
		KillSwitchLastCheckedAt:         killSwitchSummary.LastCheckedAt,
		KillSwitchOrdersCancelled:       killSwitchSummary.OrdersCancelled,
		KillSwitchLastCancelError:       killSwitchSummary.LastCancelError,
		RecentAlerts:                    append([]alerts.Alert(nil), operatorAlerts.Recent...),
		AlertCount:                      operatorAlerts.TotalCount,
		CriticalAlertCount:              operatorAlerts.CriticalCount,
		WarningAlertCount:               operatorAlerts.WarningCount,
		InfoAlertCount:                  operatorAlerts.InfoCount,
		LastAlertAt:                     operatorAlerts.LastAlertAt,
		LatestIncidentSummary:           operatorIncidents.LatestIncidentSummary,
		LatestIncidentSeverity:          string(operatorIncidents.LatestIncidentSeverity),
		LatestIncidentRunbookHint:       operatorIncidents.LatestIncidentRunbookHint,
	}
}

func primaryOrderIssueField(issue *orders.Issue, field string) string {
	if issue == nil {
		return ""
	}
	switch field {
	case "local_id":
		return issue.LocalID
	case "broker_id":
		return issue.BrokerOrderID
	case "authority":
		return string(issue.Authority)
	case "confidence":
		return string(issue.Confidence)
	case "message":
		return issue.Message
	default:
		return ""
	}
}

func runtimeMinutesSince(start time.Time) int {
	if start.IsZero() {
		return 0
	}
	return int(time.Since(start).Minutes())
}

func formatRFC3339(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}
