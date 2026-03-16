package trader

import (
	"fmt"
	"northstar/alerts"
	"northstar/broker"
	"strings"
	"time"
)

func (at *AutoTrader) currentAlertsSummary() alerts.Summary {
	if at == nil || at.alertManager == nil {
		return alerts.Summary{Recent: []alerts.Alert{}}
	}
	return at.alertManager.Summary()
}

func (at *AutoTrader) emitAlert(category alerts.Category, event, key, message string, metadata map[string]string) {
	if at == nil || at.alertManager == nil {
		return
	}
	at.alertManager.Emit(alerts.Alert{
		Key:        key,
		Category:   category,
		Event:      event,
		Service:    "northstar",
		TraderID:   at.id,
		TraderName: at.name,
		Message:    message,
		Metadata:   metadata,
	})
}

func (at *AutoTrader) alertBrokerDisconnect(stage string, err error) {
	if err == nil {
		return
	}
	if broker.ClassifyIBKRError(err) != broker.IBKRErrorTransient {
		return
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "broker"
	}
	message := fmt.Sprintf("broker disconnect detected during %s: %v", stage, err)
	at.emitAlert(alerts.CategoryCritical, "broker_disconnect", "broker_disconnect|"+stage, message, map[string]string{
		"stage": stage,
		"error": err.Error(),
	})
}

func (at *AutoTrader) alertReconciliationFailure(stage string, err error) {
	if err == nil {
		return
	}
	at.observeRiskSupervisorReconciliationFailure()
	if !strings.Contains(strings.ToLower(strings.TrimSpace(stage)), "position_reconciliation") {
		at.observeReconciliationIncident(stage, err)
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "broker_reconciliation"
	}
	message := fmt.Sprintf("broker reconciliation failed during %s: %v", stage, err)
	at.emitAlert(alerts.CategoryCritical, "reconciliation_failure", "reconciliation_failure|"+stage, message, map[string]string{
		"stage": stage,
		"error": err.Error(),
	})
}

func (at *AutoTrader) alertDailyLossLimit(dailyPnL, limit float64, until time.Time) {
	message := fmt.Sprintf("daily loss limit reached: pnl %.2f <= limit %.2f", dailyPnL, limit)
	metadata := map[string]string{
		"daily_pnl":  fmt.Sprintf("%.2f", dailyPnL),
		"limit":      fmt.Sprintf("%.2f", limit),
		"stop_until": until.Format(time.RFC3339),
	}
	at.emitAlert(alerts.CategoryCritical, "daily_loss_limit_reached", "daily_loss_limit_reached", message, metadata)
}

func (at *AutoTrader) alertTradingBlocked(reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "trading blocked"
	}
	at.emitAlert(alerts.CategoryWarning, "trading_blocked", "trading_blocked|"+reason, "trading blocked: "+reason, map[string]string{
		"reason": reason,
	})
}

func (at *AutoTrader) alertRuntimeDegraded(state BrokerRuntimeState, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "runtime degraded"
	}
	at.emitAlert(alerts.CategoryWarning, "runtime_degraded", "runtime_degraded|"+string(state)+"|"+reason, fmt.Sprintf("runtime %s: %s", state, reason), map[string]string{
		"state":  string(state),
		"reason": reason,
	})
}

func (at *AutoTrader) alertRuntimeInfo(event, message string, metadata map[string]string) {
	event = strings.TrimSpace(event)
	if event == "" {
		event = "runtime_info"
	}
	at.emitAlert(alerts.CategoryInfo, event, event+"|"+strings.TrimSpace(message), message, metadata)
}
