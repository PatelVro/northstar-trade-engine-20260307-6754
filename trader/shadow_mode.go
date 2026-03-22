package trader

import (
	"fmt"
	"log"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"strings"
	"time"
)

const shadowDecisionHistoryLimit = 20

type shadowPosition struct {
	Symbol        string
	Side          string
	Quantity      float64
	EntryPrice    float64
	CurrentPrice  float64
	Notional      float64
	OpenedAt      time.Time
	LastMarkedAt  time.Time
	UnrealizedPnL float64
}

type shadowModeState struct {
	Available                    bool
	Active                       bool
	LastDecisionAt               time.Time
	LastDecisionSymbol           string
	LastDecisionAction           string
	LastDecisionStatus           string
	TotalDecisions               int
	WouldTradeCount              int
	BlockedCount                 int
	OpenPositions                int
	ClosedTrades                 int
	HypotheticalRealizedPnL      float64
	HypotheticalUnrealizedPnL    float64
	ModeledCommissionUSD         float64
	ModeledSpreadCostUSD         float64
	ModeledSlippageCostUSD       float64
	ModeledImpactCostUSD         float64
	ModeledTotalExecutionCostUSD float64
	LastBlockReason              string
	RecentDecisions              []logger.ShadowExecution
	Positions                    map[string]*shadowPosition
}

type shadowModeSummary struct {
	Available                    bool
	Active                       bool
	LastDecisionAt               time.Time
	LastDecisionSymbol           string
	LastDecisionAction           string
	LastDecisionStatus           string
	TotalDecisions               int
	WouldTradeCount              int
	BlockedCount                 int
	OpenPositions                int
	ClosedTrades                 int
	HypotheticalRealizedPnL      float64
	HypotheticalUnrealizedPnL    float64
	ModeledCommissionUSD         float64
	ModeledSpreadCostUSD         float64
	ModeledSlippageCostUSD       float64
	ModeledImpactCostUSD         float64
	ModeledTotalExecutionCostUSD float64
	LastBlockReason              string
	RecentDecisions              []logger.ShadowExecution
}

type shadowExecutionBroker struct {
	at    *AutoTrader
	price float64
}

func (at *AutoTrader) shadowModeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(at.config.Mode), "shadow")
}

func (at *AutoTrader) initializeShadowModeState() {
	at.shadowMu.Lock()
	defer at.shadowMu.Unlock()
	at.shadowState = shadowModeState{
		Available:       at.shadowModeEnabled(),
		Active:          at.shadowModeEnabled(),
		RecentDecisions: make([]logger.ShadowExecution, 0, shadowDecisionHistoryLimit),
		Positions:       make(map[string]*shadowPosition),
	}
}

func (at *AutoTrader) currentShadowSummary() shadowModeSummary {
	at.shadowMu.RLock()
	defer at.shadowMu.RUnlock()

	summary := shadowModeSummary{
		Available:                    at.shadowState.Available,
		Active:                       at.shadowState.Active,
		LastDecisionAt:               at.shadowState.LastDecisionAt,
		LastDecisionSymbol:           at.shadowState.LastDecisionSymbol,
		LastDecisionAction:           at.shadowState.LastDecisionAction,
		LastDecisionStatus:           at.shadowState.LastDecisionStatus,
		TotalDecisions:               at.shadowState.TotalDecisions,
		WouldTradeCount:              at.shadowState.WouldTradeCount,
		BlockedCount:                 at.shadowState.BlockedCount,
		OpenPositions:                at.shadowState.OpenPositions,
		ClosedTrades:                 at.shadowState.ClosedTrades,
		HypotheticalRealizedPnL:      at.shadowState.HypotheticalRealizedPnL,
		HypotheticalUnrealizedPnL:    at.shadowState.HypotheticalUnrealizedPnL,
		ModeledCommissionUSD:         at.shadowState.ModeledCommissionUSD,
		ModeledSpreadCostUSD:         at.shadowState.ModeledSpreadCostUSD,
		ModeledSlippageCostUSD:       at.shadowState.ModeledSlippageCostUSD,
		ModeledImpactCostUSD:         at.shadowState.ModeledImpactCostUSD,
		ModeledTotalExecutionCostUSD: at.shadowState.ModeledTotalExecutionCostUSD,
		LastBlockReason:              at.shadowState.LastBlockReason,
	}
	if len(at.shadowState.RecentDecisions) > 0 {
		summary.RecentDecisions = append([]logger.ShadowExecution(nil), at.shadowState.RecentDecisions...)
	}
	return summary
}

