package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
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
	"os"
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
		sleepDuration := at.config.ScanInterval
		if blocked := at.currentBlockedCycle(); blocked.ExpectedNonTradable {
			// Back off when market is closed to reduce unnecessary API calls and log noise
			sleepDuration = at.marketClosedBackoffInterval()
		}
		time.Sleep(sleepDuration)
	}

	return nil
}

// marketClosedBackoffInterval returns a longer sleep interval when the market
// is closed. Uses 15 minutes as a reasonable backoff to keep broker truth fresh
// while avoiding unnecessary API and log churn.
func (at *AutoTrader) marketClosedBackoffInterval() time.Duration {
	const closedInterval = 15 * time.Minute
	if at.config.ScanInterval >= closedInterval {
		return at.config.ScanInterval
	}
	return closedInterval
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

func (at *AutoTrader) runDemoCycle() error {
	if at.demoRand == nil {
		at.demoRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	phase := float64(at.callCount%96) / 96.0 * 2.0 * math.Pi
	wavePct := 0.04*math.Sin(phase) + 0.02*math.Cos(phase*0.5)
	noisePct := (at.demoRand.Float64() - 0.5) * 0.06
	changePct := wavePct + noisePct

	nextEquity := at.demoEquity * (1.0 + (changePct / 100.0))
	floor := at.initialBalance * 0.82
	ceiling := at.initialBalance * 1.40
	if nextEquity < floor {
		nextEquity = floor + at.initialBalance*0.01*at.demoRand.Float64()
	}
	if nextEquity > ceiling {
		nextEquity = ceiling - at.initialBalance*0.01*at.demoRand.Float64()
	}

	at.demoEquity = nextEquity
	at.demoPositionCount = at.demoRand.Intn(4)
	at.demoMarginUsedPct = 8.0 + at.demoRand.Float64()*28.0
	if at.demoPositionCount == 0 {
		at.demoMarginUsedPct = 0
	}
	now := time.Now()
	at.demoSnapshotSeed = now.UnixNano()

	positions := at.buildDemoPositions()
	totalMarginUsed := 0.0
	totalUnrealized := 0.0
	for _, pos := range positions {
		if v, ok := pos["margin_used"].(float64); ok {
			totalMarginUsed += v
		}
		if v, ok := pos["unrealized_pnl"].(float64); ok {
			totalUnrealized += v
		}
	}
	if at.demoEquity > 0 {
		at.demoMarginUsedPct = (totalMarginUsed / at.demoEquity) * 100.0
	} else {
		at.demoMarginUsedPct = 0
	}
	at.demoPositionCount = len(positions)
	walletBalance := at.demoEquity - totalUnrealized
	if walletBalance < 0 {
		walletBalance = 0
	}
	at.demoAvailableBalance = walletBalance - totalMarginUsed
	if at.demoAvailableBalance < 0 {
		at.demoAvailableBalance = 0
	}
	at.demoLastCycleTime = now

	totalPnL := at.demoEquity - at.initialBalance
	at.dailyPnL = totalPnL
	summary := at.buildDemoAccountSummary(positions)

	record := &logger.DecisionRecord{
		InputPrompt:  "Demo mode cycle: synthetic paper update",
		CoTTrace:     "Demo mode is enabled. No live broker, market data, or AI API call was used in this cycle.",
		DecisionJSON: "[]",
		AccountState: logger.AccountSnapshot{
			AccountingVersion:      summary.AccountingVersion,
			AccountCash:            summary.AccountCash,
			AccountEquity:          summary.AccountEquity,
			AvailableBalance:       summary.AvailableBalance,
			GrossMarketValue:       summary.GrossMarketValue,
			UnrealizedPnL:          summary.UnrealizedPnL,
			RealizedPnL:            summary.RealizedPnL,
			TotalPnL:               summary.TotalPnL,
			StrategyInitialCapital: summary.StrategyInitialCapital,
			StrategyEquity:         summary.StrategyEquity,
			StrategyReturnPct:      summary.StrategyReturnPct,
			DailyPnL:               summary.DailyPnL,
			PositionCount:          summary.PositionCount,
			MarginUsed:             summary.MarginUsed,
			MarginUsedPct:          summary.MarginUsedPct,
			TotalBalance:           summary.AccountEquity,
			TotalUnrealizedProfit:  summary.UnrealizedPnL,
		},
		Decisions:    []logger.DecisionAction{},
		ExecutionLog: []string{fmt.Sprintf("demo cycle update: equity=%.2f pnl=%.2f delta=%.4f%%", at.demoEquity, totalPnL, changePct)},
		Success:      true,
	}

	if err := at.logDecisionAndAudit(record, nil, nil); err != nil {
		return fmt.Errorf("failed to write demo decision record: %w", err)
	}
	at.clearBlockedCycle()

	log.Printf(" Demo cycle #%d | equity=%.2f | pnl=%.2f (%.2f%%)",
		at.callCount, at.demoEquity, totalPnL, (totalPnL/at.initialBalance)*100.0)

	return nil
}

func (at *AutoTrader) restoreStrategyAccountingState() {
	records, err := at.decisionLogger.GetLatestRecords(1)
	if err != nil || len(records) == 0 {
		return
	}
	snapshot := records[len(records)-1].AccountState
	if snapshot.AccountingVersion < accountingVersion {
		return
	}
	at.strategyRealizedPnL = snapshot.RealizedPnL
}

func (at *AutoTrader) buildAccountSummaryFromRaw(balance map[string]interface{}, positions []map[string]interface{}) AccountSummary {
	broker := normalizeBrokerAccount(balance, positions)
	return buildAccountSummary(broker, at.initialBalance, at.strategyRealizedPnL, at.dailyPnL)
}

func (at *AutoTrader) buildDemoAccountSummary(positions []map[string]interface{}) AccountSummary {
	grossMarketValue := 0.0
	unrealizedPnL := 0.0
	marginUsed := 0.0
	for _, pos := range positions {
		markPrice, _ := parseFloat(pos["mark_price"])
		quantity, _ := parseFloat(pos["quantity"])
		if quantity < 0 {
			quantity = -quantity
		}
		grossMarketValue += markPrice * quantity
		if pnl, ok := parseFloat(pos["unrealized_pnl"]); ok {
			unrealizedPnL += pnl
		}
		if used, ok := parseFloat(pos["margin_used"]); ok {
			marginUsed += used
		}
	}

	accountCash := at.demoEquity - grossMarketValue - unrealizedPnL
	if accountCash < 0 {
		accountCash = 0
	}
	realizedPnL := (at.demoEquity - at.initialBalance) - unrealizedPnL
	marginUsedPct := 0.0
	if at.demoEquity > 0 {
		marginUsedPct = (marginUsed / at.demoEquity) * 100.0
	}

	return buildAccountSummary(normalizedBrokerAccount{
		AccountCash:      accountCash,
		AvailableBalance: at.demoAvailableBalance,
		AccountEquity:    at.demoEquity,
		GrossMarketValue: grossMarketValue,
		UnrealizedPnL:    unrealizedPnL,
		RealizedPnL:      realizedPnL,
		MarginUsed:       marginUsed,
		MarginUsedPct:    marginUsedPct,
		PositionCount:    len(positions),
	}, at.initialBalance, realizedPnL, at.dailyPnL)
}

func (at *AutoTrader) applyActionAccountingMetadata(actionRecord *logger.DecisionAction, order map[string]interface{}) {
	if actionRecord == nil || order == nil {
		return
	}
	if localID := strings.TrimSpace(toString(firstPresent(order["localOrderId"], order["local_order_id"]))); localID != "" {
		actionRecord.LocalOrderID = localID
	}
	if brokerOrderID := strings.TrimSpace(toString(firstPresent(order["brokerOrderId"], order["broker_order_id"], order["orderId"], order["order_id"], order["id"]))); brokerOrderID != "" {
		actionRecord.BrokerOrderID = brokerOrderID
		if numericOrderID, ok := parseFloat(brokerOrderID); ok {
			actionRecord.OrderID = int64(numericOrderID)
		}
	}
	if status := strings.TrimSpace(toString(firstPresent(order["status"], order["orderStatus"], order["order_status"]))); status != "" {
		actionRecord.OrderStatus = status
	}
	if filledQty, ok := parseFloat(order["filled_qty"]); ok && filledQty > 0 {
		actionRecord.Quantity = filledQty
	}
	if price, ok := parseFloat(order["price"]); ok && price > 0 {
		actionRecord.Price = price
	}
	if fees, ok := parseFloat(order["fees"]); ok {
		actionRecord.FeesUSD = fees
	}
	if pnl, ok := parseFloat(order["pnl"]); ok {
		actionRecord.RealizedPnL = pnl
	}
}

func positionActionKey(symbol, side string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "_" + strings.ToLower(strings.TrimSpace(side))
}

func (at *AutoTrader) updateStrategyAccountingFromAction(actionRecord *logger.DecisionAction, positionsByKey map[string]decision.PositionInfo) {
	if actionRecord == nil || !actionHasImmediatePositionEffect(*actionRecord) {
		return
	}

	switch actionRecord.Action {
	case "open_long", "open_short":
		if actionRecord.FeesUSD != 0 {
			at.strategyRealizedPnL -= actionRecord.FeesUSD
		}
	case "close_long", "close_short":
		if actionRecord.RealizedPnL == 0 {
			side := "long"
			if actionRecord.Action == "close_short" {
				side = "short"
			}
			pos, exists := positionsByKey[positionActionKey(actionRecord.Symbol, side)]
			if !exists {
				return
			}
			quantity := actionRecord.Quantity
			if quantity <= 0 || quantity > pos.Quantity {
				quantity = pos.Quantity
			}
			exitPrice := actionRecord.Price
			if exitPrice <= 0 {
				exitPrice = pos.MarkPrice
			}
			if side == "long" {
				actionRecord.RealizedPnL = (exitPrice - pos.EntryPrice) * quantity
			} else {
				actionRecord.RealizedPnL = (pos.EntryPrice - exitPrice) * quantity
			}
			if actionRecord.FeesUSD != 0 {
				actionRecord.RealizedPnL -= actionRecord.FeesUSD
			}
		}
		at.strategyRealizedPnL += actionRecord.RealizedPnL
	}
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

	// Persist durable runtime state at the end of each cycle so that
	// recent state survives a mid-session crash. Run in a goroutine to
	// avoid blocking the next cycle on disk I/O.
	go at.persistDurableRuntimeState("cycle_end")

	return nil
}

func (at *AutoTrader) isExpectedMarketDataBlock(err error) bool {
	_, ok := classifyExpectedMarketDataBlock(err)
	return ok
}

func classifyExpectedMarketDataBlock(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "market is closed"):
		return message, true
	case strings.Contains(lower, "market-data feed delayed"),
		strings.Contains(lower, "market-data feed unavailable"),
		strings.Contains(lower, "runtime market-data probe failed"):
		return message, true
	case strings.Contains(lower, "data quality blocked"):
		return message, true
	case strings.Contains(lower, "stale by"):
		return message, true
	case strings.Contains(lower, "chart data unavailable"):
		return "IBKR chart history is currently unavailable", true
	case strings.Contains(lower, "/iserver/marketdata/history"):
		return "IBKR market-data history request failed", true
	case strings.Contains(lower, "client.timeout exceeded while awaiting headers"),
		strings.Contains(lower, "context deadline exceeded"):
		return "IBKR market-data history request timed out", true
	case strings.Contains(lower, "failed to load market data for momentum strategy"):
		return message, true
	default:
		return "", false
	}
}

