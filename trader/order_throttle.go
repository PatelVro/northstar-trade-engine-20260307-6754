package trader

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// orderThrottle is a token-bucket rate limiter for broker order submissions.
// It prevents rapid-fire order storms that cause broker pacing rejects (HTTP 429).
type orderThrottle struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NewOrderThrottle creates a token-bucket throttle.
//
//   - maxBurst: the maximum number of tokens in the bucket (burst capacity).
//   - ratePerMinute: steady-state token refill rate in tokens per minute.
//     Pass 0 to disable refill (bucket drains to zero and never refills).
func NewOrderThrottle(maxBurst int, ratePerMinute int) *orderThrottle {
	if maxBurst < 0 {
		maxBurst = 0
	}
	burst := float64(maxBurst)
	rate := 0.0
	if ratePerMinute > 0 {
		rate = float64(ratePerMinute) / 60.0
	}
	return &orderThrottle{
		tokens:     burst,
		maxTokens:  burst,
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// refill adds tokens that have accrued since lastRefill. Must be called with mu held.
func (ot *orderThrottle) refill(now time.Time) {
	if ot.refillRate <= 0 {
		return
	}
	elapsed := now.Sub(ot.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	ot.tokens += elapsed * ot.refillRate
	if ot.tokens > ot.maxTokens {
		ot.tokens = ot.maxTokens
	}
	ot.lastRefill = now
}

// Allow attempts to consume one token. Returns true if a token was available
// and the caller may proceed, false if the bucket is empty.
func (ot *orderThrottle) Allow() bool {
	ot.mu.Lock()
	defer ot.mu.Unlock()
	ot.refill(time.Now())
	if ot.tokens < 1.0 {
		return false
	}
	ot.tokens -= 1.0
	return true
}

// Wait blocks until a token becomes available or ctx is cancelled.
// Returns nil when a token is successfully consumed, or ctx.Err() if the
// context is cancelled before a token could be obtained.
func (ot *orderThrottle) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("order throttle wait cancelled: %w", err)
		}
		if ot.Allow() {
			return nil
		}
		// Sleep a short interval before re-checking.
		select {
		case <-ctx.Done():
			return fmt.Errorf("order throttle wait cancelled: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
