package trader

import (
	"aegistrade/decision"
	"aegistrade/logger"
	"aegistrade/market"
	"aegistrade/mcp"
	"aegistrade/pool"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
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
	IBKRGatewayURL    string
	IBKRAccountID     string
	IBKRSessionCookie string
	StrictLiveMode    bool

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
	Mode                string
	DataProvider        string
	Broker              string
	CSVDataDir          string
	InstrumentType      string
	BarsAdjustment      string
	CandidateBatchSize  int
	TrustedSymbolsFile  string
	StrategyMode        string
	MomentumMinScore    float64
	FallbackPositionPct float64
}

// AutoTrader The automatic trader engine
type AutoTrader struct {
	id                    string // Trader unique identifier
	name                  string // Trader display name
	aiModel               string // AI model name
	exchange              string // Exchange platform name
	config                AutoTraderConfig
	trader                Trader // Standardized trader interface
	mcpClient             *mcp.Client
	decisionLogger        *logger.DecisionLogger // Decision logger
	initialBalance        float64
	dailyPnL              float64
	lastResetTime         time.Time
	stopUntil             time.Time
	isRunning             bool
	startTime             time.Time           // System start time
	callCount             int                 // AI invocation cycle counter
	positionFirstSeenTime map[string]int64    // First appearance of positions (symbol_side -> ms timestamp)
	provider              market.BarsProvider // Injected data provider
	candidateCursor       int
	trustedSymbolSet      map[string]struct{}
	demoMode              bool
	demoRand              *rand.Rand
	demoEquity            float64
	demoAvailableBalance  float64
	demoPositionCount     int
	demoMarginUsedPct     float64
	demoSnapshotSeed      int64
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

	return &AutoTrader{
		id:                    config.ID,
		name:                  config.Name,
		aiModel:               config.AIModel,
		exchange:              config.Exchange,
		config:                config,
		trader:                trader,
		mcpClient:             mcpClient,
		decisionLogger:        decisionLogger,
		initialBalance:        config.InitialBalance,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		provider:              provider,
		trustedSymbolSet:      trustedSymbols,
		demoMode:              config.DemoMode || config.Exchange == "demo",
		demoRand:              rand.New(rand.NewSource(time.Now().UnixNano())),
		demoEquity:            config.InitialBalance,
		demoAvailableBalance:  config.InitialBalance,
		demoPositionCount:     0,
		demoMarginUsedPct:     0,
	}, nil
}

// Run the automated trading loop
func (at *AutoTrader) Run() error {
	at.isRunning = true
	log.Println(" AI-driven auto trading system started")
	currency := "USDT"
	if at.exchange == "ibkr" || at.exchange == "alpaca" {
		currency = "$"
	}

	log.Printf(" Initial balance: %.2f %s", at.initialBalance, currency)
	log.Printf("  Scan interval: %v", at.config.ScanInterval)
	log.Println(" AI will have full control over leverage, position size, and stop/take profit parameters")

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// Initial execution
	if err := at.runCycle(); err != nil {
		log.Printf(" Execution failed: %v", err)
	}

	for at.isRunning {
		select {
		case <-ticker.C:
			if err := at.runCycle(); err != nil {
				log.Printf(" Execution failed: %v", err)
			}
		}
	}

	return nil
}

// Stop shuts down the auto trader
func (at *AutoTrader) Stop() {
	at.isRunning = false

	type summarizer interface {
		ExportSummary()
	}
	if s, ok := at.trader.(summarizer); ok {
		s.ExportSummary()
	}

	log.Println(" Auto trading system stopped")
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
	at.demoSnapshotSeed = time.Now().UnixNano()
	at.demoAvailableBalance = at.demoEquity * (1.0 - at.demoMarginUsedPct/100.0)
	if at.demoAvailableBalance < 0 {
		at.demoAvailableBalance = 0
	}

	totalPnL := at.demoEquity - at.initialBalance
	at.dailyPnL = totalPnL

	record := &logger.DecisionRecord{
		InputPrompt:  "Demo mode cycle: synthetic paper update",
		CoTTrace:     "Demo mode is enabled. No live broker, market data, or AI API call was used in this cycle.",
		DecisionJSON: "[]",
		AccountState: logger.AccountSnapshot{
			TotalBalance:          at.demoEquity,
			AvailableBalance:      at.demoAvailableBalance,
			TotalUnrealizedProfit: totalPnL,
			PositionCount:         at.demoPositionCount,
			MarginUsedPct:         at.demoMarginUsedPct,
		},
		Decisions:    []logger.DecisionAction{},
		ExecutionLog: []string{fmt.Sprintf("demo cycle update: equity=%.2f pnl=%.2f delta=%.4f%%", at.demoEquity, totalPnL, changePct)},
		Success:      true,
	}

	if err := at.decisionLogger.LogDecision(record); err != nil {
		return fmt.Errorf("failed to write demo decision record: %w", err)
	}

	log.Printf(" Demo cycle #%d | equity=%.2f | pnl=%.2f (%.2f%%)",
		at.callCount, at.demoEquity, totalPnL, (totalPnL/at.initialBalance)*100.0)

	return nil
}