func (at *AutoTrader) getDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	if at.usesCanonicalEquityPipeline() {
		if err := at.prepareCanonicalEquityContext(ctx); err != nil {
			return nil, err
		}
		switch at.config.StrategyMode {
		case "momentum_only":
			return at.buildMomentumOnlyDecision(ctx), nil
		case "multi_factor":
			return at.buildMultiFactorDecision(ctx), nil
		case "hybrid_ai":
			fullDecision, err := decision.GetFullDecision(ctx, at.mcpClient)
			if err != nil {
				// Keep the system autonomous: fallback to local factors when AI API is unavailable.
				if len(ctx.MarketDataMap) > 0 {
					log.Printf(" Hybrid AI fallback activated: switching to local multi-factor engine (%v)", err)
					return at.buildMultiFactorDecision(ctx), nil
				}
				return nil, err
			}
			if len(ctx.MarketDataMap) > 0 {
				at.applyHybridFactorFilter(ctx, fullDecision)
			}
			return fullDecision, nil
		}
	}
	return decision.GetFullDecision(ctx, at.mcpClient)
}

func (at *AutoTrader) loadMomentumMarketData(ctx *decision.Context) error {
	if ctx == nil {
		return fmt.Errorf("missing context")
	}
	if err := at.preflightRuntimeMarketData(ctx); err != nil {
		return err
	}

	ctx.MarketDataMap = make(map[string]*market.Data)
	var lastErr error

	maxSymbols := 32
	if at.config.CandidateBatchSize > 0 && at.config.CandidateBatchSize < maxSymbols {
		maxSymbols = at.config.CandidateBatchSize + 8 // include room for benchmarks and held positions
	}
	if at.config.DataProvider == "ibkr" && maxSymbols > 28 {
		maxSymbols = 28 // avoid aggressive IBKR pacing
	}

	seen := make(map[string]struct{}, maxSymbols+8)
	addUnique := func(target *[]string, raw string) {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			return
		}
		if _, exists := seen[symbol]; exists {
			return
		}
		seen[symbol] = struct{}{}
		*target = append(*target, symbol)
	}

	mandatory := make([]string, 0, len(ctx.Positions)+len(at.config.BenchmarkSymbols))
	for _, pos := range ctx.Positions {
		addUnique(&mandatory, pos.Symbol)
	}
	if at.config.UseMacroFilters {
		for _, benchmark := range at.config.BenchmarkSymbols {
			addUnique(&mandatory, benchmark)
		}
	}

	candidates := make([]string, 0, len(ctx.CandidateCoins))
	for _, coin := range ctx.CandidateCoins {
		addUnique(&candidates, coin.Symbol)
	}

	loadOrder := make([]string, 0, maxSymbols)
	for _, symbol := range mandatory {
		if len(loadOrder) >= maxSymbols {
			break
		}
		loadOrder = append(loadOrder, symbol)
	}
	for _, symbol := range candidates {
		if len(loadOrder) >= maxSymbols {
			break
		}
		loadOrder = append(loadOrder, symbol)
	}
	at.recordUniverseCycleSelection(candidates, mandatory, loadOrder)

	for _, symbol := range loadOrder {
		data, err := at.getValidatedMarketData(symbol)
		if err != nil {
			lastErr = err
			continue
		}
		ctx.MarketDataMap[symbol] = data
	}

	if len(ctx.MarketDataMap) == 0 {
		if lastErr != nil {
			if summary, ok := classifyExpectedMarketDataBlock(lastErr); ok {
				at.syncMarketDataAvailabilityIncident(true, summary, map[string]string{
					"error": strings.TrimSpace(lastErr.Error()),
				})
			}
			return lastErr
		}
		at.syncMarketDataAvailabilityIncident(true, "market data unavailable for runtime decision cycle", nil)
		return fmt.Errorf("failed to load market data for momentum strategy")
	}
	at.syncMarketDataAvailabilityIncident(false, "market data available", nil)
	return nil
}

