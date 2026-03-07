package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"aegistrade/market"
	"aegistrade/mcp"
	"aegistrade/pool"
	"strings"
	"time"
)

// PositionInfo holds position information
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // "long" or "short"
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	Quantity         float64 `json:"quantity"`
	Leverage         int     `json:"leverage"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	LiquidationPrice float64 `json:"liquidation_price"`
	MarginUsed       float64 `json:"margin_used"`
	UpdateTime       int64   `json:"update_time"` // Position update timestamp in ms
}

// AccountInfo holds account information
type AccountInfo struct {
	TotalEquity      float64 `json:"total_equity"`      // Total account equity
	AvailableBalance float64 `json:"available_balance"` // Available balance
	TotalPnL         float64 `json:"total_pnl"`         // Total P&L
	TotalPnLPct      float64 `json:"total_pnl_pct"`     // Total P&L percentage
	MarginUsed       float64 `json:"margin_used"`       // Margin already used
	MarginUsedPct    float64 `json:"margin_used_pct"`   // Margin usage percentage
	PositionCount    int     `json:"position_count"`    // Number of active positions
}

// CandidateCoin holds candidate coin data (from coin pool)
type CandidateCoin struct {
	Symbol  string   `json:"symbol"`
	Sources []string `json:"sources"` // Source: "ai500" and/or "oi_top"
}

// OITopData holds open interest growth top data (for AI reference)
type OITopData struct {
	Rank              int     // OI Top rank
	OIDeltaPercent    float64 // OI change percentage (1 hour)
	OIDeltaValue      float64 // OI change value
	PriceDeltaPercent float64 // Price change percentage
	NetLong           float64 // Net long volume
	NetShort          float64 // Net short volume
}

// Context holds trading context (complete information sent to AI)
type Context struct {
	CurrentTime     string                  `json:"current_time"`
	RuntimeMinutes  int                     `json:"runtime_minutes"`
	CallCount       int                     `json:"call_count"`
	Account         AccountInfo             `json:"account"`
	Positions       []PositionInfo          `json:"positions"`
	CandidateCoins  []CandidateCoin         `json:"candidate_coins"`
	MarketDataMap   map[string]*market.Data `json:"-"` // Internal use, not serialized
	OITopDataMap    map[string]*OITopData   `json:"-"` // OI Top data map
	Performance     interface{}             `json:"-"` // Historical performance analysis
	BTCETHLeverage  int                     `json:"-"` // BTC/ETH leverage multiplier
	AltcoinLeverage int                     `json:"-"` // Altcoin leverage multiplier

	// Data Provider settings
	Provider       market.BarsProvider `json:"-"`
	InstrumentType string              `json:"-"` // "crypto_perp" or "equity"
	BarsAdjustment string              `json:"-"` // "raw", "split", "dividend", "all"
	IsReplay       bool                `json:"-"`
}

// Decision represents AI trading decision
type Decision struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"` // "open_long", "open_short", "close_long", "close_short", "hold", "wait"
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLoss        float64 `json:"stop_loss,omitempty"`
	TakeProfit      float64 `json:"take_profit,omitempty"`
	Confidence      int     `json:"confidence,omitempty"` // Confidence level (0-100)
	RiskUSD         float64 `json:"risk_usd,omitempty"`   // Maximum risk in USD
	Reasoning       string  `json:"reasoning"`
}

// FullDecision represents complete AI decision (including Chain of Thought)
type FullDecision struct {
	UserPrompt string     `json:"user_prompt"` // Input prompt sent to AI
	CoTTrace   string     `json:"cot_trace"`   // Chain of Thought analysis
	Decisions  []Decision `json:"decisions"`   // Specific decision list
	Timestamp  time.Time  `json:"timestamp"`
}

