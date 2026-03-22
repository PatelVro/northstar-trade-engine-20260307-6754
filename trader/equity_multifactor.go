package trader

import (
	"fmt"
	"math"
	"northstar/decision"
	"northstar/logger"
	"northstar/market"
	"northstar/news"
	"sort"
	"strings"
	"time"
)

type equityFactorSnapshot struct {
	Symbol       string
	Price        float64
	ATRPct       float64
	Trend        float64
	Momentum     float64
	RSI          float64
	Volume       float64
	Relative     float64
	Quality      float64
	Reversion    float64
	Volatility   float64
	Liquidity    float64
	DollarVolume float64
	Edge         float64
	Macro        float64
	Total        float64
}

type equityMarketRegime struct {
	Score        float64
	Label        string
	Breadth      float64
	BenchmarkMap map[string]float64
	Stress       float64
	Dispersion   float64
	AbsCorr      float64
}

func (at *AutoTrader) refreshPositionState(positions []decision.PositionInfo) {
	active := make(map[string]struct{}, len(positions))
	for _, pos := range positions {
		key := strings.ToUpper(strings.TrimSpace(pos.Symbol)) + "_" + strings.ToLower(strings.TrimSpace(pos.Side))
		if key == "_" {
			continue
		}
		active[key] = struct{}{}

		if _, ok := at.positionEntryCycle[key]; !ok {
			at.positionEntryCycle[key] = at.callCount
		}
		if prev, ok := at.positionPeakPnLPct[key]; !ok || pos.UnrealizedPnLPct > prev {
			at.positionPeakPnLPct[key] = pos.UnrealizedPnLPct
		}
	}

	for key := range at.positionEntryCycle {
		if _, ok := active[key]; !ok {
			delete(at.positionEntryCycle, key)
			delete(at.positionPeakPnLPct, key)
			delete(at.positionNewsBias, key)
			delete(at.plannedNewsBias, key)
		}
	}
}

func (at *AutoTrader) updateExecutionState(actions []logger.DecisionAction) {
	at.updateLocalPositionStateFromActions(actions)

	cooldown := at.config.SymbolCooldownCycles
	if cooldown <= 0 {
		cooldown = 1
	}

	for _, action := range actions {
		if !actionHasImmediatePositionEffect(action) {
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(action.Symbol))
		switch action.Action {
		case "open_long":
			key := symbol + "_long"
			at.positionEntryCycle[key] = at.callCount
			at.positionPeakPnLPct[key] = 0
			at.promotePlannedNewsBias(symbol, "long")
			delete(at.symbolCooldownUntil, symbol)
		case "open_short":
			key := symbol + "_short"
			at.positionEntryCycle[key] = at.callCount
			at.positionPeakPnLPct[key] = 0
			at.promotePlannedNewsBias(symbol, "short")
			delete(at.symbolCooldownUntil, symbol)
		case "close_long":
			delete(at.positionEntryCycle, symbol+"_long")
			delete(at.positionPeakPnLPct, symbol+"_long")
			delete(at.plannedNewsBias, symbol+"_long")
			at.symbolCooldownUntil[symbol] = at.callCount + cooldown
		case "close_short":
			delete(at.positionEntryCycle, symbol+"_short")
			delete(at.positionPeakPnLPct, symbol+"_short")
			delete(at.plannedNewsBias, symbol+"_short")
			at.symbolCooldownUntil[symbol] = at.callCount + cooldown
		}
	}
}

func (at *AutoTrader) updateSymbolEdgeFromActions(actions []logger.DecisionAction, closePnLPct map[string]float64) {
	if len(actions) == 0 || len(closePnLPct) == 0 {
		return
	}
	for _, action := range actions {
		if !actionHasImmediatePositionEffect(action) {
			continue
		}
		side := ""
		switch action.Action {
		case "close_long":
			side = "long"
		case "close_short":
			side = "short"
		default:
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(action.Symbol))
		if symbol == "" {
			continue
		}
		key := symbol + "_" + side
		pnlPct, ok := closePnLPct[key]
		if !ok {
			continue
		}

		normalized := clampFloat(pnlPct/3.0, -1.5, 1.5)
		prev := at.symbolEdgeScore[symbol]
		count := at.symbolTradeCount[symbol]
		alpha := 0.18
		if count < 5 {
			alpha = 0.28
		}
		next := (1.0-alpha)*prev + alpha*normalized
		at.symbolEdgeScore[symbol] = clampFloat(next, -1.0, 1.0)
		at.symbolTradeCount[symbol] = count + 1
	}
}

func (at *AutoTrader) getSymbolEdge(symbol string) (float64, int) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return 0, 0
	}
	return at.symbolEdgeScore[symbol], at.symbolTradeCount[symbol]
}

func (at *AutoTrader) updateClosePerformanceFromActions(actions []logger.DecisionAction, closePnLPct map[string]float64) {
	if len(actions) == 0 || len(closePnLPct) == 0 {
		return
	}
	lookback := at.config.PerformanceRiskLookback
	if lookback <= 0 {
		lookback = 20
	}

	for _, action := range actions {
		if !actionHasImmediatePositionEffect(action) {
			continue
		}
		side := ""
		switch action.Action {
		case "close_long":
			side = "long"
		case "close_short":
			side = "short"
		default:
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(action.Symbol))
		if symbol == "" {
			continue
		}
		key := symbol + "_" + side
		pnlPct, ok := closePnLPct[key]
		if !ok {
			continue
		}
		at.learnNewsOutcome(symbol, side, pnlPct)

		if len(at.recentClosePnLPct) == 0 {
			at.closePnLEMA = pnlPct
		} else {
			at.closePnLEMA = at.closePnLEMA*0.80 + pnlPct*0.20
		}
		at.recentClosePnLPct = append(at.recentClosePnLPct, pnlPct)
		if len(at.recentClosePnLPct) > lookback {
			at.recentClosePnLPct = at.recentClosePnLPct[len(at.recentClosePnLPct)-lookback:]
		}

		if pnlPct < 0 {
			at.consecutiveLossCloses++
		} else if pnlPct > 0 {
			at.consecutiveLossCloses = 0
		}
	}

	pauseThreshold := at.config.LossStreakPauseThreshold
	if pauseThreshold <= 0 {
		pauseThreshold = 3
	}
	pauseCycles := at.config.LossStreakPauseCycles
	if pauseCycles <= 0 {
		pauseCycles = 5
	}
	if at.consecutiveLossCloses >= pauseThreshold {
		blockUntil := at.callCount + pauseCycles
		if blockUntil > at.openEntryBlockedUntil {
			at.openEntryBlockedUntil = blockUntil
		}
	}
}

func (at *AutoTrader) performanceRiskScale() float64 {
	if len(at.recentClosePnLPct) == 0 {
		return 1.0
	}

	lookback := at.config.PerformanceRiskLookback
	if lookback <= 0 {
		lookback = 20
	}
	window := at.recentClosePnLPct
	if len(window) > lookback {
		window = window[len(window)-lookback:]
	}

	mean := 0.0
	winCount := 0
	for _, pnl := range window {
		mean += pnl
		if pnl > 0 {
			winCount++
		}
	}
	mean /= float64(len(window))
	winRate := float64(winCount) / float64(len(window))

	perfSignal := clampFloat((0.55*at.closePnLEMA+0.45*mean)/1.6, -1.0, 1.0)
	winSignal := clampFloat((winRate-0.5)*2.0, -1.0, 1.0)
	scale := 1.0 + 0.28*perfSignal + 0.14*winSignal

	pauseThreshold := at.config.LossStreakPauseThreshold
	if pauseThreshold <= 0 {
		pauseThreshold = 3
	}
	if at.consecutiveLossCloses >= pauseThreshold-1 {
		scale *= 0.80
	}
	return clampFloat(scale, 0.50, 1.25)
}

