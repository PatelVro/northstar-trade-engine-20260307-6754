package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"northstar/buildinfo"
	"northstar/manager"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Server HTTP API server
type Server struct {
	router        *gin.Engine
	traderManager *manager.TraderManager
	port          int
	startedAt     time.Time

	// WebSocket Hub
	wsClients map[*websocket.Conn]bool
	wsMutex   sync.Mutex
	Broadcast chan interface{}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all for local dashboard
	},
}

// NewServer initializes the API server
func NewServer(traderManager *manager.TraderManager, port int) *Server {
	// Set to Release mode to reduce log output
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()

	// Enable CORS middleware
	router.Use(corsMiddleware())

	s := &Server{
		router:        router,
		traderManager: traderManager,
		port:          port,
		startedAt:     time.Now().UTC(),
		wsClients:     make(map[*websocket.Conn]bool),
		Broadcast:     make(chan interface{}, 256),
	}

	go s.runWSHub()
	go s.startTelemetry()

	// Setup endpoints
	s.setupRoutes()

	return s
}

func (s *Server) runWSHub() {
	for {
		msg := <-s.Broadcast

		s.wsMutex.Lock()
		for client := range s.wsClients {
			err := client.WriteJSON(msg)
			if err != nil {
				client.Close()
				delete(s.wsClients, client)
			}
		}
		s.wsMutex.Unlock()
	}
}

// startTelemetry automatically polls the trading engine and streams updates via WS
func (s *Server) startTelemetry() {
	ticker := time.NewTicker(2 * time.Second)
	for range ticker.C {
		// Only poll & broadcast if someone is listening
		s.wsMutex.Lock()
		activeClients := len(s.wsClients)
		s.wsMutex.Unlock()

		if activeClients == 0 {
			continue
		}

		traders := s.traderManager.GetAllTraders()
		if len(traders) == 0 {
			continue
		}

		for _, t := range traders {
			// Focus telemetry on the primary instance for UI
			s.Broadcast <- map[string]interface{}{"type": "connection_status", "data": "connected"}

			if acc, err := t.GetAccountInfo(); err == nil {
				s.Broadcast <- map[string]interface{}{"type": "portfolio_update", "data": acc}
			}

			if pos, err := t.GetPositions(); err == nil {
				s.Broadcast <- map[string]interface{}{"type": "order_update", "data": pos}
			}

			s.Broadcast <- map[string]interface{}{"type": "strategy_update", "data": t.GetStatus()}

			break // Only broadcast the first active trader's state
		}
	}
}

// handleWebSocket upgrades the HTTP connection to a WebSocket
func (s *Server) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v", err)
		return
	}

	s.wsMutex.Lock()
	s.wsClients[conn] = true
	s.wsMutex.Unlock()

	// Keep alive loop
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			s.wsMutex.Lock()
			delete(s.wsClients, conn)
			s.wsMutex.Unlock()
			conn.Close()
			break
		}
	}
}

// corsMiddleware handles CORS policy
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

// setupRoutes configuration
func (s *Server) setupRoutes() {
	// health check
	s.router.Any("/health", s.handleHealth)

	// WebSocket endpoint
	s.router.GET("/ws", s.handleWebSocket)

	// API route group
	api := s.router.Group("/api")
	{
		// Competition overview
		api.GET("/competition", s.handleCompetition)

		// Trader list
		api.GET("/traders", s.handleTraderList)

		// Specific trader data (via query parameter ?trader_id=xxx)
		api.GET("/status", s.handleStatus)
		api.GET("/account", s.handleAccount)
		api.GET("/positions", s.handlePositions)
		api.GET("/decisions", s.handleDecisions)
		api.GET("/decisions/latest", s.handleLatestDecisions)
		api.GET("/audit/trades/recent", s.handleRecentTradeAudit)
		api.GET("/statistics", s.handleStatistics)
		api.GET("/equity-history", s.handleEquityHistory)
		api.GET("/performance", s.handlePerformance)
		api.GET("/candles", s.handleCandles)
	}

	// Serve the built web dashboard if the dist directory exists
	distPath := filepath.Join("web", "dist")
	if info, err := os.Stat(distPath); err == nil && info.IsDir() {
		distFS := os.DirFS(distPath)
		fileServer := http.FileServer(http.FS(distFS))
		s.router.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			// Try to serve the exact file first
			if f, err := fs.Stat(distFS, path[1:]); err == nil && !f.IsDir() {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
			// SPA fallback: serve index.html for all other routes
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
		log.Printf("  Dashboard served from %s", distPath)
	}
}

// handleHealth reports shallow service liveness and build diagnostics only.
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, s.buildHealthResponse(time.Now()))
}

