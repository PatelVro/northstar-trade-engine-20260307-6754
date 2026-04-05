package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"northstar/buildinfo"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PromotionStatus string

const (
	PromotionNotApplicable PromotionStatus = "not_applicable"
	PromotionPass          PromotionStatus = "pass"
	PromotionWarn          PromotionStatus = "warn"
	PromotionFail          PromotionStatus = "fail"
)

type PromotionSeverity string

const (
	PromotionSeverityInfo     PromotionSeverity = "info"
	PromotionSeverityWarning  PromotionSeverity = "warning"
	PromotionSeverityCritical PromotionSeverity = "critical"
)

type PromotionCheck struct {
	Name               string            `json:"name"`
	Status             PromotionStatus   `json:"status"`
	Severity           PromotionSeverity `json:"severity"`
	Message            string            `json:"message"`
	CheckedAt          time.Time         `json:"checked_at"`
	LiveTradingAllowed bool              `json:"live_trading_allowed"`
}

type PromotionSummary struct {
	Status             PromotionStatus  `json:"status"`
	Message            string           `json:"message"`
	CheckedAt          time.Time        `json:"checked_at"`
	Required           bool             `json:"required"`
	LiveTradingAllowed bool             `json:"live_trading_allowed"`
	PassCount          int              `json:"pass_count"`
	WarnCount          int              `json:"warn_count"`
	FailCount          int              `json:"fail_count"`
	Checks             []PromotionCheck `json:"checks"`
}

type promotionBuilder struct {
	now    time.Time
	checks []PromotionCheck
}

type promotionPaperEvidence struct {
	MatchingReports int
	LatestReport    *PaperSessionReport
	LatestPath      string
	ParseErrors     int
}

type promotionStudySummary struct {
	GeneratedAt          time.Time `json:"generated_at"`
	CompletedProfiles    int       `json:"completed_profiles"`
	CredibleProfiles     int       `json:"credible_profiles"`
	ProvisionalProfiles  int       `json:"provisional_profiles"`
	InsufficientProfiles int       `json:"insufficient_profiles"`
	Warnings             []string  `json:"warnings"`
}

type promotionBacktestEvidence struct {
	Summary     *promotionStudySummary
	Path        string
	ParseErrors int
}

func (at *AutoTrader) requiresLivePromotion() bool {
	return !at.demoMode && strings.EqualFold(at.config.Mode, "live")
}

func (at *AutoTrader) initializePromotionSummary() {
	if at.requiresLivePromotion() {
		at.setPromotionSummary(PromotionSummary{
			Status:             PromotionFail,
			Message:            "live promotion checklist pending",
			CheckedAt:          time.Now(),
			Required:           true,
			LiveTradingAllowed: false,
			Checks:             []PromotionCheck{},
		})
		return
	}

	at.setPromotionSummary(PromotionSummary{
		Status:             PromotionNotApplicable,
		Message:            fmt.Sprintf("live promotion not required for mode=%s", at.config.Mode),
		CheckedAt:          time.Now(),
		Required:           false,
		LiveTradingAllowed: true,
		Checks:             []PromotionCheck{},
	})
}

func (at *AutoTrader) setPromotionSummary(summary PromotionSummary) {
	at.promotionMu.Lock()
	at.promotionSummary = summary
	at.promotionMu.Unlock()
	at.syncPromotionIncident(summary)
}

func (at *AutoTrader) getPromotionSummary() PromotionSummary {
	at.promotionMu.RLock()
	defer at.promotionMu.RUnlock()

	summary := at.promotionSummary
	if summary.Checks == nil {
		summary.Checks = []PromotionCheck{}
	}
	return summary
}

func (at *AutoTrader) waitForLivePromotionApproval() error {
	if !at.requiresLivePromotion() {
		return nil
	}

	for at.isRunning.Load() {
		summary := at.runPromotionChecks()
		at.logPromotionSummary(summary)
		if summary.LiveTradingAllowed {
			log.Printf(" [%s] Live trading permitted: readiness and promotion checks passed", at.name)
			return nil
		}
		at.alertTradingBlocked(summary.Message)

		delay := at.startupReadinessRetryInterval()
		log.Printf(" [%s] Live trading blocked: promotion checklist failed; retrying in %s", at.name, delay)
		if !at.sleepWhileRunning(delay) {
			return nil
		}
	}
	return nil
}

