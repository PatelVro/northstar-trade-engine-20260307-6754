package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"northstar/broker"
	"northstar/market"
	"northstar/pool"
	"northstar/trader"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type strategyProfile struct {
	StrategyMode string
	MinScore     float64
	PositionPct  float64
}

func (p strategyProfile) slug() string {
	score := strings.ReplaceAll(fmt.Sprintf("%.2f", p.MinScore), ".", "p")
	pct := strings.ReplaceAll(fmt.Sprintf("%.2f", p.PositionPct), ".", "p")
	return fmt.Sprintf("%s_s%s_p%s", p.StrategyMode, score, pct)
}

func (p strategyProfile) requiresAI() bool {
	return p.StrategyMode == "ai_only" || p.StrategyMode == "momentum_fallback" || p.StrategyMode == "hybrid_ai"
}

type replaySummary struct {
	TotalTrades   int     `json:"total_trades"`
	WinRatePct    float64 `json:"win_rate_pct"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	FinalEquity   float64 `json:"final_equity"`
	ReturnPct     float64 `json:"return_pct"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	SortinoRatio  float64 `json:"sortino_ratio"`
	ProfitFactor  float64 `json:"profit_factor"`
	ExpectancyUSD float64 `json:"expectancy_usd"`
	AvgWinUSD     float64 `json:"avg_win_usd"`
	AvgLossUSD    float64 `json:"avg_loss_usd"`
	TotalFeesUSD  float64 `json:"total_fees_usd"`
	PartialFills  int     `json:"partial_fills"`
	RejectedFills int     `json:"rejected_fills"`
}

type profileResult struct {
	ProfileSlug              string    `json:"profile_slug"`
	StrategyMode             string    `json:"strategy_mode"`
	MinScore                 float64   `json:"min_score"`
	PositionPct              float64   `json:"position_pct"`
	ConfiguredSymbolCount    int       `json:"configured_symbol_count"`
	UsableSymbolCount        int       `json:"usable_symbol_count"`
	CoverageRatio            float64   `json:"coverage_ratio"`
	SymbolCount              int       `json:"symbol_count"`
	CyclesExecuted           int       `json:"cycles_executed"`
	DurationSeconds          float64   `json:"duration_seconds"`
	StartedAt                time.Time `json:"started_at"`
	FinishedAt               time.Time `json:"finished_at"`
	StudyStart               string    `json:"study_start"`
	StudyEnd                 string    `json:"study_end"`
	StudyWindowDays          int       `json:"study_window_days"`
	MinBarsAvailable         int       `json:"min_bars_available"`
	MedianBarsAvailable      float64   `json:"median_bars_available"`
	MaxBarsAvailable         int       `json:"max_bars_available"`
	OverlapBarsAvailable     int       `json:"overlap_bars_available"`
	ActiveBarsTested         int       `json:"active_bars_tested"`
	ActiveDaysEstimate       float64   `json:"active_days_estimate"`
	TotalTrades              int       `json:"total_trades"`
	WinRatePct               float64   `json:"win_rate_pct"`
	MaxDrawdownPct           float64   `json:"max_drawdown_pct"`
	FinalEquity              float64   `json:"final_equity"`
	ReturnPct                float64   `json:"return_pct"`
	SharpeRatio              float64   `json:"sharpe_ratio"`
	SortinoRatio             float64   `json:"sortino_ratio"`
	ProfitFactor             float64   `json:"profit_factor"`
	ExpectancyUSD            float64   `json:"expectancy_usd"`
	AvgWinUSD                float64   `json:"avg_win_usd"`
	AvgLossUSD               float64   `json:"avg_loss_usd"`
	TotalFeesUSD             float64   `json:"total_fees_usd"`
	PartialFills             int       `json:"partial_fills"`
	RejectedFills            int       `json:"rejected_fills"`
	FirstHalfReturnPct       float64   `json:"first_half_return_pct"`
	SecondHalfReturnPct      float64   `json:"second_half_return_pct"`
	RobustnessScore          float64   `json:"robustness_score"`
	MonteCarloP05Pct         float64   `json:"mc_p05_return_pct"`
	MonteCarloP50Pct         float64   `json:"mc_p50_return_pct"`
	MonteCarloWinPct         float64   `json:"mc_positive_rate_pct"`
	TradedSymbols            int       `json:"traded_symbols"`
	TradeHHI                 float64   `json:"trade_hhi"`
	Diversification          float64   `json:"diversification_score"`
	UlcerIndexPct            float64   `json:"ulcer_index_pct"`
	SegmentStability         float64   `json:"segment_stability_score"`
	CalmarRatio              float64   `json:"calmar_ratio"`
	CVaR95Pct                float64   `json:"cvar95_pct"`
	TailRatio                float64   `json:"tail_ratio"`
	ReturnPerFee             float64   `json:"return_per_fee"`
	CompositeScore           float64   `json:"composite_score"`
	AvgTradesPerActiveSymbol float64   `json:"avg_trades_per_active_symbol"`
	DominantSymbolTradeShare float64   `json:"dominant_symbol_trade_share"`
	EvidenceScore            float64   `json:"evidence_score"`
	CredibilityTier          string    `json:"credibility_tier"`
	RankingEligible          bool      `json:"ranking_eligible"`
	QualityFlags             []string  `json:"quality_flags,omitempty"`
	QualitySummary           string    `json:"quality_summary"`
	RankingScore             float64   `json:"ranking_score"`
	ReplaySummaryRel         string    `json:"replay_summary_rel"`
	PipelineSummaryRel       string    `json:"pipeline_summary_rel"`
	WorkDirRel               string    `json:"work_dir_rel"`
}

