package trader

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// minimalAutoTrader returns a minimal AutoTrader suitable for startup check tests.
func minimalStartupTrader(cfg AutoTraderConfig) *AutoTrader {
	at := &AutoTrader{
		id:                 cfg.ID,
		name:               cfg.Name,
		aiModel:            cfg.AIModel,
		exchange:           cfg.Exchange,
		config:             cfg,
		initialBalance:     cfg.InitialBalance,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
		newsCredibility:    map[string]float64{},
		newsSampleCount:    map[string]int{},
		symbolEdgeScore:    map[string]float64{},
		symbolTradeCount:   map[string]int{},
		symbolCooldownUntil: map[string]int{},
	}
	return at
}

// --- Config sanity checks ---

func TestStartupCheck_ConfigSanity_Pass(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "test_trader",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Broker:         "sim",
		InitialBalance: 10000,
		DeepSeekKey:    "sk-test-key",
	})
	check := checkStartupConfigSanity(at)
	if !check.Passed {
		t.Fatalf("expected config sanity to pass, got: %s / action: %s", check.Detail, check.Action)
	}
}

func TestStartupCheck_ConfigSanity_MissingID(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		InitialBalance: 10000,
	})
	check := checkStartupConfigSanity(at)
	if check.Passed {
		t.Fatal("expected config sanity to fail when trader ID is missing")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing trader ID")
	}
}

func TestStartupCheck_ConfigSanity_MissingMode(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "test_trader",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "",
		InitialBalance: 10000,
	})
	check := checkStartupConfigSanity(at)
	if check.Passed {
		t.Fatal("expected config sanity to fail when mode is missing")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing mode")
	}
}

func TestStartupCheck_ConfigSanity_ZeroBalance(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "test_trader",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		InitialBalance: 0,
	})
	check := checkStartupConfigSanity(at)
	if check.Passed {
		t.Fatal("expected config sanity to fail when initial_balance is zero")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for zero initial_balance")
	}
}

// --- Credential checks ---

func TestStartupCheck_Credentials_DeepSeekPresent(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:          "test_trader",
		Name:        "Test Trader",
		AIModel:     "deepseek",
		Mode:        "paper",
		DeepSeekKey: "sk-real-key",
	})
	check := checkStartupCredentials(at)
	if !check.Passed {
		t.Fatalf("expected credentials check to pass with DeepSeek key set: %s", check.Detail)
	}
}

func TestStartupCheck_Credentials_DeepSeekMissing(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:          "test_trader",
		Name:        "Test Trader",
		AIModel:     "deepseek",
		Mode:        "paper",
		DeepSeekKey: "",
	})
	check := checkStartupCredentials(at)
	if check.Passed {
		t.Fatal("expected credentials check to fail when DeepSeek key is absent")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing DeepSeek key")
	}
}

func TestStartupCheck_Credentials_QwenMissing(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:      "test_trader",
		Name:    "Test Trader",
		AIModel: "qwen",
		Mode:    "paper",
		QwenKey: "",
	})
	check := checkStartupCredentials(at)
	if check.Passed {
		t.Fatal("expected credentials check to fail when Qwen key is absent")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing Qwen key")
	}
}

func TestStartupCheck_Credentials_CustomMissingURL(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:           "test_trader",
		Name:         "Test Trader",
		AIModel:      "custom",
		Mode:         "paper",
		CustomAPIURL: "",
		CustomAPIKey: "apikey",
	})
	check := checkStartupCredentials(at)
	if check.Passed {
		t.Fatal("expected credentials check to fail when custom AI URL is missing")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing custom AI URL")
	}
}

func TestStartupCheck_Credentials_DemoMode(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:       "demo_trader",
		Name:     "Demo Trader",
		AIModel:  "deepseek",
		Mode:     "paper",
		DemoMode: true,
	})
	at.demoMode = true
	check := checkStartupCredentials(at)
	if !check.Passed {
		t.Fatalf("expected credentials check to pass in demo mode: %s", check.Detail)
	}
}

// --- Data file checks ---

func TestStartupCheck_DataFiles_NoFileConfigured(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		TrustedSymbolsFile: "",
	})
	check := checkStartupDataFiles(at)
	if !check.Passed {
		t.Fatalf("expected data files check to pass when no file is configured: %s", check.Detail)
	}
}

func TestStartupCheck_DataFiles_FileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "symbols.txt")
	if err := os.WriteFile(path, []byte("AAPL\nMSFT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := minimalStartupTrader(AutoTraderConfig{
		TrustedSymbolsFile: path,
	})
	check := checkStartupDataFiles(at)
	if !check.Passed {
		t.Fatalf("expected data files check to pass when file exists: %s", check.Detail)
	}
}

func TestStartupCheck_DataFiles_FileMissing(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		TrustedSymbolsFile: "/nonexistent/path/symbols.txt",
	})
	check := checkStartupDataFiles(at)
	if check.Passed {
		t.Fatal("expected data files check to fail when file does not exist")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing symbols file")
	}
}

func TestStartupCheck_DataFiles_FileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "symbols.txt")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	at := minimalStartupTrader(AutoTraderConfig{
		TrustedSymbolsFile: path,
	})
	check := checkStartupDataFiles(at)
	if check.Passed {
		t.Fatal("expected data files check to fail when file is empty")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for empty symbols file")
	}
}

// --- Broker connectivity checks ---

func TestStartupCheck_BrokerConnectivity_DemoMode(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		Mode:     "paper",
		Broker:   "sim",
		DemoMode: true,
	})
	at.demoMode = true
	check := checkStartupBrokerConnectivity(at)
	if !check.Passed {
		t.Fatalf("expected broker connectivity to pass in demo mode: %s", check.Detail)
	}
}

