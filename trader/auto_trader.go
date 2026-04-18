package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"northstar/alerts"
	"northstar/audit"
	"northstar/decision"
	"northstar/execution"
	"northstar/incidents"
	"northstar/logger"
	"northstar/market"
	"northstar/mcp"
	"northstar/news"
	"northstar/orders"
	"northstar/pool"
	"northstar/positions"
	"northstar/risk"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AutoTraderConfig Auto-trading configuration (simplified - AI full control)
type AutoTraderConfig struct {
	// Trader identifier
	ID      string // Unique trader ID (used for log directories, etc.)
	Name    string // Trader display name
	AIModel string // AI model: "qwen" or "deepseek"

	// Exchange selection
	Exchange string // "binance", "hyperliquid", "aster", "alpaca", "ibkr", or "demo"

	// Binance API Config
	BinanceAPIKey    string
	BinanceSecretKey string

	// Hyperliquid Config
	HyperliquidPrivateKey string
	HyperliquidWalletAddr string
	HyperliquidTestnet    bool

	// Aster Config
	AsterUser       string // Aster main wallet address
	AsterSigner     string // Aster API wallet address
	AsterPrivateKey string // Aster API wallet private key

	// Alpaca Config
	AlpacaAPIKey       string
	AlpacaSecretKey    string
	AlpacaPaperTrading bool

	// Interactive Brokers Config
	IBKRGatewayURL              string
	IBKRAccountID               string
	IBKRSessionCookie           string
	StrictLiveMode              bool
	LivePromotionApproved       bool
	PromotionSourceTraderID     string
	MinPaperSessionReports      int
	RequireBacktestSummary      bool
	RequireReleaseBuildForLive  bool
	PromotionMaxEvidenceAgeDays int
	EmergencyKillSwitch         bool
	KillSwitchFile              string

	CoinPoolAPIURL string

	// AI Config
	UseQwen     bool
	DeepSeekKey string
	QwenKey     string

	// Custom AI API Config
	CustomAPIURL    string
	CustomAPIKey    string
	CustomModelName string
	DemoMode        bool

	// Scanning configuration
	ScanInterval time.Duration // Scan interval (recommended 3 minutes)

	// Account configuration
	InitialBalance float64 // Initial balance (used to calculate P&L, set manually)

	// Leverage configuration
	BTCETHLeverage  int // Leverage multiplier for BTC and ETH
	AltcoinLeverage int // Leverage multiplier for Altcoins

	// Risk control (hints, AI retains final decision)
	MaxDailyLoss    float64       // Max daily loss percentage (hint)
	MaxDrawdown     float64       // Max drawdown percentage (hint)
	StopTradingTime time.Duration // Pause duration after triggering risk control

	// Execution mode and data provider config
	Mode                                string
	DataProvider                        string
	Broker                              string
	CSVDataDir                          string
	InstrumentType                      string
	ConfiguredDefaultSymbols            []string
	ConfiguredDefaultSymbolsFile        string
	BarsAdjustment                      string
	CandidateBatchSize                  int
	TrustedSymbolsFile                  string
	StrategyMode                        string
	MomentumMinScore                    float64
	FallbackPositionPct                 float64
	MinFactorScore                      float64
	RiskPerTradePct                     float64
	ProfitLockThreshold                 float64
	TrailingStopATRMult                 float64
	MaxHoldingCycles                    int
	MaxConcurrentPos                    int
	SymbolCooldownCycles                int
	MaxGrossExposure                    float64
	MaxPositionPct                      float64
	MaxDailyLossPct                     float64
	MaxPairCorrelation                  float64
	MinLiquidityUSD                     float64
	MinDecisionConfidence               int
	ExecutionCommissionBps              float64
	ExecutionSpreadBps                  float64
	ExecutionSlippageBps                float64
	ExecutionImpactBps                  float64
	MaxParticipationRate                float64
	DrawdownThrottleStartPct            float64
	DrawdownThrottleMinScale            float64
	MaxPortfolioHeatPct                 float64
	MaxNetExposurePct                   float64
	MaxSectorExposurePct                float64
	MaxCorrelatedPositions              int
	LossStreakPauseThreshold            int
	LossStreakPauseCycles               int
	PerformanceRiskLookback             int
	VolatilityBrakeTargetPct            float64
	VolatilityBrakeLookback             int
	VolatilityBrakeMinScale             float64
	KellyFractionCap                    float64
	KellyLookback                       int
	KellyMinTrades                      int
	MarketStressEntryBlock              float64
	MarketStressRiskMinScale            float64
	MaxRuntimeDegradationsPerSession    int
	MaxReconciliationFailuresPerSession int
	MaxOrderRejectsPerSession           int
	SupervisorReduceOnlyOnDrawdown      bool
	SupervisorReduceOnlyOnDrawdownSet   bool
	UseNewsRisk                         bool
	EnableNewsInReplay                  bool
	NewsProvider                        string
	NewsLookbackMinutes                 int
	NewsRefreshSeconds                  int
	NewsMarketImpactThresh              float64
	NewsSymbolImpactThresh              float64
	NewsHardBlockThresh                 float64
	NewsMaxRiskReduction                float64
	AllowShort                          bool
	UseMacroFilters                     bool
	DynamicPositionSizing               bool
	RegimeRiskScaling                   bool
	BenchmarkSymbols                    []string
	MaxCycles                           int
	ReplayWarmupBars                    int

	// Session guard (#13)
	AllowExtendedHours bool   // Permit pre-market/after-hours equity entry (default false)
	SessionTimezone    string // IANA timezone for session guard (default "America/New_York")

	// Order throttle (#14)
	OrderThrottleMaxBurst  int // Token bucket burst capacity (default 10)
	OrderThrottlePerMinute int // Steady-state refill rate in orders per minute (default 20)
}

