package config

import (
	"encoding/json"
	"os"
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
				ID: "replay_1",
				Name: "Replay Test",
				Enabled: true,
				AIModel: "qwen",
				QwenKey: "test",
				Exchange: "alpaca",
				AlpacaAPIKey: "key",
				AlpacaSecretKey: "secret",
				Mode: "replay",
				CSVDataDir: "./data/csv",
				InitialBalance: 1000,
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
