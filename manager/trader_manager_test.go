package manager

import (
	"northstar/config"
	"reflect"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

// fullIBKRTraderConfig returns a TraderConfig with every field populated
// to non-zero/non-default values for mapping verification.
func fullIBKRTraderConfig() config.TraderConfig {
	return config.TraderConfig{
		ID:                        "ibkr-test-01",
		Name:                      "IBKR Test Trader",
		Enabled:                   true,
		AIModel:                   "custom",
		Exchange:                  "ibkr",
		DemoMode:                  false,
		Mode:                      "paper",
		DataProvider:              "ibkr",
		Broker:                    "ibkr",
		InstrumentType:            "equity",
		IBKRGatewayURL:            "https://127.0.0.1:5002/v1/api",
		IBKRAccountID:             "U1234567",
		IBKRSessionCookie:         "sess-abc",
		StrictLiveMode:            true,
		LivePromotionApproved:     true,
		PromotionSourceTraderID:   "paper-01",
		MinPaperSessionReports:    3,
		PromotionMaxEvidenceAgeDays: 14,
		EmergencyKillSwitch:       true,
		KillSwitchFile:            "/tmp/killswitch",
		InitialBalance:            50000,
		ScanIntervalSeconds:       180,
		CustomAPIURL:              "http://ai.local/v1",
		CustomAPIKey:              "custom-key",
		CustomModelName:           "gpt-4o",
		DeepSeekKey:               "ds-key",
		QwenKey:                   "qw-key",
		BarsAdjustment:            "split",
		CandidateBatchSize:        10,
		TrustedSymbolsFile:        "symbols.txt",
		StrategyMode:              "hybrid_ai",
		MomentumMinScore:          1.5,
		FallbackPositionPct:       0.08,
		MinFactorScore:            0.4,
		RiskPerTradePct:           0.01,
		ProfitLockThreshold:       1.5,
		TrailingStopATRMult:       2.0,
		MaxHoldingCycles:          200,
		MaxConcurrentPos:          5,
		SymbolCooldownCycles:      8,
		MaxGrossExposure:          0.8,
		MaxPositionPct:            0.15,
		MaxDailyLossPct:           0.03,
		MaxPairCorrelation:        0.75,
		MinLiquidityUSD:           3_000_000,
		MinDecisionConfidence:     65,
		ExecutionCommissionBps:    1.5,
		ExecutionSpreadBps:        2.0,
		ExecutionSlippageBps:      1.0,
		ExecutionImpactBps:        0.5,
		MaxParticipationRate:      0.10,
		DrawdownThrottleStartPct:  0.04,
		DrawdownThrottleMinScale:  0.40,
		MaxPortfolioHeatPct:       0.025,
		MaxNetExposurePct:         0.55,
		MaxSectorExposurePct:      0.30,
		MaxCorrelatedPositions:    2,
		MaxRuntimeDegradationsPerSession:    5,
		MaxReconciliationFailuresPerSession: 4,
		MaxOrderRejectsPerSession:           7,
		LossStreakPauseThreshold:  4,
		LossStreakPauseCycles:     6,
		PerformanceRiskLookback:   25,
		VolatilityBrakeTargetPct:  0.010,
		VolatilityBrakeLookback:   50,
		VolatilityBrakeMinScale:   0.50,
		KellyFractionCap:         0.25,
		KellyLookback:            40,
		KellyMinTrades:           15,
		MarketStressEntryBlock:    0.90,
		MarketStressRiskMinScale:  0.40,
		CSVDataDir:                "/data/csv",
		MaxCycles:                 100,
		ReplayWarmupBars:          150,
		NewsProvider:              "rss",
		NewsLookbackMinutes:       300,
		NewsRefreshSeconds:        90,
		NewsMarketImpactThresh:    0.70,
		NewsSymbolImpactThresh:    0.75,
		NewsHardBlockThresh:       0.90,
		NewsMaxRiskReduction:      0.60,
		BenchmarkSymbols:          []string{"SPY", "QQQ"},
		BinanceAPIKey:             "bk",
		BinanceSecretKey:          "bs",
		HyperliquidPrivateKey:     "hpk",
		HyperliquidWalletAddr:     "hwa",
		HyperliquidTestnet:        true,
		AsterUser:                 "au",
		AsterSigner:               "as",
		AsterPrivateKey:           "apk",
		AlpacaAPIKey:              "aak",
		AlpacaSecretKey:           "ask",
		AlpacaPaperTrading:        true,
		// *bool fields: set to non-default values to verify override
		AllowShort:                     boolPtr(false),
		UseMacroFilters:                boolPtr(false),
		DynamicPositionSizing:          boolPtr(false),
		RegimeRiskScaling:              boolPtr(false),
		UseNewsRisk:                    boolPtr(false),
		EnableNewsInReplay:             boolPtr(true),
		RequireBacktestSummary:         boolPtr(true),
		RequireReleaseBuildForLive:     boolPtr(true),
		SupervisorReduceOnlyOnDrawdown: boolPtr(false),
	}
}

func TestBuildTraderConfig_SafetyCriticalFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 3}

	tc := BuildTraderConfig(cfg, []string{"AAPL"}, "default.txt", "http://pool", 500, 1000, 30, lev)

	// Identity
	assertEqual(t, "ID", tc.ID, "ibkr-test-01")
	assertEqual(t, "Name", tc.Name, "IBKR Test Trader")
	assertEqual(t, "Exchange", tc.Exchange, "ibkr")
	assertEqual(t, "Mode", tc.Mode, "paper")

	// Safety-critical IBKR fields
	assertEqual(t, "StrictLiveMode", tc.StrictLiveMode, true)
	assertEqual(t, "LivePromotionApproved", tc.LivePromotionApproved, true)
	assertEqual(t, "EmergencyKillSwitch", tc.EmergencyKillSwitch, true)
	assertEqual(t, "KillSwitchFile", tc.KillSwitchFile, "/tmp/killswitch")
	assertEqual(t, "IBKRGatewayURL", tc.IBKRGatewayURL, "https://127.0.0.1:5002/v1/api")
	assertEqual(t, "IBKRAccountID", tc.IBKRAccountID, "U1234567")
	assertEqual(t, "IBKRSessionCookie", tc.IBKRSessionCookie, "sess-abc")

	// Promotion fields
	assertEqual(t, "PromotionSourceTraderID", tc.PromotionSourceTraderID, "paper-01")
	assertEqual(t, "MinPaperSessionReports", tc.MinPaperSessionReports, 3)
	assertEqual(t, "PromotionMaxEvidenceAgeDays", tc.PromotionMaxEvidenceAgeDays, 14)
	assertEqual(t, "RequireBacktestSummary", tc.RequireBacktestSummary, true)
	assertEqual(t, "RequireReleaseBuildForLive", tc.RequireReleaseBuildForLive, true)

	// Risk limits from global params
	assertEqual(t, "MaxDailyLoss", tc.MaxDailyLoss, 500.0)
	assertEqual(t, "MaxDrawdown", tc.MaxDrawdown, 1000.0)
	assertEqual(t, "StopTradingTime", tc.StopTradingTime, 30*time.Minute)

	// Leverage from global config
	assertEqual(t, "BTCETHLeverage", tc.BTCETHLeverage, 5)
	assertEqual(t, "AltcoinLeverage", tc.AltcoinLeverage, 3)
}

