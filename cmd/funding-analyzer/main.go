// Command funding-analyzer pulls historical funding rates from Aster for a
// set of perpetual symbols and evaluates whether a funding-rate carry trade
// would actually be profitable after realistic costs.
//
// The core question: if we'd run a delta-neutral funding harvest strategy
// over the past N days, would net PnL (funding collected minus fees and
// hedge costs) actually be positive and worth the operational complexity?
//
// Usage:
//   go run ./cmd/funding-analyzer -symbols BTCUSDT,ETHUSDT,SOLUSDT -days 180
//
// Output per symbol:
//   - Mean / median / p5 / p95 funding rate
//   - Fraction of time funding is positive / negative / near zero
//   - Annualized carry (raw, before costs)
//   - Realistic net annualized return after Aster fees + estimated hedge
//     cost + rebalancing drag
//   - Verdict: fund-able, marginal, or lose-money
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fundingPoint struct {
	Time int64
	Rate float64
}

type symbolStats struct {
	Symbol          string
	Samples         int
	FirstTime       time.Time
	LastTime        time.Time
	MeanRate        float64 // per 8h
	MedianRate      float64
	P5Rate          float64
	P95Rate         float64
	StdDev          float64
	FractionPos     float64
	FractionNeg     float64
	FractionZero    float64 // |rate| < 1e-5
	AnnualizedCarry float64 // raw: mean * 3 * 365
	// Hedged-harvest simulation (both directions):
	//   - If symbol funding is usually positive, simulate SHORT perp + LONG spot (collect funding)
	//   - If symbol funding is usually negative, simulate LONG perp + SHORT spot
	// Cost model assumes:
	//   - Entry fees 2x 4bps (Aster taker) = 8bps one round trip
	//   - Hedge asset friction: 2x 3bps = 6bps assumed for spot side
	//   - Rebalancing drag: re-hedge weekly if basis drifts, 1x 4bps/wk = ~20bps/yr
	//   - Slippage: 2x 5bps = 10bps per round trip
	NetAnnualizedLongBias  float64 // if we bias SHORT perp (collect positive funding)
	NetAnnualizedShortBias float64 // if we bias LONG perp (collect negative funding)
}

