package risk

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type evaluationContext struct {
	config    Config
	account   AccountSnapshot
	positions []PositionSnapshot
	market    MarketSnapshot
	order     OrderRequest
}

type orderSizing struct {
	quantity float64
	notional float64
}

type ruleFunc func(evaluationContext, orderSizing) (orderSizing, RuleResult)

func evaluateSymbolTradableRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.market.TradableKnown {
		return current, passRule("symbol_tradable", "tradability not explicitly restricted", current)
	}
	if ctx.market.Tradable {
		return current, passRule("symbol_tradable", "symbol marked tradable", current)
	}
	message := strings.TrimSpace(ctx.market.TradableReason)
	if message == "" {
		message = "symbol is not tradable"
	}
	return rejectRule("symbol_tradable", message)
}

func evaluateTradingHaltRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.market.HaltedKnown {
		return current, passRule("trading_halted", "no halt signal detected", current)
	}
	if !ctx.market.Halted {
		return current, passRule("trading_halted", "symbol not halted", current)
	}
	message := strings.TrimSpace(ctx.market.HaltReason)
	if message == "" {
		message = "symbol appears halted or unavailable for execution"
	}
	return rejectRule("trading_halted", message)
}

func evaluatePriceSanityRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	price := ctx.market.CurrentPrice
	if !isFinitePositive(price) {
		return rejectRule("price_sanity", "invalid current market price")
	}
	return current, passRule("price_sanity", fmt.Sprintf("current price %.4f is valid", price), current)
}

func evaluateAverageVolumeRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	minDollarVolume := ctx.config.MinAverageDollarVolume
	if minDollarVolume <= 0 {
		return current, passRule("min_average_volume", "minimum average volume disabled", current)
	}
	avgDollarVolume := ctx.market.AverageDollarVolume
	if avgDollarVolume <= 0 {
		return rejectRule("min_average_volume", "average dollar volume unavailable")
	}
	if avgDollarVolume < minDollarVolume {
		return rejectRule(
			"min_average_volume",
			fmt.Sprintf("average dollar volume %.0f below minimum %.0f", avgDollarVolume, minDollarVolume),
		)
	}
	return current, passRule(
		"min_average_volume",
		fmt.Sprintf("average dollar volume %.0f meets minimum %.0f", avgDollarVolume, minDollarVolume),
		current,
	)
}

func evaluateParticipationRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	maxParticipation := ctx.config.MaxParticipationRate
	if maxParticipation <= 0 {
		return current, passRule("max_participation_rate", "participation limit disabled", current)
	}
	liquidityVolume := ctx.market.CurrentVolume
	if liquidityVolume <= 0 {
		liquidityVolume = ctx.market.AverageVolume
	}
	if liquidityVolume <= 0 {
		return rejectRule("max_participation_rate", "volume unavailable for participation check")
	}
	maxQuantity := liquidityVolume * maxParticipation
	if maxQuantity <= 0 {
		return rejectRule("max_participation_rate", "no executable volume available after participation cap")
	}
	if current.quantity <= maxQuantity {
		return current, passRule(
			"max_participation_rate",
			fmt.Sprintf("participation %.4f within limit %.4f", current.quantity/liquidityVolume, maxParticipation),
			current,
		)
	}
	cappedQuantity := maxQuantity
	cappedNotional := cappedQuantity * ctx.market.CurrentPrice
	if cappedNotional <= 0 {
		return rejectRule("max_participation_rate", "participation cap reduced order below executable size")
	}
	next := orderSizing{
		quantity: cappedQuantity,
		notional: cappedNotional,
	}
	return next, reduceRule(
		"max_participation_rate",
		fmt.Sprintf("reduced order to participation cap %.4f", maxParticipation),
		next,
	)
}

func evaluateDailyLossRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_daily_loss", "daily loss gate skipped for exit order", current)
	}
	// Skip the rule only when the limit is exactly zero (equity baseline not yet
	// established) rather than using >= 0, which would also skip the rule for a
	// misconfigured positive limit value.
	if ctx.config.MaxDailyLossPct <= 0 || ctx.account.DailyLossLimit == 0 {
		return current, passRule("max_daily_loss", "daily loss limit not configured", current)
	}
	if ctx.account.DailyPnL <= ctx.account.DailyLossLimit {
		return rejectRule(
			"max_daily_loss",
			fmt.Sprintf("daily pnl %.2f breached limit %.2f", ctx.account.DailyPnL, ctx.account.DailyLossLimit),
		)
	}
	return current, passRule(
		"max_daily_loss",
		fmt.Sprintf("daily pnl %.2f above limit %.2f", ctx.account.DailyPnL, ctx.account.DailyLossLimit),
		current,
	)
}

func evaluateDrawdownStopRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_drawdown_stop", "drawdown stop skipped for exit order", current)
	}
	limit := ctx.config.MaxDrawdownStopPct
	if limit <= 0 {
		return current, passRule("max_drawdown_stop", "drawdown stop disabled", current)
	}
	metrics := calculatePortfolioMetrics(ctx, current)
	if metrics.CurrentDrawdownPct >= limit {
		return rejectRule(
			"max_drawdown_stop",
			fmt.Sprintf("current drawdown %s breached stop %s", formatPercent(metrics.CurrentDrawdownPct), formatPercent(limit)),
		)
	}
	return current, passRule(
		"max_drawdown_stop",
		fmt.Sprintf("current drawdown %s below stop %s", formatPercent(metrics.CurrentDrawdownPct), formatPercent(limit)),
		current,
	)
}

func evaluateConcurrentPositionsRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_concurrent_positions", "concurrent position cap skipped for exit order", current)
	}
	limit := ctx.config.MaxConcurrentPositions
	if limit <= 0 {
		return current, passRule("max_concurrent_positions", "concurrent position cap disabled", current)
	}
	if hasSameSidePosition(ctx.positions, ctx.order.Symbol, ctx.order.Side) {
		return current, passRule("max_concurrent_positions", "existing same-side position already counted", current)
	}
	positionCount := countOpenPositions(ctx.positions)
	if positionCount >= limit {
		return rejectRule(
			"max_concurrent_positions",
			fmt.Sprintf("open positions %d reached limit %d", positionCount, limit),
		)
	}
	return current, passRule(
		"max_concurrent_positions",
		fmt.Sprintf("open positions %d below limit %d", positionCount, limit),
		current,
	)
}

func evaluatePositionLimitRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_position_size_per_symbol", "position size cap skipped for exit order", current)
	}
	limitPct := ctx.config.MaxPositionPct
	equity := decisionSizingEquity(ctx.account)
	if limitPct <= 0 || equity <= 0 {
		return current, passRule("max_position_size_per_symbol", "position size cap unavailable", current)
	}
	limitNotional := equity * limitPct
	if limitNotional <= 0 {
		return current, passRule("max_position_size_per_symbol", "position size cap unavailable", current)
	}
	existingNotional := sameSideExposure(ctx.positions, ctx.order.Symbol, ctx.order.Side)
	remaining := limitNotional - existingNotional
	if remaining <= 0 {
		return rejectRule(
			"max_position_size_per_symbol",
			fmt.Sprintf("existing %s exposure %.2f reached symbol cap %.2f", ctx.order.Symbol, existingNotional, limitNotional),
		)
	}
	if current.notional <= remaining {
		return current, passRule(
			"max_position_size_per_symbol",
			fmt.Sprintf("projected symbol exposure %.2f within cap %.2f", existingNotional+current.notional, limitNotional),
			current,
		)
	}
	next := capNotional(current, remaining, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("max_position_size_per_symbol", "symbol cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_position_size_per_symbol",
		fmt.Sprintf("reduced order to remaining symbol cap %.2f", remaining),
		next,
	)
}

func evaluatePortfolioExposureRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_portfolio_exposure", "portfolio exposure cap skipped for exit order", current)
	}
	limitPct := ctx.config.MaxPortfolioExposure
	equity := decisionSizingEquity(ctx.account)
	if limitPct <= 0 || equity <= 0 {
		return current, passRule("max_portfolio_exposure", "portfolio exposure cap unavailable", current)
	}
	limitNotional := equity * limitPct
	metrics := calculatePortfolioMetrics(ctx, current)
	remaining := limitNotional - metrics.CurrentGrossExposure
	if remaining <= 0 {
		return rejectRule(
			"max_portfolio_exposure",
			fmt.Sprintf("gross exposure %.2f reached cap %.2f", metrics.CurrentGrossExposure, limitNotional),
		)
	}
	if current.notional <= remaining {
		return current, passRule(
			"max_portfolio_exposure",
			fmt.Sprintf("projected gross exposure %.2f within cap %.2f", metrics.ProjectedGrossExposure, limitNotional),
			current,
		)
	}
	next := capNotional(current, remaining, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("max_portfolio_exposure", "portfolio exposure cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_portfolio_exposure",
		fmt.Sprintf("reduced order to remaining gross exposure budget %.2f", remaining),
		next,
	)
}

func evaluateNetExposureRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_net_exposure", "net exposure cap skipped for exit order", current)
	}
	limitPct := ctx.config.MaxNetExposurePct
	equity := decisionSizingEquity(ctx.account)
	if limitPct <= 0 || equity <= 0 {
		return current, passRule("max_net_exposure", "net exposure cap unavailable", current)
	}
	sign := orderSideSign(ctx.order.Side)
	if sign == 0 {
		return rejectRule("max_net_exposure", "order side unavailable for net exposure calculation")
	}

	limitNotional := equity * limitPct
	metrics := calculatePortfolioMetrics(ctx, current)
	if math.Abs(metrics.ProjectedNetExposure) <= limitNotional {
		return current, passRule(
			"max_net_exposure",
			fmt.Sprintf("projected net exposure %s within cap %s", formatSignedMoney(metrics.ProjectedNetExposure), formatSignedMoney(limitNotional)),
			current,
		)
	}

	var remaining float64
	if sign > 0 {
		remaining = limitNotional - metrics.CurrentNetExposure
	} else {
		remaining = limitNotional + metrics.CurrentNetExposure
	}
	if remaining <= 0 {
		return rejectRule(
			"max_net_exposure",
			fmt.Sprintf("net exposure %s reached cap %s", formatSignedMoney(metrics.CurrentNetExposure), formatSignedMoney(limitNotional)),
		)
	}
	next := capNotional(current, remaining, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("max_net_exposure", "net exposure cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_net_exposure",
		fmt.Sprintf("reduced order to remaining net exposure budget %.2f", remaining),
		next,
	)
}

func evaluateSectorExposureRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_sector_exposure", "sector exposure cap skipped for exit order", current)
	}
	limitPct := ctx.config.MaxSectorExposurePct
	equity := decisionSizingEquity(ctx.account)
	if limitPct <= 0 || equity <= 0 {
		return current, passRule("max_sector_exposure", "sector exposure cap unavailable", current)
	}
	if !ctx.market.SectorKnown {
		return current, passRule("max_sector_exposure", "sector classification unavailable; sector cap skipped", current)
	}

	metrics := calculatePortfolioMetrics(ctx, current)
	limitNotional := equity * limitPct
	currentSectorExposure := metrics.ProjectedOrderSectorExposure - current.notional
	remaining := limitNotional - currentSectorExposure
	if remaining <= 0 {
		return rejectRule(
			"max_sector_exposure",
			fmt.Sprintf("sector %s exposure %.2f reached cap %.2f", ctx.market.Sector, currentSectorExposure, limitNotional),
		)
	}
	if current.notional <= remaining {
		return current, passRule(
			"max_sector_exposure",
			fmt.Sprintf("projected %s exposure %.2f within cap %.2f", ctx.market.Sector, metrics.ProjectedOrderSectorExposure, limitNotional),
			current,
		)
	}
	next := capNotional(current, remaining, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("max_sector_exposure", "sector exposure cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_sector_exposure",
		fmt.Sprintf("reduced order to remaining %s sector budget %.2f", ctx.market.Sector, remaining),
		next,
	)
}

func evaluateGrossLeverageRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_gross_leverage", "gross leverage cap skipped for exit order", current)
	}
	limit := ctx.config.MaxGrossLeverage
	accountEquity := math.Max(ctx.account.AccountEquity, 0)
	if limit <= 0 || accountEquity <= 0 {
		return current, passRule("max_gross_leverage", "gross leverage cap unavailable", current)
	}
	maxGross := accountEquity * limit
	metrics := calculatePortfolioMetrics(ctx, current)
	remaining := maxGross - metrics.CurrentGrossExposure
	if remaining <= 0 {
		return rejectRule(
			"max_gross_leverage",
			fmt.Sprintf("gross leverage budget exhausted: gross %.2f, cap %.2f", metrics.CurrentGrossExposure, maxGross),
		)
	}
	if current.notional <= remaining {
		return current, passRule(
			"max_gross_leverage",
			fmt.Sprintf("projected gross leverage %.2fx within %.2fx", metrics.ProjectedGrossExposure/accountEquity, limit),
			current,
		)
	}
	next := capNotional(current, remaining, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("max_gross_leverage", "gross leverage cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_gross_leverage",
		fmt.Sprintf("reduced order to remaining gross leverage budget %.2f", remaining),
		next,
	)
}

func evaluateCorrelationRiskRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_correlated_positions", "correlation cap skipped for exit order", current)
	}
	limit := ctx.config.MaxCorrelatedPositions
	threshold := ctx.config.MaxPairCorrelation
	if limit <= 0 || threshold <= 0 || threshold >= 1 {
		return current, passRule("max_correlated_positions", "correlation cap disabled", current)
	}
	if !ctx.market.CorrelationKnown {
		return current, passRule("max_correlated_positions", "correlation data unavailable; correlation cap skipped", current)
	}
	if ctx.market.CorrelatedPositionCount < limit {
		return current, passRule(
			"max_correlated_positions",
			fmt.Sprintf(
				"%d correlated peer(s) below limit %d (max corr %.2f)",
				ctx.market.CorrelatedPositionCount,
				limit,
				ctx.market.MaxObservedCorrelation,
			),
			current,
		)
	}
	peers := strings.Join(ctx.market.CorrelatedSymbols, ", ")
	if peers == "" && strings.TrimSpace(ctx.market.MaxObservedCorrelationSymbol) != "" {
		peers = ctx.market.MaxObservedCorrelationSymbol
	}
	if peers == "" {
		peers = "same-side portfolio peers"
	}
	return rejectRule(
		"max_correlated_positions",
		fmt.Sprintf(
			"%d correlated peer(s) at/above %.2f reached limit %d: %s",
			ctx.market.CorrelatedPositionCount,
			threshold,
			limit,
			peers,
		),
	)
}

func evaluateCashAvailabilityRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("cash_availability", "cash availability cap skipped for exit order", current)
	}
	bufferPct := ctx.config.CashBufferPct
	if bufferPct <= 0 || bufferPct > 1.0 {
		return current, passRule("cash_availability", "cash availability cap disabled", current)
	}
	available := math.Max(ctx.account.AvailableBalance, 0)
	capNotionalValue := available * bufferPct
	if capNotionalValue <= 0 {
		return rejectRule("cash_availability", "no available balance for new position")
	}
	if current.notional <= capNotionalValue {
		return current, passRule(
			"cash_availability",
			fmt.Sprintf("order notional %.2f within cash cap %.2f", current.notional, capNotionalValue),
			current,
		)
	}
	next := capNotional(current, capNotionalValue, ctx.market.CurrentPrice)
	if next.notional <= 0 {
		return rejectRule("cash_availability", "cash cap reduced order below executable size")
	}
	return next, reduceRule(
		"cash_availability",
		fmt.Sprintf("reduced order to cash availability cap %.2f", capNotionalValue),
		next,
	)
}

func evaluatePerTradeRiskRule(ctx evaluationContext, current orderSizing) (orderSizing, RuleResult) {
	if !ctx.order.IsEntry {
		return current, passRule("max_per_trade_risk", "per-trade risk cap skipped for exit order", current)
	}
	riskPct := ctx.config.MaxPerTradeRiskPct
	equity := decisionSizingEquity(ctx.account)
	if riskPct <= 0 || equity <= 0 {
		return current, passRule("max_per_trade_risk", "per-trade risk cap unavailable", current)
	}
	stopLoss := ctx.order.StopLoss
	entryPrice := ctx.market.CurrentPrice
	riskPerUnit := math.Abs(entryPrice - stopLoss)
	if !isFinitePositive(stopLoss) || !isFinitePositive(riskPerUnit) {
		return rejectRule("max_per_trade_risk", "stop loss is required for per-trade risk calculation")
	}
	allowedRiskUSD := equity * riskPct
	maxQuantity := allowedRiskUSD / riskPerUnit
	maxNotional := maxQuantity * entryPrice
	if maxNotional <= 0 {
		return rejectRule("max_per_trade_risk", "per-trade risk cap reduced order below executable size")
	}
	if current.notional <= maxNotional {
		return current, passRule(
			"max_per_trade_risk",
			fmt.Sprintf("estimated trade risk %.2f within cap %.2f", current.quantity*riskPerUnit, allowedRiskUSD),
			current,
		)
	}
	next := capNotional(current, maxNotional, entryPrice)
	if next.notional <= 0 {
		return rejectRule("max_per_trade_risk", "per-trade risk cap reduced order below executable size")
	}
	return next, reduceRule(
		"max_per_trade_risk",
		fmt.Sprintf("reduced order to per-trade risk cap %.2f", allowedRiskUSD),
		next,
	)
}