// getTraderFromQuery extracts trader from query parameter
func (s *Server) getTraderFromQuery(c *gin.Context) (*manager.TraderManager, string, error) {
	traderID := c.Query("trader_id")
	if traderID == "" {
		// If no trader_id is specified, return the first available trader
		ids := s.traderManager.GetTraderIDs()
		if len(ids) == 0 {
			return nil, "", fmt.Errorf("No available trader")
		}
		traderID = ids[0]
	}
	return s.traderManager, traderID, nil
}

// handleCompetition handles overview comparing all traders
func (s *Server) handleCompetition(c *gin.Context) {
	comparison, err := s.traderManager.GetComparisonData()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get comparison data: %v", err),
		})
		return
	}
	c.JSON(http.StatusOK, comparison)
}

// handleTraderList handles trader list
func (s *Server) handleTraderList(c *gin.Context) {
	traders := s.traderManager.GetAllTraders()
	result := make([]map[string]interface{}, 0, len(traders))

	for _, t := range traders {
		result = append(result, map[string]interface{}{
			"trader_id":   t.GetID(),
			"trader_name": t.GetName(),
			"ai_model":    t.GetAIModel(),
		})
	}

	c.JSON(http.StatusOK, result)
}

// handleStatus handles system status
func (s *Server) handleStatus(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, buildOperatorStatusResponse(trader.GetOperatorStatus(), time.Now()))
}

// handleCandles serves historical candlestick data for the Symbol Chart
func (s *Server) handleCandles(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol parameter is required"})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	provider := trader.GetProvider()
	if provider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data provider not initialized for this trader"})
		return
	}

	// Fetch up to 200 candles for the charts
	bars, err := provider.GetBars([]string{symbol}, "1m", 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch bars: %v", err)})
		return
	}

	klines, exists := bars[symbol]
	if !exists || len(klines) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no candlestick data found for symbol"})
		return
	}

	c.JSON(http.StatusOK, klines)
}

// handleAccount handles account info
func (s *Server) handleAccount(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	log.Printf(" Received account info request [%s]", trader.GetName())
	account, err := trader.GetAccountInfo()
	if err != nil {
		log.Printf(" Failed to get account info [%s]: %v", trader.GetName(), err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get account info: %v", err),
		})
		return
	}

	log.Printf(" Returning account info [%s]: broker_equity=%.2f, strategy_equity=%.2f, total_pnl=%.2f (%.2f%%)",
		trader.GetName(),
		account.AccountEquity,
		account.StrategyEquity,
		account.TotalPnL,
		account.StrategyReturnPct)
	c.JSON(http.StatusOK, account)
}

// handlePositions handles position list
func (s *Server) handlePositions(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	positions, err := trader.GetPositions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get position list: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, positions)
}

// handleDecisions handles decision logs list
func (s *Server) handleDecisions(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Get all historical decision records (no limit)
	records, err := trader.GetDecisionLogger().GetLatestRecords(10000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get decision logs: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, records)
}

// handleLatestDecisions handles latest decisions (top 5 latest first)
func (s *Server) handleLatestDecisions(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	records, err := trader.GetDecisionLogger().GetLatestRecords(5)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get decision logs: %v", err),
		})
		return
	}

	// Reverse the array to show latest on top (for list views)
	// GetLatestRecords returns oldest to newest (for charts), here we need newest to oldest
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	c.JSON(http.StatusOK, records)
}

