package market

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

// BinanceProvider implements the BarsProvider interface using a Binance-compatible
// klines endpoint. By default hits Binance Futures directly; set
// CRYPTO_KLINES_URL (e.g. "https://fapi.asterdex.com") to override when the
// deployment environment can't reach Binance (geo-blocks from US data centers
// like Hostinger are common; Aster serves the exact same API shape).
type BinanceProvider struct{}

// NewBinanceProvider creates a new BinanceProvider.
func NewBinanceProvider() *BinanceProvider {
	return &BinanceProvider{}
}

// klinesBaseURL returns the configured crypto klines base URL.
// Override via CRYPTO_KLINES_URL env var (no trailing slash).
func (p *BinanceProvider) klinesBaseURL() string {
	if v := os.Getenv("CRYPTO_KLINES_URL"); v != "" {
		return v
	}
	return "https://fapi.binance.com"
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
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		p.klinesBaseURL(), symbol, interval, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawData [][]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, err
	}

	klines := make([]Kline, len(rawData))
	for i, item := range rawData {
		openTime := int64(item[0].(float64))
		open, _ := parseFloat(item[1])
		high, _ := parseFloat(item[2])
		low, _ := parseFloat(item[3])
		close, _ := parseFloat(item[4])
		volume, _ := parseFloat(item[5])
		closeTime := int64(item[6].(float64))

		klines[i] = Kline{
			OpenTime:  openTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: closeTime,
		}
	}

	return klines, nil
}
