package selector

import (
	"fmt"
	"sort"
	"strings"

	"northstar/regime"
)

type Selector struct{}

func New() *Selector {
	return &Selector{}
}

func Default() *Selector {
	return New()
}

func (s *Selector) Select(result *regime.Result) *Selection {
	selection := &Selection{
		SchemaVersion:       SchemaVersion,
		SelectorVersion:     SelectorVersion,
		SelectedFamily:      StrategyFamilyNoTrade,
		SelectedStrategy:    "",
		AllowTrading:        false,
		RecommendedRiskMode: RiskModeNoTrade,
		FallbackStrategy:    "momentum_fallback",
		Valid:               false,
	}
	if result == nil {
		selection.Warnings = []string{"missing_regime_result"}
		selection.SelectionReason = "no_trade because no regime result was available"
		return selection
	}

	selection.Symbol = result.Symbol
	selection.Timestamp = result.Timestamp
	selection.Timeframe = result.Timeframe
	selection.Regime = result.Regime
	selection.RegimeConfidence = clamp(result.Confidence, 0, 1)
	selection.Warnings = dedupe(append([]string(nil), result.Warnings...))
	selection.ContributingSignals = topSignalNames(result.Contributing, 3)

	if !result.Valid || result.Regime == regime.RegimeUnknown || result.Confidence < 0.30 {
		selection.SelectionScore = clamp(result.RegimeScore, 0, 1)
		selection.Confidence = clamp(result.Confidence*0.6, 0, 1)
		selection.SelectionReason = "no_trade because the regime signal is invalid, unknown, or too weak"
		if result.Regime == regime.RegimeUnknown {
			selection.Warnings = dedupe(append(selection.Warnings, "unknown_regime"))
		}
		return selection
	}

	selection.Valid = true
	switch result.Regime {
	case regime.RegimeTrend:
		selection.SelectedFamily = StrategyFamilyMomentum
		selection.SelectedStrategy = "momentum_only"
		selection.FallbackStrategy = "momentum_fallback"
		selection.SelectionScore = clamp(result.TrendScore, 0, 1)
		selection.Confidence = clamp(0.65*result.Confidence+0.35*result.TrendScore, 0, 1)
		selection.AllowTrading = true
		selection.RecommendedRiskMode = RiskModeNormal
		selection.SelectionReason = fmt.Sprintf("momentum selected because %s", lowerFirst(result.Explanation))
	case regime.RegimeMeanReversion:
		selection.SelectedFamily = StrategyFamilyMeanReversion
		selection.SelectedStrategy = "multi_factor"
		selection.FallbackStrategy = "hybrid_ai"
		selection.SelectionScore = clamp(result.MeanReversionScore, 0, 1)
		selection.Confidence = clamp(0.70*result.Confidence+0.30*result.MeanReversionScore, 0, 1)
		selection.AllowTrading = true
		selection.RecommendedRiskMode = RiskModeNormal
		selection.SelectionReason = fmt.Sprintf("mean-reversion family selected because %s", lowerFirst(result.Explanation))
		selection.Warnings = dedupe(append(selection.Warnings, "mean_reversion_maps_to_multi_factor"))
	case regime.RegimeHighVolatility:
		selection.SelectedFamily = StrategyFamilyDefensive
		selection.SelectedStrategy = "momentum_fallback"
		selection.FallbackStrategy = "multi_factor"
		selection.SelectionScore = clamp(result.VolatilityScore, 0, 1)
		selection.Confidence = clamp(0.75*result.Confidence+0.25*result.VolatilityScore, 0, 1)
		selection.AllowTrading = true
		selection.RecommendedRiskMode = RiskModeReducedRisk
		selection.SelectionReason = fmt.Sprintf("defensive posture selected because %s", lowerFirst(result.Explanation))
	case regime.RegimeLowVolatility:
		selection.SelectedFamily = StrategyFamilyHybrid
		selection.SelectedStrategy = "hybrid_ai"
		selection.FallbackStrategy = "multi_factor"
		selection.SelectionScore = clamp(result.LowVolatilityScore, 0, 1)
		selection.Confidence = clamp(0.70*result.Confidence+0.30*result.LowVolatilityScore, 0, 1)
		selection.AllowTrading = true
		selection.RecommendedRiskMode = RiskModeNormal
		selection.SelectionReason = fmt.Sprintf("hybrid family selected because %s", lowerFirst(result.Explanation))
	case regime.RegimeUnstable:
		selection.SelectedFamily = StrategyFamilyNoTrade
		selection.SelectedStrategy = ""
		selection.FallbackStrategy = "momentum_fallback"
		selection.SelectionScore = clamp(result.InstabilityScore, 0, 1)
		selection.Confidence = clamp(0.80*result.Confidence+0.20*result.InstabilityScore, 0, 1)
		selection.AllowTrading = false
		selection.RecommendedRiskMode = RiskModeNoTrade
		selection.SelectionReason = fmt.Sprintf("no_trade selected because %s", lowerFirst(result.Explanation))
	default:
		selection.SelectionScore = clamp(result.RegimeScore, 0, 1)
		selection.Confidence = clamp(result.Confidence*0.6, 0, 1)
		selection.SelectionReason = "no_trade because the regime did not map cleanly to a known strategy family"
	}

	if selection.AllowTrading && selection.Confidence < 0.40 {
		selection.RecommendedRiskMode = RiskModeReducedRisk
		selection.Warnings = dedupe(append(selection.Warnings, "low_selector_confidence"))
	}
	return selection
}

func (s *Selector) SelectSet(results *regime.ResultSet) *SelectionSet {
	if results == nil {
		return &SelectionSet{SchemaVersion: SchemaVersion}
	}
	set := &SelectionSet{
		SchemaVersion: SchemaVersion,
		Symbol:        results.Symbol,
		GeneratedAt:   results.GeneratedAt,
		Selections:    make(map[string]*Selection, len(results.Results)),
	}
	keys := make([]string, 0, len(results.Results))
	for timeframe := range results.Results {
		keys = append(keys, timeframe)
	}
	sort.Strings(keys)
	for _, timeframe := range keys {
		set.Selections[timeframe] = s.Select(results.Results[timeframe])
	}
	return set
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func topSignalNames(values []regime.Contribution, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func lowerFirst(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}
