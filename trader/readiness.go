package trader

import (
	"fmt"
	"log"
	"northstar/broker"
	"northstar/market"
	"northstar/news"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReadinessStatus string

const (
	ReadinessPass ReadinessStatus = "pass"
	ReadinessWarn ReadinessStatus = "warn"
	ReadinessFail ReadinessStatus = "fail"
)

type ReadinessSeverity string

const (
	ReadinessSeverityInfo     ReadinessSeverity = "info"
	ReadinessSeverityWarning  ReadinessSeverity = "warning"
	ReadinessSeverityCritical ReadinessSeverity = "critical"
)

type ReadinessCheck struct {
	Name           string            `json:"name"`
	Status         ReadinessStatus   `json:"status"`
	Severity       ReadinessSeverity `json:"severity"`
	Message        string            `json:"message"`
	Timestamp      time.Time         `json:"timestamp"`
	TradingAllowed bool              `json:"trading_allowed"`
}

type ReadinessSummary struct {
	Status         ReadinessStatus  `json:"status"`
	Message        string           `json:"message"`
	CheckedAt      time.Time        `json:"checked_at"`
	TradingAllowed bool             `json:"trading_allowed"`
	PassCount      int              `json:"pass_count"`
	WarnCount      int              `json:"warn_count"`
	FailCount      int              `json:"fail_count"`
	Checks         []ReadinessCheck `json:"checks"`
}

type readinessBuilder struct {
	now    time.Time
	checks []ReadinessCheck
}

type liveOrdersReadinessFetcher interface {
	GetLiveOrders() ([]map[string]interface{}, error)
}

func (at *AutoTrader) initializeReadinessSummary() {
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessWarn,
		Message:        "startup readiness pending",
		CheckedAt:      time.Now(),
		TradingAllowed: false,
	})
}

func (at *AutoTrader) setReadinessSummary(summary ReadinessSummary) {
	at.readinessMu.Lock()
	at.readinessSummary = summary
	at.readinessMu.Unlock()
	at.syncReadinessIncident(summary)
}

func (at *AutoTrader) getReadinessSummary() ReadinessSummary {
	at.readinessMu.RLock()
	defer at.readinessMu.RUnlock()

	summary := at.readinessSummary
	if summary.Checks == nil {
		summary.Checks = []ReadinessCheck{}
	}
	return summary
}

func (at *AutoTrader) waitForStartupReadiness() error {
	for at.isRunning.Load() {
		summary := at.runReadinessChecks()
		at.logReadinessSummary(summary)
		if summary.TradingAllowed {
			return nil
		}
		at.alertTradingBlocked(summary.Message)

		delay := at.startupReadinessRetryInterval()
		log.Printf(" [%s] Active trading remains blocked by startup readiness; retrying in %s", at.name, delay)
		if !at.sleepWhileRunning(delay) {
			return nil
		}
	}
	return nil
}

