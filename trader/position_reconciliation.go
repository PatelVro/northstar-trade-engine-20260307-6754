package trader

import (
	"fmt"
	"log"
	"math"
	"northstar/logger"
	"northstar/positions"
	"strings"
	"time"
)

type PositionReconciliationStatus string

const (
	PositionReconciliationNotApplicable PositionReconciliationStatus = "not_applicable"
	PositionReconciliationPending       PositionReconciliationStatus = "pending"
	PositionReconciliationHealthy       PositionReconciliationStatus = "healthy"
	PositionReconciliationBlocked       PositionReconciliationStatus = "blocked"
	PositionReconciliationReconciling   PositionReconciliationStatus = "reconciling"
)

type positionReconciliationSummary struct {
	Available            bool
	Status               PositionReconciliationStatus
	Summary              string
	TradingAllowed       bool
	LastRunAt            time.Time
	LastSuccessAt        time.Time
	LastIncidentAt       time.Time
	LastReconciledAt     time.Time
	LastError            string
	TotalRuns            int
	TotalIncidents       int
	TotalMismatches      int
	LocalMissingAtBroker int
	BrokerMissingLocally int
	SizeMismatches       int
	PriceMismatches      int
	LocalPositions       int
	BrokerPositions      int
	LastIssues           []positions.Issue
	loopActive           bool
}

func (at *AutoTrader) initializePositionReconciliationState() {
	summary := positionReconciliationSummary{
		Available:      at.managesPositionReconciliation(),
		Status:         PositionReconciliationNotApplicable,
		Summary:        "position reconciliation is not required for this trader",
		TradingAllowed: true,
		LastIssues:     []positions.Issue{},
	}
	if summary.Available {
		summary.Status = PositionReconciliationPending
		summary.Summary = "startup broker position baseline not established"
		summary.TradingAllowed = false
	}

	at.positionReconMu.Lock()
	at.positionReconSummary = summary
	if at.localPositionSnapshots == nil {
		at.localPositionSnapshots = make(map[string]positions.Snapshot)
	}
	at.positionReconMu.Unlock()
}

func (at *AutoTrader) managesPositionReconciliation() bool {
	return !at.demoMode &&
		!strings.EqualFold(at.config.Mode, "replay") &&
		at.trader != nil &&
		!strings.EqualFold(at.config.Broker, "sim")
}

func (at *AutoTrader) ensurePositionReconciliationReady() error {
	if !at.managesPositionReconciliation() {
		return nil
	}
	summary := at.currentPositionReconciliationSummary()
	if summary == nil || !summary.TradingAllowed {
		reason := "position reconciliation not ready"
		if summary != nil && strings.TrimSpace(summary.Summary) != "" {
			reason = summary.Summary
		}
		status := PositionReconciliationBlocked
		if summary != nil && summary.Status != "" {
			status = summary.Status
		}
		return fmt.Errorf("position reconciliation %s: %s", status, reason)
	}
	return nil
}

func (at *AutoTrader) currentPositionReconciliationSummary() *positionReconciliationSummary {
	at.positionReconMu.RLock()
	defer at.positionReconMu.RUnlock()
	if !at.positionReconSummary.Available && at.positionReconSummary.Status == "" {
		return nil
	}
	summary := at.positionReconSummary
	summary.LastIssues = append([]positions.Issue(nil), at.positionReconSummary.LastIssues...)
	return &summary
}

func (at *AutoTrader) positionReconciliationTradingAllowed() bool {
	summary := at.currentPositionReconciliationSummary()
	return summary == nil || summary.TradingAllowed
}

