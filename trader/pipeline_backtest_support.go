package trader

import (
	"fmt"
	"northstar/decision"
	"northstar/features"
	"northstar/logger"
	"northstar/market"
	"northstar/regime"
	"northstar/selector"
	"sort"
	"strings"
	"time"
)

const (
	canonicalPipelineBlockedPrefix = "Canonical pipeline blocked "
	legacyPipelineBlockedPrefix    = "Research pipeline blocked "
)

func blockedPipelineOriginalAction(d decision.Decision) string {
	action := strings.ToLower(strings.TrimSpace(d.Action))
	if action != "wait" && action != "hold" {
		return action
	}

	reasoning := strings.TrimSpace(d.Reasoning)
	prefix := ""
	switch {
	case strings.HasPrefix(reasoning, canonicalPipelineBlockedPrefix):
		prefix = canonicalPipelineBlockedPrefix
	case strings.HasPrefix(reasoning, legacyPipelineBlockedPrefix):
		prefix = legacyPipelineBlockedPrefix
	default:
		return action
	}
	remainder := strings.TrimPrefix(reasoning, prefix)
	parts := strings.SplitN(remainder, ":", 2)
	if len(parts) == 0 {
		return action
	}
	tokens := strings.Fields(parts[0])
	if len(tokens) < 2 {
		return action
	}
	candidate := strings.ToLower(strings.TrimSpace(tokens[len(tokens)-1]))
	switch candidate {
	case "open_long", "open_short", "close_long", "close_short":
		return candidate
	default:
		return action
	}
}

func preferredFeatureVector(data *market.Data) (string, *features.FeatureVector) {
	if data == nil || data.Features == nil {
		return "", nil
	}
	if vector := data.Features.Vector("4h"); vector != nil {
		return "4h", vector
	}
	if vector := data.Features.Vector("3m"); vector != nil {
		return "3m", vector
	}
	for timeframe, vector := range data.Features.Vectors {
		if vector != nil {
			return timeframe, vector
		}
	}
	return "", nil
}

func preferredRegimeResult(data *market.Data) *regime.Result {
	if data == nil || data.Regimes == nil {
		return nil
	}
	if result := data.Regimes.Result("4h"); result != nil {
		return result
	}
	if result := data.Regimes.Result("3m"); result != nil {
		return result
	}
	for _, result := range data.Regimes.Results {
		if result != nil {
			return result
		}
	}
	return nil
}

func preferredSelection(data *market.Data) *selector.Selection {
	if data == nil {
		return nil
	}
	return allocationSelectionForMarketDataStatic(data, "")
}

func allocationSelectionForMarketDataStatic(data *market.Data, strategyMode string) *selector.Selection {
	if data != nil && data.Selections != nil {
		if selected := data.Selections.Selection("4h"); selected != nil {
			cloned := *selected
			return &cloned
		}
		if selected := data.Selections.Selection("3m"); selected != nil {
			cloned := *selected
			return &cloned
		}
	}

	fallback := &selector.Selection{
		SchemaVersion:       selector.SchemaVersion,
		SelectorVersion:     selector.SelectorVersion,
		SelectedStrategy:    strategyMode,
		Confidence:          0.55,
		AllowTrading:        true,
		RecommendedRiskMode: selector.RiskModeNormal,
		FallbackStrategy:    "momentum_fallback",
		Valid:               true,
		Warnings:            []string{"missing_selector_output_using_strategy_mode_fallback"},
	}

	switch strings.ToLower(strings.TrimSpace(strategyMode)) {
	case "momentum_only":
		fallback.SelectedFamily = selector.StrategyFamilyMomentum
	case "momentum_fallback":
		fallback.SelectedFamily = selector.StrategyFamilyDefensive
		fallback.RecommendedRiskMode = selector.RiskModeReducedRisk
	case "multi_factor":
		fallback.SelectedFamily = selector.StrategyFamilyHybrid
	case "hybrid_ai", "ai_only":
		fallback.SelectedFamily = selector.StrategyFamilyHybrid
	default:
		fallback.SelectedFamily = selector.StrategyFamilyHybrid
	}
	fallback.SelectionReason = "strategy-mode fallback used because canonical selector output was unavailable"
	return fallback
}

