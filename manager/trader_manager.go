package manager

import (
	"aegistrade/config"
	"aegistrade/trader"
	"fmt"
	"log"
	"sync"
	"time"
)

// TraderManager configures and oversees multiple trader instances
type TraderManager struct {
	traders map[string]*trader.AutoTrader // key: trader ID
	mu      sync.RWMutex
}

// NewTraderManager instantiates a trader manager
func NewTraderManager() *TraderManager {
	return &TraderManager{
		traders: make(map[string]*trader.AutoTrader),
	}
}

// AddTrader initializes a new trader setup
func (tm *TraderManager) AddTrader(cfg config.TraderConfig, coinPoolURL string, maxDailyLoss, maxDrawdown float64, stopTradingMinutes int, leverage config.LeverageConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.traders[cfg.ID]; exists {
		return fmt.Errorf("trader ID '%s' already exists", cfg.ID)
	}

	allowShort := true
	if cfg.AllowShort != nil {
		allowShort = *cfg.AllowShort
	}
	useMacroFilters := true
	if cfg.UseMacroFilters != nil {
		useMacroFilters = *cfg.UseMacroFilters
	}
	dynamicSizing := true
	if cfg.DynamicPositionSizing != nil {
		dynamicSizing = *cfg.DynamicPositionSizing
	}
	regimeRiskScaling := true
	if cfg.RegimeRiskScaling != nil {
		regimeRiskScaling = *cfg.RegimeRiskScaling
	}

	// Construct AutoTraderConfig parameter map
	traderConfig := trader.AutoTraderConfig{
		ID:                    cfg.ID,
		Name:                  cfg.Name,
		AIModel:               cfg.AIModel,
		Exchange:              cfg.Exchange,
		BinanceAPIKey:         cfg.BinanceAPIKey,
		BinanceSecretKey:      cfg.BinanceSecretKey,
		HyperliquidPrivateKey: cfg.HyperliquidPrivateKey,
		HyperliquidWalletAddr: cfg.HyperliquidWalletAddr,
		HyperliquidTestnet:    cfg.HyperliquidTestnet,
		AsterUser:             cfg.AsterUser,
		AsterSigner:           cfg.AsterSigner,
		AsterPrivateKey:       cfg.AsterPrivateKey,
		AlpacaAPIKey:          cfg.AlpacaAPIKey,
		AlpacaSecretKey:       cfg.AlpacaSecretKey,
		AlpacaPaperTrading:    cfg.AlpacaPaperTrading,
		IBKRGatewayURL:        cfg.IBKRGatewayURL,
		IBKRAccountID:         cfg.IBKRAccountID,
		IBKRSessionCookie:     cfg.IBKRSessionCookie,
		StrictLiveMode:        cfg.StrictLiveMode,
		CoinPoolAPIURL:        coinPoolURL,
		UseQwen:               cfg.AIModel == "qwen",
		DeepSeekKey:           cfg.DeepSeekKey,
		QwenKey:               cfg.QwenKey,
		CustomAPIURL:          cfg.CustomAPIURL,
		CustomAPIKey:          cfg.CustomAPIKey,
		CustomModelName:       cfg.CustomModelName,
		DemoMode:              cfg.DemoMode,
		ScanInterval:          cfg.GetScanInterval(),
		InitialBalance:        cfg.InitialBalance,
		BTCETHLeverage:        leverage.BTCETHLeverage,  // Bind configured leverage sizing
		AltcoinLeverage:       leverage.AltcoinLeverage, // Bind configured leverage sizing
		MaxDailyLoss:          maxDailyLoss,
		MaxDrawdown:           maxDrawdown,
		StopTradingTime:       time.Duration(stopTradingMinutes) * time.Minute,

		// Exec mode and data provider settings integration
		Mode:                     cfg.Mode,
		DataProvider:             cfg.DataProvider,
		Broker:                   cfg.Broker,
		CSVDataDir:               cfg.CSVDataDir,
		InstrumentType:           cfg.InstrumentType,
		BarsAdjustment:           cfg.BarsAdjustment,
		CandidateBatchSize:       cfg.CandidateBatchSize,
		TrustedSymbolsFile:       cfg.TrustedSymbolsFile,
		StrategyMode:             cfg.StrategyMode,
		MomentumMinScore:         cfg.MomentumMinScore,
		FallbackPositionPct:      cfg.FallbackPositionPct,
		MaxCycles:                cfg.MaxCycles,
		ReplayWarmupBars:         cfg.ReplayWarmupBars,
		MaxGrossExposure:         cfg.MaxGrossExposure,
		MaxPositionPct:           cfg.MaxPositionPct,
		MaxDailyLossPct:          cfg.MaxDailyLossPct,
		MaxPairCorrelation:       cfg.MaxPairCorrelation,
		MinLiquidityUSD:          cfg.MinLiquidityUSD,
		MinDecisionConfidence:    cfg.MinDecisionConfidence,
		ExecutionCommissionBps:   cfg.ExecutionCommissionBps,
		ExecutionSlippageBps:     cfg.ExecutionSlippageBps,
		ExecutionImpactBps:       cfg.ExecutionImpactBps,
		MaxParticipationRate:     cfg.MaxParticipationRate,
		DrawdownThrottleStartPct: cfg.DrawdownThrottleStartPct,
		DrawdownThrottleMinScale: cfg.DrawdownThrottleMinScale,
		MaxPortfolioHeatPct:      cfg.MaxPortfolioHeatPct,
		MaxNetExposurePct:        cfg.MaxNetExposurePct,
		LossStreakPauseThreshold: cfg.LossStreakPauseThreshold,
		LossStreakPauseCycles:    cfg.LossStreakPauseCycles,
		PerformanceRiskLookback:  cfg.PerformanceRiskLookback,
		VolatilityBrakeTargetPct: cfg.VolatilityBrakeTargetPct,
		VolatilityBrakeLookback:  cfg.VolatilityBrakeLookback,
		VolatilityBrakeMinScale:  cfg.VolatilityBrakeMinScale,
		KellyFractionCap:         cfg.KellyFractionCap,
		KellyLookback:            cfg.KellyLookback,
		KellyMinTrades:           cfg.KellyMinTrades,
		MarketStressEntryBlock:   cfg.MarketStressEntryBlock,
		MarketStressRiskMinScale: cfg.MarketStressRiskMinScale,
		MinFactorScore:           cfg.MinFactorScore,
		RiskPerTradePct:          cfg.RiskPerTradePct,
		ProfitLockThreshold:      cfg.ProfitLockThreshold,
		TrailingStopATRMult:      cfg.TrailingStopATRMult,
		MaxHoldingCycles:         cfg.MaxHoldingCycles,
		MaxConcurrentPos:         cfg.MaxConcurrentPos,
		SymbolCooldownCycles:     cfg.SymbolCooldownCycles,
		AllowShort:               allowShort,
		UseMacroFilters:          useMacroFilters,
		DynamicPositionSizing:    dynamicSizing,
		RegimeRiskScaling:        regimeRiskScaling,
		BenchmarkSymbols:         cfg.BenchmarkSymbols,
	}

	// Create new AutoTrader execution wrapper
	at, err := trader.NewAutoTrader(traderConfig)
	if err != nil {
		return fmt.Errorf("failed generating new trader: %w", err)
	}

	tm.traders[cfg.ID] = at
	log.Printf(" Trader '%s' (%s) added", cfg.Name, cfg.AIModel)
	return nil
}

