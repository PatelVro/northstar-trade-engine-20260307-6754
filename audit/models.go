package audit

import (
	"northstar/decision"
	"northstar/logger"
	"northstar/orders"
	"time"
)

const recordVersion = 1

type Metadata struct {
	TraderID     string
	TraderName   string
	Mode         string
	Broker       string
	StrategyMode string
}

type DecisionContext struct {
	CurrentTime    string                  `json:"current_time"`
	RuntimeMinutes int                     `json:"runtime_minutes"`
	CallCount      int                     `json:"call_count"`
	Account        decision.AccountInfo    `json:"account"`
	Positions      []decision.PositionInfo `json:"positions"`
	CandidateCoins []string                `json:"candidate_coins"`
	UserPrompt     string                  `json:"user_prompt"`
	CoTTrace       string                  `json:"cot_trace"`
}

type RiskCheck struct {
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	Message          string  `json:"message"`
	ApprovedQuantity float64 `json:"approved_quantity,omitempty"`
	ApprovedNotional float64 `json:"approved_notional,omitempty"`
}

type RiskSummary struct {
	Outcome           string      `json:"outcome"`
	Summary           string      `json:"summary"`
	RequestedQuantity float64     `json:"requested_quantity,omitempty"`
	RequestedNotional float64     `json:"requested_notional,omitempty"`
	ApprovedQuantity  float64     `json:"approved_quantity,omitempty"`
	ApprovedNotional  float64     `json:"approved_notional,omitempty"`
	Checks            []RiskCheck `json:"checks,omitempty"`
}

type ExecutionSummary struct {
	Result          string    `json:"result"`
	Success         bool      `json:"success"`
	ShadowMode      bool      `json:"shadow_mode,omitempty"`
	ShadowStatus    string    `json:"shadow_status,omitempty"`
	Error           string    `json:"error,omitempty"`
	RequestedAction string    `json:"requested_action"`
	RequestedQty    float64   `json:"requested_qty,omitempty"`
	ExecutedQty     float64   `json:"executed_qty,omitempty"`
	Price           float64   `json:"price,omitempty"`
	OrderStatus     string    `json:"order_status,omitempty"`
	LocalOrderID    string    `json:"local_order_id,omitempty"`
	BrokerOrderID   string    `json:"broker_order_id,omitempty"`
	LegacyOrderID   int64     `json:"legacy_order_id,omitempty"`
	ExecutedAt      time.Time `json:"executed_at"`
}

type OrderLifecycle struct {
	LocalOrderID    string    `json:"local_order_id,omitempty"`
	BrokerOrderID   string    `json:"broker_order_id,omitempty"`
	Status          string    `json:"status,omitempty"`
	RawBrokerStatus string    `json:"raw_broker_status,omitempty"`
	RequestedQty    float64   `json:"requested_qty,omitempty"`
	FilledQty       float64   `json:"filled_qty,omitempty"`
	RemainingQty    float64   `json:"remaining_qty,omitempty"`
	AvgFillPrice    float64   `json:"avg_fill_price,omitempty"`
	Source          string    `json:"source,omitempty"`
	LastMessage     string    `json:"last_message,omitempty"`
	TruthAuthority  string    `json:"truth_authority,omitempty"`
	TruthConfidence string    `json:"truth_confidence,omitempty"`
	TruthReason     string    `json:"truth_reason,omitempty"`
	NeedsReview     bool      `json:"needs_review"`
	SubmittedAt     time.Time `json:"submitted_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	LastSeenAt      time.Time `json:"last_seen_at,omitempty"`
	Reconciled      bool      `json:"reconciled"`
}

type PnLSummary struct {
	RealizedPnL          float64 `json:"realized_pnl"`
	FeesUSD              float64 `json:"fees_usd"`
	StrategyEquityBefore float64 `json:"strategy_equity_before,omitempty"`
	StrategyReturnBefore float64 `json:"strategy_return_pct_before,omitempty"`
	TotalPnLBefore       float64 `json:"total_pnl_before,omitempty"`
	UnrealizedPnLBefore  float64 `json:"unrealized_pnl_before,omitempty"`
}

type TradeRecord struct {
	RecordVersion   int              `json:"record_version"`
	TradeID         string           `json:"trade_id"`
	Timestamp       time.Time        `json:"timestamp"`
	TraderID        string           `json:"trader_id"`
	TraderName      string           `json:"trader_name"`
	Mode            string           `json:"mode"`
	Broker          string           `json:"broker"`
	Strategy        string           `json:"strategy"`
	CycleNumber     int              `json:"cycle_number"`
	Symbol          string           `json:"symbol"`
	Action          string           `json:"action"`
	Reason          string           `json:"reason"`
	RiskResult      string           `json:"risk_result"`
	ExecutionResult string           `json:"execution_result"`
	Confidence      int              `json:"confidence,omitempty"`
	DecisionContext DecisionContext  `json:"decision_context"`
	Risk            RiskSummary      `json:"risk"`
	Execution       ExecutionSummary `json:"execution"`
	OrderLifecycle  OrderLifecycle   `json:"order_lifecycle"`
	PnL             PnLSummary       `json:"pnl"`
}

type DecisionRecord struct {
	RecordVersion   int                   `json:"record_version"`
	DecisionID      string                `json:"decision_id"`
	Timestamp       time.Time             `json:"timestamp"`
	TraderID        string                `json:"trader_id"`
	TraderName      string                `json:"trader_name"`
	Mode            string                `json:"mode"`
	Broker          string                `json:"broker"`
	Strategy        string                `json:"strategy"`
	CycleNumber     int                   `json:"cycle_number"`
	Symbol          string                `json:"symbol"`
	Reason          string                `json:"reason"`
	RiskResult      string                `json:"risk_result"`
	ExecutionResult string                `json:"execution_result"`
	Context         DecisionContext       `json:"context"`
	DecisionLog     logger.DecisionRecord `json:"decision_log"`
}

type OrderRecord struct {
	RecordVersion   int                          `json:"record_version"`
	EventID         string                       `json:"event_id"`
	Timestamp       time.Time                    `json:"timestamp"`
	TraderID        string                       `json:"trader_id"`
	TraderName      string                       `json:"trader_name"`
	Mode            string                       `json:"mode"`
	Broker          string                       `json:"broker"`
	Strategy        string                       `json:"strategy"`
	TradeID         string                       `json:"trade_id,omitempty"`
	Symbol          string                       `json:"symbol"`
	Reason          string                       `json:"reason"`
	RiskResult      string                       `json:"risk_result"`
	ExecutionResult string                       `json:"execution_result"`
	EventType       string                       `json:"event_type"`
	Message         string                       `json:"message,omitempty"`
	LocalOrderID    string                       `json:"local_order_id,omitempty"`
	BrokerOrderID   string                       `json:"broker_order_id,omitempty"`
	Lifecycle       OrderLifecycle               `json:"lifecycle"`
	Reconciliation  *orders.ReconciliationResult `json:"reconciliation,omitempty"`
}
