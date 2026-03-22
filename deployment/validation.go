package deployment

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"northstar/buildinfo"
	"northstar/config"
	"northstar/manager"
	"northstar/trader"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

type TraderSummary struct {
	TraderID           string                  `json:"trader_id"`
	TraderName         string                  `json:"trader_name"`
	Status             Status                  `json:"status"`
	Message            string                  `json:"message"`
	LiveTradingAllowed bool                    `json:"live_trading_allowed"`
	Readiness          trader.ReadinessSummary `json:"readiness"`
	Promotion          trader.PromotionSummary `json:"promotion"`
}

type Summary struct {
	Status            Status          `json:"status"`
	Message           string          `json:"message"`
	CheckedAt         time.Time       `json:"checked_at"`
	ConfigFile        string          `json:"config_file"`
	RepositoryRoot    string          `json:"repository_root"`
	LiveReady         bool            `json:"live_ready"`
	PassCount         int             `json:"pass_count"`
	WarnCount         int             `json:"warn_count"`
	FailCount         int             `json:"fail_count"`
	Checks            []Check         `json:"checks"`
	TraderValidations []TraderSummary `json:"trader_validations"`
}

type GitStatus struct {
	Root         string
	DirtyEntries []string
}

type GitInspector interface {
	Inspect(startDir string) (GitStatus, error)
}

type liveStartValidator interface {
	ValidateLiveStart() trader.LiveStartValidation
}

type liveTraderFactory func(cfg *config.Config, liveTraders []config.TraderConfig) ([]liveStartValidator, error)

type Validator struct {
	Now           func() time.Time
	BuildInfo     func() buildinfo.Info
	GitInspector  GitInspector
	TraderFactory liveTraderFactory
}

func NewValidator() *Validator {
	return &Validator{
		Now:           time.Now,
		BuildInfo:     buildinfo.Current,
		GitInspector:  defaultGitInspector{},
		TraderFactory: buildLiveTraders,
	}
}

func (v *Validator) ValidateLiveConfig(configFile string) Summary {
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}

	summary := Summary{
		Status:            StatusPass,
		Message:           "live deployment validation passed",
		CheckedAt:         now,
		ConfigFile:        configFile,
		LiveReady:         true,
		Checks:            []Check{},
		TraderValidations: []TraderSummary{},
	}

	addCheck := func(check Check) {
		if check.CheckedAt.IsZero() {
			check.CheckedAt = now
		}
		summary.Checks = append(summary.Checks, check)
		switch check.Status {
		case StatusPass:
			summary.PassCount++
		case StatusWarn:
			summary.WarnCount++
			if summary.Status == StatusPass {
				summary.Status = StatusWarn
				summary.Message = "live deployment validation passed with warnings"
			}
		case StatusFail:
			summary.FailCount++
			summary.Status = StatusFail
			summary.LiveReady = false
			summary.Message = "live deployment validation failed"
		}
	}

	buildInfoFn := v.BuildInfo
	if buildInfoFn == nil {
		buildInfoFn = buildinfo.Current
	}
	addCheck(buildIdentityCheck(buildInfoFn(), now))

	if wd, err := os.Getwd(); err != nil {
		addCheck(Check{Name: "clean_working_tree", Status: StatusFail, Message: fmt.Sprintf("failed to determine working directory: %v", err)})
	} else {
		gitInspector := v.GitInspector
		if gitInspector == nil {
			gitInspector = defaultGitInspector{}
		}
		gitStatus, err := gitInspector.Inspect(wd)
		if err != nil {
			addCheck(Check{Name: "clean_working_tree", Status: StatusFail, Message: fmt.Sprintf("failed to verify git working tree cleanliness: %v", err)})
		} else {
			summary.RepositoryRoot = gitStatus.Root
			if len(gitStatus.DirtyEntries) > 0 {
				addCheck(Check{Name: "clean_working_tree", Status: StatusFail, Message: fmt.Sprintf("working tree has %d uncommitted path(s): %s", len(gitStatus.DirtyEntries), summarizeDirtyEntries(gitStatus.DirtyEntries))})
			} else {
				addCheck(Check{Name: "clean_working_tree", Status: StatusPass, Message: fmt.Sprintf("working tree is clean (%s)", gitStatus.Root)})
			}
		}
	}

	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		addCheck(Check{Name: "config_ready", Status: StatusFail, Message: fmt.Sprintf("failed to load config: %v", err)})
		return summary
	}
	addCheck(Check{Name: "config_ready", Status: StatusPass, Message: fmt.Sprintf("config parsed successfully (%d trader entries)", len(cfg.Traders))})

	liveTraders := enabledLiveTraders(cfg.Traders)
	if len(liveTraders) == 0 {
		addCheck(Check{Name: "live_traders_present", Status: StatusFail, Message: "no enabled live traders were found in config"})
		return summary
	}
	addCheck(Check{Name: "live_traders_present", Status: StatusPass, Message: fmt.Sprintf("%d enabled live trader(s) found", len(liveTraders))})

	for _, traderCfg := range liveTraders {
		addCheck(riskLimitsCheck(cfg, traderCfg, now))
	}

	traderFactory := v.TraderFactory
	if traderFactory == nil {
		traderFactory = buildLiveTraders
	}
	liveTraderInstances, err := traderFactory(cfg, liveTraders)
	if err != nil {
		addCheck(Check{Name: "live_trader_initialization", Status: StatusFail, Message: fmt.Sprintf("failed to initialize live trader validation: %v", err)})
		return summary
	}

	for _, liveTrader := range liveTraderInstances {
		validation := liveTrader.ValidateLiveStart()
		traderStatus := StatusPass
		if !validation.LiveTradingAllowed {
			traderStatus = StatusFail
		} else if validation.Readiness.Status == trader.ReadinessWarn || validation.Promotion.Status == trader.PromotionWarn {
			traderStatus = StatusWarn
		}

		summary.TraderValidations = append(summary.TraderValidations, TraderSummary{
			TraderID:           validation.TraderID,
			TraderName:         validation.TraderName,
			Status:             traderStatus,
			Message:            validation.ValidationMessage,
			LiveTradingAllowed: validation.LiveTradingAllowed,
			Readiness:          validation.Readiness,
			Promotion:          validation.Promotion,
		})

		readinessStatus := StatusPass
		if !validation.Readiness.TradingAllowed {
			readinessStatus = StatusFail
		} else if validation.Readiness.Status == trader.ReadinessWarn {
			readinessStatus = StatusWarn
		}
		addCheck(Check{
			Name:      fmt.Sprintf("readiness[%s]", validation.TraderID),
			Status:    readinessStatus,
			Message:   validation.Readiness.Message,
			CheckedAt: validation.Readiness.CheckedAt,
		})

		promotionStatus := StatusPass
		switch validation.Promotion.Status {
		case trader.PromotionWarn:
			promotionStatus = StatusWarn
		case trader.PromotionFail:
			promotionStatus = StatusFail
		}
		addCheck(Check{
			Name:      fmt.Sprintf("promotion[%s]", validation.TraderID),
			Status:    promotionStatus,
			Message:   validation.Promotion.Message,
			CheckedAt: validation.Promotion.CheckedAt,
		})
	}

	return summary
}

