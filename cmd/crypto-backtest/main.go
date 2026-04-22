// Command crypto-backtest runs a walk-forward backtest of the momentum_only
// strategy against historical Binance Futures data. It's designed to answer a
// single question honestly: does this strategy have edge, and what's the
// realistic ceiling after fees + slippage?
//
// This is a simplified but faithful re-implementation of the signal logic in
// `trader/momentum_fallback.go` (selectBestMomentumSignal). The allocator
// path is not reproduced here — we use a fixed fractional-risk sizer — but
// the signal scoring, regime filter (via selector package), and exit logic
// match the live strategy's contracts closely enough to answer the edge
// question.
//
// Usage:
//   go run ./cmd/crypto-backtest \
//       -symbols BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,XRPUSDT,DOGEUSDT \
//       -interval 4h -bars 2000 \
//       -min-score 1.0 -risk-pct 0.0075 \
//       -fee-bps 4 -slip-bps 5
//
// Output: per-symbol trade log plus aggregate metrics (return, Sharpe, max
// drawdown, win rate, profit factor). If any of those look weak on your
// chosen test window, do not fund the bot until the strategy is improved.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"northstar/features"
	"northstar/regime"
	"northstar/selector"
)

type trade struct {
	Symbol    string
	Side      string // "long" or "short"
	EntryTime time.Time
	ExitTime  time.Time
	EntryPx   float64
	ExitPx    float64
	PnLPct    float64 // net of fees+slippage
	BarsHeld  int
	ExitKind  string // "tp", "sl", "timeout", "reversal"
	Score     float64
}

type backtestConfig struct {
	Symbols            []string
	Interval           string
	Bars               int
	MinScore           float64
	RiskPerTrade       float64
	TakerFeeBps        float64 // one-way, e.g. 4 = 0.04%
	SlippageBps        float64 // one-way, e.g. 5 = 0.05%
	TPPct              float64 // take-profit, e.g. 0.045
	SLPct              float64 // stop-loss (absolute), e.g. 0.015
	MaxHoldBars        int
	WarmupBars         int
	Verbose            bool
	UseRegimeGate      bool
	UseFundingFilter   bool    // if true, block entries against crowded positioning
	FundingEntryThresh float64 // e.g. 0.0004 = 0.04%/8h — skip long if funding > this, skip short if funding < -this
	CollectFunding     bool    // credit/debit funding rate during holding
	PortfolioDDHalt    float64 // e.g. 0.15 = halt new entries when portfolio DD >= 15%
	CooldownDays       int
}

// fundingPoint is (time, rate) for a single funding payment. rate is per 8h.
type fundingPoint struct {
	Time int64
	Rate float64
}

