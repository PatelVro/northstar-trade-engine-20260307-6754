package dataquality

import (
	"testing"
	"time"
)

func TestValidateBarsFlagsStaleData(t *testing.T) {
	now := time.Date(2026, 3, 15, 15, 30, 0, 0, time.UTC)
	bars := buildBars(now.Add(-80*time.Minute), time.Minute, 40, 100, 1000)
	result := ValidateBars("AAPL", "1m", bars, Options{
		Now:            now,
		CheckStaleness: true,
		ExpectedBars:   40,
	})
	if !result.Failed() {
		t.Fatalf("expected stale data validation failure")
	}
	if result.Issues[0].Type != IssueStaleData {
		t.Fatalf("expected stale data issue, got %s", result.Issues[0].Type)
	}
}

func TestValidateBarsFlagsZeroVolume(t *testing.T) {
	now := time.Now().UTC()
	bars := buildBars(now.Add(-39*time.Minute), time.Minute, 40, 100, 1000)
	bars[len(bars)-1].Volume = 0
	result := ValidateBars("AAPL", "1m", bars, Options{
		Now:            now,
		CheckStaleness: true,
		ExpectedBars:   40,
	})
	if !result.Failed() {
		t.Fatalf("expected zero volume validation failure")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Type == IssueZeroVolume {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected zero volume issue")
	}
}

func TestValidateBarsFlagsExtremeJump(t *testing.T) {
	now := time.Now().UTC()
	bars := buildBars(now.Add(-39*time.Minute), time.Minute, 40, 100, 1000)
	bars[20].Close = bars[19].Close * 1.5
	result := ValidateBars("AAPL", "1m", bars, Options{
		Now:            now,
		CheckStaleness: true,
		ExpectedBars:   40,
	})
	if !result.Failed() {
		t.Fatalf("expected extreme jump validation failure")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Type == IssueExtremePriceJump {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected extreme price jump issue")
	}
}

func TestValidateBarsFlagsMissingBars(t *testing.T) {
	now := time.Now().UTC()
	bars := buildBars(now.Add(-29*time.Minute), time.Minute, 30, 100, 1000)
	result := ValidateBars("AAPL", "1m", bars, Options{
		Now:            now,
		CheckStaleness: true,
		ExpectedBars:   40,
	})
	if !result.Failed() {
		t.Fatalf("expected missing bars validation failure")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Type == IssueMissingBars {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected missing bars issue")
	}
}

func buildBars(start time.Time, step time.Duration, count int, startPrice float64, volume float64) []Bar {
	out := make([]Bar, 0, count)
	price := startPrice
	for i := 0; i < count; i++ {
		openTime := start.Add(time.Duration(i) * step)
		closeTime := openTime.Add(step)
		out = append(out, Bar{
			OpenTime:  openTime.UnixMilli(),
			Open:      price,
			High:      price * 1.01,
			Low:       price * 0.99,
			Close:     price * 1.001,
			Volume:    volume,
			CloseTime: closeTime.UnixMilli(),
		})
		price = price * 1.001
	}
	return out
}
