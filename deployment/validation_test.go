package deployment

import (
	"testing"
	"time"

	"northstar/buildinfo"
	"northstar/config"
	"northstar/trader"
)

type fakeLiveTrader struct {
	validation trader.LiveStartValidation
}

func (f fakeLiveTrader) ValidateLiveStart() trader.LiveStartValidation {
	return f.validation
}

type fakeGitInspector struct {
	status GitStatus
	err    error
}

func (f fakeGitInspector) Inspect(startDir string) (GitStatus, error) {
	return f.status, f.err
}

func TestBuildIdentityCheckFailsForLocalBuild(t *testing.T) {
	check := buildIdentityCheck(buildinfo.Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
		Channel:   "local",
		Dirty:     "dirty",
	}, time.Unix(0, 0))

	if check.Status != StatusFail {
		t.Fatalf("expected fail, got %s", check.Status)
	}
}

func TestRiskLimitsCheckFailsWhenLiveRiskBoundsMissing(t *testing.T) {
	check := riskLimitsCheck(&config.Config{}, config.TraderConfig{
		ID:                   "live_ibkr",
		Exchange:             "ibkr",
		InstrumentType:       "equity",
		MaxConcurrentPos:     0,
		MaxGrossExposure:     0,
		MaxPositionPct:       0,
		MaxDailyLossPct:      0,
		MaxNetExposurePct:    0,
		MaxSectorExposurePct: 0,
	}, time.Unix(0, 0))

	if check.Status != StatusFail {
		t.Fatalf("expected fail, got %s", check.Status)
	}
}

func TestValidateLiveConfigFailsWhenNoEnabledLiveTraders(t *testing.T) {
	validator := &Validator{
		Now: func() time.Time { return time.Unix(0, 0) },
		BuildInfo: func() buildinfo.Info {
			return buildinfo.Info{
				Version:   "v1.0.0",
				Commit:    "abcdef123456",
				BuildTime: "2026-03-15T00:00:00Z",
				Channel:   "release",
				Dirty:     "clean",
			}
		},
		GitInspector: fakeGitInspector{status: GitStatus{Root: "C:/repo"}},
	}

	configPath := writeTempConfig(t, `{
		"max_daily_loss": 0.02,
		"max_drawdown": 0.10,
		"stop_trading_minutes": 30,
		"traders": [
			{
				"id": "paper_only",
				"name": "Paper Only",
				"enabled": true,
				"exchange": "ibkr",
				"broker": "ibkr",
				"data_provider": "ibkr",
				"mode": "paper",
				"instrument_type": "equity",
				"ai_model": "deepseek",
				"initial_balance": 100000,
				"scan_interval_minutes": 5
			}
		]
	}`)

	summary := validator.ValidateLiveConfig(configPath)
	if summary.LiveReady {
		t.Fatalf("expected validation to fail when no live traders are configured")
	}
}

func TestValidateLiveConfigFailsWhenTraderValidationFails(t *testing.T) {
	validator := &Validator{
		Now: func() time.Time { return time.Unix(0, 0) },
		BuildInfo: func() buildinfo.Info {
			return buildinfo.Info{
				Version:   "v1.0.0",
				Commit:    "abcdef123456",
				BuildTime: "2026-03-15T00:00:00Z",
				Channel:   "release",
				Dirty:     "clean",
			}
		},
		GitInspector: fakeGitInspector{status: GitStatus{Root: "C:/repo"}},
		TraderFactory: func(cfg *config.Config, liveTraders []config.TraderConfig) ([]liveStartValidator, error) {
			return []liveStartValidator{
				fakeLiveTrader{validation: trader.LiveStartValidation{
					TraderID:           "live_ibkr",
					TraderName:         "Live IBKR",
					Mode:               "live",
					Broker:             "ibkr",
					LiveTradingAllowed: false,
					ValidationMessage:  "startup readiness blocks live trading",
					Readiness: trader.ReadinessSummary{
						Status:         trader.ReadinessFail,
						Message:        "broker bootstrap failed",
						CheckedAt:      time.Unix(0, 0),
						TradingAllowed: false,
						Checks:         []trader.ReadinessCheck{{Name: "broker_bootstrap", Status: trader.ReadinessFail, Message: "broker bootstrap failed"}},
					},
					Promotion: trader.PromotionSummary{
						Status:             trader.PromotionFail,
						Message:            "promotion blocked",
						CheckedAt:          time.Unix(0, 0),
						Required:           true,
						LiveTradingAllowed: false,
						Checks:             []trader.PromotionCheck{{Name: "live_mode_acknowledged", Status: trader.PromotionFail, Message: "explicit approval missing"}},
					},
				}},
			}, nil
		},
	}

	configPath := writeTempConfig(t, `{
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

	summary := validator.ValidateLiveConfig(configPath)
	if summary.LiveReady {
		t.Fatalf("expected validation to fail")
	}
	if len(summary.TraderValidations) != 1 {
		t.Fatalf("expected one trader validation, got %d", len(summary.TraderValidations))
	}
}

func TestValidateLiveConfigPassesWhenChecksPass(t *testing.T) {
	validator := &Validator{
		Now: func() time.Time { return time.Unix(0, 0) },
		BuildInfo: func() buildinfo.Info {
			return buildinfo.Info{
				Version:   "v1.0.0",
				Commit:    "abcdef123456",
				BuildTime: "2026-03-15T00:00:00Z",
				Channel:   "release",
				Dirty:     "clean",
			}
		},
		GitInspector: fakeGitInspector{status: GitStatus{Root: "C:/repo"}},
		TraderFactory: func(cfg *config.Config, liveTraders []config.TraderConfig) ([]liveStartValidator, error) {
			return []liveStartValidator{
				fakeLiveTrader{validation: trader.LiveStartValidation{
					TraderID:           "live_ibkr",
					TraderName:         "Live IBKR",
					Mode:               "live",
					Broker:             "ibkr",
					LiveTradingAllowed: true,
					ValidationMessage:  "live deployment validation passed",
					Readiness: trader.ReadinessSummary{
						Status:         trader.ReadinessPass,
						Message:        "startup readiness passed",
						CheckedAt:      time.Unix(0, 0),
						TradingAllowed: true,
					},
					Promotion: trader.PromotionSummary{
						Status:             trader.PromotionPass,
						Message:            "live promotion checklist passed",
						CheckedAt:          time.Unix(0, 0),
						Required:           true,
						LiveTradingAllowed: true,
					},
				}},
			}, nil
		},
	}

	configPath := writeTempConfig(t, `{
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
				"live_promotion_approved": true,
				"risk_per_trade_pct": 0.01,
				"max_gross_exposure": 1.0,
				"max_position_pct": 0.20,
				"max_daily_loss_pct": 0.05,
				"max_concurrent_positions": 2,
				"max_net_exposure_pct": 0.50,
				"max_sector_exposure_pct": 0.30,
				"max_correlated_positions": 1,
				"ibkr_gateway_url": "https://127.0.0.1:5002/v1/api",
				"ibkr_account_id": "DU1234567",
				"deepseek_key": "test-key",
				"initial_balance": 100000,
				"scan_interval_minutes": 5
			}
		]
	}`)

	summary := validator.ValidateLiveConfig(configPath)
	if !summary.LiveReady {
		t.Fatalf("expected validation to pass: %+v", summary)
	}
}
