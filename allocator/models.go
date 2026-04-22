package allocator

import (
	"time"

	"northstar/selector"
)

const (
	SchemaVersion    = 1
	AllocatorVersion = 1
)

type Config struct {
	DynamicSizing            bool
	BaseRiskPerTradePct      float64
	FallbackPositionPct      float64
	MaxPositionPct           float64
	MaxGrossExposurePct      float64
	MaxNetExposurePct        float64
	CashBufferPct            float64
	DrawdownThrottleStartPct float64
	DrawdownThrottleMinScale float64
	VolatilityTargetPct      float64
	VolatilityMinScale       float64
	MinTradeNotional         float64
}

type AccountSnapshot struct {
	StrategyEquity        float64
	AccountEquity         float64
	AvailableBalance      float64
	CurrentGrossExposure  float64
	CurrentNetExposure    float64
	CurrentSymbolExposure float64
	PeakStrategyEquity    float64
}

type Input struct {
	Symbol            string
	Action            string
	EntryPrice        float64
	StopLoss          float64
	CurrentPrice      float64
	IncreasesExposure bool
	Selection         *selector.Selection
	Account           AccountSnapshot
	Config            Config
	ATR14Pct          float64
	RealizedVol20     float64
	// FractionalQuantity disables the "cannot buy one share" capacity check
	// for instruments (crypto, perps) where sub-unit positions are allowed.
	// For equity this stays false; one-share minimum remains enforced.
	FractionalQuantity bool
}

type Result struct {
	SchemaVersion        int                 `json:"schema_version"`
	AllocatorVersion     int                 `json:"allocator_version"`
	Symbol               string              `json:"symbol"`
	Timestamp            time.Time           `json:"timestamp"`
	AllowTrade           bool                `json:"allow_trade"`
	RecommendedQuantity  float64             `json:"recommended_quantity"`
	RecommendedNotional  float64             `json:"recommended_notional"`
	TargetPositionPct    float64             `json:"target_position_pct"`
	RiskBudgetUsed       float64             `json:"risk_budget_used"`
	BaseRiskBudget       float64             `json:"base_risk_budget"`
	BaseNotional         float64             `json:"base_notional"`
	VolatilityAdjustment float64             `json:"volatility_adjustment"`
	ConfidenceAdjustment float64             `json:"confidence_adjustment"`
	RegimeAdjustment     float64             `json:"regime_adjustment"`
	DrawdownAdjustment   float64             `json:"drawdown_adjustment"`
	CapacityAdjustment   float64             `json:"capacity_adjustment"`
	SizingReason         string              `json:"sizing_reason"`
	ReducedSize          bool                `json:"reduced_size"`
	Warnings             []string            `json:"warnings,omitempty"`
	Selection            *selector.Selection `json:"selection,omitempty"`
}
