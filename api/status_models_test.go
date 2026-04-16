package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"northstar/manager"
	"northstar/trader"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestBuildHealthResponseClarifiesLivenessOnly(t *testing.T) {
	s := &Server{
		traderManager: manager.NewTraderManager(),
		startedAt:     time.Now().Add(-90 * time.Second),
	}

	resp := s.buildHealthResponse(time.Date(2026, 3, 15, 17, 0, 0, 0, time.UTC))
	if resp.Status != "ok" {
		t.Fatalf("expected ok health status, got %q", resp.Status)
	}
	if resp.Service != "northstar-api" {
		t.Fatalf("expected service name, got %q", resp.Service)
	}
	if !strings.Contains(resp.Message, "/api/status") {
		t.Fatalf("expected health message to point operators to /api/status, got %q", resp.Message)
	}
	if strings.Contains(strings.ToLower(resp.Purpose), "safe to trade") {
		t.Fatalf("health purpose should not imply trading safety, got %q", resp.Purpose)
	}
	if resp.UptimeSeconds <= 0 {
		t.Fatalf("expected positive uptime seconds, got %d", resp.UptimeSeconds)
	}
	if resp.Build["summary"] == "" {
		t.Fatalf("expected build metadata in health response")
	}
}

func TestHandleHealthReturnsLivenessPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &Server{
		router:        gin.New(),
		traderManager: manager.NewTraderManager(),
		startedAt:     time.Now().Add(-time.Minute),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	s.handleHealth(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 health response, got %d", w.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected health status ok, got %#v", payload["status"])
	}
	if payload["trading_allowed"] != nil {
		t.Fatalf("health response should not expose trading_allowed as a safety signal")
	}
	if payload["message"] == nil || !strings.Contains(payload["message"].(string), "/api/status") {
		t.Fatalf("expected liveness message to reference /api/status, got %#v", payload["message"])
	}
}

// --- GET /readiness endpoint tests ---

func TestHandleReadiness_EmptyTraderManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{
		router:        gin.New(),
		traderManager: manager.NewTraderManager(),
		startedAt:     time.Now().Add(-time.Minute),
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/readiness", nil)

	s.handleReadiness(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for empty manager, got %d", w.Code)
	}
	var resp ReadinessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status 'ok' for empty trader list, got %q", resp.Status)
	}
	if resp.CheckedAt == "" {
		t.Fatal("expected non-empty checked_at")
	}
	if resp.Traders == nil {
		t.Fatal("expected non-nil traders slice")
	}
}

func TestBuildReadinessResponse_AllOk(t *testing.T) {
	traders := map[string]*trader.AutoTrader{}
	now := time.Date(2026, 4, 16, 1, 0, 0, 0, time.UTC)
	resp := buildReadinessResponse(traders, now, 3600)

	if resp.Status != "ok" {
		t.Fatalf("expected 'ok' for empty traders map, got %q", resp.Status)
	}
	if resp.UptimeSeconds != 3600 {
		t.Fatalf("expected uptime 3600, got %d", resp.UptimeSeconds)
	}
	if resp.CheckedAt != "2026-04-16T01:00:00Z" {
		t.Fatalf("unexpected checked_at: %q", resp.CheckedAt)
	}
}

func TestBuildReadinessResponse_RollupDegraded(t *testing.T) {
	// Single trader with a degraded broker state — rollup should be "degraded"
	ops := trader.OperatorStatusSummary{
		TraderID:   "t1",
		TraderName: "Trader One",
		Mode:       "paper",
		BrokerRuntime: trader.OperatorBrokerRuntimeSummary{
			Managed: true,
			State:   trader.BrokerRuntimeDegraded,
			Reason:  "gateway unreachable",
		},
		RiskSupervisor: trader.OperatorRiskSupervisorSummary{
			TradingAllowed: true,
			Summary:        "no incidents",
		},
	}
	tr := buildTraderReadiness(nil, ops)
	if tr.Checks.Broker.Status != "degraded" {
		t.Fatalf("expected broker check 'degraded', got %q", tr.Checks.Broker.Status)
	}
	if tr.Status != "degraded" {
		t.Fatalf("expected overall trader status 'degraded', got %q", tr.Status)
	}
}

func TestBuildReadinessResponse_RollupDown(t *testing.T) {
	// Broker state other than healthy/degraded should map to "down"
	ops := trader.OperatorStatusSummary{
		TraderID:   "t1",
		TraderName: "Trader One",
		Mode:       "paper",
		BrokerRuntime: trader.OperatorBrokerRuntimeSummary{
			Managed: true,
			State:   trader.BrokerRuntimeState("paused"),
			Reason:  "auth failed",
		},
		RiskSupervisor: trader.OperatorRiskSupervisorSummary{
			TradingAllowed: true,
		},
	}
	tr := buildTraderReadiness(nil, ops)
	if tr.Checks.Broker.Status != "down" {
		t.Fatalf("expected broker check 'down' for unknown state, got %q", tr.Checks.Broker.Status)
	}
	if tr.Status != "down" {
		t.Fatalf("expected overall trader status 'down', got %q", tr.Status)
	}
}

