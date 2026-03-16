package execution

import "time"

type Status string

const (
	StatusPending             Status = "pending"
	StatusBlocked             Status = "blocked"
	StatusDuplicateSuppressed Status = "duplicate_suppressed"
	StatusSubmitted           Status = "submitted"
	StatusPartiallyFilled     Status = "partially_filled"
	StatusFilled              Status = "filled"
	StatusCancelled           Status = "cancelled"
	StatusRejected            Status = "rejected"
	StatusStale               Status = "stale"
	StatusFailed              Status = "failed"
)

func (s Status) Terminal() bool {
	switch s {
	case StatusFilled, StatusCancelled, StatusRejected, StatusFailed, StatusBlocked, StatusDuplicateSuppressed, StatusStale:
		return true
	default:
		return false
	}
}

type Intent struct {
	IntentID            string    `json:"intent_id"`
	TraderID            string    `json:"trader_id"`
	TraderName          string    `json:"trader_name,omitempty"`
	Symbol              string    `json:"symbol"`
	Side                string    `json:"side"`
	ActionType          string    `json:"action_type"`
	Quantity            float64   `json:"quantity"`
	OrderType           string    `json:"order_type"`
	LimitPrice          float64   `json:"limit_price,omitempty"`
	StopPrice           float64   `json:"stop_price,omitempty"`
	TIF                 string    `json:"tif,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	DecisionReason      string    `json:"decision_reason,omitempty"`
	DecisionConfidence  int       `json:"decision_confidence,omitempty"`
	DecisionReference   string    `json:"decision_reference,omitempty"`
	RiskReference       string    `json:"risk_reference,omitempty"`
	IncreasesExposure   bool      `json:"increases_exposure"`
	ReduceOnly          bool      `json:"reduce_only"`
	Environment         string    `json:"environment,omitempty"`
	LocalRequestKey     string    `json:"local_request_key,omitempty"`
	RequestedBrokerSide string    `json:"requested_broker_side,omitempty"`
	Leverage            int       `json:"leverage,omitempty"`
}

type Gate struct {
	Mode           string `json:"mode"`
	TradingAllowed bool   `json:"trading_allowed"`
	EntriesAllowed bool   `json:"entries_allowed"`
	ExitsAllowed   bool   `json:"exits_allowed"`
	ReduceOnly     bool   `json:"reduce_only"`
	BlockReason    string `json:"block_reason,omitempty"`
}

type Result struct {
	IntentID             string    `json:"intent_id"`
	Status               Status    `json:"status"`
	SubmittedAt          time.Time `json:"submitted_at,omitempty"`
	CompletedAt          time.Time `json:"completed_at,omitempty"`
	LocalOrderID         string    `json:"local_order_id,omitempty"`
	BrokerOrderID        string    `json:"broker_order_id,omitempty"`
	FillQuantity         float64   `json:"fill_quantity,omitempty"`
	AverageFillPrice     float64   `json:"average_fill_price,omitempty"`
	RetryCount           int       `json:"retry_count"`
	DuplicateSuppressed  bool      `json:"duplicate_suppressed"`
	Stale                bool      `json:"stale"`
	Success              bool      `json:"success"`
	Error                string    `json:"error,omitempty"`
	Message              string    `json:"message,omitempty"`
	Symbol               string    `json:"symbol"`
	ActionType           string    `json:"action_type"`
	DedupeKey            string    `json:"dedupe_key,omitempty"`
	IncreasesExposure    bool      `json:"increases_exposure"`
	ReduceOnly           bool      `json:"reduce_only"`
	ObservedBrokerStatus string    `json:"observed_broker_status,omitempty"`
}

type Summary struct {
	Available                 bool      `json:"available"`
	InFlightCount             int       `json:"in_flight_count"`
	StaleCount                int       `json:"stale_count"`
	LastExecutionAt           time.Time `json:"last_execution_at"`
	LastExecutionSymbol       string    `json:"last_execution_symbol"`
	LastExecutionStatus       Status    `json:"last_execution_status"`
	DuplicateSuppressedCount  int       `json:"duplicate_suppressed_count"`
	BlockedExecutionCount     int       `json:"blocked_execution_count"`
	SubmittedCount            int       `json:"submitted_count"`
	FilledCount               int       `json:"filled_count"`
	RejectedCount             int       `json:"rejected_count"`
	FailedCount               int       `json:"failed_count"`
}

type Config struct {
	DedupeWindow time.Duration
	StaleAfter   time.Duration
	MaxHistory   int
}

type Broker interface {
	OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error)
	OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error)
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)
}