func (at *AutoTrader) buildMomentumOnlyDecision(ctx *decision.Context) *decision.FullDecision {
	decisions := make([]decision.Decision, 0, 4)
	for _, pos := range ctx.Positions {
		closeAction := ""
		reason := ""

		if pos.UnrealizedPnLPct >= 4.5 {
			if pos.Side == "long" {
				closeAction = "close_long"
			} else {
				closeAction = "close_short"
			}
			reason = "Momentum-only exit: take-profit threshold reached"
		} else if pos.UnrealizedPnLPct <= -1.5 {
			if pos.Side == "long" {
				closeAction = "close_long"
			} else {
				closeAction = "close_short"
			}
			reason = "Momentum-only exit: stop-loss threshold reached"
		} else if data, ok := ctx.MarketDataMap[pos.Symbol]; ok {
			if pos.Side == "long" && data.CurrentMACD < 0 && data.PriceChange1h < 0 {
				closeAction = "close_long"
				reason = "Momentum-only exit: trend reversal against long"
			}
			if pos.Side == "short" && data.CurrentMACD > 0 && data.PriceChange1h > 0 {
				closeAction = "close_short"
				reason = "Momentum-only exit: trend reversal against short"
			}
		}

		if closeAction != "" {
			decisions = append(decisions, decision.Decision{
				Symbol:    pos.Symbol,
				Action:    closeAction,
				Reasoning: reason,
			})
		}
	}

	if len(decisions) == 0 && len(ctx.Positions) == 0 {
		fallback, ok := buildMomentumFallbackDecision(ctx, at.config.MomentumMinScore, at.config.FallbackPositionPct)
		if ok {
			decisions = append(decisions, fallback)
		}
	}

	if len(decisions) == 0 {
		decisions = append(decisions, decision.Decision{
			Action:    "wait",
			Reasoning: "Momentum-only strategy: no qualified setup in this cycle",
		})
	}

	return &decision.FullDecision{
		UserPrompt: "Momentum-only local strategy decision (no external AI call)",
		CoTTrace:   "Using local momentum signals and fixed risk constraints to manage entries and exits.",
		Decisions:  decisions,
		Timestamp:  time.Now(),
	}
}