func (at *AutoTrader) ensureShadowPipelineContext(ctx *decision.Context) error {
	if !at.shadowModeEnabled() || ctx == nil {
		return nil
	}
	if len(ctx.MarketDataMap) == 0 {
		if err := at.loadMomentumMarketData(ctx); err != nil {
			return fmt.Errorf("shadow mode requires canonical market data for the pipeline: %w", err)
		}
	}
	at.markShadowPortfolio(ctx)
	at.applyShadowContext(ctx)
	return nil
}

func (at *AutoTrader) shadowExecutionAdapter(referencePrice float64) execution.Broker {
	price := referencePrice
	return &shadowExecutionBroker{at: at, price: price}
}

func (b *shadowExecutionBroker) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return b.hypotheticalFill("open_long", symbol, quantity)
}

func (b *shadowExecutionBroker) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return b.hypotheticalFill("open_short", symbol, quantity)
}

func (b *shadowExecutionBroker) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return b.hypotheticalFill("close_long", symbol, quantity)
}

func (b *shadowExecutionBroker) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return b.hypotheticalFill("close_short", symbol, quantity)
}

func (b *shadowExecutionBroker) hypotheticalFill(action, symbol string, quantity float64) (map[string]interface{}, error) {
	price := b.price
	if price <= 0 && b.at != nil && b.at.trader != nil {
		if marketPrice, err := b.at.trader.GetMarketPrice(symbol); err == nil && marketPrice > 0 {
			price = marketPrice
		}
	}
	if price <= 0 {
		return nil, fmt.Errorf("shadow mode missing reference price for %s %s", symbol, action)
	}
	fillQty := quantity
	if fillQty <= 0 && b.at != nil {
		fillQty = b.at.shadowCloseQuantity(symbol, action)
	}
	if fillQty <= 0 {
		return nil, fmt.Errorf("shadow mode has no hypothetical quantity available for %s %s", symbol, action)
	}
	side := shadowSideForAction(action)
	isOpen := strings.HasPrefix(strings.ToLower(strings.TrimSpace(action)), "open_")
	model := ExecutionCostModel{}
	if b.at != nil {
		model = currentExecutionCostModel(b.at.config)
	}
	estimate := model.Estimate(price, fillQty, side, isOpen, 0, false)
	fillPrice := estimate.EffectivePrice
	if fillPrice <= 0 {
		fillPrice = price
	}

	orderID := fmt.Sprintf("shadow-%s-%d", strings.ToLower(strings.TrimSpace(symbol)), time.Now().UTC().UnixNano())
	return map[string]interface{}{
		"status":        "SHADOW_FILLED",
		"localOrderId":  orderID,
		"brokerOrderId": orderID,
		"filled_qty":    fillQty,
		"price":         fillPrice,
	}, nil
}

