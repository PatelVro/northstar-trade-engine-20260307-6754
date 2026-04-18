package trader

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const universePreviewLimit = 25

type runtimeUniverseState struct {
	Available           bool
	InstrumentType      string
	SelectionMode       string
	ConfiguredSource    string
	ConfiguredSymbols   []string
	EffectiveSymbols    []string
	TrustedSymbolsFile  string
	TrustedSymbolsCount int
	BenchmarkSymbols    []string
	ManifestPath        string
	ManifestPersisted   bool
	ManifestLastError   string
	LastUpdatedAt       time.Time
	LastCandidateWindow []string
	LastMandatory       []string
	LastLoadOrder       []string
	Message             string
}

type tradingUniverseManifest struct {
	GeneratedAt         time.Time `json:"generated_at"`
	TraderID            string    `json:"trader_id"`
	TraderName          string    `json:"trader_name"`
	Mode                string    `json:"mode"`
	InstrumentType      string    `json:"instrument_type"`
	SelectionMode       string    `json:"selection_mode"`
	ConfiguredSource    string    `json:"configured_source"`
	ConfiguredSymbols   []string  `json:"configured_symbols"`
	EffectiveSymbols    []string  `json:"effective_symbols"`
	TrustedSymbolsFile  string    `json:"trusted_symbols_file,omitempty"`
	TrustedSymbolsCount int       `json:"trusted_symbols_count"`
	BenchmarkSymbols    []string  `json:"benchmark_symbols,omitempty"`
	Message             string    `json:"message"`
}

func universeManifestPath(traderID string) string {
	return filepath.Join("output", "universe", traderID, "active_universe.json")
}

func cloneUniverseStrings(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	return append([]string(nil), symbols...)
}

func previewUniverseSymbols(symbols []string) ([]string, bool) {
	if len(symbols) <= universePreviewLimit {
		return cloneUniverseStrings(symbols), false
	}
	return cloneUniverseStrings(symbols[:universePreviewLimit]), true
}

func normalizeConfiguredUniverseSymbols(symbols []string, equity bool) []string {
	seen := make(map[string]struct{}, len(symbols))
	normalized := make([]string, 0, len(symbols))
	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(strings.Trim(raw, "\"'")))
		if symbol == "" {
			continue
		}
		if equity && strings.HasSuffix(symbol, "USDT") {
			continue
		}
		if _, exists := seen[symbol]; exists {
			continue
		}
		seen[symbol] = struct{}{}
		normalized = append(normalized, symbol)
	}
	return normalized
}

func describeConfiguredUniverseSource(configured []string, configuredFile string) string {
	hasInline := len(configured) > 0
	hasFile := strings.TrimSpace(configuredFile) != ""
	switch {
	case hasInline && hasFile:
		return "default_coins + default_coins_file"
	case hasFile:
		return "default_coins_file"
	case hasInline:
		return "default_coins"
	default:
		return "none"
	}
}

func (at *AutoTrader) setUniverseState(state runtimeUniverseState) {
	at.universeMu.Lock()
	defer at.universeMu.Unlock()
	at.universeState = state
}

func (at *AutoTrader) currentUniverseSummary() runtimeUniverseState {
	at.universeMu.RLock()
	defer at.universeMu.RUnlock()
	state := at.universeState
	state.ConfiguredSymbols = cloneUniverseStrings(state.ConfiguredSymbols)
	state.EffectiveSymbols = cloneUniverseStrings(state.EffectiveSymbols)
	state.BenchmarkSymbols = cloneUniverseStrings(state.BenchmarkSymbols)
	state.LastCandidateWindow = cloneUniverseStrings(state.LastCandidateWindow)
	state.LastMandatory = cloneUniverseStrings(state.LastMandatory)
	state.LastLoadOrder = cloneUniverseStrings(state.LastLoadOrder)
	return state
}

func (at *AutoTrader) activeEntryUniverseSymbols() []string {
	summary := at.currentUniverseSummary()
	if len(summary.EffectiveSymbols) > 0 {
		return summary.EffectiveSymbols
	}
	return nil
}

func (at *AutoTrader) recordUniverseCycleSelection(candidates, mandatory, loadOrder []string) {
	at.universeMu.Lock()
	defer at.universeMu.Unlock()
	at.universeState.LastCandidateWindow = cloneUniverseStrings(candidates)
	at.universeState.LastMandatory = cloneUniverseStrings(mandatory)
	at.universeState.LastLoadOrder = cloneUniverseStrings(loadOrder)
	at.universeState.LastUpdatedAt = time.Now()
}

