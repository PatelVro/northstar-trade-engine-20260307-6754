package trader

import (
	"errors"
	"fmt"
	"log"
	dataquality "northstar/data"
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

type dataQualityState struct {
	LastCheckedAt  time.Time
	TotalChecks    int
	TotalFailures  int
	BlockedSymbols map[string]dataQualityBlockedSymbol
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
		Now:            time.Now().UTC(),
		CheckStaleness: !at.backtestMode && !strings.EqualFold(at.config.Mode, "replay"),
	}
}

func (at *AutoTrader) getValidatedMarketData(symbol string) (*market.Data, error) {
	req := market.GetRequest{
		Symbol:            symbol,
		Provider:          at.provider,
		InstrumentType:    at.config.InstrumentType,
		BarsAdjustment:    at.config.BarsAdjustment,
		ValidationOptions: at.currentDataValidationOptions(),
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
		at.dataQualityState.TotalFailures++
		res := validationErr.Result
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
	}
}