func (at *AutoTrader) bootstrapPositionReconciliation() error {
	if !at.managesPositionReconciliation() {
		return nil
	}
	now := time.Now()
	rawPositions, err := at.trader.GetPositions()
	if err != nil {
		at.observePositionReconciliationFailure("failed to fetch broker positions", err, now)
		at.markPositionReconciliationBlocked("startup broker position baseline failed", err, nil)
		at.alertReconciliationFailure("position_reconciliation_startup", err)
		return err
	}
	snapshots := positionSnapshotsFromBrokerMaps(rawPositions)
	at.setLocalPositionSnapshots(snapshots, "broker_startup_baseline", now)
	at.refreshPositionState(positionInfoFromBrokerMaps(rawPositions))

	at.positionReconMu.Lock()
	at.positionReconSummary.Status = PositionReconciliationHealthy
	at.positionReconSummary.Summary = fmt.Sprintf("startup position baseline synchronized from broker (%d positions)", len(snapshots))
	at.positionReconSummary.TradingAllowed = true
	at.positionReconSummary.LastRunAt = now
	at.positionReconSummary.LastSuccessAt = now
	at.positionReconSummary.LastReconciledAt = now
	at.positionReconSummary.LastError = ""
	at.positionReconSummary.LocalPositions = len(snapshots)
	at.positionReconSummary.BrokerPositions = len(snapshots)
	at.positionReconSummary.LastIssues = []positions.Issue{}
	at.positionReconMu.Unlock()
	at.syncPositionReconciliationIncident("startup position baseline synchronized from broker", nil, nil)

	log.Printf(" [%s] Position reconciliation baseline established from broker (%d positions)", at.name, len(snapshots))
	return nil
}

func (at *AutoTrader) waitForPositionReconciliationBootstrap() error {
	if !at.managesPositionReconciliation() {
		return nil
	}
	for at.isRunning {
		if err := at.bootstrapPositionReconciliation(); err == nil {
			return nil
		} else {
			at.alertTradingBlocked("position reconciliation startup blocked")
			delay := at.positionReconciliationInterval()
			log.Printf(" [%s] Active trading remains blocked by position reconciliation startup; retrying in %s", at.name, delay)
			if !at.sleepWhileRunning(delay) {
				return nil
			}
		}
	}
	return nil
}

func (at *AutoTrader) startPositionReconciliationLoop() {
	if !at.managesPositionReconciliation() || !at.isRunning {
		return
	}

	at.positionReconMu.Lock()
	if at.positionReconSummary.loopActive {
		at.positionReconMu.Unlock()
		return
	}
	at.positionReconSummary.loopActive = true
	at.positionReconMu.Unlock()

	go at.runPositionReconciliationLoop()
}

func (at *AutoTrader) runPositionReconciliationLoop() {
	defer func() {
		at.positionReconMu.Lock()
		at.positionReconSummary.loopActive = false
		at.positionReconMu.Unlock()
	}()

	for at.isRunning {
		at.runPositionReconciliationCheck("periodic")
		if !at.sleepWhileRunning(at.positionReconciliationInterval()) {
			return
		}
	}
}

