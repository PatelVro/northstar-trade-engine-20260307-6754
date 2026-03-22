package trader

import (
	"fmt"
	"northstar/audit"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/orders"
	"path/filepath"
	"testing"
	"time"
)

type protectionCall struct {
	symbol       string
	positionSide string
	quantity     float64
	price        float64
}

type protectionTestTrader struct {
	orderStore  *orders.Store
	stopCalls   []protectionCall
	targetCalls []protectionCall
	balance     map[string]interface{}
	positions   []map[string]interface{}
}

type acknowledgedExecutionBroker struct{}

func (b *acknowledgedExecutionBroker) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "ACCEPTED", "localOrderId": "local-accepted", "orderId": int64(909)}, nil
}

func (b *acknowledgedExecutionBroker) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, fmt.Errorf("unexpected open_short")
}

func (b *acknowledgedExecutionBroker) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, fmt.Errorf("unexpected close_long")
}

func (b *acknowledgedExecutionBroker) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, fmt.Errorf("unexpected close_short")
}

func (t *protectionTestTrader) GetBalance() (map[string]interface{}, error) {
	if t.balance == nil {
		return map[string]interface{}{"accountEquity": 100000.0, "availableBalance": 100000.0}, nil
	}
	out := make(map[string]interface{}, len(t.balance))
	for key, value := range t.balance {
		out[key] = value
	}
	return out, nil
}

func (t *protectionTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return clonePositionMaps(t.positions), nil
}

func (t *protectionTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *protectionTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *protectionTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *protectionTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "submitted"}, nil
}

func (t *protectionTestTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *protectionTestTrader) GetMarketPrice(symbol string) (float64, error) { return 100, nil }
func (t *protectionTestTrader) CancelAllOrders(symbol string) error           { return nil }

func (t *protectionTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	t.stopCalls = append(t.stopCalls, protectionCall{
		symbol:       symbol,
		positionSide: positionSide,
		quantity:     quantity,
		price:        stopPrice,
	})
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.RegisterSubmitted(protectiveIntent(positionSide, "stop"), symbol, "SELL", positionSide, quantity, time.Now().UTC())
	return nil
}

func (t *protectionTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	t.targetCalls = append(t.targetCalls, protectionCall{
		symbol:       symbol,
		positionSide: positionSide,
		quantity:     quantity,
		price:        takeProfitPrice,
	})
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.RegisterSubmitted(protectiveIntent(positionSide, "target"), symbol, "SELL", positionSide, quantity, time.Now().UTC())
	return nil
}

func (t *protectionTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

func (t *protectionTestTrader) SnapshotOrderStoreState() orders.StoreState {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	return t.orderStore.SnapshotState()
}

func (t *protectionTestTrader) RestoreOrderStoreState(state orders.StoreState) error {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	return t.orderStore.RestoreState(state)
}

func (t *protectionTestTrader) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	if t.orderStore == nil {
		return nil
	}
	return t.orderStore.Lookup(localID, brokerOrderID)
}

func newProtectionTestAutoTrader(cfg AutoTraderConfig, tr *protectionTestTrader) *AutoTrader {
	at := &AutoTrader{
		id:                 cfg.ID,
		name:               cfg.Name,
		exchange:           cfg.Exchange,
		config:             cfg,
		trader:             tr,
		executionManager:   execution.NewManager(execution.Config{DedupeWindow: time.Minute, StaleAfter: 5 * time.Minute, MaxHistory: 32}),
		initialBalance:     100000,
		positionEntryCycle: map[string]int{},
		positionPeakPnLPct: map[string]float64{},
		positionNewsBias:   map[string]float64{},
		plannedNewsBias:    map[string]float64{},
		isRunning:          true,
	}
	at.executionManager.SetOrderLookup(tr)
	at.initializeBrokerRuntimeState()
	at.initializeReadinessSummary()
	at.initializePromotionSummary()
	at.initializeKillSwitchState()
	at.initializeDataQualityState()
	at.initializePositionReconciliationState()
	at.initializeRiskSupervisorState()
	at.initializePendingProtectionState()
	at.initializeShadowModeState()
	at.initializeRestartRecoveryState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", TradingAllowed: true, CheckedAt: time.Now()})
	at.setLatestAccountSummary(&AccountSummary{AccountingVersion: accountingVersion, StrategyInitialCapital: 100000, StrategyEquity: 100000, AccountEquity: 100000, AvailableBalance: 100000})
	return at
}

func TestHandleEntryProtectionQueuesPendingUntilFillConfirmed(t *testing.T) {
	cfg := AutoTraderConfig{ID: "protect_pending", Name: "Protect Pending", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)

	at.handleEntryProtection(&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110}, &logger.DecisionAction{
		Symbol:        "AAPL",
		Quantity:      10,
		OrderStatus:   string(execution.StatusAcknowledged),
		LocalOrderID:  "local-1",
		BrokerOrderID: "101",
	}, "long", 10)

	summary := at.currentProtectionSummary()
	if summary.PendingCount != 1 {
		t.Fatalf("expected one pending protection request, got %d", summary.PendingCount)
	}
	if summary.ActiveProtectiveCount != 0 {
		t.Fatalf("expected no active protective orders yet, got %d", summary.ActiveProtectiveCount)
	}
	if len(trader.stopCalls) != 0 || len(trader.targetCalls) != 0 {
		t.Fatalf("expected no protective submissions before fill confirmation")
	}
}

