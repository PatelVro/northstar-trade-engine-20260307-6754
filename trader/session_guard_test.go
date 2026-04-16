package trader

import (
	"testing"
	"time"
)

func mustSessionGuard(t *testing.T, tz string, extended bool) *sessionGuard {
	t.Helper()
	sg, err := NewSessionGuard(tz, extended)
	if err != nil {
		t.Fatalf("NewSessionGuard(%q, %v): %v", tz, extended, err)
	}
	return sg
}

// etTime builds a time.Time in America/New_York for the given date/hour/minute.
func etTime(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func TestSessionGuard_MarketOpen(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", false)

	// Wednesday 2026-04-15 10:00 ET — regular session
	ts := etTime(t, 2026, time.April, 15, 10, 0)
	if !sg.IsMarketOpen(ts) {
		t.Errorf("expected IsMarketOpen=true for Wednesday 10:00 ET, got false")
	}
	if !sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=true for Wednesday 10:00 ET (no extended), got false")
	}
}

func TestSessionGuard_MarketClosedWeekend(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", false)

	// Saturday 2026-04-18 10:00 ET
	ts := etTime(t, 2026, time.April, 18, 10, 0)
	if sg.IsMarketOpen(ts) {
		t.Errorf("expected IsMarketOpen=false for Saturday 10:00 ET, got true")
	}
	if sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=false for Saturday 10:00 ET, got true")
	}
}

func TestSessionGuard_AfterHoursBlockedByDefault(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", false)

	// Wednesday 2026-04-15 17:00 ET — after regular close
	ts := etTime(t, 2026, time.April, 15, 17, 0)
	if sg.IsMarketOpen(ts) {
		t.Errorf("expected IsMarketOpen=false for Wednesday 17:00 ET, got true")
	}
	if sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=false for Wednesday 17:00 ET with extended=false, got true")
	}
}

func TestSessionGuard_AfterHoursAllowedWhenExtended(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", true)

	// Wednesday 2026-04-15 17:00 ET — inside extended hours window
	ts := etTime(t, 2026, time.April, 15, 17, 0)
	if !sg.IsExtendedHoursOpen(ts) {
		t.Errorf("expected IsExtendedHoursOpen=true for Wednesday 17:00 ET, got false")
	}
	if !sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=true for Wednesday 17:00 ET with extended=true, got false")
	}
}

func TestSessionGuard_BeforeOpen(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", false)

	// Wednesday 2026-04-15 08:00 ET — before regular open, extended hours not enabled
	ts := etTime(t, 2026, time.April, 15, 8, 0)
	if sg.IsMarketOpen(ts) {
		t.Errorf("expected IsMarketOpen=false for Wednesday 08:00 ET, got true")
	}
	if sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=false for Wednesday 08:00 ET with extended=false, got true")
	}
}

func TestSessionGuard_InvalidTimezone(t *testing.T) {
	_, err := NewSessionGuard("Not/AReal_Zone", false)
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}
}

func TestSessionGuard_DefaultTimezone(t *testing.T) {
	// Empty string should default to America/New_York without error.
	sg, err := NewSessionGuard("", false)
	if err != nil {
		t.Fatalf("NewSessionGuard with empty tz: %v", err)
	}
	// Wednesday 10:00 ET should be open.
	ts := etTime(t, 2026, time.April, 15, 10, 0)
	if !sg.AllowsTrading(ts) {
		t.Errorf("expected AllowsTrading=true for Wednesday 10:00 ET with default timezone")
	}
}

func TestSessionGuard_ExactBoundaries(t *testing.T) {
	sg := mustSessionGuard(t, "America/New_York", true)

	cases := []struct {
		name         string
		h, m         int
		wantRegular  bool
		wantExtended bool
	}{
		{"at 09:30", 9, 30, true, false},
		{"at 09:29", 9, 29, false, true},  // inside pre-market
		{"at 16:00", 16, 0, false, true},  // inside after-hours
		{"at 15:59", 15, 59, true, false}, // last minute of regular
		{"at 04:00", 4, 0, false, true},
		{"at 03:59", 3, 59, false, false},
		{"at 20:00", 20, 0, false, false}, // extended closes at 20:00
	}

	for _, tc := range cases {
		ts := etTime(t, 2026, time.April, 15, tc.h, tc.m) // Wednesday
		if got := sg.IsMarketOpen(ts); got != tc.wantRegular {
			t.Errorf("%s: IsMarketOpen=%v, want %v", tc.name, got, tc.wantRegular)
		}
		if got := sg.IsExtendedHoursOpen(ts); got != tc.wantExtended {
			t.Errorf("%s: IsExtendedHoursOpen=%v, want %v", tc.name, got, tc.wantExtended)
		}
	}
}
