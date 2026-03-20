package trader

import (
	"fmt"
	"northstar/incidents"
	"northstar/positions"
	"northstar/risk"
	"strings"
	"time"
)

func (at *AutoTrader) currentIncidentSummary() incidents.Summary {
	if at == nil || at.incidentManager == nil {
		return incidents.Summary{
			OpenIncidents:           []incidents.Incident{},
			RecentResolvedIncidents: []incidents.Incident{},
		}
	}
	return at.incidentManager.Summary()
}

func (at *AutoTrader) observeIncident(signal incidents.Signal) incidents.Incident {
	if at == nil || at.incidentManager == nil {
		return incidents.Incident{}
	}
	signal.TraderID = at.id
	incident, _ := at.incidentManager.Observe(signal)
	at.observePaperSessionIncident(incident)
	return incident
}

func (at *AutoTrader) resolveIncident(signal incidents.Signal, status string) {
	if at == nil || at.incidentManager == nil {
		return
	}
	signal.TraderID = at.id
	if incident, ok := at.incidentManager.Resolve(signal, status); ok {
		at.observePaperSessionIncident(incident)
	}
}

func (at *AutoTrader) syncReadinessIncident(summary ReadinessSummary) {
	if isPendingReadinessSummary(summary) {
		return
	}
	signal := incidents.Signal{
		IncidentType:  incidents.TypeStartupReadinessFailed,
		Severity:      incidents.SeverityCritical,
		Source:        "readiness",
		Summary:       strings.TrimSpace(summary.Message),
		CurrentStatus: strings.TrimSpace(summary.Message),
		OccurredAt:    summary.CheckedAt,
	}
	if !summary.TradingAllowed {
		at.observeIncident(signal)
		return
	}
	at.resolveIncident(signal, "startup readiness passed")
}

func (at *AutoTrader) syncPromotionIncident(summary PromotionSummary) {
	if summary.Required && strings.EqualFold(strings.TrimSpace(summary.Message), "live promotion checklist pending") {
		return
	}
	if !summary.Required {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeLivePromotionFailed,
			Source:       "promotion",
			OccurredAt:   summary.CheckedAt,
		}, "live promotion not required")
		return
	}

	signal := incidents.Signal{
		IncidentType:  incidents.TypeLivePromotionFailed,
		Severity:      incidents.SeverityCritical,
		Source:        "promotion",
		Summary:       strings.TrimSpace(summary.Message),
		CurrentStatus: strings.TrimSpace(summary.Message),
		OccurredAt:    summary.CheckedAt,
	}
	if !summary.LiveTradingAllowed {
		at.observeIncident(signal)
		return
	}
	at.resolveIncident(signal, "live promotion checklist passed")
}

func (at *AutoTrader) syncBrokerRuntimeIncident(state BrokerRuntimeState, reason string) {
	signal := incidents.Signal{
		IncidentType:  incidents.TypeBrokerRuntimeDegraded,
		Severity:      incidents.SeverityWarning,
		Source:        "broker_runtime",
		Summary:       strings.TrimSpace(reason),
		CurrentStatus: strings.TrimSpace(reason),
		OccurredAt:    time.Now().UTC(),
	}

	switch state {
	case BrokerRuntimeHealthy:
		at.resolveIncident(signal, "broker runtime healthy")
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeBrokerRuntimeReconnectFailed,
			Source:       "reconciliation",
			ExtraKey:     "recovery_loop",
			OccurredAt:   signal.OccurredAt,
		}, "broker runtime healthy")
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeBrokerRuntimeReconcileFailed,
			Source:       "reconciliation",
			OccurredAt:   signal.OccurredAt,
		}, "broker runtime healthy")
	case BrokerRuntimePaused:
		signal.Severity = incidents.SeverityCritical
		if signal.Summary == "" {
			signal.Summary = "broker runtime paused"
		}
		signal.CurrentStatus = signal.Summary
		at.observeIncident(signal)
	case BrokerRuntimeDegraded, BrokerRuntimeReconnecting, BrokerRuntimeReconciling:
		if signal.Summary == "" {
			signal.Summary = "broker runtime degraded"
		}
		signal.CurrentStatus = signal.Summary
		at.observeIncident(signal)
	}
}

