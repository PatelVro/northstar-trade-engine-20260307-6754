package trader

import (
	"fmt"
	"math"
	"northstar/decision"
	"northstar/execution"
	"northstar/features"
	"northstar/incidents"
	"northstar/logger"
	"northstar/market"
	"northstar/regime"
	"northstar/risk"
	"northstar/selector"
	"strings"
	"testing"
	"time"
)

func TestExecuteDecisionWithRecord_ShadowModeDoesNotSubmitRealOrder(t *testing.T) {
	mockTrader := &riskExecutionTrader{
		balance: map[string]interface{}{
			"accountCash":      100000.0,
			"accountEquity":    100000.0,
			"availableBalance": 100000.0,
			"grossMarketValue": 0.0,
			"unrealizedPnL":    0.0,
			"realizedPnL":      0.0,
		},
	}

	cfg := AutoTraderConfig{
		Mode:                 "shadow",
		Broker:               "ibkr",
		InstrumentType:       "equity",
		MaxDailyLossPct:      0.05,
		RiskPerTradePct:      0.01,
		MaxGrossExposure:     1.0,
		MaxPositionPct:       0.10,
		MaxConcurrentPos:     3,
		MinLiquidityUSD:      1_000_000,
		MaxParticipationRate: 0.15,
	}

	at := &AutoTrader{
		id:                    "shadow_trader",
		name:                  "Shadow Trader",
		exchange:              "ibkr",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
	}
	at.initializeShadowModeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.initializeBrokerRuntimeState()

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        95,
		TakeProfit:      110,
		Confidence:      78,
		Reasoning:       "shadow-mode test entry",
	}, actionRecord)
	if err != nil {
		t.Fatalf("expected shadow execution to succeed, got %v", err)
	}
	if mockTrader.openLongCalls != 0 {
		t.Fatalf("expected no real broker open-long call, got %d", mockTrader.openLongCalls)
	}
	if got := actionRecord.OrderStatus; got != string(execution.StatusFilled) {
		t.Fatalf("expected hypothetical execution to resolve as filled, got %q", got)
	}
	if actionRecord.Shadow == nil || !actionRecord.Shadow.Active {
		t.Fatalf("expected shadow execution metadata to be recorded")
	}
	if !actionRecord.Shadow.WouldTrade {
		t.Fatalf("expected shadow execution to be marked would_trade")
	}
	if actionRecord.Shadow.HypotheticalNotional <= 0 {
		t.Fatalf("expected positive hypothetical notional, got %.2f", actionRecord.Shadow.HypotheticalNotional)
	}

	shadowSummary := at.currentShadowSummary()
	if shadowSummary.WouldTradeCount != 1 {
		t.Fatalf("expected one shadow would-trade decision, got %d", shadowSummary.WouldTradeCount)
	}
	if shadowSummary.OpenPositions != 1 {
		t.Fatalf("expected one open shadow position, got %d", shadowSummary.OpenPositions)
	}

	status := at.GetOperatorStatus()
	if !status.ShadowMode.Active {
		t.Fatalf("expected operator status to show active shadow mode")
	}
	if status.ShadowMode.DecisionCount != 1 {
		t.Fatalf("expected shadow decision count 1, got %d", status.ShadowMode.DecisionCount)
	}
}

type orderedMarketDataProvider struct {
	errors map[string]error
	calls  []string
}

func (p *orderedMarketDataProvider) GetBars(symbols []string, interval string, limit int) (map[string][]market.Kline, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	result := make(map[string][]market.Kline, len(symbols))
	now := time.Now().UTC()
	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		p.calls = append(p.calls, symbol+"|"+interval)
		if err, ok := p.errors[symbol]; ok && err != nil {
			return nil, err
		}
		bars := buildMarketBars(now.Add(-117*time.Minute), 3*time.Minute, 40, 100, 1000)
		if interval == "4h" {
			bars = buildMarketBars(now.Add(-236*time.Hour), 4*time.Hour, 60, 100, 1000)
		}
		result[symbol] = bars
	}
	return result, nil
}

