package broker

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scmhub/ibapi"
)

// IBKRTWSClient is a socket-based TWS API client for IB Gateway.
// It replaces IBKRClient (Client Portal REST API) with a persistent
// socket connection that doesn't require browser login or session tickles.
type IBKRTWSClient struct {
	Host      string
	Port      int
	ClientID  int64
	AccountID string

	client  *ibapi.EClient
	wrapper *twsWrapper

	mu         sync.RWMutex
	connected  atomic.Bool
	conIDCache map[string]int
	cacheMu    sync.RWMutex

	// acctSummaryMu serializes GetAccountSummary calls to prevent TWS error 322
	// (max account summary subscriptions exceeded).
	acctSummaryMu    sync.Mutex
	acctSummaryCache map[string]string
	acctSummaryCacheTime time.Time

	// nextReqID is atomically incremented for each API request.
	nextReqID atomic.Int64
}

// twsWrapper implements the ibapi.EWrapper interface to receive callbacks from TWS.
type twsWrapper struct {
	ibapi.Wrapper // embed default no-op implementation

	// Channels for synchronous request/response patterns.
	accountUpdate      chan accountUpdateItem
	accountUpdateEnd   chan string
	positions          chan positionItem
	positionsEnd       chan struct{}
	contractDetails    chan contractDetailsItem
	contractDetailsEnd chan int64
	historicalData     chan historicalDataItem
	historicalDataEnd  chan int64
	tickPrice          chan tickPriceItem
	orderStatus        chan orderStatusItem
	openOrderEnd       chan struct{}
	openOrder          chan openOrderItem
	nextValidID        chan int64
	errChan            chan twsError

	mu sync.Mutex
}

type accountUpdateItem struct {
	Tag      string
	Value    string
	Currency string
	Account  string
}

type positionItem struct {
	Account  string
	Contract ibapi.Contract
	Position float64
	AvgCost  float64
}

type contractDetailsItem struct {
	ReqID   int64
	Details ibapi.ContractDetails
}

type historicalDataItem struct {
	ReqID  int64
	Date   string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type tickPriceItem struct {
	ReqID   int64
	Field   int64
	Price   float64
	Attribs ibapi.TickAttrib
}

type orderStatusItem struct {
	OrderID   int64
	Status    string
	Filled    float64
	Remaining float64
	AvgPrice  float64
}

type openOrderItem struct {
	OrderID  int64
	Contract ibapi.Contract
	Order    ibapi.Order
	State    ibapi.OrderState
}

type twsError struct {
	ReqID   int64
	Code    int64
	Message string
}

func NewIBKRTWSClient(host string, port int, clientID int64, accountID string) *IBKRTWSClient {
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 4002 // IB Gateway paper trading default
	}
	if clientID == 0 {
		clientID = 1
	}

	c := &IBKRTWSClient{
		Host:       host,
		Port:       port,
		ClientID:   clientID,
		AccountID:  accountID,
		conIDCache: make(map[string]int),
	}

	c.wrapper = newTWSWrapper()
	c.client = ibapi.NewEClient(c.wrapper)

	return c
}

func newTWSWrapper() *twsWrapper {
	return &twsWrapper{
		accountUpdate:      make(chan accountUpdateItem, 200),
		accountUpdateEnd:   make(chan string, 10),
		positions:          make(chan positionItem, 100),
		positionsEnd:       make(chan struct{}, 10),
		contractDetails:    make(chan contractDetailsItem, 100),
		contractDetailsEnd: make(chan int64, 10),
		historicalData:     make(chan historicalDataItem, 5000),
		historicalDataEnd:  make(chan int64, 10),
		tickPrice:          make(chan tickPriceItem, 100),
		orderStatus:        make(chan orderStatusItem, 100),
		openOrderEnd:       make(chan struct{}, 10),
		openOrder:          make(chan openOrderItem, 100),
		nextValidID:        make(chan int64, 10),
		errChan:            make(chan twsError, 100),
	}
}

// ---- EWrapper callback implementations ----

func (w *twsWrapper) NextValidID(reqID int64) {
	select {
	case w.nextValidID <- reqID:
	default:
	}
}

