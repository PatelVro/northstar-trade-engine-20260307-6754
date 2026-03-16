package alerts

import "testing"

type captureProvider struct {
	alerts []Alert
}

func (p *captureProvider) Name() string { return "capture" }

func (p *captureProvider) Send(alert Alert) error {
	p.alerts = append(p.alerts, alert)
	return nil
}

func TestManagerStoresRecentAlertsAndCounts(t *testing.T) {
	provider := &captureProvider{}
	manager := NewManager(provider)
	manager.Emit(Alert{
		Category: CategoryCritical,
		Event:    "broker_disconnect",
		TraderID: "paper_1",
		Message:  "broker disconnect detected",
	})

	summary := manager.Summary()
	if summary.TotalCount != 1 {
		t.Fatalf("expected total count 1, got %d", summary.TotalCount)
	}
	if summary.CriticalCount != 1 {
		t.Fatalf("expected critical count 1, got %d", summary.CriticalCount)
	}
	if len(summary.Recent) != 1 {
		t.Fatalf("expected one recent alert, got %d", len(summary.Recent))
	}
	if summary.Recent[0].Event != "broker_disconnect" {
		t.Fatalf("unexpected recent alert event %q", summary.Recent[0].Event)
	}
}

func TestManagerDedupesSameAlertKeyWithinWindow(t *testing.T) {
	provider := &captureProvider{}
	manager := NewManager(provider)
	first := manager.Emit(Alert{
		Key:      "same",
		Category: CategoryWarning,
		Event:    "trading_blocked",
		TraderID: "paper_1",
		Message:  "trading blocked",
	})
	second := manager.Emit(Alert{
		Key:      "same",
		Category: CategoryWarning,
		Event:    "trading_blocked",
		TraderID: "paper_1",
		Message:  "trading blocked",
	})

	if !first {
		t.Fatalf("expected first alert to emit")
	}
	if second {
		t.Fatalf("expected duplicate alert to be suppressed")
	}
	summary := manager.Summary()
	if summary.TotalCount != 1 {
		t.Fatalf("expected total count 1 after dedupe, got %d", summary.TotalCount)
	}
}
