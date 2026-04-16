package trader

import (
	"testing"
	"time"
)

func TestOrderDedupWindow_NoDuplicateInitially(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	if d.IsDuplicate("AAPL", "BUY", 10) {
		t.Fatal("expected IsDuplicate=false before any Mark")
	}
}

func TestOrderDedupWindow_DetectsDuplicateAfterMark(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	d.Mark("AAPL", "BUY", 10)
	if !d.IsDuplicate("AAPL", "BUY", 10) {
		t.Fatal("expected IsDuplicate=true immediately after Mark")
	}
}

func TestOrderDedupWindow_DifferentSideIsNotDuplicate(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	d.Mark("AAPL", "BUY", 10)
	if d.IsDuplicate("AAPL", "SELL", 10) {
		t.Fatal("expected IsDuplicate=false for different side")
	}
}

func TestOrderDedupWindow_DifferentSymbolIsNotDuplicate(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	d.Mark("AAPL", "BUY", 10)
	if d.IsDuplicate("TSLA", "BUY", 10) {
		t.Fatal("expected IsDuplicate=false for different symbol")
	}
}

func TestOrderDedupWindow_ExpiresAfterTTL(t *testing.T) {
	ttl := 50 * time.Millisecond
	d := NewOrderDedupWindow(ttl)
	d.Mark("AAPL", "BUY", 5)
	if !d.IsDuplicate("AAPL", "BUY", 5) {
		t.Fatal("expected IsDuplicate=true within TTL")
	}
	time.Sleep(ttl + 20*time.Millisecond)
	if d.IsDuplicate("AAPL", "BUY", 5) {
		t.Fatal("expected IsDuplicate=false after TTL expiry")
	}
}

func TestOrderDedupWindow_QtyRoundingGroupsSameQty(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	// 10.001 and 10.004 should round to 10.00 and be treated as the same key.
	d.Mark("NVDA", "SELL", 10.001)
	if !d.IsDuplicate("NVDA", "SELL", 10.004) {
		t.Fatal("expected IsDuplicate=true for qty that rounds to same 2dp value")
	}
}

func TestOrderDedupWindow_QtyRoundingDistinguishesDifferentQty(t *testing.T) {
	d := NewOrderDedupWindow(60 * time.Second)
	d.Mark("NVDA", "BUY", 10.0)
	if d.IsDuplicate("NVDA", "BUY", 11.0) {
		t.Fatal("expected IsDuplicate=false for different rounded qty")
	}
}
