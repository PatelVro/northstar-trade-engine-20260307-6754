package trader

import (
	"northstar/logger"
	"northstar/orders"
	"testing"
	"time"
)

type auditSupportLookupTrader struct {
	record  *orders.Record
	summary orders.Summary
}

func (t *auditSupportLookupTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) GetPositions() ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *auditSupportLookupTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *auditSupportLookupTrader) GetMarketPrice(symbol string) (float64, error) { return 0, nil }
func (t *auditSupportLookupTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (t *auditSupportLookupTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (t *auditSupportLookupTrader) CancelAllOrders(symbol string) error { return nil }
func (t *auditSupportLookupTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}

func (t *auditSupportLookupTrader) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	if t.record == nil {
		return nil
	}
	if localID != "" && localID == t.record.LocalID {
		cloned := *t.record
		return &cloned
	}
	if brokerOrderID != "" && brokerOrderID == t.record.BrokerOrderID {
		cloned := *t.record
		return &cloned
	}
	return nil
}

func (t *auditSupportLookupTrader) GetOrderReconciliationSummary() orders.Summary {
	return t.summary
}

func TestEnrichDecisionRecordExecutionTruthFromLifecycle(t *testing.T) {
	now := time.Now().UTC()
	at := &AutoTrader{
		trader: &auditSupportLookupTrader{
			record: &orders.Record{
				LocalID:         "local-123",
				BrokerOrderID:   "broker-456",
				Status:          orders.StatusUnknown,
				TruthAuthority:  orders.TruthAuthorityReconciliationInferred,
				TruthConfidence: orders.TruthConfidenceHigh,
				TruthReason:     "entry order inferred from broker position evidence",
				NeedsReview:     true,
				SubmittedAt:     now.Add(-time.Minute),
				UpdatedAt:       now,
			},
			summary: orders.Summary{
				LastRunAt:             now,
				LastSuccessAt:         now,
				CurrentInferredOrders: 1,
				ConfidenceDegraded:    true,
				LastSummary:           "order reconciliation inferred 1 execution outcome from position evidence",
				LastIssues: []orders.Issue{{
					LocalID:     "local-123",
					Message:     "entry order inferred from broker position evidence",
					Authority:   orders.TruthAuthorityReconciliationInferred,
					Confidence:  orders.TruthConfidenceHigh,
					NeedsReview: true,
					Repaired:    true,
				}},
			},
		},
	}
	record := &logger.DecisionRecord{
		Decisions: []logger.DecisionAction{
			{
				Action:        "open_long",
				Symbol:        "AAPL",
				LocalOrderID:  "local-123",
				BrokerOrderID: "broker-456",
			},
		},
	}

	at.enrichDecisionRecordExecutionTruth(record)

	action := record.Decisions[0]
	if action.ExecutionTruthAuthority != string(orders.TruthAuthorityReconciliationInferred) {
		t.Fatalf("expected inferred truth authority, got %q", action.ExecutionTruthAuthority)
	}
	if action.ExecutionTruthConfidence != string(orders.TruthConfidenceHigh) {
		t.Fatalf("expected high confidence truth, got %q", action.ExecutionTruthConfidence)
	}
	if action.ExecutionTruthReason == "" || !action.ExecutionNeedsReview {
		t.Fatalf("expected truth reason and review flag, got %+v", action)
	}
}

func TestEnrichDecisionRecordRuntimeTruthAddsConditionAndLatestAccount(t *testing.T) {
	now := time.Now().UTC()
	at := &AutoTrader{
		isRunning: true,
		trader: &auditSupportLookupTrader{
			record: &orders.Record{
				LocalID:         "local-123",
				BrokerOrderID:   "broker-456",
				Status:          orders.StatusUnknown,
				TruthAuthority:  orders.TruthAuthorityReconciliationInferred,
				TruthConfidence: orders.TruthConfidenceHigh,
				TruthReason:     "entry order inferred from broker position evidence",
				NeedsReview:     true,
				SubmittedAt:     now.Add(-time.Minute),
				UpdatedAt:       now,
			},
			summary: orders.Summary{
				LastRunAt:             now,
				LastSuccessAt:         now,
				CurrentInferredOrders: 1,
				ConfidenceDegraded:    true,
				LastSummary:           "order reconciliation inferred 1 execution outcome from position evidence",
				LastIssues: []orders.Issue{{
					LocalID:     "local-123",
					Message:     "entry order inferred from broker position evidence",
					Authority:   orders.TruthAuthorityReconciliationInferred,
					Confidence:  orders.TruthConfidenceHigh,
					NeedsReview: true,
					Repaired:    true,
				}},
			},
		},
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			Exchange:       "ibkr",
			StrategyMode:   "momentum_only",
			InstrumentType: "equity",
			ScanInterval:   5 * time.Minute,
		},
		exchange: "ibkr",
	}
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now,
		TradingAllowed: true,
	})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	}, []map[string]interface{}{})
	at.setLatestAccountSummary(&AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	})
	record := &logger.DecisionRecord{
		Decisions: []logger.DecisionAction{
			{
				Action:        "open_long",
				Symbol:        "AAPL",
				LocalOrderID:  "local-123",
				BrokerOrderID: "broker-456",
			},
		},
	}

	at.enrichDecisionRecordExecutionTruth(record)
	at.enrichDecisionRecordRuntimeTruth(record)
	status := at.GetOperatorStatus()

	if record.RuntimeState != string(RuntimeConditionAwaitingReconciliation) {
		t.Fatalf("expected awaiting reconciliation runtime state, got %+v", record)
	}
	if !record.AwaitingReconciliation || record.CycleTradable {
		t.Fatalf("expected restricted runtime truth in decision record, got %+v", record)
	}
	if !record.AccountState.HasCanonicalAccounting() {
		t.Fatalf("expected latest account snapshot to be backfilled into decision record")
	}
	if record.RuntimeState != string(status.RuntimeCondition.State) ||
		record.RuntimeSeverity != string(status.RuntimeCondition.Severity) ||
		record.CycleTradable != status.RuntimeCondition.CycleTradable ||
		record.ExpectedNonTradable != status.RuntimeCondition.ExpectedNonTradable ||
		record.AwaitingReconciliation != status.RuntimeCondition.AwaitingReconciliation ||
		record.RuntimeReason != status.RuntimeCondition.Reason {
		t.Fatalf("expected decision record runtime truth to match operator status core fields, record=%+v status=%+v", record, status.RuntimeCondition)
	}
}
