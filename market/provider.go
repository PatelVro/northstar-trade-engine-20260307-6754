package market

// BarsProvider is the interface for fetching historical OHLCV data.
type BarsProvider interface {
	GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error)
}

// Quote represents the latest bid and ask prices.
type Quote struct {
	BidPrice float64
	BidSize  float64
	AskPrice float64
	AskSize  float64
}

// QuoteProvider is the interface for fetching the latest quote.
type QuoteProvider interface {
	GetLatestQuote(symbol string) (*Quote, error)
}
