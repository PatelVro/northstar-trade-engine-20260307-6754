package trader

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"northstar/market"
	"os"
	"path/filepath"
	"time"
)

// SimPosition represents a simulated position
type SimPosition struct {
	Symbol     string
	Quantity   float64
	EntryPrice float64
	Side       string // "long" or "short"
}

// SimTrader implements Trader interface but executes "paper fills" against the local provider.
type SimTrader struct {
	balance               float64
	initialBal            float64
	provider              market.BarsProvider
	positions             map[string]*SimPosition
	realizedPnL           float64
	tradeCount            int
	winCount              int
	lossCount             int
	maxDrawdown           float64
	peakEquity            float64
	equityCurve           []float64
	tradePnLs             []float64
	commissionBps         float64
	spreadBps             float64
	slippageBps           float64
	impactBps             float64
	maxPartRate           float64
	maxImpactBps          float64
	totalFeesUSD          float64
	totalSpreadCostUSD    float64
	totalSlippageCostUSD  float64
	totalImpactCostUSD    float64
	totalExecutionCostUSD float64
	partialFills          int
	rejectedFills         int
}

// NewSimTrader creates a new simulated broker for testing
func NewSimTrader(initialBalance float64, provider market.BarsProvider) *SimTrader {
	os.MkdirAll("output", os.ModePerm)
	// Initialize trades file headers
	if f, err := os.Create(filepath.Join("output", "trades.csv")); err == nil {
		f.WriteString("timestamp,symbol,action,quantity,entry_price,exit_price,realized_pnl,fees_usd,participation,spread_bps,slippage_bps,impact_bps,spread_cost_usd,slippage_cost_usd,impact_cost_usd,total_execution_cost_usd,reason\n")
		f.Close()
	}
	// Initialize equity curve headers
	if f, err := os.Create(filepath.Join("output", "equity_curve.csv")); err == nil {
		f.WriteString("timestamp,equity,cash,unrealized_pnl,realized_pnl,position_count\n")
		f.Close()
	}

	return &SimTrader{
		balance:               initialBalance,
		initialBal:            initialBalance,
		provider:              provider,
		positions:             make(map[string]*SimPosition),
		peakEquity:            initialBalance,
		equityCurve:           []float64{initialBalance},
		tradePnLs:             make([]float64, 0, 128),
		commissionBps:         0,
		spreadBps:             0,
		slippageBps:           0,
		impactBps:             0,
		maxPartRate:           0.15,
		maxImpactBps:          120.0,
		totalFeesUSD:          0,
		totalSpreadCostUSD:    0,
		totalSlippageCostUSD:  0,
		totalImpactCostUSD:    0,
		totalExecutionCostUSD: 0,
		partialFills:          0,
		rejectedFills:         0,
	}
}

// SetExecutionCosts configures simulated commission and slippage in basis points.
func (s *SimTrader) SetExecutionCosts(commissionBps, slippageBps float64) {
	if commissionBps < 0 {
		commissionBps = 0
	}
	if slippageBps < 0 {
		slippageBps = 0
	}
	s.commissionBps = commissionBps
	s.slippageBps = slippageBps
}

// SetExecutionCostModel configures the canonical simulated friction model.
func (s *SimTrader) SetExecutionCostModel(model ExecutionCostModel) {
	if s == nil {
		return
	}
	s.commissionBps = sanitizeNonNegative(model.CommissionBps)
	s.spreadBps = sanitizeNonNegative(model.SpreadBps)
	s.slippageBps = sanitizeNonNegative(model.SlippageBps)
	s.impactBps = sanitizeNonNegative(model.ImpactBps)
	if model.MaxParticipationRate > 0 && model.MaxParticipationRate <= 1.0 {
		s.maxPartRate = model.MaxParticipationRate
	} else {
		s.maxPartRate = 0.15
	}
}

// SetExecutionImpactModel configures liquidity participation and impact slippage.
func (s *SimTrader) SetExecutionImpactModel(impactBps, maxParticipationRate float64) {
	if impactBps < 0 {
		impactBps = 0
	}
	if maxParticipationRate <= 0 || maxParticipationRate > 1.0 {
		maxParticipationRate = 0.15
	}
	s.impactBps = impactBps
	s.maxPartRate = maxParticipationRate
}

