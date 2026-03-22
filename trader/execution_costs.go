package trader

import (
	"fmt"
	"math"
	"strings"
)

const maxExecutionImpactModelBps = 120.0

type ExecutionCostModel struct {
	CommissionBps        float64 `json:"commission_bps"`
	SpreadBps            float64 `json:"spread_bps"`
	SlippageBps          float64 `json:"slippage_bps"`
	ImpactBps            float64 `json:"impact_bps"`
	MaxParticipationRate float64 `json:"max_participation_rate"`
}

type ExecutionCostTotals struct {
	ActualFeesUSD          float64 `json:"actual_fees_usd"`
	ModeledCommissionUSD   float64 `json:"modeled_commission_usd"`
	ModeledSpreadCostUSD   float64 `json:"modeled_spread_cost_usd"`
	ModeledSlippageCostUSD float64 `json:"modeled_slippage_cost_usd"`
	ModeledImpactCostUSD   float64 `json:"modeled_impact_cost_usd"`
	ModeledTotalCostUSD    float64 `json:"modeled_total_cost_usd"`
}

type EvaluationCostSummary struct {
	Available                   bool                `json:"available"`
	ModelApplied                bool                `json:"model_applied"`
	ActualFeesObserved          bool                `json:"actual_fees_observed"`
	AppliesToSimulatedExecution bool                `json:"applies_to_simulated_execution"`
	AppliesToShadowHypothetical bool                `json:"applies_to_shadow_hypothetical"`
	AppliesToBrokerManaged      bool                `json:"applies_to_broker_managed"`
	Summary                     string              `json:"summary"`
	Model                       ExecutionCostModel  `json:"model"`
	Totals                      ExecutionCostTotals `json:"totals"`
	Warnings                    []string            `json:"warnings,omitempty"`
}

type executionCostEstimate struct {
	ReferencePrice      float64
	EffectivePrice      float64
	Quantity            float64
	Participation       float64
	CommissionBps       float64
	SpreadBps           float64
	SlippageBps         float64
	ImpactBps           float64
	AppliedFrictionBps  float64
	CommissionUSD       float64
	SpreadCostUSD       float64
	SlippageCostUSD     float64
	ImpactCostUSD       float64
	TotalModeledCostUSD float64
	ImpactApplied       bool
}

func currentExecutionCostModel(config AutoTraderConfig) ExecutionCostModel {
	model := ExecutionCostModel{
		CommissionBps:        sanitizeNonNegative(config.ExecutionCommissionBps),
		SpreadBps:            sanitizeNonNegative(config.ExecutionSpreadBps),
		SlippageBps:          sanitizeNonNegative(config.ExecutionSlippageBps),
		ImpactBps:            sanitizeNonNegative(config.ExecutionImpactBps),
		MaxParticipationRate: config.MaxParticipationRate,
	}
	if model.MaxParticipationRate <= 0 || model.MaxParticipationRate > 1.0 {
		model.MaxParticipationRate = 0.15
	}
	return model
}

func sanitizeNonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

func (m ExecutionCostModel) HasAnyCosts() bool {
	return m.CommissionBps > 0 || m.SpreadBps > 0 || m.SlippageBps > 0 || m.ImpactBps > 0
}

func (m ExecutionCostModel) Estimate(referencePrice, quantity float64, side string, isOpen bool, participation float64, allowImpact bool) executionCostEstimate {
	estimate := executionCostEstimate{
		ReferencePrice: referencePrice,
		EffectivePrice: referencePrice,
		Quantity:       quantity,
		Participation:  math.Max(participation, 0),
		CommissionBps:  sanitizeNonNegative(m.CommissionBps),
		SpreadBps:      sanitizeNonNegative(m.SpreadBps),
		SlippageBps:    sanitizeNonNegative(m.SlippageBps),
	}
	if referencePrice <= 0 || quantity <= 0 {
		return estimate
	}
	if allowImpact && m.ImpactBps > 0 && estimate.Participation > 0 {
		estimate.ImpactApplied = true
		estimate.ImpactBps = math.Min(sanitizeNonNegative(m.ImpactBps)*math.Sqrt(estimate.Participation), maxExecutionImpactModelBps)
	}
	estimate.AppliedFrictionBps = estimate.SpreadBps + estimate.SlippageBps + estimate.ImpactBps
	referenceNotional := referencePrice * quantity
	effectiveNotional := referenceNotional
	if estimate.AppliedFrictionBps > 0 {
		friction := estimate.AppliedFrictionBps / 10000.0
		if executionIsBuy(side, isOpen) {
			estimate.EffectivePrice = referencePrice * (1.0 + friction)
		} else {
			estimate.EffectivePrice = referencePrice * (1.0 - friction)
		}
		effectiveNotional = estimate.EffectivePrice * quantity
	}
	estimate.SpreadCostUSD = referenceNotional * (estimate.SpreadBps / 10000.0)
	estimate.SlippageCostUSD = referenceNotional * (estimate.SlippageBps / 10000.0)
	estimate.ImpactCostUSD = referenceNotional * (estimate.ImpactBps / 10000.0)
	estimate.CommissionUSD = effectiveNotional * (estimate.CommissionBps / 10000.0)
	estimate.TotalModeledCostUSD = estimate.CommissionUSD + estimate.SpreadCostUSD + estimate.SlippageCostUSD + estimate.ImpactCostUSD
	return estimate
}

