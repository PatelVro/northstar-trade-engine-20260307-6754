package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"northstar/orders"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const journalRecordVersion = 1

type JournalSeverity string

const (
	JournalSeverityInfo     JournalSeverity = "info"
	JournalSeverityWarning  JournalSeverity = "warning"
	JournalSeverityCritical JournalSeverity = "critical"
)

type JournalEvent struct {
	RecordVersion   int                    `json:"record_version"`
	EventID         string                 `json:"event_id"`
	Timestamp       time.Time              `json:"timestamp"`
	TraderID        string                 `json:"trader_id"`
	TraderName      string                 `json:"trader_name"`
	Mode            string                 `json:"mode"`
	Broker          string                 `json:"broker"`
	Strategy        string                 `json:"strategy"`
	Family          string                 `json:"family"`
	Type            string                 `json:"type"`
	Severity        JournalSeverity        `json:"severity"`
	Symbol          string                 `json:"symbol,omitempty"`
	LocalOrderID    string                 `json:"local_order_id,omitempty"`
	BrokerOrderID   string                 `json:"broker_order_id,omitempty"`
	TruthAuthority  string                 `json:"truth_authority,omitempty"`
	TruthConfidence string                 `json:"truth_confidence,omitempty"`
	NeedsReview     bool                   `json:"needs_review,omitempty"`
	TradingBlocked  bool                   `json:"trading_blocked,omitempty"`
	Message         string                 `json:"message"`
	Payload         map[string]interface{} `json:"payload,omitempty"`
}

type JournalSummary struct {
	Available     bool
	Path          string
	EventCount    int
	LastEventAt   time.Time
	LastEventType string
	LastSeverity  JournalSeverity
	LastError     string
}

type Journal struct {
	root string
	meta Metadata
	path string

	seq uint64

	mu      sync.RWMutex
	summary JournalSummary
}

func NewJournal(root string, meta Metadata) *Journal {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("output", "audit")
	}
	traderID := strings.TrimSpace(meta.TraderID)
	if traderID == "" {
		traderID = "default_trader"
	}
	journal := &Journal{
		root: root,
		meta: meta,
		path: filepath.Join(root, "journal", traderID, "events.jsonl"),
		summary: JournalSummary{
			Available: true,
			Path:      filepath.Join(root, "journal", traderID, "events.jsonl"),
		},
	}
	journal.loadSummary()
	return journal
}

func (j *Journal) Summary() JournalSummary {
	if j == nil {
		return JournalSummary{}
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.summary
}

func (j *Journal) Append(event JournalEvent) error {
	if j == nil {
		return nil
	}
	event.RecordVersion = journalRecordVersion
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if strings.TrimSpace(event.EventID) == "" {
		event.EventID = makeRecordID(strings.TrimSpace(event.Type), event.Timestamp, atomic.AddUint64(&j.seq, 1))
	}
	if strings.TrimSpace(event.TraderID) == "" {
		event.TraderID = j.meta.TraderID
		event.TraderName = j.meta.TraderName
		event.Mode = j.meta.Mode
		event.Broker = j.meta.Broker
		event.Strategy = j.meta.StrategyMode
	}

	data, err := json.Marshal(event)
	if err != nil {
		j.setLastError(fmt.Errorf("marshal journal event: %w", err))
		return err
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		j.setLastError(fmt.Errorf("create journal directory: %w", err))
		return err
	}

	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		j.setLastError(fmt.Errorf("open journal file: %w", err))
		return err
	}
	defer file.Close()

	if _, err := file.Write(append(data, '\n')); err != nil {
		j.setLastError(fmt.Errorf("append journal event: %w", err))
		return err
	}
	if err := file.Sync(); err != nil {
		j.setLastError(fmt.Errorf("sync journal file: %w", err))
		return err
	}

	j.mu.Lock()
	j.summary.Available = true
	j.summary.Path = j.path
	j.summary.EventCount++
	j.summary.LastEventAt = event.Timestamp
	j.summary.LastEventType = strings.TrimSpace(event.Type)
	j.summary.LastSeverity = event.Severity
	j.summary.LastError = ""
	j.mu.Unlock()
	return nil
}