func buildIdentityCheck(info buildinfo.Info, checkedAt time.Time) Check {
	issues := make([]string, 0, 4)
	if strings.EqualFold(strings.TrimSpace(info.Version), "dev") || strings.TrimSpace(info.Version) == "" {
		issues = append(issues, "version is dev")
	}
	if strings.EqualFold(strings.TrimSpace(info.Commit), "unknown") || strings.TrimSpace(info.Commit) == "" {
		issues = append(issues, "commit is unknown")
	}
	if !strings.EqualFold(strings.TrimSpace(info.Dirty), "clean") {
		issues = append(issues, fmt.Sprintf("dirty state is %s", strings.TrimSpace(info.Dirty)))
	}
	if strings.EqualFold(strings.TrimSpace(info.Channel), "local") || strings.TrimSpace(info.Channel) == "" {
		issues = append(issues, "build channel is local")
	}

	if len(issues) == 0 {
		return Check{
			Name:      "release_build",
			Status:    StatusPass,
			Message:   fmt.Sprintf("release build identity present (%s)", info.Summary()),
			CheckedAt: checkedAt,
		}
	}

	return Check{
		Name:      "release_build",
		Status:    StatusFail,
		Message:   "build is not release-ready: " + strings.Join(issues, "; "),
		CheckedAt: checkedAt,
	}
}