// AutoTrader The automatic trader engine
type AutoTrader struct {
	id                                    string // Trader unique identifier
	name                                  string // Trader display name
	aiModel                               string // AI model name
	exchange                              string // Exchange platform name
	config                                AutoTraderConfig
	riskEngine                            *risk.Engine
	executionManager                      *execution.Manager
	auditRecorder                         *audit.Recorder
	eventJournal                          *audit.Journal
	alertManager                          *alerts.Manager
	incidentManager                       *incidents.Manager
	trader                                Trader // Standardized trader interface
	mcpClient                             *mcp.Client
	decisionLogger                        *logger.DecisionLogger // Decision logger
	initialBalance                        float64
	strategyRealizedPnL                   float64
	dailyPnL                              float64
	dailyStartEquity                      float64
	peakEquitySeen                        float64
	lastResetTime                         time.Time
	stopUntil                             time.Time
	isRunning                             bool
	startTime                             time.Time // System start time
	callCount                             int       // AI invocation cycle counter
	runDone                               chan struct{}
	positionFirstSeenTime                 map[string]int64 // First appearance of positions (symbol_side -> ms timestamp)
	positionEntryCycle                    map[string]int
	positionPeakPnLPct                    map[string]float64
	symbolCooldownUntil                   map[string]int
	symbolEdgeScore                       map[string]float64
	symbolTradeCount                      map[string]int
	openEntryBlockedUntil                 int
	consecutiveLossCloses                 int
	recentClosePnLPct                     []float64
	closePnLEMA                           float64
	recentEquity                          []float64
	latestMarketStress                    float64
	latestStressDispersion                float64
	latestStressCorrelation               float64
	latestKellyScale                      float64
	latestNewsSentiment                   float64
	latestNewsImpact                      float64
	latestNewsScale                       float64
	lastNewsRefresh                       time.Time
	newsLastError                         string
	cachedNews                            *news.Snapshot
	newsProvider                          news.Provider
	newsCredibilityGlobal                 float64
	newsCredibility                       map[string]float64
	newsSampleCount                       map[string]int
	plannedNewsBias                       map[string]float64
	positionNewsBias                      map[string]float64
	lastNewsLearnDelta                    float64
	lastNewsLearnSymbol                   string
	newsMemoryPath                        string
	provider                              market.BarsProvider // Injected data provider
	candidateCursor                       int
	trustedSymbolSet                      map[string]struct{}
	universeMu                            sync.RWMutex
	universeState                         runtimeUniverseState
	demoMode                              bool
	demoRand                              *rand.Rand
	demoEquity                            float64
	demoAvailableBalance                  float64
	demoPositionCount                     int
	demoMarginUsedPct                     float64
	demoSnapshotSeed                      int64
	demoLastCycleTime                     time.Time
	replayInitialized                     bool
	backtestMode                          bool
	readinessMu                           sync.RWMutex
	readinessSummary                      ReadinessSummary
	promotionMu                           sync.RWMutex
	promotionSummary                      PromotionSummary
	accountSummaryMu                      sync.RWMutex
	lastAccountSummary                    *AccountSummary
	accountSnapshotMu                     sync.RWMutex
	runtimeAccountSnapshot                *runtimeAccountSnapshot
	riskSupervisorMu                      sync.RWMutex
	riskSupervisor                        *risk.Supervisor
	riskSupervisorState                   risk.SupervisorState
	riskSupervisorSessionDay              string
	riskSupervisorBrokerDegradationEvents int
	riskSupervisorReconciliationFailures  int
	riskSupervisorOrderRejects            int
	strictLiveMu                          sync.RWMutex
	strictLiveHealthy                     bool
	strictLiveMessage                     string
	strictLiveLastCheckedAt               time.Time
	killSwitchMu                          sync.RWMutex
	killSwitchState                       killSwitchSummary
	dataQualityMu                         sync.RWMutex
	dataQualityState                      dataQualityState
	positionReconMu                       sync.RWMutex
	positionReconSummary                  positionReconciliationSummary
	localPositionSnapshots                map[string]positions.Snapshot
	portfolioRiskMu                       sync.RWMutex
	portfolioRiskState                    *portfolioRiskState
	protectionMu                          sync.RWMutex
	pendingProtections                    map[string]pendingProtectionState
	protectionLastUpdatedAt               time.Time
	shadowMu                              sync.RWMutex
	shadowState                           shadowModeState
	restartRecoveryMu                     sync.RWMutex
	restartRecoveryState                  restartRecoverySummary
	restartPersistMu                      sync.Mutex
	blockedCycleMu                        sync.RWMutex
	lastBlockedCycle                      blockedCycleState
	eventJournalMu                        sync.Mutex
	lastJournaledTradingGateKey           string
	lastJournaledBrokerTruthKey           string
	lastJournaledProtectionStateByKey     map[string]string
	sessionReportMu                       sync.Mutex
	sessionReportState                    *paperSessionTracker
	lastSessionReportPath                 string
	lastSessionReportAt                   time.Time
	lastSessionReportStatus               string
	brokerStateMu                         sync.RWMutex
	brokerState                           BrokerRuntimeState
	brokerStateReason                     string
	brokerLastError                       string
	brokerStateSince                      time.Time
	brokerLastHealthyAt                   time.Time
	brokerLastReconciledAt                time.Time
	brokerReconnectAttempts               int
	brokerNextRetryAt                     time.Time
	brokerRecoveryActive                  bool
	brokerLastStateLogKey                 string
	brokerLastStateLogAt                  time.Time
	timeNow                               func() time.Time

	// #13 exchange session guard — non-nil for equity instrument type
	sessionGuard *sessionGuard
	// #14 order submission throttle — always non-nil after init
	orderThrottle *orderThrottle
}

