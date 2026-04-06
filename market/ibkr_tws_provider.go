package market

import (
	"fmt"
	"northstar/broker"
	"strings"
	"time"
)

// IBKRTWSProvider implements BarsProvider and QuoteProvider using IB Gateway's TWS API.
// This replaces IBKRProvider (Client Portal REST API) with a socket-based connection
// that doesn't require browser login or session management.
type IBKRTWSProvider struct {
	Client *broker.IBKRTWSClient
}

// NewIBKRTWSProvider creates a new TWS-based market data provider and connects to IB Gateway.
func NewIBKRTWSProvider(host string, port int, clientID int64, accountID string) (*IBKRTWSProvider, error) {
	client := broker.NewIBKRTWSClient(host, port, clientID, accountID)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to IB Gateway: %w", err)
	}
	return &IBKRTWSProvider{Client: client}, nil
}

func (p *IBKRTWSProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols provided")
	}
	if limit <= 0 {
		limit = 200
	}

	barSize, duration, aggregateBucket := broker.ResolveTWSInterval(interval, limit)
	result := make(map[string][]Kline)

	for _, symbol := range symbols {
		conID, err := p.Client.ResolveContract(symbol)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve contract for %s: %w", symbol, err)
		}

		// Small delay between symbols to avoid pacing violations
		if len(result) > 0 {
			time.Sleep(200 * time.Millisecond)
		}

		bars, err := p.Client.GetHistoricalBars(symbol, conID, barSize, duration, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to get historical bars for %s: %w", symbol, err)
		}

		klines := twsBarsToKlines(bars, barSize)

		// Trim to fetchLimit for aggregation, then aggregate, then trim to requested limit
		fetchLimit := limit
		if aggregateBucket > 1 {
			fetchLimit = limit * aggregateBucket
		}
		if len(klines) > fetchLimit {
			klines = klines[len(klines)-fetchLimit:]
		}
		if aggregateBucket > 1 {
			klines = aggregateKlines(klines, aggregateBucket)
		}
		if len(klines) > limit {
			klines = klines[len(klines)-limit:]
		}

		result[symbol] = klines
	}

	return result, nil
}

func (p *IBKRTWSProvider) GetLatestQuote(symbol string) (*Quote, error) {
	conID, err := p.Client.ResolveContract(symbol)
	if err != nil {
		return nil, fmt.Errorf("error resolving conid for %s: %w", symbol, err)
	}

	bid, ask, bidSz, askSz, err := p.Client.GetSnapshot(symbol, conID)
	if err != nil {
		return nil, err
	}

	return &Quote{
		BidPrice: bid,
		AskPrice: ask,
		BidSize:  bidSz,
		AskSize:  askSz,
	}, nil
}

// twsBarsToKlines converts TWS historical bar results to standard Klines.
// TWS returns dates as "yyyyMMdd HH:mm:ss" for intraday or "yyyyMMdd" for daily.
func twsBarsToKlines(bars []broker.HistoricalBarResult, barSize string) []Kline {
	barDurationMs := twsBarDurationMillis(barSize)
	klines := make([]Kline, 0, len(bars))

	for _, bar := range bars {
		openTime := parseTWSDate(bar.Date)
		if openTime == 0 {
			continue
		}
		closeTime := openTime + barDurationMs - 1

		klines = append(klines, Kline{
			OpenTime:  openTime,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
			CloseTime: closeTime,
		})
	}

	return klines
}

func parseTWSDate(dateStr string) int64 {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return 0
	}

	// TWS intraday format: "20240102 09:30:00" or epoch seconds
	layouts := []string{
		"20060102 15:04:05",
		"20060102  15:04:05", // double space variant
		"20060102",
	}

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, dateStr, loc); err == nil {
			return t.UnixMilli()
		}
	}

	// Try parsing as epoch seconds (some TWS versions return this)
	var epoch int64
	if _, err := fmt.Sscanf(dateStr, "%d", &epoch); err == nil && epoch > 1000000000 {
		return epoch * 1000 // Convert seconds to milliseconds
	}

	return 0
}

func twsBarDurationMillis(barSize string) int64 {
	switch strings.ToLower(strings.TrimSpace(barSize)) {
	case "1 min":
		return 60_000
	case "5 mins":
		return 5 * 60_000
	case "1 hour":
		return 60 * 60_000
	case "1 day":
		return 24 * 60 * 60_000
	default:
		return 60_000
	}
}
