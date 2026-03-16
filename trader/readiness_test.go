package trader

import (
	"net/http"
	"net/http/httptest"
	"northstar/broker"
	"northstar/market"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestRunReadinessChecks_IBKRFailureBlocksTrading(t *testing.T) {
	at := &AutoTrader{
		id:       "ibkr_blocked",
		name:     "IBKR Blocked",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "ibkr_blocked",
			Name:           "IBKR Blocked",
			Mode:           "paper",
			Broker:         "ibkr",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		trader:             &runtimeTestTrader{},
		provider:           &market.IBKRProvider{Client: &broker.IBKRClient{}},
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()

	summary := at.runReadinessChecks()
	if summary.TradingAllowed {
		t.Fatalf("expected trading to be blocked when IBKR readiness fails")
	}
	if summary.Status != ReadinessFail {
		t.Fatalf("expected fail readiness status, got %s", summary.Status)
	}
	check := findReadinessCheck(summary, "broker_config")
	if check == nil || check.Status != ReadinessFail {
		t.Fatalf("expected broker_config readiness failure, got %+v", check)
	}
}

func TestRunReadinessChecks_ReplayCSVSkipsBrokerRequirements(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "AAPL.csv")
	if err := os.WriteFile(csvPath, []byte("timestamp,open,high,low,close,volume\n1700000000,1,1,1,1,100\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	at := &AutoTrader{
		id:       "replay_csv",
		name:     "Replay CSV",
		aiModel:  "deepseek",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:             "replay_csv",
			Name:           "Replay CSV",
			Mode:           "replay",
			Broker:         "sim",
			DataProvider:   "csv",
			CSVDataDir:     dir,
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		trader:             &runtimeTestTrader{},
		provider:           market.NewCSVProvider(dir),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()

	summary := at.runReadinessChecks()
	if !summary.TradingAllowed {
		t.Fatalf("expected replay/csv/sim readiness to allow trading, got %+v", summary)
	}
	if check := findReadinessCheck(summary, "broker_config"); check == nil || check.Status != ReadinessPass {
		t.Fatalf("expected broker_config check to pass/skip for replay, got %+v", check)
	}
}

func TestRunReadinessChecks_ReplayCSVSkipsIBKRSessionWhenBrokerIsSim(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "AAPL.csv")
	if err := os.WriteFile(csvPath, []byte("timestamp,open,high,low,close,volume\n1700000000,1,1,1,1,100\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	at := &AutoTrader{
		id:       "replay_csv_ibkr_exchange",
		name:     "Replay CSV IBKR Exchange",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "replay_csv_ibkr_exchange",
			Name:           "Replay CSV IBKR Exchange",
			Mode:           "replay",
			Broker:         "sim",
			DataProvider:   "csv",
			CSVDataDir:     dir,
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		trader:             &runtimeTestTrader{},
		provider:           market.NewCSVProvider(dir),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()

	summary := at.runReadinessChecks()
	if !summary.TradingAllowed {
		t.Fatalf("expected replay/csv/sim readiness to allow trading even with exchange=ibkr, got %+v", summary)
	}
	if check := findReadinessCheck(summary, "broker_connectivity"); check == nil || check.Status != ReadinessPass {
		t.Fatalf("expected broker_connectivity to skip/pass for replay csv sim, got %+v", check)
	}
}

func TestRunReadinessChecks_AIMissingFailsWhenStrategyRequiresIt(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "AAPL.csv")
	if err := os.WriteFile(csvPath, []byte("timestamp,open,high,low,close,volume\n1700000000,1,1,1,1,100\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	at := &AutoTrader{
		id:       "ai_required",
		name:     "AI Required",
		aiModel:  "deepseek",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:             "ai_required",
			Name:           "AI Required",
			Mode:           "replay",
			Broker:         "sim",
			DataProvider:   "csv",
			CSVDataDir:     dir,
			InstrumentType: "equity",
			StrategyMode:   "ai_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		trader:             &runtimeTestTrader{},
		provider:           market.NewCSVProvider(dir),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()

	summary := at.runReadinessChecks()
	if summary.TradingAllowed {
		t.Fatalf("expected missing AI config to block trading")
	}
	if check := findReadinessCheck(summary, "ai_readiness"); check == nil || check.Status != ReadinessFail {
		t.Fatalf("expected ai_readiness check to fail, got %+v", check)
	}
}

func TestRunReadinessChecks_IBKRSuccessRequiresBootstrapReconciliation(t *testing.T) {
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
			Balance:    map[string]interface{}{"accountEquity": 100000.0},
			Positions:  []map[string]interface{}{},
			OpenOrders: []map[string]interface{}{},
		},
	}

	at := &AutoTrader{
		id:       "ibkr_ready",
		name:     "IBKR Ready",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "ibkr_ready",
			Name:           "IBKR Ready",
			Mode:           "paper",
			Broker:         "ibkr",
			DataProvider:   "ibkr",
			IBKRGatewayURL: server.URL,
			IBKRAccountID:  "DU123456",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		trader:             mockTrader,
		provider:           provider,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()

	summary := at.runReadinessChecks()
	if !summary.TradingAllowed || summary.Status == ReadinessFail {
		t.Fatalf("expected IBKR readiness to pass, got %+v", summary)
	}
	if atomic.LoadInt32(&mockTrader.reconcileCount) == 0 {
		t.Fatalf("expected readiness to require bootstrap reconciliation")
	}
	if check := findReadinessCheck(summary, "broker_bootstrap"); check == nil || check.Status != ReadinessPass {
		t.Fatalf("expected broker_bootstrap pass, got %+v", check)
	}
}

func findReadinessCheck(summary ReadinessSummary, name string) *ReadinessCheck {
	for i := range summary.Checks {
		if summary.Checks[i].Name == name {
			return &summary.Checks[i]
		}
	}
	return nil
}