func (at *AutoTrader) canonicalDecisionArchitecture() string {
	if at.usesCanonicalEquityPipeline() {
		return "equity_generator_plus_canonical_pipeline"
	}
	return "strategy_mode_only"
}

func (at *AutoTrader) usesCanonicalEquityPipeline() bool {
	switch {
	case strings.EqualFold(at.config.InstrumentType, "equity"):
		return true
	case strings.EqualFold(at.exchange, "ibkr"), strings.EqualFold(at.exchange, "alpaca"):
		return true
	case strings.EqualFold(at.config.Broker, "ibkr"), strings.EqualFold(at.config.Broker, "alpaca"):
		return true
	default:
		return false
	}
}

func (at *AutoTrader) prepareCanonicalEquityContext(ctx *decision.Context) error {
	if ctx == nil || !at.usesCanonicalEquityPipeline() {
		return nil
	}
	if len(ctx.MarketDataMap) > 0 {
		return nil
	}
	return at.loadMomentumMarketData(ctx)
}

func (at *AutoTrader) pipelineObservationForSymbol(symbol string, data *market.Data) logger.PipelineObservation {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	observation := logger.PipelineObservation{
		Symbol: symbol,
	}
	timeframe, vector := preferredFeatureVector(data)
	if vector != nil {
		observation.Timestamp = vector.Timestamp
		observation.Timeframe = timeframe
		observation.FeatureValid = vector.Valid
		observation.FeatureInsufficientHistory = vector.InsufficientHistory
		observation.FeatureWarnings = append([]string(nil), vector.ValidationWarnings...)
	}

	if result := preferredRegimeResult(data); result != nil {
		if observation.Timestamp.IsZero() {
			observation.Timestamp = result.Timestamp
		}
		if observation.Timeframe == "" {
			observation.Timeframe = result.Timeframe
		}
		observation.Regime = string(result.Regime)
		observation.RegimeScore = result.RegimeScore
		observation.RegimeConfidence = result.Confidence
		observation.RegimeExplanation = strings.TrimSpace(result.Explanation)
	}

	selected := preferredSelection(data)
	if selected == nil {
		selected = allocationSelectionForMarketDataStatic(data, at.config.StrategyMode)
	}
	if selected != nil {
		if observation.Timestamp.IsZero() {
			observation.Timestamp = selected.Timestamp
		}
		if observation.Timeframe == "" {
			observation.Timeframe = selected.Timeframe
		}
		observation.SelectedFamily = string(selected.SelectedFamily)
		observation.SelectedStrategy = selected.SelectedStrategy
		observation.SelectionConfidence = selected.Confidence
		observation.SelectionAllowTrade = selected.AllowTrading
		observation.SelectionRiskMode = string(selected.RecommendedRiskMode)
		observation.SelectionReason = strings.TrimSpace(selected.SelectionReason)
		observation.SelectionWarnings = append([]string(nil), selected.Warnings...)
	}

	if observation.Timestamp.IsZero() {
		observation.Timestamp = time.Now().UTC()
	}

	return observation
}

func (at *AutoTrader) buildPipelineObservations(ctx *decision.Context) []logger.PipelineObservation {
	if ctx == nil || len(ctx.MarketDataMap) == 0 {
		return nil
	}

	symbols := make([]string, 0, len(ctx.MarketDataMap))
	for symbol := range ctx.MarketDataMap {
		upper := strings.ToUpper(strings.TrimSpace(symbol))
		if upper != "" {
			symbols = append(symbols, upper)
		}
	}
	sort.Strings(symbols)

	observations := make([]logger.PipelineObservation, 0, len(symbols))
	for _, symbol := range symbols {
		observations = append(observations, at.pipelineObservationForSymbol(symbol, lookupMarketData(ctx, symbol)))
	}
	return observations
}

