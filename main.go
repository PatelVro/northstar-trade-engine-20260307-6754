package main

import (
	"flag"
	"fmt"
	"log"
	"northstar/api"
	"northstar/buildinfo"
	"northstar/config"
	"northstar/deployment"
	"northstar/manager"
	"northstar/pool"
	"northstar/startup"
	"northstar/trader"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var ensureLiveStartupValidation = func(configFile string, cfg *config.Config) (startup.LiveValidationStatus, error) {
	return startup.EnsureValidatedLiveStartup(configFile, cfg.Traders, time.Now())
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "validate-live" {
		return runValidateLiveCommand(args[1:])
	}

	flagSet := flag.NewFlagSet("northstar", flag.ContinueOnError)
	showVersion := flagSet.Bool("version", false, "print build information and exit")
	if err := flagSet.Parse(args); err != nil {
		return 2
	}

	info := buildinfo.Current()
	if *showVersion {
		fmt.Println("Northstar")
		fmt.Println(info.Summary())
		return 0
	}

	// Load configuration file
	configFile := "config.json"
	if flagSet.NArg() > 1 {
		log.Printf("expected at most one config file argument, got %d", flagSet.NArg())
		return 2
	}
	if flagSet.NArg() == 1 {
		configFile = flagSet.Arg(0)
	}

	log.Printf(" Northstar build: %s", info.Summary())
	log.Printf(" Loading config file: %s", configFile)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		log.Printf(" Failed to load config: %v", err)
		return 1
	}

	log.Printf(" Config loaded successfully, %d traders participating", len(cfg.Traders))
	fmt.Println()

	liveValidationStatus, err := ensureLiveStartupValidation(configFile, cfg)
	if err != nil {
		log.Printf(" LIVE START BLOCK: %v", err)
		return 1
	}
	if liveValidationStatus.Required {
		log.Printf(
			" Live deployment validation confirmed: source=%s checked_at=%s config=%s",
			liveValidationStatus.Source,
			liveValidationStatus.CheckedAt.Format(time.RFC3339),
			liveValidationStatus.ValidatedConfigFile,
		)
	}

	//  Safety Check: Ensure CONFIRM_LIVE_TRADING=true if any trader is in live mode
	for _, traderCfg := range cfg.Traders {
		if traderCfg.Enabled && traderCfg.Mode == "live" {
			if os.Getenv("CONFIRM_LIVE_TRADING") != "true" {
				log.Printf(" SAFETY BLOCK: Trader '%s' is set to LIVE mode, but CONFIRM_LIVE_TRADING=true environment variable is missing. Aborting.", traderCfg.Name)
				return 1
			}
		}
	}

	// Set default major coins list
	pool.SetDefaultCoins(cfg.DefaultCoins)

	// Determine if equity symbols should be used (if any enabled trader requires equities)
	useEquityPool := false
	for _, traderCfg := range cfg.Traders {
		if traderCfg.Enabled && traderCfg.InstrumentType == "equity" {
			useEquityPool = true
			break
		}
	}

	// Determine whether to use the default major coins list
	pool.SetUseDefaultCoins(cfg.UseDefaultCoins, useEquityPool)
	if cfg.UseDefaultCoins {
		listType := "Crypto"
		if useEquityPool {
			listType = "Equity"
		}
		log.Printf(" Default %s coins list enabled (%d ticker/coins)", listType, len(cfg.DefaultCoins))
	}

	// Set API URL for the coin pool
	if cfg.CoinPoolAPIURL != "" {
		pool.SetCoinPoolAPI(cfg.CoinPoolAPIURL)
		log.Printf(" AI500 coin pool API configured")
	}
	if cfg.OITopAPIURL != "" {
		pool.SetOITopAPI(cfg.OITopAPIURL)
		log.Printf(" OI Top API configured")
	}

	// Initialize TraderManager
	traderManager := manager.NewTraderManager()

	// Register all enabled traders
	enabledCount := 0
	for i, traderCfg := range cfg.Traders {
		// Skip disabled traders
		if !traderCfg.Enabled {
			log.Printf("  [%d/%d] Skipping disabled trader: %s", i+1, len(cfg.Traders), traderCfg.Name)
			continue
		}

		enabledCount++
		log.Printf(" [%d/%d] Initializing %s (%s model)...",
			i+1, len(cfg.Traders), traderCfg.Name, strings.ToUpper(traderCfg.AIModel))

		err := traderManager.AddTrader(
			traderCfg,
			cfg.DefaultCoins,
			cfg.DefaultCoinsFile,
			cfg.CoinPoolAPIURL,
			cfg.MaxDailyLoss,
			cfg.MaxDrawdown,
			cfg.StopTradingMinutes,
			cfg.Leverage, // Pass leverage configuration
		)
		if err != nil {
			log.Printf(" Failed to initialize trader: %v", err)
			return 1
		}
	}

	// Ensure at least one trader is enabled
	if enabledCount == 0 {
		log.Printf(" No traders enabled: please set at least one trader's enabled=true in config.json")
		return 1
	}

	fmt.Println()
	fmt.Println(" Northstar Active Traders:")
	for _, traderCfg := range cfg.Traders {
		// Display only enabled traders
		if !traderCfg.Enabled {
			continue
		}
		currency := "USDT"
		if traderCfg.Exchange == "ibkr" || traderCfg.Exchange == "alpaca" {
			currency = "$"
		}
		fmt.Printf("   %s (%s) - Initial Balance: %.0f %s\n",
			traderCfg.Name, strings.ToUpper(traderCfg.AIModel), traderCfg.InitialBalance, currency)
	}

	fmt.Println()
	fmt.Println(" Northstar Execution Overview:")
	if !useEquityPool {
		fmt.Printf("   AI will autonomously decide leverage for each trade (up to %dx for altcoins, %dx for BTC/ETH)\n",
			cfg.Leverage.AltcoinLeverage, cfg.Leverage.BTCETHLeverage)
	} else {
		fmt.Println("   AI will autonomously decide margin and leverage for each equity trade")
	}
	fmt.Println("   AI will autonomously decide position size for each trade")
	fmt.Println("   AI will autonomously set stop-loss and take-profit prices")
	fmt.Println("   AI will make comprehensive analysis based on market data, technical indicators, and account status")
	fmt.Println()
	fmt.Println("  Risk Warning: Automated AI trading carries risk, test with small amounts first!")
	fmt.Println()
	fmt.Println("Northstar is running. Press Ctrl+C to stop")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// Create and start API server
	apiServer := api.NewServer(traderManager, cfg.APIServerPort)
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Printf(" API server error: %v", err)
		}
	}()

	// Configure graceful shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start all traders
	traderManager.StartAll()

	// Wait for exit signal
	<-sigChan
	fmt.Println()
	fmt.Println()
	log.Println(" Received exit signal, stopping all traders...")
	traderManager.StopAll()

	fmt.Println()
	fmt.Println(" Northstar shutdown complete.")
	return 0
}