func (at *AutoTrader) ensureLivePromotionReady() error {
	if !at.requiresLivePromotion() {
		return nil
	}

	summary := at.runPromotionChecks()
	if summary.LiveTradingAllowed {
		return nil
	}

	return fmt.Errorf("live promotion blocked trading: %s", summary.Message)
}

func (at *AutoTrader) runPromotionChecks() PromotionSummary {
	if !at.requiresLivePromotion() {
		summary := PromotionSummary{
			Status:             PromotionNotApplicable,
			Message:            fmt.Sprintf("live promotion not required for mode=%s", at.config.Mode),
			CheckedAt:          time.Now(),
			Required:           false,
			LiveTradingAllowed: true,
			Checks:             []PromotionCheck{},
		}
		at.setPromotionSummary(summary)
		return summary
	}

	builder := promotionBuilder{
		now:    time.Now(),
		checks: make([]PromotionCheck, 0, 8),
	}

	builder.add(at.checkPromotionLiveConfigSanity())
	builder.add(at.checkPromotionAcknowledgement())
	builder.add(at.checkPromotionReadiness())
	builder.add(at.checkPromotionBrokerRuntime())
	builder.add(at.checkPromotionPaperSessionEvidence())
	builder.add(at.checkPromotionBacktestEvidence())
	builder.add(at.checkPromotionBuildIdentity())

	summary := builder.summary()
	at.setPromotionSummary(summary)
	return summary
}

func (at *AutoTrader) logPromotionSummary(summary PromotionSummary) {
	log.Printf(
		" [%s] Live promotion: status=%s live_trading_allowed=%t pass=%d warn=%d fail=%d | %s",
		at.name,
		summary.Status,
		summary.LiveTradingAllowed,
		summary.PassCount,
		summary.WarnCount,
		summary.FailCount,
		summary.Message,
	)
	for _, check := range summary.Checks {
		if check.Status == PromotionPass {
			continue
		}
		log.Printf(" [%s] Promotion %s (%s): %s", at.name, check.Name, check.Status, check.Message)
	}
}

func (pb *promotionBuilder) add(check PromotionCheck) {
	if check.CheckedAt.IsZero() {
		check.CheckedAt = pb.now
	}
	pb.checks = append(pb.checks, check)
}

func (pb *promotionBuilder) summary() PromotionSummary {
	summary := PromotionSummary{
		Status:             PromotionPass,
		Message:            "live promotion checklist passed",
		CheckedAt:          pb.now,
		Required:           true,
		LiveTradingAllowed: true,
		Checks:             append([]PromotionCheck(nil), pb.checks...),
	}

	warnings := 0
	failures := 0
	for _, check := range pb.checks {
		switch check.Status {
		case PromotionPass:
			summary.PassCount++
		case PromotionWarn:
			summary.WarnCount++
			warnings++
			if summary.Status == PromotionPass {
				summary.Status = PromotionWarn
				summary.Message = "live promotion checklist passed with warnings"
			}
		case PromotionFail:
			summary.FailCount++
			failures++
			summary.Status = PromotionFail
			summary.LiveTradingAllowed = false
		}
		if !check.LiveTradingAllowed {
			summary.LiveTradingAllowed = false
		}
	}

	switch {
	case failures > 0:
		summary.Message = fmt.Sprintf("%d promotion check(s) failed", failures)
	case warnings > 0:
		summary.Message = fmt.Sprintf("%d promotion warning(s) present", warnings)
	}

	return summary
}

func promotionPass(name, message string) PromotionCheck {
	return PromotionCheck{
		Name:               name,
		Status:             PromotionPass,
		Severity:           PromotionSeverityInfo,
		Message:            message,
		LiveTradingAllowed: true,
	}
}

