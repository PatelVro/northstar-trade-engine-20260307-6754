package trader

import (
	"errors"
	"northstar/orders"
	"strings"
	"time"
)

type brokerTruthSummary struct {
	Available            bool
	Required             bool
	BrokerManaged        bool
	Verified             bool
	PreflightReady       bool
	CoherenceVerified    bool
	TradingBlocked       bool
	EntriesRestricted    bool
	ConfidenceDegraded   bool
	RestrictionReason    string
	AccountRequired      bool
	AccountVerified      bool
	AccountFresh         bool
	OrdersRequired       bool
	OrdersVerified       bool
	OrdersFresh          bool
	PositionsRequired    bool
	PositionsVerified    bool
	PositionsFresh       bool
	MarketDataRequired   bool
	MarketDataVerified   bool
	MarketDataFresh      bool
	PreflightCheckedAt   time.Time
	AccountCapturedAt    time.Time
	OrdersCheckedAt      time.Time
	PositionsCheckedAt   time.Time
	MarketDataCheckedAt  time.Time
	InferredOrderCount   int
	UnresolvedOrderCount int
	PrimaryIssueLocalID  string
	PrimaryIssueBrokerID string
	PrimaryAuthority     orders.TruthAuthority
	PrimaryConfidence    orders.TruthConfidence
	PrimaryReason        string
	PrimaryNeedsReview   bool
	Message              string
	BlockingReasons      []string
}

func (at *AutoTrader) requiresHardBrokerTruthGate() bool {
	if at == nil || at.demoMode || at.backtestMode || strings.EqualFold(at.config.Mode, "replay") {
		return false
	}
	if at.shadowModeEnabled() {
		return at.requiresRuntimeMarketDataTruth()
	}
	return at.requiresBrokerDependency() || at.requiresRuntimeMarketDataTruth()
}

func (at *AutoTrader) requiresRuntimeMarketDataTruth() bool {
	if at == nil || at.demoMode || at.backtestMode || strings.EqualFold(at.config.Mode, "replay") {
		return false
	}
	return strings.EqualFold(at.config.InstrumentType, "equity") &&
		strings.EqualFold(at.config.DataProvider, "ibkr")
}

func (at *AutoTrader) brokerTruthAccountMaxAge() time.Duration {
	if at == nil {
		return 5 * time.Minute
	}
	age := at.config.ScanInterval
	if age <= 0 {
		age = 5 * time.Minute
	}
	if age < 2*time.Minute {
		age = 2 * time.Minute
	}
	if age > 10*time.Minute {
		age = 10 * time.Minute
	}
	return age
}

func (at *AutoTrader) brokerTruthOrderMaxAge() time.Duration {
	age := 20 * time.Second
	if at != nil && at.config.ScanInterval > 0 {
		candidate := at.config.ScanInterval / 6
		if candidate > age {
			age = candidate
		}
	}
	if age > 45*time.Second {
		age = 45 * time.Second
	}
	return age
}

func (at *AutoTrader) brokerTruthPositionMaxAge() time.Duration {
	age := 20 * time.Second
	if at != nil {
		candidate := at.positionReconciliationInterval() * 2
		if candidate > age {
			age = candidate
		}
	}
	if age > 60*time.Second {
		age = 60 * time.Second
	}
	return age
}

func (at *AutoTrader) brokerTruthMarketDataMaxAge() time.Duration {
	age := at.config.ScanInterval
	if age <= 0 {
		age = time.Minute
	}
	if age < 30*time.Second {
		age = 30 * time.Second
	}
	if age > 5*time.Minute {
		age = 5 * time.Minute
	}
	return age
}

func staleBrokerTruth(ts time.Time, maxAge time.Duration) bool {
	if ts.IsZero() {
		return true
	}
	if maxAge <= 0 {
		return false
	}
	return time.Since(ts) > maxAge
}