func main() {
	studyPresetDefault := previewStringFlag(os.Args[1:], "study-preset", "quick")
	studyPresetCfg, presetErr := resolveStudyPreset(studyPresetDefault)
	if presetErr != nil {
		log.Fatalf("invalid study-preset: %v", presetErr)
	}
	universePresetDefault := previewStringFlag(os.Args[1:], "universe-preset", "core")
	defaultSymbolsFile := resolveUniverseSymbolsFile(universePresetDefault, "data/universe/us_canada_tradable_core.txt")

	var (
		gatewayURL    = flag.String("gateway-url", envOrDefault("https://127.0.0.1:5002/v1/api", "NORTHSTAR_IBKR_BASE_URL"), "IBKR Client Portal API URL")
		accountID     = flag.String("account-id", envOrFirst("NORTHSTAR_IBKR_ACCOUNT_ID"), "IBKR account ID (required)")
		sessionCookie = flag.String("session-cookie", envOrFirst("NORTHSTAR_IBKR_SESSION_COOKIE", "IBKR_SESSION_COOKIE"), "Optional IBKR session cookie (x-sess-uuid=...)")

		studyPreset    = flag.String("study-preset", studyPresetDefault, "Research preset: quick|standard|broad|extended|custom. Use custom to control range flags manually.")
		universePreset = flag.String("universe-preset", universePresetDefault, "Universe preset: core|broad|custom")
		symbolsCSV     = flag.String("symbols", "", "Comma-separated symbols (overrides symbols-file)")
		symbolsFile    = flag.String("symbols-file", defaultSymbolsFile, "Path to symbols list file")
		maxSymbols     = flag.Int("max-symbols", studyPresetCfg.MaxSymbols, "Maximum symbols to include (0 = use all resolved symbols)")

		barInterval      = flag.String("bar-interval", "1h", "History bar interval for IBKR download: 1m,5m,1h,1d")
		barLimit         = flag.Int("bar-limit", studyPresetCfg.BarLimit, "Maximum bars per symbol to export")
		minBarsPerSymbol = flag.Int("min-bars-per-symbol", studyPresetCfg.MinBarsPerSymbol, "Minimum bars required per symbol; symbols below this are skipped")
		skipFetch        = flag.Bool("skip-fetch", false, "Skip IBKR download and use existing CSV files")
		csvDataDir       = flag.String("csv-data-dir", "", "Optional existing CSV directory for replay/backtest (implies -skip-fetch)")

		maxCycles                      = flag.Int("max-cycles", studyPresetCfg.MaxCycles, "Backtest cycles per profile")
		warmupBars                     = flag.Int("replay-warmup-bars", 120, "Replay warmup bars before first cycle")
		initialBalance                 = flag.Float64("initial-balance", 100000, "Initial balance for simulated broker")
		candidateBatch                 = flag.Int("candidate-batch-size", studyPresetCfg.CandidateBatch, "Candidate symbols analyzed per cycle")
		maxPairCorr                    = flag.Float64("max-pair-correlation", 0.82, "Maximum same-side pair correlation for new entries")
		minLiquidityUSD                = flag.Float64("min-liquidity-usd", 2000000, "Minimum estimated dollar volume for entries")
		minConfidence                  = flag.Int("min-confidence", 58, "Minimum confidence required for open decisions")
		regimeRiskScale                = flag.Bool("regime-risk-scaling", true, "Enable regime-aware risk-per-trade scaling")
		commissionBps                  = flag.Float64("commission-bps", 0.35, "Simulated commission in basis points per side")
		slippageBps                    = flag.Float64("slippage-bps", 0.75, "Simulated slippage in basis points per side")
		executionImpactBps             = flag.Float64("execution-impact-bps", 12.0, "Impact slippage coefficient in bps scaled by sqrt(participation)")
		maxParticipationRate           = flag.Float64("max-participation-rate", 0.15, "Maximum per-bar participation for simulated fills (0-1]")
		drawdownThrottleStart          = flag.Float64("drawdown-throttle-start", 0.03, "Drawdown level (fraction) where risk throttling begins")
		drawdownThrottleMinScale       = flag.Float64("drawdown-throttle-min-scale", 0.35, "Minimum risk scale under drawdown throttling")
		maxPortfolioHeatPct            = flag.Float64("max-portfolio-heat-pct", 0.035, "Maximum portfolio heat budget as fraction of equity")
		maxNetExposurePct              = flag.Float64("max-net-exposure-pct", 0.65, "Maximum absolute net long-short exposure as fraction of equity")
		lossStreakPauseThreshold       = flag.Int("loss-streak-pause-threshold", 3, "Consecutive losing closes before pausing new entries")
		lossStreakPauseCycles          = flag.Int("loss-streak-pause-cycles", 5, "Cycles to pause new entries after loss streak trigger")
		performanceRiskLookback        = flag.Int("performance-risk-lookback", 20, "Closed-trade lookback used for performance-aware risk scaling")
		volatilityBrakeTargetPct       = flag.Float64("volatility-brake-target-pct", 0.008, "Target equity volatility (fraction) for risk brake")
		volatilityBrakeLookback        = flag.Int("volatility-brake-lookback", 40, "Lookback cycles for realized equity volatility")
		volatilityBrakeMinScale        = flag.Float64("volatility-brake-min-scale", 0.45, "Minimum risk scale under volatility brake")
		kellyFractionCap               = flag.Float64("kelly-fraction-cap", 0.33, "Fraction of Kelly estimate used for adaptive risk scaling")
		kellyLookback                  = flag.Int("kelly-lookback", 30, "Closed-trade lookback window used for Kelly scaling")
		kellyMinTrades                 = flag.Int("kelly-min-trades", 10, "Minimum closed trades before Kelly scaling activates")
		marketStressEntryBlock         = flag.Float64("market-stress-entry-block", 0.82, "Block new entries above this market stress score")
		marketStressRiskMinScale       = flag.Float64("market-stress-risk-min-scale", 0.35, "Minimum risk scale applied under market stress")
		useNewsRisk                    = flag.Bool("use-news-risk", false, "Enable headline-driven risk filter (disabled by default for replay)")
		enableNewsInReplay             = flag.Bool("enable-news-in-replay", false, "Allow news risk module to run in replay mode")
		newsProvider                   = flag.String("news-provider", "rss", "News provider id (rss)")
		newsLookbackMinutes            = flag.Int("news-lookback-minutes", 240, "News aggregation lookback window in minutes")
		newsRefreshSeconds             = flag.Int("news-refresh-seconds", 120, "News refresh interval in seconds")
		newsMarketImpactThresh         = flag.Float64("news-market-impact-thresh", 0.65, "Market news threshold for stricter directional filtering")
		newsSymbolImpactThresh         = flag.Float64("news-symbol-impact-thresh", 0.70, "Symbol news threshold for entry blocking")
		newsHardBlockThresh            = flag.Float64("news-hard-block-thresh", 0.85, "Hard block threshold for adverse directional news")
		newsMaxRiskReduction           = flag.Float64("news-max-risk-reduction", 0.55, "Maximum multiplicative risk reduction from news")
		minTradesForScore              = flag.Int("min-trades-for-score", 4, "Minimum trades threshold before a profile receives full scoring credit")
		minTradedSymbols               = flag.Int("min-traded-symbols", 2, "Minimum traded symbols threshold before full scoring credit")
		minTradesForCredibility        = flag.Int("min-trades-for-credibility", 12, "Minimum closed trades required before a profile can be treated as credible")
		minActiveBarsForCredibility    = flag.Int("min-active-bars-for-credibility", 180, "Minimum replay bars tested before a profile can be treated as credible")
		minTestedDaysForCredibility    = flag.Float64("min-tested-days-for-credibility", 20, "Minimum estimated tested days before a profile can be treated as credible")
		minStudyWindowDays             = flag.Int("min-study-window-days", 45, "Minimum dataset window in days before the study is treated as broad enough")
		minUsableSymbolsForCredibility = flag.Int("min-usable-symbols-for-credibility", 6, "Minimum usable symbols required before results are treated as credible")
		minCoverageRatio               = flag.Float64("min-coverage-ratio", 0.60, "Minimum usable/configured symbol coverage ratio required before results are treated as credible")
		maxDominantSymbolShare         = flag.Float64("max-dominant-symbol-share", 0.65, "Maximum acceptable share of trades from one symbol before concentration warnings apply")
		maxSegmentGapPct               = flag.Float64("max-segment-gap-pct", 12.0, "Maximum acceptable gap between first-half and second-half returns before instability warnings apply")
		mcSims                         = flag.Int("mc-sims", 300, "Monte Carlo bootstrap simulations over closed trades")
		mcSeed                         = flag.Int64("mc-seed", 0, "Monte Carlo RNG seed (0 = auto)")
		profilesRaw                    = flag.String("profiles", "multi_factor:0.35:0.08,multi_factor:0.45:0.10,momentum_only:1.25:0.10,momentum_fallback:1.25:0.10", "Strategy profiles: strategy:minScore:positionPct,...")
		autoGrid                       = flag.Bool("auto-grid", false, "Auto-generate profile grid and ignore -profiles")
		strategyGridRaw                = flag.String("strategy-grid", "multi_factor,momentum_only,momentum_fallback", "Strategy modes used when -auto-grid is enabled")
		scoreGridRaw                   = flag.String("score-grid", "0.30,0.35,0.45,0.55,1.25", "Min-score grid used when -auto-grid is enabled")
		positionGridRaw                = flag.String("position-grid", "0.06,0.08,0.10,0.12", "Position pct grid used when -auto-grid is enabled")
		outputRoot                     = flag.String("output-root", "output/ibkr_backtests", "Backtest output root directory")
		writeBestProfile               = flag.String("write-best-profile", "", "Optional output path for best profile config JSON")
		aiModel                        = flag.String("ai-model", "deepseek", "AI model for AI-enabled profiles: deepseek|qwen|custom")
		deepseekKey                    = flag.String("deepseek-key", envOrFirst("NORTHSTAR_DEEPSEEK_API_KEY", "DEEPSEEK_KEY"), "DeepSeek API key (required for deepseek AI profiles)")
		qwenKey                        = flag.String("qwen-key", envOrFirst("NORTHSTAR_QWEN_API_KEY", "QWEN_KEY"), "Qwen API key (required for qwen AI profiles)")
		customAPIURL                   = flag.String("custom-api-url", envOrFirst("NORTHSTAR_CUSTOM_API_URL", "CUSTOM_API_URL"), "Custom OpenAI-compatible API URL")
		customAPIKey                   = flag.String("custom-api-key", envOrFirst("NORTHSTAR_CUSTOM_API_KEY", "CUSTOM_API_KEY"), "Custom API key")
		customModelName                = flag.String("custom-model-name", envOrFirst("NORTHSTAR_CUSTOM_MODEL_NAME", "CUSTOM_MODEL_NAME"), "Custom model name")
	)
	flag.Parse()

	if strings.TrimSpace(*accountID) == "" {
		log.Fatal("account-id is required (set -account-id or NORTHSTAR_IBKR_ACCOUNT_ID)")
	}
	switch strings.ToLower(strings.TrimSpace(*universePreset)) {
	case "", "core", "broad", "custom":
	default:
		log.Fatalf("unknown universe-preset %q", *universePreset)
	}
	if *maxCycles <= 0 {
		log.Fatal("max-cycles must be > 0")
	}
	if *warmupBars < 80 {
		*warmupBars = 80
	}
	if *candidateBatch <= 0 {
		*candidateBatch = 20
	}
	if *barLimit <= 0 {
		*barLimit = 500
	}
	if *minBarsPerSymbol <= 0 {
		*minBarsPerSymbol = 80
	}
	if *maxPairCorr <= 0 || *maxPairCorr >= 1 {
		*maxPairCorr = 0.82
	}
	if *minLiquidityUSD < 0 {
		*minLiquidityUSD = 0
	}
	if *minConfidence < 0 {
		*minConfidence = 0
	}
	if *minConfidence > 100 {
		*minConfidence = 100
	}
	if *commissionBps < 0 {
		*commissionBps = 0
	}
	if *slippageBps < 0 {
		*slippageBps = 0
	}
	if *executionImpactBps < 0 {
		*executionImpactBps = 0
	}
	if *maxParticipationRate <= 0 || *maxParticipationRate > 1 {
		*maxParticipationRate = 0.15
	}
	if *drawdownThrottleStart <= 0 {
		*drawdownThrottleStart = 0.03
	}
	if *drawdownThrottleMinScale <= 0 || *drawdownThrottleMinScale > 1 {
		*drawdownThrottleMinScale = 0.35
	}
	if *maxPortfolioHeatPct <= 0 || *maxPortfolioHeatPct > 0.30 {
		*maxPortfolioHeatPct = 0.035
	}
	if *maxNetExposurePct <= 0 || *maxNetExposurePct > 1 {
		*maxNetExposurePct = 0.65
	}
	if *lossStreakPauseThreshold <= 0 {
		*lossStreakPauseThreshold = 3
	}
	if *lossStreakPauseCycles <= 0 {
		*lossStreakPauseCycles = 5
	}
	if *performanceRiskLookback <= 0 {
		*performanceRiskLookback = 20
	}
	if *volatilityBrakeTargetPct <= 0 || *volatilityBrakeTargetPct >= 1 {
		*volatilityBrakeTargetPct = 0.008
	}
	if *volatilityBrakeLookback <= 1 {
		*volatilityBrakeLookback = 40
	}
	if *volatilityBrakeMinScale <= 0 || *volatilityBrakeMinScale > 1 {
		*volatilityBrakeMinScale = 0.45
	}
	if *kellyFractionCap < 0 || *kellyFractionCap > 1 {
		*kellyFractionCap = 0.33
	}
	if *kellyLookback <= 1 {
		*kellyLookback = 30
	}
	if *kellyMinTrades <= 0 {
		*kellyMinTrades = 10
	}
	if *marketStressEntryBlock <= 0 || *marketStressEntryBlock > 1 {
		*marketStressEntryBlock = 0.82
	}
	if *marketStressRiskMinScale <= 0 || *marketStressRiskMinScale > 1 {
		*marketStressRiskMinScale = 0.35
	}
	if strings.TrimSpace(*newsProvider) == "" {
		*newsProvider = "rss"
	}
	if *newsLookbackMinutes <= 0 {
		*newsLookbackMinutes = 240
	}
	if *newsRefreshSeconds <= 0 {
		*newsRefreshSeconds = 120
	}
	if *newsMarketImpactThresh <= 0 || *newsMarketImpactThresh > 1 {
		*newsMarketImpactThresh = 0.65
	}
	if *newsSymbolImpactThresh <= 0 || *newsSymbolImpactThresh > 1 {
		*newsSymbolImpactThresh = 0.70
	}
	if *newsHardBlockThresh <= 0 || *newsHardBlockThresh > 1 {
		*newsHardBlockThresh = 0.85
	}
	if *newsMaxRiskReduction <= 0 || *newsMaxRiskReduction > 0.95 {
		*newsMaxRiskReduction = 0.55
	}
	if *minTradesForScore < 0 {
		*minTradesForScore = 0
	}
	if *minTradedSymbols < 0 {
		*minTradedSymbols = 0
	}
	if *minTradesForCredibility < 0 {
		*minTradesForCredibility = 0
	}
	if *minActiveBarsForCredibility < 0 {
		*minActiveBarsForCredibility = 0
	}
	if *minTestedDaysForCredibility < 0 {
		*minTestedDaysForCredibility = 0
	}
	if *minStudyWindowDays < 0 {
		*minStudyWindowDays = 0
	}
	if *minUsableSymbolsForCredibility < 0 {
		*minUsableSymbolsForCredibility = 0
	}
	if *minCoverageRatio < 0 {
		*minCoverageRatio = 0
	}
	if *minCoverageRatio > 1 {
		*minCoverageRatio = 1
	}
	if *maxDominantSymbolShare <= 0 || *maxDominantSymbolShare > 1 {
		*maxDominantSymbolShare = 0.65
	}
	if *maxSegmentGapPct < 0 {
		*maxSegmentGapPct = 0
	}
	if *mcSims < 0 {
		*mcSims = 0
	}

	symbols, err := resolveSymbols(*symbolsCSV, *symbolsFile)
	if err != nil {
		log.Fatalf("failed to load symbols: %v", err)
	}
	if len(symbols) == 0 {
		log.Fatal("no symbols resolved")
	}
	resolvedSymbolCount := len(symbols)
	if *maxSymbols > 0 && len(symbols) > *maxSymbols {
		symbols = symbols[:*maxSymbols]
	}
	configuredSymbols := append([]string(nil), symbols...)
	log.Printf("Backtest research plan: preset=%s universe=%s resolved_symbols=%d configured_symbols=%d max_cycles=%d bar_limit=%d",
		*studyPreset, *universePreset, resolvedSymbolCount, len(configuredSymbols), *maxCycles, *barLimit)

	var profiles []strategyProfile
	if *autoGrid {
		profiles, err = buildProfileGrid(*strategyGridRaw, *scoreGridRaw, *positionGridRaw)
		if err != nil {
			log.Fatalf("failed to build auto profile grid: %v", err)
		}
		log.Printf("Auto-grid enabled: generated %d profiles", len(profiles))
	} else {
		profiles, err = parseProfiles(*profilesRaw)
		if err != nil {
			log.Fatalf("failed to parse profiles: %v", err)
		}
	}
	if len(profiles) == 0 {
		log.Fatal("no profiles to run")
	}

	runID := "run_" + time.Now().Format("20060102_150405")
	runRoot, err := filepath.Abs(filepath.Join(*outputRoot, runID))
	if err != nil {
		log.Fatalf("failed to resolve run root: %v", err)
	}
	dataDir := filepath.Join(runRoot, "csv")
	if strings.TrimSpace(*csvDataDir) != "" {
		*skipFetch = true
		dataDir, err = filepath.Abs(strings.TrimSpace(*csvDataDir))
		if err != nil {
			log.Fatalf("failed to resolve csv-data-dir: %v", err)
		}
	} else {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Fatalf("failed to create data dir: %v", err)
		}
	}

	availableSymbols := symbols
	if !*skipFetch {
		log.Printf("Downloading IBKR history for %d symbols (%s, limit=%d)...", len(symbols), *barInterval, *barLimit)
		downloaded, err := downloadHistory(*gatewayURL, *accountID, *sessionCookie, symbols, *barInterval, *barLimit, *minBarsPerSymbol, dataDir)
		if err != nil {
			log.Fatalf("history download failed: %v", err)
		}
		availableSymbols = downloaded
		if len(availableSymbols) == 0 {
			log.Fatal("no symbols were downloaded successfully")
		}
	} else {
		var existing []string
		for _, sym := range symbols {
			path := filepath.Join(dataDir, strings.ToUpper(sym)+".csv")
			if _, err := os.Stat(path); err != nil {
				continue
			}
			rows, err := countCSVDataRows(path)
			if err != nil {
				log.Printf("  [%s] skip existing CSV read error: %v", strings.ToUpper(sym), err)
				continue
			}
			if rows < *minBarsPerSymbol {
				log.Printf("  [%s] skipped existing CSV: only %d bars (min=%d)", strings.ToUpper(sym), rows, *minBarsPerSymbol)
				continue
			}
			existing = append(existing, strings.ToUpper(sym))
		}
		availableSymbols = dedupeSymbols(existing)
		if len(availableSymbols) == 0 {
			log.Fatal("skip-fetch is set but no CSV files found for requested symbols")
		}
	}

	log.Printf("Using %d symbols for backtests", len(availableSymbols))
	datasetStats, err := inspectDatasetCoverage(dataDir, configuredSymbols, availableSymbols)
	if err != nil {
		log.Fatalf("failed to inspect dataset coverage: %v", err)
	}
	log.Printf("Dataset coverage: usable=%d/%d (%.1f%%) | bars min/median/max=%d/%.1f/%d | window=%s to %s (%d days)",
		datasetStats.UsableSymbolCount,
		datasetStats.ConfiguredSymbolCount,
		datasetStats.CoverageRatio*100.0,
		datasetStats.MinBarsPerSymbol,
		datasetStats.MedianBarsPerSymbol,
		datasetStats.MaxBarsPerSymbol,
		datasetStats.DataStart.Format("2006-01-02"),
		datasetStats.DataEnd.Format("2006-01-02"),
		datasetStats.StudyWindowDays,
	)
	pool.SetDefaultCoins(availableSymbols)
	pool.SetUseDefaultCoins(true, true)

	thresholds := evidenceThresholds{
		MinTradesForCredibility:     *minTradesForCredibility,
		MinActiveBarsForCredibility: *minActiveBarsForCredibility,
		MinTestedDaysForCredibility: *minTestedDaysForCredibility,
		MinStudyWindowDays:          *minStudyWindowDays,
		MinUsableSymbols:            *minUsableSymbolsForCredibility,
		MinCoverageRatio:            *minCoverageRatio,
		MaxDominantSymbolShare:      *maxDominantSymbolShare,
		MaxSegmentGapPct:            *maxSegmentGapPct,
	}

	origWD, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to read working directory: %v", err)
	}
	defer os.Chdir(origWD)

	results := make([]profileResult, 0, len(profiles))
	seed := *mcSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	*mcSeed = seed
	effectiveParams := captureEffectiveFlagValues(flag.CommandLine)
	for idx, profile := range profiles {
		if profile.requiresAI() && !canRunAIProfile(*aiModel, *deepseekKey, *qwenKey, *customAPIURL, *customAPIKey, *customModelName) {
			log.Printf("Skipping profile %s: AI credentials not configured for ai-model=%s", profile.slug(), *aiModel)
			continue
		}

		profileSlug := profile.slug()
		profileDir := filepath.Join(runRoot, "profiles", profileSlug)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			log.Printf("Skipping %s: failed to create profile directory: %v", profileSlug, err)
			continue
		}

		if err := os.Chdir(profileDir); err != nil {
			log.Printf("Skipping %s: failed to enter profile directory: %v", profileSlug, err)
			continue
		}

		started := time.Now()
		atCfg := trader.AutoTraderConfig{
			ID:                "bt_" + profileSlug,
			Name:              "IBKR Backtest " + profileSlug,
			AIModel:           *aiModel,
			Exchange:          "ibkr",
			IBKRGatewayURL:    *gatewayURL,
			IBKRAccountID:     *accountID,
			IBKRSessionCookie: *sessionCookie,
			Mode:              "replay",
			DataProvider:      "csv",
			Broker:            "sim",
			CSVDataDir:        dataDir,
			InstrumentType:    "equity",
			CandidateBatchSize: func() int {
				if *candidateBatch > len(availableSymbols) {
					return len(availableSymbols)
				}
				return *candidateBatch
			}(),
			StrategyMode:             profile.StrategyMode,
			MomentumMinScore:         profile.MinScore,
			FallbackPositionPct:      profile.PositionPct,
			MinFactorScore:           profile.MinScore,
			MaxConcurrentPos:         3,
			MaxGrossExposure:         1.0,
			MaxPositionPct:           0.20,
			MaxPairCorrelation:       *maxPairCorr,
			MinLiquidityUSD:          *minLiquidityUSD,
			MinDecisionConfidence:    *minConfidence,
			ExecutionCommissionBps:   *commissionBps,
			ExecutionSlippageBps:     *slippageBps,
			ExecutionImpactBps:       *executionImpactBps,
			MaxParticipationRate:     *maxParticipationRate,
			DrawdownThrottleStartPct: *drawdownThrottleStart,
			DrawdownThrottleMinScale: *drawdownThrottleMinScale,
			MaxPortfolioHeatPct:      *maxPortfolioHeatPct,
			MaxNetExposurePct:        *maxNetExposurePct,
			LossStreakPauseThreshold: *lossStreakPauseThreshold,
			LossStreakPauseCycles:    *lossStreakPauseCycles,
			PerformanceRiskLookback:  *performanceRiskLookback,
			VolatilityBrakeTargetPct: *volatilityBrakeTargetPct,
			VolatilityBrakeLookback:  *volatilityBrakeLookback,
			VolatilityBrakeMinScale:  *volatilityBrakeMinScale,
			KellyFractionCap:         *kellyFractionCap,
			KellyLookback:            *kellyLookback,
			KellyMinTrades:           *kellyMinTrades,
			MarketStressEntryBlock:   *marketStressEntryBlock,
			MarketStressRiskMinScale: *marketStressRiskMinScale,
			UseNewsRisk:              *useNewsRisk,
			EnableNewsInReplay:       *enableNewsInReplay,
			NewsProvider:             *newsProvider,
			NewsLookbackMinutes:      *newsLookbackMinutes,
			NewsRefreshSeconds:       *newsRefreshSeconds,
			NewsMarketImpactThresh:   *newsMarketImpactThresh,
			NewsSymbolImpactThresh:   *newsSymbolImpactThresh,
			NewsHardBlockThresh:      *newsHardBlockThresh,
			NewsMaxRiskReduction:     *newsMaxRiskReduction,
			RiskPerTradePct:          0.0075,
			ProfitLockThreshold:      1.25,
			TrailingStopATRMult:      1.6,
			MaxHoldingCycles:         *maxCycles,
			SymbolCooldownCycles:     6,
			AllowShort:               true,
			UseMacroFilters:          true,
			DynamicPositionSizing:    true,
			RegimeRiskScaling:        *regimeRiskScale,
			BenchmarkSymbols:         []string{"SPY", "QQQ", "IWM", "DIA"},
			InitialBalance:           *initialBalance,
			ScanInterval:             time.Second,
			MaxCycles:                *maxCycles,
			ReplayWarmupBars:         *warmupBars,
			DeepSeekKey:              *deepseekKey,
			QwenKey:                  *qwenKey,
			UseQwen:                  strings.EqualFold(*aiModel, "qwen"),
			CustomAPIURL:             *customAPIURL,
			CustomAPIKey:             *customAPIKey,
			CustomModelName:          *customModelName,
		}
		decisionLogDir := filepath.Join(profileDir, "decision_logs", atCfg.ID)

		bt, err := trader.NewAutoTrader(atCfg)
		if err != nil {
			log.Printf("Skipping %s: trader init failed: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}

		log.Printf("Running profile %s ...", profileSlug)
		if err := bt.RunBacktest(*maxCycles); err != nil {
			log.Printf("Profile %s failed: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}
		finished := time.Now()

		summaryPath := filepath.Join(profileDir, "output", "replay_summary.json")
		summary, err := readReplaySummary(summaryPath)
		if err != nil {
			log.Printf("Profile %s failed to read replay summary: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}
		equityCurvePath := filepath.Join(profileDir, "output", "equity_curve.csv")
		firstHalfRet, secondHalfRet, robustness := 0.0, 0.0, 0.0
		ulcerIndexPct, segmentStability := 0.0, 0.0
		calmarRatio, cvar95Pct, tailRatio := 0.0, 0.0, 0.0
		if points, curveErr := readEquityCurve(equityCurvePath); curveErr == nil {
			firstHalfRet, secondHalfRet = splitHalfReturns(points)
			robustness = returnConsistencyScore(firstHalfRet, secondHalfRet)
			ulcerIndexPct = equityUlcerIndexPct(points)
			segmentStability = segmentStabilityScore(points, 6)
			calmarRatio = equityCalmarRatio(points)
			cvar95Pct = equityCVaR95Pct(points)
			tailRatio = equityTailRatio(points)
		}
		returnPerFee := returnPerFeeScore(summary.ReturnPct, summary.TotalFeesUSD, summary.FinalEquity)
		pipelineSummaryPath := filepath.Join(profileDir, "output", "pipeline_summary.json")
		pipelineAttributionCSVPath := filepath.Join(profileDir, "output", "pipeline_attribution.csv")
		pipelineSummary, pipelineErr := analyzePipelineBacktest(decisionLogDir)
		if pipelineErr != nil {
			log.Printf("Profile %s failed to analyze pipeline backtest: %v", profileSlug, pipelineErr)
		} else {
			if err := writePipelineSummaryJSON(pipelineSummaryPath, pipelineSummary); err != nil {
				log.Printf("Profile %s failed to write pipeline summary: %v", profileSlug, err)
			}
			if err := writePipelineAttributionCSV(pipelineAttributionCSVPath, pipelineSummary); err != nil {
				log.Printf("Profile %s failed to write pipeline attribution csv: %v", profileSlug, err)
			}
		}
		tradesPath := filepath.Join(profileDir, "output", "trades.csv")
		tradeStats, tradeStatsErr := readTradeStudyStats(tradesPath)
		mcP05, mcP50, mcWinPct := 0.0, 0.0, 0.0
		if *mcSims > 0 && tradeStatsErr == nil && len(tradeStats.ClosedTradePnLs) > 0 {
			if tradePnLs := tradeStats.ClosedTradePnLs; len(tradePnLs) > 0 {
				mcProfileSeed := seed + int64((idx+1)*7919)
				mcP05, mcP50, mcWinPct = monteCarloTradeReturnStats(tradePnLs, *initialBalance, *mcSims, mcProfileSeed)
			}
		}
		tradedSymbols, tradeHHI, diversification := 0, 1.0, 0.0
		avgTradesPerActiveSymbol, dominantSymbolTradeShare := 0.0, 0.0
		if tradeStatsErr == nil {
			tradedSymbols = tradeStats.TradedSymbols
			tradeHHI = tradeStats.TradeHHI
			diversification = tradeStats.Diversification
			avgTradesPerActiveSymbol = tradeStats.AvgTradesPerActiveSymbol
			dominantSymbolTradeShare = tradeStats.DominantSymbolTradeShare
		}

		status := bt.GetStatus()
		cyclesExecuted := intFromAny(status["call_count"])
		activeBarsTested := cyclesExecuted
		if datasetStats.OverlapBars > 0 && activeBarsTested > datasetStats.OverlapBars {
			activeBarsTested = datasetStats.OverlapBars
		}
		activeDaysEstimate := estimateActiveDays(activeBarsTested, *barInterval)
		if activeDaysEstimate == 0 && datasetStats.OverlapBars > 0 && datasetStats.StudyWindowDays > 0 && activeBarsTested > 0 {
			activeDaysEstimate = (float64(activeBarsTested) / float64(datasetStats.OverlapBars)) * float64(datasetStats.StudyWindowDays)
		}

		studyStart := ""
		if !datasetStats.DataStart.IsZero() {
			studyStart = datasetStats.DataStart.Format(time.RFC3339)
		}
		studyEnd := ""
		if !datasetStats.DataEnd.IsZero() {
			studyEnd = datasetStats.DataEnd.Format(time.RFC3339)
		}

		relSummary, _ := filepath.Rel(runRoot, summaryPath)
		relPipelineSummary, _ := filepath.Rel(runRoot, pipelineSummaryPath)
		relProfile, _ := filepath.Rel(runRoot, profileDir)
		results = append(results, profileResult{
			ProfileSlug:              profileSlug,
			StrategyMode:             profile.StrategyMode,
			MinScore:                 profile.MinScore,
			PositionPct:              profile.PositionPct,
			ConfiguredSymbolCount:    len(configuredSymbols),
			UsableSymbolCount:        len(availableSymbols),
			CoverageRatio:            datasetStats.CoverageRatio,
			SymbolCount:              len(availableSymbols),
			CyclesExecuted:           cyclesExecuted,
			DurationSeconds:          finished.Sub(started).Seconds(),
			StartedAt:                started,
			FinishedAt:               finished,
			StudyStart:               studyStart,
			StudyEnd:                 studyEnd,
			StudyWindowDays:          datasetStats.StudyWindowDays,
			MinBarsAvailable:         datasetStats.MinBarsPerSymbol,
			MedianBarsAvailable:      datasetStats.MedianBarsPerSymbol,
			MaxBarsAvailable:         datasetStats.MaxBarsPerSymbol,
			OverlapBarsAvailable:     datasetStats.OverlapBars,
			ActiveBarsTested:         activeBarsTested,
			ActiveDaysEstimate:       activeDaysEstimate,
			TotalTrades:              summary.TotalTrades,
			WinRatePct:               summary.WinRatePct,
			MaxDrawdownPct:           summary.MaxDrawdown,
			FinalEquity:              summary.FinalEquity,
			ReturnPct:                summary.ReturnPct,
			SharpeRatio:              summary.SharpeRatio,
			SortinoRatio:             summary.SortinoRatio,
			ProfitFactor:             summary.ProfitFactor,
			ExpectancyUSD:            summary.ExpectancyUSD,
			AvgWinUSD:                summary.AvgWinUSD,
			AvgLossUSD:               summary.AvgLossUSD,
			TotalFeesUSD:             summary.TotalFeesUSD,
			PartialFills:             summary.PartialFills,
			RejectedFills:            summary.RejectedFills,
			FirstHalfReturnPct:       firstHalfRet,
			SecondHalfReturnPct:      secondHalfRet,
			RobustnessScore:          robustness,
			MonteCarloP05Pct:         mcP05,
			MonteCarloP50Pct:         mcP50,
			MonteCarloWinPct:         mcWinPct,
			TradedSymbols:            tradedSymbols,
			TradeHHI:                 tradeHHI,
			Diversification:          diversification,
			UlcerIndexPct:            ulcerIndexPct,
			SegmentStability:         segmentStability,
			CalmarRatio:              calmarRatio,
			CVaR95Pct:                cvar95Pct,
			TailRatio:                tailRatio,
			ReturnPerFee:             returnPerFee,
			AvgTradesPerActiveSymbol: avgTradesPerActiveSymbol,
			DominantSymbolTradeShare: dominantSymbolTradeShare,
			ReplaySummaryRel:         filepath.ToSlash(relSummary),
			PipelineSummaryRel:       filepath.ToSlash(relPipelineSummary),
			WorkDirRel:               filepath.ToSlash(relProfile),
		})

		_ = os.Chdir(origWD)
	}

	if len(results) == 0 {
		log.Fatal("no profile completed successfully")
	}

	for i := range results {
		results[i].CompositeScore = riskAdjustedScore(results[i], *minTradesForScore, *minTradedSymbols)
		assessment := assessProfileEvidence(results[i], thresholds, *minTradedSymbols)
		results[i].EvidenceScore = assessment.EvidenceScore
		results[i].CredibilityTier = assessment.CredibilityTier
		results[i].RankingEligible = assessment.RankingEligible
		results[i].QualityFlags = assessment.QualityFlags
		results[i].QualitySummary = assessment.QualitySummary
		results[i].RankingScore = assessment.RankingScore
	}

	sortProfileResults(results)

	if err := writeResultsJSON(filepath.Join(runRoot, "leaderboard.json"), results); err != nil {
		log.Printf("failed to write leaderboard.json: %v", err)
	}
	if err := writeResultsCSV(filepath.Join(runRoot, "leaderboard.csv"), results); err != nil {
		log.Printf("failed to write leaderboard.csv: %v", err)
	}
	studySummary := buildStudySummary(runID, *studyPreset, *universePreset, *barInterval, datasetStats, thresholds, *maxCycles, *warmupBars, len(profiles), results)
	if err := writeStudySummaryJSON(filepath.Join(runRoot, "study_summary.json"), studySummary); err != nil {
		log.Printf("failed to write study_summary.json: %v", err)
	}
	if err := writeStudySummaryMarkdown(filepath.Join(runRoot, "study_summary.md"), studySummary); err != nil {
		log.Printf("failed to write study_summary.md: %v", err)
	}
	if out := strings.TrimSpace(*writeBestProfile); out != "" {
		if !filepath.IsAbs(out) {
			out = filepath.Join(runRoot, out)
		}
		best := results[0]
		if !best.RankingEligible {
			log.Printf("warning: writing best profile config from an under-sampled result because no ranking-eligible profile was available")
		}
		if err := writeBestProfileConfig(out, best, availableSymbols, *initialBalance, *candidateBatch, *maxPairCorr, *minLiquidityUSD, *minConfidence, *regimeRiskScale, *commissionBps, *slippageBps, *executionImpactBps, *maxParticipationRate, *drawdownThrottleStart, *drawdownThrottleMinScale, *maxPortfolioHeatPct, *maxNetExposurePct, *lossStreakPauseThreshold, *lossStreakPauseCycles, *performanceRiskLookback, *volatilityBrakeTargetPct, *volatilityBrakeLookback, *volatilityBrakeMinScale, *kellyFractionCap, *kellyLookback, *kellyMinTrades, *marketStressEntryBlock, *marketStressRiskMinScale, *useNewsRisk, *enableNewsInReplay, *newsProvider, *newsLookbackMinutes, *newsRefreshSeconds, *newsMarketImpactThresh, *newsSymbolImpactThresh, *newsHardBlockThresh, *newsMaxRiskReduction); err != nil {
			log.Printf("failed to write best profile config: %v", err)
		} else {
			log.Printf("Best profile config written to %s", out)
		}
	}
	if err := registerBacktestExperiment(runID, runRoot, origWD, dataDir, *symbolsFile, configuredSymbols, availableSymbols, datasetStats, studySummary, results, effectiveParams); err != nil {
		log.Printf("failed to register experiment manifest: %v", err)
	} else {
		log.Printf("Experiment manifest registered for %s", runID)
	}

	log.Printf("Backtests completed. Results in %s", runRoot)
	log.Printf("Study evidence summary: eligible=%d | credible=%d | provisional=%d | insufficient=%d",
		studySummary.RankingEligibleProfiles,
		studySummary.CredibleProfiles,
		studySummary.ProvisionalProfiles,
		studySummary.InsufficientProfiles,
	)
	for _, warning := range studySummary.Warnings {
		log.Printf("  warning: %s", warning)
	}
	log.Println("Top profiles:")
	for i, r := range results {
		if i >= 10 {
			break
		}
		flags := strings.Join(r.QualityFlags, ",")
		if flags == "" {
			flags = "none"
		}
		log.Printf("  %d) %s | rank=%.3f | perf=%.3f | evidence=%.2f | tier=%s | return=%.2f%% | mcP05=%.2f%% | sharpe=%.2f | maxDD=%.2f%% | trades=%d | traded=%d | usable=%d/%d | activeBars=%d | flags=%s",
			i+1, r.ProfileSlug, r.RankingScore, r.CompositeScore, r.EvidenceScore, r.CredibilityTier, r.ReturnPct, r.MonteCarloP05Pct, r.SharpeRatio, r.MaxDrawdownPct, r.TotalTrades, r.TradedSymbols, r.UsableSymbolCount, r.ConfiguredSymbolCount, r.ActiveBarsTested, flags)
	}
}

func canRunAIProfile(aiModel, deepseekKey, qwenKey, customURL, customKey, customModel string) bool {
	switch strings.ToLower(strings.TrimSpace(aiModel)) {
	case "qwen":
		return strings.TrimSpace(qwenKey) != ""
	case "custom":
		return strings.TrimSpace(customURL) != "" &&
			strings.TrimSpace(customKey) != "" &&
			strings.TrimSpace(customModel) != ""
	default:
		return strings.TrimSpace(deepseekKey) != ""
	}
}

func parseProfiles(raw string) ([]strategyProfile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("profiles cannot be empty")
	}

	specs := strings.Split(raw, ",")
	profiles := make([]strategyProfile, 0, len(specs))
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid profile '%s' (expected strategy:minScore:positionPct)", spec)
		}

		mode := strings.ToLower(strings.TrimSpace(parts[0]))
		switch mode {
		case "ai_only", "momentum_fallback", "momentum_only", "multi_factor", "hybrid_ai":
		default:
			return nil, fmt.Errorf("unsupported strategy mode '%s' in profile '%s'", mode, spec)
		}

		score, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid minScore in profile '%s': %w", spec, err)
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid positionPct in profile '%s': %w", spec, err)
		}

		profiles = append(profiles, strategyProfile{
			StrategyMode: mode,
			MinScore:     score,
			PositionPct:  pct,
		})
	}
	return profiles, nil
}

