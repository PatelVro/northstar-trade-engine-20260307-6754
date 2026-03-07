package market

import (
	"fmt"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
)

// AlpacaProvider implements BarsProvider and QuoteProvider using Alpaca's Market Data v2 API.
type AlpacaProvider struct {
	client *marketdata.Client
}

// NewAlpacaProvider creates a new AlpacaProvider.
// If apiKey and apiSecret are empty, it will rely on APCA_API_KEY_ID and APCA_API_SECRET_KEY env vars.
func NewAlpacaProvider(apiKey, apiSecret string) *AlpacaProvider {
	client := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})
	return &AlpacaProvider{
		client: client,
	}
}

// GetBars fetches multi-symbol historical bars from Alpaca and converts them to the internal Kline format.
func (p *AlpacaProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols provided")
	}

	// Default to 1Min if not specified or if mapping is complex, 
	// for a real trading system you'd map "1m" -> marketdata.OneMin, "5m" -> marketdata.FiveMin etc.
	// We'll stick to 1Min for the granularity and calculate larger timeframes if necessary.
	timeframe := marketdata.OneMin
	
	// If the interval requested is higher, we could either request that TF directly 
	// or request 1Min and aggregate. Alpaca supports various TFs directly.
	switch interval {
	case "3m":
		timeframe = marketdata.NewTimeFrame(3, marketdata.Min)
	case "4h":
		timeframe = marketdata.NewTimeFrame(4, marketdata.Hour)
	case "1d":
		timeframe = marketdata.NewTimeFrame(1, marketdata.Day)
	}

	// Calculate a start time far enough back to guarantee we get `limit` bars.
	// Since markets are only open 6.5h/day, a naive hours subtraction might omit weekends/nights.
	// 5 days should be enough for most small limits.
	daysBack := (limit / 390) + 5 // 390 = 6.5h * 60m
	if daysBack < 5 {
		daysBack = 5
	}
	start := time.Now().AddDate(0, 0, -daysBack)

	req := marketdata.GetBarsRequest{
		TimeFrame: timeframe,
		Start:     start,
		End:       time.Now(),
		PageLimit: limit,
		// Adjustment: marketdata.AdjustmentSplit, // You can configure this based on bars_adjustment config
	}

	// Clean symbols (remove USDT if present)
	cleanSymbols := make([]string, len(symbols))
	for i, s := range symbols {
		cleanSymbols[i] = strings.TrimSuffix(strings.ToUpper(s), "USDT")
	}

	bars, err := p.client.GetMultiBars(cleanSymbols, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bars from alpaca: %w", err)
	}

	result := make(map[string][]Kline)

	for _, symbol := range symbols {
		cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
		alpacaBars := bars[cleanSymbol]
		
		klines := make([]Kline, 0, len(alpacaBars))
		for _, b := range alpacaBars {
			openTime := b.Timestamp.UnixMilli()
			
			// Estimate CloseTime based on the interval.
			var durationMs int64
			switch timeframe.Unit {
			case marketdata.Min:
				durationMs = int64(timeframe.N) * 60 * 1000
			case marketdata.Hour:
				durationMs = int64(timeframe.N) * 60 * 60 * 1000
			case marketdata.Day:
				durationMs = int64(timeframe.N) * 24 * 60 * 60 * 1000
			}
			closeTime := openTime + durationMs - 1

			klines = append(klines, Kline{
				OpenTime:  openTime,
				Open:      b.Open,
				High:      b.High,
				Low:       b.Low,
				Close:     b.Close,
				Volume:    float64(b.Volume),
				CloseTime: closeTime,
			})
		}
		
		// Ensure we don't return more than requested if Alpaca API returned extra
		if len(klines) > limit {
			klines = klines[len(klines)-limit:]
		}
		
		result[symbol] = klines
	}

	return result, nil
}

// GetLatestQuote fetches the latest bid/ask for a symbol from Alpaca.
func (p *AlpacaProvider) GetLatestQuote(symbol string) (*Quote, error) {
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")
	
	quote, err := p.client.GetLatestQuote(cleanSymbol, marketdata.GetLatestQuoteRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get latest quote for %s: %w", symbol, err)
	}

	return &Quote{
		BidPrice: quote.BidPrice,
		BidSize:  float64(quote.BidSize),
		AskPrice: quote.AskPrice,
		AskSize:  float64(quote.AskSize),
	}, nil
}
