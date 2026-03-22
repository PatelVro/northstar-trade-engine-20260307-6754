package regime

import "time"

const (
	SchemaVersion   = 1
	DetectorVersion = 1
)

type Regime string

const (
	RegimeUnknown        Regime = "unknown"
	RegimeTrend          Regime = "trend"
	RegimeMeanReversion  Regime = "mean_reversion"
	RegimeHighVolatility Regime = "high_volatility"
	RegimeLowVolatility  Regime = "low_volatility"
	RegimeUnstable       Regime = "unstable"
)

type Contribution struct {
	Name         string  `json:"name"`
	Value        float64 `json:"value"`
	Contribution float64 `json:"contribution"`
	Note         string  `json:"note,omitempty"`
}

type Result struct {
	SchemaVersion      int            `json:"schema_version"`
	DetectorVersion    int            `json:"detector_version"`
	Symbol             string         `json:"symbol"`
	Timestamp          time.Time      `json:"timestamp"`
	Timeframe          string         `json:"timeframe"`
	Regime             Regime         `json:"regime"`
	RegimeScore        float64        `json:"regime_score"`
	TrendScore         float64        `json:"trend_score"`
	MeanReversionScore float64        `json:"mean_reversion_score"`
	VolatilityScore    float64        `json:"volatility_score"`
	LowVolatilityScore float64        `json:"low_volatility_score"`
	InstabilityScore   float64        `json:"instability_score"`
	Confidence         float64        `json:"confidence"`
	Explanation        string         `json:"explanation"`
	Contributing       []Contribution `json:"contributing_features,omitempty"`
	Valid              bool           `json:"valid"`
	Warnings           []string       `json:"warnings,omitempty"`
}

type ResultSet struct {
	SchemaVersion int                `json:"schema_version"`
	Symbol        string             `json:"symbol"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Results       map[string]*Result `json:"results"`
}

func (s *ResultSet) Result(timeframe string) *Result {
	if s == nil || s.Results == nil {
		return nil
	}
	return s.Results[timeframe]
}