func (at *AutoTrader) startupReadinessRetryInterval() time.Duration {
	delay := at.config.ScanInterval
	if delay <= 0 {
		delay = 30 * time.Second
	}
	if delay < 10*time.Second {
		delay = 10 * time.Second
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

func (at *AutoTrader) runReadinessChecks() ReadinessSummary {
	builder := readinessBuilder{
		now:    time.Now(),
		checks: make([]ReadinessCheck, 0, 8),
	}

	builder.add(at.checkConfigReadiness())
	builder.add(at.checkModeCompatibilityReadiness())
	builder.add(at.checkDataReadiness())
	builder.add(at.checkAIReadiness())
	builder.add(at.checkNewsReadiness())
	builder.add(at.checkBrokerConfigReadiness())
	builder.add(at.checkBrokerConnectivityReadiness())
	builder.add(at.checkBrokerBootstrapReadiness())
	builder.add(at.checkRestartRecoveryReadiness())

	summary := builder.summary()
	at.setReadinessSummary(summary)
	at.observeReadinessSummary(summary)
	return summary
}

func (at *AutoTrader) logReadinessSummary(summary ReadinessSummary) {
	log.Printf(
		" [%s] Startup readiness: status=%s trading_allowed=%t pass=%d warn=%d fail=%d | %s",
		at.name,
		summary.Status,
		summary.TradingAllowed,
		summary.PassCount,
		summary.WarnCount,
		summary.FailCount,
		summary.Message,
	)
	for _, check := range summary.Checks {
		if check.Status == ReadinessPass {
			continue
		}
		log.Printf(" [%s] Readiness %s (%s): %s", at.name, check.Name, check.Status, check.Message)
	}
}

func (rb *readinessBuilder) add(check ReadinessCheck) {
	if check.Timestamp.IsZero() {
		check.Timestamp = rb.now
	}
	rb.checks = append(rb.checks, check)
}

func (rb *readinessBuilder) summary() ReadinessSummary {
	summary := ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      rb.now,
		TradingAllowed: true,
		Checks:         append([]ReadinessCheck(nil), rb.checks...),
	}

	blockers := make([]string, 0, 2)
	warnings := make([]string, 0, 2)
	for _, check := range rb.checks {
		switch check.Status {
		case ReadinessPass:
			summary.PassCount++
		case ReadinessWarn:
			summary.WarnCount++
			if summary.Status == ReadinessPass {
				summary.Status = ReadinessWarn
				summary.Message = "startup readiness passed with warnings"
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", check.Name, check.Message))
		case ReadinessFail:
			summary.FailCount++
			summary.Status = ReadinessFail
			summary.TradingAllowed = false
			blockers = append(blockers, fmt.Sprintf("%s: %s", check.Name, check.Message))
		}
		if !check.TradingAllowed {
			summary.TradingAllowed = false
		}
	}

	switch {
	case len(blockers) > 0:
		summary.Message = fmt.Sprintf("%d blocking readiness check(s) failed", len(blockers))
	case len(warnings) > 0:
		summary.Message = fmt.Sprintf("%d readiness warning(s) present", len(warnings))
	}

	return summary
}

func readinessPass(name, message string) ReadinessCheck {
	return ReadinessCheck{
		Name:           name,
		Status:         ReadinessPass,
		Severity:       ReadinessSeverityInfo,
		Message:        message,
		TradingAllowed: true,
	}
}

func readinessWarn(name, message string) ReadinessCheck {
	return ReadinessCheck{
		Name:           name,
		Status:         ReadinessWarn,
		Severity:       ReadinessSeverityWarning,
		Message:        message,
		TradingAllowed: true,
	}
}

func readinessFail(name, message string) ReadinessCheck {
	return ReadinessCheck{
		Name:           name,
		Status:         ReadinessFail,
		Severity:       ReadinessSeverityCritical,
		Message:        message,
		TradingAllowed: false,
	}
}

func (at *AutoTrader) checkConfigReadiness() ReadinessCheck {
	switch {
	case strings.TrimSpace(at.id) == "":
		return readinessFail("config_sanity", "trader ID is missing")
	case strings.TrimSpace(at.name) == "":
		return readinessFail("config_sanity", "trader name is missing")
	case at.initialBalance <= 0:
		return readinessFail("config_sanity", "initial balance must be greater than zero")
	case at.trader == nil:
		return readinessFail("config_sanity", "trader execution engine is not initialized")
	default:
		return readinessPass("config_sanity", fmt.Sprintf("mode=%s exchange=%s broker=%s", at.config.Mode, at.exchange, at.config.Broker))
	}
}

func (at *AutoTrader) checkModeCompatibilityReadiness() ReadinessCheck {
	mode := strings.ToLower(strings.TrimSpace(at.config.Mode))
	if mode == "" {
		mode = "paper"
	}

	switch mode {
	case "paper", "live", "replay", "shadow":
	default:
		return readinessFail("mode_compatibility", fmt.Sprintf("unsupported mode: %s", at.config.Mode))
	}

	if at.demoMode {
		return readinessPass("mode_compatibility", "demo mode uses synthetic broker/data dependencies only")
	}
	if mode == "replay" {
		return readinessPass("mode_compatibility", fmt.Sprintf("replay mode active with data_provider=%s broker=%s", at.config.DataProvider, at.config.Broker))
	}
	if at.provider == nil {
		return readinessFail("mode_compatibility", "active trading mode requires an initialized market data provider")
	}
	return readinessPass("mode_compatibility", fmt.Sprintf("active trading mode ready with data_provider=%s broker=%s", at.config.DataProvider, at.config.Broker))
}

func (at *AutoTrader) checkDataReadiness() ReadinessCheck {
	if at.demoMode {
		return readinessPass("data_readiness", "demo mode does not require an external market data source")
	}
	if strings.EqualFold(at.config.Mode, "replay") && strings.EqualFold(at.config.DataProvider, "csv") {
		dir := strings.TrimSpace(at.config.CSVDataDir)
		if dir == "" {
			return readinessFail("data_readiness", "replay mode requires csv_data_dir")
		}
		info, err := os.Stat(dir)
		if err != nil {
			return readinessFail("data_readiness", fmt.Sprintf("csv data directory unavailable: %v", err))
		}
		if !info.IsDir() {
			return readinessFail("data_readiness", "csv_data_dir must point to a directory")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return readinessFail("data_readiness", fmt.Sprintf("csv data directory is not readable: %v", err))
		}
		csvCount := 0
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".csv") {
				csvCount++
			}
		}
		if csvCount == 0 {
			return readinessFail("data_readiness", "csv data directory contains no .csv files")
		}
		return readinessPass("data_readiness", fmt.Sprintf("csv replay dataset ready (%d files)", csvCount))
	}
	if at.provider == nil {
		return readinessFail("data_readiness", "market data provider is not initialized")
	}
	if at.requiresIBKRSessionReadiness() && strings.EqualFold(at.config.DataProvider, "ibkr") {
		return readinessPass("data_readiness", "IBKR-backed market data dependency will be validated through broker session readiness")
	}
	return readinessPass("data_readiness", fmt.Sprintf("market data provider initialized (%T)", at.provider))
}

