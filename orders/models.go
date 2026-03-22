package orders

import "time"

type Status string

const (
	StatusSubmitted       Status = "submitted"
	StatusAccepted        Status = "accepted"
	StatusPartiallyFilled Status = "partially_filled"
	StatusFilled          Status = "filled"
	StatusCancelled       Status = "cancelled"
	StatusRejected        Status = "rejected"
	StatusUnknown         Status = "unknown"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusFilled, StatusCancelled, StatusRejected:
		return true
	default:
		return false
	}
}

type Intent string

const (
	IntentEntryLong             Intent = "entry_long"
	IntentEntryShort            Intent = "entry_short"
	IntentExitLong              Intent = "exit_long"
	IntentExitShort             Intent = "exit_short"
	IntentProtectiveStopLong    Intent = "protective_stop_long"
	IntentProtectiveStopShort   Intent = "protective_stop_short"
	IntentProtectiveTargetLong  Intent = "protective_target_long"
	IntentProtectiveTargetShort Intent = "protective_target_short"
	IntentUnknown               Intent = "unknown"
)

type Record struct {
	LocalID         string    `json:"local_id"`
	BrokerOrderID   string    `json:"broker_order_id,omitempty"`
	Intent          Intent    `json:"intent"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	PositionSide    string    `json:"position_side,omitempty"`
	Status          Status    `json:"status"`
	RawBrokerStatus string    `json:"raw_broker_status,omitempty"`
	RequestedQty    float64   `json:"requested_qty"`
	FilledQty       float64   `json:"filled_qty"`
	RemainingQty    float64   `json:"remaining_qty"`
	AvgFillPrice    float64   `json:"avg_fill_price,omitempty"`
	Source          string    `json:"source"`
	LastMessage     string    `json:"last_message,omitempty"`
	SubmittedAt     time.Time `json:"submitted_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastSeenAt      time.Time `json:"last_seen_at,omitempty"`
}

type BrokerOrder struct {
	OrderID      string    `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	PositionSide string    `json:"position_side,omitempty"`
	Status       Status    `json:"status"`
	RawStatus    string    `json:"raw_status,omitempty"`
	Quantity     float64   `json:"quantity"`
	FilledQty    float64   `json:"filled_qty"`
	RemainingQty float64   `json:"remaining_qty"`
	AvgFillPrice float64   `json:"avg_fill_price,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
}

type PositionSnapshot struct {
	Symbol   string
	Side     string
	Quantity float64
}

type IssueType string

const (
	IssueUnknownBrokerOrder   IssueType = "unknown_broker_order"
	IssueLocalMissingAtBroker IssueType = "local_missing_at_broker"
	IssueFillMismatch         IssueType = "fill_mismatch"
	IssueMatchedPendingOrder  IssueType = "matched_pending_order"
)

type Issue struct {
	Type          IssueType `json:"type"`
	LocalID       string    `json:"local_id,omitempty"`
	BrokerOrderID string    `json:"broker_order_id,omitempty"`
	Message       string    `json:"message"`
	Repaired      bool      `json:"repaired"`
}

type ReconciliationResult struct {
	RanAt                time.Time `json:"ran_at"`
	LocalOrders          int       `json:"local_orders"`
	ActiveLocalOrders    int       `json:"active_local_orders"`
	BrokerOpenOrders     int       `json:"broker_open_orders"`
	Mismatches           int       `json:"mismatches"`
	Repairs              int       `json:"repairs"`
	UnknownBrokerOrders  int       `json:"unknown_broker_orders"`
	LocalMissingAtBroker int       `json:"local_missing_at_broker"`
	FillMismatches       int       `json:"fill_mismatches"`
	ImportedOrders       int       `json:"imported_orders"`
	ResolvedOrders       int       `json:"resolved_orders"`
	Summary              string    `json:"summary"`
	Issues               []Issue   `json:"issues,omitempty"`
}

type Summary struct {
	LastRunAt            time.Time `json:"last_run_at"`
	LastSuccessAt        time.Time `json:"last_success_at"`
	LastError            string    `json:"last_error,omitempty"`
	TotalRuns            int       `json:"total_runs"`
	TotalMismatches      int       `json:"total_mismatches"`
	TotalRepairs         int       `json:"total_repairs"`
	UnknownBrokerOrders  int       `json:"unknown_broker_orders"`
	LocalMissingAtBroker int       `json:"local_missing_at_broker"`
	FillMismatches       int       `json:"fill_mismatches"`
	ImportedOrders       int       `json:"imported_orders"`
	ResolvedOrders       int       `json:"resolved_orders"`
	TrackedOrders        int       `json:"tracked_orders"`
	ActiveLocalOrders    int       `json:"active_local_orders"`
	BrokerOpenOrders     int       `json:"broker_open_orders"`
	LastSummary          string    `json:"last_summary,omitempty"`
	LastIssues           []Issue   `json:"last_issues,omitempty"`
}

const storeStateVersion = 1

type StoreState struct {
	Version int      `json:"version"`
	NextID  int64    `json:"next_id"`
	Orders  []Record `json:"orders"`
	Summary Summary  `json:"summary"`
}

type EventType string

const (
	EventSubmitted       EventType = "submitted"
	EventAccepted        EventType = "accepted"
	EventPartiallyFilled EventType = "partially_filled"
	EventFilled          EventType = "filled"
	EventCancelled       EventType = "cancelled"
	EventRejected        EventType = "rejected"
	EventMatched         EventType = "matched"
	EventImported        EventType = "imported"
	EventUpdated         EventType = "updated"
)

type Event struct {
	EventID        string    `json:"event_id"`
	Timestamp      time.Time `json:"timestamp"`
	Type           EventType `json:"type"`
	Message        string    `json:"message"`
	PreviousStatus Status    `json:"previous_status,omitempty"`
	CurrentStatus  Status    `json:"current_status,omitempty"`
	Record         Record    `json:"record"`
}

type Observer interface {
	OnOrderEvent(event Event)
	OnReconciliation(result ReconciliationResult)
}
