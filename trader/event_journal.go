package trader

import (
	"log"
	"northstar/audit"
	"sort"
	"strconv"
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
	at.journalBrokerTruthState(stage, at.currentBrokerTruthSummary())
}

func (at *AutoTrader) journalBrokerTruthState(stage string, summary brokerTruthSummary) {
	if at == nil || at.eventJournal == nil {
		return
	}
	stage = strings.TrimSpace(stage)
	keyParts := []string{
		boolString(summary.Required),
		boolString(summary.BrokerManaged),
		boolString(summary.Verified),
		boolString(summary.TradingBlocked),
		boolString(summary.EntriesRestricted),
		boolString(summary.ConfidenceDegraded),
		boolString(summary.AccountVerified),
		boolString(summary.OrdersVerified),
		boolString(summary.PositionsVerified),
		boolString(summary.MarketDataVerified),
		strings.TrimSpace(summary.RestrictionReason),
		strings.TrimSpace(summary.Message),
		strings.TrimSpace(summary.PrimaryIssueLocalID),
		strings.TrimSpace(summary.PrimaryIssueBrokerID),
		string(summary.PrimaryAuthority),
		string(summary.PrimaryConfidence),
		strings.TrimSpace(summary.PrimaryReason),
	}
	key := strings.Join(keyParts, "|")

	at.eventJournalMu.Lock()
	if key == at.lastJournaledBrokerTruthKey {
		at.eventJournalMu.Unlock()
		return
	}
	at.lastJournaledBrokerTruthKey = key
	at.eventJournalMu.Unlock()

	eventType := "broker_truth_verified"
	severity := audit.JournalSeverityInfo
	switch {
	case !summary.Required:
		eventType = "broker_truth_not_required"
	case summary.TradingBlocked:
		eventType = "broker_truth_blocked"
		severity = audit.JournalSeverityCritical
	case summary.EntriesRestricted || summary.ConfidenceDegraded:
		eventType = "broker_truth_restricted"
		severity = audit.JournalSeverityWarning
	}

	payload := map[string]interface{}{
		"stage":                   stage,
		"required":                summary.Required,
		"broker_managed":          summary.BrokerManaged,
		"verified":                summary.Verified,
		"trading_blocked":         summary.TradingBlocked,
		"entries_restricted":      summary.EntriesRestricted,
		"confidence_degraded":     summary.ConfidenceDegraded,
		"account_verified":        summary.AccountVerified,
		"orders_verified":         summary.OrdersVerified,
		"positions_verified":      summary.PositionsVerified,
		"market_data_verified":    summary.MarketDataVerified,
		"inferred_order_count":    summary.InferredOrderCount,
		"unresolved_order_count":  summary.UnresolvedOrderCount,
		"blocking_reasons":        append([]string(nil), summary.BlockingReasons...),
		"restriction_reason":      strings.TrimSpace(summary.RestrictionReason),
		"primary_issue_local_id":  strings.TrimSpace(summary.PrimaryIssueLocalID),
		"primary_issue_broker_id": strings.TrimSpace(summary.PrimaryIssueBrokerID),
		"primary_issue_reason":    strings.TrimSpace(summary.PrimaryReason),
	}
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:       time.Now().UTC(),
		Family:          "safety",
		Type:            eventType,
		Severity:        severity,
		LocalOrderID:    strings.TrimSpace(summary.PrimaryIssueLocalID),
		BrokerOrderID:   strings.TrimSpace(summary.PrimaryIssueBrokerID),
		TruthAuthority:  string(summary.PrimaryAuthority),
		TruthConfidence: string(summary.PrimaryConfidence),
		NeedsReview:     summary.PrimaryNeedsReview,
		TradingBlocked:  summary.TradingBlocked,
		Message:         firstNonEmpty(strings.TrimSpace(summary.Message), "broker truth state updated"),
		Payload:         payload,
	})
}

