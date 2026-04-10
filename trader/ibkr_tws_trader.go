package trader

import (
	"fmt"
	"log"
	"math"
	"northstar/market"
	"northstar/orders"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scmhub/ibapi"
)

// IBKRTWSTrader implements the Trader interface using IB Gateway's TWS API.
// This replaces IBKRTrader (Client Portal REST API) with a socket-based connection
// that doesn't require browser login or session management.
type IBKRTWSTrader struct {
	AccountID     string
	Provider      *market.IBKRTWSProvider
	orderStore    *orders.Store
	orderMu       sync.Mutex
	lifecycleHook func()
	balanceMu     sync.RWMutex
	fallbackCash  float64
	protectMu     sync.Mutex
	protectiveOCA map[string]string
}

func NewIBKRTWSTrader(provider *market.IBKRTWSProvider, accountID string, initialBalance float64) *IBKRTWSTrader {
	if initialBalance <= 0 {
		initialBalance = 100000.0
	}

	trader := &IBKRTWSTrader{
		AccountID:     accountID,
		Provider:      provider,
		orderStore:    orders.NewStore(),
		fallbackCash:  initialBalance,
		protectiveOCA: make(map[string]string),
	}

	go trader.reconcilerLoop()

	return trader
}

func (t *IBKRTWSTrader) reconcilerLoop() {
	ticker := time.NewTicker(3 * time.Second)
	for range ticker.C {
		_ = t.reconcileOrderLifecycle()
	}
}

func (t *IBKRTWSTrader) GetOrderReconciliationSummary() orders.Summary {
	if t.orderStore == nil {
		return orders.Summary{}
	}
	return t.orderStore.SnapshotSummary()
}

func (t *IBKRTWSTrader) SetOrderObserver(observer orders.Observer) {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.SetObserver(observer)
}

func (t *IBKRTWSTrader) LookupOrderRecord(localID, brokerOrderID string) *orders.Record {
	if t.orderStore == nil {
		return nil
	}
	return t.orderStore.Lookup(localID, brokerOrderID)
}

func (t *IBKRTWSTrader) SnapshotOrderStoreState() orders.StoreState {
	if t.orderStore == nil {
		return orders.StoreState{}
	}
	return t.orderStore.SnapshotState()
}

func (t *IBKRTWSTrader) RestoreOrderStoreState(state orders.StoreState) error {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	return t.orderStore.RestoreState(state)
}

func (t *IBKRTWSTrader) SetLifecyclePersistenceHook(hook func()) {
	t.orderMu.Lock()
	t.lifecycleHook = hook
	t.orderMu.Unlock()
}

func (t *IBKRTWSTrader) notifyLifecyclePersistence() {
	t.orderMu.Lock()
	hook := t.lifecycleHook
	t.orderMu.Unlock()
	if hook != nil {
		hook()
	}
}

func (t *IBKRTWSTrader) setFallbackCash(v float64) {
	if v <= 0 {
		return
	}
	t.balanceMu.Lock()
	t.fallbackCash = v
	t.balanceMu.Unlock()
}

func (t *IBKRTWSTrader) getFallbackCash() float64 {
	t.balanceMu.RLock()
	defer t.balanceMu.RUnlock()
	if t.fallbackCash <= 0 {
		return 100000.0
	}
	return t.fallbackCash
}

func (t *IBKRTWSTrader) GetBalance() (map[string]interface{}, error) {
	summary, err := t.Provider.Client.GetAccountSummary()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TWS account summary: %w", err)
	}

	result := make(map[string]interface{})

	accountEquity := t.getFallbackCash()
	if v, ok := summary["netliquidation"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			accountEquity = f
		}
	}
	result["accountEquity"] = accountEquity

	accountCash := t.getFallbackCash()
	if v, ok := summary["totalcashvalue"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			accountCash = f
		}
	} else if v, ok := summary["settledcash"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			accountCash = f
		}
	}
	result["accountCash"] = accountCash
	t.setFallbackCash(accountCash)

	if v, ok := summary["availablefunds"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			result["availableBalance"] = f
		} else {
			result["availableBalance"] = accountCash
		}
	} else {
		result["availableBalance"] = accountCash
	}

	if v, ok := summary["grosspositionvalue"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			result["grossMarketValue"] = math.Abs(f)
		} else {
			result["grossMarketValue"] = 0.0
		}
	} else {
		result["grossMarketValue"] = 0.0
	}

	if v, ok := summary["unrealizedpnl"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			result["unrealizedPnL"] = f
		} else {
			result["unrealizedPnL"] = 0.0
		}
	} else {
		result["unrealizedPnL"] = 0.0
	}

	if v, ok := summary["realizedpnl"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			result["realizedPnL"] = f
		} else {
			result["realizedPnL"] = 0.0
		}
	} else {
		result["realizedPnL"] = 0.0
	}

	result["totalWalletBalance"] = accountCash
	result["totalUnrealizedProfit"] = result["unrealizedPnL"]
	result["totalEquity"] = accountEquity

	return result, nil
}