func TestBuildReadinessResponse_AICheckFromReadinessGate(t *testing.T) {
	ops := trader.OperatorStatusSummary{
		TraderID: "t1",
		AIProvider: "DeepSeek",
		Readiness: trader.OperatorReadinessSummary{
			Checks: []trader.ReadinessCheck{
				{Name: "ai_readiness", Status: trader.ReadinessFail, Message: "DeepSeek key missing"},
			},
		},
		BrokerRuntime: trader.OperatorBrokerRuntimeSummary{Managed: false},
		RiskSupervisor: trader.OperatorRiskSupervisorSummary{TradingAllowed: true},
	}
	tr := buildTraderReadiness(nil, ops)
	if tr.Checks.AI.Status != "down" {
		t.Fatalf("expected AI check 'down' when readiness gate fails, got %q", tr.Checks.AI.Status)
	}
}

func TestBuildReadinessResponse_RiskSupervisorCriticalIncident(t *testing.T) {
	ops := trader.OperatorStatusSummary{
		TraderID:      "t1",
		BrokerRuntime: trader.OperatorBrokerRuntimeSummary{Managed: false},
		RiskSupervisor: trader.OperatorRiskSupervisorSummary{
			TradingAllowed:        false,
			CriticalIncidentCount: 1,
			Summary:               "daily loss limit exceeded",
		},
	}
	tr := buildTraderReadiness(nil, ops)
	if tr.Checks.RiskSupervisor.Status != "degraded" {
		t.Fatalf("expected risk_supervisor 'degraded' with critical incident, got %q", tr.Checks.RiskSupervisor.Status)
	}
}

func TestRollupStatus(t *testing.T) {
	cases := []struct {
		statuses []string
		want     string
	}{
		{[]string{"ok", "ok"}, "ok"},
		{[]string{"ok", "degraded"}, "degraded"},
		{[]string{"degraded", "down"}, "down"},
		{[]string{"ok", "ok", "down", "degraded"}, "down"},
		{[]string{}, "ok"},
	}
	for _, tc := range cases {
		got := rollupStatus(tc.statuses...)
		if got != tc.want {
			t.Errorf("rollupStatus(%v) = %q, want %q", tc.statuses, got, tc.want)
		}
	}
}

func TestBuildOperatorStatusResponseIncludesClearOperatorSummary(t *testing.T) {
	now := time.Date(2026, 3, 15, 17, 30, 0, 0, time.UTC)
	resp := buildOperatorStatusResponse(trader.OperatorStatusSummary{
		TraderID:           "paper_1",
		TraderName:         "Paper One",
		Mode:               "paper",
		Broker:             "ibkr",
		StrategyMode:       "multi_factor",
		TradingAllowed:     false,
		TradingBlockReason: "broker runtime degraded",
		OperatorMessage:    "trading blocked: broker runtime degraded",
		Readiness: trader.OperatorReadinessSummary{
			Status:         trader.ReadinessPass,
			Message:        "startup readiness passed",
			TradingAllowed: true,
		},
		BrokerRuntime: trader.OperatorBrokerRuntimeSummary{
			State:          trader.BrokerRuntimeDegraded,
			Reason:         "gateway connection refused",
			TradingAllowed: false,
		},
		Promotion: trader.OperatorPromotionSummary{
			Status:             trader.PromotionFail,
			Message:            "1 promotion check(s) failed",
			Required:           true,
			LiveTradingAllowed: false,
		},
		Session: trader.OperatorSessionSummary{
			LastSessionReportStatus: "degraded",
		},
	}, now)

	if resp.Service.Name != "northstar-api" {
		t.Fatalf("expected service name in operator status, got %q", resp.Service.Name)
	}
	if resp.Now != now.Format(time.RFC3339) {
		t.Fatalf("expected operator status timestamp %q, got %q", now.Format(time.RFC3339), resp.Now)
	}
	if resp.TradingBlockReason != "broker runtime degraded" {
		t.Fatalf("expected clear trading block reason, got %q", resp.TradingBlockReason)
	}
	if resp.BrokerRuntime.State != trader.BrokerRuntimeDegraded {
		t.Fatalf("expected nested broker runtime state, got %s", resp.BrokerRuntime.State)
	}
	if resp.Promotion.Status != trader.PromotionFail {
		t.Fatalf("expected promotion status in operator summary, got %s", resp.Promotion.Status)
	}
	if resp.Build["summary"] == "" {
		t.Fatalf("expected build metadata in operator status response")
	}
}