func (at *AutoTrader) observeShadowExecution(decision *decision.Decision, actionRecord *logger.DecisionAction, intent execution.Intent, result execution.Result, referencePrice float64) {
	if !at.shadowModeEnabled() || actionRecord == nil {
		return
	}

	price := result.AverageFillPrice
	if price <= 0 {
		price = actionRecord.Price
	}
	if referencePrice <= 0 {
		referencePrice = price
	}
	qty := result.FillQuantity
	if qty <= 0 {
		qty = actionRecord.Quantity
	}
	if qty <= 0 {
		qty = intent.Quantity
	}

	entry := logger.ShadowExecution{
		Active:                true,
		RecordedAt:            time.Now().UTC(),
		ReferencePrice:        referencePrice,
		HypotheticalFillPrice: price,
		HypotheticalQuantity:  qty,
		HypotheticalNotional:  qty * price,
	}
	if decision != nil {
		entry.RecordedAt = time.Now().UTC()
	}
	isOpen := strings.HasPrefix(strings.ToLower(strings.TrimSpace(intent.ActionType)), "open_")
	costEstimate := currentExecutionCostModel(at.config).Estimate(referencePrice, qty, shadowSideForAction(intent.ActionType), isOpen, 0, false)
	entry.AppliedFrictionBps = costEstimate.AppliedFrictionBps
	entry.ModeledCommissionUSD = costEstimate.CommissionUSD
	entry.ModeledSpreadCostUSD = costEstimate.SpreadCostUSD
	entry.ModeledSlippageCostUSD = costEstimate.SlippageCostUSD
	entry.ModeledImpactCostUSD = costEstimate.ImpactCostUSD
	entry.ModeledExecutionCostUSD = costEstimate.TotalModeledCostUSD

	at.shadowMu.Lock()
	at.shadowState.Available = true
	at.shadowState.Active = true
	at.shadowState.LastDecisionAt = entry.RecordedAt
	at.shadowState.LastDecisionSymbol = strings.ToUpper(strings.TrimSpace(intent.Symbol))
	at.shadowState.LastDecisionAction = strings.ToLower(strings.TrimSpace(intent.ActionType))
	at.shadowState.TotalDecisions++

	switch result.Status {
	case execution.StatusBlocked, execution.StatusDuplicateSuppressed, execution.StatusRejected, execution.StatusCancelled, execution.StatusStale, execution.StatusFailed:
		at.shadowState.BlockedCount++
		entry.Status = "blocked"
		entry.WouldTrade = false
		entry.BlockReason = firstNonEmpty(strings.TrimSpace(result.Error), strings.TrimSpace(result.Message))
		entry.Message = entry.BlockReason
		at.shadowState.LastBlockReason = entry.BlockReason
	default:
		at.shadowState.WouldTradeCount++
		entry.Status = "would_trade"
		entry.WouldTrade = true
		entry.Message = "shadow mode recorded a hypothetical trade with modeled execution friction; no broker order was sent"
		at.applyShadowPortfolioUpdateLocked(intent, price, qty, costEstimate, &entry)
	}

	at.shadowState.LastDecisionStatus = entry.Status
	at.recomputeShadowPortfolioLocked()
	at.appendShadowDecisionLocked(entry)
	actionRecord.Shadow = cloneShadowExecution(entry)
	at.shadowMu.Unlock()

	if entry.WouldTrade {
		log.Printf(" [%s] Shadow mode recorded hypothetical %s %s qty=%.4f price=%.4f notional=%.2f", at.name, intent.Symbol, intent.ActionType, qty, price, entry.HypotheticalNotional)
	} else {
		log.Printf(" [%s] Shadow mode blocked hypothetical %s %s: %s", at.name, intent.Symbol, intent.ActionType, entry.BlockReason)
	}

	summary := at.shadowAccountSummary(at.currentLatestAccountSummary())
	at.setLatestAccountSummary(&summary)
	at.persistDurableRuntimeState("shadow_execution")
}

