package trader

import (
	"errors"
	"fmt"
	"log"
	dataquality "northstar/data"
	"northstar/decision"
	"northstar/market"
	"sort"
	"strings"
	"time"
)

type dataQualityBlockedSymbol struct {
	Symbol        string
	Interval      string
	Summary       string
	LastCheckedAt time.Time
	LastFailedAt  time.Time
	FailureCount  int
	IssueTypes    []dataquality.IssueType
}

type dataQualityFeedStatus struct {
	Delayed       bool
	Summary       string
	LastCheckedAt time.Time
	DetectedAt    time.Time
	ProbeSymbols  []string
}

type dataQualityState struct {
	LastCheckedAt  time.Time
	TotalChecks    int
	TotalFailures  int
	BlockedSymbols map[string]dataQualityBlockedSymbol
	FeedStatus     dataQualityFeedStatus
}

func (at *AutoTrader) initializeDataQualityState() {
	at.dataQualityMu.Lock()
	defer at.dataQualityMu.Unlock()
	at.dataQualityState = dataQualityState{
		BlockedSymbols: make(map[string]dataQualityBlockedSymbol),
	}
}

func (at *AutoTrader) currentDataValidationOptions() dataquality.Options {
	return dataquality.Options{
		Now:            at.currentTimeUTC(),
		CheckStaleness: !at.backtestMode && !strings.EqualFold(at.config.Mode, "replay"),
		InstrumentType: at.config.InstrumentType,
	}
}

func (at *AutoTrader) currentTimeUTC() time.Time {
	if at != nil && at.timeNow != nil {
		return at.timeNow().UTC()
	}
	return time.Now().UTC()
}

func (at *AutoTrader) getValidatedMarketData(symbol string) (*market.Data, error) {
	vopts := at.currentDataValidationOptions()
	vopts.StaleAfterBars = 6 // tolerate IBKR paper account 15-min delayed data
	req := market.GetRequest{
		Symbol:            symbol,
		Provider:          at.provider,
		InstrumentType:    at.config.InstrumentType,
		BarsAdjustment:    at.config.BarsAdjustment,
		ValidationOptions: vopts,
	}
	marketData, err := market.Get(req)
	at.observeDataQualityEvent(symbol, nil, err)
	if err != nil {
		return nil, err
	}
	return marketData, nil
}

func (at *AutoTrader) observeDataQualityEvent(symbol string, result *dataquality.Result, err error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}

	var validationErr *dataquality.ValidationError
	if err != nil && !errors.As(err, &validationErr) {
		return
	}

	at.dataQualityMu.Lock()
	defer at.dataQualityMu.Unlock()

	if at.dataQualityState.BlockedSymbols == nil {
		at.dataQualityState.BlockedSymbols = make(map[string]dataQualityBlockedSymbol)
	}
	at.dataQualityState.LastCheckedAt = time.Now().UTC()
	at.dataQualityState.TotalChecks++

	if validationErr != nil {
		res := validationErr.Result
		if len(res.Issues) > 0 && res.Issues[0].Type == dataquality.IssueMarketClosed {
			delete(at.dataQualityState.BlockedSymbols, symbol)
			at.syncDataQualityIncident(symbol, false, "market closed", nil)
			return
		}
		at.dataQualityState.TotalFailures++
		issueTypes := make([]dataquality.IssueType, 0, len(res.Issues))
		issueTypeStrings := make([]string, 0, len(res.Issues))
		for _, issue := range res.Issues {
			issueTypes = append(issueTypes, issue.Type)
			issueTypeStrings = append(issueTypeStrings, string(issue.Type))
		}
		prev, existed := at.dataQualityState.BlockedSymbols[symbol]
		entry := dataQualityBlockedSymbol{
			Symbol:        symbol,
			Interval:      res.Interval,
			Summary:       res.Summary,
			LastCheckedAt: res.CheckedAt,
			LastFailedAt:  res.CheckedAt,
			FailureCount:  1,
			IssueTypes:    issueTypes,
		}
		if existed {
			entry.FailureCount = prev.FailureCount + 1
		}
		at.dataQualityState.BlockedSymbols[symbol] = entry
		if !existed || prev.Summary != entry.Summary {
			log.Printf(" [%s] Data quality warning for %s: %s", at.name, symbol, entry.Summary)
			at.recordPaperSessionWarning(fmt.Sprintf("data quality blocked %s: %s", symbol, entry.Summary))
		}
		at.syncDataQualityIncident(symbol, true, entry.Summary, issueTypeStrings)
		return
	}

	delete(at.dataQualityState.BlockedSymbols, symbol)
	at.syncDataQualityIncident(symbol, false, "data quality restored", nil)
}

