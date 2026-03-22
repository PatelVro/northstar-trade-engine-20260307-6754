package features

import (
	"sort"
	"strings"
	"time"
)

type Engine struct {
	cfg Config
}

func NewEngine(cfg Config) *Engine {
	if cfg.ShortReturnWindow <= 0 {
		cfg = DefaultConfig()
	}
	return &Engine{cfg: cfg}
}

func DefaultEngine() *Engine {
	return NewEngine(DefaultConfig())
}

func RequiredHistory(cfg Config) int {
	required := cfg.LongReturnWindow + 1
	candidates := []int{
		cfg.ATRWindow + 1,
		cfg.VolumeWindow,
		cfg.VolatilityLongWindow + 1,
		cfg.RSIWindow + 1,
		cfg.TrendWindow + 1,
		cfg.EMASlowWindow,
		cfg.PriceRankWindow,
	}
	for _, candidate := range candidates {
		if candidate > required {
			required = candidate
		}
	}
	return required
}

func FeatureNames() []string {
	return []string{
		"return_1_bar",
		"return_5_bar",
		"return_10_bar",
		"return_20_bar",
		"ema_5_distance",
		"ema_20_distance",
		"ema_5_vs_20_spread",
		"momentum_rank_proxy_20",
		"realized_vol_10",
		"realized_vol_20",
		"atr_14",
		"atr_14_pct",
		"true_range_avg_14",
		"volatility_ratio_10v20",
		"distance_from_mean_20",
		"price_zscore_20",
		"return_zscore_20",
		"rsi_14",
		"average_volume_20",
		"volume_spike_ratio_20",
		"dollar_volume_20",
		"zero_volume",
		"intrabar_range_pct",
		"close_location_in_bar_01",
		"gap_vs_prev_close",
		"high_low_expansion_ratio_5",
		"up_bar_hit_rate_20",
		"trend_consistency_20",
		"volatility_expansion",
		"trend_strength_20",
	}
}

func (e *Engine) Compute(symbol, timeframe string, bars []Bar) *FeatureVector {
	vector := &FeatureVector{
		SchemaVersion:   SchemaVersion,
		Symbol:          strings.ToUpper(strings.TrimSpace(symbol)),
		Timeframe:       strings.TrimSpace(timeframe),
		BarCount:        len(bars),
		RequiredHistory: RequiredHistory(e.cfg),
		Valid:           true,
	}
	if len(bars) == 0 {
		vector.Valid = false
		vector.InsufficientHistory = true
		vector.MissingInputs = append(vector.MissingInputs, "bars")
		vector.ValidationWarnings = append(vector.ValidationWarnings, "no_bars")
		return vector
	}

	lastBar := bars[len(bars)-1]
	vector.Timestamp = featureTimestamp(lastBar)
	vector.ZeroVolume = lastBar.Volume <= 0
	vector.ValidationWarnings = append(vector.ValidationWarnings, ValidateBars(bars)...)

	if len(bars) < vector.RequiredHistory {
		vector.InsufficientHistory = true
		vector.Valid = false
		vector.ValidationWarnings = appendUnique(vector.ValidationWarnings, "insufficient_history")
	}

	returns := Returns(bars)
	if len(returns) == 0 {
		vector.Valid = false
		vector.MissingInputs = appendUnique(vector.MissingInputs, "returns")
		return vector
	}

	setReturnFeature(&vector.Return1Bar, returns, 1, "return_1_bar", vector)
	setReturnFeature(&vector.Return5Bar, returns, e.cfg.ShortReturnWindow, "return_5_bar", vector)
	setReturnFeature(&vector.Return10Bar, returns, e.cfg.MediumReturnWindow, "return_10_bar", vector)
	setReturnFeature(&vector.Return20Bar, returns, e.cfg.LongReturnWindow, "return_20_bar", vector)

	setEMAFeatures(vector, bars, e.cfg)
	setVolatilityFeatures(vector, bars, returns, e.cfg)
	setMeanReversionFeatures(vector, bars, returns, e.cfg)
	setVolumeFeatures(vector, bars, e.cfg)
	setRangeFeatures(vector, bars, e.cfg)
	setStateFeatures(vector, bars, returns, e.cfg)

	vector.MissingInputs = dedupeStrings(vector.MissingInputs)
	vector.ValidationWarnings = dedupeStrings(vector.ValidationWarnings)
	if len(vector.MissingInputs) > 0 || len(vector.ValidationWarnings) > 0 && vector.InsufficientHistory {
		vector.Valid = !vector.InsufficientHistory && len(vector.MissingInputs) == 0
	}
	sanitizeVector(vector)
	return vector
}

