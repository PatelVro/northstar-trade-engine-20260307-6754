package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sonirico/go-hyperliquid"
)

// HyperliquidTrader handles Hyperliquid interactions
type HyperliquidTrader struct {
	exchange   *hyperliquid.Exchange
	ctx        context.Context
	walletAddr string
	meta       *hyperliquid.Meta // Cache meta info (includes precision, etc.)
}

// NewHyperliquidTrader creates a new Hyperliquid trader
func NewHyperliquidTrader(privateKeyHex string, walletAddr string, testnet bool) (*HyperliquidTrader, error) {
	// Parse private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Select API URL
	apiURL := hyperliquid.MainnetAPIURL
	if testnet {
		apiURL = hyperliquid.TestnetAPIURL
	}

	// // Generate wallet address from private key
	// pubKey := privateKey.Public()
	// publicKeyECDSA, ok := pubKey.(*ecdsa.PublicKey)
	// if !ok {
	// 	return nil, fmt.Errorf("unable to cast public key")
	// }
	// walletAddr := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

	ctx := context.Background()

	// Create Exchange client (includes Info functionality)
	exchange := hyperliquid.NewExchange(
		ctx,
		privateKey,
		apiURL,
		nil,        // Meta will be fetched automatically
		"",         // vault address (empty for personal account)
		walletAddr, // wallet address
		nil,        // SpotMeta will be fetched automatically
	)

	log.Printf(" Hyperliquid trader initialized successfully (testnet=%v, wallet=%s)", testnet, walletAddr)

	// Get meta info (includes precision and other configs)
	meta, err := exchange.Info().Meta(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get meta info: %w", err)
	}

	return &HyperliquidTrader{
		exchange:   exchange,
		ctx:        ctx,
		walletAddr: walletAddr,
		meta:       meta,
	}, nil
}

// GetBalance retrieves account balance
func (t *HyperliquidTrader) GetBalance() (map[string]interface{}, error) {
	log.Printf(" Calling Hyperliquid API to get account balance...")

	// Get account state
	accountState, err := t.exchange.Info().UserState(t.ctx, t.walletAddr)
	if err != nil {
		log.Printf(" Hyperliquid API call failed: %v", err)
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}

	// Parse margin summary (all fields are strings)
	result := make(map[string]interface{})

	//  DEBUG: Print the raw margin summary from Hyperliquid
	summaryJSON, _ := json.MarshalIndent(accountState.MarginSummary, "  ", "  ")
	log.Printf(" [DEBUG] Hyperliquid API CrossMarginSummary data:")
	log.Printf("%s", string(summaryJSON))

	accountValue, _ := strconv.ParseFloat(accountState.MarginSummary.AccountValue, 64)
	totalMarginUsed, _ := strconv.ParseFloat(accountState.MarginSummary.TotalMarginUsed, 64)

	//  Fix: Accumulate actual unrealized P&L from all positions
	totalUnrealizedPnl := 0.0
	for _, assetPos := range accountState.AssetPositions {
		unrealizedPnl, _ := strconv.ParseFloat(assetPos.Position.UnrealizedPnl, 64)
		totalUnrealizedPnl += unrealizedPnl
	}

	//  Hyperliquid fields mapping:
	// AccountValue = Total equity (includes free capital, position margins, and unrealized P&L)
	// TotalMarginUsed = Total margin allocated to positions (already included in AccountValue, mostly for display)
	//
	// To be compatible with auto_trader.go's calculation logic (totalEquity = walletBalance + unrealizedProfit)
	// we must return the "wallet balance EXCLUDING unrealized P&L"
	walletBalanceWithoutUnrealized := accountValue - totalUnrealizedPnl

	result["totalWalletBalance"] = walletBalanceWithoutUnrealized // wallet balance (excluding unrealized P&L)
	result["availableBalance"] = accountValue - totalMarginUsed   // available balance (total equity - margin used)
	result["totalUnrealizedProfit"] = totalUnrealizedPnl          // unrealized P&L

	log.Printf(" Hyperliquid Account: total equity=%.2f (wallet %.2f + unrealized %.2f), available=%.2f, margin used=%.2f",
		accountValue,
		walletBalanceWithoutUnrealized,
		totalUnrealizedPnl,
		result["availableBalance"],
		totalMarginUsed)

	return result, nil
}

