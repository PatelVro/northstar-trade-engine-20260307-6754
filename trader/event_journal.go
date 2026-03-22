package trader

import (
	"log"
	"northstar/audit"
	"sort"
	"strings"
	"time"
)

func (at *AutoTrader) currentEventJournalSummary() audit.JournalSummary {
	if at == nil || at.eventJournal == nil {
		return audit.JournalSummary{}
	}
	return at.eventJournal.Summary()
}

func (at *AutoTrader) appendJournalEvent(event audit.JournalEvent) {
	if at == nil || at.eventJournal == nil {
		return
	}
	if err := at.eventJournal.Append(event); err != nil {
		log.Printf(" [%s] Failed to append event journal entry (%s/%s): %v", at.name, event.Family, event.Type, err)
	}
}

func (at *AutoTrader) journalTradingGateDecision(stage string, gate tradingGateDecision) {
	if at == nil || at.eventJournal == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	keyParts := append([]string{
		string(gate.Mode),
		boolString(gate.TradingAllowed),
		boolString(gate.EntriesAllowed),
		boolString(gate.ExitsAllowed),
		boolString(gate.ReduceOnly),
		strings.TrimSpace(gate.BlockReason),
	}, append([]string(nil), gate.BlockingReasons...)...)
	key := strings.Join(keyParts, "|")

	at.eventJournalMu.Lock()
	if key == at.lastJournaledTradingGateKey {
		at.eventJournalMu.Unlock()
		return
	}
	at.lastJournaledTradingGateKey = key
	at.eventJournalMu.Unlock()

	severity := audit.JournalSeverityInfo
	if !gate.TradingAllowed || !gate.ExitsAllowed {
		severity = audit.JournalSeverityCritical
	} else if !gate.EntriesAllowed || gate.ReduceOnly {
		severity = audit.JournalSeverityWarning
	}
	message := strings.TrimSpace(gate.Message)
	if message == "" {
		if gate.TradingAllowed {
			message = "trading gate allows entries and exits"
		} else {
			message = "trading gate blocked trading"
		}
	}
	blockingReasons := append([]string(nil), gate.BlockingReasons...)
	sort.Strings(blockingReasons)
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:      time.Now().UTC(),
		Family:         "safety",
		Type:           "trading_gate_changed",
		Severity:       severity,
		TradingBlocked: !gate.TradingAllowed,
		Message:        message,
		Payload: map[string]interface{}{
			"stage":            stage,
			"mode":             string(gate.Mode),
			"trading_allowed":  gate.TradingAllowed,
			"entries_allowed":  gate.EntriesAllowed,
			"exits_allowed":    gate.ExitsAllowed,
			"reduce_only":      gate.ReduceOnly,
			"block_reason":     strings.TrimSpace(gate.BlockReason),
			"blocking_reasons": blockingReasons,
		},
	})
}

func (at *AutoTrader) journalKillSwitchEvent(eventType string, severity audit.JournalSeverity, summary killSwitchSummary, extra map[string]interface{}) {
	if at == nil {
		return
	}
	payload := map[string]interface{}{
		"active":                 summary.Active,
		"source":                 strings.TrimSpace(summary.Source),
		"file_path":              strings.TrimSpace(summary.FilePath),
		"orders_cancelled":       summary.OrdersCancelled,
		"activation_count":       summary.ActivationCount,
		"last_cancel_error":      strings.TrimSpace(summary.LastCancelError),
		"last_cancel_attempt_at": summary.LastCancelAttemptAt,
	}
	for key, value := range extra {
		payload[key] = value
	}
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:      time.Now().UTC(),
		Family:         "safety",
		Type:           strings.TrimSpace(eventType),
		Severity:       severity,
		TradingBlocked: summary.Active,
		Message:        firstNonEmpty(strings.TrimSpace(summary.Message), "kill switch state changed"),
		Payload:        payload,
	})
}

func (at *AutoTrader) journalRestartRecoveryEvent(eventType string, severity audit.JournalSeverity, summary restartRecoverySummary, extra map[string]interface{}) {
	if at == nil {
		return
	}
	payload := map[string]interface{}{
		"status":                    strings.TrimSpace(summary.Status),
		"state_path":                strings.TrimSpace(summary.StatePath),
		"state_present":             summary.StatePresent,
		"restored":                  summary.Restored,
		"pending_reconciliation":    summary.PendingReconciliation,
		"trading_blocked":           summary.TradingBlocked,
		"partial":                   summary.Partial,
		"corrupt":                   summary.Corrupt,
		"saved_at":                  summary.SavedAt,
		"restored_at":               summary.RestoredAt,
		"last_persisted_at":         summary.LastPersistedAt,
		"restored_execution_count":  summary.RestoredExecutionCount,
		"restored_in_flight_count":  summary.RestoredInFlightCount,
		"restored_active_orders":    summary.RestoredActiveOrders,
		"restored_local_positions":  summary.RestoredLocalPositions,
		"restored_pending_protect":  summary.RestoredPendingProtect,
		"restored_shadow_positions": summary.RestoredShadowPositions,
		"last_load_error":           strings.TrimSpace(summary.LastLoadError),
		"last_save_error":           strings.TrimSpace(summary.LastSaveError),
	}
	for key, value := range extra {
		payload[key] = value
	}
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:      time.Now().UTC(),
		Family:         "restart",
		Type:           strings.TrimSpace(eventType),
		Severity:       severity,
		TradingBlocked: summary.TradingBlocked,
		Message:        firstNonEmpty(strings.TrimSpace(summary.Message), "restart recovery state updated"),
		Payload:        payload,
	})
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
