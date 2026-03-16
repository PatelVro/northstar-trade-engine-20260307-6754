package risk

import "testing"

func TestPositionLimitReducesSize(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	eval := engine.Evaluate(
		baseAccount(),
		nil,
		baseMarket(),
		baseEntryOrder(30000, 95),
	)

	if eval.Outcome != OutcomeReduceSize {
		t.Fatalf("expected reduce_size outcome, got %s", eval.Outcome)
	}
	if eval.ApprovedNotional != 20000 {
		t.Fatalf("expected approved notional 20000, got %.2f", eval.ApprovedNotional)
	}
	assertRuleStatus(t, eval, "max_position_size_per_symbol", RuleReduceSize)
}

func TestPortfolioExposureRejectsWhenGrossFull(t *testing.T) {
	account := baseAccount()
	account.GrossMarketValue = 100000

	engine := NewEngine(baseTestConfig())
	eval := engine.Evaluate(account, nil, baseMarket(), baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_portfolio_exposure", RuleReject)
}

func TestNetExposureReducesSize(t *testing.T) {
	cfg := baseTestConfig()
	cfg.MaxPositionPct = 0.50
	cfg.MaxPortfolioExposure = 2.0
	cfg.MaxSectorExposurePct = 0
	engine := NewEngine(cfg)
	positions := []PositionSnapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 1, MarketValue: 60000, Sector: "technology", SectorKnown: true},
	}
	order := baseEntryOrder(20000, 95)
	order.Symbol = "TSLA"
	market := baseMarket()
	market.Symbol = "TSLA"
	market.Sector = "consumer_discretionary"

	eval := engine.Evaluate(baseAccount(), positions, market, order)

	if eval.Outcome != OutcomeReduceSize {
		t.Fatalf("expected reduce_size outcome, got %s", eval.Outcome)
	}
	if eval.ApprovedNotional != 5000 {
		t.Fatalf("expected approved notional 5000, got %.2f", eval.ApprovedNotional)
	}
	assertRuleStatus(t, eval, "max_net_exposure", RuleReduceSize)
}

func TestConcurrentPositionsRejectsNewEntry(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	positions := []PositionSnapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 10, MarketValue: 1000, Sector: "technology", SectorKnown: true},
		{Symbol: "MSFT", Side: "long", Quantity: 10, MarketValue: 1000, Sector: "technology", SectorKnown: true},
		{Symbol: "NVDA", Side: "long", Quantity: 10, MarketValue: 1000, Sector: "technology", SectorKnown: true},
	}
	order := baseEntryOrder(10000, 95)
	order.Symbol = "TSLA"
	eval := engine.Evaluate(baseAccount(), positions, baseMarket(), order)

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_concurrent_positions", RuleReject)
}

func TestDailyLossRejectsEntry(t *testing.T) {
	account := baseAccount()
	account.DailyPnL = -6000

	engine := NewEngine(baseTestConfig())
	eval := engine.Evaluate(account, nil, baseMarket(), baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_daily_loss", RuleReject)
}

func TestDrawdownStopRejectsEntry(t *testing.T) {
	account := baseAccount()
	account.StrategyEquity = 85000
	account.PeakStrategyEquity = 100000

	engine := NewEngine(baseTestConfig())
	eval := engine.Evaluate(account, nil, baseMarket(), baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_drawdown_stop", RuleReject)
}

func TestPerTradeRiskRejectsWhenStopMissing(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	eval := engine.Evaluate(baseAccount(), nil, baseMarket(), baseEntryOrder(10000, 0))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_per_trade_risk", RuleReject)
}

func TestGrossLeverageReducesSize(t *testing.T) {
	cfg := baseTestConfig()
	cfg.MaxPositionPct = 0.50
	cfg.MaxPortfolioExposure = 2.0
	cfg.MaxPerTradeRiskPct = 0.05
	engine := NewEngine(cfg)
	account := baseAccount()
	account.AccountEquity = 50000
	account.GrossMarketValue = 40000

	eval := engine.Evaluate(account, nil, baseMarket(), baseEntryOrder(20000, 95))

	if eval.Outcome != OutcomeReduceSize {
		t.Fatalf("expected reduce_size outcome, got %s", eval.Outcome)
	}
	if eval.ApprovedNotional != 10000 {
		t.Fatalf("expected approved notional 10000, got %.2f", eval.ApprovedNotional)
	}
	assertRuleStatus(t, eval, "max_gross_leverage", RuleReduceSize)
}

func TestSectorExposureRejectsConcentratedEntry(t *testing.T) {
	cfg := baseTestConfig()
	cfg.MaxPositionPct = 0.50
	cfg.MaxPortfolioExposure = 2.0
	engine := NewEngine(cfg)
	positions := []PositionSnapshot{
		{Symbol: "AAPL", Side: "long", Quantity: 1, MarketValue: 35000, Sector: "technology", SectorKnown: true},
	}
	market := baseMarket()
	market.Sector = "technology"
	market.SectorKnown = true

	eval := engine.Evaluate(baseAccount(), positions, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_sector_exposure", RuleReject)
}

func TestAverageVolumeRejectsThinSymbol(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	market := baseMarket()
	market.AverageDollarVolume = 500000

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "min_average_volume", RuleReject)
}

func TestParticipationRateReducesSize(t *testing.T) {
	cfg := baseTestConfig()
	cfg.MaxPositionPct = 0.50
	engine := NewEngine(cfg)

	market := baseMarket()
	market.CurrentVolume = 1000
	market.AverageVolume = 1000

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(15000, 95))

	if eval.Outcome != OutcomeReduceSize {
		t.Fatalf("expected reduce_size outcome, got %s", eval.Outcome)
	}
	if eval.ApprovedQuantity != 100 {
		t.Fatalf("expected approved quantity 100, got %.2f", eval.ApprovedQuantity)
	}
	assertRuleStatus(t, eval, "max_participation_rate", RuleReduceSize)
}

func TestCorrelatedPositionsRejectEntry(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	market := baseMarket()
	market.CorrelationKnown = true
	market.MaxObservedCorrelation = 0.91
	market.MaxObservedCorrelationSymbol = "MSFT"
	market.CorrelatedPositionCount = 1
	market.CorrelatedSymbols = []string{"MSFT"}

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "max_correlated_positions", RuleReject)
}