func (at *AutoTrader) checkAIReadiness() ReadinessCheck {
	if at.demoMode {
		return readinessPass("ai_readiness", "demo mode does not require AI credentials")
	}
	if !at.requiresAIReadiness() {
		return readinessPass("ai_readiness", fmt.Sprintf("strategy mode %s can run without remote AI credentials", at.config.StrategyMode))
	}

	switch strings.ToLower(strings.TrimSpace(at.aiModel)) {
	case "qwen":
		if strings.TrimSpace(at.config.QwenKey) == "" {
			return readinessFail("ai_readiness", "Qwen strategy mode requires qwen_key or NORTHSTAR_QWEN_API_KEY")
		}
		return readinessPass("ai_readiness", "Qwen API credentials configured")
	case "custom":
		switch {
		case strings.TrimSpace(at.config.CustomAPIURL) == "":
			return readinessFail("ai_readiness", "custom AI provider requires custom_api_url")
		case strings.TrimSpace(at.config.CustomAPIKey) == "":
			return readinessFail("ai_readiness", "custom AI provider requires custom_api_key")
		case strings.TrimSpace(at.config.CustomModelName) == "":
			return readinessFail("ai_readiness", "custom AI provider requires custom_model_name")
		default:
			return readinessPass("ai_readiness", "custom AI provider configuration present")
		}
	default:
		if strings.TrimSpace(at.config.DeepSeekKey) == "" {
			return readinessFail("ai_readiness", "DeepSeek strategy mode requires deepseek_key or NORTHSTAR_DEEPSEEK_API_KEY")
		}
		return readinessPass("ai_readiness", "DeepSeek API credentials configured")
	}
}

func (at *AutoTrader) checkNewsReadiness() ReadinessCheck {
	if at.config.InstrumentType != "equity" || !at.config.UseNewsRisk {
		return readinessPass("news_readiness", "news risk filter not enabled")
	}
	if strings.EqualFold(at.config.Mode, "replay") && !at.config.EnableNewsInReplay {
		return readinessPass("news_readiness", "news risk is intentionally disabled during replay mode")
	}
	if at.newsProvider != nil {
		return readinessPass("news_readiness", fmt.Sprintf("news provider ready (%s)", at.newsProvider.Name()))
	}
	if _, err := news.NewProvider(at.config.NewsProvider); err != nil {
		return readinessWarn("news_readiness", fmt.Sprintf("news risk provider unavailable: %v", err))
	}
	return readinessWarn("news_readiness", "news risk provider configuration is present but provider did not initialize")
}

func (at *AutoTrader) checkBrokerConfigReadiness() ReadinessCheck {
	if !at.requiresBrokerDependency() {
		return readinessPass("broker_config", "broker connectivity is not required for this mode")
	}
	if at.requiresIBKRSessionReadiness() {
		switch {
		case strings.TrimSpace(at.config.IBKRGatewayURL) == "":
			return readinessFail("broker_config", "IBKR gateway URL is missing")
		case strings.TrimSpace(at.config.IBKRAccountID) == "":
			return readinessFail("broker_config", "IBKR account ID is missing")
		default:
			return readinessPass("broker_config", "IBKR broker configuration present")
		}
	}
	return readinessPass("broker_config", fmt.Sprintf("broker configuration present for %s", at.config.Broker))
}

func (at *AutoTrader) checkBrokerConnectivityReadiness() ReadinessCheck {
	if !at.requiresIBKRSessionReadiness() {
		if at.requiresBrokerDependency() {
			return readinessPass("broker_connectivity", fmt.Sprintf("no explicit startup connectivity probe required for broker=%s", at.config.Broker))
		}
		return readinessPass("broker_connectivity", "broker connectivity is not required for this mode")
	}

	ibkrProvider, ok := at.provider.(*market.IBKRProvider)
	if !ok || ibkrProvider == nil || ibkrProvider.Client == nil {
		return readinessFail("broker_connectivity", "IBKR data/broker provider is not initialized")
	}

	if err := ibkrProvider.Client.CheckSessionReadiness(at.config.IBKRAccountID); err != nil {
		at.applyIBKRStartupReadinessFailure("broker_connectivity", err)
		return readinessFail("broker_connectivity", fmt.Sprintf("IBKR session/connectivity check failed: %v", err))
	}

	return readinessPass("broker_connectivity", "IBKR session and account connectivity are ready")
}