func passRule(name, message string, current orderSizing) RuleResult {
	return RuleResult{
		Name:             name,
		Status:           RulePass,
		Message:          message,
		ApprovedQuantity: current.quantity,
		ApprovedNotional: current.notional,
	}
}

func rejectRule(name, message string) (orderSizing, RuleResult) {
	return orderSizing{}, RuleResult{
		Name:    name,
		Status:  RuleReject,
		Message: message,
	}
}

func reduceRule(name, message string, current orderSizing) RuleResult {
	return RuleResult{
		Name:             name,
		Status:           RuleReduceSize,
		Message:          message,
		ApprovedQuantity: current.quantity,
		ApprovedNotional: current.notional,
	}
}

func capNotional(current orderSizing, maxNotional, price float64) orderSizing {
	if maxNotional <= 0 || !isFinitePositive(price) {
		return orderSizing{}
	}
	next := current
	next.notional = math.Min(current.notional, maxNotional)
	next.quantity = next.notional / price
	return next
}

func countOpenPositions(positions []PositionSnapshot) int {
	count := 0
	for _, pos := range positions {
		if pos.MarketValue <= 0 {
			continue
		}
		count++
	}
	return count
}

func calculatePortfolioMetrics(ctx evaluationContext, current orderSizing) PortfolioMetrics {
	metrics := PortfolioMetrics{
		SectorExposure:               make(map[string]float64),
		SectorExposurePct:            make(map[string]float64),
		OrderSector:                  strings.TrimSpace(ctx.market.Sector),
		OrderSectorKnown:             ctx.market.SectorKnown,
		PeakStrategyEquity:           sanitizeFloat(ctx.account.PeakStrategyEquity),
		MaxObservedCorrelation:       sanitizeFloat(ctx.market.MaxObservedCorrelation),
		MaxObservedCorrelationSymbol: strings.ToUpper(strings.TrimSpace(ctx.market.MaxObservedCorrelationSymbol)),
		CorrelatedPositionCount:      ctx.market.CorrelatedPositionCount,
	}
	if len(ctx.market.CorrelatedSymbols) > 0 {
		metrics.CorrelatedSymbols = append([]string(nil), ctx.market.CorrelatedSymbols...)
	}

	equity := decisionSizingEquity(ctx.account)
	metrics.CurrentGrossExposure = grossExposure(ctx.positions)
	if metrics.CurrentGrossExposure <= 0 && ctx.account.GrossMarketValue > 0 {
		metrics.CurrentGrossExposure = math.Max(ctx.account.GrossMarketValue, 0)
	}
	metrics.CurrentNetExposure = netExposure(ctx.positions)

	for _, pos := range ctx.positions {
		exposure := math.Max(pos.MarketValue, 0)
		if exposure <= 0 {
			continue
		}
		if pos.SectorKnown && strings.TrimSpace(pos.Sector) != "" {
			sector := strings.ToLower(strings.TrimSpace(pos.Sector))
			metrics.SectorExposure[sector] += exposure
		} else {
			metrics.UnclassifiedExposure += exposure
		}
	}

	metrics.ProjectedGrossExposure = metrics.CurrentGrossExposure
	metrics.ProjectedNetExposure = metrics.CurrentNetExposure
	metrics.ProjectedOrderSectorExposure = sectorExposureFor(metrics.SectorExposure, metrics.OrderSector)

	if ctx.order.IsEntry {
		metrics.ProjectedGrossExposure += current.notional
		metrics.ProjectedNetExposure += float64(orderSideSign(ctx.order.Side)) * current.notional
		if metrics.OrderSectorKnown && metrics.OrderSector != "" {
			metrics.ProjectedOrderSectorExposure += current.notional
		}
	}

	if equity > 0 {
		metrics.CurrentGrossExposurePct = sanitizeFloat(metrics.CurrentGrossExposure / equity)
		metrics.ProjectedGrossExposurePct = sanitizeFloat(metrics.ProjectedGrossExposure / equity)
		metrics.CurrentNetExposurePct = sanitizeFloat(metrics.CurrentNetExposure / equity)
		metrics.ProjectedNetExposurePct = sanitizeFloat(metrics.ProjectedNetExposure / equity)
		for sector, exposure := range metrics.SectorExposure {
			metrics.SectorExposurePct[sector] = sanitizeFloat(exposure / equity)
		}
		metrics.UnclassifiedExposurePct = sanitizeFloat(metrics.UnclassifiedExposure / equity)
		metrics.ProjectedOrderSectorExposurePct = sanitizeFloat(metrics.ProjectedOrderSectorExposure / equity)
	}

	largestSector := ""
	largestExposure := 0.0
	for sector, exposure := range metrics.SectorExposure {
		if exposure > largestExposure {
			largestSector = sector
			largestExposure = exposure
		}
	}
	metrics.LargestSector = largestSector
	metrics.LargestSectorExposure = sanitizeFloat(largestExposure)
	if equity > 0 {
		metrics.LargestSectorExposurePct = sanitizeFloat(largestExposure / equity)
	}

	if ctx.account.PeakStrategyEquity > 0 && ctx.account.StrategyEquity > 0 && ctx.account.PeakStrategyEquity >= ctx.account.StrategyEquity {
		metrics.CurrentDrawdownPct = sanitizeFloat((ctx.account.PeakStrategyEquity - ctx.account.StrategyEquity) / ctx.account.PeakStrategyEquity)
	}

	if len(metrics.CorrelatedSymbols) > 0 {
		sort.Strings(metrics.CorrelatedSymbols)
	}

	return metrics
}