func (s *Server) handleRecentTradeAudit(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	limit := 20
	if rawLimit := c.Query("limit"); rawLimit != "" {
		if _, err := fmt.Sscanf(rawLimit, "%d", &limit); err != nil || limit <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		if limit > 100 {
			limit = 100
		}
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	records, err := trader.GetRecentTradeAudits(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get trade audit records: %v", err)})
		return
	}

	c.JSON(http.StatusOK, records)
}

// handleStatistics handles analytics statistics
func (s *Server) handleStatistics(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	stats, err := trader.GetDecisionLogger().GetStatistics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get statistics: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// handleEquityHistory handles equity return history
func (s *Server) handleEquityHistory(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Retrieve as much historical data as possible (several days)
	// At 3min/cycle: 10000 records = ~20 days data
	records, err := trader.GetDecisionLogger().GetLatestRecords(10000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get historical data: %v", err),
		})
		return
	}

	// Build strategy performance history from canonical accounting snapshots only.
	type EquityPoint struct {
		Timestamp              string  `json:"timestamp"`
		AccountEquity          float64 `json:"account_equity"`
		AccountCash            float64 `json:"account_cash"`
		AvailableBalance       float64 `json:"available_balance"`
		GrossMarketValue       float64 `json:"gross_market_value"`
		UnrealizedPnL          float64 `json:"unrealized_pnl"`
		RealizedPnL            float64 `json:"realized_pnl"`
		TotalPnL               float64 `json:"total_pnl"`
		StrategyInitialCapital float64 `json:"strategy_initial_capital"`
		StrategyEquity         float64 `json:"strategy_equity"`
		StrategyReturnPct      float64 `json:"strategy_return_pct"`
		PositionCount          int     `json:"position_count"`
		MarginUsedPct          float64 `json:"margin_used_pct"`
		CycleNumber            int     `json:"cycle_number"`
	}

	var history []EquityPoint
	for _, record := range records {
		if !record.AccountState.HasCanonicalAccounting() {
			// Pre-fix records mixed broker equity and strategy P&L, so they are excluded
			// from strategy-return charts instead of showing nonsensical percentages.
			continue
		}

		history = append(history, EquityPoint{
			Timestamp:              record.Timestamp.Format("2006-01-02 15:04:05"),
			AccountEquity:          record.AccountState.AccountEquity,
			AccountCash:            record.AccountState.AccountCash,
			AvailableBalance:       record.AccountState.AvailableBalance,
			GrossMarketValue:       record.AccountState.GrossMarketValue,
			UnrealizedPnL:          record.AccountState.UnrealizedPnL,
			RealizedPnL:            record.AccountState.RealizedPnL,
			TotalPnL:               record.AccountState.TotalPnL,
			StrategyInitialCapital: record.AccountState.StrategyInitialCapital,
			StrategyEquity:         record.AccountState.StrategyEquity,
			StrategyReturnPct:      record.AccountState.StrategyReturnPct,
			PositionCount:          record.AccountState.PositionCount,
			MarginUsedPct:          record.AccountState.MarginUsedPct,
			CycleNumber:            record.CycleNumber,
		})
	}

	c.JSON(http.StatusOK, history)
}

// handlePerformance AI historical performance analysis (for displaying AI learning and retrospection)
func (s *Server) handlePerformance(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	trader, err := s.traderManager.GetTrader(traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Analyze trading performance of latest 100 cycles to prevent dropping long-term positions
	// Assuming 3min/cycle, 100 cycles = 5 hours, sufficient to cover most trades
	performance, err := trader.GetDecisionLogger().AnalyzePerformance(100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to analyze historical performance: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, performance)
}

// Start runs the API server
func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	log.Printf(" Northstar API build: %s", buildinfo.Current().Summary())
	log.Printf(" API server started at http://%s", addr)
	log.Printf(" API documentation:")
	log.Printf("   GET  /api/competition      - Competition overview (comparing all traders)")
	log.Printf("   GET  /api/traders          - Trader list")
	log.Printf("   GET  /api/status?trader_id=xxx     - operator trading status summary")
	log.Printf("   GET  /api/account?trader_id=xxx    - specified trader's account info")
	log.Printf("   GET  /api/positions?trader_id=xxx  - specified trader's position list")
	log.Printf("   GET  /api/decisions?trader_id=xxx  - specified trader's decision logs")
	log.Printf("   GET  /api/decisions/latest?trader_id=xxx - specified trader's latest decision")
	log.Printf("   GET  /api/audit/trades/recent?trader_id=xxx&limit=20 - specified trader's recent trade audit records")
	log.Printf("   GET  /api/statistics?trader_id=xxx - specified trader's statistics")
	log.Printf("   GET  /api/equity-history?trader_id=xxx - specified trader's equity history")
	log.Printf("   GET  /api/performance?trader_id=xxx - specified trader's AI learning performance analysis")
	log.Printf("   GET  /health               - liveness and build diagnostics only (not a trading-ready signal)")
	log.Println()

	return s.router.Run(addr)
}

func (s *Server) uptimeSeconds(now time.Time) int64 {
	if s.startedAt.IsZero() {
		return 0
	}
	uptime := int64(now.UTC().Sub(s.startedAt).Seconds())
	if uptime < 0 {
		uptime = int64(time.Since(s.startedAt).Seconds())
		if uptime < 0 {
			return 0
		}
	}
	return uptime
}
