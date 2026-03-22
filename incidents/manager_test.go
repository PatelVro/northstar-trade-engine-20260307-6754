package incidents

import (
	"testing"
	"time"
)

func TestManagerObserveCorrelatesRepeatedIncident(t *testing.T) {
	manager := NewManager("paper_trader")
	now := time.Now()

	first, opened := manager.Observe(Signal{
		IncidentType: TypeBrokerRuntimeDegraded,
		Severity:     SeverityWarning,
		Source:       "broker_runtime",
		Summary:      "gateway refused connection",
		OccurredAt:   now,
	})
	if !opened {
		t.Fatalf("expected first observation to open incident")
	}
	second, opened := manager.Observe(Signal{
		IncidentType: TypeBrokerRuntimeDegraded,
		Severity:     SeverityWarning,
		Source:       "broker_runtime",
		Summary:      "gateway refused connection",
		OccurredAt:   now.Add(time.Minute),
	})
	if opened {
		t.Fatalf("expected repeated observation to update existing incident")
	}
	if first.IncidentID != second.IncidentID {
		t.Fatalf("expected repeated observation to reuse incident id")
	}
	if second.OccurrenceCount != 2 {
		t.Fatalf("expected occurrence count 2, got %d", second.OccurrenceCount)
	}
	if manager.Summary().OpenCount != 1 {
		t.Fatalf("expected one open incident, got %d", manager.Summary().OpenCount)
	}
}

func TestManagerResolveClearsActiveIncident(t *testing.T) {
	manager := NewManager("paper_trader")
	manager.Observe(Signal{
		IncidentType: TypeKillSwitchActivated,
		Severity:     SeverityCritical,
		Source:       "kill_switch",
		Summary:      "kill switch active",
	})

	resolved, ok := manager.Resolve(Signal{
		IncidentType: TypeKillSwitchActivated,
		Source:       "kill_switch",
	}, "kill switch cleared")
	if !ok {
		t.Fatalf("expected resolve to succeed")
	}
	if resolved.State != StateResolved {
		t.Fatalf("expected resolved state, got %s", resolved.State)
	}
	summary := manager.Summary()
	if summary.OpenCount != 0 {
		t.Fatalf("expected no open incidents, got %d", summary.OpenCount)
	}
	if len(summary.RecentResolvedIncidents) != 1 {
		t.Fatalf("expected one recent resolved incident, got %d", len(summary.RecentResolvedIncidents))
	}
}

func TestManagerAttachesRunbookActions(t *testing.T) {
	manager := NewManager("paper_trader")
	incident, _ := manager.Observe(Signal{
		IncidentType: TypeStartupReadinessFailed,
		Severity:     SeverityCritical,
		Source:       "readiness",
		Summary:      "broker bootstrap failed",
	})

	if len(incident.RecommendedActions) == 0 {
		t.Fatalf("expected runbook actions for startup readiness incident")
	}
	if RunbookHint(TypeStartupReadinessFailed) == "" {
		t.Fatalf("expected non-empty runbook hint")
	}
}

func TestManagerSummaryCountsInfoWarningAndCriticalOpenIncidents(t *testing.T) {
	manager := NewManager("paper_trader")
	now := time.Now().UTC()

	manager.Observe(Signal{
		IncidentType: TypeMarketDataValidationFailed,
		Severity:     SeverityInfo,
		Source:       "market_data",
		Summary:      "market is closed for equity session",
		OccurredAt:   now,
	})
	manager.Observe(Signal{
		IncidentType: TypeBrokerRuntimeDegraded,
		Severity:     SeverityWarning,
		Source:       "broker_runtime",
		Summary:      "gateway reconnecting",
		OccurredAt:   now.Add(time.Minute),
	})
	manager.Observe(Signal{
		IncidentType: TypeKillSwitchActivated,
		Severity:     SeverityCritical,
		Source:       "kill_switch",
		Summary:      "kill switch active",
		OccurredAt:   now.Add(2 * time.Minute),
	})

	summary := manager.Summary()
	if summary.InfoOpenCount != 1 || summary.WarningOpenCount != 1 || summary.CriticalOpenCount != 1 {
		t.Fatalf("unexpected severity counts: %+v", summary)
	}
}