func buildProfileGrid(strategyCSV, scoreCSV, positionCSV string) ([]strategyProfile, error) {
	strategies := parseStringGrid(strategyCSV)
	if len(strategies) == 0 {
		return nil, fmt.Errorf("strategy-grid cannot be empty")
	}
	for _, mode := range strategies {
		switch mode {
		case "ai_only", "momentum_fallback", "momentum_only", "multi_factor", "hybrid_ai":
		default:
			return nil, fmt.Errorf("unsupported strategy mode '%s' in strategy-grid", mode)
		}
	}

	scores, err := parseFloatGrid(scoreCSV)
	if err != nil {
		return nil, fmt.Errorf("invalid score-grid: %w", err)
	}
	if len(scores) == 0 {
		return nil, fmt.Errorf("score-grid cannot be empty")
	}

	positions, err := parseFloatGrid(positionCSV)
	if err != nil {
		return nil, fmt.Errorf("invalid position-grid: %w", err)
	}
	if len(positions) == 0 {
		return nil, fmt.Errorf("position-grid cannot be empty")
	}

	profiles := make([]strategyProfile, 0, len(strategies)*len(scores)*len(positions))
	seen := make(map[string]struct{})
	for _, mode := range strategies {
		for _, score := range scores {
			if (mode == "momentum_only" || mode == "momentum_fallback") && score < 0.8 {
				continue
			}
			for _, pct := range positions {
				if pct <= 0 {
					continue
				}
				p := strategyProfile{
					StrategyMode: mode,
					MinScore:     score,
					PositionPct:  pct,
				}
				key := p.slug()
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				profiles = append(profiles, p)
			}
		}
	}
	return profiles, nil
}