func TestBuildTraderConfig_BoolPtrDefaults(t *testing.T) {
	cfg := config.TraderConfig{
		ID:             "default-test",
		Name:           "Default Test",
		Exchange:       "demo",
		InitialBalance: 10000,
		DemoMode:       true,
	}
	// All *bool fields are nil → should get defaults
	lev := config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 3}
	tc := BuildTraderConfig(cfg, nil, "", "", 100, 200, 10, lev)

	// Defaults: true
	assertEqual(t, "AllowShort (nil→true)", tc.AllowShort, true)
	assertEqual(t, "UseMacroFilters (nil→true)", tc.UseMacroFilters, true)
	assertEqual(t, "DynamicPositionSizing (nil→true)", tc.DynamicPositionSizing, true)
	assertEqual(t, "RegimeRiskScaling (nil→true)", tc.RegimeRiskScaling, true)
	assertEqual(t, "UseNewsRisk (nil→true)", tc.UseNewsRisk, true)
	assertEqual(t, "SupervisorReduceOnlyOnDrawdown (nil→true)", tc.SupervisorReduceOnlyOnDrawdown, true)
	assertEqual(t, "SupervisorReduceOnlyOnDrawdownSet (nil→false)", tc.SupervisorReduceOnlyOnDrawdownSet, false)

	// Defaults: false
	assertEqual(t, "EnableNewsInReplay (nil→false)", tc.EnableNewsInReplay, false)
	assertEqual(t, "RequireBacktestSummary (nil→false)", tc.RequireBacktestSummary, false)
	assertEqual(t, "RequireReleaseBuildForLive (nil→false)", tc.RequireReleaseBuildForLive, false)
}

