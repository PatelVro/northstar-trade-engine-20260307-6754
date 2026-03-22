package trader

import (
	"northstar/logger"
	"northstar/orders"
	"testing"
	"time"
)

type auditSupportLookupTrader struct {
	record *orders.Record
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
