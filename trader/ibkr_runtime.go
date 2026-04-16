package trader

import (
	"fmt"
	"log"
	"northstar/broker"
	"northstar/decision"
	"northstar/market"
	"strings"
	"time"
)

type BrokerRuntimeState string

const (
	BrokerRuntimeHealthy      BrokerRuntimeState = "healthy"
	BrokerRuntimeDegraded     BrokerRuntimeState = "degraded"
	BrokerRuntimeReconnecting BrokerRuntimeState = "reconnecting"
	BrokerRuntimeReconciling  BrokerRuntimeState = "reconciling"
	BrokerRuntimePaused       BrokerRuntimeState = "paused"
)

type brokerRuntimeSnapshot struct {
	State             BrokerRuntimeState
	Reason            string
	LastError         string
	Since             time.Time
	LastHealthyAt     time.Time
	LastReconciledAt  time.Time
	ReconnectAttempts int
	NextRetryAt       time.Time
	RecoveryActive    bool
}

type ibkrRuntimeReconciler interface {
	ReconcileBrokerState() (*IBKRBrokerSnapshot, error)
}

func (at *AutoTrader) initializeBrokerRuntimeState() {
	now := time.Now()

	at.brokerStateMu.Lock()
	defer at.brokerStateMu.Unlock()

	at.brokerState = BrokerRuntimeHealthy
	at.brokerStateReason = "broker runtime ready"
	at.brokerStateSince = now
	at.brokerLastHealthyAt = now
}

func (at *AutoTrader) managesIBKRBrokerRuntime() bool {
	return !at.demoMode &&
		strings.EqualFold(at.exchange, "ibkr") &&
		strings.EqualFold(at.config.Broker, "ibkr") &&
		!strings.EqualFold(at.config.Mode, "replay")
}

func (at *AutoTrader) brokerRuntimeStatus() brokerRuntimeSnapshot {
	at.brokerStateMu.RLock()
	defer at.brokerStateMu.RUnlock()

	return brokerRuntimeSnapshot{
		State:             at.brokerState,
		Reason:            at.brokerStateReason,
		LastError:         at.brokerLastError,
		Since:             at.brokerStateSince,
		LastHealthyAt:     at.brokerLastHealthyAt,
		LastReconciledAt:  at.brokerLastReconciledAt,
		ReconnectAttempts: at.brokerReconnectAttempts,
		NextRetryAt:       at.brokerNextRetryAt,
		RecoveryActive:    at.brokerRecoveryActive,
	}
}

func (at *AutoTrader) ensureIBKRRuntimeReady() error {
	if !at.managesIBKRBrokerRuntime() {
		return nil
	}

	snapshot := at.brokerRuntimeStatus()
	if snapshot.State != BrokerRuntimeHealthy {
		if snapshot.State != BrokerRuntimePaused {
			at.startIBKRRecoveryLoop()
		}
		return fmt.Errorf("ibkr broker state=%s: %s", snapshot.State, snapshot.Reason)
	}

	ibkrProv, ok := at.provider.(*market.IBKRProvider)
	if ok && ibkrProv != nil && ibkrProv.Client != nil && !ibkrProv.Client.IsAuthenticated() {
		return at.handleIBKRRuntimeError("session_check", fmt.Errorf("IBKR session not ready"))
	}

	return nil
}

func (at *AutoTrader) handleIBKRRuntimeError(stage string, err error) error {
	if err == nil || !at.managesIBKRBrokerRuntime() {
		return err
	}

	reason := fmt.Sprintf("%s: %v", stage, err)
	switch broker.ClassifyIBKRError(err) {
	case broker.IBKRErrorTransient:
		at.alertBrokerDisconnect(stage, err)
		at.setBrokerRuntimeState(BrokerRuntimeDegraded, reason, err, false, time.Time{})
		at.startIBKRRecoveryLoop()
	case broker.IBKRErrorAuth:
		at.setBrokerRuntimeState(BrokerRuntimePaused, reason, err, false, time.Time{})
	}

	return err
}

func (at *AutoTrader) startIBKRRecoveryLoop() {
	if !at.managesIBKRBrokerRuntime() || !at.isRunning {
		return
	}

	at.brokerStateMu.Lock()
	if at.brokerRecoveryActive || at.brokerState == BrokerRuntimePaused {
		at.brokerStateMu.Unlock()
		return
	}
	at.brokerRecoveryActive = true
	at.brokerStateMu.Unlock()

	go at.runIBKRRecoveryLoop()
}

// ibkrMaxConsecutiveFailures is the number of consecutive reconnect failures
// that triggers a runtime degradation escalation incident.
const ibkrMaxConsecutiveFailures = 5