func (at *AutoTrader) positionReconciliationInterval() time.Duration {
	delay := at.config.ScanInterval / 2
	if delay <= 0 {
		delay = 15 * time.Second
	}
	if delay < 5*time.Second {
		delay = 5 * time.Second
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

func (at *AutoTrader) runPositionReconciliationCheck(stage string) {
	if !at.managesPositionReconciliation() {
		return
	}
	now := time.Now()
	local := at.snapshotLocalPositions()
	rawBrokerPositions, err := at.trader.GetPositions()
	if err != nil {
		at.observePositionReconciliationFailure("failed to fetch broker positions", err, now)
		at.markPositionReconciliationBlocked("failed to fetch broker positions", err, nil)
		at.alertReconciliationFailure("position_reconciliation_"+stage, err)
		at.recordPaperSessionError(fmt.Sprintf("position reconciliation failed to fetch broker positions: %v", err))
		return
	}
	brokerSnapshots := positionSnapshotsFromBrokerMaps(rawBrokerPositions)
	result := positions.Compare(local, brokerSnapshots, now)
	at.observePositionReconciliationRun(result, "")

	if result.Mismatches == 0 {
		at.markPositionReconciliationHealthy(result.Summary, now, len(local), len(brokerSnapshots))
		return
	}

	at.logPositionReconciliationIncident(result)
	at.markPositionReconciliationBlocked(result.Summary, nil, result.Issues)
	at.recordPaperSessionWarning(result.Summary)
	at.alertTradingBlocked("position reconciliation mismatch detected")

	reconciledSnapshots, err := at.reconcileLocalPositionsFromBroker(stage, rawBrokerPositions)
	if err != nil {
		at.observePositionReconciliationFailure("broker position reconciliation failed", err, time.Now())
		at.markPositionReconciliationBlocked("broker position reconciliation failed", err, result.Issues)
		at.alertReconciliationFailure("position_reconciliation_"+stage, err)
		at.recordPaperSessionError(fmt.Sprintf("position reconciliation repair failed: %v", err))
		return
	}

	at.markPositionReconciliationHealthy(
		fmt.Sprintf("reconciled local positions from broker truth after %d mismatch(es)", result.Mismatches),
		time.Now(),
		len(reconciledSnapshots),
		len(reconciledSnapshots),
	)
}

func (at *AutoTrader) reconcileLocalPositionsFromBroker(stage string, knownBrokerPositions []map[string]interface{}) ([]positions.Snapshot, error) {
	at.setPositionReconciliationStatus(PositionReconciliationReconciling, "reconciling local positions from broker truth", false, nil, nil)

	var rawPositions []map[string]interface{}
	if reconciler, ok := at.trader.(ibkrRuntimeReconciler); ok {
		snapshot, err := reconciler.ReconcileBrokerState()
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, fmt.Errorf("broker reconciliation returned no snapshot")
		}
		rawPositions = snapshot.Positions
	} else if knownBrokerPositions != nil {
		rawPositions = knownBrokerPositions
	} else {
		var err error
		rawPositions, err = at.trader.GetPositions()
		if err != nil {
			return nil, err
		}
	}

	snapshots := positionSnapshotsFromBrokerMaps(rawPositions)
	at.setLocalPositionSnapshots(snapshots, "broker_reconciliation", time.Now())
	at.refreshPositionState(positionInfoFromBrokerMaps(rawPositions))
	log.Printf(" [%s] Position reconciliation refreshed local state from broker (%d positions) during %s", at.name, len(snapshots), stage)
	return snapshots, nil
}

func (at *AutoTrader) observePositionReconciliationRun(result positions.Result, lastError string) {
	at.positionReconMu.Lock()
	defer at.positionReconMu.Unlock()

	at.positionReconSummary.LastRunAt = result.RanAt
	at.positionReconSummary.TotalRuns++
	at.positionReconSummary.LocalPositions = result.LocalPositions
	at.positionReconSummary.BrokerPositions = result.BrokerPositions
	if strings.TrimSpace(lastError) != "" {
		at.positionReconSummary.LastError = lastError
	}
	if result.Mismatches > 0 {
		at.positionReconSummary.TotalIncidents++
		at.positionReconSummary.LastIncidentAt = result.RanAt
		at.positionReconSummary.TotalMismatches += result.Mismatches
		at.positionReconSummary.LocalMissingAtBroker += result.LocalMissingAtBroker
		at.positionReconSummary.BrokerMissingLocally += result.BrokerMissingLocally
		at.positionReconSummary.SizeMismatches += result.SizeMismatches
		at.positionReconSummary.PriceMismatches += result.PriceMismatches
		at.positionReconSummary.LastIssues = append([]positions.Issue(nil), result.Issues...)
	}
}

func (at *AutoTrader) markPositionReconciliationHealthy(summary string, now time.Time, localCount, brokerCount int) {
	at.positionReconMu.Lock()
	at.positionReconSummary.Status = PositionReconciliationHealthy
	at.positionReconSummary.Summary = strings.TrimSpace(summary)
	at.positionReconSummary.TradingAllowed = true
	at.positionReconSummary.LastSuccessAt = now
	at.positionReconSummary.LastReconciledAt = now
	at.positionReconSummary.LastError = ""
	at.positionReconSummary.LocalPositions = localCount
	at.positionReconSummary.BrokerPositions = brokerCount
	if at.positionReconSummary.LastIssues == nil {
		at.positionReconSummary.LastIssues = []positions.Issue{}
	}
	at.positionReconMu.Unlock()
	at.resolveRestartRecoveryAfterBrokerReconciliation("broker position reconciliation confirmed restored runtime state")
	at.syncPositionReconciliationIncident(summary, nil, nil)
}

func (at *AutoTrader) markPositionReconciliationBlocked(summary string, err error, issues []positions.Issue) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "position reconciliation blocked"
	}
	at.setPositionReconciliationStatus(PositionReconciliationBlocked, summary, false, err, issues)
	at.syncPositionReconciliationIncident(summary, err, issues)
}

