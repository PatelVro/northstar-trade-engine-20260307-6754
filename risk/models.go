package risk

type Outcome string

const (
	OutcomePass       Outcome = "pass"
	OutcomeReject     Outcome = "reject"
	OutcomeReduceSize Outcome = "reduce_size"
)

type RuleStatus string

const (
	RulePass       RuleStatus = "pass"
	RuleReject     RuleStatus = "reject"
	RuleReduceSize RuleStatus = "reduce_size"
)

type Config struct {
	MaxPositionPct         float64
	MaxPortfolioExposure   float64
	MaxNetExposurePct      float64
	MaxSectorExposurePct   float64
	MaxConcurrentPositions int
	MaxCorrelatedPositions int
	MaxDailyLossPct        float64
	MaxDrawdownStopPct     float64
	MaxPerTradeRiskPct     float64
	MaxPairCorrelation     float64
	MaxGrossLeverage       float64
	MinAverageDollarVolume float64
	MaxParticipationRate   float64
	CashBufferPct          float64
}

type AccountSnapshot struct {
	StrategyInitialCapital float64
	StrategyEquity         float64
	AccountEquity          float64
	AvailableBalance       float64
	GrossMarketValue       float64
	DailyPnL               float64
	DailyLossLimit         float64
	PeakStrategyEquity     float64
	PositionCount          int
}

type PositionSnapshot struct {
	Symbol      string
	Side        string
	Quantity    float64
	MarketValue float64
	Sector      string
	SectorKnown bool
}

type MarketSnapshot struct {
	Symbol                       string
	CurrentPrice                 float64
	AverageVolume                float64
	CurrentVolume                float64
	AverageDollarVolume          float64
	Tradable                     bool
	TradableKnown                bool
	TradableReason               string
	Halted                       bool
	HaltedKnown                  bool
	HaltReason                   string
	Sector                       string
	SectorKnown                  bool
	CorrelationKnown             bool
	MaxObservedCorrelation       float64
	MaxObservedCorrelationSymbol string
	CorrelatedPositionCount      int
	CorrelatedSymbols            []string
}

type OrderRequest struct {
	Symbol            string
	Action            string
	Side              string
	RequestedQuantity float64
	RequestedNotional float64
	StopLoss          float64
	IsEntry           bool
	IsExit            bool
}

type RuleResult struct {
	Name             string     `json:"name"`
	Status           RuleStatus `json:"status"`
	Message          string     `json:"message"`
	ApprovedQuantity float64    `json:"approved_quantity,omitempty"`
	ApprovedNotional float64    `json:"approved_notional,omitempty"`
}

type PortfolioMetrics struct {
	CurrentGrossExposure            float64            `json:"current_gross_exposure"`
	CurrentGrossExposurePct         float64            `json:"current_gross_exposure_pct"`
	ProjectedGrossExposure          float64            `json:"projected_gross_exposure"`
	ProjectedGrossExposurePct       float64            `json:"projected_gross_exposure_pct"`
	CurrentNetExposure              float64            `json:"current_net_exposure"`
	CurrentNetExposurePct           float64            `json:"current_net_exposure_pct"`
	ProjectedNetExposure            float64            `json:"projected_net_exposure"`
	ProjectedNetExposurePct         float64            `json:"projected_net_exposure_pct"`
	SectorExposure                  map[string]float64 `json:"sector_exposure,omitempty"`
	SectorExposurePct               map[string]float64 `json:"sector_exposure_pct,omitempty"`
	LargestSector                   string             `json:"largest_sector,omitempty"`
	LargestSectorExposure           float64            `json:"largest_sector_exposure,omitempty"`
	LargestSectorExposurePct        float64            `json:"largest_sector_exposure_pct,omitempty"`
	UnclassifiedExposure            float64            `json:"unclassified_exposure,omitempty"`
	UnclassifiedExposurePct         float64            `json:"unclassified_exposure_pct,omitempty"`
	OrderSector                     string             `json:"order_sector,omitempty"`
	OrderSectorKnown                bool               `json:"order_sector_known,omitempty"`
	ProjectedOrderSectorExposure    float64            `json:"projected_order_sector_exposure,omitempty"`
	ProjectedOrderSectorExposurePct float64            `json:"projected_order_sector_exposure_pct,omitempty"`
	CorrelatedPositionCount         int                `json:"correlated_position_count,omitempty"`
	MaxObservedCorrelation          float64            `json:"max_observed_correlation,omitempty"`
	MaxObservedCorrelationSymbol    string             `json:"max_observed_correlation_symbol,omitempty"`
	CorrelatedSymbols               []string           `json:"correlated_symbols,omitempty"`
	CurrentDrawdownPct              float64            `json:"current_drawdown_pct,omitempty"`
	PeakStrategyEquity              float64            `json:"peak_strategy_equity,omitempty"`
}

type Evaluation struct {
	Outcome           Outcome          `json:"outcome"`
	Summary           string           `json:"summary"`
	RequestedQuantity float64          `json:"requested_quantity,omitempty"`
	RequestedNotional float64          `json:"requested_notional,omitempty"`
	ApprovedQuantity  float64          `json:"approved_quantity,omitempty"`
	ApprovedNotional  float64          `json:"approved_notional,omitempty"`
	Portfolio         PortfolioMetrics `json:"portfolio"`
	RuleResults       []RuleResult     `json:"rule_results,omitempty"`
}

func (o Outcome) TradingAllowed() bool {
	return o != OutcomeReject
}

func (m PortfolioMetrics) Clone() PortfolioMetrics {
	cloned := m
	if len(m.SectorExposure) > 0 {
		cloned.SectorExposure = make(map[string]float64, len(m.SectorExposure))
		for key, value := range m.SectorExposure {
			cloned.SectorExposure[key] = value
		}
	}
	if len(m.SectorExposurePct) > 0 {
		cloned.SectorExposurePct = make(map[string]float64, len(m.SectorExposurePct))
		for key, value := range m.SectorExposurePct {
			cloned.SectorExposurePct[key] = value
		}
	}
	if len(m.CorrelatedSymbols) > 0 {
		cloned.CorrelatedSymbols = append([]string(nil), m.CorrelatedSymbols...)
	}
	return cloned
}
