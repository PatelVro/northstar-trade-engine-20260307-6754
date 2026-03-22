package incidents

func RunbookActions(incidentType Type) []string {
	switch incidentType {
	case TypeBrokerRuntimeDegraded, TypeBrokerRuntimeReconnectFailed, TypeBrokerRuntimeReconcileFailed:
		return []string{
			"Verify the IBKR gateway or broker session is logged in and reachable.",
			"Inspect /api/status broker_runtime and incidents sections together before resuming.",
			"Review recent reconciliation failures and session reports for any blocked cycles.",
		}
	case TypeStartupReadinessFailed:
		return []string{
			"Review /api/status readiness checks to find the failing prerequisite.",
			"Fix the missing config, credential, broker bootstrap, or provider dependency.",
			"Re-run the startup gate and confirm readiness passes before trading resumes.",
		}
	case TypeLivePromotionFailed:
		return []string{
			"Confirm live promotion approval and local evidence requirements are satisfied.",
			"Inspect /api/status promotion details and validate-live output before retrying live mode.",
			"Do not override the gate until promotion evidence is intentionally reviewed.",
		}
	case TypeKillSwitchActivated:
		return []string{
			"Verify the kill switch source: config, env var, or local kill-switch file.",
			"Confirm open orders were cancelled and trading stayed paused.",
			"Clear the trigger only after confirming the reason for the stop is understood.",
		}
	case TypeDailyLossBreached, TypeDrawdownBreached, TypeRiskSupervisorHalted, TypeExcessiveOrderRejects:
		return []string{
			"Inspect /api/status risk_supervisor, portfolio_risk, and trading_gate sections.",
			"Review the latest session report and trade audit before re-enabling entries.",
			"Reduce exposure or keep the system paused until the risk cause is understood.",
		}
	case TypePositionReconciliationFailed, TypePositionMismatchDetected, TypeOrderReconciliationFailed:
		return []string{
			"Compare local state against broker truth before allowing any new entries.",
			"Inspect order/position reconciliation summaries and recent audit records.",
			"Do not resume normal trading until the broker and local state match again.",
		}
	case TypeOrderReconciliationInferredExecution:
		return []string{
			"Review the order-reconciliation summary to see which execution outcomes were inferred from position evidence.",
			"Inspect recent order audit records before treating the fill outcome as fully broker-confirmed.",
			"Keep monitoring reconciliation until broker lifecycle truth catches up or the ambiguity is otherwise explained.",
		}
	case TypeOrderReconciliationUnresolvedExecution:
		return []string{
			"Treat the missing broker-managed order as unresolved until broker and position truth are reconciled.",
			"Inspect open orders, positions, and recent order audit records before re-enabling new entries.",
			"Do not assume protective coverage or execution completion until the unresolved state is cleared.",
		}
	case TypeSymbolDataQualityBlocked, TypeMarketDataValidationFailed:
		return []string{
			"First confirm whether the market is simply closed or otherwise expected to be non-tradable before treating the condition as a live failure.",
			"Inspect the affected symbol and the data-quality summary in /api/status.",
			"Verify the market-data provider is delivering fresh, sane bars and volume.",
			"Keep the symbol blocked until validation passes again.",
		}
	default:
		return []string{
			"Inspect /api/status for the relevant subsystem and current block reason.",
			"Review the latest session report and recent alerts for surrounding context.",
			"Keep trading restricted until the incident is understood and resolved.",
		}
	}
}

func RunbookHint(incidentType Type) string {
	actions := RunbookActions(incidentType)
	if len(actions) == 0 {
		return ""
	}
	return actions[0]
}
