package allocator

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"northstar/selector"
)

type PortfolioAllocator struct{}

func New() *PortfolioAllocator {
	return &PortfolioAllocator{}
}

func Default() *PortfolioAllocator {
	return New()
}

func (a *PortfolioAllocator) Allocate(input Input) Result {
	cfg := normalizeConfig(input.Config)
	result := Result{
		SchemaVersion:        SchemaVersion,
		AllocatorVersion:     AllocatorVersion,
		Symbol:               strings.ToUpper(strings.TrimSpace(input.Symbol)),
		VolatilityAdjustment: 1.0,
		ConfidenceAdjustment: 1.0,
		RegimeAdjustment:     1.0,
		DrawdownAdjustment:   1.0,
		CapacityAdjustment:   1.0,
		Selection:            input.Selection,
	}
	if input.Selection != nil {
		result.Timestamp = input.Selection.Timestamp
	}

	equity := sizingEquity(input.Account)
	if equity <= 0 {
		result.Warnings = append(result.Warnings, "non_positive_sizing_equity")
		result.SizingReason = "allocation blocked because sizing equity is unavailable"
		return finalizeResult(result)
	}
	if !input.IncreasesExposure {
		result.Warnings = append(result.Warnings, "allocator_only_handles_risk_increasing_actions")
		result.SizingReason = "allocation skipped because the request does not increase exposure"
		return finalizeResult(result)
	}
	if input.EntryPrice <= 0 || input.CurrentPrice <= 0 {
		result.Warnings = append(result.Warnings, "missing_entry_price")
		result.SizingReason = "allocation blocked because current or entry price is unavailable"
		return finalizeResult(result)
	}

	if input.Selection == nil {
		result.Warnings = append(result.Warnings, "missing_strategy_selection")
		result.SizingReason = "allocation blocked because strategy selection is unavailable"
		return finalizeResult(result)
	}
	if !input.Selection.AllowTrading || input.Selection.RecommendedRiskMode == selector.RiskModeNoTrade {
		result.Warnings = append(result.Warnings, "selector_no_trade")
		result.SizingReason = "allocation blocked because strategy selection recommends no trade"
		return finalizeResult(result)
	}

	baseRiskBudget := equity * cfg.BaseRiskPerTradePct
	baseNotional := equity * cfg.FallbackPositionPct
	if cfg.DynamicSizing && input.StopLoss > 0 {
		riskPerShare := math.Abs(input.EntryPrice - input.StopLoss)
		if riskPerShare > 0 {
			baseNotional = (baseRiskBudget / riskPerShare) * input.EntryPrice
		}
	}
	result.BaseRiskBudget = sanitize(baseRiskBudget)
	result.BaseNotional = sanitize(baseNotional)
	if baseNotional <= 0 {
		result.Warnings = append(result.Warnings, "non_positive_base_notional")
		result.SizingReason = "allocation blocked because the base notional is non-positive"
		return finalizeResult(result)
	}

	result.VolatilityAdjustment = volatilityAdjustment(input.ATR14Pct, input.RealizedVol20, cfg, input.Selection)
	result.ConfidenceAdjustment = confidenceAdjustment(input.Selection.Confidence)
	result.RegimeAdjustment = regimeAdjustment(input.Selection)
	result.DrawdownAdjustment = drawdownAdjustment(input.Account, cfg)

	scaledNotional := baseNotional *
		result.VolatilityAdjustment *
		result.ConfidenceAdjustment *
		result.RegimeAdjustment *
		result.DrawdownAdjustment

	capPosition := maxFloat(0, equity*cfg.MaxPositionPct-input.Account.CurrentSymbolExposure)
	capGross := maxFloat(0, equity*cfg.MaxGrossExposurePct-input.Account.CurrentGrossExposure)
	capNet := maxFloat(0, remainingNetCapacity(input.Action, equity*cfg.MaxNetExposurePct, input.Account.CurrentNetExposure))
	capCash := maxFloat(0, input.Account.AvailableBalance*cfg.CashBufferPct)
	capOneShare := input.EntryPrice

	maxAllowed := minPositive(capPosition, capGross, capNet, capCash)
	if maxAllowed <= 0 {
		result.Warnings = append(result.Warnings, "portfolio_capacity_exhausted")
		result.SizingReason = "allocation blocked because portfolio or cash capacity is exhausted"
		return finalizeResult(result)
	}
	if maxAllowed < capOneShare {
		result.Warnings = append(result.Warnings, "insufficient_capacity_for_one_share")
		result.SizingReason = "allocation blocked because capacity is below one share notional"
		return finalizeResult(result)
	}
	result.CapacityAdjustment = clamp(maxAllowed/baseNotional, 0, 1)

	finalNotional := math.Min(scaledNotional, maxAllowed)
	if finalNotional < cfg.MinTradeNotional {
		result.Warnings = append(result.Warnings, "below_min_trade_notional")
		result.SizingReason = fmt.Sprintf("allocation blocked because final notional %.2f is below minimum %.2f", finalNotional, cfg.MinTradeNotional)
		return finalizeResult(result)
	}
	if finalNotional < capOneShare {
		result.Warnings = append(result.Warnings, "below_one_share_notional")
		result.SizingReason = fmt.Sprintf("allocation blocked because final notional %.2f is below one share price %.2f", finalNotional, capOneShare)
		return finalizeResult(result)
	}

	result.AllowTrade = true
	result.RecommendedNotional = sanitize(finalNotional)
	result.RecommendedQuantity = sanitize(finalNotional / input.EntryPrice)
	result.TargetPositionPct = sanitize((input.Account.CurrentSymbolExposure + finalNotional) / equity)
	result.RiskBudgetUsed = sanitize(estimateRiskBudgetUsed(finalNotional, input.EntryPrice, input.StopLoss, baseRiskBudget))
	result.ReducedSize = finalNotional < baseNotional*0.999 ||
		result.VolatilityAdjustment < 0.999 ||
		result.ConfidenceAdjustment < 0.999 ||
		result.RegimeAdjustment < 0.999 ||
		result.DrawdownAdjustment < 0.999 ||
		result.CapacityAdjustment < 0.999
	result.SizingReason = buildSizingReason(result, input.Selection)
	return finalizeResult(result)
}