// GetFullDecision gets complete AI trading decision (batch analyze all coins and positions)
func GetFullDecision(ctx *Context, mcpClient *mcp.Client) (*FullDecision, error) {
	// 1. Fetch market data for all coins
	if err := fetchMarketDataForContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch market data: %w", err)
	}

	// 2. Build System Prompt (fixed rules) and User Prompt (dynamic data)
	systemPrompt := buildSystemPrompt(ctx.Account.TotalEquity, ctx.BTCETHLeverage, ctx.AltcoinLeverage, ctx.InstrumentType, ctx.IsReplay)
	userPrompt := buildUserPrompt(ctx)

	// 3. Call AI API (using system + user prompt)
	aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}

	// 4. Parse AI response
	decision, err := parseFullDecisionResponse(aiResponse, ctx.Account.TotalEquity, ctx.BTCETHLeverage, ctx.AltcoinLeverage, ctx.InstrumentType, ctx.IsReplay)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	decision.Timestamp = time.Now()
	decision.UserPrompt = userPrompt // Save input prompt
	return decision, nil
}

// fetchMarketDataForContext fetches market and OI data for all coins in context
func fetchMarketDataForContext(ctx *Context) error {
	ctx.MarketDataMap = make(map[string]*market.Data)
	ctx.OITopDataMap = make(map[string]*OITopData)

	// Collect all coins that require data fetching
	symbolSet := make(map[string]bool)

	// 1. Prioritize symbols with active positions (mandatory)
	for _, pos := range ctx.Positions {
		symbolSet[pos.Symbol] = true
	}

	// 2. Adjust candidate coin quantity dynamically based on Account state
	maxCandidates := calculateMaxCandidates(ctx)
	for i, coin := range ctx.CandidateCoins {
		if i >= maxCandidates {
			break
		}
		symbolSet[coin.Symbol] = true
	}

	// Active position symbols (used to determine whether to skip OI check)
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	totalSymbols := len(symbolSet)
	failedFetches := 0

	// Fetch market data for selected symbols
	for symbol := range symbolSet {
		req := market.GetRequest{
			Symbol:         symbol,
			Provider:       ctx.Provider,
			InstrumentType: ctx.InstrumentType,
			BarsAdjustment: ctx.BarsAdjustment,
		}

		data, err := market.Get(req)
		if err != nil {
			failedFetches++
			if failedFetches <= 5 {
				log.Printf(" Market data fetch failed for %s: %v", symbol, err)
			}
			continue
		}

		//  Liquidity filtering: skip coins with OI value < 15M USD (both longs and shorts)
		// Position value = OI size  current price
		// But existing positions must be kept (need decision whether to close)
		isExistingPosition := positionSymbols[symbol]
		if ctx.InstrumentType != "equity" && !isExistingPosition && data.OpenInterest != nil && data.CurrentPrice > 0 {
			// Calculate OI value (USD)
			oiValue := data.OpenInterest.Latest * data.CurrentPrice
			oiValueInMillions := oiValue / 1_000_000 // Convert to millions
			if oiValueInMillions < 15 {
				log.Printf("  Skipping %s due to low OI value (%.2fM USD < 15M) [size: %.0f  price: %.4f]",
					symbol, oiValueInMillions, data.OpenInterest.Latest, data.CurrentPrice)
				continue
			}
		}

		ctx.MarketDataMap[symbol] = data
	}

	if failedFetches > 0 {
		log.Printf(" Market data fetch failures: %d/%d symbols", failedFetches, totalSymbols)
	}
	if len(ctx.MarketDataMap) == 0 && totalSymbols > 0 {
		log.Printf(" No market data loaded for this cycle (instrument=%s)", ctx.InstrumentType)
	}

	// Load OI Top data (does not affect main workflow)
	oiPositions, err := pool.GetOITopPositions()
	if err == nil {
		for _, pos := range oiPositions {
			// Normalize symbol formatting
			symbol := pos.Symbol
			ctx.OITopDataMap[symbol] = &OITopData{
				Rank:              pos.Rank,
				OIDeltaPercent:    pos.OIDeltaPercent,
				OIDeltaValue:      pos.OIDeltaValue,
				PriceDeltaPercent: pos.PriceDeltaPercent,
				NetLong:           pos.NetLong,
				NetShort:          pos.NetShort,
			}
		}
	}

	return nil
}

// calculateMaxCandidates calculate required number of candidate coins based on account state
func calculateMaxCandidates(ctx *Context) int {
	// Limit IBKR equity batch size to reduce pacing/timeouts.
	if ctx.InstrumentType == "equity" {
		if len(ctx.CandidateCoins) > 12 {
			return 12
		}
	}
	return len(ctx.CandidateCoins)
}