// NewAutoTrader Creates a new automatic trader
func NewAutoTrader(config AutoTraderConfig) (*AutoTrader, error) {
	// Set defaults
	if config.ID == "" {
		config.ID = "default_trader"
	}
	if config.Name == "" {
		config.Name = "Default Trader"
	}
	if config.AIModel == "" {
		if config.UseQwen {
			config.AIModel = "qwen"
		} else {
			config.AIModel = "deepseek"
		}
	}
	if config.CandidateBatchSize <= 0 {
		if config.InstrumentType == "equity" {
			config.CandidateBatchSize = 30
		} else {
			config.CandidateBatchSize = 20
		}
	}
	if config.InstrumentType == "equity" && config.DataProvider == "ibkr" && config.CandidateBatchSize > 12 {
		config.CandidateBatchSize = 12
	}
	if config.InstrumentType == "equity" {
		if config.StrategyMode == "" {
			config.StrategyMode = "momentum_fallback"
		}
		if config.MomentumMinScore <= 0 {
			config.MomentumMinScore = 1.25
		}
		if config.FallbackPositionPct <= 0 || config.FallbackPositionPct > 0.20 {
			config.FallbackPositionPct = 0.10
		}
		if config.MinFactorScore <= 0 {
			config.MinFactorScore = 0.35
		}
		if config.RiskPerTradePct <= 0 {
			config.RiskPerTradePct = 0.0075
		}
		if config.ProfitLockThreshold <= 0 {
			config.ProfitLockThreshold = 1.25
		}
		if config.TrailingStopATRMult <= 0 {
			config.TrailingStopATRMult = 1.6
		}
		if config.MaxHoldingCycles <= 0 {
			config.MaxHoldingCycles = 180
		}
		if config.MaxConcurrentPos <= 0 {
			config.MaxConcurrentPos = 3
		}
		if config.SymbolCooldownCycles <= 0 {
			config.SymbolCooldownCycles = 6
		}
		if config.MaxGrossExposure <= 0 {
			config.MaxGrossExposure = 1.0
		}
		if config.MaxPositionPct <= 0 || config.MaxPositionPct > 1.0 {
			config.MaxPositionPct = 0.20
		}
		if config.MaxDailyLossPct <= 0 {
			config.MaxDailyLossPct = 0.05
		}
		if config.MaxPairCorrelation <= 0 || config.MaxPairCorrelation > 0.99 {
			config.MaxPairCorrelation = 0.82
		}
		if config.MinLiquidityUSD <= 0 {
			config.MinLiquidityUSD = 2_000_000
		}
		if config.MinDecisionConfidence <= 0 {
			config.MinDecisionConfidence = 58
		}
		if config.ExecutionCommissionBps < 0 {
			config.ExecutionCommissionBps = 0
		}
		if config.ExecutionSpreadBps < 0 {
			config.ExecutionSpreadBps = 0
		}
		if config.ExecutionSlippageBps < 0 {
			config.ExecutionSlippageBps = 0
		}
		if config.ExecutionImpactBps < 0 {
			config.ExecutionImpactBps = 0
		}
		if config.MaxParticipationRate <= 0 || config.MaxParticipationRate > 1.0 {
			config.MaxParticipationRate = 0.15
		}
		if config.DrawdownThrottleStartPct <= 0 {
			config.DrawdownThrottleStartPct = 0.03
		}
		if config.DrawdownThrottleMinScale <= 0 || config.DrawdownThrottleMinScale > 1.0 {
			config.DrawdownThrottleMinScale = 0.35
		}
		if config.MaxPortfolioHeatPct <= 0 || config.MaxPortfolioHeatPct > 0.30 {
			config.MaxPortfolioHeatPct = 0.035
		}
		if config.MaxNetExposurePct <= 0 || config.MaxNetExposurePct > 1.0 {
			config.MaxNetExposurePct = 0.65
		}
		if config.MaxSectorExposurePct <= 0 || config.MaxSectorExposurePct > 1.0 {
			config.MaxSectorExposurePct = 0.35
		}
		if config.MaxCorrelatedPositions <= 0 {
			config.MaxCorrelatedPositions = 1
		}
		if config.MaxRuntimeDegradationsPerSession <= 0 {
			config.MaxRuntimeDegradationsPerSession = 3
		}
		if config.MaxReconciliationFailuresPerSession <= 0 {
			config.MaxReconciliationFailuresPerSession = 3
		}
		if config.MaxOrderRejectsPerSession <= 0 {
			config.MaxOrderRejectsPerSession = 5
		}
		if !config.SupervisorReduceOnlyOnDrawdownSet {
			config.SupervisorReduceOnlyOnDrawdown = true
		}
		if config.LossStreakPauseThreshold <= 0 {
			config.LossStreakPauseThreshold = 3
		}
		if config.LossStreakPauseCycles <= 0 {
			config.LossStreakPauseCycles = 5
		}
		if config.PerformanceRiskLookback <= 0 {
			config.PerformanceRiskLookback = 20
		}
		if config.VolatilityBrakeTargetPct <= 0 {
			config.VolatilityBrakeTargetPct = 0.008
		}
		if config.VolatilityBrakeLookback <= 0 {
			config.VolatilityBrakeLookback = 40
		}
		if config.VolatilityBrakeMinScale <= 0 || config.VolatilityBrakeMinScale > 1.0 {
			config.VolatilityBrakeMinScale = 0.45
		}
		if config.KellyFractionCap <= 0 || config.KellyFractionCap > 1.0 {
			config.KellyFractionCap = 0.33
		}
		if config.KellyLookback <= 0 {
			config.KellyLookback = 30
		}
		if config.KellyMinTrades <= 0 {
			config.KellyMinTrades = 10
		}
		if config.MarketStressEntryBlock <= 0 || config.MarketStressEntryBlock > 1.0 {
			config.MarketStressEntryBlock = 0.82
		}
		if config.MarketStressRiskMinScale <= 0 || config.MarketStressRiskMinScale > 1.0 {
			config.MarketStressRiskMinScale = 0.35
		}
		if config.NewsProvider == "" {
			config.NewsProvider = "rss"
		}
		if config.NewsLookbackMinutes <= 0 {
			config.NewsLookbackMinutes = 240
		}
		if config.NewsRefreshSeconds <= 0 {
			config.NewsRefreshSeconds = 120
		}
		if config.NewsMarketImpactThresh <= 0 || config.NewsMarketImpactThresh > 1.0 {
			config.NewsMarketImpactThresh = 0.65
		}
		if config.NewsSymbolImpactThresh <= 0 || config.NewsSymbolImpactThresh > 1.0 {
			config.NewsSymbolImpactThresh = 0.70
		}
		if config.NewsHardBlockThresh <= 0 || config.NewsHardBlockThresh > 1.0 {
			config.NewsHardBlockThresh = 0.85
		}
		if config.NewsMaxRiskReduction <= 0 || config.NewsMaxRiskReduction > 0.95 {
			config.NewsMaxRiskReduction = 0.55
		}
		if config.Mode == "replay" && !config.EnableNewsInReplay {
			config.UseNewsRisk = false
		}
		if len(config.BenchmarkSymbols) == 0 {
			config.BenchmarkSymbols = []string{"SPY", "QQQ", "IWM", "DIA"}
		}
	}
	if config.Mode == "replay" && config.ReplayWarmupBars <= 0 {
		config.ReplayWarmupBars = 120
	}
	if config.MaxRuntimeDegradationsPerSession <= 0 {
		config.MaxRuntimeDegradationsPerSession = 3
	}
	if config.MaxReconciliationFailuresPerSession <= 0 {
		config.MaxReconciliationFailuresPerSession = 3
	}
	if config.MaxOrderRejectsPerSession <= 0 {
		config.MaxOrderRejectsPerSession = 5
	}
	if !config.SupervisorReduceOnlyOnDrawdownSet {
		config.SupervisorReduceOnlyOnDrawdown = true
	}
	if config.DemoMode {
		if config.Mode == "" {
			config.Mode = "paper"
		}
		if config.Exchange == "" {
			config.Exchange = "demo"
		}
	}

	mcpClient := mcp.New()

	// Initialize AI
	if config.DemoMode {
		log.Printf(" [%s] Demo mode enabled (synthetic paper feed, no AI/broker calls)", config.Name)
	} else if config.AIModel == "custom" {
		// Custom API
		mcpClient.SetCustomAPI(config.CustomAPIURL, config.CustomAPIKey, config.CustomModelName)
		log.Printf(" [%s] Using custom AI API: %s (model: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)
	} else if config.UseQwen || config.AIModel == "qwen" {
		// Qwen
		mcpClient.SetQwenAPIKey(config.QwenKey, "")
		log.Printf(" [%s] Using Alibaba Cloud Qwen AI", config.Name)
	} else {
		// Default to DeepSeek
		mcpClient.SetDeepSeekAPIKey(config.DeepSeekKey)
		log.Printf(" [%s] Using DeepSeek AI", config.Name)
	}

	// Initialize coin pool API
	if config.CoinPoolAPIURL != "" {
		pool.SetCoinPoolAPI(config.CoinPoolAPIURL)
	}

	// Default exchange
	if config.Exchange == "" {
		config.Exchange = "binance"
	}

	// Build the appropriate trader instance
	var trader Trader
	var err error
	var provider market.BarsProvider

	if config.DemoMode {
		if config.Mode == "" {
			config.Mode = "paper"
		}
		if config.Exchange == "" {
			config.Exchange = "demo"
		}
		trader = NewSimTrader(config.InitialBalance, nil)
		log.Printf(" [%s] Using built-in demo paper simulator", config.Name)
	} else {
		switch config.Exchange {
		case "binance":
			log.Printf(" [%s] Using Binance Futures", config.Name)
			trader = NewFuturesTrader(config.BinanceAPIKey, config.BinanceSecretKey)
		case "hyperliquid":
			log.Printf(" [%s] Using Hyperliquid", config.Name)
			trader, err = NewHyperliquidTrader(config.HyperliquidPrivateKey, config.HyperliquidWalletAddr, config.HyperliquidTestnet)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize Hyperliquid trader: %w", err)
			}
		case "aster":
			log.Printf(" [%s] Using Aster", config.Name)
			trader, err = NewAsterTrader(config.AsterUser, config.AsterSigner, config.AsterPrivateKey)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize Aster trader: %w", err)
			}
		case "ibkr":
			log.Printf(" [%s] Using Interactive Brokers", config.Name)
			// IBKR trader instantiated explicitly after provider setup below
		case "alpaca":
			log.Printf(" [%s] Using Alpaca", config.Name)
			// Alpaca trader will be instantiated after checking modes
		case "demo":
			trader = NewSimTrader(config.InitialBalance, nil)
			log.Printf(" [%s] Using built-in demo paper simulator", config.Name)
		default:
			return nil, fmt.Errorf("unsupported exchange platform: %s", config.Exchange)
		}
	}

	// Validate initial balance
	if config.InitialBalance <= 0 {
		return nil, fmt.Errorf("initial balance must be greater than 0, please configure InitialBalance")
	}

	// Initialize decision logger (independent directory using trader ID)
	logDir := fmt.Sprintf("decision_logs/%s", config.ID)
	decisionLogger := logger.NewDecisionLogger(logDir)

	// Determine data provider based on config
	if !config.DemoMode && config.Exchange != "demo" {
		if config.InstrumentType == "equity" {
			if config.DataProvider == "csv" {
				provider = market.NewCSVProvider(config.CSVDataDir)
				log.Printf(" [%s] Using CSV Data Provider from %s", config.Name, config.CSVDataDir)
			} else if config.DataProvider == "ibkr" {
				ibkrProvider := market.NewIBKRProvider(config.IBKRGatewayURL, config.IBKRAccountID, config.IBKRSessionCookie)
				provider = ibkrProvider
				log.Printf(" [%s] Using IBKR Data Provider", config.Name)
			} else {
				provider = market.NewAlpacaProvider(config.AlpacaAPIKey, config.AlpacaSecretKey)
				log.Printf(" [%s] Using Alpaca Data Provider", config.Name)
			}

			// Initialize trader based on exchange
			if config.Exchange == "ibkr" {
				if config.Broker == "sim" {
					trader = NewSimTrader(config.InitialBalance, provider)
					log.Printf(" [%s] Using Simulated Broker against IBKR Data", config.Name)
				} else {
					trader = NewIBKRTrader(config.IBKRGatewayURL, config.IBKRAccountID, config.IBKRSessionCookie, provider.(*market.IBKRProvider), config.InitialBalance)
					log.Printf(" [%s] Using Interactive Brokers Live Execution Engine", config.Name)
				}
			} else if config.Exchange == "alpaca" {
				log.Printf("DEBUG: AlpacaPaperTrading flag is: %v", config.AlpacaPaperTrading)
				if config.Broker == "sim" {
					trader = NewSimTrader(config.InitialBalance, provider)
					log.Printf(" [%s] Using Simulated Broker (Replay/Mock Mode)", config.Name)
				} else {
					trader = NewAlpacaTrader(config.AlpacaAPIKey, config.AlpacaSecretKey, config.AlpacaPaperTrading, config.InstrumentType)
					if config.AlpacaPaperTrading {
						log.Printf(" [%s] Using Alpaca Paper Broker", config.Name)
					} else {
						log.Printf(" [%s] Using Alpaca LIVE Broker", config.Name)
					}
				}
			}
		} else {
			provider = market.NewBinanceProvider()
			log.Printf(" [%s] Using Binance Data Provider", config.Name)
		}
	}

	trustedSymbols := map[string]struct{}{}
	if !config.DemoMode && config.InstrumentType == "equity" && strings.TrimSpace(config.TrustedSymbolsFile) != "" {
		set, err := loadSymbolSetFromFile(config.TrustedSymbolsFile)
		if err != nil {
			log.Printf(" [%s] Failed to load trusted_symbols_file '%s': %v", config.Name, config.TrustedSymbolsFile, err)
		} else {
			trustedSymbols = set
			log.Printf(" [%s] Trusted equity symbol list loaded (%d symbols)", config.Name, len(trustedSymbols))
		}
	}

	if config.InstrumentType == "equity" {
		normalizedBenchmarks := make([]string, 0, len(config.BenchmarkSymbols))
		seen := make(map[string]struct{}, len(config.BenchmarkSymbols))
		for _, raw := range config.BenchmarkSymbols {
			symbol := strings.ToUpper(strings.TrimSpace(raw))
			if symbol == "" {
				continue
			}
			if _, ok := seen[symbol]; ok {
				continue
			}
			seen[symbol] = struct{}{}
			normalizedBenchmarks = append(normalizedBenchmarks, symbol)
		}
		if len(normalizedBenchmarks) > 0 {
			config.BenchmarkSymbols = normalizedBenchmarks
		}
	}

	if sim, ok := trader.(*SimTrader); ok {
		sim.SetExecutionCostModel(currentExecutionCostModel(config))
	}

	var newsProvider news.Provider
	if config.InstrumentType == "equity" && config.UseNewsRisk {
		if np, err := news.NewProvider(config.NewsProvider); err != nil {
			log.Printf(" [%s] News provider disabled: %v", config.Name, err)
		} else {
			newsProvider = np
			log.Printf(" [%s] News risk provider active: %s", config.Name, np.Name())
		}
	}

	at := &AutoTrader{
		id:               config.ID,
		name:             config.Name,
		aiModel:          config.AIModel,
		exchange:         config.Exchange,
		config:           config,
		riskEngine:       risk.NewEngine(buildRiskConfig(config)),
		executionManager: execution.NewManager(execution.Config{}),
		auditRecorder: audit.NewRecorder(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     config.ID,
			TraderName:   config.Name,
			Mode:         config.Mode,
			Broker:       config.Broker,
			StrategyMode: config.StrategyMode,
		}),
		eventJournal: audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
			TraderID:     config.ID,
			TraderName:   config.Name,
			Mode:         config.Mode,
			Broker:       config.Broker,
			StrategyMode: config.StrategyMode,
		}),
		alertManager:          newAlertManager(config),
		incidentManager:       incidents.NewManager(config.ID),
		trader:                trader,
		mcpClient:             mcpClient,
		decisionLogger:        decisionLogger,
		initialBalance:        config.InitialBalance,
		dailyStartEquity:      config.InitialBalance,
		peakEquitySeen:        config.InitialBalance,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		positionEntryCycle:    make(map[string]int),
		positionPeakPnLPct:    make(map[string]float64),
		symbolCooldownUntil:   make(map[string]int),
		symbolEdgeScore:       make(map[string]float64),
		symbolTradeCount:      make(map[string]int),
		recentClosePnLPct:     make([]float64, 0, 32),
		recentEquity:          []float64{config.InitialBalance},
		latestKellyScale:      1.0,
		latestNewsScale:       1.0,
		newsProvider:          newsProvider,
		newsCredibilityGlobal: 1.0,
		newsCredibility:       make(map[string]float64),
		newsSampleCount:       make(map[string]int),
		plannedNewsBias:       make(map[string]float64),
		positionNewsBias:      make(map[string]float64),
		provider:              provider,
		trustedSymbolSet:      trustedSymbols,
		demoMode:              config.DemoMode || config.Exchange == "demo",
		demoRand:              rand.New(rand.NewSource(time.Now().UnixNano())),
		demoEquity:            config.InitialBalance,
		demoAvailableBalance:  config.InitialBalance,
		demoPositionCount:     0,
		demoMarginUsedPct:     0,
	}
	if err := at.initializeTradingUniverse(); err != nil {
		return nil, err
	}
	if observerSetter, ok := trader.(interface{ SetOrderObserver(orders.Observer) }); ok {
		if observer := at.buildOrderObserver(); observer != nil {
			observerSetter.SetOrderObserver(observer)
		}
	}
	if orderLookup, ok := trader.(execution.OrderLookup); ok && at.executionManager != nil {
		at.executionManager.SetOrderLookup(orderLookup)
	}

	if at.newsProvider != nil && at.config.UseNewsRisk {
		if err := at.loadNewsLearningState(); err != nil {
			log.Printf(" [%s] News learning state unavailable: %v", config.Name, err)
		}
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()
	at.initializePromotionSummary()
	at.initializeKillSwitchState()
	at.initializeDataQualityState()
	at.initializePositionReconciliationState()
	at.initializeRiskSupervisorState()
	at.initializePendingProtectionState()
	at.initializeShadowModeState()
	at.initializeRestartRecoveryState()
	at.restoreStrategyAccountingState()
	if lifecycleHookSetter, ok := trader.(interface{ SetLifecyclePersistenceHook(func()) }); ok {
		lifecycleHookSetter.SetLifecyclePersistenceHook(func() {
			at.persistDurableRuntimeState("order_lifecycle_reconcile")
		})
	}
	at.restoreDurableRuntimeState()

	// #13 — session guard: only required for equity instrument type.
	if config.InstrumentType == "equity" {
		tz := config.SessionTimezone
		if tz == "" {
			tz = "America/New_York"
		}
		sg, err := NewSessionGuard(tz, config.AllowExtendedHours)
		if err != nil {
			log.Printf(" [%s] session guard init failed (timezone=%q): %v; session guard disabled", config.Name, tz, err)
		} else {
			at.sessionGuard = sg
		}
	}

	// #14 — order throttle: always initialized.
	throttleBurst := config.OrderThrottleMaxBurst
	if throttleBurst <= 0 {
		throttleBurst = 10
	}
	throttleRate := config.OrderThrottlePerMinute
	if throttleRate <= 0 {
		throttleRate = 20
	}
	at.orderThrottle = NewOrderThrottle(throttleBurst, throttleRate)

	return at, nil
}

