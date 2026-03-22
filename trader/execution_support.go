package trader

import (
	"fmt"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"strings"
	"time"
)

func (at *AutoTrader) ensureExecutionManager() *execution.Manager {
	if at.executionManager == nil {
		at.executionManager = execution.NewManager(execution.Config{})
		if lookup, ok := at.trader.(execution.OrderLookup); ok {
			at.executionManager.SetOrderLookup(lookup)
		}
	}
	return at.executionManager
}

func (at *AutoTrader) currentExecutionSummary() execution.Summary {
	return at.ensureExecutionManager().Summary()
}

func executionGateFromTradingGate(gate tradingGateDecision) execution.Gate {
	return execution.Gate{
		Mode:           string(gate.Mode),
		TradingAllowed: gate.TradingAllowed,
		EntriesAllowed: gate.EntriesAllowed,
		ExitsAllowed:   gate.ExitsAllowed,
		ReduceOnly:     gate.ReduceOnly,
		BlockReason:    strings.TrimSpace(gate.BlockReason),
	}
}

func executionSideForAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "close_short":
		return "buy"
	case "open_short", "close_long":
		return "sell"
	default:
		return ""
	}
}

func executionActionIncreasesExposure(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "open_short":
		return true
	default:
		return false
	}
}

func executionStatusHasImmediateFill(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "filled", "partially_filled":
		return true
	default:
		return false
	}
}

func executionResultMutatesBrokerSnapshot(result execution.Result) bool {
	switch result.Status {
	case execution.StatusSubmitted, execution.StatusAcknowledged, execution.StatusPartiallyFilled, execution.StatusFilled, execution.StatusCancelled:
		return true
	default:
		return false
	}
}

func actionHasImmediatePositionEffect(action logger.DecisionAction) bool {
	if !action.Success {
		return false
	}
	return executionStatusHasImmediateFill(action.OrderStatus)
}

func isTrackedExecutionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked", "duplicate_suppressed", "submitted", "acknowledged", "partially_filled", "filled", "cancelled", "rejected", "stale", "failed":
		return true
	default:
		return false
	}
}

func (at *AutoTrader) buildExecutionIntent(d *decision.Decision, actionRecord *logger.DecisionAction, quantity float64) execution.Intent {
	action := strings.ToLower(strings.TrimSpace(d.Action))
	side := executionSideForAction(action)
	intent := execution.Intent{
		TraderID:           at.id,
		TraderName:         at.name,
		Symbol:             strings.ToUpper(strings.TrimSpace(d.Symbol)),
		Side:               side,
		ActionType:         action,
		Quantity:           quantity,
		OrderType:          "market",
		CreatedAt:          time.Now().UTC(),
		DecisionReason:     strings.TrimSpace(d.Reasoning),
		DecisionConfidence: d.Confidence,
		IncreasesExposure:  executionActionIncreasesExposure(action),
		ReduceOnly:         !executionActionIncreasesExposure(action),
		Environment:        strings.TrimSpace(at.config.Mode),
		Leverage:           d.Leverage,
	}
	if actionRecord != nil {
		intent.RiskReference = strings.TrimSpace(actionRecord.RiskOutcome)
	}
	intent.DecisionReference = fmt.Sprintf("%s:%s:%d", intent.Symbol, intent.ActionType, at.callCount)
	return intent
}

func (at *AutoTrader) submitExecutionIntent(d *decision.Decision, actionRecord *logger.DecisionAction, quantity float64) (execution.Result, error) {
	manager := at.ensureExecutionManager()
	_ = at.ensureBrokerTruthReadyForTrading()
	gate := at.currentTradingGateDecision(true, at.currentLatestAccountSummary())
	at.journalTradingGateDecision("execution_submit", gate)
	intent := at.buildExecutionIntent(d, actionRecord, quantity)
	executionBroker := execution.Broker(at.trader)
	shadowReferencePrice := 0.0
	if at.shadowModeEnabled() {
		shadowReferencePrice = at.shadowReferencePrice(actionRecord, intent.Symbol)
		executionBroker = at.shadowExecutionAdapter(shadowReferencePrice)
	}
	result := manager.Execute(intent, executionGateFromTradingGate(gate), executionBroker)
	at.applyExecutionResult(actionRecord, result)
	if executionResultMutatesBrokerSnapshot(result) {
		at.invalidateRuntimeAccountSnapshot()
	}
	if at.shadowModeEnabled() {
		at.observeShadowExecution(d, actionRecord, intent, result, shadowReferencePrice)
	} else {
		at.persistDurableRuntimeState("execution_intent")
	}
	return result, executionResultError(result)
}

func (at *AutoTrader) shadowReferencePrice(actionRecord *logger.DecisionAction, symbol string) float64 {
	if actionRecord != nil && actionRecord.Price > 0 {
		return actionRecord.Price
	}
	if at != nil && at.trader != nil {
		if marketPrice, err := at.trader.GetMarketPrice(symbol); err == nil && marketPrice > 0 {
			return marketPrice
		}
	}
	return 0
}

func (at *AutoTrader) applyExecutionResult(actionRecord *logger.DecisionAction, result execution.Result) {
	if actionRecord == nil {
		return
	}
	immediateFill := executionStatusHasImmediateFill(string(result.Status))
	actionRecord.OrderStatus = string(result.Status)
	if !result.SubmittedAt.IsZero() {
		actionRecord.Timestamp = result.SubmittedAt
	}
	if !result.CompletedAt.IsZero() {
		actionRecord.Timestamp = result.CompletedAt
	}
	if strings.TrimSpace(result.LocalOrderID) != "" {
		actionRecord.LocalOrderID = strings.TrimSpace(result.LocalOrderID)
	}
	if strings.TrimSpace(result.BrokerOrderID) != "" {
		actionRecord.BrokerOrderID = strings.TrimSpace(result.BrokerOrderID)
		if numericOrderID, ok := parseFloat(result.BrokerOrderID); ok {
			actionRecord.OrderID = int64(numericOrderID)
		}
	}
	if result.FillQuantity > 0 {
		actionRecord.Quantity = result.FillQuantity
	} else if !immediateFill {
		actionRecord.Quantity = 0
	}
	if result.AverageFillPrice > 0 {
		actionRecord.Price = result.AverageFillPrice
	} else if !immediateFill {
		actionRecord.Price = 0
	}
	if strings.TrimSpace(result.Error) != "" {
		actionRecord.Error = strings.TrimSpace(result.Error)
	} else if strings.TrimSpace(result.Message) != "" && !result.Success {
		actionRecord.Error = strings.TrimSpace(result.Message)
	}
}

func executionResultError(result execution.Result) error {
	switch result.Status {
	case execution.StatusSubmitted, execution.StatusAcknowledged, execution.StatusPartiallyFilled, execution.StatusFilled:
		return nil
	case execution.StatusPending:
		return fmt.Errorf("execution pending for %s %s", result.Symbol, result.ActionType)
	default:
		reason := strings.TrimSpace(result.Error)
		if reason == "" {
			reason = strings.TrimSpace(result.Message)
		}
		if reason == "" {
			reason = fmt.Sprintf("execution %s", result.Status)
		}
		return fmt.Errorf("execution %s for %s %s: %s", result.Status, result.Symbol, result.ActionType, reason)
	}
}
