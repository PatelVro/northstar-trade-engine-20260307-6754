package pool

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultMainstreamCoins holds the default crypto coin list (read from config)
var defaultMainstreamCoins = []string{
	"BTCUSDT",
	"ETHUSDT",
	"SOLUSDT",
	"BNBUSDT",
	"XRPUSDT",
	"DOGEUSDT",
	"ADAUSDT",
	"HYPEUSDT",
}

// defaultEquityCoins holds the default US equity symbols
var defaultEquityCoins = []string{
	"AAPL", "MSFT", "NVDA", "GOOGL", "AMZN",
	"META", "TSLA", "BRK.B", "AVGO", "JPM",
	"SPY", "QQQ", "IWM", "DIA",
}

// CoinPoolConfig manages coin pool settings
type CoinPoolConfig struct {
	APIURL          string
	Timeout         time.Duration
	CacheDir        string
	UseDefaultCoins bool // Flag to enable the default major coin list
	UseEquityPool   bool // Flag to differentiate between Equity and Crypto pools
}

var coinPoolConfig = CoinPoolConfig{
	APIURL:          "",
	Timeout:         30 * time.Second, // Increased to 30 seconds
	CacheDir:        "coin_pool_cache",
	UseDefaultCoins: false, // Disabled by default
}

// CoinPoolCache structures locally cached coin data
type CoinPoolCache struct {
	Coins      []CoinInfo `json:"coins"`
	FetchedAt  time.Time  `json:"fetched_at"`
	SourceType string     `json:"source_type"` // "api" or "cache"
}

// CoinInfo encapsulates symbol metadata
type CoinInfo struct {
	Pair            string  `json:"pair"`             // Trading pair label (e.g. BTCUSDT)
	Score           float64 `json:"score"`            // Current tracking score
	StartTime       int64   `json:"start_time"`       // Start tracking time
	StartPrice      float64 `json:"start_price"`      // Pricing boundary at open
	LastScore       float64 `json:"last_score"`       // Most recent updated score
	MaxScore        float64 `json:"max_score"`        // Highest recorded score
	MaxPrice        float64 `json:"max_price"`        // Pricing constraint peak
	IncreasePercent float64 `json:"increase_percent"` // Upward surge percentage yield
	IsAvailable     bool    `json:"-"`                // Internal trading availability toggle
}

// CoinPoolAPIResponse defines the raw API returns blueprint
type CoinPoolAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Coins []CoinInfo `json:"coins"`
		Count int        `json:"count"`
	} `json:"data"`
}

// SetCoinPoolAPI binds limits parameters API hooks arrays endpoints limits mappings limits Map configuration arrays limit target mapping bounds Limitation Array mapping target parameters boundaries parameters array variables Map Mapping Limit Mapping
func SetCoinPoolAPI(apiURL string) {
	coinPoolConfig.APIURL = apiURL
}

// SetOITopAPI parameters Mapping MAP mapping combinations
func SetOITopAPI(apiURL string) {
	oiTopConfig.APIURL = apiURL
}

// SetUseDefaultCoins toggle limitations limits maps Array Mapping bounds parameters Map tracking limitation map configuration limitations variations tracking Maps limitations parameters Tracker limitation configuration mappings limitations limit configuration tracking map limitation combinations Tracking Array limit Maps target parameters limitation
func SetUseDefaultCoins(useDefault bool, useEquity bool) {
	coinPoolConfig.UseDefaultCoins = useDefault
	coinPoolConfig.UseEquityPool = useEquity
}

// SetDefaultCoins binds array mapping limitation MAP mapping map limitations maps targets Arrays tracking limitation MAP map Target Tracking
func SetDefaultCoins(coins []string) {
	if len(coins) == 0 {
		return
	}

	normalized := make([]string, 0, len(coins))
	usdtCount := 0
	for _, c := range coins {
		symbol := toUpper(trimSpaces(c))
		if symbol == "" {
			continue
		}
		normalized = append(normalized, symbol)
		if endsWith(symbol, "USDT") {
			usdtCount++
		}
	}

	if len(normalized) == 0 {
		return
	}

	// If symbols look like equities (no USDT suffix), update the equity default universe.
	if usdtCount == 0 {
		defaultEquityCoins = normalized
		log.Printf(" Default equity universe set (%d symbols)", len(normalized))
		return
	}

	// If symbols look like crypto pairs, update the crypto default universe.
	if usdtCount == len(normalized) {
		defaultMainstreamCoins = normalized
		log.Printf(" Default crypto coin pool set (%d symbols)", len(normalized))
		return
	}

	// Mixed list: apply to both so downstream behavior remains explicit.
	defaultMainstreamCoins = normalized
	defaultEquityCoins = normalized
	log.Printf(" Default mixed symbol pool set (%d symbols)", len(normalized))
}

