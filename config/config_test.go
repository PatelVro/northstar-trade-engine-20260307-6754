package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAlpacaConfigDefaults(t *testing.T) {
	// Create a temporary JSON file with minimal Alpaca settings
	mockConfig := `
	{
		"use_default_coins": false,
		"default_coins": ["AAPL", "MSFT", "TSLA"],
		"coin_pool_api_url": "",
		"oi_top_api_url": "",
		"api_server_port": 8080,
		"max_daily_loss": -50.0,
		"max_drawdown": -100.0,
		"stop_trading_minutes": 60,
		"leverage": {
		  "btc_eth_leverage": 5,
		  "altcoin_leverage": 5
		},
		"traders": [
		  {
			"id": "alpaca_test_1",
			"name": "Alpaca Paper Bot",
			"enabled": true,
			"ai_model": "qwen",
			"qwen_key": "test_key",
			"exchange": "alpaca",
			"alpaca_api_key": "PKB2XXXX",
			"alpaca_secret_key": "YYYYY",
			"alpaca_paper_trading": true,
			"initial_balance": 1000.0,
			"scan_interval_minutes": 5
		  }
		]
	}
	`

	tmpFile, err := os.CreateTemp("", "config_test_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(mockConfig)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Load the config
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Traders) != 1 {
		t.Fatalf("Expected 1 trader, got %d", len(cfg.Traders))
	}

	trader := cfg.Traders[0]

	// Verify the injected defaults specifically for Alpaca
	if trader.InstrumentType != "equity" {
		t.Errorf("Expected InstrumentType 'equity', got '%s'", trader.InstrumentType)
	}
	if trader.Mode != "paper" {
		t.Errorf("Expected Mode 'paper', got '%s'", trader.Mode)
	}
	if trader.DataProvider != "alpaca" {
		t.Errorf("Expected DataProvider 'alpaca', got '%s'", trader.DataProvider)
	}
	if trader.Broker != "alpaca" {
		t.Errorf("Expected Broker 'alpaca', got '%s'", trader.Broker)
	}
	if trader.OrderSizingMode != "qty" {
		t.Errorf("Expected OrderSizingMode 'qty', got '%s'", trader.OrderSizingMode)
	}
	if trader.BarsAdjustment != "split" {
		t.Errorf("Expected BarsAdjustment 'split', got '%s'", trader.BarsAdjustment)
	}
	if trader.MaxGrossExposure != 1.0 {
		t.Errorf("Expected MaxGrossExposure 1.0, got %f", trader.MaxGrossExposure)
	}
	if trader.MaxPositionPct != 0.20 {
		t.Errorf("Expected MaxPositionPct 0.20, got %f", trader.MaxPositionPct)
	}
}

func TestAlpacaReplayConfig(t *testing.T) {
	// Setup a trader with mode "replay"
	cfg := Config{
		Traders: []TraderConfig{
			{
				ID:              "replay_1",
				Name:            "Replay Test",
				Enabled:         true,
				AIModel:         "qwen",
				QwenKey:         "test",
				Exchange:        "alpaca",
				AlpacaAPIKey:    "key",
				AlpacaSecretKey: "secret",
				Mode:            "replay",
				CSVDataDir:      "./data/csv",
				InitialBalance:  1000,
			},
		},
	}

	// Marshall and unmarshal to trigger Validate logic easily
	jsonData, _ := json.Marshal(cfg)

	tmpFile, _ := os.CreateTemp("", "config_test_replay_*.json")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(jsonData)
	tmpFile.Close()

	loadedCfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	trader := loadedCfg.Traders[0]
	if trader.DataProvider != "csv" {
		t.Errorf("Expected DataProvider 'csv', got '%s'", trader.DataProvider)
	}
	if trader.Broker != "sim" {
		t.Errorf("Expected Broker 'sim', got '%s'", trader.Broker)
	}
}

