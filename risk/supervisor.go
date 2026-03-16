package risk

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type Supervisor struct {
	mu        sync.Mutex
	config    SupervisorConfig
	incidents map[IncidentType]Incident
}

func NewSupervisor(config SupervisorConfig) *Supervisor {
	if config.MaxRuntimeDegradationsPerSession <= 0 {
		config.MaxRuntimeDegradationsPerSession = 3
	}
	if config.MaxReconciliationFailuresPerSession <= 0 {
		config.MaxReconciliationFailuresPerSession = 3
	}
	if config.MaxOrderRejectsPerSession <= 0 {
		config.MaxOrderRejectsPerSession = 5
	}
	return &Supervisor{
		config:    config,
		incidents: make(map[IncidentType]Incident),
	}
}

func (s *Supervisor) Evaluate(snapshot SupervisorSnapshot) SupervisorState {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := snapshot.Now
	if now.IsZero() {
		now = time.Now()
	}

	desired := evaluateSupervisorIncidents(snapshot, s.config, now)
	desiredByType := make(map[IncidentType]Incident, len(desired))
	for _, incident := range desired {
		desiredByType[incident.Type] = incident
	}

	for incidentType, incident := range s.incidents {
		if next, ok := desiredByType[incidentType]; ok {
			next.ID = incident.ID
			next.OpenedAt = incident.OpenedAt
			next.UpdatedAt = now
			next.ResolvedAt = nil
			next.Active = true
			s.incidents[incidentType] = next
			continue
		}
		if incident.Active {
			resolvedAt := now
			incident.Active = false
			incident.UpdatedAt = now
			incident.ResolvedAt = &resolvedAt
			s.incidents[incidentType] = incident
		}
	}

	for incidentType, incident := range desiredByType {
		if existing, ok := s.incidents[incidentType]; ok {
			if existing.ID != "" {
				incident.ID = existing.ID
			}
			if !existing.OpenedAt.IsZero() {
				incident.OpenedAt = existing.OpenedAt
			}
		}
		if incident.ID == "" {
			incident.ID = string(incident.Type)
		}
		if incident.OpenedAt.IsZero() {
			incident.OpenedAt = now
		}
		incident.UpdatedAt = now
		incident.ResolvedAt = nil
		incident.Active = true
		s.incidents[incidentType] = incident
	}

	active := make([]Incident, 0, len(s.incidents))
	for _, incident := range s.incidents {
		if !incident.Active {
			continue
		}
		active = append(active, cloneIncident(incident))
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].EnforcedMode.priority() != active[j].EnforcedMode.priority() {
			return active[i].EnforcedMode.priority() > active[j].EnforcedMode.priority()
		}
		if active[i].Severity != active[j].Severity {
			return active[i].Severity > active[j].Severity
		}
		return active[i].Type < active[j].Type
	})

	mode := SupervisorModeAllow
	critical := 0
	for _, incident := range active {
		if incident.Severity == IncidentSeverityCritical {
			critical++
		}
		if incident.EnforcedMode.priority() > mode.priority() {
			mode = incident.EnforcedMode
		}
	}

	summary := "risk supervisor allows normal trading"
	if len(active) > 0 {
		summary = fmt.Sprintf("%s enforced by %d active incident(s)", mode, len(active))
		if active[0].Summary != "" {
			summary = active[0].Summary
		}
	}

	return SupervisorState{
		EvaluatedAt:           now,
		Mode:                  mode,
		TradingAllowed:        mode.TradingAllowed(),
		EntriesAllowed:        mode.EntriesAllowed(),
		ExitsAllowed:          mode.ExitsAllowed(),
		ReduceOnly:            mode.IsReduceOnly(),
		Summary:               summary,
		ActiveIncidentCount:   len(active),
		CriticalIncidentCount: critical,
		Incidents:             active,
	}
}

