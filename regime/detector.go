package regime

import (
	"fmt"
	"northstar/features"
	"sort"
	"strings"
)

type Detector struct{}

func NewDetector() *Detector {
	return &Detector{}
}

func DefaultDetector() *Detector {
	return NewDetector()
}

func (d *Detector) Detect(vector *features.FeatureVector) *Result {
	if vector == nil {
		return &Result{
			SchemaVersion:   SchemaVersion,
			DetectorVersion: DetectorVersion,
			Regime:          RegimeUnknown,
			Valid:           false,
			Warnings:        []string{"missing_feature_vector"},
			Explanation:     "unknown regime due to missing feature vector",
		}
	}

	result := &Result{
		SchemaVersion:   SchemaVersion,
		DetectorVersion: DetectorVersion,
		Symbol:          vector.Symbol,
		Timestamp:       vector.Timestamp,
		Timeframe:       vector.Timeframe,
		Regime:          RegimeUnknown,
		Valid:           vector.Valid,
		Warnings:        dedupeWarnings(append(append([]string(nil), vector.ValidationWarnings...), vector.MissingInputs...)),
	}

	if !vector.Valid {
		result.Explanation = "unknown regime due to invalid or insufficient features"
		return result
	}

	trendMomentum := clamp(abs(vector.Return20Bar)/0.08, 0, 1)
	maSpread := clamp(abs(vector.EMA5Vs20Spread)/0.04, 0, 1)
	trendConsistency := clamp(vector.TrendConsistency20, 0, 1)
	trendStrength := clamp(vector.TrendStrength20/4.0, 0, 1)
	trendScore := clamp(0.30*trendMomentum+0.25*maSpread+0.20*trendConsistency+0.25*trendStrength, 0, 1)

	stretch := clamp(max(abs(vector.PriceZScore20)/2.2, abs(vector.ReturnZScore20)/2.2, abs(vector.RSI14-50.0)/30.0), 0, 1)
	weakTrend := 1.0 - clamp(0.55*trendConsistency+0.45*trendStrength, 0, 1)
	reversionBias := clamp(0.65*stretch+0.35*weakTrend, 0, 1)

	volLevel := clamp(max(vector.RealizedVol20/0.03, vector.ATR14Pct/0.03), 0, 1)
	volRatio := clamp((vector.VolatilityRatio10v20-1.0)/0.45, 0, 1)
	volExpansion := 0.0
	if vector.VolatilityExpansion {
		volExpansion = 1.0
	}
	volatilityScore := clamp(0.45*volLevel+0.30*volRatio+0.25*volExpansion, 0, 1)

	calmVol := 1.0 - clamp(max(vector.RealizedVol20/0.015, vector.ATR14Pct/0.015), 0, 1)
	calmRatio := 1.0 - clamp(abs(vector.VolatilityRatio10v20-1.0)/0.35, 0, 1)
	lowVolatilityScore := clamp(0.65*calmVol+0.35*calmRatio, 0, 1)

	conflict := clamp((1.0-abs(trendScore-reversionBias))*minScore(trendScore, reversionBias), 0, 1)
	warningPenalty := clamp(float64(len(result.Warnings))/4.0, 0, 1)
	instabilityScore := clamp(0.45*volatilityScore+0.25*(1.0-trendConsistency)+0.20*conflict+0.10*warningPenalty, 0, 1)

	result.TrendScore = trendScore
	result.MeanReversionScore = reversionBias
	result.VolatilityScore = volatilityScore
	result.LowVolatilityScore = lowVolatilityScore
	result.InstabilityScore = instabilityScore

	contribs := []Contribution{
		{Name: "return_20_bar", Value: vector.Return20Bar, Contribution: 0.30 * trendMomentum, Note: "longer-window momentum magnitude"},
		{Name: "ema_5_vs_20_spread", Value: vector.EMA5Vs20Spread, Contribution: 0.25 * maSpread, Note: "moving-average spread strength"},
		{Name: "trend_consistency_20", Value: vector.TrendConsistency20, Contribution: 0.20 * trendConsistency, Note: "directional consistency of recent bars"},
		{Name: "trend_strength_20", Value: vector.TrendStrength20, Contribution: 0.25 * trendStrength, Note: "magnitude relative to realized volatility"},
		{Name: "price_zscore_20", Value: vector.PriceZScore20, Contribution: 0.35 * stretch, Note: "price stretch versus recent mean"},
		{Name: "return_zscore_20", Value: vector.ReturnZScore20, Contribution: 0.20 * stretch, Note: "return stretch versus recent distribution"},
		{Name: "rsi_14", Value: vector.RSI14, Contribution: 0.10 * stretch, Note: "bounded momentum stretch"},
		{Name: "realized_vol_20", Value: vector.RealizedVol20, Contribution: 0.25 * volLevel, Note: "absolute volatility level"},
		{Name: "atr_14_pct", Value: vector.ATR14Pct, Contribution: 0.20 * volLevel, Note: "range-based volatility"},
		{Name: "volatility_ratio_10v20", Value: vector.VolatilityRatio10v20, Contribution: 0.30 * volRatio, Note: "short-vs-long volatility expansion"},
	}
	if vector.VolatilityExpansion {
		contribs = append(contribs, Contribution{Name: "volatility_expansion", Value: 1, Contribution: 0.25, Note: "short-window volatility exceeds long-window baseline"})
	}

	type regimeScore struct {
		regime Regime
		score  float64
	}
	scores := []regimeScore{
		{regime: RegimeTrend, score: trendScore},
		{regime: RegimeMeanReversion, score: reversionBias},
		{regime: RegimeHighVolatility, score: volatilityScore},
		{regime: RegimeLowVolatility, score: lowVolatilityScore},
		{regime: RegimeUnstable, score: instabilityScore},
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})
	top := scores[0]
	second := scores[1]

	switch {
	case instabilityScore >= 0.70:
		result.Regime = RegimeUnstable
		result.RegimeScore = instabilityScore
	case volatilityScore >= 0.62 && volatilityScore >= max(trendScore, reversionBias, lowVolatilityScore)+0.05:
		result.Regime = RegimeHighVolatility
		result.RegimeScore = volatilityScore
	case lowVolatilityScore >= 0.62 && lowVolatilityScore >= max(trendScore, reversionBias)+0.05:
		result.Regime = RegimeLowVolatility
		result.RegimeScore = lowVolatilityScore
	case trendScore >= 0.55 && trendScore >= reversionBias+0.05:
		result.Regime = RegimeTrend
		result.RegimeScore = trendScore
	case reversionBias >= 0.55 && reversionBias >= trendScore+0.05:
		result.Regime = RegimeMeanReversion
		result.RegimeScore = reversionBias
	default:
		result.Regime = RegimeUnknown
		result.RegimeScore = top.score
		result.Warnings = dedupeWarnings(append(result.Warnings, "weak_regime_signal"))
	}

	margin := clamp(top.score-second.score, 0, 1)
	result.Confidence = clamp(0.55*result.RegimeScore+0.45*margin, 0, 1)
	if result.Regime == RegimeUnknown {
		result.Confidence = clamp(result.Confidence*0.6, 0, 1)
	}
	result.Contributing = topContributions(contribs, 4)
	result.Explanation = buildExplanation(result)
	return result
}