func (e *Engine) ComputeSet(symbol string, series map[string][]Bar) *FeatureSet {
	set := &FeatureSet{
		SchemaVersion: SchemaVersion,
		Symbol:        strings.ToUpper(strings.TrimSpace(symbol)),
		GeneratedAt:   time.Time{},
		Vectors:       make(map[string]*FeatureVector, len(series)),
	}
	keys := make([]string, 0, len(series))
	for timeframe := range series {
		keys = append(keys, timeframe)
	}
	sort.Strings(keys)
	for _, timeframe := range keys {
		vector := e.Compute(symbol, timeframe, series[timeframe])
		set.Vectors[timeframe] = vector
		if set.GeneratedAt.IsZero() || (!vector.Timestamp.IsZero() && vector.Timestamp.After(set.GeneratedAt)) {
			set.GeneratedAt = vector.Timestamp
		}
	}
	return set
}

func featureTimestamp(bar Bar) time.Time {
	if bar.CloseTime > 0 {
		return time.UnixMilli(bar.CloseTime).UTC()
	}
	if bar.OpenTime > 0 {
		return time.UnixMilli(bar.OpenTime).UTC()
	}
	return time.Time{}
}

func setReturnFeature(target *float64, returns []float64, lookback int, name string, vector *FeatureVector) {
	if lookback <= 0 || len(returns) < lookback {
		vector.MissingInputs = appendUnique(vector.MissingInputs, name)
		return
	}
	sum := 0.0
	for _, v := range returns[len(returns)-lookback:] {
		sum += v
	}
	*target = sanitizeFloat(sum)
}

func setEMAFeatures(vector *FeatureVector, bars []Bar, cfg Config) {
	if len(bars) < cfg.EMAFastWindow {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "ema_5_distance")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "ema_5_vs_20_spread")
	} else {
		emaFast := EMA(bars, cfg.EMAFastWindow)
		if emaFast > 0 {
			vector.EMA5Distance = sanitizeFloat((bars[len(bars)-1].Close - emaFast) / emaFast)
		}
	}
	if len(bars) < cfg.EMASlowWindow {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "ema_20_distance")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "ema_5_vs_20_spread")
	} else {
		emaSlow := EMA(bars, cfg.EMASlowWindow)
		if emaSlow > 0 {
			vector.EMA20Distance = sanitizeFloat((bars[len(bars)-1].Close - emaSlow) / emaSlow)
		}
		if len(bars) >= cfg.EMAFastWindow {
			emaFast := EMA(bars, cfg.EMAFastWindow)
			if emaSlow > 0 {
				vector.EMA5Vs20Spread = sanitizeFloat((emaFast - emaSlow) / emaSlow)
			}
		}
	}
	if len(bars) >= cfg.PriceRankWindow {
		window := bars[len(bars)-cfg.PriceRankWindow:]
		rank := 0
		for _, bar := range window {
			if bar.Close <= bars[len(bars)-1].Close {
				rank++
			}
		}
		vector.MomentumRankProxy20 = sanitizeFloat(float64(rank) / float64(len(window)))
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "momentum_rank_proxy_20")
	}
}

