package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"northstar/audit"
	"northstar/execution"
	"northstar/logger"
	"northstar/orders"
	"northstar/positions"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const durableRuntimeStateVersion = 1

type durableRuntimeState struct {
	Version        int                      `json:"version"`
	TraderID       string                   `json:"trader_id"`
	TraderName     string                   `json:"trader_name"`
	Mode           string                   `json:"mode"`
	Broker         string                   `json:"broker"`
	Exchange       string                   `json:"exchange"`
	ShadowMode     bool                     `json:"shadow_mode"`
	SavedAt        time.Time                `json:"saved_at"`
	Execution      execution.ManagerState   `json:"execution"`
	OrderLifecycle *orders.StoreState       `json:"order_lifecycle,omitempty"`
	LocalPositions []positions.Snapshot     `json:"local_positions,omitempty"`
	LatestAccount  *AccountSummary          `json:"latest_account,omitempty"`
	PendingProtect []pendingProtectionState `json:"pending_protections,omitempty"`
	ShadowState    *shadowModeState         `json:"shadow_state,omitempty"`
}

type restartRecoverySummary struct {
	Available               bool
	Status                  string
	StatePath               string
	StatePresent            bool
	Restored                bool
	PendingReconciliation   bool
	TradingBlocked          bool
	Partial                 bool
	Corrupt                 bool
	Message                 string
	SavedAt                 time.Time
	RestoredAt              time.Time
	LastPersistedAt         time.Time
	LastLoadError           string
	LastSaveError           string
	RestoredExecutionCount  int
	RestoredInFlightCount   int
	RestoredActiveOrders    int
	RestoredLocalPositions  int
	RestoredPendingProtect  int
	RestoredShadowPositions int
	ConsecutiveSaveFailures int
}

type orderLifecycleStateCarrier interface {
	SnapshotOrderStoreState() orders.StoreState
	RestoreOrderStoreState(state orders.StoreState) error
}

func (at *AutoTrader) initializeRestartRecoveryState() {
	at.restartRecoveryMu.Lock()
	at.restartRecoveryState = restartRecoverySummary{
		Available:    true,
		Status:       "not_restored",
		StatePath:    at.durableRuntimeStatePath(),
		Message:      "no durable runtime state restored for this session",
		StatePresent: false,
	}
	at.restartRecoveryMu.Unlock()
}

func (at *AutoTrader) durableRuntimeStatePath() string {
	traderID := strings.TrimSpace(at.id)
	if traderID == "" {
		traderID = "default_trader"
	}
	return filepath.Join("output", "state", traderID, "runtime_state.json")
}

func (at *AutoTrader) currentRestartRecoverySummary() restartRecoverySummary {
	at.restartRecoveryMu.RLock()
	defer at.restartRecoveryMu.RUnlock()
	return at.restartRecoveryState
}

func (at *AutoTrader) restartRecoveryTradingAllowed() bool {
	summary := at.currentRestartRecoverySummary()
	return !summary.TradingBlocked
}