func promotionWarn(name, message string) PromotionCheck {
	return PromotionCheck{
		Name:               name,
		Status:             PromotionWarn,
		Severity:           PromotionSeverityWarning,
		Message:            message,
		LiveTradingAllowed: true,
	}
}

func promotionFail(name, message string) PromotionCheck {
	return PromotionCheck{
		Name:               name,
		Status:             PromotionFail,
		Severity:           PromotionSeverityCritical,
		Message:            message,
		LiveTradingAllowed: false,
	}
}

func (at *AutoTrader) checkPromotionLiveConfigSanity() PromotionCheck {
	problems := make([]string, 0, 4)

	if !strings.EqualFold(at.config.Mode, "live") {
		problems = append(problems, "mode must be live")
	}
	if strings.TrimSpace(at.config.Broker) == "" || strings.EqualFold(at.config.Broker, "sim") {
		problems = append(problems, "live mode requires a real broker, not sim")
	}

	switch strings.ToLower(strings.TrimSpace(at.exchange)) {
	case "ibkr":
		if !at.config.StrictLiveMode {
			problems = append(problems, "strict_live_mode must be enabled for IBKR live trading")
		}
		if strings.EqualFold(at.config.DataProvider, "csv") {
			problems = append(problems, "live IBKR trading cannot use csv data_provider")
		}
		if strings.TrimSpace(at.config.IBKRGatewayURL) == "" {
			problems = append(problems, "ibkr_gateway_url is required")
		}
		if strings.TrimSpace(at.config.IBKRAccountID) == "" {
			problems = append(problems, "ibkr_account_id is required")
		}
	case "alpaca":
		if strings.TrimSpace(at.config.AlpacaAPIKey) == "" {
			problems = append(problems, "alpaca_api_key is required")
		}
		if strings.TrimSpace(at.config.AlpacaSecretKey) == "" {
			problems = append(problems, "alpaca_secret_key is required")
		}
		if at.config.AlpacaPaperTrading {
			problems = append(problems, "alpaca_paper_trading must be false in live mode")
		}
	case "binance":
		if strings.TrimSpace(at.config.BinanceAPIKey) == "" {
			problems = append(problems, "binance_api_key is required")
		}
		if strings.TrimSpace(at.config.BinanceSecretKey) == "" {
			problems = append(problems, "binance_secret_key is required")
		}
	case "hyperliquid":
		if strings.TrimSpace(at.config.HyperliquidPrivateKey) == "" {
			problems = append(problems, "hyperliquid_private_key is required")
		}
		if strings.TrimSpace(at.config.HyperliquidWalletAddr) == "" {
			problems = append(problems, "hyperliquid_wallet_addr is required")
		}
	case "aster":
		if strings.TrimSpace(at.config.AsterUser) == "" {
			problems = append(problems, "aster_user is required")
		}
		if strings.TrimSpace(at.config.AsterSigner) == "" {
			problems = append(problems, "aster_signer is required")
		}
		if strings.TrimSpace(at.config.AsterPrivateKey) == "" {
			problems = append(problems, "aster_private_key is required")
		}
	default:
		problems = append(problems, fmt.Sprintf("exchange %s is not supported for live promotion", at.exchange))
	}

	if len(problems) > 0 {
		return promotionFail("live_config_sanity", strings.Join(problems, "; "))
	}

	return promotionPass("live_config_sanity", fmt.Sprintf("live config sanity checks passed for broker=%s exchange=%s", at.config.Broker, at.exchange))
}

func (at *AutoTrader) checkPromotionAcknowledgement() PromotionCheck {
	if !at.config.LivePromotionApproved {
		return promotionFail("live_mode_acknowledged", "live_promotion_approved is false; explicit local operator approval is required")
	}
	return promotionPass("live_mode_acknowledged", "explicit live promotion approval is present")
}

