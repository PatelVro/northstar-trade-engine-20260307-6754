package trader

import (
	"errors"
	"strings"
	"time"
)

type brokerTruthSummary struct {
	Available           bool
	Required            bool
	BrokerManaged       bool
	Verified            bool
	TradingBlocked      bool
	AccountRequired     bool
	AccountVerified     bool
	OrdersRequired      bool
	OrdersVerified      bool
	PositionsRequired   bool
	PositionsVerified   bool
	MarketDataRequired  bool
	MarketDataVerified  bool
	AccountCapturedAt   time.Time
	OrdersCheckedAt     time.Time
	PositionsCheckedAt  time.Time
	MarketDataCheckedAt time.Time
	Message             string
	BlockingReasons     []string
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
	age := at.config.ScanInterval * 2
	if age < 5*time.Minute {
		age = 5 * time.Minute
	}
	if age > 30*time.Minute {
		age = 30 * time.Minute
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
		Available:       true,
		Required:        at.requiresHardBrokerTruthGate(),
		BrokerManaged:   at.requiresBrokerDependency() && !strings.EqualFold(at.config.Broker, "sim") && !at.shadowModeEnabled(),
		Verified:        true,
		TradingBlocked:  false,
		BlockingReasons: []string{},
	}
	if !summary.Required {
		summary.Message = "hard broker-truth gate is not required for this mode"
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
			at.accountSnapshotMu.RLock()
			if at.runtimeAccountSnapshot != nil {
				summary.AccountCapturedAt = at.runtimeAccountSnapshot.CapturedAt
			}
			at.accountSnapshotMu.RUnlock()
		} else {
			blocking = append(blocking, "broker account snapshot is unavailable or stale")
		}

		orderRecon := at.currentOrderReconciliationSummary()
		summary.OrdersRequired = orderRecon != nil
		if summary.OrdersRequired {
			summary.OrdersCheckedAt = orderRecon.LastRunAt
			lastError := strings.TrimSpace(orderRecon.LastError)
			switch {
			case orderRecon.LastRunAt.IsZero():
				blocking = append(blocking, "broker open-order truth has not been reconciled yet")
			case staleBrokerTruth(orderRecon.LastRunAt, at.brokerTruthOrderMaxAge()):
				blocking = append(blocking, "broker open-order truth is stale")
			case lastError != "":
				blocking = append(blocking, "broker open-order truth is degraded: "+lastError)
			default:
				summary.OrdersVerified = true
			}
		}

		positionRecon := at.currentPositionReconciliationSummary()
		summary.PositionsRequired = positionRecon != nil && positionRecon.Available
		if summary.PositionsRequired {
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
			}
		}
	}

	if at.requiresRuntimeMarketDataTruth() {
		summary.MarketDataRequired = true
		at.dataQualityMu.RLock()
		feedStatus := at.dataQualityState.FeedStatus
		at.dataQualityMu.RUnlock()
		summary.MarketDataCheckedAt = feedStatus.LastCheckedAt
		if feedStatus.Delayed {
			blocking = append(blocking, firstNonEmpty(strings.TrimSpace(feedStatus.Summary), "market-data feed is delayed or unavailable"))
		} else {
			summary.MarketDataVerified = true
		}
	}

	if len(blocking) > 0 {
		summary.Verified = false
		summary.TradingBlocked = true
		summary.BlockingReasons = append(summary.BlockingReasons, blocking...)
		summary.Message = blocking[0]
		return summary
	}

	summary.Verified = true
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
	if !summary.AccountVerified || !summary.OrdersVerified {
		return true
	}
	return false
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

	summary := at.currentBrokerTruthSummary()
	if !summary.TradingBlocked {
		return nil
	}
	return errors.New(strings.TrimSpace(summary.Message))
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
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
