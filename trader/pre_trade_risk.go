package trader

import (
	"fmt"
	"log"
	"math"
	"northstar/decision"
	"northstar/logger"
	"northstar/market"
	"northstar/risk"
	"sort"
	"strings"
)

type preTradeRiskContext struct {
	positions   []map[string]interface{}
	marketData  *market.Data
	evaluation  risk.Evaluation
	requested   risk.OrderRequest
	accountInfo AccountSummary
}

func buildRiskConfig(config AutoTraderConfig) risk.Config {
	maxDrawdownStop := config.MaxDrawdown
	if maxDrawdownStop > 1 {
		maxDrawdownStop = maxDrawdownStop / 100.0
	}
	return risk.Config{
		MaxPositionPct:         config.MaxPositionPct,
		MaxPortfolioExposure:   config.MaxGrossExposure,
		MaxNetExposurePct:      config.MaxNetExposurePct,
		MaxSectorExposurePct:   config.MaxSectorExposurePct,
		MaxConcurrentPositions: config.MaxConcurrentPos,
		MaxCorrelatedPositions: config.MaxCorrelatedPositions,
		MaxDailyLossPct:        config.MaxDailyLossPct,
		MaxDrawdownStopPct:     maxDrawdownStop,
		MaxPerTradeRiskPct:     config.RiskPerTradePct,
		MaxPairCorrelation:     config.MaxPairCorrelation,
		MaxGrossLeverage:       config.MaxGrossExposure,
		MinAverageDollarVolume: config.MinLiquidityUSD,
		MaxParticipationRate:   config.MaxParticipationRate,
		CashBufferPct:          0.95,
	}
}

func (at *AutoTrader) evaluatePreTradeRisk(d *decision.Decision) (*preTradeRiskContext, error) {
	if d == nil {
		return nil, fmt.Errorf("missing decision for risk evaluation")
	}
	if at.riskEngine == nil {
		at.riskEngine = risk.NewEngine(buildRiskConfig(at.config))
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("risk evaluation positions fetch failed: %w", err)
	}

	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("risk evaluation balance fetch failed: %w", err)
	}

	marketData, err := at.getValidatedMarketData(d.Symbol)
	if err != nil {
		return nil, fmt.Errorf("risk evaluation market data failed for %s: %w", d.Symbol, err)
	}

	accountSummary := at.buildAccountSummaryFromRaw(balance, positions)
	orderRequest, err := at.buildRiskOrderRequest(d, positions, marketData)
	if err != nil {
		return nil, err
	}

	accountSnapshot := risk.AccountSnapshot{
		StrategyInitialCapital: accountSummary.StrategyInitialCapital,
		StrategyEquity:         accountSummary.StrategyEquity,
		AccountEquity:          accountSummary.AccountEquity,
		AvailableBalance:       accountSummary.AvailableBalance,
		GrossMarketValue:       accountSummary.GrossMarketValue,
		DailyPnL:               at.dailyPnL,
		DailyLossLimit:         at.currentDailyLossLimit(),
		PeakStrategyEquity:     at.peakEquitySeen,
		PositionCount:          accountSummary.PositionCount,
	}

	marketSnapshot := at.buildRiskMarketSnapshot(d.Symbol, orderRequest.Side, positions, marketData)

	evaluation := at.riskEngine.Evaluate(
		accountSnapshot,
		positionSnapshotsFromRaw(positions),
		marketSnapshot,
		orderRequest,
	)
	at.observePortfolioRiskEvaluation(evaluation)

	return &preTradeRiskContext{
		positions:   positions,
		marketData:  marketData,
		evaluation:  evaluation,
		requested:   orderRequest,
		accountInfo: accountSummary,
	}, nil
}

