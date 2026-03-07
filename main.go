package main

import (
	"fmt"
	"log"
	"aegistrade/api"
	"aegistrade/config"
	"aegistrade/manager"
	"aegistrade/pool"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {

	// Load configuration file
	configFile := "config.json"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	log.Printf(" Loading config file: %s", configFile)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf(" Failed to load config: %v", err)
	}

	log.Printf(" Config loaded successfully, %d traders participating", len(cfg.Traders))
	fmt.Println()

	//  Safety Check: Ensure CONFIRM_LIVE_TRADING=true if any trader is in live mode
	for _, traderCfg := range cfg.Traders {
		if traderCfg.Enabled && traderCfg.Mode == "live" {
			if os.Getenv("CONFIRM_LIVE_TRADING") != "true" {
				log.Fatalf(" SAFETY BLOCK: Trader '%s' is set to LIVE mode, but CONFIRM_LIVE_TRADING=true environment variable is missing. Aborting.", traderCfg.Name)
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
			cfg.CoinPoolAPIURL,
			cfg.MaxDailyLoss,
			cfg.MaxDrawdown,
			cfg.StopTradingMinutes,
			cfg.Leverage, // Pass leverage configuration
		)
		if err != nil {
			log.Fatalf(" Failed to initialize trader: %v", err)
		}
	}

	// Ensure at least one trader is enabled
	if enabledCount == 0 {
		log.Fatalf(" No traders enabled: please set at least one trader's enabled=true in config.json")
	}

	fmt.Println()
	fmt.Println(" Competition Participants:")
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
	fmt.Println(" AI Full Decision Mode:")
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
	fmt.Println("Press Ctrl+C to stop")
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
	fmt.Println(" Thank you for using the AI Trading Competition System!")
}
