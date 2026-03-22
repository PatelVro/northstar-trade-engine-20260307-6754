package trader

import (
	"fmt"
	"northstar/alerts"
	"northstar/incidents"
	"northstar/orders"
	"strings"
	"time"
)

type orderObserverMux struct {
	observers []orders.Observer
}

func (m orderObserverMux) OnOrderEvent(event orders.Event) {
	for _, observer := range m.observers {
		if observer == nil {
			continue
		}
		observer.OnOrderEvent(event)
	}
}

func (m orderObserverMux) OnReconciliation(result orders.ReconciliationResult) {
	for _, observer := range m.observers {
		if observer == nil {
			continue
		}
		observer.OnReconciliation(result)
	}
}

type orderReconciliationRuntimeObserver struct {
	at *AutoTrader
}

func (o orderReconciliationRuntimeObserver) OnOrderEvent(event orders.Event) {}

func (o orderReconciliationRuntimeObserver) OnReconciliation(result orders.ReconciliationResult) {
	if o.at == nil {
		return
	}
	o.at.observeOrderReconciliationExecutionTruth(result)
}

func (at *AutoTrader) buildOrderObserver() orders.Observer {
	observers := make([]orders.Observer, 0, 3)
	if at.auditRecorder != nil {
		observers = append(observers, at.auditRecorder)
	}
	if at.eventJournal != nil {
		observers = append(observers, at.eventJournal)
	}
	observers = append(observers, orderReconciliationRuntimeObserver{at: at})
	filtered := make([]orders.Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return orderObserverMux{observers: filtered}
	}
}

func (at *AutoTrader) observeOrderReconciliationExecutionTruth(result orders.ReconciliationResult) {
	if at == nil {
		return
	}
	occurredAt := result.RanAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	if result.InferredOutcomes > 0 {
		message := fmt.Sprintf("order reconciliation inferred %d execution outcome(s) from position evidence", result.InferredOutcomes)
		if summary := strings.TrimSpace(result.Summary); summary != "" {
			message = summary
		}
		details := map[string]string{
			"inferred_outcomes":   fmt.Sprintf("%d", result.InferredOutcomes),
			"unresolved_outcomes": fmt.Sprintf("%d", result.UnresolvedOutcomes),
		}
		if issue := firstOrderIssueWithAuthority(result.Issues, orders.TruthAuthorityReconciliationInferred); issue != nil {
			details["local_order_id"] = strings.TrimSpace(issue.LocalID)
			details["confidence"] = string(issue.Confidence)
		}
		at.observeIncident(incidents.Signal{
			IncidentType:  incidents.TypeOrderReconciliationInferredExecution,
			Severity:      incidents.SeverityWarning,
			Source:        "order_reconciliation",
			ExtraKey:      "inferred_execution_truth",
			Summary:       message,
			CurrentStatus: "execution truth inferred from position evidence",
			Details:       details,
			OccurredAt:    occurredAt,
		})
		at.emitAlert(alerts.CategoryWarning, "order_reconciliation_inferred_execution", "order_reconciliation_inferred_execution|"+at.id, message, details)
		at.recordPaperSessionWarning(message)
	} else {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeOrderReconciliationInferredExecution,
			Source:       "order_reconciliation",
			ExtraKey:     "inferred_execution_truth",
			OccurredAt:   occurredAt,
		}, "order reconciliation has no inferred execution outcomes")
	}

	if result.UnresolvedOutcomes > 0 {
		message := fmt.Sprintf("order reconciliation left %d execution outcome(s) unresolved", result.UnresolvedOutcomes)
		if summary := strings.TrimSpace(result.Summary); summary != "" {
			message = summary
		}
		details := map[string]string{
			"inferred_outcomes":   fmt.Sprintf("%d", result.InferredOutcomes),
			"unresolved_outcomes": fmt.Sprintf("%d", result.UnresolvedOutcomes),
		}
		if issue := firstOrderIssueWithAuthority(result.Issues, orders.TruthAuthorityUnresolved); issue != nil {
			details["local_order_id"] = strings.TrimSpace(issue.LocalID)
			details["confidence"] = string(issue.Confidence)
		}
		at.observeIncident(incidents.Signal{
			IncidentType:  incidents.TypeOrderReconciliationUnresolvedExecution,
			Severity:      incidents.SeverityCritical,
			Source:        "order_reconciliation",
			ExtraKey:      "unresolved_execution_truth",
			Summary:       message,
			CurrentStatus: "execution truth unresolved pending broker follow-up",
			Details:       details,
			Escalate:      true,
			OccurredAt:    occurredAt,
		})
		at.emitAlert(alerts.CategoryCritical, "order_reconciliation_unresolved_execution", "order_reconciliation_unresolved_execution|"+at.id, message, details)
		at.recordPaperSessionError(message)
	} else {
		at.resolveIncident(incidents.Signal{
			IncidentType: incidents.TypeOrderReconciliationUnresolvedExecution,
			Source:       "order_reconciliation",
			ExtraKey:     "unresolved_execution_truth",
			OccurredAt:   occurredAt,
		}, "order reconciliation has no unresolved execution outcomes")
	}
}

func firstOrderIssueWithAuthority(issues []orders.Issue, authority orders.TruthAuthority) *orders.Issue {
	for _, issue := range issues {
		if issue.Authority != authority {
			continue
		}
		cloned := issue
		return &cloned
	}
	return nil
}
