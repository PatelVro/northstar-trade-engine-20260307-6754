package trader

import (
	"northstar/startup"
	"strings"
	"time"
)

type ModeParityProfile string

const (
	ModeParityProfileDemoSynthetic      ModeParityProfile = "demo_synthetic"
	ModeParityProfileReplayHistorical   ModeParityProfile = "replay_historical"
	ModeParityProfileShadowHypothetical ModeParityProfile = "shadow_hypothetical_execution"
	ModeParityProfilePaperSimulated     ModeParityProfile = "paper_simulated_execution"
	ModeParityProfilePaperBrokerManaged ModeParityProfile = "paper_broker_managed"
	ModeParityProfileLiveBrokerManaged  ModeParityProfile = "live_broker_managed"
)

type ModeParitySummary struct {
	Available                     bool              `json:"available"`
	Mode                          string            `json:"mode"`
	Profile                       ModeParityProfile `json:"profile"`
	Summary                       string            `json:"summary"`
	CanonicalDecisionArchitecture string            `json:"canonical_decision_architecture"`
	ExplicitUniverse              bool              `json:"explicit_universe"`
	RestartRecoveryEnabled        bool              `json:"restart_recovery_enabled"`
	EventJournalEnabled           bool              `json:"event_journal_enabled"`
	RealMarketDataConfigured      bool              `json:"real_market_data_configured"`
	RealMarketDataVerified        bool              `json:"real_market_data_verified"`
	BrokerManagedExecution        bool              `json:"broker_managed_execution"`
	BrokerManagedOrderTruth       bool              `json:"broker_managed_order_truth"`
	BrokerManagedPositionTruth    bool              `json:"broker_managed_position_truth"`
	BrokerManagedAccountTruth     bool              `json:"broker_managed_account_truth"`
	BrokerTruthPreflightReady     bool              `json:"broker_truth_preflight_ready"`
	HypotheticalExecution         bool              `json:"hypothetical_execution"`
	ShadowPortfolio               bool              `json:"shadow_portfolio"`
	LiveCapitalAtRisk             bool              `json:"live_capital_at_risk"`
	DeploymentValidationRequired  bool              `json:"deployment_validation_required"`
	DeploymentValidationPassed    bool              `json:"deployment_validation_passed"`
	PromotionRequired             bool              `json:"promotion_required"`
	PromotionPassed               bool              `json:"promotion_passed"`
	ProvenCount                   int               `json:"proven_count"`
	GapCount                      int               `json:"gap_count"`
	WarningCount                  int               `json:"warning_count"`
	Proven                        []string          `json:"proven"`
	Gaps                          []string          `json:"gaps"`
	Warnings                      []string          `json:"warnings,omitempty"`
}