func (at *AutoTrader) checkPromotionReadiness() PromotionCheck {
	summary := at.getReadinessSummary()
	if !summary.TradingAllowed {
		return promotionFail("readiness_passed", fmt.Sprintf("startup readiness is blocking trading: %s", summary.Message))
	}
	if summary.Status == ReadinessWarn {
		return promotionWarn("readiness_passed", fmt.Sprintf("startup readiness passed with warnings: %s", summary.Message))
	}
	return promotionPass("readiness_passed", "startup readiness passed")
}

func (at *AutoTrader) checkPromotionBrokerRuntime() PromotionCheck {
	if at.managesIBKRBrokerRuntime() {
		snapshot := at.brokerRuntimeStatus()
		if snapshot.State != BrokerRuntimeHealthy {
			reason := strings.TrimSpace(snapshot.Reason)
			if reason == "" {
				reason = "broker runtime is not healthy"
			}
			return promotionFail("broker_runtime_healthy", fmt.Sprintf("broker runtime is %s: %s", snapshot.State, reason))
		}
		return promotionPass("broker_runtime_healthy", "broker runtime is healthy")
	}

	return promotionPass("broker_runtime_healthy", fmt.Sprintf("no dedicated broker runtime health model is required for broker=%s", at.config.Broker))
}

func (at *AutoTrader) checkPromotionPaperSessionEvidence() PromotionCheck {
	requiredReports := at.config.MinPaperSessionReports
	if requiredReports <= 0 {
		requiredReports = 1
	}

	evidence, err := at.findRecentPaperSessionEvidence()
	if err != nil {
		return promotionFail("paper_session_evidence_present", fmt.Sprintf("paper session evidence check failed: %v", err))
	}
	if evidence.MatchingReports < requiredReports {
		return promotionFail(
			"paper_session_evidence_present",
			fmt.Sprintf("need at least %d recent parseable paper session report(s); found %d", requiredReports, evidence.MatchingReports),
		)
	}

	msg := fmt.Sprintf("found %d recent paper session report(s)", evidence.MatchingReports)
	if evidence.LatestPath != "" {
		msg += fmt.Sprintf(" (latest: %s)", evidence.LatestPath)
	}
	return promotionPass("paper_session_evidence_present", msg)
}

func (at *AutoTrader) checkPromotionBacktestEvidence() PromotionCheck {
	evidence, err := at.findRecentBacktestEvidence()
	if err != nil {
		if at.config.RequireBacktestSummary {
			return promotionFail("backtest_evidence_present", fmt.Sprintf("backtest evidence check failed: %v", err))
		}
		return promotionWarn("backtest_evidence_present", fmt.Sprintf("backtest evidence check failed: %v", err))
	}

	if evidence.Summary == nil {
		if at.config.RequireBacktestSummary {
			return promotionFail("backtest_evidence_present", "no recent parseable backtest study summary was found")
		}
		return promotionWarn("backtest_evidence_present", "no recent backtest study summary was found")
	}

	summary := evidence.Summary
	if summary.CompletedProfiles <= 0 {
		if at.config.RequireBacktestSummary {
			return promotionFail("backtest_evidence_present", fmt.Sprintf("backtest study summary has no completed profiles (%s)", evidence.Path))
		}
		return promotionWarn("backtest_evidence_present", fmt.Sprintf("backtest study summary has no completed profiles (%s)", evidence.Path))
	}

	msg := fmt.Sprintf(
		"backtest study summary present (%s): completed_profiles=%d credible=%d provisional=%d insufficient=%d",
		evidence.Path,
		summary.CompletedProfiles,
		summary.CredibleProfiles,
		summary.ProvisionalProfiles,
		summary.InsufficientProfiles,
	)
	if summary.CredibleProfiles == 0 && summary.ProvisionalProfiles == 0 {
		return promotionWarn("backtest_evidence_present", msg+"; treat this as exploratory evidence only")
	}
	return promotionPass("backtest_evidence_present", msg)
}

