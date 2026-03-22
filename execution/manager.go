package execution

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu          sync.Mutex
	cfg         Config
	nextID      int64
	lookup      OrderLookup
	history     []*trackedExecution
	historyByID map[string]*trackedExecution
	latestByKey map[string]string
}

type trackedExecution struct {
	Intent Intent
	Result Result
}

func NewManager(cfg Config) *Manager {
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = 15 * time.Second
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 45 * time.Second
	}
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 250
	}
	return &Manager{
		cfg:         cfg,
		history:     make([]*trackedExecution, 0, cfg.MaxHistory),
		historyByID: make(map[string]*trackedExecution),
		latestByKey: make(map[string]string),
	}
}

func (m *Manager) SnapshotState() ManagerState {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweepLocked(now)

	state := ManagerState{
		Version:    managerStateVersion,
		NextID:     m.nextID,
		Executions: make([]PersistedExecution, 0, len(m.history)),
	}
	for _, tracked := range m.history {
		if tracked == nil {
			continue
		}
		state.Executions = append(state.Executions, PersistedExecution{
			Intent: tracked.Intent,
			Result: tracked.Result,
		})
	}
	return state
}

func (m *Manager) RestoreState(state ManagerState) error {
	if state.Version == 0 {
		return nil
	}
	if state.Version != managerStateVersion {
		return fmt.Errorf("unsupported execution manager state version %d", state.Version)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID = state.NextID
	m.history = make([]*trackedExecution, 0, minInt(len(state.Executions), m.cfg.MaxHistory))
	m.historyByID = make(map[string]*trackedExecution, len(state.Executions))
	m.latestByKey = make(map[string]string)

	maxID := state.NextID
	start := 0
	if len(state.Executions) > m.cfg.MaxHistory {
		start = len(state.Executions) - m.cfg.MaxHistory
	}
	for _, persisted := range state.Executions[start:] {
		intent := persisted.Intent
		result := persisted.Result
		intent.IntentID = strings.TrimSpace(intent.IntentID)
		if intent.IntentID == "" {
			intent.IntentID = strings.TrimSpace(result.IntentID)
		}
		if intent.IntentID == "" {
			return errors.New("execution manager state contains an execution with no intent_id")
		}
		if strings.TrimSpace(result.IntentID) == "" {
			result.IntentID = intent.IntentID
		}
		if strings.TrimSpace(result.DedupeKey) == "" {
			result.DedupeKey = BuildDedupeKey(intent)
		}
		tracked := &trackedExecution{Intent: intent, Result: result}
		m.history = append(m.history, tracked)
		m.historyByID[intent.IntentID] = tracked
		if shouldRegisterLatest(result.Status) {
			m.latestByKey[result.DedupeKey] = intent.IntentID
		}
		if parsed := parseExecutionSequence(intent.IntentID); parsed > maxID {
			maxID = parsed
		}
	}
	m.nextID = maxID
	m.sweepLocked(time.Now().UTC())
	return nil
}

func (m *Manager) Execute(intent Intent, gate Gate, broker Broker) Result {
	now := intent.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
		intent.CreatedAt = now
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweepLocked(now)
	m.nextID++
	if strings.TrimSpace(intent.IntentID) == "" {
		intent.IntentID = fmt.Sprintf("exec-%06d", m.nextID)
	}
	if strings.TrimSpace(intent.OrderType) == "" {
		intent.OrderType = "market"
	}
	if strings.TrimSpace(intent.LocalRequestKey) == "" {
		intent.LocalRequestKey = fmt.Sprintf("%s:%s", strings.ToLower(strings.TrimSpace(intent.ActionType)), strings.ToUpper(strings.TrimSpace(intent.Symbol)))
	}

	result := Result{
		IntentID:          intent.IntentID,
		Status:            StatusPending,
		Symbol:            intent.Symbol,
		ActionType:        intent.ActionType,
		DedupeKey:         BuildDedupeKey(intent),
		IncreasesExposure: intent.IncreasesExposure,
		ReduceOnly:        intent.ReduceOnly,
	}

	if reason := validateIntent(intent, gate); reason != "" {
		result.Status = StatusBlocked
		result.Error = reason
		result.Message = reason
		result.CompletedAt = now
		return m.storeLocked(intent, result)
	}

	if existingID, ok := m.latestByKey[result.DedupeKey]; ok {
		if tracked := m.historyByID[existingID]; tracked != nil {
			m.refreshTrackedFromLookupLocked(tracked, now)
			switch tracked.Result.Status {
			case StatusPending, StatusSubmitted, StatusPartiallyFilled:
				result.Status = StatusDuplicateSuppressed
				result.DuplicateSuppressed = true
				result.Error = "equivalent execution intent already in flight"
				result.Message = "duplicate execution suppressed while equivalent intent is still active"
				result.CompletedAt = now
				return m.storeLocked(intent, result)
			case StatusStale:
				result.Status = StatusBlocked
				result.Stale = true
				result.Error = "equivalent execution intent remains unresolved and stale"
				result.Message = "execution blocked because a matching prior intent is stale and unresolved"
				result.CompletedAt = now
				return m.storeLocked(intent, result)
			default:
				ref := tracked.Result.CompletedAt
				if ref.IsZero() {
					ref = tracked.Intent.CreatedAt
				}
				if !ref.IsZero() && now.Sub(ref) <= m.cfg.DedupeWindow {
					result.Status = StatusDuplicateSuppressed
					result.DuplicateSuppressed = true
					result.Error = "equivalent execution intent was submitted recently"
					result.Message = "duplicate execution suppressed within recent submission window"
					result.CompletedAt = now
					return m.storeLocked(intent, result)
				}
			}
		}
	}

	tracked := &trackedExecution{Intent: intent, Result: result}
	m.appendLocked(tracked)
	if shouldRegisterLatest(result.Status) {
		m.latestByKey[result.DedupeKey] = intent.IntentID
	}

	order, err := submitToBroker(intent, broker)
	if err != nil {
		tracked.Result.Status = mapSubmitError(err)
		tracked.Result.Error = err.Error()
		tracked.Result.Message = err.Error()
		tracked.Result.CompletedAt = now
		tracked.Result.Success = false
		return tracked.Result
	}

	tracked.Result.SubmittedAt = now
	tracked.Result.Success = true
	tracked.Result.LocalOrderID = strings.TrimSpace(toString(firstPresent(order["localOrderId"], order["local_order_id"])))
	tracked.Result.BrokerOrderID = strings.TrimSpace(toString(firstPresent(order["brokerOrderId"], order["broker_order_id"], order["orderId"], order["order_id"])))
	if tracked.Result.BrokerOrderID == "" {
		if legacy, ok := firstPresent(order["orderId"], order["order_id"]).(int64); ok {
			tracked.Result.BrokerOrderID = strconv.FormatInt(legacy, 10)
		}
	}
	status, rawStatus, fillQty, avgFillPrice := mapBrokerPayloadStatus(order, intent.Quantity)
	tracked.Result.Status = status
	tracked.Result.ObservedBrokerStatus = rawStatus
	tracked.Result.FillQuantity = fillQty
	tracked.Result.AverageFillPrice = avgFillPrice
	if status.Terminal() {
		tracked.Result.CompletedAt = now
	}
	m.refreshTrackedFromLookupLocked(tracked, now)
	return tracked.Result
}

func (m *Manager) Summary() Summary {
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweepLocked(now)

	summary := Summary{Available: true}
	for _, tracked := range m.history {
		if tracked == nil {
			continue
		}
		result := tracked.Result
		latestAt := result.SubmittedAt
		if result.CompletedAt.After(latestAt) {
			latestAt = result.CompletedAt
		}
		if tracked.Intent.CreatedAt.After(latestAt) {
			latestAt = tracked.Intent.CreatedAt
		}
		if latestAt.After(summary.LastExecutionAt) || summary.LastExecutionAt.IsZero() {
			summary.LastExecutionAt = latestAt
			summary.LastExecutionSymbol = result.Symbol
			summary.LastExecutionStatus = result.Status
		}
		switch result.Status {
		case StatusPending, StatusSubmitted, StatusPartiallyFilled:
			summary.InFlightCount++
		case StatusStale:
			summary.StaleCount++
		case StatusDuplicateSuppressed:
			summary.DuplicateSuppressedCount++
		case StatusBlocked:
			summary.BlockedExecutionCount++
		case StatusFilled:
			summary.FilledCount++
		case StatusRejected:
			summary.RejectedCount++
		case StatusFailed, StatusCancelled:
			summary.FailedCount++
		}
		if result.Status == StatusSubmitted || result.Status == StatusPartiallyFilled || result.Status == StatusFilled {
			summary.SubmittedCount++
		}
	}
	return summary
}

func (m *Manager) storeLocked(intent Intent, result Result) Result {
	tracked := &trackedExecution{Intent: intent, Result: result}
	m.appendLocked(tracked)
	if shouldRegisterLatest(result.Status) {
		m.latestByKey[result.DedupeKey] = intent.IntentID
	}
	return result
}

func (m *Manager) appendLocked(tracked *trackedExecution) {
	m.history = append(m.history, tracked)
	m.historyByID[tracked.Intent.IntentID] = tracked
	if len(m.history) <= m.cfg.MaxHistory {
		return
	}
	evicted := m.history[0]
	m.history = m.history[1:]
	if evicted != nil {
		delete(m.historyByID, evicted.Intent.IntentID)
		if latestID, ok := m.latestByKey[evicted.Result.DedupeKey]; ok && latestID == evicted.Intent.IntentID {
			delete(m.latestByKey, evicted.Result.DedupeKey)
		}
	}
}

func submitToBroker(intent Intent, broker Broker) (map[string]interface{}, error) {
	if broker == nil {
		return nil, fmt.Errorf("execution broker is not initialized")
	}
	switch strings.ToLower(strings.TrimSpace(intent.ActionType)) {
	case "open_long":
		return broker.OpenLong(intent.Symbol, intent.Quantity, intent.Leverage)
	case "open_short":
		return broker.OpenShort(intent.Symbol, intent.Quantity, intent.Leverage)
	case "close_long":
		return broker.CloseLong(intent.Symbol, intent.Quantity)
	case "close_short":
		return broker.CloseShort(intent.Symbol, intent.Quantity)
	default:
		return nil, fmt.Errorf("unsupported execution action %s", intent.ActionType)
	}
}

func shouldRegisterLatest(status Status) bool {
	switch status {
	case StatusBlocked, StatusDuplicateSuppressed:
		return false
	default:
		return true
	}
}

func mapSubmitError(err error) Status {
	if err == nil {
		return StatusFailed
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "reject"):
		return StatusRejected
	case strings.Contains(msg, "cancel"):
		return StatusCancelled
	default:
		return StatusFailed
	}
}

func firstPresent(values ...interface{}) interface{} {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
			return v
		default:
			return v
		}
	}
	return nil
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func toFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func parseExecutionSequence(intentID string) int64 {
	intentID = strings.TrimSpace(intentID)
	if !strings.HasPrefix(intentID, "exec-") {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(intentID, "exec-"), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
