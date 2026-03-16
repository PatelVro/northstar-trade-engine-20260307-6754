package risk

import (
	"testing"
	"time"
)

func TestSupervisorEvaluate_CriticalIncidentHaltsTrading(t *testing.T) {
	supervisor := NewSupervisor(SupervisorConfig{})
	state := supervisor.Evaluate(SupervisorSnapshot{
		Now:               time.Now(),
		ReadinessAllowed:  false,
		ReadinessMessage:  "ibkr bootstrap failed",
		PromotionAllowed:  true,
		PromotionRequired: false,
	})

	if state.Mode != SupervisorModeHalted {
		t.Fatalf("expected halted mode, got %s", state.Mode)
	}
	if state.TradingAllowed {
		t.Fatalf("expected trading to be blocked")
	}
	if state.EntriesAllowed || state.ExitsAllowed {
		t.Fatalf("expected both entries and exits to be blocked")
	}
	if state.CriticalIncidentCount != 1 {
		t.Fatalf("expected one critical incident, got %d", state.CriticalIncidentCount)
	}
}

func TestSupervisorEvaluate_DrawdownEnablesReduceOnly(t *testing.T) {
	supervisor := NewSupervisor(SupervisorConfig{EnableReduceOnlyOnDrawdown: true})
	state := supervisor.Evaluate(SupervisorSnapshot{
		Now:                time.Now(),
		ReadinessAllowed:   true,
		PromotionAllowed:   true,
		CurrentDrawdownPct: 0.11,
		MaxDrawdownPct:     0.10,
	})

	if state.Mode != SupervisorModeReduceOnly {
		t.Fatalf("expected reduce_only mode, got %s", state.Mode)
	}
	if !state.TradingAllowed {
		t.Fatalf("expected exits to remain allowed in reduce_only")
	}
	if state.EntriesAllowed {
		t.Fatalf("expected new entries to be blocked in reduce_only")
	}
	if !state.ExitsAllowed {
		t.Fatalf("expected exits to remain allowed")
	}
}

func TestSupervisorEvaluate_RuntimeDegradationEscalatesToBlockNewEntries(t *testing.T) {
	supervisor := NewSupervisor(SupervisorConfig{MaxRuntimeDegradationsPerSession: 2})
	state := supervisor.Evaluate(SupervisorSnapshot{
		Now:                     time.Now(),
		ReadinessAllowed:        true,
		PromotionAllowed:        true,
		BrokerDegradationEvents: 2,
	})

	if state.Mode != SupervisorModeBlockNewEntries {
		t.Fatalf("expected block_new_entries mode, got %s", state.Mode)
	}
	if !state.TradingAllowed || state.EntriesAllowed || !state.ExitsAllowed {
		t.Fatalf("unexpected trade permissions: %+v", state)
	}
}

func TestSupervisorEvaluate_ClearsResolvedIncident(t *testing.T) {
	supervisor := NewSupervisor(SupervisorConfig{})
	halted := supervisor.Evaluate(SupervisorSnapshot{
		Now:              time.Now(),
		KillSwitchActive: true,
		PromotionAllowed: true,
	})
	if halted.Mode != SupervisorModeHalted {
		t.Fatalf("expected halted mode, got %s", halted.Mode)
	}

	healthy := supervisor.Evaluate(SupervisorSnapshot{
		Now:               time.Now().Add(time.Minute),
		ReadinessAllowed:  true,
		PromotionAllowed:  true,
		PromotionRequired: false,
	})
	if healthy.Mode != SupervisorModeAllow {
		t.Fatalf("expected allow mode after incident resolved, got %s", healthy.Mode)
	}
	if healthy.ActiveIncidentCount != 0 {
		t.Fatalf("expected no active incidents, got %d", healthy.ActiveIncidentCount)
	}
}