func TestBuildTraderConfig_BoolPtrOverrides(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 3}
	tc := BuildTraderConfig(cfg, nil, "", "", 100, 200, 10, lev)

	// All explicitly set to non-default values in fullIBKRTraderConfig
	assertEqual(t, "AllowShort (false)", tc.AllowShort, false)
	assertEqual(t, "UseMacroFilters (false)", tc.UseMacroFilters, false)
	assertEqual(t, "DynamicPositionSizing (false)", tc.DynamicPositionSizing, false)
	assertEqual(t, "RegimeRiskScaling (false)", tc.RegimeRiskScaling, false)
	assertEqual(t, "UseNewsRisk (false)", tc.UseNewsRisk, false)
	assertEqual(t, "EnableNewsInReplay (true)", tc.EnableNewsInReplay, true)
	assertEqual(t, "RequireBacktestSummary (true)", tc.RequireBacktestSummary, true)
	assertEqual(t, "RequireReleaseBuildForLive (true)", tc.RequireReleaseBuildForLive, true)
	assertEqual(t, "SupervisorReduceOnlyOnDrawdown (false)", tc.SupervisorReduceOnlyOnDrawdown, false)
	assertEqual(t, "SupervisorReduceOnlyOnDrawdownSet (true)", tc.SupervisorReduceOnlyOnDrawdownSet, true)
}

func TestBuildTraderConfig_BrokerCredentials(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "BinanceAPIKey", tc.BinanceAPIKey, "bk")
	assertEqual(t, "BinanceSecretKey", tc.BinanceSecretKey, "bs")
	assertEqual(t, "HyperliquidPrivateKey", tc.HyperliquidPrivateKey, "hpk")
	assertEqual(t, "HyperliquidWalletAddr", tc.HyperliquidWalletAddr, "hwa")
	assertEqual(t, "HyperliquidTestnet", tc.HyperliquidTestnet, true)
	assertEqual(t, "AsterUser", tc.AsterUser, "au")
	assertEqual(t, "AsterSigner", tc.AsterSigner, "as")
	assertEqual(t, "AsterPrivateKey", tc.AsterPrivateKey, "apk")
	assertEqual(t, "AlpacaAPIKey", tc.AlpacaAPIKey, "aak")
	assertEqual(t, "AlpacaSecretKey", tc.AlpacaSecretKey, "ask")
	assertEqual(t, "AlpacaPaperTrading", tc.AlpacaPaperTrading, true)
}

func TestBuildTraderConfig_AIModelRouting(t *testing.T) {
	lev := config.LeverageConfig{}

	// qwen model → UseQwen=true
	cfg := config.TraderConfig{ID: "q", Name: "Q", AIModel: "qwen", Exchange: "demo", DemoMode: true, InitialBalance: 1000}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)
	assertEqual(t, "UseQwen (qwen model)", tc.UseQwen, true)

	// deepseek model → UseQwen=false
	cfg.AIModel = "deepseek"
	tc = BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)
	assertEqual(t, "UseQwen (deepseek model)", tc.UseQwen, false)

	// custom model → UseQwen=false, custom fields set
	cfg.AIModel = "custom"
	cfg.CustomAPIURL = "http://custom"
	cfg.CustomAPIKey = "ck"
	cfg.CustomModelName = "cm"
	tc = BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)
	assertEqual(t, "UseQwen (custom model)", tc.UseQwen, false)
	assertEqual(t, "CustomAPIURL", tc.CustomAPIURL, "http://custom")
	assertEqual(t, "CustomAPIKey", tc.CustomAPIKey, "ck")
	assertEqual(t, "CustomModelName", tc.CustomModelName, "cm")
}

func TestBuildTraderConfig_DefaultSymbolsCopied(t *testing.T) {
	cfg := config.TraderConfig{ID: "s", Name: "S", Exchange: "demo", DemoMode: true, InitialBalance: 1000}
	lev := config.LeverageConfig{}
	origSymbols := []string{"AAPL", "MSFT", "GOOG"}
	tc := BuildTraderConfig(cfg, origSymbols, "defaults.txt", "http://pool.api", 0, 0, 0, lev)

	if !reflect.DeepEqual(tc.ConfiguredDefaultSymbols, origSymbols) {
		t.Fatalf("ConfiguredDefaultSymbols: got %v, want %v", tc.ConfiguredDefaultSymbols, origSymbols)
	}
	assertEqual(t, "ConfiguredDefaultSymbolsFile", tc.ConfiguredDefaultSymbolsFile, "defaults.txt")
	assertEqual(t, "CoinPoolAPIURL", tc.CoinPoolAPIURL, "http://pool.api")

	// Verify the slice is a copy, not a shared reference
	origSymbols[0] = "CHANGED"
	if tc.ConfiguredDefaultSymbols[0] == "CHANGED" {
		t.Fatal("ConfiguredDefaultSymbols should be a copy, not alias the original slice")
	}
}

