// Package trader - momentum_fallback.go
// Momentum-only decision helpers and the equity momentum fallback path.
// Kept separate from auto_trader.go so the cycle orchestration code stays
// focused on lifecycle concerns. Every AutoTrader method here stays on the
// *AutoTrader receiver.
package trader

import (
	"fmt"
	"log"
	"math"
	"northstar/decision"
	"northstar/market"
	"strings"
	"time"
)

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