func (at *AutoTrader) kellyRiskScale() float64 {
	lookback := at.config.KellyLookback
	if lookback <= 0 {
		lookback = 30
	}
	minTrades := at.config.KellyMinTrades
	if minTrades <= 0 {
		minTrades = 10
	}
	window := at.recentClosePnLPct
	if len(window) > lookback {
		window = window[len(window)-lookback:]
	}
	if len(window) < minTrades {
		return 1.0
	}

	wins := 0
	avgWin := 0.0
	avgLoss := 0.0
	losses := 0
	for _, pnlPct := range window {
		if pnlPct > 0 {
			wins++
			avgWin += pnlPct
		} else if pnlPct < 0 {
			losses++
			avgLoss += math.Abs(pnlPct)
		}
	}
	if wins == 0 || losses == 0 {
		return 1.0
	}
	avgWin /= float64(wins)
	avgLoss /= float64(losses)
	if avgWin <= 0 || avgLoss <= 0 {
		return 1.0
	}

	p := float64(wins) / float64(len(window))
	b := avgWin / avgLoss
	if b <= 0 {
		return 1.0
	}
	rawKelly := p - ((1.0 - p) / b)

	kellyCap := at.config.KellyFractionCap
	if kellyCap <= 0 || kellyCap > 1 {
		kellyCap = 0.33
	}
	sampleConfidence := clampFloat(float64(len(window))/float64(lookback), 0.30, 1.0)
	target := clampFloat(rawKelly*kellyCap*sampleConfidence, -0.25, 0.25)
	scale := 1.0 + target*1.6
	return clampFloat(scale, 0.55, 1.35)
}

func (at *AutoTrader) currentNewsSnapshot(symbols []string) *news.Snapshot {
	if at.newsProvider == nil || !at.config.UseNewsRisk {
		return nil
	}
	refreshSec := at.config.NewsRefreshSeconds
	if refreshSec <= 0 {
		refreshSec = 120
	}
	if at.cachedNews != nil && time.Since(at.lastNewsRefresh) < time.Duration(refreshSec)*time.Second {
		return at.cachedNews
	}

	lookback := time.Duration(at.config.NewsLookbackMinutes) * time.Minute
	if lookback <= 0 {
		lookback = 4 * time.Hour
	}
	snapshot, err := at.newsProvider.Fetch(symbols, lookback)
	if err != nil {
		at.newsLastError = err.Error()
		return at.cachedNews
	}
	at.cachedNews = snapshot
	at.lastNewsRefresh = time.Now()
	at.newsLastError = ""
	return at.cachedNews
}

func newsDirectionalPressure(action string, sentiment float64) float64 {
	switch action {
	case "open_long":
		return clampFloat(-sentiment, 0, 1)
	case "open_short":
		return clampFloat(sentiment, 0, 1)
	default:
		return 0
	}
}

func (at *AutoTrader) newsRiskScale(action string, snapshot *news.Snapshot) float64 {
	if snapshot == nil {
		return 1.0
	}
	maxReduction := at.config.NewsMaxRiskReduction
	if maxReduction <= 0 || maxReduction > 0.95 {
		maxReduction = 0.55
	}
	directional := newsDirectionalPressure(action, snapshot.MarketSentiment)
	pressure := snapshot.MarketImpact * directional * at.effectiveNewsCredibility("")
	scale := 1.0 - clampFloat(pressure*maxReduction, 0, maxReduction)
	return clampFloat(scale, 1.0-maxReduction, 1.0)
}

func collectNewsWatchSymbols(ctx *decision.Context, decisions []decision.Decision) []string {
	seen := make(map[string]struct{}, 24)
	out := make([]string, 0, 24)
	add := func(raw string) {
		s := strings.ToUpper(strings.TrimSpace(raw))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	if ctx != nil {
		for _, pos := range ctx.Positions {
			add(pos.Symbol)
		}
		for _, coin := range ctx.CandidateCoins {
			add(coin.Symbol)
			if len(out) >= 14 {
				break
			}
		}
	}
	for _, d := range decisions {
		add(d.Symbol)
	}
	sort.Strings(out)
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func (at *AutoTrader) recordEquityObservation(equity float64) {
	if equity <= 0 {
		return
	}
	at.recentEquity = append(at.recentEquity, equity)
	maxKeep := at.config.VolatilityBrakeLookback*3 + 20
	if maxKeep < 60 {
		maxKeep = 60
	}
	if len(at.recentEquity) > maxKeep {
		at.recentEquity = at.recentEquity[len(at.recentEquity)-maxKeep:]
	}
}

func (at *AutoTrader) realizedEquityVolPct() float64 {
	lookback := at.config.VolatilityBrakeLookback
	if lookback <= 0 {
		lookback = 40
	}
	if len(at.recentEquity) < lookback+1 {
		return 0
	}
	window := at.recentEquity[len(at.recentEquity)-(lookback+1):]
	returns := make([]float64, 0, len(window)-1)
	for i := 1; i < len(window); i++ {
		prev := window[i-1]
		curr := window[i]
		if prev <= 0 || curr <= 0 {
			continue
		}
		returns = append(returns, (curr-prev)/prev)
	}
	if len(returns) < 2 {
		return 0
	}
	_, std := meanStd(returns)
	if std < 0 {
		return 0
	}
	return std
}

func (at *AutoTrader) buildMultiFactorDecision(ctx *decision.Context) *decision.FullDecision {
	factors, regime := at.computeEquityFactors(ctx)

	decisions := make([]decision.Decision, 0, 6)
	decisions = append(decisions, at.multiFactorExitDecisions(ctx, factors)...)

	plannedClose := 0
	for _, d := range decisions {
		if d.Action == "close_long" || d.Action == "close_short" {
			plannedClose++
		}
	}
	capacity := at.config.MaxConcurrentPos - (len(ctx.Positions) - plannedClose)
	if capacity < 0 {
		capacity = 0
	}
	if capacity > 0 {
		decisions = append(decisions, at.multiFactorEntryDecisions(ctx, factors, regime, capacity)...)
	}

	if len(decisions) == 0 {
		decisions = append(decisions, decision.Decision{
			Action:    "wait",
			Reasoning: "Multi-factor strategy: no edge above threshold after risk and regime filters",
		})
	}

	topScore := 0.0
	topSymbol := ""
	for _, snap := range factors {
		if math.Abs(snap.Total) > topScore {
			topScore = math.Abs(snap.Total)
			topSymbol = snap.Symbol
		}
	}

	cot := fmt.Sprintf(
		"Local multi-factor execution | regime=%s(%.2f) breadth=%.2f stress=%.2f corr=%.2f disp=%.2f | tracked=%d symbols | best_signal=%s %.2f",
		regime.Label, regime.Score, regime.Breadth, regime.Stress, regime.AbsCorr, regime.Dispersion, len(factors), topSymbol, topScore,
	)

	return &decision.FullDecision{
		UserPrompt: "Multi-factor local strategy decision (no external AI call)",
		CoTTrace:   cot,
		Decisions:  decisions,
		Timestamp:  time.Now(),
	}
}

func (at *AutoTrader) applyHybridFactorFilter(ctx *decision.Context, fullDecision *decision.FullDecision) {
	if fullDecision == nil || ctx == nil || at.config.InstrumentType != "equity" {
		return
	}
	factors, regime := at.computeEquityFactors(ctx)
	minScore := at.config.MinFactorScore * 0.65
	if minScore <= 0 {
		minScore = 0.20
	}
	stressBlock := at.config.MarketStressEntryBlock
	if stressBlock <= 0 || stressBlock > 1.0 {
		stressBlock = 0.82
	}

	filtered := make([]decision.Decision, 0, len(fullDecision.Decisions))
	for _, d := range fullDecision.Decisions {
		d.Symbol = strings.ToUpper(strings.TrimSpace(d.Symbol))
		switch d.Action {
		case "open_long":
			snap, ok := factors[d.Symbol]
			if !ok || snap.Total < minScore {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked long %s: factor score too weak", d.Symbol),
				})
				continue
			}
			if at.config.UseMacroFilters && regime.Score <= -0.25 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked long %s: bearish macro regime", d.Symbol),
				})
				continue
			}
			if regime.Stress >= stressBlock {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked long %s: market stress %.2f exceeds %.2f", d.Symbol, regime.Stress, stressBlock),
				})
				continue
			}
			d.Confidence = clampInt(maxInt(d.Confidence, 65)+int(math.Round(math.Abs(snap.Total)*12)), 65, 98)
			d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | factor=%.2f trend=%.2f momentum=%.2f stress=%.2f", snap.Total, snap.Trend, snap.Momentum, regime.Stress))
			filtered = append(filtered, d)
		case "open_short":
			if !at.config.AllowShort {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked short %s: shorting disabled", d.Symbol),
				})
				continue
			}
			snap, ok := factors[d.Symbol]
			if !ok || snap.Total > -minScore {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked short %s: factor score too weak", d.Symbol),
				})
				continue
			}
			if at.config.UseMacroFilters && regime.Score >= 0.25 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked short %s: bullish macro regime", d.Symbol),
				})
				continue
			}
			if regime.Stress >= stressBlock {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Hybrid filter blocked short %s: market stress %.2f exceeds %.2f", d.Symbol, regime.Stress, stressBlock),
				})
				continue
			}
			d.Confidence = clampInt(maxInt(d.Confidence, 65)+int(math.Round(math.Abs(snap.Total)*12)), 65, 98)
			d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | factor=%.2f trend=%.2f momentum=%.2f stress=%.2f", snap.Total, snap.Trend, snap.Momentum, regime.Stress))
			filtered = append(filtered, d)
		default:
			filtered = append(filtered, d)
		}
	}

	if len(filtered) > 0 {
		fullDecision.Decisions = filtered
	}
}