func (t *IBKRTWSTrader) GetPositions() ([]map[string]interface{}, error) {
	rawPositions, err := t.Provider.Client.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch TWS positions: %w", err)
	}

	var positions []map[string]interface{}
	for _, p := range rawPositions {
		if p.Position == 0 {
			continue
		}

		symbol := strings.TrimSpace(p.Contract.Symbol)
		if symbol == "" {
			symbol = fmt.Sprintf("%d", p.Contract.ConID)
		}

		side := "long"
		if p.Position < 0 {
			side = "short"
		}

		positions = append(positions, map[string]interface{}{
			"symbol":           symbol,
			"side":             side,
			"positionAmt":      p.Position,
			"entryPrice":       p.AvgCost,
			"markPrice":        0.0, // TWS positions don't include mark price directly
			"unRealizedProfit": 0.0, // Would need separate PnL request
			"leverage":         1.0,
			"liquidationPrice": 0.0,
		})
	}

	return positions, nil
}

func (t *IBKRTWSTrader) CreateOrder(symbol string, side string, intent orders.Intent, positionSide string, price float64, quantity float64, leverage int, takeProfit string, stopLoss string) (map[string]interface{}, error) {
	conID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ConID for %s: %w", symbol, err)
	}

	ibkrSide := "BUY"
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		ibkrSide = "SELL"
	}

	if quantity <= 0 {
		return nil, fmt.Errorf("invalid quantity: %f", quantity)
	}
	wholeQty := math.Floor(quantity)
	if wholeQty < 1 {
		return nil, fmt.Errorf("quantity too small after whole-share normalization: %.4f", quantity)
	}

	contract := ibapi.Contract{
		ConID:    int64(conID),
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}

	order := ibapi.NewOrder()
	order.Action = ibkrSide
	order.TotalQuantity = ibapi.StringToDecimal(fmt.Sprintf("%.0f", wholeQty))
	order.OrderType = "MKT"
	order.TIF = "DAY"
	order.OutsideRTH = false

	if price > 0 {
		order.OrderType = "LMT"
		order.LmtPrice = price
	}

	log.Printf("TWS: Placing entry order for %.0f %s %s...", wholeQty, symbol, ibkrSide)

	entryLocalID := t.registerLocalOrder(intent, symbol, ibkrSide, positionSide, wholeQty, time.Now())
	orderID, err := t.Provider.Client.PlaceOrder(contract, *order)
	if err != nil {
		t.recordLocalOrderReject(entryLocalID, err)
		return nil, fmt.Errorf("entry order failed: %w", err)
	}

	log.Printf(" TWS: Entry order submitted with broker ID %d", orderID)
	_ = t.reconcileOrderLifecycle()

	result := t.localExecutionResult(entryLocalID, symbol, wholeQty, leverage)
	if strings.TrimSpace(takeProfit) != "" || strings.TrimSpace(stopLoss) != "" {
		result["protection_pending"] = true
		result["protection_message"] = "protective orders must wait for broker-confirmed entry fill"
	}
	return result, nil
}

func (t *IBKRTWSTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.CreateOrder(symbol, "long", orders.IntentEntryLong, "long", 0, quantity, leverage, "", "")
}

func (t *IBKRTWSTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.CreateOrder(symbol, "short", orders.IntentEntryShort, "short", 0, quantity, leverage, "", "")
}

func (t *IBKRTWSTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.ClosePosition(symbol, "long")
}

func (t *IBKRTWSTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.ClosePosition(symbol, "short")
}

func (t *IBKRTWSTrader) ClosePosition(symbol string, side string) (map[string]interface{}, error) {
	log.Printf("TWS: Flattening position for %s", symbol)
	positions, err := t.GetPositions()
	if err != nil {
		return nil, err
	}

	var qty float64
	targetSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	for _, p := range positions {
		posSymbol := strings.ToUpper(strings.TrimSpace(toString(p["symbol"])))
		if posSymbol == targetSymbol {
			qty = toFloat(p["positionAmt"])
			break
		}
	}

	if qty == 0 {
		return nil, fmt.Errorf("no active position found for %s to close", symbol)
	}

	closeSide := "long"
	if qty > 0 {
		closeSide = "short"
	} else {
		qty = -qty
	}

	intent := orders.IntentExitLong
	positionSide := "long"
	if closeSide == "long" {
		intent = orders.IntentExitShort
		positionSide = "short"
	}
	return t.CreateOrder(symbol, closeSide, intent, positionSide, 0, qty, 1, "", "")
}

