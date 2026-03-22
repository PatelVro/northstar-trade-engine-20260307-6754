package trader

import (
	"fmt"
	"log"
	"northstar/audit"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	killSwitchEnvVarName     = "NORTHSTAR_KILL_SWITCH"
	killSwitchFileEnvVarName = "NORTHSTAR_KILL_SWITCH_FILE"
)

type killSwitchSignal struct {
	Active   bool
	Source   string
	Message  string
	FilePath string
}

type killSwitchSummary struct {
	Available           bool
	Active              bool
	Source              string
	Message             string
	FilePath            string
	TriggeredAt         time.Time
	LastCheckedAt       time.Time
	LastClearedAt       time.Time
	LastCancelAttemptAt time.Time
	LastCancelError     string
	OrdersCancelled     bool
	ActivationCount     int
	loopActive          bool
}

type killSwitchLiveOrdersFetcher interface {
	GetLiveOrders() ([]map[string]interface{}, error)
}

func (at *AutoTrader) initializeKillSwitchState() {
	at.killSwitchMu.Lock()
	defer at.killSwitchMu.Unlock()
	at.killSwitchState = killSwitchSummary{
		Available: true,
		Message:   "kill switch clear",
	}
}

func (at *AutoTrader) currentKillSwitchSummary() killSwitchSummary {
	at.killSwitchMu.RLock()
	defer at.killSwitchMu.RUnlock()
	return at.killSwitchState
}

func (at *AutoTrader) killSwitchPollInterval() time.Duration {
	delay := at.config.ScanInterval / 2
	if delay <= 0 {
		delay = 5 * time.Second
	}
	if delay < 2*time.Second {
		delay = 2 * time.Second
	}
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	return delay
}

func (at *AutoTrader) waitForKillSwitchClear() error {
	for at.isRunning {
		summary := at.runKillSwitchCheck("startup")
		if !summary.Active {
			return nil
		}
		at.alertTradingBlocked("emergency kill switch active")
		delay := at.killSwitchPollInterval()
		log.Printf(" [%s] Emergency kill switch remains active; retrying in %s", at.name, delay)
		if !at.sleepWhileRunning(delay) {
			return nil
		}
	}
	return nil
}

func (at *AutoTrader) ensureKillSwitchClear() error {
	summary := at.runKillSwitchCheck("gate")
	if !summary.Active {
		return nil
	}
	reason := strings.TrimSpace(summary.Message)
	if reason == "" {
		reason = "kill switch active"
	}
	return fmt.Errorf("emergency kill switch active: %s", reason)
}

func (at *AutoTrader) startKillSwitchMonitor() {
	if !at.isRunning {
		return
	}
	at.killSwitchMu.Lock()
	if at.killSwitchState.loopActive {
		at.killSwitchMu.Unlock()
		return
	}
	at.killSwitchState.loopActive = true
	at.killSwitchMu.Unlock()

	go at.runKillSwitchMonitorLoop()
}

func (at *AutoTrader) runKillSwitchMonitorLoop() {
	defer func() {
		at.killSwitchMu.Lock()
		at.killSwitchState.loopActive = false
		at.killSwitchMu.Unlock()
	}()

	for at.isRunning {
		at.runKillSwitchCheck("monitor")
		if !at.sleepWhileRunning(at.killSwitchPollInterval()) {
			return
		}
	}
}

func (at *AutoTrader) runKillSwitchCheck(stage string) killSwitchSummary {
	now := time.Now().UTC()
	signal := at.evaluateKillSwitchSignal()
	if signal.Active {
		changed := at.activateKillSwitch(signal, now)
		at.ensureKillSwitchOrdersCancelled(stage, now)
		if changed {
			at.alertTradingBlocked("emergency kill switch active")
			at.emitAlert("critical", "emergency_kill_switch", "emergency_kill_switch|"+signal.Source, "emergency kill switch activated: "+signal.Message, map[string]string{
				"source":    signal.Source,
				"file_path": signal.FilePath,
			})
		}
	} else {
		at.clearKillSwitch(now)
	}
	return at.currentKillSwitchSummary()
}

