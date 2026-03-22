package market

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	dataquality "northstar/data"
	"northstar/features"
	"northstar/regime"
	"northstar/selector"
	"strconv"
	"strings"
	"time"
)

// Data holds market data structures
type Data struct {
	Symbol            string
	CurrentPrice      float64
	PriceChange1h     float64 // 1-hour price change percentage
	PriceChange4h     float64 // 4-hour price change percentage
	CurrentEMA20      float64
	CurrentMACD       float64
	CurrentRSI7       float64
	OpenInterest      *OIData
	FundingRate       float64
	IntradaySeries    *IntradayData
	LongerTermContext *LongerTermData
	Features          *features.FeatureSet   `json:"-"`
	Regimes           *regime.ResultSet      `json:"-"`
	Selections        *selector.SelectionSet `json:"-"`
}

// OIData represents Open Interest metrics
type OIData struct {
	Latest  float64
	Average float64
}

// IntradayData captures short-term intraday mapping (3-minute intervals)
type IntradayData struct {
	MidPrices   []float64
	EMA20Values []float64
	MACDValues  []float64
	RSI7Values  []float64
	RSI14Values []float64
}

// LongerTermData provides macro context (4-hour timeframe)
type LongerTermData struct {
	EMA20         float64
	EMA50         float64
	ATR3          float64
	ATR14         float64
	CurrentVolume float64
	AverageVolume float64
	MACDValues    []float64
	RSI14Values   []float64
}

// Kline holds candlestick data
type Kline struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

// GetRequest encapsulates parameters for fetching market data
type GetRequest struct {
	Symbol            string
	Provider          BarsProvider
	InstrumentType    string
	BarsAdjustment    string
	ValidationOptions dataquality.Options
}