func (t *IBKRTWSTrader) SetLeverage(symbol string, leverage int) error {
	return nil // IBKR handles margin natively
}

func (t *IBKRTWSTrader) GetMarketPrice(symbol string) (float64, error) {
	quote, err := t.Provider.GetLatestQuote(symbol)
	if err != nil {
		return 0, err
	}
	if quote.AskPrice > 0 {
		return quote.AskPrice, nil
	}
	return quote.BidPrice, nil
}

func (t *IBKRTWSTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	if stopPrice <= 0 || quantity <= 0 {
		return nil
	}
	conID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return fmt.Errorf("failed to resolve ConID for %s stop-loss: %w", symbol, err)
	}
	qtyValue, err := wholeStockQty(quantity)
	if err != nil {
		return fmt.Errorf("invalid stop-loss quantity for %s: %w", symbol, err)
	}
	exitSide, err := protectiveExitSide(positionSide)
	if err != nil {
		return err
	}
	ocaGroup := t.protectiveGroup(symbol, positionSide)

	contract := ibapi.Contract{
		ConID:    int64(conID),
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}
	order := ibapi.NewOrder()
	order.Action = exitSide
	order.TotalQuantity = ibapi.StringToDecimal(fmt.Sprintf("%d", qtyValue))
	order.OrderType = "STP"
	order.AuxPrice = stopPrice
	order.TIF = "GTC"
	order.OutsideRTH = false
	order.OCAGroup = ocaGroup

	localID := t.registerLocalOrder(protectiveIntent(strings.ToLower(positionSide), "stop"), symbol, exitSide, strings.ToLower(positionSide), float64(qtyValue), time.Now())
	orderID, err := t.Provider.Client.PlaceOrder(contract, *order)
	if err != nil {
		t.recordLocalOrderReject(localID, err)
		return fmt.Errorf("failed to submit stop-loss for %s: %w", symbol, err)
	}
	_ = t.reconcileOrderLifecycle()
	log.Printf("TWS: Submitted stop-loss for %s at %.4f (qty=%d side=%s oca=%s brokerID=%d)", symbol, stopPrice, qtyValue, exitSide, ocaGroup, orderID)
	return nil
}

func (t *IBKRTWSTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	if takeProfitPrice <= 0 || quantity <= 0 {
		return nil
	}
	conID, err := t.Provider.Client.ResolveContract(symbol)
	if err != nil {
		return fmt.Errorf("failed to resolve ConID for %s take-profit: %w", symbol, err)
	}
	qtyValue, err := wholeStockQty(quantity)
	if err != nil {
		return fmt.Errorf("invalid take-profit quantity for %s: %w", symbol, err)
	}
	exitSide, err := protectiveExitSide(positionSide)
	if err != nil {
		return err
	}
	ocaGroup := t.protectiveGroup(symbol, positionSide)

	contract := ibapi.Contract{
		ConID:    int64(conID),
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}
	order := ibapi.NewOrder()
	order.Action = exitSide
	order.TotalQuantity = ibapi.StringToDecimal(fmt.Sprintf("%d", qtyValue))
	order.OrderType = "LMT"
	order.LmtPrice = takeProfitPrice
	order.TIF = "GTC"
	order.OutsideRTH = false
	order.OCAGroup = ocaGroup

	localID := t.registerLocalOrder(protectiveIntent(strings.ToLower(positionSide), "target"), symbol, exitSide, strings.ToLower(positionSide), float64(qtyValue), time.Now())
	orderID, err := t.Provider.Client.PlaceOrder(contract, *order)
	if err != nil {
		t.recordLocalOrderReject(localID, err)
		return fmt.Errorf("failed to submit take-profit for %s: %w", symbol, err)
	}
	_ = t.reconcileOrderLifecycle()
	log.Printf("TWS: Submitted take-profit for %s at %.4f (qty=%d side=%s oca=%s brokerID=%d)", symbol, takeProfitPrice, qtyValue, exitSide, ocaGroup, orderID)
	return nil
}

func (t *IBKRTWSTrader) CancelAllOrders(symbol string) error {
	t.Provider.Client.CancelAllOrders()
	log.Printf("TWS: Cancelled all open orders for account %s", t.AccountID)
	return nil
}