func TestLoadMomentumMarketDataPreservesCandidateOrderAndResolvesAvailabilityIncident(t *testing.T) {
	at := &AutoTrader{
		id:       "shadow_trader",
		name:     "Shadow Trader",
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:               "shadow",
			Broker:             "sim",
			DataProvider:       "ibkr",
			InstrumentType:     "equity",
			StrategyMode:       "momentum_only",
			CandidateBatchSize: 3,
			UseMacroFilters:    false,
		},
		provider:        &orderedMarketDataProvider{},
		incidentManager: incidents.NewManager("shadow_trader"),
	}

	ctx := &decision.Context{
		CandidateCoins: []decision.CandidateCoin{
			{Symbol: "MSFT"},
			{Symbol: "AAPL"},
			{Symbol: "NVDA"},
		},
	}

	if err := at.loadMomentumMarketData(ctx); err != nil {
		t.Fatalf("expected market data load to succeed, got %v", err)
	}

	provider := at.provider.(*orderedMarketDataProvider)
	wantPrefix := []string{"AAPL|3m", "MSFT|3m", "NVDA|3m", "SPY|3m", "QQQ|3m", "MSFT|3m", "MSFT|4h", "AAPL|3m", "AAPL|4h", "NVDA|3m", "NVDA|4h"}
	if len(provider.calls) < len(wantPrefix) {
		t.Fatalf("expected at least %d provider calls, got %d", len(wantPrefix), len(provider.calls))
	}
	for i, want := range wantPrefix {
		if got := provider.calls[i]; got != want {
			t.Fatalf("expected call %d to be %s, got %s", i, want, got)
		}
	}

	summary := at.currentIncidentSummary()
	if summary.OpenCount != 0 {
		t.Fatalf("expected no open market-data incident after successful load, got %d", summary.OpenCount)
	}
}

func TestLoadMomentumMarketDataRaisesExpectedBlockForChartUnavailable(t *testing.T) {
	provider := &orderedMarketDataProvider{
		errors: map[string]error{
			"AAPL": fmt.Errorf("GET: /v1/api/iserver/marketdata/history: HTTP 500: {\"error\":\"Chart data unavailable\"}"),
			"MSFT": fmt.Errorf("GET: /v1/api/iserver/marketdata/history: HTTP 500: {\"error\":\"Chart data unavailable\"}"),
		},
	}
	at := &AutoTrader{
		id:       "shadow_trader",
		name:     "Shadow Trader",
		exchange: "ibkr",
		config: AutoTraderConfig{
			Mode:               "shadow",
			Broker:             "sim",
			DataProvider:       "ibkr",
			InstrumentType:     "equity",
			StrategyMode:       "momentum_only",
			CandidateBatchSize: 2,
			UseMacroFilters:    false,
		},
		provider:        provider,
		incidentManager: incidents.NewManager("shadow_trader"),
	}

	ctx := &decision.Context{
		CandidateCoins: []decision.CandidateCoin{
			{Symbol: "AAPL"},
			{Symbol: "MSFT"},
		},
	}

	err := at.loadMomentumMarketData(ctx)
	if err == nil {
		t.Fatalf("expected market-data load to fail when IBKR chart history is unavailable")
	}
	if !at.isExpectedMarketDataBlock(err) {
		t.Fatalf("expected chart-unavailable error to be recognized as an expected market-data block, got %v", err)
	}

	summary := at.currentIncidentSummary()
	if summary.OpenCount != 1 {
		t.Fatalf("expected one open market-data incident, got %d", summary.OpenCount)
	}
	if got := summary.OpenIncidents[0].IncidentType; got != incidents.TypeMarketDataValidationFailed {
		t.Fatalf("expected incident type %s, got %s", incidents.TypeMarketDataValidationFailed, got)
	}
	if !strings.Contains(strings.ToLower(summary.OpenIncidents[0].Summary), "chart history") {
		t.Fatalf("expected incident summary to mention chart history, got %q", summary.OpenIncidents[0].Summary)
	}

	provider.errors = nil
	if err := at.loadMomentumMarketData(ctx); err != nil {
		t.Fatalf("expected market-data load to recover, got %v", err)
	}
	summary = at.currentIncidentSummary()
	if summary.OpenCount != 0 {
		t.Fatalf("expected market-data incident to resolve after recovery, got %d open incidents", summary.OpenCount)
	}
}

