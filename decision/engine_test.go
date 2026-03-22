package decision

import (
	dataquality "northstar/data"
	"northstar/market"
	"testing"
	"time"
)

type marketDataReuseProvider struct {
	series map[string]map[string][]market.Kline
	calls  []string
}

func (p *marketDataReuseProvider) GetBars(symbols []string, interval string, limit int) (map[string][]market.Kline, error) {
	out := make(map[string][]market.Kline, len(symbols))
	for _, symbol := range symbols {
		p.calls = append(p.calls, symbol+":"+interval)
		if intervals, ok := p.series[symbol]; ok {
			if bars, ok := intervals[interval]; ok {
				if len(bars) > limit {
					out[symbol] = append([]market.Kline(nil), bars[len(bars)-limit:]...)
				} else {
					out[symbol] = append([]market.Kline(nil), bars...)
				}
			}
		}
	}
	return out, nil
}

func TestFetchMarketDataForContextPreservesPreloadedEquityData(t *testing.T) {
	now := time.Date(2026, 3, 16, 15, 30, 0, 0, time.UTC)
	provider := &marketDataReuseProvider{
		series: map[string]map[string][]market.Kline{
			"MSFT": {
				"3m": buildDecisionTestKlines(now.Add(-117*time.Minute), 3*time.Minute, 40, 100, 8_000),
				"4h": buildDecisionTestKlines(now.Add(-236*time.Hour), 4*time.Hour, 60, 95, 25_000),
			},
		},
	}
	preloaded := &market.Data{Symbol: "AAPL", CurrentPrice: 123.45}
	ctx := &Context{
		CandidateCoins: []CandidateCoin{
			{Symbol: "AAPL"},
			{Symbol: "MSFT"},
		},
		MarketDataMap: map[string]*market.Data{
			"AAPL": preloaded,
		},
		Provider:       provider,
		InstrumentType: "equity",
		DataValidationOptions: dataquality.Options{
			Now: now,
		},
	}

	if err := fetchMarketDataForContext(ctx); err != nil {
		t.Fatalf("fetchMarketDataForContext failed: %v", err)
	}
	if got := ctx.MarketDataMap["AAPL"]; got != preloaded {
		t.Fatalf("expected preloaded AAPL market data to be preserved")
	}
	if got := ctx.MarketDataMap["MSFT"]; got == nil {
		t.Fatalf("expected missing symbol MSFT to be fetched")
	}
	for _, call := range provider.calls {
		if call == "AAPL:3m" || call == "AAPL:4h" {
			t.Fatalf("expected preloaded AAPL data to be reused without refetch, saw call %q", call)
		}
	}
}

func buildDecisionTestKlines(start time.Time, step time.Duration, count int, startPrice, startVolume float64) []market.Kline {
	out := make([]market.Kline, 0, count)
	price := startPrice
	for i := 0; i < count; i++ {
		drift := 0.0015 + float64(i%4)*0.0005
		open := price
		closePrice := price * (1 + drift)
		high := closePrice * 1.003
		low := open * 0.997
		openTime := start.Add(time.Duration(i) * step)
		closeTime := openTime.Add(step)
		out = append(out, market.Kline{
			OpenTime:  openTime.UnixMilli(),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    startVolume + float64(i*120),
			CloseTime: closeTime.UnixMilli(),
		})
		price = closePrice
	}
	return out
}
