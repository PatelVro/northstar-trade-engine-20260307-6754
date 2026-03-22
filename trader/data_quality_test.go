package trader

import (
	"fmt"
	"northstar/decision"
	"northstar/incidents"
	"northstar/market"
	"strings"
	"testing"
	"time"
)

type dataQualityTestProvider struct {
	series map[string]map[string][]market.Kline
}

func (p *dataQualityTestProvider) GetBars(symbols []string, interval string, limit int) (map[string][]market.Kline, error) {
	result := make(map[string][]market.Kline, len(symbols))
	for _, symbol := range symbols {
		intervals, ok := p.series[symbol]
		if !ok {
			return nil, fmt.Errorf("missing symbol %s", symbol)
		}
		bars, ok := intervals[interval]
		if !ok {
			return nil, fmt.Errorf("missing interval %s for %s", interval, symbol)
		}
		result[symbol] = append([]market.Kline(nil), bars...)
	}
	return result, nil
}

func TestGetValidatedMarketDataBlocksAndClearsSymbol(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load market timezone: %v", err)
	}
	now := time.Date(2026, 3, 20, 10, 30, 0, 0, loc).UTC()
	provider := &dataQualityTestProvider{
		series: map[string]map[string][]market.Kline{
			"AAPL": {
				"3m": buildMarketBars(now.Add(-117*time.Minute), 3*time.Minute, 40, 100, 1000),
				"4h": buildMarketBars(now.Add(-236*time.Hour), 4*time.Hour, 60, 100, 1000),
			},
		},
	}

	at := &AutoTrader{
		name:     "data-test",
		id:       "data_test",
		provider: provider,
		config:   AutoTraderConfig{Mode: "paper", Broker: "ibkr", InstrumentType: "equity"},
	}
	at.initializeDataQualityState()

	provider.series["AAPL"]["3m"][len(provider.series["AAPL"]["3m"])-1].Volume = 0
	opts := at.currentDataValidationOptions()
	opts.Now = now
	req := market.GetRequest{
		Symbol:            "AAPL",
		Provider:          at.provider,
		InstrumentType:    at.config.InstrumentType,
		BarsAdjustment:    at.config.BarsAdjustment,
		ValidationOptions: opts,
	}
	if _, err := market.Get(req); err == nil {
		t.Fatalf("expected data quality validation error")
	} else {
		at.observeDataQualityEvent("AAPL", nil, err)
	}
	status := at.GetOperatorStatus()
	if status.DataQualityBlockedSymbols != 1 {
		t.Fatalf("expected one blocked symbol, got %d", status.DataQualityBlockedSymbols)
	}

	provider.series["AAPL"]["3m"] = buildMarketBars(now.Add(-117*time.Minute), 3*time.Minute, 40, 100, 1000)
	if _, err := market.Get(req); err != nil {
		t.Fatalf("expected data quality to recover, got %v", err)
	} else {
		at.observeDataQualityEvent("AAPL", nil, nil)
	}
	status = at.GetOperatorStatus()
	if status.DataQualityBlockedSymbols != 0 {
		t.Fatalf("expected blocked symbol count to clear, got %d", status.DataQualityBlockedSymbols)
	}
}

func TestGetValidatedMarketDataTreatsClosedEquitySessionAsNonIncident(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load market timezone: %v", err)
	}
	now := time.Date(2026, 3, 20, 8, 0, 0, 0, loc).UTC()

	provider := &dataQualityTestProvider{
		series: map[string]map[string][]market.Kline{
			"AAPL": {
				"3m": buildMarketBars(time.Date(2026, 3, 19, 13, 0, 0, 0, loc).UTC(), 3*time.Minute, 40, 100, 1000),
				"4h": buildMarketBars(now.Add(-236*time.Hour), 4*time.Hour, 60, 100, 1000),
			},
		},
	}

	at := &AutoTrader{
		name:     "data-test",
		id:       "data_test",
		provider: provider,
		config:   AutoTraderConfig{Mode: "shadow", Broker: "sim", InstrumentType: "equity"},
	}
	at.initializeDataQualityState()

	opts := at.currentDataValidationOptions()
	opts.Now = now
	req := market.GetRequest{
		Symbol:            "AAPL",
		Provider:          at.provider,
		InstrumentType:    at.config.InstrumentType,
		BarsAdjustment:    at.config.BarsAdjustment,
		ValidationOptions: opts,
	}
	_, err = market.Get(req)
	if err == nil {
		t.Fatalf("expected market-closed validation error")
	}
	at.observeDataQualityEvent("AAPL", nil, err)

	status := at.GetOperatorStatus()
	if status.DataQualityBlockedSymbols != 0 {
		t.Fatalf("expected market-closed condition not to block symbol state, got %d", status.DataQualityBlockedSymbols)
	}
	if status.DataQuality.TotalFailures != 0 {
		t.Fatalf("expected market-closed condition not to increment failures, got %d", status.DataQuality.TotalFailures)
	}
}