func setVolatilityFeatures(vector *FeatureVector, bars []Bar, returns []float64, cfg Config) {
	if len(returns) >= cfg.VolatilityShortWindow {
		_, std := MeanStd(returns[len(returns)-cfg.VolatilityShortWindow:])
		vector.RealizedVol10 = std
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "realized_vol_10")
	}
	if len(returns) >= cfg.VolatilityLongWindow {
		_, std := MeanStd(returns[len(returns)-cfg.VolatilityLongWindow:])
		vector.RealizedVol20 = std
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "realized_vol_20")
	}
	if vector.RealizedVol20 > 0 {
		vector.VolatilityRatio10v20 = sanitizeFloat(vector.RealizedVol10 / vector.RealizedVol20)
	}
	if len(bars) > cfg.ATRWindow {
		vector.ATR14 = ATR(bars, cfg.ATRWindow)
		vector.TrueRangeAvg14 = TrueRangeAverage(bars, cfg.ATRWindow)
		if bars[len(bars)-1].Close > 0 {
			vector.ATR14Pct = sanitizeFloat(vector.ATR14 / bars[len(bars)-1].Close)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "atr_14")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "true_range_avg_14")
	}
	vector.VolatilityExpansion = vector.RealizedVol10 > 0 && vector.RealizedVol20 > 0 && vector.RealizedVol10 > vector.RealizedVol20*1.2
}

func setMeanReversionFeatures(vector *FeatureVector, bars []Bar, returns []float64, cfg Config) {
	if len(bars) >= cfg.LongReturnWindow {
		closes := lastCloses(bars, cfg.LongReturnWindow)
		mean, std := MeanStd(closes)
		lastClose := bars[len(bars)-1].Close
		if mean > 0 {
			vector.DistanceFromMean20 = sanitizeFloat((lastClose - mean) / mean)
		}
		if std > 0 {
			vector.PriceZScore20 = sanitizeFloat((lastClose - mean) / std)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "distance_from_mean_20")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "price_zscore_20")
	}
	if len(returns) >= cfg.LongReturnWindow {
		window := returns[len(returns)-cfg.LongReturnWindow:]
		mean, std := MeanStd(window)
		lastRet := returns[len(returns)-1]
		if std > 0 {
			vector.ReturnZScore20 = sanitizeFloat((lastRet - mean) / std)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "return_zscore_20")
	}
	if len(bars) > cfg.RSIWindow {
		vector.RSI14 = RSI(bars, cfg.RSIWindow)
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "rsi_14")
	}
}

func setVolumeFeatures(vector *FeatureVector, bars []Bar, cfg Config) {
	if len(bars) >= cfg.VolumeWindow {
		sum := 0.0
		dollarVolSum := 0.0
		for _, bar := range bars[len(bars)-cfg.VolumeWindow:] {
			sum += bar.Volume
			dollarVolSum += bar.Close * bar.Volume
		}
		vector.AverageVolume20 = sanitizeFloat(sum / float64(cfg.VolumeWindow))
		vector.DollarVolume20 = sanitizeFloat(dollarVolSum / float64(cfg.VolumeWindow))
		if vector.AverageVolume20 > 0 {
			vector.VolumeSpikeRatio20 = sanitizeFloat(bars[len(bars)-1].Volume / vector.AverageVolume20)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "average_volume_20")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "dollar_volume_20")
	}
}

func setRangeFeatures(vector *FeatureVector, bars []Bar, cfg Config) {
	lastBar := bars[len(bars)-1]
	if lastBar.Close > 0 {
		vector.IntrabarRangePct = sanitizeFloat((lastBar.High - lastBar.Low) / lastBar.Close)
	}
	barRange := lastBar.High - lastBar.Low
	if barRange > 0 {
		vector.CloseLocationInBar01 = sanitizeFloat((lastBar.Close - lastBar.Low) / barRange)
	}
	if len(bars) >= 2 {
		prevClose := bars[len(bars)-2].Close
		if prevClose > 0 {
			vector.GapVsPrevClose = sanitizeFloat((lastBar.Open - prevClose) / prevClose)
		}
	}
	if len(bars) > cfg.ExpansionWindow {
		sumRange := 0.0
		for _, bar := range bars[len(bars)-cfg.ExpansionWindow-1 : len(bars)-1] {
			sumRange += bar.High - bar.Low
		}
		avgRange := sumRange / float64(cfg.ExpansionWindow)
		if avgRange > 0 {
			vector.HighLowExpansionRatio5 = sanitizeFloat((lastBar.High - lastBar.Low) / avgRange)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "high_low_expansion_ratio_5")
	}
}