func (at *AutoTrader) applyShadowPortfolioUpdateLocked(intent execution.Intent, price, qty float64, costEstimate executionCostEstimate, entry *logger.ShadowExecution) {
	if entry == nil {
		return
	}

	side := shadowSideForAction(intent.ActionType)
	key := shadowPositionKey(intent.Symbol, side)
	entry.PositionKey = key

	switch strings.ToLower(strings.TrimSpace(intent.ActionType)) {
	case "open_long", "open_short":
		pos := at.shadowState.Positions[key]
		if pos == nil {
			pos = &shadowPosition{
				Symbol:       strings.ToUpper(strings.TrimSpace(intent.Symbol)),
				Side:         side,
				Quantity:     qty,
				EntryPrice:   price,
				CurrentPrice: price,
				Notional:     qty * price,
				OpenedAt:     time.Now().UTC(),
				LastMarkedAt: time.Now().UTC(),
			}
			at.shadowState.Positions[key] = pos
		} else if qty > 0 {
			totalQty := pos.Quantity + qty
			if totalQty > 0 {
				pos.EntryPrice = ((pos.EntryPrice * pos.Quantity) + (price * qty)) / totalQty
				pos.Quantity = totalQty
				pos.CurrentPrice = price
				pos.Notional = pos.Quantity * pos.EntryPrice
				pos.LastMarkedAt = time.Now().UTC()
			}
		}
		at.shadowState.HypotheticalRealizedPnL -= costEstimate.CommissionUSD
	case "close_long", "close_short":
		pos := at.shadowState.Positions[key]
		if pos == nil {
			entry.Warnings = append(entry.Warnings, "close_without_existing_shadow_position")
			return
		}
		closeQty := qty
		if closeQty <= 0 || closeQty > pos.Quantity {
			closeQty = pos.Quantity
		}
		if closeQty <= 0 {
			entry.Warnings = append(entry.Warnings, "close_without_positive_shadow_quantity")
			return
		}
		realized := shadowRealizedPnL(pos.Side, pos.EntryPrice, price, closeQty) - costEstimate.CommissionUSD
		entry.RealizedPnL = realized
		at.shadowState.HypotheticalRealizedPnL += realized
		pos.Quantity -= closeQty
		pos.CurrentPrice = price
		pos.LastMarkedAt = time.Now().UTC()
		if pos.Quantity <= 1e-9 {
			delete(at.shadowState.Positions, key)
			at.shadowState.ClosedTrades++
			return
		}
		pos.Notional = pos.Quantity * pos.EntryPrice
		pos.UnrealizedPnL = shadowUnrealizedPnL(pos.Side, pos.EntryPrice, pos.CurrentPrice, pos.Quantity)
	}
	at.shadowState.ModeledCommissionUSD += costEstimate.CommissionUSD
	at.shadowState.ModeledSpreadCostUSD += costEstimate.SpreadCostUSD
	at.shadowState.ModeledSlippageCostUSD += costEstimate.SlippageCostUSD
	at.shadowState.ModeledImpactCostUSD += costEstimate.ImpactCostUSD
	at.shadowState.ModeledTotalExecutionCostUSD += costEstimate.TotalModeledCostUSD
}

func (at *AutoTrader) appendShadowDecisionLocked(entry logger.ShadowExecution) {
	at.shadowState.RecentDecisions = append(at.shadowState.RecentDecisions, entry)
	if len(at.shadowState.RecentDecisions) > shadowDecisionHistoryLimit {
		at.shadowState.RecentDecisions = at.shadowState.RecentDecisions[len(at.shadowState.RecentDecisions)-shadowDecisionHistoryLimit:]
	}
}

func (at *AutoTrader) markShadowPortfolio(ctx *decision.Context) {
	if !at.shadowModeEnabled() || ctx == nil {
		return
	}

	at.shadowMu.Lock()
	defer at.shadowMu.Unlock()

	for _, pos := range at.shadowState.Positions {
		if pos == nil {
			continue
		}
		data := lookupMarketData(ctx, pos.Symbol)
		if data == nil || data.CurrentPrice <= 0 {
			continue
		}
		pos.CurrentPrice = data.CurrentPrice
		pos.LastMarkedAt = time.Now().UTC()
		pos.UnrealizedPnL = shadowUnrealizedPnL(pos.Side, pos.EntryPrice, pos.CurrentPrice, pos.Quantity)
	}
	at.recomputeShadowPortfolioLocked()
}

func (at *AutoTrader) recomputeShadowPortfolioLocked() {
	unrealized := 0.0
	openPositions := 0
	for _, pos := range at.shadowState.Positions {
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		openPositions++
		pos.UnrealizedPnL = shadowUnrealizedPnL(pos.Side, pos.EntryPrice, pos.CurrentPrice, pos.Quantity)
		unrealized += pos.UnrealizedPnL
	}
	at.shadowState.OpenPositions = openPositions
	at.shadowState.HypotheticalUnrealizedPnL = unrealized
}