func normalizeConfig(cfg Config) Config {
	if cfg.BaseRiskPerTradePct <= 0 {
		cfg.BaseRiskPerTradePct = 0.0075
	}
	if cfg.FallbackPositionPct <= 0 {
		cfg.FallbackPositionPct = 0.10
	}
	if cfg.MaxPositionPct <= 0 || cfg.MaxPositionPct > 1 {
		cfg.MaxPositionPct = 0.20
	}
	if cfg.MaxGrossExposurePct <= 0 || cfg.MaxGrossExposurePct > 2 {
		cfg.MaxGrossExposurePct = 1.0
	}
	if cfg.MaxNetExposurePct <= 0 || cfg.MaxNetExposurePct > 1 {
		cfg.MaxNetExposurePct = 0.65
	}
	if cfg.CashBufferPct <= 0 || cfg.CashBufferPct > 1 {
		cfg.CashBufferPct = 0.95
	}
	if cfg.DrawdownThrottleStartPct <= 0 {
		cfg.DrawdownThrottleStartPct = 0.03
	}
	if cfg.DrawdownThrottleMinScale <= 0 || cfg.DrawdownThrottleMinScale > 1 {
		cfg.DrawdownThrottleMinScale = 0.35
	}
	if cfg.VolatilityTargetPct <= 0 {
		cfg.VolatilityTargetPct = 0.015
	}
	if cfg.VolatilityMinScale <= 0 || cfg.VolatilityMinScale > 1 {
		cfg.VolatilityMinScale = 0.35
	}
	if cfg.MinTradeNotional <= 0 {
		cfg.MinTradeNotional = 100
	}
	return cfg
}

func sizingEquity(account AccountSnapshot) float64 {
	cap := account.StrategyEquity
	if account.AccountEquity > 0 && (cap <= 0 || account.AccountEquity < cap) {
		cap = account.AccountEquity
	}
	if cap < 0 {
		return 0
	}
	return cap
}

func volatilityAdjustment(atrPct, realizedVol float64, cfg Config, sel *selector.Selection) float64 {
	proxy := maxFloat(atrPct, realizedVol)
	scale := 1.0
	if proxy > 0 && proxy > cfg.VolatilityTargetPct {
		scale = clamp(cfg.VolatilityTargetPct/proxy, cfg.VolatilityMinScale, 1.0)
	}
	if sel != nil {
		switch sel.SelectedFamily {
		case selector.StrategyFamilyDefensive:
			scale = math.Min(scale, 0.65)
		case selector.StrategyFamilyHybrid:
			scale = math.Min(scale, 0.90)
		case selector.StrategyFamilyMeanReversion:
			scale = math.Min(scale, 0.85)
		}
		if sel.RecommendedRiskMode == selector.RiskModeReducedRisk {
			scale = math.Min(scale, 0.65)
		}
	}
	return sanitize(scale)
}