func main() {
	symbolList := flag.String("symbols", "BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,XRPUSDT,DOGEUSDT", "comma-separated Binance futures symbols")
	interval := flag.String("interval", "4h", "Binance kline interval (1h, 4h, 1d)")
	barsStr := flag.Int("bars", 2000, "number of historical bars to fetch per symbol (max 1500 per call; we page)")
	minScore := flag.Float64("min-score", 1.0, "minimum signal score to enter")
	riskPct := flag.Float64("risk-pct", 0.0075, "fraction of equity risked per trade")
	feeBps := flag.Float64("fee-bps", 4.0, "taker fee per side in basis points (Aster=4, Binance=4, Hyperliquid=3.5)")
	slipBps := flag.Float64("slip-bps", 5.0, "slippage per side in basis points")
	tp := flag.Float64("tp-pct", 0.045, "take-profit fraction (e.g. 0.045 = +4.5%)")
	sl := flag.Float64("sl-pct", 0.015, "stop-loss fraction as absolute value (e.g. 0.015 = 1.5%)")
	maxHold := flag.Int("max-hold", 30, "max bars to hold before forced exit")
	warmup := flag.Int("warmup", 60, "bars of warmup before first trade (features need history)")
	useRegime := flag.Bool("regime-gate", true, "honor the selector.AllowTrading flag + family-specific boosts")
	verbose := flag.Bool("v", false, "print every trade")
	endDate := flag.String("end-date", "", "backtest window ending date (YYYY-MM-DD). default: now")
	fundingFilter := flag.Bool("funding-filter", false, "block entries when funding rate signals crowded positioning")
	fundingThresh := flag.Float64("funding-thresh", 0.0004, "funding rate threshold for filter (0.0004 = 0.04%/8h = 44%/yr)")
	collectFunding := flag.Bool("collect-funding", false, "credit/debit actual funding payments during holding")
	portfolioDD := flag.Float64("portfolio-dd-halt", 0.0, "halt new entries when portfolio equity DD >= this fraction (e.g. 0.15 = 15%)")
	cooldownDays := flag.Int("cooldown-days", 7, "halt duration after kill-switch fires")
	flag.Parse()

	cfg := backtestConfig{
		Symbols:            splitCSV(*symbolList),
		Interval:           *interval,
		Bars:               *barsStr,
		MinScore:           *minScore,
		RiskPerTrade:       *riskPct,
		TakerFeeBps:        *feeBps,
		SlippageBps:        *slipBps,
		TPPct:              *tp,
		SLPct:              *sl,
		MaxHoldBars:        *maxHold,
		WarmupBars:         *warmup,
		Verbose:            *verbose,
		UseRegimeGate:      *useRegime,
		UseFundingFilter:   *fundingFilter,
		FundingEntryThresh: *fundingThresh,
		CollectFunding:     *collectFunding,
		PortfolioDDHalt:    *portfolioDD,
		CooldownDays:       *cooldownDays,
	}

	fmt.Printf("=== Crypto Momentum Backtest ===\n")
	fmt.Printf("Symbols:       %v\n", cfg.Symbols)
	fmt.Printf("Interval:      %s, %d bars, warmup=%d\n", cfg.Interval, cfg.Bars, cfg.WarmupBars)
	fmt.Printf("Signal:        min_score=%.2f, regime_gate=%v\n", cfg.MinScore, cfg.UseRegimeGate)
	fmt.Printf("Funding:       filter=%v (thresh=%.4f=%.2f%%/8h) collect=%v\n",
		cfg.UseFundingFilter, cfg.FundingEntryThresh, cfg.FundingEntryThresh*100, cfg.CollectFunding)
	fmt.Printf("Exits:         TP=%.2f%% SL=%.2f%% max_hold=%d bars\n", cfg.TPPct*100, cfg.SLPct*100, cfg.MaxHoldBars)
	fmt.Printf("Costs:         fee=%.1fbps/side slippage=%.1fbps/side risk=%.2f%%/trade\n\n",
		cfg.TakerFeeBps, cfg.SlippageBps, cfg.RiskPerTrade*100)

	var allTrades []trade
	type perSymbolResult struct {
		symbol     string
		trades     []trade
		totalRet   float64
		buyHoldRet float64
	}
	var perSymbol []perSymbolResult

	endMs := time.Now().UnixMilli()
	if strings.TrimSpace(*endDate) != "" {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(*endDate))
		if err != nil {
			fmt.Printf("invalid -end-date %q: %v\n", *endDate, err)
			os.Exit(1)
		}
		endMs = t.UnixMilli()
		fmt.Printf("Window end:    %s (custom)\n\n", t.UTC().Format("2006-01-02"))
	} else {
		fmt.Printf("Window end:    now\n\n")
	}

	for _, sym := range cfg.Symbols {
		fmt.Printf(">> %s: fetching %d bars of %s...\n", sym, cfg.Bars, cfg.Interval)
		bars, err := fetchBinanceKlinesEnding(sym, cfg.Interval, cfg.Bars, endMs)
		if err != nil {
			fmt.Printf("   ERROR: %v (skipping)\n", err)
			continue
		}
		fmt.Printf("   got %d bars from %s to %s\n",
			len(bars),
			time.UnixMilli(bars[0].OpenTime).UTC().Format("2006-01-02"),
			time.UnixMilli(bars[len(bars)-1].OpenTime).UTC().Format("2006-01-02"),
		)
		var funding []fundingPoint
		if cfg.UseFundingFilter || cfg.CollectFunding {
			// Fetch from Aster. Binance has its own funding rate endpoint
			// with nearly identical behavior and rates; for the backtest
			// we use Aster's data since that's where trading will happen.
			f, ferr := fetchAsterFundingHistoryInWindow(sym, bars[0].OpenTime, bars[len(bars)-1].OpenTime)
			if ferr != nil {
				fmt.Printf("   funding fetch failed: %v (continuing without funding data)\n", ferr)
			} else {
				funding = f
				fmt.Printf("   got %d funding points\n", len(funding))
			}
		}
		trades := runBacktest(sym, bars, funding, cfg)
		totalRet := 0.0
		for _, t := range trades {
			totalRet += t.PnLPct
		}
		// Buy-and-hold baseline: enter at warmup-bar close, exit at last bar.
		buyHoldRet := 0.0
		if len(bars) > cfg.WarmupBars {
			entryPx := bars[cfg.WarmupBars].Close
			exitPx := bars[len(bars)-1].Close
			if entryPx > 0 {
				buyHoldRet = (exitPx - entryPx) / entryPx
			}
		}
		allTrades = append(allTrades, trades...)
		perSymbol = append(perSymbol, perSymbolResult{
			symbol: sym, trades: trades, totalRet: totalRet, buyHoldRet: buyHoldRet,
		})
		fmt.Printf("   %d trades, total return (sum of PnL%%): %+.2f%%\n\n", len(trades), totalRet*100)

		if cfg.Verbose {
			for _, t := range trades {
				fmt.Printf("   [%s] %s %s @ %.4f -> %.4f  %+.2f%%  %dbars  %s\n",
					t.Symbol, t.Side,
					t.EntryTime.Format("2006-01-02 15:04"),
					t.EntryPx, t.ExitPx, t.PnLPct*100, t.BarsHeld, t.ExitKind)
			}
			fmt.Println()
		}
	}

	// Apply portfolio kill switch if configured. Walk trades chronologically
	// by entry time; track running equity with fractional notional risk; if
	// peak-to-trough DD breaches the threshold, skip all subsequent entries
	// until equity recovers to the prior peak. This models a pragmatic
	// operator who pulls the plug when the account bleeds.
	if cfg.PortfolioDDHalt > 0 {
		allTrades = applyPortfolioKillSwitch(allTrades, cfg)
	}

	reportMetrics(allTrades, cfg)
	fmt.Println("\n=== Per-Symbol Breakdown vs Buy-and-Hold ===")
	sort.Slice(perSymbol, func(i, j int) bool { return perSymbol[i].totalRet > perSymbol[j].totalRet })
	fmt.Printf("%-12s  %5s  %10s  %10s  %10s  %8s\n",
		"Symbol", "trades", "sum-pnl%", "buy&hold%", "strat−B&H", "winrate")
	for _, r := range perSymbol {
		wins := 0
		for _, t := range r.trades {
			if t.PnLPct > 0 {
				wins++
			}
		}
		wr := 0.0
		if len(r.trades) > 0 {
			wr = float64(wins) / float64(len(r.trades)) * 100
		}
		delta := r.totalRet - r.buyHoldRet
		fmt.Printf("%-12s  %5d  %+9.2f%%  %+9.2f%%  %+9.2f%%  %6.1f%%\n",
			r.symbol, len(r.trades), r.totalRet*100, r.buyHoldRet*100, delta*100, wr)
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

// fetchBinanceKlines pages through Binance's public klines endpoint.
// endTime moves backwards until we have the requested number of bars.
func fetchBinanceKlines(symbol, interval string, limit int) ([]features.Bar, error) {
	return fetchBinanceKlinesEnding(symbol, interval, limit, time.Now().UnixMilli())
}

// fetchBinanceKlinesEnding fetches `limit` bars ending at or before endMs.
// Use this for date-range backtesting (e.g. stress-test on the 2022 bear).
func fetchBinanceKlinesEnding(symbol, interval string, limit int, endMsInitial int64) ([]features.Bar, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	out := make([]features.Bar, 0, limit)
	endMs := endMsInitial

	for len(out) < limit {
		batchLimit := 1500
		if limit-len(out) < batchLimit {
			batchLimit = limit - len(out)
		}
		url := fmt.Sprintf(
			"https://fapi.binance.com/fapi/v1/klines?symbol=%s&interval=%s&limit=%d&endTime=%d",
			symbol, interval, batchLimit, endMs,
		)
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("binance %d: %s", resp.StatusCode, string(body))
		}
		var raw [][]interface{}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		if len(raw) == 0 {
			break
		}
		batch := make([]features.Bar, 0, len(raw))
		for _, r := range raw {
			if len(r) < 6 {
				continue
			}
			openTime, _ := r[0].(float64)
			open := parseNum(r[1])
			high := parseNum(r[2])
			low := parseNum(r[3])
			closePx := parseNum(r[4])
			vol := parseNum(r[5])
			closeTime, _ := r[6].(float64)
			batch = append(batch, features.Bar{
				OpenTime: int64(openTime), Open: open, High: high, Low: low,
				Close: closePx, Volume: vol, CloseTime: int64(closeTime),
			})
		}
		if len(batch) == 0 {
			break
		}
		out = append(batch, out...) // prepend
		endMs = batch[0].OpenTime - 1
		time.Sleep(150 * time.Millisecond) // gentle rate limit
	}

	// Deduplicate + sort ascending
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	dedup := make([]features.Bar, 0, len(out))
	var lastTS int64 = -1
	for _, b := range out {
		if b.OpenTime == lastTS {
			continue
		}
		dedup = append(dedup, b)
		lastTS = b.OpenTime
	}
	return dedup, nil
}

