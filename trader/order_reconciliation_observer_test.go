package trader

import (
	"northstar/alerts"
	"northstar/incidents"
	"northstar/orders"
	"testing"
	"time"
)

func TestObserveOrderReconciliationExecutionTruthEscalatesInferredAndUnresolved(t *testing.T) {
	at := &AutoTrader{
		id:              "paper_trader",
		name:            "Paper Trader",
		alertManager:    alerts.NewManager(),
		incidentManager: incidents.NewManager("paper_trader"),
	}

	now := time.Now().UTC()
	at.observeOrderReconciliationExecutionTruth(orders.ReconciliationResult{
		RanAt:            now,
		Mismatches:       1,
		InferredOutcomes: 1,
		Summary:          "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=1 unresolved=0",
		Issues: []orders.Issue{{
			Type:        orders.IssueLocalMissingAtBroker,
			LocalID:     "local-1",
			Message:     "position evidence indicates fill",
			Authority:   orders.TruthAuthorityReconciliationInferred,
			Confidence:  orders.TruthConfidenceHigh,
			NeedsReview: true,
		}},
	})

	incidentSummary := at.currentIncidentSummary()
	if incidentSummary.OpenCount != 1 || incidentSummary.OpenIncidents[0].IncidentType != incidents.TypeOrderReconciliationInferredExecution {
		t.Fatalf("expected inferred-execution incident, got %+v", incidentSummary)
	}
	alertSummary := at.currentAlertsSummary()
	if alertSummary.WarningCount != 1 {
		t.Fatalf("expected warning alert for inferred execution truth, got %+v", alertSummary)
	}

	at.observeOrderReconciliationExecutionTruth(orders.ReconciliationResult{
		RanAt:              now.Add(time.Second),
		Mismatches:         1,
		UnresolvedOutcomes: 1,
		TradingBlocked:     true,
		Summary:            "order reconciliation handled 1 mismatch(es): local_missing=1 unknown_broker=0 fill_mismatches=0 inferred=0 unresolved=1",
		Issues: []orders.Issue{{
			Type:        orders.IssueLocalMissingAtBroker,
			LocalID:     "local-2",
			Message:     "execution truth remains unresolved",
			Authority:   orders.TruthAuthorityUnresolved,
			Confidence:  orders.TruthConfidenceUnresolved,
			NeedsReview: true,
		}},
	})

	incidentSummary = at.currentIncidentSummary()
	if incidentSummary.CriticalOpenCount != 1 {
		t.Fatalf("expected unresolved execution truth to escalate critically, got %+v", incidentSummary)
	}
	if incidentSummary.OpenCount != 1 || incidentSummary.OpenIncidents[0].IncidentType != incidents.TypeOrderReconciliationUnresolvedExecution {
		t.Fatalf("expected unresolved-execution incident to remain open, got %+v", incidentSummary)
	}
	alertSummary = at.currentAlertsSummary()
	if alertSummary.CriticalCount != 1 {
		t.Fatalf("expected critical alert for unresolved execution truth, got %+v", alertSummary)
	}
}