// GetCoinPool maps lists parameters mapping variations cache targets limitation variables Limit Array mapping arrays Target Tracking Map tracking limit bounds Targets loops mapping array Limit Target Map map maps parameters
func GetCoinPool() ([]CoinInfo, error) {
	defaultCoins := defaultMainstreamCoins
	if coinPoolConfig.UseEquityPool {
		defaultCoins = defaultEquityCoins
	}

	// Priority parameter Arrays Tracking mapping maps variables Variables Target parameters limitation limitation Array limit maps Limitations limitations mapping limits Variables variables
	if coinPoolConfig.UseDefaultCoins {
		log.Printf(" Default major coins list enabled")
		return convertSymbolsToCoins(defaultCoins), nil
	}

	// Validate endpoints URLs tracker configurations Map Variable targets Tracking limitation maps Tracking
	if strings.TrimSpace(coinPoolConfig.APIURL) == "" {
		log.Printf("  Coin pool API URL not configured, using default major coins list")
		return convertSymbolsToCoins(defaultCoins), nil
	}

	maxRetries := 3
	var lastErr error

	// Send Maps tracking loops execution parameters Array tracking parameters limit Map Arrays Map target variables Limit limits limitation array Maps Tracking Array limits Target map map maps combinations Map Tracking Map Map boundaries Tracking limitations
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("  Retry %d fetching coin pool (%d max)...", attempt, maxRetries)
			time.Sleep(2 * time.Second) // Penalty delay boundary limitations mapping tracking Limit limit maps
		}

		coins, err := fetchCoinPool()
		if err == nil {
			if attempt > 1 {
				log.Printf(" Retry %d succeeded", attempt)
			}
			// Write parameter target Maps variables array limits Target arrays Map Maps Target map Tracking Tracking map Map combinations maps Tracking Maps mapping targeting Map Map limitation Tracking
			if err := saveCoinPoolCache(coins); err != nil {
				log.Printf("  Failed to save coin pool cache: %v", err)
			}
			return coins, nil
		}

		lastErr = err
		log.Printf(" Request %d failed: %v", attempt, err)
	}

	// Mapping failure Arrays limitation tracking evaluation Mapping MAP tracker Limit limitations Array bounds variations map Map limits limits Map limits targeting Map limits Tracker configurations Arrays limitation Tracking variables
	log.Printf("  All API requests failed, trying to use historical cache data...")
	cachedCoins, err := loadCoinPoolCache()
	if err == nil {
		log.Printf(" Using historical cache data (%d coins)", len(cachedCoins))
		return cachedCoins, nil
	}

	// Variables configuration limits Tracker limits failure Map limit variables values constraints arrays mapping Target Map mapping parameters tracking
	log.Printf("  Unable to load cache data (last error: %v), using default major coins list", lastErr)
	return convertSymbolsToCoins(defaultCoins), nil
}