func (at *AutoTrader) applyEquityDecisionOverlay(ctx *decision.Context, fullDecision *decision.FullDecision) {
	if fullDecision == nil || ctx == nil || at.config.InstrumentType != "equity" {
		return
	}
	for key := range at.plannedNewsBias {
		delete(at.plannedNewsBias, key)
	}

	if len(fullDecision.Decisions) == 0 {
		fullDecision.Decisions = []decision.Decision{{Action: "wait", Reasoning: "No decisions returned"}}
		return
	}

	breadth := marketBreadth(ctx)
	regime := at.computeMarketRegime(ctx, breadth)
	stressBlock := at.config.MarketStressEntryBlock
	if stressBlock <= 0 || stressBlock > 1.0 {
		stressBlock = 0.82
	}
	stressMinScale := at.config.MarketStressRiskMinScale
	if stressMinScale <= 0 || stressMinScale > 1.0 {
		stressMinScale = 0.35
	}
	newsSnapshot := at.currentNewsSnapshot(collectNewsWatchSymbols(ctx, fullDecision.Decisions))
	if newsSnapshot != nil {
		at.latestNewsSentiment = newsSnapshot.MarketSentiment
		at.latestNewsImpact = newsSnapshot.MarketImpact
	}
	newsMarketThresh := at.config.NewsMarketImpactThresh
	if newsMarketThresh <= 0 || newsMarketThresh > 1.0 {
		newsMarketThresh = 0.65
	}
	newsSymbolThresh := at.config.NewsSymbolImpactThresh
	if newsSymbolThresh <= 0 || newsSymbolThresh > 1.0 {
		newsSymbolThresh = 0.70
	}
	newsHardBlock := at.config.NewsHardBlockThresh
	if newsHardBlock <= 0 || newsHardBlock > 1.0 {
		newsHardBlock = 0.85
	}
	newsMaxReduction := at.config.NewsMaxRiskReduction
	if newsMaxReduction <= 0 || newsMaxReduction > 0.95 {
		newsMaxReduction = 0.55
	}

	decisionEquity := ctx.Account.DecisionSizingEquity()
	maxGross := at.config.MaxGrossExposure * decisionEquity
	if maxGross <= 0 {
		maxGross = decisionEquity
	}
	if maxGross <= 0 {
		maxGross = 1
	}

	currentExposure := estimateGrossExposure(ctx.Positions)
	currentNetExposure := estimateNetExposure(ctx.Positions)
	currentPosCount := len(ctx.Positions)
	maxPositions := at.config.MaxConcurrentPos
	if maxPositions <= 0 {
		maxPositions = 3
	}

	existingBySymbol := make(map[string]string, len(ctx.Positions))
	for _, pos := range ctx.Positions {
		symbol := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		side := strings.ToLower(strings.TrimSpace(pos.Side))
		if symbol != "" && side != "" {
			existingBySymbol[symbol] = side
		}
	}

	filtered := make([]decision.Decision, 0, len(fullDecision.Decisions))
	plannedAdds := 0
	plannedExposure := 0.0
	plannedNetExposure := currentNetExposure
	availableAfterPlans := ctx.Account.AvailableBalance
	plannedOpenSides := make(map[string]string, len(fullDecision.Decisions))
	minConfidence := at.config.MinDecisionConfidence
	if minConfidence <= 0 {
		minConfidence = 58
	}
	maxPairCorrelation := at.config.MaxPairCorrelation
	if maxPairCorrelation <= 0 || maxPairCorrelation > 0.99 {
		maxPairCorrelation = 0.82
	}
	maxHeatPct := at.config.MaxPortfolioHeatPct
	if maxHeatPct <= 0 || maxHeatPct > 0.30 {
		maxHeatPct = 0.035
	}
	heatLimitUSD := decisionEquity * maxHeatPct
	currentHeatUSD := at.estimateOpenPortfolioHeatUSD(ctx)
	plannedHeatUSD := 0.0
	maxNetPct := at.config.MaxNetExposurePct
	if maxNetPct <= 0 || maxNetPct > 1.0 {
		maxNetPct = 0.65
	}
	netLimitUSD := decisionEquity * maxNetPct

	for _, d := range fullDecision.Decisions {
		d.Symbol = strings.ToUpper(strings.TrimSpace(d.Symbol))
		switch d.Action {
		case "open_long", "open_short":
			if d.Symbol == "" {
				filtered = append(filtered, decision.Decision{Action: "wait", Reasoning: "Execution overlay skipped unnamed symbol"})
				continue
			}
			newsCredibility := at.effectiveNewsCredibility(d.Symbol)
			marketNewsAdverse := 0.0
			symbolNewsAdverse := 0.0
			newsSupportScore := 0.0
			if regime.Stress >= stressBlock {
				filtered = append(filtered, decision.Decision{
					Action: "wait",
					Reasoning: fmt.Sprintf(
						"Execution overlay blocked %s: market stress %.2f exceeds %.2f",
						d.Symbol, regime.Stress, stressBlock,
					),
				})
				continue
			}
			if newsSnapshot != nil {
				marketDirectional := newsDirectionalPressure(d.Action, newsSnapshot.MarketSentiment)
				marketScore := newsSnapshot.MarketImpact * marketDirectional * newsCredibility
				marketNewsAdverse = marketScore

				marketSupport := newsSnapshot.MarketSentiment * newsSnapshot.MarketImpact * newsCredibility
				if d.Action == "open_short" {
					marketSupport = -marketSupport
				}
				newsSupportScore += 0.55 * marketSupport

				if marketScore >= newsHardBlock || (newsSnapshot.MarketImpact >= newsMarketThresh && marketDirectional*newsCredibility >= 0.98) {
					filtered = append(filtered, decision.Decision{
						Action: "wait",
						Reasoning: fmt.Sprintf(
							"Execution overlay blocked %s: adverse market news score %.2f (impact %.2f sentiment %.2f cred=%.2f)",
							d.Symbol, marketScore, newsSnapshot.MarketImpact, newsSnapshot.MarketSentiment, newsCredibility,
						),
					})
					continue
				}
				if signal, ok := newsSnapshot.SymbolSignals[d.Symbol]; ok {
					symbolDirectional := newsDirectionalPressure(d.Action, signal.Sentiment)
					symbolScore := signal.Impact * symbolDirectional * newsCredibility
					symbolNewsAdverse = symbolScore

					symbolSupport := signal.Sentiment * signal.Impact * newsCredibility
					if d.Action == "open_short" {
						symbolSupport = -symbolSupport
					}
					newsSupportScore += 0.45 * symbolSupport

					if symbolScore >= newsSymbolThresh {
						filtered = append(filtered, decision.Decision{
							Action: "wait",
							Reasoning: fmt.Sprintf(
								"Execution overlay blocked %s: symbol news score %.2f (impact %.2f sentiment %.2f cred=%.2f)",
								d.Symbol, symbolScore, signal.Impact, signal.Sentiment, newsCredibility,
							),
						})
						continue
					}
				}
			}
			edgeScore, edgeTrades := at.getSymbolEdge(d.Symbol)
			if edgeTrades >= 4 && edgeScore < -0.35 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: symbol edge score %.2f after %d closes", d.Symbol, edgeScore, edgeTrades),
				})
				continue
			}
			if d.Action == "open_short" && !at.config.AllowShort {
				filtered = append(filtered, decision.Decision{Action: "wait", Reasoning: fmt.Sprintf("Execution overlay blocked short %s: shorting disabled", d.Symbol)})
				continue
			}
			if until, ok := at.symbolCooldownUntil[d.Symbol]; ok && at.callCount < until {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: cooldown active for %d more cycles", d.Symbol, until-at.callCount),
				})
				continue
			}
			if at.openEntryBlockedUntil > at.callCount {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: loss-streak pause active for %d more cycles", d.Symbol, at.openEntryBlockedUntil-at.callCount),
				})
				continue
			}

			wantSide := "long"
			if d.Action == "open_short" {
				wantSide = "short"
			}
			if currentSide, ok := existingBySymbol[d.Symbol]; ok && currentSide == wantSide {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: %s already open", d.Symbol, wantSide),
				})
				continue
			}
			if currentPosCount+plannedAdds >= maxPositions {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: max concurrent positions reached", d.Symbol),
				})
				continue
			}

			entry := at.resolveEntryPrice(ctx, d.Symbol)
			if entry <= 0 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: no valid entry price", d.Symbol),
				})
				continue
			}
			if minLiq := at.config.MinLiquidityUSD; minLiq > 0 {
				if dollarVol := at.estimateDollarVolume(ctx, d.Symbol); dollarVol > 0 && dollarVol < minLiq {
					filtered = append(filtered, decision.Decision{
						Action:    "wait",
						Reasoning: fmt.Sprintf("Execution overlay blocked %s: liquidity %.0f below minimum %.0f", d.Symbol, dollarVol, minLiq),
					})
					continue
				}
			}
			maxCorr, peer := at.maxSameSideCorrelation(ctx, d.Symbol, d.Action, plannedOpenSides)
			if maxCorr > maxPairCorrelation {
				filtered = append(filtered, decision.Decision{
					Action: "wait",
					Reasoning: fmt.Sprintf(
						"Execution overlay blocked %s: same-side correlation %.2f exceeds limit %.2f with %s",
						d.Symbol, maxCorr, maxPairCorrelation, peer,
					),
				})
				continue
			}

			stop, take := at.ensureStopsAndTargets(ctx, d, entry)
			d.StopLoss = stop
			d.TakeProfit = take
			d.Leverage = 1

			if d.PositionSizeUSD <= 0 || at.config.DynamicPositionSizing {
				suggested := at.suggestedPositionSizeUSD(ctx, d.Symbol, d.Action, entry, d.StopLoss)
				if suggested > 0 {
					if d.PositionSizeUSD <= 0 {
						d.PositionSizeUSD = suggested
					} else {
						// Keep AI intent but cap with model-based risk sizing.
						d.PositionSizeUSD = math.Min(d.PositionSizeUSD, suggested*1.3)
					}
				}
			}

			maxPerPosition := at.config.MaxPositionPct * decisionEquity
			if maxPerPosition <= 0 {
				maxPerPosition = decisionEquity * 0.20
			}
			remainingGross := maxGross - (currentExposure + plannedExposure)
			if remainingGross < 0 {
				remainingGross = 0
			}
			capByBalance := availableAfterPlans * 0.95
			if capByBalance < 0 {
				capByBalance = 0
			}

			d.PositionSizeUSD = minFloat(d.PositionSizeUSD, maxPerPosition)
			d.PositionSizeUSD = minFloat(d.PositionSizeUSD, remainingGross)
			d.PositionSizeUSD = minFloat(d.PositionSizeUSD, capByBalance)

			riskPctPerNotional := at.estimateRiskPctPerNotional(ctx, d.Symbol, entry, d.StopLoss)
			if riskPctPerNotional <= 0 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: invalid risk model", d.Symbol),
				})
				continue
			}
			if heatLimitUSD > 0 {
				remainingHeatUSD := heatLimitUSD - (currentHeatUSD + plannedHeatUSD)
				if remainingHeatUSD <= 0 {
					filtered = append(filtered, decision.Decision{
						Action: "wait",
						Reasoning: fmt.Sprintf(
							"Execution overlay blocked %s: portfolio heat %.0f exceeds limit %.0f",
							d.Symbol, currentHeatUSD+plannedHeatUSD, heatLimitUSD,
						),
					})
					continue
				}
				heatCapNotional := remainingHeatUSD / riskPctPerNotional
				if heatCapNotional <= 0 {
					filtered = append(filtered, decision.Decision{
						Action:    "wait",
						Reasoning: fmt.Sprintf("Execution overlay blocked %s: no heat budget available", d.Symbol),
					})
					continue
				}
				if heatCapNotional < d.PositionSizeUSD {
					d.PositionSizeUSD = heatCapNotional
					d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | heat_cap=%.0f", heatCapNotional))
				}
			}
			if edgeTrades >= 2 && edgeScore < 0 {
				edgeScale := 1.0 + edgeScore*0.35
				edgeScale = clampFloat(edgeScale, 0.55, 1.10)
				d.PositionSizeUSD *= edgeScale
				d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | edge=%.2f trades=%d edge_scale=%.2f", edgeScore, edgeTrades, edgeScale))
			}
			if maxCorr > 0.35 {
				corrScale := 1.0 - clampFloat((maxCorr-0.35)/0.55, 0, 0.45)
				d.PositionSizeUSD *= corrScale
				d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | corr=%.2f vs %s scaled_notional=%.2f", maxCorr, peer, d.PositionSizeUSD))
			}
			if regime.Stress > 0.55 {
				stressScale := 1.0 - (regime.Stress-0.55)*1.35
				stressScale = clampFloat(stressScale, stressMinScale, 1.0)
				d.PositionSizeUSD *= stressScale
				d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | stress=%.2f stress_scale=%.2f", regime.Stress, stressScale))
			}
			if newsSnapshot != nil {
				if marketNewsAdverse > 0 {
					marketScale := 1.0 - clampFloat(marketNewsAdverse*newsMaxReduction, 0, newsMaxReduction)
					d.PositionSizeUSD *= marketScale
					d.Confidence = clampInt(d.Confidence-int(math.Round(marketNewsAdverse*18)), 35, 99)
					d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(
						" | news_market=%.2f impact=%.2f sentiment=%.2f cred=%.2f scale=%.2f",
						marketNewsAdverse, newsSnapshot.MarketImpact, newsSnapshot.MarketSentiment, newsCredibility, marketScale,
					))
				}
				if symbolNewsAdverse > 0 {
					symbolScale := 1.0 - clampFloat(symbolNewsAdverse*newsMaxReduction, 0, newsMaxReduction)
					d.PositionSizeUSD *= symbolScale
					d.Confidence = clampInt(d.Confidence-int(math.Round(symbolNewsAdverse*12)), 35, 99)
					if signal, ok := newsSnapshot.SymbolSignals[d.Symbol]; ok {
						d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(
							" | news_symbol=%.2f sym_impact=%.2f sym_sent=%.2f cred=%.2f scale=%.2f",
							symbolNewsAdverse, signal.Impact, signal.Sentiment, newsCredibility, symbolScale,
						))
					}
				}
			}
			signedNotional := d.PositionSizeUSD
			if wantSide == "short" {
				signedNotional = -signedNotional
			}
			if netLimitUSD > 0 {
				projectedNet := plannedNetExposure + signedNotional
				if math.Abs(projectedNet) > netLimitUSD {
					sameDir := (signedNotional >= 0 && plannedNetExposure >= 0) || (signedNotional <= 0 && plannedNetExposure <= 0)
					remaining := netLimitUSD - math.Abs(plannedNetExposure)
					if sameDir {
						if remaining <= 0 {
							filtered = append(filtered, decision.Decision{
								Action: "wait",
								Reasoning: fmt.Sprintf(
									"Execution overlay blocked %s: net exposure %.0f exceeds limit %.0f",
									d.Symbol, plannedNetExposure, netLimitUSD,
								),
							})
							continue
						}
						capped := math.Min(math.Abs(signedNotional), remaining)
						if capped < math.Abs(signedNotional) {
							d.PositionSizeUSD = capped
							signedNotional = math.Copysign(capped, signedNotional)
							d.Reasoning = strings.TrimSpace(d.Reasoning + fmt.Sprintf(" | net_cap=%.0f", capped))
							projectedNet = plannedNetExposure + signedNotional
						}
					}
					if math.Abs(projectedNet) > netLimitUSD {
						filtered = append(filtered, decision.Decision{
							Action: "wait",
							Reasoning: fmt.Sprintf(
								"Execution overlay blocked %s: projected net exposure %.0f exceeds limit %.0f",
								d.Symbol, projectedNet, netLimitUSD,
							),
						})
						continue
					}
				}
			}

			if d.PositionSizeUSD < 100 {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: capped size below minimum tradable notional", d.Symbol),
				})
				continue
			}

			if d.Confidence == 0 {
				d.Confidence = minConfidence
			}
			d.Confidence = clampInt(d.Confidence, 45, 99)
			if d.Confidence < minConfidence {
				filtered = append(filtered, decision.Decision{
					Action:    "wait",
					Reasoning: fmt.Sprintf("Execution overlay blocked %s: confidence %d below minimum %d", d.Symbol, d.Confidence, minConfidence),
				})
				continue
			}
			at.trackPlannedNewsBias(d.Symbol, wantSide, clampFloat(newsSupportScore, -1, 1))
			filtered = append(filtered, d)
			plannedAdds++
			plannedExposure += d.PositionSizeUSD
			plannedHeatUSD += d.PositionSizeUSD * riskPctPerNotional
			plannedNetExposure += signedNotional
			availableAfterPlans -= d.PositionSizeUSD
			plannedOpenSides[d.Symbol] = wantSide
		default:
			filtered = append(filtered, d)
		}
	}

	if len(filtered) == 0 {
		fullDecision.Decisions = []decision.Decision{
			{Action: "wait", Reasoning: "Execution overlay blocked all orders this cycle"},
		}
		return
	}
	fullDecision.Decisions = filtered
}

