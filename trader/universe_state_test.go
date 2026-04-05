package trader

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"northstar/logger"
)

func TestInitializeTradingUniverseUsesExplicitEquityConfig(t *testing.T) {
	at := &AutoTrader{
		id:   "equity_trader",
		name: "Equity Trader",
		config: AutoTraderConfig{
			ID:                           "equity_trader",
			Name:                         "Equity Trader",
			Mode:                         "paper",
			Broker:                       "sim",
			InstrumentType:               "equity",
			ConfiguredDefaultSymbols:     []string{"AAPL", "MSFT", "NVDA", "AAPL"},
			ConfiguredDefaultSymbolsFile: "data/universe/us_companies.txt",
			BenchmarkSymbols:             []string{"SPY", "QQQ"},
		},
	}

	if err := at.initializeTradingUniverse(); err != nil {
		t.Fatalf("initializeTradingUniverse failed: %v", err)
	}

	summary := at.currentUniverseSummary()
	if summary.SelectionMode != "explicit_configured_equity" {
		t.Fatalf("expected explicit equity universe mode, got %q", summary.SelectionMode)
	}
	if summary.ConfiguredSource != "default_coins + default_coins_file" {
		t.Fatalf("unexpected configured source: %q", summary.ConfiguredSource)
	}
	want := []string{"AAPL", "MSFT", "NVDA"}
	if len(summary.EffectiveSymbols) != len(want) {
		t.Fatalf("expected %d effective symbols, got %d", len(want), len(summary.EffectiveSymbols))
	}
	for i, symbol := range want {
		if summary.EffectiveSymbols[i] != symbol {
			t.Fatalf("expected effective symbol %d to be %s, got %s", i, symbol, summary.EffectiveSymbols[i])
		}
	}
}

func TestInitializeTradingUniverseAppliesTrustedSymbolFilter(t *testing.T) {
	at := &AutoTrader{
		id:   "trusted_trader",
		name: "Trusted Trader",
		config: AutoTraderConfig{
			ID:                       "trusted_trader",
			Name:                     "Trusted Trader",
			Mode:                     "paper",
			Broker:                   "sim",
			InstrumentType:           "equity",
			ConfiguredDefaultSymbols: []string{"AAPL", "MSFT", "NVDA"},
			TrustedSymbolsFile:       "data/universe/us_canada_tradable_core.txt",
		},
		trustedSymbolSet: map[string]struct{}{
			"AAPL": {},
			"NVDA": {},
		},
	}

	if err := at.initializeTradingUniverse(); err != nil {
		t.Fatalf("initializeTradingUniverse failed: %v", err)
	}

	summary := at.currentUniverseSummary()
	want := []string{"AAPL", "NVDA"}
	if len(summary.EffectiveSymbols) != len(want) {
		t.Fatalf("expected %d effective symbols, got %d", len(want), len(summary.EffectiveSymbols))
	}
	for i, symbol := range want {
		if summary.EffectiveSymbols[i] != symbol {
			t.Fatalf("expected effective symbol %d to be %s, got %s", i, symbol, summary.EffectiveSymbols[i])
		}
	}
	if summary.TrustedSymbolsCount != 2 {
		t.Fatalf("expected trusted symbol count 2, got %d", summary.TrustedSymbolsCount)
	}
}

func TestPersistTradingUniverseManifestWritesExplicitUniverse(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir tempdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	at := &AutoTrader{
		id:   "manifest_trader",
		name: "Manifest Trader",
		config: AutoTraderConfig{
			ID:                       "manifest_trader",
			Name:                     "Manifest Trader",
			Mode:                     "paper",
			Broker:                   "sim",
			InstrumentType:           "equity",
			ConfiguredDefaultSymbols: []string{"AAPL", "MSFT", "NVDA"},
		},
	}
	if err := at.initializeTradingUniverse(); err != nil {
		t.Fatalf("initializeTradingUniverse failed: %v", err)
	}

	at.persistTradingUniverseManifest()

	path := filepath.Join("output", "universe", "manifest_trader", "active_universe.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected manifest to exist at %s: %v", path, err)
	}
}

func TestGetOperatorStatusIncludesUniverseSummary(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:       "status_trader",
		name:     "Status Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:                           "status_trader",
			Name:                         "Status Trader",
			Mode:                         "paper",
			Broker:                       "sim",
			InstrumentType:               "equity",
			StrategyMode:                 "multi_factor",
			ScanInterval:                 5 * time.Minute,
			InitialBalance:               100000,
			ConfiguredDefaultSymbols:     []string{"AAPL", "MSFT", "NVDA"},
			ConfiguredDefaultSymbolsFile: "data/universe/us_companies.txt",
		},
		initialBalance: 100000,
		startTime:      now.Add(-20 * time.Minute),
	}
	at.isRunning.Store(true)
	if err := at.initializeTradingUniverse(); err != nil {
		t.Fatalf("initializeTradingUniverse failed: %v", err)
	}
	at.recordUniverseCycleSelection([]string{"AAPL", "MSFT"}, []string{"SPY"}, []string{"SPY", "AAPL", "MSFT"})
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now.Add(-5 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()

	status := at.GetOperatorStatus()
	if !status.Universe.Available {
		t.Fatalf("expected universe summary to be available")
	}
	if status.Universe.SelectionMode != "explicit_configured_equity" {
		t.Fatalf("unexpected universe selection mode: %q", status.Universe.SelectionMode)
	}
	if status.UniverseConfiguredCount != 3 || status.UniverseEffectiveCount != 3 {
		t.Fatalf("expected configured/effective universe counts to be 3, got %d/%d", status.UniverseConfiguredCount, status.UniverseEffectiveCount)
	}
	if len(status.Universe.LastCandidateWindow) != 2 || status.Universe.LastCandidateWindow[0] != "AAPL" {
		t.Fatalf("unexpected last candidate window: %+v", status.Universe.LastCandidateWindow)
	}
	if status.UniverseManifestPath == "" {
		t.Fatalf("expected universe manifest path to be populated")
	}
}

func TestBuildTradingContextUsesExplicitUniverseWindowForEquities(t *testing.T) {
	at := &AutoTrader{
		id:                   "context_trader",
		name:                 "Context Trader",
		config:               AutoTraderConfig{ID: "context_trader", Name: "Context Trader", Mode: "paper", Broker: "sim", InstrumentType: "equity", CandidateBatchSize: 2, ConfiguredDefaultSymbols: []string{"AAPL", "MSFT", "NVDA"}},
		demoMode:             true,
		demoEquity:           100000,
		demoAvailableBalance: 100000,
		demoRand:             nil,
		decisionLogger:       logger.NewDecisionLogger(filepath.Join(t.TempDir(), "decision_logs")),
	}
	if err := at.initializeTradingUniverse(); err != nil {
		t.Fatalf("initializeTradingUniverse failed: %v", err)
	}

	ctx, err := at.buildTradingContext()
	if err != nil {
		t.Fatalf("buildTradingContext failed: %v", err)
	}
	if len(ctx.CandidateCoins) != 2 {
		t.Fatalf("expected 2 candidate coins, got %d", len(ctx.CandidateCoins))
	}
	if ctx.CandidateCoins[0].Symbol != "AAPL" || ctx.CandidateCoins[1].Symbol != "MSFT" {
		t.Fatalf("expected first explicit universe window [AAPL MSFT], got %+v", ctx.CandidateCoins)
	}
}