func (w *twsWrapper) Error(reqID int64, errorTime int64, errCode int64, errString string, advancedOrderRejectJson string) {
	// Codes 2104, 2106, 2107, 2108, 2119, 2158 are informational data farm messages
	if errCode == 2104 || errCode == 2106 || errCode == 2107 || errCode == 2108 || errCode == 2158 || errCode == 2119 {
		log.Printf(" TWS info [%d]: %s", errCode, errString)
		return
	}
	// Suppress 504 spam — only log once per batch (handled by reconnect monitor)
	if errCode == 504 {
		return
	}
	// 2100 = "API client has been unsubscribed from account data" — informational
	if errCode == 2100 {
		log.Printf(" TWS info [%d]: %s", errCode, errString)
		return
	}
	log.Printf(" TWS error [reqID=%d code=%d]: %s", reqID, errCode, errString)
	select {
	case w.errChan <- twsError{ReqID: reqID, Code: errCode, Message: errString}:
	default:
	}
}

func (w *twsWrapper) UpdateAccountValue(tag string, val string, currency string, accountName string) {
	select {
	case w.accountUpdate <- accountUpdateItem{Tag: tag, Value: val, Currency: currency, Account: accountName}:
	default:
	}
}

func (w *twsWrapper) AccountDownloadEnd(accountName string) {
	select {
	case w.accountUpdateEnd <- accountName:
	default:
	}
}

func (w *twsWrapper) Position(account string, contract *ibapi.Contract, position ibapi.Decimal, avgCost float64) {
	c := ibapi.Contract{}
	if contract != nil {
		c = *contract
	}
	select {
	case w.positions <- positionItem{Account: account, Contract: c, Position: position.Float(), AvgCost: avgCost}:
	default:
	}
}

func (w *twsWrapper) PositionEnd() {
	select {
	case w.positionsEnd <- struct{}{}:
	default:
	}
}

func (w *twsWrapper) ContractDetails(reqID int64, details *ibapi.ContractDetails) {
	d := ibapi.ContractDetails{}
	if details != nil {
		d = *details
	}
	select {
	case w.contractDetails <- contractDetailsItem{ReqID: reqID, Details: d}:
	default:
	}
}

func (w *twsWrapper) ContractDetailsEnd(reqID int64) {
	select {
	case w.contractDetailsEnd <- reqID:
	default:
	}
}

func (w *twsWrapper) HistoricalData(reqID int64, bar *ibapi.Bar) {
	if bar == nil {
		return
	}
	select {
	case w.historicalData <- historicalDataItem{
		ReqID:  reqID,
		Date:   bar.Date,
		Open:   bar.Open,
		High:   bar.High,
		Low:    bar.Low,
		Close:  bar.Close,
		Volume: bar.Volume.Float(),
	}:
	default:
	}
}

func (w *twsWrapper) HistoricalDataEnd(reqID int64, startDateStr string, endDateStr string) {
	select {
	case w.historicalDataEnd <- reqID:
	default:
	}
}

func (w *twsWrapper) TickPrice(reqID int64, tickType ibapi.TickType, price float64, attrib ibapi.TickAttrib) {
	select {
	case w.tickPrice <- tickPriceItem{ReqID: reqID, Field: tickType, Price: price, Attribs: attrib}:
	default:
	}
}

func (w *twsWrapper) OrderStatus(orderID int64, status string, filled ibapi.Decimal, remaining ibapi.Decimal, avgFillPrice float64, permID int64, parentID int64, lastFillPrice float64, clientID int64, whyHeld string, mktCapPrice float64) {
	select {
	case w.orderStatus <- orderStatusItem{OrderID: orderID, Status: status, Filled: filled.Float(), Remaining: remaining.Float(), AvgPrice: avgFillPrice}:
	default:
	}
}

