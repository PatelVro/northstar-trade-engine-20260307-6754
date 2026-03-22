package trader

import (
	"log"
	"math"
	"northstar/decision"
	"northstar/logger"
	"northstar/orders"
	"sort"
	"strings"
	"time"
)

const protectionQtyTolerance = 0.01

type pendingProtectionState struct {
	Symbol                string    `json:"symbol"`
	PositionSide          string    `json:"position_side"`
	EntryLocalOrderID     string    `json:"entry_local_order_id,omitempty"`
	EntryBrokerOrderID    string    `json:"entry_broker_order_id,omitempty"`
	EntryStatus           string    `json:"entry_status,omitempty"`
	RequestedQuantity     float64   `json:"requested_quantity,omitempty"`
	ConfirmedQuantity     float64   `json:"confirmed_quantity,omitempty"`
	StopProtectedQuantity float64   `json:"stop_protected_quantity,omitempty"`
	TargetProtectedQty    float64   `json:"target_protected_quantity,omitempty"`
	StopPrice             float64   `json:"stop_price,omitempty"`
	TakeProfitPrice       float64   `json:"take_profit_price,omitempty"`
	Status                string    `json:"status"`
	Message               string    `json:"message,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type protectionStateSummary struct {
	Available             bool
	PendingCount          int
	ActiveProtectiveCount int
	LastUpdatedAt         time.Time
	Message               string
	Pending               []pendingProtectionState
}

type orderStoreStateSnapshotter interface {
	SnapshotOrderStoreState() orders.StoreState
}

func (at *AutoTrader) initializePendingProtectionState() {
	at.protectionMu.Lock()
	defer at.protectionMu.Unlock()
	at.pendingProtections = make(map[string]pendingProtectionState)
	at.protectionLastUpdatedAt = time.Time{}
}

func pendingProtectionKey(symbol, positionSide string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + ":" + strings.ToLower(strings.TrimSpace(positionSide))
}

func normalizePendingProtectionState(state pendingProtectionState) pendingProtectionState {
	state.Symbol = strings.ToUpper(strings.TrimSpace(state.Symbol))
	state.PositionSide = strings.ToLower(strings.TrimSpace(state.PositionSide))
	state.EntryLocalOrderID = strings.TrimSpace(state.EntryLocalOrderID)
	state.EntryBrokerOrderID = strings.TrimSpace(state.EntryBrokerOrderID)
	state.EntryStatus = strings.ToLower(strings.TrimSpace(state.EntryStatus))
	state.Status = strings.ToLower(strings.TrimSpace(state.Status))
	state.Message = strings.TrimSpace(state.Message)
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now().UTC()
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = state.CreatedAt
	}
	if state.RequestedQuantity < 0 {
		state.RequestedQuantity = math.Abs(state.RequestedQuantity)
	}
	if state.ConfirmedQuantity < 0 {
		state.ConfirmedQuantity = math.Abs(state.ConfirmedQuantity)
	}
	if state.StopProtectedQuantity < 0 {
		state.StopProtectedQuantity = math.Abs(state.StopProtectedQuantity)
	}
	if state.TargetProtectedQty < 0 {
		state.TargetProtectedQty = math.Abs(state.TargetProtectedQty)
	}
	return state
}

func (at *AutoTrader) snapshotPendingProtections() []pendingProtectionState {
	at.protectionMu.RLock()
	defer at.protectionMu.RUnlock()

	out := make([]pendingProtectionState, 0, len(at.pendingProtections))
	for _, state := range at.pendingProtections {
		out = append(out, normalizePendingProtectionState(state))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol == out[j].Symbol {
			return out[i].PositionSide < out[j].PositionSide
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

func (at *AutoTrader) restorePendingProtections(states []pendingProtectionState) int {
	at.protectionMu.Lock()
	defer at.protectionMu.Unlock()

	at.pendingProtections = make(map[string]pendingProtectionState, len(states))
	lastUpdatedAt := time.Time{}
	for _, state := range states {
		state = normalizePendingProtectionState(state)
		if state.Symbol == "" || state.PositionSide == "" {
			continue
		}
		at.pendingProtections[pendingProtectionKey(state.Symbol, state.PositionSide)] = state
		if state.UpdatedAt.After(lastUpdatedAt) {
			lastUpdatedAt = state.UpdatedAt
		}
	}
	at.protectionLastUpdatedAt = lastUpdatedAt
	return len(at.pendingProtections)
}

func (at *AutoTrader) upsertPendingProtection(state pendingProtectionState) {
	state = normalizePendingProtectionState(state)
	if state.Symbol == "" || state.PositionSide == "" {
		return
	}

	var previous *pendingProtectionState
	at.protectionMu.Lock()
	if at.pendingProtections == nil {
		at.pendingProtections = make(map[string]pendingProtectionState)
	}
	state.UpdatedAt = time.Now().UTC()
	if existing, ok := at.pendingProtections[pendingProtectionKey(state.Symbol, state.PositionSide)]; ok {
		cloned := existing
		previous = &cloned
		if state.CreatedAt.IsZero() {
			state.CreatedAt = existing.CreatedAt
		}
		if state.ConfirmedQuantity < existing.ConfirmedQuantity {
			state.ConfirmedQuantity = existing.ConfirmedQuantity
		}
		if state.StopProtectedQuantity < existing.StopProtectedQuantity {
			state.StopProtectedQuantity = existing.StopProtectedQuantity
		}
		if state.TargetProtectedQty < existing.TargetProtectedQty {
			state.TargetProtectedQty = existing.TargetProtectedQty
		}
	}
	at.pendingProtections[pendingProtectionKey(state.Symbol, state.PositionSide)] = state
	at.protectionLastUpdatedAt = state.UpdatedAt
	at.protectionMu.Unlock()
	at.journalProtectionState(state, previous)
	at.persistDurableRuntimeState("pending_protection_update")
}

func (at *AutoTrader) clearPendingProtection(symbol, positionSide, reason string) {
	key := pendingProtectionKey(symbol, positionSide)
	var removed pendingProtectionState
	at.protectionMu.Lock()
	if at.pendingProtections == nil {
		at.protectionMu.Unlock()
		return
	}
	existing, exists := at.pendingProtections[key]
	if !exists {
		at.protectionMu.Unlock()
		return
	}
	removed = normalizePendingProtectionState(existing)
	delete(at.pendingProtections, key)
	at.protectionLastUpdatedAt = time.Now().UTC()
	at.protectionMu.Unlock()
	if strings.TrimSpace(reason) != "" {
		log.Printf(" [%s] Cleared pending protection for %s %s: %s", at.name, strings.ToUpper(strings.TrimSpace(symbol)), strings.ToLower(strings.TrimSpace(positionSide)), strings.TrimSpace(reason))
	}
	at.journalProtectionCleared(removed, reason)
	at.persistDurableRuntimeState("pending_protection_clear")
}

func (at *AutoTrader) activeProtectiveOrderCount() int {
	snapshotter, ok := at.trader.(orderStoreStateSnapshotter)
	if !ok {
		return 0
	}
	state := snapshotter.SnapshotOrderStoreState()
	count := 0
	for _, record := range state.Orders {
		if record.Status.Terminal() {
			continue
		}
		if isProtectiveIntent(record.Intent) {
			count++
		}
	}
	return count
}

func (at *AutoTrader) currentProtectionSummary() protectionStateSummary {
	pending := at.snapshotPendingProtections()
	lastUpdatedAt := time.Time{}
	at.protectionMu.RLock()
	lastUpdatedAt = at.protectionLastUpdatedAt
	at.protectionMu.RUnlock()

	summary := protectionStateSummary{
		Available:             true,
		PendingCount:          len(pending),
		ActiveProtectiveCount: at.activeProtectiveOrderCount(),
		LastUpdatedAt:         lastUpdatedAt,
		Pending:               pending,
	}
	switch {
	case summary.PendingCount > 0:
		summary.Message = "protective orders are pending broker-confirmed fills or submission retries"
	case summary.ActiveProtectiveCount > 0:
		summary.Message = "protective orders are active in broker lifecycle tracking"
	default:
		summary.Message = "no pending protective action"
	}
	return summary
}

func (at *AutoTrader) processPendingProtections(ctx *decision.Context) {
	if at == nil || at.shadowModeEnabled() {
		return
	}
	pending := at.snapshotPendingProtections()
	if len(pending) == 0 {
		return
	}

	lookup, _ := at.trader.(orderLookupSource)
	for _, state := range pending {
		record := (*orders.Record)(nil)
		if lookup != nil {
			record = lookup.LookupOrderRecord(state.EntryLocalOrderID, state.EntryBrokerOrderID)
		}
		if record != nil {
			state.EntryStatus = string(record.Status)
			if state.EntryBrokerOrderID == "" {
				state.EntryBrokerOrderID = record.BrokerOrderID
			}
		}

		positionQty := pendingProtectionPositionQty(state.Symbol, state.PositionSide, ctx)
		if positionQty <= protectionQtyTolerance && state.ConfirmedQuantity > protectionQtyTolerance {
			if record == nil || record.Status.Terminal() {
				at.clearPendingProtection(state.Symbol, state.PositionSide, "position no longer open")
				continue
			}
		}

		switch {
		case record != nil && (record.Status == orders.StatusCancelled || record.Status == orders.StatusRejected):
			at.clearPendingProtection(state.Symbol, state.PositionSide, "entry order did not fill")
			continue
		case record != nil && (record.Status == orders.StatusFilled || record.Status == orders.StatusPartiallyFilled):
			state.ConfirmedQuantity = maxProtectionQty(state.ConfirmedQuantity, confirmedQtyFromRecord(record, state.RequestedQuantity))
			state.Status = "protection_submission_pending"
		case positionQty > protectionQtyTolerance:
			state.ConfirmedQuantity = maxProtectionQty(state.ConfirmedQuantity, positionQty)
			if state.Status == "" || state.Status == "pending_fill" {
				state.Status = "protection_submission_pending"
			}
			if strings.TrimSpace(state.Message) == "" {
				state.Message = "position truth indicates protective orders should be submitted"
			}
		default:
			state.Status = "pending_fill"
			state.Message = firstNonEmpty(
				statusMessageForPendingProtection(record),
				"waiting for broker-confirmed entry fill before protective order submission",
			)
			at.upsertPendingProtection(state)
			continue
		}

		desiredQty := state.ConfirmedQuantity
		if state.RequestedQuantity > 0 && desiredQty > state.RequestedQuantity {
			desiredQty = state.RequestedQuantity
		}
		stopDone, stopErr := at.ensurePendingStopProtection(&state, desiredQty)
		targetDone, targetErr := at.ensurePendingTargetProtection(&state, desiredQty)

		switch {
		case stopErr != nil || targetErr != nil:
			state.Status = "protection_submission_pending"
			state.Message = firstNonEmpty(errorMessage(stopErr), errorMessage(targetErr), "protective order submission is pending retry")
			at.upsertPendingProtection(state)
		case protectionSatisfied(state, desiredQty, record):
			at.clearPendingProtection(state.Symbol, state.PositionSide, "protective orders submitted from broker-confirmed fill state")
		default:
			if stopDone || targetDone {
				state.Status = "protection_submission_pending"
				state.Message = "protective orders partially submitted; awaiting remaining submission or fill confirmation"
			} else {
				state.Status = "pending_fill"
				state.Message = "waiting for additional confirmed fill quantity before protective submission"
			}
			at.upsertPendingProtection(state)
		}
	}
}

func pendingProtectionPositionQty(symbol, positionSide string, ctx *decision.Context) float64 {
	if ctx == nil {
		return 0
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	positionSide = strings.ToLower(strings.TrimSpace(positionSide))
	for _, pos := range ctx.Positions {
		if strings.EqualFold(strings.TrimSpace(pos.Symbol), symbol) && strings.EqualFold(strings.TrimSpace(pos.Side), positionSide) {
			return math.Abs(pos.Quantity)
		}
	}
	return 0
}

func confirmedQtyFromRecord(record *orders.Record, requested float64) float64 {
	if record == nil {
		return 0
	}
	if record.FilledQty > protectionQtyTolerance {
		return math.Abs(record.FilledQty)
	}
	if record.Status == orders.StatusFilled && requested > protectionQtyTolerance {
		return math.Abs(requested)
	}
	if record.Status == orders.StatusFilled && record.RequestedQty > protectionQtyTolerance {
		return math.Abs(record.RequestedQty)
	}
	return 0
}

func (at *AutoTrader) ensurePendingStopProtection(state *pendingProtectionState, desiredQty float64) (bool, error) {
	if state == nil || state.StopPrice <= 0 || desiredQty <= protectionQtyTolerance {
		return false, nil
	}
	if desiredQty-state.StopProtectedQuantity <= protectionQtyTolerance {
		return false, nil
	}
	qtyToProtect := desiredQty - state.StopProtectedQuantity
	if qtyToProtect < 1.0-protectionQtyTolerance {
		return false, nil
	}
	if err := at.trader.SetStopLoss(state.Symbol, strings.ToUpper(state.PositionSide), qtyToProtect, state.StopPrice); err != nil {
		return false, err
	}
	state.StopProtectedQuantity = desiredQty
	return true, nil
}

func (at *AutoTrader) ensurePendingTargetProtection(state *pendingProtectionState, desiredQty float64) (bool, error) {
	if state == nil || state.TakeProfitPrice <= 0 || desiredQty <= protectionQtyTolerance {
		return false, nil
	}
	if desiredQty-state.TargetProtectedQty <= protectionQtyTolerance {
		return false, nil
	}
	qtyToProtect := desiredQty - state.TargetProtectedQty
	if qtyToProtect < 1.0-protectionQtyTolerance {
		return false, nil
	}
	if err := at.trader.SetTakeProfit(state.Symbol, strings.ToUpper(state.PositionSide), qtyToProtect, state.TakeProfitPrice); err != nil {
		return false, err
	}
	state.TargetProtectedQty = desiredQty
	return true, nil
}

func protectionSatisfied(state pendingProtectionState, desiredQty float64, record *orders.Record) bool {
	stopSatisfied := state.StopPrice <= 0 || state.StopProtectedQuantity >= desiredQty-protectionQtyTolerance
	targetSatisfied := state.TakeProfitPrice <= 0 || state.TargetProtectedQty >= desiredQty-protectionQtyTolerance
	if !(stopSatisfied && targetSatisfied) {
		return false
	}
	if record == nil {
		return desiredQty > 0 && state.RequestedQuantity > 0 && desiredQty >= state.RequestedQuantity-protectionQtyTolerance
	}
	return record.Status == orders.StatusFilled && desiredQty > 0
}

func statusMessageForPendingProtection(record *orders.Record) string {
	if record == nil {
		return ""
	}
	switch record.Status {
	case orders.StatusSubmitted:
		return "entry order submitted; waiting for broker acknowledgement or fill"
	case orders.StatusAccepted:
		return "entry order acknowledged; waiting for broker-confirmed fill"
	case orders.StatusPartiallyFilled:
		return "entry order partially filled; protection is being scaled to confirmed size"
	default:
		return strings.TrimSpace(record.LastMessage)
	}
}

func isProtectiveIntent(intent orders.Intent) bool {
	switch intent {
	case orders.IntentProtectiveStopLong, orders.IntentProtectiveStopShort, orders.IntentProtectiveTargetLong, orders.IntentProtectiveTargetShort:
		return true
	default:
		return false
	}
}

func maxProtectionQty(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func protectionPendingStateForEntry(decision *decision.Decision, actionRecordLocalID, actionRecordBrokerID, positionSide string, requestedQty float64, status string) pendingProtectionState {
	state := pendingProtectionState{
		Symbol:             strings.ToUpper(strings.TrimSpace(decision.Symbol)),
		PositionSide:       strings.ToLower(strings.TrimSpace(positionSide)),
		EntryLocalOrderID:  strings.TrimSpace(actionRecordLocalID),
		EntryBrokerOrderID: strings.TrimSpace(actionRecordBrokerID),
		EntryStatus:        strings.ToLower(strings.TrimSpace(status)),
		RequestedQuantity:  math.Abs(requestedQty),
		StopPrice:          decision.StopLoss,
		TakeProfitPrice:    decision.TakeProfit,
		Status:             "pending_fill",
		Message:            "waiting for broker-confirmed entry fill before protective order submission",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	if strings.TrimSpace(status) == string(orders.StatusPartiallyFilled) || strings.TrimSpace(status) == "partially_filled" {
		state.Status = "protection_submission_pending"
		state.Message = "entry partially filled; protective orders will be submitted for confirmed quantity"
	}
	return state
}

func (at *AutoTrader) handleEntryProtection(decision *decision.Decision, actionRecord *logger.DecisionAction, positionSide string, requestedQty float64) {
	if at == nil || decision == nil || actionRecord == nil {
		return
	}
	if at.shadowModeEnabled() {
		log.Printf("   Shadow mode active for %s: protective broker orders were not placed", decision.Symbol)
		return
	}
	if decision.StopLoss <= 0 && decision.TakeProfit <= 0 {
		at.clearPendingProtection(decision.Symbol, positionSide, "no protective orders configured")
		return
	}

	state := protectionPendingStateForEntry(decision, actionRecord.LocalOrderID, actionRecord.BrokerOrderID, positionSide, requestedQty, actionRecord.OrderStatus)
	if !executionStatusHasImmediateFill(actionRecord.OrderStatus) {
		state.Message = firstNonEmpty(
			state.Message,
			"waiting for broker-confirmed entry fill before protective order submission",
		)
		at.upsertPendingProtection(state)
		log.Printf("   Protection pending for %s %s until execution is broker-confirmed filled; current status=%s", decision.Symbol, positionSide, actionRecord.OrderStatus)
		return
	}

	state.ConfirmedQuantity = math.Abs(actionRecord.Quantity)
	if state.ConfirmedQuantity <= protectionQtyTolerance {
		state.ConfirmedQuantity = math.Abs(state.RequestedQuantity)
	}
	state.Status = "protection_submission_pending"
	state.Message = "entry fill confirmed; submitting protective orders"

	stopDone, stopErr := at.ensurePendingStopProtection(&state, state.ConfirmedQuantity)
	targetDone, targetErr := at.ensurePendingTargetProtection(&state, state.ConfirmedQuantity)
	if stopErr != nil || targetErr != nil {
		state.Message = firstNonEmpty(errorMessage(stopErr), errorMessage(targetErr), "protective order submission is pending retry")
		at.upsertPendingProtection(state)
		log.Printf("   Protective order submission for %s %s remains pending: %s", decision.Symbol, positionSide, state.Message)
		return
	}
	if protectionSatisfied(state, state.ConfirmedQuantity, &orders.Record{Status: orders.StatusFilled}) {
		at.clearPendingProtection(decision.Symbol, positionSide, "protective orders submitted after confirmed fill")
		return
	}
	if stopDone || targetDone {
		state.Message = "protective orders partially submitted; awaiting remaining submission"
	} else {
		state.Message = "protective orders are pending confirmed quantity updates"
	}
	at.upsertPendingProtection(state)
}