// GetPositions retrieves all positions
func (t *HyperliquidTrader) GetPositions() ([]map[string]interface{}, error) {
	// Get account state
	accountState, err := t.exchange.Info().UserState(t.ctx, t.walletAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var result []map[string]interface{}

	// Iterate through all positions
	for _, assetPos := range accountState.AssetPositions {
		position := assetPos.Position

		// Position quantity (string type)
		posAmt, _ := strconv.ParseFloat(position.Szi, 64)

		if posAmt == 0 {
			continue // Skip empty positions
		}

		posMap := make(map[string]interface{})

		// Standardize symbol format (Hyperliquid uses "BTC", we use "BTCUSDT")
		symbol := position.Coin + "USDT"
		posMap["symbol"] = symbol

		// Position size and side
		if posAmt > 0 {
			posMap["side"] = "long"
			posMap["positionAmt"] = posAmt
		} else {
			posMap["side"] = "short"
			posMap["positionAmt"] = -posAmt // convert to positive
		}

		// Price info (EntryPx and LiquidationPx are pointers)
		var entryPrice, liquidationPx float64
		if position.EntryPx != nil {
			entryPrice, _ = strconv.ParseFloat(*position.EntryPx, 64)
		}
		if position.LiquidationPx != nil {
			liquidationPx, _ = strconv.ParseFloat(*position.LiquidationPx, 64)
		}

		positionValue, _ := strconv.ParseFloat(position.PositionValue, 64)
		unrealizedPnl, _ := strconv.ParseFloat(position.UnrealizedPnl, 64)

		// Compute mark price (positionValue / abs(posAmt))
		var markPrice float64
		if posAmt != 0 {
			markPrice = positionValue / absFloat(posAmt)
		}

		posMap["entryPrice"] = entryPrice
		posMap["markPrice"] = markPrice
		posMap["unRealizedProfit"] = unrealizedPnl
		posMap["leverage"] = float64(position.Leverage.Value)
		posMap["liquidationPrice"] = liquidationPx

		result = append(result, posMap)
	}

	return result, nil
}

// SetLeverage configures account leverage for a symbol
func (t *HyperliquidTrader) SetLeverage(symbol string, leverage int) error {
	// Hyperliquid symbol format (strip USDT suffix)
	coin := convertSymbolToHyperliquid(symbol)

	// Call UpdateLeverage
	_, err := t.exchange.UpdateLeverage(t.ctx, leverage, coin, false) // false = isolated margin
	if err != nil {
		return fmt.Errorf("failed to set leverage: %w", err)
	}

	log.Printf("   %s leverage set to %dx", symbol, leverage)
	return nil
}

// OpenLong executes a long position order
func (t *HyperliquidTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// Cancel all pending orders for the symbol
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   failed to cancel old orders: %v", err)
	}

	// SetLeverage configures account leverage for a symbol
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	// Hyperliquid symbol format (strip USDT suffix)
	coin := convertSymbolToHyperliquid(symbol)

	// Get current price (used for market order approximation)
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)
	log.Printf("   Quantity precision: %.8f -> %.8f (szDecimals=%d)", quantity, roundedQuantity, t.getSzDecimals(coin))

	//  Critical: Price must also be rounded to 5 significant figures
	aggressivePrice := t.roundPriceToSigfigs(price * 1.01)
	log.Printf("   Price precision: %.8f -> %.8f (5 significant figures)", price*1.01, aggressivePrice)

	// Create market buy order (using IOC limit order with aggressive price slippage allowance)
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: true,
		Size:  roundedQuantity, // Use rounded quantity
		Price: aggressivePrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Limit: &hyperliquid.LimitOrderType{
				Tif: hyperliquid.TifIoc, // Immediate or Cancel (acts like market order)
			},
		},
		ReduceOnly: false,
	}

	_, err = t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return nil, fmt.Errorf("Open long failed: %w", err)
	}

	log.Printf(" Long position opened successfully: %s quantity: %.4f", symbol, roundedQuantity)

	result := make(map[string]interface{})
	result["orderId"] = 0 // Hyperliquid doesn't return an order ID directly for this flow
	result["symbol"] = symbol
	result["status"] = "FILLED"

	return result, nil
}

// OpenShort executes a short position order
func (t *HyperliquidTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// Cancel all pending orders for the symbol
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   failed to cancel old orders: %v", err)
	}

	// SetLeverage configures account leverage for a symbol
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	// Hyperliquid symbol format (strip USDT suffix)
	coin := convertSymbolToHyperliquid(symbol)

	// Get market price
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)
	log.Printf("   Quantity precision: %.8f -> %.8f (szDecimals=%d)", quantity, roundedQuantity, t.getSzDecimals(coin))

	//  Critical: Price must also be rounded to 5 significant figures
	aggressivePrice := t.roundPriceToSigfigs(price * 0.99)
	log.Printf("   Price precision: %.8f -> %.8f (5 significant figures)", price*0.99, aggressivePrice)

	// Create market sell order
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: false,
		Size:  roundedQuantity, // Use rounded quantity
		Price: aggressivePrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Limit: &hyperliquid.LimitOrderType{
				Tif: hyperliquid.TifIoc,
			},
		},
		ReduceOnly: false,
	}

	_, err = t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return nil, fmt.Errorf("Open short failed: %w", err)
	}

	log.Printf(" Short position opened successfully: %s quantity: %.4f", symbol, roundedQuantity)

	result := make(map[string]interface{})
	result["orderId"] = 0
	result["symbol"] = symbol
	result["status"] = "FILLED"

	return result, nil
}