func TestProcessPendingProtectionsSubmitsAfterConfirmedFill(t *testing.T) {
	cfg := AutoTraderConfig{ID: "protect_submit", Name: "Protect Submit", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)

	now := time.Now().UTC()
	if err := trader.RestoreOrderStoreState(orders.StoreState{
		Version: 1,
		Orders: []orders.Record{
			{
				LocalID:       "local-1",
				BrokerOrderID: "101",
				Intent:        orders.IntentEntryLong,
				Symbol:        "AAPL",
				Side:          "BUY",
				PositionSide:  "long",
				Status:        orders.StatusFilled,
				RequestedQty:  10,
				FilledQty:     10,
				RemainingQty:  0,
				SubmittedAt:   now.Add(-10 * time.Second),
				UpdatedAt:     now,
				LastSeenAt:    now,
			},
		},
	}); err != nil {
		t.Fatalf("RestoreOrderStoreState failed: %v", err)
	}

	at.upsertPendingProtection(protectionPendingStateForEntry(
		&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110},
		"local-1",
		"101",
		"long",
		10,
		string(execution.StatusAcknowledged),
	))

	at.processPendingProtections(&decision.Context{
		Positions: []decision.PositionInfo{
			{Symbol: "AAPL", Side: "long", Quantity: 10},
		},
	})

	if len(trader.stopCalls) != 1 {
		t.Fatalf("expected one stop-loss submission, got %d", len(trader.stopCalls))
	}
	if len(trader.targetCalls) != 1 {
		t.Fatalf("expected one take-profit submission, got %d", len(trader.targetCalls))
	}
	if trader.stopCalls[0].quantity != 10 || trader.targetCalls[0].quantity != 10 {
		t.Fatalf("expected protective submissions for confirmed quantity 10, got stop=%.4f target=%.4f", trader.stopCalls[0].quantity, trader.targetCalls[0].quantity)
	}
	summary := at.currentProtectionSummary()
	if summary.PendingCount != 0 {
		t.Fatalf("expected pending protections to clear after successful submission, got %d", summary.PendingCount)
	}
	if summary.ActiveProtectiveCount != 2 {
		t.Fatalf("expected two active protective orders after submission, got %d", summary.ActiveProtectiveCount)
	}
}

func TestProcessPendingProtectionsScalesToPartialFill(t *testing.T) {
	cfg := AutoTraderConfig{ID: "protect_partial", Name: "Protect Partial", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)

	now := time.Now().UTC()
	if err := trader.RestoreOrderStoreState(orders.StoreState{
		Version: 1,
		Orders: []orders.Record{
			{
				LocalID:       "local-1",
				BrokerOrderID: "101",
				Intent:        orders.IntentEntryLong,
				Symbol:        "AAPL",
				Side:          "BUY",
				PositionSide:  "long",
				Status:        orders.StatusPartiallyFilled,
				RequestedQty:  10,
				FilledQty:     4,
				RemainingQty:  6,
				SubmittedAt:   now.Add(-10 * time.Second),
				UpdatedAt:     now,
				LastSeenAt:    now,
			},
		},
	}); err != nil {
		t.Fatalf("RestoreOrderStoreState failed: %v", err)
	}

	at.upsertPendingProtection(protectionPendingStateForEntry(
		&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110},
		"local-1",
		"101",
		"long",
		10,
		string(execution.StatusAcknowledged),
	))

	at.processPendingProtections(&decision.Context{
		Positions: []decision.PositionInfo{
			{Symbol: "AAPL", Side: "long", Quantity: 4},
		},
	})

	if len(trader.stopCalls) != 1 || len(trader.targetCalls) != 1 {
		t.Fatalf("expected partial-fill protection submissions for both stop and target, got stop=%d target=%d", len(trader.stopCalls), len(trader.targetCalls))
	}
	if trader.stopCalls[0].quantity != 4 || trader.targetCalls[0].quantity != 4 {
		t.Fatalf("expected protection sized to confirmed partial fill of 4 shares, got stop=%.4f target=%.4f", trader.stopCalls[0].quantity, trader.targetCalls[0].quantity)
	}
	summary := at.currentProtectionSummary()
	if summary.PendingCount != 1 {
		t.Fatalf("expected pending protection to remain while fill is partial, got %d", summary.PendingCount)
	}
	if summary.Pending[0].ConfirmedQuantity != 4 {
		t.Fatalf("expected pending protection to record confirmed qty 4, got %.4f", summary.Pending[0].ConfirmedQuantity)
	}
}