func (w *twsWrapper) OpenOrder(orderID int64, contract *ibapi.Contract, order *ibapi.Order, orderState *ibapi.OrderState) {
	c := ibapi.Contract{}
	o := ibapi.Order{}
	s := ibapi.OrderState{}
	if contract != nil {
		c = *contract
	}
	if order != nil {
		o = *order
	}
	if orderState != nil {
		s = *orderState
	}
	select {
	case w.openOrder <- openOrderItem{OrderID: orderID, Contract: c, Order: o, State: s}:
	default:
	}
}

func (w *twsWrapper) OpenOrderEnd() {
	select {
	case w.openOrderEnd <- struct{}{}:
	default:
	}
}

// ---- Connection management ----

// Connect establishes the TCP socket connection to IB Gateway.
func (c *IBKRTWSClient) Connect() error {
	if c.connected.Load() {
		return nil
	}

	log.Printf(" TWS: Connecting to %s:%d (clientID=%d)...", c.Host, c.Port, c.ClientID)
	if err := c.client.Connect(c.Host, c.Port, c.ClientID); err != nil {
		return fmt.Errorf("TWS connection failed: %w", err)
	}

	// Wait for nextValidID which confirms connection is established.
	// The ibapi library handles message processing internally after Connect().
	select {
	case id := <-c.wrapper.nextValidID:
		c.nextReqID.Store(id)
		log.Printf(" TWS: Connected successfully. Next valid order ID: %d", id)
	case <-time.After(10 * time.Second):
		_ = c.client.Disconnect()
		return fmt.Errorf("TWS connection timeout: no nextValidID received within 10s")
	}

	c.connected.Store(true)

	// Start background reconnection monitor
	go c.monitorConnection()

	return nil
}

// Disconnect closes the TWS connection.
func (c *IBKRTWSClient) Disconnect() {
	if c.connected.Load() {
		_ = c.client.Disconnect()
		c.connected.Store(false)
		log.Printf(" TWS: Disconnected")
	}
}

func (c *IBKRTWSClient) monitorConnection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// Check both our flag AND the library's internal connection state.
		needsReconnect := !c.connected.Load()
		if !needsReconnect && c.client != nil && !c.client.IsConnected() {
			log.Printf(" TWS: Library reports disconnected, marking for reconnect")
			c.connected.Store(false)
			needsReconnect = true
		}
		if needsReconnect {
			log.Printf(" TWS: Connection lost, attempting reconnect...")
			if err := c.reconnect(); err != nil {
				log.Printf(" TWS: Reconnection failed: %v", err)
			}
		}
	}
}

func (c *IBKRTWSClient) reconnect() error {
	c.connected.Store(false)
	c.wrapper = newTWSWrapper()
	c.client = ibapi.NewEClient(c.wrapper)

	if err := c.client.Connect(c.Host, c.Port, c.ClientID); err != nil {
		return err
	}

	select {
	case id := <-c.wrapper.nextValidID:
		c.nextReqID.Store(id)
		log.Printf(" TWS: Reconnected successfully. Next valid order ID: %d", id)
	case <-time.After(10 * time.Second):
		_ = c.client.Disconnect()
		return fmt.Errorf("TWS reconnection timeout")
	}

	c.connected.Store(true)
	return nil
}

func (c *IBKRTWSClient) IsAuthenticated() bool {
	return c.connected.Load()
}

func (c *IBKRTWSClient) getReqID() int64 {
	return c.nextReqID.Add(1)
}

// ---- Contract resolution ----

