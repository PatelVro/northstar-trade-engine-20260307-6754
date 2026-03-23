package execution

import (
	"northstar/orders"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// mapBrokerPayloadStatus — broker response status normalization
// ---------------------------------------------------------------------------

func TestMapBrokerPayloadStatus_Filled(t *testing.T) {
	payload := map[string]interface{}{"status": "FILLED", "filled_qty": 10.0, "price": 150.25}
	status, raw, fillQty, avgPrice := mapBrokerPayloadStatus(payload, 10)
	if status != StatusFilled {
		t.Errorf("expected filled, got %s", status)
	}
	if raw != "FILLED" {
		t.Errorf("expected raw 'FILLED', got %q", raw)
	}
	if fillQty != 10 {
		t.Errorf("expected fill qty 10, got %.4f", fillQty)
	}
	if avgPrice != 150.25 {
		t.Errorf("expected avg price 150.25, got %.4f", avgPrice)
	}
}

func TestMapBrokerPayloadStatus_FilledFallsBackToRequested(t *testing.T) {
	// When fill_qty is 0 but status is FILLED, use requested qty
	payload := map[string]interface{}{"status": "FILLED"}
	status, _, fillQty, _ := mapBrokerPayloadStatus(payload, 25)
	if status != StatusFilled {
		t.Errorf("expected filled, got %s", status)
	}
	if fillQty != 25 {
		t.Errorf("expected fallback to requested qty 25, got %.4f", fillQty)
	}
}

func TestMapBrokerPayloadStatus_PartiallyFilled(t *testing.T) {
	for _, rawStatus := range []string{"PARTIALLY_FILLED", "PARTIAL", "PARTIAL_FILL"} {
		payload := map[string]interface{}{"status": rawStatus, "filled_qty": 5.0}
		status, _, fillQty, _ := mapBrokerPayloadStatus(payload, 10)
		if status != StatusPartiallyFilled {
			t.Errorf("status %q: expected partially_filled, got %s", rawStatus, status)
		}
		if fillQty != 5 {
			t.Errorf("status %q: expected fill qty 5, got %.4f", rawStatus, fillQty)
		}
	}
}

func TestMapBrokerPayloadStatus_Cancelled(t *testing.T) {
	for _, rawStatus := range []string{"CANCELLED", "CANCELED"} {
		payload := map[string]interface{}{"status": rawStatus}
		status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
		if status != StatusCancelled {
			t.Errorf("status %q: expected cancelled, got %s", rawStatus, status)
		}
	}
}

func TestMapBrokerPayloadStatus_Rejected(t *testing.T) {
	payload := map[string]interface{}{"status": "REJECTED"}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusRejected {
		t.Errorf("expected rejected, got %s", status)
	}
}

func TestMapBrokerPayloadStatus_Acknowledged(t *testing.T) {
	for _, rawStatus := range []string{"ACCEPTED", "PRESUBMITTED", "PRE_SUBMITTED"} {
		payload := map[string]interface{}{"status": rawStatus}
		status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
		if status != StatusAcknowledged {
			t.Errorf("status %q: expected acknowledged, got %s", rawStatus, status)
		}
	}
}

func TestMapBrokerPayloadStatus_Submitted(t *testing.T) {
	for _, rawStatus := range []string{"SUBMITTED", "PENDING", "PENDING_SUBMIT"} {
		payload := map[string]interface{}{"status": rawStatus}
		status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
		if status != StatusSubmitted {
			t.Errorf("status %q: expected submitted, got %s", rawStatus, status)
		}
	}
}

func TestMapBrokerPayloadStatus_PolymorphicStatusKey(t *testing.T) {
	// Uses order_status when status is missing
	payload := map[string]interface{}{"order_status": "FILLED", "filled_qty": 10.0}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusFilled {
		t.Errorf("expected filled via order_status key, got %s", status)
	}
}

func TestMapBrokerPayloadStatus_PolymorphicFillQtyKeys(t *testing.T) {
	keys := []string{"filled_qty", "filledQty", "fill_qty"}
	for _, key := range keys {
		payload := map[string]interface{}{"status": "PARTIALLY_FILLED", key: 7.0}
		_, _, fillQty, _ := mapBrokerPayloadStatus(payload, 10)
		if fillQty != 7 {
			t.Errorf("key %q: expected fill qty 7, got %.4f", key, fillQty)
		}
	}
}

func TestMapBrokerPayloadStatus_PolymorphicPriceKeys(t *testing.T) {
	keys := []string{"avg_fill_price", "average_fill_price", "price"}
	for _, key := range keys {
		payload := map[string]interface{}{"status": "FILLED", key: 99.5}
		_, _, _, avgPrice := mapBrokerPayloadStatus(payload, 10)
		if avgPrice != 99.5 {
			t.Errorf("key %q: expected price 99.5, got %.4f", key, avgPrice)
		}
	}
}

func TestMapBrokerPayloadStatus_InferFilledFromFillQty(t *testing.T) {
	// Unknown status but fill qty >= requested → infer filled
	payload := map[string]interface{}{"status": "UNKNOWN_STATUS", "filled_qty": 10.0}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusFilled {
		t.Errorf("expected inferred filled, got %s", status)
	}
}

func TestMapBrokerPayloadStatus_InferPartialFromFillQty(t *testing.T) {
	// Unknown status but fill qty < requested → infer partial
	payload := map[string]interface{}{"status": "UNKNOWN_STATUS", "filled_qty": 5.0}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusPartiallyFilled {
		t.Errorf("expected inferred partially_filled, got %s", status)
	}
}

func TestMapBrokerPayloadStatus_UnknownFallsBackToSubmitted(t *testing.T) {
	payload := map[string]interface{}{"status": "UNKNOWN_STATUS"}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusSubmitted {
		t.Errorf("expected fallback submitted, got %s", status)
	}
}

func TestMapBrokerPayloadStatus_CaseInsensitive(t *testing.T) {
	// Status is trimmed and uppercased internally
	payload := map[string]interface{}{"status": " filled "}
	status, _, _, _ := mapBrokerPayloadStatus(payload, 10)
	if status != StatusFilled {
		t.Errorf("expected case-insensitive match, got %s", status)
	}
}

// ---------------------------------------------------------------------------
// mapOrderRecordStatus — orders.Record to execution.Status mapping
// ---------------------------------------------------------------------------

func TestMapOrderRecordStatus_AllMappings(t *testing.T) {
	cases := []struct {
		input orders.Status
		want  Status
	}{
		{orders.StatusFilled, StatusFilled},
		{orders.StatusPartiallyFilled, StatusPartiallyFilled},
		{orders.StatusCancelled, StatusCancelled},
		{orders.StatusRejected, StatusRejected},
		{orders.StatusAccepted, StatusAcknowledged},
		{orders.StatusSubmitted, StatusSubmitted},
	}
	for _, tc := range cases {
		record := &orders.Record{Status: tc.input}
		got, ok := mapOrderRecordStatus(record)
		if !ok {
			t.Errorf("expected mapping for %q", tc.input)
			continue
		}
		if got != tc.want {
			t.Errorf("mapOrderRecordStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMapOrderRecordStatus_Nil(t *testing.T) {
	_, ok := mapOrderRecordStatus(nil)
	if ok {
		t.Error("expected false for nil record")
	}
}

func TestMapOrderRecordStatus_Unknown(t *testing.T) {
	record := &orders.Record{Status: orders.StatusUnknown}
	_, ok := mapOrderRecordStatus(record)
	if ok {
		t.Error("expected false for unknown status")
	}
}

// ---------------------------------------------------------------------------
// fillQtyOrRequested
// ---------------------------------------------------------------------------

func TestFillQtyOrRequested_PositiveFillQty(t *testing.T) {
	if got := fillQtyOrRequested(15, 10); got != 15 {
		t.Errorf("expected 15, got %.4f", got)
	}
}

func TestFillQtyOrRequested_ZeroFillQtyFallsBack(t *testing.T) {
	if got := fillQtyOrRequested(0, 10); got != 10 {
		t.Errorf("expected fallback 10, got %.4f", got)
	}
}

func TestFillQtyOrRequested_NegativeFillQtyFallsBack(t *testing.T) {
	if got := fillQtyOrRequested(-1, 10); got != 10 {
		t.Errorf("expected fallback 10, got %.4f", got)
	}
}

// ---------------------------------------------------------------------------
// chooseEventTime
// ---------------------------------------------------------------------------

func TestChooseEventTime_FirstNonZero(t *testing.T) {
	now := time.Now().UTC()
	got := chooseEventTime(time.Time{}, now, now.Add(time.Hour))
	if got != now {
		t.Errorf("expected first non-zero time")
	}
}

func TestChooseEventTime_AllZero(t *testing.T) {
	got := chooseEventTime(time.Time{}, time.Time{})
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestChooseEventTime_SingleValue(t *testing.T) {
	now := time.Now().UTC()
	got := chooseEventTime(now)
	if got != now {
		t.Errorf("expected %v, got %v", now, got)
	}
}

func TestChooseEventTime_Empty(t *testing.T) {
	got := chooseEventTime()
	if !got.IsZero() {
		t.Errorf("expected zero time for no args, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Status.Terminal
// ---------------------------------------------------------------------------

func TestStatusTerminal(t *testing.T) {
	terminal := []Status{StatusFilled, StatusCancelled, StatusRejected, StatusFailed, StatusBlocked, StatusDuplicateSuppressed, StatusStale}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	nonTerminal := []Status{StatusPending, StatusSubmitted, StatusAcknowledged, StatusPartiallyFilled}
	for _, s := range nonTerminal {
		if s.Terminal() {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}