func (at *AutoTrader) activateKillSwitch(signal killSwitchSignal, now time.Time) bool {
	at.killSwitchMu.Lock()
	changed := !at.killSwitchState.Active ||
		at.killSwitchState.Source != signal.Source ||
		at.killSwitchState.Message != signal.Message ||
		at.killSwitchState.FilePath != signal.FilePath

	at.killSwitchState.Available = true
	at.killSwitchState.Active = true
	at.killSwitchState.Source = signal.Source
	at.killSwitchState.Message = signal.Message
	at.killSwitchState.FilePath = signal.FilePath
	at.killSwitchState.LastCheckedAt = now
	if at.killSwitchState.TriggeredAt.IsZero() || changed {
		at.killSwitchState.TriggeredAt = now
	}
	if changed {
		at.killSwitchState.ActivationCount++
		at.killSwitchState.OrdersCancelled = false
		at.killSwitchState.LastCancelAttemptAt = time.Time{}
		at.killSwitchState.LastCancelError = ""
		log.Printf(" [%s] Emergency kill switch activated via %s: %s", at.name, signal.Source, signal.Message)
		at.recordPaperSessionWarning(fmt.Sprintf("emergency kill switch activated via %s: %s", signal.Source, signal.Message))
	}
	current := at.killSwitchState
	at.killSwitchMu.Unlock()
	at.syncKillSwitchIncident(current)
	if changed {
		at.journalKillSwitchEvent("kill_switch_activated", audit.JournalSeverityCritical, current, nil)
	}

	return changed
}

func (at *AutoTrader) clearKillSwitch(now time.Time) {
	at.killSwitchMu.Lock()
	wasActive := at.killSwitchState.Active
	prevSource := at.killSwitchState.Source
	prevMessage := at.killSwitchState.Message
	at.killSwitchState.Available = true
	at.killSwitchState.Active = false
	at.killSwitchState.Source = ""
	at.killSwitchState.Message = "kill switch clear"
	at.killSwitchState.FilePath = ""
	at.killSwitchState.LastCheckedAt = now
	at.killSwitchState.LastClearedAt = now
	at.killSwitchState.TriggeredAt = time.Time{}
	at.killSwitchState.LastCancelAttemptAt = time.Time{}
	at.killSwitchState.LastCancelError = ""
	at.killSwitchState.OrdersCancelled = false
	current := at.killSwitchState
	at.killSwitchMu.Unlock()
	at.syncKillSwitchIncident(current)

	if wasActive {
		log.Printf(" [%s] Emergency kill switch cleared (previous source=%s): %s", at.name, prevSource, prevMessage)
		at.journalKillSwitchEvent("kill_switch_cleared", audit.JournalSeverityInfo, current, map[string]interface{}{
			"previous_source":  prevSource,
			"previous_message": prevMessage,
		})
		at.emitAlert("info", "emergency_kill_switch_cleared", "emergency_kill_switch_cleared|"+prevSource, "emergency kill switch cleared", map[string]string{
			"previous_source":  prevSource,
			"previous_message": prevMessage,
		})
	}
}

func (at *AutoTrader) ensureKillSwitchOrdersCancelled(stage string, now time.Time) {
	at.killSwitchMu.RLock()
	active := at.killSwitchState.Active
	lastAttempt := at.killSwitchState.LastCancelAttemptAt
	alreadyCancelled := at.killSwitchState.OrdersCancelled && strings.TrimSpace(at.killSwitchState.LastCancelError) == ""
	at.killSwitchMu.RUnlock()

	if !active || alreadyCancelled {
		return
	}
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < 15*time.Second {
		return
	}

	at.killSwitchMu.Lock()
	at.killSwitchState.LastCancelAttemptAt = now
	at.killSwitchMu.Unlock()

	err := at.cancelOpenOrdersForKillSwitch()

	at.killSwitchMu.Lock()
	if err != nil {
		at.killSwitchState.OrdersCancelled = false
		at.killSwitchState.LastCancelError = err.Error()
		current := at.killSwitchState
		log.Printf(" [%s] Emergency kill switch order cancel failed during %s: %v", at.name, stage, err)
		at.recordPaperSessionWarning(fmt.Sprintf("emergency kill switch cancel-open-orders failed: %v", err))
		at.killSwitchMu.Unlock()
		at.journalKillSwitchEvent("kill_switch_cancel_failed", audit.JournalSeverityCritical, current, map[string]interface{}{
			"stage": stage,
			"error": err.Error(),
		})
		at.emitAlert("critical", "emergency_kill_switch_cancel_failed", "emergency_kill_switch_cancel_failed|"+stage, "emergency kill switch failed to cancel open orders: "+err.Error(), map[string]string{
			"stage": stage,
			"error": err.Error(),
		})
		return
	}
	at.killSwitchState.OrdersCancelled = true
	at.killSwitchState.LastCancelError = ""
	current := at.killSwitchState
	log.Printf(" [%s] Emergency kill switch cancelled open orders", at.name)
	at.killSwitchMu.Unlock()
	at.journalKillSwitchEvent("kill_switch_orders_cancelled", audit.JournalSeverityWarning, current, map[string]interface{}{
		"stage": stage,
	})
}