// buildSystemPrompt builds the System Prompt (fixed rules, cacheable)
func buildSystemPrompt(accountEquity float64, btcEthLeverage, altcoinLeverage int, instrumentType string, isReplay bool) string {
	var sb strings.Builder

	isEquity := instrumentType == "equity"

	// === Core Mission ===
	if isReplay {
		sb.WriteString("You are the replay testing program for the AegisTrade trading system.\n")
		sb.WriteString("#  Replay Demo Mode (CRITICAL)\n\n")
		sb.WriteString("Currently in demo replay mode, you must **relax all strict opening requirements**, main purpose is **system execution testing**.\n")
		sb.WriteString("1. **Force Execution Testing**: Please select at least one symbol you consider potential in the current cycle to open a position (long/short both fine). Do not keep waiting. We are testing the order execution system!\n")
		sb.WriteString("2. **Stop-loss / Take-profit**: Just set reasonable values.\n\n")
	} else if isEquity {
		sb.WriteString("You are a professional US equities trading AI, conducting autonomous trades in the US stock market.\n\n")
	} else {
		sb.WriteString("You are a professional crypto trading AI, conducting autonomous trades in the futures market.\n\n")
	}

	sb.WriteString("#  Core Goals\n\n")
	sb.WriteString("**Maximize Sharpe Ratio**\n\n")
	sb.WriteString("Sharpe Ratio = Average Return / Return Volatility\n\n")
	sb.WriteString("**Which means**:\n")
	sb.WriteString("-  High quality trades (High win rate, high P&L ratio)  Boosts Sharpe\n")
	sb.WriteString("-  Stable returns, controlled drawdown  Boosts Sharpe\n")
	sb.WriteString("-  Patience with positions, letting profits run  Boosts Sharpe\n")
	sb.WriteString("-  Frequent trading, tiny gains/losses  Increases volatility, severely degrades Sharpe\n")
	sb.WriteString("-  Over-trading, fee depletion  Direct loss\n")
	sb.WriteString("-  Closing too early, impatient entries/exits  Missing big trends\n\n")
	if !isReplay {
		sb.WriteString("**Critical Insight**: System scans periodically, but doesn't mean you trade every time!\n")
		sb.WriteString("Most of the time it should be `wait` or `hold`, only open positions during excellent opportunities.\n\n")
	}

	// === Hard Constraints (Risk Control) ===
	sb.WriteString("#  Hard Constraints (Risk Control)\n\n")
	sb.WriteString("1. **Risk-Reward Ratio**: Must be >= 1:3 (Risk 1% to make 3%+ return)\n")
	sb.WriteString("2. **Max Positions**: 3 positions (Quality > Quantity)\n")

	if isEquity {
		sb.WriteString(fmt.Sprintf("3. **Single Asset Max Size**: Max 20%% of account equity (about %.0f USD), no leverage required\n", accountEquity*0.2))
	} else {
		sb.WriteString(fmt.Sprintf("3. **Single Coin Max Size**: Altcoins %.0f-%.0f U (%dx Leverage) | BTC/ETH %.0f-%.0f U (%dx Leverage)\n",
			accountEquity*0.8, accountEquity*1.5, altcoinLeverage, accountEquity*5, accountEquity*10, btcEthLeverage))
	}

	sb.WriteString("4. **Capital Utilization**: Total usage <= 90%\n\n")

	// === Shorting Incentive ===
	sb.WriteString("#  Long-Short Balance\n\n")
	sb.WriteString("**Important**: Profit from shorting downtrends = Profit from longing uptrends\n\n")
	sb.WriteString("- Uptrend  Go Long\n")
	sb.WriteString("- Downtrend  Go Short\n")
	sb.WriteString("- Ranging Market  Wait\n\n")
	sb.WriteString("**Do NOT have a long bias! Shorting is one of your core tools.**\n\n")

	// === Trading Frequency Insight ===
	sb.WriteString("#  Trading Frequency Insight\n\n")
	sb.WriteString("**Quantitative standards**:\n")
	if !isReplay {
		sb.WriteString("- Elite traders: 2-4 trades a day = 0.1-0.2 trades per hour\n")
		sb.WriteString("- Over-trading: >2 trades per hour = Severe problem\n")
	} else {
		sb.WriteString("- Replay Mode: Please actively test trading executions\n")
	}
	sb.WriteString("- Optimal pacing: Hold positions for at least 30-60 minutes after opening\n\n")
	if !isReplay {
		sb.WriteString("**Self-Check**:\n")
		sb.WriteString("If you find yourself executing trades every cycle  Standards are too low\n")
		sb.WriteString("If you close positions within <30 mins  You are too impatient\n\n")
	}

	// === Signal Strength ===
	sb.WriteString("#  Entry Standards (Strict)\n\n")
	if !isReplay {
		sb.WriteString("Only open positions during **Strong Signals**, wait if uncertain.\n\n")
	} else {
		sb.WriteString("In Demo mode, you can open positions freely, no need to strictly follow strong signals.\n\n")
	}
	sb.WriteString("**Complete data available to you**:\n")
	sb.WriteString("-  **Price Series**: Short-term k-lines (MidPrices array) + Long-term k-lines\n")
	sb.WriteString("-  **Technical Series**: EMA20, MACD, RSI7, RSI14 series\n")
	sb.WriteString("-  **Volume Series**: Trading volume series\n")
	if !isEquity {
		sb.WriteString("-  **Derivatives Data**: Open Interest (OI) series, Funding Rates\n")
	}
	sb.WriteString("**Analysis Methods** (Completely up to you):\n")
	sb.WriteString("- Freely utilize series data for trend analysis, pattern recognition, support/resistance detection, fibonacci, or volatility bands constraints\n")
	sb.WriteString("- Multi-dimensional cross validation (Price + Volume + OI + Indicators + K-line Patterns)\n")
	sb.WriteString("- Use the most effective methods you know to spot high certainty opportunities\n")
	if !isReplay {
		sb.WriteString("- Composite confidence score >= 75 is required to open positions\n\n")
		sb.WriteString("**Avoid weak signals**:\n")
		sb.WriteString("- Single dimension (Only relying on one indicator)\n")
		sb.WriteString("- Contradicting metrics (Price rising but volume shrinking)\n")
		sb.WriteString("- Sideways ranging chop\n")
		sb.WriteString("- Just closed a position recently (< 15 mins ago)\n\n")
	}

	// === Sharpe Ratio Self-Evolution ===
	sb.WriteString("#  Sharpe Ratio Self-Evolution\n\n")
	sb.WriteString("Every cycle you will receive **Sharpe Ratio** as a performance feedback metric:\n\n")
	sb.WriteString("**Sharpe Ratio < -0.5** (Consistent Losses):\n")
	sb.WriteString("    Stop trading immediately, stand by for at least 6 cycles (18 minutes)\n")
	sb.WriteString("    Deep Retrospection:\n")
	sb.WriteString("      Trading frequency too high? (>2x an hour is excessive)\n")
	sb.WriteString("      Holding duration too short? (<30 mins is closing too early)\n")
	sb.WriteString("      Weak signal strength? (Confidence < 75)\n")
	sb.WriteString("      Are you shorting when applicable? (Permabull long bias is wrong)\n\n")
	sb.WriteString("**Sharpe Ratio -0.5 ~ 0** (Mild Losses):\n")
	sb.WriteString("    Strict Control: Only execute confidence > 80 trades\n")
	sb.WriteString("   Reduce frequency: Max 1 new trade per hour\n")
	sb.WriteString("   Be patient: Hold positions for at least 30 minutes\n\n")
	sb.WriteString("**Sharpe Ratio 0 ~ 0.7** (Positive Returns):\n")
	sb.WriteString("    Maintain current strategy\n\n")
	sb.WriteString("**Sharpe Ratio > 0.7** (Exceptional Returns):\n")
	sb.WriteString("    Moderately increase position sizing\n\n")
	sb.WriteString("**CRITICAL**: Sharpe ratio is the holy grail metric, it naturally penalizes excessive trading and chop.\n\n")

	// === Decision Workflow ===
	sb.WriteString("#  Decision Workflow\n\n")
	sb.WriteString("1. **Analyze Sharpe Ratio**: Is current strategy working? Needs adjustment?\n")
	sb.WriteString("2. **Evaluate Open Positions**: Trend changed? Need to take profit/stop loss?\n")
	sb.WriteString("3. **Scan New Opportunities**: Strong signals present? Long or short?\n")
	sb.WriteString("4. **Output Decision**: Chain of Thought reasoning + JSON result\n\n")

	// === Output Formatting ===
	sb.WriteString("#  Output Formatting\n\n")
	sb.WriteString("**Step 1: Chain of Thought (Plain text)**\n")
	sb.WriteString("Concise elaboration of your analysis process\n\n")
	sb.WriteString("**Step 2: JSON Decision Array**\n\n")
	sb.WriteString("```json\n[\n")

	if isEquity {
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"AAPL\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 250, \"take_profit\": 230, \"confidence\": 85, \"risk_usd\": 300, \"reasoning\": \"Downtrend + MACD bearish cross\"},\n", 1, accountEquity*0.2))
		sb.WriteString("  {\"symbol\": \"MSFT\", \"action\": \"close_long\", \"reasoning\": \"Take profit exit\"}\n")
	} else {
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"risk_usd\": 300, \"reasoning\": \"Downtrend + MACD bearish cross\"},\n", btcEthLeverage, accountEquity*5))
		sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"reasoning\": \"Take profit exit\"}\n")
	}

	sb.WriteString("]\n```\n\n")
	sb.WriteString("**Field Definitions**:\n")
	sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
	sb.WriteString("- `confidence`: 0-100 (Suggest >= 75 for entries)\n")
	sb.WriteString("- Required for entries: leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd, reasoning\n\n")

	// === Critical Reminders ===
	sb.WriteString("---\n\n")
	sb.WriteString("**Remember**: \n")
	sb.WriteString("- The goal is Sharpe Ratio, not trading frequently\n")
	sb.WriteString("- Shorting = Longing, both are tools to make money\n")
	sb.WriteString("- Better to miss out than force a low conviction trade\n")
	sb.WriteString("- Risk-Reward of 1:3 is the hard boundary\n")

	return sb.String()
}

