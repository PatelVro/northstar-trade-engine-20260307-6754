// Package trader - session_guard.go
// sessionGuard prevents order submission outside regular exchange trading hours.
// For IBKR equity traders this means: Mon-Fri 09:30-16:00 America/New_York (regular session).
// Extended hours trading is opt-in via config.
package trader

import (
	"fmt"
	"time"
)

// regularOpen and regularClose define the NYSE/NASDAQ regular session window.
const (
	regularOpenHour   = 9
	regularOpenMinute = 30
	regularCloseHour  = 16
	regularCloseMin   = 0

	extendedOpenHour  = 4
	extendedOpenMin   = 0
	extendedCloseHour = 20
	extendedCloseMin  = 0
)

// sessionGuard prevents order submission outside regular exchange trading hours.
type sessionGuard struct {
	loc                *time.Location
	allowExtendedHours bool
}

// NewSessionGuard constructs a sessionGuard for the given IANA timezone name.
// Pass allowExtendedHours=true to permit pre-market (04:00-09:30) and
// after-hours (16:00-20:00) trading on weekdays.
func NewSessionGuard(timezone string, allowExtendedHours bool) (*sessionGuard, error) {
	if timezone == "" {
		timezone = "America/New_York"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("session_guard: invalid timezone %q: %w", timezone, err)
	}
	return &sessionGuard{
		loc:                loc,
		allowExtendedHours: allowExtendedHours,
	}, nil
}

// IsMarketOpen returns true if t falls within the regular trading session
// (Mon-Fri 09:30:00-15:59:59 in the guard's configured timezone).
func (sg *sessionGuard) IsMarketOpen(t time.Time) bool {
	local := t.In(sg.loc)
	day := local.Weekday()
	if day == time.Saturday || day == time.Sunday {
		return false
	}
	h, m, _ := local.Clock()
	afterOpen := h > regularOpenHour || (h == regularOpenHour && m >= regularOpenMinute)
	beforeClose := h < regularCloseHour
	return afterOpen && beforeClose
}

// IsExtendedHoursOpen returns true if t falls within the extended-hours window
// (Mon-Fri 04:00-09:29 or 16:00-19:59 in the guard's configured timezone).
func (sg *sessionGuard) IsExtendedHoursOpen(t time.Time) bool {
	local := t.In(sg.loc)
	day := local.Weekday()
	if day == time.Saturday || day == time.Sunday {
		return false
	}
	h, m, _ := local.Clock()
	totalMins := h*60 + m

	extOpenMins := extendedOpenHour*60 + extendedOpenMin    // 240
	regOpenMins := regularOpenHour*60 + regularOpenMinute   // 570
	regCloseMins := regularCloseHour*60 + regularCloseMin   // 960
	extCloseMins := extendedCloseHour*60 + extendedCloseMin // 1200

	preMarket := totalMins >= extOpenMins && totalMins < regOpenMins
	afterHours := totalMins >= regCloseMins && totalMins < extCloseMins
	return preMarket || afterHours
}

// AllowsTrading returns true if the given time is within a tradable session
// according to the guard's configuration. Regular session always permits
// trading; extended hours are only permitted when allowExtendedHours is set.
func (sg *sessionGuard) AllowsTrading(t time.Time) bool {
	if sg.IsMarketOpen(t) {
		return true
	}
	if sg.allowExtendedHours && sg.IsExtendedHoursOpen(t) {
		return true
	}
	return false
}
