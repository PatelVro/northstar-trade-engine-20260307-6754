package orders

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	matchWindow                       = 30 * time.Second
	missingBrokerInferenceGraceWindow = 15 * time.Second
	qtyTolerance                      = 0.02
	issueCap                          = 12
)

type missingBrokerResolution struct {
	Status      Status
	FilledQty   float64
	Authority   TruthAuthority
	Confidence  TruthConfidence
	Message     string
	Repaired    bool
	NeedsReview bool
}

func (s *Store) Reconcile(openOrders []BrokerOrder, positions []PositionSnapshot, now time.Time) ReconciliationResult {
	s.mu.Lock()

	now = normalizeTime(now)
	result := ReconciliationResult{
		RanAt:            now,
		LocalOrders:      len(s.ordersByLocal),
		BrokerOpenOrders: len(openOrders),
		Issues:           make([]Issue, 0, issueCap),
	}
	s.summary.TotalRuns++
	s.summary.LastRunAt = now
	s.summary.LastError = ""
	observer := s.observer
	events := make([]Event, 0, 8)

	brokerByID := make(map[string]BrokerOrder, len(openOrders))
	unmatchedBroker := make([]BrokerOrder, 0, len(openOrders))
	for _, brokerOrder := range openOrders {
		if brokerOrder.OrderID != "" {
			brokerByID[brokerOrder.OrderID] = brokerOrder
		}
		unmatchedBroker = append(unmatchedBroker, brokerOrder)
	}

	usedBroker := make(map[string]bool, len(openOrders))
	resolvedOrders := 0
	importedOrders := 0
	repairs := 0
	mismatches := 0
	unknownBroker := 0
	localMissing := 0
	fillMismatches := 0
	inferredOutcomes := 0
	unresolvedOutcomes := 0

	localRecords := make([]*Record, 0, len(s.ordersByLocal))
	for _, record := range s.ordersByLocal {
		localRecords = append(localRecords, record)
	}
	sort.Slice(localRecords, func(i, j int) bool {
		return localRecords[i].SubmittedAt.Before(localRecords[j].SubmittedAt)
	})

	for _, record := range localRecords {
		// Mark broker orders as used even for terminal records to prevent
		// re-importing orders that were already imported and reached terminal state.
		if record.BrokerOrderID != "" {
			if _, brokerStillPresent := brokerByID[record.BrokerOrderID]; brokerStillPresent {
				usedBroker[record.BrokerOrderID] = true
			}
		}
		if record.Status.Terminal() {
			continue
		}

		var brokerOrder BrokerOrder
		found := false
		if record.BrokerOrderID != "" {
			brokerOrder, found = brokerByID[record.BrokerOrderID]
			if found {
				usedBroker[brokerOrder.OrderID] = true
			}
		}
		if !found {
			brokerOrder, found = findPendingBrokerMatch(record, unmatchedBroker, usedBroker, now)
			if found {
				previousStatus := record.Status
				record.BrokerOrderID = brokerOrder.OrderID
				s.localByBroker[brokerOrder.OrderID] = record.LocalID
				usedBroker[brokerOrder.OrderID] = true
				repairs++
				mismatches++
				events = append(events, newEvent(
					EventMatched,
					*record,
					previousStatus,
					record.Status,
					fmt.Sprintf("matched local order %s to broker order %s", record.LocalID, brokerOrder.OrderID),
					now,
				))
				appendIssue(&result.Issues, Issue{
					Type:          IssueMatchedPendingOrder,
					LocalID:       record.LocalID,
					BrokerOrderID: brokerOrder.OrderID,
					Message:       fmt.Sprintf("matched local order %s to broker order %s", record.LocalID, brokerOrder.OrderID),
					Repaired:      true,
					Authority:     TruthAuthorityBrokerConfirmed,
					Confidence:    TruthConfidenceConfirmed,
				})
			}
		}

		if found {
			previousStatus := record.Status
			changed, fillMismatch := applyBrokerTruth(record, brokerOrder, now)
			if fillMismatch {
				fillMismatches++
				mismatches++
				appendIssue(&result.Issues, Issue{
					Type:          IssueFillMismatch,
					LocalID:       record.LocalID,
					BrokerOrderID: brokerOrder.OrderID,
					Message:       fmt.Sprintf("broker filled quantity %.4f differed from local %.4f; repaired from broker truth", brokerOrder.FilledQty, record.FilledQty),
					Repaired:      true,
					Authority:     TruthAuthorityBrokerConfirmed,
					Confidence:    TruthConfidenceConfirmed,
				})
			}
			if changed {
				repairs++
				events = append(events, newEvent(
					eventTypeForStatus(record.Status, fillMismatch),
					*record,
					previousStatus,
					record.Status,
					record.LastMessage,
					now,
				))
			}
			if record.Status.Terminal() {
				resolvedOrders++
			}
			continue
		}

		resolution := inferMissingBrokerState(record, positions, now)
		if resolution.Status == "" {
			continue
		}
		localMissing++
		mismatches++
		previousStatus := record.Status
		record.Status = resolution.Status
		record.FilledQty = resolution.FilledQty
		record.RemainingQty = math.Max(record.RequestedQty-resolution.FilledQty, 0)
		record.LastMessage = resolution.Message
		record.TruthAuthority = resolution.Authority
		record.TruthConfidence = resolution.Confidence
		record.TruthReason = resolution.Message
		record.NeedsReview = resolution.NeedsReview
		record.UpdatedAt = now
		record.LastSeenAt = now
		switch resolution.Authority {
		case TruthAuthorityReconciliationInferred:
			inferredOutcomes++
		case TruthAuthorityUnresolved:
			unresolvedOutcomes++
		}
		if resolution.Repaired {
			repairs++
		}
		if resolution.Status.Terminal() {
			resolvedOrders++
		}
		events = append(events, newEvent(eventTypeForResolution(*record, resolution, false), *record, previousStatus, record.Status, resolution.Message, now))
		appendIssue(&result.Issues, Issue{
			Type:          IssueLocalMissingAtBroker,
			LocalID:       record.LocalID,
			BrokerOrderID: record.BrokerOrderID,
			Message:       resolution.Message,
			Repaired:      resolution.Repaired,
			Authority:     resolution.Authority,
			Confidence:    resolution.Confidence,
			NeedsReview:   resolution.NeedsReview,
		})
	}

	for _, brokerOrder := range unmatchedBroker {
		if brokerOrder.OrderID != "" && usedBroker[brokerOrder.OrderID] {
			continue
		}
		unknownBroker++
		mismatches++
		repairs++
		importedOrders++
		s.nextID++
		localID := fmt.Sprintf("broker-%06d", s.nextID)
		record := &Record{
			LocalID:         localID,
			BrokerOrderID:   brokerOrder.OrderID,
			Intent:          inferIntentFromBrokerOrder(brokerOrder),
			Symbol:          strings.ToUpper(strings.TrimSpace(brokerOrder.Symbol)),
			Side:            normalizeSide(brokerOrder.Side),
			PositionSide:    normalizePositionSide(brokerOrder.PositionSide),
			Status:          brokerOrder.Status,
			RawBrokerStatus: brokerOrder.RawStatus,
			RequestedQty:    brokerOrder.Quantity,
			FilledQty:       brokerOrder.FilledQty,
			RemainingQty:    brokerOrder.RemainingQty,
			AvgFillPrice:    brokerOrder.AvgFillPrice,
			Source:          "broker_discovered",
			LastMessage:     "discovered active broker order with no local record",
			TruthAuthority:  TruthAuthorityBrokerConfirmed,
			TruthConfidence: TruthConfidenceConfirmed,
			TruthReason:     "discovered active broker order directly from broker",
			SubmittedAt:     now,
			UpdatedAt:       now,
			LastSeenAt:      now,
		}
		s.ordersByLocal[localID] = record
		if brokerOrder.OrderID != "" {
			s.localByBroker[brokerOrder.OrderID] = localID
		}
		events = append(events, newEvent(EventImported, *record, StatusUnknown, record.Status, record.LastMessage, now))
		appendIssue(&result.Issues, Issue{
			Type:          IssueUnknownBrokerOrder,
			LocalID:       localID,
			BrokerOrderID: brokerOrder.OrderID,
			Message:       fmt.Sprintf("imported unknown broker order %s for %s", brokerOrder.OrderID, brokerOrder.Symbol),
			Repaired:      true,
			Authority:     TruthAuthorityBrokerConfirmed,
			Confidence:    TruthConfidenceConfirmed,
		})
	}

	result.Mismatches = mismatches
	result.Repairs = repairs
	result.UnknownBrokerOrders = unknownBroker
	result.LocalMissingAtBroker = localMissing
	result.FillMismatches = fillMismatches
	result.ImportedOrders = importedOrders
	result.ResolvedOrders = resolvedOrders
	result.InferredOutcomes = inferredOutcomes
	result.UnresolvedOutcomes = unresolvedOutcomes
	result.NeedsReview = inferredOutcomes > 0 || unresolvedOutcomes > 0
	result.TradingBlocked = unresolvedOutcomes > 0
	s.refreshSummaryCountsLocked(len(openOrders))
	result.ActiveLocalOrders = s.summary.ActiveLocalOrders
	result.Summary = buildReconciliationSummary(result)

	s.summary.LastSuccessAt = now
	s.summary.TotalMismatches += mismatches
	s.summary.TotalRepairs += repairs
	s.summary.UnknownBrokerOrders += unknownBroker
	s.summary.LocalMissingAtBroker += localMissing
	s.summary.FillMismatches += fillMismatches
	s.summary.ImportedOrders += importedOrders
	s.summary.ResolvedOrders += resolvedOrders
	s.summary.TotalInferredOutcomes += inferredOutcomes
	s.summary.TotalUnresolvedOutcomes += unresolvedOutcomes
	if inferredOutcomes > 0 {
		s.summary.LastInferredAt = now
	}
	if unresolvedOutcomes > 0 {
		s.summary.LastUnresolvedAt = now
	}
	s.summary.LastSummary = result.Summary
	s.summary.LastIssues = append([]Issue(nil), result.Issues...)
	result.LocalOrders = len(s.ordersByLocal)
	s.mu.Unlock()
	if observer != nil {
		for _, event := range events {
			observer.OnOrderEvent(event)
		}
		observer.OnReconciliation(result)
	}
	return result
}