func (at *AutoTrader) buildPipelineDecision(ctx *decision.Context, d decision.Decision) *logger.PipelineDecision {
	symbol := strings.ToUpper(strings.TrimSpace(d.Symbol))
	if symbol == "" {
		return nil
	}

	data := lookupMarketData(ctx, symbol)
	observation := at.pipelineObservationForSymbol(symbol, data)
	pipeline := &logger.PipelineDecision{
		PipelineObservation:  observation,
		DecisionAction:       blockedPipelineOriginalAction(d),
		DecisionAllowed:      true,
		AllocationAllowTrade: true,
	}

	if !executionActionIncreasesExposure(pipeline.DecisionAction) {
		return pipeline
	}

	if !pipeline.SelectionAllowTrade || strings.EqualFold(pipeline.SelectedFamily, string(selector.StrategyFamilyNoTrade)) || strings.EqualFold(pipeline.SelectionRiskMode, string(selector.RiskModeNoTrade)) {
		pipeline.DecisionAllowed = false
		pipeline.AllocationAllowTrade = false
		pipeline.BlockingReason = strings.TrimSpace(pipeline.SelectionReason)
		if pipeline.BlockingReason == "" {
			pipeline.BlockingReason = fmt.Sprintf("selector blocked entry for %s in %s regime", symbol, pipeline.Regime)
		}
		return pipeline
	}

	entryPrice := 0.0
	if data != nil && data.CurrentPrice > 0 {
		entryPrice = data.CurrentPrice
	}

	allocation := at.suggestAllocation(ctx, symbol, pipeline.DecisionAction, entryPrice, d.StopLoss)
	pipeline.AllocationAllowTrade = allocation.AllowTrade
	pipeline.AllocationReducedSize = allocation.ReducedSize
	pipeline.RecommendedQuantity = allocation.RecommendedQuantity
	pipeline.RecommendedNotional = allocation.RecommendedNotional
	pipeline.TargetPositionPct = allocation.TargetPositionPct
	pipeline.RiskBudgetUsed = allocation.RiskBudgetUsed
	pipeline.SizingReason = strings.TrimSpace(allocation.SizingReason)
	pipeline.AllocationWarnings = append([]string(nil), allocation.Warnings...)
	if !allocation.AllowTrade {
		pipeline.DecisionAllowed = false
		pipeline.BlockingReason = strings.TrimSpace(allocation.SizingReason)
		if pipeline.BlockingReason == "" && len(allocation.Warnings) > 0 {
			pipeline.BlockingReason = strings.Join(allocation.Warnings, "; ")
		}
		if pipeline.BlockingReason == "" {
			pipeline.BlockingReason = fmt.Sprintf("allocator blocked entry for %s", symbol)
		}
	}

	return pipeline
}

func (at *AutoTrader) applyCanonicalRuntimeStrategyDispatch(ctx *decision.Context, fullDecision *decision.FullDecision) {
	if !at.usesCanonicalEquityPipeline() || fullDecision == nil || len(fullDecision.Decisions) == 0 {
		return
	}

	filtered := make([]decision.Decision, 0, len(fullDecision.Decisions))
	for _, d := range fullDecision.Decisions {
		action := strings.ToLower(strings.TrimSpace(d.Action))
		if !executionActionIncreasesExposure(action) {
			filtered = append(filtered, d)
			continue
		}

		pipeline := at.buildPipelineDecision(ctx, d)
		if pipeline == nil {
			filtered = append(filtered, d)
			continue
		}

		if !pipeline.DecisionAllowed {
			filtered = append(filtered, decision.Decision{
				Symbol:    d.Symbol,
				Action:    "wait",
				Reasoning: fmt.Sprintf("%s%s %s: %s", canonicalPipelineBlockedPrefix, d.Symbol, d.Action, pipeline.BlockingReason),
			})
			continue
		}

		if pipeline.RecommendedNotional > 0 {
			if d.PositionSizeUSD <= 0 || pipeline.RecommendedNotional < d.PositionSizeUSD {
				d.PositionSizeUSD = pipeline.RecommendedNotional
			}
		}
		filtered = append(filtered, d)
	}

	fullDecision.Decisions = filtered
}

func (at *AutoTrader) applyBacktestResearchPipeline(ctx *decision.Context, fullDecision *decision.FullDecision) {
	at.applyCanonicalRuntimeStrategyDispatch(ctx, fullDecision)
}
