package trader

import (
	"northstar/alerts"
	dataquality "northstar/data"
	"northstar/execution"
	"northstar/incidents"
	"northstar/orders"
	"northstar/risk"
	"northstar/startup"
	"strings"
	"testing"
	"time"
)

func TestGetOperatorStatus_ReadinessFailureBlocksTradingClearly(t *testing.T) {
	at := &AutoTrader{
		id:       "blocked_trader",
		name:     "Blocked Trader",
		aiModel:  "deepseek",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:             "blocked_trader",
			Name:           "Blocked Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "ai_only",
			ScanInterval:   3 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-15 * time.Minute),
		lastResetTime:  time.Now().Add(-time.Hour),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessFail,
		Message:        "1 blocking readiness check(s) failed",
		CheckedAt:      time.Now(),
		TradingAllowed: false,
		PassCount:      2,
		FailCount:      1,
		Checks: []ReadinessCheck{
			readinessFail("ai_readiness", "DeepSeek strategy mode requires deepseek_key or NORTHSTAR_DEEPSEEK_API_KEY"),
		},
	})
	at.initializeBrokerRuntimeState()

	status := at.GetOperatorStatus()
	if status.TradingAllowed {
		t.Fatalf("expected trading to be blocked")
	}
	if status.DecisionArchitecture != "equity_generator_plus_canonical_pipeline" {
		t.Fatalf("expected equity trader to report canonical pipeline architecture, got %q", status.DecisionArchitecture)
	}
	if status.TradingBlockReason != "startup readiness failed" {
		t.Fatalf("expected readiness block reason, got %q", status.TradingBlockReason)
	}
	if status.OperatorMessage != "startup readiness failed" {
		t.Fatalf("unexpected operator message: %q", status.OperatorMessage)
	}
	if status.Readiness.Status != ReadinessFail {
		t.Fatalf("expected nested readiness fail, got %s", status.Readiness.Status)
	}
	if len(status.BlockingReasons) == 0 || !strings.Contains(status.BlockingReasons[0], "startup readiness failed") {
		t.Fatalf("expected detailed blocking reasons, got %+v", status.BlockingReasons)
	}
}

func TestGetOperatorStatus_BrokerDegradedAppearsDistinctFromLiveness(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:       "ibkr_trader",
		name:     "IBKR Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "ibkr_trader",
			Name:           "IBKR Trader",
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      now.Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now.Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	}, []map[string]interface{}{})
	at.setBrokerRuntimeState(BrokerRuntimeDegraded, "gateway connection refused", errString("dial tcp 127.0.0.1:5002: connectex: connection refused"), true, now.Add(30*time.Second))

	status := at.GetOperatorStatus()
	if status.TradingAllowed {
		t.Fatalf("expected degraded broker runtime to block trading")
	}
	if status.DecisionArchitecture != "equity_generator_plus_canonical_pipeline" {
		t.Fatalf("expected equity trader to report canonical pipeline architecture, got %q", status.DecisionArchitecture)
	}
	if status.TradingBlockReason != "broker runtime degraded" {
		t.Fatalf("expected broker degraded block reason, got %q", status.TradingBlockReason)
	}
	if status.BrokerRuntime.State != BrokerRuntimeDegraded {
		t.Fatalf("expected nested broker runtime degraded, got %s", status.BrokerRuntime.State)
	}
	if status.BrokerRuntime.Managed != true {
		t.Fatalf("expected broker runtime to be marked managed")
	}
	if status.Readiness.Status != ReadinessPass {
		t.Fatalf("expected readiness to remain pass while broker runtime is degraded, got %s", status.Readiness.Status)
	}
}

