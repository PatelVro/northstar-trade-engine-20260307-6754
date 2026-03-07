package market

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"aegistrade/broker"
	"time"
)

// IBKRProvider implements BarsProvider and QuoteProvider using IBKR Client Portal API
type IBKRProvider struct {
	Client *broker.IBKRClient
}

// NewIBKRProvider sets up the IBKR Gateway provider.
func NewIBKRProvider(baseURL, accountID, sessionCookie string) *IBKRProvider {
	return &IBKRProvider{
		Client: broker.NewIBKRClient(baseURL, accountID, sessionCookie),
	}
}

// getConID resolves a ticker symbol (e.g., AAPL) to an IBKR Contract ID (conid)
func (p *IBKRProvider) getConID(symbol string) (int, error) {
	return p.Client.ResolveContract(symbol)
}

func (p *IBKRProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols provided")
	}

	result := make(map[string][]Kline)

	// 2. Map Interval
	// IBKR accepts "1min", "5min", "1h", "1d"
	ibkrPeriod := "1d"
	ibkrBar := "1 min"
	switch interval {
	case "1m":
		ibkrBar = "1 min"
		if limit > 300 {
			ibkrPeriod = "3d" // IBKR max for 1min requests usually
		}
	case "5m":
		ibkrBar = "5 min"
		ibkrPeriod = "5d"
	case "1h":
		ibkrBar = "1 hour"
		ibkrPeriod = "1m"
	case "1d":
		ibkrBar = "1 day"
		ibkrPeriod = "1y"
	}

	for _, symbol := range symbols {
		// 1. Resolve Contract using the Broker Client
		conid, err := p.Client.ResolveContract(symbol)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve contract for bar data: %w", err)
		}

		// Wait before querying to prevent aggressive pacing immediately after a secdef search
		time.Sleep(100 * time.Millisecond)

		url := fmt.Sprintf("%s/iserver/marketdata/history?conid=%d&period=%s&bar=%s", p.Client.BaseURL, conid, ibkrPeriod, ibkrBar)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := p.Client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("IBKR history error (status %d): %s", resp.StatusCode, string(body))
		}

		// 3. Parse Response
		var histResp struct {
			Data []struct {
				T int64   `json:"t"` // timestamp ms
				O float64 `json:"o"`
				H float64 `json:"h"`
				L float64 `json:"l"`
				C float64 `json:"c"`
				V float64 `json:"v"`
			} `json:"data"`
		}

		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &histResp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		// 4. Map to Standard Klines
		var klines []Kline
		for _, bar := range histResp.Data {
			openTime := bar.T
			closeTime := openTime + 60000 - 1
			
			klines = append(klines, Kline{
				OpenTime:  openTime,
				Open:      bar.O,
				High:      bar.H,
				Low:       bar.L,
				Close:     bar.C,
				Volume:    bar.V,
				CloseTime: closeTime,
			})
		}

		// Slice to limit
		if len(klines) > limit {
			klines = klines[len(klines)-limit:]
		}

		result[symbol] = klines
	}

	return result, nil
}

// GetLatestQuote fetches the latest snapshot for a symbol
func (p *IBKRProvider) GetLatestQuote(symbol string) (*Quote, error) {
	conid, err := p.getConID(symbol)
	if err != nil {
		return nil, fmt.Errorf("error resolving conid for %s: %w", symbol, err)
	}

	// We must request the conid snapshot and include fields 84 (BidPrice), 86 (AskPrice), 88 (BidSize), 85 (AskSize)
	url := fmt.Sprintf("%s/iserver/marketdata/snapshot?conids=%d&fields=84,86,88,85", p.Client.BaseURL, conid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IBKR snapshot error for %s: status %d", symbol, resp.StatusCode)
	}

	var snapshots []struct {
		Conid    int     `json:"conid"`
		BidPrice string  `json:"84"`
		AskPrice string  `json:"86"`
		BidSize  string  `json:"88"`
		AskSize  string  `json:"85"`
	}

	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot response: %w", err)
	}

	if len(snapshots) == 0 {
		return nil, fmt.Errorf("no snapshot data returned for %s", symbol)
	}

	snap := snapshots[0]
	
	// Convert strings to floats
	var bid, ask, bidSz, askSz float64
	fmt.Sscanf(snap.BidPrice, "%f", &bid)
	fmt.Sscanf(snap.AskPrice, "%f", &ask)
	fmt.Sscanf(snap.BidSize, "%f", &bidSz)
	fmt.Sscanf(snap.AskSize, "%f", &askSz)

	return &Quote{
		BidPrice: bid,
		AskPrice: ask,
		BidSize:  bidSz,
		AskSize:  askSz,
	}, nil
}