func (at *AutoTrader) restoreDurableRuntimeState() {
	if at == nil {
		return
	}

	path := at.durableRuntimeStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			at.restartRecoveryMu.Lock()
			at.restartRecoveryState.StatePath = path
			at.restartRecoveryState.Status = "not_restored"
			at.restartRecoveryState.StatePresent = false
			at.restartRecoveryState.Message = "no persisted runtime state checkpoint found"
			at.restartRecoveryState.TradingBlocked = false
			summary := at.restartRecoveryState
			at.restartRecoveryMu.Unlock()
			at.journalRestartRecoveryEvent("restart_state_absent", audit.JournalSeverityInfo, summary, nil)
			return
		}
		at.markRestartRecoveryLoadFailure(path, fmt.Errorf("read durable runtime state: %w", err))
		return
	}

	var state durableRuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		at.markRestartRecoveryLoadFailure(path, fmt.Errorf("decode durable runtime state: %w", err))
		return
	}
	if err := at.validateDurableRuntimeState(state); err != nil {
		at.markRestartRecoveryLoadFailure(path, err)
		return
	}

	if err := at.executionManager.RestoreState(state.Execution); err != nil {
		at.markRestartRecoveryLoadFailure(path, fmt.Errorf("restore execution manager state: %w", err))
		return
	}

	partial := false
	activeOrders := 0
	if state.OrderLifecycle != nil {
		if carrier, ok := at.trader.(orderLifecycleStateCarrier); ok {
			if err := carrier.RestoreOrderStoreState(*state.OrderLifecycle); err != nil {
				at.markRestartRecoveryLoadFailure(path, fmt.Errorf("restore order lifecycle state: %w", err))
				return
			}
			activeOrders = state.OrderLifecycle.Summary.ActiveLocalOrders
		} else {
			partial = true
		}
	}

	if len(state.LocalPositions) > 0 {
		at.restoreLocalPositionSnapshotsFromState(state.LocalPositions, "durable_restore", state.SavedAt)
		at.markRestartRecoveryPositionBaselinePending(len(state.LocalPositions))
	}
	if state.LatestAccount != nil {
		at.setLatestAccountSummary(state.LatestAccount)
	}
	pendingProtect := 0
	if len(state.PendingProtect) > 0 {
		pendingProtect = at.restorePendingProtections(state.PendingProtect)
	}
	shadowPositions := 0
	if state.ShadowState != nil {
		at.restoreShadowStateFromState(*state.ShadowState)
		shadowPositions = len(state.ShadowState.Positions)
	}

	executionCount := len(state.Execution.Executions)
	inFlightExecutions := countInFlightExecutions(state.Execution)
	localPositions := len(state.LocalPositions)
	needsBrokerReconciliation := at.managesPositionReconciliation() && (inFlightExecutions > 0 || activeOrders > 0 || localPositions > 0 || pendingProtect > 0)

	message := "durable runtime state restored"
	status := "restored"
	blocked := false
	if needsBrokerReconciliation {
		status = "pending_reconciliation"
		blocked = true
		message = "durable runtime state restored; broker reconciliation must confirm orders and positions before trading resumes"
	} else if partial {
		message = "durable runtime state restored partially"
	}

	restoredAt := time.Now().UTC()
	at.restartRecoveryMu.Lock()
	at.restartRecoveryState = restartRecoverySummary{
		Available:               true,
		Status:                  status,
		StatePath:               path,
		StatePresent:            true,
		Restored:                true,
		PendingReconciliation:   needsBrokerReconciliation,
		TradingBlocked:          blocked,
		Partial:                 partial,
		Corrupt:                 false,
		Message:                 message,
		SavedAt:                 state.SavedAt,
		RestoredAt:              restoredAt,
		LastPersistedAt:         state.SavedAt,
		RestoredExecutionCount:  executionCount,
		RestoredInFlightCount:   inFlightExecutions,
		RestoredActiveOrders:    activeOrders,
		RestoredLocalPositions:  localPositions,
		RestoredPendingProtect:  pendingProtect,
		RestoredShadowPositions: shadowPositions,
	}
	summary := at.restartRecoveryState
	at.restartRecoveryMu.Unlock()

	log.Printf(" [%s] %s from %s", at.name, message, path)
	eventType := "restart_state_restored"
	severity := audit.JournalSeverityInfo
	if needsBrokerReconciliation {
		eventType = "restart_state_pending_reconciliation"
		severity = audit.JournalSeverityWarning
	}
	at.journalRestartRecoveryEvent(eventType, severity, summary, nil)
}

func (at *AutoTrader) validateDurableRuntimeState(state durableRuntimeState) error {
	if state.Version != durableRuntimeStateVersion {
		return fmt.Errorf("unsupported durable runtime state version %d", state.Version)
	}
	if strings.TrimSpace(state.TraderID) == "" {
		return fmt.Errorf("durable runtime state missing trader_id")
	}
	if state.TraderID != at.id {
		return fmt.Errorf("durable runtime state trader_id %q does not match current trader %q", state.TraderID, at.id)
	}
	if !strings.EqualFold(strings.TrimSpace(state.Mode), strings.TrimSpace(at.config.Mode)) {
		return fmt.Errorf("durable runtime state mode %q does not match current mode %q", state.Mode, at.config.Mode)
	}
	if !strings.EqualFold(strings.TrimSpace(state.Broker), strings.TrimSpace(at.config.Broker)) {
		return fmt.Errorf("durable runtime state broker %q does not match current broker %q", state.Broker, at.config.Broker)
	}
	if !strings.EqualFold(strings.TrimSpace(state.Exchange), strings.TrimSpace(at.exchange)) {
		return fmt.Errorf("durable runtime state exchange %q does not match current exchange %q", state.Exchange, at.exchange)
	}
	if state.ShadowMode != at.shadowModeEnabled() {
		return fmt.Errorf("durable runtime state shadow_mode=%t does not match current mode shadow=%t", state.ShadowMode, at.shadowModeEnabled())
	}
	return nil
}

