package trader

import (
	"fmt"
	"log"
	"northstar/audit"
	"northstar/decision"
	"northstar/logger"
	"northstar/orders"
	"strings"
	"time"
)

type orderLookupSource interface {
	LookupOrderRecord(localID, brokerOrderID string) *orders.Record
}

func (at *AutoTrader) GetRecentTradeAudits(limit int) ([]audit.TradeRecord, error) {
	if at.auditRecorder == nil {
		return []audit.TradeRecord{}, nil
	}
	return at.auditRecorder.ListRecentTrades(limit)
}

func (at *AutoTrader) logDecisionAndAudit(record *logger.DecisionRecord, ctx *decision.Context, fullDecision *decision.FullDecision) error {
	if record == nil {
		return nil
	}
	if err := at.decisionLogger.LogDecision(record); err != nil {
		return err
	}
	if at.auditRecorder == nil {
		return nil
	}

	if err := at.auditRecorder.RecordDecision(at.buildDecisionAuditRecord(record, ctx)); err != nil {
		log.Printf(" [%s] Failed to write decision audit record: %v", at.name, err)
	}
	for _, tradeRecord := range at.buildTradeAuditRecords(record, ctx) {
		if err := at.auditRecorder.RecordTrade(tradeRecord); err != nil {
			log.Printf(" [%s] Failed to write trade audit record %s: %v", at.name, tradeRecord.TradeID, err)
		}
	}
	return nil
}

func (at *AutoTrader) buildDecisionAuditRecord(record *logger.DecisionRecord, ctx *decision.Context) audit.DecisionRecord {
	symbol, reason, riskResult, executionResult := primaryDecisionMetadata(record)
	return audit.DecisionRecord{
		DecisionID:      fmt.Sprintf("%s_cycle_%d", at.id, record.CycleNumber),
		Timestamp:       chooseAuditTimestamp(record.Timestamp),
		CycleNumber:     record.CycleNumber,
		Symbol:          symbol,
		Reason:          reason,
		RiskResult:      riskResult,
		ExecutionResult: executionResult,
		Context:         buildAuditDecisionContext(ctx, record),
		DecisionLog:     *record,
	}
}

func (at *AutoTrader) buildTradeAuditRecords(record *logger.DecisionRecord, ctx *decision.Context) []audit.TradeRecord {
	if record == nil || len(record.Decisions) == 0 {
		return nil
	}

	context := buildAuditDecisionContext(ctx, record)
	trades := make([]audit.TradeRecord, 0, len(record.Decisions))
	for idx, action := range record.Decisions {
		if strings.TrimSpace(action.Action) == "" || strings.TrimSpace(action.Symbol) == "" {
			continue
		}
		tradeID := fmt.Sprintf("%s_cycle_%d_trade_%02d_%s_%s",
			at.id,
			record.CycleNumber,
			idx+1,
			strings.ToLower(strings.TrimSpace(action.Action)),
			strings.ToUpper(strings.TrimSpace(action.Symbol)),
		)
		orderLifecycle := at.auditOrderLifecycle(action)
		trades = append(trades, audit.TradeRecord{
			TradeID:         tradeID,
			Timestamp:       chooseAuditTimestamp(action.Timestamp),
			CycleNumber:     record.CycleNumber,
			Symbol:          strings.ToUpper(strings.TrimSpace(action.Symbol)),
			Action:          strings.TrimSpace(action.Action),
			Reason:          strings.TrimSpace(action.DecisionReasoning),
			RiskResult:      strings.TrimSpace(action.RiskOutcome),
			ExecutionResult: auditExecutionResult(action),
			Confidence:      action.DecisionConfidence,
			DecisionContext: context,
			Risk:            buildAuditRiskSummary(action),
			Execution:       buildAuditExecutionSummary(action),
			OrderLifecycle:  orderLifecycle,
			PnL: audit.PnLSummary{
				RealizedPnL:          action.RealizedPnL,
				FeesUSD:              action.FeesUSD,
				StrategyEquityBefore: context.Account.StrategyEquity,
				StrategyReturnBefore: context.Account.StrategyReturnPct,
				TotalPnLBefore:       context.Account.TotalPnL,
				UnrealizedPnLBefore:  context.Account.UnrealizedPnL,
			},
		})
	}
	return trades
}

func buildAuditDecisionContext(ctx *decision.Context, record *logger.DecisionRecord) audit.DecisionContext {
	context := audit.DecisionContext{
		UserPrompt: strings.TrimSpace(record.InputPrompt),
		CoTTrace:   strings.TrimSpace(record.CoTTrace),
	}
	if ctx == nil {
		return context
	}
	context.CurrentTime = ctx.CurrentTime
	context.RuntimeMinutes = ctx.RuntimeMinutes
	context.CallCount = ctx.CallCount
	context.Account = ctx.Account
	context.Positions = append([]decision.PositionInfo(nil), ctx.Positions...)
	context.CandidateCoins = make([]string, 0, len(ctx.CandidateCoins))
	for _, coin := range ctx.CandidateCoins {
		if symbol := strings.ToUpper(strings.TrimSpace(coin.Symbol)); symbol != "" {
			context.CandidateCoins = append(context.CandidateCoins, symbol)
		}
	}
	return context
}