func (at *AutoTrader) multiFactorExitDecisions(ctx *decision.Context, factors map[string]equityFactorSnapshot) []decision.Decision {
	decisions := make([]decision.Decision, 0, len(ctx.Positions))

	stopOutPct := math.Max(1.0, at.config.RiskPerTradePct*100*1.8)
	lockThreshold := at.config.ProfitLockThreshold
	if lockThreshold <= 0 {
		lockThreshold = 1.25
	}

	for _, pos := range ctx.Positions {
		symbol := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		side := strings.ToLower(strings.TrimSpace(pos.Side))
		key := symbol + "_" + side

		heldCycles := at.callCount - at.positionEntryCycle[key]
		if heldCycles < 0 {
			heldCycles = 0
		}
		peakPnL := at.positionPeakPnLPct[key]
		curPnL := pos.UnrealizedPnLPct
		if curPnL > peakPnL {
			peakPnL = curPnL
			at.positionPeakPnLPct[key] = curPnL
		}

		reason := ""
		action := ""
		if curPnL <= -stopOutPct {
			reason = fmt.Sprintf("Multi-factor risk exit: stop threshold reached (%.2f%% <= -%.2f%%)", curPnL, stopOutPct)
		}
		if reason == "" && peakPnL >= lockThreshold && curPnL <= peakPnL*0.55 {
			reason = fmt.Sprintf("Multi-factor profit lock exit: drawdown from peak %.2f%% to %.2f%%", peakPnL, curPnL)
		}
		if reason == "" && at.config.MaxHoldingCycles > 0 && heldCycles >= at.config.MaxHoldingCycles {
			reason = fmt.Sprintf("Multi-factor time exit: max holding cycles reached (%d)", heldCycles)
		}
		if reason == "" {
			if snap, ok := factors[symbol]; ok {
				if side == "long" && snap.Total < -0.15 {
					reason = fmt.Sprintf("Multi-factor reversal exit: long score flipped to %.2f", snap.Total)
				}
				if side == "short" && snap.Total > 0.15 {
					reason = fmt.Sprintf("Multi-factor reversal exit: short score flipped to %.2f", snap.Total)
				}
			}
		}

		if reason == "" {
			continue
		}
		if side == "long" {
			action = "close_long"
		} else {
			action = "close_short"
		}
		decisions = append(decisions, decision.Decision{
			Symbol:    symbol,
			Action:    action,
			Reasoning: reason,
		})
	}

	return decisions
}

