package incidents

import "time"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type State string

const (
	StateOpen         State = "open"
	StateAcknowledged State = "acknowledged"
	StateResolved     State = "resolved"
	StateEscalated    State = "escalated"
)

type Type string

const (
	TypeBrokerRuntimeDegraded        Type = "broker_runtime_degraded"
	TypeBrokerRuntimeReconnectFailed Type = "broker_runtime_reconnect_failed"
	TypeBrokerRuntimeReconcileFailed Type = "broker_runtime_reconciliation_failed"
	TypeStartupReadinessFailed       Type = "startup_readiness_failed"
	TypeLivePromotionFailed          Type = "live_promotion_failed"
	TypeRiskSupervisorHalted         Type = "risk_supervisor_halted"
	TypeDailyLossBreached            Type = "daily_loss_breached"
	TypeDrawdownBreached             Type = "drawdown_breached"
	TypeKillSwitchActivated          Type = "kill_switch_activated"
	TypeExcessiveOrderRejects        Type = "excessive_order_rejects"
	TypePositionReconciliationFailed Type = "position_reconciliation_failed"
	TypePositionMismatchDetected     Type = "position_mismatch_detected"
	TypeOrderReconciliationFailed    Type = "order_reconciliation_failed"
	TypeSymbolDataQualityBlocked     Type = "symbol_data_quality_blocked"
	TypeMarketDataValidationFailed   Type = "market_data_validation_failed"
)

type Incident struct {
	IncidentID         string            `json:"incident_id"`
	IncidentType       Type              `json:"incident_type"`
	Severity           Severity          `json:"severity"`
	State              State             `json:"state"`
	TraderID           string            `json:"trader_id"`
	OpenedAt           time.Time         `json:"opened_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	AcknowledgedAt     *time.Time        `json:"acknowledged_at,omitempty"`
	ResolvedAt         *time.Time        `json:"resolved_at,omitempty"`
	FirstSeenAt        time.Time         `json:"first_seen_at"`
	LastSeenAt         time.Time         `json:"last_seen_at"`
	OccurrenceCount    int               `json:"occurrence_count"`
	Summary            string            `json:"summary"`
	Details            map[string]string `json:"details,omitempty"`
	Source             string            `json:"source"`
	CurrentStatus      string            `json:"current_status"`
	RecommendedActions []string          `json:"recommended_actions,omitempty"`
	Active             bool              `json:"active"`
	CorrelationKey     string            `json:"correlation_key"`
}

type Signal struct {
	IncidentType  Type
	Severity      Severity
	TraderID      string
	Summary       string
	Details       map[string]string
	Source        string
	CurrentStatus string
	Symbol        string
	ExtraKey      string
	Escalate      bool
	OccurredAt    time.Time
}

type Summary struct {
	OpenCount                 int        `json:"open_count"`
	AcknowledgedCount         int        `json:"acknowledged_count"`
	CriticalOpenCount         int        `json:"critical_open_count"`
	LatestIncidentAt          time.Time  `json:"latest_incident_at"`
	LatestIncidentSummary     string     `json:"latest_incident_summary"`
	LatestIncidentSeverity    Severity   `json:"latest_incident_severity"`
	LatestIncidentRunbookHint string     `json:"latest_incident_runbook_hint"`
	LatestCriticalIncident    *Incident  `json:"latest_critical_incident,omitempty"`
	OpenIncidents             []Incident `json:"open_incidents"`
	RecentResolvedIncidents   []Incident `json:"recent_resolved_incidents"`
}

func (i Incident) Clone() Incident {
	cloned := i
	if len(i.Details) > 0 {
		cloned.Details = make(map[string]string, len(i.Details))
		for key, value := range i.Details {
			cloned.Details[key] = value
		}
	}
	if len(i.RecommendedActions) > 0 {
		cloned.RecommendedActions = append([]string(nil), i.RecommendedActions...)
	}
	if i.AcknowledgedAt != nil {
		at := *i.AcknowledgedAt
		cloned.AcknowledgedAt = &at
	}
	if i.ResolvedAt != nil {
		rt := *i.ResolvedAt
		cloned.ResolvedAt = &rt
	}
	return cloned
}