func parseNum(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

// runBacktest walks forward one bar at a time. At each bar it computes
// features on the trailing window, scores via the same momentum logic as
// the live strategy, and — if a signal fires and no position is open —
// enters. Open positions are evaluated for TP/SL/timeout on each bar.
// When funding data is provided and the filter is enabled, entries are
// skipped when the most recent funding rate signals overcrowded positioning.
func runBacktest(symbol string, bars []features.Bar, funding []fundingPoint, cfg backtestConfig) []trade {
	if len(bars) <= cfg.WarmupBars {
		return nil
	}

	engine := features.DefaultEngine()
	detector := regime.DefaultDetector()
	sel := selector.Default()

	var trades []trade
	var openTrade *trade
	var entryBar int
	var entryTimeMs int64
	var fundingAccrued float64

	for i := cfg.WarmupBars; i < len(bars); i++ {
		window := bars[:i+1]
		price := window[len(window)-1].Close
		barTimeMs := bars[i].OpenTime

		// If we have an open trade, check exit conditions first.
		if openTrade != nil {
			bar := bars[i]
			// Accrue funding collected/paid since last bar.
			if cfg.CollectFunding && len(funding) > 0 {
				direction := 1.0
				if openTrade.Side == "short" {
					direction = -1.0
				}
				prevTime := barTimeMs
				if i > 0 {
					prevTime = bars[i-1].OpenTime
				}
				// Sum funding rates paid in (prevTime, barTimeMs].
				// Long position PAYS funding when rate > 0 (to shorts); short RECEIVES.
				// So position P&L += -direction * rate at each funding event.
				for _, fp := range funding {
					if fp.Time > prevTime && fp.Time <= barTimeMs {
						fundingAccrued += -direction * fp.Rate
					}
				}
			}

			// For TP/SL, use the bar's path (high/low) to be realistic about
			// intra-bar moves rather than only close-to-close.
			entryPx := openTrade.EntryPx
			direction := 1.0
			if openTrade.Side == "short" {
				direction = -1.0
			}
			moveUp := direction * (bar.High - entryPx) / entryPx
			moveDown := direction * (bar.Low - entryPx) / entryPx
			// Worst-case ordering: if a bar touches both TP and SL, assume SL hit.
			hitSL := moveDown <= -cfg.SLPct
			hitTP := moveUp >= cfg.TPPct

			exitKind := ""
			exitPx := 0.0
			if hitSL {
				exitKind = "sl"
				exitPx = entryPx * (1 - direction*cfg.SLPct)
			} else if hitTP {
				exitKind = "tp"
				exitPx = entryPx * (1 + direction*cfg.TPPct)
			} else if i-entryBar >= cfg.MaxHoldBars {
				exitKind = "timeout"
				exitPx = bar.Close
			}

			if exitKind != "" {
				gross := direction * (exitPx - entryPx) / entryPx
				// Deduct two-way costs: fees + slippage on entry and exit.
				cost := 2 * (cfg.TakerFeeBps + cfg.SlippageBps) / 10000.0
				openTrade.ExitPx = exitPx
				openTrade.ExitTime = time.UnixMilli(bar.OpenTime)
				openTrade.PnLPct = gross - cost + fundingAccrued
				openTrade.BarsHeld = i - entryBar
				openTrade.ExitKind = exitKind
				trades = append(trades, *openTrade)
				openTrade = nil
				fundingAccrued = 0
				entryTimeMs = 0
			} else {
				continue // still holding, don't look for new signals
			}
		}

		// Look for a new entry signal.
		// Compute features on the trailing window. The features package needs
		// at least 60 bars of history — we guaranteed that via WarmupBars.
		featureSet := engine.ComputeSet(symbol, map[string][]features.Bar{"4h": window})
		regimeSet := detector.DetectSet(featureSet)
		selSet := sel.SelectSet(regimeSet)

		var sig *selector.Selection
		if cfg.UseRegimeGate && selSet != nil {
			sig = selSet.Selection("4h")
			if sig != nil && !sig.AllowTrading {
				continue
			}
		}

		trend, macd, rsi7 := momentumInputs(window)
		directionScore, rawScore, score, shortBias := scoreSignal(trend, macd, rsi7, sig)
		_ = directionScore // kept for future telemetry

		if score < cfg.MinScore {
			continue
		}

		// Funding filter: avoid entries that align with crowded positioning.
		// If we'd go LONG but funding is very positive (longs paying premium to hold):
		//   crowded long → skip
		// If we'd go SHORT but funding is very negative:
		//   crowded short → skip
		if cfg.UseFundingFilter && len(funding) > 0 {
			currentFunding := mostRecentFunding(funding, barTimeMs)
			side := "long"
			if shortBias {
				side = "short"
			}
			if side == "long" && currentFunding > cfg.FundingEntryThresh {
				continue // longs overcrowded
			}
			if side == "short" && currentFunding < -cfg.FundingEntryThresh {
				continue // shorts overcrowded
			}
		}

		// Enter.
		side := "long"
		if shortBias {
			side = "short"
		}
		bar := window[len(window)-1]
		openTrade = &trade{
			Symbol:    symbol,
			Side:      side,
			EntryTime: time.UnixMilli(bar.OpenTime),
			EntryPx:   price,
			Score:     rawScore,
		}
		entryBar = i
		entryTimeMs = bar.OpenTime
		fundingAccrued = 0
	}
	_ = entryTimeMs

	// If a trade is still open at the end, close at last bar.
	if openTrade != nil {
		bar := bars[len(bars)-1]
		direction := 1.0
		if openTrade.Side == "short" {
			direction = -1.0
		}
		gross := direction * (bar.Close - openTrade.EntryPx) / openTrade.EntryPx
		cost := 2 * (cfg.TakerFeeBps + cfg.SlippageBps) / 10000.0
		openTrade.ExitPx = bar.Close
		openTrade.ExitTime = time.UnixMilli(bar.OpenTime)
		openTrade.PnLPct = gross - cost + fundingAccrued
		openTrade.BarsHeld = len(bars) - 1 - entryBar
		openTrade.ExitKind = "dataset-end"
		trades = append(trades, *openTrade)
	}

	return trades
}

// applyPortfolioKillSwitch walks trades chronologically with a running
// equity curve. When peak-to-trough DD breaches the configured threshold,
// trading halts for a fixed cooldown period (7 days), after which entries
// resume. During cooldown, the equity peak is reset to the current equity —
// i.e., we accept the drawdown as our new baseline rather than waiting to
// claw back the prior peak (which traps the simulation in endless halt).
//
// This matches what a rational operator does: "I'm down 15%, stop trading
// for a week, then come back with a cooler head." It's not optimal stopping
// theory but it's a realistic, implementable rule.
func applyPortfolioKillSwitch(trades []trade, cfg backtestConfig) []trade {
	if len(trades) == 0 || cfg.PortfolioDDHalt <= 0 {
		return trades
	}
	sort.Slice(trades, func(i, j int) bool { return trades[i].EntryTime.Before(trades[j].EntryTime) })
	notionalFrac := cfg.RiskPerTrade / cfg.SLPct
	if notionalFrac <= 0 {
		notionalFrac = 0.5
	}
	cooldownDays := cfg.CooldownDays
	if cooldownDays <= 0 {
		cooldownDays = 7
	}
	cooldown := time.Duration(cooldownDays) * 24 * time.Hour

	equity := 1.0
	peak := 1.0
	var haltedUntil time.Time
	halts := 0
	dropped := 0
	kept := make([]trade, 0, len(trades))
	for _, t := range trades {
		if !haltedUntil.IsZero() && t.EntryTime.Before(haltedUntil) {
			dropped++
			continue
		}
		if !haltedUntil.IsZero() && !t.EntryTime.Before(haltedUntil) {
			// Cooldown expired — reset peak to current equity and resume.
			peak = equity
			haltedUntil = time.Time{}
		}
		// Execute this trade and update equity.
		equity *= 1 + notionalFrac*t.PnLPct
		if equity > peak {
			peak = equity
		}
		dd := (peak - equity) / peak
		if dd >= cfg.PortfolioDDHalt {
			halts++
			haltedUntil = t.ExitTime.Add(cooldown)
		}
		kept = append(kept, t)
	}
	fmt.Printf("\n[kill-switch @ %.0f%% DD, %dd cooldown] halts=%d dropped=%d / kept=%d\n",
		cfg.PortfolioDDHalt*100, cooldownDays, halts, dropped, len(kept))
	return kept
}

// mostRecentFunding returns the last funding rate recorded at or before atMs.
// Binary search since funding list is sorted ascending by time.
func mostRecentFunding(funding []fundingPoint, atMs int64) float64 {
	if len(funding) == 0 {
		return 0
	}
	lo, hi := 0, len(funding)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if funding[mid].Time <= atMs {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if funding[lo].Time <= atMs {
		return funding[lo].Rate
	}
	return 0
}

// fetchAsterFundingHistoryInWindow pulls Aster's funding rate archive for
// `symbol` in the inclusive time window [startMs, endMs]. Aster returns up
// to 1000 entries at a time, ordered ascending by time, starting from
// startTime. We page forward until we pass endMs.
func fetchAsterFundingHistoryInWindow(symbol string, startMs, endMs int64) ([]fundingPoint, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	var out []fundingPoint
	cursor := startMs
	for cursor <= endMs {
		url := fmt.Sprintf(
			"https://fapi.asterdex.com/fapi/v1/fundingRate?symbol=%s&limit=1000&startTime=%d",
			symbol, cursor,
		)
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("aster funding %d: %s", resp.StatusCode, string(body))
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
			if r.FundingTime < startMs || r.FundingTime > endMs {
				continue
			}
			rate, _ := strconv.ParseFloat(r.FundingRate, 64)
			out = append(out, fundingPoint{Time: r.FundingTime, Rate: rate})
		}
		if maxTime <= cursor || len(raw) < 1000 {
			break
		}
		cursor = maxTime + 1
		time.Sleep(150 * time.Millisecond)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, nil
}

// momentumInputs reproduces the inputs used by selectBestMomentumSignal:
// 1h and 4h price changes (as percentages), current MACD sign, and RSI(7).
// For 4h-only bars, "1h" is approximated as the last bar's close-to-open,
// and "4h" is last-bar close vs 2 bars ago (one bar = 4h).
func momentumInputs(bars []features.Bar) (trend, macd, rsi7 float64) {
	if len(bars) < 15 {
		return 0, 0, 50
	}
	last := bars[len(bars)-1]
	// Approximations: intra-bar change is ~1h equivalent on the 4h timeframe.
	priceChange1h := 0.0
	if last.Open > 0 {
		priceChange1h = (last.Close - last.Open) / last.Open * 100
	}
	priceChange4h := 0.0
	if len(bars) >= 2 {
		prev := bars[len(bars)-2]
		if prev.Close > 0 {
			priceChange4h = (last.Close - prev.Close) / prev.Close * 100
		}
	}
	trend = priceChange1h*0.55 + priceChange4h*0.45

	// MACD = 12-EMA minus 26-EMA on close prices.
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}
	macd = ema(closes, 12) - ema(closes, 26)

	// RSI(7) — standard Wilder-smoothed.
	rsi7 = rsi(closes, 7)
	return trend, macd, rsi7
}

func ema(values []float64, period int) float64 {
	if len(values) < period || period <= 0 {
		return 0
	}
	k := 2.0 / float64(period+1)
	e := values[0]
	for i := 1; i < len(values); i++ {
		e = values[i]*k + e*(1-k)
	}
	return e
}

func rsi(values []float64, period int) float64 {
	if len(values) < period+1 || period <= 0 {
		return 50
	}
	gainSum, lossSum := 0.0, 0.0
	for i := 1; i <= period; i++ {
		delta := values[i] - values[i-1]
		if delta > 0 {
			gainSum += delta
		} else {
			lossSum += -delta
		}
	}
	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	for i := period + 1; i < len(values); i++ {
		delta := values[i] - values[i-1]
		gain := 0.0
		loss := 0.0
		if delta > 0 {
			gain = delta
		} else {
			loss = -delta
		}
		avgGain = (avgGain*(float64(period-1)) + gain) / float64(period)
		avgLoss = (avgLoss*(float64(period-1)) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// scoreSignal mirrors selectBestMomentumSignal's scoring semantics.
func scoreSignal(trend, macd, rsi7 float64, sig *selector.Selection) (directionScore, rawScore, score float64, shortBias bool) {
	macdBias := 0.0
	if macd > 0 {
		macdBias = 0.8
	} else if macd < 0 {
		macdBias = -0.8
	}
	directionScore = trend + macdBias
	if math.Abs(directionScore) < 0.4 {
		return directionScore, 0, 0, false
	}
	rsiDistance := math.Abs(rsi7-50.0) / 50.0
	quality := 1.0 - math.Min(1.0, rsiDistance)
	rawScore = math.Abs(directionScore) + quality*0.6
	boost := 1.0
	if sig != nil {
		switch sig.SelectedFamily {
		case selector.StrategyFamilyMomentum:
			boost = 1.15
		case selector.StrategyFamilyHybrid:
			boost = 1.00
		case selector.StrategyFamilyDefensive:
			boost = 0.85
		case selector.StrategyFamilyMeanReversion:
			boost = 0.70
		default:
			boost = 0.60
		}
		conf := sig.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		boost *= (0.7 + 0.3*conf)
	}
	score = rawScore * boost
	shortBias = directionScore < 0
	return directionScore, rawScore, score, shortBias
}

// reportMetrics prints a rigorous set of summary statistics, emphasizing
// risk-adjusted return rather than raw total. For retail algo-trading, a
// Sharpe < 0.8 post-fees is usually not worth the hassle — cite the numbers,
// not the vibes.
func reportMetrics(trades []trade, cfg backtestConfig) {
	fmt.Println("=== Aggregate Metrics ===")
	if len(trades) == 0 {
		fmt.Println("Zero trades fired. Strategy was too selective for the test window.")
		return
	}
	// Sort chronologically so we can compute running equity.
	sort.Slice(trades, func(i, j int) bool { return trades[i].ExitTime.Before(trades[j].ExitTime) })

	totalPnL := 0.0
	wins, losses := 0, 0
	grossWin, grossLoss := 0.0, 0.0
	for _, t := range trades {
		totalPnL += t.PnLPct
		if t.PnLPct > 0 {
			wins++
			grossWin += t.PnLPct
		} else if t.PnLPct < 0 {
			losses++
			grossLoss += -t.PnLPct
		}
	}

	winRate := float64(wins) / float64(len(trades)) * 100
	avgTrade := totalPnL / float64(len(trades)) * 100
	profitFactor := 0.0
	if grossLoss > 0 {
		profitFactor = grossWin / grossLoss
	}

	// Per-trade Sharpe using sample std. For a proper annualized Sharpe we'd
	// need return-per-period; this is a reasonable approximation when trades
	// are independent and roughly uniformly timed.
	mean := totalPnL / float64(len(trades))
	variance := 0.0
	for _, t := range trades {
		variance += (t.PnLPct - mean) * (t.PnLPct - mean)
	}
	variance /= float64(len(trades))
	std := math.Sqrt(variance)
	sharpePerTrade := 0.0
	if std > 0 {
		sharpePerTrade = mean / std
	}

	// Sum-of-pct drawdown (useful as a raw signal shape but NOT realistic
	// for equity, since each trade only puts a fraction of equity at risk).
	cum := 0.0
	peak := 0.0
	maxDDSum := 0.0
	for _, t := range trades {
		cum += t.PnLPct
		if cum > peak {
			peak = cum
		}
		dd := peak - cum
		if dd > maxDDSum {
			maxDDSum = dd
		}
	}

	// Compounded equity curve using fractional-risk sizing.
	// notional_fraction = risk_per_trade / stop_loss_pct  → this is the
	// fraction of equity we're actually exposed to each trade. PnL on
	// equity is then (notional_fraction × trade_pnl_pct).
	notionalFraction := cfg.RiskPerTrade / cfg.SLPct
	if notionalFraction <= 0 {
		notionalFraction = 0.5 // sane fallback
	}
	equity := 1.0
	peakEq := 1.0
	maxDDEq := 0.0
	equitySeries := make([]float64, 0, len(trades))
	for _, t := range trades {
		equity *= 1 + notionalFraction*t.PnLPct
		equitySeries = append(equitySeries, equity)
		if equity > peakEq {
			peakEq = equity
		}
		dd := (peakEq - equity) / peakEq
		if dd > maxDDEq {
			maxDDEq = dd
		}
	}
	compoundedReturn := equity - 1.0

	// Bucket exit reasons.
	exitKinds := map[string]int{}
	for _, t := range trades {
		exitKinds[t.ExitKind]++
	}

	totalDays := 0.0
	if len(trades) >= 2 {
		totalDays = trades[len(trades)-1].ExitTime.Sub(trades[0].EntryTime).Hours() / 24
	}

	fmt.Printf("Total trades:         %d\n", len(trades))
	fmt.Printf("Win rate:             %.1f%% (W=%d L=%d)\n", winRate, wins, losses)
	fmt.Printf("Avg trade P&L:        %+.3f%% (per-trade, on notional)\n", avgTrade)
	fmt.Printf("Profit factor:        %.2f   (grossWin/grossLoss)\n", profitFactor)
	fmt.Printf("Per-trade Sharpe:     %.2f   (mean/std, unitless)\n", sharpePerTrade)
	fmt.Printf("Sum-of-PnL:           %+.2f%% (ADDING per-trade returns — NOT equity)\n", totalPnL*100)
	fmt.Printf("Compounded equity:    %+.2f%% (notional fraction=%.2f of equity per trade)\n", compoundedReturn*100, notionalFraction)
	fmt.Printf("Max equity DD:        %.2f%% (peak-to-trough on compounded curve)\n", maxDDEq*100)
	fmt.Printf("Max sum-of-PnL DD:    %.2f%% (raw shape, not real equity loss)\n", maxDDSum*100)
	fmt.Printf("Approx span:          %.1f days\n", totalDays)

	// Annualized equity return from compounded series.
	if totalDays > 30 && equity > 0 {
		years := totalDays / 365.0
		annEquity := math.Pow(equity, 1.0/years) - 1.0
		fmt.Printf("Annualized equity:    %+.2f%% (compounded, years=%.2f)\n", annEquity*100, years)

		// Annualized Sharpe on compounded log-returns — the most honest figure.
		if len(equitySeries) > 1 {
			logReturns := make([]float64, 0, len(equitySeries))
			prev := 1.0
			for _, e := range equitySeries {
				if prev > 0 && e > 0 {
					logReturns = append(logReturns, math.Log(e/prev))
				}
				prev = e
			}
			if len(logReturns) > 1 {
				lm := 0.0
				for _, r := range logReturns {
					lm += r
				}
				lm /= float64(len(logReturns))
				lv := 0.0
				for _, r := range logReturns {
					lv += (r - lm) * (r - lm)
				}
				lv /= float64(len(logReturns) - 1)
				lstd := math.Sqrt(lv)
				tradesPerYear := float64(len(trades)) * 365 / totalDays
				if lstd > 0 {
					annSharpe := (lm / lstd) * math.Sqrt(tradesPerYear)
					fmt.Printf("Annualized Sharpe:    %.2f (on log-returns, correct scaling)\n", annSharpe)
				}
			}
		}

		// Buy-and-hold benchmark per symbol (simple, no leverage, no costs).
		fmt.Printf("Buy-and-hold bench:   (see per-symbol table)\n")
	}

	fmt.Printf("Exit kinds:           ")
	for k, v := range exitKinds {
		fmt.Printf("%s=%d  ", k, v)
	}
	fmt.Println()

	// Interpretive verdict. Keep it blunt. The important metrics here are
	// compounded equity, max equity drawdown, and whether we beat buy-and-hold
	// per-symbol (printed separately). Per-trade Sharpe is always low with
	// many trades and shouldn't drive the verdict.
	fmt.Println()
	fmt.Println("=== Honest Verdict ===")
	switch {
	case len(trades) < 20:
		fmt.Println("Too few trades to draw a meaningful conclusion. Widen the universe or lengthen the window.")
	case compoundedReturn < 0:
		fmt.Println("Negative compounded return — strategy loses money after costs on this data. Do not fund as-is.")
	case profitFactor < 1.05:
		fmt.Println("Profit factor near or below 1.0 — edge is within noise. Not fundable.")
	case maxDDEq > 0.45:
		fmt.Println("Edge exists but equity drawdown is extreme (>45%). Reduce risk_per_trade to tame it before funding.")
	case compoundedReturn >= 0.5 && maxDDEq <= 0.40 && profitFactor >= 1.15:
		fmt.Println("Edge is measurable. Calmar (return/DD) is respectable. Stress-test on other windows, then paper trade.")
	case compoundedReturn >= 0.1 && maxDDEq <= 0.25:
		fmt.Println("Modest edge with acceptable drawdown. Paper trade to confirm, then fund conservatively.")
	default:
		fmt.Println("Mixed signal. Worth further stress-testing (different windows, OOS periods) before live trading.")
	}
	// Calmar ratio — annualized return / max DD. >1 is decent, >2 is strong.
	if maxDDEq > 0 && totalDays > 30 {
		years := totalDays / 365.0
		annEquity := math.Pow(equity, 1.0/years) - 1.0
		calmar := annEquity / maxDDEq
		fmt.Printf("Calmar ratio:         %.2f (annualized return / max DD; >1 is decent, >2 strong)\n", calmar)
	}

	// Flag curve-fit risk.
	if cfg.Bars < 500 {
		fmt.Println("WARNING: Small test window — results are not statistically reliable. Re-run with -bars 2000+.")
	}
	if exitKinds["timeout"] > exitKinds["tp"]+exitKinds["sl"] {
		fmt.Println("WARNING: Most trades hit max-hold timeout, not TP/SL. Stops may be mis-sized for this regime.")
	}

	// Sanity-check cost assumptions.
	fmt.Printf("\nCost check: each round-trip costs %.4f%% (2× (fee+slip)). If avg_trade is below this, there's no real edge — just noise.\n",
		2*(cfg.TakerFeeBps+cfg.SlippageBps)/100.0)

	_ = os.Args // silence unused import if we trim later
}