func (at *AutoTrader) initializeTradingUniverse() error {
	state := runtimeUniverseState{
		Available:           true,
		InstrumentType:      at.config.InstrumentType,
		TrustedSymbolsFile:  strings.TrimSpace(at.config.TrustedSymbolsFile),
		TrustedSymbolsCount: len(at.trustedSymbolSet),
		BenchmarkSymbols:    cloneUniverseStrings(at.config.BenchmarkSymbols),
		ManifestPath:        universeManifestPath(at.id),
		LastUpdatedAt:       time.Now(),
	}

	if at.config.InstrumentType == "equity" {
		configured := normalizeConfiguredUniverseSymbols(at.config.ConfiguredDefaultSymbols, true)
		if len(configured) == 0 {
			return fmt.Errorf("equity trader requires an explicit non-empty default_coins/default_coins_file universe")
		}
		effective := configured
		if len(at.trustedSymbolSet) > 0 {
			effective = filterTradableEquitySymbols(configured, at.trustedSymbolSet)
		}
		if len(effective) == 0 {
			return fmt.Errorf("configured equity universe is empty after trusted symbol filtering")
		}
		state.SelectionMode = "explicit_configured_equity"
		state.ConfiguredSource = describeConfiguredUniverseSource(configured, at.config.ConfiguredDefaultSymbolsFile)
		state.ConfiguredSymbols = configured
		state.EffectiveSymbols = effective
		state.Message = fmt.Sprintf(
			"equity entry universe resolved explicitly from %s (%d configured, %d effective)",
			state.ConfiguredSource,
			len(configured),
			len(effective),
		)
	} else {
		configured := normalizeConfiguredUniverseSymbols(at.config.ConfiguredDefaultSymbols, false)
		state.SelectionMode = "dynamic_merged_pool"
		state.ConfiguredSource = describeConfiguredUniverseSource(configured, at.config.ConfiguredDefaultSymbolsFile)
		state.ConfiguredSymbols = configured
		state.Message = "runtime candidate universe is resolved dynamically from merged pool sources"
	}

	at.setUniverseState(state)

	preview, truncated := previewUniverseSymbols(state.EffectiveSymbols)
	if state.SelectionMode == "explicit_configured_equity" {
		log.Printf(
			" [%s] Canonical equity universe resolved: source=%s configured=%d effective=%d trusted=%d manifest=%s preview=%s%s",
			at.name,
			state.ConfiguredSource,
			len(state.ConfiguredSymbols),
			len(state.EffectiveSymbols),
			state.TrustedSymbolsCount,
			state.ManifestPath,
			strings.Join(preview, ","),
			map[bool]string{true: ",...", false: ""}[truncated],
		)
	}

	return nil
}

func (at *AutoTrader) persistTradingUniverseManifest() {
	summary := at.currentUniverseSummary()
	if !summary.Available || summary.ManifestPath == "" {
		return
	}

	payload := tradingUniverseManifest{
		GeneratedAt:         time.Now(),
		TraderID:            at.id,
		TraderName:          at.name,
		Mode:                at.config.Mode,
		InstrumentType:      summary.InstrumentType,
		SelectionMode:       summary.SelectionMode,
		ConfiguredSource:    summary.ConfiguredSource,
		ConfiguredSymbols:   cloneUniverseStrings(summary.ConfiguredSymbols),
		EffectiveSymbols:    cloneUniverseStrings(summary.EffectiveSymbols),
		TrustedSymbolsFile:  summary.TrustedSymbolsFile,
		TrustedSymbolsCount: summary.TrustedSymbolsCount,
		BenchmarkSymbols:    cloneUniverseStrings(summary.BenchmarkSymbols),
		Message:             summary.Message,
	}

	if err := os.MkdirAll(filepath.Dir(summary.ManifestPath), 0755); err != nil {
		at.universeMu.Lock()
		at.universeState.ManifestPersisted = false
		at.universeState.ManifestLastError = err.Error()
		at.universeMu.Unlock()
		log.Printf(" [%s] Failed to persist trading universe manifest: %v", at.name, err)
		return
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		at.universeMu.Lock()
		at.universeState.ManifestPersisted = false
		at.universeState.ManifestLastError = err.Error()
		at.universeMu.Unlock()
		log.Printf(" [%s] Failed to marshal trading universe manifest: %v", at.name, err)
		return
	}
	if err := os.WriteFile(summary.ManifestPath, data, 0644); err != nil {
		at.universeMu.Lock()
		at.universeState.ManifestPersisted = false
		at.universeState.ManifestLastError = err.Error()
		at.universeMu.Unlock()
		log.Printf(" [%s] Failed to write trading universe manifest: %v", at.name, err)
		return
	}

	at.universeMu.Lock()
	at.universeState.ManifestPersisted = true
	at.universeState.ManifestLastError = ""
	at.universeMu.Unlock()
}

func loadSymbolSetFromFile(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		})
		for _, token := range parts {
			symbol := strings.ToUpper(strings.Trim(strings.TrimSpace(token), "\"'"))
			if symbol != "" {
				set[symbol] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no symbols found")
	}
	return set, nil
}

func filterTradableEquitySymbols(symbols []string, trusted map[string]struct{}) []string {
	filtered := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, raw := range symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if !isLikelyTradableEquitySymbol(symbol) {
			continue
		}
		if len(trusted) > 0 {
			if _, ok := trusted[symbol]; !ok {
				continue
			}
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		filtered = append(filtered, symbol)
	}
	return filtered
}

func isLikelyTradableEquitySymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	if strings.Contains(symbol, "/") {
		return false
	}
	if strings.HasSuffix(symbol, ".WS") || strings.HasSuffix(symbol, ".WT") || strings.HasSuffix(symbol, ".U") || strings.HasSuffix(symbol, ".R") {
		return false
	}
	if strings.HasSuffix(symbol, "WS") || strings.HasSuffix(symbol, "WT") || strings.HasSuffix(symbol, "RT") {
		return false
	}

	dotCount := strings.Count(symbol, ".")
	if dotCount > 1 {
		return false
	}
	if dotCount == 1 {
		parts := strings.Split(symbol, ".")
		if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 5 || len(parts[1]) != 1 {
			return false
		}
	}

	base := strings.ReplaceAll(symbol, ".", "")
	if len(base) == 0 || len(base) > 5 {
		return false
	}
	if len(base) == 5 {
		last := base[len(base)-1]
		if last == 'W' || last == 'U' || last == 'R' {
			return false
		}
	}

	for _, ch := range symbol {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' {
			continue
		}
		return false
	}
	return true
}