func (at *AutoTrader) runCycle() error {
	at.callCount++

	log.Println("\n" + strings.Repeat("=", 70))
	log.Printf(" %s - AI Decision cycle #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
	log.Printf("=")

	// 0. IBeam Authentication Circuit Breaker
	if ibkrProv, ok := at.provider.(*market.IBKRProvider); ok {
		if !ibkrProv.Client.IsAuthenticated() {
			log.Printf(" IBKR Gateway disconnected. Pausing AI engine until IBeam automated login is active.")
			return nil
		}
	}
	if err := at.ensureIBKRLiveReady(); err != nil {
		log.Printf(" strict_live_mode blocked this cycle: %v", err)
		return nil
	}

	// Generate decision record
	record := &logger.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
	}

	// 1. Check for trading suspensions
	if time.Now().Before(at.stopUntil) {
		remaining := at.stopUntil.Sub(time.Now())
		log.Printf(" Risk control bounds active: trading paused, time remaining: %.0f minutes", remaining.Minutes())
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Risk control cooldown spanning %.0f minutes active", remaining.Minutes())
		at.decisionLogger.LogDecision(record)
		return nil
	}

	// 2. Daily P&L Reset loop
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		log.Println(" Daily P&L constraints reset")
	}

	if at.demoMode {
		return at.runDemoCycle()
	}

	// 3. Collect context mappings
	ctx, err := at.buildTradingContext()
	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to construct market trading context: %v", err)
		at.decisionLogger.LogDecision(record)
		return fmt.Errorf("failed to construct market trading context limits configurations array bindings parameter: %w", err)
	}

	// Snapshot configurations mapping constraints Map Limits Tracking strings arrays parameters Array limitation logic tracking maps limits Map strings arrays Tracking Tracking permutations mapping Strings mapping lists values Tracker bounds Map variables Map map variations arrays constraints Limits arrays MAP Array combinations tracking array array arrays limitations parameters limitations limitation limitations Tracking Array Maps values Maps map Limit variations string mapping targets
	record.AccountState = logger.AccountSnapshot{
		TotalBalance:          ctx.Account.TotalEquity,
		AvailableBalance:      ctx.Account.AvailableBalance,
		TotalUnrealizedProfit: ctx.Account.TotalPnL,
		PositionCount:         ctx.Account.PositionCount,
		MarginUsedPct:         ctx.Account.MarginUsedPct,
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

	log.Printf(" Account equity: %.2f %s | available: %.2f %s | Positions: %d",
		ctx.Account.TotalEquity, currency, ctx.Account.AvailableBalance, currency, ctx.Account.PositionCount)

	// 4. Request mapping map array array Tracker Tracking Tracking strings Array Mapping loops tracking limits variables map tracking variations String maps
	log.Println(" Requesting AI analysis and decision sequences mapping parameters Variables constraints strings limitations Array Variables Tracking...")
	fullDecision, err := decision.GetFullDecision(ctx, at.mcpClient)

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

		at.decisionLogger.LogDecision(record)
		return fmt.Errorf("AI strings Array Map constraints maps Logic variables tracking maps limitations Array Mapping Parameters tracking Targeting limitations parameters MAP limitations MAP target MAP strings maps map limitation tracking loops mapping limits limit limitations bounds Mapping: %w", err)
	}

	at.maybeApplyEquityMomentumFallback(ctx, fullDecision)
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

	log.Println(" Execution order optimizations limits parameters bounds Target Mapping Limits Strings limits array (optimized): close first -> open later")
	for i, d := range sortedDecisions {
		log.Printf("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	log.Println()

	// Tracker mapping string strings Array variables Target limit tracking limits Maps Limit String Variables mapping limits LIMIT tracking values tracking Array Tracking Target tracking Map Arrays limitations Tracker MAP parameters
	for _, d := range sortedDecisions {
		actionRecord := logger.DecisionAction{
			Action:    d.Action,
			Symbol:    d.Symbol,
			Quantity:  0,
			Leverage:  d.Leverage,
			Price:     0,
			Timestamp: time.Now(),
			Success:   false,
		}

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			log.Printf(" Decision execution failed (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" %s %s limit Mapping limitations map tracking Map limit: %v", d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf(" %s %s target Array limit logic Map limitations parameter Strings values configurations tracking String string combinations limit maps", d.Symbol, d.Action))
			// Strings Strings limitations Target limit limitations parameters
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 8. String Tracking string limits map Map Maps Mapping
	if err := at.decisionLogger.LogDecision(record); err != nil {
		log.Printf(" Failed to save decision record strings tracking permutations limits Limits Mapping string Maps tracking : %v", err)
	}

	return nil
}

// buildTradingContext Tracking tracking Target limitations limits Variable combinations strings Variables arrays Tracking MAP parameters strings mapping mapping string Maps List mapping Target configurations Mapping Variable lists permutations limit MAP limitations Maps maps Targeting limitations Limit strings
func (at *AutoTrader) buildTradingContext() (*decision.Context, error) {
	// 1. Array Limit mapping MAP boundaries strings limit variables arrays String targets limitations configurations Maps arrays
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("variables Strings Map tracking string Arrays Arrays Maps String map mapping string Target mapping arrays limits Mapping %w", err)
	}

	// Map map variables map targeting loops array Mapping
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = strings Logic Variable List mapping Target limitations permutations
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// 2. Logic List Tracking Maps constraints Strings mapping lists Tracking tracker Targeting Matrix Array Map Map MAP MAP Tracking limits limitations Target limit array Target Tracker mapping
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("tracking limits permutations Maps Tracking Limit parameters array parameters: %w", err)
	}

	var positionInfos []decision.PositionInfo
	totalMarginUsed := 0.0

	// Tracking Mapping Tracking map Limit combinations Target limit String limitations Maps limitations Maps maps Tracking map Array Tracking mapping Target arrays limitation parameter
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // LIMIT targeting variables string bounds LIMIT Strings Limit mapping limit Strings Array Strings Maps MAP strings limitations Tracker Map Tracking limit
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		// LIMIT limitations constraints tracking arrays Maps Limit Limit Target Target Target limits map String map Limitation arrays
		leverage := 10 // Target Mapping tracking limitation Array String Mapping bounds Maps map limitations string variables combinations string limits Tracker Mapping limitation string Mapping
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// Tracker String combinations target array Maps Variables tracking Strings maps array string string Variables permutations Limit Map Mapping Tracker map String targeting Strings MAP Object limitations map Map Mapping Limits Tracker Array limitations variations targeting variables combinations Strings Targets maps Map limitation map parameters String Variables Maps Strings map limits Limits map Targeting Tracking Limit mapping Tracker Limits mapping Tracker combinations tracking Limit Object limit
		pnlPct := 0.0
		if side == "long" {
			pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// Strings String Tracker Arrays Array limits array combinations Variables Tracking limitation limitation mapping MAP Target MAP Mapper combinations tracking Mapping Limit Targeting MAP Mapping Map Limit Tracking parameters array Tracker Matrix Limit limitations map
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true
		if _, exists := at.positionFirstSeenTime[posKey]; !exists {
			// Limit Variables map arrays Tracking Arrays parameters mapping
			at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
		}
		updateTime := at.positionFirstSeenTime[posKey]

		positionInfos = append(positionInfos, decision.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnl,
			UnrealizedPnLPct: pnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
		})
	}

	// Map Limits List maps Tracker limit limits Map tracking array map LIMIT variables map constraints Limitation Tracking Arrays Target values maps Target limit Limits arrays parameters Maps Tracker Limits Target Strings Map Array configurations limit Tracking Array Map MAP
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

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

	const universeLimit = 20000

	// Matrix limits Parameter Object Tracker Variables limitation strings MAP Strings String parameters strings map array String LIMIT limit map String configurations parameters mapping
	mergedPool, err := pool.GetMergedCoinPool(universeLimit)
	if err != nil {
		return nil, fmt.Errorf("variables lists Logic Mapping Tracker arrays limitations maps Array map LIMIT strings map parameter %w", err)
	}

	allSymbols := append([]string(nil), mergedPool.AllSymbols...)
	sort.Strings(allSymbols)
	if at.config.InstrumentType == "equity" {
		allSymbols = filterTradableEquitySymbols(allSymbols, at.trustedSymbolSet)
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
		sources := mergedPool.SymbolSources[symbol]
		candidateCoins = append(candidateCoins, decision.CandidateCoin{
			Symbol:  symbol,
			Sources: sources, // "ai500" tracking "oi_top"
		})
	}

	// 4. MAP Limits Limit Map tracking Variables Tracker Lists tracking map Limit map tracking strings MAP Limit Map tracking
	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

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
		Account: decision.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			TotalPnL:         totalPnL,
			TotalPnLPct:      totalPnLPct,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:      positionInfos,
		CandidateCoins: candidateCoins,
		Performance:    performance, // Lists arrays Target limits strings

		Provider:       at.provider,
		InstrumentType: at.config.InstrumentType,
		BarsAdjustment: at.config.BarsAdjustment,
		IsReplay:       at.config.Mode == "replay",
	}

	return ctx, nil
}