type OperatorDataQualityBlockedSymbol struct {
	Symbol        string                  `json:"symbol"`
	Interval      string                  `json:"interval"`
	Summary       string                  `json:"summary"`
	LastCheckedAt string                  `json:"last_checked_at"`
	LastFailedAt  string                  `json:"last_failed_at"`
	FailureCount  int                     `json:"failure_count"`
	IssueTypes    []dataquality.IssueType `json:"issue_types"`
}

type OperatorDataQualitySummary struct {
	Available           bool                               `json:"available"`
	LastCheckedAt       string                             `json:"last_checked_at"`
	TotalChecks         int                                `json:"total_checks"`
	TotalFailures       int                                `json:"total_failures"`
	BlockedSymbols      []OperatorDataQualityBlockedSymbol `json:"blocked_symbols"`
	BlockedSymbolsCount int                                `json:"blocked_symbols_count"`
	FeedDelayed         bool                               `json:"feed_delayed"`
	FeedSummary         string                             `json:"feed_summary"`
	FeedDetectedAt      string                             `json:"feed_detected_at"`
	FeedProbeSymbols    []string                           `json:"feed_probe_symbols"`
}

func (at *AutoTrader) currentDataQualitySummary() OperatorDataQualitySummary {
	at.dataQualityMu.RLock()
	defer at.dataQualityMu.RUnlock()

	blocked := make([]OperatorDataQualityBlockedSymbol, 0, len(at.dataQualityState.BlockedSymbols))
	for _, item := range at.dataQualityState.BlockedSymbols {
		blocked = append(blocked, OperatorDataQualityBlockedSymbol{
			Symbol:        item.Symbol,
			Interval:      item.Interval,
			Summary:       item.Summary,
			LastCheckedAt: formatRFC3339(item.LastCheckedAt),
			LastFailedAt:  formatRFC3339(item.LastFailedAt),
			FailureCount:  item.FailureCount,
			IssueTypes:    append([]dataquality.IssueType(nil), item.IssueTypes...),
		})
	}
	sort.Slice(blocked, func(i, j int) bool {
		if blocked[i].LastFailedAt == blocked[j].LastFailedAt {
			return blocked[i].Symbol < blocked[j].Symbol
		}
		return blocked[i].LastFailedAt > blocked[j].LastFailedAt
	})

	return OperatorDataQualitySummary{
		Available:           true,
		LastCheckedAt:       formatRFC3339(at.dataQualityState.LastCheckedAt),
		TotalChecks:         at.dataQualityState.TotalChecks,
		TotalFailures:       at.dataQualityState.TotalFailures,
		BlockedSymbols:      blocked,
		BlockedSymbolsCount: len(blocked),
		FeedDelayed:         at.dataQualityState.FeedStatus.Delayed,
		FeedSummary:         at.dataQualityState.FeedStatus.Summary,
		FeedDetectedAt:      formatRFC3339(at.dataQualityState.FeedStatus.DetectedAt),
		FeedProbeSymbols:    append([]string(nil), at.dataQualityState.FeedStatus.ProbeSymbols...),
	}
}

func (at *AutoTrader) updateMarketDataFeedStatus(delayed bool, summary string, probeSymbols []string) {
	at.dataQualityMu.Lock()
	defer at.dataQualityMu.Unlock()

	status := &at.dataQualityState.FeedStatus
	now := time.Now().UTC()
	status.LastCheckedAt = now
	status.ProbeSymbols = append([]string(nil), probeSymbols...)

	if !delayed {
		status.Delayed = false
		status.Summary = ""
		status.DetectedAt = time.Time{}
		return
	}

	if !status.Delayed {
		status.DetectedAt = now
	}
	status.Delayed = true
	status.Summary = strings.TrimSpace(summary)
}