func TestApplyCanonicalRuntimeStrategyDispatchBlocksNoTradeEntriesInShadowMode(t *testing.T) {
	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	at := &AutoTrader{
		config: AutoTraderConfig{
			Mode:                  "shadow",
			InstrumentType:        "equity",
			StrategyMode:          "momentum_only",
			DynamicPositionSizing: true,
			FallbackPositionPct:   0.10,
			RiskPerTradePct:       0.01,
			MaxPositionPct:        0.20,
			MaxGrossExposure:      1.0,
			MaxNetExposurePct:     0.65,
		},
		peakEquitySeen: 100000,
	}

	ctx := &decision.Context{
		Account: decision.AccountInfo{
			AccountEquity:    100000,
			StrategyEquity:   100000,
			AvailableBalance: 100000,
		},
		MarketDataMap: map[string]*market.Data{
			"AAPL": {
				Symbol:       "AAPL",
				CurrentPrice: 100,
				Features: &features.FeatureSet{
					Vectors: map[string]*features.FeatureVector{
						"4h": {Symbol: "AAPL", Timestamp: now, Timeframe: "4h", Valid: true},
					},
				},
				Regimes: &regime.ResultSet{
					Results: map[string]*regime.Result{
						"4h": {Symbol: "AAPL", Timestamp: now, Timeframe: "4h", Regime: regime.RegimeUnstable, Confidence: 0.9, Valid: true},
					},
				},
				Selections: &selector.SelectionSet{
					Selections: map[string]*selector.Selection{
						"4h": {
							Symbol:              "AAPL",
							Timestamp:           now,
							Timeframe:           "4h",
							SelectedFamily:      selector.StrategyFamilyNoTrade,
							SelectedStrategy:    "momentum_fallback",
							AllowTrading:        false,
							RecommendedRiskMode: selector.RiskModeNoTrade,
							SelectionReason:     "unstable regime",
							Valid:               true,
						},
					},
				},
			},
		},
	}

	fullDecision := &decision.FullDecision{
		Decisions: []decision.Decision{
			{Symbol: "AAPL", Action: "open_long", PositionSizeUSD: 10000, StopLoss: 95, Confidence: 80, Reasoning: "test entry"},
		},
	}

	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
	if len(fullDecision.Decisions) != 1 {
		t.Fatalf("expected one decision, got %d", len(fullDecision.Decisions))
	}
	if got := fullDecision.Decisions[0].Action; got != "wait" {
		t.Fatalf("expected blocked decision to become wait, got %s", got)
	}
	if !strings.Contains(fullDecision.Decisions[0].Reasoning, "Canonical pipeline blocked AAPL open_long") {
		t.Fatalf("expected blocking reason to be preserved, got %q", fullDecision.Decisions[0].Reasoning)
	}
}

func TestPaperSessionTrackerCapturesShadowDecisionCounts(t *testing.T) {
	tracker := newPaperSessionTracker(&AutoTrader{
		id:     "shadow_trader",
		name:   "Shadow Trader",
		config: AutoTraderConfig{Mode: "shadow", Broker: "ibkr", StrategyMode: "multi_factor"},
	}, time.Now())

	now := time.Now().UTC()
	record := &logger.DecisionRecord{
		ShadowMode: true,
		Decisions: []logger.DecisionAction{
			{
				Action:      "open_long",
				Symbol:      "AAPL",
				Success:     true,
				OrderStatus: string(execution.StatusFilled),
				Quantity:    5,
				Price:       100,
				Shadow: &logger.ShadowExecution{
					Active:               true,
					RecordedAt:           now,
					Status:               "would_trade",
					WouldTrade:           true,
					HypotheticalQuantity: 5,
					HypotheticalNotional: 500,
				},
			},
			{
				Action:      "open_long",
				Symbol:      "MSFT",
				Success:     false,
				OrderStatus: string(execution.StatusBlocked),
				Shadow: &logger.ShadowExecution{
					Active:      true,
					RecordedAt:  now.Add(time.Second),
					Status:      "blocked",
					WouldTrade:  false,
					BlockReason: "selector blocked entry",
				},
			},
		},
	}

	tracker.observeDecisionRecord(record)

	if !tracker.report.ShadowModeActive {
		t.Fatalf("expected shadow mode to be marked active")
	}
	if tracker.report.ShadowDecisionsTotal != 2 {
		t.Fatalf("expected 2 shadow decisions, got %d", tracker.report.ShadowDecisionsTotal)
	}
	if tracker.report.ShadowWouldTradeCount != 1 {
		t.Fatalf("expected 1 shadow would-trade decision, got %d", tracker.report.ShadowWouldTradeCount)
	}
	if tracker.report.ShadowBlockedCount != 1 {
		t.Fatalf("expected 1 shadow blocked decision, got %d", tracker.report.ShadowBlockedCount)
	}
	if tracker.report.ShadowLastDecisionAt == "" {
		t.Fatalf("expected shadow last decision timestamp to be recorded")
	}
}