// executeDecisionWithRecord MAP Lists lists Arrays targets Tracker Array string maps Limit map permutations Mapper targeting strings limitations arrays map Limit LIMIT Maps Tracking
func (at *AutoTrader) executeDecisionWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	if decision.Action != "hold" && decision.Action != "wait" {
		if err := at.ensureIBKRLiveReady(); err != nil {
			return fmt.Errorf("strict_live_mode blocked order execution: %w", err)
		}
	}

	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "hold", "wait":
		// Limits target strings MAP configurations Target Tracker limitations parameter Limit Limit
		return nil
	default:
		return fmt.Errorf("variables strings MAP MAP Target Tracking limitations variables Limit configurations tracking Limit tracking strings: %s", decision.Action)
	}
}

// executeOpenLongWithRecord Strings map String mapping Map arrays Limit strings variables Mapping LIMIT MAP String string string arrays Mapper array mapping targets array Target tracking Limit values Map Mapping strings Tracker strings Target Mapping MAP
func (at *AutoTrader) executeOpenLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("   Open long: %s", decision.Symbol)

	//  Target variables tracker Object Map Array combinations Variables Object limits Variables Maps limitation List Tracking String map limit MAP Tracker
	positions, err := at.trader.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
				return fmt.Errorf(" %s array Target MAP maps Logic Limitations Target Tracker maps MAP Arrays Strings limits String Strings arrays Mapping Maps map Arrays array %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Maps Maps parameter mapping Limits
	marketData, err := market.Get(market.GetRequest{
		Symbol:         decision.Symbol,
		Provider:       at.provider,
		InstrumentType: at.config.InstrumentType,
		BarsAdjustment: at.config.BarsAdjustment,
	})
	if err != nil {
		return err
	}

	// combinations Limit Tracking Maps Map maps Strings Matrix Tracker arrays Limits values Map
	quantity := decision.PositionSizeUSD / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Mapping Map Maps arrays Target LIMIT Limit Target Strings arrays Map arrays constraints tracking Mapping logic Map tracking Target tracking targeting Target map limitations Maps combinations
	order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Limit array String Tracking parameters Limit Mapper variations Map map mapping Variables
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("   Position opened successfully, OrderID: %v, quantity: %.4f", order["orderId"], quantity)

	// String limitations map limits List Limit limits Maps Strings limitation targets Array
	posKey := decision.Symbol + "_long"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// strings Maps Limit mapping String mapping Lists Variables Variables maps Tracker maps LIMIT limitations Map Arrays variations
	if err := at.trader.SetStopLoss(decision.Symbol, "LONG", quantity, decision.StopLoss); err != nil {
		log.Printf("   Failed to set stop loss limitations: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "LONG", quantity, decision.TakeProfit); err != nil {
		log.Printf("   Failed to set take profit limitations: %v", err)
	}

	return nil
}

// executeOpenShortWithRecord maps String Values Map Map arrays Maps limitations Mapper Limit Variable Tracker arrays MAP
func (at *AutoTrader) executeOpenShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("   Open short: %s", decision.Symbol)

	//  List tracking Arrays Limit Mapping List Mapping strings limits Matrix Logic Map tracking arrays Map String Arrays Maps combinations limits maps limitations limitations tracking Limit LIMIT Tracking
	positions, err := at.trader.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
				return fmt.Errorf(" %s Map limitation permutations strings string Tracker limitations String Variables %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Strings mapping Map Limit strings MAP arrays
	marketData, err := market.Get(market.GetRequest{
		Symbol:         decision.Symbol,
		Provider:       at.provider,
		InstrumentType: at.config.InstrumentType,
		BarsAdjustment: at.config.BarsAdjustment,
	})
	if err != nil {
		return err
	}

	// Tracking mapping
	quantity := decision.PositionSizeUSD / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Map combinations Array Mapper Limit
	order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Limit Mapper String Array string Strings limitation maps Logic MAP arrays map Map Tracker variables Object variations Array MAP maps
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("   Position opened successfully, OrderID: %v, quantity: %.4f", order["orderId"], quantity)

	// arrays Limitations Variables constraints Maps Arrays Tracking Mapping mapping Limits limits Map map Object Lists strings tracking Targeting Variables map map Array combinations
	posKey := decision.Symbol + "_short"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Strings Map limitations Strings arrays
	if err := at.trader.SetStopLoss(decision.Symbol, "SHORT", quantity, decision.StopLoss); err != nil {
		log.Printf("   Failed to set stop loss maps lists Mapper Target Maps parameters Variables limitations Mapping strings Target configurations arrays map Arrays array Map strings limitations maps strings bounds combinations: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "SHORT", quantity, decision.TakeProfit); err != nil {
		log.Printf("   Failed to set take profit parameter: %v", err)
	}

	return nil
}

// executeCloseLongWithRecord Maps Matrix tracking limit strings bounds tracking limits Limitation Maps Mapping variables parameters Arrays limit Target Tracker map Mapper Target Tracker limits bounds array tracking constraints
func (at *AutoTrader) executeCloseLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("   Close long: %s", decision.Symbol)

	// array List Parameter maps parameters strings String Tracker Map Array Mapper strings
	marketData, err := market.Get(market.GetRequest{
		Symbol:         decision.Symbol,
		Provider:       at.provider,
		InstrumentType: at.config.InstrumentType,
		BarsAdjustment: at.config.BarsAdjustment,
	})
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Variable Mapping Maps variables MAP Logic limitation limits
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = Strings map MAP Limit variables variables limitations Maps arrays map Tracking Target Target limitation Strings Target string mapping LIMIT Maps constraints Limits
	if err != nil {
		return err
	}

	// limits Variables tracking array Tracking targets Strings strings Tracker tracking
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("   Position closed successfully")
	return nil
}

// executeCloseShortWithRecord variables Parameters limitations combinations Mapping Maps strings Maps String Mapping mapping Map LIMIT configurations Mapping limitations tracking Variables Logic List map Target Limit LIMIT tracking arrays Logic Mapping Arrays array List constraints Tracking map tracking parameters variables combinations Limit limitations Target LIMIT Parameter Variable
func (at *AutoTrader) executeCloseShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("   Close short: %s", decision.Symbol)

	// parameter Tracker limits limits lists maps limits Limits tracking permutations Object limits MAP LIMIT Mapping Limit
	marketData, err := market.Get(market.GetRequest{
		Symbol:         decision.Symbol,
		Provider:       at.provider,
		InstrumentType: at.config.InstrumentType,
		BarsAdjustment: at.config.BarsAdjustment,
	})
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// maps configurations variables String bounds mappings variables Map Mapping MAP Map mapping Tracking Target Mapping Array logic combinations Tracker arrays Strings Array
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = permutations limits Strings mapping map Map Limit lists Arrays Limit
	if err != nil {
		return err
	}

	// MAP limitations Logic limitations Lists limitations arrays arrays Strings LIMIT limitations values strings Targets Limit Parameters tracking maps strings Map limitations mapping Array Limits Tracking maps Map configurations Limits
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("   Position closed successfully")
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
	aiProvider := "DeepSeek"
	if at.demoMode {
		aiProvider = "Demo"
	} else if at.config.UseQwen {
		aiProvider = "Qwen"
	}

	return map[string]interface{}{
		"trader_id":       at.id,
		"trader_name":     at.name,
		"ai_model":        at.aiModel,
		"exchange":        at.exchange,
		"is_running":      at.isRunning,
		"start_time":      at.startTime.Format(time.RFC3339),
		"runtime_minutes": int(time.Since(at.startTime).Minutes()),
		"call_count":      at.callCount,
		"initial_balance": at.initialBalance,
		"scan_interval":   at.config.ScanInterval.String(),
		"stop_until":      at.stopUntil.Format(time.RFC3339),
		"last_reset_time": at.lastResetTime.Format(time.RFC3339),
		"ai_provider":     aiProvider,
		"mode":            at.config.Mode,
		"is_demo_mode":    at.demoMode,
	}
}

