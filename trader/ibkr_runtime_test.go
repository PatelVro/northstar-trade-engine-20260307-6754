package trader

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"northstar/alerts"
	"northstar/broker"
	"northstar/market"
	"sync/atomic"
	"testing"
	"time"
)

type runtimeTestTrader struct {
	reconcileCount int32
	reconcileErr   error
	snapshot       *IBKRBrokerSnapshot
}

func (t *runtimeTestTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *runtimeTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return nil, nil
}

func (t *runtimeTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}

func (t *runtimeTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}

func (t *runtimeTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}

func (t *runtimeTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}

func (t *runtimeTestTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

func (t *runtimeTestTrader) GetMarketPrice(symbol string) (float64, error) {
	return 0, nil
}

func (t *runtimeTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}

func (t *runtimeTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}

func (t *runtimeTestTrader) CancelAllOrders(symbol string) error {
	return nil
}

func (t *runtimeTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}

func (t *runtimeTestTrader) ReconcileBrokerState() (*IBKRBrokerSnapshot, error) {
	atomic.AddInt32(&t.reconcileCount, 1)
	if t.reconcileErr != nil {
		return nil, t.reconcileErr
	}
	if t.snapshot != nil {
		return t.snapshot, nil
	}
	return &IBKRBrokerSnapshot{}, nil
}

func TestHandleIBKRRuntimeError_TransientBlocksTradingWhenRecoveryIsInactive(t *testing.T) {
	at := &AutoTrader{
		name:               "runtime-test",
		id:                 "runtime_test",
		exchange:           "ibkr",
		config:             AutoTraderConfig{Broker: "ibkr", Mode: "paper"},
		alertManager:       alerts.NewManager(),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()

	err := at.handleIBKRRuntimeError("orders", broker.NewIBKRTransportError("GET", "/iserver/account/orders", errors.New("connection refused")))
	if err == nil {
		t.Fatalf("expected runtime error to be returned")
	}

	status := at.brokerRuntimeStatus()
	if status.State != BrokerRuntimeDegraded {
		t.Fatalf("expected degraded state, got %s", status.State)
	}
	if gateErr := at.ensureIBKRRuntimeReady(); gateErr == nil {
		t.Fatalf("expected degraded runtime to block trading")
	}
	if at.currentAlertsSummary().CriticalCount == 0 {
		t.Fatalf("expected broker disconnect alert to be recorded")
	}
}

func TestHandleIBKRRuntimeError_RequestErrorDoesNotChangeHealthyState(t *testing.T) {
	at := &AutoTrader{
		name:               "runtime-test",
		id:                 "runtime_test",
		exchange:           "ibkr",
		config:             AutoTraderConfig{Broker: "ibkr", Mode: "paper"},
		alertManager:       alerts.NewManager(),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()

	_ = at.handleIBKRRuntimeError("resolve_contract", errors.New("no contract found for symbol BAD"))

	status := at.brokerRuntimeStatus()
	if status.State != BrokerRuntimeHealthy {
		t.Fatalf("expected request error to leave runtime healthy, got %s", status.State)
	}
}

func TestRunIBKRRecoveryLoop_ReconcilesBeforeHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iserver/auth/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"authenticated":true,"connected":true}`))
		case "/iserver/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accounts":["DU123456"]}`))
		case "/portfolio/accounts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["DU123456"]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &market.IBKRProvider{
		Client: &broker.IBKRClient{
			BaseURL:    server.URL,
			AccountID:  "DU123456",
			HTTPClient: server.Client(),
		},
	}

	mockTrader := &runtimeTestTrader{
		snapshot: &IBKRBrokerSnapshot{
			Positions: []map[string]interface{}{
				{
					"symbol":           "AAPL",
					"side":             "long",
					"entryPrice":       150.0,
					"markPrice":        152.0,
					"positionAmt":      10.0,
					"unRealizedProfit": 20.0,
					"leverage":         1.0,
					"liquidationPrice": 0.0,
				},
			},
		},
	}

	at := &AutoTrader{
		name:               "runtime-test",
		id:                 "runtime_test",
		exchange:           "ibkr",
		config:             AutoTraderConfig{Broker: "ibkr", Mode: "paper", IBKRAccountID: "DU123456"},
		alertManager:       alerts.NewManager(),
		provider:           provider,
		trader:             mockTrader,
		isRunning:          true,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	defer func() {
		at.isRunning = false
	}()

	_ = at.handleIBKRRuntimeError("orders", broker.NewIBKRTransportError("GET", "/iserver/account/orders", errors.New("connection refused")))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := at.brokerRuntimeStatus()
		if status.State == BrokerRuntimeHealthy && !status.LastReconciledAt.IsZero() && atomic.LoadInt32(&mockTrader.reconcileCount) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	status := at.brokerRuntimeStatus()
	t.Fatalf("expected runtime to recover to healthy after reconciliation, got state=%s reconcile_count=%d", status.State, atomic.LoadInt32(&mockTrader.reconcileCount))
}
