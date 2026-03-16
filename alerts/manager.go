package alerts

import (
	"log"
	"strings"
	"sync"
	"time"
)

type Category string

const (
	CategoryCritical Category = "critical"
	CategoryWarning  Category = "warning"
	CategoryInfo     Category = "info"
)

type Alert struct {
	ID         string            `json:"id"`
	Key        string            `json:"key"`
	Category   Category          `json:"category"`
	Event      string            `json:"event"`
	Service    string            `json:"service"`
	TraderID   string            `json:"trader_id,omitempty"`
	TraderName string            `json:"trader_name,omitempty"`
	Message    string            `json:"message"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

type Provider interface {
	Name() string
	Send(Alert) error
}

type Summary struct {
	Recent        []Alert `json:"recent"`
	TotalCount    int     `json:"total_count"`
	CriticalCount int     `json:"critical_count"`
	WarningCount  int     `json:"warning_count"`
	InfoCount     int     `json:"info_count"`
	LastAlertAt   string  `json:"last_alert_at"`
}

type Manager struct {
	mu           sync.RWMutex
	providers    []Provider
	recent       []Alert
	lastSent     map[string]time.Time
	recentLimit  int
	dedupeWindow time.Duration
	totalCount   int
	critical     int
	warning      int
	info         int
	lastAlertAt  time.Time
}

func NewManager(providers ...Provider) *Manager {
	filtered := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		filtered = append(filtered, provider)
	}
	return &Manager{
		providers:    filtered,
		recent:       make([]Alert, 0, 20),
		lastSent:     make(map[string]time.Time),
		recentLimit:  20,
		dedupeWindow: 5 * time.Minute,
	}
}

func (m *Manager) Emit(alert Alert) bool {
	if m == nil {
		return false
	}

	now := time.Now().UTC()
	alert.CreatedAt = now
	alert.Service = strings.TrimSpace(alert.Service)
	if alert.Service == "" {
		alert.Service = "northstar"
	}
	alert.Event = strings.TrimSpace(alert.Event)
	if alert.Event == "" {
		alert.Event = "runtime_event"
	}
	alert.Message = strings.TrimSpace(alert.Message)
	if alert.Message == "" {
		alert.Message = alert.Event
	}
	if alert.Key == "" {
		alert.Key = defaultAlertKey(alert)
	}
	alert.ID = now.Format("20060102T150405.000000000Z07:00") + "_" + sanitizeIDPart(alert.Key)

	m.mu.Lock()
	lastAt, duplicate := m.lastSent[alert.Key]
	if duplicate && now.Sub(lastAt) < m.dedupeWindow {
		m.mu.Unlock()
		return false
	}
	m.lastSent[alert.Key] = now
	m.lastAlertAt = now
	m.totalCount++
	switch alert.Category {
	case CategoryCritical:
		m.critical++
	case CategoryWarning:
		m.warning++
	default:
		m.info++
	}
	m.recent = append([]Alert{cloneAlert(alert)}, m.recent...)
	if len(m.recent) > m.recentLimit {
		m.recent = m.recent[:m.recentLimit]
	}
	providers := append([]Provider(nil), m.providers...)
	m.mu.Unlock()

	go m.dispatch(alert, providers)
	return true
}

func (m *Manager) Summary() Summary {
	if m == nil {
		return Summary{Recent: []Alert{}}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	recent := make([]Alert, 0, len(m.recent))
	for _, alert := range m.recent {
		recent = append(recent, cloneAlert(alert))
	}
	return Summary{
		Recent:        recent,
		TotalCount:    m.totalCount,
		CriticalCount: m.critical,
		WarningCount:  m.warning,
		InfoCount:     m.info,
		LastAlertAt:   formatRFC3339(m.lastAlertAt),
	}
}

func (m *Manager) dispatch(alert Alert, providers []Provider) {
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if err := provider.Send(alert); err != nil {
			log.Printf("[alerts] provider=%s event=%s category=%s send failed: %v", provider.Name(), alert.Event, alert.Category, err)
		}
	}
}

func defaultAlertKey(alert Alert) string {
	keyParts := []string{
		string(alert.Category),
		strings.TrimSpace(alert.Event),
		strings.TrimSpace(alert.TraderID),
		strings.TrimSpace(alert.Message),
	}
	return strings.Join(keyParts, "|")
}

func sanitizeIDPart(value string) string {
	replacer := strings.NewReplacer(" ", "_", "|", "_", "/", "_", "\\", "_", ":", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func cloneAlert(alert Alert) Alert {
	cloned := alert
	if len(alert.Metadata) > 0 {
		cloned.Metadata = make(map[string]string, len(alert.Metadata))
		for key, value := range alert.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return cloned
}

func formatRFC3339(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}