func (at *AutoTrader) currentModeParitySummary() ModeParitySummary {
	brokerTruth := at.currentBrokerTruthSummary()
	deploymentValidation := startup.CurrentLiveValidationStatus("", strings.EqualFold(at.config.Mode, "live"), time.Now())
	promotion := at.getPromotionSummary()
	universe := at.currentUniverseSummary()
	restart := at.currentRestartRecoverySummary()
	journal := at.currentEventJournalSummary()

	summary := ModeParitySummary{
		Available:                     true,
		Mode:                          strings.ToLower(strings.TrimSpace(at.config.Mode)),
		Profile:                       at.currentModeParityProfile(),
		CanonicalDecisionArchitecture: at.canonicalDecisionArchitecture(),
		ExplicitUniverse:              universe.Available && !strings.EqualFold(strings.TrimSpace(universe.SelectionMode), "dynamic_merged_pool"),
		RestartRecoveryEnabled:        restart.Available,
		EventJournalEnabled:           journal.Available,
		RealMarketDataConfigured:      at.usesExternalRealMarketDataPath(),
		RealMarketDataVerified:        brokerTruth.MarketDataVerified && brokerTruth.MarketDataFresh,
		BrokerManagedExecution:        brokerTruth.BrokerManaged,
		BrokerManagedOrderTruth:       brokerTruth.OrdersVerified && brokerTruth.OrdersFresh,
		BrokerManagedPositionTruth:    brokerTruth.PositionsVerified && brokerTruth.PositionsFresh,
		BrokerManagedAccountTruth:     brokerTruth.AccountVerified && brokerTruth.AccountFresh,
		BrokerTruthPreflightReady:     brokerTruth.PreflightReady,
		HypotheticalExecution:         at.usesHypotheticalExecutionEvidence(),
		ShadowPortfolio:               at.shadowModeEnabled(),
		LiveCapitalAtRisk:             strings.EqualFold(at.config.Mode, "live") && brokerTruth.BrokerManaged && !at.demoMode,
		DeploymentValidationRequired:  deploymentValidation.Required,
		DeploymentValidationPassed:    deploymentValidation.Required && deploymentValidation.Passed && deploymentValidation.Fresh && deploymentValidation.ConfigMatches,
		PromotionRequired:             promotion.Required,
		PromotionPassed:               promotion.Required && promotion.LiveTradingAllowed,
		Proven:                        []string{},
		Gaps:                          []string{},
		Warnings:                      []string{},
	}

	if strings.EqualFold(summary.CanonicalDecisionArchitecture, "equity_generator_plus_canonical_pipeline") {
		summary.Proven = append(summary.Proven, "runtime uses the canonical equity decision dispatch")
	}
	if summary.ExplicitUniverse {
		summary.Proven = append(summary.Proven, "effective trading universe is explicit and operator-visible")
	}
	if summary.RestartRecoveryEnabled {
		summary.Proven = append(summary.Proven, "restart recovery checkpointing is active")
	}
	if summary.EventJournalEnabled {
		summary.Proven = append(summary.Proven, "append-only safety journal is active")
	}
	if summary.RealMarketDataVerified {
		summary.Proven = append(summary.Proven, "real market-data path has current preflight evidence")
	}
	if summary.BrokerManagedExecution {
		summary.Proven = append(summary.Proven, "broker-managed execution path is active")
	}
	if summary.BrokerTruthPreflightReady && summary.BrokerManagedAccountTruth && summary.BrokerManagedOrderTruth && summary.BrokerManagedPositionTruth {
		summary.Proven = append(summary.Proven, "broker account, order, and position truth preflight is clean for the active mode")
	}
	if summary.DeploymentValidationPassed {
		summary.Proven = append(summary.Proven, "live deployment validation passed")
	}
	if summary.PromotionPassed {
		summary.Proven = append(summary.Proven, "live promotion evidence passed")
	}
	if summary.LiveCapitalAtRisk {
		summary.Proven = append(summary.Proven, "active mode exercises live-capital order flow")
	}

	switch summary.Profile {
	case ModeParityProfileDemoSynthetic:
		summary.Gaps = append(summary.Gaps,
			"demo mode uses synthetic market and execution behavior",
			"demo evidence does not prove broker-managed execution, account truth, or live capital behavior",
		)
	case ModeParityProfileReplayHistorical:
		summary.Gaps = append(summary.Gaps,
			"replay mode uses historical data and does not prove live market timing",
			"replay evidence does not prove broker-managed execution, reconciliation, or live capital behavior",
		)
	case ModeParityProfileShadowHypothetical:
		summary.Gaps = append(summary.Gaps,
			"shadow mode does not submit broker-managed orders",
			"shadow portfolio and execution outcomes are hypothetical rather than broker-confirmed",
			"shadow evidence does not prove broker-managed account, order, or position truth",
		)
	case ModeParityProfilePaperSimulated:
		summary.Gaps = append(summary.Gaps,
			"paper mode with simulated execution does not prove broker-managed lifecycle or reconciliation truth",
			"simulated paper evidence does not prove live-capital behavior",
		)
	case ModeParityProfilePaperBrokerManaged:
		summary.Gaps = append(summary.Gaps,
			"paper mode does not put live capital at risk",
			"paper evidence does not prove live deployment validation or live promotion behavior",
		)
	case ModeParityProfileLiveBrokerManaged:
		// Live mode has the smallest structural parity gap; active warnings handle anything still unproven.
	default:
		summary.Gaps = append(summary.Gaps, "active mode parity is not fully classified")
	}

	if summary.RealMarketDataConfigured && !summary.RealMarketDataVerified {
		summary.Warnings = append(summary.Warnings, "real market-data path is configured but not currently verified by preflight")
	}
	if summary.BrokerManagedExecution && !summary.BrokerTruthPreflightReady {
		summary.Warnings = append(summary.Warnings, "broker-managed mode is active but broker/account/order/position truth preflight is not fully clean")
	}
	if summary.BrokerManagedExecution && brokerTruth.ConfidenceDegraded {
		summary.Warnings = append(summary.Warnings, firstNonEmpty(strings.TrimSpace(brokerTruth.RestrictionReason), strings.TrimSpace(brokerTruth.Message), "broker-managed execution truth is degraded by reconciliation-inferred outcomes"))
	}
	if summary.DeploymentValidationRequired && !summary.DeploymentValidationPassed {
		summary.Warnings = append(summary.Warnings, firstNonEmpty(strings.TrimSpace(deploymentValidation.Message), "live deployment validation has not passed"))
	}
	if summary.PromotionRequired && !summary.PromotionPassed {
		summary.Warnings = append(summary.Warnings, firstNonEmpty(strings.TrimSpace(promotion.Message), "live promotion evidence has not passed"))
	}

	summary.ProvenCount = len(summary.Proven)
	summary.GapCount = len(summary.Gaps)
	summary.WarningCount = len(summary.Warnings)
	summary.Summary = at.modeParitySummaryMessage(summary)
	return summary
}