func TestGetOperatorStatus_LivePromotionFailureAppearsInTradingGate(t *testing.T) {
	now := time.Now()
	t.Setenv(startup.EnvActiveConfigFile, `C:\repo\config_ibkr_live.json`)
	t.Setenv(startup.EnvLiveValidationPassed, "true")
	t.Setenv(startup.EnvLiveValidationConfig, `C:\repo\config_ibkr_live.json`)
	t.Setenv(startup.EnvLiveValidationCheckedAt, now.UTC().Format(time.RFC3339Nano))
	t.Setenv(startup.EnvLiveValidationSource, "run_ibkr_live.cmd")

	at := &AutoTrader{
		id:       "live_trader",
		name:     "Live Trader",
		aiModel:  "deepseek",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "live_trader",
			Name:           "Live Trader",
			Mode:           "live",
			Broker:         "ibkr",
			StrategyMode:   "multi_factor",
			StrictLiveMode: true,
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      now.Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now.Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	}, []map[string]interface{}{})
	at.setPromotionSummary(PromotionSummary{
		Status:             PromotionFail,
		Message:            "1 promotion check(s) failed",
		CheckedAt:          time.Now(),
		Required:           true,
		LiveTradingAllowed: false,
		FailCount:          1,
		Checks: []PromotionCheck{
			promotionFail("live_mode_acknowledged", "live_promotion_approved is false"),
		},
	})

	status := at.GetOperatorStatus()
	if status.TradingAllowed {
		t.Fatalf("expected promotion failure to block live trading")
	}
	if status.TradingBlockReason != "live promotion checklist failed" {
		t.Fatalf("expected promotion block reason, got %q", status.TradingBlockReason)
	}
	if status.Promotion.Status != PromotionFail {
		t.Fatalf("expected nested promotion fail, got %s", status.Promotion.Status)
	}
	if !status.Promotion.Required {
		t.Fatalf("expected promotion to be required for live mode")
	}
	if !status.DeploymentValidation.Required {
		t.Fatalf("expected deployment validation to be required for live mode")
	}
	if !status.DeploymentValidation.Passed {
		t.Fatalf("expected deployment validation to be reported as passed")
	}
}

func TestGetOperatorStatus_IncludesOrderReconciliationSummary(t *testing.T) {
	reporter := &operatorStatusOrderReporter{
		summary: orders.Summary{
			LastRunAt:               time.Now().Add(-time.Minute),
			LastSuccessAt:           time.Now().Add(-time.Minute),
			TotalRuns:               5,
			TotalMismatches:         2,
			TotalRepairs:            2,
			UnknownBrokerOrders:     1,
			LocalMissingAtBroker:    1,
			FillMismatches:          0,
			TotalInferredOutcomes:   1,
			TotalUnresolvedOutcomes: 0,
			TrackedOrders:           4,
			ActiveLocalOrders:       1,
			BrokerOpenOrders:        1,
			CurrentPendingOrders:    1,
			CurrentConfirmedOrders:  2,
			CurrentInferredOrders:   1,
			CurrentUnresolvedOrders: 0,
			ConfidenceDegraded:      true,
			LastSummary:             "order reconciliation handled 2 mismatch(es): local_missing=1 unknown_broker=1 fill_mismatches=0 inferred=1 unresolved=0",
			LastIssues: []orders.Issue{
				{
					LocalID:     "paper-local-1",
					Message:     "entry order inferred from broker position evidence",
					Authority:   orders.TruthAuthorityReconciliationInferred,
					Confidence:  orders.TruthConfidenceMedium,
					NeedsReview: true,
					Repaired:    true,
				},
			},
		},
	}

	at := &AutoTrader{
		id:       "paper_trader",
		name:     "Paper Trader",
		aiModel:  "deepseek",
		exchange: "alpaca",
		trader:   reporter,
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "multi_factor",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()

	status := at.GetOperatorStatus()
	if !status.OrderReconciliation.Available {
		t.Fatalf("expected order reconciliation summary to be available")
	}
	if status.OrderReconciliation.TotalMismatches != 2 {
		t.Fatalf("expected reconciliation mismatches, got %d", status.OrderReconciliation.TotalMismatches)
	}
	if status.OrderReconciliation.TotalInferredOutcomes != 1 || !status.OrderReconciliation.ConfidenceDegraded {
		t.Fatalf("expected inferred/degraded reconciliation summary, got %+v", status.OrderReconciliation)
	}
	if status.OrderReconciliation.PrimaryAuthority != string(orders.TruthAuthorityReconciliationInferred) || status.OrderReconciliation.PrimaryIssueLocalID != "paper-local-1" {
		t.Fatalf("expected primary inferred issue in operator reconciliation summary, got %+v", status.OrderReconciliation)
	}
	if status.OrderReconciliationSummary == "" {
		t.Fatalf("expected compatibility order reconciliation summary")
	}
}

func TestGetOperatorStatus_IncludesPortfolioRiskSummary(t *testing.T) {
	at := &AutoTrader{
		id:       "paper_trader",
		name:     "Paper Trader",
		aiModel:  "deepseek",
		exchange: "alpaca",
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "multi_factor",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.portfolioRiskState = &portfolioRiskState{
		EvaluatedAt: time.Now().Add(-30 * time.Second),
		Outcome:     risk.OutcomeReduceSize,
		Summary:     "risk reduce_size: reduced order to remaining technology sector budget",
		Metrics: risk.PortfolioMetrics{
			CurrentGrossExposurePct:         0.40,
			CurrentNetExposurePct:           0.35,
			LargestSector:                   "technology",
			LargestSectorExposurePct:        0.32,
			CorrelatedPositionCount:         1,
			MaxObservedCorrelation:          0.88,
			CurrentDrawdownPct:              0.04,
			SectorExposurePct:               map[string]float64{"technology": 0.32},
			CorrelatedSymbols:               []string{"MSFT"},
			OrderSector:                     "technology",
			OrderSectorKnown:                true,
			ProjectedOrderSectorExposurePct: 0.35,
		},
	}

	status := at.GetOperatorStatus()
	if !status.PortfolioRisk.Available {
		t.Fatalf("expected portfolio risk summary to be available")
	}
	if status.PortfolioRisk.Outcome != risk.OutcomeReduceSize {
		t.Fatalf("expected nested portfolio risk outcome, got %s", status.PortfolioRisk.Outcome)
	}
	if status.PortfolioLargestSector != "technology" {
		t.Fatalf("expected compatibility largest sector, got %q", status.PortfolioLargestSector)
	}
	if status.PortfolioCorrelatedPositions != 1 {
		t.Fatalf("expected compatibility correlated positions, got %d", status.PortfolioCorrelatedPositions)
	}
}

func TestGetOperatorStatus_IncludesExecutionSummary(t *testing.T) {
	manager := execution.NewManager(execution.Config{})
	broker := &executionStatusBroker{
		order: map[string]interface{}{
			"status":       "FILLED",
			"orderId":      int64(42),
			"filled_qty":   5.0,
			"price":        101.25,
			"localOrderId": "local-42",
		},
	}
	manager.Execute(execution.Intent{
		TraderID:          "paper_trader",
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          5,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}, execution.Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}, broker)

	at := &AutoTrader{
		id:               "paper_trader",
		name:             "Paper Trader",
		aiModel:          "deepseek",
		exchange:         "alpaca",
		executionManager: manager,
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "multi_factor",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()

	status := at.GetOperatorStatus()
	if !status.Execution.Available {
		t.Fatalf("expected execution summary to be available")
	}
	if status.Execution.FilledCount != 1 {
		t.Fatalf("expected filled execution count 1, got %d", status.Execution.FilledCount)
	}
	if status.Execution.LastExecutionSymbol != "AAPL" {
		t.Fatalf("expected last execution symbol AAPL, got %q", status.Execution.LastExecutionSymbol)
	}
	if status.ExecutionLastStatus != string(execution.StatusFilled) {
		t.Fatalf("expected compatibility execution last status filled, got %q", status.ExecutionLastStatus)
	}
}

type executionStatusBroker struct {
	order map[string]interface{}
	err   error
}

func (b *executionStatusBroker) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return cloneStatusOrder(b.order), b.err
}

func (b *executionStatusBroker) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return cloneStatusOrder(b.order), b.err
}

