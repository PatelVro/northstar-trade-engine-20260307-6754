package trader

import (
	"northstar/orders"
	"strings"
	"testing"
	"time"
)

type brokerTruthTestTrader struct {
	orderSummary orders.Summary
}

func freshBrokerPositionViews(symbols ...string) []map[string]interface{} {
	views := make([]map[string]interface{}, 0, len(symbols))
	for _, symbol := range symbols {
		views = append(views, map[string]interface{}{
			"symbol":             symbol,
			"side":               "long",
			"entryPrice":         100.0,
			"markPrice":          100.0,
			"positionAmt":        1.0,
			"unRealizedProfit":   0.0,
			"leverage":           1.0,
			"liquidationPrice":   0.0,
			"unrealized_pnl_pct": 0.0,
			"margin_used":        100.0,
		})
	}
	return views
}

func (t *brokerTruthTestTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (t *brokerTruthTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

func (t *brokerTruthTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *brokerTruthTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *brokerTruthTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *brokerTruthTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *brokerTruthTestTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

func (t *brokerTruthTestTrader) GetMarketPrice(symbol string) (float64, error) {
	return 0, nil
}

func (t *brokerTruthTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}

func (t *brokerTruthTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}

func (t *brokerTruthTestTrader) CancelAllOrders(symbol string) error {
	return nil
}

func (t *brokerTruthTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return "", nil
}

func (t *brokerTruthTestTrader) GetOrderReconciliationSummary() orders.Summary {
	return t.orderSummary
}

func freshPositionReconSummary(now time.Time) positionReconciliationSummary {
	return positionReconciliationSummary{
		Available:        true,
		Status:           PositionReconciliationHealthy,
		Summary:          "broker positions synchronized",
		TradingAllowed:   true,
		LastRunAt:        now,
		LastSuccessAt:    now,
		LastReconciledAt: now,
	}
}

func TestBrokerTruthGateBlocksPaperIBKRWithoutFreshAccountSnapshot(t *testing.T) {
	now := time.Now()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:     now,
			LastSuccessAt: now,
			LastSummary:   "order reconciliation clean",
		},
	}

	at := &AutoTrader{
		id:       "broker_truth_missing_account",
		name:     "Broker Truth Missing Account",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)

	gate := at.currentTradingGateDecision(false, nil)
	if gate.TradingAllowed {
		t.Fatalf("expected broker-truth gate to block trading, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "account snapshot") {
		t.Fatalf("expected account snapshot block reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthGateAllowsPaperIBKRWithFreshTruth(t *testing.T) {
	now := time.Now()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:              now,
			LastSuccessAt:          now,
			LastSummary:            "order reconciliation clean",
			CurrentConfirmedOrders: 1,
		},
	}

	at := &AutoTrader{
		id:       "broker_truth_ready",
		name:     "Broker Truth Ready",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          0,
	}, []map[string]interface{}{})

	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	if !gate.TradingAllowed || !gate.EntriesAllowed || !gate.ExitsAllowed {
		t.Fatalf("expected fresh broker truth to allow trading, got %+v", gate)
	}
}

func TestBrokerTruthGateBlocksPaperIBKRWhenOrderTruthIsUnresolved(t *testing.T) {
	now := time.Now()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:               now,
			LastSuccessAt:           now,
			LastSummary:             "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=0 unresolved=1",
			CurrentUnresolvedOrders: 1,
			ConfidenceDegraded:      true,
		},
	}

	at := &AutoTrader{
		id:       "broker_truth_unresolved",
		name:     "Broker Truth Unresolved",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          0,
	}, []map[string]interface{}{})

	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	if gate.TradingAllowed {
		t.Fatalf("expected unresolved order truth to block trading, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "unresolved") {
		t.Fatalf("expected unresolved broker-truth block reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthSummaryMarksInferredOrderTruthAsDegraded(t *testing.T) {
	now := time.Now()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:              now,
			LastSuccessAt:          now,
			LastSummary:            "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0",
			CurrentInferredOrders:  1,
			ConfidenceDegraded:     true,
			CurrentConfirmedOrders: 1,
			LastIssues: []orders.Issue{
				{
					LocalID:     "local-inferred",
					Message:     "entry order inferred from broker position evidence",
					Authority:   orders.TruthAuthorityReconciliationInferred,
					Confidence:  orders.TruthConfidenceHigh,
					NeedsReview: true,
					Repaired:    true,
				},
			},
		},
	}

	at := &AutoTrader{
		id:       "broker_truth_inferred",
		name:     "Broker Truth Inferred",
		exchange: "ibkr",
		trader:   trader,
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          1,
	}, freshBrokerPositionViews("AAPL"))

	summary := at.currentBrokerTruthSummary()
	if summary.TradingBlocked {
		t.Fatalf("expected inferred order truth not to hard-block trading, got %+v", summary)
	}
	if !summary.ConfidenceDegraded || summary.InferredOrderCount != 1 {
		t.Fatalf("expected degraded inferred broker truth summary, got %+v", summary)
	}
	if !summary.EntriesRestricted {
		t.Fatalf("expected inferred broker truth to restrict entries, got %+v", summary)
	}
	if summary.PreflightReady {
		t.Fatalf("expected inferred broker truth preflight to remain not-ready for normal entries, got %+v", summary)
	}
	if summary.PrimaryAuthority != orders.TruthAuthorityReconciliationInferred || summary.PrimaryConfidence != orders.TruthConfidenceHigh {
		t.Fatalf("expected inferred primary issue metadata, got %+v", summary)
	}
	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	if !gate.TradingAllowed || gate.EntriesAllowed || !gate.ExitsAllowed || !gate.ReduceOnly {
		t.Fatalf("expected inferred broker truth to put trading into reduce_only, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "reconciliation-inferred") {
		t.Fatalf("expected inferred broker truth reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthGateBlocksShadowWhenFeedDelayed(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:       "shadow_feed_delayed",
		name:     "Shadow Feed Delayed",
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:           "shadow",
			Broker:         "sim",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.updateMarketDataFeedStatus(true, "IBKR market data delayed/unusable for runtime probes", []string{"AAPL", "MSFT"})

	gate := at.currentTradingGateDecision(false, nil)
	if gate.TradingAllowed {
		t.Fatalf("expected delayed shadow feed to block trading, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "market data delayed") && !strings.Contains(strings.ToLower(gate.BlockReason), "market data") {
		t.Fatalf("expected market-data block reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthGateBlocksShadowWhenMarketDataHasNotBeenPreflighted(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:        "shadow_feed_unchecked",
		name:      "Shadow Feed Unchecked",
		exchange:  "ibkr",
		config: AutoTraderConfig{
			Mode:           "shadow",
			Broker:         "sim",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})

	gate := at.currentTradingGateDecision(false, nil)
	if gate.TradingAllowed {
		t.Fatalf("expected unchecked market-data truth to block shadow trading, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "preflighted") {
		t.Fatalf("expected unchecked market-data preflight reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthGateBlocksPaperIBKRWithoutOrderReconciliationSummary(t *testing.T) {
	now := time.Now()
	at := &AutoTrader{
		id:       "broker_truth_missing_orders",
		name:     "Broker Truth Missing Orders",
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "ibkr",
			StrategyMode:   "momentum_only",
			ScanInterval:   5 * time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          0,
	}, []map[string]interface{}{})

	gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
	if gate.TradingAllowed {
		t.Fatalf("expected missing order reconciliation summary to block trading, got %+v", gate)
	}
	if !strings.Contains(strings.ToLower(gate.BlockReason), "reconciliation summary is unavailable") {
		t.Fatalf("expected missing order reconciliation reason, got %q", gate.BlockReason)
	}
}

func TestBrokerTruthSummaryMarksStaleMarketDataPreflightAsBlocked(t *testing.T) {
	now := time.Now().UTC()
	at := &AutoTrader{
		id:       "shadow_feed_stale",
		name:     "Shadow Feed Stale",
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:           "shadow",
			Broker:         "sim",
			DataProvider:   "ibkr",
			InstrumentType: "equity",
			StrategyMode:   "momentum_only",
			ScanInterval:   time.Minute,
			InitialBalance: 100000,
		},
		initialBalance: 100000,
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.initializeDataQualityState()
	at.updateMarketDataFeedStatus(false, "", []string{"AAPL", "MSFT"})
	at.dataQualityMu.Lock()
	at.dataQualityState.FeedStatus.LastCheckedAt = now.Add(-6 * time.Minute)
	at.dataQualityMu.Unlock()

	summary := at.currentBrokerTruthSummary()
	if !summary.TradingBlocked || summary.MarketDataFresh {
		t.Fatalf("expected stale market-data preflight to block trading, got %+v", summary)
	}
	if !strings.Contains(strings.ToLower(summary.Message), "stale") {
		t.Fatalf("expected stale market-data message, got %q", summary.Message)
	}
}

func TestGetOperatorStatus_IncludesBrokerTruthSummary(t *testing.T) {
	now := time.Now()
	trader := &brokerTruthTestTrader{
		orderSummary: orders.Summary{
			LastRunAt:             now,
			LastSuccessAt:         now,
			LastSummary:           "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0",
			CurrentInferredOrders: 1,
			ConfidenceDegraded:    true,
			LastIssues: []orders.Issue{
				{
					LocalID:     "status-local-order",
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
		id:       "broker_truth_status",
		name:     "Broker Truth Status",
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
		startTime:      now.Add(-10 * time.Minute),
	}
	at.isRunning.Store(true)
	at.initializeBrokerRuntimeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: now})
	at.positionReconSummary = freshPositionReconSummary(now)
	at.setRuntimeAccountSnapshot(AccountSummary{
		AccountingVersion:      accountingVersion,
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		PositionCount:          1,
	}, freshBrokerPositionViews("AAPL"))

	status := at.GetOperatorStatus()
	if !status.BrokerTruth.Available || !status.BrokerTruth.Required {
		t.Fatalf("expected broker truth summary to be available and required, got %+v", status.BrokerTruth)
	}
	if status.BrokerTruth.TradingBlocked {
		t.Fatalf("expected inferred broker truth to restrict but not block, got %+v", status.BrokerTruth)
	}
	if !status.BrokerTruth.EntriesRestricted || status.BrokerTruth.PrimaryAuthority != string(orders.TruthAuthorityReconciliationInferred) {
		t.Fatalf("expected operator status to expose inferred broker truth restriction, got %+v", status.BrokerTruth)
	}
	if status.BrokerTruth.PreflightReady || !status.BrokerTruth.AccountFresh || !status.BrokerTruth.OrdersFresh || !status.BrokerTruth.PositionsFresh {
		t.Fatalf("expected operator status to expose component freshness and non-ready preflight for inferred truth, got %+v", status.BrokerTruth)
	}
	if status.BrokerTruth.PrimaryIssueLocalID != "status-local-order" || !status.BrokerTruth.PrimaryNeedsReview {
		t.Fatalf("expected operator status to expose primary issue details, got %+v", status.BrokerTruth)
	}
	if status.BrokerTruthMessage == "" || status.BrokerTruthRestrictionReason == "" {
		t.Fatalf("expected compatibility broker truth message to be populated")
	}
}