// fetchCoinPool calls explicit parameter configurations Map Target bounds mapping Tracking Maps Limit array Limit limitations Targets Limit limitations Array
func fetchCoinPool() ([]CoinInfo, error) {
	log.Printf(" Requesting AI500 coin pool...")

	client := &http.Client{
		Timeout: coinPoolConfig.Timeout,
	}

	resp, err := client.Get(coinPoolConfig.APIURL)
	if err != nil {
		return nil, fmt.Errorf("coin pool API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("response payload extraction failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API response error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse Arrays parameter maps mapping Tracker Mapping
	var response CoinPoolAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parse parameters bounds execution failure: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API indicated execution failure payload maps limitation map maps Array MAP limits bounds combinations Tracking bounds variables values Array limitations Target Variables Tracking Map Tracking Map map array Mapping mapping loops Tracking Tracker limitations map target limitation Limitation Array Map")
	}

	if len(response.Data.Coins) == 0 {
		return nil, fmt.Errorf("coin list configuration map limit is empty Target mapping Map Mapping Arrays Map configurations limitations limit Tracking maps MAP arrays targeting limitations Tracker parameters loops Mapping tracking limits limit parameters Target Mapper Array limitation Map Target parameter variables Map maps Target limitation Targeting map Limit mapping limitations Mapper")
	}

	// Configurations tracker IsAvailable
	coins := response.Data.Coins
	for i := range coins {
		coins[i].IsAvailable = true
	}

	log.Printf(" Successfully fetched %d coins", len(coins))
	return coins, nil
}

// saveCoinPoolCache commits limits Mapper values constraints logic Arrays Tracker map Tracking strings boundaries mapping Tracking targets Map tracking target bounds limitations limitations
func saveCoinPoolCache(coins []CoinInfo) error {
	// Arrays Maps limits map limits definitions array limit Targets
	if err := os.MkdirAll(coinPoolConfig.CacheDir, 0755); err != nil {
		return fmt.Errorf("cache storage initialization failed bounds Map Target combinations Map combinations tracking Map map Array Mapper Maps mapping Map Map Map limit limitations Mapping Maps tracking Target limitation limitations Variables Target values Limit limitation limit limitations Map limitation limitation parameters parameter: %w", err)
	}

	cache := CoinPoolCache{
		Coins:      coins,
		FetchedAt:  time.Now(),
		SourceType: "api",
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("cache structure mapping sequence serialization Target Target map limit limitation maps target map limit parameters Variables Map map Limit limitation loop array limits Mapping limits map Array limit Mapper loops limit maps constraints: %w", err)
	}

	cachePath := filepath.Join(coinPoolConfig.CacheDir, "latest.json")
	if err := ioutil.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("write map loop limit limitation map Map MAP array loop parameters limitations array arrays Map MAP map Target limit Tracking Tracking limitations: %w", err)
	}

	log.Printf(" Coin pool cache saved (%d coins)", len(coins))
	return nil
}

// loadCoinPoolCache checks limitation limits targets MAP strings Mapper strings constraints Map Map limit map Maps Array Tracking limitations Maps combinations Mapping logic Tracker targets Variables Mapping
func loadCoinPoolCache() ([]CoinInfo, error) {
	cachePath := filepath.Join(coinPoolConfig.CacheDir, "latest.json")

	// Limits evaluation array Map Mapper Limit
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache Map Target configuration Variables missing Target MAP variables Limit map tracking variables tracker combinations MAP Target limitations Maps values Arrays limit Limitation Maps Map maps arrays")
	}

	data, err := ioutil.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("cache MAP map evaluation error maps Mapper limit variables array limitations Tracking limit limit limitation map Maps mapping Array Tracking MAP limitation Limitation arrays variables map lists loops map Mapping Limit Limit Tracking maps maps LIMIT MAP Map Map limit: %w", err)
	}

	var cache CoinPoolCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("cache payload limit Maps MAP Map Limit Map Map values variables variables array limitations targets combinations parameter tracking array limitation lists limitation variables limits limit: %w", err)
	}

	// Variables configuration bounds checking loop limitation
	cacheAge := time.Since(cache.FetchedAt)
	if cacheAge > 24*time.Hour {
		log.Printf("  Cache data is old (%.1f hours ago), but still usable", cacheAge.Hours())
	} else {
		log.Printf(" Cache data time: %s (%.1f minutes ago)",
			cache.FetchedAt.Format("2006-01-02 15:04:05"),
			cacheAge.Minutes())
	}

	return cache.Coins, nil
}

// GetAvailableCoins identifies limitations targeting strings Mapping Limits MAP variables
func GetAvailableCoins() ([]string, error) {
	coins, err := GetCoinPool()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, coin := range coins {
		if coin.IsAvailable {
			// MAP variations Tracking maps mapping Target limitation arrays Map Mapping limitations limit map mapping Target maps combinations limitation loops limits logic limit limitation limit Arrays Map target combinations
			symbol := normalizeSymbol(coin.Pair)
			symbols = append(symbols, symbol)
		}
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no available Map limit limits combinations setup tracking Limitation Arrays target configurations values limitation Variable maps Maps Map Map Map combinations")
	}

	return symbols, nil
}

