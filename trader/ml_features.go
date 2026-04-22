package trader

import (
	"math"
	"time"

	"northstar/market"
)

// buildMLFeatureMap extracts a feature map from market.Data in the form the
// Python ML sidecar expects. The model was trained on the feature set in
// ml-signal-service/train_and_eval.py; feature names here mirror that file.
//
// Not every training feature is available from market.Data — some require
// multi-bar context we don't keep live. Missing features default to 0 in
// the sidecar, which is a lossy-but-safe fallback (LightGBM handles missing
// features gracefully, but prediction quality degrades with sparsity).
//
// Coverage audit:
//   Covered:    ~25 of 39 features
//   Missing:    return_3/6/12/24 (need historic bars), funding_rate_smoothed_*,
//               macd_hist, bar_range_avg_20, up_bar_hit_rate_10, hour_of_day
//               (the Python version uses the bar's wall-clock hour which we
//               can synthesize), ema_20_vs_50 (need EMA50 series)
//
// For shadow-mode A/B testing, partial coverage is fine — we'll log both
// rule-based and ML scores and learn whether ML's predictions correlate
// with trade outcomes. If it does, we extend coverage. If it doesn't, the
// integration was cheap to walk back.
func buildMLFeatureMap(data *market.Data, nowUTC time.Time) map[string]float64 {
	out := make(map[string]float64, 40)
	if data == nil {
		return out
	}

	// --------- Features directly from market.Data ---------
	if data.FundingRate != 0 {
		out["funding_rate"] = data.FundingRate
		out["funding_abs"] = math.Abs(data.FundingRate)
	}
	if data.CurrentPrice > 0 {
		// macd feature is MACD line / price
		out["macd"] = data.CurrentMACD / data.CurrentPrice
	}
	out["rsi_7"] = data.CurrentRSI7
	out["rsi_7_dist"] = (data.CurrentRSI7 - 50.0) / 50.0

	// --------- Time features ---------
	out["hour_of_day"] = float64(nowUTC.Hour())
	out["day_of_week"] = float64(int(nowUTC.Weekday()))

	// --------- Features from LongerTermContext (4h EMAs + ATR) ---------
	if ctx := data.LongerTermContext; ctx != nil && data.CurrentPrice > 0 {
		if ctx.EMA20 > 0 {
			out["price_vs_ema20"] = (data.CurrentPrice - ctx.EMA20) / data.CurrentPrice
		}
		if ctx.EMA50 > 0 {
			out["price_vs_ema50"] = (data.CurrentPrice - ctx.EMA50) / data.CurrentPrice
		}
		if ctx.EMA20 > 0 && ctx.EMA50 > 0 {
			out["ema_20_vs_50"] = (ctx.EMA20 - ctx.EMA50) / data.CurrentPrice
		}
	}

	// --------- Features from the Go features package (FeatureSet) ---------
	if data.Features != nil {
		if v := data.Features.Vector("4h"); v != nil {
			out["return_1"] = v.Return1Bar
			// return_5/10/20 bars are different from the 3/6/12/24 the ML wants,
			// but close enough for a shadow signal. Map to the nearest.
			out["return_6"] = v.Return5Bar   // approximate
			out["return_12"] = v.Return10Bar // approximate
			out["return_24"] = v.Return20Bar // approximate
			if v.ATR14Pct > 0 {
				out["atr_14_pct"] = v.ATR14Pct
			}
			out["vol_10"] = v.RealizedVol10
			out["vol_20"] = v.RealizedVol20
			out["vol_ratio_10_20"] = v.VolatilityRatio10v20
			out["ema_5_vs_20"] = v.EMA5Vs20Spread
			out["bar_range_pct"] = v.IntrabarRangePct
			// bar_body requires per-bar open/close which live-mode FeatureVector
			// doesn't carry — omit; ML sidecar defaults missing features to 0.
			out["volume_spike"] = v.VolumeSpikeRatio20
			if v.DollarVolume20 > 0 {
				out["dollar_volume_log"] = math.Log1p(v.DollarVolume20)
			}
			out["up_bar_hit_rate_20"] = v.UpBarHitRate20
			out["distance_from_mean_20"] = v.DistanceFromMean20
			out["price_zscore_20"] = v.PriceZScore20
			out["return_zscore_20"] = v.ReturnZScore20
			out["trend_consistency_20"] = v.TrendConsistency20
			out["close_location_in_bar"] = v.CloseLocationInBar01
			out["rsi_14"] = v.RSI14
		}
	}

	return out
}