// Run the automated trading loop
func (at *AutoTrader) Run() error {
	if at.config.MaxCycles > 0 {
		return at.RunBacktest(at.config.MaxCycles)
	}

	at.isRunning = true
	at.runDone = make(chan struct{})
	defer close(at.runDone)
	at.startPaperSessionReporting(time.Now())
	defer at.finalizePaperSessionReport("run_exit")
	log.Println(" AI-driven auto trading system started")
	currency := "USDT"
	if at.exchange == "ibkr" || at.exchange == "alpaca" {
		currency = "$"
	}

	log.Printf(" Initial balance: %.2f %s", at.initialBalance, currency)
	log.Printf("  Scan interval: %v", at.config.ScanInterval)
	log.Printf(" Strategy mode: %s", at.config.StrategyMode)
	log.Printf(" Decision architecture: %s", at.canonicalDecisionArchitecture())
	at.persistTradingUniverseManifest()
	if at.config.InstrumentType == "equity" && (at.config.StrategyMode == "momentum_only" || at.config.StrategyMode == "multi_factor") {
		log.Println(" Local strategy engine controls position sizing and exits for this trader")
	} else {
		log.Println(" AI will have full control over leverage, position size, and stop/take profit parameters")
	}
	if err := at.waitForKillSwitchClear(); err != nil {
		return err
	}
	at.startKillSwitchMonitor()
	// Run startup self-check for actionable diagnostics before the readiness gate.
	// This is informational only — it does not block startup.
	selfCheckReport := RunStartupSelfCheck(at)
	LogStartupCheckReport(at, selfCheckReport)
	if err := at.waitForStartupReadiness(); err != nil {
		return err
	}
	if err := at.waitForLivePromotionApproval(); err != nil {
		return err
	}
	if err := at.waitForPositionReconciliationBootstrap(); err != nil {
		return err
	}
	if err := at.waitForKillSwitchClear(); err != nil {
		return err
	}
	at.startPositionReconciliationLoop()
	modeParity := at.currentModeParitySummary()
	log.Printf(" Mode parity: profile=%s | %s", modeParity.Profile, modeParity.Summary)
	if len(modeParity.Gaps) > 0 {
		log.Printf("  Evidence gaps: %s", strings.Join(modeParity.Gaps, "; "))
	}
	if len(modeParity.Warnings) > 0 {
		log.Printf("  Evidence warnings: %s", strings.Join(modeParity.Warnings, "; "))
	}

	for at.isRunning {
		if err := at.runCycle(); err != nil {
			log.Printf(" Execution failed: %v", err)
		}
		if !at.isRunning {
			break
		}
		time.Sleep(at.config.ScanInterval)
	}

	return nil
}