func TestBuildTraderConfig_EquityRiskFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "MaxGrossExposure", tc.MaxGrossExposure, 0.8)
	assertEqual(t, "MaxPositionPct", tc.MaxPositionPct, 0.15)
	assertEqual(t, "MaxDailyLossPct", tc.MaxDailyLossPct, 0.03)
	assertEqual(t, "MaxNetExposurePct", tc.MaxNetExposurePct, 0.55)
	assertEqual(t, "MaxSectorExposurePct", tc.MaxSectorExposurePct, 0.30)
	assertEqual(t, "MaxCorrelatedPositions", tc.MaxCorrelatedPositions, 2)
	assertEqual(t, "MaxPairCorrelation", tc.MaxPairCorrelation, 0.75)
	assertEqual(t, "MinLiquidityUSD", tc.MinLiquidityUSD, 3_000_000.0)
	assertEqual(t, "MinDecisionConfidence", tc.MinDecisionConfidence, 65)
}

func TestBuildTraderConfig_ExecutionCostFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "ExecutionCommissionBps", tc.ExecutionCommissionBps, 1.5)
	assertEqual(t, "ExecutionSpreadBps", tc.ExecutionSpreadBps, 2.0)
	assertEqual(t, "ExecutionSlippageBps", tc.ExecutionSlippageBps, 1.0)
	assertEqual(t, "ExecutionImpactBps", tc.ExecutionImpactBps, 0.5)
	assertEqual(t, "MaxParticipationRate", tc.MaxParticipationRate, 0.10)
}

func TestBuildTraderConfig_SupervisorFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "MaxRuntimeDegradationsPerSession", tc.MaxRuntimeDegradationsPerSession, 5)
	assertEqual(t, "MaxReconciliationFailuresPerSession", tc.MaxReconciliationFailuresPerSession, 4)
	assertEqual(t, "MaxOrderRejectsPerSession", tc.MaxOrderRejectsPerSession, 7)
	assertEqual(t, "LossStreakPauseThreshold", tc.LossStreakPauseThreshold, 4)
	assertEqual(t, "LossStreakPauseCycles", tc.LossStreakPauseCycles, 6)
}

func TestBuildTraderConfig_AdvancedRiskScaling(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "DrawdownThrottleStartPct", tc.DrawdownThrottleStartPct, 0.04)
	assertEqual(t, "DrawdownThrottleMinScale", tc.DrawdownThrottleMinScale, 0.40)
	assertEqual(t, "MaxPortfolioHeatPct", tc.MaxPortfolioHeatPct, 0.025)
	assertEqual(t, "VolatilityBrakeTargetPct", tc.VolatilityBrakeTargetPct, 0.010)
	assertEqual(t, "VolatilityBrakeLookback", tc.VolatilityBrakeLookback, 50)
	assertEqual(t, "VolatilityBrakeMinScale", tc.VolatilityBrakeMinScale, 0.50)
	assertEqual(t, "KellyFractionCap", tc.KellyFractionCap, 0.25)
	assertEqual(t, "KellyLookback", tc.KellyLookback, 40)
	assertEqual(t, "KellyMinTrades", tc.KellyMinTrades, 15)
	assertEqual(t, "MarketStressEntryBlock", tc.MarketStressEntryBlock, 0.90)
	assertEqual(t, "MarketStressRiskMinScale", tc.MarketStressRiskMinScale, 0.40)
}

func TestBuildTraderConfig_NewsFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "NewsProvider", tc.NewsProvider, "rss")
	assertEqual(t, "NewsLookbackMinutes", tc.NewsLookbackMinutes, 300)
	assertEqual(t, "NewsRefreshSeconds", tc.NewsRefreshSeconds, 90)
	assertEqual(t, "NewsMarketImpactThresh", tc.NewsMarketImpactThresh, 0.70)
	assertEqual(t, "NewsSymbolImpactThresh", tc.NewsSymbolImpactThresh, 0.75)
	assertEqual(t, "NewsHardBlockThresh", tc.NewsHardBlockThresh, 0.90)
	assertEqual(t, "NewsMaxRiskReduction", tc.NewsMaxRiskReduction, 0.60)
}