func (at *AutoTrader) setPositionReconciliationStatus(status PositionReconciliationStatus, summary string, tradingAllowed bool, err error, issues []positions.Issue) {
	at.positionReconMu.Lock()
	at.positionReconSummary.Status = status
	at.positionReconSummary.Summary = strings.TrimSpace(summary)
	at.positionReconSummary.TradingAllowed = tradingAllowed
	if err != nil {
		at.positionReconSummary.LastError = err.Error()
	}
	if issues != nil {
		at.positionReconSummary.LastIssues = append([]positions.Issue(nil), issues...)
	} else if status == PositionReconciliationBlocked {
		at.positionReconSummary.LastIssues = []positions.Issue{}
	}
	at.positionReconMu.Unlock()
}

func (at *AutoTrader) observePositionReconciliationFailure(summary string, err error, now time.Time) {
	at.positionReconMu.Lock()
	defer at.positionReconMu.Unlock()

	at.positionReconSummary.LastRunAt = now
	at.positionReconSummary.TotalRuns++
	at.positionReconSummary.TotalIncidents++
	at.positionReconSummary.LastIncidentAt = now
	at.positionReconSummary.Summary = strings.TrimSpace(summary)
	at.positionReconSummary.LastIssues = []positions.Issue{}
	if err != nil {
		at.positionReconSummary.LastError = err.Error()
	}
}

func (at *AutoTrader) logPositionReconciliationIncident(result positions.Result) {
	log.Printf(" [%s] Position reconciliation incident: %s", at.name, result.Summary)
	for _, issue := range result.Issues {
		log.Printf(" [%s] Position reconciliation issue [%s] %s %s: %s", at.name, issue.Type, issue.Symbol, issue.Side, issue.Message)
	}
}

func (at *AutoTrader) snapshotLocalPositions() []positions.Snapshot {
	at.positionReconMu.RLock()
	defer at.positionReconMu.RUnlock()

	result := make([]positions.Snapshot, 0, len(at.localPositionSnapshots))
	for _, snapshot := range at.localPositionSnapshots {
		result = append(result, positions.NormalizeSnapshot(snapshot))
	}
	return result
}

func (at *AutoTrader) setLocalPositionSnapshots(snapshots []positions.Snapshot, source string, now time.Time) {
	at.positionReconMu.Lock()
	at.localPositionSnapshots = make(map[string]positions.Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshot = positions.NormalizeSnapshot(snapshot)
		if snapshot.Symbol == "" || snapshot.Side == "" {
			continue
		}
		snapshot.Source = source
		if snapshot.UpdatedAt.IsZero() {
			snapshot.UpdatedAt = now
		}
		at.localPositionSnapshots[positions.Key(snapshot.Symbol, snapshot.Side)] = snapshot
	}
	at.positionReconMu.Unlock()
	at.persistDurableRuntimeState("local_positions_snapshot")
}

