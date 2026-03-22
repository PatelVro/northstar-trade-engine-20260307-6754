package dataquality

import (
	"fmt"
	"time"
)

type Bar struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

type IssueType string

const (
	IssueStaleData        IssueType = "stale_data"
	IssueZeroVolume       IssueType = "zero_volume"
	IssueExtremePriceJump IssueType = "extreme_price_jump"
	IssueMissingBars      IssueType = "missing_bars"
	IssueMarketClosed     IssueType = "market_closed"
)

type Issue struct {
	Type    IssueType `json:"type"`
	Message string    `json:"message"`
}

type Options struct {
	Now                   time.Time
	CheckStaleness        bool
	ExpectedBars          int
	StaleAfterBars        int
	MaxPriceJumpPct       float64
	MissingBarGapMultiple float64
	InstrumentType        string
}

type Result struct {
	Symbol        string    `json:"symbol"`
	Interval      string    `json:"interval"`
	CheckedAt     time.Time `json:"checked_at"`
	ExpectedBars  int       `json:"expected_bars"`
	ActualBars    int       `json:"actual_bars"`
	LatestBarTime time.Time `json:"latest_bar_time"`
	MaxJumpPct    float64   `json:"max_jump_pct"`
	Issues        []Issue   `json:"issues,omitempty"`
	Summary       string    `json:"summary"`
}

func (r Result) Failed() bool {
	return len(r.Issues) > 0
}

type ValidationError struct {
	Result Result
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Result.Summary != "" {
		return e.Result.Summary
	}
	return fmt.Sprintf("data validation failed for %s %s", e.Result.Symbol, e.Result.Interval)
}