func TestLoadMomentumMarketDataDetectsGlobalFeedDelay(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load market timezone: %v", err)
	}
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, loc).UTC()
	stale3m := buildMarketBars(now.Add(-140*time.Minute), 3*time.Minute, 40, 100, 1000)
	provider := &dataQualityTestProvider{
		series: map[string]map[string][]market.Kline{
			"AAPL": {"3m": stale3m},
			"MSFT": {"3m": stale3m},
			"NVDA": {"3m": stale3m},
			"SPY":  {"3m": stale3m},
			"QQQ":  {"3m": stale3m},
		},
	}

	at := &AutoTrader{
		name:     "data-test",
		id:       "data_test",
		provider: provider,
		timeNow:  func() time.Time { return now },
		config: AutoTraderConfig{
			Mode:               "shadow",
			Broker:             "sim",
			DataProvider:       "ibkr",
			InstrumentType:     "equity",
			CandidateBatchSize: 10,
		},
	}
	at.initializeDataQualityState()
	at.incidentManager = incidents.NewManager(at.id)

	ctx := &decision.Context{
		CandidateCoins: []decision.CandidateCoin{
			{Symbol: "AAPL"},
			{Symbol: "MSFT"},
			{Symbol: "NVDA"},
		},
	}

	err = at.preflightRuntimeMarketData(ctx)
	if err == nil {
		t.Fatalf("expected delayed market-data probe error")
	}
	if got := err.Error(); !strings.Contains(got, "market-data feed delayed") {
		t.Fatalf("expected delayed-feed error, got %q", got)
	}

	status := at.GetOperatorStatus()
	if !status.DataQuality.FeedDelayed {
		t.Fatalf("expected feed_delayed status to be set")
	}
	if status.DataQualityBlockedSymbols != 0 {
		t.Fatalf("expected global feed delay not to create per-symbol blocks, got %d", status.DataQualityBlockedSymbols)
	}
	if !status.DataQualityFeedDelayed {
		t.Fatalf("expected flattened feed-delayed flag to be set")
	}
	if status.Incidents.OpenCount == 0 {
		t.Fatalf("expected feed-delay incident to be opened")
	}
}

func TestSyncMarketDataAvailabilityIncidentTreatsMarketClosedAsInfo(t *testing.T) {
	at := &AutoTrader{
		id:              "data_test",
		name:            "Data Test",
		incidentManager: incidents.NewManager("data_test"),
	}

	at.syncMarketDataAvailabilityIncident(true, "market is closed for equity session", nil)

	summary := at.currentIncidentSummary()
	if summary.OpenCount != 1 {
		t.Fatalf("expected one open incident, got %d", summary.OpenCount)
	}
	if summary.InfoOpenCount != 1 || summary.CriticalOpenCount != 0 {
		t.Fatalf("expected market-closed incident to be info-only, got %+v", summary)
	}
}

func buildMarketBars(start time.Time, step time.Duration, count int, startPrice float64, volume float64) []market.Kline {
	out := make([]market.Kline, 0, count)
	price := startPrice
	for i := 0; i < count; i++ {
		openTime := start.Add(time.Duration(i) * step)
		closeTime := openTime.Add(step)
		out = append(out, market.Kline{
			OpenTime:  openTime.UnixMilli(),
			Open:      price,
			High:      price * 1.01,
			Low:       price * 0.99,
			Close:     price * 1.001,
			Volume:    volume,
			CloseTime: closeTime.UnixMilli(),
		})
		price = price * 1.001
	}
	return out
}
