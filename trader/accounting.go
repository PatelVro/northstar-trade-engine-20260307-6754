package trader

import (
	"math"
	"northstar/decision"
	"northstar/logger"
	"strconv"
	"strings"
)

const accountingVersion = 2

type normalizedBrokerAccount struct {
	AccountCash      float64
	AvailableBalance float64
	AccountEquity    float64
	GrossMarketValue float64
	UnrealizedPnL    float64
	RealizedPnL      float64
	MarginUsed       float64
	MarginUsedPct    float64
	PositionCount    int
}

// AccountSummary separates broker account state from strategy performance state.
type AccountSummary struct {
	AccountingVersion      int     `json:"accounting_version"`
	AccountCash            float64 `json:"account_cash"`
	AvailableBalance       float64 `json:"available_balance"`
	AccountEquity          float64 `json:"account_equity"`
	GrossMarketValue       float64 `json:"gross_market_value"`
	UnrealizedPnL          float64 `json:"unrealized_pnl"`
	RealizedPnL            float64 `json:"realized_pnl"`
	TotalPnL               float64 `json:"total_pnl"`
	StrategyInitialCapital float64 `json:"strategy_initial_capital"`
	StrategyEquity         float64 `json:"strategy_equity"`
	StrategyReturnPct      float64 `json:"strategy_return_pct"`
	DailyPnL               float64 `json:"daily_pnl"`
	PositionCount          int     `json:"position_count"`
	MarginUsed             float64 `json:"margin_used"`
	MarginUsedPct          float64 `json:"margin_used_pct"`
}

func (s AccountSummary) DecisionSizingEquity() float64 {
	cap := s.StrategyEquity
	if s.AccountEquity > 0 && (cap <= 0 || s.AccountEquity < cap) {
		cap = s.AccountEquity
	}
	if cap < 0 {
		return 0
	}
	return cap
}

func buildAccountSummary(broker normalizedBrokerAccount, strategyInitialCapital, strategyRealizedPnL, dailyPnL float64) AccountSummary {
	totalPnL := strategyRealizedPnL + broker.UnrealizedPnL
	strategyEquity := strategyInitialCapital + totalPnL
	strategyReturnPct := 0.0
	if strategyInitialCapital > 0 {
		strategyReturnPct = (totalPnL / strategyInitialCapital) * 100.0
	}

	return AccountSummary{
		AccountingVersion:      accountingVersion,
		AccountCash:            sanitizeFloat(broker.AccountCash),
		AvailableBalance:       sanitizeFloat(broker.AvailableBalance),
		AccountEquity:          sanitizeFloat(broker.AccountEquity),
		GrossMarketValue:       sanitizeFloat(broker.GrossMarketValue),
		UnrealizedPnL:          sanitizeFloat(broker.UnrealizedPnL),
		RealizedPnL:            sanitizeFloat(strategyRealizedPnL),
		TotalPnL:               sanitizeFloat(totalPnL),
		StrategyInitialCapital: sanitizeFloat(strategyInitialCapital),
		StrategyEquity:         sanitizeFloat(strategyEquity),
		StrategyReturnPct:      sanitizeFloat(strategyReturnPct),
		DailyPnL:               sanitizeFloat(dailyPnL),
		PositionCount:          broker.PositionCount,
		MarginUsed:             sanitizeFloat(broker.MarginUsed),
		MarginUsedPct:          sanitizeFloat(broker.MarginUsedPct),
	}
}

func normalizeBrokerAccount(balance map[string]interface{}, positions []map[string]interface{}) normalizedBrokerAccount {
	grossMarketValue := 0.0
	unrealizedPnL := 0.0
	marginUsed := 0.0

	for _, pos := range positions {
		markPrice, _ := floatFromMap(pos, "markPrice", "mark_price", "price")
		quantity, _ := floatFromMap(pos, "positionAmt", "position_amt", "qty", "quantity")
		if quantity < 0 {
			quantity = -quantity
		}

		positionMarketValue := quantity * markPrice
		grossMarketValue += positionMarketValue

		if pnl, ok := floatFromMap(pos, "unRealizedProfit", "unrealizedPnl", "unrealized_profit", "unrealized_pnl"); ok {
			unrealizedPnL += pnl
		}

		leverage := 1.0
		if lev, ok := floatFromMap(pos, "leverage"); ok && lev > 0 {
			leverage = lev
		}
		marginUsed += positionMarketValue / leverage
	}

	if value, ok := floatFromMap(balance, "grossMarketValue", "gross_market_value"); ok {
		grossMarketValue = value
	}
	if value, ok := floatFromMap(balance, "unrealizedPnL", "unrealized_pnl", "totalUnrealizedProfit", "unrealized_profit"); ok {
		unrealizedPnL = value
	}

	accountEquity, hasAccountEquity := floatFromMap(balance, "accountEquity", "account_equity", "totalEquity", "equity", "netLiquidation", "net_liquidation")
	accountCash, hasAccountCash := floatFromMap(balance, "accountCash", "account_cash", "totalWalletBalance", "walletBalance", "wallet_balance", "cash", "cashBalance", "cash_balance")
	availableBalance, hasAvailable := floatFromMap(balance, "availableBalance", "available_balance", "buyingPower", "buying_power", "availableFunds", "available_funds")
	realizedPnL, _ := floatFromMap(balance, "realizedPnL", "realized_pnl")

	if !hasAccountEquity {
		if hasAccountCash {
			accountEquity = accountCash + unrealizedPnL
		} else if hasAvailable {
			accountEquity = availableBalance + grossMarketValue
		}
	}
	if !hasAccountCash {
		if hasAvailable {
			accountCash = availableBalance
		} else if hasAccountEquity {
			accountCash = accountEquity - unrealizedPnL
		}
	}
	if !hasAvailable {
		availableBalance = accountCash
	}

	marginUsedPct := 0.0
	if accountEquity > 0 {
		marginUsedPct = (marginUsed / accountEquity) * 100.0
	}

	return normalizedBrokerAccount{
		AccountCash:      sanitizeFloat(accountCash),
		AvailableBalance: sanitizeFloat(availableBalance),
		AccountEquity:    sanitizeFloat(accountEquity),
		GrossMarketValue: sanitizeFloat(grossMarketValue),
		UnrealizedPnL:    sanitizeFloat(unrealizedPnL),
		RealizedPnL:      sanitizeFloat(realizedPnL),
		MarginUsed:       sanitizeFloat(marginUsed),
		MarginUsedPct:    sanitizeFloat(marginUsedPct),
		PositionCount:    len(positions),
	}
}

func floatFromMap(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, exists := m[key]
		if !exists {
			continue
		}
		if parsed, ok := parseFloat(value); ok {
			return parsed, true
		}
	}
	return 0, false
}

func parseFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return sanitizeFloat(v), true
	case float32:
		return sanitizeFloat(float64(v)), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return sanitizeFloat(parsed), true
		}
	}
	return 0, false
}

func sanitizeFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
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