// RunBacktest executes a finite number of replay/backtest cycles without scan interval waits.
func (at *AutoTrader) RunBacktest(maxCycles int) error {
	if maxCycles <= 0 {
		return fmt.Errorf("max backtest cycles must be greater than 0")
	}

	at.isRunning = true
	at.backtestMode = true
	defer func() { at.backtestMode = false }()
	log.Printf(" Backtest mode started (%d cycles)", maxCycles)
	if err := at.waitForKillSwitchClear(); err != nil {
		at.isRunning = false
		return err
	}
	summary := at.runReadinessChecks()
	at.logReadinessSummary(summary)
	if !summary.TradingAllowed {
		at.isRunning = false
		return fmt.Errorf("startup readiness blocked backtest start: %s", summary.Message)
	}

	for at.isRunning && at.callCount < maxCycles {
		if err := at.runCycle(); err != nil {
			log.Printf(" Backtest cycle error: %v", err)
		}
	}

	at.closeAllOpenPositions()
	at.Stop()
	return nil
}

// Stop shuts down the auto trader
func (at *AutoTrader) Stop() {
	at.isRunning = false
	done := at.runDone

	type summarizer interface {
		ExportSummary()
	}
	if s, ok := at.trader.(summarizer); ok {
		s.ExportSummary()
	}

	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			at.finalizePaperSessionReport("stop_timeout")
		}
	} else {
		at.finalizePaperSessionReport("stop")
	}
	at.persistDurableRuntimeState("stop")

	log.Println(" Auto trading system stopped")
}

