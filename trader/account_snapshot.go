package trader

import (
	"fmt"
	"northstar/decision"
	"strings"
	"time"
)

const runtimeAccountSnapshotTTL = 2 * time.Second

type runtimeAccountSnapshot struct {
	CapturedAt time.Time
	Summary    AccountSummary
	Positions  []map[string]interface{}
}

func (at *AutoTrader) snapshotAccountAndPositions() (AccountSummary, []map[string]interface{}, error) {
	if at.demoMode {
		positions := at.buildDemoPositions()
		summary := at.buildDemoAccountSummary(positions)
		at.setLatestAccountSummary(&summary)
		return summary, positions, nil
	}
	if at.shadowModeEnabled() {
		summary := at.shadowAccountSummary(at.currentLatestAccountSummary())
		positions := at.shadowPositionViews()
		at.setLatestAccountSummary(&summary)
		return summary, positions, nil
	}
	if summary, positions, ok := at.currentRuntimeAccountSnapshot(runtimeAccountSnapshotTTL); ok {
		at.setLatestAccountSummary(summary)
		return *summary, positions, nil
	}
	if at.trader == nil {
		return AccountSummary{}, nil, fmt.Errorf("trader is not initialized")
	}

	balance, err := at.trader.GetBalance()
	if err != nil {
		return AccountSummary{}, nil, fmt.Errorf("failed to get broker balance: %w", err)
	}
	rawPositions, err := at.trader.GetPositions()
	if err != nil {
		return AccountSummary{}, nil, fmt.Errorf("failed to get positions: %w", err)
	}

	summary := at.buildAccountSummaryFromRaw(balance, rawPositions)
	positions := normalizePositionViews(rawPositions)
	at.setRuntimeAccountSnapshot(summary, positions)
	at.setLatestAccountSummary(&summary)
	return summary, positions, nil
}

func (at *AutoTrader) currentRuntimeAccountSnapshot(maxAge time.Duration) (*AccountSummary, []map[string]interface{}, bool) {
	at.accountSnapshotMu.RLock()
	snapshot := at.runtimeAccountSnapshot
	at.accountSnapshotMu.RUnlock()

	if snapshot == nil || snapshot.CapturedAt.IsZero() {
		return nil, nil, false
	}
	if maxAge > 0 && time.Since(snapshot.CapturedAt) > maxAge {
		return nil, nil, false
	}

	summary := snapshot.Summary
	return &summary, clonePositionMaps(snapshot.Positions), true
}

func (at *AutoTrader) setRuntimeAccountSnapshot(summary AccountSummary, positions []map[string]interface{}) {
	at.accountSnapshotMu.Lock()
	at.runtimeAccountSnapshot = &runtimeAccountSnapshot{
		CapturedAt: time.Now(),
		Summary:    summary,
		Positions:  clonePositionMaps(positions),
	}
	at.accountSnapshotMu.Unlock()
}

func (at *AutoTrader) invalidateRuntimeAccountSnapshot() {
	at.accountSnapshotMu.Lock()
	at.runtimeAccountSnapshot = nil
	at.accountSnapshotMu.Unlock()
}

func clonePositionMaps(positions []map[string]interface{}) []map[string]interface{} {
	if len(positions) == 0 {
		return []map[string]interface{}{}
	}
	cloned := make([]map[string]interface{}, 0, len(positions))
	for _, pos := range positions {
		if pos == nil {
			cloned = append(cloned, map[string]interface{}{})
			continue
		}
		next := make(map[string]interface{}, len(pos))
		for key, value := range pos {
			next[key] = value
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func normalizePositionViews(positions []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(positions))
	for _, pos := range positions {
		symbol := strings.ToUpper(strings.TrimSpace(toString(firstPresent(pos["symbol"], pos["ticker"]))))
		side := strings.ToLower(strings.TrimSpace(toString(firstPresent(pos["side"], pos["positionSide"], pos["position_side"]))))
		entryPrice, _ := parseFloat(firstPresent(pos["entryPrice"], pos["entry_price"]))
		markPrice, _ := parseFloat(firstPresent(pos["markPrice"], pos["mark_price"], pos["price"]))
		quantity, _ := parseFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"]))
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnL, _ := parseFloat(firstPresent(pos["unRealizedProfit"], pos["unrealizedPnl"], pos["unrealized_profit"], pos["unrealized_pnl"]))
		liquidationPrice, _ := parseFloat(firstPresent(pos["liquidationPrice"], pos["liquidation_price"]))

		leverage := 1
		if lev, ok := parseFloat(firstPresent(pos["leverage"])); ok && lev > 0 {
			leverage = int(lev)
		}
		if leverage <= 0 {
			leverage = 1
		}

		pnlPct := 0.0
		if entryPrice > 0 {
			if side == "short" {
				pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
			} else {
				pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
			}
		}

		marginUsed := 0.0
		if markPrice > 0 {
			marginUsed = (quantity * markPrice) / float64(leverage)
		}

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entryPrice":         entryPrice,
			"entry_price":        entryPrice,
			"markPrice":          markPrice,
			"mark_price":         markPrice,
			"price":              markPrice,
			"positionAmt":        quantity,
			"position_amt":       quantity,
			"qty":                quantity,
			"quantity":           quantity,
			"leverage":           leverage,
			"unRealizedProfit":   unrealizedPnL,
			"unrealizedPnl":      unrealizedPnL,
			"unrealized_pnl":     unrealizedPnL,
			"unrealized_pnl_pct": pnlPct,
			"liquidationPrice":   liquidationPrice,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		})
	}
	return result
}