// ResolveContract resolves a symbol to a contract ID (conid) with caching.
func (c *IBKRTWSClient) ResolveContract(symbol string) (int, error) {
	cleanSymbol := strings.TrimSpace(strings.ToUpper(strings.TrimSuffix(strings.ToUpper(symbol), "USDT")))

	c.cacheMu.RLock()
	if conid, exists := c.conIDCache[cleanSymbol]; exists {
		c.cacheMu.RUnlock()
		return conid, nil
	}
	c.cacheMu.RUnlock()

	if !c.connected.Load() {
		return 0, fmt.Errorf("TWS not connected")
	}

	contract := ibapi.Contract{
		Symbol:   cleanSymbol,
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}

	reqID := c.getReqID()
	drainContractDetails(c.wrapper)

	c.client.ReqContractDetails(reqID, &contract)

	var bestConID int
	bestScore := -1
	timeout := time.After(10 * time.Second)

	for {
		select {
		case item := <-c.wrapper.contractDetails:
			if item.ReqID != reqID {
				continue
			}
			conid := int(item.Details.Contract.ConID)
			score := scoreTWSContract(cleanSymbol, item.Details)
			if score > bestScore {
				bestScore = score
				bestConID = conid
			}
		case endID := <-c.wrapper.contractDetailsEnd:
			if endID != reqID {
				continue
			}
			goto done
		case err := <-c.wrapper.errChan:
			if err.ReqID == reqID {
				if err.Code == 200 {
					return 0, fmt.Errorf("no contract found for symbol %s", cleanSymbol)
				}
				return 0, fmt.Errorf("TWS contract lookup error for %s: [%d] %s", cleanSymbol, err.Code, err.Message)
			}
		case <-timeout:
			return 0, fmt.Errorf("TWS contract lookup timed out for %s", cleanSymbol)
		}
	}

done:
	if bestConID == 0 {
		return 0, fmt.Errorf("no contract found for symbol %s", cleanSymbol)
	}

	c.cacheMu.Lock()
	c.conIDCache[cleanSymbol] = bestConID
	c.cacheMu.Unlock()

	log.Printf("TWS: Resolved %s to conid %d", cleanSymbol, bestConID)
	return bestConID, nil
}

func drainContractDetails(w *twsWrapper) {
	for {
		select {
		case <-w.contractDetails:
		case <-w.contractDetailsEnd:
		default:
			return
		}
	}
}

func scoreTWSContract(symbol string, details ibapi.ContractDetails) int {
	score := 0
	c := details.Contract

	if strings.EqualFold(c.Symbol, symbol) {
		score += 15
	}
	if strings.EqualFold(c.SecType, "STK") {
		score += 100
	}
	exchange := strings.ToUpper(c.Exchange + " " + c.PrimaryExchange)
	if strings.Contains(exchange, "SMART") {
		score += 30
	}
	if strings.Contains(exchange, "NYSE") || strings.Contains(exchange, "NASDAQ") {
		score += 25
	}
	if strings.Contains(exchange, "TSE") || strings.Contains(exchange, "TSX") {
		score += 20
	}
	if strings.Contains(exchange, "AMEX") || strings.Contains(exchange, "ARCA") || strings.Contains(exchange, "BATS") {
		score += 10
	}
	return score
}

// ---- Account data ----

// GetAccountSummary fetches account data using ReqAccountUpdates (subscription model).
// This avoids TWS error 322 from ReqAccountSummary subscription limits.
// Results are cached for 10 seconds to avoid rapid repeated requests.
func (c *IBKRTWSClient) GetAccountSummary() (map[string]string, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("TWS not connected")
	}

	// Serialize: only one account update request at a time.
	c.acctSummaryMu.Lock()
	defer c.acctSummaryMu.Unlock()

	// Return cached result if fresh (within 10 seconds).
	if len(c.acctSummaryCache) > 0 && time.Since(c.acctSummaryCacheTime) < 10*time.Second {
		result := make(map[string]string, len(c.acctSummaryCache))
		for k, v := range c.acctSummaryCache {
			result[k] = v
		}
		return result, nil
	}

	// Drain any stale data from channels.
	drainAccountUpdates(c.wrapper)

	// Subscribe to account updates.
	acctName := c.AccountID
	log.Printf(" TWS: Requesting account updates for '%s'...", acctName)
	c.client.ReqAccountUpdates(true, acctName)

	// Tags we care about (case-insensitive match).
	wantTags := map[string]bool{
		"netliquidation":     true,
		"totalcashvalue":     true,
		"settledcash":        true,
		"availablefunds":     true,
		"grosspositionvalue": true,
		"unrealizedpnl":      true,
		"realizedpnl":        true,
	}

	result := make(map[string]string)
	timeout := time.After(15 * time.Second)

	for {
		select {
		case item := <-c.wrapper.accountUpdate:
			tagLower := strings.ToLower(item.Tag)
			// Collect USD or BASE currency values for the tags we need.
			// Also accept empty currency for fields that don't have one.
			if wantTags[tagLower] {
				cur := strings.ToUpper(strings.TrimSpace(item.Currency))
				if cur == "USD" || cur == "BASE" || cur == "" {
					result[tagLower] = item.Value
				}
			}
		case <-c.wrapper.accountUpdateEnd:
			// Unsubscribe.
			c.client.ReqAccountUpdates(false, acctName)
			time.Sleep(100 * time.Millisecond)
			if len(result) > 0 {
				c.acctSummaryCache = result
				c.acctSummaryCacheTime = time.Now()
				log.Printf(" TWS: Account updates received %d fields", len(result))
			}
			return result, nil
		case err := <-c.wrapper.errChan:
			if err.ReqID == -1 || err.Code == 321 || err.Code == 322 {
				c.client.ReqAccountUpdates(false, acctName)
				return nil, fmt.Errorf("TWS account update error: [%d] %s", err.Code, err.Message)
			}
		case <-timeout:
			c.client.ReqAccountUpdates(false, acctName)
			log.Printf(" TWS: Account updates timed out with %d fields", len(result))
			if len(result) > 0 {
				c.acctSummaryCache = result
				c.acctSummaryCacheTime = time.Now()
			}
			return result, nil
		}
	}
}