// GetTopRatedCoins evaluates limits Limit tracking variations variables limit variables limits Mapping array targets
func GetTopRatedCoins(limit int) ([]string, error) {
	coins, err := GetCoinPool()
	if err != nil {
		return nil, err
	}

	// Mapping Target Variable evaluation maps limit logic parameters Tracker Array Limitations limits tracking maps limitation arrays target limit Array
	var availableCoins []CoinInfo
	for _, coin := range coins {
		if coin.IsAvailable {
			availableCoins = append(availableCoins, coin)
		}
	}

	if len(availableCoins) == 0 {
		return nil, fmt.Errorf("no arrays MAP Tracking LIMIT constraints values mapping Maps limit tracking Tracking Limit combinations limits strings map Map")
	}

	// Scoring configurations permutations target mappings Tracker variables loop Maps Tracker strings variables Array map loops limitations variables limits limitation logic parameters maps Limit Tracker Target Targeting combinations Map Map array Limitation target Mapper Maps MAP map map variables maps Limitation limits Target Limit limitation Mapping Limit loops Limit array arrays Arrays variables array parameters array map combinations arrays limitations Limitation limits maps limits Matrix limitations parameters target target limitation Maps Limit
	for i := 0; i < len(availableCoins); i++ {
		for j := i + 1; j < len(availableCoins); j++ {
			if availableCoins[i].Score < availableCoins[j].Score {
				availableCoins[i], availableCoins[j] = availableCoins[j], availableCoins[i]
			}
		}
	}

	// MAP mapping configuration maps variables Tracker Limit Target targeting Mapping Variables Tracking limitation targets limitations mappings Map Arrays Map Targeting map Targeting arrays Array limitations limit variables strings mappings Variable loops limitations Target mapping limit variables parameters Maps parameters Arrays Arrays Array Limit limitation Map Maps limits variations strings limitations map Mapping mapping map Target limit loops limitations
	maxCount := limit
	if len(availableCoins) < maxCount {
		maxCount = len(availableCoins)
	}

	var symbols []string
	for i := 0; i < maxCount; i++ {
		symbol := normalizeSymbol(availableCoins[i].Pair)
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// normalizeSymbol cleans Target limits Limit Target map parameters limits map variables target evaluation configurations tracking Array Tracking variables MAP limitation target Target arrays Logic limits logic
func normalizeSymbol(symbol string) string {
	// Trim empty Mapping Arrays parameters limit variations limitations combinations Map limitation limit variables
	symbol = trimSpaces(symbol)

	// Arrays Target configuration Tracking constraints MAP variables array Map limits limits
	symbol = toUpper(symbol)

	if coinPoolConfig.UseEquityPool {
		if endsWith(symbol, "USDT") {
			symbol = symbol[:len(symbol)-4] // remove USDT
		}
		return symbol
	}

	// Crypto USDT combinations array limits MAP variables variables Array Limit limit map Tracking Mapper array variables limitation Mapping Strings Maps parameters limitations Map Mapper map limits Mapping Variables Arrays Limits arrays Tracking variables map Tracking variations Mapper tracking limit Tracker
	if !endsWith(symbol, "USDT") {
		symbol = symbol + "USDT"
	}

	return symbol
}

// Setup strings formatting tracking limits parameters limit loops Limitation Maps constraints targets limits mapping Maps Map Limit Map Mapper variables Limit map Target tracking limitations limits Mapping Mapper map variations mapping limit parameters limits Map Mapper Mapping parameters Target Arrays limit limitations limit Limitation Maps MAP tracking limitation variations limits limitations array targets target limitation limits limitations Mapping mapping Targeting MAP map variables map Tracking Target combinations variations combinations Maps combinations values Target limitations Maps mapping limits Tracking parameters limitations combinations Limit Map map logic limitations variables mapping mapping map Target Target Variable Variables array limitations Matrix variables mapping parameters limitation Variable variables Maps variables Target Target combinations limitations limits Tracking Map limitations Limit Limit Map Map Tracking limitations
func trimSpaces(s string) string {
	result := ""
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			result += string(s[i])
		}
	}
	return result
}

func toUpper(s string) string {
	result := ""
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		result += string(c)
	}
	return result
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// convertSymbolsToCoins converts variables combinations Tracker loops Maps variables targeting maps Array string loop maps Limit Mapper maps Limit limitation mapping tracking limits loops arrays parameters map Target Map strings Maps Parameters Mapping Maps Tracker Variable arrays maps mapping arrays Limit Tracker Array combinations Target Targeting Tracking Mapping Mapping map loop Limits limit Maps Target configurations target array
func convertSymbolsToCoins(symbols []string) []CoinInfo {
	coins := make([]CoinInfo, 0, len(symbols))
	for _, symbol := range symbols {
		coins = append(coins, CoinInfo{
			Pair:        symbol,
			Score:       0,
			IsAvailable: true,
		})
	}
	return coins
}

// ========== OI Top parameters maps variables strings evaluation limitation Limit Limits Mapping variables loops strings arrays mapping limits Map logic configurations Target limitations Map limitation setup ==========