func (at *AutoTrader) currentBrokerTruthSummary() brokerTruthSummary {
	summary := brokerTruthSummary{
		Available:         true,
		Required:          at.requiresHardBrokerTruthGate(),
		BrokerManaged:     at.requiresBrokerDependency() && !strings.EqualFold(at.config.Broker, "sim") && !at.shadowModeEnabled(),
		CoherenceVerified: true,
		Verified:          true,
		TradingBlocked:    false,
		BlockingReasons:   []string{},
	}
	if !summary.Required {
		summary.Message = "hard broker-truth gate is not required for this mode"
		summary.PreflightReady = true
		return summary
	}

	blocking := make([]string, 0, 4)

	if summary.BrokerManaged {
		brokerStatus := at.brokerRuntimeStatus()
		if brokerStatus.State != BrokerRuntimeHealthy {
			summary.Verified = false
			summary.Message = firstNonEmpty(strings.TrimSpace(brokerStatus.Reason), "broker runtime is not healthy enough to verify broker truth")
			return summary
		}

		summary.AccountRequired = true
		if account, _, ok := at.currentRuntimeAccountSnapshot(at.brokerTruthAccountMaxAge()); ok && account != nil {
			summary.AccountVerified = true
			summary.AccountFresh = true
			at.accountSnapshotMu.RLock()
			if at.runtimeAccountSnapshot != nil {
				summary.AccountCapturedAt = at.runtimeAccountSnapshot.CapturedAt
			}
			at.accountSnapshotMu.RUnlock()
		} else {
			blocking = append(blocking, "broker account snapshot is unavailable or stale")
		}

		orderRecon := at.currentOrderReconciliationSummary()
		summary.OrdersRequired = true
		if orderRecon == nil {
			blocking = append(blocking, "broker open-order reconciliation summary is unavailable")
		} else {
			summary.OrdersCheckedAt = orderRecon.LastRunAt
			summary.InferredOrderCount = orderRecon.CurrentInferredOrders
			summary.UnresolvedOrderCount = orderRecon.CurrentUnresolvedOrders
			if issue := orders.PrimaryExecutionTruthIssue(orderRecon.LastIssues); issue != nil {
				summary.PrimaryIssueLocalID = strings.TrimSpace(issue.LocalID)
				summary.PrimaryIssueBrokerID = strings.TrimSpace(issue.BrokerOrderID)
				summary.PrimaryAuthority = issue.Authority
				summary.PrimaryConfidence = issue.Confidence
				summary.PrimaryReason = strings.TrimSpace(issue.Message)
				summary.PrimaryNeedsReview = issue.NeedsReview
			}
			lastError := strings.TrimSpace(orderRecon.LastError)
			switch {
			case orderRecon.LastRunAt.IsZero():
				blocking = append(blocking, "broker open-order truth has not been reconciled yet")
			case staleBrokerTruth(orderRecon.LastRunAt, at.brokerTruthOrderMaxAge()):
				blocking = append(blocking, "broker open-order truth is stale")
			case lastError != "":
				blocking = append(blocking, "broker open-order truth is degraded: "+lastError)
			case orderRecon.CurrentUnresolvedOrders > 0:
				blocking = append(blocking, "broker order truth remains unresolved for one or more broker-missing orders")
			default:
				summary.OrdersVerified = true
				summary.OrdersFresh = true
				if orderRecon.CurrentInferredOrders > 0 {
					summary.ConfidenceDegraded = true
				}
			}
		}

		positionRecon := at.currentPositionReconciliationSummary()
		summary.PositionsRequired = true
		if positionRecon == nil || !positionRecon.Available {
			blocking = append(blocking, "broker position reconciliation summary is unavailable")
		} else {
			summary.PositionsCheckedAt = firstNonZeroTime(positionRecon.LastReconciledAt, positionRecon.LastSuccessAt, positionRecon.LastRunAt)
			switch {
			case !positionRecon.TradingAllowed:
				summary.Verified = false
				summary.Message = firstNonEmpty(strings.TrimSpace(positionRecon.Summary), "broker position truth is blocked pending reconciliation")
				return summary
			case summary.PositionsCheckedAt.IsZero():
				blocking = append(blocking, "broker position truth has not been established yet")
			case staleBrokerTruth(summary.PositionsCheckedAt, at.brokerTruthPositionMaxAge()):
				blocking = append(blocking, "broker position truth is stale")
			default:
				summary.PositionsVerified = true
				summary.PositionsFresh = true
			}
		}
	}

	if at.requiresRuntimeMarketDataTruth() {
		summary.MarketDataRequired = true
		at.dataQualityMu.RLock()
		feedStatus := at.dataQualityState.FeedStatus
		at.dataQualityMu.RUnlock()
		summary.MarketDataCheckedAt = feedStatus.LastCheckedAt
		switch {
		case feedStatus.LastCheckedAt.IsZero():
			blocking = append(blocking, "market-data truth has not been preflighted yet")
		case staleBrokerTruth(feedStatus.LastCheckedAt, at.brokerTruthMarketDataMaxAge()):
			blocking = append(blocking, "market-data truth is stale and must be revalidated")
		case feedStatus.Delayed:
			blocking = append(blocking, firstNonEmpty(strings.TrimSpace(feedStatus.Summary), "market-data feed is delayed or unavailable"))
		default:
			summary.MarketDataVerified = true
			summary.MarketDataFresh = true
		}
	}

	if len(blocking) > 0 {
		summary.Verified = false
		summary.TradingBlocked = true
		summary.BlockingReasons = append(summary.BlockingReasons, blocking...)
		summary.Message = blocking[0]
		if summary.UnresolvedOrderCount > 0 && summary.PrimaryReason != "" {
			summary.Message = summary.PrimaryReason
			if !containsString(summary.BlockingReasons, summary.PrimaryReason) {
				summary.BlockingReasons = append(summary.BlockingReasons, summary.PrimaryReason)
			}
		}
		return summary
	}

	summary.PreflightCheckedAt = oldestNonZeroTime(
		requiredPreflightTime(summary.AccountRequired, summary.AccountCapturedAt),
		requiredPreflightTime(summary.OrdersRequired, summary.OrdersCheckedAt),
		requiredPreflightTime(summary.PositionsRequired, summary.PositionsCheckedAt),
		requiredPreflightTime(summary.MarketDataRequired, summary.MarketDataCheckedAt),
	)
	summary.Verified = true
	if summary.ConfidenceDegraded && summary.InferredOrderCount > 0 {
		summary.EntriesRestricted = true
		summary.RestrictionReason = "broker truth confidence is degraded by reconciliation-inferred execution outcomes; new entries are restricted pending clean reconciliation"
		summary.PreflightReady = false
		if summary.PrimaryAuthority == orders.TruthAuthorityReconciliationInferred && summary.PrimaryReason != "" {
			summary.Message = summary.RestrictionReason + ": " + summary.PrimaryReason
			return summary
		}
		summary.Message = summary.RestrictionReason
		return summary
	}
	summary.PreflightReady = true
	summary.Message = "broker/account/order/position/data truth verified for active mode"
	return summary
}

