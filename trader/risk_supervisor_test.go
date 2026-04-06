package trader

import (
	"northstar/decision"
	"northstar/risk"
	"testing"
	"time"
)

func TestCurrentTradingGateDecision_ReduceOnlyBlocksEntriesButAllowsExits(t *testing.T) {
	at := &AutoTrader{
		id:       "risk_supervisor_test",
		name:     "Risk Supervisor Test",
		exchange: "alpaca",
		config: AutoTraderConfig{
			Mode:                           "paper",
			Broker:                         "sim",
			InstrumentType:                 "equity",
			MaxDrawdown:                    10.0,
			SupervisorReduceOnlyOnDrawdown: true,
		},
		isRunning:      true,
		peakEquitySeen: 100000,
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.setLatestAccountSummary(&AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         85000,
		AccountEquity:          85000,
		GrossMarketValue:       25000,
		PositionCount:          1,
	})

	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	if gate.Mode != risk.SupervisorModeReduceOnly {
		t.Fatalf("expected reduce_only mode, got %s", gate.Mode)
	}
	if !gate.TradingAllowed {
		t.Fatalf("expected trading gate to allow exits in reduce_only mode")
	}
	if gate.EntriesAllowed {
		t.Fatalf("expected entries to be blocked")
	}
	if !gate.ExitsAllowed {
		t.Fatalf("expected exits to remain allowed")
	}

	entryErr := at.ensureDecisionAllowedByGate(&decision.Decision{Symbol: "AAPL", Action: "open_long"}, gate)
	if entryErr == nil {
		t.Fatalf("expected entry decision to be blocked in reduce_only mode")
	}
	exitErr := at.ensureDecisionAllowedByGate(&decision.Decision{Symbol: "AAPL", Action: "close_long"}, gate)
	if exitErr != nil {
		t.Fatalf("expected exit decision to remain allowed, got %v", exitErr)
	}
}

func TestGetOperatorStatus_IncludesRiskSupervisorSummary(t *testing.T) {
	at := &AutoTrader{
		id:       "risk_supervisor_status",
		name:     "Risk Supervisor Status",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:                             "risk_supervisor_status",
			Name:                           "Risk Supervisor Status",
			Mode:                           "paper",
			Broker:                         "sim",
			InstrumentType:                 "equity",
			MaxDrawdown:                    10.0,
			SupervisorReduceOnlyOnDrawdown: true,
			ScanInterval:                   5 * time.Minute,
			InitialBalance:                 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-15 * time.Minute),
		peakEquitySeen: 100000,
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.setLatestAccountSummary(&AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         85000,
		AccountEquity:          85000,
		GrossMarketValue:       25000,
		PositionCount:          1,
	})

	status := at.GetOperatorStatus()
	if status.RiskSupervisor.Mode != risk.SupervisorModeReduceOnly {
		t.Fatalf("expected reduce_only supervisor mode, got %s", status.RiskSupervisor.Mode)
	}
	if status.EntriesAllowed {
		t.Fatalf("expected top-level entries_allowed to be false")
	}
	if !status.ExitsAllowed {
		t.Fatalf("expected top-level exits_allowed to remain true")
	}
	if status.TradingGate.Mode != risk.SupervisorModeReduceOnly {
		t.Fatalf("expected trading gate mode reduce_only, got %s", status.TradingGate.Mode)
	}
}

func TestCurrentTradingGateDecision_ReadinessFailureOutranksRestartRecovery(t *testing.T) {
	at := &AutoTrader{
		id:       "risk_supervisor_readiness_priority",
		name:     "Risk Supervisor Readiness Priority",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "risk_supervisor_readiness_priority",
			Name:           "Risk Supervisor Readiness Priority",
			Mode:           "paper",
			Broker:         "ibkr",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
	}
	maintenance := "IBKR nightly reset window is active; broker_bootstrap account-state endpoints are temporarily unavailable and trading will remain paused until broker truth returns"
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessFail,
		Message:        "1 blocking readiness check(s) failed",
		CheckedAt:      time.Now(),
		TradingAllowed: false,
		Checks: []ReadinessCheck{
			readinessFail("broker_bootstrap", maintenance),
			readinessWarn("restart_recovery", "durable runtime state restored; broker reconciliation must confirm orders and positions before trading resumes"),
		},
	})
	at.restartRecoveryState = restartRecoverySummary{
		Available:             true,
		Restored:              true,
		PendingReconciliation: true,
		TradingBlocked:        true,
		Message:               "durable runtime state restored; broker reconciliation must confirm orders and positions before trading resumes",
	}

	gate := at.currentTradingGateDecision(false, nil)
	if gate.TradingAllowed {
		t.Fatalf("expected readiness failure to block trading")
	}
	if gate.BlockReason != maintenance {
		t.Fatalf("expected readiness block reason to win, got %q", gate.BlockReason)
	}
	if len(gate.BlockingReasons) == 0 || gate.BlockingReasons[0] != maintenance {
		t.Fatalf("expected readiness blocking reasons to lead, got %+v", gate.BlockingReasons)
	}
}