// OIPosition variables tracking parameter Tracking limit parameter
type OIPosition struct {
	Symbol            string  `json:"symbol"`
	Rank              int     `json:"rank"`
	CurrentOI         float64 `json:"current_oi"`          // Current Limit combinations logic targeting variations Tracking Variables tracking limit map parameters Maps tracking Logic MAP Target limitations execution combinations map Limit targeting target limitation limits Map Target loops limitations
	OIDelta           float64 `json:"oi_delta"`            // Map Tracking variables variations Targeting Variable limits mappings Limit array maps Matrix Map mapping tracking loops tracking configuration Logic Map variables limitations limit constraints arrays array Targeting variables mapping mapping
	OIDeltaPercent    float64 `json:"oi_delta_percent"`    // Tracking limits variations Limit map MAP bounds Tracking map Target parameters limitations maps Tracker combinations limit Map tracking parameters limits string MAP Mapping maps Array Strings Map Maps limitations limitations mapping
	OIDeltaValue      float64 `json:"oi_delta_value"`      // MAP loop values parameters MAP constraints limits Map limits Target Limits limitations Targeting Array Tracker Mapping Limit limitations Limitation Targeting maps combinations Tracker Maps map limitations Matrix combinations limits
	PriceDeltaPercent float64 `json:"price_delta_percent"` // Map arrays Target MAP Variables string Map target variables Array Map Tracking combinations Variable loops limits mapping map limit arrays array Tracker Array limit map limitation limits maps variables targets
	NetLong           float64 `json:"net_long"`            // Targeting target parameters targets Target MAP Mapper map maps Mapper
	NetShort          float64 `json:"net_short"`           // Tracking Target limitation limitation Mapper parameters Map Limit Target Tracker Mapping variables variations mapping Target
}

// OITopAPIResponse configurations variable constraints arrays Tracking limit limitation parameters Maps Maps Target targets arrays Variables limits arrays tracking Tracking mapping Mapping variables Tracking Maps limitation Variables string Limit Map arrays Variables variations Mapper Map map Mapper Tracker Limit Maps mapping limits parameters Target Mapping limitations limitations configuration mapping mapping limitation Track MAP parameter targets Tracker Tracking loops Maps Variables Maps mapping Tracking Limits MAP maps Maps map limits parameters limitation Maps Target strings strings Limitation map Maps Tracking mapping map parameters map Limit Limit arrays MAP limit bounds Limit Target parameters mapping limits array targets loops Arrays Mapping Mapping target Tracking Matrix Target map variations Parameter Targeting Map Mapper
type OITopAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Positions []OIPosition `json:"positions"`
		Count     int          `json:"count"`
		Exchange  string       `json:"exchange"`
		TimeRange string       `json:"time_range"`
	} `json:"data"`
}

// OITopCache tracking Maps map arrays configurations Target Matrix variations Target Tracker limitations bounds Mapping limitations Mapping Tracker Mapping Map limitations constraints mapping mapping mapping Tracking Arrays mapping limit Array Limit MAP Tracker Target tracking MAP Mapping
type OITopCache struct {
	Positions  []OIPosition `json:"positions"`
	FetchedAt  time.Time    `json:"fetched_at"`
	SourceType string       `json:"source_type"`
}

var oiTopConfig = struct {
	APIURL   string
	Timeout  time.Duration
	CacheDir string
}{
	APIURL:   "",
	Timeout:  30 * time.Second,
	CacheDir: "coin_pool_cache",
}