// buildTradingContext Tracking tracking Target limitations limits Variable combinations strings Variables arrays Tracking MAP parameters strings mapping mapping string Maps List mapping Target configurations Mapping Variable lists permutations limit MAP limitations Maps maps Targeting limitations Limit strings
func (at *AutoTrader) buildTradingContext() (*decision.Context, error) {
	// 1. Load the current account snapshot and positions from the canonical runtime view.
	summary, positions, err := at.snapshotAccountAndPositions()
	if err != nil {
		return nil, fmt.Errorf("tracking limits permutations Maps Tracking Limit parameters array parameters: %w", err)
	}
	positionInfos := at.buildDecisionPositionInfos(positions)

	// 3. String Limits Limit Tracker Target arrays parameter Map Tracking map strings Tracking Logic Limit Target limits constraints limitations Mapping Arrays Limitations parameters strings
	// Targeting Logic tracking tracking variables String Variables array limits MAP mapping Limits Maps tracking
	// Target String Strings map tracking map combinations strings Tracking limits Limit limitation Maps Array variables Tracking MAP Mapping Tracker
	batchSize := at.config.CandidateBatchSize
	if batchSize <= 0 {
		batchSize = 20
	}
	if at.config.InstrumentType == "equity" && at.config.DataProvider == "ibkr" && batchSize > 12 {
		batchSize = 12
	}

	var (
		allSymbols    []string
		symbolSources map[string][]string
		universeErr   error
	)
	if at.config.InstrumentType == "equity" {
		allSymbols = at.activeEntryUniverseSymbols()
		symbolSources = make(map[string][]string, len(allSymbols))
		for _, symbol := range allSymbols {
			sources := []string{"configured_universe"}
			if len(at.trustedSymbolSet) > 0 {
				sources = append(sources, "trusted_symbol_filter")
			}
			symbolSources[symbol] = sources
		}
	} else {
		const universeLimit = 20000
		var mergedPool *pool.MergedCoinPool
		mergedPool, universeErr = pool.GetMergedCoinPool(universeLimit)
		if universeErr != nil {
			return nil, fmt.Errorf("variables lists Logic Mapping Tracker arrays limitations maps Array map LIMIT strings map parameter %w", universeErr)
		}
		allSymbols = append([]string(nil), mergedPool.AllSymbols...)
		symbolSources = mergedPool.SymbolSources
	}
	if len(allSymbols) == 0 {
		return nil, fmt.Errorf("candidate universe is empty")
	}

	selectedSymbols := allSymbols
	if len(allSymbols) > batchSize {
		start := at.candidateCursor % len(allSymbols)
		selectedSymbols = make([]string, 0, batchSize)
		for i := 0; i < batchSize; i++ {
			idx := (start + i) % len(allSymbols)
			selectedSymbols = append(selectedSymbols, allSymbols[idx])
		}
		at.candidateCursor = (start + batchSize) % len(allSymbols)
		log.Printf(" Candidate universe: %d symbols, analyzing rotating window of %d symbols (start index %d)",
			len(allSymbols), len(selectedSymbols), start)
	} else {
		log.Printf(" Candidate universe: %d symbols, analyzing all symbols", len(allSymbols))
	}

	// Strings Tracking Lists Array Strings limits variations Tracker limits arrays string mapping map combinations Target Strings Target limitation Target Tracking limits target configurations string Tracking Maps mapping LIMIT tracking arrays
	var candidateCoins []decision.CandidateCoin
	for _, symbol := range selectedSymbols {
		sources := symbolSources[symbol]
		candidateCoins = append(candidateCoins, decision.CandidateCoin{
			Symbol:  symbol,
			Sources: sources, // "ai500" tracking "oi_top"
		})
	}
	at.recordUniverseCycleSelection(selectedSymbols, nil, nil)

	// 5. String strings Array Tracking mapping constraints limitations limits Array Targeting variables tracking string Limitations Arrays Strings strings Map Target MAP Target Tracker Limits Variables Mapping logic arrays Limit map Array variations Map Tracking Map Object strings limits limitation constraints LIMIT arrays
	// Limitations maps Tracking Variables Tracker limitation Strings Target MAP Array variables target Variables Map Tracking Tracker tracking maps configurations Mapping Maps parameter Tracking Maps limitations tracking strings Array array variables array
	performance, err := at.decisionLogger.AnalyzePerformance(100)
	if err != nil {
		log.Printf("  Failed to analyze historical performance variables string maps Limit parameter limitation Map array map Lists Matrix Target Limits arrays Map LIMIT Tracker: %v", err)
		// limitation tracking Map Map limitations combinations Target maps limits Track Target limitation Targets map Mapping Mapping limits Map strings variables Target limits map limitations limit MAP List
		performance = nil
	}

	// 6. Limits Strings mapping maps Array Logic Maps tracking Limit List MAP Mapping parameters limitations Strings Mapping Limit Mapper limits Map Target Strings limits Array List Matrix values
	ctx := &decision.Context{
		CurrentTime:     time.Now().Format("2006-01-02 15:04:05"),
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		BTCETHLeverage:  at.config.BTCETHLeverage,  // Limit Mapper Parameter arrays Limit
		AltcoinLeverage: at.config.AltcoinLeverage, // permutations variations strings combinations Arrays
		Account:         decisionAccountInfoFromSummary(summary),
		Positions:       positionInfos,
		CandidateCoins:  candidateCoins,
		Performance:     performance, // Lists arrays Target limits strings

		Provider:              at.provider,
		InstrumentType:        at.config.InstrumentType,
		BarsAdjustment:        at.config.BarsAdjustment,
		IsReplay:              at.config.Mode == "replay",
		DataValidationOptions: at.currentDataValidationOptions(),
		DataQualityObserver:   at.observeDataQualityEvent,
	}

	return ctx, nil
}

