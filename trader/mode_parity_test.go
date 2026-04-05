package trader

import (
	"northstar/audit"
	"northstar/orders"
	"northstar/startup"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newModeParityTestTrader(t *testing.T, mode, brokerName string) *AutoTrader {
	t.Helper()

	dir := t.TempDir()
	now := time.Now()
	trader := &AutoTrader{
		id:       "mode_parity_trader",
		name:     "Mode Parity Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		trader: &brokerTruthTestTrader{
			orderSummary: orders.Summary{
				LastRunAt:              now,
				LastSuccessAt:          now,
				LastSummary:            "order reconciliation clean",
				CurrentConfirmedOrders: 1,
			},
		},
		config: AutoTraderConfig{
			ID:               "mode_parity_trader",
			Name:             "Mode Parity Trader",
			Mode:             mode,
			Broker:           brokerName,
			DataProvider:     "ibkr",
			InstrumentType:   "equity",
			StrategyMode:     "momentum_only",
			ScanInterval:     3 * time.Minute,
			InitialBalance:   100000,
			BenchmarkSymbols: []string{"SPY", "QQQ"},
		},
		initialBalance: 100000,
		eventJournal: audit.NewJournal(filepath.Join(dir, "output", "audit"), audit.Metadata{
			TraderID:     "mode_parity_trader",
			TraderName:   "Mode Parity Trader",
			Mode:         mode,
			Broker:       brokerName,
			StrategyMode: "momentum_only",
		}),
	}
	trader.isRunning.Store(true)
	trader.initializeBrokerRuntimeState()
	trader.initializeDataQualityState()
	trader.initializeRestartRecoveryState()
	trader.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now,
		TradingAllowed: true,
		PassCount:      4,
	})
	trader.universeState = runtimeUniverseState{
		Available:         true,
		InstrumentType:    "equity",
		SelectionMode:     "explicit_configured_equity",
		ConfiguredSource:  "default_coins+default_coins_file",
		ConfiguredSymbols: []string{"AAPL", "MSFT", "NVDA"},
		EffectiveSymbols:  []string{"AAPL", "MSFT", "NVDA"},
		ManifestPersisted: true,
		ManifestPath:      filepath.Join(dir, "output", "universe", "mode_parity_trader", "active_universe.json"),
		LastUpdatedAt:     now,
		Message:           "explicit equity trading universe resolved",
	}
	trader.positionReconSummary = freshPositionReconSummary(now)
	trader.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AccountCash:            100000,
		AvailableBalance:       100000,
		PositionCount:          0,
	}, freshBrokerPositionViews())
	trader.updateMarketDataFeedStatus(false, "", []string{"AAPL", "MSFT"})
	return trader
}

func TestCurrentModeParitySummary_ShadowHighlightsHypotheticalExecution(t *testing.T) {
	at := newModeParityTestTrader(t, "shadow", "ibkr")
	at.initializeShadowModeState()

	summary := at.currentModeParitySummary()
	if summary.Profile != ModeParityProfileShadowHypothetical {
		t.Fatalf("expected shadow profile, got %s", summary.Profile)
	}
	if !summary.HypotheticalExecution || !summary.ShadowPortfolio {
		t.Fatalf("expected shadow mode to report hypothetical execution and shadow portfolio, got %+v", summary)
	}
	if summary.BrokerManagedExecution {
		t.Fatalf("shadow mode should not claim broker-managed execution truth")
	}
	if !summary.RealMarketDataVerified {
		t.Fatalf("expected IBKR shadow market-data preflight to count as verified")
	}
	if !containsString(summary.Gaps, "shadow mode does not submit broker-managed orders") {
		t.Fatalf("expected shadow execution gap, got %+v", summary.Gaps)
	}
}