func confidenceAdjustment(confidence float64) float64 {
	if confidence <= 0 {
		return 0.35
	}
	return sanitize(clamp(0.35+confidence*0.65, 0.35, 1.0))
}

func regimeAdjustment(sel *selector.Selection) float64 {
	if sel == nil {
		return 0.50
	}
	switch sel.SelectedFamily {
	case selector.StrategyFamilyMomentum:
		return 1.0
	case selector.StrategyFamilyHybrid:
		return 0.90
	case selector.StrategyFamilyMeanReversion:
		return 0.85
	case selector.StrategyFamilyDefensive:
		return 0.65
	default:
		// Unknown strategy family: use a conservative non-zero scale rather
		// than returning 0, which would silently zero out all position sizing
		// and block all trades without any visible error.
		log.Printf("WARN: unknown strategy family %q in regimeAdjustment; using 0.50", sel.SelectedFamily)
		return 0.50
	}
}

func drawdownAdjustment(account AccountSnapshot, cfg Config) float64 {
	if account.PeakStrategyEquity <= 0 || account.StrategyEquity <= 0 || account.StrategyEquity >= account.PeakStrategyEquity {
		return 1.0
	}
	if cfg.DrawdownThrottleStartPct <= 0 {
		return 1.0
	}
	drawdown := (account.PeakStrategyEquity - account.StrategyEquity) / account.PeakStrategyEquity
	if drawdown <= cfg.DrawdownThrottleStartPct {
		return 1.0
	}
	excessRatio := (drawdown - cfg.DrawdownThrottleStartPct) / cfg.DrawdownThrottleStartPct
	return sanitize(clamp(1.0-excessRatio*0.55, cfg.DrawdownThrottleMinScale, 1.0))
}

func remainingNetCapacity(action string, limit, currentNet float64) float64 {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_short":
		return limit + currentNet
	default:
		return limit - currentNet
	}
}

func estimateRiskBudgetUsed(notional, entry, stop, fallbackRiskBudget float64) float64 {
	if notional <= 0 || entry <= 0 || stop <= 0 {
		return sanitize(fallbackRiskBudget)
	}
	riskPerShare := math.Abs(entry - stop)
	if riskPerShare <= 0 {
		return sanitize(fallbackRiskBudget)
	}
	qty := notional / entry
	return sanitize(qty * riskPerShare)
}

func buildSizingReason(result Result, sel *selector.Selection) string {
	family := ""
	if sel != nil {
		family = string(sel.SelectedFamily)
	}
	return fmt.Sprintf(
		"allocation for %s uses base_notional=%.2f, vol_adj=%.2f, conf_adj=%.2f, regime_adj=%.2f, drawdown_adj=%.2f, capacity_adj=%.2f -> final_notional=%.2f",
		family,
		result.BaseNotional,
		result.VolatilityAdjustment,
		result.ConfidenceAdjustment,
		result.RegimeAdjustment,
		result.DrawdownAdjustment,
		result.CapacityAdjustment,
		result.RecommendedNotional,
	)
}

func finalizeResult(result Result) Result {
	result.RecommendedQuantity = sanitize(result.RecommendedQuantity)
	result.RecommendedNotional = sanitize(result.RecommendedNotional)
	result.TargetPositionPct = sanitize(result.TargetPositionPct)
	result.RiskBudgetUsed = sanitize(result.RiskBudgetUsed)
	result.BaseRiskBudget = sanitize(result.BaseRiskBudget)
	result.BaseNotional = sanitize(result.BaseNotional)
	result.VolatilityAdjustment = sanitize(result.VolatilityAdjustment)
	result.ConfidenceAdjustment = sanitize(result.ConfidenceAdjustment)
	result.RegimeAdjustment = sanitize(result.RegimeAdjustment)
	result.DrawdownAdjustment = sanitize(result.DrawdownAdjustment)
	result.CapacityAdjustment = sanitize(result.CapacityAdjustment)
	result.Warnings = dedupeStrings(result.Warnings)
	return result
}

func sanitize(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
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

func maxFloat(values ...float64) float64 {
	best := 0.0
	for i, value := range values {
		if i == 0 || value > best {
			best = value
		}
	}
	return best
}

func minPositive(values ...float64) float64 {
	best := 0.0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if best == 0 || value < best {
			best = value
		}
	}
	return best
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