func executionIsBuy(side string, isOpen bool) bool {
	side = strings.ToLower(strings.TrimSpace(side))
	return (isOpen && side == "long") || (!isOpen && side == "short")
}

func addExecutionCostTotals(dst *ExecutionCostTotals, estimate executionCostEstimate) {
	if dst == nil {
		return
	}
	dst.ModeledCommissionUSD += estimate.CommissionUSD
	dst.ModeledSpreadCostUSD += estimate.SpreadCostUSD
	dst.ModeledSlippageCostUSD += estimate.SlippageCostUSD
	dst.ModeledImpactCostUSD += estimate.ImpactCostUSD
	dst.ModeledTotalCostUSD += estimate.TotalModeledCostUSD
}

func (at *AutoTrader) currentEvaluationCostSummary(actualFeesUSD float64) EvaluationCostSummary {
	if at == nil {
		return EvaluationCostSummary{
			Available: false,
			Summary:   "execution-cost assumptions unavailable",
		}
	}
	model := currentExecutionCostModel(at.config)
	summary := EvaluationCostSummary{
		Available:          true,
		Model:              model,
		Totals:             ExecutionCostTotals{ActualFeesUSD: sanitizeFloat(actualFeesUSD)},
		ActualFeesObserved: actualFeesUSD > 0,
	}

	switch {
	case at.shadowModeEnabled():
		shadow := at.currentShadowSummary()
		summary.ModelApplied = true
		summary.AppliesToShadowHypothetical = true
		summary.Totals.ModeledCommissionUSD = sanitizeFloat(shadow.ModeledCommissionUSD)
		summary.Totals.ModeledSpreadCostUSD = sanitizeFloat(shadow.ModeledSpreadCostUSD)
		summary.Totals.ModeledSlippageCostUSD = sanitizeFloat(shadow.ModeledSlippageCostUSD)
		summary.Totals.ModeledImpactCostUSD = sanitizeFloat(shadow.ModeledImpactCostUSD)
		summary.Totals.ModeledTotalCostUSD = sanitizeFloat(shadow.ModeledTotalExecutionCostUSD)
		if model.ImpactBps > 0 {
			summary.Warnings = append(summary.Warnings, "shadow-mode cost modeling does not apply participation-scaled impact because broker/bar participation is unknown")
		}
		if !model.HasAnyCosts() {
			summary.Warnings = append(summary.Warnings, "shadow-mode friction model is effectively zero; hypothetical performance will remain optimistic")
		}
		summary.Summary = fmt.Sprintf("shadow evaluation applies configured modeled friction to hypothetical fills; totals are modeled and do not prove broker-managed live execution costs")
	case at.usesHypotheticalExecutionEvidence():
		if sim, ok := at.trader.(*SimTrader); ok {
			simSummary := sim.currentEvaluationCostSummary()
			simSummary.Totals.ActualFeesUSD = sanitizeFloat(actualFeesUSD)
			return simSummary
		}
		summary.ModelApplied = model.HasAnyCosts()
		summary.AppliesToSimulatedExecution = true
		if !model.HasAnyCosts() {
			summary.Warnings = append(summary.Warnings, "simulated execution currently has zero configured friction assumptions")
		}
		summary.Summary = "simulated evaluation can use the configured friction model, but no live broker fee truth is implied"
	default:
		summary.AppliesToBrokerManaged = true
		if model.HasAnyCosts() {
			summary.Warnings = append(summary.Warnings, "configured evaluation friction exists but is not applied to broker-managed execution truth")
		}
		if summary.ActualFeesObserved {
			summary.Summary = "broker-managed mode reports observed fees when available; Northstar does not apply a modeled friction layer to broker-confirmed execution"
		} else {
			summary.Summary = "broker-managed mode does not use the modeled evaluation friction layer; compare simulated/shadow results to broker-managed results with care"
		}
	}

	return summary
}

func (s *SimTrader) currentEvaluationCostSummary() EvaluationCostSummary {
	model := s.currentExecutionCostModel()
	summary := EvaluationCostSummary{
		Available:                   true,
		ModelApplied:                true,
		AppliesToSimulatedExecution: true,
		Model:                       model,
		Totals: ExecutionCostTotals{
			ModeledCommissionUSD:   sanitizeFloat(s.totalFeesUSD),
			ModeledSpreadCostUSD:   sanitizeFloat(s.totalSpreadCostUSD),
			ModeledSlippageCostUSD: sanitizeFloat(s.totalSlippageCostUSD),
			ModeledImpactCostUSD:   sanitizeFloat(s.totalImpactCostUSD),
			ModeledTotalCostUSD:    sanitizeFloat(s.totalExecutionCostUSD),
		},
	}
	if !model.HasAnyCosts() {
		summary.Warnings = append(summary.Warnings, "simulated execution friction model is effectively zero")
	}
	summary.Summary = "simulated execution results include the configured modeled friction assumptions; totals are evaluation proxies, not broker-confirmed costs"
	return summary
}

func (s *SimTrader) currentExecutionCostModel() ExecutionCostModel {
	if s == nil {
		return ExecutionCostModel{}
	}
	return ExecutionCostModel{
		CommissionBps:        sanitizeNonNegative(s.commissionBps),
		SpreadBps:            sanitizeNonNegative(s.spreadBps),
		SlippageBps:          sanitizeNonNegative(s.slippageBps),
		ImpactBps:            sanitizeNonNegative(s.impactBps),
		MaxParticipationRate: s.maxPartRate,
	}
}