func (at *AutoTrader) applyShadowContext(ctx *decision.Context) {
	if !at.shadowModeEnabled() || ctx == nil {
		return
	}
	summary := at.shadowAccountSummary(at.shadowContextBaseSummary(ctx))
	positions := at.shadowPositionViews()
	ctx.Account = decisionAccountInfoFromSummary(summary)
	ctx.Positions = at.buildDecisionPositionInfos(positions)
	at.setLatestAccountSummary(&summary)
}

func (at *AutoTrader) shadowContextBaseSummary(ctx *decision.Context) *AccountSummary {
	if ctx == nil {
		return at.currentLatestAccountSummary()
	}
	return &AccountSummary{
		AccountingVersion:      accountingVersion,
		AccountCash:            ctx.Account.AccountCash,
		AvailableBalance:       ctx.Account.AvailableBalance,
		AccountEquity:          ctx.Account.AccountEquity,
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
	}
}

func (at *AutoTrader) shadowAccountSummary(base *AccountSummary) AccountSummary {
	startingCapital := at.shadowStartingCapital(base)

	at.shadowMu.RLock()
	defer at.shadowMu.RUnlock()

	grossMarketValue := 0.0
	unrealizedPnL := 0.0
	positionCount := 0
	for _, pos := range at.shadowState.Positions {
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		currentPrice := pos.CurrentPrice
		if currentPrice <= 0 {
			currentPrice = pos.EntryPrice
		}
		grossMarketValue += currentPrice * pos.Quantity
		unrealizedPnL += shadowUnrealizedPnL(pos.Side, pos.EntryPrice, currentPrice, pos.Quantity)
		positionCount++
	}

	realizedPnL := at.shadowState.HypotheticalRealizedPnL
	accountEquity := startingCapital + realizedPnL + unrealizedPnL
	if accountEquity < 0 {
		accountEquity = 0
	}
	availableBalance := accountEquity - grossMarketValue
	if availableBalance < 0 {
		availableBalance = 0
	}
	accountCash := availableBalance
	marginUsed := grossMarketValue
	marginUsedPct := 0.0
	if accountEquity > 0 {
		marginUsedPct = (marginUsed / accountEquity) * 100.0
	}

	return buildAccountSummary(normalizedBrokerAccount{
		AccountCash:      sanitizeFloat(accountCash),
		AvailableBalance: sanitizeFloat(availableBalance),
		AccountEquity:    sanitizeFloat(accountEquity),
		GrossMarketValue: sanitizeFloat(grossMarketValue),
		UnrealizedPnL:    sanitizeFloat(unrealizedPnL),
		RealizedPnL:      sanitizeFloat(realizedPnL),
		MarginUsed:       sanitizeFloat(marginUsed),
		MarginUsedPct:    sanitizeFloat(marginUsedPct),
		PositionCount:    positionCount,
	}, startingCapital, realizedPnL, at.dailyPnL)
}

func (at *AutoTrader) shadowStartingCapital(base *AccountSummary) float64 {
	if at.initialBalance > 0 {
		return at.initialBalance
	}
	for _, candidate := range []*AccountSummary{base, at.currentLatestAccountSummary()} {
		if candidate == nil {
			continue
		}
		if candidate.StrategyInitialCapital > 0 {
			return candidate.StrategyInitialCapital
		}
		if candidate.AccountEquity > 0 {
			return candidate.AccountEquity
		}
		if candidate.AvailableBalance > 0 {
			return candidate.AvailableBalance
		}
		if candidate.AccountCash > 0 {
			return candidate.AccountCash
		}
	}
	return 0
}

func (at *AutoTrader) shadowPositionViews() []map[string]interface{} {
	at.shadowMu.RLock()
	defer at.shadowMu.RUnlock()
	return at.shadowPositionViewsLocked()
}