func (at *AutoTrader) currentModeParityProfile() ModeParityProfile {
	switch {
	case at == nil:
		return ModeParityProfilePaperSimulated
	case at.demoMode:
		return ModeParityProfileDemoSynthetic
	case strings.EqualFold(at.config.Mode, "replay"):
		return ModeParityProfileReplayHistorical
	case at.shadowModeEnabled():
		return ModeParityProfileShadowHypothetical
	case strings.EqualFold(at.config.Mode, "live") && at.requiresBrokerDependency() && !strings.EqualFold(at.config.Broker, "sim"):
		return ModeParityProfileLiveBrokerManaged
	case strings.EqualFold(at.config.Mode, "paper") && at.requiresBrokerDependency() && !strings.EqualFold(at.config.Broker, "sim"):
		return ModeParityProfilePaperBrokerManaged
	default:
		return ModeParityProfilePaperSimulated
	}
}

func (at *AutoTrader) usesExternalRealMarketDataPath() bool {
	if at == nil || at.demoMode {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(at.config.DataProvider)) {
	case "ibkr", "alpaca", "binance", "hyperliquid", "aster":
		return true
	case "csv", "demo", "":
		// Fall through to exchange check for empty provider.
	}
	switch strings.ToLower(strings.TrimSpace(at.exchange)) {
	case "ibkr", "alpaca", "binance", "hyperliquid", "aster":
		return !strings.EqualFold(at.config.Mode, "replay")
	case "demo":
		return false
	default:
		return false
	}
}

func (at *AutoTrader) usesHypotheticalExecutionEvidence() bool {
	if at == nil {
		return false
	}
	return at.demoMode ||
		at.shadowModeEnabled() ||
		strings.EqualFold(at.config.Broker, "sim") ||
		strings.EqualFold(at.config.Mode, "replay")
}

func (at *AutoTrader) modeParitySummaryMessage(summary ModeParitySummary) string {
	base := ""
	switch summary.Profile {
	case ModeParityProfileDemoSynthetic:
		base = "demo mode exercises synthetic runtime behavior only"
	case ModeParityProfileReplayHistorical:
		base = "replay mode exercises historical strategy/runtime behavior without proving live market or broker behavior"
	case ModeParityProfileShadowHypothetical:
		base = "shadow mode can prove runtime decision and market-data behavior, but not broker-managed execution truth"
	case ModeParityProfilePaperSimulated:
		base = "paper mode is using simulated execution, so it does not prove broker-managed lifecycle truth"
	case ModeParityProfilePaperBrokerManaged:
		base = "paper mode exercises broker-managed paper execution, but it does not prove live-capital deployment"
	case ModeParityProfileLiveBrokerManaged:
		base = "live mode exercises broker-managed execution with live-capital risk when deployment and broker truth remain clean"
	default:
		base = "mode parity evidence is only partially classified"
	}
	if len(summary.Warnings) > 0 {
		base += "; current evidence warning: " + summary.Warnings[0]
	}
	return base
}
