package regime

import (
	"math"
	"sort"
	"strings"
)

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func max(values ...float64) float64 {
	best := 0.0
	for i, value := range values {
		if i == 0 || value > best {
			best = value
		}
	}
	return best
}

func dedupeWarnings(values []string) []string {
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

func topContributions(contribs []Contribution, limit int) []Contribution {
	if len(contribs) == 0 || limit <= 0 {
		return nil
	}
	copied := append([]Contribution(nil), contribs...)
	sort.SliceStable(copied, func(i, j int) bool {
		return abs(copied[i].Contribution) > abs(copied[j].Contribution)
	})
	if len(copied) > limit {
		copied = copied[:limit]
	}
	return copied
}
