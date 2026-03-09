package news

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// RSSProvider pulls Yahoo Finance RSS headlines and converts them to signals.
type RSSProvider struct {
	client *http.Client
}

type rssEnvelope struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
}

// NewRSSProvider creates a Yahoo RSS-backed provider.
func NewRSSProvider() *RSSProvider {
	return &RSSProvider{
		client: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

func (p *RSSProvider) Name() string { return "rss" }

func (p *RSSProvider) Fetch(symbols []string, lookback time.Duration) (*Snapshot, error) {
	if lookback <= 0 {
		lookback = 4 * time.Hour
	}
	now := time.Now()
	cutoff := now.Add(-lookback)
	watch := buildWatchlist(symbols)

	snapshot := &Snapshot{
		GeneratedAt:   now,
		Lookback:      lookback,
		Provider:      p.Name(),
		SymbolSignals: make(map[string]SymbolSignal, len(watch)),
	}

	type accum struct {
		sentWeighted float64
		impactWeight float64
		count        int
		lastTitle    string
		lastTime     time.Time
	}

	perSymbol := make(map[string]*accum, len(watch))
	marketSentWeighted := 0.0
	marketImpactWeight := 0.0
	totalHeadlines := 0

	for _, symbol := range watch {
		items, err := p.fetchSymbol(symbol)
		if err != nil {
			continue
		}
		for _, item := range items {
			published, ok := parsePubDate(item.PubDate)
			if !ok || published.Before(cutoff) {
				continue
			}
			ageHours := now.Sub(published).Hours()
			if ageHours < 0 {
				ageHours = 0
			}
			// Recent headlines matter more; half-life around ~8h.
			recencyWeight := 1.0 / (1.0 + ageHours/8.0)
			sent, imp := AnalyzeText(item.Title + " " + item.Description)
			weight := recencyWeight * imp
			if weight <= 0 {
				continue
			}

			acc := perSymbol[symbol]
			if acc == nil {
				acc = &accum{}
				perSymbol[symbol] = acc
			}
			acc.sentWeighted += sent * weight
			acc.impactWeight += weight
			acc.count++
			if published.After(acc.lastTime) {
				acc.lastTime = published
				acc.lastTitle = strings.TrimSpace(item.Title)
			}

			if isMarketSymbol(symbol) {
				marketSentWeighted += sent * weight
				marketImpactWeight += weight
			}
			totalHeadlines++
		}
	}

	for symbol, acc := range perSymbol {
		if acc == nil || acc.impactWeight <= 0 {
			continue
		}
		sentiment := acc.sentWeighted / acc.impactWeight
		if sentiment > 1 {
			sentiment = 1
		}
		if sentiment < -1 {
			sentiment = -1
		}
		impact := acc.impactWeight / float64(acc.count)
		if impact > 1 {
			impact = 1
		}
		snapshot.SymbolSignals[symbol] = SymbolSignal{
			Sentiment:     sentiment,
			Impact:        impact,
			HeadlineCount: acc.count,
			LastHeadline:  acc.lastTitle,
			LastPublished: acc.lastTime,
		}
	}

	if marketImpactWeight > 0 {
		snapshot.MarketSentiment = marketSentWeighted / marketImpactWeight
		if snapshot.MarketSentiment > 1 {
			snapshot.MarketSentiment = 1
		}
		if snapshot.MarketSentiment < -1 {
			snapshot.MarketSentiment = -1
		}
		snapshot.MarketImpact = marketImpactWeight / float64(max(1, totalHeadlines))
		if snapshot.MarketImpact > 1 {
			snapshot.MarketImpact = 1
		}
	}
	snapshot.HeadlineCount = totalHeadlines
	return snapshot, nil
}

func (p *RSSProvider) fetchSymbol(symbol string) ([]rssItem, error) {
	u := "https://feeds.finance.yahoo.com/rss/2.0/headline?s=" + url.QueryEscape(symbol) + "&region=US&lang=en-US"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "northstar-news-risk/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var parsed rssEnvelope
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Channel.Items, nil
}

func parsePubDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func buildWatchlist(symbols []string) []string {
	seen := make(map[string]struct{}, 24)
	out := make([]string, 0, 24)
	// Always include market proxies for broad sentiment.
	base := []string{"SPY", "QQQ", "IWM", "DIA"}
	for _, s := range base {
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, raw := range symbols {
		s := strings.ToUpper(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out[4:])
	// Keep network cost bounded per cycle.
	if len(out) > 14 {
		out = out[:14]
	}
	return out
}

func isMarketSymbol(symbol string) bool {
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "SPY", "QQQ", "IWM", "DIA":
		return true
	default:
		return false
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
