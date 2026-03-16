package market

import "testing"

func TestResolveIBKRInterval(t *testing.T) {
	tests := []struct {
		interval      string
		limit         int
		wantBar       string
		wantPeriod    string
		wantBucket    int
		wantFetchMult int
	}{
		{interval: "1m", limit: 40, wantBar: "1 min", wantPeriod: "1d", wantBucket: 1, wantFetchMult: 1},
		{interval: "3m", limit: 40, wantBar: "1 min", wantPeriod: "1d", wantBucket: 3, wantFetchMult: 3},
		{interval: "4h", limit: 60, wantBar: "1 hour", wantPeriod: "1m", wantBucket: 4, wantFetchMult: 4},
		{interval: "1d", limit: 20, wantBar: "1 day", wantPeriod: "1y", wantBucket: 1, wantFetchMult: 1},
	}

	for _, tc := range tests {
		bar, period, bucket, fetch := resolveIBKRInterval(tc.interval, tc.limit)
		if bar != tc.wantBar || period != tc.wantPeriod || bucket != tc.wantBucket {
			t.Fatalf("interval %s => got bar=%s period=%s bucket=%d", tc.interval, bar, period, bucket)
		}
		if fetch < tc.limit*tc.wantFetchMult {
			t.Fatalf("interval %s => fetch limit too small: got=%d need>=%d", tc.interval, fetch, tc.limit*tc.wantFetchMult)
		}
	}
}

func TestAggregateKlines(t *testing.T) {
	in := []Kline{
		{OpenTime: 1, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100, CloseTime: 2},
		{OpenTime: 3, Open: 10.5, High: 12, Low: 10, Close: 11.5, Volume: 150, CloseTime: 4},
		{OpenTime: 5, Open: 11.5, High: 13, Low: 11, Close: 12.5, Volume: 130, CloseTime: 6},
	}
	out := aggregateKlines(in, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2 aggregated bars, got %d", len(out))
	}
	if out[0].Open != 10 || out[0].Close != 11.5 || out[0].High != 12 || out[0].Low != 9 {
		t.Fatalf("unexpected first aggregate %+v", out[0])
	}
	if out[0].Volume != 250 {
		t.Fatalf("unexpected first aggregate volume %f", out[0].Volume)
	}
	if out[1].Open != 11.5 || out[1].Close != 12.5 {
		t.Fatalf("unexpected second aggregate %+v", out[1])
	}
}
