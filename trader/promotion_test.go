package trader

import (
	"encoding/json"
	"northstar/buildinfo"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunPromotionChecks_PaperModeIsNotApplicable(t *testing.T) {
	at := &AutoTrader{
		id:       "paper_trader",
		name:     "Paper Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "multi_factor",
			ScanInterval:   3 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
	}
	at.initializePromotionSummary()

	summary := at.runPromotionChecks()
	if summary.Status != PromotionNotApplicable {
		t.Fatalf("expected not_applicable for paper mode, got %s", summary.Status)
	}
	if !summary.LiveTradingAllowed {
		t.Fatalf("expected non-live promotion gate to bypass cleanly")
	}
}

func TestRunPromotionChecks_LiveBlockedWithoutApproval(t *testing.T) {
	withPromotionBuildIdentity(t, "v1.0.0", "abc123456789", "2026-03-15T18:00:00Z", "release", "clean", func() {
		withPromotionWorkspace(t, func() {
			at := newPromotionLiveTrader()
			at.setReadinessSummary(promotionPassReadiness())
			at.initializeBrokerRuntimeState()
			writePromotionPaperSessionReport(t, at.id, at.config.Broker, at.config.StrategyMode, time.Now())

			summary := at.runPromotionChecks()
			if summary.LiveTradingAllowed {
				t.Fatalf("expected live promotion to block without explicit approval")
			}
			check := findPromotionCheck(summary, "live_mode_acknowledged")
			if check == nil || check.Status != PromotionFail {
				t.Fatalf("expected live_mode_acknowledged failure, got %+v", check)
			}
		})
	})
}

func TestRunPromotionChecks_LiveBlockedWhenReadinessFailed(t *testing.T) {
	withPromotionBuildIdentity(t, "v1.0.0", "abc123456789", "2026-03-15T18:00:00Z", "release", "clean", func() {
		withPromotionWorkspace(t, func() {
			at := newPromotionLiveTrader()
			at.config.LivePromotionApproved = true
			at.setReadinessSummary(ReadinessSummary{
				Status:         ReadinessFail,
				Message:        "1 blocking readiness check(s) failed",
				CheckedAt:      time.Now(),
				TradingAllowed: false,
				FailCount:      1,
				Checks: []ReadinessCheck{
					readinessFail("broker_connectivity", "IBKR session/connectivity check failed"),
				},
			})
			at.initializeBrokerRuntimeState()
			writePromotionPaperSessionReport(t, at.id, at.config.Broker, at.config.StrategyMode, time.Now())

			summary := at.runPromotionChecks()
			if summary.LiveTradingAllowed {
				t.Fatalf("expected readiness failure to block live promotion")
			}
			check := findPromotionCheck(summary, "readiness_passed")
			if check == nil || check.Status != PromotionFail {
				t.Fatalf("expected readiness_passed failure, got %+v", check)
			}
		})
	})
}

func TestRunPromotionChecks_LiveBlockedWhenBrokerRuntimeDegraded(t *testing.T) {
	withPromotionBuildIdentity(t, "v1.0.0", "abc123456789", "2026-03-15T18:00:00Z", "release", "clean", func() {
		withPromotionWorkspace(t, func() {
			at := newPromotionLiveTrader()
			at.config.LivePromotionApproved = true
			at.setReadinessSummary(promotionPassReadiness())
			at.initializeBrokerRuntimeState()
			at.setBrokerRuntimeState(BrokerRuntimeDegraded, "gateway connection refused", errString("connection refused"), true, time.Now().Add(30*time.Second))
			writePromotionPaperSessionReport(t, at.id, at.config.Broker, at.config.StrategyMode, time.Now())

			summary := at.runPromotionChecks()
			if summary.LiveTradingAllowed {
				t.Fatalf("expected degraded broker runtime to block live promotion")
			}
			check := findPromotionCheck(summary, "broker_runtime_healthy")
			if check == nil || check.Status != PromotionFail {
				t.Fatalf("expected broker_runtime_healthy failure, got %+v", check)
			}
		})
	})
}

func TestRunPromotionChecks_LiveAllowedWhenChecklistPasses(t *testing.T) {
	withPromotionBuildIdentity(t, "v1.0.0", "abc123456789", "2026-03-15T18:00:00Z", "release", "clean", func() {
		withPromotionWorkspace(t, func() {
			at := newPromotionLiveTrader()
			at.config.LivePromotionApproved = true
			at.config.RequireBacktestSummary = true
			at.setReadinessSummary(promotionPassReadiness())
			at.initializeBrokerRuntimeState()
			writePromotionPaperSessionReport(t, at.id, at.config.Broker, at.config.StrategyMode, time.Now())
			writePromotionStudySummary(t, time.Now(), 4, 1, 1, 2)

			summary := at.runPromotionChecks()
			if !summary.LiveTradingAllowed {
				t.Fatalf("expected live promotion checklist to allow trading, got %+v", summary)
			}
			if summary.Status != PromotionPass {
				t.Fatalf("expected promotion pass, got %s", summary.Status)
			}
		})
	})
}

func TestRunPromotionChecks_MissingPaperEvidenceFailsLivePromotion(t *testing.T) {
	withPromotionBuildIdentity(t, "v1.0.0", "abc123456789", "2026-03-15T18:00:00Z", "release", "clean", func() {
		withPromotionWorkspace(t, func() {
			at := newPromotionLiveTrader()
			at.config.LivePromotionApproved = true
			at.setReadinessSummary(promotionPassReadiness())
			at.initializeBrokerRuntimeState()

			summary := at.runPromotionChecks()
			if summary.LiveTradingAllowed {
				t.Fatalf("expected missing paper evidence to block live promotion")
			}
			check := findPromotionCheck(summary, "paper_session_evidence_present")
			if check == nil || check.Status != PromotionFail {
				t.Fatalf("expected paper evidence failure, got %+v", check)
			}
		})
	})
}

func newPromotionLiveTrader() *AutoTrader {
	return &AutoTrader{
		id:       "ibkr_live_trader",
		name:     "IBKR Live Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:                          "ibkr_live_trader",
			Name:                        "IBKR Live Trader",
			Mode:                        "live",
			Broker:                      "ibkr",
			DataProvider:                "ibkr",
			StrategyMode:                "multi_factor",
			StrictLiveMode:              true,
			IBKRGatewayURL:              "https://127.0.0.1:5002/v1/api",
			IBKRAccountID:               "DU1234567",
			InstrumentType:              "equity",
			InitialBalance:              100000,
			ScanInterval:                3 * time.Minute,
			MinPaperSessionReports:      1,
			RequireBacktestSummary:      false,
			RequireReleaseBuildForLive:  true,
			PromotionMaxEvidenceAgeDays: 30,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-20 * time.Minute),
		lastResetTime:  time.Now().Add(-2 * time.Hour),
	}
}