func TestShadowModeAccountAndRiskUseHypotheticalPortfolio(t *testing.T) {
	mockTrader := &riskExecutionTrader{
		balance: map[string]interface{}{
			"accountCash":      100000.0,
			"accountEquity":    100000.0,
			"availableBalance": 100000.0,
			"grossMarketValue": 2500.0,
			"unrealizedPnL":    10.0,
			"realizedPnL":      0.0,
		},
		positions: []map[string]interface{}{
			{
				"symbol":           "MSFT",
				"side":             "long",
				"entryPrice":       250.0,
				"markPrice":        252.0,
				"positionAmt":      10.0,
				"unRealizedProfit": 20.0,
				"liquidationPrice": 0.0,
				"leverage":         1.0,
			},
		},
	}

	cfg := AutoTraderConfig{
		Mode:                 "shadow",
		Broker:               "ibkr",
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
		id:                    "shadow_trader",
		name:                  "Shadow Trader",
		exchange:              "ibkr",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
	}
	at.initializeShadowModeState()
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.initializeBrokerRuntimeState()

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        95,
		TakeProfit:      110,
		Confidence:      78,
		Reasoning:       "shadow-mode test entry",
	}, actionRecord)
	if err != nil {
		t.Fatalf("expected shadow execution to succeed, got %v", err)
	}

	positions, err := at.GetPositions()
	if err != nil {
		t.Fatalf("expected shadow positions to be available, got %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected only the hypothetical shadow position, got %d positions", len(positions))
	}
	if got := positions[0]["symbol"]; got != "AAPL" {
		t.Fatalf("expected shadow position symbol AAPL, got %v", got)
	}

	account, err := at.GetAccountInfo()
	if err != nil {
		t.Fatalf("expected shadow account snapshot, got %v", err)
	}
	if account.PositionCount != 1 {
		t.Fatalf("expected shadow account to report one position, got %d", account.PositionCount)
	}
	if account.GrossMarketValue < 9999 || account.GrossMarketValue > 10001 {
		t.Fatalf("expected gross market value near 10000, got %.2f", account.GrossMarketValue)
	}
	if account.AvailableBalance >= 100000 {
		t.Fatalf("expected available balance to reflect hypothetical deployment, got %.2f", account.AvailableBalance)
	}

	riskCtx, err := at.evaluatePreTradeRisk(&decision.Decision{
		Symbol:     "AAPL",
		Action:     "close_long",
		Confidence: 70,
		Reasoning:  "shadow-mode close validation",
	})
	if err != nil {
		t.Fatalf("expected close risk evaluation to see shadow position, got %v", err)
	}
	if riskCtx.requested.Action != "close_long" {
		t.Fatalf("expected close_long request, got %s", riskCtx.requested.Action)
	}
	if math.Abs(riskCtx.requested.RequestedQuantity-actionRecord.Quantity) > 1e-6 {
		t.Fatalf("expected close quantity %.4f to match shadow position quantity %.4f", riskCtx.requested.RequestedQuantity, actionRecord.Quantity)
	}
	if riskCtx.accountInfo.PositionCount != 1 {
		t.Fatalf("expected shadow account info in risk context, got %d positions", riskCtx.accountInfo.PositionCount)
	}
}