func TestLoadConfigResolvesEnvPlaceholders(t *testing.T) {
	t.Setenv("NORTHSTAR_DEEPSEEK_API_KEY", "deepseek-from-env")
	t.Setenv("NORTHSTAR_IBKR_ACCOUNT_ID", "DU1234567")
	t.Setenv("NORTHSTAR_IBKR_BASE_URL", "https://127.0.0.1:5002/v1/api")

	mockConfig := `{
		"traders": [
			{
				"id": "ibkr_env",
				"name": "IBKR Env",
				"enabled": true,
				"ai_model": "deepseek",
				"exchange": "ibkr",
				"mode": "paper",
				"data_provider": "ibkr",
				"broker": "ibkr",
				"ibkr_gateway_url": "${NORTHSTAR_IBKR_BASE_URL}",
				"ibkr_account_id": "${NORTHSTAR_IBKR_ACCOUNT_ID}",
				"deepseek_key": "${NORTHSTAR_DEEPSEEK_API_KEY}",
				"initial_balance": 1000.0,
				"scan_interval_minutes": 5
			}
		]
	}`

	tmpFile, err := os.CreateTemp("", "config_env_test_*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(mockConfig)); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	trader := cfg.Traders[0]
	if trader.DeepSeekKey != "deepseek-from-env" {
		t.Fatalf("expected deepseek env value, got %q", trader.DeepSeekKey)
	}
	if trader.IBKRAccountID != "DU1234567" {
		t.Fatalf("expected IBKR account env value, got %q", trader.IBKRAccountID)
	}
	if trader.IBKRGatewayURL != "https://127.0.0.1:5002/v1/api" {
		t.Fatalf("expected IBKR gateway env value, got %q", trader.IBKRGatewayURL)
	}
}

func TestLoadConfigAppliesLocalOverride(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config_ibkr.json")
	overridePath := filepath.Join(dir, "config_ibkr.local.json")

	baseConfig := `{
		"traders": [
			{
				"id": "ibkr_local",
				"name": "Base Name",
				"enabled": true,
				"ai_model": "deepseek",
				"exchange": "ibkr",
				"mode": "paper",
				"data_provider": "ibkr",
				"broker": "ibkr",
				"ibkr_gateway_url": "https://127.0.0.1:5002/v1/api",
				"ibkr_account_id": "${NORTHSTAR_IBKR_ACCOUNT_ID}",
				"deepseek_key": "${NORTHSTAR_DEEPSEEK_API_KEY}",
				"initial_balance": 1000.0,
				"scan_interval_minutes": 5
			}
		]
	}`
	overrideConfig := `{
		"traders": [
			{
				"id": "ibkr_local",
				"name": "Override Name",
				"enabled": true,
				"ai_model": "deepseek",
				"exchange": "ibkr",
				"mode": "paper",
				"data_provider": "ibkr",
				"broker": "ibkr",
				"ibkr_gateway_url": "https://127.0.0.1:5002/v1/api",
				"ibkr_account_id": "DU7654321",
				"deepseek_key": "local-file-key",
				"initial_balance": 1000.0,
				"scan_interval_minutes": 5
			}
		]
	}`

	if err := os.WriteFile(basePath, []byte(baseConfig), 0644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(overrideConfig), 0644); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	cfg, err := LoadConfig(basePath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	trader := cfg.Traders[0]
	if trader.Name != "Override Name" {
		t.Fatalf("expected override trader name, got %q", trader.Name)
	}
	if trader.DeepSeekKey != "local-file-key" {
		t.Fatalf("expected override deepseek key, got %q", trader.DeepSeekKey)
	}
	if trader.IBKRAccountID != "DU7654321" {
		t.Fatalf("expected override account ID, got %q", trader.IBKRAccountID)
	}
}

func TestLoadConfigDefaultCoinsFileExtendsInlineDefaults(t *testing.T) {
	dir := t.TempDir()
	coinsFile := filepath.Join(dir, "symbols.txt")
	configFile := filepath.Join(dir, "config.json")
	t.Setenv("NORTHSTAR_IBKR_ACCOUNT_ID", "DU123456")

	if err := os.WriteFile(coinsFile, []byte("ABBV\nAAPL\nABNB\n"), 0644); err != nil {
		t.Fatalf("write symbols file: %v", err)
	}

	configJSON := `{
		"use_default_coins": true,
		"default_coins": ["AAPL", "MSFT", "NVDA"],
		"default_coins_file": "symbols.txt",
		"traders": [
			{
				"id": "ibkr_shadow",
				"name": "IBKR Shadow",
				"enabled": true,
				"ai_model": "custom",
				"exchange": "ibkr",
				"mode": "shadow",
				"data_provider": "ibkr",
				"broker": "sim",
				"ibkr_gateway_url": "https://127.0.0.1:5002/v1/api",
				"ibkr_account_id": "${NORTHSTAR_IBKR_ACCOUNT_ID}",
				"custom_api_url": "https://example.com/v1",
				"custom_api_key": "key",
				"custom_model_name": "model",
				"initial_balance": 1000.0,
				"scan_interval_minutes": 5
			}
		]
	}`
	if err := os.WriteFile(configFile, []byte(configJSON), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	wantPrefix := []string{"AAPL", "MSFT", "NVDA", "ABBV", "ABNB"}
	if len(cfg.DefaultCoins) < len(wantPrefix) {
		t.Fatalf("expected at least %d default coins, got %d", len(wantPrefix), len(cfg.DefaultCoins))
	}
	for i, want := range wantPrefix {
		if got := cfg.DefaultCoins[i]; got != want {
			t.Fatalf("expected default coin %d to be %s, got %s", i, want, got)
		}
	}
}

func TestAlpacaReplayWithoutSecretsUsesLocalCSV(t *testing.T) {
	cfg := Config{
		Traders: []TraderConfig{
			{
				ID:                  "replay_local",
				Name:                "Replay Local CSV",
				Enabled:             true,
				AIModel:             "deepseek",
				DeepSeekKey:         "test-key",
				Exchange:            "alpaca",
				Mode:                "replay",
				DataProvider:        "csv",
				Broker:              "sim",
				CSVDataDir:          "./data/csv",
				InstrumentType:      "equity",
				InitialBalance:      1000,
				ScanIntervalMinutes: 5,
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "config_test_replay_local_*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	if _, err := LoadConfig(tmpFile.Name()); err != nil {
		t.Fatalf("expected replay config without Alpaca secrets to load, got error: %v", err)
	}
}
