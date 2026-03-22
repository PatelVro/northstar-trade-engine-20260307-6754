package main

import (
	"os"
	"path/filepath"
	"testing"

	"northstar/config"
	"northstar/startup"
)

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestRunBlocksLiveStartupWhenValidationMissing(t *testing.T) {
	original := ensureLiveStartupValidation
	defer func() { ensureLiveStartupValidation = original }()

	ensureLiveStartupValidation = func(configFile string, cfg *config.Config) (startup.LiveValidationStatus, error) {
		return startup.LiveValidationStatus{
			Required: true,
			Message:  "live deployment validation handoff missing",
		}, assertErr("live deployment validation handoff missing")
	}

	configPath := writeMainTempConfig(t, `{
		"max_daily_loss": 0.02,
		"max_drawdown": 0.10,
		"stop_trading_minutes": 30,
		"traders": [
			{
				"id": "live_ibkr",
				"name": "Live IBKR",
				"enabled": true,
				"exchange": "ibkr",
				"broker": "ibkr",
				"data_provider": "ibkr",
				"mode": "live",
				"instrument_type": "equity",
				"ai_model": "deepseek",
				"strict_live_mode": true,
				"ibkr_gateway_url": "https://127.0.0.1:5002/v1/api",
				"ibkr_account_id": "DU1234567",
				"deepseek_key": "test-key",
				"initial_balance": 100000,
				"scan_interval_minutes": 5
			}
		]
	}`)
	t.Setenv("CONFIRM_LIVE_TRADING", "true")

	exitCode := run([]string{configPath})
	if exitCode != 1 {
		t.Fatalf("expected live startup to be blocked with exit code 1, got %d", exitCode)
	}
}

func TestLiveStartupValidationHelperAllowsNonLiveConfigs(t *testing.T) {
	original := ensureLiveStartupValidation
	defer func() { ensureLiveStartupValidation = original }()

	ensureLiveStartupValidation = func(configFile string, cfg *config.Config) (startup.LiveValidationStatus, error) {
		return startup.LiveValidationStatus{
			Required: false,
			Passed:   true,
			Fresh:    true,
			Message:  "live deployment validation not required for this startup",
		}, nil
	}

	configPath := writeMainTempConfig(t, `{
		"max_daily_loss": 0.02,
		"max_drawdown": 0.10,
		"stop_trading_minutes": 30,
		"traders": [
			{
				"id": "paper_ibkr",
				"name": "Paper IBKR",
				"enabled": true,
				"exchange": "ibkr",
				"broker": "sim",
				"data_provider": "csv",
				"mode": "replay",
				"csv_data_dir": "testdata",
				"instrument_type": "equity",
				"ai_model": "deepseek",
				"initial_balance": 100000,
				"scan_interval_minutes": 5
			}
		]
	}`)

	status, err := ensureLiveStartupValidation(configPath, &config.Config{
		Traders: []config.TraderConfig{{Enabled: true, Mode: "replay"}},
	})
	if err != nil {
		t.Fatalf("expected non-live validation check to pass, got %v", err)
	}
	if status.Required {
		t.Fatalf("expected non-live validation check not to require live validation")
	}
	if !status.Passed {
		t.Fatalf("expected non-live validation check to pass")
	}
}

func writeMainTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