func (at *AutoTrader) closeAllOpenPositions() {
	positions, err := at.trader.GetPositions()
	if err != nil {
		log.Printf(" Failed to fetch positions for forced backtest close: %v", err)
		return
	}
	record := &logger.DecisionRecord{
		ExecutionLog: []string{"forced backtest closeout"},
		Success:      true,
	}

	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if symbol == "" || side == "" {
			continue
		}

		actionRecord := logger.DecisionAction{
			Symbol:    symbol,
			Timestamp: time.Now(),
		}
		var (
			closeErr error
			order    map[string]interface{}
		)
		switch side {
		case "long":
			actionRecord.Action = "close_long"
			order, closeErr = at.trader.CloseLong(symbol, 0)
		case "short":
			actionRecord.Action = "close_short"
			order, closeErr = at.trader.CloseShort(symbol, 0)
		}
		if closeErr != nil {
			log.Printf(" Failed to force-close %s %s at backtest end: %v", symbol, side, closeErr)
			actionRecord.Error = closeErr.Error()
			record.Success = false
		} else {
			actionRecord.Success = true
			at.applyActionAccountingMetadata(&actionRecord, order)
		}
		record.Decisions = append(record.Decisions, actionRecord)
	}
	if len(record.Decisions) > 0 {
		_ = at.logDecisionAndAudit(record, nil, nil)
	}
}

func (at *AutoTrader) ensureIBKRLiveReady() error {
	if at.exchange != "ibkr" || !at.config.StrictLiveMode || !strings.EqualFold(at.config.Mode, "live") {
		return nil
	}

	ibkrProv, ok := at.provider.(*market.IBKRProvider)
	if !ok || ibkrProv == nil || ibkrProv.Client == nil {
		return fmt.Errorf("strict_live_mode requires an initialized IBKR provider")
	}

	return ibkrProv.Client.CheckLiveReadiness(at.config.IBKRAccountID)
}

func (at *AutoTrader) prepareReplayStep() bool {
	if !strings.EqualFold(at.config.Mode, "replay") || at.provider == nil {
		return false
	}

	controller, ok := at.provider.(market.ReplayController)
	if !ok {
		return false
	}

	if !at.replayInitialized {
		warmup := at.config.ReplayWarmupBars
		if warmup < 80 {
			warmup = 80
		}
		controller.EnableReplay(warmup)
		at.replayInitialized = true
		cursor, maxCursor := controller.ReplayProgress()
		log.Printf(" Replay cursor initialized at %d/%d bars", cursor, maxCursor)
		return false
	}

	if controller.AdvanceReplay(1) {
		return false
	}

	cursor, maxCursor := controller.ReplayProgress()
	log.Printf(" Replay dataset exhausted at %d/%d bars, stopping backtest", cursor, maxCursor)
	if at.backtestMode {
		at.isRunning = false
	} else {
		at.Stop()
	}
	return true
}