// executeDecisionWithRecord MAP Lists lists Arrays targets Tracker Array string maps Limit map permutations Mapper targeting strings limitations arrays map Limit LIMIT Maps Tracking
func (at *AutoTrader) executeDecisionWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	var preTrade *preTradeRiskContext
	if decision.Action != "hold" && decision.Action != "wait" {
		riskCtx, err := at.evaluatePreTradeRisk(decision)
		if err != nil {
			return at.handleIBKRRuntimeError("risk_"+decision.Action, err)
		}
		preTrade = riskCtx
		at.applyRiskEvaluation(actionRecord, riskCtx.evaluation)
		logRiskEvaluation(decision.Symbol, riskCtx.evaluation)
		if riskCtx.evaluation.Outcome == risk.OutcomeReject {
			return fmt.Errorf("risk engine rejected %s %s: %s", decision.Symbol, decision.Action, riskCtx.evaluation.Summary)
		}
	}

	var err error
	switch decision.Action {
	case "open_long":
		err = at.executeOpenLongWithRecord(decision, actionRecord, preTrade)
	case "open_short":
		err = at.executeOpenShortWithRecord(decision, actionRecord, preTrade)
	case "close_long":
		err = at.executeCloseLongWithRecord(decision, actionRecord, preTrade)
	case "close_short":
		err = at.executeCloseShortWithRecord(decision, actionRecord, preTrade)
	case "hold", "wait":
		return nil
	default:
		return fmt.Errorf("variables strings MAP MAP Target Tracking limitations variables Limit configurations tracking Limit tracking strings: %s", decision.Action)
	}
	if strings.EqualFold(strings.TrimSpace(actionRecord.OrderStatus), string(execution.StatusRejected)) {
		at.observeRiskSupervisorOrderReject()
	}
	if err == nil {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(actionRecord.OrderStatus))
	switch execution.Status(status) {
	case execution.StatusFailed:
		return at.handleIBKRRuntimeError("execute_"+decision.Action, err)
	case execution.StatusBlocked, execution.StatusDuplicateSuppressed, execution.StatusRejected, execution.StatusStale, execution.StatusCancelled:
		return err
	}
	if isTrackedExecutionStatus(actionRecord.OrderStatus) {
		return err
	}
	return at.handleIBKRRuntimeError("execute_"+decision.Action, err)
}

func (at *AutoTrader) cappedEntryNotional(requested float64) float64 {
	equityCap := at.initialBalance
	available := 0.0
	if summary, err := at.GetAccountInfo(); err == nil && summary != nil {
		available = summary.AvailableBalance
		if sizingEquity := summary.DecisionSizingEquity(); sizingEquity > 0 && (equityCap <= 0 || sizingEquity < equityCap) {
			equityCap = sizingEquity
		}
	}
	return capNotional(requested, equityCap, at.initialBalance, at.config.MaxPositionPct, available)
}

// capNotional applies position-size and available-balance caps to a requested
// entry notional. Exported-via-lowercase for testability within the package.
func capNotional(requested, equityCap, initialBalance, maxPositionPct, available float64) float64 {
	notional := requested
	if notional <= 0 {
		return 0
	}

	if equityCap <= 0 {
		equityCap = initialBalance
	}
	if equityCap <= 0 {
		return notional
	}

	if maxPositionPct <= 0 {
		maxPositionPct = 0.20
	}

	maxNotional := equityCap * maxPositionPct
	if maxNotional > 0 && notional > maxNotional {
		log.Printf(" Entry notional cap applied: requested %.2f -> %.2f", notional, maxNotional)
		notional = maxNotional
	}

	if available > 0 {
		availCap := available * 0.95
		if notional > availCap {
			log.Printf(" Available-balance cap applied: requested %.2f -> %.2f", notional, availCap)
			notional = availCap
		}
	}

	return notional
}

// executeOpenLongWithRecord Strings map String mapping Map arrays Limit strings variables Mapping LIMIT MAP String string string arrays Mapper array mapping targets array Target tracking Limit values Map Mapping strings Tracker strings Target Mapping MAP
func (at *AutoTrader) executeOpenLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Open long: %s", decision.Symbol)

	//  Target variables tracker Object Map Array combinations Variables Object limits Variables Maps limitation List Tracking String map limit MAP Tracker
	positions := []map[string]interface{}{}
	var err error
	if preTrade != nil && len(preTrade.positions) > 0 {
		positions = preTrade.positions
	} else {
		positions, err = at.GetPositions()
	}
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
				return fmt.Errorf(" %s array Target MAP maps Logic Limitations Target Tracker maps MAP Arrays Strings limits String Strings arrays Mapping Maps map Arrays array %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Maps Maps parameter mapping Limits
	marketData := (*market.Data)(nil)
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}

	approvedNotional := decision.PositionSizeUSD
	quantity := 0.0
	if preTrade != nil {
		if preTrade.evaluation.ApprovedNotional > 0 {
			approvedNotional = preTrade.evaluation.ApprovedNotional
		}
		if preTrade.evaluation.ApprovedQuantity > 0 {
			quantity = preTrade.evaluation.ApprovedQuantity
		}
	}
	if approvedNotional <= 0 {
		return fmt.Errorf("invalid approved notional %.2f for %s", approvedNotional, decision.Symbol)
	}
	if quantity <= 0 && marketData.CurrentPrice > 0 {
		quantity = approvedNotional / marketData.CurrentPrice
	}
	if approvedNotional < marketData.CurrentPrice {
		return fmt.Errorf("approved notional %.2f is below one share price %.2f for %s", approvedNotional, marketData.CurrentPrice, decision.Symbol)
	}
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Mapping Map Maps arrays Target LIMIT Limit Target Strings arrays Map arrays constraints tracking Mapping logic Map tracking Target tracking targeting Target map limitations Maps combinations
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Execution accepted for %s long entry, status=%s, broker_order_id=%s, quantity=%.4f", decision.Symbol, result.Status, result.BrokerOrderID, actionRecord.Quantity)

	if executionStatusHasImmediateFill(actionRecord.OrderStatus) {
		posKey := decision.Symbol + "_long"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}
	at.handleEntryProtection(decision, actionRecord, "long", quantity)

	return nil
}