func (at *AutoTrader) syncRiskSupervisorIncidents(state risk.SupervisorState) {
	hasDailyLoss := false
	hasDrawdown := false
	hasOrderRejects := false
	hasSupervisorHalt := false

	for _, incident := range state.Incidents {
		switch incident.Type {
		case risk.IncidentMaxDailyLossBreached:
			hasDailyLoss = true
			at.observeIncident(incidents.Signal{
				IncidentType:  incidents.TypeDailyLossBreached,
				Severity:      incidents.SeverityCritical,
				Source:        "risk_supervisor",
				Summary:       strings.TrimSpace(incident.Summary),
				CurrentStatus: strings.TrimSpace(incident.Summary),
				OccurredAt:    state.EvaluatedAt,
			})
		case risk.IncidentExcessiveDrawdownDetected:
			hasDrawdown = true
			at.observeIncident(incidents.Signal{
				IncidentType:  incidents.TypeDrawdownBreached,
				Severity:      incidents.SeverityWarning,
				Source:        "risk_supervisor",
				Summary:       strings.TrimSpace(incident.Summary),
				CurrentStatus: strings.TrimSpace(incident.Summary),
				OccurredAt:    state.EvaluatedAt,
			})
		case risk.IncidentExcessiveOrderRejects:
			hasOrderRejects = true
			at.observeIncident(incidents.Signal{
				IncidentType:  incidents.TypeExcessiveOrderRejects,
				Severity:      incidents.SeverityWarning,
				Source:        "risk_supervisor",
				Summary:       strings.TrimSpace(incident.Summary),
				CurrentStatus: strings.TrimSpace(incident.Summary),
				OccurredAt:    state.EvaluatedAt,
			})
		case risk.IncidentMaxGrossExposureBreached,
			risk.IncidentMaxNetExposureBreached,
			risk.IncidentMaxConcurrentPositionsBreached,
			risk.IncidentMaxSectorExposureBreached,
			risk.IncidentMaxCorrelatedPositionsBreached,
			risk.IncidentRepeatedBrokerDegradation,
			risk.IncidentRepeatedReconciliationFailure:
			if state.Mode == risk.SupervisorModeHalted {
				hasSupervisorHalt = true
				at.observeIncident(incidents.Signal{
					IncidentType:  incidents.TypeRiskSupervisorHalted,
					Severity:      incidents.SeverityCritical,
					Source:        "risk_supervisor",
					Summary:       strings.TrimSpace(incident.Summary),
					CurrentStatus: fmt.Sprintf("%s (%s)", state.Mode, strings.TrimSpace(incident.Summary)),
					OccurredAt:    state.EvaluatedAt,
				})
			}
		}
	}

	if !hasDailyLoss {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeDailyLossBreached,
			Source:       "risk_supervisor",
			OccurredAt:   state.EvaluatedAt,
		}, "daily loss restriction cleared")
	}
	if !hasDrawdown {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeDrawdownBreached,
			Source:       "risk_supervisor",
			OccurredAt:   state.EvaluatedAt,
		}, "drawdown restriction cleared")
	}
	if !hasOrderRejects {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeExcessiveOrderRejects,
			Source:       "risk_supervisor",
			OccurredAt:   state.EvaluatedAt,
		}, "order reject restriction cleared")
	}
	if !hasSupervisorHalt {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeRiskSupervisorHalted,
			Source:       "risk_supervisor",
			OccurredAt:   state.EvaluatedAt,
		}, "risk supervisor halt cleared")
	}
}

func (at *AutoTrader) syncKillSwitchIncident(summary killSwitchSummary) {
	signal := incidents.Signal{
		IncidentType:  incidents.TypeKillSwitchActivated,
		Severity:      incidents.SeverityCritical,
		Source:        "kill_switch",
		Summary:       strings.TrimSpace(summary.Message),
		CurrentStatus: strings.TrimSpace(summary.Message),
		OccurredAt:    summary.LastCheckedAt,
	}
	if summary.Active {
		at.observeIncident(signal)
		return
	}
	at.resolveIncident(signal, "kill switch cleared")
}

func (at *AutoTrader) observeReconciliationIncident(stage string, err error) {
	if err == nil {
		return
	}
	incidentType := reconciliationIncidentType(stage)
	at.observeIncident(incidents.Signal{
		IncidentType:  incidentType,
		Severity:      incidents.SeverityCritical,
		Source:        "reconciliation",
		Summary:       fmt.Sprintf("%s failed: %v", strings.TrimSpace(stage), err),
		CurrentStatus: err.Error(),
		ExtraKey:      strings.ToLower(strings.TrimSpace(stage)),
		OccurredAt:    time.Now().UTC(),
	})
}