func (b *executionStatusBroker) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return cloneStatusOrder(b.order), b.err
}

func (b *executionStatusBroker) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return cloneStatusOrder(b.order), b.err
}

func cloneStatusOrder(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestGetOperatorStatus_PositionReconciliationBlocksTrading(t *testing.T) {
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
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	}, []map[string]interface{}{})
	at.positionReconSummary = positionReconciliationSummary{
		Available:            true,
		Status:               PositionReconciliationBlocked,
		Summary:              "position reconciliation found 1 mismatch(es): local_missing=0 broker_missing=1 size=0 price=0",
		TradingAllowed:       false,
		TotalRuns:            2,
		TotalIncidents:       1,
		TotalMismatches:      1,
		BrokerMissingLocally: 1,
	}

	status := at.GetOperatorStatus()
	if status.TradingAllowed {
		t.Fatalf("expected position reconciliation to block trading")
	}
	if status.TradingBlockReason != "position reconciliation blocked" {
		t.Fatalf("expected position reconciliation block reason, got %q", status.TradingBlockReason)
	}
	if !status.PositionReconciliation.Available {
		t.Fatalf("expected nested position reconciliation summary")
	}
	if status.PositionReconciliation.Status != PositionReconciliationBlocked {
		t.Fatalf("expected blocked position reconciliation status, got %s", status.PositionReconciliation.Status)
	}
	if status.PositionReconciliationIncidents != 1 {
		t.Fatalf("expected compatibility incident count 1, got %d", status.PositionReconciliationIncidents)
	}
}

