package market

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// SyntheticProvider implements BarsProvider with realistic synthetic OHLCV data.
// It generates price series with trends, mean-reversion, and volatility clustering
// to simulate a live market environment for end-to-end testing.
type SyntheticProvider struct {
	mu          sync.Mutex
	rng         *rand.Rand
	prices      map[string]float64 // Current price per symbol
	volatility  map[string]float64 // Current volatility regime per symbol
	trend       map[string]float64 // Current trend direction per symbol
	initialized bool
}

// symbolSeeds maps symbols to realistic starting prices
var symbolSeeds = map[string]float64{
	"AAPL": 218.50, "MSFT": 420.30, "NVDA": 138.75, "GOOGL": 168.20,
	"AMZN": 205.80, "META": 610.40, "TSLA": 275.60, "AVGO": 185.50,
	"JPM": 245.30, "V": 315.70, "UNH": 510.20, "MA": 520.80,
	"HD": 395.10, "PG": 172.40, "JNJ": 158.60, "ABBV": 190.30,
	"MRK": 105.80, "CRM": 310.50, "ORCL": 175.40, "ADBE": 480.20,
	"NFLX": 975.30, "AMD": 118.60, "INTC": 24.80, "CSCO": 60.70,
	"PEP": 148.20, "KO": 62.40, "WMT": 92.30, "DIS": 108.50,
	"BA": 175.20, "GS": 590.40,
}

// NewSyntheticProvider creates a synthetic market data provider
func NewSyntheticProvider() *SyntheticProvider {
	return &SyntheticProvider{
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		prices:     make(map[string]float64),
		volatility: make(map[string]float64),
		trend:      make(map[string]float64),
	}
}

func (p *SyntheticProvider) initSymbol(symbol string) {
	if _, ok := p.prices[symbol]; ok {
		return
	}
	base, ok := symbolSeeds[symbol]
	if !ok {
		// Generate a reasonable price for unknown symbols
		base = 50 + p.rng.Float64()*450
	}
	// Add some randomness to the starting price (±5%)
	p.prices[symbol] = base * (0.95 + p.rng.Float64()*0.10)
	p.volatility[symbol] = 0.001 + p.rng.Float64()*0.002 // 0.1%-0.3% per bar
	p.trend[symbol] = (p.rng.Float64() - 0.5) * 0.001    // slight trend
}

// GetBars generates synthetic OHLCV bars for the requested symbols
func (p *SyntheticProvider) GetBars(symbols []string, interval string, limit int) (map[string][]Kline, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make(map[string][]Kline, len(symbols))
	now := time.Now()

	// Determine bar duration from interval string
	barDuration := 3 * time.Minute // default 3m
	switch interval {
	case "1m":
		barDuration = time.Minute
	case "3m":
		barDuration = 3 * time.Minute
	case "5m":
		barDuration = 5 * time.Minute
	case "15m":
		barDuration = 15 * time.Minute
	case "1h":
		barDuration = time.Hour
	case "4h":
		barDuration = 4 * time.Hour
	case "1d":
		barDuration = 24 * time.Hour
	}

	for _, symbol := range symbols {
		p.initSymbol(symbol)
		bars := p.generateBars(symbol, limit, barDuration, now)
		result[symbol] = bars

		// Update current price to the last bar's close
		if len(bars) > 0 {
			p.prices[symbol] = bars[len(bars)-1].Close
		}
	}

	return result, nil
}

func (p *SyntheticProvider) generateBars(symbol string, count int, barDuration time.Duration, endTime time.Time) []Kline {
	bars := make([]Kline, count)
	price := p.prices[symbol]
	vol := p.volatility[symbol]
	trend := p.trend[symbol]

	// Walk backward from endTime to generate historical bars, then reverse
	startTime := endTime.Add(-time.Duration(count) * barDuration)

	for i := 0; i < count; i++ {
		barStart := startTime.Add(time.Duration(i) * barDuration)
		barEnd := barStart.Add(barDuration)

		// Evolve volatility (mean-reverting)
		vol = vol*0.95 + 0.002*0.05 + p.rng.NormFloat64()*0.0003
		if vol < 0.0005 {
			vol = 0.0005
		}
		if vol > 0.008 {
			vol = 0.008
		}

		// Evolve trend (slow random walk)
		trend += p.rng.NormFloat64() * 0.0002
		trend *= 0.98 // mean-revert toward 0

		// Generate OHLC using geometric brownian motion
		open := price
		returns := trend + p.rng.NormFloat64()*vol
		close := open * (1 + returns)

		// Generate realistic high/low
		intraVol := vol * 1.5
		high := math.Max(open, close) * (1 + math.Abs(p.rng.NormFloat64())*intraVol)
		low := math.Min(open, close) * (1 - math.Abs(p.rng.NormFloat64())*intraVol)

		// Sanity: high >= max(open, close), low <= min(open, close)
		if high < math.Max(open, close) {
			high = math.Max(open, close) * 1.001
		}
		if low > math.Min(open, close) {
			low = math.Min(open, close) * 0.999
		}

		// Generate volume (higher when volatile)
		baseVolume := 50000 + p.rng.Float64()*200000
		volume := baseVolume * (1 + vol*500)

		bars[i] = Kline{
			OpenTime:  barStart.UnixMilli(),
			Open:      math.Round(open*100) / 100,
			High:      math.Round(high*100) / 100,
			Low:       math.Round(low*100) / 100,
			Close:     math.Round(close*100) / 100,
			Volume:    math.Round(volume),
			CloseTime: barEnd.UnixMilli(),
		}

		price = close
	}

	// Update state
	p.volatility[symbol] = vol
	p.trend[symbol] = trend

	return bars
}
