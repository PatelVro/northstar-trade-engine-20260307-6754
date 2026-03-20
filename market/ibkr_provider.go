package market

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"northstar/broker"
	"strconv"
	"strings"
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
	if limit <= 0 {
		limit = 200
	}

	result := make(map[string][]Kline)

	ibkrBar, ibkrPeriod, aggregateBucket, fetchLimit := resolveIBKRInterval(interval, limit)

	for _, symbol := range symbols {
		// 1. Resolve Contract using the Broker Client
		conid, err := p.Client.ResolveContract(symbol)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve contract for bar data: %w", err)
		}

		// Wait before querying to prevent aggressive pacing immediately after a secdef search
		time.Sleep(100 * time.Millisecond)

		historyURL, err := url.Parse(p.Client.BaseURL + "/iserver/marketdata/history")
		if err != nil {
			return nil, err
		}
		q := historyURL.Query()
		q.Set("conid", strconv.Itoa(conid))
		q.Set("period", ibkrPeriod)
		q.Set("bar", ibkrBar)
		historyURL.RawQuery = q.Encode()

		req, err := http.NewRequest("GET", historyURL.String(), nil)
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
		barDuration := ibkrBarDurationMillis(ibkrBar)
		for _, bar := range histResp.Data {
			openTime := bar.T
			closeTime := openTime + barDuration - 1

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

		// Keep enough base bars for aggregation, then downsample as needed.
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

func resolveIBKRInterval(interval string, limit int) (ibkrBar string, ibkrPeriod string, aggregateBucket int, fetchLimit int) {
	if limit <= 0 {
		limit = 200
	}
	interval = strings.ToLower(strings.TrimSpace(interval))

	ibkrBar = "1 min"
	ibkrPeriod = "1d"
	aggregateBucket = 1
	fetchLimit = limit

	switch interval {
	case "1m":
		if fetchLimit > 300 {
			ibkrPeriod = "3d"
		}
	case "3m":
		aggregateBucket = 3
		fetchLimit = limit * aggregateBucket
		// IBKR's 1d minute-bar history only includes the current session, which starves
		// early-session 3m aggregation. Use a multi-day lookback so the 3m feature
		// engine has enough bars immediately after the open.
		ibkrPeriod = "3d"
	case "5m":
		ibkrBar = "5 min"
		ibkrPeriod = "5d"
	case "1h":
		ibkrBar = "1 hour"
		ibkrPeriod = "1m"
	case "4h":
		ibkrBar = "1 hour"
		ibkrPeriod = "1m"
		aggregateBucket = 4
		fetchLimit = limit * aggregateBucket
	case "1d":
		ibkrBar = "1 day"
		ibkrPeriod = "1y"
	default:
		// Keep safe defaults for unknown intervals.
		if strings.HasSuffix(interval, "h") {
			ibkrBar = "1 hour"
			ibkrPeriod = "1m"
		}
	}

	if fetchLimit < 20 {
		fetchLimit = 20
	}
	return ibkrBar, ibkrPeriod, aggregateBucket, fetchLimit
}

func ibkrBarDurationMillis(ibkrBar string) int64 {
	switch strings.ToLower(strings.TrimSpace(ibkrBar)) {
	case "1 min":
		return 60_000
	case "5 min":
		return 5 * 60_000
	case "1 hour":
		return 60 * 60_000
	case "1 day":
		return 24 * 60 * 60_000
	default:
		return 60_000
	}
}

func aggregateKlines(klines []Kline, bucket int) []Kline {
	if bucket <= 1 || len(klines) == 0 {
		return klines
	}

	out := make([]Kline, 0, (len(klines)+bucket-1)/bucket)
	for i := 0; i < len(klines); i += bucket {
		end := i + bucket
		if end > len(klines) {
			end = len(klines)
		}
		chunk := klines[i:end]
		first := chunk[0]
		last := chunk[len(chunk)-1]

		high := first.High
		low := first.Low
		volume := 0.0
		for _, k := range chunk {
			if k.High > high {
				high = k.High
			}
			if k.Low < low {
				low = k.Low
			}
			volume += k.Volume
		}

		out = append(out, Kline{
			OpenTime:  first.OpenTime,
			Open:      first.Open,
			High:      high,
			Low:       low,
			Close:     last.Close,
			Volume:    volume,
			CloseTime: last.CloseTime,
		})
	}
	return out
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
		Conid    int    `json:"conid"`
		BidPrice string `json:"84"`
		AskPrice string `json:"86"`
		BidSize  string `json:"88"`
		AskSize  string `json:"85"`
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