func (at *AutoTrader) journalProtectionState(state pendingProtectionState, previous *pendingProtectionState) {
	if at == nil || at.eventJournal == nil {
		return
	}
	state = normalizePendingProtectionState(state)
	if state.Symbol == "" || state.PositionSide == "" {
		return
	}
	previousState := pendingProtectionState{}
	if previous != nil {
		previousState = normalizePendingProtectionState(*previous)
	}
	keyState := strings.Join([]string{
		state.Status,
		state.EntryStatus,
		formatFloatKey(state.RequestedQuantity),
		formatFloatKey(state.ConfirmedQuantity),
		formatFloatKey(state.StopProtectedQuantity),
		formatFloatKey(state.TargetProtectedQty),
		formatFloatKey(state.StopPrice),
		formatFloatKey(state.TakeProfitPrice),
		strings.TrimSpace(state.Message),
	}, "|")
	journalKey := pendingProtectionKey(state.Symbol, state.PositionSide)

	at.eventJournalMu.Lock()
	if at.lastJournaledProtectionStateByKey == nil {
		at.lastJournaledProtectionStateByKey = make(map[string]string)
	}
	if at.lastJournaledProtectionStateByKey[journalKey] == keyState {
		at.eventJournalMu.Unlock()
		return
	}
	at.lastJournaledProtectionStateByKey[journalKey] = keyState
	at.eventJournalMu.Unlock()

	eventType := "protection_pending_updated"
	severity := audit.JournalSeverityInfo
	switch {
	case previous == nil || previousState.Symbol == "":
		eventType = "protection_pending_created"
	case state.Status == "protection_submission_pending":
		eventType = "protection_submission_pending"
		severity = audit.JournalSeverityWarning
	case state.Status == "pending_fill":
		eventType = "protection_pending_fill"
	}
	truthAuthority, truthConfidence, needsReview := at.lookupProtectionTruth(state)
	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:       state.UpdatedAt.UTC(),
		Family:          "protection",
		Type:            eventType,
		Severity:        severity,
		Symbol:          strings.TrimSpace(state.Symbol),
		LocalOrderID:    strings.TrimSpace(state.EntryLocalOrderID),
		BrokerOrderID:   strings.TrimSpace(state.EntryBrokerOrderID),
		TruthAuthority:  truthAuthority,
		TruthConfidence: truthConfidence,
		NeedsReview:     needsReview,
		Message:         firstNonEmpty(strings.TrimSpace(state.Message), "pending protection state updated"),
		Payload: map[string]interface{}{
			"position_side":             strings.TrimSpace(state.PositionSide),
			"entry_status":              strings.TrimSpace(state.EntryStatus),
			"status":                    strings.TrimSpace(state.Status),
			"requested_quantity":        state.RequestedQuantity,
			"confirmed_quantity":        state.ConfirmedQuantity,
			"stop_protected_quantity":   state.StopProtectedQuantity,
			"target_protected_quantity": state.TargetProtectedQty,
			"stop_price":                state.StopPrice,
			"take_profit_price":         state.TakeProfitPrice,
		},
	})
}

func (at *AutoTrader) journalProtectionCleared(state pendingProtectionState, reason string) {
	if at == nil || at.eventJournal == nil {
		return
	}
	state = normalizePendingProtectionState(state)
	reason = strings.TrimSpace(reason)
	eventType := "protection_cleared"
	if strings.Contains(strings.ToLower(reason), "submitted") {
		eventType = "protection_confirmed"
	}
	truthAuthority, truthConfidence, needsReview := at.lookupProtectionTruth(state)

	at.eventJournalMu.Lock()
	if at.lastJournaledProtectionStateByKey != nil {
		delete(at.lastJournaledProtectionStateByKey, pendingProtectionKey(state.Symbol, state.PositionSide))
	}
	at.eventJournalMu.Unlock()

	at.appendJournalEvent(audit.JournalEvent{
		Timestamp:       time.Now().UTC(),
		Family:          "protection",
		Type:            eventType,
		Severity:        audit.JournalSeverityInfo,
		Symbol:          strings.TrimSpace(state.Symbol),
		LocalOrderID:    strings.TrimSpace(state.EntryLocalOrderID),
		BrokerOrderID:   strings.TrimSpace(state.EntryBrokerOrderID),
		TruthAuthority:  truthAuthority,
		TruthConfidence: truthConfidence,
		NeedsReview:     needsReview,
		Message:         firstNonEmpty(reason, "pending protection cleared"),
		Payload: map[string]interface{}{
			"position_side":             strings.TrimSpace(state.PositionSide),
			"entry_status":              strings.TrimSpace(state.EntryStatus),
			"status":                    strings.TrimSpace(state.Status),
			"requested_quantity":        state.RequestedQuantity,
			"confirmed_quantity":        state.ConfirmedQuantity,
			"stop_protected_quantity":   state.StopProtectedQuantity,
			"target_protected_quantity": state.TargetProtectedQty,
			"stop_price":                state.StopPrice,
			"take_profit_price":         state.TakeProfitPrice,
		},
	})
}

func (at *AutoTrader) lookupProtectionTruth(state pendingProtectionState) (string, string, bool) {
	if at == nil {
		return "", "", false
	}
	lookup, ok := at.trader.(orderLookupSource)
	if !ok || lookup == nil {
		return "", "", false
	}
	record := lookup.LookupOrderRecord(state.EntryLocalOrderID, state.EntryBrokerOrderID)
	if record == nil {
		return "", "", false
	}
	return string(record.TruthAuthority), string(record.TruthConfidence), record.NeedsReview
}

func formatFloatKey(value float64) string {
	return strings.TrimSpace(strconv.FormatFloat(value, 'f', 4, 64))
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
