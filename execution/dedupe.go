package execution

import (
	"fmt"
	"strconv"
	"strings"
)

func BuildDedupeKey(intent Intent) string {
	parts := []string{
		strings.TrimSpace(intent.TraderID),
		strings.ToUpper(strings.TrimSpace(intent.Symbol)),
		strings.ToLower(strings.TrimSpace(intent.Side)),
		strings.ToLower(strings.TrimSpace(intent.ActionType)),
		formatFloatKey(intent.Quantity),
		strings.ToLower(strings.TrimSpace(intent.OrderType)),
		formatFloatKey(intent.LimitPrice),
		formatFloatKey(intent.StopPrice),
		strings.ToLower(strings.TrimSpace(intent.TIF)),
		boolKey(intent.IncreasesExposure),
		boolKey(intent.ReduceOnly),
		strings.TrimSpace(intent.LocalRequestKey),
	}
	return strings.Join(parts, "|")
}

func formatFloatKey(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', 8, 64)
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func classifyAction(intent Intent) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(intent.ActionType)) {
	case "open_long", "open_short":
		return "entry", true
	case "close_long", "close_short":
		return "exit", true
	default:
		return "unknown", false
	}
}

func validateIntent(intent Intent, gate Gate) string {
	if strings.TrimSpace(intent.Symbol) == "" {
		return "execution intent missing symbol"
	}
	if intent.Quantity <= 0 {
		return fmt.Sprintf("execution intent quantity %.8f must be positive", intent.Quantity)
	}
	orderType := strings.ToLower(strings.TrimSpace(intent.OrderType))
	if orderType == "" {
		orderType = "market"
	}
	switch orderType {
	case "market", "limit", "stop", "stop_limit":
	default:
		return fmt.Sprintf("unsupported execution order_type %s", intent.OrderType)
	}
	if (orderType == "limit" || orderType == "stop_limit") && intent.LimitPrice <= 0 {
		return "limit-style execution intent requires a positive limit_price"
	}
	if (orderType == "stop" || orderType == "stop_limit") && intent.StopPrice <= 0 {
		return "stop-style execution intent requires a positive stop_price"
	}

	classification, known := classifyAction(intent)
	if !gate.TradingAllowed {
		return blockedReason(gate)
	}
	if !known {
		if !gate.EntriesAllowed || !gate.ExitsAllowed {
			return fmt.Sprintf("%s blocked uncertain execution action %s for %s", gate.Mode, intent.ActionType, intent.Symbol)
		}
		return ""
	}
	switch classification {
	case "entry":
		if !gate.EntriesAllowed {
			return blockedReason(gate)
		}
	case "exit":
		if !gate.ExitsAllowed {
			return blockedReason(gate)
		}
	}
	return ""
}

func blockedReason(gate Gate) string {
	reason := strings.TrimSpace(gate.BlockReason)
	if reason == "" {
		reason = "execution blocked by final trading gate"
	}
	return reason
}