func (at *AutoTrader) checkBrokerBootstrapReadiness() ReadinessCheck {
	if !at.requiresBrokerBootstrapReadiness() {
		return readinessPass("broker_bootstrap", "broker bootstrap snapshot is not required for this mode")
	}

	if reconciler, ok := at.trader.(ibkrRuntimeReconciler); ok {
		snapshot, err := reconciler.ReconcileBrokerState()
		if err != nil {
			at.applyIBKRStartupReadinessFailure("broker_bootstrap", err)
			return readinessFail("broker_bootstrap", fmt.Sprintf("broker bootstrap reconciliation failed: %v", err))
		}
		at.markIBKRHealthyWithReason("startup readiness passed; broker bootstrap reconciled")
		positions := 0
		openOrders := 0
		if snapshot != nil {
			positions = len(snapshot.Positions)
			openOrders = len(snapshot.OpenOrders)
			at.seedRuntimeAccountSnapshot(snapshot.Balance, snapshot.Positions)
		}
		return readinessPass("broker_bootstrap", fmt.Sprintf("broker bootstrap reconciled account state (%d positions, %d open orders)", positions, openOrders))
	}

	balance, err := at.trader.GetBalance()
	if err != nil {
		return readinessFail("broker_bootstrap", fmt.Sprintf("startup balance snapshot failed: %v", err))
	}
	positions, err := at.trader.GetPositions()
	if err != nil {
		return readinessFail("broker_bootstrap", fmt.Sprintf("startup positions snapshot failed: %v", err))
	}
	if orderFetcher, ok := at.trader.(liveOrdersReadinessFetcher); ok {
		orders, err := orderFetcher.GetLiveOrders()
		if err != nil {
			return readinessFail("broker_bootstrap", fmt.Sprintf("startup open orders snapshot failed: %v", err))
		}
		at.seedRuntimeAccountSnapshot(balance, positions)
		_ = balance
		return readinessPass("broker_bootstrap", fmt.Sprintf("broker bootstrap loaded balance, %d positions, %d open orders", len(positions), len(orders)))
	}
	at.seedRuntimeAccountSnapshot(balance, positions)
	_ = balance
	return readinessPass("broker_bootstrap", fmt.Sprintf("broker bootstrap loaded balance and %d positions", len(positions)))
}

func (at *AutoTrader) requiresAIReadiness() bool {
	if at.demoMode {
		return false
	}
	if at.config.InstrumentType == "equity" && (at.config.StrategyMode == "momentum_only" || at.config.StrategyMode == "multi_factor") {
		return false
	}
	return true
}

func (at *AutoTrader) requiresBrokerDependency() bool {
	if at.demoMode {
		return false
	}
	if strings.EqualFold(at.config.Mode, "replay") {
		return strings.EqualFold(at.config.Broker, "ibkr") || strings.EqualFold(at.config.DataProvider, "ibkr")
	}
	return strings.TrimSpace(at.config.Broker) != "" && !strings.EqualFold(at.config.Broker, "sim")
}

func (at *AutoTrader) requiresBrokerBootstrapReadiness() bool {
	if at.demoMode {
		return false
	}
	if strings.EqualFold(at.config.Mode, "replay") {
		return false
	}
	if strings.EqualFold(at.config.Broker, "sim") {
		return false
	}
	return at.trader != nil
}

func (at *AutoTrader) requiresIBKRSessionReadiness() bool {
	if at.demoMode {
		return false
	}
	if strings.EqualFold(at.config.Mode, "replay") {
		return strings.EqualFold(at.config.Broker, "ibkr") || strings.EqualFold(at.config.DataProvider, "ibkr")
	}
	if strings.EqualFold(at.exchange, "ibkr") || strings.EqualFold(at.config.Broker, "ibkr") || strings.EqualFold(at.config.DataProvider, "ibkr") {
		return true
	}
	return false
}

func (at *AutoTrader) applyIBKRStartupReadinessFailure(stage string, err error) {
	if err == nil || !at.managesIBKRBrokerRuntime() {
		return
	}

	reason := fmt.Sprintf("startup readiness %s failed: %v", stage, err)
	switch broker.ClassifyIBKRError(err) {
	case broker.IBKRErrorTransient:
		at.alertBrokerDisconnect(stage, err)
		at.setBrokerRuntimeState(BrokerRuntimeDegraded, reason, err, false, time.Time{})
	case broker.IBKRErrorAuth:
		at.setBrokerRuntimeState(BrokerRuntimePaused, reason, err, false, time.Time{})
	}
}
