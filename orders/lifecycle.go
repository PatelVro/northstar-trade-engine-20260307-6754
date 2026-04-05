package orders

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu            sync.RWMutex
	nextID        int64
	ordersByLocal map[string]*Record
	localByBroker map[string]string
	summary       Summary
	observer      Observer
}

func NewStore() *Store {
	return &Store{
		ordersByLocal: make(map[string]*Record),
		localByBroker: make(map[string]string),
	}
}

func (s *Store) SnapshotState() StoreState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := StoreState{
		Version: storeStateVersion,
		NextID:  s.nextID,
		Orders:  make([]Record, 0, len(s.ordersByLocal)),
		Summary: s.summary,
	}
	state.Summary.LastIssues = append([]Issue(nil), s.summary.LastIssues...)

	keys := make([]string, 0, len(s.ordersByLocal))
	for localID := range s.ordersByLocal {
		keys = append(keys, localID)
	}
	sort.Strings(keys)
	for _, localID := range keys {
		record := s.ordersByLocal[localID]
		if record == nil {
			continue
		}
		state.Orders = append(state.Orders, *record)
	}
	return state
}

func (s *Store) RestoreState(state StoreState) error {
	if state.Version == 0 {
		return nil
	}
	if state.Version != storeStateVersion {
		return fmt.Errorf("unsupported order lifecycle state version %d", state.Version)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID = state.NextID
	s.ordersByLocal = make(map[string]*Record, len(state.Orders))
	s.localByBroker = make(map[string]string, len(state.Orders))

	maxID := state.NextID
	for _, record := range state.Orders {
		record.LocalID = strings.TrimSpace(record.LocalID)
		if record.LocalID == "" {
			return errors.New("order lifecycle state contains a record with no local_id")
		}
		record.Intent = normalizeIntent(record.Intent)
		record.Symbol = strings.ToUpper(strings.TrimSpace(record.Symbol))
		record.Side = normalizeSide(record.Side)
		record.PositionSide = normalizePositionSide(record.PositionSide)
		record.TruthAuthority = normalizeTruthAuthority(record.TruthAuthority, record.Status, record.Source, record.BrokerOrderID, record.RawBrokerStatus, record.LastMessage)
		record.TruthConfidence = normalizeTruthConfidence(record.TruthConfidence, record.TruthAuthority, record.LastMessage)
		record.TruthReason = normalizeTruthReason(record.TruthReason, record.TruthAuthority, record.LastMessage)
		// During restore, preserve historical timestamps as-is; only apply
		// normalizeTime (which replaces zero with time.Now) to non-zero values
		// to avoid making old stale orders appear freshly submitted, which would
		// suppress stale-detection and the dedupe window.
		if !record.SubmittedAt.IsZero() {
			record.SubmittedAt = normalizeTime(record.SubmittedAt)
		}
		if !record.UpdatedAt.IsZero() {
			record.UpdatedAt = normalizeTime(record.UpdatedAt)
		}
		if !record.LastSeenAt.IsZero() {
			record.LastSeenAt = normalizeTime(record.LastSeenAt)
		}
		cloned := record
		s.ordersByLocal[record.LocalID] = &cloned
		if brokerID := strings.TrimSpace(record.BrokerOrderID); brokerID != "" {
			s.localByBroker[brokerID] = record.LocalID
		}
		if parsed := parseOrderSequence(record.LocalID); parsed > maxID {
			maxID = parsed
		}
	}

	s.summary = state.Summary
	s.summary.LastIssues = append([]Issue(nil), state.Summary.LastIssues...)
	s.nextID = maxID
	s.refreshSummaryCountsLocked(s.summary.BrokerOpenOrders)
	return nil
}

func (s *Store) SetObserver(observer Observer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observer = observer
}

func (s *Store) RegisterSubmitted(intent Intent, symbol, side, positionSide string, requestedQty float64, submittedAt time.Time) string {
	s.mu.Lock()

	s.nextID++
	localID := fmt.Sprintf("local-%06d", s.nextID)
	record := &Record{
		LocalID:         localID,
		Intent:          normalizeIntent(intent),
		Symbol:          strings.ToUpper(strings.TrimSpace(symbol)),
		Side:            normalizeSide(side),
		PositionSide:    normalizePositionSide(positionSide),
		Status:          StatusSubmitted,
		RequestedQty:    requestedQty,
		RemainingQty:    requestedQty,
		Source:          "local_submission",
		TruthAuthority:  TruthAuthorityLocalPending,
		TruthConfidence: TruthConfidencePending,
		TruthReason:     "submitted locally; awaiting broker acknowledgement",
		LastMessage:     "submitted locally; awaiting broker acknowledgement",
		SubmittedAt:     normalizeTime(submittedAt),
		UpdatedAt:       normalizeTime(submittedAt),
	}
	s.ordersByLocal[localID] = record
	s.refreshSummaryCountsLocked(0)
	observer := s.observer
	event := newEvent(EventSubmitted, *record, StatusUnknown, record.Status, "local order submitted", record.SubmittedAt)
	s.mu.Unlock()
	if observer != nil {
		observer.OnOrderEvent(event)
	}
	return localID
}

func (s *Store) MarkRejected(localID, message string, at time.Time) {
	s.mu.Lock()

	record := s.ordersByLocal[localID]
	if record == nil {
		s.mu.Unlock()
		return
	}
	previousStatus := record.Status
	record.Status = StatusRejected
	record.LastMessage = strings.TrimSpace(message)
	record.TruthAuthority = TruthAuthorityBrokerConfirmed
	record.TruthConfidence = TruthConfidenceConfirmed
	record.TruthReason = firstNonEmpty(strings.TrimSpace(message), "broker rejected order")
	record.NeedsReview = false
	record.UpdatedAt = normalizeTime(at)
	record.LastSeenAt = normalizeTime(at)
	s.refreshSummaryCountsLocked(s.summary.BrokerOpenOrders)
	observer := s.observer
	event := newEvent(EventRejected, *record, previousStatus, record.Status, record.LastMessage, record.UpdatedAt)
	s.mu.Unlock()
	if observer != nil {
		observer.OnOrderEvent(event)
	}
}

func (s *Store) SnapshotSummary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := s.summary
	out.LastIssues = append([]Issue(nil), out.LastIssues...)
	return out
}

