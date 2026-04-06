package trader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"northstar/broker"
	"northstar/logger"
	"northstar/market"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	if cached, _, ok := at.currentRuntimeAccountSnapshot(runtimeAccountSnapshotTTL); !ok || cached == nil || cached.AccountEquity != 100000.0 {
		t.Fatalf("expected broker bootstrap to seed runtime account snapshot, got summary=%+v ok=%t", cached, ok)
	}
}

func TestPersistStartupReadinessBlockedDecisionWritesRecord(t *testing.T) {
	logDir := t.TempDir()
	at := &AutoTrader{
		id:        "paper_readiness_blocked",
		name:      "Paper Readiness Blocked",
		isRunning: true,
		config: AutoTraderConfig{
			ID:             "paper_readiness_blocked",
			Name:           "Paper Readiness Blocked",
			Mode:           "paper",
			Broker:         "ibkr",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			InitialBalance: 100000,
		},
		initialBalance:     100000,
		decisionLogger:     logger.NewDecisionLogger(logDir),
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
	}
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	at.initializeReadinessSummary()

	summary := ReadinessSummary{
		Status:         ReadinessFail,
		Message:        "1 blocking readiness check(s) failed",
		TradingAllowed: false,
		Checks: []ReadinessCheck{
			{
				Name:           "broker_bootstrap",
				Status:         ReadinessFail,
				Severity:       ReadinessSeverityCritical,
				Message:        "broker bootstrap reconciliation failed: account summary unavailable",
				TradingAllowed: false,
			},
			{
				Name:           "restart_recovery",
				Status:         ReadinessWarn,
				Severity:       ReadinessSeverityWarning,
				Message:        "durable runtime state restored; broker reconciliation must confirm orders and positions before trading resumes",
				TradingAllowed: false,
			},
		},
	}

	at.persistStartupReadinessBlockedDecision(summary)

	records, err := at.decisionLogger.GetLatestRecords(1)
	if err != nil {
		t.Fatalf("GetLatestRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 decision record, got %d", len(records))
	}
	record := records[0]
	if record.CycleNumber != 1 {
		t.Fatalf("expected first blocked readiness record to be cycle 1, got %d", record.CycleNumber)
	}
	if record.Success {
		t.Fatalf("expected blocked readiness record to be unsuccessful")
	}
	if record.ErrorMessage != summary.Message {
		t.Fatalf("expected error message %q, got %q", summary.Message, record.ErrorMessage)
	}
	joinedLog := strings.Join(record.ExecutionLog, "\n")
	if !strings.Contains(joinedLog, "startup readiness broker_bootstrap (fail): broker bootstrap reconciliation failed: account summary unavailable") {
		t.Fatalf("expected blocked readiness detail in execution log, got %q", joinedLog)
	}
}

func TestClassifyIBKRStartupReadinessFailure_NightlyResetWindow(t *testing.T) {
	at := &AutoTrader{
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:         "paper",
			Broker:       "ibkr",
			DataProvider: "ibkr",
		},
	}
	err := fmt.Errorf("account summary refresh failed: failed to fetch IBKR account summary: GET: /portfolio/DUP200062/summary: HTTP 503: {\"error\":\"Service Unavailable\",\"statusCode\":503}")

	message, expectedMaintenance := at.classifyIBKRStartupReadinessFailure(
		"broker_bootstrap",
		err,
		time.Date(2026, time.March, 27, 1, 15, 0, 0, time.Local),
	)
	if !expectedMaintenance {
		t.Fatalf("expected nightly reset window to be classified as expected maintenance")
	}
	if !strings.Contains(message, "IBKR nightly reset window is active") {
		t.Fatalf("expected maintenance message, got %q", message)
	}
	if !strings.Contains(message, "broker_bootstrap") {
		t.Fatalf("expected stage to be preserved, got %q", message)
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
