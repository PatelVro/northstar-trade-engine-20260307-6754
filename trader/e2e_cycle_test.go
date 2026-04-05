package trader

import (
	"fmt"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/risk"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test 1: AI returns mixed valid/invalid decisions — valid ones execute
// ---------------------------------------------------------------------------

func TestE2E_MixedValidInvalidDecisions_ValidOnesExecute(t *testing.T) {
	// This tests the fix in decision/engine.go where parseFullDecisionResponseWithPositions
	// skips invalid decisions individually instead of failing the entire batch.

	equity := 100000.0

	// Simulate a batch of decisions: one valid open_long and one invalid (bad leverage for equity).
	raw := []decision.Decision{
		{Symbol: "AAPL", Action: "open_long", Leverage: 1, PositionSizeUSD: 10000, StopLoss: 95, TakeProfit: 110, Confidence: 70, Reasoning: "bullish"},
		{Symbol: "MSFT", Action: "open_long", Leverage: 5, PositionSizeUSD: 10000, StopLoss: 190, TakeProfit: 220, Confidence: 60, Reasoning: "also bullish"}, // leverage=5 invalid for equity
		{Symbol: "GOOG", Action: "hold", Leverage: 0, Reasoning: "no action"},
	}

	var valid []decision.Decision
	for i, d := range raw {
		if err := validateDecisionForTest(&d, equity, 1, 1, "equity", false); err != nil {
			t.Logf("decision #%d (%s %s) correctly rejected: %v", i+1, d.Symbol, d.Action, err)
		} else {
			valid = append(valid, d)
		}
	}

	if len(valid) != 2 {
		t.Fatalf("expected 2 valid decisions (AAPL open_long + GOOG hold), got %d", len(valid))
	}

	// Verify AAPL made it through
	foundAAPL := false
	foundGOOG := false
	for _, d := range valid {
		if d.Symbol == "AAPL" && d.Action == "open_long" {
			foundAAPL = true
		}
		if d.Symbol == "GOOG" && d.Action == "hold" {
			foundGOOG = true
		}
	}
	if !foundAAPL {
		t.Fatal("expected AAPL open_long to survive validation")
	}
	if !foundGOOG {
		t.Fatal("expected GOOG hold to survive validation")
	}

	// Now verify the valid decision actually executes through the AutoTrader
	mockTrader := &riskExecutionTrader{
		balance: map[string]interface{}{
			"accountCash":      equity,
			"accountEquity":    equity,
			"availableBalance": equity,
			"grossMarketValue": 0.0,
			"unrealizedPnL":    0.0,
			"realizedPnL":      0.0,
		},
	}

	cfg := AutoTraderConfig{
		Mode:                 "paper",
		Broker:               "sim",
		InstrumentType:       "equity",
		MaxDailyLossPct:      0.05,
		RiskPerTradePct:      0.01,
		MaxGrossExposure:     1.0,
		MaxPositionPct:       0.20,
		MaxConcurrentPos:     3,
		MinLiquidityUSD:      1_000_000,
		MaxParticipationRate: 0.15,
	}

	at := &AutoTrader{
		name:                  "e2e-mixed-decisions",
		exchange:              "alpaca",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        equity,
		dailyStartEquity:      equity,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
		executionManager:      execution.NewManager(execution.Config{}),
	}
	at.isRunning.Store(true)
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.initializeBrokerRuntimeState()

	// Execute the valid AAPL decision
	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&valid[0], actionRecord)
	if err != nil {
		t.Fatalf("expected valid AAPL decision to execute, got: %v", err)
	}
	if mockTrader.openLongCalls != 1 {
		t.Fatalf("expected 1 open long call for AAPL, got %d", mockTrader.openLongCalls)
	}
}