func TestSymbolNotTradableRejects(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	market := baseMarket()
	market.Tradable = false
	market.TradableReason = "symbol not tradable"

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "symbol_tradable", RuleReject)
}

func TestTradingHaltedRejects(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	market := baseMarket()
	market.Halted = true
	market.HaltReason = "halted"

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "trading_halted", RuleReject)
}

func TestPriceSanityRejectsInvalidPrice(t *testing.T) {
	engine := NewEngine(baseTestConfig())
	market := baseMarket()
	market.CurrentPrice = 0

	eval := engine.Evaluate(baseAccount(), nil, market, baseEntryOrder(10000, 95))

	if eval.Outcome != OutcomeReject {
		t.Fatalf("expected reject outcome, got %s", eval.Outcome)
	}
	assertRuleStatus(t, eval, "price_sanity", RuleReject)
}

func baseTestConfig() Config {
	return Config{
		MaxPositionPct:         0.20,
		MaxPortfolioExposure:   1.0,
		MaxNetExposurePct:      0.65,
		MaxSectorExposurePct:   0.35,
		MaxConcurrentPositions: 3,
		MaxCorrelatedPositions: 1,
		MaxDailyLossPct:        0.05,
		MaxDrawdownStopPct:     0.10,
		MaxPerTradeRiskPct:     0.01,
		MaxPairCorrelation:     0.82,
		MaxGrossLeverage:       1.0,
		MinAverageDollarVolume: 1_000_000,
		MaxParticipationRate:   0.10,
		CashBufferPct:          0.95,
	}
}

func baseAccount() AccountSnapshot {
	return AccountSnapshot{
		StrategyInitialCapital: 100000,
		StrategyEquity:         100000,
		AccountEquity:          100000,
		AvailableBalance:       100000,
		GrossMarketValue:       0,
		DailyPnL:               0,
		DailyLossLimit:         -5000,
		PeakStrategyEquity:     100000,
		PositionCount:          0,
	}
}

func baseMarket() MarketSnapshot {
	return MarketSnapshot{
		Symbol:              "AAPL",
		CurrentPrice:        100,
		AverageVolume:       50000,
		CurrentVolume:       10000,
		AverageDollarVolume: 5_000_000,
		Tradable:            true,
		TradableKnown:       true,
		Halted:              false,
		HaltedKnown:         true,
		Sector:              "technology",
		SectorKnown:         true,
	}
}

func baseEntryOrder(notional, stopLoss float64) OrderRequest {
	return OrderRequest{
		Symbol:            "AAPL",
		Action:            "open_long",
		Side:              "long",
		RequestedNotional: notional,
		RequestedQuantity: notional / 100,
		StopLoss:          stopLoss,
		IsEntry:           true,
	}
}

func assertRuleStatus(t *testing.T, eval Evaluation, name string, want RuleStatus) {
	t.Helper()
	for _, result := range eval.RuleResults {
		if result.Name == name {
			if result.Status != want {
				t.Fatalf("expected rule %s to be %s, got %s", name, want, result.Status)
			}
			return
		}
	}
	t.Fatalf("rule %s not found in evaluation", name)
}
