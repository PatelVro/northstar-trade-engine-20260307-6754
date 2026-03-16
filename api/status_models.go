package api

import (
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