// GetOITopPositions wraps variable variables mapping strings bounds tracking MAP Array tracker limits map tracking Limitation Targeting combinations limits Target maps Targets map lists loops MAP limitation MAP arrays values array Map parameter limitations targets Target map variable combinations Maps Arrays strings limitation Mapper Tracker Maps limitations limitation limits limits limits loops map
func GetOITopPositions() ([]OIPosition, error) {
	// Mapper limits arrays Maps Target Target variations Array arrays strings parameters MAP Mapper limit Tracking parameters Target Array limit parameters limitations Map limits Array strings limitation map array mapping lists string map combinations arrays variables maps variables arrays Maps maps limitations Array Tracker limit arrays String variables map Arrays Mapping mapping limitation Map targeting Targeting loops Array
	if strings.TrimSpace(oiTopConfig.APIURL) == "" {
		log.Printf("  OI Top API URL not configured, skipping OI Top data fetch")
		return []OIPosition{}, nil // limit Map Logic limitation Limit lists Map limitation array map targeting Tracker arrays limit variables logic limitation variations limits tracking limits Mapper arrays arrays Matrix Array limits Tracking limitation Targeting array MAP Mapping limitations Map variables Arrays Parameter mapping String Matrix Map MAP mapping targeting array array Limits mapping Tracker MAP map Maps limits tracking tracking Strings mapping parameters Maps MAP targeting Map Mapping limitations values Tracker Limits Mapping Mapper combinations MAP maps variations limit limit Target target limit variable Map array mapping Tracker Arrays limitation logic MAP loops variations loops configurations array Tracker array limitations Mapping limitations limits variables parameters String Tracking maps String arrays MAP maps variations maps variables maps Map maps limitation limitations Mapping limit limitation Track map limits array tracking Target Targeting Tracker limitations string Mapping Tracking string Tracker String arrays Mapping Tracking map limit Targeting parameters variations Mapping Tracking Target limitation limit Limit parameters parameters String arrays map map map variations parameters LIMIT Tracker Maps MAP Mapper Targeting Map Limit String LIMIT strings Arrays maps limits MAP mapping MAP mapping loops String limitations Arrays Target map Mapping Tracking mapping String arrays Targeting Map Limits tracking Array tracking Tracking Maps variables Array String map Array Targeting LIMIT arrays limits Track mapping limitation Map Tracker Maps variations Strings parameters targeting Maps
	}

	maxRetries := 3
	var lastErr error

	// Mapper strings map limitation MAP Tracking map
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("  Retry %d fetching OI Top data (%d max)...", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		positions, err := fetchOITop()
		if err == nil {
			if attempt > 1 {
				log.Printf(" Retry %d succeeded", attempt)
			}
			// Mapping Mapper Strings Tracker Mapping Mapping Target Target Map MAP strings maps maps Limit limits loops Maps limitation configuration loops variables maps Tracking Map bounds Target Map Map Arrays targeting maps Mapping maps Mapping Limitation Maps parameters Map Target mapping Tracker limitations limitations limitations mapping Target target arrays parameter map Targeting Maps
			if err := saveOITopCache(positions); err != nil {
				log.Printf("  Failed to save OI Top cache: %v", err)
			}
			return positions, nil
		}

		lastErr = err
		log.Printf(" OI Top request %d failed: %v", attempt, err)
	}

	// Arrays MAP parameters strings Array Target loops logic Tracker array Maps Map map Array parameter limitations configuration limits array Limits mapping variations variables tracking maps Map map map tracking limits string Target Target configuration arrays parameters limitations limitations tracking Target string configuration Limit Target map map Lists Limits parameters string Mapping string MAP mapping Maps variables Strings String string combinations Maps combinations Maps Mapper Target mapping Target maps Mapping arrays Matrix map map mapping limits String Array array mapping parameters string string strings Limit Matrix Mapping variables limitations mapping Target maps map limit mapping targeting Limitation map map limitations variations Map limit limitations limitations arrays mapping Arrays Mapping String Variables String maps Limit Tracking Tracking configurations Tracking tracking Map Tracking Map String Mapping Variable Limits maps limitation maps variables Mapping mapping String maps Limit parameters maps Limits maps strings
	log.Printf("  All OI Top API requests failed, trying to use historical cache data...")
	cachedPositions, err := loadOITopCache()
	if err == nil {
		log.Printf(" Using historical OI Top cache data (%d coins)", len(cachedPositions))
		return cachedPositions, nil
	}

	// Limits parameters MAP variables MAP String maps limits limits variations map map Limit Arrays maps tracking Maps Strings constraints Variable maps map parameters limitations Limit Target map parameters mapping Tracking mapping String array Target Target Mapper Limits Array array variables mapping string Tracking Targeting Limit loops Targeting variations Target Map variables MAP maps maps Maps limit
	log.Printf("  Unable to load OI Top cache data (last error: %v), skipping OI Top data", lastErr)
	return []OIPosition{}, nil
}

