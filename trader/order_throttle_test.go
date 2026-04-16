package trader

import (
	"context"
	"testing"
	"time"
)

func TestOrderThrottle_Burst(t *testing.T) {
	// maxBurst=10, no refill — 10 allows must succeed; 11th must fail.
	ot := NewOrderThrottle(10, 0)

	for i := 0; i < 10; i++ {
		if !ot.Allow() {
			t.Fatalf("Allow() returned false on attempt %d (expected true)", i+1)
		}
	}
	if ot.Allow() {
		t.Error("11th Allow() returned true; expected false after burst exhausted")
	}
}

func TestOrderThrottle_Refill(t *testing.T) {
	// rate=10/min → ~1 token per 6 s.
	// Start with 0 tokens (exhaust burst=1 immediately), then wait 6+ seconds.
	ot := NewOrderThrottle(1, 10)
	if !ot.Allow() {
		t.Fatal("first Allow() should succeed with burst=1")
	}
	// Bucket is now empty.
	if ot.Allow() {
		t.Fatal("second immediate Allow() should fail; bucket empty")
	}

	// Manually wind the clock: pretend lastRefill was 6 seconds ago.
	ot.mu.Lock()
	ot.lastRefill = time.Now().Add(-6 * time.Second)
	ot.mu.Unlock()

	if !ot.Allow() {
		t.Error("Allow() should return true after 6 s elapsed at 10/min rate")
	}
}

func TestOrderThrottle_ZeroRate(t *testing.T) {
	// Confirm zero refill rate doesn't panic.
	ot := NewOrderThrottle(5, 0)
	for i := 0; i < 5; i++ {
		if !ot.Allow() {
			t.Fatalf("Allow() returned false on burst attempt %d", i+1)
		}
	}
	// Bucket empty; refill rate is zero so nothing accrues.
	if ot.Allow() {
		t.Error("Allow() should return false after burst with zero refill rate")
	}
}

func TestOrderThrottle_ZeroBurst(t *testing.T) {
	// maxBurst=0 means the bucket starts empty and stays empty (no refill).
	ot := NewOrderThrottle(0, 0)
	if ot.Allow() {
		t.Error("Allow() should return false for zero-burst throttle")
	}
}

func TestOrderThrottle_WaitSucceeds(t *testing.T) {
	// burst=0 with a fast refill rate: Wait should succeed quickly.
	// rate=600/min → 10 tokens/s → token available within ~100 ms.
	ot := NewOrderThrottle(0, 600)
	// Manually give the bucket max tokens for instant availability.
	ot.mu.Lock()
	ot.tokens = 1
	ot.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ot.Wait(ctx); err != nil {
		t.Errorf("Wait returned unexpected error: %v", err)
	}
}

func TestOrderThrottle_WaitCancelledContext(t *testing.T) {
	// burst=0, no refill, cancelled context → Wait must return an error.
	ot := NewOrderThrottle(0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if err := ot.Wait(ctx); err == nil {
		t.Error("Wait should return error for cancelled context")
	}
}

func TestOrderThrottle_NegativeBurstClamped(t *testing.T) {
	// Negative burst should be clamped to 0 without panic.
	ot := NewOrderThrottle(-5, 0)
	if ot.Allow() {
		t.Error("Allow() should return false for negative-burst (clamped to 0) throttle")
	}
}