func (at *AutoTrader) multiFactorEntryDecisions(ctx *decision.Context, factors map[string]equityFactorSnapshot, regime equityMarketRegime, capacity int) []decision.Decision {
	if capacity <= 0 {
		return nil
	}

	openSymbols := make(map[string]struct{}, len(ctx.Positions))
	for _, pos := range ctx.Positions {
		symbol := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		openSymbols[symbol] = struct{}{}
	}

	ranked := make([]equityFactorSnapshot, 0, len(factors))
	for _, snap := range factors {
		ranked = append(ranked, snap)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a := math.Abs(ranked[i].Total) * (1.0 + math.Max(0, ranked[i].Quality))
		b := math.Abs(ranked[j].Total) * (1.0 + math.Max(0, ranked[j].Quality))
		return a > b
	})

	minScore := at.config.MinFactorScore
	if minScore <= 0 {
		minScore = 0.35
	}
	minConfidence := at.config.MinDecisionConfidence
	if minConfidence <= 0 {
		minConfidence = 58
	}
	maxPairCorrelation := at.config.MaxPairCorrelation
	if maxPairCorrelation <= 0 || maxPairCorrelation > 0.99 {
		maxPairCorrelation = 0.82
	}
	stressBlock := at.config.MarketStressEntryBlock
	if stressBlock <= 0 || stressBlock > 1.0 {
		stressBlock = 0.82
	}
	stressMinScale := at.config.MarketStressRiskMinScale
	if stressMinScale <= 0 || stressMinScale > 1.0 {
		stressMinScale = 0.35
	}
	plannedOpenSides := make(map[string]string, capacity)

	decisions := make([]decision.Decision, 0, capacity)
	for _, snap := range ranked {
		if len(decisions) >= capacity {
			break
		}
		if _, ok := openSymbols[snap.Symbol]; ok {
			continue
		}
		if until, ok := at.symbolCooldownUntil[snap.Symbol]; ok && at.callCount < until {
			continue
		}

		action := ""
		if snap.Total >= minScore {
			action = "open_long"
		}
		if snap.Total <= -minScore && at.config.AllowShort {
			action = "open_short"
		}
		if action == "" {
			continue
		}
		if regime.Stress >= stressBlock {
			continue
		}
		edgeScore, edgeTrades := at.getSymbolEdge(snap.Symbol)
		if edgeTrades >= 4 && edgeScore < -0.35 {
			continue
		}
		if at.config.UseMacroFilters {
			if action == "open_long" && regime.Score <= -0.25 {
				continue
			}
			if action == "open_short" && regime.Score >= 0.25 {
				continue
			}
		}
		if minLiq := at.config.MinLiquidityUSD; minLiq > 0 && snap.DollarVolume > 0 && snap.DollarVolume < minLiq {
			continue
		}
		maxCorr, corrPeer := at.maxSameSideCorrelation(ctx, snap.Symbol, action, plannedOpenSides)
		if maxCorr > maxPairCorrelation {
			continue
		}

		stop, take := at.defaultStopsAndTargets(action, snap.Price, snap.ATRPct, math.Abs(snap.Total))
		notional := at.suggestedPositionSizeUSD(ctx, snap.Symbol, action, snap.Price, stop)
		if maxCorr > 0.35 {
			corrScale := 1.0 - clampFloat((maxCorr-0.35)/0.55, 0, 0.45)
			notional *= corrScale
		}
		if regime.Stress > 0.55 {
			stressScale := 1.0 - (regime.Stress-0.55)*1.35
			stressScale = clampFloat(stressScale, stressMinScale, 1.0)
			notional *= stressScale
		}
		if notional < 100 {
			continue
		}

		confidence := clampInt(int(60+math.Abs(snap.Total)*35+math.Max(0, snap.Quality)*8+snap.Edge*10-math.Abs(maxCorr)*8), 45, 97)
		if confidence < minConfidence {
			continue
		}
		decisions = append(decisions, decision.Decision{
			Symbol:          snap.Symbol,
			Action:          action,
			Leverage:        1,
			PositionSizeUSD: notional,
			StopLoss:        stop,
			TakeProfit:      take,
			Confidence:      confidence,
			Reasoning: fmt.Sprintf("Multi-factor score=%.2f trend=%.2f momentum=%.2f relative=%.2f quality=%.2f reversion=%.2f volatility=%.2f edge=%.2f(%d) regime=%.2f stress=%.2f corr=%.2f(%s) liq=%.0f",
				snap.Total, snap.Trend, snap.Momentum, snap.Relative, snap.Quality, snap.Reversion, snap.Volatility, snap.Edge, edgeTrades, regime.Score, regime.Stress, maxCorr, corrPeer, snap.DollarVolume),
		})
		if action == "open_long" {
			plannedOpenSides[snap.Symbol] = "long"
		} else if action == "open_short" {
			plannedOpenSides[snap.Symbol] = "short"
		}
	}

	return decisions
}