func promotionPassReadiness() ReadinessSummary {
	return ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now(),
		TradingAllowed: true,
		PassCount:      6,
		Checks: []ReadinessCheck{
			readinessPass("config_sanity", "ok"),
		},
	}
}

func writePromotionPaperSessionReport(t *testing.T, traderID, brokerName, strategyMode string, generatedAt time.Time) {
	t.Helper()

	report := PaperSessionReport{
		ReportVersion:           sessionReportVersion,
		TraderID:                traderID,
		TraderName:              "Paper Evidence",
		Mode:                    "paper",
		Broker:                  brokerName,
		StrategyMode:            strategyMode,
		GeneratedAt:             generatedAt,
		SessionDate:             generatedAt.Format("2006-01-02"),
		SessionStart:            generatedAt.Add(-2 * time.Hour),
		SessionEnd:              generatedAt.Add(-time.Hour),
		DecisionCycles:          12,
		TradingAllowedAtStart:   true,
		SessionCompletionStatus: SessionCompletionCompleted,
	}
	path := sessionReportPath(traderID, report.SessionStart, report.SessionDate)
	if err := writePaperSessionReport(path, report); err != nil {
		t.Fatalf("write paper session report: %v", err)
	}
}

func writePromotionStudySummary(t *testing.T, generatedAt time.Time, completed, credible, provisional, insufficient int) {
	t.Helper()

	dir := filepath.Join("output", "ibkr_backtests", "run_test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir study summary dir: %v", err)
	}
	payload := map[string]interface{}{
		"generated_at":          generatedAt.Format(time.RFC3339),
		"completed_profiles":    completed,
		"credible_profiles":     credible,
		"provisional_profiles":  provisional,
		"insufficient_profiles": insufficient,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal study summary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "study_summary.json"), data, 0o644); err != nil {
		t.Fatalf("write study summary: %v", err)
	}
}

func withPromotionWorkspace(t *testing.T, fn func()) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	fn()
}

func withPromotionBuildIdentity(t *testing.T, version, commit, buildTime, channel, dirty string, fn func()) {
	t.Helper()

	prevVersion := buildinfo.Version
	prevCommit := buildinfo.Commit
	prevBuildTime := buildinfo.BuildTime
	prevChannel := buildinfo.Channel
	prevDirty := buildinfo.Dirty
	buildinfo.Version = version
	buildinfo.Commit = commit
	buildinfo.BuildTime = buildTime
	buildinfo.Channel = channel
	buildinfo.Dirty = dirty
	defer func() {
		buildinfo.Version = prevVersion
		buildinfo.Commit = prevCommit
		buildinfo.BuildTime = prevBuildTime
		buildinfo.Channel = prevChannel
		buildinfo.Dirty = prevDirty
	}()

	fn()
}

func findPromotionCheck(summary PromotionSummary, name string) *PromotionCheck {
	for i := range summary.Checks {
		if summary.Checks[i].Name == name {
			return &summary.Checks[i]
		}
	}
	return nil
}