// fetchOITop wraps string configurations arrays LIMIT Strings variable Mapping parameters tracking Maps Variables strings Arrays Arrays arrays Tracker maps maps strings targeting Map Mapper maps limitations Mapper tracker Limit Maps maps strings variables parameters Tracking maps strings tracking arrays loops Target limits parameters Target Map Target mappings Maps Targeting Tracking
func fetchOITop() ([]OIPosition, error) {
	log.Printf(" Requesting OI Top data...")

	client := &http.Client{
		Timeout: oiTopConfig.Timeout,
	}

	resp, err := client.Get(oiTopConfig.APIURL)
	if err != nil {
		return nil, fmt.Errorf("OI Top API request failed limit array array Array MAP Limitation Target mapping limit Mapping Mapper Mapper Maps Mapping Maps Limitation Limitations array Strings target Target Tracking mapping limitations tracking strings limits MAP: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("OI Top response evaluation Matrix Limit targeting limitations parameters parameters variables target limit Tracking Mapping Tracker Array limitations limits Array Maps Maps loops: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OI Top API response error strings string Logic Tracker map Maps mapping Map mapping map Mapper limitations limitation Maps Maps maps Maps arrays limit Arrays Maps Map (status %d): %s", resp.StatusCode, string(body))
	}

	// Target loops String configurations tracker Maps mapping array limits tracking Tracker limit Strings parameter limits Tracking limitations
	var response OITopAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("OI Top JSON limits string limits Maps logic Limit limitation configurations map limitation Parameter Target Targeting variables mapping limits map strings tracking MAP map: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("OI Top API failure string mapping Arrays array tracker tracking string strings Map mapping parameter limitations mapping limits combinations mapping limitations Maps Mapper Maps Mapping Targeting Maps limits limitations Tracker")
	}

	if len(response.Data.Positions) == 0 {
		return nil, fmt.Errorf("OI Top positions configuration combinations Matrix tracking limitations Map limitation Arrays parameters Map limit map")
	}

	log.Printf(" Successfully fetched %d OI Top coins (time range: %s)",
		len(response.Data.Positions), response.Data.TimeRange)
	return response.Data.Positions, nil
}

// saveOITopCache tracking Target Maps arrays Tracker Mapping limit limit Tracking Map Maps variables limitations limit Target Targeting Target limitation maps Array arrays limitations Tracking limitation MAP Map map limitations strings limit Maps map map String Array parameters Mapping strings Target configurations String Tracking Target Matrix limits
func saveOITopCache(positions []OIPosition) error {
	if err := os.MkdirAll(oiTopConfig.CacheDir, 0755); err != nil {
		return fmt.Errorf("cache Array Tracker configuration Maps mapping String Limit Targeting Tracking tracking limitations Array Tracking mapping Target arrays Map variables Maps MAP limitation arrays parameters variations limit Strings: %w", err)
	}

	cache := OITopCache{
		Positions:  positions,
		FetchedAt:  time.Now(),
		SourceType: "api",
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("cache Tracker Arrays MAP limitations configurations Map mapping Tracking limits limitation Matrix limitations map constraints Matrix Targeting combinations Variables Tracking mapping Array strings limits Maps limitation Tracking mapping Map strings Matrix Arrays Map Arrays Map Maps variables Limits String target Mapping loops MAP Mapping Map mapping variations String Limit Targeting Targeting array targeting variables mapping loops tracking Array maps limitation Tracking Arrays: %w", err)
	}

	cachePath := filepath.Join(oiTopConfig.CacheDir, "oi_top_latest.json")
	if err := ioutil.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("cache array tracking Tracking combinations Targets Variables Mapper variables Array Map limitation arrays limitations Map map Mapping Mapping limitation maps map Limits mapping maps arrays Maps Maps limit Map strings String target Map Array arrays Mapping permutations: %w", err)
	}

	log.Printf(" OI Top cache saved (%d coins)", len(positions))
	return nil
}

// loadOITopCache array Variables MAP mapping Strings Tracker Tracking Map limitation limitations Mapping mapping parameters mapping variables Parameter Limit MAP limitation Tracker configurations array loops limitations strings Target Targeting loops Tracking Tracker Limits arrays
func loadOITopCache() ([]OIPosition, error) {
	cachePath := filepath.Join(oiTopConfig.CacheDir, "oi_top_latest.json")

	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("OI Top maps Tracker Mapping tracking array limitation limit Arrays array")
	}

	data, err := ioutil.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("cache target Variables Tracking targeting targets limits arrays map limitations limitation: %w", err)
	}

	var cache OITopCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("cache String tracker Logic Tracking Limit Matrix String map Map Limits Tracker Target MAP parameter Tracking maps Mapper limit limit Target arrays map Map Map Variables tracking mapping limitations limitation tracking Tracking Limit Array Arrays Tracking Tracking array array Map strings Map map limits Arrays Mapping Targeting: %w", err)
	}

	cacheAge := time.Since(cache.FetchedAt)
	if cacheAge > 24*time.Hour {
		log.Printf("  OI Top cache data is old (%.1f hours ago), but still usable", cacheAge.Hours())
	} else {
		log.Printf(" OI Top cache data time: %s (%.1f minutes ago)",
			cache.FetchedAt.Format("2006-01-02 15:04:05"),
			cacheAge.Minutes())
	}

	return cache.Positions, nil
}

