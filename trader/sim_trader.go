package trader

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"aegistrade/market"
	"os"
	"path/filepath"
	"time"
)

// SimPosition represents a simulated position
type SimPosition struct {
	Symbol       string
	Quantity     float64
	EntryPrice   float64
	Side         string // "long" or "short"
}

// SimTrader implements Trader interface but executes "paper fills" against the local provider.
type SimTrader struct {
	balance      float64
	initialBal   float64
	provider     market.BarsProvider
	positions    map[string]*SimPosition
	realizedPnL  float64
	tradeCount   int
	winCount     int
	lossCount    int
	maxDrawdown  float64
	peakEquity   float64
}

// NewSimTrader creates a new simulated broker for testing
func NewSimTrader(initialBalance float64, provider market.BarsProvider) *SimTrader {
	os.MkdirAll("output", os.ModePerm)
	// Initialize trades file headers
	if f, err := os.Create(filepath.Join("output", "trades.csv")); err == nil {
		f.WriteString("timestamp,symbol,action,quantity,entry_price,exit_price,realized_pnl,reason\n")
		f.Close()
	}
	// Initialize equity curve headers
	if f, err := os.Create(filepath.Join("output", "equity_curve.csv")); err == nil {
		f.WriteString("timestamp,equity,cash,unrealized_pnl,realized_pnl,position_count\n")
		f.Close()
	}

	return &SimTrader{
		balance:    initialBalance,
		initialBal: initialBalance,
		provider:   provider,
		positions:  make(map[string]*SimPosition),
		peakEquity: initialBalance,
	}
}

// GetBalance returns simulated account balance
func (s *SimTrader) GetBalance() (map[string]interface{}, error) {
	unrealizedPnL := 0.0

	for _, pos := range s.positions {
		currentPrice, err := s.GetMarketPrice(pos.Symbol)
		if err == nil {
			if pos.Side == "long" {
				unrealizedPnL += (currentPrice - pos.EntryPrice) * pos.Quantity
			} else {
				unrealizedPnL += (pos.EntryPrice - currentPrice) * pos.Quantity
			}
		}
	}

	totalEquity := s.balance + unrealizedPnL

	if totalEquity > s.peakEquity {
		s.peakEquity = totalEquity
	}
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
		"totalWalletBalance":    s.balance,
		"totalUnrealizedProfit": unrealizedPnL,
		"availableBalance":      s.balance, 
		"totalEquity":           totalEquity,
	}, nil
}

// GetPositions returns simulated open positions
func (s *SimTrader) GetPositions() ([]map[string]interface{}, error) {
	var result []map[string]interface{}

	for symbol, pos := range s.positions {
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
			"symbol":           symbol,
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
	price, err := s.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	key := symbol + "_" + side
	
	if existing, exists := s.positions[key]; exists {
		// Average down/up
		totalValue := (existing.Quantity * existing.EntryPrice) + (quantity * price)
		newQuantity := existing.Quantity + quantity
		existing.EntryPrice = totalValue / newQuantity
		existing.Quantity = newQuantity
	} else {
		// New position
		s.positions[key] = &SimPosition{
			Symbol:     symbol,
			Quantity:   quantity,
			EntryPrice: price,
			Side:       side,
		}
	}

	// Cost deduction
	cost := quantity * price
	s.balance -= cost 

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
			fmt.Sprintf("%.4f", quantity),
			fmt.Sprintf("%.4f", price),
			"0", // exit_price
			"0", // realized_pnl
			"AI Strategy Signal",
		})
		writer.Flush()
	}

	return map[string]interface{}{
		"orderId": time.Now().UnixNano(),
		"status":  "FILLED",
		"price":   price,
	}, nil
}

func (s *SimTrader) closePosition(symbol string, quantity float64, side string) (map[string]interface{}, error) {
	key := symbol + "_" + side
	
	pos, exists := s.positions[key]
	if !exists || pos.Quantity == 0 {
		return nil, fmt.Errorf("no open %s position for %s", side, symbol)
	}

	price, err := s.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	qtyToClose := quantity
	if quantity <= 0 || quantity > pos.Quantity {
		qtyToClose = pos.Quantity
	}

	// Calculate realized PnL and update balance
	var realizedPnL float64
	if side == "long" {
		realizedPnL = (price - pos.EntryPrice) * qtyToClose
	} else {
		realizedPnL = (pos.EntryPrice - price) * qtyToClose
	}

	s.balance += (pos.EntryPrice * qtyToClose) + realizedPnL // Return principal + PnL
	s.realizedPnL += realizedPnL
	pos.Quantity -= qtyToClose
	
	s.tradeCount++
	if realizedPnL > 0 {
		s.winCount++
	} else if realizedPnL < 0 {
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
			fmt.Sprintf("%.4f", price),
			fmt.Sprintf("%.2f", realizedPnL),
			"AI Strategy Signal",
		})
		writer.Flush()
	}

	return map[string]interface{}{
		"orderId": time.Now().UnixNano(),
		"status":  "FILLED",
		"price":   price,
		"pnl":     realizedPnL,
	}, nil
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
	if s.provider == nil {
		return 0, fmt.Errorf("provider not set in SimTrader")
	}

	barsMap, err := s.provider.GetBars([]string{symbol}, "1m", 1)
	if err != nil {
		return 0, err
	}

	bars, exists := barsMap[symbol]
	if !exists || len(bars) == 0 {
		return 0, fmt.Errorf("no recent price found for %s", symbol)
	}

	return bars[len(bars)-1].Close, nil
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
		if currentPrice, err := s.GetMarketPrice(pos.Symbol); err == nil {
			if pos.Side == "long" {
				totalEquity += (currentPrice - pos.EntryPrice) * pos.Quantity
			} else {
				totalEquity += (pos.EntryPrice - currentPrice) * pos.Quantity
			}
		}
	}

	returnPct := ((totalEquity - s.initialBal) / s.initialBal) * 100

	summary := map[string]interface{}{
		"total_trades": s.tradeCount,
		"win_rate_pct": winRate,
		"max_drawdown": s.maxDrawdown * 100,
		"final_equity": totalEquity,
		"return_pct":   returnPct,
	}

	fmt.Println("\n===== REPLAY SUMMARY =====")
	fmt.Printf("Total Trades: %d\n", s.tradeCount)
	fmt.Printf("Win Rate: %.2f%%\n", winRate)
	fmt.Printf("Max Drawdown: %.2f%%\n", s.maxDrawdown*100)
	fmt.Printf("Final Equity: %.2f\n", totalEquity)
	fmt.Printf("Return %%: %.2f%%\n", returnPct)
	fmt.Println("==========================")

	b, _ := json.MarshalIndent(summary, "", "  ")
	os.WriteFile(filepath.Join("output", "replay_summary.json"), b, 0644)
}