func (at *AutoTrader) checkPromotionBuildIdentity() PromotionCheck {
	info := buildinfo.Current()
	issues := make([]string, 0, 4)
	if info.Version == "dev" {
		issues = append(issues, "version is dev")
	}
	if info.Commit == "unknown" {
		issues = append(issues, "commit is unknown")
	}
	if info.Dirty != "clean" {
		issues = append(issues, fmt.Sprintf("dirty state is %s", info.Dirty))
	}
	if info.Channel == "local" {
		issues = append(issues, "build channel is local")
	}

	if len(issues) == 0 {
		return promotionPass("build_identity_present", "release build identity is present")
	}

	message := "build identity is not release-ready: " + strings.Join(issues, "; ")
	if at.config.RequireReleaseBuildForLive {
		return promotionFail("build_identity_present", message)
	}
	return promotionWarn("build_identity_present", message)
}

func (at *AutoTrader) findRecentPaperSessionEvidence() (promotionPaperEvidence, error) {
	evidenceTraderID := at.promotionEvidenceTraderID()
	dir := filepath.Join("output", "session_reports", evidenceTraderID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return promotionPaperEvidence{}, nil
		}
		return promotionPaperEvidence{}, err
	}

	cutoff := at.promotionEvidenceCutoff()
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	result := promotionPaperEvidence{}
	for _, path := range files {
		report, ok := loadPaperSessionReport(path)
		if !ok {
			result.ParseErrors++
			continue
		}
		if !at.isRecentPromotionEvidence(report.GeneratedAt, report.SessionEnd, cutoff) {
			continue
		}
		if !strings.EqualFold(report.Mode, "paper") {
			continue
		}
		if report.TraderID != evidenceTraderID || !strings.EqualFold(report.Broker, at.config.Broker) || report.StrategyMode != at.config.StrategyMode {
			continue
		}
		if report.DecisionCycles <= 0 || report.SessionCompletionStatus == SessionCompletionBlocked {
			continue
		}
		result.MatchingReports++
		if result.LatestReport == nil {
			copied := report
			result.LatestReport = &copied
			result.LatestPath = path
		}
	}

	return result, nil
}

func (at *AutoTrader) promotionEvidenceTraderID() string {
	if referenceID := strings.TrimSpace(at.config.PromotionSourceTraderID); referenceID != "" {
		return referenceID
	}
	return at.id
}

func (at *AutoTrader) findRecentBacktestEvidence() (promotionBacktestEvidence, error) {
	root := "output"
	cutoff := at.promotionEvidenceCutoff()
	result := promotionBacktestEvidence{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != "study_summary.json" {
			return nil
		}

		summary, ok := loadPromotionStudySummary(path)
		if !ok {
			result.ParseErrors++
			return nil
		}
		if !at.isRecentPromotionEvidence(summary.GeneratedAt, time.Time{}, cutoff) {
			return nil
		}
		if result.Summary == nil || summary.GeneratedAt.After(result.Summary.GeneratedAt) {
			copied := summary
			result.Summary = &copied
			result.Path = path
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return promotionBacktestEvidence{}, nil
		}
		return promotionBacktestEvidence{}, err
	}

	return result, nil
}

func (at *AutoTrader) promotionEvidenceCutoff() time.Time {
	days := at.config.PromotionMaxEvidenceAgeDays
	if days <= 0 {
		days = 30
	}
	return time.Now().AddDate(0, 0, -days)
}

func (at *AutoTrader) isRecentPromotionEvidence(primary, secondary, cutoff time.Time) bool {
	for _, ts := range []time.Time{primary, secondary} {
		if ts.IsZero() {
			continue
		}
		return !ts.Before(cutoff)
	}
	return false
}

func loadPaperSessionReport(path string) (PaperSessionReport, bool) {
	var report PaperSessionReport
	raw, err := os.ReadFile(path)
	if err != nil {
		return report, false
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return report, false
	}
	return report, true
}

func loadPromotionStudySummary(path string) (promotionStudySummary, bool) {
	var summary promotionStudySummary
	raw, err := os.ReadFile(path)
	if err != nil {
		return summary, false
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		return summary, false
	}
	return summary, true
}