func (s *Store) RecordReconciliationError(err error, at time.Time) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary.LastRunAt = normalizeTime(at)
	s.summary.LastError = strings.TrimSpace(err.Error())
}

func (s *Store) ActiveOrders() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Record, 0, len(s.ordersByLocal))
	for _, record := range s.ordersByLocal {
		if record.Status.Terminal() {
			continue
		}
		out = append(out, *record)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SubmittedAt.Before(out[j].SubmittedAt)
	})
	return out
}

func (s *Store) Lookup(localID, brokerOrderID string) *Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if localID = strings.TrimSpace(localID); localID != "" {
		if record := s.ordersByLocal[localID]; record != nil {
			cloned := *record
			return &cloned
		}
	}
	if brokerOrderID = strings.TrimSpace(brokerOrderID); brokerOrderID != "" {
		if mappedLocalID := s.localByBroker[brokerOrderID]; mappedLocalID != "" {
			if record := s.ordersByLocal[mappedLocalID]; record != nil {
				cloned := *record
				return &cloned
			}
		}
	}
	return nil
}

func (s *Store) refreshSummaryCountsLocked(brokerOpenOrders int) {
	s.summary.TrackedOrders = len(s.ordersByLocal)
	s.summary.BrokerOpenOrders = brokerOpenOrders
	active := 0
	pending := 0
	confirmed := 0
	inferred := 0
	unresolved := 0
	for _, record := range s.ordersByLocal {
		if !record.Status.Terminal() {
			active++
		}
		switch normalizeTruthAuthority(record.TruthAuthority, record.Status, record.Source, record.BrokerOrderID, record.RawBrokerStatus, record.LastMessage) {
		case TruthAuthorityLocalPending:
			pending++
		case TruthAuthorityBrokerConfirmed:
			confirmed++
		case TruthAuthorityReconciliationInferred:
			inferred++
		case TruthAuthorityUnresolved:
			unresolved++
		}
	}
	s.summary.ActiveLocalOrders = active
	s.summary.CurrentPendingOrders = pending
	s.summary.CurrentConfirmedOrders = confirmed
	s.summary.CurrentInferredOrders = inferred
	s.summary.CurrentUnresolvedOrders = unresolved
	s.summary.ConfidenceDegraded = inferred > 0 || unresolved > 0
}

func normalizeTime(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now()
	}
	return ts
}