func (at *AutoTrader) buildDecisionPositionInfos(positions []map[string]interface{}) []decision.PositionInfo {
	positionInfos := make([]decision.PositionInfo, 0, len(positions))
	currentPositionKeys := make(map[string]bool, len(positions))

	for _, pos := range positions {
		symbol := strings.ToUpper(strings.TrimSpace(toString(firstPresent(pos["symbol"], pos["ticker"]))))
		side := strings.ToLower(strings.TrimSpace(toString(firstPresent(pos["side"], pos["positionSide"], pos["position_side"]))))
		if symbol == "" || side == "" {
			continue
		}

		entryPrice, _ := parseFloat(firstPresent(pos["entryPrice"], pos["entry_price"]))
		markPrice, _ := parseFloat(firstPresent(pos["markPrice"], pos["mark_price"], pos["price"]))
		quantity, _ := parseFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"]))
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnL, _ := parseFloat(firstPresent(pos["unRealizedProfit"], pos["unrealizedPnl"], pos["unrealized_profit"], pos["unrealized_pnl"]))
		liquidationPrice, _ := parseFloat(firstPresent(pos["liquidationPrice"], pos["liquidation_price"]))

		leverage := 1
		if lev, ok := parseFloat(firstPresent(pos["leverage"])); ok && lev > 0 {
			leverage = int(lev)
		}
		if leverage <= 0 {
			leverage = 1
		}

		marginUsed := 0.0
		if markPrice > 0 {
			marginUsed = (quantity * markPrice) / float64(leverage)
		}

		pnlPct := 0.0
		if entryPrice > 0 {
			if side == "short" {
				pnlPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
			} else {
				pnlPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
			}
		}

		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true
		updateTime := time.Now().UnixMilli()
		if firstSeen, ok := at.positionFirstSeenTime[posKey]; ok {
			updateTime = firstSeen
		} else {
			at.positionFirstSeenTime[posKey] = updateTime
		}
		if explicitUpdateTime, ok := parseFloat(firstPresent(pos["updateTime"], pos["update_time"])); ok && explicitUpdateTime > 0 {
			updateTime = int64(explicitUpdateTime)
		}

		positionInfos = append(positionInfos, decision.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnL,
			UnrealizedPnLPct: pnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
		})
	}

	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

	return positionInfos
}

func decisionAccountInfoFromSummary(summary AccountSummary) decision.AccountInfo {
	return decision.AccountInfo{
		AccountCash:            summary.AccountCash,
		AccountEquity:          summary.AccountEquity,
		AvailableBalance:       summary.AvailableBalance,
		GrossMarketValue:       summary.GrossMarketValue,
		UnrealizedPnL:          summary.UnrealizedPnL,
		RealizedPnL:            summary.RealizedPnL,
		TotalPnL:               summary.TotalPnL,
		StrategyInitialCapital: summary.StrategyInitialCapital,
		StrategyEquity:         summary.StrategyEquity,
		StrategyReturnPct:      summary.StrategyReturnPct,
		MarginUsed:             summary.MarginUsed,
		MarginUsedPct:          summary.MarginUsedPct,
		PositionCount:          summary.PositionCount,
	}
}
