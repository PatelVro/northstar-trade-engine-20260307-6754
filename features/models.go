package features

import "time"

const SchemaVersion = 1

type Bar struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

type Config struct {
	ShortReturnWindow     int
	MediumReturnWindow    int
	LongReturnWindow      int
	VolumeWindow          int
	VolatilityShortWindow int
	VolatilityLongWindow  int
	ATRWindow             int
	RSIWindow             int
	PriceRankWindow       int
	ExpansionWindow       int
	TrendWindow           int
	EMAFastWindow         int
	EMASlowWindow         int
}

func DefaultConfig() Config {
	return Config{
		ShortReturnWindow:     5,
		MediumReturnWindow:    10,
		LongReturnWindow:      20,
		VolumeWindow:          20,
		VolatilityShortWindow: 10,
		VolatilityLongWindow:  20,
		ATRWindow:             14,
		RSIWindow:             14,
		PriceRankWindow:       20,
		ExpansionWindow:       5,
		TrendWindow:           20,
		EMAFastWindow:         5,
		EMASlowWindow:         20,
	}
}

type FeatureVector struct {
	SchemaVersion       int       `json:"schema_version"`
	Symbol              string    `json:"symbol"`
	Timestamp           time.Time `json:"timestamp"`
	Timeframe           string    `json:"timeframe"`
	BarCount            int       `json:"bar_count"`
	RequiredHistory     int       `json:"required_history"`
	Valid               bool      `json:"valid"`
	InsufficientHistory bool      `json:"insufficient_history"`
	MissingInputs       []string  `json:"missing_inputs,omitempty"`
	ValidationWarnings  []string  `json:"validation_warnings,omitempty"`

	Return1Bar          float64 `json:"return_1_bar"`
	Return5Bar          float64 `json:"return_5_bar"`
	Return10Bar         float64 `json:"return_10_bar"`
	Return20Bar         float64 `json:"return_20_bar"`
	EMA5Distance        float64 `json:"ema_5_distance"`
	EMA20Distance       float64 `json:"ema_20_distance"`
	EMA5Vs20Spread      float64 `json:"ema_5_vs_20_spread"`
	MomentumRankProxy20 float64 `json:"momentum_rank_proxy_20"`

	RealizedVol10        float64 `json:"realized_vol_10"`
	RealizedVol20        float64 `json:"realized_vol_20"`
	ATR14                float64 `json:"atr_14"`
	ATR14Pct             float64 `json:"atr_14_pct"`
	TrueRangeAvg14       float64 `json:"true_range_avg_14"`
	VolatilityRatio10v20 float64 `json:"volatility_ratio_10v20"`

	DistanceFromMean20 float64 `json:"distance_from_mean_20"`
	PriceZScore20      float64 `json:"price_zscore_20"`
	ReturnZScore20     float64 `json:"return_zscore_20"`
	RSI14              float64 `json:"rsi_14"`

	AverageVolume20    float64 `json:"average_volume_20"`
	VolumeSpikeRatio20 float64 `json:"volume_spike_ratio_20"`
	DollarVolume20     float64 `json:"dollar_volume_20"`
	ZeroVolume         bool    `json:"zero_volume"`

	IntrabarRangePct       float64 `json:"intrabar_range_pct"`
	CloseLocationInBar01   float64 `json:"close_location_in_bar_01"`
	GapVsPrevClose         float64 `json:"gap_vs_prev_close"`
	HighLowExpansionRatio5 float64 `json:"high_low_expansion_ratio_5"`

	UpBarHitRate20      float64 `json:"up_bar_hit_rate_20"`
	TrendConsistency20  float64 `json:"trend_consistency_20"`
	VolatilityExpansion bool    `json:"volatility_expansion"`
	TrendStrength20     float64 `json:"trend_strength_20"`
}

type FeatureSet struct {
	SchemaVersion int                       `json:"schema_version"`
	Symbol        string                    `json:"symbol"`
	GeneratedAt   time.Time                 `json:"generated_at"`
	Vectors       map[string]*FeatureVector `json:"vectors"`
}

func (s *FeatureSet) Vector(timeframe string) *FeatureVector {
	if s == nil || s.Vectors == nil {
		return nil
	}
	return s.Vectors[timeframe]
}