func hasSameSidePosition(positions []PositionSnapshot, symbol, side string) bool {
	for _, pos := range positions {
		if !strings.EqualFold(pos.Symbol, symbol) || !strings.EqualFold(pos.Side, side) {
			continue
		}
		if pos.MarketValue > 0 || pos.Quantity > 0 {
			return true
		}
	}
	return false
}

func grossExposure(positions []PositionSnapshot) float64 {
	total := 0.0
	for _, pos := range positions {
		total += math.Max(pos.MarketValue, 0)
	}
	return total
}

func netExposure(positions []PositionSnapshot) float64 {
	total := 0.0
	for _, pos := range positions {
		exposure := math.Max(pos.MarketValue, 0)
		if exposure <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(pos.Side), "short") {
			total -= exposure
			continue
		}
		total += exposure
	}
	return total
}

func sameSideExposure(positions []PositionSnapshot, symbol, side string) float64 {
	total := 0.0
	for _, pos := range positions {
		if strings.EqualFold(pos.Symbol, symbol) && strings.EqualFold(pos.Side, side) {
			total += math.Max(pos.MarketValue, 0)
		}
	}
	return total
}

func sectorExposureFor(exposures map[string]float64, sector string) float64 {
	if len(exposures) == 0 {
		return 0
	}
	return exposures[strings.ToLower(strings.TrimSpace(sector))]
}

func decisionSizingEquity(account AccountSnapshot) float64 {
	cap := account.StrategyEquity
	if account.AccountEquity > 0 && (cap <= 0 || account.AccountEquity < cap) {
		cap = account.AccountEquity
	}
	if cap < 0 {
		return 0
	}
	return cap
}

func isFinitePositive(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func orderSideSign(side string) int {
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		return -1
	}
	if strings.EqualFold(strings.TrimSpace(side), "long") {
		return 1
	}
	return 0
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.2f%%", value*100.0)
}

func formatSignedMoney(value float64) string {
	if value >= 0 {
		return fmt.Sprintf("+%.2f", value)
	}
	return fmt.Sprintf("%.2f", value)
}