func TestRestartRecoveryRestoresPendingProtectionAndBlocksTrading(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "protect_restart", Name: "Protect Restart", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)
	at.upsertPendingProtection(protectionPendingStateForEntry(
		&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110},
		"local-1",
		"101",
		"long",
		10,
		string(execution.StatusAcknowledged),
	))
	at.persistDurableRuntimeState("pending_protection_restart")

	restoredTrader := &protectionTestTrader{orderStore: orders.NewStore()}
	restored := newProtectionTestAutoTrader(cfg, restoredTrader)
	restored.restoreDurableRuntimeState()

	summary := restored.currentRestartRecoverySummary()
	if !summary.PendingReconciliation || !summary.TradingBlocked {
		t.Fatalf("expected restored pending protection to block trading until reconciliation, got %+v", summary)
	}
	if summary.RestoredPendingProtect != 1 {
		t.Fatalf("expected one restored pending protection, got %d", summary.RestoredPendingProtect)
	}
}

func TestOperatorStatusIncludesAcknowledgedExecutionAndPendingProtection(t *testing.T) {
	cfg := AutoTraderConfig{ID: "protect_status", Name: "Protect Status", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only", ScanInterval: 5 * time.Minute}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)

	result := at.executionManager.Execute(execution.Intent{
		TraderID:          at.id,
		Symbol:            "AAPL",
		Side:              "buy",
		ActionType:        "open_long",
		Quantity:          5,
		OrderType:         "market",
		CreatedAt:         time.Now().UTC(),
		IncreasesExposure: true,
	}, execution.Gate{Mode: "allow", TradingAllowed: true, EntriesAllowed: true, ExitsAllowed: true}, &acknowledgedExecutionBroker{})
	if result.Status != execution.StatusAcknowledged {
		t.Fatalf("expected acknowledged execution status, got %s", result.Status)
	}
	at.handleEntryProtection(&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110}, &logger.DecisionAction{
		Symbol:        "AAPL",
		Quantity:      5,
		OrderStatus:   string(result.Status),
		LocalOrderID:  result.LocalOrderID,
		BrokerOrderID: result.BrokerOrderID,
	}, "long", 5)

	status := at.GetOperatorStatus()
	if status.Execution.AcknowledgedCount != 1 {
		t.Fatalf("expected acknowledged execution count 1, got %d", status.Execution.AcknowledgedCount)
	}
	if status.Protection.PendingCount != 1 {
		t.Fatalf("expected one pending protection in operator status, got %d", status.Protection.PendingCount)
	}
	if status.Protection.Message == "" {
		t.Fatalf("expected protection summary message to be populated")
	}
}

func TestProtectionStateTransitionsWriteJournalEvents(t *testing.T) {
	cleanup := withTempWorkingDir(t)
	defer cleanup()

	cfg := AutoTraderConfig{ID: "protect_journal", Name: "Protect Journal", Mode: "paper", Broker: "ibkr", Exchange: "ibkr", StrategyMode: "momentum_only"}
	trader := &protectionTestTrader{orderStore: orders.NewStore()}
	at := newProtectionTestAutoTrader(cfg, trader)
	at.eventJournal = audit.NewJournal(filepath.Join("output", "audit"), audit.Metadata{
		TraderID:     cfg.ID,
		TraderName:   cfg.Name,
		Mode:         cfg.Mode,
		Broker:       cfg.Broker,
		StrategyMode: cfg.StrategyMode,
	})

	at.handleEntryProtection(&decision.Decision{Symbol: "AAPL", StopLoss: 95, TakeProfit: 110}, &logger.DecisionAction{
		Symbol:        "AAPL",
		Quantity:      10,
		OrderStatus:   string(execution.StatusAcknowledged),
		LocalOrderID:  "local-1",
		BrokerOrderID: "101",
	}, "long", 10)

	now := time.Now().UTC()
	if err := trader.RestoreOrderStoreState(orders.StoreState{
		Version: 1,
		Orders: []orders.Record{
			{
				LocalID:         "local-1",
				BrokerOrderID:   "101",
				Intent:          orders.IntentEntryLong,
				Symbol:          "AAPL",
				Side:            "BUY",
				PositionSide:    "long",
				Status:          orders.StatusFilled,
				RequestedQty:    10,
				FilledQty:       10,
				RemainingQty:    0,
				TruthAuthority:  orders.TruthAuthorityBrokerConfirmed,
				TruthConfidence: orders.TruthConfidenceConfirmed,
				SubmittedAt:     now.Add(-10 * time.Second),
				UpdatedAt:       now,
				LastSeenAt:      now,
			},
		},
	}); err != nil {
		t.Fatalf("RestoreOrderStoreState failed: %v", err)
	}

	at.processPendingProtections(&decision.Context{
		Positions: []decision.PositionInfo{
			{Symbol: "AAPL", Side: "long", Quantity: 10},
		},
	})

	events := readTraderJournalEvents(t, filepath.Join("output", "audit", "journal", cfg.ID, "events.jsonl"))
	hasPending := false
	hasConfirmed := false
	for _, event := range events {
		switch event.Type {
		case "protection_pending_created", "protection_pending_fill", "protection_submission_pending":
			hasPending = true
		case "protection_confirmed":
			hasConfirmed = true
		}
	}
	if !hasPending || !hasConfirmed {
		t.Fatalf("expected protection journal to capture pending and confirmed states, got %+v", events)
	}
}