func normalizeIntent(intent Intent) Intent {
	switch intent {
	case IntentEntryLong, IntentEntryShort, IntentExitLong, IntentExitShort, IntentProtectiveStopLong, IntentProtectiveStopShort, IntentProtectiveTargetLong, IntentProtectiveTargetShort:
		return intent
	default:
		return IntentUnknown
	}
}

func normalizeSide(side string) string {
	side = strings.ToUpper(strings.TrimSpace(side))
	switch side {
	case "BUY", "BOT", "B", "LONG":
		return "BUY"
	case "SELL", "SLD", "S", "SHORT":
		return "SELL"
	default:
		return side
	}
}

func normalizePositionSide(side string) string {
	return strings.ToLower(strings.TrimSpace(side))
}

func normalizeTruthAuthority(current TruthAuthority, status Status, source, brokerOrderID, rawBrokerStatus, lastMessage string) TruthAuthority {
	switch current {
	case TruthAuthorityLocalPending, TruthAuthorityBrokerConfirmed, TruthAuthorityReconciliationInferred, TruthAuthorityUnresolved:
		return current
	}
	message := strings.ToLower(strings.TrimSpace(lastMessage))
	switch {
	case strings.Contains(message, "missing at broker active list"):
		if strings.Contains(message, "position evidence indicates") ||
			strings.Contains(message, "position closed") ||
			strings.Contains(message, "remaining position indicates") ||
			strings.Contains(message, "protected position closed") {
			return TruthAuthorityReconciliationInferred
		}
		return TruthAuthorityUnresolved
	case status == StatusSubmitted || status == StatusAccepted:
		return TruthAuthorityLocalPending
	case strings.TrimSpace(rawBrokerStatus) != "" || strings.TrimSpace(brokerOrderID) != "" || strings.EqualFold(strings.TrimSpace(source), "broker_discovered"):
		return TruthAuthorityBrokerConfirmed
	default:
		return TruthAuthorityUnresolved
	}
}

func normalizeTruthConfidence(current TruthConfidence, authority TruthAuthority, lastMessage string) TruthConfidence {
	switch current {
	case TruthConfidencePending, TruthConfidenceConfirmed, TruthConfidenceHigh, TruthConfidenceMedium, TruthConfidenceUnresolved:
		return current
	}
	message := strings.ToLower(strings.TrimSpace(lastMessage))
	switch authority {
	case TruthAuthorityLocalPending:
		return TruthConfidencePending
	case TruthAuthorityBrokerConfirmed:
		return TruthConfidenceConfirmed
	case TruthAuthorityReconciliationInferred:
		if strings.Contains(message, "partial fill") {
			return TruthConfidenceMedium
		}
		return TruthConfidenceHigh
	default:
		return TruthConfidenceUnresolved
	}
}

func normalizeTruthReason(current string, authority TruthAuthority, lastMessage string) string {
	current = strings.TrimSpace(current)
	if current != "" {
		return current
	}
	switch authority {
	case TruthAuthorityLocalPending:
		return "submitted locally; awaiting broker acknowledgement"
	case TruthAuthorityBrokerConfirmed:
		return firstNonEmpty(strings.TrimSpace(lastMessage), "broker-confirmed order state")
	case TruthAuthorityReconciliationInferred:
		return firstNonEmpty(strings.TrimSpace(lastMessage), "reconciled from position evidence")
	default:
		return firstNonEmpty(strings.TrimSpace(lastMessage), "execution truth unresolved pending broker follow-up")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newEvent(eventType EventType, record Record, previousStatus, currentStatus Status, message string, at time.Time) Event {
	return Event{
		EventID:        fmt.Sprintf("%s_%s_%d", eventType, record.LocalID, normalizeTime(at).UnixNano()),
		Timestamp:      normalizeTime(at),
		Type:           eventType,
		Message:        strings.TrimSpace(message),
		PreviousStatus: previousStatus,
		CurrentStatus:  currentStatus,
		Record:         record,
	}
}

func parseOrderSequence(localID string) int64 {
	localID = strings.TrimSpace(localID)
	switch {
	case strings.HasPrefix(localID, "local-"):
		localID = strings.TrimPrefix(localID, "local-")
	case strings.HasPrefix(localID, "broker-"):
		localID = strings.TrimPrefix(localID, "broker-")
	default:
		return 0
	}
	value, err := strconv.ParseInt(localID, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