func main() {
	symbolList := flag.String("symbols", "BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,XRPUSDT,DOGEUSDT,ADAUSDT,LINKUSDT,AVAXUSDT,MATICUSDT", "Aster perp symbols to analyze")
	days := flag.Int("days", 180, "lookback window in days (funding paid every 8h, so ~3 samples/day)")
	entryFeeBps := flag.Float64("entry-fee-bps", 8.0, "total round-trip entry fees for perp leg (2x taker)")
	hedgeFeeBps := flag.Float64("hedge-fee-bps", 6.0, "round-trip cost for spot hedge")
	slippageBps := flag.Float64("slippage-bps", 10.0, "round-trip slippage estimate")
	rebalanceBpsPerYr := flag.Float64("rebalance-bps", 20.0, "annualized rebalancing drag from re-hedging")
	flag.Parse()

	symbols := splitCSV(*symbolList)
	perSample8h := 3.0 * 365.0 // samples per year (8h funding period)

	fmt.Printf("=== Aster Funding Rate Analyzer ===\n")
	fmt.Printf("Symbols:         %v\n", symbols)
	fmt.Printf("Lookback:        %d days (~%d samples)\n", *days, *days*3)
	fmt.Printf("Cost model:      perp=%.0fbps, hedge=%.0fbps, slip=%.0fbps, rebal=%.0fbps/yr\n\n",
		*entryFeeBps, *hedgeFeeBps, *slippageBps, *rebalanceBpsPerYr)

	totalCostOneTimeBps := *entryFeeBps + *hedgeFeeBps + *slippageBps
	totalCostAnnualizedBps := *rebalanceBpsPerYr

	var allStats []symbolStats
	for _, sym := range symbols {
		fmt.Printf(">> %s: fetching funding history...\n", sym)
		points, err := fetchFundingHistory(sym, *days)
		if err != nil {
			fmt.Printf("   ERROR: %v (skipping)\n", err)
			continue
		}
		if len(points) < 30 {
			fmt.Printf("   only %d samples, skipping\n", len(points))
			continue
		}
		s := analyze(sym, points)
		// Net annualized return, SHORT-PERP bias (collect positive funding).
		// If average funding is X per 8h and we short perp: we receive X per 8h
		// annualized = X * 3 * 365 = s.AnnualizedCarry (sign flipped: if we short
		// perp and funding is positive, we RECEIVE the funding from longs).
		// So collectedAnnualized = s.AnnualizedCarry (when positive = good for shorts).
		s.NetAnnualizedLongBias = s.AnnualizedCarry - (totalCostOneTimeBps/10000.0) - (totalCostAnnualizedBps / 10000.0)
		// SHORT-perp (long-bias on spot hedge): collects positive funding
		s.NetAnnualizedShortBias = -s.AnnualizedCarry - (totalCostOneTimeBps/10000.0) - (totalCostAnnualizedBps / 10000.0)
		allStats = append(allStats, s)
		fmt.Printf("   %d samples, mean=%.4f%%/8h, median=%.4f%%/8h, annualized carry=%+.2f%%\n",
			s.Samples, s.MeanRate*100, s.MedianRate*100, s.AnnualizedCarry*100)
	}

	fmt.Println()
	fmt.Println("=== Per-Symbol Analysis ===")
	fmt.Printf("%-10s %7s %9s %9s %9s %9s %7s %7s %9s %9s %9s\n",
		"Symbol", "samples", "mean%/8h", "std%/8h", "p5%/8h", "p95%/8h", "frac+", "frac-",
		"annCarry", "shortPerp", "longPerp")
	for _, s := range allStats {
		fmt.Printf("%-10s %7d %+9.4f %9.4f %+9.4f %+9.4f %6.1f%% %6.1f%% %+8.2f%% %+8.2f%% %+8.2f%%\n",
			s.Symbol, s.Samples,
			s.MeanRate*100, s.StdDev*100, s.P5Rate*100, s.P95Rate*100,
			s.FractionPos*100, s.FractionNeg*100,
			s.AnnualizedCarry*100,
			s.NetAnnualizedLongBias*100,   // collect when funding positive (short perp)
			s.NetAnnualizedShortBias*100,  // collect when funding negative (long perp)
		)
	}
	_ = perSample8h

	// Verdict
	fmt.Println()
	fmt.Println("=== Verdict ===")
	profitableShortBias, profitableLongBias := 0, 0
	bestShortBias, bestLongBias := symbolStats{}, symbolStats{}
	for _, s := range allStats {
		if s.NetAnnualizedLongBias > 0.05 { // > 5% net annualized
			profitableShortBias++
			if s.NetAnnualizedLongBias > bestShortBias.NetAnnualizedLongBias {
				bestShortBias = s
			}
		}
		if s.NetAnnualizedShortBias > 0.05 {
			profitableLongBias++
			if s.NetAnnualizedShortBias > bestLongBias.NetAnnualizedShortBias {
				bestLongBias = s
			}
		}
	}
	fmt.Printf("Symbols where SHORT-perp + LONG-spot beats 5%%/yr net: %d of %d\n", profitableShortBias, len(allStats))
	if bestShortBias.Symbol != "" {
		fmt.Printf("  Best: %s @ %+.2f%% net annualized\n", bestShortBias.Symbol, bestShortBias.NetAnnualizedLongBias*100)
	}
	fmt.Printf("Symbols where LONG-perp + SHORT-spot beats 5%%/yr net: %d of %d\n", profitableLongBias, len(allStats))
	if bestLongBias.Symbol != "" {
		fmt.Printf("  Best: %s @ %+.2f%% net annualized\n", bestLongBias.Symbol, bestLongBias.NetAnnualizedShortBias*100)
	}

	fmt.Println()
	fmt.Println("=== Reality Check ===")
	anyGood := profitableShortBias > 0 || profitableLongBias > 0
	if !anyGood {
		fmt.Println("No symbols show net-positive carry >5%/yr after costs.")
		fmt.Println("Funding rate arbitrage is NOT viable on this window.")
		fmt.Println("Possible reasons: recent market regime has compressed funding;")
		fmt.Println("costs are higher than assumed; or this exchange's funding is too")
		fmt.Println("competitive for retail after fees.")
		fmt.Println()
		fmt.Println("Recommendation: do not build the execution layer. Pivot.")
		return
	}
	// There's at least one profitable symbol. But consider the effective capital
	// required: delta-neutral needs matching notional on both legs, so $500
	// working capital = $500 perp + $500 spot hedge = $1000 total deployed.
	// Annual profit in $ is notional × net% — scale matters.
	fmt.Println("Some symbols show positive expected carry. Next step:")
	fmt.Println("  1. Validate the hedge leg is feasible on Aster (need spot margin or use Binance spot)")
	fmt.Println("  2. Build a small-scale simulator that accounts for basis drift + rebalancing")
	fmt.Println("  3. Compare dollar profit at $500-$1000 scale against implementation complexity")
	fmt.Println()
	fmt.Println("If the best net-annualized return is < 8%, the dollar amounts at retail scale")
	fmt.Println("are small enough that implementation cost exceeds profit. Consider DCA instead.")

	// Show top 3 symbols by best bias, ranked
	fmt.Println()
	fmt.Println("=== Ranked Opportunities ===")
	type opp struct {
		Symbol  string
		Bias    string
		NetAnn  float64
		Funding float64
	}
	var opps []opp
	for _, s := range allStats {
		if s.NetAnnualizedLongBias > 0 {
			opps = append(opps, opp{s.Symbol, "SHORT perp + LONG spot", s.NetAnnualizedLongBias, s.MeanRate})
		}
		if s.NetAnnualizedShortBias > 0 {
			opps = append(opps, opp{s.Symbol, "LONG perp + SHORT spot", s.NetAnnualizedShortBias, s.MeanRate})
		}
	}
	sort.Slice(opps, func(i, j int) bool { return opps[i].NetAnn > opps[j].NetAnn })
	if len(opps) > 10 {
		opps = opps[:10]
	}
	for i, o := range opps {
		fmt.Printf("  #%d  %s  %s  →  %+.2f%% net/yr (raw funding %+.4f%%/8h)\n",
			i+1, o.Symbol, o.Bias, o.NetAnn*100, o.Funding*100)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// fetchFundingHistory pulls funding rate snapshots from Aster. Aster returns
// up to 1000 entries per call, ordered ascending by time. When `startTime`
// is set the response begins at-or-after that time (up to the 1000-entry
// limit). We page forward by setting startTime to (maxTime + 1ms) each
// iteration until we reach either "now" or the desired sample count.
func fetchFundingHistory(symbol string, days int) ([]fundingPoint, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	startMs := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	nowMs := time.Now().UnixMilli()

	var out []fundingPoint
	for {
		url := fmt.Sprintf(
			"https://fapi.asterdex.com/fapi/v1/fundingRate?symbol=%s&limit=1000&startTime=%d",
			symbol, startMs,
		)
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("aster %d: %s", resp.StatusCode, string(body))
		}
		var raw []struct {
			Symbol      string `json:"symbol"`
			FundingTime int64  `json:"fundingTime"`
			FundingRate string `json:"fundingRate"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		if len(raw) == 0 {
			break
		}
		maxTime := int64(0)
		for _, r := range raw {
			if r.FundingTime > maxTime {
				maxTime = r.FundingTime
			}
			rate, _ := strconv.ParseFloat(r.FundingRate, 64)
			out = append(out, fundingPoint{Time: r.FundingTime, Rate: rate})
		}
		if maxTime <= startMs || maxTime >= nowMs || len(raw) < 1000 {
			break
		}
		startMs = maxTime + 1
		time.Sleep(150 * time.Millisecond)
	}

	// Dedupe + sort ascending
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	dedup := make([]fundingPoint, 0, len(out))
	var lastT int64 = -1
	for _, p := range out {
		if p.Time == lastT {
			continue
		}
		dedup = append(dedup, p)
		lastT = p.Time
	}
	// Trim to exact window (should already be fine but defensive)
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	i := 0
	for i < len(dedup) && dedup[i].Time < cutoff {
		i++
	}
	dedup = dedup[i:]
	return dedup, nil
}

func analyze(symbol string, points []fundingPoint) symbolStats {
	rates := make([]float64, len(points))
	for i, p := range points {
		rates[i] = p.Rate
	}

	mean := 0.0
	for _, r := range rates {
		mean += r
	}
	mean /= float64(len(rates))

	variance := 0.0
	for _, r := range rates {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(rates))
	std := math.Sqrt(variance)

	sorted := make([]float64, len(rates))
	copy(sorted, rates)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	p5 := sorted[int(0.05*float64(len(sorted)))]
	p95 := sorted[int(0.95*float64(len(sorted)))]

	pos, neg, zero := 0, 0, 0
	const zeroTol = 1e-5
	for _, r := range rates {
		if r > zeroTol {
			pos++
		} else if r < -zeroTol {
			neg++
		} else {
			zero++
		}
	}

	// Funding is paid every 8h → 3x per day → 3×365 = 1095 per year.
	annualized := mean * 3 * 365

	return symbolStats{
		Symbol:          symbol,
		Samples:         len(points),
		FirstTime:       time.UnixMilli(points[0].Time),
		LastTime:        time.UnixMilli(points[len(points)-1].Time),
		MeanRate:        mean,
		MedianRate:      median,
		P5Rate:          p5,
		P95Rate:         p95,
		StdDev:          std,
		FractionPos:     float64(pos) / float64(len(rates)),
		FractionNeg:     float64(neg) / float64(len(rates)),
		FractionZero:    float64(zero) / float64(len(rates)),
		AnnualizedCarry: annualized,
	}
}
