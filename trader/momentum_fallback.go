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
	"northstar/selector"
	"os"
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
			// Trend-reversal exit — require MEANINGFUL reversal to avoid
			// whipsaw overtrading. Previous bare sign-flip caused whipsaws;
			// original over-correction (require +0.5% profit) caused us to
			// hold losers into the full stop-loss even when trend had
			// clearly turned against us.
			//
			// Final rules:
			//   - MACD must be clearly past zero in the opposite direction
			//     (|macd| > 0.005), not just a technical crossing.
			//   - 1h price change must have moved >=0.4% in the opposite
			//     direction — distinguishes a real reversal from noise.
			//   - Position must be out of the |uPnL| < 0.3% "noise zone"
			//     in EITHER direction. Locks in winners that show clear
			//     reversal; cuts losers that show clear reversal (before
			//     they grind to the full -1.5% stop).
			out_of_noise := math.Abs(pos.UnrealizedPnLPct) >= 0.3
			if pos.Side == "long" && out_of_noise &&
				data.CurrentMACD < -0.005 && data.PriceChange1h < -0.4 {
				closeAction = "close_long"
				reason = "Momentum-only exit: meaningful trend reversal against long"
			}
			if pos.Side == "short" && out_of_noise &&
				data.CurrentMACD > 0.005 && data.PriceChange1h > 0.4 {
				closeAction = "close_short"
				reason = "Momentum-only exit: meaningful trend reversal against short"
			}
		}

		if closeAction != "" {
			decisions = append(decisions, decision.Decision{
				Symbol:    pos.Symbol,
				Action:    closeAction,
				Reasoning: reason,
			})
			// Cooldown is set downstream by updateExecutionState() using
			// config.SymbolCooldownCycles. We just log here for visibility.
			log.Printf(" [COOLDOWN-PENDING] %s will be cooled for %d cycles after %s",
				pos.Symbol, at.config.SymbolCooldownCycles, closeAction)
		}
	}

	if len(decisions) == 0 && len(ctx.Positions) == 0 {
		fallback, ok := at.buildMomentumFallbackDecision(ctx, at.config.MomentumMinScore, at.config.FallbackPositionPct)
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

	fallback, ok := at.buildMomentumFallbackDecision(ctx, at.config.MomentumMinScore, at.config.FallbackPositionPct)
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

// momentumSignal captures the winning scoring snapshot for a symbol.
// RawScore is the pre-boost strength; Score is post regime-boost. StopPct is
// the ATR-scaled stop distance (fraction, e.g. 0.015 = 1.5%) used for sizing.
type momentumSignal struct {
	Symbol      string
	Price       float64
	Score       float64
	RawScore    float64
	TrendScore  float64
	MACD        float64
	RSI7        float64
	ShortBias   bool
	Regime      string
	Family      selector.StrategyFamily
	RegimeBoost float64
	Confidence  float64
	StopPct     float64
}

func (at *AutoTrader) selectBestMomentumSignal(ctx *decision.Context, minScore float64) (momentumSignal, bool) {
	if minScore <= 0 {
		minScore = 1.25
	}

	// Track why each symbol was rejected so operators can see whether the
	// gates are too tight. A summary line is logged at the end of the loop.
	rejectReasons := make(map[string]string, len(ctx.MarketDataMap))
	var bestScore float64

	best := momentumSignal{}
	found := false
	for symbol, data := range ctx.MarketDataMap {
		if data == nil || data.CurrentPrice <= 0 {
			rejectReasons[symbol] = "no_price"
			continue
		}

		// Symbol cooldown: after closing a position on this symbol, block
		// re-entry for a window of cycles. Stops the chop/whipsaw loop where
		// the same symbol keeps flip-flopping on MACD sign changes and each
		// round-trip eats fees + slippage. The cooldown is set in
		// buildMomentumOnlyDecision when we emit a close action.
		if until, ok := at.symbolCooldownUntil[symbol]; ok && at.callCount < until {
			rejectReasons[symbol] = fmt.Sprintf("cooldown(%d_left)", until-at.callCount)
			continue
		}

		// Regime-aware filter: honor the selector's AllowTrading flag and
		// apply a family-specific boost so momentum signals align with the
		// classifier output rather than ignoring it.
		var selection *selector.Selection
		if data.Selections != nil {
			selection = data.Selections.Selection("4h")
			if selection == nil {
				selection = data.Selections.Selection("3m")
			}
		}
		family := selector.StrategyFamilyMomentum
		regimeBoost := 1.0
		regimeName := ""
		selectionConfidence := 0.5
		if selection != nil {
			family = selection.SelectedFamily
			regimeName = string(selection.Regime)
			selectionConfidence = selection.Confidence
			if !selection.AllowTrading {
				rejectReasons[symbol] = fmt.Sprintf("no_trade_regime(%s,conf=%.2f)", selection.Regime, selection.Confidence)
				continue
			}
			switch family {
			case selector.StrategyFamilyMomentum:
				regimeBoost = 1.15
			case selector.StrategyFamilyHybrid:
				regimeBoost = 1.00
			case selector.StrategyFamilyDefensive:
				regimeBoost = 0.85
			case selector.StrategyFamilyMeanReversion:
				// Previously 0.70 (heavily penalized). Bumped to 1.00 so MR
				// signals compete on equal footing with momentum when the
				// regime classifier explicitly allows the trade. Without
				// this, the ~1,400 MR signals/day observed in production
				// all got filtered out below minScore.
				regimeBoost = 1.00
			default:
				regimeBoost = 0.60
			}
			confWeight := 0.7 + 0.3*clampFloat(selection.Confidence, 0.0, 1.0)
			regimeBoost *= confWeight
		}

		// Dual scoring: momentum path (trend-following) vs. mean-reversion
		// path (fade stretches). The regime classifier picks the family,
		// and we score/direction accordingly. If neither path qualifies,
		// skip the symbol.
		var directionScore, rawScore, trend float64
		if family == selector.StrategyFamilyMeanReversion {
			// Mean-reversion entry: fade RSI extremes.
			// Direction is OPPOSITE of current stretch (overbought → short,
			// oversold → long). Requires a clear extreme; RSI 40-60 is no-go.
			rsiDev := data.CurrentRSI7 - 50.0
			if math.Abs(rsiDev) < 15.0 {
				rejectReasons[symbol] = fmt.Sprintf("mr_rsi_too_tame(rsi=%.1f)", data.CurrentRSI7)
				continue // not stretched enough; no fade setup
			}
			// directionScore sign: positive = long (oversold fade up), negative = short (overbought fade down).
			directionScore = -rsiDev / 30.0 // normalize: |rsiDev|=15 -> |dir|=0.5, |rsiDev|=45 -> |dir|=1.5

			// MACD confluence bonus: add when MACD is turning with the fade.
			macdConfluence := 0.0
			if directionScore > 0 && data.CurrentMACD > 0 {
				macdConfluence = 0.25 // long fade + MACD turning up
			} else if directionScore < 0 && data.CurrentMACD < 0 {
				macdConfluence = 0.25 // short fade + MACD turning down
			}

			// Extremity quality: more stretched = higher quality (opposite of momentum).
			rsiExtremity := math.Min(1.0, math.Abs(rsiDev)/30.0)
			rawScore = math.Abs(directionScore) + (rsiExtremity * 0.6) + macdConfluence
		} else {
			// Momentum entry: trend + MACD bias.
			trend = data.PriceChange1h*0.55 + data.PriceChange4h*0.45
			macdBias := 0.0
			if data.CurrentMACD > 0 {
				macdBias = 0.8
			} else if data.CurrentMACD < 0 {
				macdBias = -0.8
			}
			directionScore = trend + macdBias
			// Lowered from 0.4 → 0.25 to catch modest momentum setups where
			// MACD is only mildly positive/negative. Paired with the 0.75
			// minScore cut, this roughly doubles the eligible setup count
			// without degrading quality (still requires MACD agreement).
			if math.Abs(directionScore) < 0.25 {
				rejectReasons[symbol] = fmt.Sprintf("weak_direction(dir=%.2f)", directionScore)
				continue
			}
			rsiDistance := math.Abs(data.CurrentRSI7-50.0) / 50.0
			quality := 1.0 - math.Min(1.0, rsiDistance)
			rawScore = math.Abs(directionScore) + (quality * 0.6)
		}
		score := rawScore * regimeBoost
		if score > bestScore {
			bestScore = score
		}
		if score < minScore {
			rejectReasons[symbol] = fmt.Sprintf("below_minscore(score=%.2f,fam=%s,boost=%.2f)", score, family, regimeBoost)
			continue
		}

		// Funding rate filter: skip entries aligned with crowded positioning.
		// Backtest across 2023 / 2024 / 2025-26 windows showed a meaningful
		// improvement (2024: +61% → +131% annualized; 2025-26: +293% → +360%).
		// The rate lives on market.Data.FundingRate — fraction per 8h funding
		// interval. Positive = longs paying shorts = market is crowded long.
		longBias := directionScore > 0
		longThresh := at.config.FundingRateLongFilterPct
		shortThresh := at.config.FundingRateShortFilterPct
		if longBias && longThresh > 0 && data.FundingRate > longThresh {
			rejectReasons[symbol] = fmt.Sprintf("funding_long_crowded(rate=%.5f>%.5f)", data.FundingRate, longThresh)
			continue // longs overcrowded; fade the late entry
		}
		if !longBias && shortThresh > 0 && data.FundingRate < -shortThresh {
			rejectReasons[symbol] = fmt.Sprintf("funding_short_crowded(rate=%.5f<-%.5f)", data.FundingRate, shortThresh)
			continue // shorts overcrowded
		}

		if !found || score > best.Score {
			found = true
			best = momentumSignal{
				Symbol:      symbol,
				Price:       data.CurrentPrice,
				Score:       score,
				RawScore:    rawScore,
				TrendScore:  trend,
				MACD:        data.CurrentMACD,
				RSI7:        data.CurrentRSI7,
				ShortBias:   directionScore < 0,
				Regime:      regimeName,
				Family:      family,
				RegimeBoost: regimeBoost,
				Confidence:  selectionConfidence,
				StopPct:     at.momentumStopDistancePct(data),
			}
		}
	}

	if !found && len(rejectReasons) > 0 {
		// Aggregate-count style summary so we don't spam per-symbol lines.
		// Formatted as "reason=count,reason=count ...". Sorted for stability.
		counts := make(map[string]int)
		for _, r := range rejectReasons {
			// strip parenthetical detail for aggregation (keep reason prefix)
			key := r
			if idx := strings.Index(r, "("); idx >= 0 {
				key = r[:idx]
			}
			counts[key]++
		}
		parts := make([]string, 0, len(counts))
		for k, v := range counts {
			parts = append(parts, fmt.Sprintf("%s=%d", k, v))
		}
		log.Printf(" [MOMENTUM-SCAN] no setup; symbols=%d best_score=%.2f min=%.2f rejects: %s",
			len(ctx.MarketDataMap), bestScore, minScore, strings.Join(parts, ","))
	}

	return best, found
}

func (at *AutoTrader) buildMomentumFallbackDecision(ctx *decision.Context, minScore, positionPct float64) (decision.Decision, bool) {
	// Portfolio kill switch: honor any existing entry block from loss-streak
	// pauses OR from the portfolio DD gate below. Backtest on the 2023
	// whipsaw window showed this converts the strategy's disaster-year
	// return from -48% to roughly -15% (at 12% DD halt + 14d cooldown).
	if at.openEntryBlockedUntil > at.callCount {
		return decision.Decision{}, false
	}
	at.maybeTripPortfolioKillSwitch(ctx)
	if at.openEntryBlockedUntil > at.callCount {
		return decision.Decision{}, false
	}

	candidate, ok := at.selectBestMomentumSignal(ctx, minScore)
	if !ok {
		return decision.Decision{}, false
	}

	// ML agreement check: in SHADOW mode logs the comparison; in CONFIRMED
	// mode (NORTHSTAR_ML_REQUIRE_AGREEMENT=1) blocks the trade if ML
	// disagrees with rule-based. The check returns false on disagreement
	// in confirmed mode, true otherwise (including sidecar unreachable).
	if !at.checkMLAgreement(ctx, &candidate) {
		return decision.Decision{}, false
	}

	stopPct := candidate.StopPct
	if stopPct <= 0 {
		stopPct = 0.015
	}
	// 1:3 base risk-reward. Take-profit is three times the stop distance so
	// the configured risk budget translates into a coherent expected return.
	rewardPct := stopPct * 3.0

	action := "open_long"
	stopLoss := candidate.Price * (1 - stopPct)
	takeProfit := candidate.Price * (1 + rewardPct)
	if candidate.ShortBias {
		action = "open_short"
		stopLoss = candidate.Price * (1 + stopPct)
		takeProfit = candidate.Price * (1 - rewardPct)
	}

	// Defer sizing to the shared allocator: it applies risk-per-trade,
	// drawdown throttle, regime scaling, volatility targeting, and the
	// performance feedback loop (performanceRiskScale reads recent close P&L
	// so every completed trade feeds back into the next sizing decision).
	// This replaces the previous hardcoded $250 notional floor that had
	// blocked every candidate since cycle 1.
	allocation := at.suggestAllocation(ctx, candidate.Symbol, action, candidate.Price, stopLoss)
	if !allocation.AllowTrade || allocation.RecommendedNotional <= 0 {
		log.Printf(" [ALLOC-BLOCK] %s %s: allow=%v notional=%.2f risk=%.2f reason=%s",
			candidate.Symbol, action, allocation.AllowTrade, allocation.RecommendedNotional,
			allocation.RiskBudgetUsed, strings.TrimSpace(allocation.SizingReason))
		return decision.Decision{}, false
	}

	confidence := int(math.Round(70 + candidate.Score*6))
	if confidence < 70 {
		confidence = 70
	}
	if confidence > 95 {
		confidence = 95
	}

	reason := fmt.Sprintf(
		"Momentum: regime=%s family=%s score=%.2f raw=%.2f boost=%.2f conf=%.2f trend=%.2f%% rsi7=%.1f macd=%.4f stop=%.2f%% notional=$%.2f risk=$%.2f sizing=%s",
		candidate.Regime, candidate.Family,
		candidate.Score, candidate.RawScore, candidate.RegimeBoost, candidate.Confidence,
		candidate.TrendScore, candidate.RSI7, candidate.MACD,
		stopPct*100, allocation.RecommendedNotional, allocation.RiskBudgetUsed,
		strings.TrimSpace(allocation.SizingReason),
	)
	// positionPct is retained in the signature for callers that still pass
	// at.config.FallbackPositionPct; when the allocator is disabled the
	// value feeds the allocator's FallbackPositionPct under the hood.
	_ = positionPct

	return decision.Decision{
		Symbol:          candidate.Symbol,
		Action:          action,
		Leverage:        1,
		PositionSizeUSD: allocation.RecommendedNotional,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		RiskUSD:         allocation.RiskBudgetUsed,
		Confidence:      confidence,
		Reasoning:       reason,
	}, true
}

// checkMLAgreement queries the Python ML sidecar (if enabled via env var)
// for its opinion on the rule-based candidate and returns whether the trade
// should proceed.
//
// Two modes:
//
//   - SHADOW (NORTHSTAR_ML_SHADOW=1): always returns true (proceed). ML
//     opinion is logged for A/B comparison but does NOT alter the decision.
//
//   - CONFIRMED (NORTHSTAR_ML_REQUIRE_AGREEMENT=1): returns false when ML
//     disagrees (says "wait" or picks the opposite side). The rule-based
//     candidate is skipped. This is the ML-as-filter mode — preserves the
//     proven rule-based signal generator but uses ML as a quality gate.
//
// Both modes can be active simultaneously. CONFIRMED implies SHADOW logging.
//
// Graceful degradation: if the sidecar is unreachable (timeout, circuit
// breaker open, etc.), CONFIRMED mode returns true (proceed). Never block
// live trading on infrastructure failure — fall back to rule-based alone.
//
// Safety: the HTTP client has a 500ms timeout and a 3-fail circuit breaker,
// so a dead sidecar adds at most one slow call per minute.
func (at *AutoTrader) checkMLAgreement(ctx *decision.Context, candidate *momentumSignal) bool {
	if candidate == nil {
		return true
	}
	// Per-trader config beats env vars. Env vars are still honored as a
	// fallback for quick testing but config fields are the canonical source.
	shadowOn := at.config.MLShadowEnabled ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("NORTHSTAR_ML_SHADOW")), "1")
	requireAgreement := at.config.MLRequireAgreement ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("NORTHSTAR_ML_REQUIRE_AGREEMENT")), "1")
	if !shadowOn && !requireAgreement {
		return true
	}
	data := lookupMarketData(ctx, candidate.Symbol)
	if data == nil {
		return true // can't evaluate; don't block
	}
	// Lazy-init the client on first use. Safe because this runs serialized
	// per cycle; no concurrency concern here.
	if at.mlShadowClient == nil {
		url := at.config.MLSidecarURL
		if url == "" {
			url = os.Getenv("NORTHSTAR_ML_URL")
		}
		at.mlShadowClient = NewMLSignalClient(url)
	}
	feats := buildMLFeatureMap(data, time.Now().UTC())
	resp, err := at.mlShadowClient.Score(candidate.Symbol, feats, time.Now().UnixMilli())
	ruleSide := "long"
	if candidate.ShortBias {
		ruleSide = "short"
	}
	if err != nil {
		// Sidecar down — degrade gracefully. Log once, let rule-based decide.
		if requireAgreement {
			log.Printf(" [ML-check] %s rule=%s score=%.2f | ml=UNREACHABLE (%v) — proceeding with rule-based only",
				candidate.Symbol, ruleSide, candidate.Score, err)
		} else {
			log.Printf(" [ML-shadow] %s rule=%s score=%.2f | ml=ERR %v",
				candidate.Symbol, ruleSide, candidate.Score, err)
		}
		return true
	}
	// Classify ML's verdict relative to rule-based direction.
	verdict := "disagree"
	switch {
	case resp.ActionHint == "open_long" && ruleSide == "long":
		verdict = "agree"
	case resp.ActionHint == "open_short" && ruleSide == "short":
		verdict = "agree"
	case resp.ActionHint == "wait":
		verdict = "ml-wait"
	}
	prefix := "[ML-shadow]"
	if requireAgreement {
		prefix = "[ML-check]"
	}
	log.Printf(" %s %s rule=%s(%.2f) ml=%s(score=%.2f conf=%.2f) verdict=%s model=%s",
		prefix, candidate.Symbol, ruleSide, candidate.Score,
		resp.ActionHint, resp.Score, resp.Confidence, verdict, resp.ModelVersion)

	if requireAgreement && verdict != "agree" {
		// Skip this trade. The skip is deliberate — "ML doesn't confirm"
		// acts as the quality filter we designed for the confirmed mode.
		log.Printf(" [ML-check] BLOCKING entry on %s — ML verdict=%s (require-agreement mode)",
			candidate.Symbol, verdict)
		return false
	}
	return true
}

