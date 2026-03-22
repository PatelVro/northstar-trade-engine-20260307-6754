package regime

import (
	"reflect"
	"testing"
	"time"

	"northstar/features"
)

func TestTrendRegimeClassification(t *testing.T) {
	result := DefaultDetector().Detect(&features.FeatureVector{
		Symbol:               "AAPL",
		Timestamp:            time.Now().UTC(),
		Timeframe:            "4h",
		Valid:                true,
		Return20Bar:          0.09,
		EMA5Vs20Spread:       0.05,
		TrendConsistency20:   0.86,
		TrendStrength20:      4.8,
		RealizedVol20:        0.012,
		ATR14Pct:             0.014,
		VolatilityRatio10v20: 1.05,
		PriceZScore20:        1.0,
		ReturnZScore20:       0.8,
		RSI14:                64,
	})
	if result.Regime != RegimeTrend {
		t.Fatalf("expected trend regime, got %s", result.Regime)
	}
	if result.TrendScore <= result.MeanReversionScore {
		t.Fatalf("expected trend score to dominate")
	}
}

func TestMeanReversionRegimeClassification(t *testing.T) {
	result := DefaultDetector().Detect(&features.FeatureVector{
		Symbol:               "MSFT",
		Timestamp:            time.Now().UTC(),
		Timeframe:            "4h",
		Valid:                true,
		Return20Bar:          0.01,
		EMA5Vs20Spread:       0.004,
		TrendConsistency20:   0.22,
		TrendStrength20:      0.4,
		RealizedVol20:        0.009,
		ATR14Pct:             0.010,
		VolatilityRatio10v20: 0.96,
		PriceZScore20:        2.5,
		ReturnZScore20:       2.1,
		RSI14:                76,
	})
	if result.Regime != RegimeMeanReversion {
		t.Fatalf("expected mean_reversion regime, got %s", result.Regime)
	}
}

func TestHighVolatilityRegimeClassification(t *testing.T) {
	result := DefaultDetector().Detect(&features.FeatureVector{
		Symbol:               "NVDA",
		Timestamp:            time.Now().UTC(),
		Timeframe:            "4h",
		Valid:                true,
		Return20Bar:          0.02,
		EMA5Vs20Spread:       0.01,
		TrendConsistency20:   0.35,
		TrendStrength20:      0.7,
		RealizedVol20:        0.04,
		ATR14Pct:             0.042,
		VolatilityRatio10v20: 1.6,
		VolatilityExpansion:  true,
		PriceZScore20:        0.6,
		ReturnZScore20:       0.4,
		RSI14:                55,
	})
	if result.Regime != RegimeHighVolatility {
		t.Fatalf("expected high_volatility regime, got %s", result.Regime)
	}
}

func TestLowVolatilityRegimeClassification(t *testing.T) {
	result := DefaultDetector().Detect(&features.FeatureVector{
		Symbol:               "SPY",
		Timestamp:            time.Now().UTC(),
		Timeframe:            "4h",
		Valid:                true,
		Return20Bar:          0.015,
		EMA5Vs20Spread:       0.007,
		TrendConsistency20:   0.48,
		TrendStrength20:      0.9,
		RealizedVol20:        0.004,
		ATR14Pct:             0.006,
		VolatilityRatio10v20: 0.94,
		PriceZScore20:        0.5,
		ReturnZScore20:       0.4,
		RSI14:                53,
	})
	if result.Regime != RegimeLowVolatility {
		t.Fatalf("expected low_volatility regime, got %s", result.Regime)
	}
}

func TestInvalidFeatureVectorReturnsUnknown(t *testing.T) {
	result := DefaultDetector().Detect(&features.FeatureVector{
		Symbol:              "QQQ",
		Timeframe:           "4h",
		Valid:               false,
		InsufficientHistory: true,
		MissingInputs:       []string{"return_20_bar"},
	})
	if result.Regime != RegimeUnknown {
		t.Fatalf("expected unknown regime, got %s", result.Regime)
	}
	if result.Valid {
		t.Fatalf("expected invalid regime result")
	}
}

func TestDetectorDeterministic(t *testing.T) {
	vector := &features.FeatureVector{
		Symbol:               "IWM",
		Timestamp:            time.Date(2026, 3, 16, 14, 0, 0, 0, time.UTC),
		Timeframe:            "4h",
		Valid:                true,
		Return20Bar:          0.05,
		EMA5Vs20Spread:       0.03,
		TrendConsistency20:   0.75,
		TrendStrength20:      2.2,
		RealizedVol20:        0.015,
		ATR14Pct:             0.018,
		VolatilityRatio10v20: 1.12,
		PriceZScore20:        0.9,
		ReturnZScore20:       0.7,
		RSI14:                61,
	}
	first := DefaultDetector().Detect(vector)
	second := DefaultDetector().Detect(vector)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic regime output")
	}
}