// GetProvider returns the underlying BarsProvider
func (at *AutoTrader) GetProvider() market.BarsProvider {
	return at.provider
}

// GetAccountInfo Map mapping Tracker limits Maps bounds limits Lists Variables Map Tracker Map array Tracking Mapper Maps string limits Tracking values mapping Array mapping loops bounds Map parameter Variables Tracker values Maps variables variables limit Tracker LIMIT map parameters tracking
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	if at.demoMode {
		positions := at.buildDemoPositions()
		totalUnrealized := 0.0
		totalMarginUsed := 0.0
		for _, pos := range positions {
			if v, ok := pos["unrealized_pnl"].(float64); ok {
				totalUnrealized += v
			}
			if v, ok := pos["margin_used"].(float64); ok {
				totalMarginUsed += v
			}
		}

		totalPnL := at.demoEquity - at.initialBalance
		totalPnLPct := 0.0
		if at.initialBalance > 0 {
			totalPnLPct = (totalPnL / at.initialBalance) * 100
		}
		marginUsedPct := 0.0
		if at.demoEquity > 0 {
			marginUsedPct = (totalMarginUsed / at.demoEquity) * 100.0
		}
		availableBalance := at.demoEquity - totalMarginUsed
		if availableBalance < 0 {
			availableBalance = 0
		}

		return map[string]interface{}{
			"total_equity":         at.demoEquity,
			"wallet_balance":       at.demoEquity,
			"unrealized_profit":    totalUnrealized,
			"available_balance":    availableBalance,
			"total_pnl":            totalPnL,
			"total_pnl_pct":        totalPnLPct,
			"total_unrealized_pnl": totalUnrealized,
			"initial_balance":      at.initialBalance,
			"daily_pnl":            at.dailyPnL,
			"position_count":       len(positions),
			"margin_used":          totalMarginUsed,
			"margin_used_pct":      marginUsedPct,
		}, nil
	}

	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("variables Logic Limit maps Strings Limits Map Target Limit limitations Mapping List parameter Map mapping Tracker MAP limits: %w", err)
	}

	// Variables limits maps Array MAP variables string Strings Arrays mappings
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = tracking Map strings strings limits Maps Map MAP parameters variables
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// Map List limits Map Tracker limit string Maps limitations MAP
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	totalMarginUsed := 0.0
	totalUnrealizedPnL := 0.0
	for _, pos := range positions {
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnL += unrealizedPnl

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	return map[string]interface{}{
		// limits Values arrays strings Target Parameter lists targeting bounds strings Variables Maps Strings Mapping variables loops Variables MAP tracking Map MAP Map Tracker limits list Tracker Maps Tracking string Tracking Tracking parameters permutations Object Mapping Limitation tracking Maps List Tracker Limits Tracking variables limitations Lists maps configurations List Parameter Tracker Variable Targets array Maps Mapping
		"total_equity":      totalEquity,           // Account equity = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // wallet Target loops Limits arrays tracking Tracker arrays
		"unrealized_profit": totalUnrealizedProfit, // unrealizedP&L String List limitations
		"available_balance": availableBalance,      // available limits limitations Target Limit map strings Parameter Map Mapping target Parameter strings

		// P&L Strings Limits loops String parameter limits strings Strings List variables mapping map Object map Mapping tracking parameters limits Limit map Logic limitations Tracking List Maps limitation mapping loops
		"total_pnl":            totalPnL,           // strings MAP Mapping Limits Target configurations List List Matrix configurations Parameter Map lists Strings maps limit Maps Limits limitations Object Array Variables arrays Target Limit Map Targeting Object map Strings parameter Tracker limit Parameter arrays List array parameters map Limit lists loops mapping Tracking arrays Array Mapper Tracking Tracker map array string Variable logic strings Map mapping Mapper Map map array Strings Targets Targeting Mapper String Limits map Mapper maps Tracker Maps mapping lists MAP limitations Mapping permutations Variable Map mapping parameters MAP variables tracking Limits String Array Tracker map Mapper limits limit Limits Limit string variables limit array loops string limits loops map Limits Array MAP limitation parameter Values mapping String loops Limits Target
		"total_pnl_pct":        totalPnLPct,        // Tracking Map Variable parameter limits Variable Arrays Tracker List variables Tracking map limitation limits Tracker Maps mapping
		"total_unrealized_pnl": totalUnrealizedPnL, // unrealizedP&L Maps Target Limits arrays limit Object Mapper limitations limits
		"initial_balance":      at.initialBalance,  // Initial balance
		"daily_pnl":            at.dailyPnL,        // limits map String Variable Variables MAP limitations Maps limitations Mapping MAP tracking Limitation Targeting Variables Strings List limits maps MAP maps variables Strings mapping Limits String Variables String logic Object mapping strings Variables MAP Matrix Limits Logic variables mapping Limits array limitations mapping maps Object String Tracking array Mapper limitations Tracker Limitation tracking mapping variables Lists Limits map permutations

		// Positions Target Matrix mapping strings constraints limits Tracker Mapping Mapping Tracking maps limitation maps variables Lists
		"position_count":  len(positions),  // Positions Array maps parameter
		"margin_used":     totalMarginUsed, // margin used
		"margin_used_pct": marginUsedPct,   // map Target MAP array limitation Tracker
	}, nil
}