func marketDataFeedProbeSymbols(ctx *decision.Context) []string {
	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 5)
	add := func(raw string) {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			return
		}
		if _, ok := seen[symbol]; ok {
			return
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}

	for _, symbol := range []string{"AAPL", "MSFT", "NVDA", "SPY", "QQQ"} {
		if len(out) >= 5 {
			break
		}
		add(symbol)
	}
	if ctx != nil {
		for _, coin := range ctx.CandidateCoins {
			if len(out) >= 5 {
				break
			}
			add(coin.Symbol)
		}
	}
	return out
}

func klinesToDataQualityBars(klines []market.Kline) []dataquality.Bar {
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
	return bars
}

func dataQualityIssueTypes(issues []dataquality.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, string(issue.Type))
	}
	return out
}

func nonMarketClosedIssueCount(issues []dataquality.Issue) int {
	count := 0
	for _, issue := range issues {
		if issue.Type != dataquality.IssueMarketClosed {
			count++
		}
	}
	return count
}

func (at *AutoTrader) preflightRuntimeMarketData(ctx *decision.Context) error {
	if at == nil || at.provider == nil {
		return nil
	}
	if !strings.EqualFold(at.config.InstrumentType, "equity") || !strings.EqualFold(at.config.DataProvider, "ibkr") {
		at.updateMarketDataFeedStatus(false, "", nil)
		at.syncMarketDataFeedDelayIncident(false, "market-data feed fresh", nil)
		return nil
	}
	if at.backtestMode || strings.EqualFold(at.config.Mode, "replay") {
		at.updateMarketDataFeedStatus(false, "", nil)
		at.syncMarketDataFeedDelayIncident(false, "market-data feed fresh", nil)
		return nil
	}

	probeSymbols := marketDataFeedProbeSymbols(ctx)
	if len(probeSymbols) == 0 {
		at.updateMarketDataFeedStatus(false, "", nil)
		at.syncMarketDataFeedDelayIncident(false, "market-data feed fresh", nil)
		return nil
	}

	opts := at.currentDataValidationOptions()
	opts.ExpectedBars = 40
	opts.StaleAfterBars = 6 // tolerate IBKR paper account 15-min delayed data
	series, err := at.provider.GetBars(probeSymbols, "3m", 40)
	if err != nil {
		summary := fmt.Sprintf("runtime market-data probe failed: %v", err)
		if classified, ok := classifyExpectedMarketDataBlock(err); ok && classified != "" {
			summary = classified
		}
		at.updateMarketDataFeedStatus(true, summary, probeSymbols)
		at.syncMarketDataFeedDelayIncident(true, summary, map[string]string{
			"probe_symbols": strings.Join(probeSymbols, ","),
			"error":         strings.TrimSpace(err.Error()),
		})
		return fmt.Errorf("market-data feed unavailable: %w", err)
	}

	passed := 0
	failures := make([]dataquality.Result, 0, len(probeSymbols))
	nonClosedFailures := 0
	for _, symbol := range probeSymbols {
		result := dataquality.ValidateBars(symbol, "3m", klinesToDataQualityBars(series[symbol]), opts)
		if !result.Failed() {
			passed++
			continue
		}
		failures = append(failures, result)
		nonClosedFailures += nonMarketClosedIssueCount(result.Issues)
	}

	if passed > 0 || nonClosedFailures == 0 {
		at.updateMarketDataFeedStatus(false, "", probeSymbols)
		at.syncMarketDataFeedDelayIncident(false, "market-data feed fresh", nil)
		return nil
	}

	summaries := make([]string, 0, len(failures))
	details := map[string]string{
		"probe_symbols": strings.Join(probeSymbols, ","),
	}
	for _, failure := range failures {
		summaries = append(summaries, failure.Summary)
		details[strings.ToLower(failure.Symbol)] = strings.Join(dataQualityIssueTypes(failure.Issues), ",")
	}
	summary := fmt.Sprintf("IBKR market data delayed/unusable for runtime probes: %s", strings.Join(summaries, "; "))
	at.updateMarketDataFeedStatus(true, summary, probeSymbols)
	at.syncMarketDataFeedDelayIncident(true, summary, details)
	return fmt.Errorf("market-data feed delayed: %s", summary)
}