func (j *Journal) OnOrderEvent(event orders.Event) {
	if j == nil {
		return
	}
	payload := map[string]interface{}{
		"event_type":        string(event.Type),
		"previous_status":   string(event.PreviousStatus),
		"current_status":    string(event.CurrentStatus),
		"intent":            string(event.Record.Intent),
		"side":              event.Record.Side,
		"position_side":     event.Record.PositionSide,
		"requested_qty":     event.Record.RequestedQty,
		"filled_qty":        event.Record.FilledQty,
		"remaining_qty":     event.Record.RemainingQty,
		"avg_fill_price":    event.Record.AvgFillPrice,
		"raw_broker_status": strings.TrimSpace(event.Record.RawBrokerStatus),
		"source":            strings.TrimSpace(event.Record.Source),
		"truth_reason":      strings.TrimSpace(event.Record.TruthReason),
	}
	severity := journalSeverityForOrderEvent(event)
	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = fmt.Sprintf("order lifecycle moved to %s", event.CurrentStatus)
	}
	_ = j.Append(JournalEvent{
		Timestamp:       event.Timestamp,
		Family:          "execution",
		Type:            "order_" + string(event.Type),
		Severity:        severity,
		Symbol:          strings.TrimSpace(event.Record.Symbol),
		LocalOrderID:    strings.TrimSpace(event.Record.LocalID),
		BrokerOrderID:   strings.TrimSpace(event.Record.BrokerOrderID),
		TruthAuthority:  string(event.Record.TruthAuthority),
		TruthConfidence: string(event.Record.TruthConfidence),
		NeedsReview:     event.Record.NeedsReview,
		Message:         message,
		Payload:         payload,
	})
}

func (j *Journal) OnReconciliation(result orders.ReconciliationResult) {
	if j == nil {
		return
	}
	if result.Mismatches == 0 && result.Repairs == 0 && !result.NeedsReview {
		return
	}
	payload := map[string]interface{}{
		"local_orders":            result.LocalOrders,
		"active_local_orders":     result.ActiveLocalOrders,
		"broker_open_orders":      result.BrokerOpenOrders,
		"mismatches":              result.Mismatches,
		"repairs":                 result.Repairs,
		"unknown_broker_orders":   result.UnknownBrokerOrders,
		"local_missing_at_broker": result.LocalMissingAtBroker,
		"fill_mismatches":         result.FillMismatches,
		"imported_orders":         result.ImportedOrders,
		"resolved_orders":         result.ResolvedOrders,
		"inferred_outcomes":       result.InferredOutcomes,
		"unresolved_outcomes":     result.UnresolvedOutcomes,
		"needs_review":            result.NeedsReview,
		"trading_blocked":         result.TradingBlocked,
		"issues":                  result.Issues,
	}
	severity := JournalSeverityWarning
	if result.UnresolvedOutcomes > 0 || result.TradingBlocked {
		severity = JournalSeverityCritical
	}
	_ = j.Append(JournalEvent{
		Timestamp:      result.RanAt,
		Family:         "reconciliation",
		Type:           "order_reconciliation",
		Severity:       severity,
		NeedsReview:    result.NeedsReview,
		TradingBlocked: result.TradingBlocked,
		Message:        journalFirstNonEmpty(strings.TrimSpace(result.Summary), "order reconciliation observed mismatches or repairs"),
		Payload:        payload,
	})
}

func (j *Journal) loadSummary() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.summary.Available = true
	j.summary.Path = j.path

	file, err := os.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			j.summary.LastError = ""
			return
		}
		j.summary.LastError = err.Error()
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	lastLine := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		count++
		lastLine = line
	}
	if err := scanner.Err(); err != nil {
		j.summary.LastError = err.Error()
		return
	}
	j.summary.EventCount = count
	if lastLine == "" {
		return
	}
	var event JournalEvent
	if err := json.Unmarshal([]byte(lastLine), &event); err != nil {
		j.summary.LastError = fmt.Sprintf("decode last journal event: %v", err)
		return
	}
	j.summary.LastEventAt = event.Timestamp
	j.summary.LastEventType = event.Type
	j.summary.LastSeverity = event.Severity
	j.summary.LastError = ""
}

func (j *Journal) setLastError(err error) {
	if j == nil || err == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.summary.Available = true
	j.summary.Path = j.path
	j.summary.LastError = err.Error()
}

func journalSeverityForOrderEvent(event orders.Event) JournalSeverity {
	if event.Record.TruthAuthority == orders.TruthAuthorityUnresolved {
		return JournalSeverityCritical
	}
	if event.Record.NeedsReview || event.Record.TruthAuthority == orders.TruthAuthorityReconciliationInferred {
		return JournalSeverityWarning
	}
	switch event.CurrentStatus {
	case orders.StatusRejected:
		return JournalSeverityWarning
	default:
		return JournalSeverityInfo
	}
}

func journalFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