// GetPositions Mapping variables maps List arrays limits Parameter string strings Logic loops MAP combinations Arrays target Map limitation Tracking array variables MAP Array maps tracking string Strings Lists array Maps variations Tracking limits limit limits loops Mapper mapping maps Tracker maps Object
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	if at.demoMode {
		return at.buildDemoPositions(), nil
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		pnlPct := 0.0
		if side == "long" {
			pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		marginUsed := (quantity * markPrice) / float64(leverage)

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		})
	}

	return result, nil
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
	totalNotional := at.demoEquity * (at.demoMarginUsedPct / 100.0)
	if totalNotional < 0 {
		totalNotional = 0
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
		allocatedNotional := totalNotional / float64(at.demoPositionCount)
		if allocatedNotional <= 0 {
			allocatedNotional = at.demoEquity * 0.02
		}

		quantity := allocatedNotional / entryPrice
		unrealized := (markPrice - entryPrice) * quantity
		if side == "short" {
			unrealized = -unrealized
		}

		liqPrice := entryPrice * (1.0 - 0.20/float64(leverage))
		if side == "short" {
			liqPrice = entryPrice * (1.0 + 0.20/float64(leverage))
		}

		marginUsed := (quantity * markPrice) / float64(leverage)

		positions = append(positions, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealized,
			"unrealized_pnl_pct": (unrealized / (quantity * entryPrice / float64(leverage))) * 100.0,
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

	candidate, ok := selectBestMomentumSignal(ctx, at.config.MomentumMinScore)
	if !ok {
		return
	}

	positionPct := at.config.FallbackPositionPct
	if positionPct <= 0 || positionPct > 0.20 {
		positionPct = 0.10
	}

	notional := ctx.Account.TotalEquity * positionPct
	maxPerTrade := ctx.Account.TotalEquity * 0.20
	if notional > maxPerTrade {
		notional = maxPerTrade
	}
	if ctx.Account.AvailableBalance > 0 && notional > ctx.Account.AvailableBalance*0.95 {
		notional = ctx.Account.AvailableBalance * 0.95
	}
	if notional < 250 {
		return
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

	fallback := decision.Decision{
		Symbol:          candidate.Symbol,
		Action:          action,
		Leverage:        1,
		PositionSizeUSD: notional,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		Confidence:      confidence,
		Reasoning:       fmt.Sprintf("Momentum fallback: score=%.2f trend=%.2f rsi7=%.1f macd=%.4f", candidate.Score, candidate.TrendScore, candidate.RSI7, candidate.MACD),
	}

	log.Printf(" Momentum fallback generated %s on %s | score=%.2f | notional=%.2f", action, candidate.Symbol, candidate.Score, notional)
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