func parseStringGrid(raw string) []string {
	out := make([]string, 0, 16)
	seen := make(map[string]struct{})
	for _, part := range strings.Split(strings.TrimSpace(raw), ",") {
		item := strings.ToLower(strings.TrimSpace(part))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func parseFloatGrid(raw string) ([]float64, error) {
	items := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]float64, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		value, err := strconv.ParseFloat(item, 64)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%.6f", value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func envOrFirst(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func envOrDefault(fallback string, names ...string) string {
	if value := envOrFirst(names...); value != "" {
		return value
	}
	return fallback
}

func resolveSymbols(symbolsCSV, symbolsFile string) ([]string, error) {
	if strings.TrimSpace(symbolsCSV) != "" {
		return dedupeSymbols(strings.Split(symbolsCSV, ",")), nil
	}
	data, err := os.ReadFile(symbolsFile)
	if err != nil {
		return nil, err
	}

	symbols := make([]string, 0, 1024)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		for _, token := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		}) {
			symbols = append(symbols, token)
		}
	}
	return dedupeSymbols(symbols), nil
}

func dedupeSymbols(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		symbol := strings.ToUpper(strings.TrimSpace(strings.Trim(raw, "\"'")))
		if symbol == "" {
			continue
		}
		if symbol == "SYMBOL" || symbol == "TICKER" || symbol == "ACTSYMBOL" || symbol == "CQSSYMBOL" {
			continue
		}
		if !isValidSymbol(symbol) {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func isValidSymbol(symbol string) bool {
	for _, ch := range symbol {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func downloadHistory(gatewayURL, accountID, sessionCookie string, symbols []string, interval string, limit int, minBars int, outDir string) ([]string, error) {
	client := broker.NewIBKRClient(gatewayURL, accountID, sessionCookie)
	provider := &market.IBKRProvider{Client: client}
	if minBars <= 0 {
		minBars = 80
	}

	okSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		barsMap, err := provider.GetBars([]string{symbol}, interval, limit)
		if err != nil {
			log.Printf("  [%s] download failed: %v", symbol, err)
			continue
		}

		bars := barsMap[symbol]
		if len(bars) == 0 {
			log.Printf("  [%s] no bars returned", symbol)
			continue
		}
		if len(bars) < minBars {
			log.Printf("  [%s] skipped: only %d bars (min=%d)", symbol, len(bars), minBars)
			continue
		}

		if err := writeBarsCSV(filepath.Join(outDir, strings.ToUpper(symbol)+".csv"), bars); err != nil {
			log.Printf("  [%s] failed to write csv: %v", symbol, err)
			continue
		}

		okSymbols = append(okSymbols, strings.ToUpper(symbol))
		log.Printf("  [%s] wrote %d bars", symbol, len(bars))
	}

	return dedupeSymbols(okSymbols), nil
}

func writeBarsCSV(path string, bars []market.Kline) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		return err
	}
	for _, b := range bars {
		row := []string{
			strconv.FormatInt(b.OpenTime, 10),
			strconv.FormatFloat(b.Open, 'f', 8, 64),
			strconv.FormatFloat(b.High, 'f', 8, 64),
			strconv.FormatFloat(b.Low, 'f', 8, 64),
			strconv.FormatFloat(b.Close, 'f', 8, 64),
			strconv.FormatFloat(b.Volume, 'f', 8, 64),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func countCSVDataRows(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return 0, err
	}
	if len(rows) <= 1 {
		return 0, nil
	}
	return len(rows) - 1, nil
}

func readReplaySummary(path string) (*replaySummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s replaySummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeResultsJSON(path string, results []profileResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeResultsCSV(path string, results []profileResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"rank",
		"profile_slug",
		"ranking_score",
		"composite_score",
		"evidence_score",
		"credibility_tier",
		"ranking_eligible",
		"quality_flags",
		"quality_summary",
		"strategy_mode",
		"min_score",
		"position_pct",
		"configured_symbol_count",
		"usable_symbol_count",
		"coverage_ratio",
		"symbol_count",
		"cycles_executed",
		"duration_seconds",
		"study_start",
		"study_end",
		"study_window_days",
		"min_bars_available",
		"median_bars_available",
		"max_bars_available",
		"overlap_bars_available",
		"active_bars_tested",
		"active_days_estimate",
		"total_trades",
		"win_rate_pct",
		"max_drawdown_pct",
		"final_equity",
		"return_pct",
		"sharpe_ratio",
		"sortino_ratio",
		"profit_factor",
		"expectancy_usd",
		"avg_win_usd",
		"avg_loss_usd",
		"total_fees_usd",
		"partial_fills",
		"rejected_fills",
		"first_half_return_pct",
		"second_half_return_pct",
		"robustness_score",
		"mc_p05_return_pct",
		"mc_p50_return_pct",
		"mc_positive_rate_pct",
		"traded_symbols",
		"trade_hhi",
		"diversification_score",
		"ulcer_index_pct",
		"segment_stability_score",
		"calmar_ratio",
		"cvar95_pct",
		"tail_ratio",
		"return_per_fee",
		"avg_trades_per_active_symbol",
		"dominant_symbol_trade_share",
		"replay_summary_rel",
		"pipeline_summary_rel",
		"work_dir_rel",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for i, r := range results {
		row := []string{
			strconv.Itoa(i + 1),
			r.ProfileSlug,
			fmt.Sprintf("%.4f", r.RankingScore),
			fmt.Sprintf("%.4f", r.CompositeScore),
			fmt.Sprintf("%.4f", r.EvidenceScore),
			r.CredibilityTier,
			strconv.FormatBool(r.RankingEligible),
			strings.Join(r.QualityFlags, ";"),
			r.QualitySummary,
			r.StrategyMode,
			fmt.Sprintf("%.4f", r.MinScore),
			fmt.Sprintf("%.4f", r.PositionPct),
			strconv.Itoa(r.ConfiguredSymbolCount),
			strconv.Itoa(r.UsableSymbolCount),
			fmt.Sprintf("%.4f", r.CoverageRatio),
			strconv.Itoa(r.SymbolCount),
			strconv.Itoa(r.CyclesExecuted),
			fmt.Sprintf("%.2f", r.DurationSeconds),
			r.StudyStart,
			r.StudyEnd,
			strconv.Itoa(r.StudyWindowDays),
			strconv.Itoa(r.MinBarsAvailable),
			fmt.Sprintf("%.1f", r.MedianBarsAvailable),
			strconv.Itoa(r.MaxBarsAvailable),
			strconv.Itoa(r.OverlapBarsAvailable),
			strconv.Itoa(r.ActiveBarsTested),
			fmt.Sprintf("%.2f", r.ActiveDaysEstimate),
			strconv.Itoa(r.TotalTrades),
			fmt.Sprintf("%.2f", r.WinRatePct),
			fmt.Sprintf("%.2f", r.MaxDrawdownPct),
			fmt.Sprintf("%.2f", r.FinalEquity),
			fmt.Sprintf("%.2f", r.ReturnPct),
			fmt.Sprintf("%.4f", r.SharpeRatio),
			fmt.Sprintf("%.4f", r.SortinoRatio),
			fmt.Sprintf("%.4f", r.ProfitFactor),
			fmt.Sprintf("%.2f", r.ExpectancyUSD),
			fmt.Sprintf("%.2f", r.AvgWinUSD),
			fmt.Sprintf("%.2f", r.AvgLossUSD),
			fmt.Sprintf("%.2f", r.TotalFeesUSD),
			strconv.Itoa(r.PartialFills),
			strconv.Itoa(r.RejectedFills),
			fmt.Sprintf("%.4f", r.FirstHalfReturnPct),
			fmt.Sprintf("%.4f", r.SecondHalfReturnPct),
			fmt.Sprintf("%.4f", r.RobustnessScore),
			fmt.Sprintf("%.4f", r.MonteCarloP05Pct),
			fmt.Sprintf("%.4f", r.MonteCarloP50Pct),
			fmt.Sprintf("%.4f", r.MonteCarloWinPct),
			strconv.Itoa(r.TradedSymbols),
			fmt.Sprintf("%.6f", r.TradeHHI),
			fmt.Sprintf("%.4f", r.Diversification),
			fmt.Sprintf("%.4f", r.UlcerIndexPct),
			fmt.Sprintf("%.4f", r.SegmentStability),
			fmt.Sprintf("%.4f", r.CalmarRatio),
			fmt.Sprintf("%.4f", r.CVaR95Pct),
			fmt.Sprintf("%.4f", r.TailRatio),
			fmt.Sprintf("%.4f", r.ReturnPerFee),
			fmt.Sprintf("%.4f", r.AvgTradesPerActiveSymbol),
			fmt.Sprintf("%.4f", r.DominantSymbolTradeShare),
			r.ReplaySummaryRel,
			r.PipelineSummaryRel,
			r.WorkDirRel,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func readEquityCurve(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return nil, fmt.Errorf("equity curve has no data rows")
	}

	points := make([]float64, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 2 {
			continue
		}
		eq, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		if err != nil || eq <= 0 {
			continue
		}
		points = append(points, eq)
	}
	if len(points) < 4 {
		return nil, fmt.Errorf("insufficient equity curve points")
	}
	return points, nil
}

func readClosedTradePnLs(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return nil, fmt.Errorf("trades file has no rows")
	}

	header := rows[0]
	actionIdx := -1
	pnlIdx := -1
	for i, h := range header {
		col := strings.ToLower(strings.TrimSpace(h))
		switch col {
		case "action":
			actionIdx = i
		case "realized_pnl":
			pnlIdx = i
		}
	}
	if actionIdx < 0 || pnlIdx < 0 {
		return nil, fmt.Errorf("trades file missing action/realized_pnl columns")
	}

	pnls := make([]float64, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= pnlIdx || len(row) <= actionIdx {
			continue
		}
		action := strings.ToUpper(strings.TrimSpace(row[actionIdx]))
		if !strings.HasPrefix(action, "CLOSE_") {
			continue
		}
		pnl, err := strconv.ParseFloat(strings.TrimSpace(row[pnlIdx]), 64)
		if err != nil {
			continue
		}
		pnls = append(pnls, pnl)
	}
	if len(pnls) == 0 {
		return nil, fmt.Errorf("no closed trades parsed")
	}
	return pnls, nil
}

func readTradeDiversity(path string) (int, float64, float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return 0, 0, 0, err
	}
	if len(rows) <= 1 {
		return 0, 0, 0, fmt.Errorf("trades file has no rows")
	}

	header := rows[0]
	actionIdx := -1
	symbolIdx := -1
	for i, h := range header {
		col := strings.ToLower(strings.TrimSpace(h))
		switch col {
		case "action":
			actionIdx = i
		case "symbol":
			symbolIdx = i
		}
	}
	if actionIdx < 0 || symbolIdx < 0 {
		return 0, 0, 0, fmt.Errorf("trades file missing action/symbol columns")
	}

	closeCounts := make(map[string]int)
	anyCounts := make(map[string]int)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= actionIdx || len(row) <= symbolIdx {
			continue
		}
		action := strings.ToUpper(strings.TrimSpace(row[actionIdx]))
		symbol := strings.ToUpper(strings.TrimSpace(row[symbolIdx]))
		if symbol == "" || action == "" {
			continue
		}
		anyCounts[symbol]++
		if strings.HasPrefix(action, "CLOSE_") {
			closeCounts[symbol]++
		}
	}

	counts := closeCounts
	if len(counts) == 0 {
		counts = anyCounts
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0, 0, 0, fmt.Errorf("no trade rows parsed")
	}

	hhi := 0.0
	for _, c := range counts {
		share := float64(c) / float64(total)
		hhi += share * share
	}
	tradedSymbols := len(counts)
	diversification := 0.0
	if tradedSymbols > 1 {
		minHHI := 1.0 / float64(tradedSymbols)
		diversification = clamp((1.0-hhi)/(1.0-minHHI), 0.0, 1.0)
	}
	return tradedSymbols, hhi, diversification, nil
}

func monteCarloTradeReturnStats(tradePnLs []float64, initialBalance float64, sims int, seed int64) (float64, float64, float64) {
	if len(tradePnLs) == 0 || sims <= 0 || initialBalance <= 0 {
		return 0, 0, 0
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	returns := make([]float64, 0, sims)
	positive := 0

	for i := 0; i < sims; i++ {
		equity := initialBalance
		for j := 0; j < len(tradePnLs); j++ {
			equity += tradePnLs[rng.Intn(len(tradePnLs))]
		}
		retPct := ((equity - initialBalance) / initialBalance) * 100.0
		returns = append(returns, retPct)
		if retPct > 0 {
			positive++
		}
	}
	sort.Float64s(returns)
	p05 := percentile(returns, 0.05)
	p50 := percentile(returns, 0.50)
	posPct := (float64(positive) / float64(sims)) * 100.0
	return p05, p50, posPct
}

func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return values[0]
	}
	if q >= 1 {
		return values[len(values)-1]
	}
	pos := q * float64(len(values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return values[lo]
	}
	frac := pos - float64(lo)
	return values[lo]*(1.0-frac) + values[hi]*frac
}

func splitHalfReturns(points []float64) (float64, float64) {
	if len(points) < 4 {
		return 0, 0
	}
	mid := len(points) / 2
	if mid < 1 || mid >= len(points) {
		return 0, 0
	}
	start := points[0]
	midVal := points[mid-1]
	end := points[len(points)-1]
	if start <= 0 || midVal <= 0 {
		return 0, 0
	}

	first := ((midVal - start) / start) * 100.0
	second := 0.0
	if midVal > 0 {
		second = ((end - midVal) / midVal) * 100.0
	}
	return first, second
}

func returnConsistencyScore(firstHalfPct, secondHalfPct float64) float64 {
	sumAbs := math.Abs(firstHalfPct) + math.Abs(secondHalfPct)
	if sumAbs <= 0 {
		return 0
	}
	consistency := 1.0 - (math.Abs(firstHalfPct-secondHalfPct) / (sumAbs + 0.001))
	consistency = clamp(consistency, 0.0, 1.0)
	level := clamp((firstHalfPct+secondHalfPct)/2.0, -1.0, 1.0)

	score := 0.65*consistency + 0.35*(0.5+0.5*level)
	if firstHalfPct <= 0 || secondHalfPct <= 0 {
		score *= 0.55
	}
	return clamp(score, 0.0, 1.0)
}

func equityUlcerIndexPct(points []float64) float64 {
	if len(points) < 3 {
		return 0
	}
	peak := points[0]
	if peak <= 0 {
		return 0
	}
	sumSq := 0.0
	n := 0
	for _, eq := range points {
		if eq > peak {
			peak = eq
		}
		if peak <= 0 {
			continue
		}
		ddPct := ((peak - eq) / peak) * 100.0
		if ddPct < 0 {
			ddPct = 0
		}
		sumSq += ddPct * ddPct
		n++
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sumSq / float64(n))
}

func segmentStabilityScore(points []float64, segments int) float64 {
	if len(points) < 8 {
		return 0
	}
	if segments < 2 {
		segments = 2
	}
	segmentLen := len(points) / segments
	if segmentLen < 2 {
		return 0
	}
	rets := make([]float64, 0, segments)
	for i := 0; i < segments; i++ {
		startIdx := i * segmentLen
		endIdx := (i+1)*segmentLen - 1
		if i == segments-1 {
			endIdx = len(points) - 1
		}
		if startIdx >= len(points) || endIdx <= startIdx {
			continue
		}
		start := points[startIdx]
		end := points[endIdx]
		if start <= 0 || end <= 0 {
			continue
		}
		rets = append(rets, ((end-start)/start)*100.0)
	}
	if len(rets) < 2 {
		return 0
	}
	positive := 0
	sum := 0.0
	for _, v := range rets {
		sum += v
		if v > 0 {
			positive++
		}
	}
	mean := sum / float64(len(rets))
	variance := 0.0
	for _, v := range rets {
		d := v - mean
		variance += d * d
	}
	std := math.Sqrt(variance / float64(len(rets)))
	signComponent := float64(positive) / float64(len(rets))
	stdComponent := clamp(1.0-std/2.5, 0.0, 1.0)
	score := 0.55*signComponent + 0.45*stdComponent
	return clamp(score, 0.0, 1.0)
}

func equityReturns(points []float64) []float64 {
	if len(points) < 3 {
		return nil
	}
	returns := make([]float64, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		prev := points[i-1]
		curr := points[i]
		if prev <= 0 || curr <= 0 {
			continue
		}
		returns = append(returns, (curr-prev)/prev)
	}
	return returns
}

func equityCalmarRatio(points []float64) float64 {
	if len(points) < 4 {
		return 0
	}
	start := points[0]
	end := points[len(points)-1]
	if start <= 0 || end <= 0 {
		return 0
	}
	retPct := ((end - start) / start) * 100.0
	maxDDPct := 0.0
	peak := start
	for _, eq := range points {
		if eq > peak {
			peak = eq
		}
		if peak <= 0 {
			continue
		}
		ddPct := ((peak - eq) / peak) * 100.0
		if ddPct > maxDDPct {
			maxDDPct = ddPct
		}
	}
	if maxDDPct <= 0 {
		if retPct > 0 {
			return 5.0
		}
		return 0
	}
	return retPct / maxDDPct
}

func equityCVaR95Pct(points []float64) float64 {
	returns := equityReturns(points)
	if len(returns) < 8 {
		return 0
	}
	sorted := append([]float64(nil), returns...)
	sort.Float64s(sorted)
	tailN := int(math.Ceil(float64(len(sorted)) * 0.05))
	if tailN < 1 {
		tailN = 1
	}
	sum := 0.0
	for i := 0; i < tailN && i < len(sorted); i++ {
		sum += sorted[i]
	}
	return (sum / float64(tailN)) * 100.0
}

func equityTailRatio(points []float64) float64 {
	returns := equityReturns(points)
	if len(returns) < 12 {
		return 0
	}
	sorted := append([]float64(nil), returns...)
	sort.Float64s(sorted)
	topQ := percentile(sorted, 0.95)
	botQ := percentile(sorted, 0.05)
	if botQ >= 0 {
		if topQ > 0 {
			return 5.0
		}
		return 0
	}
	return topQ / math.Abs(botQ)
}

func returnPerFeeScore(returnPct, totalFeesUSD, finalEquity float64) float64 {
	if finalEquity <= 0 {
		return 0
	}
	feePct := 0.0
	if totalFeesUSD > 0 {
		feePct = (totalFeesUSD / finalEquity) * 100.0
	}
	return returnPct / (feePct + 0.01)
}

func riskAdjustedScore(r profileResult, minTrades, minTradedSymbols int) float64 {
	returnComponent := clamp(r.ReturnPct/15.0, -1.0, 2.0)
	drawdownComponent := clamp(1.0-r.MaxDrawdownPct/20.0, 0.0, 1.0)
	sharpeComponent := clamp(r.SharpeRatio/2.5, -1.0, 2.0)
	sortinoComponent := clamp(r.SortinoRatio/3.0, -1.0, 2.0)
	profitFactorComponent := clamp((r.ProfitFactor-1.0)/1.5, -1.0, 2.0)
	winRateComponent := clamp(r.WinRatePct/100.0, 0.0, 1.0)
	tradeActivity := clamp(float64(r.TotalTrades)/25.0, 0.0, 1.0)
	robustnessComponent := clamp(r.RobustnessScore, 0.0, 1.0)
	mcDownsideComponent := clamp(r.MonteCarloP05Pct/8.0, -1.0, 2.0)
	mcMedianComponent := clamp(r.MonteCarloP50Pct/12.0, -1.0, 2.0)
	mcWinRateComponent := clamp(r.MonteCarloWinPct/100.0, 0.0, 1.0)
	diversificationComponent := clamp(r.Diversification, 0.0, 1.0)
	ulcerComponent := clamp(1.0-r.UlcerIndexPct/5.0, 0.0, 1.0)
	stabilityComponent := clamp(r.SegmentStability, 0.0, 1.0)
	calmarComponent := clamp(r.CalmarRatio/2.0, -1.0, 2.0)
	cvarComponent := clamp(r.CVaR95Pct/2.5, -2.0, 1.0)
	tailComponent := clamp((r.TailRatio-1.0)/1.5, -1.0, 2.0)
	feeEfficiencyComponent := clamp(r.ReturnPerFee/10.0, -1.0, 2.0)
	feePenalty := 0.0
	if r.FinalEquity > 0 && r.TotalFeesUSD > 0 {
		feePenalty = clamp((r.TotalFeesUSD/r.FinalEquity)*100.0/3.0, 0.0, 0.45)
	}
	executionPenalty := 0.0
	if totalExec := r.TotalTrades + r.PartialFills + r.RejectedFills; totalExec > 0 {
		executionPenalty = clamp(float64(r.RejectedFills)/float64(totalExec), 0.0, 0.18)
	}
	tradePenalty := 0.0
	if minTrades > 0 && r.TotalTrades < minTrades {
		gap := float64(minTrades-r.TotalTrades) / float64(minTrades)
		tradePenalty = clamp(gap*0.28, 0.0, 0.28)
	}
	symbolPenalty := 0.0
	if minTradedSymbols > 0 && r.TradedSymbols < minTradedSymbols {
		gap := float64(minTradedSymbols-r.TradedSymbols) / float64(minTradedSymbols)
		symbolPenalty = clamp(gap*0.20, 0.0, 0.20)
	}

	score := 0.24*returnComponent +
		0.18*sharpeComponent +
		0.10*sortinoComponent +
		0.12*drawdownComponent +
		0.10*profitFactorComponent +
		0.10*winRateComponent +
		0.05*tradeActivity +
		0.03*robustnessComponent +
		0.04*mcDownsideComponent +
		0.02*mcMedianComponent +
		0.02*mcWinRateComponent +
		0.02*diversificationComponent +
		0.03*ulcerComponent +
		0.03*stabilityComponent +
		0.03*calmarComponent +
		0.03*cvarComponent +
		0.03*tailComponent +
		0.03*feeEfficiencyComponent
	return score - feePenalty - tradePenalty - symbolPenalty - executionPenalty
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func writeBestProfileConfig(path string, best profileResult, symbols []string, initialBalance float64, candidateBatch int, maxPairCorr, minLiquidityUSD float64, minConfidence int, regimeRiskScale bool, commissionBps, slippageBps, impactBps, maxParticipation, drawdownStart, drawdownMinScale, maxPortfolioHeat, maxNetExposure float64, lossStreakThreshold, lossStreakCycles, performanceLookback int, volBrakeTarget float64, volBrakeLookback int, volBrakeMinScale float64, kellyFractionCap float64, kellyLookback int, kellyMinTrades int, marketStressEntryBlock float64, marketStressRiskMinScale float64, useNewsRisk bool, enableNewsInReplay bool, newsProvider string, newsLookbackMinutes int, newsRefreshSeconds int, newsMarketImpactThresh float64, newsSymbolImpactThresh float64, newsHardBlockThresh float64, newsMaxRiskReduction float64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if candidateBatch <= 0 {
		candidateBatch = 20
	}
	if candidateBatch > len(symbols) {
		candidateBatch = len(symbols)
	}

	payload := map[string]interface{}{
		"generated_at": time.Now().Format(time.RFC3339),
		"best_profile": best,
		"suggested_trader_config": map[string]interface{}{
			"id":                           "ibkr_autotuned",
			"name":                         "IBKR Auto-Tuned Trader",
			"enabled":                      true,
			"ai_model":                     "deepseek",
			"exchange":                     "ibkr",
			"mode":                         "paper",
			"data_provider":                "ibkr",
			"broker":                       "ibkr",
			"instrument_type":              "equity",
			"strategy_mode":                best.StrategyMode,
			"momentum_min_score":           best.MinScore,
			"min_factor_score":             best.MinScore,
			"fallback_position_pct":        best.PositionPct,
			"initial_balance":              initialBalance,
			"candidate_batch_size":         candidateBatch,
			"max_pair_correlation":         maxPairCorr,
			"min_liquidity_usd":            minLiquidityUSD,
			"min_decision_confidence":      minConfidence,
			"regime_risk_scaling":          regimeRiskScale,
			"execution_commission_bps":     commissionBps,
			"execution_slippage_bps":       slippageBps,
			"execution_impact_bps":         impactBps,
			"max_participation_rate":       maxParticipation,
			"drawdown_throttle_start":      drawdownStart,
			"drawdown_throttle_min_scale":  drawdownMinScale,
			"max_portfolio_heat_pct":       maxPortfolioHeat,
			"max_net_exposure_pct":         maxNetExposure,
			"loss_streak_pause_threshold":  lossStreakThreshold,
			"loss_streak_pause_cycles":     lossStreakCycles,
			"performance_risk_lookback":    performanceLookback,
			"volatility_brake_target_pct":  volBrakeTarget,
			"volatility_brake_lookback":    volBrakeLookback,
			"volatility_brake_min_scale":   volBrakeMinScale,
			"kelly_fraction_cap":           kellyFractionCap,
			"kelly_lookback":               kellyLookback,
			"kelly_min_trades":             kellyMinTrades,
			"market_stress_entry_block":    marketStressEntryBlock,
			"market_stress_risk_min_scale": marketStressRiskMinScale,
			"use_news_risk":                useNewsRisk,
			"enable_news_in_replay":        enableNewsInReplay,
			"news_provider":                newsProvider,
			"news_lookback_minutes":        newsLookbackMinutes,
			"news_refresh_seconds":         newsRefreshSeconds,
			"news_market_impact_thresh":    newsMarketImpactThresh,
			"news_symbol_impact_thresh":    newsSymbolImpactThresh,
			"news_hard_block_thresh":       newsHardBlockThresh,
			"news_max_risk_reduction":      newsMaxRiskReduction,
			"benchmark_symbols":            []string{"SPY", "QQQ", "IWM", "DIA"},
		},
		"symbols": symbols,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func intFromAny(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
