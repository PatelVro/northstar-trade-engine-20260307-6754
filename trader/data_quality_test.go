package trader

import (
	"fmt"
	"northstar/market"
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
	now := time.Now().UTC()
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
	if _, err := at.getValidatedMarketData("AAPL"); err == nil {
		t.Fatalf("expected data quality validation error")
	}
	status := at.GetOperatorStatus()
	if status.DataQualityBlockedSymbols != 1 {
		t.Fatalf("expected one blocked symbol, got %d", status.DataQualityBlockedSymbols)
	}

	provider.series["AAPL"]["3m"] = buildMarketBars(now.Add(-117*time.Minute), 3*time.Minute, 40, 100, 1000)
	if _, err := at.getValidatedMarketData("AAPL"); err != nil {
		t.Fatalf("expected data quality to recover, got %v", err)
	}
	status = at.GetOperatorStatus()
	if status.DataQualityBlockedSymbols != 0 {
		t.Fatalf("expected blocked symbol count to clear, got %d", status.DataQualityBlockedSymbols)
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