func (at *AutoTrader) updateLocalPositionStateFromActions(actions []logger.DecisionAction) {
	if !at.managesPositionReconciliation() || len(actions) == 0 {
		return
	}

	now := time.Now()
	at.positionReconMu.Lock()
	if at.localPositionSnapshots == nil {
		at.localPositionSnapshots = make(map[string]positions.Snapshot)
	}

	for _, action := range actions {
		if !action.Success {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(action.OrderStatus))
		if status == "submitted" || status == "accepted" || status == "acknowledged" || status == "pending" {
			continue
		}

		symbol := strings.ToUpper(strings.TrimSpace(action.Symbol))
		if symbol == "" {
			continue
		}

		switch action.Action {
		case "open_long":
			at.applyLocalPositionOpenLocked(symbol, "long", action.Quantity, action.Price, now)
		case "open_short":
			at.applyLocalPositionOpenLocked(symbol, "short", action.Quantity, action.Price, now)
		case "close_long":
			at.applyLocalPositionCloseLocked(symbol, "long", action.Quantity, now)
		case "close_short":
			at.applyLocalPositionCloseLocked(symbol, "short", action.Quantity, now)
		}
	}
	at.positionReconMu.Unlock()
	at.persistDurableRuntimeState("local_positions_action_update")
}

func (at *AutoTrader) applyLocalPositionOpenLocked(symbol, side string, qty, entryPrice float64, now time.Time) {
	qty = math.Abs(qty)
	if qty <= 0 {
		return
	}
	key := positions.Key(symbol, side)
	current, exists := at.localPositionSnapshots[key]
	if !exists {
		at.localPositionSnapshots[key] = positions.Snapshot{
			Symbol:     symbol,
			Side:       side,
			Quantity:   qty,
			EntryPrice: entryPrice,
			UpdatedAt:  now,
			Source:     "local_execution",
		}
		return
	}
	nextQty := current.Quantity + qty
	nextPrice := current.EntryPrice
	if current.EntryPrice > 0 && entryPrice > 0 && nextQty > 0 {
		nextPrice = ((current.EntryPrice * current.Quantity) + (entryPrice * qty)) / nextQty
	} else if entryPrice > 0 {
		nextPrice = entryPrice
	}
	current.Quantity = nextQty
	current.EntryPrice = nextPrice
	current.UpdatedAt = now
	current.Source = "local_execution"
	at.localPositionSnapshots[key] = current
}

func (at *AutoTrader) applyLocalPositionCloseLocked(symbol, side string, qty float64, now time.Time) {
	key := positions.Key(symbol, side)
	current, exists := at.localPositionSnapshots[key]
	if !exists {
		return
	}
	qty = math.Abs(qty)
	if qty <= 0 || qty >= current.Quantity-0.01 {
		delete(at.localPositionSnapshots, key)
		return
	}
	current.Quantity = math.Max(current.Quantity-qty, 0)
	current.UpdatedAt = now
	current.Source = "local_execution"
	at.localPositionSnapshots[key] = current
}

func positionSnapshotsFromBrokerMaps(raw []map[string]interface{}) []positions.Snapshot {
	result := make([]positions.Snapshot, 0, len(raw))
	for _, pos := range raw {
		symbol := strings.ToUpper(strings.TrimSpace(positionStringValue(pos["symbol"])))
		side := strings.ToLower(strings.TrimSpace(positionStringValue(pos["side"])))
		qty, _ := parseFloat(pos["positionAmt"])
		if qty == 0 {
			qty, _ = parseFloat(pos["quantity"])
		}
		entryPrice, _ := parseFloat(pos["entryPrice"])
		if entryPrice == 0 {
			entryPrice, _ = parseFloat(pos["entry_price"])
		}
		if symbol == "" || side == "" || math.Abs(qty) == 0 {
			continue
		}
		result = append(result, positions.Snapshot{
			Symbol:     symbol,
			Side:       side,
			Quantity:   math.Abs(qty),
			EntryPrice: entryPrice,
		})
	}
	return result
}

func positionStringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}

func errStringValue(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