// buildUserPrompt builds User Prompt (dynamic payload)
func buildUserPrompt(ctx *Context) string {
	var sb strings.Builder

	// System State
	sb.WriteString(fmt.Sprintf("**Time**: %s | **Cycle**: #%d | **Uptime**: %d minutes\n\n",
		ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))

	// BTC Market Context
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		sb.WriteString(fmt.Sprintf("**BTC**: %.2f (1h: %+.2f%%, 4h: %+.2f%%) | MACD: %.4f | RSI: %.2f\n\n",
			btcData.CurrentPrice, btcData.PriceChange1h, btcData.PriceChange4h,
			btcData.CurrentMACD, btcData.CurrentRSI7))
	}

	// Account Data
	sb.WriteString(fmt.Sprintf("**Account**: Equity %.2f | Available %.2f (%.1f%%) | P&L %+.2f%% | Margin Used %.1f%% | Positions: %d\n\n",
		ctx.Account.TotalEquity,
		ctx.Account.AvailableBalance,
		(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
		ctx.Account.TotalPnLPct,
		ctx.Account.MarginUsedPct,
		ctx.Account.PositionCount))

	// Active Positions (with full market context)
	if len(ctx.Positions) > 0 {
		sb.WriteString("## Active Positions\n")
		for i, pos := range ctx.Positions {
			// Calculate holding duration
			holdingDuration := ""
			if pos.UpdateTime > 0 {
				durationMs := time.Now().UnixMilli() - pos.UpdateTime
				durationMin := durationMs / (1000 * 60) // Convert to minutes
				if durationMin < 60 {
					holdingDuration = fmt.Sprintf(" | Holding %d mins", durationMin)
				} else {
					durationHour := durationMin / 60
					durationMinRemainder := durationMin % 60
					holdingDuration = fmt.Sprintf(" | Holding %dh %dm", durationHour, durationMinRemainder)
				}
			}

			sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Current %.4f | P&L %+.2f%% | %dx Lev | Margin %.0f | Liq %.4f%s\n\n",
				i+1, pos.Symbol, strings.ToUpper(pos.Side),
				pos.EntryPrice, pos.MarkPrice, pos.UnrealizedPnLPct,
				pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))

			// Use FormatMarketData to inject complete market insights
			if marketData, ok := ctx.MarketDataMap[pos.Symbol]; ok {
				sb.WriteString(market.Format(marketData))
				sb.WriteString("\n")
			}
		}
	} else {
		sb.WriteString("**Active Positions**: None\n\n")
	}

	// Candidate Coins (with full market context)
	sb.WriteString(fmt.Sprintf("## Candidate Coins (%d)\n\n", len(ctx.MarketDataMap)))
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		sourceTags := ""
		if len(coin.Sources) > 1 {
			sourceTags = " (AI500 + OI_Top Dual Signal)"
		} else if len(coin.Sources) == 1 && coin.Sources[0] == "oi_top" {
			sourceTags = " (OI_Top Growth Spike)"
		}

		// Inject Market Context
		sb.WriteString(fmt.Sprintf("### %d. %s%s\n\n", displayedCount, coin.Symbol, sourceTags))
		sb.WriteString(market.Format(marketData))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Sharpe Ratio metrics
	if ctx.Performance != nil {
		type PerformanceData struct {
			SharpeRatio float64 `json:"sharpe_ratio"`
		}
		var perfData PerformanceData
		if jsonData, err := json.Marshal(ctx.Performance); err == nil {
			if err := json.Unmarshal(jsonData, &perfData); err == nil {
				sb.WriteString(fmt.Sprintf("##  Sharpe Ratio: %.2f\n\n", perfData.SharpeRatio))
			}
		}
	}

	sb.WriteString("---\n\n")
	sb.WriteString("Please analyze thoroughly and output decision (Chain of Thought + JSON payload)\n")

	return sb.String()
}

