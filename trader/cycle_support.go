// Package trader - cycle_support.go
// Support helpers for the AutoTrader cycle loop: trading-context construction,
// decision engine dispatch, and market-data-block error classification.
// Split out so auto_trader.go stays focused on lifecycle orchestration.
// All AutoTrader methods remain on the *AutoTrader receiver.
package trader

import (
	"fmt"
	"log"
	"northstar/decision"
	"northstar/pool"
	"strings"
	"time"
)

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
	usesCanonical := at.usesCanonicalEquityPipeline()
	if usesCanonical {
		if err := at.prepareCanonicalEquityContext(ctx); err != nil {
			return nil, err
		}
	}

	// Local strategies (momentum_only / multi_factor) work across equity and
	// crypto — they only need ctx.MarketDataMap populated. For non-canonical
	// (crypto) pipelines, load market data here since prepareCanonicalEquityContext
	// is a no-op outside the canonical path.
	needsLocalMarketData := at.config.StrategyMode == "momentum_only" || at.config.StrategyMode == "multi_factor"
	if needsLocalMarketData && !usesCanonical && len(ctx.MarketDataMap) == 0 {
		if err := at.loadMomentumMarketData(ctx); err != nil {
			return nil, err
		}
	}

	switch at.config.StrategyMode {
	case "momentum_only":
		return at.buildMomentumOnlyDecision(ctx), nil
	case "multi_factor":
		return at.buildMultiFactorDecision(ctx), nil
	case "hybrid_ai":
		if !usesCanonical {
			return decision.GetFullDecision(ctx, at.mcpClient)
		}
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
	return decision.GetFullDecision(ctx, at.mcpClient)
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