func TestBuildTraderConfig_StrategyAndBacktestFields(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)

	assertEqual(t, "StrategyMode", tc.StrategyMode, "hybrid_ai")
	assertEqual(t, "MomentumMinScore", tc.MomentumMinScore, 1.5)
	assertEqual(t, "FallbackPositionPct", tc.FallbackPositionPct, 0.08)
	assertEqual(t, "MinFactorScore", tc.MinFactorScore, 0.4)
	assertEqual(t, "RiskPerTradePct", tc.RiskPerTradePct, 0.01)
	assertEqual(t, "ProfitLockThreshold", tc.ProfitLockThreshold, 1.5)
	assertEqual(t, "TrailingStopATRMult", tc.TrailingStopATRMult, 2.0)
	assertEqual(t, "MaxHoldingCycles", tc.MaxHoldingCycles, 200)
	assertEqual(t, "MaxConcurrentPos", tc.MaxConcurrentPos, 5)
	assertEqual(t, "SymbolCooldownCycles", tc.SymbolCooldownCycles, 8)
	assertEqual(t, "MaxCycles", tc.MaxCycles, 100)
	assertEqual(t, "ReplayWarmupBars", tc.ReplayWarmupBars, 150)
	assertEqual(t, "BenchmarkSymbols", reflect.DeepEqual(tc.BenchmarkSymbols, []string{"SPY", "QQQ"}), true)
}

func TestBuildTraderConfig_ScanInterval(t *testing.T) {
	lev := config.LeverageConfig{}

	// ScanIntervalSeconds takes precedence
	cfg := config.TraderConfig{
		ID: "s", Name: "S", Exchange: "demo", DemoMode: true, InitialBalance: 1000,
		ScanIntervalSeconds: 120,
	}
	tc := BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)
	assertEqual(t, "ScanInterval from seconds", tc.ScanInterval, 120*time.Second)

	// ScanIntervalMinutes fallback
	cfg = config.TraderConfig{
		ID: "s", Name: "S", Exchange: "demo", DemoMode: true, InitialBalance: 1000,
		ScanIntervalMinutes: 5,
	}
	tc = BuildTraderConfig(cfg, nil, "", "", 0, 0, 0, lev)
	assertEqual(t, "ScanInterval from minutes", tc.ScanInterval, 5*time.Minute)
}

// TestBuildTraderConfig_FieldCountRegression uses reflection to detect if new
// AutoTraderConfig fields are added without being set in BuildTraderConfig.
// This catches the case where a developer adds a field to AutoTraderConfig but
// forgets to map it in the manager.
func TestBuildTraderConfig_FieldCountRegression(t *testing.T) {
	cfg := fullIBKRTraderConfig()
	lev := config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 3}
	tc := BuildTraderConfig(cfg, []string{"AAPL"}, "f.txt", "http://pool", 500, 1000, 30, lev)

	tcType := reflect.TypeOf(tc)
	tcValue := reflect.ValueOf(tc)

	// Fields that are legitimately zero/false given our test config:
	// - UseQwen: false because AIModel is "custom", not "qwen"
	// - DemoMode: false in test config (IBKR traders are never demo)
	// - Bool fields from *bool overrides set to false to test override path
	// - SupervisorReduceOnlyOnDrawdownSet: true when ptr is non-nil (checked separately)
	expectedZero := map[string]bool{
		"UseQwen":                    true,
		"DemoMode":                   true,
		"AllowShort":                 true,
		"UseMacroFilters":            true,
		"DynamicPositionSizing":      true,
		"RegimeRiskScaling":          true,
		"UseNewsRisk":                true,
		"SupervisorReduceOnlyOnDrawdown": true,
	}

	var zeroFields []string
	for i := 0; i < tcType.NumField(); i++ {
		field := tcType.Field(i)
		value := tcValue.Field(i)
		if value.IsZero() && !expectedZero[field.Name] {
			zeroFields = append(zeroFields, field.Name)
		}
	}

	if len(zeroFields) > 0 {
		t.Errorf("AutoTraderConfig fields are zero after BuildTraderConfig with full config input.\n"+
			"This likely means new fields were added to AutoTraderConfig without updating BuildTraderConfig.\n"+
			"Zero fields: %v", zeroFields)
	}
}

func TestNewTraderManager(t *testing.T) {
	tm := NewTraderManager()
	if tm == nil {
		t.Fatal("NewTraderManager returned nil")
	}
	ids := tm.GetTraderIDs()
	if len(ids) != 0 {
		t.Fatalf("expected 0 traders, got %d", len(ids))
	}
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}