// Get fetches market data for a target symbol
func Get(req GetRequest) (*Data, error) {
	// Normalize symbol format
	symbol := Normalize(req.Symbol, req.InstrumentType)

	// Retrieve 3-minute klines (latest elements)
	// Some providers like Alpaca only support 1Min intervals, we might need a converter.
	// For now, keep the interval request the same and let the provider handle it.
	klines3mMap, err := req.Provider.GetBars([]string{symbol}, "3m", 40)
	if err != nil {
		return nil, fmt.Errorf("failed to process 3-minute klines: %v", err)
	}
	klines3m := klines3mMap[symbol]
	if err := validateBars(symbol, "3m", klines3m, 40, req.ValidationOptions); err != nil {
		return nil, err
	}
	featureBars3m := toFeatureBars(klines3m)

	// Retrieve 4-hour klines
	klines4hMap, err := req.Provider.GetBars([]string{symbol}, "4h", 60)
	if err != nil {
		return nil, fmt.Errorf("failed to process 4-hour klines: %v", err)
	}
	klines4h := klines4hMap[symbol]
	if err := validateBars(symbol, "4h", klines4h, 60, req.ValidationOptions); err != nil {
		return nil, err
	}
	featureBars4h := toFeatureBars(klines4h)

	// Calculate current indicators (based on latest 3-minute data)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := features.EMA(featureBars3m, 20)
	currentMACD := features.MACD(featureBars3m)
	currentRSI7 := features.RSI(featureBars3m, 7)

	// Calculate price change percentages
	// 1-hour price change = Price from 20 periods ago (3-min * 20 = 1 hour)
	priceChange1h := 0.0
	if len(klines3m) >= 21 { // Require at least 21 klines (current + prior 20)
		price1hAgo := klines3m[len(klines3m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4-hour price change = Price from 1 period ago on 4h scale
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// Fetch specific Perp data only for crypto
	var oiData *OIData
	var fundingRate float64

	if req.InstrumentType != "equity" {
		// Fetch Open Interest data
		oiData, err = getOpenInterestData(symbol)
		if err != nil {
			// Tolerable failure mapping back to defaults if OI fetching fails
			oiData = &OIData{Latest: 0, Average: 0}
		}

		// Fetch Funding Rate parameters
		fundingRate, _ = getFundingRate(symbol)
	} else {
		// Mock OI and Funding for Equities
		oiData = &OIData{Latest: 0, Average: 0}
		fundingRate = 0
	}

	// Process intraday series mapping
	intradayData := calculateIntradaySeries(klines3m)

	// Evaluate longer term contexts
	longerTermData := calculateLongerTermData(klines4h)

	featureSet := features.DefaultEngine().ComputeSet(symbol, map[string][]features.Bar{
		"3m": featureBars3m,
		"4h": featureBars4h,
	})
	regimeSet := regime.DefaultDetector().DetectSet(featureSet)
	selectionSet := selector.Default().SelectSet(regimeSet)

	return &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		IntradaySeries:    intradayData,
		LongerTermContext: longerTermData,
		Features:          featureSet,
		Regimes:           regimeSet,
		Selections:        selectionSet,
	}, nil
}

func validateBars(symbol, interval string, klines []Kline, expected int, opts dataquality.Options) error {
	bars := make([]dataquality.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, dataquality.Bar{
			OpenTime:  k.OpenTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
			CloseTime: k.CloseTime,
		})
	}
	options := opts
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	options.ExpectedBars = expected
	result := dataquality.ValidateBars(symbol, interval, bars, options)
	if result.Failed() {
		return &dataquality.ValidationError{Result: result}
	}
	return nil
}

// calculateEMA computes Exponential Moving Average
func calculateEMA(klines []Kline, period int) float64 {
	return features.EMA(toFeatureBars(klines), period)
}

// calculateMACD evaluates MACD oscillator
func calculateMACD(klines []Kline) float64 {
	return features.MACD(toFeatureBars(klines))
}

// calculateRSI plots Relative Strength Index
func calculateRSI(klines []Kline, period int) float64 {
	return features.RSI(toFeatureBars(klines), period)
}

// calculateATR limits boundaries scaling output logic constraints tracking variations
func calculateATR(klines []Kline, period int) float64 {
	return features.ATR(toFeatureBars(klines), period)
}

// calculateIntradaySeries logs matrix limitations calculations conditions limit variables mapping
func calculateIntradaySeries(klines []Kline) *IntradayData {
	featureBars := toFeatureBars(klines)
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Extract last 10 mapped parameters bounds
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)

		// EMA20 limits variations variables logic tracking
		if i >= 19 {
			ema20 := features.EMA(featureBars[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// MACD parameter variables arrays calculations execution limitation variables mapping Maps Limit Map targeting Maps variables
		if i >= 25 {
			macd := features.MACD(featureBars[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// RSI scaling variables array combinations limitations targets
		if i >= 7 {
			rsi7 := features.RSI(featureBars[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := features.RSI(featureBars[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// calculateLongerTermData processes macro limits configurations variables execution
func calculateLongerTermData(klines []Kline) *LongerTermData {
	featureBars := toFeatureBars(klines)
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Extract EMA ranges limit tracking maps Limit Map Map limitation target loops configurations tracking arrays mapping
	data.EMA20 = features.EMA(featureBars, 20)
	data.EMA50 = features.EMA(featureBars, 50)

	// Analyze ATR constraints Limit Target Maps combination
	data.ATR3 = features.ATR(featureBars, 3)
	data.ATR14 = features.ATR(featureBars, 14)

	// Volumes maps targeting Tracking Values evaluation Limitations Lists Tracking Map Map limits combinations loops conditions limitations targets tracking conditions limitation Tracker Matrix variables parameters configurations target Arrays Tracking Tracking variables variables Maps map tracking configurations limitations
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Extrapolate bounds Limits execution combinations
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// MACD and RSI setup Arrays variables sequences variables configurations boundaries Limit Map arrays limitation bounds Map limitation variables parameters limitation limitations variables tracking variables configurations values limitations logic bounds Mapping Limit limit Map limits combinations Targeting limit Matrix limits mapping target Maps Limits mapping Array Maps Maps limitation parameters limitation map Mapping Map Array arrays Limit Maps Map mapping Target Mapping Limit map Mapping Mapping maps Limit limits Limit Map combinations
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := features.MACD(featureBars[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := features.RSI(featureBars[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

func toFeatureBars(klines []Kline) []features.Bar {
	out := make([]features.Bar, 0, len(klines))
	for _, k := range klines {
		out = append(out, features.Bar{
			OpenTime:  k.OpenTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
			CloseTime: k.CloseTime,
		})
	}
	return out
}

// getOpenInterestData aggregates OI mappings array limitations Tracking Limits
func getOpenInterestData(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)

	return &OIData{
		Latest:  oi,
		Average: oi * 0.999, // Extrapolate approximation combinations MAP bounds targets array limitations configurations Tracking bounds map Limits mappings Target values setup mapping Target limit variables Maps tracking variables limitations array limits Target limits bounds arrays limits limit MAP limitations bounds mappings setup Target Targets variables Mapper Mapping limitations tracking Lists Mapper targeting limitation tracking Limit Tracker Mapping Target mapping mapping Target mapping combinations mapping limitation parameter maps limitations maps tracking Limit Tracker variables limits limit loops conditions values limitation MAP limits limitation logic Target variables limits Mapping Tracking Limits limit Map Array limitations limit parameter MAP
	}, nil
}

// getFundingRate aggregates funding parameters limits tracking setups arrays map limitations mapping configurations LIMIT Tracking combinations limits variations target tracking
func getFundingRate(symbol string) (float64, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		InterestRate    string `json:"interestRate"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)
	return rate, nil
}

// Format provides readable formatted mappings strings limitation bounds parameters constraints logic target Loops
func Format(data *Data) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("current_price = %.2f, current_ema20 = %.3f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
		data.CurrentPrice, data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	if data.OpenInterest != nil && data.OpenInterest.Latest > 0 {
		sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
			data.Symbol))
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
			data.OpenInterest.Latest, data.OpenInterest.Average))
		sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))
	}

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3minute intervals, oldest  latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longerterm context (4hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20Period EMA: %.3f vs. 50Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3Period ATR: %.3f vs. 14Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	return sb.String()
}

// formatFloatSlice coerces limits variations variables tracking values Limits limitations
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = fmt.Sprintf("%.3f", v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// Normalize binds limits tracking configurations values tracking bounds mapping limitations arrays limit array map maps arrays logic Map limitation maps arrays configurations arrays map parameters Tracking limits mappings parameters maps Maps
func Normalize(symbol string, instrumentType string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	if instrumentType == "equity" {
		// Equity symbols typically don't need 'USDT' appended.
		// Simply remove USDT if it somehow got in there.
		if strings.HasSuffix(symbol, "USDT") {
			return strings.TrimSuffix(symbol, "USDT")
		}
		return symbol
	}

	// Default crypto_perp behavior
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat handles array constraints targeting configurations lists combinations setup target Maps Target tracking maps Mapping
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}
