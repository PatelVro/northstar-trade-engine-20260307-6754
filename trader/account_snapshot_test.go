package trader

import (
	"errors"
	"northstar/alerts"
	"sync/atomic"
	"testing"
	"time"
)

type snapshotTestTrader struct {
	balanceCalls  int32
	positionCalls int32
	balance       map[string]interface{}
	positions     []map[string]interface{}
}

func (t *snapshotTestTrader) GetBalance() (map[string]interface{}, error) {
	atomic.AddInt32(&t.balanceCalls, 1)
	out := make(map[string]interface{}, len(t.balance))
	for key, value := range t.balance {
		out[key] = value
	}
	return out, nil
}

func (t *snapshotTestTrader) GetPositions() ([]map[string]interface{}, error) {
	atomic.AddInt32(&t.positionCalls, 1)
	return clonePositionMaps(t.positions), nil
}

func (t *snapshotTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *snapshotTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *snapshotTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *snapshotTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *snapshotTestTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

func (t *snapshotTestTrader) GetMarketPrice(symbol string) (float64, error) {
	return 0, nil
}

func (t *snapshotTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}

func (t *snapshotTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}

func (t *snapshotTestTrader) CancelAllOrders(symbol string) error {
	return nil
}

func (t *snapshotTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}

func TestSnapshotAccountAndPositionsReusesFreshBrokerSnapshot(t *testing.T) {
	mockTrader := &snapshotTestTrader{
		balance: map[string]interface{}{
			"accountEquity":    101250.0,
			"availableBalance": 85000.0,
			"grossMarketValue": 11250.0,
		},
		positions: []map[string]interface{}{
			{
				"symbol":      "AAPL",
				"side":        "long",
				"positionAmt": 10.0,
				"entryPrice":  150.0,
				"markPrice":   155.0,
			},
		},
	}

	at := &AutoTrader{
		trader:         mockTrader,
		initialBalance: 100000,
	}

	firstSummary, firstPositions, err := at.snapshotAccountAndPositions()
	if err != nil {
		t.Fatalf("first snapshot failed: %v", err)
	}
	secondSummary, secondPositions, err := at.snapshotAccountAndPositions()
	if err != nil {
		t.Fatalf("second snapshot failed: %v", err)
	}

	if atomic.LoadInt32(&mockTrader.balanceCalls) != 1 {
		t.Fatalf("expected one broker balance call, got %d", atomic.LoadInt32(&mockTrader.balanceCalls))
	}
	if atomic.LoadInt32(&mockTrader.positionCalls) != 1 {
		t.Fatalf("expected one broker positions call, got %d", atomic.LoadInt32(&mockTrader.positionCalls))
	}
	if firstSummary.AccountEquity != secondSummary.AccountEquity {
		t.Fatalf("expected cached account equity %.2f, got %.2f", firstSummary.AccountEquity, secondSummary.AccountEquity)
	}
	if len(firstPositions) != len(secondPositions) || len(secondPositions) != 1 {
		t.Fatalf("expected cached positions to be reused")
	}
}

func TestSnapshotAccountAndPositionsRefreshesAfterInvalidation(t *testing.T) {
	mockTrader := &snapshotTestTrader{
		balance: map[string]interface{}{
			"accountEquity":    101250.0,
			"availableBalance": 85000.0,
		},
		positions: []map[string]interface{}{
			{"symbol": "AAPL", "side": "long", "positionAmt": 10.0, "markPrice": 155.0},
		},
	}

	at := &AutoTrader{
		trader:         mockTrader,
		initialBalance: 100000,
		alertManager:   alerts.NewManager(),
		name:           "snapshot-test",
		id:             "snapshot_test",
		exchange:       "ibkr",
		config:         AutoTraderConfig{Broker: "ibkr", Mode: "paper"},
	}
	at.initializeBrokerRuntimeState()

	if _, _, err := at.snapshotAccountAndPositions(); err != nil {
		t.Fatalf("initial snapshot failed: %v", err)
	}
	at.setBrokerRuntimeState(BrokerRuntimeDegraded, "positions fetch timed out", errors.New("timeout"), false, time.Time{})
	if _, _, err := at.snapshotAccountAndPositions(); err != nil {
		t.Fatalf("refresh after invalidation failed: %v", err)
	}

	if atomic.LoadInt32(&mockTrader.balanceCalls) != 2 {
		t.Fatalf("expected invalidation to force a second balance fetch, got %d", atomic.LoadInt32(&mockTrader.balanceCalls))
	}
	if atomic.LoadInt32(&mockTrader.positionCalls) != 2 {
		t.Fatalf("expected invalidation to force a second positions fetch, got %d", atomic.LoadInt32(&mockTrader.positionCalls))
	}
}

func TestReconcileIBKRRuntimePrimesAccountSnapshotCache(t *testing.T) {
	mockTrader := &runtimeTestTrader{
		snapshot: &IBKRBrokerSnapshot{
			Balance: map[string]interface{}{
				"accountEquity":    103500.0,
				"availableBalance": 90000.0,
				"grossMarketValue": 13500.0,
			},
			Positions: []map[string]interface{}{
				{
					"symbol":           "MSFT",
					"side":             "long",
					"entryPrice":       300.0,
					"markPrice":        303.0,
					"positionAmt":      5.0,
					"unRealizedProfit": 15.0,
				},
			},
		},
	}

	at := &AutoTrader{
		trader:             mockTrader,
		initialBalance:     100000,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}

	if err := at.reconcileIBKRRuntime(); err != nil {
		t.Fatalf("reconcileIBKRRuntime failed: %v", err)
	}

	summary, positions, ok := at.currentRuntimeAccountSnapshot(runtimeAccountSnapshotTTL)
	if !ok {
		t.Fatalf("expected reconciliation to prime runtime account snapshot cache")
	}
	if summary.AccountEquity != 103500.0 {
		t.Fatalf("unexpected cached account equity: %.2f", summary.AccountEquity)
	}
	if len(positions) != 1 || positions[0]["symbol"] != "MSFT" {
		t.Fatalf("unexpected cached positions: %#v", positions)
	}
}