func evaluateSupervisorIncidents(snapshot SupervisorSnapshot, config SupervisorConfig, now time.Time) []Incident {
	incidents := make([]Incident, 0, 10)
	add := func(active bool, incident Incident) {
		if !active {
			return
		}
		if incident.ID == "" {
			incident.ID = string(incident.Type)
		}
		if incident.OpenedAt.IsZero() {
			incident.OpenedAt = now
		}
		incident.UpdatedAt = now
		incident.Active = true
		incidents = append(incidents, incident)
	}

	if !snapshot.ReadinessAllowed {
		message := normalizeSupervisorMessage(snapshot.ReadinessMessage, "startup readiness failed")
		add(true, newIncident(IncidentStartupReadinessFailed, IncidentSeverityCritical, "readiness", message, SupervisorModeHalted, nil))
	}

	if snapshot.PromotionRequired && !snapshot.PromotionAllowed {
		message := normalizeSupervisorMessage(snapshot.PromotionMessage, "live promotion checklist failed")
		add(true, newIncident(IncidentLivePromotionFailed, IncidentSeverityCritical, "promotion", message, SupervisorModeHalted, nil))
	}

	if snapshot.KillSwitchActive {
		message := normalizeSupervisorMessage(snapshot.KillSwitchMessage, "emergency kill switch active")
		add(true, newIncident(IncidentKillSwitchActive, IncidentSeverityCritical, "kill_switch", message, SupervisorModeHalted, nil))
	}

	if snapshot.BrokerManaged && !snapshot.BrokerHealthy {
		message := normalizeSupervisorMessage(snapshot.BrokerReason, "broker runtime is unhealthy")
		add(true, newIncident(IncidentBrokerRuntimeUnhealthy, IncidentSeverityCritical, "broker_runtime", message, SupervisorModeHalted, map[string]string{
			"state": snapshot.BrokerState,
		}))
	} else if snapshot.StrictLiveRequired && !snapshot.StrictLiveHealthy {
		message := normalizeSupervisorMessage(snapshot.StrictLiveMessage, "strict live readiness check failed")
		add(true, newIncident(IncidentBrokerRuntimeUnhealthy, IncidentSeverityCritical, "strict_live", message, SupervisorModeHalted, nil))
	}

	if snapshot.PositionReconciliationManaged && !snapshot.PositionReconciliationAllowed {
		message := normalizeSupervisorMessage(snapshot.PositionReconciliationSummary, "position reconciliation detected a broker/local mismatch")
		add(true, newIncident(IncidentPositionMismatchDetected, IncidentSeverityCritical, "position_reconciliation", message, SupervisorModeHalted, map[string]string{
			"status": snapshot.PositionReconciliationStatus,
		}))
	}

	if snapshot.SessionDailyLossLimit < 0 && snapshot.SessionDailyPnL <= snapshot.SessionDailyLossLimit {
		message := fmt.Sprintf("daily loss breached: pnl %.2f <= limit %.2f", snapshot.SessionDailyPnL, snapshot.SessionDailyLossLimit)
		add(true, newIncident(IncidentMaxDailyLossBreached, IncidentSeverityCritical, "accounting", message, SupervisorModeHalted, nil))
	} else if !snapshot.StopTradingUntil.IsZero() && snapshot.StopTradingUntil.After(now) {
		message := fmt.Sprintf("daily loss cooldown active until %s", snapshot.StopTradingUntil.Format(time.RFC3339))
		add(true, newIncident(IncidentMaxDailyLossBreached, IncidentSeverityCritical, "accounting", message, SupervisorModeHalted, nil))
	}

	if snapshot.MaxConcurrentPositions > 0 && snapshot.CurrentPositionCount > snapshot.MaxConcurrentPositions {
		message := fmt.Sprintf("open positions %d exceed max concurrent positions %d", snapshot.CurrentPositionCount, snapshot.MaxConcurrentPositions)
		add(true, newIncident(IncidentMaxConcurrentPositionsBreached, IncidentSeverityWarning, "portfolio", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.CurrentGrossExposurePct > 0 && snapshot.MaxGrossExposurePct > 0 && snapshot.CurrentGrossExposurePct > snapshot.MaxGrossExposurePct+1e-9 {
		message := fmt.Sprintf("gross exposure %.2f%% exceeds limit %.2f%%", snapshot.CurrentGrossExposurePct*100.0, snapshot.MaxGrossExposurePct*100.0)
		add(true, newIncident(IncidentMaxGrossExposureBreached, IncidentSeverityWarning, "portfolio", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.MaxNetExposurePct > 0 && math.Abs(snapshot.CurrentNetExposurePct) > snapshot.MaxNetExposurePct+1e-9 {
		message := fmt.Sprintf("net exposure %.2f%% exceeds limit %.2f%%", math.Abs(snapshot.CurrentNetExposurePct)*100.0, snapshot.MaxNetExposurePct*100.0)
		add(true, newIncident(IncidentMaxNetExposureBreached, IncidentSeverityWarning, "portfolio", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.MaxSectorExposurePct > 0 && snapshot.LargestSectorExposurePct > snapshot.MaxSectorExposurePct+1e-9 {
		sector := strings.TrimSpace(snapshot.LargestSector)
		if sector == "" {
			sector = "unknown"
		}
		message := fmt.Sprintf("%s sector exposure %.2f%% exceeds limit %.2f%%", sector, snapshot.LargestSectorExposurePct*100.0, snapshot.MaxSectorExposurePct*100.0)
		add(true, newIncident(IncidentMaxSectorExposureBreached, IncidentSeverityWarning, "portfolio", message, SupervisorModeBlockNewEntries, map[string]string{
			"sector": sector,
		}))
	}

	if snapshot.MaxCorrelatedPositions > 0 && snapshot.CorrelatedPositionCount > snapshot.MaxCorrelatedPositions {
		message := fmt.Sprintf("correlated positions %d exceed limit %d", snapshot.CorrelatedPositionCount, snapshot.MaxCorrelatedPositions)
		add(true, newIncident(IncidentMaxCorrelatedPositionsBreached, IncidentSeverityWarning, "portfolio", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.MaxDrawdownPct > 0 && snapshot.CurrentDrawdownPct > snapshot.MaxDrawdownPct+1e-9 {
		message := fmt.Sprintf("current drawdown %.2f%% exceeds limit %.2f%%", snapshot.CurrentDrawdownPct*100.0, snapshot.MaxDrawdownPct*100.0)
		mode := SupervisorModeBlockNewEntries
		if config.EnableReduceOnlyOnDrawdown {
			mode = SupervisorModeReduceOnly
		}
		add(true, newIncident(IncidentExcessiveDrawdownDetected, IncidentSeverityWarning, "portfolio", message, mode, nil))
	}

	if snapshot.BrokerDegradationEvents >= config.MaxRuntimeDegradationsPerSession {
		message := fmt.Sprintf("broker degraded %d times this session (limit %d)", snapshot.BrokerDegradationEvents, config.MaxRuntimeDegradationsPerSession)
		add(true, newIncident(IncidentRepeatedBrokerDegradation, IncidentSeverityWarning, "broker_runtime", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.ReconciliationFailureEvents >= config.MaxReconciliationFailuresPerSession {
		message := fmt.Sprintf("reconciliation failed %d times this session (limit %d)", snapshot.ReconciliationFailureEvents, config.MaxReconciliationFailuresPerSession)
		add(true, newIncident(IncidentRepeatedReconciliationFailure, IncidentSeverityWarning, "reconciliation", message, SupervisorModeBlockNewEntries, nil))
	}

	if snapshot.OrderRejectEvents >= config.MaxOrderRejectsPerSession {
		message := fmt.Sprintf("orders were rejected %d times this session (limit %d)", snapshot.OrderRejectEvents, config.MaxOrderRejectsPerSession)
		add(true, newIncident(IncidentExcessiveOrderRejects, IncidentSeverityWarning, "execution", message, SupervisorModeBlockNewEntries, nil))
	}

	return incidents
}

func newIncident(incidentType IncidentType, severity IncidentSeverity, source, summary string, mode SupervisorMode, details map[string]string) Incident {
	clonedDetails := map[string]string(nil)
	if len(details) > 0 {
		clonedDetails = make(map[string]string, len(details))
		for key, value := range details {
			clonedDetails[key] = value
		}
	}
	return Incident{
		ID:           string(incidentType),
		Type:         incidentType,
		Severity:     severity,
		Source:       strings.TrimSpace(source),
		Summary:      strings.TrimSpace(summary),
		Details:      clonedDetails,
		EnforcedMode: mode,
	}
}

func normalizeSupervisorMessage(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func cloneIncident(incident Incident) Incident {
	cloned := incident
	if len(incident.Details) > 0 {
		cloned.Details = make(map[string]string, len(incident.Details))
		for key, value := range incident.Details {
			cloned.Details[key] = value
		}
	}
	if incident.ResolvedAt != nil {
		resolvedAt := *incident.ResolvedAt
		cloned.ResolvedAt = &resolvedAt
	}
	return cloned
}
