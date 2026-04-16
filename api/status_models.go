package api

import (
	"fmt"
	"northstar/buildinfo"
	"northstar/trader"
	"time"
)

type ServiceSummary struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Purpose string `json:"purpose"`
	Message string `json:"message"`
}

type HealthResponse struct {
	Status        string            `json:"status"`
	Service       string            `json:"service"`
	Purpose       string            `json:"purpose"`
	Message       string            `json:"message"`
	Now           string            `json:"now"`
	Time          string            `json:"time"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	TraderCount   int               `json:"trader_count"`
	Build         map[string]string `json:"build"`
}

type OperatorStatusResponse struct {
	Service ServiceSummary    `json:"service"`
	Build   map[string]string `json:"build"`
	Now     string            `json:"now"`
	trader.OperatorStatusSummary
}

func (s *Server) buildHealthResponse(now time.Time) HealthResponse {
	now = now.UTC()
	return HealthResponse{
		Status:        "ok",
		Service:       "northstar-api",
		Purpose:       "service liveness and basic diagnostics only",
		Message:       "service alive; trading readiness must be checked via /api/status",
		Now:           now.Format(time.RFC3339),
		Time:          now.Format(time.RFC3339),
		UptimeSeconds: s.uptimeSeconds(now),
		TraderCount:   len(s.traderManager.GetTraderIDs()),
		Build:         buildinfo.Current().Map(),
	}
}

// ReadinessCheckResult is a single named check within the readiness matrix.
type ReadinessCheckResult struct {
	Status string `json:"status"` // "ok" | "degraded" | "down"
	Detail string `json:"detail"`
}

// TraderReadiness holds the per-trader health matrix.
type TraderReadiness struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Status string `json:"status"` // "ok" | "degraded" | "down"
	Checks struct {
		Broker         ReadinessCheckResult `json:"broker"`
		Data           ReadinessCheckResult `json:"data"`
		AI             ReadinessCheckResult `json:"ai"`
		RiskSupervisor ReadinessCheckResult `json:"risk_supervisor"`
	} `json:"checks"`
}

// ReadinessResponse is the response body for GET /readiness.
type ReadinessResponse struct {
	Status        string            `json:"status"` // "ok" | "degraded" | "down"
	CheckedAt     string            `json:"checked_at"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Traders       []TraderReadiness `json:"traders"`
}

// buildReadinessResponse constructs the readiness matrix from live operator status.
func buildReadinessResponse(traders map[string]*trader.AutoTrader, now time.Time, uptimeSeconds int64) ReadinessResponse {
	traderList := make([]TraderReadiness, 0, len(traders))
	overallStatus := "ok"

	for _, t := range traders {
		ops := t.GetOperatorStatus()
		tr := buildTraderReadiness(t, ops)
		traderList = append(traderList, tr)

		// Roll up: "down" beats "degraded" beats "ok"
		switch tr.Status {
		case "down":
			overallStatus = "down"
		case "degraded":
			if overallStatus != "down" {
				overallStatus = "degraded"
			}
		}
	}

	return ReadinessResponse{
		Status:        overallStatus,
		CheckedAt:     now.Format(time.RFC3339),
		UptimeSeconds: uptimeSeconds,
		Traders:       traderList,
	}
}

// buildTraderReadiness maps OperatorStatusSummary to a compact health matrix.
func buildTraderReadiness(t *trader.AutoTrader, ops trader.OperatorStatusSummary) TraderReadiness {
	tr := TraderReadiness{
		ID:   ops.TraderID,
		Name: ops.TraderName,
		Mode: ops.Mode,
	}

	// --- Broker check ---
	tr.Checks.Broker = brokerCheck(ops)

	// --- Data check: use readiness checks for data_readiness gate ---
	tr.Checks.Data = dataCheck(ops)

	// --- AI check: use readiness checks for ai_readiness gate ---
	tr.Checks.AI = aiCheck(ops)

	// --- Risk supervisor check ---
	tr.Checks.RiskSupervisor = riskSupervisorCheck(ops)

	// Overall trader status: "down" if any check is "down", "degraded" if any "degraded"
	tr.Status = rollupStatus(
		tr.Checks.Broker.Status,
		tr.Checks.Data.Status,
		tr.Checks.AI.Status,
		tr.Checks.RiskSupervisor.Status,
	)

	return tr
}