func (at *AutoTrader) computeEquityFactors(ctx *decision.Context) (map[string]equityFactorSnapshot, equityMarketRegime) {
	candidateSet := make(map[string]struct{}, len(ctx.CandidateCoins)+len(ctx.Positions))
	for _, coin := range ctx.CandidateCoins {
		symbol := strings.ToUpper(strings.TrimSpace(coin.Symbol))
		if symbol != "" {
			candidateSet[symbol] = struct{}{}
		}
	}
	for _, pos := range ctx.Positions {
		symbol := strings.ToUpper(strings.TrimSpace(pos.Symbol))
		if symbol != "" {
			candidateSet[symbol] = struct{}{}
		}
	}

	returns := make([]float64, 0, len(candidateSet))
	for symbol := range candidateSet {
		if data, ok := ctx.MarketDataMap[symbol]; ok && data != nil {
			returns = append(returns, data.PriceChange4h)
		}
	}
	retMean, retStd := meanStd(returns)
	if retStd < 0.0001 {
		retStd = 1.0
	}

	breadth := 0.5
	if len(returns) > 0 {
		posCount := 0
		for _, r := range returns {
			if r > 0 {
				posCount++
			}
		}
		breadth = float64(posCount) / float64(len(returns))
	}

	regime := at.computeMarketRegime(ctx, breadth)

	factors := make(map[string]equityFactorSnapshot, len(candidateSet))
	for symbol := range candidateSet {
		data, ok := ctx.MarketDataMap[symbol]
		if !ok || data == nil || data.CurrentPrice <= 0 {
			continue
		}

		trendCore := clampFloat((data.PriceChange1h*0.55+data.PriceChange4h*0.45)/3.5, -1, 1)
		emaBias := 0.0
		if data.CurrentPrice > data.CurrentEMA20 {
			emaBias += 0.25
		} else {
			emaBias -= 0.25
		}
		if data.LongerTermContext != nil {
			if data.LongerTermContext.EMA20 > data.LongerTermContext.EMA50 {
				emaBias += 0.20
			} else {
				emaBias -= 0.20
			}
		}
		trend := clampFloat(trendCore*0.7+emaBias, -1, 1)

		macdScale := math.Max(data.CurrentPrice*0.0025, 0.0001)
		macdNorm := clampFloat(data.CurrentMACD/macdScale, -1.2, 1.2)
		intraSlope := 0.0
		if data.IntradaySeries != nil && len(data.IntradaySeries.MidPrices) >= 2 {
			start := data.IntradaySeries.MidPrices[0]
			end := data.IntradaySeries.MidPrices[len(data.IntradaySeries.MidPrices)-1]
			if start > 0 {
				intraSlope = ((end - start) / start) * 100
			}
		}
		momentum := clampFloat(macdNorm*0.6+clampFloat(intraSlope/1.2, -1, 1)*0.4, -1, 1)

		rsi := clampFloat((data.CurrentRSI7-50.0)/22.0, -1, 1)
		if data.CurrentRSI7 > 82 {
			rsi -= 0.25
		}
		if data.CurrentRSI7 < 18 {
			rsi += 0.25
		}
		rsi = clampFloat(rsi, -1, 1)

		volumeRatio := 1.0
		atrPct := 0.0
		liqQuality := 0.0
		dollarVol := 0.0
		volatility := 0.0
		if data.LongerTermContext != nil {
			if data.LongerTermContext.AverageVolume > 0 {
				volumeRatio = data.LongerTermContext.CurrentVolume / data.LongerTermContext.AverageVolume
			}
			if data.CurrentPrice > 0 {
				atrPct = (data.LongerTermContext.ATR14 / data.CurrentPrice) * 100.0
			}
			if data.LongerTermContext.ATR14 > 0 {
				atrRatio := data.LongerTermContext.ATR3 / data.LongerTermContext.ATR14
				volatility = clampFloat((atrRatio-1.0)/0.65, -1, 1) * signFloatNonZero(momentum, trend)
			}
			dollarVol = data.CurrentPrice * data.LongerTermContext.CurrentVolume
			if dollarVol > 0 {
				liqQuality = clampFloat((math.Log10(dollarVol)-5.5)/2.5, 0, 1)
			}
		}
		volume := clampFloat((volumeRatio-1.0)/1.5, -1, 1)
		volumeDirectional := volume * signFloatNonZero(trend, momentum)

		volQuality := 0.5
		if atrPct > 0 {
			volQuality = 1.0 - clampFloat(math.Abs(atrPct-1.8)/2.0, 0, 1)
		}
		quality := clampFloat((volQuality*0.55+liqQuality*0.45)*2.0-1.0, -1, 1)
		liquidity := clampFloat(liqQuality*2.0-1.0, -1, 1)

		relative := clampFloat((data.PriceChange4h-retMean)/(retStd*2.0), -1, 1)
		macroAlign := regime.Score * signFloatNonZero(trend, momentum)
		edgeScore, edgeTrades := at.getSymbolEdge(symbol)
		edgeWeight := 0.0
		if edgeTrades >= 2 {
			edgeWeight = clampFloat(float64(edgeTrades)/10.0, 0.25, 1.0)
		}
		edge := clampFloat(edgeScore*edgeWeight, -1, 1)
		reversion := 0.0
		if data.IntradaySeries != nil && len(data.IntradaySeries.MidPrices) >= 8 {
			intradayMean, intradayStd := meanStd(data.IntradaySeries.MidPrices)
			if intradayStd > 0 {
				z := (data.CurrentPrice - intradayMean) / intradayStd
				reversion = clampFloat(-z/2.5, -1, 1)
				if math.Abs(trend) > 0.55 {
					reversion *= 0.5
				}
			}
		}

		trendWeight := 0.22
		momentumWeight := 0.18
		reversionWeight := 0.07
		if math.Abs(regime.Score) >= 0.35 {
			trendWeight += 0.05
			momentumWeight += 0.03
			reversionWeight -= 0.04
		} else {
			trendWeight -= 0.02
			reversionWeight += 0.03
		}

		total := trendWeight*trend +
			momentumWeight*momentum +
			0.09*rsi +
			0.09*relative +
			0.08*volumeDirectional +
			0.09*macroAlign +
			0.10*quality +
			reversionWeight*reversion +
			0.05*volatility +
			0.07*liquidity +
			0.08*edge
		if quality < -0.25 {
			total *= 0.7
		}
		if atrPct > 5.0 {
			total *= 0.78
		}
		if at.config.MinLiquidityUSD > 0 && dollarVol > 0 && dollarVol < at.config.MinLiquidityUSD {
			total *= 0.65
		}
		total = clampFloat(total, -1, 1)

		factors[symbol] = equityFactorSnapshot{
			Symbol:       symbol,
			Price:        data.CurrentPrice,
			ATRPct:       atrPct,
			Trend:        trend,
			Momentum:     momentum,
			RSI:          rsi,
			Volume:       volumeDirectional,
			Relative:     relative,
			Quality:      quality,
			Reversion:    reversion,
			Volatility:   volatility,
			Liquidity:    liquidity,
			DollarVolume: dollarVol,
			Edge:         edge,
			Macro:        macroAlign,
			Total:        total,
		}
	}

	return factors, regime
}

func (at *AutoTrader) computeMarketRegime(ctx *decision.Context, breadth float64) equityMarketRegime {
	regime := equityMarketRegime{
		Score:        0,
		Label:        "neutral",
		Breadth:      breadth,
		BenchmarkMap: make(map[string]float64),
		Stress:       0,
		Dispersion:   0,
		AbsCorr:      0,
	}

	benchTotal := 0.0
	benchCount := 0
	for _, symbol := range at.config.BenchmarkSymbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		data, ok := ctx.MarketDataMap[symbol]
		if !ok || data == nil {
			continue
		}
		score := clampFloat((data.PriceChange1h*0.6+data.PriceChange4h*0.4)/3.0, -1, 1)
		regime.BenchmarkMap[symbol] = score
		benchTotal += score
		benchCount++
	}

	benchScore := 0.0
	if benchCount > 0 {
		benchScore = benchTotal / float64(benchCount)
	}
	if at.config.UseMacroFilters {
		breadthScore := clampFloat((breadth-0.5)*2.0, -1, 1)
		regime.Score = clampFloat(benchScore*0.65+breadthScore*0.35, -1, 1)
	}

	dispersion, corrMean, atrBurst := crossSectionStressInputs(ctx)
	bearishPressure := clampFloat(-benchScore, 0, 1)
	corrPressure := clampFloat((corrMean-0.45)/0.45, 0, 1)
	dispersionPressure := clampFloat((dispersion-0.75)/2.25, 0, 1)
	atrPressure := clampFloat(atrBurst/1.0, 0, 1)
	regime.Stress = clampFloat(0.34*bearishPressure+0.30*corrPressure+0.21*dispersionPressure+0.15*atrPressure, 0, 1)
	regime.Dispersion = dispersion
	regime.AbsCorr = corrMean

	switch {
	case regime.Score >= 0.25:
		regime.Label = "bullish"
	case regime.Score <= -0.25:
		regime.Label = "bearish"
	default:
		regime.Label = "neutral"
	}
	if regime.Stress >= 0.75 {
		regime.Label += "_stress"
	}
	at.latestMarketStress = regime.Stress
	at.latestStressDispersion = regime.Dispersion
	at.latestStressCorrelation = regime.AbsCorr
	return regime
}