func (at *AutoTrader) persistDurableRuntimeState(reason string) {
	if at == nil || at.backtestMode {
		return
	}

	at.restartPersistMu.Lock()
	defer at.restartPersistMu.Unlock()

	state := at.snapshotDurableRuntimeState()
	path := at.durableRuntimeStatePath()
	if err := writeDurableRuntimeState(path, state); err != nil {
		at.markRestartRecoverySaveFailure(path, fmt.Errorf("persist durable runtime state (%s): %w", strings.TrimSpace(reason), err))
		return
	}

	at.restartRecoveryMu.Lock()
	recoveredFromSaveFailure := strings.TrimSpace(at.restartRecoveryState.LastSaveError) != ""
	at.restartRecoveryState.Available = true
	at.restartRecoveryState.StatePath = path
	at.restartRecoveryState.StatePresent = true
	at.restartRecoveryState.LastPersistedAt = state.SavedAt
	at.restartRecoveryState.LastSaveError = ""
	at.restartRecoveryState.ConsecutiveSaveFailures = 0
	if at.restartRecoveryState.Corrupt {
		at.restartRecoveryState.TradingBlocked = true
	} else if recoveredFromSaveFailure && !at.restartRecoveryState.PendingReconciliation {
		at.restartRecoveryState.TradingBlocked = false
		if at.restartRecoveryState.Restored {
			if at.restartRecoveryState.Status == "blocked" {
				at.restartRecoveryState.Status = "restored"
			}
		} else {
			at.restartRecoveryState.Status = "not_restored"
		}
		at.restartRecoveryState.Message = "durable runtime state checkpoint updated successfully"
	}
	at.restartRecoveryMu.Unlock()
}

func (at *AutoTrader) snapshotDurableRuntimeState() durableRuntimeState {
	state := durableRuntimeState{
		Version:    durableRuntimeStateVersion,
		TraderID:   at.id,
		TraderName: at.name,
		Mode:       at.config.Mode,
		Broker:     at.config.Broker,
		Exchange:   at.exchange,
		ShadowMode: at.shadowModeEnabled(),
		SavedAt:    time.Now().UTC(),
	}
	if at.executionManager != nil {
		state.Execution = at.executionManager.SnapshotState()
	}
	if carrier, ok := at.trader.(orderLifecycleStateCarrier); ok {
		snapshot := carrier.SnapshotOrderStoreState()
		if snapshot.Version != 0 {
			state.OrderLifecycle = &snapshot
		}
	}
	state.LocalPositions = at.snapshotLocalPositions()
	if account := at.currentLatestAccountSummary(); account != nil {
		state.LatestAccount = account
	}
	state.PendingProtect = at.snapshotPendingProtections()
	if shadow := at.snapshotShadowStateForPersistence(); shadow != nil {
		state.ShadowState = shadow
	}
	return state
}

func writeDurableRuntimeState(path string, state durableRuntimeState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create runtime state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime state: %w", err)
	}

	// Atomic write: write to a temporary file first, then rename.
	// This prevents corruption if the process crashes mid-write.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write runtime state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Clean up the temp file on rename failure.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename runtime state temp file: %w", err)
	}
	return nil
}