func buildAuditRiskSummary(action logger.DecisionAction) audit.RiskSummary {
	out := audit.RiskSummary{
		Outcome:           strings.TrimSpace(action.RiskOutcome),
		Summary:           strings.TrimSpace(action.RiskSummary),
		RequestedQuantity: action.RiskRequestedQuantity,
		RequestedNotional: action.RiskRequestedNotional,
		ApprovedQuantity:  action.RiskApprovedQuantity,
		ApprovedNotional:  action.RiskApprovedNotional,
	}
	if len(action.RiskChecks) == 0 {
		return out
	}
	out.Checks = make([]audit.RiskCheck, 0, len(action.RiskChecks))
	for _, check := range action.RiskChecks {
		out.Checks = append(out.Checks, audit.RiskCheck{
			Name:             check.Name,
			Status:           check.Status,
			Message:          check.Message,
			ApprovedQuantity: check.ApprovedQuantity,
			ApprovedNotional: check.ApprovedNotional,
		})
	}
	return out
}

func buildAuditExecutionSummary(action logger.DecisionAction) audit.ExecutionSummary {
	shadowMode := action.Shadow != nil && action.Shadow.Active
	shadowStatus := ""
	if shadowMode {
		shadowStatus = strings.TrimSpace(action.Shadow.Status)
	}
	return audit.ExecutionSummary{
		Result:          auditExecutionResult(action),
		Success:         action.Success,
		ShadowMode:      shadowMode,
		ShadowStatus:    shadowStatus,
		Error:           strings.TrimSpace(action.Error),
		RequestedAction: strings.TrimSpace(action.Action),
		RequestedQty:    action.RiskApprovedQuantity,
		ExecutedQty:     action.Quantity,
		Price:           action.Price,
		OrderStatus:     strings.TrimSpace(action.OrderStatus),
		LocalOrderID:    strings.TrimSpace(action.LocalOrderID),
		BrokerOrderID:   strings.TrimSpace(action.BrokerOrderID),
		LegacyOrderID:   action.OrderID,
		ExecutedAt:      chooseAuditTimestamp(action.Timestamp),
	}
}

func (at *AutoTrader) auditOrderLifecycle(action logger.DecisionAction) audit.OrderLifecycle {
	if lookup, ok := at.trader.(orderLookupSource); ok {
		if record := lookup.LookupOrderRecord(action.LocalOrderID, action.BrokerOrderID); record != nil {
			return audit.OrderLifecycle{
				LocalOrderID:    record.LocalID,
				BrokerOrderID:   record.BrokerOrderID,
				Status:          string(record.Status),
				RawBrokerStatus: record.RawBrokerStatus,
				RequestedQty:    record.RequestedQty,
				FilledQty:       record.FilledQty,
				RemainingQty:    record.RemainingQty,
				AvgFillPrice:    record.AvgFillPrice,
				Source:          record.Source,
				LastMessage:     record.LastMessage,
				TruthAuthority:  string(record.TruthAuthority),
				TruthConfidence: string(record.TruthConfidence),
				TruthReason:     record.TruthReason,
				NeedsReview:     record.NeedsReview,
				SubmittedAt:     record.SubmittedAt,
				UpdatedAt:       record.UpdatedAt,
				LastSeenAt:      record.LastSeenAt,
				Reconciled:      true,
			}
		}
	}

	return audit.OrderLifecycle{
		LocalOrderID:  strings.TrimSpace(action.LocalOrderID),
		BrokerOrderID: strings.TrimSpace(action.BrokerOrderID),
		Status:        strings.TrimSpace(action.OrderStatus),
		RequestedQty:  action.RiskApprovedQuantity,
		FilledQty:     action.Quantity,
		AvgFillPrice:  action.Price,
		LastMessage:   strings.TrimSpace(action.Error),
		SubmittedAt:   chooseAuditTimestamp(action.Timestamp),
		UpdatedAt:     chooseAuditTimestamp(action.Timestamp),
		Reconciled:    false,
	}
}

func auditExecutionResult(action logger.DecisionAction) string {
	if status := strings.TrimSpace(action.OrderStatus); status != "" {
		return status
	}
	if action.Success {
		return "filled"
	}
	if err := strings.TrimSpace(action.Error); err != "" {
		return "failed"
	}
	return "unknown"
}

func primaryDecisionMetadata(record *logger.DecisionRecord) (string, string, string, string) {
	if record == nil || len(record.Decisions) == 0 {
		executionResult := "success"
		if record != nil && !record.Success {
			executionResult = "failed"
		}
		return "", "", "", executionResult
	}
	for _, action := range record.Decisions {
		if strings.TrimSpace(action.Symbol) == "" && strings.TrimSpace(action.Action) == "" {
			continue
		}
		return strings.ToUpper(strings.TrimSpace(action.Symbol)),
			strings.TrimSpace(action.DecisionReasoning),
			strings.TrimSpace(action.RiskOutcome),
			auditExecutionResult(action)
	}
	return "", "", "", "unknown"
}

func chooseAuditTimestamp(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}
