package risk

import "time"

type SupervisorMode string

const (
	SupervisorModeAllow           SupervisorMode = "allow"
	SupervisorModeReduceOnly      SupervisorMode = "reduce_only"
	SupervisorModeBlockNewEntries SupervisorMode = "block_new_entries"
	SupervisorModeHalted          SupervisorMode = "halted"
)

type IncidentSeverity string

const (
	IncidentSeverityInfo     IncidentSeverity = "info"
	IncidentSeverityWarning  IncidentSeverity = "warning"
	IncidentSeverityCritical IncidentSeverity = "critical"
)

type IncidentType string

const (
	IncidentMaxDailyLossBreached           IncidentType = "max_daily_loss_breached"
	IncidentMaxGrossExposureBreached       IncidentType = "max_gross_exposure_breached"
	IncidentMaxNetExposureBreached         IncidentType = "max_net_exposure_breached"
	IncidentMaxConcurrentPositionsBreached IncidentType = "max_concurrent_positions_breached"
	IncidentMaxSectorExposureBreached      IncidentType = "max_sector_exposure_breached"
	IncidentMaxCorrelatedPositionsBreached IncidentType = "max_correlated_positions_breached"
	IncidentExcessiveDrawdownDetected      IncidentType = "excessive_drawdown_detected"
	IncidentRepeatedBrokerDegradation      IncidentType = "repeated_broker_degradation"
	IncidentRepeatedReconciliationFailure  IncidentType = "repeated_reconciliation_failure"
	IncidentExcessiveOrderRejects          IncidentType = "excessive_order_rejects"
	IncidentPositionMismatchDetected       IncidentType = "position_mismatch_detected"
	IncidentKillSwitchActive               IncidentType = "kill_switch_active"
	IncidentLivePromotionFailed            IncidentType = "live_promotion_failed"
	IncidentStartupReadinessFailed         IncidentType = "startup_readiness_failed"
	IncidentBrokerRuntimeUnhealthy         IncidentType = "broker_runtime_unhealthy"
)

type Incident struct {
	ID           string            `json:"id"`
	Type         IncidentType      `json:"type"`
	Severity     IncidentSeverity  `json:"severity"`
	OpenedAt     time.Time         `json:"opened_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	ResolvedAt   *time.Time        `json:"resolved_at,omitempty"`
	Source       string            `json:"source"`
	Summary      string            `json:"summary"`
	Details      map[string]string `json:"details,omitempty"`
	EnforcedMode SupervisorMode    `json:"enforced_mode"`
	Active       bool              `json:"active"`
}

type SupervisorConfig struct {
	MaxRuntimeDegradationsPerSession    int
	MaxReconciliationFailuresPerSession int
	MaxOrderRejectsPerSession           int
	EnableReduceOnlyOnDrawdown          bool
}

type SupervisorSnapshot struct {
	Now time.Time

	ReadinessAllowed bool
	ReadinessMessage string

	PromotionRequired bool
	PromotionAllowed  bool
	PromotionMessage  string

	BrokerManaged bool
	BrokerHealthy bool
	BrokerState   string
	BrokerReason  string

	KillSwitchActive  bool
	KillSwitchMessage string

	PositionReconciliationManaged bool
	PositionReconciliationAllowed bool
	PositionReconciliationStatus  string
	PositionReconciliationSummary string

	StrictLiveRequired bool
	StrictLiveHealthy  bool
	StrictLiveMessage  string

	SessionDailyPnL       float64
	SessionDailyLossLimit float64
	StopTradingUntil      time.Time

	CurrentPositionCount   int
	MaxConcurrentPositions int

	PortfolioRiskAvailable   bool
	PortfolioRiskSummary     string
	CurrentGrossExposurePct  float64
	MaxGrossExposurePct      float64
	CurrentNetExposurePct    float64
	MaxNetExposurePct        float64
	LargestSector            string
	LargestSectorExposurePct float64
	MaxSectorExposurePct     float64
	CorrelatedPositionCount  int
	MaxCorrelatedPositions   int
	CurrentDrawdownPct       float64
	MaxDrawdownPct           float64

	BrokerDegradationEvents     int
	ReconciliationFailureEvents int
	OrderRejectEvents           int
}

type SupervisorState struct {
	EvaluatedAt           time.Time      `json:"evaluated_at"`
	Mode                  SupervisorMode `json:"mode"`
	TradingAllowed        bool           `json:"trading_allowed"`
	EntriesAllowed        bool           `json:"entries_allowed"`
	ExitsAllowed          bool           `json:"exits_allowed"`
	ReduceOnly            bool           `json:"reduce_only"`
	Summary               string         `json:"summary"`
	ActiveIncidentCount   int            `json:"active_incident_count"`
	CriticalIncidentCount int            `json:"critical_incident_count"`
	Incidents             []Incident     `json:"incidents"`
}

func (m SupervisorMode) priority() int {
	switch m {
	case SupervisorModeHalted:
		return 4
	case SupervisorModeReduceOnly:
		return 3
	case SupervisorModeBlockNewEntries:
		return 2
	default:
		return 1
	}
}

func (m SupervisorMode) TradingAllowed() bool {
	switch m {
	case SupervisorModeAllow, SupervisorModeReduceOnly, SupervisorModeBlockNewEntries:
		return true
	default:
		return false
	}
}

func (m SupervisorMode) EntriesAllowed() bool {
	return m == SupervisorModeAllow
}

func (m SupervisorMode) ExitsAllowed() bool {
	return m != SupervisorModeHalted
}

func (m SupervisorMode) IsReduceOnly() bool {
	return m == SupervisorModeReduceOnly
}