func TestStartupCheck_BrokerConnectivity_SimBroker(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		Mode:   "paper",
		Broker: "sim",
	})
	check := checkStartupBrokerConnectivity(at)
	if !check.Passed {
		t.Fatalf("expected broker connectivity to pass for sim broker: %s", check.Detail)
	}
}

func TestStartupCheck_BrokerConnectivity_IBKRReachable(t *testing.T) {
	// Start a fake IBKR gateway
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "ibkr_trader",
		Name:           "IBKR Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Exchange:       "ibkr",
		Broker:         "ibkr",
		DataProvider:   "ibkr",
		InstrumentType: "equity",
		IBKRGatewayURL: srv.URL,
		IBKRAccountID:  "U123456",
	})
	check := checkStartupBrokerConnectivity(at)
	if !check.Passed {
		t.Fatalf("expected broker connectivity to pass when gateway is reachable: %s / action: %s", check.Detail, check.Action)
	}
}

func TestStartupCheck_BrokerConnectivity_IBKRUnreachable(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "ibkr_trader",
		Name:           "IBKR Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Exchange:       "ibkr",
		Broker:         "ibkr",
		DataProvider:   "ibkr",
		InstrumentType: "equity",
		IBKRGatewayURL: "http://127.0.0.1:19999", // nothing listening here
		IBKRAccountID:  "U123456",
	})
	check := checkStartupBrokerConnectivity(at)
	if check.Passed {
		t.Fatal("expected broker connectivity to fail when gateway is unreachable")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for unreachable IBKR gateway")
	}
}

func TestStartupCheck_BrokerConnectivity_IBKRMissingGatewayURL(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "ibkr_trader",
		Name:           "IBKR Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Exchange:       "ibkr",
		Broker:         "ibkr",
		DataProvider:   "ibkr",
		InstrumentType: "equity",
		IBKRGatewayURL: "",
		IBKRAccountID:  "U123456",
	})
	check := checkStartupBrokerConnectivity(at)
	if check.Passed {
		t.Fatal("expected broker connectivity to fail when IBKRGatewayURL is empty")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for missing gateway URL")
	}
}

// --- AI endpoint checks ---

func TestStartupCheck_AIEndpoint_ManagedProvider(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		AIModel: "deepseek",
	})
	check := checkStartupAIEndpoint(at)
	if !check.Passed {
		t.Fatalf("expected AI endpoint check to pass for managed provider: %s", check.Detail)
	}
}

func TestStartupCheck_AIEndpoint_CustomReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	at := minimalStartupTrader(AutoTraderConfig{
		AIModel:         "custom",
		CustomAPIURL:    srv.URL,
		CustomAPIKey:    "key",
		CustomModelName: "model",
	})
	check := checkStartupAIEndpoint(at)
	if !check.Passed {
		t.Fatalf("expected AI endpoint check to pass when custom endpoint is reachable: %s", check.Detail)
	}
}

func TestStartupCheck_AIEndpoint_CustomUnreachable(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		AIModel:         "custom",
		CustomAPIURL:    "http://127.0.0.1:19998", // nothing listening
		CustomAPIKey:    "key",
		CustomModelName: "model",
	})
	check := checkStartupAIEndpoint(at)
	if check.Passed {
		t.Fatal("expected AI endpoint check to fail when custom endpoint is unreachable")
	}
	if check.Action == "" {
		t.Fatal("expected non-empty Action for unreachable custom AI endpoint")
	}
}

// --- Full report ---

func TestRunStartupSelfCheck_AllPassed(t *testing.T) {
	dir := t.TempDir()
	symbolsPath := filepath.Join(dir, "symbols.txt")
	if err := os.WriteFile(symbolsPath, []byte("AAPL\nMSFT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	at := minimalStartupTrader(AutoTraderConfig{
		ID:                 "test_trader",
		Name:               "Test Trader",
		AIModel:            "deepseek",
		Mode:               "paper",
		Broker:             "sim",
		InitialBalance:     10000,
		DeepSeekKey:        "sk-test-key",
		TrustedSymbolsFile: symbolsPath,
	})

	report := RunStartupSelfCheck(at)
	if !report.AllPassed {
		for _, c := range report.Checks {
			if !c.Passed {
				t.Errorf("check %q failed: %s | action: %s", c.Name, c.Detail, c.Action)
			}
		}
		t.FailNow()
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected at least one check in the report")
	}
}

func TestRunStartupSelfCheck_PartialFailure(t *testing.T) {
	// Missing ID → config_sanity should fail
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Broker:         "sim",
		InitialBalance: 10000,
		DeepSeekKey:    "sk-test-key",
	})

	report := RunStartupSelfCheck(at)
	if report.AllPassed {
		t.Fatal("expected AllPassed to be false when config_sanity fails")
	}

	var configCheck *StartupCheck
	for i := range report.Checks {
		if report.Checks[i].Name == "config_sanity" {
			configCheck = &report.Checks[i]
			break
		}
	}
	if configCheck == nil {
		t.Fatal("expected config_sanity check to be present in report")
	}
	if configCheck.Passed {
		t.Fatal("expected config_sanity check to be failed")
	}
	if configCheck.Action == "" {
		t.Fatal("expected non-empty Action for failed config_sanity check")
	}
}

func TestLogStartupCheckReport_DoesNotPanic(t *testing.T) {
	at := minimalStartupTrader(AutoTraderConfig{
		ID:             "test_trader",
		Name:           "Test Trader",
		AIModel:        "deepseek",
		Mode:           "paper",
		Broker:         "sim",
		InitialBalance: 10000,
		DeepSeekKey:    "sk-key",
	})
	report := RunStartupSelfCheck(at)
	// Should not panic
	LogStartupCheckReport(at, report)
}
