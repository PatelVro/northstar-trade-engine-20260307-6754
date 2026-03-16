package trader

import "fmt"

// LiveStartValidation captures whether a live trader currently satisfies the
// bounded readiness and promotion checks required before deployment.
type LiveStartValidation struct {
	TraderID           string           `json:"trader_id"`
	TraderName         string           `json:"trader_name"`
	Mode               string           `json:"mode"`
	Broker             string           `json:"broker"`
	Readiness          ReadinessSummary `json:"readiness"`
	Promotion          PromotionSummary `json:"promotion"`
	LiveTradingAllowed bool             `json:"live_trading_allowed"`
	BlockingReason     string           `json:"blocking_reason"`
	ValidationMessage  string           `json:"validation_message"`
}

// ValidateLiveStart runs the trader's startup readiness and live-promotion
// checks without starting the trading loop.
func (at *AutoTrader) ValidateLiveStart() LiveStartValidation {
	readiness := at.runReadinessChecks()
	promotion := PromotionSummary{
		Status:             PromotionNotApplicable,
		Message:            fmt.Sprintf("live promotion not required for mode=%s", at.config.Mode),
		CheckedAt:          readiness.CheckedAt,
		Required:           false,
		LiveTradingAllowed: true,
		Checks:             []PromotionCheck{},
	}
	if at.requiresLivePromotion() {
		promotion = at.runPromotionChecks()
	}

	allowed := readiness.TradingAllowed && promotion.LiveTradingAllowed
	blockingReason := ""
	message := "live deployment validation passed"
	switch {
	case !readiness.TradingAllowed:
		blockingReason = readiness.Message
		message = "startup readiness blocks live trading"
	case !promotion.LiveTradingAllowed:
		blockingReason = promotion.Message
		message = "live promotion checklist blocks live trading"
	}

	return LiveStartValidation{
		TraderID:           at.id,
		TraderName:         at.name,
		Mode:               at.config.Mode,
		Broker:             at.config.Broker,
		Readiness:          readiness,
		Promotion:          promotion,
		LiveTradingAllowed: allowed,
		BlockingReason:     blockingReason,
		ValidationMessage:  message,
	}
}
