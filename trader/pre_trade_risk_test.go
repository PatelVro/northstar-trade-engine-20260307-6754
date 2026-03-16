package trader

import (
	"fmt"
	"math"
	"northstar/decision"
	"northstar/execution"
	"northstar/logger"
	"northstar/market"
	"northstar/risk"
	"strings"
	"testing"
	"time"
)

type riskTestProvider struct {
	price         float64
	currentVolume float64
	averageVolume float64
}

func (p *riskTestProvider) GetBars(symbols []string, interval string, limit int) (map[string][]market.Kline, error) {
	if limit < 60 {
		limit = 60
	}
	step := 3 * time.Minute
	switch interval {
	case "4h":
		step = 4 * time.Hour
	case "1h":
		step = time.Hour
	case "1m":
		step = time.Minute
	}
	start := time.Now().UTC().Add(-time.Duration(limit) * step)
	out := make(map[string][]market.Kline, len(symbols))
	for _, symbol := range symbols {
		bars := make([]market.Kline, 0, limit)
		for i := 0; i < limit; i++ {
			volume := p.averageVolume
			if i == limit-1 && p.currentVolume > 0 {
				volume = p.currentVolume
			}
			price := p.price + float64(i)*0.01
			openTime := start.Add(time.Duration(i) * step)
			closeTime := openTime.Add(step)
			bars = append(bars, market.Kline{
				OpenTime:  openTime.UnixMilli(),
				Open:      price - 0.25,
				High:      price + 0.5,
				Low:       price - 0.5,
				Close:     price,
				Volume:    volume,
				CloseTime: closeTime.UnixMilli(),
			})
		}
		out[symbol] = bars
	}
	return out, nil
}

type riskExecutionTrader struct {
	balance           map[string]interface{}
	positions         []map[string]interface{}
	openLongCalls     int
	openShortCalls    int
	closeLongCalls    int
	closeShortCalls   int
	lastOpenLongQty   float64
	lastCloseLongQty  float64
	lastCloseShortQty float64
	lastOpenShortQty  float64
}

func (t *riskExecutionTrader) GetBalance() (map[string]interface{}, error) {
	return t.balance, nil
}

func (t *riskExecutionTrader) GetPositions() ([]map[string]interface{}, error) {
	return t.positions, nil
}

func (t *riskExecutionTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	t.openLongCalls++
	t.lastOpenLongQty = quantity
	return map[string]interface{}{"orderId": int64(1)}, nil
}

func (t *riskExecutionTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	t.openShortCalls++
	t.lastOpenShortQty = quantity
	return map[string]interface{}{"orderId": int64(2)}, nil
}

func (t *riskExecutionTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	t.closeLongCalls++
	t.lastCloseLongQty = quantity
	return map[string]interface{}{"orderId": int64(3)}, nil
}

func (t *riskExecutionTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	t.closeShortCalls++
	t.lastCloseShortQty = quantity
	return map[string]interface{}{"orderId": int64(4)}, nil
}

func (t *riskExecutionTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *riskExecutionTrader) GetMarketPrice(symbol string) (float64, error) { return 100, nil }
func (t *riskExecutionTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (t *riskExecutionTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (t *riskExecutionTrader) CancelAllOrders(symbol string) error { return nil }
func (t *riskExecutionTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

func TestExecuteDecisionWithRecord_RiskRejectStopsExecution(t *testing.T) {
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

	at := &AutoTrader{
		name:                  "risk-test",
		exchange:              "alpaca",
		config:                AutoTraderConfig{Mode: "paper", Broker: "sim", InstrumentType: "equity", MaxDailyLossPct: 0.05, RiskPerTradePct: 0.01, MaxGrossExposure: 1.0, MaxPositionPct: 0.20, MaxConcurrentPos: 3, MinLiquidityUSD: 1_000_000, MaxParticipationRate: 0.15},
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		dailyPnL:              0,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(AutoTraderConfig{MaxDailyLossPct: 0.05, RiskPerTradePct: 0.01, MaxGrossExposure: 1.0, MaxPositionPct: 0.20, MaxConcurrentPos: 3, MinLiquidityUSD: 1_000_000, MaxParticipationRate: 0.15})),
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        0,
		TakeProfit:      110,
	}, actionRecord)
	if err == nil {
		t.Fatalf("expected risk rejection error")
	}
	if mockTrader.openLongCalls != 0 {
		t.Fatalf("expected no execution call, got %d", mockTrader.openLongCalls)
	}
	if actionRecord.RiskOutcome != string(risk.OutcomeReject) {
		t.Fatalf("expected reject risk outcome, got %q", actionRecord.RiskOutcome)
	}
	if !strings.Contains(err.Error(), "risk engine rejected") {
		t.Fatalf("expected risk rejection error, got %v", err)
	}
}

func TestExecuteDecisionWithRecord_RiskReduceSizeAdjustsExecutionQuantity(t *testing.T) {
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
		Mode:                 "paper",
		Broker:               "sim",
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
		name:                  "risk-test",
		exchange:              "alpaca",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 20000,
		StopLoss:        95,
		TakeProfit:      110,
	}, actionRecord)
	if err != nil {
		t.Fatalf("expected execution to proceed after size reduction, got %v", err)
	}
	if mockTrader.openLongCalls != 1 {
		t.Fatalf("expected one execution call, got %d", mockTrader.openLongCalls)
	}
	if actionRecord.RiskOutcome != string(risk.OutcomeReduceSize) {
		t.Fatalf("expected reduce_size outcome, got %q", actionRecord.RiskOutcome)
	}
	if math.Abs(actionRecord.RiskApprovedNotional-10000) > 0.01 {
		t.Fatalf("expected approved notional 10000, got %.2f", actionRecord.RiskApprovedNotional)
	}
	if math.Abs(mockTrader.lastOpenLongQty-actionRecord.RiskApprovedQuantity) > 0.0001 {
		t.Fatalf("expected execution quantity %.4f, got %.4f", actionRecord.RiskApprovedQuantity, mockTrader.lastOpenLongQty)
	}
}