func (t *IBKRTWSTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

func (t *IBKRTWSTrader) GetLiveOrders() ([]map[string]interface{}, error) {
	openOrders, err := t.Provider.Client.GetOpenOrders()
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, o := range openOrders {
		result = append(result, map[string]interface{}{
			"orderId":  fmt.Sprintf("%d", o.OrderID),
			"symbol":   o.Contract.Symbol,
			"side":     o.Order.Action,
			"status":   o.State.Status,
			"quantity": o.Order.TotalQuantity.Float(),
		})
	}
	return result, nil
}

func (t *IBKRTWSTrader) reconcileOrderLifecycle() error {
	openOrders, err := t.GetLiveOrders()
	if err != nil {
		if t.orderStore != nil {
			t.orderStore.RecordReconciliationError(err, time.Now())
		}
		t.notifyLifecyclePersistence()
		return err
	}
	positions, err := t.GetPositions()
	if err != nil {
		if t.orderStore != nil {
			t.orderStore.RecordReconciliationError(err, time.Now())
		}
		t.notifyLifecyclePersistence()
		return err
	}
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	result := t.orderStore.Reconcile(toBrokerOrders(openOrders, time.Now()), toOrderPositions(positions), time.Now())
	if result.Mismatches > 0 {
		log.Printf(" TWS order reconciliation: %s", result.Summary)
		for _, issue := range result.Issues {
			log.Printf(" TWS order issue [%s]: %s", issue.Type, issue.Message)
		}
	}
	t.notifyLifecyclePersistence()
	return nil
}

func (t *IBKRTWSTrader) ReconcileBrokerState() (*IBKRBrokerSnapshot, error) {
	balance, err := t.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("account summary refresh failed: %w", err)
	}
	positions, err := t.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("positions refresh failed: %w", err)
	}
	openOrders, err := t.GetLiveOrders()
	if err != nil {
		return nil, fmt.Errorf("open orders refresh failed: %w", err)
	}
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	t.orderStore.Reconcile(toBrokerOrders(openOrders, time.Now()), toOrderPositions(positions), time.Now())
	t.notifyLifecyclePersistence()
	log.Printf(" TWS reconciliation refreshed account summary, %d positions, %d open orders", len(positions), len(openOrders))

	return &IBKRBrokerSnapshot{
		Balance:    balance,
		Positions:  positions,
		OpenOrders: openOrders,
	}, nil
}

func (t *IBKRTWSTrader) registerLocalOrder(intent orders.Intent, symbol, side, positionSide string, qty float64, at time.Time) string {
	if t.orderStore == nil {
		t.orderStore = orders.NewStore()
	}
	return t.orderStore.RegisterSubmitted(intent, symbol, side, positionSide, qty, at)
}

func (t *IBKRTWSTrader) localExecutionResult(localID, symbol string, quantity float64, leverage int) map[string]interface{} {
	result := map[string]interface{}{
		"status":       "submitted",
		"symbol":       strings.ToUpper(strings.TrimSpace(symbol)),
		"quantity":     quantity,
		"leverage":     leverage,
		"localOrderId": strings.TrimSpace(localID),
	}
	record := t.LookupOrderRecord(localID, "")
	if record == nil {
		return result
	}
	result["status"] = string(record.Status)
	if strings.TrimSpace(record.BrokerOrderID) != "" {
		result["brokerOrderId"] = strings.TrimSpace(record.BrokerOrderID)
		result["orderId"] = strings.TrimSpace(record.BrokerOrderID)
	}
	if record.FilledQty > 0 {
		result["filled_qty"] = record.FilledQty
		result["filledQty"] = record.FilledQty
	}
	if record.AvgFillPrice > 0 {
		result["avg_fill_price"] = record.AvgFillPrice
		result["average_fill_price"] = record.AvgFillPrice
		result["price"] = record.AvgFillPrice
	}
	if strings.TrimSpace(record.RawBrokerStatus) != "" {
		result["order_status"] = strings.TrimSpace(record.RawBrokerStatus)
	}
	return result
}

func (t *IBKRTWSTrader) recordLocalOrderReject(localID string, err error) {
	if t.orderStore == nil || strings.TrimSpace(localID) == "" || err == nil {
		return
	}
	t.orderStore.MarkRejected(localID, err.Error(), time.Now())
}

func (t *IBKRTWSTrader) protectiveGroup(symbol, positionSide string) string {
	key := strings.ToUpper(strings.TrimSpace(symbol)) + ":" + strings.ToUpper(strings.TrimSpace(positionSide))
	t.protectMu.Lock()
	defer t.protectMu.Unlock()
	if g, ok := t.protectiveOCA[key]; ok && strings.TrimSpace(g) != "" {
		return g
	}
	group := fmt.Sprintf("NS_PROTECT_%s_%d", strings.ReplaceAll(key, ":", "_"), time.Now().UnixNano())
	t.protectiveOCA[key] = group
	return group
}
