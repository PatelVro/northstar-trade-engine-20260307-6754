package trader

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// orderDedupWindow prevents duplicate orders for the same (symbol, side, qty) within a time bucket.
// It is NOT the same as the execution manager's 15s intent window — this is a
// broker-level safety net that survives intent dedup failures.
type orderDedupWindow struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

// NewOrderDedupWindow creates a dedup guard with the given TTL.
func NewOrderDedupWindow(ttl time.Duration) *orderDedupWindow {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &orderDedupWindow{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

// SetTTL updates the dedup TTL. Safe to call concurrently.
func (d *orderDedupWindow) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	d.mu.Lock()
	d.ttl = ttl
	d.mu.Unlock()
}

// dedupKey returns a canonical key for (symbol, side, qty rounded to 2dp).
func dedupKey(symbol, side string, qty float64) string {
	rounded := math.Round(qty*100) / 100
	return fmt.Sprintf("%s_%s_%.2f", symbol, side, rounded)
}

// IsDuplicate returns true if the same (symbol, side, qty) was submitted within the TTL window.
func (d *orderDedupWindow) IsDuplicate(symbol, side string, qty float64) bool {
	key := dedupKey(symbol, side, qty)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	d.sweepLocked(now)

	ts, ok := d.seen[key]
	if !ok {
		return false
	}
	return now.Before(ts.Add(d.ttl))
}

// Mark records a successful submission for (symbol, side, qty).
func (d *orderDedupWindow) Mark(symbol, side string, qty float64) {
	key := dedupKey(symbol, side, qty)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	d.sweepLocked(now)
	d.seen[key] = now
}

// sweepLocked removes entries that have expired. Must be called with d.mu held.
func (d *orderDedupWindow) sweepLocked(now time.Time) {
	for k, ts := range d.seen {
		if now.After(ts.Add(d.ttl)) {
			delete(d.seen, k)
		}
	}
}