func (at *AutoTrader) resolveEntryPrice(ctx *decision.Context, symbol string) float64 {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if data, ok := ctx.MarketDataMap[symbol]; ok && data != nil && data.CurrentPrice > 0 {
		return data.CurrentPrice
	}
	for _, pos := range ctx.Positions {
		if strings.EqualFold(pos.Symbol, symbol) && pos.MarkPrice > 0 {
			return pos.MarkPrice
		}
	}
	return 0
}

func (at *AutoTrader) suggestedPositionSizeUSD(ctx *decision.Context, symbol, action string, entryPrice, stopLoss float64) float64 {
	if ctx == nil {
		return 0
	}
	allocation := at.suggestAllocation(ctx, symbol, action, entryPrice, stopLoss)
	if !allocation.AllowTrade {
		return 0
	}
	return allocation.RecommendedNotional
}

func (at *AutoTrader) ensureStopsAndTargets(ctx *decision.Context, d decision.Decision, entryPrice float64) (float64, float64) {
	stop := d.StopLoss
	take := d.TakeProfit
	atrPct := 0.0
	if data, ok := ctx.MarketDataMap[d.Symbol]; ok && data != nil && data.LongerTermContext != nil && entryPrice > 0 {
		atrPct = (data.LongerTermContext.ATR14 / entryPrice) * 100.0
	}
	if stop <= 0 || take <= 0 {
		stop, take = at.defaultStopsAndTargets(d.Action, entryPrice, atrPct, 0.45)
	}

	rr := riskRewardRatio(d.Action, entryPrice, stop, take)
	if rr < 1.8 {
		rewardMult := 2.0
		distance := math.Abs(entryPrice - stop)
		if distance > 0 {
			if d.Action == "open_long" {
				take = entryPrice + distance*rewardMult
			} else if d.Action == "open_short" {
				take = entryPrice - distance*rewardMult
			}
		}
	}
	return stop, take
}

func (at *AutoTrader) defaultStopsAndTargets(action string, entryPrice float64, atrPct float64, scoreAbs float64) (float64, float64) {
	stopPct := at.adaptiveStopPct(atrPct)
	rewardMult := 2.1 + math.Min(1.0, scoreAbs)
	if rewardMult < 2.0 {
		rewardMult = 2.0
	}

	switch action {
	case "open_short":
		stop := entryPrice * (1.0 + stopPct)
		take := entryPrice * (1.0 - stopPct*rewardMult)
		return stop, take
	default:
		stop := entryPrice * (1.0 - stopPct)
		take := entryPrice * (1.0 + stopPct*rewardMult)
		return stop, take
	}
}

func (at *AutoTrader) adaptiveStopPct(atrPct float64) float64 {
	if atrPct <= 0 {
		atrPct = 1.1
	}
	mult := at.config.TrailingStopATRMult
	if mult <= 0 {
		mult = 1.6
	}
	stopPct := (atrPct * mult) / 100.0
	stopPct = clampFloat(stopPct, 0.006, 0.04)
	return stopPct
}

func (at *AutoTrader) effectiveRiskPerTradePct(ctx *decision.Context, action string) float64 {
	base := at.config.RiskPerTradePct
	if base <= 0 {
		base = 0.0075
	}
	scale := 1.0
	if at.config.RegimeRiskScaling && ctx != nil && len(ctx.MarketDataMap) > 0 {
		breadth := marketBreadth(ctx)
		regime := at.computeMarketRegime(ctx, breadth)
		if at.config.UseMacroFilters {
			switch action {
			case "open_long":
				scale += regime.Score * 0.45
			case "open_short":
				scale -= regime.Score * 0.45
			}
			if math.Abs(regime.Score) < 0.15 {
				scale *= 0.85
			}
		}
		stressMinScale := at.config.MarketStressRiskMinScale
		if stressMinScale <= 0 || stressMinScale > 1.0 {
			stressMinScale = 0.35
		}
		stressScale := 1.0 - regime.Stress*0.78
		stressScale = clampFloat(stressScale, stressMinScale, 1.0)
		scale *= stressScale
	}

	if at.peakEquitySeen > 0 && ctx != nil && ctx.Account.StrategyEquity > 0 {
		drawdown := (at.peakEquitySeen - ctx.Account.StrategyEquity) / at.peakEquitySeen
		start := at.config.DrawdownThrottleStartPct
		if start <= 0 {
			start = 0.03
		}
		minScale := at.config.DrawdownThrottleMinScale
		if minScale <= 0 || minScale > 1.0 {
			minScale = 0.35
		}
		if drawdown > start {
			excessRatio := (drawdown - start) / start
			ddScale := 1.0 - excessRatio*0.55
			ddScale = clampFloat(ddScale, minScale, 1.0)
			scale *= ddScale
		}
	}
	scale *= at.performanceRiskScale()
	volTarget := at.config.VolatilityBrakeTargetPct
	if volTarget <= 0 {
		volTarget = 0.008
	}
	volScaleMin := at.config.VolatilityBrakeMinScale
	if volScaleMin <= 0 || volScaleMin > 1.0 {
		volScaleMin = 0.45
	}
	volLookback := at.config.VolatilityBrakeLookback
	if volLookback <= 0 {
		volLookback = 40
	}
	eqVol := at.realizedEquityVolPct()
	if eqVol > 0 {
		ratio := volTarget / eqVol
		if ratio < 1.0 {
			scale *= clampFloat(ratio, volScaleMin, 1.0)
		} else if len(at.recentEquity) >= volLookback/2 {
			boost := 1.0 + math.Min(0.15, (ratio-1.0)*0.22)
			scale *= clampFloat(boost, 1.0, 1.15)
		}
	}
	kellyScale := at.kellyRiskScale()
	at.latestKellyScale = kellyScale
	scale *= kellyScale
	newsScale := 1.0
	if at.config.UseNewsRisk && ctx != nil {
		snapshot := at.currentNewsSnapshot(collectNewsWatchSymbols(ctx, nil))
		newsScale = at.newsRiskScale(action, snapshot)
		if snapshot != nil {
			at.latestNewsSentiment = snapshot.MarketSentiment
			at.latestNewsImpact = snapshot.MarketImpact
		}
	}
	at.latestNewsScale = newsScale
	scale *= newsScale

	scale = clampFloat(scale, 0.25, 1.45)
	return clampFloat(base*scale, 0.0015, 0.025)
}

func (at *AutoTrader) estimateOpenPortfolioHeatUSD(ctx *decision.Context) float64 {
	if ctx == nil {
		return 0
	}
	total := 0.0
	for _, pos := range ctx.Positions {
		notional := math.Abs(pos.Quantity) * pos.MarkPrice
		if notional <= 0 {
			continue
		}
		riskPct := at.estimateRiskPctPerNotional(ctx, pos.Symbol, pos.MarkPrice, 0)
		total += notional * riskPct
	}
	return total
}

func (at *AutoTrader) estimateRiskPctPerNotional(ctx *decision.Context, symbol string, entryPrice, stopLoss float64) float64 {
	if entryPrice > 0 && stopLoss > 0 {
		riskPct := math.Abs(entryPrice-stopLoss) / entryPrice
		if riskPct > 0 {
			return clampFloat(riskPct, 0.003, 0.08)
		}
	}

	atrPct := 0.0
	data := lookupMarketData(ctx, symbol)
	if data != nil && data.CurrentPrice > 0 && data.LongerTermContext != nil && data.LongerTermContext.ATR14 > 0 {
		atrPct = (data.LongerTermContext.ATR14 / data.CurrentPrice) * 100.0
	}
	fallback := at.adaptiveStopPct(atrPct)
	if fallback <= 0 {
		fallback = 0.012
	}
	return clampFloat(fallback, 0.003, 0.08)
}

