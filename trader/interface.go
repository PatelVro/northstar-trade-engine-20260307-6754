package trader

// Trader standardized interface
// Supports multiple exchanges (Binance, Hyperliquid, etc.)
type Trader interface {
	// GetBalance retrieves account balance
	GetBalance() (map[string]interface{}, error)

	// GetPositions retrieves all positions
	GetPositions() ([]map[string]interface{}, error)

	// OpenLong executes a long position order
	OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// OpenShort executes a short position order
	OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// CloseLong closes a long position (quantity=0 means close all)
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)

	// CloseShort closes a short position (quantity=0 means close all)
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)

	// SetLeverage configures account leverage for a symbol
	SetLeverage(symbol string, leverage int) error

	// GetMarketPrice retrieves the current market price
	GetMarketPrice(symbol string) (float64, error)

	// SetStopLoss configures a stop loss order
	SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error

	// SetTakeProfit configures a take profit order
	SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error

	// CancelAllOrders cancels all open orders for a specific symbol
	CancelAllOrders(symbol string) error

	// FormatQuantity adjusts the quantity string to the required precision
	FormatQuantity(symbol string, quantity float64) (string, error)
}