func buildReconciliationSummary(result ReconciliationResult) string {
	if result.Mismatches == 0 {
		return fmt.Sprintf("order reconciliation clean: %d active local / %d broker open", result.ActiveLocalOrders, result.BrokerOpenOrders)
	}
	return fmt.Sprintf(
		"order reconciliation handled %d mismatch(es): local_missing=%d unknown_broker=%d fill_mismatches=%d inferred=%d unresolved=%d",
		result.Mismatches,
		result.LocalMissingAtBroker,
		result.UnknownBrokerOrders,
		result.FillMismatches,
		result.InferredOutcomes,
		result.UnresolvedOutcomes,
	)
}

func appendIssue(issues *[]Issue, issue Issue) {
	if len(*issues) >= issueCap {
		return
	}
	*issues = append(*issues, issue)
}

func findPendingBrokerMatch(record *Record, openOrders []BrokerOrder, used map[string]bool, now time.Time) (BrokerOrder, bool) {
	for _, brokerOrder := range openOrders {
		if brokerOrder.OrderID != "" && used[brokerOrder.OrderID] {
			continue
		}
		if !strings.EqualFold(record.Symbol, brokerOrder.Symbol) {
			continue
		}
		if normalizeSide(record.Side) != normalizeSide(brokerOrder.Side) {
			continue
		}
		if !record.SubmittedAt.IsZero() && brokerOrder.ObservedAt.Sub(record.SubmittedAt) > matchWindow {
			continue
		}
		total := brokerOrder.Quantity
		if total <= 0 {
			total = brokerOrder.FilledQty + brokerOrder.RemainingQty
		}
		if !qtyApproxEqual(record.RequestedQty, total) {
			continue
		}
		return brokerOrder, true
	}
	return BrokerOrder{}, false
}