func TestCurrentModeParitySummary_PaperBrokerManagedMakesLiveGapExplicit(t *testing.T) {
	at := newModeParityTestTrader(t, "paper", "ibkr")

	summary := at.currentModeParitySummary()
	if summary.Profile != ModeParityProfilePaperBrokerManaged {
		t.Fatalf("expected broker-managed paper profile, got %s", summary.Profile)
	}
	if !summary.BrokerManagedExecution || !summary.BrokerTruthPreflightReady {
		t.Fatalf("expected broker-managed paper truth to be preflight ready, got %+v", summary)
	}
	if summary.LiveCapitalAtRisk {
		t.Fatalf("paper mode must not claim live capital risk")
	}
	if !containsString(summary.Gaps, "paper mode does not put live capital at risk") {
		t.Fatalf("expected live-capital gap for paper mode, got %+v", summary.Gaps)
	}
}

func TestCurrentModeParitySummary_LiveSurfacesDeploymentAndPromotionEvidence(t *testing.T) {
	now := time.Now()
	activeConfig := filepath.Join(t.TempDir(), "config_ibkr_live.json")
	t.Setenv(startup.EnvActiveConfigFile, activeConfig)
	t.Setenv(startup.EnvLiveValidationPassed, "true")
	t.Setenv(startup.EnvLiveValidationConfig, activeConfig)
	t.Setenv(startup.EnvLiveValidationCheckedAt, now.UTC().Format(time.RFC3339Nano))
	t.Setenv(startup.EnvLiveValidationSource, "run_ibkr_live.cmd")

	at := newModeParityTestTrader(t, "live", "ibkr")
	at.config.StrictLiveMode = true
	at.setPromotionSummary(PromotionSummary{
		Status:             PromotionPass,
		Message:            "live promotion checklist passed",
		CheckedAt:          now,
		Required:           true,
		LiveTradingAllowed: true,
		PassCount:          3,
	})

	summary := at.currentModeParitySummary()
	if summary.Profile != ModeParityProfileLiveBrokerManaged {
		t.Fatalf("expected live broker-managed profile, got %s", summary.Profile)
	}
	if !summary.LiveCapitalAtRisk {
		t.Fatalf("expected live mode to report live capital at risk")
	}
	if !summary.DeploymentValidationPassed || !summary.PromotionPassed {
		t.Fatalf("expected live mode to surface deployment+promotion evidence, got %+v", summary)
	}
	if strings.Contains(strings.ToLower(summary.Summary), "does not prove live-capital deployment") {
		t.Fatalf("live mode summary should not report paper-style live capital gap: %s", summary.Summary)
	}
}

func TestGetOperatorStatus_ExposesModeParitySummary(t *testing.T) {
	at := newModeParityTestTrader(t, "paper", "ibkr")

	status := at.GetOperatorStatus()
	if status.ModeParity.Profile != ModeParityProfilePaperBrokerManaged {
		t.Fatalf("expected nested mode parity profile, got %s", status.ModeParity.Profile)
	}
	if status.ModeParityProfile != string(ModeParityProfilePaperBrokerManaged) {
		t.Fatalf("expected compatibility mode parity profile, got %q", status.ModeParityProfile)
	}
	if status.ModeParityGapCount == 0 {
		t.Fatalf("expected paper mode parity gap count to be non-zero")
	}
	if !strings.Contains(strings.ToLower(status.ModeParitySummary), "paper mode exercises broker-managed paper execution") {
		t.Fatalf("unexpected mode parity summary: %q", status.ModeParitySummary)
	}
}

func TestNewPaperSessionTrackerIncludesModeParitySummary(t *testing.T) {
	at := newModeParityTestTrader(t, "shadow", "ibkr")
	at.initializeShadowModeState()

	tracker := newPaperSessionTracker(at, time.Now())
	if tracker.report.ModeParity.Profile != ModeParityProfileShadowHypothetical {
		t.Fatalf("expected session report mode parity profile shadow_hypothetical_execution, got %s", tracker.report.ModeParity.Profile)
	}
	if tracker.report.ModeParity.GapCount == 0 {
		t.Fatalf("expected shadow session report to include explicit parity gaps")
	}
	if !containsString(tracker.report.ModeParity.Gaps, "shadow portfolio and execution outcomes are hypothetical rather than broker-confirmed") {
		t.Fatalf("expected shadow session report to preserve hypothetical execution gap, got %+v", tracker.report.ModeParity.Gaps)
	}
}