func (at *AutoTrader) buildRiskOrderRequest(d *decision.Decision, positions []map[string]interface{}, marketData *market.Data) (risk.OrderRequest, error) {
	action := strings.ToLower(strings.TrimSpace(d.Action))
	side := "long"
	if action == "open_short" || action == "close_short" {
		side = "short"
	}

	request := risk.OrderRequest{
		Symbol:   strings.ToUpper(strings.TrimSpace(d.Symbol)),
		Action:   action,
		Side:     side,
		StopLoss: d.StopLoss,
		IsEntry:  action == "open_long" || action == "open_short",
		IsExit:   action == "close_long" || action == "close_short",
	}

	switch action {
	case "open_long", "open_short":
		request.RequestedNotional = math.Max(d.PositionSizeUSD, 0)
		if marketData != nil && marketData.CurrentPrice > 0 && request.RequestedNotional > 0 {
			request.RequestedQuantity = request.RequestedNotional / marketData.CurrentPrice
		}
	case "close_long", "close_short":
		quantity := currentPositionQuantityForSide(positions, d.Symbol, side)
		if quantity <= 0 {
			return request, fmt.Errorf("risk evaluation found no open %s position for %s", side, d.Symbol)
		}
		request.RequestedQuantity = quantity
		if marketData != nil && marketData.CurrentPrice > 0 {
			request.RequestedNotional = quantity * marketData.CurrentPrice
		}
	default:
		return request, fmt.Errorf("risk evaluation does not support action %s", d.Action)
	}

	return request, nil
}

func (at *AutoTrader) buildRiskMarketSnapshot(symbol, orderSide string, positions []map[string]interface{}, data *market.Data) risk.MarketSnapshot {
	snapshot := risk.MarketSnapshot{
		Symbol:        strings.ToUpper(strings.TrimSpace(symbol)),
		Tradable:      true,
		TradableKnown: true,
	}
	if data != nil {
		snapshot.CurrentPrice = data.CurrentPrice
		if data.LongerTermContext != nil {
			snapshot.AverageVolume = data.LongerTermContext.AverageVolume
			snapshot.CurrentVolume = data.LongerTermContext.CurrentVolume
			if snapshot.CurrentPrice > 0 && snapshot.AverageVolume > 0 {
				snapshot.AverageDollarVolume = snapshot.CurrentPrice * snapshot.AverageVolume
			}
			if data.LongerTermContext.AverageVolume > 0 {
				snapshot.HaltedKnown = true
				if data.LongerTermContext.CurrentVolume <= 0 {
					snapshot.Halted = true
					snapshot.HaltReason = "current bar volume is zero while historical average volume is positive"
				} else {
					snapshot.HaltReason = "current bar volume indicates active trading"
				}
			}
		}
	}

	tradable, reason := at.riskTradableState(symbol)
	snapshot.Tradable = tradable
	snapshot.TradableReason = reason
	if sector, ok := portfolioRiskSector(snapshot.Symbol); ok {
		snapshot.Sector = sector
		snapshot.SectorKnown = true
	}
	correlationKnown, maxCorr, maxSymbol, correlatedSymbols := at.buildRiskCorrelationSnapshot(snapshot.Symbol, orderSide, positions)
	snapshot.CorrelationKnown = correlationKnown
	snapshot.MaxObservedCorrelation = maxCorr
	snapshot.MaxObservedCorrelationSymbol = maxSymbol
	snapshot.CorrelatedPositionCount = len(correlatedSymbols)
	if len(correlatedSymbols) > 0 {
		snapshot.CorrelatedSymbols = correlatedSymbols
	}
	return snapshot
}

func (at *AutoTrader) riskTradableState(symbol string) (bool, string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return false, "missing symbol"
	}
	if at.config.InstrumentType == "equity" {
		if !isLikelyTradableEquitySymbol(symbol) {
			return false, "symbol failed local tradability filter"
		}
		if len(at.trustedSymbolSet) > 0 {
			if _, ok := at.trustedSymbolSet[symbol]; !ok {
				return false, "symbol not present in trusted symbol universe"
			}
		}
	}
	return true, "symbol passed local tradability checks"
}

