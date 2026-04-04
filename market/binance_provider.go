package market

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// BinanceProvider implements the BarsProvider interface using Binance Futures API.
type BinanceProvider struct{}

// NewBinanceProvider creates a new BinanceProvider.
func NewBinanceProvider() *BinanceProvider {
	return &BinanceProvider{}
}

// GetBars fetches historical OHLCV data from Binance.
// Note: Binance doesn't support multi-symbol fetching in a single call for klines,
// so this implementation fetches them sequentially or relies on the caller to handle concurrency.
func (p *BinanceProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	result := make(map[string][]Kline)

	for _, symbol := range symbols {
		klines, err := p.getKlines(symbol, interval, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to get klines for %s: %w", symbol, err)
		}
		result[symbol] = klines
	}

	return result, nil
}

// getKlines is the original Binance implementation.
func (p *BinanceProvider) getKlines(symbol, interval string, limit int) ([]Kline, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		symbol, interval, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawData [][]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, err
	}

	klines := make([]Kline, 0, len(rawData))
	for _, item := range rawData {
		if len(item) < 7 {
			return nil, fmt.Errorf("unexpected kline item length: got %d, want at least 7", len(item))
		}
		openTimeF, ok := item[0].(float64)
		if !ok {
			return nil, fmt.Errorf("kline open time is not a number: %T", item[0])
		}
		closeTimeF, ok := item[6].(float64)
		if !ok {
			return nil, fmt.Errorf("kline close time is not a number: %T", item[6])
		}
		open, err := parseFloat(item[1])
		if err != nil {
			return nil, fmt.Errorf("kline open price parse error: %w", err)
		}
		high, err := parseFloat(item[2])
		if err != nil {
			return nil, fmt.Errorf("kline high price parse error: %w", err)
		}
		low, err := parseFloat(item[3])
		if err != nil {
			return nil, fmt.Errorf("kline low price parse error: %w", err)
		}
		closePrice, err := parseFloat(item[4])
		if err != nil {
			return nil, fmt.Errorf("kline close price parse error: %w", err)
		}
		volume, err := parseFloat(item[5])
		if err != nil {
			return nil, fmt.Errorf("kline volume parse error: %w", err)
		}

		klines = append(klines, Kline{
			OpenTime:  int64(openTimeF),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: int64(closeTimeF),
		})
	}

	return klines, nil
}
