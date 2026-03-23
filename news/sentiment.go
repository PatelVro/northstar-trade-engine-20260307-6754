package news

import (
	"regexp"
	"strings"
)

var (
	percentPattern = regexp.MustCompile(`\b\d+(\.\d+)?%`)
)

var positiveTerms = []string{
	"beat", "beats", "strong", "record", "raise guidance", "upgrades",
	"upgrade", "growth", "surge", "rally", "outperform", "bullish",
	"buyback", "approval", "expands", "profit rises",
}

var negativeTerms = []string{
	"miss", "misses", "downgrade", "downgrades", "warning", "warns",
	"cuts guidance", "cut guidance", "lawsuit", "probe", "investigation",
	"fraud", "recall", "plunge", "slump", "bankruptcy", "default",
	"bearish", "layoff", "regulatory risk", "delist", "halted",
}

var impactTerms = []string{
	"fed", "fomc", "cpi", "inflation", "interest rate", "rate hike",
	"earnings", "guidance", "sec", "tariff", "sanction", "war",
	"merger", "acquisition", "bankruptcy", "default", "recall",
	"lawsuit", "investigation", "credit rating", "downgrade",
}

// AnalyzeText returns normalized sentiment and impact from one headline/body pair.
func AnalyzeText(text string) (sentiment float64, impact float64) {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return 0, 0
	}

	pos := countTerms(normalized, positiveTerms)
	neg := countTerms(normalized, negativeTerms)
	imp := countTerms(normalized, impactTerms)

	// Sentiment in [-1, 1], with additive smoothing to avoid division by zero spikes.
	sentiment = float64(pos-neg) / float64(pos+neg+1)
	if sentiment > 1 {
		sentiment = 1
	}
	if sentiment < -1 {
		sentiment = -1
	}

	impact = 0.20 + 0.18*float64(imp)
	if percentPattern.MatchString(normalized) {
		impact += 0.12
	}
	if pos+neg >= 2 {
		impact += 0.10
	}
	if impact > 1 {
		impact = 1
	}
	if impact < 0 {
		impact = 0
	}
	return sentiment, impact
}

func countTerms(text string, terms []string) int {
	total := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			total++
		}
	}
	return total
}
