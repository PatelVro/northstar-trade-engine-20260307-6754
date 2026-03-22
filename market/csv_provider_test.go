package market

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCSVProviderReplayNoLookahead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AAPL.csv")
	data := "timestamp,open,high,low,close,volume\n" +
		"1700000000,100,101,99,100,1000\n" +
		"1700000060,101,102,100,101,1000\n" +
		"1700000120,102,103,101,102,1000\n" +
		"1700000180,103,104,102,103,1000\n" +
		"1700000240,104,105,103,104,1000\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	provider := NewCSVProvider(dir)
	provider.EnableReplay(3)

	bars, err := provider.GetBars([]string{"AAPL"}, "1m", 10)
	if err != nil {
		t.Fatalf("GetBars: %v", err)
	}
	if got := len(bars["AAPL"]); got != 3 {
		t.Fatalf("expected 3 replay bars before advance, got %d", got)
	}
	if bars["AAPL"][2].Close != 102 {
		t.Fatalf("expected replay cursor to stop at close 102, got %.2f", bars["AAPL"][2].Close)
	}

	if ok := provider.AdvanceReplay(1); !ok {
		t.Fatalf("expected replay advance to succeed")
	}
	bars, err = provider.GetBars([]string{"AAPL"}, "1m", 10)
	if err != nil {
		t.Fatalf("GetBars after advance: %v", err)
	}
	if got := len(bars["AAPL"]); got != 4 {
		t.Fatalf("expected 4 replay bars after advance, got %d", got)
	}
	if bars["AAPL"][3].Close != 103 {
		t.Fatalf("expected next visible close to be 103, got %.2f", bars["AAPL"][3].Close)
	}
}