func applyBrokerTruth(record *Record, brokerOrder BrokerOrder, now time.Time) (bool, bool) {
	changed := false
	fillMismatch := false
	if record.BrokerOrderID != brokerOrder.OrderID && brokerOrder.OrderID != "" {
		record.BrokerOrderID = brokerOrder.OrderID
		changed = true
	}
	if record.RawBrokerStatus != brokerOrder.RawStatus {
		record.RawBrokerStatus = brokerOrder.RawStatus
		changed = true
	}
	if record.Status != brokerOrder.Status {
		record.Status = brokerOrder.Status
		changed = true
	}
	if !qtyApproxEqual(record.FilledQty, brokerOrder.FilledQty) {
		fillMismatch = record.FilledQty > 0 || brokerOrder.FilledQty > 0
		record.FilledQty = brokerOrder.FilledQty
		changed = true
	}
	if !qtyApproxEqual(record.RemainingQty, brokerOrder.RemainingQty) {
		record.RemainingQty = brokerOrder.RemainingQty
		changed = true
	}
	if !qtyApproxEqual(record.RequestedQty, brokerOrder.Quantity) && brokerOrder.Quantity > 0 {
		record.RequestedQty = brokerOrder.Quantity
		changed = true
	}
	if brokerOrder.AvgFillPrice > 0 && math.Abs(record.AvgFillPrice-brokerOrder.AvgFillPrice) > 0.0001 {
		record.AvgFillPrice = brokerOrder.AvgFillPrice
		changed = true
	}
	if record.TruthAuthority != TruthAuthorityBrokerConfirmed {
		record.TruthAuthority = TruthAuthorityBrokerConfirmed
		changed = true
	}
	if record.TruthConfidence != TruthConfidenceConfirmed {
		record.TruthConfidence = TruthConfidenceConfirmed
		changed = true
	}
	reason := firstNonEmpty(fmt.Sprintf("reconciled from broker status %s", brokerOrder.RawStatus), "reconciled from broker truth")
	if record.TruthReason != reason {
		record.TruthReason = reason
		changed = true
	}
	if record.NeedsReview {
		record.NeedsReview = false
		changed = true
	}
	record.LastSeenAt = now
	record.UpdatedAt = now
	record.LastMessage = reason
	return changed, fillMismatch
}