// executeOpenShortWithRecord maps String Values Map Map arrays Maps limitations Mapper Limit Variable Tracker arrays MAP
func (at *AutoTrader) executeOpenShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Open short: %s", decision.Symbol)

	//  List tracking Arrays Limit Mapping List Mapping strings limits Matrix Logic Map tracking arrays Map String Arrays Maps combinations limits maps limitations limitations tracking Limit LIMIT Tracking
	positions := []map[string]interface{}{}
	var err error
	if preTrade != nil && len(preTrade.positions) > 0 {
		positions = preTrade.positions
	} else {
		positions, err = at.GetPositions()
	}
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
				return fmt.Errorf(" %s Map limitation permutations strings string Tracker limitations String Variables %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Strings mapping Map Limit strings MAP arrays
	marketData := (*market.Data)(nil)
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}

	approvedNotional := decision.PositionSizeUSD
	quantity := 0.0
	if preTrade != nil {
		if preTrade.evaluation.ApprovedNotional > 0 {
			approvedNotional = preTrade.evaluation.ApprovedNotional
		}
		if preTrade.evaluation.ApprovedQuantity > 0 {
			quantity = preTrade.evaluation.ApprovedQuantity
		}
	}
	if approvedNotional <= 0 {
		return fmt.Errorf("invalid approved notional %.2f for %s", approvedNotional, decision.Symbol)
	}
	if quantity <= 0 && marketData.CurrentPrice > 0 {
		quantity = approvedNotional / marketData.CurrentPrice
	}
	if approvedNotional < marketData.CurrentPrice {
		return fmt.Errorf("approved notional %.2f is below one share price %.2f for %s", approvedNotional, marketData.CurrentPrice, decision.Symbol)
	}
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Map combinations Array Mapper Limit
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Execution accepted for %s short entry, status=%s, broker_order_id=%s, quantity=%.4f", decision.Symbol, result.Status, result.BrokerOrderID, actionRecord.Quantity)

	if executionStatusHasImmediateFill(actionRecord.OrderStatus) {
		posKey := decision.Symbol + "_short"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}
	at.handleEntryProtection(decision, actionRecord, "short", quantity)

	return nil
}

// executeCloseLongWithRecord Maps Matrix tracking limit strings bounds tracking limits Limitation Maps Mapping variables parameters Arrays limit Target Tracker map Mapper Target Tracker limits bounds array tracking constraints
func (at *AutoTrader) executeCloseLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Close long: %s", decision.Symbol)

	// array List Parameter maps parameters strings String Tracker Map Array Mapper strings
	marketData := (*market.Data)(nil)
	var err error
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}
	actionRecord.Price = marketData.CurrentPrice

	// Variable Mapping Maps variables MAP Logic limitation limits
	quantity := 0.0
	if preTrade != nil && preTrade.evaluation.ApprovedQuantity > 0 {
		quantity = preTrade.evaluation.ApprovedQuantity
	}
	actionRecord.Quantity = quantity
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Close long execution accepted for %s, status=%s, broker_order_id=%s", decision.Symbol, result.Status, result.BrokerOrderID)
	return nil
}

// executeCloseShortWithRecord variables Parameters limitations combinations Mapping Maps strings Maps String Mapping mapping Map LIMIT configurations Mapping limitations tracking Variables Logic List map Target Limit LIMIT tracking arrays Logic Mapping Arrays array List constraints Tracking map tracking parameters variables combinations Limit limitations Target LIMIT Parameter Variable
func (at *AutoTrader) executeCloseShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Close short: %s", decision.Symbol)

	// parameter Tracker limits limits lists maps limits Limits tracking permutations Object limits MAP LIMIT Mapping Limit
	marketData := (*market.Data)(nil)
	var err error
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}
	actionRecord.Price = marketData.CurrentPrice

	// maps configurations variables String bounds mappings variables Map Mapping MAP Map mapping Tracking Target Mapping Array logic combinations Tracker arrays Strings Array
	quantity := 0.0
	if preTrade != nil && preTrade.evaluation.ApprovedQuantity > 0 {
		quantity = preTrade.evaluation.ApprovedQuantity
	}
	actionRecord.Quantity = quantity
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Close short execution accepted for %s, status=%s, broker_order_id=%s", decision.Symbol, result.Status, result.BrokerOrderID)
	return nil
}

// GetID variables Parameter Object limits maps MAP MAP Matrix Logic constraints Map string Object configurations List
func (at *AutoTrader) GetID() string {
	return at.id
}

// GetName tracking Maps Parameter Object tracking LIMIT Tracker constraints boundaries loops
func (at *AutoTrader) GetName() string {
	return at.name
}

// GetAIModel target parameters variables Tracking Target maps Limit Arrays Object limitation string Map Array array Strings mapping parameters Map mapping Limits limits Limits tracking mapping Map Map Tracking combinations Map Mapping Mapper maps Tracker arrays limits tracking Variables limitation Limits Arrays map map Tracking arrays Tracker parameters tracking Variables tracking
func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