func (at *AutoTrader) markRestartRecoveryLoadFailure(path string, err error) {
	message := strings.TrimSpace(err.Error())
	at.restartRecoveryMu.Lock()
	at.restartRecoveryState.Available = true
	at.restartRecoveryState.Status = "blocked"
	at.restartRecoveryState.StatePath = path
	at.restartRecoveryState.StatePresent = true
	at.restartRecoveryState.Restored = false
	at.restartRecoveryState.PendingReconciliation = false
	at.restartRecoveryState.TradingBlocked = true
	at.restartRecoveryState.Partial = false
	at.restartRecoveryState.Corrupt = true
	at.restartRecoveryState.Message = "durable runtime state could not be restored; trading remains blocked"
	at.restartRecoveryState.LastLoadError = message
	summary := at.restartRecoveryState
	at.restartRecoveryMu.Unlock()
	log.Printf(" [%s] Durable runtime state recovery blocked trading: %s", at.name, message)
	at.journalRestartRecoveryEvent("restart_state_restore_failed", audit.JournalSeverityCritical, summary, map[string]interface{}{
		"error": message,
	})
}

// maxConsecutiveSaveFailuresBeforeBlock is the number of consecutive state
// persistence failures allowed before trading is blocked. A single transient
// I/O error (disk spike, NFS hiccup) should not immediately halt all trading.
const maxConsecutiveSaveFailuresBeforeBlock = 3

func (at *AutoTrader) markRestartRecoverySaveFailure(path string, err error) {
	message := strings.TrimSpace(err.Error())
	at.restartRecoveryMu.Lock()
	at.restartRecoveryState.Available = true
	at.restartRecoveryState.StatePath = path
	at.restartRecoveryState.LastSaveError = message
	at.restartRecoveryState.ConsecutiveSaveFailures++
	consecutiveFailures := at.restartRecoveryState.ConsecutiveSaveFailures

	// Only block trading after several consecutive failures to tolerate transient
	// disk errors. A single write timeout must not halt all position management.
	if consecutiveFailures >= maxConsecutiveSaveFailuresBeforeBlock {
		at.restartRecoveryState.Status = "blocked"
		at.restartRecoveryState.TradingBlocked = true
		at.restartRecoveryState.Message = "durable runtime state persistence failed; trading remains blocked"
	} else {
		at.restartRecoveryState.Message = fmt.Sprintf("durable runtime state persistence failed (%d/%d attempts); will retry", consecutiveFailures, maxConsecutiveSaveFailuresBeforeBlock)
	}
	summary := at.restartRecoveryState
	at.restartRecoveryMu.Unlock()

	if consecutiveFailures >= maxConsecutiveSaveFailuresBeforeBlock {
		log.Printf(" [%s] Durable runtime state persistence blocked trading after %d consecutive failures: %s", at.name, consecutiveFailures, message)
		at.journalRestartRecoveryEvent("restart_state_persist_failed", audit.JournalSeverityCritical, summary, map[string]interface{}{
			"error":               message,
			"consecutive_failures": consecutiveFailures,
		})
	} else {
		log.Printf(" [%s] Durable runtime state persistence failed (attempt %d/%d): %s", at.name, consecutiveFailures, maxConsecutiveSaveFailuresBeforeBlock, message)
	}
}

func (at *AutoTrader) resolveRestartRecoveryAfterBrokerReconciliation(message string) {
	if strings.TrimSpace(message) == "" {
		message = "durable runtime state reconciled with broker truth"
	}

	at.restartRecoveryMu.Lock()
	if !at.restartRecoveryState.PendingReconciliation {
		at.restartRecoveryMu.Unlock()
		return
	}
	at.restartRecoveryState.Status = "reconciled"
	at.restartRecoveryState.PendingReconciliation = false
	at.restartRecoveryState.TradingBlocked = false
	at.restartRecoveryState.Partial = false
	at.restartRecoveryState.Message = strings.TrimSpace(message)
	summary := at.restartRecoveryState
	at.restartRecoveryMu.Unlock()

	at.persistDurableRuntimeState("recovery_reconciled")
	at.journalRestartRecoveryEvent("restart_state_reconciled", audit.JournalSeverityInfo, summary, nil)
}

