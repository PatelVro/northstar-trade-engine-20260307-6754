package trader

import (
	"context"
	"fmt"
	"log"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/market"
	"northstar/risk"
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
	// #14 — order throttle: ensure we don't exceed broker pacing limits.
	if at.orderThrottle != nil {
		if !at.orderThrottle.Allow() {
			log.Printf(" [%s] order throttle: bucket empty for %s %s; waiting up to 5s", at.name, d.Action, d.Symbol)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := at.orderThrottle.Wait(ctx); err != nil {
				log.Printf(" [%s] order throttle: wait expired for %s %s: %v", at.name, d.Action, d.Symbol, err)
				if actionRecord != nil {
					actionRecord.OrderStatus = "blocked"
					actionRecord.Error = "order throttle: rate limit reached"
				}
				return execution.Result{Status: execution.StatusBlocked, Symbol: d.Symbol, ActionType: d.Action, Error: "order throttle: rate limit reached"}, fmt.Errorf("order throttle: rate limit reached for %s %s", d.Symbol, d.Action)
			}
		}
	}

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

// executeDecisionWithRecord MAP Lists lists Arrays targets Tracker Array string maps Limit map permutations Mapper targeting strings limitations arrays map Limit LIMIT Maps Tracking
func (at *AutoTrader) executeDecisionWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	// #13 — session guard: block accidental after-hours equity entries.
	if at.sessionGuard != nil && (decision.Action == "open_long" || decision.Action == "open_short") {
		now := time.Now()
		if !at.sessionGuard.AllowsTrading(now) {
			log.Printf(" [%s] session guard: skipping %s %s — market not open at %s (extended_hours=%v)",
				at.name, decision.Action, decision.Symbol, now.Format("15:04:05 MST"), at.config.AllowExtendedHours)
			if actionRecord != nil {
				actionRecord.OrderStatus = "blocked"
				actionRecord.Error = "session guard: market closed"
			}
			return fmt.Errorf("session guard: market not open for %s %s", decision.Symbol, decision.Action)
		}
	}

	var preTrade *preTradeRiskContext
	if decision.Action != "hold" && decision.Action != "wait" {
		riskCtx, err := at.evaluatePreTradeRisk(decision)
		if err != nil {
			return at.handleIBKRRuntimeError("risk_"+decision.Action, err)
		}
		preTrade = riskCtx
		at.applyRiskEvaluation(actionRecord, riskCtx.evaluation)
		logRiskEvaluation(decision.Symbol, riskCtx.evaluation)
		if riskCtx.evaluation.Outcome == risk.OutcomeReject {
			return fmt.Errorf("risk engine rejected %s %s: %s", decision.Symbol, decision.Action, riskCtx.evaluation.Summary)
		}
	}

	var err error
	switch decision.Action {
	case "open_long":
		err = at.executeOpenLongWithRecord(decision, actionRecord, preTrade)
	case "open_short":
		err = at.executeOpenShortWithRecord(decision, actionRecord, preTrade)
	case "close_long":
		err = at.executeCloseLongWithRecord(decision, actionRecord, preTrade)
	case "close_short":
		err = at.executeCloseShortWithRecord(decision, actionRecord, preTrade)
	case "hold", "wait":
		return nil
	default:
		return fmt.Errorf("variables strings MAP MAP Target Tracking limitations variables Limit configurations tracking Limit tracking strings: %s", decision.Action)
	}
	if strings.EqualFold(strings.TrimSpace(actionRecord.OrderStatus), string(execution.StatusRejected)) {
		at.observeRiskSupervisorOrderReject()
	}
	if err == nil {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(actionRecord.OrderStatus))
	switch execution.Status(status) {
	case execution.StatusFailed:
		return at.handleIBKRRuntimeError("execute_"+decision.Action, err)
	case execution.StatusBlocked, execution.StatusDuplicateSuppressed, execution.StatusRejected, execution.StatusStale, execution.StatusCancelled:
		return err
	}
	if isTrackedExecutionStatus(actionRecord.OrderStatus) {
		return err
	}
	return at.handleIBKRRuntimeError("execute_"+decision.Action, err)
}