func (at *AutoTrader) resolveReconciliationIncident(incidentType incidents.Type, stage, status string) {
	at.resolveIncident(incidents.Signal{
		IncidentType: incidentType,
		Source:       "reconciliation",
		ExtraKey:     strings.ToLower(strings.TrimSpace(stage)),
		OccurredAt:   time.Now().UTC(),
	}, status)
}

func (at *AutoTrader) syncPositionReconciliationIncident(summary string, err error, issues []positions.Issue) {
	now := time.Now().UTC()
	if err != nil {
		at.observeIncident(incidents.Signal{
			IncidentType:  incidents.TypePositionReconciliationFailed,
			Severity:      incidents.SeverityCritical,
			Source:        "position_reconciliation",
			Summary:       strings.TrimSpace(summary),
			CurrentStatus: err.Error(),
			OccurredAt:    now,
		})
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypePositionMismatchDetected,
			Source:       "position_reconciliation",
			OccurredAt:   now,
		}, "position mismatch cleared")
		return
	}
	if len(issues) > 0 {
		at.observeIncident(incidents.Signal{
			IncidentType:  incidents.TypePositionMismatchDetected,
			Severity:      incidents.SeverityCritical,
			Source:        "position_reconciliation",
			Summary:       strings.TrimSpace(summary),
			CurrentStatus: "position mismatch detected",
			OccurredAt:    now,
		})
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypePositionReconciliationFailed,
			Source:       "position_reconciliation",
			OccurredAt:   now,
		}, "position reconciliation fetch failure cleared")
		return
	}
	at.resolveIncident(incidents.Signal{
		IncidentType: incidents.TypePositionMismatchDetected,
		Source:       "position_reconciliation",
		OccurredAt:   now,
	}, "position reconciliation healthy")
	at.resolveIncident(incidents.Signal{
		IncidentType: incidents.TypePositionReconciliationFailed,
		Source:       "position_reconciliation",
		OccurredAt:   now,
	}, "position reconciliation healthy")
}

func (at *AutoTrader) syncDataQualityIncident(symbol string, blocked bool, summary string, issueTypes []string) {
	signal := incidents.Signal{
		IncidentType:  incidents.TypeSymbolDataQualityBlocked,
		Severity:      incidents.SeverityWarning,
		Source:        "data_quality",
		Symbol:        strings.ToUpper(strings.TrimSpace(symbol)),
		Summary:       strings.TrimSpace(summary),
		CurrentStatus: strings.TrimSpace(summary),
		OccurredAt:    time.Now().UTC(),
	}
	if len(issueTypes) > 0 {
		signal.Details = map[string]string{
			"issue_types": strings.Join(issueTypes, ","),
		}
	}
	if blocked {
		at.observeIncident(signal)
		return
	}
	at.resolveIncident(signal, "data quality restored")
}

func (at *AutoTrader) syncMarketDataAvailabilityIncident(blocked bool, summary string, details map[string]string) {
	signal := incidents.Signal{
		IncidentType:  incidents.TypeMarketDataValidationFailed,
		Severity:      incidents.SeverityCritical,
		Source:        "market_data",
		Summary:       strings.TrimSpace(summary),
		CurrentStatus: strings.TrimSpace(summary),
		ExtraKey:      "runtime_history",
		OccurredAt:    time.Now().UTC(),
	}
	if len(details) > 0 {
		signal.Details = make(map[string]string, len(details))
		for key, value := range details {
			if strings.TrimSpace(key) == "" {
				continue
			}
			signal.Details[key] = strings.TrimSpace(value)
		}
	}
	if blocked {
		at.observeIncident(signal)
		return
	}
	at.resolveIncident(signal, "market data available")
}

func reconciliationIncidentType(stage string) incidents.Type {
	stage = strings.ToLower(strings.TrimSpace(stage))
	switch {
	case strings.Contains(stage, "position_reconciliation"):
		return incidents.TypePositionReconciliationFailed
	case strings.Contains(stage, "order_reconciliation"):
		return incidents.TypeOrderReconciliationFailed
	case strings.Contains(stage, "recovery_loop"):
		return incidents.TypeBrokerRuntimeReconnectFailed
	default:
		return incidents.TypeBrokerRuntimeReconcileFailed
	}
}
