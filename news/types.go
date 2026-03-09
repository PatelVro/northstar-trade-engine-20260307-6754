package news

import "time"

// SymbolSignal summarizes news tone and impact for one symbol.
type SymbolSignal struct {
	Sentiment     float64   // [-1, 1], negative means bearish
	Impact        float64   // [0, 1], event intensity
	HeadlineCount int       // Number of recent headlines considered
	LastHeadline  string    // Most recent title for debugging
	LastPublished time.Time // Timestamp of most recent title
}

// Snapshot contains aggregated market and per-symbol news state.
type Snapshot struct {
	GeneratedAt     time.Time
	Lookback        time.Duration
	Provider        string
	MarketSentiment float64
	MarketImpact    float64
	HeadlineCount   int
	SymbolSignals   map[string]SymbolSignal
}

// Provider fetches and aggregates finance news signals.
type Provider interface {
	Fetch(symbols []string, lookback time.Duration) (*Snapshot, error)
	Name() string
}