func drainAccountUpdates(w *twsWrapper) {
	for {
		select {
		case <-w.accountUpdate:
		case <-w.accountUpdateEnd:
		default:
			return
		}
	}
}

// GetPositions fetches all positions synchronously.
func (c *IBKRTWSClient) GetPositions() ([]positionItem, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("TWS not connected")
	}

	drainPositions(c.wrapper)

	c.client.ReqPositions()
	defer c.client.CancelPositions()

	var positions []positionItem
	timeout := time.After(15 * time.Second)

	for {
		select {
		case item := <-c.wrapper.positions:
			if strings.TrimSpace(c.AccountID) != "" && !strings.EqualFold(item.Account, c.AccountID) {
				continue
			}
			positions = append(positions, item)
		case <-c.wrapper.positionsEnd:
			return positions, nil
		case <-timeout:
			return positions, nil
		}
	}
}

func drainPositions(w *twsWrapper) {
	for {
		select {
		case <-w.positions:
		case <-w.positionsEnd:
		default:
			return
		}
	}
}

// ---- Historical data ----

// HistoricalBarResult is the parsed output from a TWS historical data request.
type HistoricalBarResult struct {
	Date   string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// GetHistoricalBars fetches historical OHLCV bars for a symbol.
func (c *IBKRTWSClient) GetHistoricalBars(symbol string, conID int, barSize string, duration string, limit int) ([]HistoricalBarResult, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("TWS not connected")
	}

	contract := ibapi.Contract{
		ConID:    int64(conID),
		Symbol:   symbol,
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}

	reqID := c.getReqID()
	drainHistoricalData(c.wrapper)

	// Empty end date = current time; useRTH=true; formatDate=1 (yyyyMMdd HH:mm:ss)
	c.client.ReqHistoricalData(reqID, &contract, "", duration, barSize, "TRADES", true, 1, false, nil)

	var bars []HistoricalBarResult
	timeout := time.After(30 * time.Second)

	for {
		select {
		case item := <-c.wrapper.historicalData:
			if item.ReqID != reqID {
				continue
			}
			bars = append(bars, HistoricalBarResult{
				Date:   item.Date,
				Open:   item.Open,
				High:   item.High,
				Low:    item.Low,
				Close:  item.Close,
				Volume: item.Volume,
			})
		case endID := <-c.wrapper.historicalDataEnd:
			if endID != reqID {
				continue
			}
			return bars, nil
		case err := <-c.wrapper.errChan:
			if err.ReqID == reqID {
				return nil, fmt.Errorf("TWS historical data error for %s: [%d] %s", symbol, err.Code, err.Message)
			}
		case <-timeout:
			if len(bars) > 0 {
				return bars, nil
			}
			return nil, fmt.Errorf("TWS historical data timed out for %s", symbol)
		}
	}
}

