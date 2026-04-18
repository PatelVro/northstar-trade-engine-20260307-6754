// Package trader - demo_cycle.go
// Demo-mode cycle orchestration and synthetic position generation.
// Separated from auto_trader.go so the demo path (which never touches a
// broker, market data provider, or AI API) can evolve independently of
// the live cycle. All AutoTrader methods stay on the *AutoTrader receiver.
package trader

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"northstar/logger"
	"time"
)

func (at *AutoTrader) runDemoCycle() error {
	if at.demoRand == nil {
		at.demoRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	phase := float64(at.callCount%96) / 96.0 * 2.0 * math.Pi
	wavePct := 0.04*math.Sin(phase) + 0.02*math.Cos(phase*0.5)
	noisePct := (at.demoRand.Float64() - 0.5) * 0.06
	changePct := wavePct + noisePct

	nextEquity := at.demoEquity * (1.0 + (changePct / 100.0))
	floor := at.initialBalance * 0.82
	ceiling := at.initialBalance * 1.40
	if nextEquity < floor {
		nextEquity = floor + at.initialBalance*0.01*at.demoRand.Float64()
	}
	if nextEquity > ceiling {
		nextEquity = ceiling - at.initialBalance*0.01*at.demoRand.Float64()
	}

	at.demoEquity = nextEquity
	at.demoPositionCount = at.demoRand.Intn(4)
	at.demoMarginUsedPct = 8.0 + at.demoRand.Float64()*28.0
	if at.demoPositionCount == 0 {
		at.demoMarginUsedPct = 0
	}
	now := time.Now()
	at.demoSnapshotSeed = now.UnixNano()

	positions := at.buildDemoPositions()
	totalMarginUsed := 0.0
	totalUnrealized := 0.0
	for _, pos := range positions {
		if v, ok := pos["margin_used"].(float64); ok {
			totalMarginUsed += v
		}
		if v, ok := pos["unrealized_pnl"].(float64); ok {
			totalUnrealized += v
		}
	}
	if at.demoEquity > 0 {
		at.demoMarginUsedPct = (totalMarginUsed / at.demoEquity) * 100.0
	} else {
		at.demoMarginUsedPct = 0
	}
	at.demoPositionCount = len(positions)
	walletBalance := at.demoEquity - totalUnrealized
	if walletBalance < 0 {
		walletBalance = 0
	}
	at.demoAvailableBalance = walletBalance - totalMarginUsed
	if at.demoAvailableBalance < 0 {
		at.demoAvailableBalance = 0
	}
	at.demoLastCycleTime = now

	totalPnL := at.demoEquity - at.initialBalance
	at.dailyPnL = totalPnL
	summary := at.buildDemoAccountSummary(positions)

	record := &logger.DecisionRecord{
		InputPrompt:  "Demo mode cycle: synthetic paper update",
		CoTTrace:     "Demo mode is enabled. No live broker, market data, or AI API call was used in this cycle.",
		DecisionJSON: "[]",
		AccountState: logger.AccountSnapshot{
			AccountingVersion:      summary.AccountingVersion,
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
			DailyPnL:               summary.DailyPnL,
			PositionCount:          summary.PositionCount,
			MarginUsed:             summary.MarginUsed,
			MarginUsedPct:          summary.MarginUsedPct,
			TotalBalance:           summary.AccountEquity,
			TotalUnrealizedProfit:  summary.UnrealizedPnL,
		},
		Decisions:    []logger.DecisionAction{},
		ExecutionLog: []string{fmt.Sprintf("demo cycle update: equity=%.2f pnl=%.2f delta=%.4f%%", at.demoEquity, totalPnL, changePct)},
		Success:      true,
	}

	if err := at.logDecisionAndAudit(record, nil, nil); err != nil {
		return fmt.Errorf("failed to write demo decision record: %w", err)
	}
	at.clearBlockedCycle()

	log.Printf(" Demo cycle #%d | equity=%.2f | pnl=%.2f (%.2f%%)",
		at.callCount, at.demoEquity, totalPnL, (totalPnL/at.initialBalance)*100.0)

	return nil
}

func (at *AutoTrader) buildDemoAccountSummary(positions []map[string]interface{}) AccountSummary {
	grossMarketValue := 0.0
	unrealizedPnL := 0.0
	marginUsed := 0.0
	for _, pos := range positions {
		markPrice, _ := parseFloat(pos["mark_price"])
		quantity, _ := parseFloat(pos["quantity"])
		if quantity < 0 {
			quantity = -quantity
		}
		grossMarketValue += markPrice * quantity
		if pnl, ok := parseFloat(pos["unrealized_pnl"]); ok {
			unrealizedPnL += pnl
		}
		if used, ok := parseFloat(pos["margin_used"]); ok {
			marginUsed += used
		}
	}

	accountCash := at.demoEquity - grossMarketValue - unrealizedPnL
	if accountCash < 0 {
		accountCash = 0
	}
	realizedPnL := (at.demoEquity - at.initialBalance) - unrealizedPnL
	marginUsedPct := 0.0
	if at.demoEquity > 0 {
		marginUsedPct = (marginUsed / at.demoEquity) * 100.0
	}

	return buildAccountSummary(normalizedBrokerAccount{
		AccountCash:      accountCash,
		AvailableBalance: at.demoAvailableBalance,
		AccountEquity:    at.demoEquity,
		GrossMarketValue: grossMarketValue,
		UnrealizedPnL:    unrealizedPnL,
		RealizedPnL:      realizedPnL,
		MarginUsed:       marginUsed,
		MarginUsedPct:    marginUsedPct,
		PositionCount:    len(positions),
	}, at.initialBalance, realizedPnL, at.dailyPnL)
}

func (at *AutoTrader) buildDemoPositions() []map[string]interface{} {
	if at.demoPositionCount <= 0 {
		return []map[string]interface{}{}
	}

	seed := at.demoSnapshotSeed
	if seed == 0 {
		seed = int64(at.callCount + 1)
	}
	r := rand.New(rand.NewSource(seed))

	symbols := []string{"AAPL", "MSFT", "NVDA", "AMZN", "GOOGL", "META", "TSLA", "SHOP", "RY", "TD", "BNS", "ENB"}
	positions := make([]map[string]interface{}, 0, at.demoPositionCount)
	totalMarginBudget := at.demoEquity * (at.demoMarginUsedPct / 100.0)
	if totalMarginBudget < 0 {
		totalMarginBudget = 0
	}

	for i := 0; i < at.demoPositionCount; i++ {
		symbol := symbols[(at.callCount+i)%len(symbols)]
		base := demoSymbolBasePrice(symbol)
		leverage := 2 + r.Intn(4) // 2x..5x
		side := "long"
		if r.Float64() > 0.6 {
			side = "short"
		}

		entryPrice := base * (0.97 + r.Float64()*0.06)
		drift := (r.Float64() - 0.5) * 0.04 // +/-2%
		markPrice := entryPrice * (1.0 + drift)
		allocatedMargin := totalMarginBudget / float64(at.demoPositionCount)
		if allocatedMargin <= 0 {
			allocatedMargin = at.demoEquity * 0.02
		}

		quantity := (allocatedMargin * float64(leverage)) / entryPrice
		unrealized := (markPrice - entryPrice) * quantity
		if side == "short" {
			unrealized = -unrealized
		}

		liqPrice := entryPrice * (1.0 - 0.20/float64(leverage))
		if side == "short" {
			liqPrice = entryPrice * (1.0 + 0.20/float64(leverage))
		}

		marginUsed := (quantity * markPrice) / float64(leverage)
		entryMarginUsed := (quantity * entryPrice) / float64(leverage)
		unrealizedPct := 0.0
		if entryMarginUsed > 0 {
			unrealizedPct = (unrealized / entryMarginUsed) * 100.0
		}

		positions = append(positions, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealized,
			"unrealized_pnl_pct": unrealizedPct,
			"liquidation_price":  liqPrice,
			"margin_used":        marginUsed,
		})
	}

	return positions
}

func demoSymbolBasePrice(symbol string) float64 {
	switch symbol {
	case "AAPL":
		return 195
	case "MSFT":
		return 420
	case "NVDA":
		return 880
	case "AMZN":
		return 185
	case "GOOGL":
		return 165
	case "META":
		return 505
	case "TSLA":
		return 220
	case "SHOP":
		return 95
	case "RY":
		return 128
	case "TD":
		return 83
	case "BNS":
		return 70
	case "ENB":
		return 51
	default:
		return 100
	}
}
