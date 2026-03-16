package trader

import (
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/shopspring/decimal"
)

// AlpacaTrader implements Trader interface using Alpaca API
type AlpacaTrader struct {
	client     *alpaca.Client
	mdClient   *marketdata.Client
	paper      bool
	instrument string
}

// NewAlpacaTrader creates a new Alpaca trader instance
func NewAlpacaTrader(apiKey, apiSecret string, paper bool, instrumentType string) *AlpacaTrader {
	baseURL := "https://paper-api.alpaca.markets"
	if !paper {
		baseURL = "https://api.alpaca.markets"
	}

	client := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	})

	mdClient := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})

	return &AlpacaTrader{
		client:     client,
		mdClient:   mdClient,
		paper:      paper,
		instrument: instrumentType,
	}
}

// GetBalance returns account balance
func (a *AlpacaTrader) GetBalance() (map[string]interface{}, error) {
	account, err := a.client.GetAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to get alpaca account: %w", err)
	}

	equity := account.Equity.InexactFloat64()
	cash := account.Cash.InexactFloat64()
	buyingPower := account.BuyingPower.InexactFloat64()
	longMarketValue := account.LongMarketValue.InexactFloat64()
	shortMarketValue := account.ShortMarketValue.InexactFloat64()

	return map[string]interface{}{
		"accountCash":           cash,
		"accountEquity":         equity,
		"availableBalance":      buyingPower,
		"grossMarketValue":      math.Abs(longMarketValue) + math.Abs(shortMarketValue),
		"unrealizedPnL":         0.0,
		"realizedPnL":           0.0,
		"totalWalletBalance":    cash,
		"totalUnrealizedProfit": 0.0,
		"totalEquity":           equity,
	}, nil
}

// GetPositions returns all open positions
func (a *AlpacaTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := a.client.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get alpaca positions: %w", err)
	}

	var result []map[string]interface{}
	for _, p := range positions {
		side := "long"
		if string(p.Side) == "short" {
			side = "short"
		}

		qty := p.Qty.InexactFloat64()
		if qty < 0 {
			qty = -qty
		}

		var unrealizedPnL float64
		if p.UnrealizedPL != nil {
			unrealizedPnL = p.UnrealizedPL.InexactFloat64()
		}

		result = append(result, map[string]interface{}{
			"symbol":           p.Symbol,
			"side":             side,
			"positionAmt":      qty,
			"entryPrice":       p.AvgEntryPrice.InexactFloat64(),
			"markPrice":        p.CurrentPrice.InexactFloat64(),
			"unRealizedProfit": unrealizedPnL,
			"leverage":         float64(1), // Default 1x for equity
			"liquidationPrice": float64(0), // Not provided directly
		})
	}

	return result, nil
}

// checkMarketOpen blocks orders if the market is closed
func (a *AlpacaTrader) checkMarketOpen() error {
	clock, err := a.client.GetClock()
	if err != nil {
		return fmt.Errorf("could not check market clock: %w", err)
	}
	if !clock.IsOpen {
		return fmt.Errorf("market is closed, next open is %v", clock.NextOpen.Format("2006-01-02 15:04 MST"))
	}
	return nil
}

// OpenLong opens a long position
func (a *AlpacaTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	if err := a.checkMarketOpen(); err != nil {
		return nil, err
	}

	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	qty := decimal.NewFromFloat(quantity)

	req := alpaca.PlaceOrderRequest{
		Symbol:      cleanSymbol,
		Side:        alpaca.Buy,
		Type:        alpaca.Market,
		TimeInForce: alpaca.Day,
		Qty:         &qty,
	}

	order, err := a.client.PlaceOrder(req)
	if err != nil {
		return nil, fmt.Errorf("failed to place alpaca open long order: %w", err)
	}

	return map[string]interface{}{
		"orderId": order.ID,
		"status":  order.Status,
	}, nil
}

// OpenShort opens a short position, ensuring capability requirements
func (a *AlpacaTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	if err := a.checkMarketOpen(); err != nil {
		return nil, err
	}

	// Fetch Account to check margin/short capabilities
	account, err := a.client.GetAccount()
	if err != nil {
		return nil, err
	}

	if !account.ShortingEnabled {
		return nil, fmt.Errorf("shorting is not enabled on this Alpaca account")
	}

	if account.Multiplier.String() == "1" {
		return nil, fmt.Errorf("margin is required for shorting but account multiplier is 1x")
	}

	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	// Double check the asset is shortable
	asset, err := a.client.GetAsset(cleanSymbol)
	if err != nil {
		return nil, err
	}

	if !asset.Shortable {
		return nil, fmt.Errorf("asset %s is not shortable", cleanSymbol)
	}

	qty := decimal.NewFromFloat(quantity)

	req := alpaca.PlaceOrderRequest{
		Symbol:      cleanSymbol,
		Side:        alpaca.Sell,
		Type:        alpaca.Market,
		TimeInForce: alpaca.Day,
		Qty:         &qty,
	}

	order, err := a.client.PlaceOrder(req)
	if err != nil {
		return nil, fmt.Errorf("failed to place alpaca open short order: %w", err)
	}

	return map[string]interface{}{
		"orderId": order.ID,
		"status":  order.Status,
	}, nil
}