// GetDecisionLogger Map mapping Maps Lists string LIMIT limitations maps configurations maps logic Map strings limits Limit LIMIT MAP Mapping MAP Mapper limits Maps tracking Strings List Object limit Array Mapper limits tracking Variables Tracker maps values Limit arrays lists String Tracking variables Mapping Arrays array List List tracking Matrix Limits strings Map Array logic combinations
func (at *AutoTrader) GetDecisionLogger() *logger.DecisionLogger {
	return at.decisionLogger
}

// GetStatus Object Map parameters limit Targeting strings LIMIT parameters Object Limit Target Strings String Limits Lists Tracker tracking Variable tracking Tracker Arrays mappings Variables
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

// GetProvider returns the underlying BarsProvider
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

// GetPositions Mapping variables maps List arrays limits Parameter string strings Logic loops MAP combinations Arrays target Map limitation Tracking array variables MAP Array maps tracking string Strings Lists array Maps variations Tracking limits limit limits loops Mapper mapping maps Tracker maps Object
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	_, positions, err := at.snapshotAccountAndPositions()
	if err != nil {
		return nil, err
	}
	return positions, nil
}

func (at *AutoTrader) buildDemoPositions() []map[string]interface{} {
	if at.demoPositionCount <= 0 {
		return []map[string]interface{}{}
	}

	seed := at.demoSnapshotSeed
	if seed == 0 {
		seed = int64(at.callCount + 1)
	}
	r := rand.New(rand.NewSource(seed))

	symbols := []string{"AAPL", "MSFT", "NVDA", "AMZN", "GOOGL", "META", "TSLA", "SHOP", "RY", "TD", "BNS", "ENB"}
	positions := make([]map[string]interface{}, 0, at.demoPositionCount)
	totalMarginBudget := at.demoEquity * (at.demoMarginUsedPct / 100.0)
	if totalMarginBudget < 0 {
		totalMarginBudget = 0
	}

	for i := 0; i < at.demoPositionCount; i++ {
		symbol := symbols[(at.callCount+i)%len(symbols)]
		base := demoSymbolBasePrice(symbol)
		leverage := 2 + r.Intn(4) // 2x..5x
		side := "long"
		if r.Float64() > 0.6 {
			side = "short"
		}

		entryPrice := base * (0.97 + r.Float64()*0.06)
		drift := (r.Float64() - 0.5) * 0.04 // +/-2%
		markPrice := entryPrice * (1.0 + drift)
		allocatedMargin := totalMarginBudget / float64(at.demoPositionCount)
		if allocatedMargin <= 0 {
			allocatedMargin = at.demoEquity * 0.02
		}

		quantity := (allocatedMargin * float64(leverage)) / entryPrice
		unrealized := (markPrice - entryPrice) * quantity
		if side == "short" {
			unrealized = -unrealized
		}

		liqPrice := entryPrice * (1.0 - 0.20/float64(leverage))
		if side == "short" {
			liqPrice = entryPrice * (1.0 + 0.20/float64(leverage))
		}

		marginUsed := (quantity * markPrice) / float64(leverage)
		entryMarginUsed := (quantity * entryPrice) / float64(leverage)
		unrealizedPct := 0.0
		if entryMarginUsed > 0 {
			unrealizedPct = (unrealized / entryMarginUsed) * 100.0
		}

		positions = append(positions, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealized,
			"unrealized_pnl_pct": unrealizedPct,
			"liquidation_price":  liqPrice,
			"margin_used":        marginUsed,
		})
	}

	return positions
}

func demoSymbolBasePrice(symbol string) float64 {
	switch symbol {
	case "AAPL":
		return 195
	case "MSFT":
		return 420
	case "NVDA":
		return 880
	case "AMZN":
		return 185
	case "GOOGL":
		return 165
	case "META":
		return 505
	case "TSLA":
		return 220
	case "SHOP":
		return 95
	case "RY":
		return 128
	case "TD":
		return 83
	case "BNS":
		return 70
	case "ENB":
		return 51
	default:
		return 100
	}
}

func loadSymbolSetFromFile(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		})
		for _, token := range parts {
			symbol := strings.ToUpper(strings.Trim(strings.TrimSpace(token), "\"'"))
			if symbol != "" {
				set[symbol] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no symbols found")
	}
	return set, nil
}

func filterTradableEquitySymbols(symbols []string, trusted map[string]struct{}) []string {
	filtered := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if !isLikelyTradableEquitySymbol(symbol) {
			continue
		}
		if len(trusted) > 0 {
			if _, ok := trusted[symbol]; !ok {
				continue
			}
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		filtered = append(filtered, symbol)
	}
	return filtered
}

func isLikelyTradableEquitySymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	if strings.Contains(symbol, "/") {
		return false
	}
	if strings.HasSuffix(symbol, ".WS") || strings.HasSuffix(symbol, ".WT") || strings.HasSuffix(symbol, ".U") || strings.HasSuffix(symbol, ".R") {
		return false
	}
	if strings.HasSuffix(symbol, "WS") || strings.HasSuffix(symbol, "WT") || strings.HasSuffix(symbol, "RT") {
		return false
	}

	dotCount := strings.Count(symbol, ".")
	if dotCount > 1 {
		return false
	}
	if dotCount == 1 {
		parts := strings.Split(symbol, ".")
		if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 5 || len(parts[1]) != 1 {
			return false
		}
	}

	base := strings.ReplaceAll(symbol, ".", "")
	if len(base) == 0 || len(base) > 5 {
		return false
	}
	if len(base) == 5 {
		last := base[len(base)-1]
		if last == 'W' || last == 'U' || last == 'R' {
			return false
		}
	}

	for _, ch := range symbol {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func (at *AutoTrader) maybeApplyEquityMomentumFallback(ctx *decision.Context, fullDecision *decision.FullDecision) {
	if fullDecision == nil || ctx == nil {
		return
	}
	if at.config.InstrumentType != "equity" || at.config.StrategyMode != "momentum_fallback" {
		return
	}
	if len(ctx.Positions) > 0 {
		return
	}
	if !allPassiveDecisions(fullDecision.Decisions) {
		return
	}

	fallback, ok := buildMomentumFallbackDecision(ctx, at.config.MomentumMinScore, at.config.FallbackPositionPct)
	if !ok {
		return
	}

	log.Printf(" Momentum fallback generated %s on %s | notional=%.2f", fallback.Action, fallback.Symbol, fallback.PositionSizeUSD)
	fullDecision.Decisions = []decision.Decision{fallback}
}

func allPassiveDecisions(decisions []decision.Decision) bool {
	if len(decisions) == 0 {
		return true
	}
	for _, d := range decisions {
		if d.Action != "wait" && d.Action != "hold" {
			return false
		}
	}
	return true
}

type momentumSignal struct {
	Symbol     string
	Price      float64
	Score      float64
	TrendScore float64
	MACD       float64
	RSI7       float64
	ShortBias  bool
}

func selectBestMomentumSignal(ctx *decision.Context, minScore float64) (momentumSignal, bool) {
	if minScore <= 0 {
		minScore = 1.25
	}

	best := momentumSignal{}
	found := false
	for symbol, data := range ctx.MarketDataMap {
		if data == nil || data.CurrentPrice <= 0 {
			continue
		}

		trend := data.PriceChange1h*0.55 + data.PriceChange4h*0.45
		macdBias := 0.0
		if data.CurrentMACD > 0 {
			macdBias = 0.8
		} else if data.CurrentMACD < 0 {
			macdBias = -0.8
		}
		directionScore := trend + macdBias
		if math.Abs(directionScore) < 0.4 {
			continue
		}

		rsiDistance := math.Abs(data.CurrentRSI7-50.0) / 50.0
		quality := 1.0 - math.Min(1.0, rsiDistance)
		score := math.Abs(directionScore) + (quality * 0.6)
		if score < minScore {
			continue
		}

		if !found || score > best.Score {
			found = true
			best = momentumSignal{
				Symbol:     symbol,
				Price:      data.CurrentPrice,
				Score:      score,
				TrendScore: trend,
				MACD:       data.CurrentMACD,
				RSI7:       data.CurrentRSI7,
				ShortBias:  directionScore < 0,
			}
		}
	}

	return best, found
}

func buildMomentumFallbackDecision(ctx *decision.Context, minScore, positionPct float64) (decision.Decision, bool) {
	candidate, ok := selectBestMomentumSignal(ctx, minScore)
	if !ok {
		return decision.Decision{}, false
	}

	if positionPct <= 0 || positionPct > 0.20 {
		positionPct = 0.10
	}

	decisionEquity := ctx.Account.DecisionSizingEquity()
	notional := decisionEquity * positionPct
	maxPerTrade := decisionEquity * 0.20
	if notional > maxPerTrade {
		notional = maxPerTrade
	}
	if ctx.Account.AvailableBalance > 0 && notional > ctx.Account.AvailableBalance*0.95 {
		notional = ctx.Account.AvailableBalance * 0.95
	}
	if notional < 250 {
		return decision.Decision{}, false
	}

	riskPct := 0.015
	rewardPct := 0.045
	action := "open_long"
	stopLoss := candidate.Price * (1 - riskPct)
	takeProfit := candidate.Price * (1 + rewardPct)
	if candidate.ShortBias {
		action = "open_short"
		stopLoss = candidate.Price * (1 + riskPct)
		takeProfit = candidate.Price * (1 - rewardPct)
	}

	confidence := int(math.Round(70 + candidate.Score*6))
	if confidence < 75 {
		confidence = 75
	}
	if confidence > 95 {
		confidence = 95
	}

	return decision.Decision{
		Symbol:          candidate.Symbol,
		Action:          action,
		Leverage:        1,
		PositionSizeUSD: notional,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		Confidence:      confidence,
		Reasoning:       fmt.Sprintf("Momentum fallback: score=%.2f trend=%.2f rsi7=%.1f macd=%.4f", candidate.Score, candidate.TrendScore, candidate.RSI7, candidate.MACD),
	}, true
}

// sortDecisionsByPriority limit mapping mapping Array Maps Variable Lists lists Tracking limits Limit loops Limit Strings Tracker variations Target MAP mapping Tracking
// parameters List limitations arrays Strings Parameters array MAP tracking limit string arrays mapping loops Limit tracking mapping strings Map Parameter String map configurations Tracking Limits arrays
func sortDecisionsByPriority(decisions []decision.Decision) []decision.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// LIMIT String Tracker map lists tracking map
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short":
			return 1 // Strings Tracking List MAP mapping Targeting Limit string loops tracking logic Object strings limitations Variables Tracker mapping limits List combinations map Map
		case "open_long", "open_short":
			return 2 // Target lists maps Tracker configurations String Target limits Tracking Mapper string Maps Tracker Tracker tracking array list MAP
		case "hold", "wait":
			return 3 // arrays limitations arrays Matrix strings List Map tracking Targeting variables maps Limit Strings
		default:
			return 999 // Arrays Array map limits String List Arrays logic mapping
		}
	}

	// Target Decision Strings array limit limitation strings parameter Lists List Target limitation Tracker Array lists Array Map strings Target target Tracker Tracking Tracking Map Limits parameters Strings Parameters tracking variables string Map mapping loops string Maps Limit loops variations Tracking arrays Tracker Limit variations Tracking List Map variables Limit arrays strings mapping Tracker strings Tracking Limitation
	sorted := make([]decision.Decision, len(decisions))
	copy(sorted, decisions)

	// Arrays List Lists combinations Arrays Arrays String List Map MAP Tracking Strings limitations limitations Logic Tracker parameters parameters Limit Values limit array tracking variables strings limits Limit MAP configurations Logic Limit Matrix Array
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}