func (at *AutoTrader) runIBKRRecoveryLoop() {
	defer func() {
		at.brokerStateMu.Lock()
		at.brokerRecoveryActive = false
		at.brokerStateMu.Unlock()
	}()

	consecutiveFailures := 0

	for attempt := 1; at.isRunning; attempt++ {
		backoff := ibkrRecoveryBackoff(attempt)
		nextRetryAt := time.Now().Add(backoff)

		log.Printf(" [%s] IBKR recovery: attempt=%d next_retry=%s backoff=%s",
			at.name, attempt, nextRetryAt.Format(time.RFC3339), backoff)

		at.setBrokerReconnectState(attempt, nextRetryAt)
		if err := at.checkIBKRSessionReadiness(); err != nil {
			consecutiveFailures++
			at.setBrokerRuntimeState(
				BrokerRuntimeDegraded,
				fmt.Sprintf("reconnect attempt %d failed: %v", attempt, err),
				err,
				true,
				nextRetryAt,
			)

			if consecutiveFailures >= ibkrMaxConsecutiveFailures {
				log.Printf(" [%s] IBKR recovery: %d consecutive failures — escalating to runtime degradation incident",
					at.name, consecutiveFailures)
				at.setBrokerRuntimeState(
					BrokerRuntimeDegraded,
					fmt.Sprintf("escalated: %d consecutive reconnect failures; last error: %v", consecutiveFailures, err),
					err,
					true,
					nextRetryAt,
				)
				at.alertRuntimeDegraded(BrokerRuntimeDegraded,
					fmt.Sprintf("IBKR broker unreachable after %d consecutive reconnect attempts", consecutiveFailures))
			}

			if !at.sleepWhileRunning(backoff) {
				return
			}
			continue
		}

		consecutiveFailures = 0

		at.setBrokerRuntimeState(
			BrokerRuntimeReconciling,
			"connectivity restored; reconciling account summary, positions, and open orders",
			nil,
			true,
			time.Time{},
		)
		if err := at.reconcileIBKRRuntime(); err != nil {
			consecutiveFailures++
			at.alertReconciliationFailure("recovery_loop", err)
			at.setBrokerRuntimeState(
				BrokerRuntimeDegraded,
				fmt.Sprintf("reconciliation failed after reconnect: %v", err),
				err,
				true,
				nextRetryAt,
			)
			if !at.sleepWhileRunning(backoff) {
				return
			}
			continue
		}

		at.markIBKRHealthy()
		return
	}
}

func (at *AutoTrader) setBrokerReconnectState(attempt int, nextRetryAt time.Time) {
	at.brokerStateMu.Lock()
	at.brokerReconnectAttempts = attempt
	at.brokerNextRetryAt = nextRetryAt
	at.brokerStateMu.Unlock()
	at.observePaperBrokerReconnectAttempt()

	at.setBrokerRuntimeState(
		BrokerRuntimeReconnecting,
		fmt.Sprintf("reconnect attempt %d in progress", attempt),
		nil,
		true,
		nextRetryAt,
	)
}

func (at *AutoTrader) setBrokerRuntimeState(state BrokerRuntimeState, reason string, err error, recoveryActive bool, nextRetryAt time.Time) {
	now := time.Now()
	logLine := ""

	at.brokerStateMu.Lock()
	prevState := at.brokerState
	at.brokerState = state
	at.brokerStateReason = strings.TrimSpace(reason)
	if state != prevState || at.brokerStateSince.IsZero() {
		at.brokerStateSince = now
	}
	if err != nil {
		at.brokerLastError = err.Error()
	}
	at.brokerRecoveryActive = recoveryActive
	if !nextRetryAt.IsZero() {
		at.brokerNextRetryAt = nextRetryAt
	} else if state == BrokerRuntimeHealthy || state == BrokerRuntimePaused {
		at.brokerNextRetryAt = time.Time{}
	}
	logLine = at.brokerRuntimeLogLineLocked(state)
	shouldLog := at.shouldLogBrokerStateLocked(logLine, now)
	at.brokerStateMu.Unlock()

	if state != BrokerRuntimeHealthy {
		at.invalidateRuntimeAccountSnapshot()
	}

	if shouldLog {
		log.Printf(" [%s] %s", at.name, logLine)
	}
	at.syncBrokerRuntimeIncident(state, reason)
	if (state == BrokerRuntimeDegraded || state == BrokerRuntimePaused) && prevState != state {
		at.observeRiskSupervisorBrokerDegradation()
	}
	if state == BrokerRuntimeDegraded || state == BrokerRuntimePaused {
		at.alertRuntimeDegraded(state, reason)
	}
	at.observePaperBrokerState(state, reason)
}

func (at *AutoTrader) markIBKRHealthy() {
	at.markIBKRHealthyWithReason("reconciliation complete; trading resumed")
}

func (at *AutoTrader) markIBKRHealthyWithReason(reason string) {
	now := time.Now()
	if strings.TrimSpace(reason) == "" {
		reason = "reconciliation complete; trading resumed"
	}

	at.brokerStateMu.Lock()
	at.brokerState = BrokerRuntimeHealthy
	at.brokerStateReason = reason
	at.brokerStateSince = now
	at.brokerLastHealthyAt = now
	at.brokerLastReconciledAt = now
	at.brokerReconnectAttempts = 0
	at.brokerNextRetryAt = time.Time{}
	at.brokerRecoveryActive = false
	logLine := at.brokerRuntimeLogLineLocked(BrokerRuntimeHealthy)
	shouldLog := at.shouldLogBrokerStateLocked(logLine, now)
	at.brokerStateMu.Unlock()

	if shouldLog {
		log.Printf(" [%s] %s", at.name, logLine)
	}
	at.observePaperBrokerState(BrokerRuntimeHealthy, reason)
}