func (at *AutoTrader) runCycle() error {
	if at.prepareReplayStep() {
		return nil
	}
	at.ensurePaperSessionReportingForTime(time.Now())
	at.recordPaperSessionCycleStart()
	at.callCount++

	log.Println("\n" + strings.Repeat("=", 70))
	log.Printf(" %s - AI Decision cycle #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
	log.Println(strings.Repeat("=", 70))

	if err := at.ensureBrokerTruthReadyForTrading(); err != nil {
		at.recordPaperSessionBlockedCycle(err.Error())
		log.Printf(" broker-truth gate active: %v", err)
	}

	// 0. Independent supervisory risk gate
	initialGate := at.currentTradingGateDecision(true, at.currentLatestAccountSummary())
	at.journalTradingGateDecision("cycle_initial", initialGate)
	if !initialGate.ExitsAllowed {
		at.recordPaperSessionBlockedCycle(initialGate.BlockReason)
		log.Printf(" risk supervisor gate active: %s", initialGate.Message)
		return nil
	}
	if !initialGate.EntriesAllowed {
		log.Printf(" risk supervisor mode=%s: existing exposure may be reduced, new entries are blocked", initialGate.Mode)
	}

	// Generate decision record
	record := &logger.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
		ShadowMode:   at.shadowModeEnabled(),
	}
	defer at.observePaperSessionDecisionRecord(record)

	// 1. Daily P&L reset checkpoint
	needsDailyReset := time.Since(at.lastResetTime) > 24*time.Hour

	if at.demoMode {
		return at.runDemoCycle()
	}

	// 2. Collect context mappings
	ctx, err := at.buildTradingContext()
	if err != nil {
		err = at.handleIBKRRuntimeError("build_context", err)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to construct market trading context: %v", err)
		_ = at.logDecisionAndAudit(record, nil, nil)
		return fmt.Errorf("failed to construct market trading context limits configurations array bindings parameter: %w", err)
	}
	if err := at.ensureShadowPipelineContext(ctx); err != nil {
		if at.shadowModeEnabled() && at.isExpectedMarketDataBlock(err) {
			reason := fmt.Sprintf("Shadow pipeline blocked: %v", err)
			at.recordPaperSessionBlockedCycle(reason)
			record.ExecutionLog = append(record.ExecutionLog, reason)
			log.Printf(" [%s] %s", at.name, reason)
			_ = at.logDecisionAndAudit(record, ctx, nil)
			return nil
		}
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to prepare shadow-mode pipeline context: %v", err)
		_ = at.logDecisionAndAudit(record, ctx, nil)
		return err
	}
	at.refreshPositionState(ctx.Positions)
	at.processPendingProtections(ctx)
	if ctx.Account.StrategyEquity > at.peakEquitySeen {
		at.peakEquitySeen = ctx.Account.StrategyEquity
	}
	if at.peakEquitySeen <= 0 {
		at.peakEquitySeen = ctx.Account.StrategyEquity
	}
	if needsDailyReset {
		at.dailyStartEquity = ctx.Account.StrategyEquity
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		log.Println(" Daily P&L constraints reset")
	} else {
		if at.dailyStartEquity <= 0 {
			at.dailyStartEquity = ctx.Account.StrategyEquity
		}
		at.dailyPnL = ctx.Account.StrategyEquity - at.dailyStartEquity
	}
	if at.config.InstrumentType == "equity" && at.config.MaxDailyLossPct > 0 {
		baseline := at.dailyStartEquity
		if baseline <= 0 {
			baseline = at.initialBalance
		}
		dailyLossLimit := -baseline * at.config.MaxDailyLossPct
		if at.dailyPnL <= dailyLossLimit {
			if at.stopUntil.Before(time.Now()) {
				at.stopUntil = time.Now().Add(at.config.StopTradingTime)
				at.alertDailyLossLimit(at.dailyPnL, dailyLossLimit, at.stopUntil)
				log.Printf(" Equity daily loss guard triggered: PnL %.2f <= %.2f. Trading paused until %s",
					at.dailyPnL, dailyLossLimit, at.stopUntil.Format(time.RFC3339))
			}
		}
	}
	postAccount := at.currentLatestAccountSummary()
	if postAccount == nil {
		postAccount = &AccountSummary{AccountingVersion: accountingVersion}
	}
	postAccount.AccountingVersion = accountingVersion
	postAccount.AccountCash = ctx.Account.AccountCash
	postAccount.AvailableBalance = ctx.Account.AvailableBalance
	postAccount.AccountEquity = ctx.Account.AccountEquity
	postAccount.GrossMarketValue = ctx.Account.GrossMarketValue
	postAccount.UnrealizedPnL = ctx.Account.UnrealizedPnL
	postAccount.RealizedPnL = ctx.Account.RealizedPnL
	postAccount.TotalPnL = ctx.Account.TotalPnL
	postAccount.StrategyInitialCapital = ctx.Account.StrategyInitialCapital
	postAccount.StrategyEquity = ctx.Account.StrategyEquity
	postAccount.StrategyReturnPct = ctx.Account.StrategyReturnPct
	postAccount.DailyPnL = at.dailyPnL
	postAccount.PositionCount = ctx.Account.PositionCount
	postAccount.MarginUsed = ctx.Account.MarginUsed
	postAccount.MarginUsedPct = ctx.Account.MarginUsedPct
	at.setLatestAccountSummary(postAccount)
	postAccount = at.currentLatestAccountSummary()
	postGate := at.currentTradingGateDecision(false, postAccount)
	at.journalTradingGateDecision("cycle_post_decision", postGate)
	if !postGate.ExitsAllowed {
		at.recordPaperSessionBlockedCycle(postGate.BlockReason)
		record.Success = false
		record.ErrorMessage = postGate.BlockReason
		_ = at.logDecisionAndAudit(record, ctx, nil)
		return nil
	}
	at.recordEquityObservation(ctx.Account.StrategyEquity)

	record.AccountState = logger.AccountSnapshot{
		AccountingVersion:      accountingVersion,
		AccountCash:            ctx.Account.AccountCash,
		AccountEquity:          ctx.Account.AccountEquity,
		AvailableBalance:       ctx.Account.AvailableBalance,
		GrossMarketValue:       ctx.Account.GrossMarketValue,
		UnrealizedPnL:          ctx.Account.UnrealizedPnL,
		RealizedPnL:            ctx.Account.RealizedPnL,
		TotalPnL:               ctx.Account.TotalPnL,
		StrategyInitialCapital: ctx.Account.StrategyInitialCapital,
		StrategyEquity:         ctx.Account.StrategyEquity,
		StrategyReturnPct:      ctx.Account.StrategyReturnPct,
		DailyPnL:               at.dailyPnL,
		PositionCount:          ctx.Account.PositionCount,
		MarginUsed:             ctx.Account.MarginUsed,
		MarginUsedPct:          ctx.Account.MarginUsedPct,
		TotalBalance:           ctx.Account.AccountEquity,
		TotalUnrealizedProfit:  ctx.Account.UnrealizedPnL,
	}

	// Save Strings Limit Tracker
	for _, pos := range ctx.Positions {
		record.Positions = append(record.Positions, logger.PositionSnapshot{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			PositionAmt:      pos.Quantity,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedProfit: pos.UnrealizedPnL,
			Leverage:         float64(pos.Leverage),
			LiquidationPrice: pos.LiquidationPrice,
		})
	}

	// Collect string strings limits mapping
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	currency := "USDT"
	if at.exchange == "ibkr" || at.exchange == "alpaca" {
		currency = "$"
	}

	log.Printf(" Broker equity: %.2f %s | strategy equity: %.2f %s | total P&L: %.2f %s | Positions: %d",
		ctx.Account.AccountEquity, currency,
		ctx.Account.StrategyEquity, currency,
		ctx.Account.TotalPnL, currency,
		ctx.Account.PositionCount)

	// 4. Request decision logic
	log.Println(" Requesting decision analysis...")
	fullDecision, err := at.getDecision(ctx)

	// Log configurations maps strings map Array limits Strings
	if fullDecision != nil {
		record.InputPrompt = fullDecision.UserPrompt
		record.CoTTrace = fullDecision.CoTTrace
		if len(fullDecision.Decisions) > 0 {
			decisionJSON, _ := json.MarshalIndent(fullDecision.Decisions, "", "  ")
			record.DecisionJSON = string(decisionJSON)
		}
	}

	if err != nil {
		if at.isExpectedMarketDataBlock(err) {
			reason := fmt.Sprintf("Decision generation blocked: %v", err)
			at.recordPaperSessionBlockedCycle(reason)
			record.ExecutionLog = append(record.ExecutionLog, reason)
			log.Printf(" [%s] %s", at.name, reason)
			_ = at.logDecisionAndAudit(record, ctx, nil)
			return nil
		}
		err = at.handleIBKRRuntimeError("decision_generation", err)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("AI string array targeting Map array MAP maps limitations array configurations constraints limitation: %v", err)

		// Tracker String Map map limitation Strings Object
		if fullDecision != nil && fullDecision.CoTTrace != "" {
			log.Println("\n" + strings.Repeat("-", 70))
			log.Println(" AI Chain of Thought analysis (error case):")
			log.Println(strings.Repeat("-", 70))
			log.Println(fullDecision.CoTTrace)
			log.Println(strings.Repeat("-", 70))
		}

		_ = at.logDecisionAndAudit(record, ctx, nil)
		return fmt.Errorf("AI strings Array Map constraints maps Logic variables tracking maps limitations Array Mapping Parameters tracking Targeting limitations parameters MAP limitations MAP target MAP strings maps map limitation tracking loops mapping limits limit limitations bounds Mapping: %w", err)
	}

	at.maybeApplyEquityMomentumFallback(ctx, fullDecision)
	at.applyEquityDecisionOverlay(ctx, fullDecision)
	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
	record.Pipeline = at.buildPipelineObservations(ctx)
	if len(fullDecision.Decisions) > 0 {
		decisionJSON, _ := json.MarshalIndent(fullDecision.Decisions, "", "  ")
		record.DecisionJSON = string(decisionJSON)
	}

	// 5. String strings maps Limit Limit Limit maps Tracking map mapping Mapping Tracker Strings limitations MAP limitation Tracking Array permutations MAP
	log.Println("\n" + strings.Repeat("-", 70))
	log.Println(" AI Chain of Thought analysis:")
	log.Println(strings.Repeat("-", 70))
	log.Println(fullDecision.CoTTrace)
	log.Println(strings.Repeat("-", 70))

	// 6. Limits limits Variable tracking logic Map variables string arrays tracking LIMIT Mapper map Mapping limits Strings
	log.Printf(" AI Decision list (%d limits Variables): \n", len(fullDecision.Decisions))
	for i, d := range fullDecision.Decisions {
		log.Printf("  [%d] %s: %s - %s", i+1, d.Symbol, d.Action, d.Reasoning)
		if d.Action == "open_long" || d.Action == "open_short" {
			currency := "USDT"
			if at.exchange == "ibkr" || at.exchange == "alpaca" {
				currency = "$"
			}
			log.Printf("      Leverage: %dx | Position: %.2f %s | Stop loss: %.4f | Take profit: %.4f",
				d.Leverage, d.PositionSizeUSD, currency, d.StopLoss, d.TakeProfit)
		}
	}
	log.Println()

	// 7. Tracker mapping Tracker map Strings limitation Tracking String Map tracking String LIMIT
	sortedDecisions := sortDecisionsByPriority(fullDecision.Decisions)
	positionPnLPctByKey := make(map[string]float64, len(ctx.Positions))
	positionsByKey := make(map[string]decision.PositionInfo, len(ctx.Positions))
	for _, pos := range ctx.Positions {
		key := positionActionKey(pos.Symbol, pos.Side)
		if key != "_" {
			positionPnLPctByKey[key] = pos.UnrealizedPnLPct
			positionsByKey[key] = pos
		}
	}

	log.Println(" Execution order optimizations limits parameters bounds Target Mapping Limits Strings limits array (optimized): close first -> open later")
	for i, d := range sortedDecisions {
		log.Printf("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	log.Println()

	// Tracker mapping string strings Array variables Target limit tracking limits Maps Limit String Variables mapping limits LIMIT tracking values tracking Array Tracking Target tracking Map Arrays limitations Tracker MAP parameters
	for _, d := range sortedDecisions {
		actionRecord := logger.DecisionAction{
			Action:               d.Action,
			Symbol:               d.Symbol,
			DecisionReasoning:    d.Reasoning,
			DecisionConfidence:   d.Confidence,
			DecisionPositionSize: d.PositionSizeUSD,
			DecisionStopLoss:     d.StopLoss,
			DecisionTakeProfit:   d.TakeProfit,
			Quantity:             0,
			Leverage:             d.Leverage,
			Price:                0,
			Timestamp:            time.Now(),
			Success:              false,
		}
		actionRecord.Pipeline = at.buildPipelineDecision(ctx, d)

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			log.Printf(" Decision execution failed (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			if strings.TrimSpace(actionRecord.RiskSummary) != "" {
				record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" risk %s %s", d.Symbol, actionRecord.RiskSummary))
			}
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" %s %s limit Mapping limitations map tracking Map limit: %v", d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			at.updateStrategyAccountingFromAction(&actionRecord, positionsByKey)
			if strings.TrimSpace(actionRecord.RiskSummary) != "" {
				record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" risk %s %s", d.Symbol, actionRecord.RiskSummary))
			}
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" %s %s target Array limit logic Map limitations parameter Strings values configurations tracking String string combinations limit maps", d.Symbol, d.Action))
			// Strings Strings limitations Target limit limitations parameters
			if !strings.EqualFold(at.config.Mode, "replay") {
				time.Sleep(1 * time.Second)
			}
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}
	at.updateExecutionState(record.Decisions)
	at.updateSymbolEdgeFromActions(record.Decisions, positionPnLPctByKey)
	at.updateClosePerformanceFromActions(record.Decisions, positionPnLPctByKey)

	// 8. String Tracking string limits map Map Maps Mapping
	if err := at.logDecisionAndAudit(record, ctx, fullDecision); err != nil {
		log.Printf(" Failed to save decision record strings tracking permutations limits Limits Mapping string Maps tracking : %v", err)
	}
	at.clearBlockedCycle()

	return nil
}