func TestGetOperatorStatus_IncludesDataQualitySummary(t *testing.T) {
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
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	now := time.Now().UTC()
	at.dataQualityState.LastCheckedAt = now
	at.dataQualityState.TotalChecks = 3
	at.dataQualityState.TotalFailures = 1
	at.dataQualityState.BlockedSymbols["AAPL"] = dataQualityBlockedSymbol{
		Symbol:        "AAPL",
		Interval:      "3m",
		Summary:       "data quality blocked AAPL 3m: latest 3m bar for AAPL has zero volume",
		LastCheckedAt: now,
		LastFailedAt:  now,
		FailureCount:  1,
		IssueTypes:    []dataquality.IssueType{dataquality.IssueZeroVolume},
	}

	status := at.GetOperatorStatus()
	if !status.DataQuality.Available {
		t.Fatalf("expected data quality summary to be available")
	}
	if status.DataQuality.BlockedSymbolsCount != 1 {
		t.Fatalf("expected nested blocked symbol count 1, got %d", status.DataQuality.BlockedSymbolsCount)
	}
	if status.DataQualityBlockedSymbols != 1 {
		t.Fatalf("expected compatibility blocked symbol count 1, got %d", status.DataQualityBlockedSymbols)
	}
	if len(status.DataQuality.BlockedSymbols) != 1 || status.DataQuality.BlockedSymbols[0].Symbol != "AAPL" {
		t.Fatalf("expected blocked AAPL symbol in data quality summary, got %+v", status.DataQuality.BlockedSymbols)
	}
}

func TestGetOperatorStatus_IncludesRecentAlerts(t *testing.T) {
	manager := alerts.NewManager()
	manager.Emit(alerts.Alert{
		Category:   alerts.CategoryCritical,
		Event:      "broker_disconnect",
		TraderID:   "paper_trader",
		TraderName: "Paper Trader",
		Message:    "broker disconnect detected during orders: connection refused",
	})

	at := &AutoTrader{
		id:           "paper_trader",
		name:         "Paper Trader",
		aiModel:      "deepseek",
		exchange:     "alpaca",
		alertManager: manager,
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "multi_factor",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()

	status := at.GetOperatorStatus()
	if !status.Alerts.Available {
		t.Fatalf("expected alerts summary to be available")
	}
	if status.AlertCount != 1 {
		t.Fatalf("expected alert count 1, got %d", status.AlertCount)
	}
	if len(status.RecentAlerts) != 1 {
		t.Fatalf("expected one recent alert, got %d", len(status.RecentAlerts))
	}
	if status.RecentAlerts[0].Event != "broker_disconnect" {
		t.Fatalf("unexpected recent alert event %q", status.RecentAlerts[0].Event)
	}
}

func TestGetOperatorStatus_IncludesIncidentSummary(t *testing.T) {
	at := &AutoTrader{
		id:              "paper_trader",
		name:            "Paper Trader",
		aiModel:         "deepseek",
		exchange:        "ibkr",
		incidentManager: incidents.NewManager("paper_trader"),
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "multi_factor",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-25 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-10 * time.Minute),
		TradingAllowed: true,
		PassCount:      6,
	})
	at.initializeBrokerRuntimeState()
	at.observeIncident(incidents.Signal{
		IncidentType:  incidents.TypeKillSwitchActivated,
		Severity:      incidents.SeverityCritical,
		Source:        "kill_switch",
		Summary:       "emergency kill switch activated via file",
		CurrentStatus: "kill switch active",
		OccurredAt:    time.Now(),
	})

	status := at.GetOperatorStatus()
	if !status.Incidents.Available {
		t.Fatalf("expected incidents summary to be available")
	}
	if status.Incidents.OpenCount != 1 {
		t.Fatalf("expected one open incident, got %d", status.Incidents.OpenCount)
	}
	if status.LatestIncidentSummary == "" {
		t.Fatalf("expected top-level latest incident summary")
	}
	if status.LatestIncidentSeverity != string(incidents.SeverityCritical) {
		t.Fatalf("expected critical latest incident severity, got %q", status.LatestIncidentSeverity)
	}
	if len(status.Incidents.OpenIncidents) != 1 {
		t.Fatalf("expected one open incident entry, got %d", len(status.Incidents.OpenIncidents))
	}
	if status.Incidents.LatestIncidentRunbookHint == "" {
		t.Fatalf("expected runbook hint for known incident type")
	}
}