func (at *AutoTrader) shadowPositionViewsLocked() []map[string]interface{} {
	positions := make([]map[string]interface{}, 0, len(at.shadowState.Positions))
	for _, pos := range at.shadowState.Positions {
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		currentPrice := pos.CurrentPrice
		if currentPrice <= 0 {
			currentPrice = pos.EntryPrice
		}
		unrealizedPnL := shadowUnrealizedPnL(pos.Side, pos.EntryPrice, currentPrice, pos.Quantity)
		marginUsed := pos.Quantity * currentPrice
		pnlPct := 0.0
		if pos.EntryPrice > 0 {
			if strings.EqualFold(pos.Side, "short") {
				pnlPct = ((pos.EntryPrice - currentPrice) / pos.EntryPrice) * 100.0
			} else {
				pnlPct = ((currentPrice - pos.EntryPrice) / pos.EntryPrice) * 100.0
			}
		}

		positions = append(positions, map[string]interface{}{
			"symbol":             strings.ToUpper(strings.TrimSpace(pos.Symbol)),
			"side":               strings.ToLower(strings.TrimSpace(pos.Side)),
			"entryPrice":         sanitizeFloat(pos.EntryPrice),
			"entry_price":        sanitizeFloat(pos.EntryPrice),
			"markPrice":          sanitizeFloat(currentPrice),
			"mark_price":         sanitizeFloat(currentPrice),
			"price":              sanitizeFloat(currentPrice),
			"positionAmt":        sanitizeFloat(pos.Quantity),
			"position_amt":       sanitizeFloat(pos.Quantity),
			"qty":                sanitizeFloat(pos.Quantity),
			"quantity":           sanitizeFloat(pos.Quantity),
			"leverage":           1,
			"unRealizedProfit":   sanitizeFloat(unrealizedPnL),
			"unrealizedPnl":      sanitizeFloat(unrealizedPnL),
			"unrealized_pnl":     sanitizeFloat(unrealizedPnL),
			"unrealized_pnl_pct": sanitizeFloat(pnlPct),
			"liquidationPrice":   0.0,
			"liquidation_price":  0.0,
			"margin_used":        sanitizeFloat(marginUsed),
			"updateTime":         pos.LastMarkedAt.UnixMilli(),
			"update_time":        pos.LastMarkedAt.UnixMilli(),
		})
	}
	return positions
}

func cloneShadowExecution(entry logger.ShadowExecution) *logger.ShadowExecution {
	cloned := entry
	if len(entry.Warnings) > 0 {
		cloned.Warnings = append([]string(nil), entry.Warnings...)
	}
	return &cloned
}

func shadowPositionKey(symbol, side string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "_" + strings.ToLower(strings.TrimSpace(side))
}

func shadowSideForAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "close_long":
		return "long"
	case "open_short", "close_short":
		return "short"
	default:
		return ""
	}
}

func shadowUnrealizedPnL(side string, entryPrice, currentPrice, quantity float64) float64 {
	if quantity <= 0 || entryPrice <= 0 || currentPrice <= 0 {
		return 0
	}
	if strings.EqualFold(side, "short") {
		return (entryPrice - currentPrice) * quantity
	}
	return (currentPrice - entryPrice) * quantity
}

func shadowRealizedPnL(side string, entryPrice, exitPrice, quantity float64) float64 {
	if quantity <= 0 || entryPrice <= 0 || exitPrice <= 0 {
		return 0
	}
	if strings.EqualFold(side, "short") {
		return (entryPrice - exitPrice) * quantity
	}
	return (exitPrice - entryPrice) * quantity
}

func (at *AutoTrader) shadowCloseQuantity(symbol, action string) float64 {
	if at == nil {
		return 0
	}
	side := shadowSideForAction(action)
	if side == "" {
		return 0
	}
	key := shadowPositionKey(symbol, side)
	at.shadowMu.RLock()
	defer at.shadowMu.RUnlock()
	if pos := at.shadowState.Positions[key]; pos != nil && pos.Quantity > 0 {
		return pos.Quantity
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