// GetBalance returns simulated account balance
func (s *SimTrader) GetBalance() (map[string]interface{}, error) {
	unrealizedPnL := 0.0
	lockedPrincipal := 0.0

	for _, pos := range s.positions {
		lockedPrincipal += pos.EntryPrice * pos.Quantity

		currentPrice, err := s.GetMarketPrice(pos.Symbol)
		if err == nil {
			if pos.Side == "long" {
				unrealizedPnL += (currentPrice - pos.EntryPrice) * pos.Quantity
			} else {
				unrealizedPnL += (pos.EntryPrice - currentPrice) * pos.Quantity
			}
		}
	}

	// Equity = free cash + principal currently allocated to positions + unrealized PnL.
	totalWalletBalance := s.balance + lockedPrincipal
	totalEquity := totalWalletBalance + unrealizedPnL

	if totalEquity > s.peakEquity {
		s.peakEquity = totalEquity
	}
	s.equityCurve = append(s.equityCurve, totalEquity)
	drawdown := (s.peakEquity - totalEquity) / s.peakEquity
	if drawdown > s.maxDrawdown {
		s.maxDrawdown = drawdown
	}

	log.Printf("[ACCOUNT] cash=%.2f equity=%.2f unrealized_pnl=%.2f realized_pnl=%.2f positions=%d",
		s.balance, totalEquity, unrealizedPnL, s.realizedPnL, len(s.positions))

	// Export equity curve
	f, err := os.OpenFile(filepath.Join("output", "equity_curve.csv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		writer := csv.NewWriter(f)
		writer.Write([]string{
			fmt.Sprintf("%d", time.Now().Unix()),
			fmt.Sprintf("%.2f", totalEquity),
			fmt.Sprintf("%.2f", s.balance),
			fmt.Sprintf("%.2f", unrealizedPnL),
			fmt.Sprintf("%.2f", s.realizedPnL),
			fmt.Sprintf("%d", len(s.positions)),
		})
		writer.Flush()
	}

	return map[string]interface{}{
		"accountCash":           s.balance,
		"accountEquity":         totalEquity,
		"availableBalance":      s.balance,
		"grossMarketValue":      lockedPrincipal,
		"unrealizedPnL":         unrealizedPnL,
		"realizedPnL":           s.realizedPnL,
		"totalWalletBalance":    totalWalletBalance,
		"totalUnrealizedProfit": unrealizedPnL,
		"totalEquity":           totalEquity,
	}, nil
}

// GetPositions returns simulated open positions
func (s *SimTrader) GetPositions() ([]map[string]interface{}, error) {
	var result []map[string]interface{}

	for _, pos := range s.positions {
		if pos.Quantity == 0 {
			continue
		}

		currentPrice, err := s.GetMarketPrice(pos.Symbol)
		if err != nil {
			// Skip if price unavailable
			continue
		}

		unrealizedPnL := 0.0
		if pos.Side == "long" {
			unrealizedPnL = (currentPrice - pos.EntryPrice) * pos.Quantity
		} else {
			unrealizedPnL = (pos.EntryPrice - currentPrice) * pos.Quantity
		}

		result = append(result, map[string]interface{}{
			"symbol":           pos.Symbol,
			"side":             pos.Side,
			"positionAmt":      pos.Quantity,
			"entryPrice":       pos.EntryPrice,
			"markPrice":        currentPrice,
			"unRealizedProfit": unrealizedPnL,
			"leverage":         float64(1), // Default 1x for stocks
			"liquidationPrice": float64(0), // No liquidation for now
		})
	}

	return result, nil
}

func (s *SimTrader) openPosition(symbol string, quantity float64, side string) (map[string]interface{}, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("invalid quantity %.6f", quantity)
	}
	bar, err := s.getLatestBar(symbol)
	if err != nil {
		return nil, err
	}
	price := bar.Close
	fillQty, participation := s.applyParticipationCap(quantity, bar.Volume)
	if fillQty <= 0 {
		s.rejectedFills++
		return nil, fmt.Errorf("order rejected for %s: not enough bar liquidity for participation cap", symbol)
	}
	if fillQty < quantity {
		s.partialFills++
	}
	costEstimate := s.currentExecutionCostModel().Estimate(price, fillQty, side, true, participation, true)
	execPrice := costEstimate.EffectivePrice
	notional := fillQty * execPrice
	fees := costEstimate.CommissionUSD
	if notional <= 0 {
		s.rejectedFills++
		return nil, fmt.Errorf("order rejected for %s: invalid notional %.6f", symbol, notional)
	}

	// Keep simulated cash non-negative by reducing fill quantity when required.
	if maxNotional := s.balance / (1.0 + (s.commissionBps / 10000.0)); maxNotional > 0 && notional+fees > s.balance {
		reducedQty := maxNotional / execPrice
		if reducedQty <= 0 {
			s.rejectedFills++
			return nil, fmt.Errorf("order rejected for %s: insufficient balance", symbol)
		}
		if reducedQty < fillQty {
			fillQty = reducedQty
			s.partialFills++
			if bar.Volume > 0 {
				participation = fillQty / bar.Volume
			}
			costEstimate = s.currentExecutionCostModel().Estimate(price, fillQty, side, true, participation, true)
			execPrice = costEstimate.EffectivePrice
			notional = fillQty * execPrice
			fees = costEstimate.CommissionUSD
		}
	}

	key := symbol + "_" + side

	if existing, exists := s.positions[key]; exists {
		// Average down/up
		totalValue := (existing.Quantity * existing.EntryPrice) + (fillQty * execPrice)
		newQuantity := existing.Quantity + fillQty
		existing.EntryPrice = totalValue / newQuantity
		existing.Quantity = newQuantity
	} else {
		// New position
		s.positions[key] = &SimPosition{
			Symbol:     symbol,
			Quantity:   fillQty,
			EntryPrice: execPrice,
			Side:       side,
		}
	}

	// Cost deduction
	s.balance -= notional + fees
	s.realizedPnL -= fees
	s.totalFeesUSD += fees
	s.totalSpreadCostUSD += costEstimate.SpreadCostUSD
	s.totalSlippageCostUSD += costEstimate.SlippageCostUSD
	s.totalImpactCostUSD += costEstimate.ImpactCostUSD
	s.totalExecutionCostUSD += costEstimate.TotalModeledCostUSD

	// Write trade log
	f, err := os.OpenFile(filepath.Join("output", "trades.csv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		writer := csv.NewWriter(f)
		action := "OPEN_LONG"
		if side == "short" {
			action = "OPEN_SHORT"
		}
		writer.Write([]string{
			fmt.Sprintf("%d", time.Now().Unix()),
			symbol,
			action,
			fmt.Sprintf("%.4f", fillQty),
			fmt.Sprintf("%.4f", execPrice),
			"0", // exit_price
			"0", // realized_pnl
			fmt.Sprintf("%.4f", fees),
			fmt.Sprintf("%.5f", participation),
			fmt.Sprintf("%.4f", costEstimate.SpreadBps),
			fmt.Sprintf("%.4f", costEstimate.SlippageBps),
			fmt.Sprintf("%.4f", costEstimate.ImpactBps),
			fmt.Sprintf("%.4f", costEstimate.SpreadCostUSD),
			fmt.Sprintf("%.4f", costEstimate.SlippageCostUSD),
			fmt.Sprintf("%.4f", costEstimate.ImpactCostUSD),
			fmt.Sprintf("%.4f", costEstimate.TotalModeledCostUSD),
			fmt.Sprintf("AI Strategy Signal | requested_qty=%.4f", quantity),
		})
		writer.Flush()
	}

	return map[string]interface{}{
		"orderId":                  time.Now().UnixNano(),
		"status":                   "FILLED",
		"price":                    execPrice,
		"fees":                     fees,
		"filled_qty":               fillQty,
		"requested_qty":            quantity,
		"participation":            participation,
		"spread_bps":               costEstimate.SpreadBps,
		"slippage_bps":             costEstimate.SlippageBps,
		"impact_bps":               costEstimate.ImpactBps,
		"spread_cost":              costEstimate.SpreadCostUSD,
		"slippage_cost":            costEstimate.SlippageCostUSD,
		"impact_cost":              costEstimate.ImpactCostUSD,
		"total_execution_cost_usd": costEstimate.TotalModeledCostUSD,
	}, nil
}

func (s *SimTrader) closePosition(symbol string, quantity float64, side string) (map[string]interface{}, error) {
	key := symbol + "_" + side

	pos, exists := s.positions[key]
	if !exists || pos.Quantity == 0 {
		return nil, fmt.Errorf("no open %s position for %s", side, symbol)
	}

	bar, err := s.getLatestBar(symbol)
	if err != nil {
		return nil, err
	}
	price := bar.Close

	qtyToClose := quantity
	if quantity <= 0 || quantity > pos.Quantity {
		qtyToClose = pos.Quantity
	}
	qtyToClose, participation := s.applyParticipationCap(qtyToClose, bar.Volume)
	if qtyToClose <= 0 {
		s.rejectedFills++
		return nil, fmt.Errorf("close rejected for %s: not enough bar liquidity for participation cap", symbol)
	}
	if quantity > 0 && qtyToClose < quantity {
		s.partialFills++
	}
	costEstimate := s.currentExecutionCostModel().Estimate(price, qtyToClose, side, false, participation, true)
	execPrice := costEstimate.EffectivePrice

	// Calculate realized PnL and update balance
	var realizedPnL float64
	if side == "long" {
		realizedPnL = (execPrice - pos.EntryPrice) * qtyToClose
	} else {
		realizedPnL = (pos.EntryPrice - execPrice) * qtyToClose
	}
	fees := costEstimate.CommissionUSD
	realizedPnLNet := realizedPnL - fees

	s.balance += (pos.EntryPrice * qtyToClose) + realizedPnLNet // Return principal + net PnL
	s.realizedPnL += realizedPnLNet
	s.totalFeesUSD += fees
	s.totalSpreadCostUSD += costEstimate.SpreadCostUSD
	s.totalSlippageCostUSD += costEstimate.SlippageCostUSD
	s.totalImpactCostUSD += costEstimate.ImpactCostUSD
	s.totalExecutionCostUSD += costEstimate.TotalModeledCostUSD
	s.tradePnLs = append(s.tradePnLs, realizedPnLNet)
	pos.Quantity -= qtyToClose

	s.tradeCount++
	if realizedPnLNet > 0 {
		s.winCount++
	} else if realizedPnLNet < 0 {
		s.lossCount++
	}

	if pos.Quantity <= 0 {
		delete(s.positions, key)
	}

	// Write trade log
	f, err := os.OpenFile(filepath.Join("output", "trades.csv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		writer := csv.NewWriter(f)
		action := "CLOSE_LONG"
		if side == "short" {
			action = "CLOSE_SHORT"
		}
		writer.Write([]string{
			fmt.Sprintf("%d", time.Now().Unix()),
			symbol,
			action,
			fmt.Sprintf("%.4f", qtyToClose),
			fmt.Sprintf("%.4f", pos.EntryPrice),
			fmt.Sprintf("%.4f", execPrice),
			fmt.Sprintf("%.2f", realizedPnLNet),
			fmt.Sprintf("%.4f", fees),
			fmt.Sprintf("%.5f", participation),
			fmt.Sprintf("%.4f", costEstimate.SpreadBps),
			fmt.Sprintf("%.4f", costEstimate.SlippageBps),
			fmt.Sprintf("%.4f", costEstimate.ImpactBps),
			fmt.Sprintf("%.4f", costEstimate.SpreadCostUSD),
			fmt.Sprintf("%.4f", costEstimate.SlippageCostUSD),
			fmt.Sprintf("%.4f", costEstimate.ImpactCostUSD),
			fmt.Sprintf("%.4f", costEstimate.TotalModeledCostUSD),
			"AI Strategy Signal",
		})
		writer.Flush()
	}

	return map[string]interface{}{
		"orderId":                  time.Now().UnixNano(),
		"status":                   "FILLED",
		"price":                    execPrice,
		"pnl":                      realizedPnLNet,
		"fees":                     fees,
		"filled_qty":               qtyToClose,
		"participation":            participation,
		"spread_bps":               costEstimate.SpreadBps,
		"slippage_bps":             costEstimate.SlippageBps,
		"impact_bps":               costEstimate.ImpactBps,
		"spread_cost":              costEstimate.SpreadCostUSD,
		"slippage_cost":            costEstimate.SlippageCostUSD,
		"impact_cost":              costEstimate.ImpactCostUSD,
		"total_execution_cost_usd": costEstimate.TotalModeledCostUSD,
	}, nil
}

func (s *SimTrader) applyParticipationCap(quantity, barVolume float64) (float64, float64) {
	if quantity <= 0 {
		return 0, 0
	}
	if barVolume <= 0 || s.maxPartRate <= 0 {
		return quantity, 0
	}
	maxQty := barVolume * s.maxPartRate
	if maxQty <= 0 {
		return 0, 0
	}
	fillQty := quantity
	if fillQty > maxQty {
		fillQty = maxQty
	}
	return fillQty, fillQty / barVolume
}

// OpenLong opens a simulated long
func (s *SimTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return s.openPosition(symbol, quantity, "long")
}

// OpenShort opens a simulated short
func (s *SimTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return s.openPosition(symbol, quantity, "short")
}

// CloseLong closes a simulated long
func (s *SimTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return s.closePosition(symbol, quantity, "long")
}

// CloseShort closes a simulated short
func (s *SimTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return s.closePosition(symbol, quantity, "short")
}

// SetLeverage sets simulated leverage
func (s *SimTrader) SetLeverage(symbol string, leverage int) error {
	return nil // No-op
}

// GetMarketPrice retrieves the latest price from the provider
func (s *SimTrader) GetMarketPrice(symbol string) (float64, error) {
	bar, err := s.getLatestBar(symbol)
	if err != nil {
		return 0, err
	}
	return bar.Close, nil
}

func (s *SimTrader) getLatestBar(symbol string) (market.Kline, error) {
	if s.provider == nil {
		return market.Kline{}, fmt.Errorf("provider not set in SimTrader")
	}

	barsMap, err := s.provider.GetBars([]string{symbol}, "1m", 1)
	if err != nil {
		return market.Kline{}, err
	}

	bars, exists := barsMap[symbol]
	if !exists || len(bars) == 0 {
		return market.Kline{}, fmt.Errorf("no recent price found for %s", symbol)
	}

	return bars[len(bars)-1], nil
}

// SetStopLoss sets a simulated stop loss
func (s *SimTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil // Brackets not simulated locally for now
}

// SetTakeProfit sets a simulated take profit
func (s *SimTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}

// CancelAllOrders cancels all simulated orders
func (s *SimTrader) CancelAllOrders(symbol string) error {
	return nil
}

// FormatQuantity formats quantity natively
func (s *SimTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

// ExportSummary prints and saves the execution summary
func (s *SimTrader) ExportSummary() {
	var winRate float64
	if s.tradeCount > 0 {
		winRate = float64(s.winCount) / float64(s.tradeCount) * 100
	}

	totalEquity := s.balance
	for _, pos := range s.positions {
		totalEquity += pos.EntryPrice * pos.Quantity
		if currentPrice, err := s.GetMarketPrice(pos.Symbol); err == nil {
			if pos.Side == "long" {
				totalEquity += (currentPrice - pos.EntryPrice) * pos.Quantity
			} else {
				totalEquity += (pos.EntryPrice - currentPrice) * pos.Quantity
			}
		}
	}

	returnPct := ((totalEquity - s.initialBal) / s.initialBal) * 100
	sharpe, sortino := computeSharpeSortino(s.equityCurve)
	profitFactor, expectancy, avgWin, avgLoss := computeTradeMetrics(s.tradePnLs)
	costSummary := s.currentEvaluationCostSummary()

	summary := map[string]interface{}{
		"total_trades":            s.tradeCount,
		"win_rate_pct":            winRate,
		"max_drawdown":            s.maxDrawdown * 100,
		"final_equity":            totalEquity,
		"return_pct":              returnPct,
		"sharpe_ratio":            sharpe,
		"sortino_ratio":           sortino,
		"profit_factor":           profitFactor,
		"expectancy_usd":          expectancy,
		"avg_win_usd":             avgWin,
		"avg_loss_usd":            avgLoss,
		"total_fees_usd":          s.totalFeesUSD,
		"partial_fills":           s.partialFills,
		"rejected_fills":          s.rejectedFills,
		"impact_bps_model":        s.impactBps,
		"max_participation":       s.maxPartRate,
		"execution_cost_summary":  costSummary.Summary,
		"execution_cost_model":    costSummary.Model,
		"execution_cost_totals":   costSummary.Totals,
		"execution_cost_warnings": costSummary.Warnings,
	}

	fmt.Println("\n===== REPLAY SUMMARY =====")
	fmt.Printf("Total Trades: %d\n", s.tradeCount)
	fmt.Printf("Win Rate: %.2f%%\n", winRate)
	fmt.Printf("Max Drawdown: %.2f%%\n", s.maxDrawdown*100)
	fmt.Printf("Final Equity: %.2f\n", totalEquity)
	fmt.Printf("Return %%: %.2f%%\n", returnPct)
	fmt.Printf("Sharpe Ratio: %.3f\n", sharpe)
	fmt.Printf("Sortino Ratio: %.3f\n", sortino)
	fmt.Printf("Profit Factor: %.3f\n", profitFactor)
	fmt.Printf("Expectancy (USD): %.2f\n", expectancy)
	fmt.Printf("Total Fees (USD): %.2f\n", s.totalFeesUSD)
	fmt.Printf("Modeled Execution Costs (USD): %.2f\n", costSummary.Totals.ModeledTotalCostUSD)
	fmt.Printf("Partial Fills: %d\n", s.partialFills)
	fmt.Printf("Rejected Fills: %d\n", s.rejectedFills)
	fmt.Println("==========================")

	b, _ := json.MarshalIndent(summary, "", "  ")
	os.WriteFile(filepath.Join("output", "replay_summary.json"), b, 0644)
}

func computeSharpeSortino(equityCurve []float64) (float64, float64) {
	if len(equityCurve) < 3 {
		return 0, 0
	}

	returns := make([]float64, 0, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		prev := equityCurve[i-1]
		curr := equityCurve[i]
		if prev <= 0 {
			continue
		}
		returns = append(returns, (curr-prev)/prev)
	}
	if len(returns) < 2 {
		return 0, 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	downsideVariance := 0.0
	downsideN := 0
	for _, r := range returns {
		d := r - mean
		variance += d * d
		if r < 0 {
			downsideVariance += r * r
			downsideN++
		}
	}
	std := math.Sqrt(variance / float64(len(returns)))
	downsideStd := 0.0
	if downsideN > 0 {
		downsideStd = math.Sqrt(downsideVariance / float64(downsideN))
	}

	annualization := math.Sqrt(252.0)
	sharpe := 0.0
	if std > 0 {
		sharpe = (mean / std) * annualization
	}
	sortino := 0.0
	if downsideStd > 0 {
		sortino = (mean / downsideStd) * annualization
	}

	return sharpe, sortino
}

func computeTradeMetrics(pnls []float64) (profitFactor, expectancy, avgWin, avgLoss float64) {
	if len(pnls) == 0 {
		return 0, 0, 0, 0
	}

	total := 0.0
	grossProfit := 0.0
	grossLoss := 0.0
	winSum := 0.0
	lossSum := 0.0
	winCount := 0
	lossCount := 0

	for _, pnl := range pnls {
		total += pnl
		if pnl > 0 {
			grossProfit += pnl
			winSum += pnl
			winCount++
		} else if pnl < 0 {
			grossLoss += -pnl
			lossSum += pnl
			lossCount++
		}
	}

	expectancy = total / float64(len(pnls))
	if grossLoss > 0 {
		profitFactor = grossProfit / grossLoss
	} else if grossProfit > 0 {
		profitFactor = 999
	}
	if winCount > 0 {
		avgWin = winSum / float64(winCount)
	}
	if lossCount > 0 {
		avgLoss = lossSum / float64(lossCount)
	}

	return profitFactor, expectancy, avgWin, avgLoss
}