func TestExecuteDecisionWithRecord_PortfolioSectorRiskRejectStopsExecution(t *testing.T) {
	mockTrader := &riskExecutionTrader{
		balance: map[string]interface{}{
			"accountCash":      100000.0,
			"accountEquity":    100000.0,
			"availableBalance": 100000.0,
			"grossMarketValue": 30000.0,
			"unrealizedPnL":    0.0,
			"realizedPnL":      0.0,
		},
		positions: []map[string]interface{}{
			{
				"symbol":      "AAPL",
				"side":        "long",
				"positionAmt": 300.0,
				"markPrice":   100.0,
			},
		},
	}

	cfg := AutoTraderConfig{
		Mode:                   "paper",
		Broker:                 "sim",
		InstrumentType:         "equity",
		MaxDailyLossPct:        0.05,
		RiskPerTradePct:        0.01,
		MaxGrossExposure:       1.0,
		MaxPositionPct:         0.20,
		MaxConcurrentPos:       3,
		MinLiquidityUSD:        1_000_000,
		MaxParticipationRate:   0.15,
		MaxNetExposurePct:      0.65,
		MaxSectorExposurePct:   0.30,
		MaxCorrelatedPositions: 1,
		MaxPairCorrelation:     0.82,
		MaxDrawdown:            20.0,
	}

	at := &AutoTrader{
		name:                  "risk-test",
		exchange:              "alpaca",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		peakEquitySeen:        100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})

	actionRecord := &logger.DecisionAction{Action: "open_long", Symbol: "MSFT"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "MSFT",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        95,
		TakeProfit:      110,
	}, actionRecord)
	if err == nil {
		t.Fatalf("expected sector risk rejection")
	}
	if mockTrader.openLongCalls != 0 {
		t.Fatalf("expected no execution call, got %d", mockTrader.openLongCalls)
	}
	if actionRecord.RiskOutcome != string(risk.OutcomeReject) {
		t.Fatalf("expected reject risk outcome, got %q", actionRecord.RiskOutcome)
	}
	if !strings.Contains(actionRecord.RiskSummary, "sector") {
		t.Fatalf("expected sector risk summary, got %q", actionRecord.RiskSummary)
	}
}

func TestExecuteDecisionWithRecord_KillSwitchBlocksExecutionSubmission(t *testing.T) {
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
		name:                  "risk-test",
		exchange:              "alpaca",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
		executionManager:      execution.NewManager(execution.Config{}),
		killSwitchState: killSwitchSummary{
			Available: true,
			Active:    true,
			Message:   "operator kill switch active",
		},
	}
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
	}, actionRecord)
	if err == nil {
		t.Fatalf("expected execution manager to block while kill switch is active")
	}
	if mockTrader.openLongCalls != 0 {
		t.Fatalf("expected no broker submission, got %d", mockTrader.openLongCalls)
	}
	if actionRecord.OrderStatus != string(execution.StatusBlocked) {
		t.Fatalf("expected blocked execution status, got %q", actionRecord.OrderStatus)
	}
}

func TestExecuteDecisionWithRecord_DuplicateIntentSuppressedByExecutionManager(t *testing.T) {
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
		name:                  "risk-test",
		exchange:              "alpaca",
		config:                cfg,
		trader:                mockTrader,
		provider:              &riskTestProvider{price: 100, currentVolume: 20000, averageVolume: 50000},
		initialBalance:        100000,
		isRunning:             true,
		dailyStartEquity:      100000,
		positionFirstSeenTime: map[string]int64{},
		riskEngine:            risk.NewEngine(buildRiskConfig(cfg)),
		executionManager: execution.NewManager(execution.Config{
			DedupeWindow: time.Minute,
			StaleAfter:   time.Minute,
		}),
	}
	at.setReadinessSummary(ReadinessSummary{Status: ReadinessPass, Message: "startup readiness passed", CheckedAt: time.Now(), TradingAllowed: true})
	at.initializeBrokerRuntimeState()

	first := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	if err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        95,
		TakeProfit:      110,
	}, first); err != nil {
		t.Fatalf("expected first execution to submit cleanly, got %v", err)
	}

	second := &logger.DecisionAction{Action: "open_long", Symbol: "AAPL"}
	err := at.executeDecisionWithRecord(&decision.Decision{
		Symbol:          "AAPL",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 10000,
		StopLoss:        95,
		TakeProfit:      110,
	}, second)
	if err == nil {
		t.Fatalf("expected duplicate suppression on second execution")
	}
	if mockTrader.openLongCalls != 1 {
		t.Fatalf("expected one broker submission, got %d", mockTrader.openLongCalls)
	}
	if second.OrderStatus != string(execution.StatusDuplicateSuppressed) {
		t.Fatalf("expected duplicate_suppressed status, got %q", second.OrderStatus)
	}
}
