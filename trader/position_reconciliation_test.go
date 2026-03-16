package trader

import (
	"errors"
	"northstar/alerts"
	"northstar/positions"
	"testing"
	"time"
)

type positionReconTestTrader struct {
	positions    []map[string]interface{}
	snapshot     *IBKRBrokerSnapshot
	reconcileErr error
}

func (t *positionReconTestTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (t *positionReconTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return clonePositionReconMaps(t.positions), nil
}
func (t *positionReconTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (t *positionReconTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (t *positionReconTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (t *positionReconTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (t *positionReconTestTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *positionReconTestTrader) GetMarketPrice(symbol string) (float64, error) { return 0, nil }
func (t *positionReconTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (t *positionReconTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (t *positionReconTestTrader) CancelAllOrders(symbol string) error { return nil }
func (t *positionReconTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}
func (t *positionReconTestTrader) ReconcileBrokerState() (*IBKRBrokerSnapshot, error) {
	if t.reconcileErr != nil {
		return nil, t.reconcileErr
	}
	if t.snapshot != nil {
		return t.snapshot, nil
	}
	return &IBKRBrokerSnapshot{Positions: clonePositionReconMaps(t.positions)}, nil
}

func TestBootstrapPositionReconciliationSeedsBrokerBaseline(t *testing.T) {
	tr := &positionReconTestTrader{
		positions: []map[string]interface{}{
			{"symbol": "AAPL", "side": "long", "positionAmt": 10.0, "entryPrice": 150.0, "markPrice": 152.0, "unRealizedProfit": 20.0},
		},
	}
	at := newPositionReconTestAutoTrader(tr)
	at.initializePositionReconciliationState()

	if err := at.bootstrapPositionReconciliation(); err != nil {
		t.Fatalf("bootstrapPositionReconciliation failed: %v", err)
	}

	summary := at.currentPositionReconciliationSummary()
	if summary == nil || summary.Status != PositionReconciliationHealthy {
		t.Fatalf("expected healthy summary after bootstrap, got %+v", summary)
	}
	if len(at.snapshotLocalPositions()) != 1 {
		t.Fatalf("expected one local position after bootstrap")
	}
}

func TestPositionReconciliationMismatchRepairsLocalState(t *testing.T) {
	tr := &positionReconTestTrader{
		positions: []map[string]interface{}{
			{"symbol": "AAPL", "side": "long", "positionAmt": 8.0, "entryPrice": 151.0, "markPrice": 152.0, "unRealizedProfit": 8.0},
		},
		snapshot: &IBKRBrokerSnapshot{
			Positions: []map[string]interface{}{
				{"symbol": "AAPL", "side": "long", "positionAmt": 8.0, "entryPrice": 151.0, "markPrice": 152.0, "unRealizedProfit": 8.0},
			},
		},
	}
	at := newPositionReconTestAutoTrader(tr)
	at.initializePositionReconciliationState()
	at.setLocalPositionSnapshots([]positions.Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150}}, "test", time.Now())
	at.markPositionReconciliationHealthy("test baseline", time.Now(), 1, 1)

	at.runPositionReconciliationCheck("test")

	summary := at.currentPositionReconciliationSummary()
	if summary == nil || summary.Status != PositionReconciliationHealthy {
		t.Fatalf("expected healthy summary after repair, got %+v", summary)
	}
	if summary.TotalIncidents == 0 {
		t.Fatalf("expected incident count to increase")
	}
	local := at.snapshotLocalPositions()
	if len(local) != 1 || local[0].Quantity != 8 {
		t.Fatalf("expected local positions to be repaired from broker truth, got %+v", local)
	}
}

func TestPositionReconciliationFailureBlocksTrading(t *testing.T) {
	tr := &positionReconTestTrader{
		positions: []map[string]interface{}{
			{"symbol": "AAPL", "side": "long", "positionAmt": 8.0, "entryPrice": 151.0, "markPrice": 152.0, "unRealizedProfit": 8.0},
		},
		reconcileErr: errors.New("open orders refresh failed"),
	}
	at := newPositionReconTestAutoTrader(tr)
	at.initializePositionReconciliationState()
	at.setLocalPositionSnapshots([]positions.Snapshot{{Symbol: "AAPL", Side: "long", Quantity: 10, EntryPrice: 150}}, "test", time.Now())
	at.markPositionReconciliationHealthy("test baseline", time.Now(), 1, 1)

	at.runPositionReconciliationCheck("test")

	summary := at.currentPositionReconciliationSummary()
	if summary == nil || summary.Status != PositionReconciliationBlocked {
		t.Fatalf("expected blocked summary after failed repair, got %+v", summary)
	}
	if summary.TradingAllowed {
		t.Fatalf("expected trading to be blocked")
	}
	if err := at.ensurePositionReconciliationReady(); err == nil {
		t.Fatalf("expected position reconciliation gate to block trading")
	}
}

func newPositionReconTestAutoTrader(tr Trader) *AutoTrader {
	at := &AutoTrader{
		id:                     "paper_trader",
		name:                   "Paper Trader",
		exchange:               "ibkr",
		trader:                 tr,
		alertManager:           alerts.NewManager(),
		config:                 AutoTraderConfig{ID: "paper_trader", Name: "Paper Trader", Mode: "paper", Broker: "ibkr", StrategyMode: "multi_factor"},
		positionFirstSeenTime:  map[string]int64{},
		positionEntryCycle:     map[string]int{},
		positionPeakPnLPct:     map[string]float64{},
		positionNewsBias:       map[string]float64{},
		plannedNewsBias:        map[string]float64{},
		localPositionSnapshots: map[string]positions.Snapshot{},
		isRunning:              true,
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: time.Now()})
	at.initializeBrokerRuntimeState()
	return at
}

func clonePositionReconMaps(input []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(input))
	for _, raw := range input {
		cloned := make(map[string]interface{}, len(raw))
		for key, value := range raw {
			cloned[key] = value
		}
		out = append(out, cloned)
	}
	return out
}