func runValidateLiveCommand(args []string) int {
	flagSet := flag.NewFlagSet("validate-live", flag.ContinueOnError)
	configFile := flagSet.String("config", "config.json", "path to the config file to validate")
	if err := flagSet.Parse(args); err != nil {
		return 2
	}
	if flagSet.NArg() > 1 {
		log.Printf("validate-live accepts at most one positional config file argument, got %d", flagSet.NArg())
		return 2
	}
	if flagSet.NArg() == 1 {
		*configFile = flagSet.Arg(0)
	}

	info := buildinfo.Current()
	fmt.Println("Northstar live deployment validation")
	fmt.Println(info.Summary())
	fmt.Printf("config=%s\n", *configFile)

	summary := deployment.NewValidator().ValidateLiveConfig(*configFile)
	fmt.Printf("overall=%s live_ready=%t checked_at=%s\n", summary.Status, summary.LiveReady, summary.CheckedAt.Format("2006-01-02T15:04:05Z07:00"))
	if summary.RepositoryRoot != "" {
		fmt.Printf("repository_root=%s\n", summary.RepositoryRoot)
	}
	fmt.Println()
	for _, check := range summary.Checks {
		fmt.Printf("[%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Message)
	}
	if len(summary.TraderValidations) > 0 {
		fmt.Println()
		for _, traderSummary := range summary.TraderValidations {
			fmt.Printf("trader=%s (%s) status=%s live_trading_allowed=%t\n", traderSummary.TraderName, traderSummary.TraderID, traderSummary.Status, traderSummary.LiveTradingAllowed)
			fmt.Printf("  readiness: %s\n", traderSummary.Readiness.Message)
			for _, check := range traderSummary.Readiness.Checks {
				if check.Status == trader.ReadinessPass {
					continue
				}
				fmt.Printf("    - [%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Message)
			}
			if traderSummary.Promotion.Required {
				fmt.Printf("  promotion: %s\n", traderSummary.Promotion.Message)
				for _, check := range traderSummary.Promotion.Checks {
					if check.Status == trader.PromotionPass {
						continue
					}
					fmt.Printf("    - [%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Message)
				}
			}
		}
	}
	fmt.Println()
	if summary.LiveReady {
		fmt.Println("live deployment validation passed")
		return 0
	}
	fmt.Println("live deployment validation failed")
	return 1
}