func positionSnapshotsFromRaw(positions []map[string]interface{}) []risk.PositionSnapshot {
	snapshots := make([]risk.PositionSnapshot, 0, len(positions))
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		quantity, _ := parseFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"]))
		if quantity < 0 {
			quantity = -quantity
		}
		price, _ := parseFloat(firstPresent(pos["markPrice"], pos["mark_price"], pos["price"]))
		snapshots = append(snapshots, risk.PositionSnapshot{
			Symbol:      strings.ToUpper(strings.TrimSpace(symbol)),
			Side:        strings.ToLower(strings.TrimSpace(side)),
			Quantity:    quantity,
			MarketValue: quantity * price,
			Sector:      sectorName(strings.ToUpper(strings.TrimSpace(symbol))),
			SectorKnown: hasPortfolioSector(strings.ToUpper(strings.TrimSpace(symbol))),
		})
	}
	return snapshots
}

func currentPositionQuantityForSide(positions []map[string]interface{}, symbol, side string) float64 {
	for _, pos := range positions {
		posSymbol, _ := pos["symbol"].(string)
		posSide, _ := pos["side"].(string)
		if !strings.EqualFold(posSymbol, symbol) || !strings.EqualFold(posSide, side) {
			continue
		}
		qty, _ := parseFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"]))
		if qty < 0 {
			qty = -qty
		}
		if qty > 0 {
			return qty
		}
	}
	return 0
}

func (at *AutoTrader) currentDailyLossLimit() float64 {
	if at.config.MaxDailyLossPct <= 0 {
		return 0
	}
	baseline := at.dailyStartEquity
	if baseline <= 0 {
		baseline = at.initialBalance
	}
	if baseline <= 0 {
		return 0
	}
	return -baseline * at.config.MaxDailyLossPct
}

func (at *AutoTrader) applyRiskEvaluation(actionRecord *logger.DecisionAction, evaluation risk.Evaluation) {
	if actionRecord == nil {
		return
	}
	actionRecord.RiskOutcome = string(evaluation.Outcome)
	actionRecord.RiskSummary = evaluation.Summary
	actionRecord.RiskRequestedQuantity = evaluation.RequestedQuantity
	actionRecord.RiskRequestedNotional = evaluation.RequestedNotional
	actionRecord.RiskApprovedQuantity = evaluation.ApprovedQuantity
	actionRecord.RiskApprovedNotional = evaluation.ApprovedNotional
	if len(evaluation.RuleResults) == 0 {
		actionRecord.RiskChecks = nil
		return
	}

	checks := make([]logger.RiskCheckResult, 0, len(evaluation.RuleResults))
	for _, result := range evaluation.RuleResults {
		checks = append(checks, logger.RiskCheckResult{
			Name:             result.Name,
			Status:           string(result.Status),
			Message:          result.Message,
			ApprovedQuantity: result.ApprovedQuantity,
			ApprovedNotional: result.ApprovedNotional,
		})
	}
	actionRecord.RiskChecks = checks
}

func logRiskEvaluation(symbol string, evaluation risk.Evaluation) {
	if strings.TrimSpace(symbol) == "" {
		symbol = "unknown"
	}
	log.Printf("   Risk engine [%s]: %s", strings.ToUpper(symbol), evaluation.Summary)
}

type riskCorrelationPeer struct {
	symbol   string
	exposure float64
}