func drainHistoricalData(w *twsWrapper) {
	for {
		select {
		case <-w.historicalData:
		case <-w.historicalDataEnd:
		default:
			return
		}
	}
}

// ---- Market data snapshot ----

// GetSnapshot fetches a market data snapshot (bid/ask) for a symbol.
func (c *IBKRTWSClient) GetSnapshot(symbol string, conID int) (bid, ask, bidSize, askSize float64, err error) {
	if !c.connected.Load() {
		return 0, 0, 0, 0, fmt.Errorf("TWS not connected")
	}

	contract := ibapi.Contract{
		ConID:    int64(conID),
		Symbol:   symbol,
		SecType:  "STK",
		Exchange: "SMART",
		Currency: "USD",
	}

	reqID := c.getReqID()
	drainTickPrice(c.wrapper)

	// Request snapshot (snapshot=true, regulatorySnapshot=false)
	c.client.ReqMktData(reqID, &contract, "", true, false, nil)
	defer c.client.CancelMktData(reqID)

	timeout := time.After(10 * time.Second)
	gotBid, gotAsk := false, false

	for {
		select {
		case item := <-c.wrapper.tickPrice:
			if item.ReqID != reqID {
				continue
			}
			// TickType: 1=Bid, 2=Ask, 9=Last
			switch item.Field {
			case 1:
				bid = item.Price
				gotBid = true
			case 2:
				ask = item.Price
				gotAsk = true
			}
			if gotBid && gotAsk {
				return bid, ask, 0, 0, nil
			}
		case twsErr := <-c.wrapper.errChan:
			if twsErr.ReqID == reqID {
				return 0, 0, 0, 0, fmt.Errorf("TWS snapshot error for %s: [%d] %s", symbol, twsErr.Code, twsErr.Message)
			}
		case <-timeout:
			if bid > 0 || ask > 0 {
				return bid, ask, 0, 0, nil
			}
			return 0, 0, 0, 0, fmt.Errorf("TWS snapshot timed out for %s", symbol)
		}
	}
}

func drainTickPrice(w *twsWrapper) {
	for {
		select {
		case <-w.tickPrice:
		default:
			return
		}
	}
}

// ---- Order management ----

// PlaceOrder submits an order via TWS API.
func (c *IBKRTWSClient) PlaceOrder(contract ibapi.Contract, order ibapi.Order) (int64, error) {
	if !c.connected.Load() {
		return 0, fmt.Errorf("TWS not connected")
	}

	orderID := c.nextReqID.Add(1)
	order.OrderID = orderID

	log.Printf(" TWS: Placing order #%d: %s %v %s @ %s", orderID, order.Action, order.TotalQuantity, contract.Symbol, order.OrderType)
	c.client.PlaceOrder(orderID, &contract, &order)

	// Wait for order acknowledgment
	timeout := time.After(15 * time.Second)
	for {
		select {
		case status := <-c.wrapper.orderStatus:
			if status.OrderID == orderID {
				log.Printf(" TWS: Order #%d status: %s (filled=%.0f remaining=%.0f)", orderID, status.Status, status.Filled, status.Remaining)
				return orderID, nil
			}
		case twsErr := <-c.wrapper.errChan:
			if twsErr.ReqID == orderID {
				return 0, fmt.Errorf("TWS order error: [%d] %s", twsErr.Code, twsErr.Message)
			}
		case <-timeout:
			log.Printf(" TWS: Order #%d submitted (no status confirmation within timeout)", orderID)
			return orderID, nil
		}
	}
}

// CancelOrder cancels a specific order.
func (c *IBKRTWSClient) CancelOrder(orderID int64) error {
	if !c.connected.Load() {
		return fmt.Errorf("TWS not connected")
	}
	c.client.CancelOrder(orderID, ibapi.CancelOrderEmpty())
	return nil
}

// CancelAllOrders cancels all open orders via the global cancel.
func (c *IBKRTWSClient) CancelAllOrders() {
	if !c.connected.Load() {
		return
	}
	log.Printf(" TWS: Transmitting global order cancellation...")
	c.client.ReqGlobalCancel(ibapi.CancelOrderEmpty())
}

