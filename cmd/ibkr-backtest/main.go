package main

import (
	"aegistrade/broker"
	"aegistrade/market"
	"aegistrade/pool"
	"aegistrade/trader"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type strategyProfile struct {
	StrategyMode string
	MinScore     float64
	PositionPct  float64
}

func (p strategyProfile) slug() string {
	score := strings.ReplaceAll(fmt.Sprintf("%.2f", p.MinScore), ".", "p")
	pct := strings.ReplaceAll(fmt.Sprintf("%.2f", p.PositionPct), ".", "p")
	return fmt.Sprintf("%s_s%s_p%s", p.StrategyMode, score, pct)
}

func (p strategyProfile) requiresAI() bool {
	return p.StrategyMode != "momentum_only"
}

type replaySummary struct {
	TotalTrades int     `json:"total_trades"`
	WinRatePct  float64 `json:"win_rate_pct"`
	MaxDrawdown float64 `json:"max_drawdown"`
	FinalEquity float64 `json:"final_equity"`
	ReturnPct   float64 `json:"return_pct"`
}

type profileResult struct {
	ProfileSlug      string    `json:"profile_slug"`
	StrategyMode     string    `json:"strategy_mode"`
	MinScore         float64   `json:"min_score"`
	PositionPct      float64   `json:"position_pct"`
	SymbolCount      int       `json:"symbol_count"`
	CyclesExecuted   int       `json:"cycles_executed"`
	DurationSeconds  float64   `json:"duration_seconds"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	TotalTrades      int       `json:"total_trades"`
	WinRatePct       float64   `json:"win_rate_pct"`
	MaxDrawdownPct   float64   `json:"max_drawdown_pct"`
	FinalEquity      float64   `json:"final_equity"`
	ReturnPct        float64   `json:"return_pct"`
	ReplaySummaryRel string    `json:"replay_summary_rel"`
	WorkDirRel       string    `json:"work_dir_rel"`
}

func main() {
	var (
		gatewayURL    = flag.String("gateway-url", "https://127.0.0.1:5002/v1/api", "IBKR Client Portal API URL")
		accountID     = flag.String("account-id", "", "IBKR account ID (required)")
		sessionCookie = flag.String("session-cookie", "", "Optional IBKR session cookie (x-sess-uuid=...)")

		symbolsCSV  = flag.String("symbols", "", "Comma-separated symbols (overrides symbols-file)")
		symbolsFile = flag.String("symbols-file", "data/universe/us_canada_tradable_core.txt", "Path to symbols list file")
		maxSymbols  = flag.Int("max-symbols", 20, "Maximum symbols to include")

		barInterval = flag.String("bar-interval", "1h", "History bar interval for IBKR download: 1m,5m,1h,1d")
		barLimit    = flag.Int("bar-limit", 1000, "Maximum bars per symbol to export")
		skipFetch   = flag.Bool("skip-fetch", false, "Skip IBKR download and use existing CSV files")
		csvDataDir  = flag.String("csv-data-dir", "", "Optional existing CSV directory for replay/backtest (implies -skip-fetch)")

		maxCycles       = flag.Int("max-cycles", 240, "Backtest cycles per profile")
		warmupBars      = flag.Int("replay-warmup-bars", 120, "Replay warmup bars before first cycle")
		initialBalance  = flag.Float64("initial-balance", 100000, "Initial balance for simulated broker")
		candidateBatch  = flag.Int("candidate-batch-size", 20, "Candidate symbols analyzed per cycle")
		profilesRaw     = flag.String("profiles", "momentum_only:1.10:0.05,momentum_only:1.25:0.10,momentum_only:1.40:0.10,momentum_fallback:1.25:0.10", "Strategy profiles: strategy:minScore:positionPct,...")
		outputRoot      = flag.String("output-root", "output/ibkr_backtests", "Backtest output root directory")
		aiModel         = flag.String("ai-model", "deepseek", "AI model for AI-enabled profiles: deepseek|qwen|custom")
		deepseekKey     = flag.String("deepseek-key", os.Getenv("DEEPSEEK_KEY"), "DeepSeek API key (required for deepseek AI profiles)")
		qwenKey         = flag.String("qwen-key", os.Getenv("QWEN_KEY"), "Qwen API key (required for qwen AI profiles)")
		customAPIURL    = flag.String("custom-api-url", os.Getenv("CUSTOM_API_URL"), "Custom OpenAI-compatible API URL")
		customAPIKey    = flag.String("custom-api-key", os.Getenv("CUSTOM_API_KEY"), "Custom API key")
		customModelName = flag.String("custom-model-name", os.Getenv("CUSTOM_MODEL_NAME"), "Custom model name")
	)
	flag.Parse()

	if strings.TrimSpace(*accountID) == "" {
		log.Fatal("account-id is required")
	}
	if *maxCycles <= 0 {
		log.Fatal("max-cycles must be > 0")
	}
	if *warmupBars < 80 {
		*warmupBars = 80
	}
	if *candidateBatch <= 0 {
		*candidateBatch = 20
	}
	if *barLimit <= 0 {
		*barLimit = 500
	}

	symbols, err := resolveSymbols(*symbolsCSV, *symbolsFile)
	if err != nil {
		log.Fatalf("failed to load symbols: %v", err)
	}
	if len(symbols) == 0 {
		log.Fatal("no symbols resolved")
	}
	if *maxSymbols > 0 && len(symbols) > *maxSymbols {
		symbols = symbols[:*maxSymbols]
	}

	profiles, err := parseProfiles(*profilesRaw)
	if err != nil {
		log.Fatalf("failed to parse profiles: %v", err)
	}
	if len(profiles) == 0 {
		log.Fatal("no profiles to run")
	}

	runID := "run_" + time.Now().Format("20060102_150405")
	runRoot, err := filepath.Abs(filepath.Join(*outputRoot, runID))
	if err != nil {
		log.Fatalf("failed to resolve run root: %v", err)
	}
	dataDir := filepath.Join(runRoot, "csv")
	if strings.TrimSpace(*csvDataDir) != "" {
		*skipFetch = true
		dataDir, err = filepath.Abs(strings.TrimSpace(*csvDataDir))
		if err != nil {
			log.Fatalf("failed to resolve csv-data-dir: %v", err)
		}
	} else {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Fatalf("failed to create data dir: %v", err)
		}
	}

	availableSymbols := symbols
	if !*skipFetch {
		log.Printf("Downloading IBKR history for %d symbols (%s, limit=%d)...", len(symbols), *barInterval, *barLimit)
		downloaded, err := downloadHistory(*gatewayURL, *accountID, *sessionCookie, symbols, *barInterval, *barLimit, dataDir)
		if err != nil {
			log.Fatalf("history download failed: %v", err)
		}
		availableSymbols = downloaded
		if len(availableSymbols) == 0 {
			log.Fatal("no symbols were downloaded successfully")
		}
	} else {
		var existing []string
		for _, sym := range symbols {
			path := filepath.Join(dataDir, strings.ToUpper(sym)+".csv")
			if _, err := os.Stat(path); err == nil {
				existing = append(existing, strings.ToUpper(sym))
			}
		}
		availableSymbols = dedupeSymbols(existing)
		if len(availableSymbols) == 0 {
			log.Fatal("skip-fetch is set but no CSV files found for requested symbols")
		}
	}

	log.Printf("Using %d symbols for backtests", len(availableSymbols))
	pool.SetDefaultCoins(availableSymbols)
	pool.SetUseDefaultCoins(true, true)

	origWD, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to read working directory: %v", err)
	}
	defer os.Chdir(origWD)

	results := make([]profileResult, 0, len(profiles))
	for _, profile := range profiles {
		if profile.requiresAI() && !canRunAIProfile(*aiModel, *deepseekKey, *qwenKey, *customAPIURL, *customAPIKey, *customModelName) {
			log.Printf("Skipping profile %s: AI credentials not configured for ai-model=%s", profile.slug(), *aiModel)
			continue
		}

		profileSlug := profile.slug()
		profileDir := filepath.Join(runRoot, "profiles", profileSlug)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			log.Printf("Skipping %s: failed to create profile directory: %v", profileSlug, err)
			continue
		}

		if err := os.Chdir(profileDir); err != nil {
			log.Printf("Skipping %s: failed to enter profile directory: %v", profileSlug, err)
			continue
		}

		started := time.Now()
		atCfg := trader.AutoTraderConfig{
			ID:                "bt_" + profileSlug,
			Name:              "IBKR Backtest " + profileSlug,
			AIModel:           *aiModel,
			Exchange:          "ibkr",
			IBKRGatewayURL:    *gatewayURL,
			IBKRAccountID:     *accountID,
			IBKRSessionCookie: *sessionCookie,
			Mode:              "replay",
			DataProvider:      "csv",
			Broker:            "sim",
			CSVDataDir:        dataDir,
			InstrumentType:    "equity",
			CandidateBatchSize: func() int {
				if *candidateBatch > len(availableSymbols) {
					return len(availableSymbols)
				}
				return *candidateBatch
			}(),
			StrategyMode:        profile.StrategyMode,
			MomentumMinScore:    profile.MinScore,
			FallbackPositionPct: profile.PositionPct,
			InitialBalance:      *initialBalance,
			ScanInterval:        time.Second,
			MaxCycles:           *maxCycles,
			ReplayWarmupBars:    *warmupBars,
			DeepSeekKey:         *deepseekKey,
			QwenKey:             *qwenKey,
			UseQwen:             strings.EqualFold(*aiModel, "qwen"),
			CustomAPIURL:        *customAPIURL,
			CustomAPIKey:        *customAPIKey,
			CustomModelName:     *customModelName,
		}

		bt, err := trader.NewAutoTrader(atCfg)
		if err != nil {
			log.Printf("Skipping %s: trader init failed: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}

		log.Printf("Running profile %s ...", profileSlug)
		if err := bt.RunBacktest(*maxCycles); err != nil {
			log.Printf("Profile %s failed: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}
		finished := time.Now()

		summaryPath := filepath.Join(profileDir, "output", "replay_summary.json")
		summary, err := readReplaySummary(summaryPath)
		if err != nil {
			log.Printf("Profile %s failed to read replay summary: %v", profileSlug, err)
			_ = os.Chdir(origWD)
			continue
		}

		status := bt.GetStatus()
		cyclesExecuted := intFromAny(status["call_count"])

		relSummary, _ := filepath.Rel(runRoot, summaryPath)
		relProfile, _ := filepath.Rel(runRoot, profileDir)
		results = append(results, profileResult{
			ProfileSlug:      profileSlug,
			StrategyMode:     profile.StrategyMode,
			MinScore:         profile.MinScore,
			PositionPct:      profile.PositionPct,
			SymbolCount:      len(availableSymbols),
			CyclesExecuted:   cyclesExecuted,
			DurationSeconds:  finished.Sub(started).Seconds(),
			StartedAt:        started,
			FinishedAt:       finished,
			TotalTrades:      summary.TotalTrades,
			WinRatePct:       summary.WinRatePct,
			MaxDrawdownPct:   summary.MaxDrawdown,
			FinalEquity:      summary.FinalEquity,
			ReturnPct:        summary.ReturnPct,
			ReplaySummaryRel: filepath.ToSlash(relSummary),
			WorkDirRel:       filepath.ToSlash(relProfile),
		})

		_ = os.Chdir(origWD)
	}

	if len(results) == 0 {
		log.Fatal("no profile completed successfully")
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].ReturnPct == results[j].ReturnPct {
			return results[i].MaxDrawdownPct < results[j].MaxDrawdownPct
		}
		return results[i].ReturnPct > results[j].ReturnPct
	})

	if err := writeResultsJSON(filepath.Join(runRoot, "leaderboard.json"), results); err != nil {
		log.Printf("failed to write leaderboard.json: %v", err)
	}
	if err := writeResultsCSV(filepath.Join(runRoot, "leaderboard.csv"), results); err != nil {
		log.Printf("failed to write leaderboard.csv: %v", err)
	}

	log.Printf("Backtests completed. Results in %s", runRoot)
	log.Println("Top profiles:")
	for i, r := range results {
		if i >= 10 {
			break
		}
		log.Printf("  %d) %s | return=%.2f%% | maxDD=%.2f%% | winRate=%.2f%% | trades=%d | cycles=%d",
			i+1, r.ProfileSlug, r.ReturnPct, r.MaxDrawdownPct, r.WinRatePct, r.TotalTrades, r.CyclesExecuted)
	}
}

func canRunAIProfile(aiModel, deepseekKey, qwenKey, customURL, customKey, customModel string) bool {
	switch strings.ToLower(strings.TrimSpace(aiModel)) {
	case "qwen":
		return strings.TrimSpace(qwenKey) != ""
	case "custom":
		return strings.TrimSpace(customURL) != "" &&
			strings.TrimSpace(customKey) != "" &&
			strings.TrimSpace(customModel) != ""
	default:
		return strings.TrimSpace(deepseekKey) != ""
	}
}

func parseProfiles(raw string) ([]strategyProfile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("profiles cannot be empty")
	}

	specs := strings.Split(raw, ",")
	profiles := make([]strategyProfile, 0, len(specs))
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid profile '%s' (expected strategy:minScore:positionPct)", spec)
		}

		mode := strings.ToLower(strings.TrimSpace(parts[0]))
		switch mode {
		case "ai_only", "momentum_fallback", "momentum_only":
		default:
			return nil, fmt.Errorf("unsupported strategy mode '%s' in profile '%s'", mode, spec)
		}

		score, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid minScore in profile '%s': %w", spec, err)
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid positionPct in profile '%s': %w", spec, err)
		}

		profiles = append(profiles, strategyProfile{
			StrategyMode: mode,
			MinScore:     score,
			PositionPct:  pct,
		})
	}
	return profiles, nil
}

func resolveSymbols(symbolsCSV, symbolsFile string) ([]string, error) {
	if strings.TrimSpace(symbolsCSV) != "" {
		return dedupeSymbols(strings.Split(symbolsCSV, ",")), nil
	}
	data, err := os.ReadFile(symbolsFile)
	if err != nil {
		return nil, err
	}

	symbols := make([]string, 0, 1024)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		for _, token := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		}) {
			symbols = append(symbols, token)
		}
	}
	return dedupeSymbols(symbols), nil
}

func dedupeSymbols(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, raw := range items {
		symbol := strings.ToUpper(strings.TrimSpace(strings.Trim(raw, "\"'")))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func downloadHistory(gatewayURL, accountID, sessionCookie string, symbols []string, interval string, limit int, outDir string) ([]string, error) {
	client := broker.NewIBKRClient(gatewayURL, accountID, sessionCookie)
	provider := &market.IBKRProvider{Client: client}

	okSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		barsMap, err := provider.GetBars([]string{symbol}, interval, limit)
		if err != nil {
			log.Printf("  [%s] download failed: %v", symbol, err)
			continue
		}

		bars := barsMap[symbol]
		if len(bars) == 0 {
			log.Printf("  [%s] no bars returned", symbol)
			continue
		}

		if err := writeBarsCSV(filepath.Join(outDir, strings.ToUpper(symbol)+".csv"), bars); err != nil {
			log.Printf("  [%s] failed to write csv: %v", symbol, err)
			continue
		}

		okSymbols = append(okSymbols, strings.ToUpper(symbol))
		log.Printf("  [%s] wrote %d bars", symbol, len(bars))
	}

	return dedupeSymbols(okSymbols), nil
}

func writeBarsCSV(path string, bars []market.Kline) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		return err
	}
	for _, b := range bars {
		row := []string{
			strconv.FormatInt(b.OpenTime, 10),
			strconv.FormatFloat(b.Open, 'f', 8, 64),
			strconv.FormatFloat(b.High, 'f', 8, 64),
			strconv.FormatFloat(b.Low, 'f', 8, 64),
			strconv.FormatFloat(b.Close, 'f', 8, 64),
			strconv.FormatFloat(b.Volume, 'f', 8, 64),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func readReplaySummary(path string) (*replaySummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s replaySummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeResultsJSON(path string, results []profileResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeResultsCSV(path string, results []profileResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"rank",
		"profile_slug",
		"strategy_mode",
		"min_score",
		"position_pct",
		"symbol_count",
		"cycles_executed",
		"duration_seconds",
		"total_trades",
		"win_rate_pct",
		"max_drawdown_pct",
		"final_equity",
		"return_pct",
		"replay_summary_rel",
		"work_dir_rel",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for i, r := range results {
		row := []string{
			strconv.Itoa(i + 1),
			r.ProfileSlug,
			r.StrategyMode,
			fmt.Sprintf("%.4f", r.MinScore),
			fmt.Sprintf("%.4f", r.PositionPct),
			strconv.Itoa(r.SymbolCount),
			strconv.Itoa(r.CyclesExecuted),
			fmt.Sprintf("%.2f", r.DurationSeconds),
			strconv.Itoa(r.TotalTrades),
			fmt.Sprintf("%.2f", r.WinRatePct),
			fmt.Sprintf("%.2f", r.MaxDrawdownPct),
			fmt.Sprintf("%.2f", r.FinalEquity),
			fmt.Sprintf("%.2f", r.ReturnPct),
			r.ReplaySummaryRel,
			r.WorkDirRel,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func intFromAny(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
