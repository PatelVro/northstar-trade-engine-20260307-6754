package news

import (
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AnalyzeText — sentiment and impact scoring
// ---------------------------------------------------------------------------

func TestAnalyzeText_EmptyString(t *testing.T) {
	sent, imp := AnalyzeText("")
	assertZero(t, "sentiment empty", sent)
	assertZero(t, "impact empty", imp)
}

func TestAnalyzeText_WhitespaceOnly(t *testing.T) {
	sent, imp := AnalyzeText("   ")
	assertZero(t, "sentiment whitespace", sent)
	assertZero(t, "impact whitespace", imp)
}

func TestAnalyzeText_PositiveSentiment(t *testing.T) {
	sent, _ := AnalyzeText("Company beats earnings estimates with record growth")
	if sent <= 0 {
		t.Errorf("expected positive sentiment, got %.4f", sent)
	}
}

func TestAnalyzeText_NegativeSentiment(t *testing.T) {
	sent, _ := AnalyzeText("SEC launches investigation into possible fraud at firm")
	if sent >= 0 {
		t.Errorf("expected negative sentiment, got %.4f", sent)
	}
}

func TestAnalyzeText_NeutralText(t *testing.T) {
	sent, _ := AnalyzeText("shares traded at average volume today")
	assertZero(t, "neutral sentiment", sent)
}

func TestAnalyzeText_ImpactWithMacroTerms(t *testing.T) {
	_, impFed := AnalyzeText("fed announces rate hike of 25bps, inflation remains elevated")
	_, impNeutral := AnalyzeText("shares traded at average volume today")
	if impFed <= impNeutral {
		t.Errorf("expected macro terms to increase impact: fed=%.4f, neutral=%.4f", impFed, impNeutral)
	}
}

func TestAnalyzeText_PercentageRegexRequiresWordBoundaryAfterPercent(t *testing.T) {
	// NOTE: The current regex `\b\d+(\.\d+)?%\b` requires a word-boundary AFTER
	// the %, which means "15% drop" does NOT match (non-word to non-word).
	// Only "15%loss" (non-word to word) matches. This documents current behavior.
	_, impNoMatch := AnalyzeText("revenue plunge 15% year over year")
	_, impMatch := AnalyzeText("revenue plunge 15%drop year over year")
	if impMatch <= impNoMatch {
		t.Errorf("expected word-boundary match to boost impact: match=%.4f, noMatch=%.4f", impMatch, impNoMatch)
	}
}

func TestAnalyzeText_ImpactBoostedByMultipleSignals(t *testing.T) {
	_, impMulti := AnalyzeText("company beats earnings, upgrades guidance, outperform")
	_, impSingle := AnalyzeText("company beats earnings")
	if impMulti <= impSingle {
		t.Errorf("expected multiple terms to boost impact: multi=%.4f, single=%.4f", impMulti, impSingle)
	}
}

func TestAnalyzeText_SentimentBounded(t *testing.T) {
	// Pack many positive terms
	sent, _ := AnalyzeText("beats record surge rally outperform bullish upgrade growth strong approval expands buyback")
	if sent > 1 || sent < -1 {
		t.Errorf("sentiment out of bounds [-1, 1]: %.4f", sent)
	}
}

func TestAnalyzeText_ImpactBounded(t *testing.T) {
	// Pack many impact terms + percentage
	_, imp := AnalyzeText("fed fomc cpi inflation interest rate earnings guidance sec tariff sanction war merger acquisition bankruptcy 50% default recall lawsuit investigation credit rating downgrade")
	if imp > 1 || imp < 0 {
		t.Errorf("impact out of bounds [0, 1]: %.4f", imp)
	}
}

func TestAnalyzeText_CaseInsensitive(t *testing.T) {
	sent1, imp1 := AnalyzeText("COMPANY BEATS EARNINGS")
	sent2, imp2 := AnalyzeText("company beats earnings")
	assertFloatNear(t, "case-insensitive sentiment", sent1, sent2, 0.001)
	assertFloatNear(t, "case-insensitive impact", imp1, imp2, 0.001)
}

func TestAnalyzeText_BaselineImpact(t *testing.T) {
	// Even neutral text gets a baseline impact of 0.20
	_, imp := AnalyzeText("nothing special happening")
	if imp < 0.19 {
		t.Errorf("expected baseline impact >= 0.20, got %.4f", imp)
	}
}

// ---------------------------------------------------------------------------
// countTerms
// ---------------------------------------------------------------------------

func TestCountTerms_MultipleMatches(t *testing.T) {
	count := countTerms("the company beats earnings and raises guidance with strong growth", positiveTerms)
	if count < 3 {
		t.Errorf("expected at least 3 positive matches, got %d", count)
	}
}

func TestCountTerms_NoMatches(t *testing.T) {
	count := countTerms("the weather is nice today", positiveTerms)
	if count != 0 {
		t.Errorf("expected 0 matches, got %d", count)
	}
}

func TestCountTerms_MultiWordTerms(t *testing.T) {
	count := countTerms("the fed plans to cut guidance for next quarter", negativeTerms)
	if count < 1 {
		t.Errorf("expected 'cuts guidance' or similar to match, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// parsePubDate
// ---------------------------------------------------------------------------

func TestParsePubDate_RFC1123Z(t *testing.T) {
	parsed, ok := parsePubDate("Mon, 02 Jan 2006 15:04:05 -0700")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if parsed.Year() != 2006 || parsed.Month() != time.January || parsed.Day() != 2 {
		t.Errorf("unexpected date: %v", parsed)
	}
}

func TestParsePubDate_RFC1123(t *testing.T) {
	parsed, ok := parsePubDate("Mon, 02 Jan 2006 15:04:05 GMT")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if parsed.Year() != 2006 {
		t.Errorf("unexpected year: %d", parsed.Year())
	}
}

func TestParsePubDate_Empty(t *testing.T) {
	_, ok := parsePubDate("")
	if ok {
		t.Fatal("expected failure for empty string")
	}
}

func TestParsePubDate_Invalid(t *testing.T) {
	_, ok := parsePubDate("not-a-date")
	if ok {
		t.Fatal("expected failure for invalid date")
	}
}

func TestParsePubDate_WhitespaceHandled(t *testing.T) {
	_, ok := parsePubDate("  Mon, 02 Jan 2006 15:04:05 -0700  ")
	if !ok {
		t.Fatal("expected successful parse after trimming")
	}
}

// ---------------------------------------------------------------------------
// buildWatchlist
// ---------------------------------------------------------------------------

func TestBuildWatchlist_AlwaysIncludesMarketProxies(t *testing.T) {
	wl := buildWatchlist(nil)
	if len(wl) < 4 {
		t.Fatalf("expected at least 4 market proxies, got %d", len(wl))
	}
	expected := map[string]bool{"SPY": true, "QQQ": true, "IWM": true, "DIA": true}
	for _, s := range wl[:4] {
		if !expected[s] {
			t.Errorf("unexpected symbol in proxies: %s", s)
		}
	}
}

func TestBuildWatchlist_AddsUserSymbols(t *testing.T) {
	wl := buildWatchlist([]string{"AAPL", "MSFT"})
	found := map[string]bool{}
	for _, s := range wl {
		found[s] = true
	}
	if !found["AAPL"] || !found["MSFT"] {
		t.Error("expected AAPL and MSFT in watchlist")
	}
}

func TestBuildWatchlist_DeduplicatesMarketProxies(t *testing.T) {
	wl := buildWatchlist([]string{"SPY", "AAPL", "SPY"})
	count := 0
	for _, s := range wl {
		if s == "SPY" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected SPY once, got %d", count)
	}
}

func TestBuildWatchlist_UppercasesAndTrims(t *testing.T) {
	wl := buildWatchlist([]string{"  aapl  ", "msft"})
	found := map[string]bool{}
	for _, s := range wl {
		found[s] = true
	}
	if !found["AAPL"] || !found["MSFT"] {
		t.Error("expected uppercased symbols")
	}
}

func TestBuildWatchlist_SkipsEmpty(t *testing.T) {
	wl := buildWatchlist([]string{"AAPL", "", "  ", "MSFT"})
	for _, s := range wl {
		if s == "" {
			t.Error("empty symbol in watchlist")
		}
	}
}

func TestBuildWatchlist_CapsAt14(t *testing.T) {
	symbols := make([]string, 20)
	for i := range symbols {
		symbols[i] = "SYM" + string(rune('A'+i))
	}
	wl := buildWatchlist(symbols)
	if len(wl) > 14 {
		t.Errorf("expected max 14 symbols, got %d", len(wl))
	}
}

func TestBuildWatchlist_UserSymbolsSorted(t *testing.T) {
	wl := buildWatchlist([]string{"TSLA", "AAPL", "MSFT"})
	// First 4 are market proxies, rest should be sorted
	user := wl[4:]
	for i := 1; i < len(user); i++ {
		if user[i] < user[i-1] {
			t.Errorf("user symbols not sorted: %v", user)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// isMarketSymbol
// ---------------------------------------------------------------------------

func TestIsMarketSymbol(t *testing.T) {
	cases := []struct {
		symbol string
		want   bool
	}{
		{"SPY", true},
		{"QQQ", true},
		{"IWM", true},
		{"DIA", true},
		{"spy", true},
		{" SPY ", true},
		{"AAPL", false},
		{"MSFT", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isMarketSymbol(tc.symbol)
		if got != tc.want {
			t.Errorf("isMarketSymbol(%q) = %v, want %v", tc.symbol, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// NewProvider factory
// ---------------------------------------------------------------------------

func TestNewProvider_RSS(t *testing.T) {
	for _, name := range []string{"rss", "yahoo_rss", "", "  rss  "} {
		p, err := NewProvider(name)
		if err != nil {
			t.Errorf("NewProvider(%q) error: %v", name, err)
			continue
		}
		if p.Name() != "rss" {
			t.Errorf("NewProvider(%q) name = %q, want 'rss'", name, p.Name())
		}
	}
}

func TestNewProvider_Unsupported(t *testing.T) {
	_, err := NewProvider("bloomberg")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertZero(t *testing.T, name string, got float64) {
	t.Helper()
	if math.Abs(got) > 0.001 {
		t.Errorf("%s: expected ~0, got %.6f", name, got)
	}
}

func assertFloatNear(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s: got %.6f, want %.6f (tolerance %.4f)", name, got, want, tolerance)
	}
}