func (at *AutoTrader) checkRestartRecoveryReadiness() ReadinessCheck {
	summary := at.currentRestartRecoverySummary()
	switch {
	case !summary.Available:
		return readinessPass("restart_recovery", "restart recovery state is not initialized")
	case summary.PendingReconciliation:
		return readinessWarn("restart_recovery", firstNonEmpty(summary.Message, "durable runtime state reconciliation is pending"))
	case summary.TradingBlocked:
		return readinessFail("restart_recovery", firstNonEmpty(summary.Message, summary.LastLoadError, summary.LastSaveError, "durable runtime state recovery is blocking trading"))
	case summary.Restored:
		msg := summary.Message
		if msg == "" {
			msg = "durable runtime state restored"
		}
		if summary.Partial {
			return readinessWarn("restart_recovery", msg)
		}
		return readinessPass("restart_recovery", msg)
	default:
		return readinessPass("restart_recovery", firstNonEmpty(summary.Message, "no durable runtime state restore was required"))
	}
}

func countInFlightExecutions(state execution.ManagerState) int {
	count := 0
	for _, persisted := range state.Executions {
		switch persisted.Result.Status {
		case execution.StatusPending, execution.StatusSubmitted, execution.StatusAcknowledged, execution.StatusPartiallyFilled, execution.StatusStale:
			count++
		}
	}
	return count
}

func (at *AutoTrader) restoreLocalPositionSnapshotsFromState(snapshots []positions.Snapshot, source string, now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
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
}

func (at *AutoTrader) markRestartRecoveryPositionBaselinePending(localCount int) {
	if !at.managesPositionReconciliation() || localCount < 0 {
		return
	}
	at.positionReconMu.Lock()
	at.positionReconSummary.Status = PositionReconciliationPending
	at.positionReconSummary.Summary = fmt.Sprintf("restored %d local position snapshot(s) from durable state; broker baseline reconciliation is pending", localCount)
	at.positionReconSummary.TradingAllowed = false
	at.positionReconSummary.LocalPositions = localCount
	at.positionReconMu.Unlock()
}

func (at *AutoTrader) snapshotShadowStateForPersistence() *shadowModeState {
	at.shadowMu.RLock()
	defer at.shadowMu.RUnlock()

	if !at.shadowState.Available && len(at.shadowState.Positions) == 0 && len(at.shadowState.RecentDecisions) == 0 {
		return nil
	}
	cloned := cloneShadowModeState(at.shadowState)
	return &cloned
}

func (at *AutoTrader) restoreShadowStateFromState(state shadowModeState) {
	at.shadowMu.Lock()
	at.shadowState = cloneShadowModeState(state)
	if at.shadowState.RecentDecisions == nil {
		at.shadowState.RecentDecisions = make([]logger.ShadowExecution, 0, shadowDecisionHistoryLimit)
	}
	if at.shadowState.Positions == nil {
		at.shadowState.Positions = make(map[string]*shadowPosition)
	}
	at.recomputeShadowPortfolioLocked()
	at.shadowMu.Unlock()
}

func cloneShadowModeState(state shadowModeState) shadowModeState {
	cloned := shadowModeState{
		Available:                    state.Available,
		Active:                       state.Active,
		LastDecisionAt:               state.LastDecisionAt,
		LastDecisionSymbol:           state.LastDecisionSymbol,
		LastDecisionAction:           state.LastDecisionAction,
		LastDecisionStatus:           state.LastDecisionStatus,
		TotalDecisions:               state.TotalDecisions,
		WouldTradeCount:              state.WouldTradeCount,
		BlockedCount:                 state.BlockedCount,
		OpenPositions:                state.OpenPositions,
		ClosedTrades:                 state.ClosedTrades,
		HypotheticalRealizedPnL:      state.HypotheticalRealizedPnL,
		HypotheticalUnrealizedPnL:    state.HypotheticalUnrealizedPnL,
		ModeledCommissionUSD:         state.ModeledCommissionUSD,
		ModeledSpreadCostUSD:         state.ModeledSpreadCostUSD,
		ModeledSlippageCostUSD:       state.ModeledSlippageCostUSD,
		ModeledImpactCostUSD:         state.ModeledImpactCostUSD,
		ModeledTotalExecutionCostUSD: state.ModeledTotalExecutionCostUSD,
		LastBlockReason:              state.LastBlockReason,
		RecentDecisions:              append([]logger.ShadowExecution(nil), state.RecentDecisions...),
		Positions:                    make(map[string]*shadowPosition, len(state.Positions)),
	}
	for key, pos := range state.Positions {
		if pos == nil {
			continue
		}
		copyPos := *pos
		cloned.Positions[key] = &copyPos
	}
	return cloned
}