// maybeTripPortfolioKillSwitch evaluates current account drawdown from the
// cycle's observed peak and trips `openEntryBlockedUntil` if the configured
// threshold is breached. It uses at.peakEquitySeen (already maintained by
// the main cycle) and ctx.Account.StrategyEquity so the check is accurate
// whether we're on paper or live.
//
// Backtest rationale: the momentum strategy has brutal regime dependence —
// +293% in the 2025-26 window but -66% in 2023's V-shape recovery. A
// portfolio-level halt at 12% DD with a 14-day cooldown cuts the 2023
// disaster to roughly -15% (where "stops trading after burning the first
// ~15% of the year" is a realistic operator action), while only modestly
// reducing good-regime performance. Sweep results in cmd/crypto-backtest.
func (at *AutoTrader) maybeTripPortfolioKillSwitch(ctx *decision.Context) {
	if ctx == nil {
		return
	}
	threshold := at.config.PortfolioKillSwitchDDPct
	if threshold <= 0 {
		return // kill switch disabled
	}
	peak := at.peakEquitySeen
	if peak <= 0 {
		peak = ctx.Account.StrategyEquity
	}
	if peak <= 0 {
		return
	}
	current := ctx.Account.StrategyEquity
	if current <= 0 {
		return
	}
	dd := (peak - current) / peak
	if dd < threshold {
		return
	}
	// Halt. Default cooldown: 14 days worth at this trader's scan interval.
	cooldownCycles := at.config.PortfolioKillSwitchCooldownCycles
	scanSec := int(at.config.ScanInterval / time.Second)
	if scanSec <= 0 {
		scanSec = 60
	}
	if cooldownCycles <= 0 {
		cooldownCycles = (14 * 24 * 3600) / scanSec
		if cooldownCycles < 20 {
			cooldownCycles = 20 // safety floor
		}
	}
	until := at.callCount + cooldownCycles
	if until > at.openEntryBlockedUntil {
		at.openEntryBlockedUntil = until
		log.Printf(" [KILL-SWITCH] portfolio DD %.2f%% >= %.2f%% — halting new entries for %d cycles (~%dd)",
			dd*100, threshold*100, cooldownCycles, cooldownCycles*scanSec/86400)
	}
}