func (at *AutoTrader) cancelOpenOrdersForKillSwitch() error {
	if at == nil || at.trader == nil {
		return fmt.Errorf("trader execution engine is not initialized")
	}

	symbolSet := make(map[string]struct{})
	errs := make([]string, 0, 2)

	if fetcher, ok := at.trader.(killSwitchLiveOrdersFetcher); ok {
		liveOrders, err := fetcher.GetLiveOrders()
		if err != nil {
			errs = append(errs, fmt.Sprintf("fetch open orders: %v", err))
		} else {
			for _, order := range liveOrders {
				symbol := strings.ToUpper(strings.TrimSpace(toString(firstPresent(order["symbol"], order["ticker"]))))
				if symbol != "" {
					symbolSet[symbol] = struct{}{}
				}
			}
		}
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		errs = append(errs, fmt.Sprintf("fetch positions: %v", err))
	} else {
		for _, pos := range positions {
			symbol := strings.ToUpper(strings.TrimSpace(toString(firstPresent(pos["symbol"], pos["ticker"]))))
			if symbol != "" {
				symbolSet[symbol] = struct{}{}
			}
		}
	}

	if strings.EqualFold(at.exchange, "ibkr") || strings.EqualFold(at.config.Broker, "ibkr") {
		if err := at.trader.CancelAllOrders(""); err != nil {
			errs = append(errs, fmt.Sprintf("bulk cancel: %v", err))
		}
	} else {
		symbols := make([]string, 0, len(symbolSet))
		for symbol := range symbolSet {
			symbols = append(symbols, symbol)
		}
		sort.Strings(symbols)
		for _, symbol := range symbols {
			if err := at.trader.CancelAllOrders(symbol); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", symbol, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (at *AutoTrader) evaluateKillSwitchSignal() killSwitchSignal {
	if at.config.EmergencyKillSwitch {
		return killSwitchSignal{
			Active:  true,
			Source:  "config",
			Message: "local config flag emergency_kill_switch=true",
		}
	}

	envSuffix := killSwitchEnvSuffix(at.id)
	if signal := killSwitchSignalFromEnv(killSwitchEnvVarName+"_"+envSuffix, "env"); signal.Active {
		return signal
	}
	if signal := killSwitchSignalFromEnv(killSwitchEnvVarName, "env"); signal.Active {
		return signal
	}

	for _, candidate := range at.killSwitchFileCandidates() {
		if candidate == "" {
			continue
		}
		if signal := killSwitchSignalFromFile(candidate); signal.Active {
			return signal
		}
	}

	return killSwitchSignal{}
}

func (at *AutoTrader) killSwitchFileCandidates() []string {
	envSuffix := killSwitchEnvSuffix(at.id)
	candidates := []string{
		os.Getenv(killSwitchFileEnvVarName + "_" + envSuffix),
		os.Getenv(killSwitchFileEnvVarName),
		at.config.KillSwitchFile,
		filepath.Join("runtime", "kill_switch", at.id+".switch"),
		filepath.Join("runtime", "kill_switch", "global.switch"),
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func killSwitchSignalFromEnv(name, source string) killSwitchSignal {
	raw := strings.TrimSpace(os.Getenv(name))
	active, detail := parseKillSwitchValue(raw)
	if !active {
		return killSwitchSignal{}
	}
	message := fmt.Sprintf("environment variable %s is set", name)
	if detail != "" {
		message = fmt.Sprintf("%s: %s", message, detail)
	}
	return killSwitchSignal{
		Active:  true,
		Source:  source,
		Message: message,
	}
}

func killSwitchSignalFromFile(path string) killSwitchSignal {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return killSwitchSignal{}
	}
	absPath := path
	if resolved, resolveErr := filepath.Abs(path); resolveErr == nil {
		absPath = resolved
	}

	message := fmt.Sprintf("kill switch file present at %s", absPath)
	if data, readErr := os.ReadFile(path); readErr == nil {
		if detail := strings.TrimSpace(string(data)); detail != "" {
			lines := strings.Split(detail, "\n")
			detail = strings.TrimSpace(lines[0])
			if detail != "" {
				message = fmt.Sprintf("%s: %s", message, detail)
			}
		}
	}

	return killSwitchSignal{
		Active:   true,
		Source:   "file",
		Message:  message,
		FilePath: absPath,
	}
}

func parseKillSwitchValue(raw string) (bool, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false, ""
	}
	switch strings.ToLower(value) {
	case "0", "false", "off", "no", "clear":
		return false, ""
	case "1", "true", "on", "yes":
		return true, ""
	default:
		return true, value
	}
}

func killSwitchEnvSuffix(traderID string) string {
	traderID = strings.TrimSpace(strings.ToUpper(traderID))
	if traderID == "" {
		return "DEFAULT"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range traderID {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "DEFAULT"
	}
	return out
}