func setStateFeatures(vector *FeatureVector, bars []Bar, returns []float64, cfg Config) {
	if len(returns) >= cfg.TrendWindow {
		window := returns[len(returns)-cfg.TrendWindow:]
		upBars := 0
		sum := 0.0
		sumAbs := 0.0
		for _, value := range window {
			if value > 0 {
				upBars++
			}
			sum += value
			if value < 0 {
				sumAbs -= value
			} else {
				sumAbs += value
			}
		}
		vector.UpBarHitRate20 = sanitizeFloat(float64(upBars) / float64(len(window)))
		if sumAbs > 0 {
			vector.TrendConsistency20 = sanitizeFloat(sum / sumAbs)
			if vector.TrendConsistency20 < 0 {
				vector.TrendConsistency20 = -vector.TrendConsistency20
			}
		}
		if vector.RealizedVol20 > 0 {
			vector.TrendStrength20 = sanitizeFloat(absFloat(vector.Return20Bar) / vector.RealizedVol20)
		}
	} else {
		vector.MissingInputs = appendUnique(vector.MissingInputs, "up_bar_hit_rate_20")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "trend_consistency_20")
		vector.MissingInputs = appendUnique(vector.MissingInputs, "trend_strength_20")
	}
}

func lastCloses(bars []Bar, window int) []float64 {
	if len(bars) < window {
		window = len(bars)
	}
	out := make([]float64, 0, window)
	for _, bar := range bars[len(bars)-window:] {
		out = append(out, sanitizeFloat(bar.Close))
	}
	return out
}

func sanitizeVector(vector *FeatureVector) {
	vector.Return1Bar = sanitizeFloat(vector.Return1Bar)
	vector.Return5Bar = sanitizeFloat(vector.Return5Bar)
	vector.Return10Bar = sanitizeFloat(vector.Return10Bar)
	vector.Return20Bar = sanitizeFloat(vector.Return20Bar)
	vector.EMA5Distance = sanitizeFloat(vector.EMA5Distance)
	vector.EMA20Distance = sanitizeFloat(vector.EMA20Distance)
	vector.EMA5Vs20Spread = sanitizeFloat(vector.EMA5Vs20Spread)
	vector.MomentumRankProxy20 = sanitizeFloat(vector.MomentumRankProxy20)
	vector.RealizedVol10 = sanitizeFloat(vector.RealizedVol10)
	vector.RealizedVol20 = sanitizeFloat(vector.RealizedVol20)
	vector.ATR14 = sanitizeFloat(vector.ATR14)
	vector.ATR14Pct = sanitizeFloat(vector.ATR14Pct)
	vector.TrueRangeAvg14 = sanitizeFloat(vector.TrueRangeAvg14)
	vector.VolatilityRatio10v20 = sanitizeFloat(vector.VolatilityRatio10v20)
	vector.DistanceFromMean20 = sanitizeFloat(vector.DistanceFromMean20)
	vector.PriceZScore20 = sanitizeFloat(vector.PriceZScore20)
	vector.ReturnZScore20 = sanitizeFloat(vector.ReturnZScore20)
	vector.RSI14 = sanitizeFloat(vector.RSI14)
	vector.AverageVolume20 = sanitizeFloat(vector.AverageVolume20)
	vector.VolumeSpikeRatio20 = sanitizeFloat(vector.VolumeSpikeRatio20)
	vector.DollarVolume20 = sanitizeFloat(vector.DollarVolume20)
	vector.IntrabarRangePct = sanitizeFloat(vector.IntrabarRangePct)
	vector.CloseLocationInBar01 = sanitizeFloat(vector.CloseLocationInBar01)
	vector.GapVsPrevClose = sanitizeFloat(vector.GapVsPrevClose)
	vector.HighLowExpansionRatio5 = sanitizeFloat(vector.HighLowExpansionRatio5)
	vector.UpBarHitRate20 = sanitizeFloat(vector.UpBarHitRate20)
	vector.TrendConsistency20 = sanitizeFloat(vector.TrendConsistency20)
	vector.TrendStrength20 = sanitizeFloat(vector.TrendStrength20)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
