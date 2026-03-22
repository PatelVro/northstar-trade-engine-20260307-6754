package execution

import (
	"northstar/orders"
	"strings"
	"time"
)

type OrderLookup interface {
	LookupOrderRecord(localID, brokerOrderID string) *orders.Record
}

func (m *Manager) SetOrderLookup(lookup OrderLookup) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookup = lookup
}

func mapBrokerPayloadStatus(payload map[string]interface{}, requestedQty float64) (Status, string, float64, float64) {
	rawStatus := strings.TrimSpace(toString(firstPresent(payload["status"], payload["order_status"])))
	fillQty, _ := toFloat(firstPresent(payload["filled_qty"], payload["filledQty"], payload["fill_qty"]))
	avgFillPrice, _ := toFloat(firstPresent(payload["avg_fill_price"], payload["average_fill_price"], payload["price"]))

	normalized := strings.ToUpper(rawStatus)
	switch normalized {
	case "FILLED":
		return StatusFilled, rawStatus, fillQtyOrRequested(fillQty, requestedQty), avgFillPrice
	case "PARTIALLY_FILLED", "PARTIAL", "PARTIAL_FILL":
		return StatusPartiallyFilled, rawStatus, fillQty, avgFillPrice
	case "CANCELLED", "CANCELED":
		return StatusCancelled, rawStatus, fillQty, avgFillPrice
	case "REJECTED":
		return StatusRejected, rawStatus, fillQty, avgFillPrice
	case "ACCEPTED", "PRESUBMITTED", "PRE_SUBMITTED":
		return StatusAcknowledged, rawStatus, fillQty, avgFillPrice
	case "SUBMITTED", "PENDING", "PENDING_SUBMIT":
		return StatusSubmitted, rawStatus, fillQty, avgFillPrice
	}
	if fillQty > 0 && requestedQty > 0 {
		if fillQty >= requestedQty {
			return StatusFilled, rawStatus, fillQty, avgFillPrice
		}
		return StatusPartiallyFilled, rawStatus, fillQty, avgFillPrice
	}
	return StatusSubmitted, rawStatus, fillQty, avgFillPrice
}

func mapOrderRecordStatus(record *orders.Record) (Status, bool) {
	if record == nil {
		return "", false
	}
	switch record.Status {
	case orders.StatusFilled:
		return StatusFilled, true
	case orders.StatusPartiallyFilled:
		return StatusPartiallyFilled, true
	case orders.StatusCancelled:
		return StatusCancelled, true
	case orders.StatusRejected:
		return StatusRejected, true
	case orders.StatusAccepted:
		return StatusAcknowledged, true
	case orders.StatusSubmitted:
		return StatusSubmitted, true
	default:
		return "", false
	}
}

func fillQtyOrRequested(fillQty, requested float64) float64 {
	if fillQty > 0 {
		return fillQty
	}
	return requested
}

func (m *Manager) sweepLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	for _, tracked := range m.history {
		if tracked == nil {
			continue
		}
		m.refreshTrackedFromLookupLocked(tracked, now)
		if tracked.Result.Status.Terminal() {
			continue
		}
		if tracked.Result.SubmittedAt.IsZero() {
			continue
		}
		if now.Sub(tracked.Result.SubmittedAt) >= m.cfg.StaleAfter {
			tracked.Result.Status = StatusStale
			tracked.Result.Stale = true
			tracked.Result.Success = false
			tracked.Result.Error = "execution unresolved beyond stale threshold"
			tracked.Result.Message = "execution marked stale pending broker-truth follow-up"
			tracked.Result.CompletedAt = now
		}
	}

	for key, intentID := range m.latestByKey {
		tracked := m.historyByID[intentID]
		if tracked == nil {
			delete(m.latestByKey, key)
			continue
		}
		if !tracked.Result.Status.Terminal() {
			continue
		}
		referenceTime := tracked.Result.CompletedAt
		if referenceTime.IsZero() {
			referenceTime = tracked.Intent.CreatedAt
		}
		if referenceTime.IsZero() || now.Sub(referenceTime) <= m.cfg.DedupeWindow {
			continue
		}
		delete(m.latestByKey, key)
	}
}

func (m *Manager) refreshTrackedFromLookupLocked(tracked *trackedExecution, now time.Time) {
	if tracked == nil || m.lookup == nil {
		return
	}
	record := m.lookup.LookupOrderRecord(tracked.Result.LocalOrderID, tracked.Result.BrokerOrderID)
	if record == nil {
		return
	}
	status, ok := mapOrderRecordStatus(record)
	if !ok {
		return
	}
	tracked.Result.ObservedBrokerStatus = record.RawBrokerStatus
	tracked.Result.FillQuantity = record.FilledQty
	tracked.Result.AverageFillPrice = record.AvgFillPrice
	if strings.TrimSpace(tracked.Result.LocalOrderID) == "" {
		tracked.Result.LocalOrderID = record.LocalID
	}
	if strings.TrimSpace(tracked.Result.BrokerOrderID) == "" {
		tracked.Result.BrokerOrderID = record.BrokerOrderID
	}
	switch status {
	case StatusFilled:
		tracked.Result.Status = StatusFilled
		tracked.Result.Success = true
		tracked.Result.Stale = false
		if tracked.Result.CompletedAt.IsZero() {
			tracked.Result.CompletedAt = chooseEventTime(record.UpdatedAt, record.LastSeenAt, now)
		}
	case StatusPartiallyFilled:
		tracked.Result.Status = StatusPartiallyFilled
		tracked.Result.Success = true
		tracked.Result.Stale = false
	case StatusAcknowledged:
		tracked.Result.Status = StatusAcknowledged
		tracked.Result.Success = true
		tracked.Result.Stale = false
	case StatusCancelled:
		tracked.Result.Status = StatusCancelled
		tracked.Result.Success = false
		tracked.Result.Stale = false
		if tracked.Result.CompletedAt.IsZero() {
			tracked.Result.CompletedAt = chooseEventTime(record.UpdatedAt, record.LastSeenAt, now)
		}
	case StatusRejected:
		tracked.Result.Status = StatusRejected
		tracked.Result.Success = false
		tracked.Result.Stale = false
		if tracked.Result.CompletedAt.IsZero() {
			tracked.Result.CompletedAt = chooseEventTime(record.UpdatedAt, record.LastSeenAt, now)
		}
	case StatusSubmitted:
		tracked.Result.Status = StatusSubmitted
		tracked.Result.Success = true
		tracked.Result.Stale = false
	}
}

func chooseEventTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