func (at *AutoTrader) estimateDollarVolume(ctx *decision.Context, symbol string) float64 {
	data := lookupMarketData(ctx, symbol)
	if data == nil || data.LongerTermContext == nil {
		return 0
	}
	if data.CurrentPrice <= 0 || data.LongerTermContext.CurrentVolume <= 0 {
		return 0
	}
	return data.CurrentPrice * data.LongerTermContext.CurrentVolume
}

func (at *AutoTrader) maxSameSideCorrelation(ctx *decision.Context, symbol, action string, plannedOpenSides map[string]string) (float64, string) {
	if ctx == nil {
		return 0, ""
	}
	targetReturns := symbolReturnSeries(ctx, symbol)
	if len(targetReturns) < 8 {
		return 0, ""
	}

	wantSide := "long"
	if action == "open_short" {
		wantSide = "short"
	}

	maxCorr := 0.0
	peerSymbol := ""
	checkPeer := func(candidate string) {
		if strings.EqualFold(candidate, symbol) {
			return
		}
		corr := absPearsonCorrelation(targetReturns, symbolReturnSeries(ctx, candidate))
		if corr > maxCorr {
			maxCorr = corr
			peerSymbol = strings.ToUpper(strings.TrimSpace(candidate))
		}
	}

	for _, pos := range ctx.Positions {
		if strings.ToLower(strings.TrimSpace(pos.Side)) != wantSide {
			continue
		}
		checkPeer(pos.Symbol)
	}
	for peer, side := range plannedOpenSides {
		if strings.ToLower(strings.TrimSpace(side)) != wantSide {
			continue
		}
		checkPeer(peer)
	}

	return maxCorr, peerSymbol
}

func lookupMarketData(ctx *decision.Context, symbol string) *market.Data {
	if ctx == nil {
		return nil
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	if data, ok := ctx.MarketDataMap[symbol]; ok {
		return data
	}
	for key, data := range ctx.MarketDataMap {
		if strings.EqualFold(strings.TrimSpace(key), symbol) {
			return data
		}
	}
	return nil
}

func symbolReturnSeries(ctx *decision.Context, symbol string) []float64 {
	data := lookupMarketData(ctx, symbol)
	if data == nil || data.IntradaySeries == nil || len(data.IntradaySeries.MidPrices) < 8 {
		return nil
	}
	return priceSeriesReturns(data.IntradaySeries.MidPrices)
}

func marketBreadth(ctx *decision.Context) float64 {
	if ctx == nil || len(ctx.MarketDataMap) == 0 {
		return 0.5
	}
	upCount := 0
	obs := 0
	for _, data := range ctx.MarketDataMap {
		if data == nil {
			continue
		}
		obs++
		if data.PriceChange4h > 0 {
			upCount++
		}
	}
	if obs == 0 {
		return 0.5
	}
	return float64(upCount) / float64(obs)
}

func crossSectionStressInputs(ctx *decision.Context) (dispersion float64, corrMean float64, atrBurst float64) {
	if ctx == nil || len(ctx.MarketDataMap) == 0 {
		return 0, 0, 0
	}

	type retSeries struct {
		liq  float64
		rets []float64
	}
	shortReturns := make([]float64, 0, len(ctx.MarketDataMap))
	atrBursts := make([]float64, 0, len(ctx.MarketDataMap))
	series := make([]retSeries, 0, len(ctx.MarketDataMap))

	for _, data := range ctx.MarketDataMap {
		if data == nil {
			continue
		}
		shortReturns = append(shortReturns, data.PriceChange1h)
		if data.LongerTermContext != nil && data.LongerTermContext.ATR14 > 0 {
			burst := (data.LongerTermContext.ATR3 / data.LongerTermContext.ATR14) - 1.0
			atrBursts = append(atrBursts, math.Max(0, burst))
		}
		if data.IntradaySeries == nil || len(data.IntradaySeries.MidPrices) < 10 {
			continue
		}
		rets := priceSeriesReturns(data.IntradaySeries.MidPrices)
		if len(rets) < 8 {
			continue
		}
		liq := 0.0
		if data.LongerTermContext != nil && data.CurrentPrice > 0 {
			liq = data.CurrentPrice * data.LongerTermContext.CurrentVolume
		}
		series = append(series, retSeries{liq: liq, rets: rets})
	}

	if len(shortReturns) > 1 {
		_, dispersion = meanStd(shortReturns)
	}
	if len(atrBursts) > 0 {
		sum := 0.0
		for _, v := range atrBursts {
			sum += v
		}
		atrBurst = sum / float64(len(atrBursts))
	}

	if len(series) < 2 {
		return dispersion, 0, atrBurst
	}
	sort.Slice(series, func(i, j int) bool {
		return series[i].liq > series[j].liq
	})
	if len(series) > 10 {
		series = series[:10]
	}

	sumCorr := 0.0
	pairs := 0
	for i := 0; i < len(series); i++ {
		for j := i + 1; j < len(series); j++ {
			sumCorr += absPearsonCorrelation(series[i].rets, series[j].rets)
			pairs++
		}
	}
	if pairs > 0 {
		corrMean = sumCorr / float64(pairs)
	}
	return dispersion, corrMean, atrBurst
}

func priceSeriesReturns(prices []float64) []float64 {
	if len(prices) < 3 {
		return nil
	}
	returns := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		prev := prices[i-1]
		curr := prices[i]
		if prev <= 0 || curr <= 0 {
			continue
		}
		returns = append(returns, (curr-prev)/prev)
	}
	return returns
}

func absPearsonCorrelation(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < 6 {
		return 0
	}
	a = a[len(a)-n:]
	b = b[len(b)-n:]

	meanA := 0.0
	meanB := 0.0
	for i := 0; i < n; i++ {
		meanA += a[i]
		meanB += b[i]
	}
	meanA /= float64(n)
	meanB /= float64(n)

	varAB := 0.0
	varA := 0.0
	varB := 0.0
	for i := 0; i < n; i++ {
		da := a[i] - meanA
		db := b[i] - meanB
		varAB += da * db
		varA += da * da
		varB += db * db
	}
	if varA <= 0 || varB <= 0 {
		return 0
	}
	corr := varAB / math.Sqrt(varA*varB)
	if math.IsNaN(corr) || math.IsInf(corr, 0) {
		return 0
	}
	if corr < 0 {
		corr = -corr
	}
	return clampFloat(corr, 0, 1)
}

func estimateGrossExposure(positions []decision.PositionInfo) float64 {
	total := 0.0
	for _, pos := range positions {
		qty := math.Abs(pos.Quantity)
		if qty <= 0 || pos.MarkPrice <= 0 {
			continue
		}
		total += qty * pos.MarkPrice
	}
	return total
}

func estimateNetExposure(positions []decision.PositionInfo) float64 {
	total := 0.0
	for _, pos := range positions {
		qty := math.Abs(pos.Quantity)
		if qty <= 0 || pos.MarkPrice <= 0 {
			continue
		}
		notional := qty * pos.MarkPrice
		side := strings.ToLower(strings.TrimSpace(pos.Side))
		if side == "short" {
			total -= notional
		} else {
			total += notional
		}
	}
	return total
}

func meanStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	if len(values) == 1 {
		return mean, 0
	}
	var acc float64
	for _, v := range values {
		d := v - mean
		acc += d * d
	}
	return mean, math.Sqrt(acc / float64(len(values)))
}

func riskRewardRatio(action string, entry, stop, take float64) float64 {
	if entry <= 0 || stop <= 0 || take <= 0 {
		return 0
	}
	switch action {
	case "open_short":
		risk := stop - entry
		reward := entry - take
		if risk <= 0 {
			return 0
		}
		return reward / risk
	default:
		risk := entry - stop
		reward := take - entry
		if risk <= 0 {
			return 0
		}
		return reward / risk
	}
}

func signFloatNonZero(primary, secondary float64) float64 {
	if primary > 0 {
		return 1
	}
	if primary < 0 {
		return -1
	}
	if secondary > 0 {
		return 1
	}
	if secondary < 0 {
		return -1
	}
	return 0
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
