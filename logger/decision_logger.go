package logger

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"time"
)

// DecisionRecord holds the log entry for an AI trading decision
type DecisionRecord struct {
	Timestamp      time.Time          `json:"timestamp"`       // Decision timestamp
	CycleNumber    int                `json:"cycle_number"`    // Cycle number
	InputPrompt    string             `json:"input_prompt"`    // Input prompt sent to AI
	CoTTrace       string             `json:"cot_trace"`       // AI chain of thought trace
	DecisionJSON   string             `json:"decision_json"`   // Raw decision JSON
	AccountState   AccountSnapshot    `json:"account_state"`   // Snapshot of account state
	Positions      []PositionSnapshot `json:"positions"`       // Snapshot of open positions
	CandidateCoins []string           `json:"candidate_coins"` // List of candidate coins
	Decisions      []DecisionAction   `json:"decisions"`       // List of executed decisions
	ExecutionLog   []string           `json:"execution_log"`   // Log of execution steps
	Success        bool               `json:"success"`         // Flag indicating success
	ErrorMessage   string             `json:"error_message"`   // Error message if applicable
}

// AccountSnapshot records account balance state
type AccountSnapshot struct {
	AccountingVersion      int     `json:"accounting_version"`
	AccountCash            float64 `json:"account_cash"`
	AccountEquity          float64 `json:"account_equity"`
	AvailableBalance       float64 `json:"available_balance"`
	GrossMarketValue       float64 `json:"gross_market_value"`
	UnrealizedPnL          float64 `json:"unrealized_pnl"`
	RealizedPnL            float64 `json:"realized_pnl"`
	TotalPnL               float64 `json:"total_pnl"`
	StrategyInitialCapital float64 `json:"strategy_initial_capital"`
	StrategyEquity         float64 `json:"strategy_equity"`
	StrategyReturnPct      float64 `json:"strategy_return_pct"`
	DailyPnL               float64 `json:"daily_pnl"`
	PositionCount          int     `json:"position_count"`
	MarginUsed             float64 `json:"margin_used"`
	MarginUsedPct          float64 `json:"margin_used_pct"`

	// Legacy fields kept only so pre-fix logs can still be parsed.
	TotalBalance          float64 `json:"total_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
}

// PositionSnapshot records individual position state
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

func (s AccountSnapshot) HasCanonicalAccounting() bool {
	return s.AccountingVersion >= 2
}

func (s AccountSnapshot) EffectiveAccountEquity() float64 {
	if s.HasCanonicalAccounting() {
		return s.AccountEquity
	}
	return s.TotalBalance
}

func (s AccountSnapshot) EffectiveStrategyEquity() (float64, bool) {
	if s.HasCanonicalAccounting() {
		return s.StrategyEquity, s.StrategyInitialCapital > 0
	}
	return 0, false
}

// DecisionAction documents an individual executed decision
type DecisionAction struct {
	Action                string            `json:"action"` // open_long, open_short, close_long, close_short
	Symbol                string            `json:"symbol"` // Asset symbol
	DecisionReasoning     string            `json:"decision_reasoning,omitempty"`
	DecisionConfidence    int               `json:"decision_confidence,omitempty"`
	DecisionPositionSize  float64           `json:"decision_position_size_usd,omitempty"`
	DecisionStopLoss      float64           `json:"decision_stop_loss,omitempty"`
	DecisionTakeProfit    float64           `json:"decision_take_profit,omitempty"`
	Quantity              float64           `json:"quantity"` // Position quantity size
	Leverage              int               `json:"leverage"` // Leverage application size
	Price                 float64           `json:"price"`    // Execution price
	FeesUSD               float64           `json:"fees_usd"` // Fees paid on this execution when known
	RealizedPnL           float64           `json:"realized_pnl"`
	OrderID               int64             `json:"order_id"` // Exchange Order ID
	BrokerOrderID         string            `json:"broker_order_id,omitempty"`
	LocalOrderID          string            `json:"local_order_id,omitempty"`
	OrderStatus           string            `json:"order_status,omitempty"`
	Timestamp             time.Time         `json:"timestamp"` // Execution timestamp
	Success               bool              `json:"success"`   // Outcome flag
	Error                 string            `json:"error"`     // Error message
	RiskOutcome           string            `json:"risk_outcome,omitempty"`
	RiskSummary           string            `json:"risk_summary,omitempty"`
	RiskRequestedQuantity float64           `json:"risk_requested_quantity,omitempty"`
	RiskRequestedNotional float64           `json:"risk_requested_notional,omitempty"`
	RiskApprovedQuantity  float64           `json:"risk_approved_quantity,omitempty"`
	RiskApprovedNotional  float64           `json:"risk_approved_notional,omitempty"`
	RiskChecks            []RiskCheckResult `json:"risk_checks,omitempty"`
}

type RiskCheckResult struct {
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	Message          string  `json:"message"`
	ApprovedQuantity float64 `json:"approved_quantity,omitempty"`
	ApprovedNotional float64 `json:"approved_notional,omitempty"`
}

// DecisionLogger records system decision workflows over time
type DecisionLogger struct {
	logDir      string
	cycleNumber int
}

// NewDecisionLogger instantiates a logger configuration
func NewDecisionLogger(logDir string) *DecisionLogger {
	if logDir == "" {
		logDir = "decision_logs"
	}

	// Ensure logging directory exists locally
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf(" Failed to create log directory: %v\n", err)
	}

	return &DecisionLogger{
		logDir:      logDir,
		cycleNumber: 0,
	}
}

// LogDecision saves a decision object locally for historical tracking
func (l *DecisionLogger) LogDecision(record *DecisionRecord) error {
	l.cycleNumber++
	record.CycleNumber = l.cycleNumber
	record.Timestamp = time.Now()

	// Build log label constraint format: decision_YYYYMMDD_HHMMSS_cycleN.json
	filename := fmt.Sprintf("decision_%s_cycle%d.json",
		record.Timestamp.Format("20060102_150405"),
		record.CycleNumber)

	filepath := filepath.Join(l.logDir, filename)

	// Output pretty-printed readable JSON strings
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize decision record: %w", err)
	}

	// Render into file storage directly
	if err := ioutil.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write decision record: %w", err)
	}

	fmt.Printf(" Decision record saved: %s\n", filename)
	return nil
}

// GetLatestRecords retrieves the most recent N records chronologically
func (l *DecisionLogger) GetLatestRecords(n int) ([]*DecisionRecord, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("failed pulling log files directory: %w", err)
	}

	// Retrieve sorted by modified time inverted (newest prioritized)
	var records []*DecisionRecord
	count := 0
	for i := len(files) - 1; i >= 0 && count < n; i-- {
		file := files[i]
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
		count++
	}

	// Reverse target arrays sequentially older to newer (For chart layout constraints)
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

// GetRecordByDate fetches historical logs grouped by designated date strings
func (l *DecisionLogger) GetRecordByDate(date time.Time) ([]*DecisionRecord, error) {
	dateStr := date.Format("20060102")
	pattern := filepath.Join(l.logDir, fmt.Sprintf("decision_%s_*.json", dateStr))

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed lookup for log timeline metrics: %w", err)
	}

	var records []*DecisionRecord
	for _, filepath := range files {
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
	}

	return records, nil
}

// CleanOldRecords sweeps historically dated directory contents by constraints threshold
func (l *DecisionLogger) CleanOldRecords(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return fmt.Errorf("purge read evaluation directories failure: %w", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if file.ModTime().Before(cutoffTime) {
			filepath := filepath.Join(l.logDir, file.Name())
			if err := os.Remove(filepath); err != nil {
				fmt.Printf(" Failed to delete old record %s: %v\n", file.Name(), err)
				continue
			}
			removedCount++
		}
	}

	if removedCount > 0 {
		fmt.Printf(" Cleaned up %d old records (older than %d days)\n", removedCount, days)
	}

	return nil
}

// GetStatistics outputs globally captured session properties metadata
func (l *DecisionLogger) GetStatistics() (*Statistics, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("log mapping validation failed statistics: %w", err)
	}

	stats := &Statistics{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		stats.TotalCycles++

		for _, action := range record.Decisions {
			if action.Success {
				switch action.Action {
				case "open_long", "open_short":
					stats.TotalOpenPositions++
				case "close_long", "close_short":
					stats.TotalClosePositions++
				}
			}
		}

		if record.Success {
			stats.SuccessfulCycles++
		} else {
			stats.FailedCycles++
		}
	}

	return stats, nil
}

// Statistics tracks system runtime history attributes
type Statistics struct {
	TotalCycles         int `json:"total_cycles"`
	SuccessfulCycles    int `json:"successful_cycles"`
	FailedCycles        int `json:"failed_cycles"`
	TotalOpenPositions  int `json:"total_open_positions"`
	TotalClosePositions int `json:"total_close_positions"`
}

// TradeOutcome analyzes specific trade executions mappings metrics
type TradeOutcome struct {
	Symbol        string    `json:"symbol"`         // Symbol properties constraints
	Side          string    `json:"side"`           // long/short
	Quantity      float64   `json:"quantity"`       // Size valuation logic
	Leverage      int       `json:"leverage"`       // Trade application constraints setup ratio
	OpenPrice     float64   `json:"open_price"`     // Setup price execution constraints
	ClosePrice    float64   `json:"close_price"`    // Market target completion price configuration
	PositionValue float64   `json:"position_value"` // Evaluation size base bounds limits ratio setup mapped logic
	MarginUsed    float64   `json:"margin_used"`    // Usage calculation limits ratio evaluation metrics bounds (positionValue / leverage)
	PnL           float64   `json:"pn_l"`           // Output P&L margins ratio targets execution constraints (USDT bounds variables setup parameter values logic output values array)
	PnLPct        float64   `json:"pn_l_pct"`       // Return evaluation limits logic variables relative outputs values ratio targeting boundaries mappings configuration
	Duration      string    `json:"duration"`       // Timing metrics boundary mapping
	OpenTime      time.Time `json:"open_time"`      // Baseline evaluation metric markers limits bounds logic execution markers mapping setup arrays
	CloseTime     time.Time `json:"close_time"`     // Exit mapping tracking execution points
	WasStopLoss   bool      `json:"was_stop_loss"`  // Flag validation checking metrics boundary tracking boolean values array configuration arrays variables targeting mappings conditions setup limits bounds arrays Boolean ratio evaluation limits parameter constraints bounds evaluation setup configurations values ratios parameter maps parameter
}

// PerformanceAnalysis stores aggregated history performance analytics bounds mapping execution arrays parameters parameter values evaluation conditions setup combinations bounds variables configuration mapping setup bounds targets setup bounds logic lists parameters values variables array
type PerformanceAnalysis struct {
	TotalTrades   int                           `json:"total_trades"`   // Cumulative total operations executions loops maps array tracking combinations properties logic loops tracking evaluations combinations mapping mapping conditions variables variables mapping parameters variables mapping constraints targeting arrays array counts evaluation
	WinningTrades int                           `json:"winning_trades"` // Profit output variables execution metrics mapping tracking validation mapping values array tracking array array variables conditions arrays evaluation mapping parameters tracking combination variables constraints loops bounds targeting limits tracking arrays limitations arrays targets execution operations loops array mapping properties tracking
	LosingTrades  int                           `json:"losing_trades"`  // Loss logic arrays mapping evaluation
	WinRate       float64                       `json:"win_rate"`       // Win constraints maps ratio setup targeting mappings conditions evaluation properties parameters mapping tracking mappings combination logic targeting execution parameters values arrays bounds tracking parameters combinations limits mappings conditions logic arrays arrays arrays mapping limitations targets limitations tracking setup bounds limits values constraints conditions mapping Tracking
	AvgWin        float64                       `json:"avg_win"`        // Mean average tracking parameters bounds values array
	AvgLoss       float64                       `json:"avg_loss"`       // Output mapping calculation configuration mappings boundaries array evaluation loops parameters mapping conditions logic evaluation tracking Tracking
	ProfitFactor  float64                       `json:"profit_factor"`  // Configuration properties mapping loops limits values tracking
	SharpeRatio   float64                       `json:"sharpe_ratio"`   // Return adjusted tracking loops parameters metrics
	RecentTrades  []TradeOutcome                `json:"recent_trades"`  // Arrays variables lists setup parameters mapping values evaluation
	SymbolStats   map[string]*SymbolPerformance `json:"symbol_stats"`   // Values limitations tracking targets constraints parameters values parameters conditions mappings conditions setup
	BestSymbol    string                        `json:"best_symbol"`    // Constraints Array Lists loops Lists mapping Maps mapping values evaluation variables variables arrays Array metrics configuration loops setup bounds configuration combinations
	WorstSymbol   string                        `json:"worst_symbol"`   // Arrays lists bounds limitations tracking targets execution loops mapping evaluation evaluation configurations mapping mapping properties tracking
	MaxDrawdown   float64                       `json:"max_drawdown"`   // Maximum drawdown percentage based on peak equity
	EquityGrowth  float64                       `json:"equity_growth"`  // Total equity growth percentage from initial balance
}

// SymbolPerformance logs token operations output states bounds parameters variables logic mapping parameters values variables values constraints variables bounds limitations bounds evaluation bounds mappings limits execution targeting combinations metrics array setup targeting values configurations parameters setup limitations logic values arrays mappings arrays variables mapping Tracking loops evaluation configurations tracking Limits targeting arrays limits setup Limit Maps Limitations arrays limits
type SymbolPerformance struct {
	Symbol        string  `json:"symbol"`         // Symbol Tracking limits setups Mapping bounds bounds Limits mappings variables variables parameters conditions Tracking metrics setup combinations setups parameters Tracking limitations tracking Maps
	TotalTrades   int     `json:"total_trades"`   // Number variables mapping combinations combinations arrays arrays Loops variables target Tracking
	WinningTrades int     `json:"winning_trades"` // Loops conditions arrays Array Lists combinations variables Maps mapping combinations Tracking mapping maps Mapping
	LosingTrades  int     `json:"losing_trades"`  // Mapping Lists variables lists combinations tracking Bounds Lists setup constraints Tracking
	WinRate       float64 `json:"win_rate"`       // Evaluation metrics variables variables Limits setup combinations target boundaries variables mapping Lists Tracking variables Loops parameters Tracking limits tracking limitations targeting mappings Arrays Loops Mapping values bounds Maps Mapping configuration setups Tracking mapping maps Mapping
	TotalPnL      float64 `json:"total_pn_l"`     // PnL map Maps setup Constraints Maps limitations setup Maps Tracking values parameters tracking variables configurations combinations target mappings Arrays tracking Tracking combinations Tracking arrays Maps Lists setup combinations target Mapping tracking variables configurations arrays parameters setup Limits mappings variables limits evaluation Limit Map Limit parameters Maps configuration values limitation Variables Limits Lists Map limitations loops tracking Maps Map Map limitations setup Limits Maps Map limits evaluation loops array loops combinations setups limitations Lists Arrays Limits array configuration Variables Limits Mapping Array values limits limitations tracking limit Lists Target LIMIT combinations limitations Target Mapping limit Lists limits Mapping variables Tracking Array limitations Maps limitations combinations Array LIMIT Map limit variables Map variables Variables Lists Array combinations Target LIMIT map Mapping
	AvgPnL        float64 `json:"avg_pn_l"`       // Parameters parameters loops Tracking Target
}

// AnalyzePerformance calculates analytics mappings across specified evaluation cycles limits
func (l *DecisionLogger) AnalyzePerformance(lookbackCycles int) (*PerformanceAnalysis, error) {
	records, err := l.GetLatestRecords(lookbackCycles)
	if err != nil {
		return nil, fmt.Errorf("historical maps extraction pipeline failure maps Limit tracking array configurations bounds limitations Maps Limit targeting values Arrays Target Arrays combinations values limitations limitations Mapping variables Tracking Maps limits Mappings Tracking Maps Map limits Limits values Limit Map Map limit limitation Variables Limits lists combinations variables bounds Tracking Array Array values mapping Target Arrays limit limitations Tracking variables Maps Limit Map combinations variables limitation limitations Maps limit Limitations tracking Maps limit mapping variables Mapping Limit Target Limit limitations variables Limitations variables Mapping Mapping limit Target limitation variables limit Target variables Lists Target Mapping limits Target Target values Target Maps Target LIMIT map variables limitations limits limitation combinations limits Mapping variables limit variables map Map Map limits Mapping Map Mapping Target combinations variables Mapper Matrix LIMIT parameters Maps map Maps tracking: %w", err)
	}

	if len(records) == 0 {
		return &PerformanceAnalysis{
			RecentTrades: []TradeOutcome{},
			SymbolStats:  make(map[string]*SymbolPerformance),
		}, nil
	}

	analysis := &PerformanceAnalysis{
		RecentTrades: []TradeOutcome{},
		SymbolStats:  make(map[string]*SymbolPerformance),
	}

	// Track open states mapping: symbol_side -> {side, openPrice, openTime, quantity, leverage}
	openPositions := make(map[string]map[string]interface{})

	// Pre-fill trailing datasets loops checking arrays
	allRecords, err := l.GetLatestRecords(lookbackCycles * 3)
	if err == nil && len(allRecords) > len(records) {
		for _, record := range allRecords {
			for _, action := range record.Decisions {
				if !action.Success {
					continue
				}

				symbol := action.Symbol
				side := ""
				if action.Action == "open_long" || action.Action == "close_long" {
					side = "long"
				} else if action.Action == "open_short" || action.Action == "close_short" {
					side = "short"
				}
				posKey := symbol + "_" + side

				switch action.Action {
				case "open_long", "open_short":
					openPositions[posKey] = map[string]interface{}{
						"side":      side,
						"openPrice": action.Price,
						"openTime":  action.Timestamp,
						"quantity":  action.Quantity,
						"leverage":  action.Leverage,
					}
				case "close_long", "close_short":
					delete(openPositions, posKey)
				}
			}
		}
	}

	for _, record := range records {
		for _, action := range record.Decisions {
			if !action.Success {
				continue
			}

			symbol := action.Symbol
			side := ""
			if action.Action == "open_long" || action.Action == "close_long" {
				side = "long"
			} else if action.Action == "open_short" || action.Action == "close_short" {
				side = "short"
			}
			posKey := symbol + "_" + side

			switch action.Action {
			case "open_long", "open_short":
				openPositions[posKey] = map[string]interface{}{
					"side":      side,
					"openPrice": action.Price,
					"openTime":  action.Timestamp,
					"quantity":  action.Quantity,
					"leverage":  action.Leverage,
				}

			case "close_long", "close_short":
				if openPos, exists := openPositions[posKey]; exists {
					openPrice := openPos["openPrice"].(float64)
					openTime := openPos["openTime"].(time.Time)
					side := openPos["side"].(string)
					quantity := openPos["quantity"].(float64)
					leverage := openPos["leverage"].(int)

					// Calculate actual P&L (USDT) mappings validation bounds limit checking ratio variables limitations configurations target Tracking Array limits bounds Limit limitation Maps Array variables Tracking limitation Map LIMIT Tracking combinations variables combinations variables Maps limit limitation Target Target Map Maps Maps limit Target limit Target variables limits Target combinations limit limitations
					var pnl float64
					if side == "long" {
						pnl = quantity * (action.Price - openPrice)
					} else {
						pnl = quantity * (openPrice - action.Price)
					}

					positionValue := quantity * openPrice
					marginUsed := positionValue / float64(leverage)
					pnlPct := 0.0
					if marginUsed > 0 {
						pnlPct = (pnl / marginUsed) * 100
					}

					outcome := TradeOutcome{
						Symbol:        symbol,
						Side:          side,
						Quantity:      quantity,
						Leverage:      leverage,
						OpenPrice:     openPrice,
						ClosePrice:    action.Price,
						PositionValue: positionValue,
						MarginUsed:    marginUsed,
						PnL:           pnl,
						PnLPct:        pnlPct,
						Duration:      action.Timestamp.Sub(openTime).String(),
						OpenTime:      openTime,
						CloseTime:     action.Timestamp,
					}

					analysis.RecentTrades = append(analysis.RecentTrades, outcome)
					analysis.TotalTrades++

					if pnl > 0 {
						analysis.WinningTrades++
						analysis.AvgWin += pnl
					} else if pnl < 0 {
						analysis.LosingTrades++
						analysis.AvgLoss += pnl
					}

					if _, exists := analysis.SymbolStats[symbol]; !exists {
						analysis.SymbolStats[symbol] = &SymbolPerformance{
							Symbol: symbol,
						}
					}
					stats := analysis.SymbolStats[symbol]
					stats.TotalTrades++
					stats.TotalPnL += pnl
					if pnl > 0 {
						stats.WinningTrades++
					} else if pnl < 0 {
						stats.LosingTrades++
					}

					delete(openPositions, posKey)
				}
			}
		}
	}

	if analysis.TotalTrades > 0 {
		analysis.WinRate = (float64(analysis.WinningTrades) / float64(analysis.TotalTrades)) * 100

		totalWinAmount := analysis.AvgWin
		totalLossAmount := analysis.AvgLoss

		if analysis.WinningTrades > 0 {
			analysis.AvgWin /= float64(analysis.WinningTrades)
		}
		if analysis.LosingTrades > 0 {
			analysis.AvgLoss /= float64(analysis.LosingTrades)
		}

		if totalLossAmount != 0 {
			analysis.ProfitFactor = totalWinAmount / (-totalLossAmount)
		} else if totalWinAmount > 0 {
			analysis.ProfitFactor = 999.0
		}
	}

	bestPnL := -999999.0
	worstPnL := 999999.0
	for symbol, stats := range analysis.SymbolStats {
		if stats.TotalTrades > 0 {
			stats.WinRate = (float64(stats.WinningTrades) / float64(stats.TotalTrades)) * 100
			stats.AvgPnL = stats.TotalPnL / float64(stats.TotalTrades)

			if stats.TotalPnL > bestPnL {
				bestPnL = stats.TotalPnL
				analysis.BestSymbol = symbol
			}
			if stats.TotalPnL < worstPnL {
				worstPnL = stats.TotalPnL
				analysis.WorstSymbol = symbol
			}
		}
	}

	if len(analysis.RecentTrades) > 10 {
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
		analysis.RecentTrades = analysis.RecentTrades[:10]
	} else if len(analysis.RecentTrades) > 0 {
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
	}

	sharpe, maxDD, growth := l.calculateEquityMetrics(records)
	analysis.SharpeRatio = sharpe
	analysis.MaxDrawdown = maxDD
	analysis.EquityGrowth = growth

	return analysis, nil
}

// calculateEquityMetrics computes Sharpe Ratio, Max Drawdown, and Equity Growth
func (l *DecisionLogger) calculateEquityMetrics(records []*DecisionRecord) (float64, float64, float64) {
	if len(records) == 0 {
		return 0.0, 0.0, 0.0
	}

	var equities []float64
	for _, record := range records {
		if equity, ok := record.AccountState.EffectiveStrategyEquity(); ok && equity > 0 {
			equities = append(equities, equity)
			continue
		}
		if equity := record.AccountState.EffectiveAccountEquity(); equity > 0 {
			equities = append(equities, equity)
		}
	}

	if len(equities) == 0 {
		return 0.0, 0.0, 0.0
	}

	initialEquity := equities[0]
	finalEquity := equities[len(equities)-1]
	equityGrowth := 0.0
	if initialEquity > 0 {
		equityGrowth = ((finalEquity - initialEquity) / initialEquity) * 100
	}

	peakEquity := equities[0]
	maxDrawdown := 0.0

	for _, eq := range equities {
		if eq > peakEquity {
			peakEquity = eq
		}
		if peakEquity > 0 {
			dd := ((peakEquity - eq) / peakEquity) * 100
			if dd > maxDrawdown {
				maxDrawdown = dd
			}
		}
	}

	if len(equities) < 2 {
		return 0.0, maxDrawdown, equityGrowth
	}

	var returns []float64
	for i := 1; i < len(equities); i++ {
		if equities[i-1] > 0 {
			periodReturn := (equities[i] - equities[i-1]) / equities[i-1]
			returns = append(returns, periodReturn)
		}
	}

	if len(returns) == 0 {
		return 0.0, maxDrawdown, equityGrowth
	}

	sumReturns := 0.0
	for _, r := range returns {
		sumReturns += r
	}
	meanReturn := sumReturns / float64(len(returns))

	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	variance := sumSquaredDiff / float64(len(returns))
	stdDev := math.Sqrt(variance)

	if stdDev == 0 {
		if meanReturn > 0 {
			return 999.0, maxDrawdown, equityGrowth
		} else if meanReturn < 0 {
			return -999.0, maxDrawdown, equityGrowth
		}
		return 0.0, maxDrawdown, equityGrowth
	}

	sharpeRatio := meanReturn / stdDev
	return sharpeRatio, maxDrawdown, equityGrowth
}
