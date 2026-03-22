package startup

import (
	"errors"
	"fmt"
	"northstar/config"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	EnvActiveConfigFile        = "NORTHSTAR_ACTIVE_CONFIG_FILE"
	EnvLiveValidationPassed    = "NORTHSTAR_LIVE_VALIDATION_PASSED"
	EnvLiveValidationConfig    = "NORTHSTAR_LIVE_VALIDATION_CONFIG"
	EnvLiveValidationCheckedAt = "NORTHSTAR_LIVE_VALIDATION_CHECKED_AT"
	EnvLiveValidationSource    = "NORTHSTAR_LIVE_VALIDATION_SOURCE"
	LiveValidationMaxAge       = 10 * time.Minute
)

type LiveValidationStatus struct {
	Required            bool
	Passed              bool
	Fresh               bool
	ConfigMatches       bool
	ActiveConfigFile    string
	ValidatedConfigFile string
	CheckedAt           time.Time
	Source              string
	Message             string
}

func HasEnabledLiveTraders(traders []config.TraderConfig) bool {
	for _, traderCfg := range traders {
		if traderCfg.Enabled && strings.EqualFold(strings.TrimSpace(traderCfg.Mode), "live") {
			return true
		}
	}
	return false
}

func NormalizeConfigPath(configFile string) string {
	trimmed := strings.TrimSpace(configFile)
	if trimmed == "" {
		return ""
	}
	if abs, err := filepath.Abs(trimmed); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(trimmed)
}

func SetActiveConfigFile(configFile string) string {
	normalized := NormalizeConfigPath(configFile)
	_ = os.Setenv(EnvActiveConfigFile, normalized)
	return normalized
}

func CurrentLiveValidationStatus(activeConfigFile string, required bool, now time.Time) LiveValidationStatus {
	if now.IsZero() {
		now = time.Now()
	}

	status := LiveValidationStatus{
		Required:         required,
		ActiveConfigFile: NormalizeConfigPath(activeConfigFile),
		Source:           strings.TrimSpace(os.Getenv(EnvLiveValidationSource)),
	}
	if status.ActiveConfigFile == "" {
		status.ActiveConfigFile = NormalizeConfigPath(os.Getenv(EnvActiveConfigFile))
	}
	status.ValidatedConfigFile = NormalizeConfigPath(os.Getenv(EnvLiveValidationConfig))

	if !required {
		status.Passed = true
		status.Fresh = true
		status.ConfigMatches = true
		status.Message = "live deployment validation not required for this startup"
		return status
	}

	if !strings.EqualFold(strings.TrimSpace(os.Getenv(EnvLiveValidationPassed)), "true") {
		status.Message = "live deployment validation handoff missing; use the live launcher or run validate-live before startup"
		return status
	}

	if status.ValidatedConfigFile == "" {
		status.Message = "live deployment validation handoff is missing the validated config path"
		return status
	}

	if status.ActiveConfigFile != "" && !strings.EqualFold(status.ValidatedConfigFile, status.ActiveConfigFile) {
		status.Message = fmt.Sprintf("live deployment validation config mismatch: validated=%s active=%s", status.ValidatedConfigFile, status.ActiveConfigFile)
		return status
	}
	status.ConfigMatches = true

	checkedAtValue := strings.TrimSpace(os.Getenv(EnvLiveValidationCheckedAt))
	if checkedAtValue == "" {
		status.Message = "live deployment validation handoff is missing the validation timestamp"
		return status
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, checkedAtValue)
	if err != nil {
		status.Message = fmt.Sprintf("live deployment validation timestamp is invalid: %v", err)
		return status
	}
	status.CheckedAt = checkedAt

	if checkedAt.After(now.Add(time.Minute)) {
		status.Message = fmt.Sprintf("live deployment validation timestamp is in the future: %s", checkedAt.Format(time.RFC3339))
		return status
	}

	if now.After(checkedAt) && now.Sub(checkedAt) > LiveValidationMaxAge {
		status.Message = fmt.Sprintf("live deployment validation handoff is stale (%s old)", now.Sub(checkedAt).Round(time.Second))
		return status
	}

	status.Fresh = true
	status.Passed = true
	if status.Source == "" {
		status.Source = "unknown"
	}
	status.Message = fmt.Sprintf("live deployment validation passed at %s via %s", checkedAt.Format(time.RFC3339), status.Source)
	return status
}

func EnsureValidatedLiveStartup(configFile string, traders []config.TraderConfig, now time.Time) (LiveValidationStatus, error) {
	activeConfig := SetActiveConfigFile(configFile)
	status := CurrentLiveValidationStatus(activeConfig, HasEnabledLiveTraders(traders), now)
	if status.Required && !status.Passed {
		return status, errors.New(status.Message)
	}
	return status, nil
}
