package broker

import (
	"errors"
	"testing"
)

func TestScoreSecdefEquityCandidate_PrefersStockListings(t *testing.T) {
	futuresScore := scoreSecdefEquityCandidate(
		"BA",
		"BA",
		"ASX",
		"BARLEY FUTURES - ASX",
		"",
		[]ibkrSecdefSection{
			{SecType: "FUT", Exchange: "SNFE"},
		},
	)

	stockScore := scoreSecdefEquityCandidate(
		"BA",
		"BA",
		"NYSE",
		"BOEING CO/THE - NYSE",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "SMART;NYSE"},
		},
	)

	if stockScore <= futuresScore {
		t.Fatalf("expected stock score > futures score (stock=%d futures=%d)", stockScore, futuresScore)
	}
}

func TestScoreSecdefEquityCandidate_BonusesSmartRouting(t *testing.T) {
	plainStock := scoreSecdefEquityCandidate(
		"AAPL",
		"AAPL",
		"NASDAQ",
		"APPLE INC - NASDAQ",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "NASDAQ"},
		},
	)

	smartStock := scoreSecdefEquityCandidate(
		"AAPL",
		"AAPL",
		"NASDAQ",
		"APPLE INC - NASDAQ",
		"",
		[]ibkrSecdefSection{
			{SecType: "STK", Exchange: "SMART;NASDAQ"},
		},
	)

	if smartStock <= plainStock {
		t.Fatalf("expected SMART-routed stock score > plain stock score (smart=%d plain=%d)", smartStock, plainStock)
	}
}

func TestClassifyIBKRError_TransientTransport(t *testing.T) {
	err := NewIBKRTransportError("GET", "/iserver/account/orders", errors.New("connection refused"))
	if got := ClassifyIBKRError(err); got != IBKRErrorTransient {
		t.Fatalf("expected transient classification, got %s", got)
	}
	if !IsRetryableIBKRError(err) {
		t.Fatalf("expected retryable IBKR error")
	}
}

func TestClassifyIBKRError_TransientGatewayHTTP(t *testing.T) {
	err := NewIBKRHTTPError("GET", "/iserver/account/orders", 503, "gateway unavailable")
	if got := ClassifyIBKRError(err); got != IBKRErrorTransient {
		t.Fatalf("expected transient classification, got %s", got)
	}
}

func TestClassifyIBKRError_Auth(t *testing.T) {
	err := NewIBKRHTTPError("GET", "/portfolio/DU123456/summary", 403, "forbidden")
	if got := ClassifyIBKRError(err); got != IBKRErrorAuth {
		t.Fatalf("expected auth classification, got %s", got)
	}
	if !IsActionableIBKRError(err) {
		t.Fatalf("expected auth error to be operator-actionable")
	}
}

func TestClassifyIBKRError_Request(t *testing.T) {
	err := NewIBKRHTTPError("POST", "/iserver/account/orders", 400, "invalid contract")
	if got := ClassifyIBKRError(err); got != IBKRErrorRequest {
		t.Fatalf("expected request classification, got %s", got)
	}
	if IsRetryableIBKRError(err) {
		t.Fatalf("request error should not be retryable")
	}
}