// CloseLong closes a long position
func (t *HyperliquidTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	// If quantity is 0, fetch current position size
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "long" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("not found %s's long position", symbol)
		}
	}

	// Hyperliquid symbol format (strip USDT suffix)
	coin := convertSymbolToHyperliquid(symbol)

	// Get market price
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)
	log.Printf("   Quantity precision: %.8f -> %.8f (szDecimals=%d)", quantity, roundedQuantity, t.getSzDecimals(coin))

	//  Critical: Price must also be rounded to 5 significant figures
	aggressivePrice := t.roundPriceToSigfigs(price * 0.99)
	log.Printf("   Price precision: %.8f -> %.8f (5 significant figures)", price*0.99, aggressivePrice)

	// Create close order (Sell + ReduceOnly)
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: false,
		Size:  roundedQuantity, // Use rounded quantity
		Price: aggressivePrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Limit: &hyperliquid.LimitOrderType{
				Tif: hyperliquid.TifIoc,
			},
		},
		ReduceOnly: true, // Only close position, do not open new ones
	}

	_, err = t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return nil, fmt.Errorf("Close long failed: %w", err)
	}

	log.Printf(" Long position closed successfully: %s quantity: %.4f", symbol, roundedQuantity)

	// Cancel all pending orders after closing
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = 0
	result["symbol"] = symbol
	result["status"] = "FILLED"

	return result, nil
}

// CloseShort closes a short position
func (t *HyperliquidTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	// If quantity is 0, fetch current position size
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "short" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("not found %s's short position", symbol)
		}
	}

	// Hyperliquid symbol format (strip USDT suffix)
	coin := convertSymbolToHyperliquid(symbol)

	// Get market price
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)
	log.Printf("   Quantity precision: %.8f -> %.8f (szDecimals=%d)", quantity, roundedQuantity, t.getSzDecimals(coin))

	//  Critical: Price must also be rounded to 5 significant figures
	aggressivePrice := t.roundPriceToSigfigs(price * 1.01)
	log.Printf("   Price precision: %.8f -> %.8f (5 significant figures)", price*1.01, aggressivePrice)

	// Create close order (Buy + ReduceOnly)
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: true,
		Size:  roundedQuantity, // Use rounded quantity
		Price: aggressivePrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Limit: &hyperliquid.LimitOrderType{
				Tif: hyperliquid.TifIoc,
			},
		},
		ReduceOnly: true,
	}

	_, err = t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return nil, fmt.Errorf("Close short failed: %w", err)
	}

	log.Printf(" Short position closed successfully: %s quantity: %.4f", symbol, roundedQuantity)

	// Cancel all pending orders after closing
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = 0
	result["symbol"] = symbol
	result["status"] = "FILLED"

	return result, nil
}

// CancelAllOrders cancels all open orders for a specific symbol
func (t *HyperliquidTrader) CancelAllOrders(symbol string) error {
	coin := convertSymbolToHyperliquid(symbol)

	// Fetch all open orders
	openOrders, err := t.exchange.Info().OpenOrders(t.ctx, t.walletAddr)
	if err != nil {
		return fmt.Errorf("failed to fetch open orders: %w", err)
	}

	// Cancel all pending orders for the symbol
	for _, order := range openOrders {
		if order.Coin == coin {
			_, err := t.exchange.Cancel(t.ctx, coin, order.Oid)
			if err != nil {
				log.Printf("   Failed to cancel order (oid=%d): %v", order.Oid, err)
			}
		}
	}

	log.Printf("   Cancelled all pending orders for %s", symbol)
	return nil
}

// GetMarketPrice retrieves the current market price
func (t *HyperliquidTrader) GetMarketPrice(symbol string) (float64, error) {
	coin := convertSymbolToHyperliquid(symbol)

	// Fetch all market prices
	allMids, err := t.exchange.Info().AllMids(t.ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch price: %w", err)
	}

	// Find price for the specific coin (allMids is map[string]string)
	if priceStr, ok := allMids[coin]; ok {
		priceFloat, err := strconv.ParseFloat(priceStr, 64)
		if err == nil {
			return priceFloat, nil
		}
		return 0, fmt.Errorf("price format error: %v", err)
	}

	return 0, fmt.Errorf("market price not found for %s", symbol)
}

