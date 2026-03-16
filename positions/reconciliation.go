package positions

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	issueCap                 = 12
	defaultQuantityTolerance = 0.01
	defaultPricePctTolerance = 0.005
	defaultPriceMinTolerance = 0.05
)

func Key(symbol, side string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	side = strings.ToLower(strings.TrimSpace(side))
	if symbol == "" || side == "" {
		return ""
	}
	return symbol + "_" + side
}

func NormalizeSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Symbol = strings.ToUpper(strings.TrimSpace(snapshot.Symbol))
	snapshot.Side = strings.ToLower(strings.TrimSpace(snapshot.Side))
	if snapshot.Quantity < 0 {
		snapshot.Quantity = math.Abs(snapshot.Quantity)
	}
	return snapshot
}

func Compare(local, broker []Snapshot, now time.Time) Result {
	result := Result{
		RanAt:           now,
		LocalPositions:  len(local),
		BrokerPositions: len(broker),
		Issues:          make([]Issue, 0, issueCap),
	}

	localByKey := make(map[string]Snapshot, len(local))
	brokerByKey := make(map[string]Snapshot, len(broker))
	keys := make(map[string]struct{}, len(local)+len(broker))

	for _, snapshot := range local {
		snapshot = NormalizeSnapshot(snapshot)
		if key := Key(snapshot.Symbol, snapshot.Side); key != "" {
			localByKey[key] = snapshot
			keys[key] = struct{}{}
		}
	}
	for _, snapshot := range broker {
		snapshot = NormalizeSnapshot(snapshot)
		if key := Key(snapshot.Symbol, snapshot.Side); key != "" {
			brokerByKey[key] = snapshot
			keys[key] = struct{}{}
		}
	}

	sortedKeys := make([]string, 0, len(keys))
	for key := range keys {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		localPos, hasLocal := localByKey[key]
		brokerPos, hasBroker := brokerByKey[key]

		switch {
		case hasLocal && !hasBroker:
			result.Mismatches++
			result.LocalMissingAtBroker++
			appendIssue(&result.Issues, Issue{
				Type:            IssueLocalMissingAtBroker,
				Symbol:          localPos.Symbol,
				Side:            localPos.Side,
				LocalQuantity:   localPos.Quantity,
				LocalEntryPrice: localPos.EntryPrice,
				Message:         fmt.Sprintf("local position %s %s missing at broker", localPos.Symbol, localPos.Side),
			})
		case !hasLocal && hasBroker:
			result.Mismatches++
			result.BrokerMissingLocally++
			appendIssue(&result.Issues, Issue{
				Type:             IssueBrokerMissingLocally,
				Symbol:           brokerPos.Symbol,
				Side:             brokerPos.Side,
				BrokerQuantity:   brokerPos.Quantity,
				BrokerEntryPrice: brokerPos.EntryPrice,
				Message:          fmt.Sprintf("broker position %s %s missing locally", brokerPos.Symbol, brokerPos.Side),
			})
		case hasLocal && hasBroker:
			if !quantityApproxEqual(localPos.Quantity, brokerPos.Quantity) {
				result.Mismatches++
				result.SizeMismatches++
				appendIssue(&result.Issues, Issue{
					Type:             IssueSizeMismatch,
					Symbol:           brokerPos.Symbol,
					Side:             brokerPos.Side,
					LocalQuantity:    localPos.Quantity,
					BrokerQuantity:   brokerPos.Quantity,
					LocalEntryPrice:  localPos.EntryPrice,
					BrokerEntryPrice: brokerPos.EntryPrice,
					Message: fmt.Sprintf(
						"position size mismatch for %s %s: local %.4f vs broker %.4f",
						brokerPos.Symbol,
						brokerPos.Side,
						localPos.Quantity,
						brokerPos.Quantity,
					),
				})
			}
			if priceMismatch(localPos.EntryPrice, brokerPos.EntryPrice) {
				result.Mismatches++
				result.PriceMismatches++
				appendIssue(&result.Issues, Issue{
					Type:             IssuePriceMismatch,
					Symbol:           brokerPos.Symbol,
					Side:             brokerPos.Side,
					LocalQuantity:    localPos.Quantity,
					BrokerQuantity:   brokerPos.Quantity,
					LocalEntryPrice:  localPos.EntryPrice,
					BrokerEntryPrice: brokerPos.EntryPrice,
					Message: fmt.Sprintf(
						"position entry price mismatch for %s %s: local %.4f vs broker %.4f",
						brokerPos.Symbol,
						brokerPos.Side,
						localPos.EntryPrice,
						brokerPos.EntryPrice,
					),
				})
			}
		}
	}

	result.Summary = buildSummary(result)
	return result
}

func appendIssue(issues *[]Issue, issue Issue) {
	if len(*issues) >= issueCap {
		return
	}
	*issues = append(*issues, issue)
}

func quantityApproxEqual(left, right float64) bool {
	diff := math.Abs(left - right)
	if diff <= defaultQuantityTolerance {
		return true
	}
	baseline := math.Max(math.Abs(left), math.Abs(right))
	if baseline == 0 {
		return true
	}
	return diff <= baseline*0.001
}

func priceMismatch(localPrice, brokerPrice float64) bool {
	if localPrice <= 0 && brokerPrice <= 0 {
		return false
	}
	if localPrice <= 0 || brokerPrice <= 0 {
		return true
	}
	diff := math.Abs(localPrice - brokerPrice)
	allowed := math.Max(defaultPriceMinTolerance, math.Max(localPrice, brokerPrice)*defaultPricePctTolerance)
	return diff > allowed
}

func buildSummary(result Result) string {
	if result.Mismatches == 0 {
		return fmt.Sprintf(
			"position reconciliation clean: %d local / %d broker positions aligned",
			result.LocalPositions,
			result.BrokerPositions,
		)
	}
	return fmt.Sprintf(
		"position reconciliation found %d mismatch(es): local_missing=%d broker_missing=%d size=%d price=%d",
		result.Mismatches,
		result.LocalMissingAtBroker,
		result.BrokerMissingLocally,
		result.SizeMismatches,
		result.PriceMismatches,
	)
}
