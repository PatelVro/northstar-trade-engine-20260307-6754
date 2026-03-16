package trader

import "testing"

func TestNormalizeBrokerAccountPrefersExplicitEquity(t *testing.T) {
	balance := map[string]interface{}{
		"accountCash":      100000.0,
		"accountEquity":    105000.0,
		"availableBalance": 98000.0,
		"unrealizedPnL":    5000.0,
	}

	normalized := normalizeBrokerAccount(balance, nil)

	if normalized.AccountEquity != 105000.0 {
		t.Fatalf("expected explicit account equity 105000, got %.2f", normalized.AccountEquity)
	}
	if normalized.AccountCash != 100000.0 {
		t.Fatalf("expected explicit account cash 100000, got %.2f", normalized.AccountCash)
	}
}

func TestNormalizeBrokerAccountSupportsLegacyWalletSemantics(t *testing.T) {
	balance := map[string]interface{}{
		"totalWalletBalance":    100000.0,
		"availableBalance":      97000.0,
		"totalUnrealizedProfit": 5000.0,
	}

	normalized := normalizeBrokerAccount(balance, nil)

	if normalized.AccountEquity != 105000.0 {
		t.Fatalf("expected legacy wallet balance + unrealized to equal 105000, got %.2f", normalized.AccountEquity)
	}
	if normalized.AccountCash != 100000.0 {
		t.Fatalf("expected legacy wallet balance to map to account cash 100000, got %.2f", normalized.AccountCash)
	}
}

func TestBuildAccountSummaryKeepsStrategyReturnIndependentFromBrokerEquity(t *testing.T) {
	summary := buildAccountSummary(normalizedBrokerAccount{
		AccountCash:      900000.0,
		AvailableBalance: 850000.0,
		AccountEquity:    977079.6875,
		GrossMarketValue: 76079.6875,
		UnrealizedPnL:    750.0,
	}, 100000.0, 250.0, 0.0)

	if summary.TotalPnL != 1000.0 {
		t.Fatalf("expected total pnl 1000, got %.2f", summary.TotalPnL)
	}
	if summary.StrategyEquity != 101000.0 {
		t.Fatalf("expected strategy equity 101000, got %.2f", summary.StrategyEquity)
	}
	if summary.StrategyReturnPct != 1.0 {
		t.Fatalf("expected strategy return 1.0%%, got %.4f%%", summary.StrategyReturnPct)
	}
	if summary.AccountEquity != 977079.6875 {
		t.Fatalf("expected broker account equity to remain 977079.6875, got %.4f", summary.AccountEquity)
	}
}