func inferMissingBrokerState(record *Record, positions []PositionSnapshot, now time.Time) missingBrokerResolution {
	if missingBrokerInferenceStillWaiting(record, now) {
		return missingBrokerResolution{}
	}
	positionQty := quantityForIntentPosition(record, positions)
	switch record.Intent {
	case IntentEntryLong, IntentEntryShort:
		if positionQty >= record.RequestedQty*(1-qtyTolerance) && record.RequestedQty > 0 {
			return missingBrokerResolution{
				Status:      StatusFilled,
				FilledQty:   record.RequestedQty,
				Authority:   TruthAuthorityReconciliationInferred,
				Confidence:  TruthConfidenceHigh,
				Message:     fmt.Sprintf("local order %s missing at broker active list; position evidence indicates fill", record.LocalID),
				Repaired:    true,
				NeedsReview: true,
			}
		}
		if positionQty > 0 {
			filled := math.Min(positionQty, record.RequestedQty)
			return missingBrokerResolution{
				Status:      StatusPartiallyFilled,
				FilledQty:   filled,
				Authority:   TruthAuthorityReconciliationInferred,
				Confidence:  TruthConfidenceMedium,
				Message:     fmt.Sprintf("local order %s missing at broker active list; position evidence indicates partial fill %.4f", record.LocalID, filled),
				Repaired:    true,
				NeedsReview: true,
			}
		}
		return missingBrokerResolution{
			Status:      StatusUnknown,
			Authority:   TruthAuthorityUnresolved,
			Confidence:  TruthConfidenceUnresolved,
			Message:     fmt.Sprintf("local order %s missing at broker active list with no fill evidence; execution truth remains unresolved", record.LocalID),
			Repaired:    false,
			NeedsReview: true,
		}
	case IntentExitLong, IntentExitShort:
		if positionQty <= qtyTolerance {
			return missingBrokerResolution{
				Status:      StatusFilled,
				FilledQty:   record.RequestedQty,
				Authority:   TruthAuthorityReconciliationInferred,
				Confidence:  TruthConfidenceHigh,
				Message:     fmt.Sprintf("exit order %s missing at broker active list; position closed", record.LocalID),
				Repaired:    true,
				NeedsReview: true,
			}
		}
		if record.RequestedQty > positionQty+qtyTolerance {
			filled := math.Max(record.RequestedQty-positionQty, 0)
			return missingBrokerResolution{
				Status:      StatusPartiallyFilled,
				FilledQty:   filled,
				Authority:   TruthAuthorityReconciliationInferred,
				Confidence:  TruthConfidenceMedium,
				Message:     fmt.Sprintf("exit order %s missing at broker active list; remaining position indicates partial fill %.4f", record.LocalID, filled),
				Repaired:    true,
				NeedsReview: true,
			}
		}
		return missingBrokerResolution{
			Status:      StatusUnknown,
			Authority:   TruthAuthorityUnresolved,
			Confidence:  TruthConfidenceUnresolved,
			Message:     fmt.Sprintf("exit order %s missing at broker active list with no close evidence; execution truth remains unresolved", record.LocalID),
			Repaired:    false,
			NeedsReview: true,
		}
	case IntentProtectiveStopLong, IntentProtectiveStopShort, IntentProtectiveTargetLong, IntentProtectiveTargetShort:
		if positionQty <= qtyTolerance {
			return missingBrokerResolution{
				Status:      StatusFilled,
				FilledQty:   record.RequestedQty,
				Authority:   TruthAuthorityReconciliationInferred,
				Confidence:  TruthConfidenceMedium,
				Message:     fmt.Sprintf("protective order %s missing at broker active list; protected position closed", record.LocalID),
				Repaired:    true,
				NeedsReview: true,
			}
		}
		return missingBrokerResolution{
			Status:      StatusUnknown,
			Authority:   TruthAuthorityUnresolved,
			Confidence:  TruthConfidenceUnresolved,
			Message:     fmt.Sprintf("protective order %s missing at broker active list while position remains; protection state is unresolved", record.LocalID),
			Repaired:    false,
			NeedsReview: true,
		}
	default:
		return missingBrokerResolution{
			Status:      StatusUnknown,
			Authority:   TruthAuthorityUnresolved,
			Confidence:  TruthConfidenceUnresolved,
			Message:     fmt.Sprintf("order %s missing at broker active list; execution truth remains unresolved", record.LocalID),
			Repaired:    false,
			NeedsReview: true,
		}
	}
}

