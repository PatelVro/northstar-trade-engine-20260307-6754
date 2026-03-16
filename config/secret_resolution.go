package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loadConfigWithLocalOverrides(filename string) ([]byte, error) {
	base, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	candidates := localOverrideCandidates(filename)
	merged := base
	for _, candidate := range candidates {
		override, err := os.ReadFile(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read local override '%s': %w", candidate, err)
		}
		merged, err = mergeJSONObjects(merged, override)
		if err != nil {
			return nil, fmt.Errorf("failed to merge local override '%s': %w", candidate, err)
		}
	}

	return merged, nil
}

func localOverrideCandidates(filename string) []string {
	absFilename, _ := filepath.Abs(filename)
	baseDir := filepath.Dir(filename)
	baseName := filepath.Base(filename)
	baseExt := filepath.Ext(baseName)
	baseStem := strings.TrimSuffix(baseName, baseExt)

	candidates := []string{
		filepath.Join(baseDir, "config.local.json"),
	}
	if baseExt != "" {
		candidates = append(candidates, filepath.Join(baseDir, baseStem+".local"+baseExt))
	}
	filtered := make([]string, 0, len(candidates))
	for _, candidate := range dedupePaths(candidates) {
		absCandidate, _ := filepath.Abs(candidate)
		if absCandidate == absFilename {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func mergeJSONObjects(base, override []byte) ([]byte, error) {
	var baseMap map[string]interface{}
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, err
	}

	var overrideMap map[string]interface{}
	if err := json.Unmarshal(override, &overrideMap); err != nil {
		return nil, err
	}

	merged := mergeJSONMaps(baseMap, overrideMap)
	return json.Marshal(merged)
}

func mergeJSONMaps(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = make(map[string]interface{})
	}
	for key, overrideValue := range override {
		if overrideMap, ok := overrideValue.(map[string]interface{}); ok {
			if baseMap, ok := base[key].(map[string]interface{}); ok {
				base[key] = mergeJSONMaps(baseMap, overrideMap)
				continue
			}
		}
		base[key] = overrideValue
	}
	return base
}

func (c *Config) resolveSensitiveValues() {
	for i := range c.Traders {
		trader := &c.Traders[i]
		trader.BinanceAPIKey = resolveConfigValue(trader.BinanceAPIKey,
			"NORTHSTAR_BINANCE_API_KEY",
			"BINANCE_API_KEY",
		)
		trader.BinanceSecretKey = resolveConfigValue(trader.BinanceSecretKey,
			"NORTHSTAR_BINANCE_SECRET_KEY",
			"BINANCE_SECRET_KEY",
		)
		trader.HyperliquidPrivateKey = resolveConfigValue(trader.HyperliquidPrivateKey,
			"NORTHSTAR_HYPERLIQUID_PRIVATE_KEY",
		)
		trader.HyperliquidWalletAddr = resolveConfigValue(trader.HyperliquidWalletAddr,
			"NORTHSTAR_HYPERLIQUID_WALLET_ADDR",
		)
		trader.AsterUser = resolveConfigValue(trader.AsterUser,
			"NORTHSTAR_ASTER_USER",
		)
		trader.AsterSigner = resolveConfigValue(trader.AsterSigner,
			"NORTHSTAR_ASTER_SIGNER",
		)
		trader.AsterPrivateKey = resolveConfigValue(trader.AsterPrivateKey,
			"NORTHSTAR_ASTER_PRIVATE_KEY",
		)
		trader.AlpacaAPIKey = resolveConfigValue(trader.AlpacaAPIKey,
			"NORTHSTAR_ALPACA_API_KEY",
			"APCA_API_KEY_ID",
		)
		trader.AlpacaSecretKey = resolveConfigValue(trader.AlpacaSecretKey,
			"NORTHSTAR_ALPACA_SECRET_KEY",
			"APCA_API_SECRET_KEY",
		)
		trader.IBKRGatewayURL = resolveConfigValue(trader.IBKRGatewayURL,
			"NORTHSTAR_IBKR_BASE_URL",
		)
		trader.IBKRAccountID = resolveConfigValue(trader.IBKRAccountID,
			"NORTHSTAR_IBKR_ACCOUNT_ID",
		)
		trader.IBKRSessionCookie = resolveConfigValue(trader.IBKRSessionCookie,
			"NORTHSTAR_IBKR_SESSION_COOKIE",
			"IBKR_SESSION_COOKIE",
		)
		trader.QwenKey = resolveConfigValue(trader.QwenKey,
			"NORTHSTAR_QWEN_API_KEY",
			"QWEN_KEY",
		)
		trader.DeepSeekKey = resolveConfigValue(trader.DeepSeekKey,
			"NORTHSTAR_DEEPSEEK_API_KEY",
			"DEEPSEEK_KEY",
		)
		trader.CustomAPIURL = resolveConfigValue(trader.CustomAPIURL,
			"NORTHSTAR_CUSTOM_API_URL",
			"CUSTOM_API_URL",
		)
		trader.CustomAPIKey = resolveConfigValue(trader.CustomAPIKey,
			"NORTHSTAR_CUSTOM_API_KEY",
			"CUSTOM_API_KEY",
		)
		trader.CustomModelName = resolveConfigValue(trader.CustomModelName,
			"NORTHSTAR_CUSTOM_MODEL_NAME",
			"CUSTOM_MODEL_NAME",
		)
	}
}

func resolveConfigValue(value string, envNames ...string) string {
	trimmed := strings.TrimSpace(value)

	candidates := make([]string, 0, len(envNames)+1)
	if envName, ok := parseEnvReference(trimmed); ok {
		candidates = append(candidates, envName)
	}
	candidates = append(candidates, envNames...)

	if resolved, ok := lookupFirstEnv(candidates...); ok {
		return resolved
	}
	if trimmed == "" || isUnsetPlaceholder(trimmed) || isEnvReferenceLiteral(trimmed) {
		return ""
	}
	return trimmed
}

func lookupFirstEnv(names ...string) (string, bool) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed, true
		}
	}
	return "", false
}

func parseEnvReference(value string) (string, bool) {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}"))
		return name, name != ""
	}
	if strings.HasPrefix(strings.ToLower(value), "env:") {
		name := strings.TrimSpace(value[4:])
		return name, name != ""
	}
	return "", false
}

func isEnvReferenceLiteral(value string) bool {
	_, ok := parseEnvReference(value)
	return ok
}

func isUnsetPlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return true
	}
	switch normalized {
	case "dummy", "changeme", "change_me", "replace_me", "replace-with-real-value":
		return true
	}
	if strings.HasPrefix(normalized, "your_") || strings.HasPrefix(normalized, "your-") {
		return true
	}
	if strings.HasPrefix(normalized, "<your_") || strings.HasPrefix(normalized, "<your-") {
		return true
	}
	return false
}
