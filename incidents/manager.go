package incidents

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu             sync.RWMutex
	traderID       string
	maxOpen        int
	maxRecent      int
	nextID         int64
	records        map[string]*Incident
	recentResolved []Incident
}

func NewManager(traderID string) *Manager {
	return &Manager{
		traderID:  traderID,
		maxOpen:   10,
		maxRecent: 10,
		records:   make(map[string]*Incident),
	}
}

func (m *Manager) Observe(signal Signal) (Incident, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := correlationKey(m.traderID, signal)
	now := signal.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	record, exists := m.records[key]
	openedNow := false
	if !exists {
		m.nextID++
		record = &Incident{
			IncidentID:      fmt.Sprintf("inc-%06d", m.nextID),
			TraderID:        normalizeTraderID(m.traderID, signal.TraderID),
			IncidentType:    signal.IncidentType,
			CorrelationKey:  key,
			FirstSeenAt:     now,
			OpenedAt:        now,
			LastSeenAt:      now,
			UpdatedAt:       now,
			OccurrenceCount: 1,
			State:           StateOpen,
			Active:          true,
		}
		m.records[key] = record
		openedNow = true
	} else if !record.Active {
		record.OpenedAt = now
		record.ResolvedAt = nil
		record.AcknowledgedAt = nil
		record.State = StateOpen
		record.Active = true
		record.OccurrenceCount++
		record.LastSeenAt = now
		record.UpdatedAt = now
		openedNow = true
	} else {
		record.OccurrenceCount++
		record.LastSeenAt = now
		record.UpdatedAt = now
	}

	record.IncidentType = signal.IncidentType
	record.Source = strings.TrimSpace(signal.Source)
	record.Summary = strings.TrimSpace(signal.Summary)
	record.CurrentStatus = strings.TrimSpace(signal.CurrentStatus)
	record.Details = cloneDetails(signal.Details)
	record.RecommendedActions = RunbookActions(signal.IncidentType)
	if severityRank(signal.Severity) > severityRank(record.Severity) {
		record.Severity = signal.Severity
	}
	if record.Severity == "" {
		record.Severity = signal.Severity
	}
	if signal.Escalate || (record.Severity == SeverityCritical && record.OccurrenceCount >= 3) {
		record.State = StateEscalated
	} else if record.State == "" || record.State == StateResolved {
		record.State = StateOpen
	}

	return record.Clone(), openedNow
}

func (m *Manager) Resolve(signal Signal, status string) (Incident, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := correlationKey(m.traderID, signal)
	record, exists := m.records[key]
	if !exists || !record.Active {
		return Incident{}, false
	}
	now := signal.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	resolvedAt := now
	record.Active = false
	record.State = StateResolved
	record.ResolvedAt = &resolvedAt
	record.LastSeenAt = now
	record.UpdatedAt = now
	if strings.TrimSpace(status) != "" {
		record.CurrentStatus = strings.TrimSpace(status)
	}
	m.pushResolved(record.Clone())
	return record.Clone(), true
}

func (m *Manager) Acknowledge(key string) (Incident, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.records[strings.TrimSpace(key)]
	if !exists || !record.Active {
		return Incident{}, false
	}
	now := time.Now().UTC()
	record.State = StateAcknowledged
	record.UpdatedAt = now
	record.AcknowledgedAt = &now
	return record.Clone(), true
}

func (m *Manager) Summary() Summary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	open := make([]Incident, 0, len(m.records))
	ackCount := 0
	criticalCount := 0
	var latest *Incident
	var latestCritical *Incident
	for _, record := range m.records {
		if !record.Active {
			continue
		}
		cloned := record.Clone()
		open = append(open, cloned)
		if cloned.State == StateAcknowledged {
			ackCount++
		}
		if cloned.Severity == SeverityCritical {
			criticalCount++
			if latestCritical == nil || cloned.UpdatedAt.After(latestCritical.UpdatedAt) {
				tmp := cloned
				latestCritical = &tmp
			}
		}
		if latest == nil || cloned.UpdatedAt.After(latest.UpdatedAt) {
			tmp := cloned
			latest = &tmp
		}
	}
	sort.Slice(open, func(i, j int) bool {
		if open[i].Severity != open[j].Severity {
			return severityRank(open[i].Severity) > severityRank(open[j].Severity)
		}
		return open[i].UpdatedAt.After(open[j].UpdatedAt)
	})
	if len(open) > m.maxOpen {
		open = open[:m.maxOpen]
	}

	recentResolved := make([]Incident, 0, len(m.recentResolved))
	for _, item := range m.recentResolved {
		recentResolved = append(recentResolved, item.Clone())
	}

	summary := Summary{
		OpenCount:               countActive(m.records),
		AcknowledgedCount:       ackCount,
		CriticalOpenCount:       criticalCount,
		OpenIncidents:           open,
		RecentResolvedIncidents: recentResolved,
	}
	if latest != nil {
		summary.LatestIncidentAt = latest.UpdatedAt
		summary.LatestIncidentSummary = latest.Summary
		summary.LatestIncidentSeverity = latest.Severity
		summary.LatestIncidentRunbookHint = RunbookHint(latest.IncidentType)
	}
	if latestCritical != nil {
		tmp := latestCritical.Clone()
		summary.LatestCriticalIncident = &tmp
	}
	return summary
}

func (m *Manager) pushResolved(incident Incident) {
	m.recentResolved = append([]Incident{incident}, m.recentResolved...)
	if len(m.recentResolved) > m.maxRecent {
		m.recentResolved = m.recentResolved[:m.maxRecent]
	}
}

func correlationKey(defaultTraderID string, signal Signal) string {
	traderID := normalizeTraderID(defaultTraderID, signal.TraderID)
	parts := []string{
		traderID,
		string(signal.IncidentType),
		strings.ToLower(strings.TrimSpace(signal.Source)),
		strings.ToUpper(strings.TrimSpace(signal.Symbol)),
		strings.TrimSpace(signal.ExtraKey),
	}
	return strings.Join(parts, "|")
}

func normalizeTraderID(defaultTraderID, signalTraderID string) string {
	signalTraderID = strings.TrimSpace(signalTraderID)
	if signalTraderID != "" {
		return signalTraderID
	}
	return strings.TrimSpace(defaultTraderID)
}

func cloneDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}

func countActive(records map[string]*Incident) int {
	total := 0
	for _, record := range records {
		if record.Active {
			total++
		}
	}
	return total
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}