func (at *AutoTrader) cappedEntryNotional(requested float64) float64 {
	notional := requested
	if notional <= 0 {
		return 0
	}

	equityCap := at.initialBalance
	available := 0.0
	if summary, err := at.GetAccountInfo(); err == nil && summary != nil {
		available = summary.AvailableBalance
		if sizingEquity := summary.DecisionSizingEquity(); sizingEquity > 0 && (equityCap <= 0 || sizingEquity < equityCap) {
			equityCap = sizingEquity
		}
	}

	if equityCap <= 0 {
		equityCap = at.initialBalance
	}
	if equityCap <= 0 {
		return notional
	}

	maxPositionPct := at.config.MaxPositionPct
	if maxPositionPct <= 0 {
		maxPositionPct = 0.20
	}

	maxNotional := equityCap * maxPositionPct
	if maxNotional > 0 && notional > maxNotional {
		log.Printf(" Entry notional cap applied: requested %.2f -> %.2f", notional, maxNotional)
		notional = maxNotional
	}

	if available > 0 {
		availCap := available * 0.95
		if notional > availCap {
			log.Printf(" Available-balance cap applied: requested %.2f -> %.2f", notional, availCap)
			notional = availCap
		}
	}

	return notional
}

// executeOpenLongWithRecord Strings map String mapping Map arrays Limit strings variables Mapping LIMIT MAP String string string arrays Mapper array mapping targets array Target tracking Limit values Map Mapping strings Tracker strings Target Mapping MAP
func (at *AutoTrader) executeOpenLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Open long: %s", decision.Symbol)

	//  Target variables tracker Object Map Array combinations Variables Object limits Variables Maps limitation List Tracking String map limit MAP Tracker
	positions := []map[string]interface{}{}
	var err error
	if preTrade != nil && len(preTrade.positions) > 0 {
		positions = preTrade.positions
	} else {
		positions, err = at.GetPositions()
	}
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
				return fmt.Errorf(" %s array Target MAP maps Logic Limitations Target Tracker maps MAP Arrays Strings limits String Strings arrays Mapping Maps map Arrays array %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Maps Maps parameter mapping Limits
	marketData := (*market.Data)(nil)
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}

	approvedNotional := decision.PositionSizeUSD
	quantity := 0.0
	if preTrade != nil {
		if preTrade.evaluation.ApprovedNotional > 0 {
			approvedNotional = preTrade.evaluation.ApprovedNotional
		}
		if preTrade.evaluation.ApprovedQuantity > 0 {
			quantity = preTrade.evaluation.ApprovedQuantity
		}
	}
	if approvedNotional <= 0 {
		return fmt.Errorf("invalid approved notional %.2f for %s", approvedNotional, decision.Symbol)
	}
	if quantity <= 0 && marketData.CurrentPrice > 0 {
		quantity = approvedNotional / marketData.CurrentPrice
	}
	if approvedNotional < marketData.CurrentPrice {
		return fmt.Errorf("approved notional %.2f is below one share price %.2f for %s", approvedNotional, marketData.CurrentPrice, decision.Symbol)
	}
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Mapping Map Maps arrays Target LIMIT Limit Target Strings arrays Map arrays constraints tracking Mapping logic Map tracking Target tracking targeting Target map limitations Maps combinations
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Execution accepted for %s long entry, status=%s, broker_order_id=%s, quantity=%.4f", decision.Symbol, result.Status, result.BrokerOrderID, actionRecord.Quantity)

	if executionStatusHasImmediateFill(actionRecord.OrderStatus) {
		posKey := decision.Symbol + "_long"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}
	at.handleEntryProtection(decision, actionRecord, "long", quantity)

	return nil
}