func (at *AutoTrader) brokerTruthNeedsRefresh() bool {
	if at == nil || !at.managesIBKRBrokerRuntime() {
		return false
	}
	summary := at.currentBrokerTruthSummary()
	if !summary.Required || !summary.BrokerManaged {
		return false
	}
	if !summary.AccountVerified || !summary.OrdersVerified || !summary.PositionsVerified {
		return true
	}
	return false
}

func (at *AutoTrader) brokerTruthNeedsMarketDataRefresh() bool {
	if at == nil {
		return false
	}
	summary := at.currentBrokerTruthSummary()
	if !summary.Required || !summary.MarketDataRequired {
		return false
	}
	return !summary.MarketDataVerified || !summary.MarketDataFresh
}

func (at *AutoTrader) ensureBrokerTruthReadyForTrading() error {
	if at == nil || !at.requiresHardBrokerTruthGate() {
		return nil
	}

	if at.managesIBKRBrokerRuntime() && at.brokerTruthNeedsRefresh() {
		if err := at.ensureIBKRRuntimeReady(); err != nil {
			return err
		}
		if err := at.reconcileIBKRRuntime(); err != nil {
			return at.handleIBKRRuntimeError("broker_truth_reconcile", err)
		}
	}
	if at.brokerTruthNeedsMarketDataRefresh() {
		if err := at.preflightRuntimeMarketData(nil); err != nil {
			return err
		}
	}

	summary := at.currentBrokerTruthSummary()
	if !summary.TradingBlocked {
		return nil
	}
	return errors.New(strings.TrimSpace(summary.Message))
}

func requiredPreflightTime(required bool, ts time.Time) time.Time {
	if !required {
		return time.Time{}
	}
	return ts
}

func oldestNonZeroTime(values ...time.Time) time.Time {
	var oldest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if oldest.IsZero() || value.Before(oldest) {
			oldest = value
		}
	}
	return oldest
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func (at *AutoTrader) seedRuntimeAccountSnapshot(balance map[string]interface{}, rawPositions []map[string]interface{}) {
	if at == nil || balance == nil {
		return
	}
	summary := at.buildAccountSummaryFromRaw(balance, rawPositions)
	positions := normalizePositionViews(rawPositions)
	at.setRuntimeAccountSnapshot(summary, positions)
	at.setLatestAccountSummary(&summary)
}