func eventTypeForResolution(record Record, resolution missingBrokerResolution, fillMismatch bool) EventType {
	if resolution.Authority == TruthAuthorityUnresolved {
		return EventUpdated
	}
	return eventTypeForStatus(record.Status, fillMismatch)
}

func missingBrokerInferenceStillWaiting(record *Record, now time.Time) bool {
	if record == nil {
		return false
	}
	reference := record.LastSeenAt
	if reference.IsZero() {
		reference = record.SubmittedAt
	}
	if reference.IsZero() {
		return false
	}
	switch record.Status {
	case StatusSubmitted, StatusAccepted, StatusPartiallyFilled:
		return now.Sub(reference) <= missingBrokerInferenceGraceWindow
	default:
		return false
	}
}

func quantityForIntentPosition(record *Record, positions []PositionSnapshot) float64 {
	targetSide := record.PositionSide
	if targetSide == "" {
		switch record.Intent {
		case IntentEntryLong, IntentExitLong, IntentProtectiveStopLong, IntentProtectiveTargetLong:
			targetSide = "long"
		case IntentEntryShort, IntentExitShort, IntentProtectiveStopShort, IntentProtectiveTargetShort:
			targetSide = "short"
		}
	}
	for _, pos := range positions {
		if !strings.EqualFold(pos.Symbol, record.Symbol) || !strings.EqualFold(pos.Side, targetSide) {
			continue
		}
		return pos.Quantity
	}
	return 0
}

func qtyApproxEqual(a, b float64) bool {
	return math.Abs(a-b) <= math.Max(qtyTolerance, math.Max(math.Abs(a), math.Abs(b))*qtyTolerance)
}

func inferIntentFromBrokerOrder(order BrokerOrder) Intent {
	positionSide := normalizePositionSide(order.PositionSide)
	side := normalizeSide(order.Side)
	switch {
	case side == "BUY" && positionSide == "short":
		return IntentExitShort
	case side == "SELL" && positionSide == "long":
		return IntentExitLong
	case side == "BUY":
		return IntentEntryLong
	case side == "SELL":
		return IntentEntryShort
	default:
		return IntentUnknown
	}
}

func eventTypeForStatus(status Status, fillMismatch bool) EventType {
	switch status {
	case StatusAccepted:
		return EventAccepted
	case StatusPartiallyFilled:
		return EventPartiallyFilled
	case StatusFilled:
		return EventFilled
	case StatusCancelled:
		return EventCancelled
	case StatusRejected:
		return EventRejected
	default:
		if fillMismatch {
			return EventUpdated
		}
		return EventUpdated
	}
}

func NormalizeBrokerStatus(raw string, filledQty, totalQty, remainingQty float64) Status {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(raw, "reject"):
		return StatusRejected
	case strings.Contains(raw, "cancel"), strings.Contains(raw, "inactive"), strings.Contains(raw, "closed"):
		return StatusCancelled
	case strings.Contains(raw, "fill") && remainingQty <= qtyTolerance:
		return StatusFilled
	case filledQty > qtyTolerance && totalQty > 0 && filledQty < totalQty-qtyTolerance:
		return StatusPartiallyFilled
	case filledQty > qtyTolerance && remainingQty > qtyTolerance:
		return StatusPartiallyFilled
	case strings.Contains(raw, "presubmitted"), strings.Contains(raw, "submitted"), strings.Contains(raw, "pending"):
		return StatusAccepted
	case raw == "":
		return StatusAccepted
	default:
		return StatusUnknown
	}
}
