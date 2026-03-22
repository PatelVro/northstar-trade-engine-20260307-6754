package market

import (
	"testing"
	"time"
)

type featureTestProvider struct {
	series map[string]map[string][]Kline
}

func (p *featureTestProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	out := make(map[string][]Kline, len(symbols))
	for _, symbol := range symbols {
		if intervals, ok := p.series[symbol]; ok {
			if bars, ok := intervals[interval]; ok {
				if len(bars) > limit {
					out[symbol] = append([]Kline(nil), bars[len(bars)-limit:]...)
				} else {
					out[symbol] = append([]Kline(nil), bars...)
				}
			}
		}
	}
	return out, nil
}

func TestGetPopulatesCanonicalFeatures(t *testing.T) {
	provider := &featureTestProvider{
		series: map[string]map[string][]Kline{
			"AAPL": {
				"3m": buildFeatureTestKlines(time.Date(2026, 3, 10, 14, 30, 0, 0, time.UTC), 3*time.Minute, 40, 100, 8_000),
				"4h": buildFeatureTestKlines(time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC), 4*time.Hour, 60, 95, 25_000),
			},
		},
	}

	data, err := Get(GetRequest{
		Symbol:         "AAPL",
		Provider:       provider,
		InstrumentType: "equity",
	})
	if err != nil {
		t.Fatalf("market.Get failed: %v", err)
	}
	if data.Features == nil {
		t.Fatalf("expected canonical feature set")
	}
	if data.Regimes == nil {
		t.Fatalf("expected canonical regime set")
	}
	if data.Selections == nil {
		t.Fatalf("expected canonical strategy selection set")
	}
	intraday := data.Features.Vector("3m")
	if intraday == nil {
		t.Fatalf("expected 3m feature vector")
	}
	if !intraday.Valid {
		t.Fatalf("expected valid intraday feature vector, warnings=%v missing=%v", intraday.ValidationWarnings, intraday.MissingInputs)
	}
	longer := data.Features.Vector("4h")
	if longer == nil {
		t.Fatalf("expected 4h feature vector")
	}
	if !longer.Valid {
		t.Fatalf("expected valid 4h feature vector, warnings=%v missing=%v", longer.ValidationWarnings, longer.MissingInputs)
	}
	if longer.Return20Bar == 0 {
		t.Fatalf("expected non-zero 4h long-window return")
	}
	intradayRegime := data.Regimes.Result("3m")
	if intradayRegime == nil {
		t.Fatalf("expected 3m regime result")
	}
	longerRegime := data.Regimes.Result("4h")
	if longerRegime == nil {
		t.Fatalf("expected 4h regime result")
	}
	if !intradayRegime.Valid || !longerRegime.Valid {
		t.Fatalf("expected valid regime outputs, got intraday=%v longer=%v", intradayRegime.Valid, longerRegime.Valid)
	}
	intradaySelection := data.Selections.Selection("3m")
	if intradaySelection == nil {
		t.Fatalf("expected 3m selection")
	}
	longerSelection := data.Selections.Selection("4h")
	if longerSelection == nil {
		t.Fatalf("expected 4h selection")
	}
	if !longerSelection.Valid {
		t.Fatalf("expected valid 4h strategy selection")
	}
}

func buildFeatureTestKlines(start time.Time, step time.Duration, count int, startPrice, startVolume float64) []Kline {
	out := make([]Kline, 0, count)
	price := startPrice
	for i := 0; i < count; i++ {
		drift := 0.0015 + float64(i%4)*0.0005
		open := price
		closePrice := price * (1 + drift)
		high := closePrice * 1.003
		low := open * 0.997
		openTime := start.Add(time.Duration(i) * step)
		closeTime := openTime.Add(step)
		out = append(out, Kline{
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
