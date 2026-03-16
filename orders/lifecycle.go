package orders

import (
	"fmt"
	"sort"
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
		LocalID:      localID,
		Intent:       normalizeIntent(intent),
		Symbol:       strings.ToUpper(strings.TrimSpace(symbol)),
		Side:         normalizeSide(side),
		PositionSide: normalizePositionSide(positionSide),
		Status:       StatusSubmitted,
		RequestedQty: requestedQty,
		RemainingQty: requestedQty,
		Source:       "local_submission",
		SubmittedAt:  normalizeTime(submittedAt),
		UpdatedAt:    normalizeTime(submittedAt),
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
	for _, record := range s.ordersByLocal {
		if !record.Status.Terminal() {
			active++
		}
	}
	s.summary.ActiveLocalOrders = active
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
	case "BUY", "SELL":
		return side
	default:
		return side
	}
}

func normalizePositionSide(side string) string {
	return strings.ToLower(strings.TrimSpace(side))
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