func (at *AutoTrader) shouldLogBrokerStateLocked(line string, now time.Time) bool {
	if line == "" {
		return false
	}
	if at.brokerLastStateLogKey == line && now.Sub(at.brokerLastStateLogAt) < 20*time.Second {
		return false
	}
	at.brokerLastStateLogKey = line
	at.brokerLastStateLogAt = now
	return true
}

func (at *AutoTrader) brokerRuntimeLogLineLocked(state BrokerRuntimeState) string {
	line := fmt.Sprintf("IBKR broker state -> %s", state)
	if at.brokerStateReason != "" {
		line += fmt.Sprintf(" | %s", at.brokerStateReason)
	}
	if at.brokerReconnectAttempts > 0 && state != BrokerRuntimeHealthy {
		line += fmt.Sprintf(" | attempts=%d", at.brokerReconnectAttempts)
	}
	if !at.brokerNextRetryAt.IsZero() && state != BrokerRuntimeHealthy {
		line += fmt.Sprintf(" | next_retry=%s", at.brokerNextRetryAt.Format(time.RFC3339))
	}
	return line
}

func (at *AutoTrader) checkIBKRSessionReadiness() error {
	ibkrProv, ok := at.provider.(*market.IBKRProvider)
	if !ok || ibkrProv == nil || ibkrProv.Client == nil {
		return fmt.Errorf("IBKR provider is not initialized")
	}
	return ibkrProv.Client.CheckSessionReadiness(at.config.IBKRAccountID)
}

func (at *AutoTrader) reconcileIBKRRuntime() error {
	reconciler, ok := at.trader.(ibkrRuntimeReconciler)
	if !ok {
		return fmt.Errorf("IBKR trader does not support runtime reconciliation")
	}

	snapshot, err := reconciler.ReconcileBrokerState()
	if err != nil {
		return err
	}

	if snapshot == nil {
		return fmt.Errorf("IBKR reconciliation returned no snapshot")
	}

	if snapshot.Balance != nil {
		summary := at.buildAccountSummaryFromRaw(snapshot.Balance, snapshot.Positions)
		normalizedPositions := normalizePositionViews(snapshot.Positions)
		at.setRuntimeAccountSnapshot(summary, normalizedPositions)
		at.setLatestAccountSummary(&summary)
	} else {
		at.invalidateRuntimeAccountSnapshot()
	}

	at.refreshPositionState(positionInfoFromBrokerMaps(snapshot.Positions))
	at.persistDurableRuntimeState("broker_runtime_reconcile")
	return nil
}

func (at *AutoTrader) sleepWhileRunning(delay time.Duration) bool {
	if delay <= 0 {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return at.isRunning
		default:
			if !at.isRunning {
				return false
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// ibkrRecoveryBackoff returns the exponential backoff duration for reconnect attempt n.
// Starts at 5 s, doubles each attempt, caps at 60 s.
//
//	attempt 1 →  5 s
//	attempt 2 → 10 s
//	attempt 3 → 20 s
//	attempt 4 → 40 s
//	attempt 5+ → 60 s
func ibkrRecoveryBackoff(attempt int) time.Duration {
	const (
		initial = 5 * time.Second
		maximum = 60 * time.Second
	)
	if attempt <= 1 {
		return initial
	}
	backoff := initial
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= maximum {
			return maximum
		}
	}
	return backoff
}

func positionInfoFromBrokerMaps(raw []map[string]interface{}) []decision.PositionInfo {
	out := make([]decision.PositionInfo, 0, len(raw))
	for _, pos := range raw {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		entryPrice, _ := parseFloat(pos["entryPrice"])
		markPrice, _ := parseFloat(pos["markPrice"])
		quantity, _ := parseFloat(pos["positionAmt"])
		if quantity < 0 {
			quantity = -quantity
		}
		unrealized, _ := parseFloat(pos["unRealizedProfit"])
		liquidation, _ := parseFloat(pos["liquidationPrice"])

		leverage := 1
		if lev, ok := parseFloat(pos["leverage"]); ok && lev > 0 {
			leverage = int(lev)
		}

		unrealizedPct := 0.0
		if entryPrice > 0 && leverage > 0 {
			if strings.EqualFold(side, "short") {
				unrealizedPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100.0
			} else {
				unrealizedPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100.0
			}
		}

		out = append(out, decision.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealized,
			UnrealizedPnLPct: unrealizedPct,
			LiquidationPrice: liquidation,
			MarginUsed:       0,
			UpdateTime:       time.Now().UnixMilli(),
		})
	}
	return out
}