// parseFullDecisionResponse parses AI's complete decision payload
func parseFullDecisionResponse(aiResponse string, accountEquity float64, btcEthLeverage, altcoinLeverage int, instrumentType string, isReplay bool) (*FullDecision, error) {
	// 1. Extract Chain of Thought
	cotTrace := extractCoTTrace(aiResponse)

	// 2. Extract JSON decision array
	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: []Decision{},
		}, fmt.Errorf("Failed to extract decisions: %w\n\n=== AI Chain of Thought ===\n%s", err, cotTrace)
	}

	// 3. Validate decisions
	if err := validateDecisions(decisions, accountEquity, btcEthLeverage, altcoinLeverage, instrumentType, isReplay); err != nil {
		return &FullDecision{
			CoTTrace:  cotTrace,
			Decisions: decisions,
		}, fmt.Errorf("Decision payload validation failed: %w\n\n=== AI Chain of Thought ===\n%s", err, cotTrace)
	}

	return &FullDecision{
		CoTTrace:  cotTrace,
		Decisions: decisions,
	}, nil
}

// extractCoTTrace extracts the prepended Chain of Thought reasoning array
func extractCoTTrace(response string) string {
	jsonStart := strings.Index(response, "[")

	if jsonStart > 0 {
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}

// extractDecisions pulls the JSON payload logic from the AI's response text
func extractDecisions(response string) ([]Decision, error) {
	arrayStart := strings.Index(response, "[")
	if arrayStart == -1 {
		return nil, fmt.Errorf("could not find JSON starting array bracket")
	}

	arrayEnd := findMatchingBracket(response, arrayStart)
	if arrayEnd == -1 {
		return nil, fmt.Errorf("could not find JSON trailing array bracket")
	}

	jsonContent := strings.TrimSpace(response[arrayStart : arrayEnd+1])

	//  Fix common JSON LLM hallucinations: missing quotes
	jsonContent = fixMissingQuotes(jsonContent)

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, fmt.Errorf("JSON parse failure: %w\nPayload Output: %s", err, jsonContent)
	}

	return decisions, nil
}