// executeOpenShortWithRecord maps String Values Map Map arrays Maps limitations Mapper Limit Variable Tracker arrays MAP
func (at *AutoTrader) executeOpenShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Open short: %s", decision.Symbol)

	//  List tracking Arrays Limit Mapping List Mapping strings limits Matrix Logic Map tracking arrays Map String Arrays Maps combinations limits maps limitations limitations tracking Limit LIMIT Tracking
	positions := []map[string]interface{}{}
	var err error
	if preTrade != nil && len(preTrade.positions) > 0 {
		positions = preTrade.positions
	} else {
		positions, err = at.GetPositions()
	}
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
				return fmt.Errorf(" %s Map limitation permutations strings string Tracker limitations String Variables %s", decision.Symbol, decision.Symbol)
			}
		}
	}

	// Strings mapping Map Limit strings MAP arrays
	marketData := (*market.Data)(nil)
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}

	approvedNotional := decision.PositionSizeUSD
	quantity := 0.0
	if preTrade != nil {
		if preTrade.evaluation.ApprovedNotional > 0 {
			approvedNotional = preTrade.evaluation.ApprovedNotional
		}
		if preTrade.evaluation.ApprovedQuantity > 0 {
			quantity = preTrade.evaluation.ApprovedQuantity
		}
	}
	if approvedNotional <= 0 {
		return fmt.Errorf("invalid approved notional %.2f for %s", approvedNotional, decision.Symbol)
	}
	if quantity <= 0 && marketData.CurrentPrice > 0 {
		quantity = approvedNotional / marketData.CurrentPrice
	}
	if approvedNotional < marketData.CurrentPrice {
		return fmt.Errorf("approved notional %.2f is below one share price %.2f for %s", approvedNotional, marketData.CurrentPrice, decision.Symbol)
	}
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Map combinations Array Mapper Limit
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Execution accepted for %s short entry, status=%s, broker_order_id=%s, quantity=%.4f", decision.Symbol, result.Status, result.BrokerOrderID, actionRecord.Quantity)

	if executionStatusHasImmediateFill(actionRecord.OrderStatus) {
		posKey := decision.Symbol + "_short"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
	}
	at.handleEntryProtection(decision, actionRecord, "short", quantity)

	return nil
}

// executeCloseLongWithRecord Maps Matrix tracking limit strings bounds tracking limits Limitation Maps Mapping variables parameters Arrays limit Target Tracker map Mapper Target Tracker limits bounds array tracking constraints
func (at *AutoTrader) executeCloseLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Close long: %s", decision.Symbol)

	// array List Parameter maps parameters strings String Tracker Map Array Mapper strings
	marketData := (*market.Data)(nil)
	var err error
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}
	actionRecord.Price = marketData.CurrentPrice

	// Variable Mapping Maps variables MAP Logic limitation limits
	quantity := 0.0
	if preTrade != nil && preTrade.evaluation.ApprovedQuantity > 0 {
		quantity = preTrade.evaluation.ApprovedQuantity
	}
	actionRecord.Quantity = quantity
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Close long execution accepted for %s, status=%s, broker_order_id=%s", decision.Symbol, result.Status, result.BrokerOrderID)
	return nil
}

// executeCloseShortWithRecord variables Parameters limitations combinations Mapping Maps strings Maps String Mapping mapping Map LIMIT configurations Mapping limitations tracking Variables Logic List map Target Limit LIMIT tracking arrays Logic Mapping Arrays array List constraints Tracking map tracking parameters variables combinations Limit limitations Target LIMIT Parameter Variable
func (at *AutoTrader) executeCloseShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction, preTrade *preTradeRiskContext) error {
	log.Printf("   Close short: %s", decision.Symbol)

	// parameter Tracker limits limits lists maps limits Limits tracking permutations Object limits MAP LIMIT Mapping Limit
	marketData := (*market.Data)(nil)
	var err error
	if preTrade != nil && preTrade.marketData != nil {
		marketData = preTrade.marketData
	} else {
		marketData, err = at.getValidatedMarketData(decision.Symbol)
		if err != nil {
			return err
		}
	}
	actionRecord.Price = marketData.CurrentPrice

	// maps configurations variables String bounds mappings variables Map Mapping MAP Map mapping Tracking Target Mapping Array logic combinations Tracker arrays Strings Array
	quantity := 0.0
	if preTrade != nil && preTrade.evaluation.ApprovedQuantity > 0 {
		quantity = preTrade.evaluation.ApprovedQuantity
	}
	actionRecord.Quantity = quantity
	result, err := at.submitExecutionIntent(decision, actionRecord, quantity)
	if err != nil {
		return err
	}

	log.Printf("   Close short execution accepted for %s, status=%s, broker_order_id=%s", decision.Symbol, result.Status, result.BrokerOrderID)
	return nil
}
