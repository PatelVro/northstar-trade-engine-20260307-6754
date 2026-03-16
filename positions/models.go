package positions

import "time"

type Snapshot struct {
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"`
	Quantity   float64   `json:"quantity"`
	EntryPrice float64   `json:"entry_price"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Source     string    `json:"source,omitempty"`
}

type IssueType string

const (
	IssueLocalMissingAtBroker IssueType = "local_missing_at_broker"
	IssueBrokerMissingLocally IssueType = "broker_missing_locally"
	IssueSizeMismatch         IssueType = "size_mismatch"
	IssuePriceMismatch        IssueType = "price_mismatch"
)

type Issue struct {
	Type             IssueType `json:"type"`
	Symbol           string    `json:"symbol"`
	Side             string    `json:"side"`
	LocalQuantity    float64   `json:"local_quantity,omitempty"`
	BrokerQuantity   float64   `json:"broker_quantity,omitempty"`
	LocalEntryPrice  float64   `json:"local_entry_price,omitempty"`
	BrokerEntryPrice float64   `json:"broker_entry_price,omitempty"`
	Message          string    `json:"message"`
}

type Result struct {
	RanAt                time.Time `json:"ran_at"`
	LocalPositions       int       `json:"local_positions"`
	BrokerPositions      int       `json:"broker_positions"`
	Mismatches           int       `json:"mismatches"`
	LocalMissingAtBroker int       `json:"local_missing_at_broker"`
	BrokerMissingLocally int       `json:"broker_missing_locally"`
	SizeMismatches       int       `json:"size_mismatches"`
	PriceMismatches      int       `json:"price_mismatches"`
	Summary              string    `json:"summary"`
	Issues               []Issue   `json:"issues,omitempty"`
}
