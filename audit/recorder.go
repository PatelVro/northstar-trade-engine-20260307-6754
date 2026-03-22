package audit

import (
	"encoding/json"
	"fmt"
	"northstar/orders"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type tradeLink struct {
	TradeID    string
	Symbol     string
	Reason     string
	RiskResult string
}

type Recorder struct {
	root string
	meta Metadata

	mu           sync.RWMutex
	orderToTrade map[string]tradeLink
}

func NewRecorder(root string, meta Metadata) *Recorder {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("output", "audit")
	}
	return &Recorder{
		root:         root,
		meta:         meta,
		orderToTrade: make(map[string]tradeLink),
	}
}

func (r *Recorder) RecordDecision(record DecisionRecord) error {
	record.RecordVersion = recordVersion
	if record.TraderID == "" {
		record.TraderID = r.meta.TraderID
		record.TraderName = r.meta.TraderName
		record.Mode = r.meta.Mode
		record.Broker = r.meta.Broker
		record.Strategy = r.meta.StrategyMode
	}
	return writeJSON(r.filePath("decisions", record.Timestamp, record.DecisionID), record)
}

func (r *Recorder) RecordTrade(record TradeRecord) error {
	record.RecordVersion = recordVersion
	if record.TraderID == "" {
		record.TraderID = r.meta.TraderID
		record.TraderName = r.meta.TraderName
		record.Mode = r.meta.Mode
		record.Broker = r.meta.Broker
		record.Strategy = r.meta.StrategyMode
	}
	if err := writeJSON(r.filePath("trades", record.Timestamp, record.TradeID), record); err != nil {
		return err
	}

	link := tradeLink{
		TradeID:    record.TradeID,
		Symbol:     record.Symbol,
		Reason:     record.Reason,
		RiskResult: record.RiskResult,
	}

	r.mu.Lock()
	if key := normalizeOrderKey(record.OrderLifecycle.LocalOrderID); key != "" {
		r.orderToTrade[key] = link
	}
	if key := normalizeOrderKey(record.OrderLifecycle.BrokerOrderID); key != "" {
		r.orderToTrade[key] = link
	}
	r.mu.Unlock()

	return nil
}

func (r *Recorder) RecordOrder(record OrderRecord) error {
	record.RecordVersion = recordVersion
	if record.TraderID == "" {
		record.TraderID = r.meta.TraderID
		record.TraderName = r.meta.TraderName
		record.Mode = r.meta.Mode
		record.Broker = r.meta.Broker
		record.Strategy = r.meta.StrategyMode
	}
	return writeJSON(r.filePath("orders", record.Timestamp, record.EventID), record)
}

func (r *Recorder) OnOrderEvent(event orders.Event) {
	link := r.lookupTradeLink(event.Record.LocalID, event.Record.BrokerOrderID)
	record := OrderRecord{
		EventID:         event.EventID,
		Timestamp:       event.Timestamp,
		TradeID:         link.TradeID,
		Symbol:          event.Record.Symbol,
		Reason:          link.Reason,
		RiskResult:      link.RiskResult,
		ExecutionResult: string(event.CurrentStatus),
		EventType:       string(event.Type),
		Message:         event.Message,
		LocalOrderID:    event.Record.LocalID,
		BrokerOrderID:   event.Record.BrokerOrderID,
		Lifecycle:       lifecycleFromRecord(event.Record, true),
	}
	_ = r.RecordOrder(record)
}

func (r *Recorder) OnReconciliation(result orders.ReconciliationResult) {
	if result.Mismatches == 0 && result.Repairs == 0 {
		return
	}
	record := OrderRecord{
		EventID:         makeRecordID("reconciliation", result.RanAt, result.RanAt.UnixNano()),
		Timestamp:       result.RanAt,
		ExecutionResult: "reconciliation",
		EventType:       "reconciliation",
		Message:         result.Summary,
		Reconciliation:  cloneReconciliationResult(result),
	}
	_ = r.RecordOrder(record)
}

func (r *Recorder) ListRecentTrades(limit int) ([]TradeRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	dir := filepath.Join(r.root, "trades", r.meta.TraderID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []TradeRecord{}, nil
		}
		return nil, err
	}

	type candidate struct {
		path string
		time time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path: filepath.Join(dir, entry.Name()),
			time: info.ModTime(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].time.After(candidates[j].time)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]TradeRecord, 0, len(candidates))
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		var record TradeRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *Recorder) lookupTradeLink(ids ...string) tradeLink {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range ids {
		if key := normalizeOrderKey(id); key != "" {
			if link, ok := r.orderToTrade[key]; ok {
				return link
			}
		}
	}
	return tradeLink{}
}

func (r *Recorder) filePath(kind string, ts time.Time, id string) string {
	if ts.IsZero() {
		ts = time.Now()
	}
	safeID := sanitizeName(id)
	if safeID == "" {
		safeID = fmt.Sprintf("%d", ts.UnixNano())
	}
	filename := fmt.Sprintf("%s_%s.json", ts.Format("20060102_150405"), safeID)
	return filepath.Join(r.root, kind, r.meta.TraderID, filename)
}

func writeJSON(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write audit record: %w", err)
	}
	return nil
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, ":", "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func normalizeOrderKey(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func makeRecordID(prefix string, ts time.Time, discriminator interface{}) string {
	return sanitizeName(fmt.Sprintf("%s_%s_%v", prefix, ts.UTC().Format("20060102T150405.000000000"), discriminator))
}

func lifecycleFromRecord(record orders.Record, reconciled bool) OrderLifecycle {
	return OrderLifecycle{
		LocalOrderID:    record.LocalID,
		BrokerOrderID:   record.BrokerOrderID,
		Status:          string(record.Status),
		RawBrokerStatus: record.RawBrokerStatus,
		RequestedQty:    record.RequestedQty,
		FilledQty:       record.FilledQty,
		RemainingQty:    record.RemainingQty,
		AvgFillPrice:    record.AvgFillPrice,
		Source:          record.Source,
		LastMessage:     record.LastMessage,
		TruthAuthority:  string(record.TruthAuthority),
		TruthConfidence: string(record.TruthConfidence),
		TruthReason:     record.TruthReason,
		NeedsReview:     record.NeedsReview,
		SubmittedAt:     record.SubmittedAt,
		UpdatedAt:       record.UpdatedAt,
		LastSeenAt:      record.LastSeenAt,
		Reconciled:      reconciled,
	}
}

func cloneReconciliationResult(result orders.ReconciliationResult) *orders.ReconciliationResult {
	cloned := result
	cloned.Issues = append([]orders.Issue(nil), result.Issues...)
	return &cloned
}
