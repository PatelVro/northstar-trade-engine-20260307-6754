package startup

import (
	"path/filepath"
	"testing"
	"time"

	"northstar/config"
)

func TestEnsureValidatedLiveStartupRequiresFreshHandoffForLive(t *testing.T) {
	t.Setenv(EnvLiveValidationPassed, "")
	t.Setenv(EnvLiveValidationConfig, "")
	t.Setenv(EnvLiveValidationCheckedAt, "")
	t.Setenv(EnvLiveValidationSource, "")

	_, err := EnsureValidatedLiveStartup("config_ibkr_live.json", []config.TraderConfig{
		{Enabled: true, Mode: "live"},
	}, time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("expected missing validation handoff to block live startup")
	}
}

func TestEnsureValidatedLiveStartupRejectsConfigMismatch(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	t.Setenv(EnvLiveValidationPassed, "true")
	t.Setenv(EnvLiveValidationConfig, filepath.Join("C:\\repo", "config_a.json"))
	t.Setenv(EnvLiveValidationCheckedAt, now.Format(time.RFC3339Nano))
	t.Setenv(EnvLiveValidationSource, "run_ibkr_live.cmd")

	status, err := EnsureValidatedLiveStartup(filepath.Join("C:\\repo", "config_b.json"), []config.TraderConfig{
		{Enabled: true, Mode: "live"},
	}, now)
	if err == nil {
		t.Fatalf("expected config mismatch to block startup")
	}
	if status.ConfigMatches {
		t.Fatalf("expected config mismatch to be reported")
	}
}

func TestEnsureValidatedLiveStartupRejectsStaleValidation(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 20, 0, 0, time.UTC)
	t.Setenv(EnvLiveValidationPassed, "true")
	t.Setenv(EnvLiveValidationConfig, filepath.Join("C:\\repo", "config_ibkr_live.json"))
	t.Setenv(EnvLiveValidationCheckedAt, now.Add(-LiveValidationMaxAge-time.Minute).Format(time.RFC3339Nano))
	t.Setenv(EnvLiveValidationSource, "run_ibkr_live.cmd")

	status, err := EnsureValidatedLiveStartup(filepath.Join("C:\\repo", "config_ibkr_live.json"), []config.TraderConfig{
		{Enabled: true, Mode: "live"},
	}, now)
	if err == nil {
		t.Fatalf("expected stale validation to block startup")
	}
	if status.Fresh {
		t.Fatalf("expected stale validation to be marked non-fresh")
	}
}

func TestEnsureValidatedLiveStartupRejectsFutureValidationTimestamp(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	configPath := filepath.Join("C:\\repo", "config_ibkr_live.json")
	t.Setenv(EnvLiveValidationPassed, "true")
	t.Setenv(EnvLiveValidationConfig, configPath)
	t.Setenv(EnvLiveValidationCheckedAt, now.Add(2*time.Minute).Format(time.RFC3339Nano))
	t.Setenv(EnvLiveValidationSource, "run_ibkr_live.cmd")

	status, err := EnsureValidatedLiveStartup(configPath, []config.TraderConfig{
		{Enabled: true, Mode: "live"},
	}, now)
	if err == nil {
		t.Fatalf("expected future timestamp to block startup")
	}
	if status.Passed {
		t.Fatalf("expected future timestamp not to pass")
	}
}

func TestEnsureValidatedLiveStartupPassesWithFreshMatchingValidation(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	configPath := filepath.Join("C:\\repo", "config_ibkr_live.json")
	t.Setenv(EnvLiveValidationPassed, "true")
	t.Setenv(EnvLiveValidationConfig, configPath)
	t.Setenv(EnvLiveValidationCheckedAt, now.Format(time.RFC3339Nano))
	t.Setenv(EnvLiveValidationSource, "run_ibkr_live.cmd")

	status, err := EnsureValidatedLiveStartup(configPath, []config.TraderConfig{
		{Enabled: true, Mode: "live"},
	}, now)
	if err != nil {
		t.Fatalf("expected fresh matching validation to pass, got %v", err)
	}
	if !status.Passed {
		t.Fatalf("expected validation status to pass")
	}
	if !status.Fresh {
		t.Fatalf("expected validation status to be fresh")
	}
	if !status.ConfigMatches {
		t.Fatalf("expected config match to be true")
	}
}

func TestEnsureValidatedLiveStartupSkipsNonLiveConfigs(t *testing.T) {
	status, err := EnsureValidatedLiveStartup("config_ibkr_shadow.json", []config.TraderConfig{
		{Enabled: true, Mode: "shadow"},
	}, time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expected non-live startup to skip live validation, got %v", err)
	}
	if !status.Passed {
		t.Fatalf("expected non-live startup to be treated as passed")
	}
	if !status.Fresh {
		t.Fatalf("expected non-live startup to be marked fresh")
	}
}
