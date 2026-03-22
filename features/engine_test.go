package features

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestFeatureEngineDeterministic(t *testing.T) {
	engine := DefaultEngine()
	bars := buildFeatureBars(40, 100, 1_000)

	first := engine.Compute("AAPL", "1h", bars)
	second := engine.Compute("AAPL", "1h", bars)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic feature vector, got different results")
	}
}

func TestFeatureEngineInsufficientHistoryIsExplicit(t *testing.T) {
	engine := DefaultEngine()
	vector := engine.Compute("AAPL", "1h", buildFeatureBars(8, 100, 1_000))

	if !vector.InsufficientHistory {
		t.Fatalf("expected insufficient history")
	}
	if vector.Valid {
		t.Fatalf("expected vector to be invalid when history is insufficient")
	}
	if len(vector.MissingInputs) == 0 {
		t.Fatalf("expected missing inputs to be reported")
	}
}

func TestFeatureEngineAvoidsNaNWithZeroVolume(t *testing.T) {
	engine := DefaultEngine()
	bars := buildFeatureBars(40, 100, 1_000)
	bars[len(bars)-1].Volume = 0
	vector := engine.Compute("MSFT", "1h", bars)

	if !vector.ZeroVolume {
		t.Fatalf("expected zero volume flag")
	}
	assertFiniteVector(t, vector)
}

func TestFeatureEngineNoFutureLeakage(t *testing.T) {
	engine := DefaultEngine()
	bars := buildFeatureBars(40, 100, 1_000)
	baseline := engine.Compute("NVDA", "1h", bars[:30])

	mutated := append([]Bar(nil), bars...)
	for i := 30; i < len(mutated); i++ {
		mutated[i].Close = mutated[i].Close * 10
		mutated[i].High = mutated[i].Close * 1.01
		mutated[i].Low = mutated[i].Close * 0.99
	}
	recomputed := engine.Compute("NVDA", "1h", mutated[:30])

	if !reflect.DeepEqual(baseline, recomputed) {
		t.Fatalf("expected future bars beyond the slice not to affect output")
	}
}

func TestFeatureEngineRepresentativeSnapshot(t *testing.T) {
	engine := DefaultEngine()
	bars := buildFeatureBars(60, 100, 1_250)
	vector := engine.Compute("SPY", "4h", bars)

	if vector.Symbol != "SPY" {
		t.Fatalf("expected symbol SPY, got %s", vector.Symbol)
	}
	if vector.Timeframe != "4h" {
		t.Fatalf("expected timeframe 4h, got %s", vector.Timeframe)
	}
	if vector.BarCount != 60 {
		t.Fatalf("expected bar count 60, got %d", vector.BarCount)
	}
	if vector.Timestamp.IsZero() {
		t.Fatalf("expected timestamp")
	}
	assertFiniteVector(t, vector)
}

func buildFeatureBars(count int, startPrice, startVolume float64) []Bar {
	start := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	out := make([]Bar, 0, count)
	price := startPrice
	for i := 0; i < count; i++ {
		drift := 0.002 + float64(i%5)*0.0006
		open := price
		closePrice := price * (1 + drift)
		high := closePrice * 1.004
		low := open * 0.996
		volume := startVolume + float64(i*50)
		openTime := start.Add(time.Duration(i) * time.Hour)
		closeTime := openTime.Add(time.Hour)
		out = append(out, Bar{
			OpenTime:  openTime.UnixMilli(),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: closeTime.UnixMilli(),
		})
		price = closePrice
	}
	return out
}

func assertFiniteVector(t *testing.T, vector *FeatureVector) {
	t.Helper()
	values := []float64{
		vector.Return1Bar,
		vector.Return5Bar,
		vector.Return10Bar,
		vector.Return20Bar,
		vector.EMA5Distance,
		vector.EMA20Distance,
		vector.EMA5Vs20Spread,
		vector.MomentumRankProxy20,
		vector.RealizedVol10,
		vector.RealizedVol20,
		vector.ATR14,
		vector.ATR14Pct,
		vector.TrueRangeAvg14,
		vector.VolatilityRatio10v20,
		vector.DistanceFromMean20,
		vector.PriceZScore20,
		vector.ReturnZScore20,
		vector.RSI14,
		vector.AverageVolume20,
		vector.VolumeSpikeRatio20,
		vector.DollarVolume20,
		vector.IntrabarRangePct,
		vector.CloseLocationInBar01,
		vector.GapVsPrevClose,
		vector.HighLowExpansionRatio5,
		vector.UpBarHitRate20,
		vector.TrendConsistency20,
		vector.TrendStrength20,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("unexpected non-finite feature value: %v", value)
		}
	}
}
