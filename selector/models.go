package selector

import (
	"time"

	"northstar/regime"
)

const (
	SchemaVersion   = 1
	SelectorVersion = 1
)

type StrategyFamily string

const (
	StrategyFamilyMomentum      StrategyFamily = "momentum"
	StrategyFamilyMeanReversion StrategyFamily = "mean_reversion"
	StrategyFamilyHybrid        StrategyFamily = "hybrid"
	StrategyFamilyDefensive     StrategyFamily = "defensive"
	StrategyFamilyNoTrade       StrategyFamily = "no_trade"
)

type RiskMode string

const (
	RiskModeNormal      RiskMode = "normal"
	RiskModeReducedRisk RiskMode = "reduced_risk"
	RiskModeNoTrade     RiskMode = "no_trade"
)

type Selection struct {
	SchemaVersion       int            `json:"schema_version"`
	SelectorVersion     int            `json:"selector_version"`
	Symbol              string         `json:"symbol"`
	Timestamp           time.Time      `json:"timestamp"`
	Timeframe           string         `json:"timeframe"`
	SelectedFamily      StrategyFamily `json:"selected_family"`
	SelectedStrategy    string         `json:"selected_strategy"`
	SelectionScore      float64        `json:"selection_score"`
	SelectionReason     string         `json:"selection_reason"`
	Confidence          float64        `json:"confidence"`
	AllowTrading        bool           `json:"allow_trading"`
	RecommendedRiskMode RiskMode       `json:"recommended_risk_mode"`
	FallbackStrategy    string         `json:"fallback_strategy"`
	Regime              regime.Regime  `json:"regime"`
	RegimeConfidence    float64        `json:"regime_confidence"`
	ContributingSignals []string       `json:"contributing_signals,omitempty"`
	Valid               bool           `json:"valid"`
	Warnings            []string       `json:"warnings,omitempty"`
}

type SelectionSet struct {
	SchemaVersion int                   `json:"schema_version"`
	Symbol        string                `json:"symbol"`
	GeneratedAt   time.Time             `json:"generated_at"`
	Selections    map[string]*Selection `json:"selections"`
}

func (s *SelectionSet) Selection(timeframe string) *Selection {
	if s == nil || s.Selections == nil {
		return nil
	}
	return s.Selections[timeframe]
}