// fixMissingQuotes resolves rogue translation formatting quotes
func fixMissingQuotes(jsonStr string) string {
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"") // "
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")  // '
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")  // '
	return jsonStr
}

// validateDecisions loops validation constraints across all array objects
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, instrumentType string, isReplay bool) error {
	for i, decision := range decisions {
		if err := validateDecision(&decision, accountEquity, btcEthLeverage, altcoinLeverage, instrumentType, isReplay); err != nil {
			return fmt.Errorf("Decision #%d failed validation: %w", i+1, err)
		}
	}
	return nil
}

// findMatchingBracket extracts nested scopes safely
func findMatchingBracket(s string, start int) int {
	if start >= len(s) || s[start] != '[' {
		return -1
	}

	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

// validateDecision enforces risk control limits against individual decisions
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, instrumentType string, isReplay bool) error {
	// Validate action limits
	validActions := map[string]bool{
		"open_long":   true,
		"open_short":  true,
		"close_long":  true,
		"close_short": true,
		"hold":        true,
		"wait":        true,
	}

	if !validActions[d.Action] {
		return fmt.Errorf("invalid action designated: %s", d.Action)
	}

	// Explicit opening parameters constraint guard
	if d.Action == "open_long" || d.Action == "open_short" {
		isEquity := instrumentType == "equity"

		maxLeverage := altcoinLeverage
		maxPositionValue := accountEquity * 1.5 // Altcoin accounts max size 1.5x

		if isEquity {
			maxLeverage = 1
			maxPositionValue = accountEquity * 0.2 // Max 20% of equity per US stock position
		} else if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			maxLeverage = btcEthLeverage
			maxPositionValue = accountEquity * 10 // Max 10x equity per BTC/ETH position
		}

		if d.Leverage <= 0 || d.Leverage > maxLeverage {
			if isEquity {
				return fmt.Errorf("leverage strictly prohibited for equities, leverage parameters must be 1, found: %d", d.Leverage)
			}
			return fmt.Errorf("leverage multiplier must range 1-%d (target %s with %dx config top-limit max): %d", maxLeverage, d.Symbol, maxLeverage, d.Leverage)
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("position allocation sizing must remain greater than zero, found: %.2f", d.PositionSizeUSD)
		}

		// Validation limit constraints evaluation against size and maximum margin (with safety floats padding)
		tolerance := maxPositionValue * 0.01 // 1% calculation deviation padding buffer
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			if isEquity {
				return fmt.Errorf("max single stock position value rejected formatting. Total exceeds %.0f USD allowance ceiling (20%% limit param). Actual mapped bounds: %.0f", maxPositionValue, d.PositionSizeUSD)
			} else if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
				return fmt.Errorf("single BTC/ETH valuation exceeds %.0f USDT param-lock (10x ratio ceiling). Engine computed: %.0f", maxPositionValue, d.PositionSizeUSD)
			} else {
				return fmt.Errorf("volatile alt-coin constraint limits exceeded. Bounds crossed past %.0f USDT ratio logic (1.5x constraint ceiling params). Computed output logic value: %.0f", maxPositionValue, d.PositionSizeUSD)
			}
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			return fmt.Errorf("stop loss properties and take profit parameter arguments MUST remain explicitly above zero markers")
		}

		// Directional StopLoss/Take Profit configuration mapping
		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				return fmt.Errorf("long position structure validation halted: Stop loss must track beneath take-profit params")
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				return fmt.Errorf("short position breakdown validation halted: Stop loss marker must exceed execution target tracking take-profit fields")
			}
		}

		// Risk Reward mapping configurations constraint ratios parsing (Must hover >= 1:3 bounds constraint baseline standard)
		var entryPrice float64
		if d.Action == "open_long" {
			entryPrice = d.StopLoss + (d.TakeProfit-d.StopLoss)*0.2
		} else {
			entryPrice = d.StopLoss - (d.StopLoss-d.TakeProfit)*0.2
		}

		var riskPercent, rewardPercent, riskRewardRatio float64
		if d.Action == "open_long" {
			riskPercent = (entryPrice - d.StopLoss) / entryPrice * 100
			rewardPercent = (d.TakeProfit - entryPrice) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		} else {
			riskPercent = (d.StopLoss - entryPrice) / entryPrice * 100
			rewardPercent = (entryPrice - d.TakeProfit) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		}

		// Verification structure enforcement metric
		if !isReplay && riskRewardRatio < 3.0 {
			return fmt.Errorf("engine validation failure evaluating risk reward thresholds. Ratio drops beneath acceptable (%.2f:1) metric bounds! Values strictly require baseline ranges targeting >= 3.0:1 constraint [Calculated Risk Matrix Limits:%.2f%% Returns/Yield Evaluation:%.2f%%] [Stops logic properties params:%.2f Execution mapping TP properties bounds:%.2f]",
				riskRewardRatio, riskPercent, rewardPercent, d.StopLoss, d.TakeProfit)
		}
	}

	return nil
}