// GetTrader extracts mapping logic execution handler
func (tm *TraderManager) GetTrader(id string) (*trader.AutoTrader, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, exists := tm.traders[id]
	if !exists {
		return nil, fmt.Errorf("trader ID '%s' does not exist", id)
	}
	return t, nil
}

// GetAllTraders pulls map indexing logic references structure
func (tm *TraderManager) GetAllTraders() map[string]*trader.AutoTrader {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make(map[string]*trader.AutoTrader)
	for id, t := range tm.traders {
		result[id] = t
	}
	return result
}

// GetTraderIDs tracks configured string indexing execution IDs
func (tm *TraderManager) GetTraderIDs() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	ids := make([]string, 0, len(tm.traders))
	for id := range tm.traders {
		ids = append(ids, id)
	}
	return ids
}

// StartAll wraps execution triggering logic for concurrent setups
func (tm *TraderManager) StartAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	log.Println(" Starting all traders...")
	for id, t := range tm.traders {
		go func(traderID string, at *trader.AutoTrader) {
			log.Printf("  Starting %s...", at.GetName())
			if err := at.Run(); err != nil {
				log.Printf(" %s runtime error: %v", at.GetName(), err)
			}
		}(id, t)
	}
}

// StopAll interrupts and halts ongoing tracking events execution loops
func (tm *TraderManager) StopAll() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	log.Println("  Stopping all traders...")
	for _, t := range tm.traders {
		t.Stop()
	}
}

// GetComparisonData aggregates comparative metrics data
func (tm *TraderManager) GetComparisonData() (map[string]interface{}, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	comparison := make(map[string]interface{})
	traders := make([]map[string]interface{}, 0, len(tm.traders))

	for _, t := range tm.traders {
		account, err := t.GetAccountInfo()
		if err != nil {
			continue
		}

		status := t.GetStatus()

		traders = append(traders, map[string]interface{}{
			"trader_id":       t.GetID(),
			"trader_name":     t.GetName(),
			"ai_model":        t.GetAIModel(),
			"total_equity":    account["total_equity"],
			"total_pnl":       account["total_pnl"],
			"total_pnl_pct":   account["total_pnl_pct"],
			"position_count":  account["position_count"],
			"margin_used_pct": account["margin_used_pct"],
			"call_count":      status["call_count"],
			"is_running":      status["is_running"],
		})
	}

	comparison["traders"] = traders
	comparison["count"] = len(traders)

	return comparison, nil
}