func (at *AutoTrader) buildRiskCorrelationSnapshot(orderSymbol, orderSide string, positions []map[string]interface{}) (bool, float64, string, []string) {
	if at.provider == nil {
		return false, 0, "", nil
	}

	peers := make([]riskCorrelationPeer, 0, len(positions))
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" || strings.EqualFold(symbol, orderSymbol) {
			continue
		}
		side, _ := pos["side"].(string)
		if !strings.EqualFold(strings.TrimSpace(side), orderSide) {
			continue
		}
		qty, _ := parseFloat(firstPresent(pos["positionAmt"], pos["position_amt"], pos["qty"], pos["quantity"]))
		if qty < 0 {
			qty = -qty
		}
		price, _ := parseFloat(firstPresent(pos["markPrice"], pos["mark_price"], pos["price"]))
		exposure := qty * price
		if exposure <= 0 {
			continue
		}
		peers = append(peers, riskCorrelationPeer{symbol: symbol, exposure: exposure})
	}

	if len(peers) == 0 {
		return true, 0, "", nil
	}

	sort.Slice(peers, func(i, j int) bool {
		return peers[i].exposure > peers[j].exposure
	})
	if len(peers) > 8 {
		peers = peers[:8]
	}

	symbols := make([]string, 0, len(peers)+1)
	symbolMap := make(map[string]string, len(peers)+1)
	orderNormalized := market.Normalize(orderSymbol, at.config.InstrumentType)
	symbols = append(symbols, orderNormalized)
	symbolMap[orderNormalized] = orderSymbol
	for _, peer := range peers {
		normalized := market.Normalize(peer.symbol, at.config.InstrumentType)
		if _, exists := symbolMap[normalized]; exists {
			continue
		}
		symbols = append(symbols, normalized)
		symbolMap[normalized] = peer.symbol
	}

	bars, err := at.provider.GetBars(symbols, "3m", 40)
	if err != nil {
		return false, 0, "", nil
	}

	targetReturns := barReturnSeries(bars[orderNormalized])
	if len(targetReturns) < 8 {
		return false, 0, "", nil
	}

	threshold := at.config.MaxPairCorrelation
	if threshold <= 0 || threshold >= 1 {
		threshold = 0.82
	}

	maxCorr := 0.0
	maxSymbol := ""
	correlated := make([]string, 0, len(peers))
	considered := 0
	for _, normalized := range symbols[1:] {
		peerReturns := barReturnSeries(bars[normalized])
		if len(peerReturns) < 8 {
			continue
		}
		considered++
		corr := absPearsonCorrelationSeries(targetReturns, peerReturns)
		peerSymbol := symbolMap[normalized]
		if corr > maxCorr {
			maxCorr = corr
			maxSymbol = peerSymbol
		}
		if corr >= threshold {
			correlated = append(correlated, peerSymbol)
		}
	}
	if considered == 0 {
		return false, 0, "", nil
	}
	sort.Strings(correlated)
	return true, maxCorr, strings.ToUpper(strings.TrimSpace(maxSymbol)), correlated
}

func barReturnSeries(bars []market.Kline) []float64 {
	if len(bars) < 3 {
		return nil
	}
	returns := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		prev := bars[i-1].Close
		curr := bars[i].Close
		if prev <= 0 || curr <= 0 {
			continue
		}
		returns = append(returns, (curr-prev)/prev)
	}
	return returns
}

func absPearsonCorrelationSeries(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < 6 {
		return 0
	}
	a = a[len(a)-n:]
	b = b[len(b)-n:]

	meanA := 0.0
	meanB := 0.0
	for i := 0; i < n; i++ {
		meanA += a[i]
		meanB += b[i]
	}
	meanA /= float64(n)
	meanB /= float64(n)

	varAB := 0.0
	varA := 0.0
	varB := 0.0
	for i := 0; i < n; i++ {
		da := a[i] - meanA
		db := b[i] - meanB
		varAB += da * db
		varA += da * da
		varB += db * db
	}
	if varA <= 0 || varB <= 0 {
		return 0
	}
	corr := varAB / math.Sqrt(varA*varB)
	if math.IsNaN(corr) || math.IsInf(corr, 0) {
		return 0
	}
	if corr < 0 {
		corr = -corr
	}
	if corr > 1 {
		return 1
	}
	return corr
}