// momentumStopDistancePct returns the ATR-scaled stop distance as a fraction
// of entry price (0.015 = 1.5%). Prefers Features.Vector("4h").ATR14Pct (the
// normalized source), falls back to LongerTermContext.ATR14/CurrentPrice, and
// clamps into [0.5%, 4%] so extremes don't produce nonsense stops. Returns a
// 1.5% default when no ATR data is available.
func (at *AutoTrader) momentumStopDistancePct(data *market.Data) float64 {
	const (
		defaultPct    = 0.015
		minPct        = 0.005
		maxPct        = 0.04
		atrMultiplier = 2.0
	)
	if data == nil {
		return defaultPct
	}
	atrPct := 0.0
	if data.Features != nil {
		if v := data.Features.Vector("4h"); v != nil && v.ATR14Pct > 0 {
			atrPct = v.ATR14Pct
		}
		if atrPct <= 0 {
			if v := data.Features.Vector("3m"); v != nil && v.ATR14Pct > 0 {
				atrPct = v.ATR14Pct
			}
		}
	}
	if atrPct <= 0 && data.LongerTermContext != nil && data.LongerTermContext.ATR14 > 0 && data.CurrentPrice > 0 {
		atrPct = data.LongerTermContext.ATR14 / data.CurrentPrice
	}
	if atrPct <= 0 {
		return defaultPct
	}
	stop := atrPct * atrMultiplier
	if stop < minPct {
		stop = minPct
	}
	if stop > maxPct {
		stop = maxPct
	}
	return stop
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