func riskLimitsCheck(cfg *config.Config, traderCfg config.TraderConfig, checkedAt time.Time) Check {
	issues := make([]string, 0, 8)
	if cfg.MaxDailyLoss <= 0 {
		issues = append(issues, "max_daily_loss must be greater than zero")
	}
	if cfg.MaxDrawdown <= 0 {
		issues = append(issues, "max_drawdown must be greater than zero")
	}
	if cfg.StopTradingMinutes <= 0 {
		issues = append(issues, "stop_trading_minutes must be greater than zero")
	}

	if strings.EqualFold(traderCfg.InstrumentType, "equity") || strings.EqualFold(traderCfg.Exchange, "ibkr") || strings.EqualFold(traderCfg.Exchange, "alpaca") {
		if traderCfg.RiskPerTradePct <= 0 {
			issues = append(issues, "risk_per_trade_pct must be greater than zero")
		}
		if traderCfg.MaxGrossExposure <= 0 {
			issues = append(issues, "max_gross_exposure must be greater than zero")
		}
		if traderCfg.MaxPositionPct <= 0 {
			issues = append(issues, "max_position_pct must be greater than zero")
		}
		if traderCfg.MaxConcurrentPos <= 0 {
			issues = append(issues, "max_concurrent_positions must be greater than zero")
		}
		if traderCfg.MaxDailyLossPct <= 0 {
			issues = append(issues, "max_daily_loss_pct must be greater than zero")
		}
		if traderCfg.MaxNetExposurePct <= 0 {
			issues = append(issues, "max_net_exposure_pct must be greater than zero")
		}
		if traderCfg.MaxSectorExposurePct <= 0 {
			issues = append(issues, "max_sector_exposure_pct must be greater than zero")
		}
		if traderCfg.MaxCorrelatedPositions <= 0 {
			issues = append(issues, "max_correlated_positions must be greater than zero")
		}
	}

	name := fmt.Sprintf("risk_limits_defined[%s]", traderCfg.ID)
	if len(issues) > 0 {
		return Check{
			Name:      name,
			Status:    StatusFail,
			Message:   strings.Join(issues, "; "),
			CheckedAt: checkedAt,
		}
	}

	message := fmt.Sprintf(
		"risk limits present: max_daily_loss=%.4f max_drawdown=%.4f stop_trading_minutes=%d",
		cfg.MaxDailyLoss,
		cfg.MaxDrawdown,
		cfg.StopTradingMinutes,
	)
	if strings.EqualFold(traderCfg.InstrumentType, "equity") || strings.EqualFold(traderCfg.Exchange, "ibkr") || strings.EqualFold(traderCfg.Exchange, "alpaca") {
		message = fmt.Sprintf(
			"%s risk_per_trade_pct=%.4f max_gross_exposure=%.4f max_position_pct=%.4f max_concurrent_positions=%d",
			message,
			traderCfg.RiskPerTradePct,
			traderCfg.MaxGrossExposure,
			traderCfg.MaxPositionPct,
			traderCfg.MaxConcurrentPos,
		)
	}
	return Check{
		Name:      name,
		Status:    StatusPass,
		Message:   message,
		CheckedAt: checkedAt,
	}
}

func enabledLiveTraders(traders []config.TraderConfig) []config.TraderConfig {
	result := make([]config.TraderConfig, 0, len(traders))
	for _, traderCfg := range traders {
		if traderCfg.Enabled && strings.EqualFold(traderCfg.Mode, "live") {
			result = append(result, traderCfg)
		}
	}
	return result
}

func buildLiveTraders(cfg *config.Config, liveTraders []config.TraderConfig) ([]liveStartValidator, error) {
	traderManager := manager.NewTraderManager()
	for _, traderCfg := range liveTraders {
		if err := traderManager.AddTrader(
			traderCfg,
			cfg.DefaultCoins,
			cfg.DefaultCoinsFile,
			cfg.CoinPoolAPIURL,
			cfg.MaxDailyLoss,
			cfg.MaxDrawdown,
			cfg.StopTradingMinutes,
			cfg.Leverage,
		); err != nil {
			return nil, err
		}
	}

	result := make([]liveStartValidator, 0, len(liveTraders))
	for _, traderCfg := range liveTraders {
		at, err := traderManager.GetTrader(traderCfg.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, at)
	}
	return result, nil
}

type defaultGitInspector struct{}

func (defaultGitInspector) Inspect(startDir string) (GitStatus, error) {
	root, err := findGitRoot(startDir)
	if err != nil {
		return GitStatus{}, err
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return GitStatus{}, fmt.Errorf("git status failed: %w", err)
	}

	lines := splitNonEmptyLines(string(output))
	return GitStatus{
		Root:         root,
		DirtyEntries: lines,
	}, nil
}

func findGitRoot(startDir string) (string, error) {
	current := startDir
	for {
		if current == "" {
			return "", fmt.Errorf("no git repository found from %s", startDir)
		}
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no git repository found from %s", startDir)
		}
		current = parent
	}
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func summarizeDirtyEntries(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	var buf bytes.Buffer
	limit := len(entries)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(entries[i])
	}
	if len(entries) > limit {
		fmt.Fprintf(&buf, " (+%d more)", len(entries)-limit)
	}
	return buf.String()
}
