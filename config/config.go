package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TraderConfig  Configuration for a single trader
type TraderConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`             // Whether this trader is enabled
	AIModel  string `json:"ai_model"`            // "qwen" or "deepseek"
	DemoMode bool   `json:"demo_mode,omitempty"` // Synthetic paper demo mode (no broker/API calls)

	// Execution modes
	Mode         string `json:"mode,omitempty"`          // "replay", "paper", "live" (default: "live" if not set and using binance, "paper" if alpaca_paper_trading is true)
	DataProvider string `json:"data_provider,omitempty"` // "csv", "alpaca", "binance"
	Broker       string `json:"broker,omitempty"`        // "sim", "alpaca", "binance"
	CSVDataDir   string `json:"csv_data_dir,omitempty"`  // Path to local historical data for replay mode

	// Exchange selection (choose one)
	Exchange string `json:"exchange"` // "binance", "hyperliquid", "aster", "alpaca"

	// Instrument type
	InstrumentType string `json:"instrument_type,omitempty"` // "crypto_perp" or "equity"

	// Binance config
	BinanceAPIKey    string `json:"binance_api_key,omitempty"`
	BinanceSecretKey string `json:"binance_secret_key,omitempty"`

	// Hyperliquid config
	HyperliquidPrivateKey string `json:"hyperliquid_private_key,omitempty"`
	HyperliquidWalletAddr string `json:"hyperliquid_wallet_addr,omitempty"`
	HyperliquidTestnet    bool   `json:"hyperliquid_testnet,omitempty"`

	// Aster config
	AsterUser       string `json:"aster_user,omitempty"`        // Aster main wallet address
	AsterSigner     string `json:"aster_signer,omitempty"`      // Aster API wallet address
	AsterPrivateKey string `json:"aster_private_key,omitempty"` // Aster API wallet private key

	// Alpaca config
	AlpacaAPIKey       string `json:"alpaca_api_key,omitempty"`
	AlpacaSecretKey    string `json:"alpaca_secret_key,omitempty"`
	AlpacaPaperTrading bool   `json:"alpaca_paper_trading,omitempty"`

	// IBKR config
	IBKRGatewayURL    string `json:"ibkr_gateway_url,omitempty"`
	IBKRAccountID     string `json:"ibkr_account_id,omitempty"`
	IBKRSessionCookie string `json:"ibkr_session_cookie,omitempty"`
	StrictLiveMode    bool   `json:"strict_live_mode,omitempty"` // In live mode, block trading if account endpoints are unhealthy

	// Equity config
	OrderSizingMode       string   `json:"order_sizing_mode,omitempty"`         // "qty" or "notional"
	BarsAdjustment        string   `json:"bars_adjustment,omitempty"`           // "raw", "split", "dividend", "all"
	TrustedSymbolsFile    string   `json:"trusted_symbols_file,omitempty"`      // Optional allowlist file for tradable equity symbols
	StrategyMode          string   `json:"strategy_mode,omitempty"`             // "ai_only", "momentum_fallback", "momentum_only", "multi_factor", or "hybrid_ai"
	MomentumMinScore      float64  `json:"momentum_min_score,omitempty"`        // Minimum score to trigger fallback momentum entries
	FallbackPositionPct   float64  `json:"fallback_position_pct,omitempty"`     // Fallback entry sizing as pct of equity (max 0.20)
	MinFactorScore        float64  `json:"min_factor_score,omitempty"`          // Minimum absolute multi-factor score to open a position
	RiskPerTradePct       float64  `json:"risk_per_trade_pct,omitempty"`        // Fraction of equity risked per trade (e.g., 0.0075 = 0.75%)
	ProfitLockThreshold   float64  `json:"profit_lock_threshold_pct,omitempty"` // Start locking gains above this unrealized PnL %
	TrailingStopATRMult   float64  `json:"trailing_stop_atr_mult,omitempty"`    // ATR multiple for adaptive stop distance
	MaxHoldingCycles      int      `json:"max_holding_cycles,omitempty"`        // Force-close positions older than this many cycles
	MaxConcurrentPos      int      `json:"max_concurrent_positions,omitempty"`  // Maximum concurrent open positions
	SymbolCooldownCycles  int      `json:"symbol_cooldown_cycles,omitempty"`    // Cooldown cycles before reopening a symbol after close
	AllowShort            *bool    `json:"allow_short,omitempty"`               // Default true
	UseMacroFilters       *bool    `json:"use_macro_filters,omitempty"`         // Default true
	DynamicPositionSizing *bool    `json:"dynamic_position_sizing,omitempty"`   // Default true
	BenchmarkSymbols      []string `json:"benchmark_symbols,omitempty"`         // Macro benchmark symbols

	// Equity risk config
	MaxGrossExposure         float64 `json:"max_gross_exposure,omitempty"`           // e.g., 1.0 = 100% of equity
	MaxPositionPct           float64 `json:"max_position_pct,omitempty"`             // e.g., 0.20 = 20% of equity per symbol
	MaxDailyLossPct          float64 `json:"max_daily_loss_pct,omitempty"`           // e.g., 0.02 = 2%
	MaxPairCorrelation       float64 `json:"max_pair_correlation,omitempty"`         // Max allowed abs correlation among same-direction positions
	MinLiquidityUSD          float64 `json:"min_liquidity_usd,omitempty"`            // Minimum estimated dollar volume for entries
	MinDecisionConfidence    int     `json:"min_decision_confidence,omitempty"`      // Drop low-confidence open signals
	RegimeRiskScaling        *bool   `json:"regime_risk_scaling,omitempty"`          // Default true
	ExecutionCommissionBps   float64 `json:"execution_commission_bps,omitempty"`     // Simulated commission per side (bps)
	ExecutionSlippageBps     float64 `json:"execution_slippage_bps,omitempty"`       // Simulated slippage per side (bps)
	ExecutionImpactBps       float64 `json:"execution_impact_bps,omitempty"`         // Extra slippage component scaled by bar participation
	MaxParticipationRate     float64 `json:"max_participation_rate,omitempty"`       // Max fill participation per bar in simulator (0-1]
	DrawdownThrottleStartPct float64 `json:"drawdown_throttle_start,omitempty"`      // Drawdown threshold for risk throttling
	DrawdownThrottleMinScale float64 `json:"drawdown_throttle_min_scale,omitempty"`  // Min risk scale under drawdown throttling
	MaxPortfolioHeatPct      float64 `json:"max_portfolio_heat_pct,omitempty"`       // Max estimated stop-risk budget as a fraction of equity
	MaxNetExposurePct        float64 `json:"max_net_exposure_pct,omitempty"`         // Max absolute net (long-short) exposure as a fraction of equity
	LossStreakPauseThreshold int     `json:"loss_streak_pause_threshold,omitempty"`  // Consecutive losing closes before pausing new entries
	LossStreakPauseCycles    int     `json:"loss_streak_pause_cycles,omitempty"`     // Number of cycles to pause new entries after loss streak
	PerformanceRiskLookback  int     `json:"performance_risk_lookback,omitempty"`    // Closed-trade lookback window for performance-aware risk scaling
	VolatilityBrakeTargetPct float64 `json:"volatility_brake_target_pct,omitempty"`  // Target equity volatility (fraction) for risk brake
	VolatilityBrakeLookback  int     `json:"volatility_brake_lookback,omitempty"`    // Lookback cycles for realized equity volatility
	VolatilityBrakeMinScale  float64 `json:"volatility_brake_min_scale,omitempty"`   // Minimum scale applied by volatility brake
	KellyFractionCap         float64 `json:"kelly_fraction_cap,omitempty"`           // Fraction of Kelly to apply when sizing risk
	KellyLookback            int     `json:"kelly_lookback,omitempty"`               // Closed-trade lookback used for Kelly estimate
	KellyMinTrades           int     `json:"kelly_min_trades,omitempty"`             // Minimum closed trades before Kelly scaling activates
	MarketStressEntryBlock   float64 `json:"market_stress_entry_block,omitempty"`    // Block new entries above this stress score
	MarketStressRiskMinScale float64 `json:"market_stress_risk_min_scale,omitempty"` // Minimum risk scale under stress
	UseNewsRisk              *bool   `json:"use_news_risk,omitempty"`                // Enable headline-driven risk filter
	EnableNewsInReplay       *bool   `json:"enable_news_in_replay,omitempty"`        // Allow news risk in replay mode (off by default)
	NewsProvider             string  `json:"news_provider,omitempty"`                // News provider identifier (default: rss)
	NewsLookbackMinutes      int     `json:"news_lookback_minutes,omitempty"`        // Lookback window for headline aggregation
	NewsRefreshSeconds       int     `json:"news_refresh_seconds,omitempty"`         // Refresh interval for news fetch/cache
	NewsMarketImpactThresh   float64 `json:"news_market_impact_thresh,omitempty"`    // Market-wide news threshold for stricter filtering
	NewsSymbolImpactThresh   float64 `json:"news_symbol_impact_thresh,omitempty"`    // Symbol-level news threshold for blocking entries
	NewsHardBlockThresh      float64 `json:"news_hard_block_thresh,omitempty"`       // Hard block threshold for adverse directional news
	NewsMaxRiskReduction     float64 `json:"news_max_risk_reduction,omitempty"`      // Max multiplicative risk reduction from news

	// AI keys
	QwenKey     string `json:"qwen_key,omitempty"`
	DeepSeekKey string `json:"deepseek_key,omitempty"`

	// Custom AI API (supports any OpenAI-compatible API)
	CustomAPIURL    string `json:"custom_api_url,omitempty"`
	CustomAPIKey    string `json:"custom_api_key,omitempty"`
	CustomModelName string `json:"custom_model_name,omitempty"`

	InitialBalance      float64 `json:"initial_balance"`
	ScanIntervalMinutes int     `json:"scan_interval_minutes"`
	ScanIntervalSeconds int     `json:"scan_interval_seconds,omitempty"`
	CandidateBatchSize  int     `json:"candidate_batch_size,omitempty"` // Number of symbols analyzed per cycle
	MaxCycles           int     `json:"max_cycles,omitempty"`           // Optional finite cycle count (useful for automated backtests)
	ReplayWarmupBars    int     `json:"replay_warmup_bars,omitempty"`   // Replay warmup depth before first cycle
}

// LeverageConfig  Leverage settings
type LeverageConfig struct {
	BTCETHLeverage  int `json:"btc_eth_leverage"` // Leverage for BTC and ETH (main accounts: 550x, sub-accounts 5x)
	AltcoinLeverage int `json:"altcoin_leverage"` // Leverage for altcoins (main accounts: 520x, sub-accounts 5x)
}

// Config  Overall configuration
type Config struct {
	Traders            []TraderConfig `json:"traders"`
	UseDefaultCoins    bool           `json:"use_default_coins"`  // Whether to use default major coin list
	DefaultCoins       []string       `json:"default_coins"`      // Default major coin pool
	DefaultCoinsFile   string         `json:"default_coins_file"` // Optional file path containing one ticker per line
	CoinPoolAPIURL     string         `json:"coin_pool_api_url"`
	OITopAPIURL        string         `json:"oi_top_api_url"`
	APIServerPort      int            `json:"api_server_port"`
	MaxDailyLoss       float64        `json:"max_daily_loss"`
	MaxDrawdown        float64        `json:"max_drawdown"`
	StopTradingMinutes int            `json:"stop_trading_minutes"`
	Leverage           LeverageConfig `json:"leverage"` // Leverage configuration
}

// LoadConfig  Load configuration from file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Load default symbols from file when configured
	if strings.TrimSpace(config.DefaultCoinsFile) != "" {
		coinsFile := strings.TrimSpace(config.DefaultCoinsFile)
		if !filepath.IsAbs(coinsFile) {
			coinsFile = filepath.Join(filepath.Dir(filename), coinsFile)
		}

		coins, err := loadSymbolsFromFile(coinsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load default_coins_file '%s': %w", coinsFile, err)
		}
		config.DefaultCoins = coins
	}
	// Default: if use_default_coins is false and no coin_pool_api_url is provided, use default coin list
	if !config.UseDefaultCoins && config.CoinPoolAPIURL == "" {
		config.UseDefaultCoins = true
	}

	// Set default coin pool
	if len(config.DefaultCoins) == 0 {
		config.DefaultCoins = []string{
			// Major coins
			"BTCUSDT",
			"ETHUSDT",
			"BNBUSDT",
			"SOLUSDT",
			"XRPUSDT",
			"ADAUSDT",
			"DOGEUSDT",

			// High-volume coins
			"AVAXUSDT",
			"DOTUSDT",
			"TRXUSDT",
			"MATICUSDT",
			"LINKUSDT",
			"LTCUSDT",
			"UNIUSDT",
			"ATOMUSDT",

			// High-volatility / trending coins
			"PEPEUSDT",
			"SHIBUSDT",
			"APTUSDT",
			"ARBUSDT",
			"OPUSDT",
			"SUIUSDT",
			"SEIUSDT",
			"INJUSDT",
			"RNDRUSDT",
			"NEARUSDT",

			// Emerging coins (optional additions)
			"TIAUSDT",
			"JTOUSDT",
			"BONKUSDT",
			"HYPEUSDT",
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

func loadSymbolsFromFile(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	symbols := make([]string, 0)
	addSymbol := func(raw string) {
		s := strings.TrimSpace(strings.ToUpper(strings.Trim(raw, "\"'")))
		if s == "" {
			return
		}
		s = strings.ReplaceAll(s, " ", "")
		if s == "SYMBOL" || s == "TICKER" || s == "ACTSYMBOL" || s == "CQSSYMBOL" {
			return
		}
		for _, ch := range s {
			if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' {
				continue
			}
			return
		}
		if !seen[s] {
			seen[s] = true
			symbols = append(symbols, s)
		}
	}

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
			addSymbol(token)
		}
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no valid symbols found")
	}

	return symbols, nil
}

// Validate  Validate configuration fields
func (c *Config) Validate() error {
	if len(c.Traders) == 0 {
		return fmt.Errorf("at least one trader must be configured")
	}

	traderIDs := make(map[string]bool)
	for i, trader := range c.Traders {
		if (trader.DemoMode || trader.Exchange == "demo") && trader.AIModel == "" {
			trader.AIModel = "deepseek"
		}

		if trader.ID == "" {
			return fmt.Errorf("trader[%d]: ID cannot be empty", i)
		}
		if traderIDs[trader.ID] {
			return fmt.Errorf("trader[%d]: duplicate ID '%s'", i, trader.ID)
		}
		traderIDs[trader.ID] = true

		if trader.Name == "" {
			return fmt.Errorf("trader[%d]: Name cannot be empty", i)
		}
		if trader.AIModel != "qwen" && trader.AIModel != "deepseek" && trader.AIModel != "custom" {
			return fmt.Errorf("trader[%d]: ai_model must be 'qwen', 'deepseek', or 'custom'", i)
		}

		// Validate exchange configuration
		if trader.Exchange == "" {
			trader.Exchange = "binance" // Default: Binance
		}
		if trader.Exchange != "binance" && trader.Exchange != "hyperliquid" && trader.Exchange != "aster" && trader.Exchange != "alpaca" && trader.Exchange != "ibkr" && trader.Exchange != "demo" {
			return fmt.Errorf("trader[%d]: exchange must be 'binance', 'hyperliquid', 'aster', 'alpaca', 'ibkr', or 'demo'", i)
		}

		// Validate platform-specific keys
		if trader.DemoMode || trader.Exchange == "demo" {
			trader.DemoMode = true
			if trader.Exchange == "" {
				trader.Exchange = "demo"
			}
			if trader.Mode == "" {
				trader.Mode = "paper"
			}
			if trader.DataProvider == "" {
				trader.DataProvider = "demo"
			}
			if trader.Broker == "" {
				trader.Broker = "sim"
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "equity"
			}
			if trader.StrategyMode == "" {
				trader.StrategyMode = "ai_only"
			}
			if trader.InstrumentType == "equity" {
				if trader.MaxGrossExposure <= 0 {
					trader.MaxGrossExposure = 1.0
				}
				if trader.MaxPositionPct <= 0 {
					trader.MaxPositionPct = 0.20
				}
				if trader.MaxDailyLossPct <= 0 {
					trader.MaxDailyLossPct = 0.05
				}
				if trader.MaxPairCorrelation <= 0 || trader.MaxPairCorrelation >= 1 {
					trader.MaxPairCorrelation = 0.82
				}
				if trader.MinLiquidityUSD <= 0 {
					trader.MinLiquidityUSD = 2_000_000
				}
				if trader.MinDecisionConfidence <= 0 || trader.MinDecisionConfidence > 100 {
					trader.MinDecisionConfidence = 58
				}
				if trader.ExecutionCommissionBps < 0 {
					trader.ExecutionCommissionBps = 0
				}
				if trader.ExecutionSlippageBps < 0 {
					trader.ExecutionSlippageBps = 0
				}
				if trader.ExecutionImpactBps < 0 {
					trader.ExecutionImpactBps = 0
				}
				if trader.MaxParticipationRate <= 0 || trader.MaxParticipationRate > 1 {
					trader.MaxParticipationRate = 0.15
				}
				if trader.DrawdownThrottleStartPct <= 0 {
					trader.DrawdownThrottleStartPct = 0.03
				}
				if trader.DrawdownThrottleMinScale <= 0 || trader.DrawdownThrottleMinScale > 1 {
					trader.DrawdownThrottleMinScale = 0.35
				}
				if trader.MaxPortfolioHeatPct <= 0 || trader.MaxPortfolioHeatPct > 0.30 {
					trader.MaxPortfolioHeatPct = 0.035
				}
				if trader.MaxNetExposurePct <= 0 || trader.MaxNetExposurePct > 1 {
					trader.MaxNetExposurePct = 0.65
				}
				if trader.LossStreakPauseThreshold <= 0 {
					trader.LossStreakPauseThreshold = 3
				}
				if trader.LossStreakPauseCycles <= 0 {
					trader.LossStreakPauseCycles = 5
				}
				if trader.PerformanceRiskLookback <= 0 {
					trader.PerformanceRiskLookback = 20
				}
				if trader.VolatilityBrakeTargetPct <= 0 {
					trader.VolatilityBrakeTargetPct = 0.008
				}
				if trader.VolatilityBrakeLookback <= 0 {
					trader.VolatilityBrakeLookback = 40
				}
				if trader.VolatilityBrakeMinScale <= 0 || trader.VolatilityBrakeMinScale > 1 {
					trader.VolatilityBrakeMinScale = 0.45
				}
				if trader.KellyFractionCap <= 0 || trader.KellyFractionCap > 1 {
					trader.KellyFractionCap = 0.33
				}
				if trader.KellyLookback <= 0 {
					trader.KellyLookback = 30
				}
				if trader.KellyMinTrades <= 0 {
					trader.KellyMinTrades = 10
				}
				if trader.MarketStressEntryBlock <= 0 || trader.MarketStressEntryBlock > 1 {
					trader.MarketStressEntryBlock = 0.82
				}
				if trader.MarketStressRiskMinScale <= 0 || trader.MarketStressRiskMinScale > 1 {
					trader.MarketStressRiskMinScale = 0.35
				}
				if trader.AllowShort == nil {
					v := true
					trader.AllowShort = &v
				}
				if trader.UseMacroFilters == nil {
					v := true
					trader.UseMacroFilters = &v
				}
				if trader.DynamicPositionSizing == nil {
					v := true
					trader.DynamicPositionSizing = &v
				}
				if trader.RegimeRiskScaling == nil {
					v := true
					trader.RegimeRiskScaling = &v
				}
				if trader.UseNewsRisk == nil {
					v := trader.Mode != "replay"
					trader.UseNewsRisk = &v
				}
				if trader.EnableNewsInReplay == nil {
					v := false
					trader.EnableNewsInReplay = &v
				}
				if trader.Mode == "replay" && !*trader.EnableNewsInReplay {
					v := false
					trader.UseNewsRisk = &v
				}
				if trader.NewsProvider == "" {
					trader.NewsProvider = "rss"
				}
				if trader.NewsLookbackMinutes <= 0 {
					trader.NewsLookbackMinutes = 240
				}
				if trader.NewsRefreshSeconds <= 0 {
					trader.NewsRefreshSeconds = 120
				}
				if trader.NewsMarketImpactThresh <= 0 || trader.NewsMarketImpactThresh > 1 {
					trader.NewsMarketImpactThresh = 0.65
				}
				if trader.NewsSymbolImpactThresh <= 0 || trader.NewsSymbolImpactThresh > 1 {
					trader.NewsSymbolImpactThresh = 0.70
				}
				if trader.NewsHardBlockThresh <= 0 || trader.NewsHardBlockThresh > 1 {
					trader.NewsHardBlockThresh = 0.85
				}
				if trader.NewsMaxRiskReduction <= 0 || trader.NewsMaxRiskReduction > 0.95 {
					trader.NewsMaxRiskReduction = 0.55
				}
			}
		} else if trader.Exchange == "binance" {
			if trader.BinanceAPIKey == "" || trader.BinanceSecretKey == "" {
				return fmt.Errorf("trader[%d]: Binance requires both binance_api_key and binance_secret_key", i)
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "crypto_perp"
			}
			if trader.DataProvider == "" {
				trader.DataProvider = "binance"
			}
			if trader.Broker == "" {
				trader.Broker = "binance"
			}
		} else if trader.Exchange == "hyperliquid" {
			if trader.HyperliquidPrivateKey == "" {
				return fmt.Errorf("trader[%d]: Hyperliquid requires hyperliquid_private_key", i)
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "crypto_perp"
			}
		} else if trader.Exchange == "aster" {
			if trader.AsterUser == "" || trader.AsterSigner == "" || trader.AsterPrivateKey == "" {
				return fmt.Errorf("trader[%d]: Aster requires aster_user, aster_signer, and aster_private_key", i)
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "crypto_perp"
			}
		} else if trader.Exchange == "alpaca" {
			if trader.AlpacaAPIKey == "" || trader.AlpacaSecretKey == "" {
				return fmt.Errorf("trader[%d]: Alpaca requires both alpaca_api_key and alpaca_secret_key", i)
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "equity"
			}
			if trader.Mode == "" {
				if trader.AlpacaPaperTrading {
					trader.Mode = "paper"
				} else {
					trader.Mode = "live"
				}
			}
			if trader.DataProvider == "" {
				if trader.Mode == "replay" {
					trader.DataProvider = "csv"
				} else {
					trader.DataProvider = "alpaca"
				}
			}
			if trader.Broker == "" {
				if trader.Mode == "replay" {
					trader.Broker = "sim"
				} else {
					trader.Broker = "alpaca"
				}
			}

			// Defaults for equity config
			if trader.OrderSizingMode == "" {
				trader.OrderSizingMode = "qty"
			}
			if trader.BarsAdjustment == "" {
				trader.BarsAdjustment = "split"
			}
			if trader.MaxGrossExposure <= 0 {
				trader.MaxGrossExposure = 1.0 // Default 100% of equity
			}
			if trader.MaxPositionPct <= 0 {
				trader.MaxPositionPct = 0.20 // Default 20% of equity per position
			}
			if trader.MaxDailyLossPct <= 0 {
				trader.MaxDailyLossPct = 0.05 // Default 5% daily loss limit
			}
			if trader.MaxPairCorrelation <= 0 || trader.MaxPairCorrelation >= 1 {
				trader.MaxPairCorrelation = 0.82
			}
			if trader.MinLiquidityUSD <= 0 {
				trader.MinLiquidityUSD = 2_000_000
			}
			if trader.MinDecisionConfidence <= 0 || trader.MinDecisionConfidence > 100 {
				trader.MinDecisionConfidence = 58
			}
			if trader.ExecutionCommissionBps < 0 {
				trader.ExecutionCommissionBps = 0
			}
			if trader.ExecutionSlippageBps < 0 {
				trader.ExecutionSlippageBps = 0
			}
			if trader.ExecutionImpactBps < 0 {
				trader.ExecutionImpactBps = 0
			}
			if trader.MaxParticipationRate <= 0 || trader.MaxParticipationRate > 1 {
				trader.MaxParticipationRate = 0.15
			}
			if trader.DrawdownThrottleStartPct <= 0 {
				trader.DrawdownThrottleStartPct = 0.03
			}
			if trader.DrawdownThrottleMinScale <= 0 || trader.DrawdownThrottleMinScale > 1 {
				trader.DrawdownThrottleMinScale = 0.35
			}
			if trader.MaxPortfolioHeatPct <= 0 || trader.MaxPortfolioHeatPct > 0.30 {
				trader.MaxPortfolioHeatPct = 0.035
			}
			if trader.MaxNetExposurePct <= 0 || trader.MaxNetExposurePct > 1 {
				trader.MaxNetExposurePct = 0.65
			}
			if trader.LossStreakPauseThreshold <= 0 {
				trader.LossStreakPauseThreshold = 3
			}
			if trader.LossStreakPauseCycles <= 0 {
				trader.LossStreakPauseCycles = 5
			}
			if trader.PerformanceRiskLookback <= 0 {
				trader.PerformanceRiskLookback = 20
			}
			if trader.VolatilityBrakeTargetPct <= 0 {
				trader.VolatilityBrakeTargetPct = 0.008
			}
			if trader.VolatilityBrakeLookback <= 0 {
				trader.VolatilityBrakeLookback = 40
			}
			if trader.VolatilityBrakeMinScale <= 0 || trader.VolatilityBrakeMinScale > 1 {
				trader.VolatilityBrakeMinScale = 0.45
			}
			if trader.KellyFractionCap <= 0 || trader.KellyFractionCap > 1 {
				trader.KellyFractionCap = 0.33
			}
			if trader.KellyLookback <= 0 {
				trader.KellyLookback = 30
			}
			if trader.KellyMinTrades <= 0 {
				trader.KellyMinTrades = 10
			}
			if trader.MarketStressEntryBlock <= 0 || trader.MarketStressEntryBlock > 1 {
				trader.MarketStressEntryBlock = 0.82
			}
			if trader.MarketStressRiskMinScale <= 0 || trader.MarketStressRiskMinScale > 1 {
				trader.MarketStressRiskMinScale = 0.35
			}
			if trader.StrategyMode == "" {
				trader.StrategyMode = "momentum_fallback"
			}
			if trader.MomentumMinScore <= 0 {
				trader.MomentumMinScore = 1.25
			}
			if trader.FallbackPositionPct <= 0 || trader.FallbackPositionPct > 0.20 {
				trader.FallbackPositionPct = 0.10
			}
			if trader.MinFactorScore <= 0 {
				trader.MinFactorScore = 0.35
			}
			if trader.RiskPerTradePct <= 0 {
				trader.RiskPerTradePct = 0.0075
			}
			if trader.ProfitLockThreshold <= 0 {
				trader.ProfitLockThreshold = 1.25
			}
			if trader.TrailingStopATRMult <= 0 {
				trader.TrailingStopATRMult = 1.6
			}
			if trader.MaxHoldingCycles <= 0 {
				trader.MaxHoldingCycles = 180
			}
			if trader.MaxConcurrentPos <= 0 {
				trader.MaxConcurrentPos = 3
			}
			if trader.SymbolCooldownCycles <= 0 {
				trader.SymbolCooldownCycles = 6
			}
			if trader.AllowShort == nil {
				v := true
				trader.AllowShort = &v
			}
			if trader.UseMacroFilters == nil {
				v := true
				trader.UseMacroFilters = &v
			}
			if trader.DynamicPositionSizing == nil {
				v := true
				trader.DynamicPositionSizing = &v
			}
			if trader.RegimeRiskScaling == nil {
				v := true
				trader.RegimeRiskScaling = &v
			}
			if trader.UseNewsRisk == nil {
				v := trader.Mode != "replay"
				trader.UseNewsRisk = &v
			}
			if trader.EnableNewsInReplay == nil {
				v := false
				trader.EnableNewsInReplay = &v
			}
			if trader.Mode == "replay" && !*trader.EnableNewsInReplay {
				v := false
				trader.UseNewsRisk = &v
			}
			if trader.NewsProvider == "" {
				trader.NewsProvider = "rss"
			}
			if trader.NewsLookbackMinutes <= 0 {
				trader.NewsLookbackMinutes = 240
			}
			if trader.NewsRefreshSeconds <= 0 {
				trader.NewsRefreshSeconds = 120
			}
			if trader.NewsMarketImpactThresh <= 0 || trader.NewsMarketImpactThresh > 1 {
				trader.NewsMarketImpactThresh = 0.65
			}
			if trader.NewsSymbolImpactThresh <= 0 || trader.NewsSymbolImpactThresh > 1 {
				trader.NewsSymbolImpactThresh = 0.70
			}
			if trader.NewsHardBlockThresh <= 0 || trader.NewsHardBlockThresh > 1 {
				trader.NewsHardBlockThresh = 0.85
			}
			if trader.NewsMaxRiskReduction <= 0 || trader.NewsMaxRiskReduction > 0.95 {
				trader.NewsMaxRiskReduction = 0.55
			}

		} else if trader.Exchange == "ibkr" {
			if trader.IBKRGatewayURL == "" {
				trader.IBKRGatewayURL = "https://127.0.0.1:5002/v1/api"
			}
			if trader.IBKRAccountID == "" {
				return fmt.Errorf("trader[%d]: IBKR requires ibkr_account_id", i)
			}
			if trader.Mode == "" {
				trader.Mode = "paper"
			}
			if trader.Mode != "paper" && trader.Mode != "live" && trader.Mode != "replay" {
				return fmt.Errorf("trader[%d]: IBKR mode must be 'paper', 'live', or 'replay'", i)
			}
			if trader.InstrumentType == "" {
				trader.InstrumentType = "equity"
			}
			if trader.DataProvider == "" {
				trader.DataProvider = "ibkr"
			}
			if trader.Broker == "" {
				trader.Broker = "ibkr"
			}

			// Defaults for equity config
			if trader.OrderSizingMode == "" {
				trader.OrderSizingMode = "qty"
			}
			if trader.BarsAdjustment == "" {
				trader.BarsAdjustment = "split"
			}
			if trader.MaxGrossExposure <= 0 {
				trader.MaxGrossExposure = 1.0 // Default 100% of equity
			}
			if trader.MaxPositionPct <= 0 {
				trader.MaxPositionPct = 0.20 // Default 20% of equity per position
			}
			if trader.MaxDailyLossPct <= 0 {
				trader.MaxDailyLossPct = 0.05 // Default 5% daily loss limit
			}
			if trader.MaxPairCorrelation <= 0 || trader.MaxPairCorrelation >= 1 {
				trader.MaxPairCorrelation = 0.82
			}
			if trader.MinLiquidityUSD <= 0 {
				trader.MinLiquidityUSD = 2_000_000
			}
			if trader.MinDecisionConfidence <= 0 || trader.MinDecisionConfidence > 100 {
				trader.MinDecisionConfidence = 58
			}
			if trader.ExecutionCommissionBps < 0 {
				trader.ExecutionCommissionBps = 0
			}
			if trader.ExecutionSlippageBps < 0 {
				trader.ExecutionSlippageBps = 0
			}
			if trader.ExecutionImpactBps < 0 {
				trader.ExecutionImpactBps = 0
			}
			if trader.MaxParticipationRate <= 0 || trader.MaxParticipationRate > 1 {
				trader.MaxParticipationRate = 0.15
			}
			if trader.DrawdownThrottleStartPct <= 0 {
				trader.DrawdownThrottleStartPct = 0.03
			}
			if trader.DrawdownThrottleMinScale <= 0 || trader.DrawdownThrottleMinScale > 1 {
				trader.DrawdownThrottleMinScale = 0.35
			}
			if trader.MaxPortfolioHeatPct <= 0 || trader.MaxPortfolioHeatPct > 0.30 {
				trader.MaxPortfolioHeatPct = 0.035
			}
			if trader.MaxNetExposurePct <= 0 || trader.MaxNetExposurePct > 1 {
				trader.MaxNetExposurePct = 0.65
			}
			if trader.LossStreakPauseThreshold <= 0 {
				trader.LossStreakPauseThreshold = 3
			}
			if trader.LossStreakPauseCycles <= 0 {
				trader.LossStreakPauseCycles = 5
			}
			if trader.PerformanceRiskLookback <= 0 {
				trader.PerformanceRiskLookback = 20
			}
			if trader.VolatilityBrakeTargetPct <= 0 {
				trader.VolatilityBrakeTargetPct = 0.008
			}
			if trader.VolatilityBrakeLookback <= 0 {
				trader.VolatilityBrakeLookback = 40
			}
			if trader.VolatilityBrakeMinScale <= 0 || trader.VolatilityBrakeMinScale > 1 {
				trader.VolatilityBrakeMinScale = 0.45
			}
			if trader.KellyFractionCap <= 0 || trader.KellyFractionCap > 1 {
				trader.KellyFractionCap = 0.33
			}
			if trader.KellyLookback <= 0 {
				trader.KellyLookback = 30
			}
			if trader.KellyMinTrades <= 0 {
				trader.KellyMinTrades = 10
			}
			if trader.MarketStressEntryBlock <= 0 || trader.MarketStressEntryBlock > 1 {
				trader.MarketStressEntryBlock = 0.82
			}
			if trader.MarketStressRiskMinScale <= 0 || trader.MarketStressRiskMinScale > 1 {
				trader.MarketStressRiskMinScale = 0.35
			}
			if trader.StrategyMode == "" {
				trader.StrategyMode = "momentum_fallback"
			}
			if trader.MomentumMinScore <= 0 {
				trader.MomentumMinScore = 1.25
			}
			if trader.FallbackPositionPct <= 0 || trader.FallbackPositionPct > 0.20 {
				trader.FallbackPositionPct = 0.10
			}
			if trader.MinFactorScore <= 0 {
				trader.MinFactorScore = 0.35
			}
			if trader.RiskPerTradePct <= 0 {
				trader.RiskPerTradePct = 0.0075
			}
			if trader.ProfitLockThreshold <= 0 {
				trader.ProfitLockThreshold = 1.25
			}
			if trader.TrailingStopATRMult <= 0 {
				trader.TrailingStopATRMult = 1.6
			}
			if trader.MaxHoldingCycles <= 0 {
				trader.MaxHoldingCycles = 180
			}
			if trader.MaxConcurrentPos <= 0 {
				trader.MaxConcurrentPos = 3
			}
			if trader.SymbolCooldownCycles <= 0 {
				trader.SymbolCooldownCycles = 6
			}
			if trader.AllowShort == nil {
				v := true
				trader.AllowShort = &v
			}
			if trader.UseMacroFilters == nil {
				v := true
				trader.UseMacroFilters = &v
			}
			if trader.DynamicPositionSizing == nil {
				v := true
				trader.DynamicPositionSizing = &v
			}
			if trader.RegimeRiskScaling == nil {
				v := true
				trader.RegimeRiskScaling = &v
			}
			if trader.UseNewsRisk == nil {
				v := trader.Mode != "replay"
				trader.UseNewsRisk = &v
			}
			if trader.EnableNewsInReplay == nil {
				v := false
				trader.EnableNewsInReplay = &v
			}
			if trader.Mode == "replay" && !*trader.EnableNewsInReplay {
				v := false
				trader.UseNewsRisk = &v
			}
			if trader.NewsProvider == "" {
				trader.NewsProvider = "rss"
			}
			if trader.NewsLookbackMinutes <= 0 {
				trader.NewsLookbackMinutes = 240
			}
			if trader.NewsRefreshSeconds <= 0 {
				trader.NewsRefreshSeconds = 120
			}
			if trader.NewsMarketImpactThresh <= 0 || trader.NewsMarketImpactThresh > 1 {
				trader.NewsMarketImpactThresh = 0.65
			}
			if trader.NewsSymbolImpactThresh <= 0 || trader.NewsSymbolImpactThresh > 1 {
				trader.NewsSymbolImpactThresh = 0.70
			}
			if trader.NewsHardBlockThresh <= 0 || trader.NewsHardBlockThresh > 1 {
				trader.NewsHardBlockThresh = 0.85
			}
			if trader.NewsMaxRiskReduction <= 0 || trader.NewsMaxRiskReduction > 0.95 {
				trader.NewsMaxRiskReduction = 0.55
			}
			if trader.Mode == "live" && !trader.StrictLiveMode {
				fmt.Printf("  [Trader %s] strict_live_mode is disabled for LIVE mode. This is unsafe.\n", trader.Name)
			}

			// Validate replay mode
			if trader.Mode == "replay" && trader.DataProvider == "csv" && trader.CSVDataDir == "" {
				return fmt.Errorf("trader[%d]: Replay mode requires csv_data_dir to be set", i)
			}
		}

		isLocalOnlyEquityStrategy := trader.InstrumentType == "equity" &&
			(trader.StrategyMode == "momentum_only" || trader.StrategyMode == "multi_factor")
		requiresAIKeys := !trader.DemoMode && !isLocalOnlyEquityStrategy
		if requiresAIKeys {
			if trader.AIModel == "qwen" && trader.QwenKey == "" {
				return fmt.Errorf("trader[%d]: Qwen model requires qwen_key", i)
			}
			if trader.AIModel == "deepseek" && trader.DeepSeekKey == "" {
				return fmt.Errorf("trader[%d]: DeepSeek model requires deepseek_key", i)
			}
			if trader.AIModel == "custom" {
				if trader.CustomAPIURL == "" {
					return fmt.Errorf("trader[%d]: Custom model requires custom_api_url", i)
				}
				if trader.CustomAPIKey == "" {
					return fmt.Errorf("trader[%d]: Custom model requires custom_api_key", i)
				}
				if trader.CustomModelName == "" {
					return fmt.Errorf("trader[%d]: Custom model requires custom_model_name", i)
				}
			}
		}
		if trader.InitialBalance <= 0 {
			return fmt.Errorf("trader[%d]: initial_balance must be greater than 0", i)
		}

		//  Ensure valid scan interval
		if trader.ScanIntervalSeconds <= 0 && trader.ScanIntervalMinutes <= 0 {
			trader.ScanIntervalMinutes = 3 // Default 3 minutes, prevent ticker crash
			fmt.Printf("  [Trader %s] No scan interval set, using default 3 minutes\n", trader.Name)
		}

		if trader.ScanIntervalSeconds > 0 {
			fmt.Printf("  [Trader %s] Scan interval: %d seconds\n", trader.Name, trader.ScanIntervalSeconds)
		} else {
			fmt.Printf("  [Trader %s] Scan interval: %d minutes\n", trader.Name, trader.ScanIntervalMinutes)
		}

		if trader.CandidateBatchSize <= 0 {
			if trader.InstrumentType == "equity" {
				trader.CandidateBatchSize = 30
			} else {
				trader.CandidateBatchSize = 20
			}
		}
		if trader.InstrumentType == "equity" && trader.DataProvider == "ibkr" && trader.CandidateBatchSize > 12 {
			trader.CandidateBatchSize = 12
		}
		if trader.InstrumentType == "equity" {
			validStrategy := trader.StrategyMode == "ai_only" ||
				trader.StrategyMode == "momentum_fallback" ||
				trader.StrategyMode == "momentum_only" ||
				trader.StrategyMode == "multi_factor" ||
				trader.StrategyMode == "hybrid_ai"
			if !validStrategy {
				return fmt.Errorf("trader[%d]: strategy_mode must be 'ai_only', 'momentum_fallback', 'momentum_only', 'multi_factor', or 'hybrid_ai'", i)
			}
			if trader.MinDecisionConfidence < 0 || trader.MinDecisionConfidence > 100 {
				return fmt.Errorf("trader[%d]: min_decision_confidence must be between 0 and 100", i)
			}
			if trader.MaxPairCorrelation <= 0 || trader.MaxPairCorrelation >= 1 {
				return fmt.Errorf("trader[%d]: max_pair_correlation must be between 0 and 1 (exclusive)", i)
			}
			if trader.ExecutionCommissionBps < 0 {
				return fmt.Errorf("trader[%d]: execution_commission_bps cannot be negative", i)
			}
			if trader.ExecutionSlippageBps < 0 {
				return fmt.Errorf("trader[%d]: execution_slippage_bps cannot be negative", i)
			}
			if trader.ExecutionImpactBps < 0 {
				return fmt.Errorf("trader[%d]: execution_impact_bps cannot be negative", i)
			}
			if trader.MaxParticipationRate <= 0 || trader.MaxParticipationRate > 1 {
				return fmt.Errorf("trader[%d]: max_participation_rate must be between 0 and 1", i)
			}
			if trader.DrawdownThrottleStartPct <= 0 || trader.DrawdownThrottleStartPct >= 1 {
				return fmt.Errorf("trader[%d]: drawdown_throttle_start must be between 0 and 1", i)
			}
			if trader.DrawdownThrottleMinScale <= 0 || trader.DrawdownThrottleMinScale > 1 {
				return fmt.Errorf("trader[%d]: drawdown_throttle_min_scale must be between 0 and 1", i)
			}
			if trader.MaxPortfolioHeatPct <= 0 || trader.MaxPortfolioHeatPct > 0.30 {
				return fmt.Errorf("trader[%d]: max_portfolio_heat_pct must be between 0 and 0.30", i)
			}
			if trader.MaxNetExposurePct <= 0 || trader.MaxNetExposurePct > 1 {
				return fmt.Errorf("trader[%d]: max_net_exposure_pct must be between 0 and 1", i)
			}
			if trader.LossStreakPauseThreshold <= 0 {
				return fmt.Errorf("trader[%d]: loss_streak_pause_threshold must be > 0", i)
			}
			if trader.LossStreakPauseCycles <= 0 {
				return fmt.Errorf("trader[%d]: loss_streak_pause_cycles must be > 0", i)
			}
			if trader.PerformanceRiskLookback <= 0 {
				return fmt.Errorf("trader[%d]: performance_risk_lookback must be > 0", i)
			}
			if trader.VolatilityBrakeTargetPct <= 0 || trader.VolatilityBrakeTargetPct >= 1 {
				return fmt.Errorf("trader[%d]: volatility_brake_target_pct must be between 0 and 1", i)
			}
			if trader.VolatilityBrakeLookback <= 1 {
				return fmt.Errorf("trader[%d]: volatility_brake_lookback must be > 1", i)
			}
			if trader.VolatilityBrakeMinScale <= 0 || trader.VolatilityBrakeMinScale > 1 {
				return fmt.Errorf("trader[%d]: volatility_brake_min_scale must be between 0 and 1", i)
			}
			if trader.KellyFractionCap < 0 || trader.KellyFractionCap > 1 {
				return fmt.Errorf("trader[%d]: kelly_fraction_cap must be between 0 and 1", i)
			}
			if trader.KellyLookback <= 1 {
				return fmt.Errorf("trader[%d]: kelly_lookback must be > 1", i)
			}
			if trader.KellyMinTrades <= 0 {
				return fmt.Errorf("trader[%d]: kelly_min_trades must be > 0", i)
			}
			if trader.MarketStressEntryBlock <= 0 || trader.MarketStressEntryBlock > 1 {
				return fmt.Errorf("trader[%d]: market_stress_entry_block must be between 0 and 1", i)
			}
			if trader.MarketStressRiskMinScale <= 0 || trader.MarketStressRiskMinScale > 1 {
				return fmt.Errorf("trader[%d]: market_stress_risk_min_scale must be between 0 and 1", i)
			}
			if trader.UseNewsRisk == nil {
				return fmt.Errorf("trader[%d]: use_news_risk must be configured", i)
			}
			if trader.EnableNewsInReplay == nil {
				return fmt.Errorf("trader[%d]: enable_news_in_replay must be configured", i)
			}
			if trader.NewsProvider == "" {
				return fmt.Errorf("trader[%d]: news_provider cannot be empty", i)
			}
			if trader.NewsLookbackMinutes <= 0 {
				return fmt.Errorf("trader[%d]: news_lookback_minutes must be > 0", i)
			}
			if trader.NewsRefreshSeconds <= 0 {
				return fmt.Errorf("trader[%d]: news_refresh_seconds must be > 0", i)
			}
			if trader.NewsMarketImpactThresh <= 0 || trader.NewsMarketImpactThresh > 1 {
				return fmt.Errorf("trader[%d]: news_market_impact_thresh must be between 0 and 1", i)
			}
			if trader.NewsSymbolImpactThresh <= 0 || trader.NewsSymbolImpactThresh > 1 {
				return fmt.Errorf("trader[%d]: news_symbol_impact_thresh must be between 0 and 1", i)
			}
			if trader.NewsHardBlockThresh <= 0 || trader.NewsHardBlockThresh > 1 {
				return fmt.Errorf("trader[%d]: news_hard_block_thresh must be between 0 and 1", i)
			}
			if trader.NewsMaxRiskReduction <= 0 || trader.NewsMaxRiskReduction > 0.95 {
				return fmt.Errorf("trader[%d]: news_max_risk_reduction must be between 0 and 0.95", i)
			}
		}
		if trader.Mode == "replay" && trader.ReplayWarmupBars <= 0 {
			trader.ReplayWarmupBars = 120
		}

		// Update trader in list with defaults
		c.Traders[i] = trader
	}

	if c.APIServerPort <= 0 {
		c.APIServerPort = 8080 // Default port 8080
	}

	// Default leverage setup (safe for Binance sub-accounts, max 5x)
	if c.Leverage.BTCETHLeverage <= 0 {
		c.Leverage.BTCETHLeverage = 5
	}
	if c.Leverage.BTCETHLeverage > 5 {
		fmt.Printf("  Warning: BTC/ETH leverage set to %dx, may fail for sub-accounts (limit 5x)\n", c.Leverage.BTCETHLeverage)
	}
	if c.Leverage.AltcoinLeverage <= 0 {
		c.Leverage.AltcoinLeverage = 5
	}
	if c.Leverage.AltcoinLeverage > 5 {
		fmt.Printf("  Warning: Altcoin leverage set to %dx, may fail for sub-accounts (limit 5x)\n", c.Leverage.AltcoinLeverage)
	}

	return nil
}

// GetScanInterval  Safely return scan interval duration
func (tc *TraderConfig) GetScanInterval() time.Duration {
	if tc.ScanIntervalSeconds > 0 {
		return time.Duration(tc.ScanIntervalSeconds) * time.Second
	}
	if tc.ScanIntervalMinutes > 0 {
		return time.Duration(tc.ScanIntervalMinutes) * time.Minute
	}
	// fallback (if validation was skipped)
	return 3 * time.Minute
}