// CloseLong closes a long position
func (a *AlpacaTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	if err := a.checkMarketOpen(); err != nil {
		// Sometimes Alpaca allows closing during extended hours if specifically routed,
		// but by default we'll restrict to regular hours for simplicity.
		return nil, err
	}

	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	var order *alpaca.Order
	var err error

	if quantity == 0 {
		// Close entire position (Alpaca has a native endpoint for this)
		order, err = a.client.ClosePosition(cleanSymbol, alpaca.ClosePositionRequest{})
	} else {
		qty := decimal.NewFromFloat(quantity)
		req := alpaca.PlaceOrderRequest{
			Symbol:      cleanSymbol,
			Side:        alpaca.Sell,
			Type:        alpaca.Market,
			TimeInForce: alpaca.Day,
			Qty:         &qty,
		}
		order, err = a.client.PlaceOrder(req)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to place alpaca close long order: %w", err)
	}

	return map[string]interface{}{
		"orderId": order.ID,
		"status":  order.Status,
	}, nil
}

// CloseShort closes a short position (Buy to cover)
func (a *AlpacaTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	if err := a.checkMarketOpen(); err != nil {
		return nil, err
	}

	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	var order *alpaca.Order
	var err error

	if quantity == 0 {
		// Close entire position natively
		order, err = a.client.ClosePosition(cleanSymbol, alpaca.ClosePositionRequest{})
	} else {
		qty := decimal.NewFromFloat(quantity)
		req := alpaca.PlaceOrderRequest{
			Symbol:      cleanSymbol,
			Side:        alpaca.Buy, // Buy to cover
			Type:        alpaca.Market,
			TimeInForce: alpaca.Day,
			Qty:         &qty,
		}
		order, err = a.client.PlaceOrder(req)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to place alpaca close short order: %w", err)
	}

	return map[string]interface{}{
		"orderId": order.ID,
		"status":  order.Status,
	}, nil
}

// SetLeverage sets leverage (no-op for Alpaca equities)
func (a *AlpacaTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

// GetMarketPrice retrieves the latest price
func (a *AlpacaTrader) GetMarketPrice(symbol string) (float64, error) {
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	quote, err := a.mdClient.GetLatestQuote(cleanSymbol, marketdata.GetLatestQuoteRequest{})
	if err != nil {
		return 0, fmt.Errorf("failed to fetch latest quote for %s: %w", symbol, err)
	}

	return quote.AskPrice, nil // Returning ask as a conservative market price estimate
}

// SetStopLoss sets a stop loss order.
// Note: In Alpaca, brackets are usually sent WITH the open order.
// Doing it post-fill requires fetching the position and creating an independent stop order.
func (a *AlpacaTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	side := alpaca.Sell
	if positionSide == "SHORT" {
		side = alpaca.Buy
	}

	qty := decimal.NewFromFloat(quantity)
	sp := decimal.NewFromFloat(stopPrice)

	req := alpaca.PlaceOrderRequest{
		Symbol:      cleanSymbol,
		Side:        side,
		Type:        alpaca.Stop,
		TimeInForce: alpaca.Day,
		Qty:         &qty,
		StopPrice:   &sp,
	}

	_, err := a.client.PlaceOrder(req)
	if err != nil {
		log.Printf("Alpaca SetStopLoss failed: %v", err)
		return err
	}

	return nil
}

// SetTakeProfit sets a take profit order
func (a *AlpacaTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	side := alpaca.Sell
	if positionSide == "SHORT" {
		side = alpaca.Buy
	}

	qty := decimal.NewFromFloat(quantity)
	tp := decimal.NewFromFloat(takeProfitPrice)

	req := alpaca.PlaceOrderRequest{
		Symbol:      cleanSymbol,
		Side:        side,
		Type:        alpaca.Limit,
		TimeInForce: alpaca.Day,
		Qty:         &qty,
		LimitPrice:  &tp,
	}

	_, err := a.client.PlaceOrder(req)
	if err != nil {
		log.Printf("Alpaca SetTakeProfit failed: %v", err)
		return err
	}

	return nil
}

// CancelAllOrders cancels all open orders for a symbol
func (a *AlpacaTrader) CancelAllOrders(symbol string) error {
	cleanSymbol := strings.TrimSuffix(strings.ToUpper(symbol), "USDT")

	orders, err := a.client.GetOrders(alpaca.GetOrdersRequest{
		Status:  "open",
		Symbols: []string{cleanSymbol},
	})

	if err != nil {
		return err
	}

	for _, order := range orders {
		if err := a.client.CancelOrder(order.ID); err != nil {
			log.Printf("Failed to cancel order %s: %v", order.ID, err)
		}
	}

	return nil
}

// FormatQuantity formats quantity natively
func (a *AlpacaTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	// Let Alpaca SDK handle float formatting
	return fmt.Sprintf("%f", quantity), nil
}