// SetStopLoss configures a stop loss order
func (t *HyperliquidTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	coin := convertSymbolToHyperliquid(symbol)

	isBuy := positionSide == "SHORT" // Short stop loss = buy, Long stop loss = sell

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)

	//  Critical: Price must also be rounded to 5 significant figures
	roundedStopPrice := t.roundPriceToSigfigs(stopPrice)

	// Create stop loss order (Trigger Order)
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: isBuy,
		Size:  roundedQuantity,  // Use rounded quantity
		Price: roundedStopPrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Trigger: &hyperliquid.TriggerOrderType{
				TriggerPx: roundedStopPrice,
				IsMarket:  true,
				Tpsl:      "sl", // stop loss
			},
		},
		ReduceOnly: true,
	}

	_, err := t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return fmt.Errorf("failed to set stop loss: %w", err)
	}

	log.Printf("  Stop loss price set: %.4f", roundedStopPrice)
	return nil
}

// SetTakeProfit configures a take profit order
func (t *HyperliquidTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	coin := convertSymbolToHyperliquid(symbol)

	isBuy := positionSide == "SHORT" // Short take profit = buy, Long take profit = sell

	//  Critical: Round quantity to required precision (szDecimals)
	roundedQuantity := t.roundToSzDecimals(coin, quantity)

	//  Critical: Price must also be rounded to 5 significant figures
	roundedTakeProfitPrice := t.roundPriceToSigfigs(takeProfitPrice)

	// Create take profit order (Trigger Order)
	order := hyperliquid.CreateOrderRequest{
		Coin:  coin,
		IsBuy: isBuy,
		Size:  roundedQuantity,        // Use rounded quantity
		Price: roundedTakeProfitPrice, // Use processed price
		OrderType: hyperliquid.OrderType{
			Trigger: &hyperliquid.TriggerOrderType{
				TriggerPx: roundedTakeProfitPrice,
				IsMarket:  true,
				Tpsl:      "tp", // take profit
			},
		},
		ReduceOnly: true,
	}

	_, err := t.exchange.Order(t.ctx, order, nil)
	if err != nil {
		return fmt.Errorf("failed to set take profit: %w", err)
	}

	log.Printf("  Take profit price set: %.4f", roundedTakeProfitPrice)
	return nil
}

// FormatQuantity adjusts the quantity string to the required precision
func (t *HyperliquidTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	coin := convertSymbolToHyperliquid(symbol)
	szDecimals := t.getSzDecimals(coin)

	// Use szDecimals to format quantity
	formatStr := fmt.Sprintf("%%.%df", szDecimals)
	return fmt.Sprintf(formatStr, quantity), nil
}

// getSzDecimals retrieves the quantity precision for a symbol
func (t *HyperliquidTrader) getSzDecimals(coin string) int {
	if t.meta == nil {
		log.Printf("  meta info is empty, using default precision 4")
		return 4 // default precision
	}

	// Find the coin in meta.Universe
	for _, asset := range t.meta.Universe {
		if asset.Name == coin {
			return asset.SzDecimals
		}
	}

	log.Printf("  Precision info missing for %s, falling back to default precision of 4", coin)
	return 4 // default precision
}

// roundToSzDecimals rounds the quantity to the correct szDecimals precision
func (t *HyperliquidTrader) roundToSzDecimals(coin string, quantity float64) float64 {
	szDecimals := t.getSzDecimals(coin)

	// Calculate multiplier (10^szDecimals)
	multiplier := 1.0
	for i := 0; i < szDecimals; i++ {
		multiplier *= 10.0
	}

	// Round
	return float64(int(quantity*multiplier+0.5)) / multiplier
}

// roundPriceToSigfigs rounds the price to 5 significant figures
// Hyperliquid requires price to use 5 significant figures
func (t *HyperliquidTrader) roundPriceToSigfigs(price float64) float64 {
	if price == 0 {
		return 0
	}

	const sigfigs = 5 // Hyperliquid standard: 5 significant figures

	// Calculate magnitude of price
	var magnitude float64
	if price < 0 {
		magnitude = -price
	} else {
		magnitude = price
	}

	// Calculate required multiplier
	multiplier := 1.0
	for magnitude >= 10 {
		magnitude /= 10
		multiplier /= 10
	}
	for magnitude < 1 {
		magnitude *= 10
		multiplier *= 10
	}

	// Apply significant figures precision
	for i := 0; i < sigfigs-1; i++ {
		multiplier *= 10
	}

	// Round
	rounded := float64(int(price*multiplier+0.5)) / multiplier
	return rounded
}

// convertSymbolToHyperliquid converts standard symbols to Hyperliquid format
// e.g.: "BTCUSDT" -> "BTC"
func convertSymbolToHyperliquid(symbol string) string {
	// Strip USDT suffix
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return symbol[:len(symbol)-4]
	}
	return symbol
}

// absFloat returns the absolute value of a float
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