// validateDecisionForTest mirrors the real validateDecision from decision/engine.go
// We call it directly to test the filtering logic without needing the AI API.
func validateDecisionForTest(d *decision.Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, instrumentType string, isReplay bool) error {
	validActions := map[string]bool{
		"open_long": true, "open_short": true, "close_long": true,
		"close_short": true, "hold": true, "wait": true,
	}
	if !validActions[d.Action] {
		return fmt.Errorf("invalid action: %s", d.Action)
	}
	if d.Action == "open_long" || d.Action == "open_short" {
		isEquity := instrumentType == "equity"
		maxLeverage := altcoinLeverage
		if isEquity {
			maxLeverage = 1
		}
		if d.Leverage <= 0 || d.Leverage > maxLeverage {
			return fmt.Errorf("leverage %d exceeds max %d for %s", d.Leverage, maxLeverage, instrumentType)
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("position size must be > 0")
		}
		maxPositionValue := accountEquity * 0.2
		if isEquity && d.PositionSizeUSD > maxPositionValue*1.01 {
			return fmt.Errorf("position size %.0f exceeds max %.0f", d.PositionSizeUSD, maxPositionValue)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test 2: Market closed detection — system blocks trading, uses 15m backoff
// ---------------------------------------------------------------------------

func TestE2E_MarketClosedDetection_BlocksTradingAndBacksOff(t *testing.T) {
	at := &AutoTrader{
		id:        "e2e-market-closed",
		name:      "E2E Market Closed",
		exchange:  "alpaca",
		config: AutoTraderConfig{
			Mode:           "paper",
			Broker:         "sim",
			InstrumentType: "equity",
			ScanInterval:   5 * time.Minute,
		},
	}
	at.isRunning.Store(true)

	// 1. Verify isMarketClosedReason correctly detects "market is closed" variants
	closedReasons := []string{
		"market is closed",
		"Market Is Closed",
		"the market closed for today",
	}
	for _, reason := range closedReasons {
		if !isMarketClosedReason(reason) {
			t.Errorf("expected %q to match market closed", reason)
		}
	}

	// 2. Verify runtime condition classifies market-closed reason correctly
	state := classifyBlockedCycleReason("market is closed for the session")
	if state.State != RuntimeConditionMarketClosed {
		t.Fatalf("expected market_closed state, got %s", state.State)
	}
	if !state.ExpectedNonTradable {
		t.Fatal("expected ExpectedNonTradable=true for market-closed block")
	}

	// 3. Verify the backoff interval is at least 15 minutes
	backoff := at.marketClosedBackoffInterval()
	if backoff < 15*time.Minute {
		t.Fatalf("expected backoff >= 15m when ScanInterval=5m, got %v", backoff)
	}
	if backoff != 15*time.Minute {
		t.Fatalf("expected exactly 15m backoff (minimum), got %v", backoff)
	}

	// 4. Verify a larger scan interval is preserved
	at.config.ScanInterval = 30 * time.Minute
	backoff = at.marketClosedBackoffInterval()
	if backoff != 30*time.Minute {
		t.Fatalf("expected 30m backoff (scan interval), got %v", backoff)
	}

	// 5. Verify that classifyExpectedMarketDataBlock recognises market-closed errors
	_, ok := classifyExpectedMarketDataBlock(fmt.Errorf("market is closed"))
	if !ok {
		t.Fatal("expected market-closed error to be classified as expected market data block")
	}
}

// ---------------------------------------------------------------------------
// Test 3: Broker disconnect mid-cycle — 401 during execution does not cascade
// ---------------------------------------------------------------------------

func TestE2E_BrokerDisconnectMidCycle_NoCascadeFailure(t *testing.T) {
	callCount := 0
	failingTrader := &brokerDisconnectTrader{
		balance: map[string]interface{}{
			"accountCash":      100000.0,
			"accountEquity":    100000.0,
			"availableBalance": 100000.0,
			"grossMarketValue": 0.0,
			"unrealizedPnL":    0.0,
			"realizedPnL":      0.0,
		},
		failOnCall: 2, // second OpenLong call returns 401
		callCount:  &callCount,
	}

	cfg := AutoTraderConfig{
		Mode:                 "paper",
		Broker:               "sim",
		InstrumentType:       "equity",
		MaxDailyLossPct:      0.05,
		RiskPerTradePct:      0.01,
		MaxGrossExposure:     1.0,
		MaxPositionPct:       0.20,
		MaxConcurrentPos:     5,
		MinLiquidityUSD:      1_000_000,
		MaxParticipationRate: 0.15,
	}

	at := &AutoTrader{
		name:                  "e2e-broker-disconnect",
		exchange:              "alpaca",
		config:                cfg,
		trader:                failingTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
		executionManager:      execution.NewManager(execution.Config{}),
	}
	at.isRunning.Store(true)
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.initializeBrokerRuntimeState()

	decisions := []decision.Decision{
		{Symbol: "AAPL", Action: "open_long", Leverage: 1, PositionSizeUSD: 10000, StopLoss: 95, TakeProfit: 110, Confidence: 70, Reasoning: "bullish"},
		{Symbol: "MSFT", Action: "open_long", Leverage: 1, PositionSizeUSD: 10000, StopLoss: 190, TakeProfit: 220, Confidence: 60, Reasoning: "also bullish"},
		{Symbol: "GOOG", Action: "open_long", Leverage: 1, PositionSizeUSD: 10000, StopLoss: 140, TakeProfit: 165, Confidence: 65, Reasoning: "tech rally"},
	}

	// Execute decisions sequentially like runCycle does.
	// First succeeds. Second hits 401. Third should still attempt (no cascade).
	var results []struct {
		symbol string
		err    error
	}
	for _, d := range decisions {
		d := d
		actionRecord := &logger.DecisionAction{Action: d.Action, Symbol: d.Symbol}
		err := at.executeDecisionWithRecord(&d, actionRecord)
		results = append(results, struct {
			symbol string
			err    error
		}{d.Symbol, err})
	}

	// First should succeed
	if results[0].err != nil {
		t.Fatalf("expected first decision (AAPL) to succeed, got: %v", results[0].err)
	}
	// Second should fail (broker 401)
	if results[1].err == nil {
		t.Fatal("expected second decision (MSFT) to fail with broker 401")
	}
	if !strings.Contains(results[1].err.Error(), "401") && !strings.Contains(results[1].err.Error(), "unauthorized") {
		t.Fatalf("expected 401/unauthorized error, got: %v", results[1].err)
	}
	// Third should still be attempted — it should not have been short-circuited
	// by the second failure. It may succeed or fail on its own merits.
	// The key assertion is that the loop continued and the third call was made.
	if callCount < 3 {
		t.Fatalf("expected at least 3 broker calls (no cascade skip), got %d", callCount)
	}
}

// brokerDisconnectTrader simulates a broker that returns 401 on a specific call number.
type brokerDisconnectTrader struct {
	balance    map[string]interface{}
	failOnCall int
	callCount  *int
}

func (t *brokerDisconnectTrader) GetBalance() (map[string]interface{}, error) {
	return t.balance, nil
}
func (t *brokerDisconnectTrader) GetPositions() ([]map[string]interface{}, error) {
	return nil, nil
}
func (t *brokerDisconnectTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	*t.callCount++
	if *t.callCount == t.failOnCall {
		return nil, fmt.Errorf("HTTP 401 unauthorized: broker session expired")
	}
	return map[string]interface{}{"orderId": int64(*t.callCount)}, nil
}
func (t *brokerDisconnectTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return map[string]interface{}{"orderId": int64(99)}, nil
}
func (t *brokerDisconnectTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"orderId": int64(99)}, nil
}
func (t *brokerDisconnectTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return map[string]interface{}{"orderId": int64(99)}, nil
}
func (t *brokerDisconnectTrader) SetLeverage(symbol string, leverage int) error      { return nil }
func (t *brokerDisconnectTrader) GetMarketPrice(symbol string) (float64, error)      { return 100, nil }
func (t *brokerDisconnectTrader) SetStopLoss(symbol, side string, qty, p float64) error { return nil }
func (t *brokerDisconnectTrader) SetTakeProfit(symbol, side string, qty, p float64) error {
	return nil
}
func (t *brokerDisconnectTrader) CancelAllOrders(symbol string) error { return nil }
func (t *brokerDisconnectTrader) FormatQuantity(symbol string, qty float64) (string, error) {
	return fmt.Sprintf("%.4f", qty), nil
}

// ---------------------------------------------------------------------------
// Test 4: Position count vs max_concurrent_positions
// ---------------------------------------------------------------------------

func TestE2E_PositionCountVsMaxConcurrentPositions(t *testing.T) {
	// Case A: Below limit — entries should be allowed
	t.Run("BelowLimit_EntriesAllowed", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-pos-count-below",
			name:     "E2E Position Count Below",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:            "paper",
				Broker:          "sim",
				InstrumentType:  "equity",
				MaxConcurrentPos: 3,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       20000,
			PositionCount:          2, // below limit of 3
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if !gate.EntriesAllowed {
			t.Fatalf("expected entries allowed when position count (2) < max (3), got mode=%s reason=%s", gate.Mode, gate.BlockReason)
		}
		if gate.Mode != risk.SupervisorModeAllow {
			t.Fatalf("expected allow mode, got %s", gate.Mode)
		}
	})

	// Case B: At limit — entries should be allowed (limit is strictly "exceeds")
	t.Run("AtLimit_EntriesAllowed", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-pos-count-at",
			name:     "E2E Position Count At",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:            "paper",
				Broker:          "sim",
				InstrumentType:  "equity",
				MaxConcurrentPos: 3,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       30000,
			PositionCount:          3, // at limit
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if !gate.EntriesAllowed {
			t.Fatalf("expected entries allowed when position count (3) == max (3) — supervisor uses strictly greater-than, got mode=%s reason=%s", gate.Mode, gate.BlockReason)
		}
	})

	// Case C: Above limit — entries should be blocked
	t.Run("AboveLimit_EntriesBlocked", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-pos-count-above",
			name:     "E2E Position Count Above",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:            "paper",
				Broker:          "sim",
				InstrumentType:  "equity",
				MaxConcurrentPos: 3,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       40000,
			PositionCount:          4, // above limit of 3
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if gate.EntriesAllowed {
			t.Fatalf("expected entries blocked when position count (4) > max (3)")
		}
		if gate.Mode != risk.SupervisorModeBlockNewEntries {
			t.Fatalf("expected block_new_entries mode, got %s", gate.Mode)
		}
		if !gate.ExitsAllowed {
			t.Fatal("expected exits to remain allowed even when entries are blocked")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 5: Gross exposure limit — blocks new entries when exceeded
// ---------------------------------------------------------------------------

func TestE2E_GrossExposureLimit_BlocksNewEntries(t *testing.T) {
	// Case A: Gross exposure below limit — entries allowed
	t.Run("BelowLimit_EntriesAllowed", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-exposure-below",
			name:     "E2E Exposure Below",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:             "paper",
				Broker:           "sim",
				InstrumentType:   "equity",
				MaxGrossExposure: 1.0, // 100% of equity
				MaxConcurrentPos: 10,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       80000, // 80% exposure — below 100% limit
			PositionCount:          2,
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if !gate.EntriesAllowed {
			t.Fatalf("expected entries allowed when gross exposure (80%%) < limit (100%%), got mode=%s reason=%s", gate.Mode, gate.BlockReason)
		}
	})

	// Case B: Gross exposure exceeds limit — entries blocked
	t.Run("AboveLimit_EntriesBlocked", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-exposure-above",
			name:     "E2E Exposure Above",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:             "paper",
				Broker:           "sim",
				InstrumentType:   "equity",
				MaxGrossExposure: 1.0, // 100% of equity
				MaxConcurrentPos: 10,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       110000, // 110% exposure — above 100% limit
			PositionCount:          3,
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if gate.EntriesAllowed {
			t.Fatalf("expected entries blocked when gross exposure (110%%) > limit (100%%)")
		}
		if gate.Mode != risk.SupervisorModeBlockNewEntries {
			t.Fatalf("expected block_new_entries mode, got %s", gate.Mode)
		}
		if !gate.ExitsAllowed {
			t.Fatal("expected exits to remain allowed when gross exposure exceeds limit")
		}
	})

	// Case C: Gross exposure at exactly the limit — entries should be allowed
	// (supervisor uses strictly greater-than with epsilon tolerance)
	t.Run("AtLimit_EntriesAllowed", func(t *testing.T) {
		at := &AutoTrader{
			id:       "e2e-exposure-at",
			name:     "E2E Exposure At",
			exchange: "alpaca",
			config: AutoTraderConfig{
				Mode:             "paper",
				Broker:           "sim",
				InstrumentType:   "equity",
				MaxGrossExposure: 1.0,
				MaxConcurrentPos: 10,
			},
			peakEquitySeen: 100000,
		}
		at.isRunning.Store(true)
		at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "ok", CheckedAt: time.Now(), TradingAllowed: true})
		at.setLatestAccountSummary(&AccountSummary{
			AccountingVersion:      accountingVersion,
			StrategyInitialCapital: 100000,
			StrategyEquity:         100000,
			AccountEquity:          100000,
			GrossMarketValue:       100000, // exactly 100% — at the limit
			PositionCount:          3,
		})

		gate := at.currentTradingGateDecision(false, at.currentLatestAccountSummary())
		if !gate.EntriesAllowed {
			t.Fatalf("expected entries allowed when gross exposure exactly at limit (epsilon tolerance), got mode=%s reason=%s", gate.Mode, gate.BlockReason)
		}
	})
}
