package pool

import "testing"

func TestGetMergedCoinPoolPreservesSourceOrder(t *testing.T) {
	prevDefaultEquity := append([]string(nil), defaultEquityCoins...)
	prevUseDefault := coinPoolConfig.UseDefaultCoins
	prevUseEquity := coinPoolConfig.UseEquityPool
	prevCoinPoolAPI := coinPoolConfig.APIURL
	prevOITopAPI := oiTopConfig.APIURL
	defer func() {
		defaultEquityCoins = prevDefaultEquity
		coinPoolConfig.UseDefaultCoins = prevUseDefault
		coinPoolConfig.UseEquityPool = prevUseEquity
		coinPoolConfig.APIURL = prevCoinPoolAPI
		oiTopConfig.APIURL = prevOITopAPI
	}()

	SetDefaultCoins([]string{"AAPL", "MSFT", "NVDA", "ABBV"})
	SetUseDefaultCoins(true, true)
	SetCoinPoolAPI("")
	SetOITopAPI("")

	merged, err := GetMergedCoinPool(10)
	if err != nil {
		t.Fatalf("GetMergedCoinPool failed: %v", err)
	}

	want := []string{"AAPL", "MSFT", "NVDA", "ABBV"}
	if len(merged.AllSymbols) < len(want) {
		t.Fatalf("expected at least %d merged symbols, got %d", len(want), len(merged.AllSymbols))
	}
	for i, symbol := range want {
		if merged.AllSymbols[i] != symbol {
			t.Fatalf("expected merged symbol %d to be %s, got %s", i, symbol, merged.AllSymbols[i])
		}
	}
}