func (d *Detector) DetectSet(set *features.FeatureSet) *ResultSet {
	if set == nil {
		return &ResultSet{SchemaVersion: SchemaVersion}
	}
	out := &ResultSet{
		SchemaVersion: SchemaVersion,
		Symbol:        set.Symbol,
		GeneratedAt:   set.GeneratedAt,
		Results:       make(map[string]*Result, len(set.Vectors)),
	}
	keys := make([]string, 0, len(set.Vectors))
	for timeframe := range set.Vectors {
		keys = append(keys, timeframe)
	}
	sort.Strings(keys)
	for _, timeframe := range keys {
		out.Results[timeframe] = d.Detect(set.Vectors[timeframe])
	}
	return out
}

func buildExplanation(result *Result) string {
	if result == nil {
		return ""
	}
	if !result.Valid && result.Regime == RegimeUnknown {
		return "unknown regime due to invalid or insufficient features"
	}
	parts := make([]string, 0, len(result.Contributing))
	for _, contribution := range result.Contributing {
		if contribution.Note != "" {
			parts = append(parts, contribution.Note)
		} else {
			parts = append(parts, contribution.Name)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s regime from bounded feature scoring", result.Regime)
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return fmt.Sprintf("%s regime due to %s", strings.ReplaceAll(string(result.Regime), "_", " "), strings.Join(parts, ", "))
}

func minScore(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