// GetOpenOrders fetches all open orders.
func (c *IBKRTWSClient) GetOpenOrders() ([]openOrderItem, error) {
	if !c.connected.Load() {
		return nil, fmt.Errorf("TWS not connected")
	}

	drainOpenOrders(c.wrapper)

	c.client.ReqAllOpenOrders()

	var result []openOrderItem
	timeout := time.After(10 * time.Second)

	for {
		select {
		case item := <-c.wrapper.openOrder:
			result = append(result, item)
		case <-c.wrapper.openOrderEnd:
			return result, nil
		case <-timeout:
			return result, nil
		}
	}
}

func drainOpenOrders(w *twsWrapper) {
	for {
		select {
		case <-w.openOrder:
		case <-w.openOrderEnd:
		default:
			return
		}
	}
}

// ---- Readiness checks ----

// CheckSessionReadiness verifies the TWS connection is healthy.
// It checks connection state and optionally account summary data.
// During weekends/maintenance, account data may not be available
// but the session is still considered ready if the socket is connected.
func (c *IBKRTWSClient) CheckSessionReadiness(accountID string) error {
	if !c.connected.Load() {
		return fmt.Errorf("TWS not connected to IB Gateway at %s:%d", c.Host, c.Port)
	}

	// If we have cached account data, the session is definitely ready.
	c.acctSummaryMu.Lock()
	hasCachedData := len(c.acctSummaryCache) > 0
	c.acctSummaryMu.Unlock()
	if hasCachedData {
		return nil
	}

	// Try to get account data. If it times out with 0 fields,
	// that's OK during weekends/maintenance — connection is still valid.
	summary, err := c.GetAccountSummary()
	if err != nil {
		// Real errors (not timeout) indicate a problem.
		return fmt.Errorf("TWS session check failed: %w", err)
	}
	if len(summary) == 0 {
		// No data but no error — likely weekend/maintenance.
		// Connection is established (we checked above), so treat as ready.
		log.Printf(" TWS: Session ready (connected, account data not yet available — may be weekend/maintenance)")
	}
	return nil
}

// CheckLiveReadiness verifies the full trading readiness.
func (c *IBKRTWSClient) CheckLiveReadiness(accountID string) error {
	if err := c.CheckSessionReadiness(accountID); err != nil {
		return err
	}
	if _, err := c.GetPositions(); err != nil {
		return fmt.Errorf("TWS positions check failed: %w", err)
	}
	if _, err := c.GetOpenOrders(); err != nil {
		return fmt.Errorf("TWS open orders check failed: %w", err)
	}
	return nil
}

// ---- Interval mapping ----

// ResolveTWSInterval maps our interval strings to TWS bar size + duration strings.
func ResolveTWSInterval(interval string, limit int) (barSize string, duration string, aggregateBucket int) {
	if limit <= 0 {
		limit = 200
	}
	interval = strings.ToLower(strings.TrimSpace(interval))
	aggregateBucket = 1

	switch interval {
	case "1m":
		barSize = "1 min"
		days := int(math.Ceil(float64(limit) / 390.0))
		if days < 1 {
			days = 1
		}
		duration = fmt.Sprintf("%d D", days)
	case "3m":
		barSize = "1 min"
		aggregateBucket = 3
		needed := limit * 3
		days := int(math.Ceil(float64(needed) / 390.0))
		if days < 3 {
			days = 3
		}
		duration = fmt.Sprintf("%d D", days)
	case "5m":
		barSize = "5 mins"
		days := int(math.Ceil(float64(limit) / 78.0))
		if days < 5 {
			days = 5
		}
		duration = fmt.Sprintf("%d D", days)
	case "1h":
		barSize = "1 hour"
		duration = "1 M"
	case "4h":
		barSize = "1 hour"
		aggregateBucket = 4
		duration = "1 M"
	case "1d":
		barSize = "1 day"
		duration = "1 Y"
	default:
		barSize = "1 min"
		duration = "1 D"
	}

	return barSize, duration, aggregateBucket
}
