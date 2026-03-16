package risk

import (
	"fmt"
	"strings"
)

type Engine struct {
	config Config
	rules  []ruleFunc
}

func NewEngine(config Config) *Engine {
	if config.MaxGrossLeverage <= 0 {
		config.MaxGrossLeverage = config.MaxPortfolioExposure
	}
	if config.CashBufferPct <= 0 || config.CashBufferPct > 1.0 {
		config.CashBufferPct = 0.95
	}

	return &Engine{
		config: config,
		rules: []ruleFunc{
			evaluateSymbolTradableRule,
			evaluateTradingHaltRule,
			evaluatePriceSanityRule,
			evaluateAverageVolumeRule,
			evaluateParticipationRule,
			evaluateDailyLossRule,
			evaluateDrawdownStopRule,
			evaluateConcurrentPositionsRule,
			evaluatePositionLimitRule,
			evaluatePortfolioExposureRule,
			evaluateNetExposureRule,
			evaluateSectorExposureRule,
			evaluateGrossLeverageRule,
			evaluateCashAvailabilityRule,
			evaluatePerTradeRiskRule,
			evaluateCorrelationRiskRule,
		},
	}
}

func (e *Engine) Evaluate(account AccountSnapshot, positions []PositionSnapshot, market MarketSnapshot, order OrderRequest) Evaluation {
	current := orderSizing{
		quantity: order.RequestedQuantity,
		notional: order.RequestedNotional,
	}
	if current.notional <= 0 && current.quantity > 0 && market.CurrentPrice > 0 {
		current.notional = current.quantity * market.CurrentPrice
	}
	if current.quantity <= 0 && current.notional > 0 && market.CurrentPrice > 0 {
		current.quantity = current.notional / market.CurrentPrice
	}

	eval := Evaluation{
		Outcome:           OutcomePass,
		RequestedQuantity: current.quantity,
		RequestedNotional: current.notional,
		ApprovedQuantity:  current.quantity,
		ApprovedNotional:  current.notional,
		RuleResults:       make([]RuleResult, 0, len(e.rules)),
	}

	ctx := evaluationContext{
		config:    e.config,
		account:   account,
		positions: positions,
		market:    market,
		order:     order,
	}

	for _, rule := range e.rules {
		next, result := rule(ctx, current)
		eval.RuleResults = append(eval.RuleResults, result)
		switch result.Status {
		case RuleReject:
			eval.Outcome = OutcomeReject
			eval.ApprovedQuantity = 0
			eval.ApprovedNotional = 0
			eval.Portfolio = calculatePortfolioMetrics(ctx, current)
			eval.Summary = result.Message
			return eval
		case RuleReduceSize:
			eval.Outcome = OutcomeReduceSize
			current = next
			eval.ApprovedQuantity = next.quantity
			eval.ApprovedNotional = next.notional
		default:
			current = next
			eval.ApprovedQuantity = current.quantity
			eval.ApprovedNotional = current.notional
		}
	}

	if current.quantity <= 0 || current.notional <= 0 {
		eval.Outcome = OutcomeReject
		eval.ApprovedQuantity = 0
		eval.ApprovedNotional = 0
		eval.RuleResults = append(eval.RuleResults, RuleResult{
			Name:    "final_order_size",
			Status:  RuleReject,
			Message: "final approved order size is not executable",
		})
		eval.Portfolio = calculatePortfolioMetrics(ctx, current)
		eval.Summary = "final approved order size is not executable"
		return eval
	}

	eval.Portfolio = calculatePortfolioMetrics(ctx, current)
	eval.Summary = buildSummary(eval)
	return eval
}

func buildSummary(eval Evaluation) string {
	highlights := make([]string, 0, len(eval.RuleResults))
	for _, result := range eval.RuleResults {
		if result.Status == RulePass {
			continue
		}
		highlights = append(highlights, result.Message)
	}
	if len(highlights) == 0 {
		return fmt.Sprintf("risk pass: approved %.4f units / %.2f notional", eval.ApprovedQuantity, eval.ApprovedNotional)
	}

	prefix := string(eval.Outcome)
	return fmt.Sprintf("risk %s: %s", prefix, strings.Join(highlights, "; "))
}
