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

func TestNormalizeDecisionActions(t *testing.T) {
	positions := []PositionInfo{
		{Symbol: "LMT", Side: "long"},
		{Symbol: "VZ", Side: "short"},
	}

	tests := []struct {
		name     string
		action   string
		symbol   string
		expected string
	}{
		{"close long position", "close", "LMT", "close_long"},
		{"close short position", "close", "VZ", "close_short"},
		{"sell maps to close_long", "sell", "LMT", "close_long"},
		{"exit maps to close_short", "exit", "VZ", "close_short"},
		{"buy maps to open_long", "buy", "AAPL", "open_long"},
		{"long maps to open_long", "long", "MSFT", "open_long"},
		{"short maps to open_short", "short", "TSLA", "open_short"},
		{"open maps to open_long", "open", "GOOGL", "open_long"},
		{"close unknown defaults to close_long", "close", "UNKNOWN", "close_long"},
		{"valid action unchanged", "open_long", "AAPL", "open_long"},
		{"hold unchanged", "hold", "LMT", "hold"},
		{"wait unchanged", "wait", "AAPL", "wait"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decisions := []Decision{{Symbol: tt.symbol, Action: tt.action}}
			normalizeDecisionActions(decisions, positions)
			if decisions[0].Action != tt.expected {
				t.Errorf("got %q, want %q", decisions[0].Action, tt.expected)
			}
		})
	}
}

func TestParseFullDecisionSkipsInvalidDecisions(t *testing.T) {
	// AI response with 3 decisions: 2 valid (hold/wait), 1 invalid action
	aiResponse := `Analysis complete.
[
  {"symbol": "LMT", "action": "hold", "confidence": 80, "reasoning": "stable"},
  {"symbol": "AAPL", "action": "bogus_action", "confidence": 90, "reasoning": "test"},
  {"symbol": "VZ", "action": "wait", "confidence": 70, "reasoning": "waiting"}
]`

	result, err := parseFullDecisionResponseWithPositions(aiResponse, 100000, 1, 1, "equity", false, nil)
	if err != nil {
		t.Fatalf("should not error when some decisions are valid, got: %v", err)
	}
	if len(result.Decisions) != 2 {
		t.Fatalf("expected 2 valid decisions, got %d", len(result.Decisions))
	}
	if result.Decisions[0].Symbol != "LMT" || result.Decisions[1].Symbol != "VZ" {
		t.Errorf("unexpected decisions: %+v", result.Decisions)
	}
}

func TestParseFullDecisionAllInvalidReturnsError(t *testing.T) {
	aiResponse := `Analysis.
[
  {"symbol": "AAPL", "action": "yolo", "confidence": 90, "reasoning": "test"}
]`

	_, err := parseFullDecisionResponseWithPositions(aiResponse, 100000, 1, 1, "equity", false, nil)
	if err == nil {
		t.Fatal("expected error when all decisions are invalid")
	}
}

func TestParseFullDecisionNormalizesBeforeValidation(t *testing.T) {
	// GPT-4o says "close" for a long position — should be normalized to "close_long" and pass
	aiResponse := `Closing losing position.
[
  {"symbol": "LMT", "action": "close", "confidence": 85, "reasoning": "bearish"}
]`

	positions := []PositionInfo{{Symbol: "LMT", Side: "long"}}
	result, err := parseFullDecisionResponseWithPositions(aiResponse, 100000, 1, 1, "equity", false, positions)
	if err != nil {
		t.Fatalf("normalized close should not error: %v", err)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(result.Decisions))
	}
	if result.Decisions[0].Action != "close_long" {
		t.Errorf("expected close_long, got %q", result.Decisions[0].Action)
	}
}