func TestGetOperatorStatus_MarketClosedAppearsAsExpectedNonTradable(t *testing.T) {
	at := &AutoTrader{
		id:       "shadow_trader",
		name:     "Shadow Trader",
		aiModel:  "custom",
		exchange: "ibkr",
		config: AutoTraderConfig{
			ID:             "shadow_trader",
			Name:           "Shadow Trader",
			Mode:           "shadow",
			Broker:         "sim",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
		},
		isRunning: true,
		startTime: time.Now().Add(-10 * time.Minute),
	}
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now().Add(-time.Minute),
		TradingAllowed: true,
		PassCount:      4,
	})
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	at.recordPaperSessionBlockedCycle("market is closed for equity session")

	status := at.GetOperatorStatus()
	if status.RuntimeCondition.State != RuntimeConditionMarketClosed {
		t.Fatalf("expected market_closed runtime condition, got %+v", status.RuntimeCondition)
	}
	if status.RuntimeCondition.Severity != incidents.SeverityInfo {
		t.Fatalf("expected info severity, got %+v", status.RuntimeCondition)
	}
	if status.CycleTradable {
		t.Fatalf("expected cycle to be non-tradable while market is closed")
	}
	if !status.ExpectedNonTradable {
		t.Fatalf("expected market-closed condition to be marked expected")
	}
	if !strings.Contains(strings.ToLower(status.OperatorMessage), "market") {
		t.Fatalf("expected operator message to mention market closure, got %q", status.OperatorMessage)
	}
}

func TestGetOperatorStatus_InferredExecutionConfidenceShowsAwaitingReconciliation(t *testing.T) {
	now := time.Now().UTC()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:             now,
			LastSuccessAt:         now,
			CurrentInferredOrders: 1,
			ConfidenceDegraded:    true,
			LastSummary:           "order reconciliation inferred 1 execution outcome from position evidence",
			LastIssues: []orders.Issue{{
				LocalID:     "local-inferred-1",
				Message:     "entry order inferred from broker position evidence",
				Authority:   orders.TruthAuthorityReconciliationInferred,
				Confidence:  orders.TruthConfidenceHigh,
				NeedsReview: true,
				Repaired:    true,
			}},
		},
	}
	at := &AutoTrader{
		id:       "broker_truth_status",
		name:     "Broker Truth Status",
		aiModel:  "deepseek",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			ID:             "broker_truth_status",
			Name:           "Broker Truth Status",
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      now.Add(-10 * time.Minute),
	}
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      now,
		TradingAllowed: true,
		PassCount:      4,
	})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
	}, []map[string]interface{}{})

	status := at.GetOperatorStatus()
	if status.RuntimeCondition.State != RuntimeConditionAwaitingReconciliation {
		t.Fatalf("expected awaiting_reconciliation runtime state, got %+v", status.RuntimeCondition)
	}
	if status.RuntimeCondition.Severity != incidents.SeverityWarning {
		t.Fatalf("expected warning severity for inferred execution restriction, got %+v", status.RuntimeCondition)
	}
	if status.CycleTradable {
		t.Fatalf("expected cycle to be non-tradable for new entries while awaiting reconciliation")
	}
	if !status.AwaitingReconciliation {
		t.Fatalf("expected awaiting reconciliation compatibility field to be true")
	}
}

type operatorStatusOrderReporter struct {
	summary orders.Summary
}

func (o *operatorStatusOrderReporter) GetBalance() (map[string]interface{}, error) { return nil, nil }
func (o *operatorStatusOrderReporter) GetPositions() ([]map[string]interface{}, error) {
	return nil, nil
}
func (o *operatorStatusOrderReporter) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (o *operatorStatusOrderReporter) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (o *operatorStatusOrderReporter) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (o *operatorStatusOrderReporter) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (o *operatorStatusOrderReporter) SetLeverage(symbol string, leverage int) error { return nil }
func (o *operatorStatusOrderReporter) GetMarketPrice(symbol string) (float64, error) { return 0, nil }
func (o *operatorStatusOrderReporter) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (o *operatorStatusOrderReporter) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (o *operatorStatusOrderReporter) CancelAllOrders(symbol string) error { return nil }
func (o *operatorStatusOrderReporter) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}
func (o *operatorStatusOrderReporter) GetOrderReconciliationSummary() orders.Summary {
	return o.summary
}

type errString string

func (e errString) Error() string {
	return string(e)
}