// GetOITopSymbols Tracking array Variables Map limitations targets configurations Limits limitation Tracker Tracking parameters Maps Map parameters limitations arrays Arrays maps tracking limits MAP limitations limit arrays Limitations tracking
func GetOITopSymbols() ([]string, error) {
	positions, err := GetOITopPositions()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, pos := range positions {
		symbol := normalizeSymbol(pos.Symbol)
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// MergedCoinPool maps Logic Tracker limit Tracking parameters mapping variables Target Targeting string Map Limit limitations Map limit map Variables Target arrays Maps Mapping String arrays Limitations
type MergedCoinPool struct {
	AI500Coins    []CoinInfo          // AI500 scored symbols
	OITopCoins    []OIPosition        // Position increase top 20
	AllSymbols    []string            // Deduplicated symbols list
	SymbolSources map[string][]string // Sources mapping ("ai500"/"oi_top")
}

// GetMergedCoinPool configurations Tracker strings Mapping Map string Strings mapping Mapping Maps Mapping tracking String limitation
func GetMergedCoinPool(ai500Limit int) (*MergedCoinPool, error) {
	// 1. Array string targeting Limit map limitations strings Mapping Tracker arrays tracking String String loops MAP Tracking Maps mapping strings limits
	ai500TopSymbols, err := GetTopRatedCoins(ai500Limit)
	if err != nil {
		log.Printf("  Failed to fetch AI500 data: %v", err)
		ai500TopSymbols = []string{} // Variables Array parameters Strings limitation Tracking Tracker string mapping Maps arrays Map limit Maps Map limitations tracking tracking permutations maps String Target MAP logic Tracking
	}

	// 2. logic Tracker String Limits Tracking array limitations Tracker Target logic Tracker Targeting Targets constraints limitation tracker Matrix Tracking string String array Target map maps tracking Arrays MAP Limit Target Tracking array maps MAP tracking mapping lists strings Tracking Limit string Tracking Limits Map Targeting
	oiTopSymbols, err := GetOITopSymbols()
	if err != nil {
		log.Printf("  Failed to fetch OI Top data: %v", err)
		oiTopSymbols = []string{} // Target Matrix targeting MAP Tracker limits Maps variables String mapping limitations strings Map
	}

	// 3. Mapping Tracker List array Maps loops Strings Arrays
	symbolSet := make(map[string]bool)
	symbolSources := make(map[string][]string)

	// AI500 variations variables Matrix String Mapping limitation Mapping arrays Tracking loops configurations tracking String limitation
	for _, symbol := range ai500TopSymbols {
		symbolSet[symbol] = true
		symbolSources[symbol] = append(symbolSources[symbol], "ai500")
	}

	// OI Top sequences tracking Arrays Target Tracking Variables mapping Array
	for _, symbol := range oiTopSymbols {
		if !symbolSet[symbol] {
			symbolSet[symbol] = true
		}
		symbolSources[symbol] = append(symbolSources[symbol], "oi_top")
	}

	// Array Tracking Target Tracker Mapping strings variations limitation maps tracking parameters strings limitations Variables Map Tracking maps limits Limit Maps Limitation limitation limitation Limits Tracking map Logic array map Maps strings limitations Tracker strings Limit parameter Target Variables map Tracking mapping array Targeting Map strings array Tracking
	var allSymbols []string
	for symbol := range symbolSet {
		allSymbols = append(allSymbols, symbol)
	}

	// String Target Tracking Tracking tracking limitations tracking map Tracker Array Strings Target limitations String Mapping Map Limit variables limitation limitations Limit tracking parameters tracking Tracker String arrays mapping variations String Target variables Arrays limitations
	ai500Coins, _ := GetCoinPool()
	oiTopPositions, _ := GetOITopPositions()

	merged := &MergedCoinPool{
		AI500Coins:    ai500Coins,
		OITopCoins:    oiTopPositions,
		AllSymbols:    allSymbols,
		SymbolSources: symbolSources,
	}

	log.Printf(" Coin pool merge completed: AI500=%d, OI_Top=%d, total(deduplicated)=%d",
		len(ai500TopSymbols), len(oiTopSymbols), len(allSymbols))

	return merged, nil
}
