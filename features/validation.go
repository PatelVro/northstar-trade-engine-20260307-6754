package features

import "fmt"

func ValidateBars(bars []Bar) []string {
	warnings := make([]string, 0, 4)
	if len(bars) == 0 {
		return append(warnings, "no_bars")
	}
	for i, bar := range bars {
		if bar.Close <= 0 || bar.Open <= 0 || bar.High <= 0 || bar.Low <= 0 {
			warnings = appendUnique(warnings, fmt.Sprintf("non_positive_ohlc_at_%d", i))
		}
		if bar.High < bar.Low {
			warnings = appendUnique(warnings, fmt.Sprintf("invalid_range_at_%d", i))
		}
		if i > 0 && bar.OpenTime > 0 && bars[i-1].OpenTime > 0 && bar.OpenTime <= bars[i-1].OpenTime {
			warnings = appendUnique(warnings, "non_monotonic_open_time")
		}
	}
	return warnings
}

func appendUnique(dst []string, value string) []string {
	for _, existing := range dst {
		if existing == value {
			return dst
		}
	}
	return append(dst, value)
}