func brokerCheck(ops trader.OperatorStatusSummary) ReadinessCheckResult {
	if !ops.BrokerRuntime.Managed {
		return ReadinessCheckResult{Status: "ok", Detail: "broker runtime not managed (sim/replay/demo)"}
	}
	switch ops.BrokerRuntime.State {
	case "healthy":
		detail := "broker gateway reachable"
		if ops.BrokerRuntime.LastReconciledAt != "" {
			detail = fmt.Sprintf("broker gateway reachable; last reconciled %s", ops.BrokerRuntime.LastReconciledAt)
		}
		return ReadinessCheckResult{Status: "ok", Detail: detail}
	case "degraded":
		return ReadinessCheckResult{Status: "degraded", Detail: firstNonEmptyStr(ops.BrokerRuntime.Reason, ops.BrokerRuntime.LastError, "broker degraded")}
	default:
		return ReadinessCheckResult{Status: "down", Detail: firstNonEmptyStr(ops.BrokerRuntime.Reason, ops.BrokerRuntime.LastError, "broker unavailable")}
	}
}

func dataCheck(ops trader.OperatorStatusSummary) ReadinessCheckResult {
	check := findReadinessCheck(ops.Readiness.Checks, "data_readiness")
	if check == nil {
		// No explicit gate — check data quality state as fallback
		if ops.DataQuality.FeedDelayed {
			return ReadinessCheckResult{Status: "degraded", Detail: firstNonEmptyStr(ops.DataQuality.FeedSummary, "market data feed delayed")}
		}
		return ReadinessCheckResult{Status: "ok", Detail: "no data readiness gate present"}
	}
	switch check.Status {
	case "pass":
		return ReadinessCheckResult{Status: "ok", Detail: check.Message}
	case "warn":
		return ReadinessCheckResult{Status: "degraded", Detail: check.Message}
	default:
		return ReadinessCheckResult{Status: "down", Detail: check.Message}
	}
}

func aiCheck(ops trader.OperatorStatusSummary) ReadinessCheckResult {
	check := findReadinessCheck(ops.Readiness.Checks, "ai_readiness")
	if check == nil {
		return ReadinessCheckResult{Status: "ok", Detail: fmt.Sprintf("AI provider: %s", ops.AIProvider)}
	}
	switch check.Status {
	case "pass":
		return ReadinessCheckResult{Status: "ok", Detail: check.Message}
	case "warn":
		return ReadinessCheckResult{Status: "degraded", Detail: check.Message}
	default:
		return ReadinessCheckResult{Status: "down", Detail: check.Message}
	}
}

func riskSupervisorCheck(ops trader.OperatorStatusSummary) ReadinessCheckResult {
	rs := ops.RiskSupervisor
	if rs.CriticalIncidentCount > 0 {
		return ReadinessCheckResult{
			Status: "degraded",
			Detail: fmt.Sprintf("%d critical incident(s) active: %s", rs.CriticalIncidentCount, rs.Summary),
		}
	}
	if !rs.TradingAllowed {
		return ReadinessCheckResult{Status: "degraded", Detail: firstNonEmptyStr(rs.Summary, "risk supervisor blocking trading")}
	}
	if rs.ActiveIncidentCount > 0 {
		return ReadinessCheckResult{
			Status: "degraded",
			Detail: fmt.Sprintf("%d active incident(s): %s", rs.ActiveIncidentCount, rs.Summary),
		}
	}
	return ReadinessCheckResult{Status: "ok", Detail: firstNonEmptyStr(rs.Summary, "no active incidents")}
}

func rollupStatus(statuses ...string) string {
	result := "ok"
	for _, s := range statuses {
		switch s {
		case "down":
			return "down"
		case "degraded":
			result = "degraded"
		}
	}
	return result
}

// findReadinessCheck finds the first readiness check with the given name.
func findReadinessCheck(checks []trader.ReadinessCheck, name string) *trader.ReadinessCheck {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func buildOperatorStatusResponse(summary trader.OperatorStatusSummary, now time.Time) OperatorStatusResponse {
	return OperatorStatusResponse{
		Service: ServiceSummary{
			Name:    "northstar-api",
			Status:  "alive",
			Purpose: "operator trading status summary",
			Message: "service alive; use trading_allowed, readiness, promotion, and broker_runtime to assess live trading safety",
		},
		Build:                 buildinfo.Current().Map(),
		Now:                   now.UTC().Format(time.RFC3339),
		OperatorStatusSummary: summary,
	}
}
