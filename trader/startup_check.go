package trader

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// StartupCheck is the result of a single startup diagnostic.
type StartupCheck struct {
	Name   string // Short identifier, e.g. "config_sanity"
	Passed bool
	Detail string // Human-readable outcome
	Action string // What the operator should do if this fails (empty when passing)
}

// StartupCheckReport is the aggregate result of RunStartupSelfCheck.
type StartupCheckReport struct {
	AllPassed bool
	Checks    []StartupCheck
}

// RunStartupSelfCheck runs a comprehensive set of diagnostics before the trader begins its
// main loop. Unlike the per-gate readiness system (which blocks startup until conditions are
// met), this is informational only: it logs actionable guidance for every failed check but
// does NOT block startup. The existing readiness gate system handles blocking.
func RunStartupSelfCheck(at *AutoTrader) StartupCheckReport {
	report := StartupCheckReport{}

	report.add(checkStartupConfigSanity(at))
	report.add(checkStartupCredentials(at))
	report.add(checkStartupDataFiles(at))
	report.add(checkStartupBrokerConnectivity(at))
	report.add(checkStartupAIEndpoint(at))

	report.AllPassed = true
	for _, c := range report.Checks {
		if !c.Passed {
			report.AllPassed = false
			break
		}
	}
	return report
}

func (r *StartupCheckReport) add(c StartupCheck) {
	r.Checks = append(r.Checks, c)
}

// LogStartupCheckReport emits the report to the standard logger with clear formatting.
func LogStartupCheckReport(at *AutoTrader, report StartupCheckReport) {
	prefix := fmt.Sprintf("[%s] Startup self-check:", at.id)
	log.Println(prefix)
	for _, c := range report.Checks {
		if c.Passed {
			log.Printf("  \u2713 %s: %s", c.Name, c.Detail)
		} else {
			log.Printf("  \u2717 %s: %s", c.Name, c.Detail)
			if c.Action != "" {
				log.Printf("    \u2192 ACTION: %s", c.Action)
			}
		}
	}
	if !report.AllPassed {
		log.Printf("[%s] Startup self-check complete: some checks failed (see above). Trading will continue but may be blocked by the readiness gate.", at.id)
	} else {
		log.Printf("[%s] Startup self-check complete: all checks passed.", at.id)
	}
}

// checkStartupConfigSanity validates required config fields.
func checkStartupConfigSanity(at *AutoTrader) StartupCheck {
	name := "config_sanity"

	switch {
	case strings.TrimSpace(at.id) == "":
		return StartupCheck{Name: name, Passed: false, Detail: "trader ID is not set",
			Action: "Set a unique 'id' field in the trader config"}
	case strings.TrimSpace(at.name) == "":
		return StartupCheck{Name: name, Passed: false, Detail: "trader name is not set",
			Action: "Set a human-readable 'name' field in the trader config"}
	case strings.TrimSpace(at.config.Mode) == "":
		return StartupCheck{Name: name, Passed: false, Detail: "mode is not set",
			Action: "Set 'mode' to one of: paper, live, replay, shadow"}
	case at.initialBalance <= 0:
		return StartupCheck{Name: name, Passed: false, Detail: fmt.Sprintf("initial_balance is %.2f (must be > 0)", at.initialBalance),
			Action: "Set 'initial_balance' to the starting account value for P&L tracking"}
	case strings.TrimSpace(at.aiModel) == "" && !at.demoMode:
		return StartupCheck{Name: name, Passed: false, Detail: "ai_model is not set",
			Action: "Set 'ai_model' to 'deepseek', 'qwen', or 'custom'"}
	default:
		return StartupCheck{Name: name, Passed: true,
			Detail: fmt.Sprintf("mode=%s broker=%s ai_model=%s initial_balance=%.2f", at.config.Mode, at.config.Broker, at.aiModel, at.initialBalance)}
	}
}

// checkStartupCredentials verifies that API credentials for the configured AI provider are present.
func checkStartupCredentials(at *AutoTrader) StartupCheck {
	name := "credentials"

	if at.demoMode {
		return StartupCheck{Name: name, Passed: true, Detail: "demo mode — no external credentials required"}
	}

	switch strings.ToLower(strings.TrimSpace(at.aiModel)) {
	case "qwen":
		if strings.TrimSpace(at.config.QwenKey) == "" {
			return StartupCheck{Name: name, Passed: false,
				Detail: "Qwen AI key is missing",
				Action: "Set NORTHSTAR_QWEN_API_KEY environment variable or 'qwen_key' in the config"}
		}
		return StartupCheck{Name: name, Passed: true, Detail: "NORTHSTAR_QWEN_API_KEY is set"}
	case "custom":
		switch {
		case strings.TrimSpace(at.config.CustomAPIURL) == "":
			return StartupCheck{Name: name, Passed: false, Detail: "custom AI URL is missing",
				Action: "Set NORTHSTAR_CUSTOM_API_URL or 'custom_api_url' in the config"}
		case strings.TrimSpace(at.config.CustomAPIKey) == "":
			return StartupCheck{Name: name, Passed: false, Detail: "custom AI API key is missing",
				Action: "Set NORTHSTAR_CUSTOM_API_KEY or 'custom_api_key' in the config"}
		case strings.TrimSpace(at.config.CustomModelName) == "":
			return StartupCheck{Name: name, Passed: false, Detail: "custom AI model name is missing",
				Action: "Set NORTHSTAR_CUSTOM_MODEL_NAME or 'custom_model_name' in the config"}
		default:
			return StartupCheck{Name: name, Passed: true, Detail: fmt.Sprintf("custom AI endpoint configured (%s)", at.config.CustomAPIURL)}
		}
	default: // deepseek
		if strings.TrimSpace(at.config.DeepSeekKey) == "" {
			return StartupCheck{Name: name, Passed: false,
				Detail: "DeepSeek API key is missing",
				Action: "Set NORTHSTAR_DEEPSEEK_API_KEY environment variable or 'deepseek_key' in the config"}
		}
		return StartupCheck{Name: name, Passed: true, Detail: "NORTHSTAR_DEEPSEEK_API_KEY is set"}
	}
}

// checkStartupDataFiles verifies that the trusted_symbols_file exists and is non-empty.
func checkStartupDataFiles(at *AutoTrader) StartupCheck {
	name := "data_files"

	path := strings.TrimSpace(at.config.TrustedSymbolsFile)
	if path == "" {
		return StartupCheck{Name: name, Passed: true, Detail: "trusted_symbols_file not configured (using default universe)"}
	}

	info, err := os.Stat(path)
	if err != nil {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("trusted_symbols_file '%s' not found: %v", path, err),
			Action: fmt.Sprintf("Create the file or update 'trusted_symbols_file' in the config; path: %s", path)}
	}
	if info.Size() == 0 {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("trusted_symbols_file '%s' is empty", path),
			Action: "Add at least one symbol (one per line) to the trusted symbols file"}
	}
	return StartupCheck{Name: name, Passed: true,
		Detail: fmt.Sprintf("trusted_symbols_file '%s' exists (%d bytes)", path, info.Size())}
}

// checkStartupBrokerConnectivity does a lightweight HTTP check against the configured broker
// gateway. For IBKR this pings the /tickle endpoint; for other brokers it is a no-op pass.
func checkStartupBrokerConnectivity(at *AutoTrader) StartupCheck {
	name := "broker_connectivity"

	if at.demoMode {
		return StartupCheck{Name: name, Passed: true, Detail: "demo mode — no broker gateway required"}
	}
	if !at.requiresBrokerDependency() {
		return StartupCheck{Name: name, Passed: true, Detail: fmt.Sprintf("no broker dependency for mode=%s broker=%s", at.config.Mode, at.config.Broker)}
	}
	if !at.requiresIBKRSessionReadiness() {
		return StartupCheck{Name: name, Passed: true, Detail: fmt.Sprintf("broker=%s does not require an explicit startup probe", at.config.Broker)}
	}

	gatewayURL := strings.TrimSpace(at.config.IBKRGatewayURL)
	if gatewayURL == "" {
		return StartupCheck{Name: name, Passed: false,
			Detail: "IBKR gateway URL is not configured",
			Action: "Set 'ibkr_gateway_url' or NORTHSTAR_IBKR_BASE_URL in the config"}
	}

	// Lightweight ping: use /tickle which IBKR Gateway exposes for session keep-alive.
	// The gatewayURL typically already contains the /v1/api suffix (our default), but
	// may not if the operator configured a bare host. Handle both by only appending
	// the /v1/api path when missing.
	trimmed := strings.TrimRight(gatewayURL, "/")
	var tickleURL string
	if strings.HasSuffix(trimmed, "/v1/api") {
		tickleURL = trimmed + "/tickle"
	} else {
		tickleURL = trimmed + "/v1/api/tickle"
	}
	// IBKR Client Portal Gateway (and IBeam wrapper) use a self-signed localhost
	// cert. We explicitly skip verification only when targeting 127.0.0.1/localhost.
	// #nosec G402 – intentional for local gateway; public hosts still verify.
	transport := &http.Transport{}
	if u, perr := url.Parse(trimmed); perr == nil && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	resp, err := client.Get(tickleURL) //nolint:noctx
	if err != nil {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("IBKR gateway not reachable at %s: %v", gatewayURL, err),
			Action: fmt.Sprintf("Start the IBKR Client Portal Gateway and ensure it is listening at %s", gatewayURL)}
	}
	resp.Body.Close()

	if resp.StatusCode >= 500 {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("IBKR gateway at %s returned HTTP %d", gatewayURL, resp.StatusCode),
			Action: "Check IBKR Client Portal Gateway logs; it may need to be restarted or re-authenticated"}
	}
	return StartupCheck{Name: name, Passed: true,
		Detail: fmt.Sprintf("IBKR gateway reachable at %s (HTTP %d)", gatewayURL, resp.StatusCode)}
}

// checkStartupAIEndpoint does a lightweight HTTP HEAD/GET against the configured AI endpoint.
// Only meaningful when a custom AI URL is configured; for cloud providers (DeepSeek, Qwen) it
// is a no-op pass since the SDK handles connectivity internally.
func checkStartupAIEndpoint(at *AutoTrader) StartupCheck {
	name := "ai_endpoint"

	if at.demoMode {
		return StartupCheck{Name: name, Passed: true, Detail: "demo mode — no AI endpoint required"}
	}

	if strings.ToLower(strings.TrimSpace(at.aiModel)) != "custom" {
		aiModel := strings.TrimSpace(at.aiModel)
		if aiModel == "" {
			aiModel = "deepseek"
		}
		return StartupCheck{Name: name, Passed: true,
			Detail: fmt.Sprintf("%s endpoint is a managed cloud provider; connectivity verified at first request", aiModel)}
	}

	customURL := strings.TrimSpace(at.config.CustomAPIURL)
	if customURL == "" {
		return StartupCheck{Name: name, Passed: false,
			Detail: "custom AI endpoint URL is empty",
			Action: "Set NORTHSTAR_CUSTOM_API_URL or 'custom_api_url' in the config"}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodHead, customURL, nil)
	if err != nil {
		// Malformed URL
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("custom AI endpoint URL is malformed (%s): %v", customURL, err),
			Action: "Correct the 'custom_api_url' value in the config"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("custom AI endpoint not reachable at %s: %v", customURL, err),
			Action: fmt.Sprintf("Ensure the custom AI server is running and accessible at %s", customURL)}
	}
	resp.Body.Close()

	if resp.StatusCode >= 500 {
		return StartupCheck{Name: name, Passed: false,
			Detail: fmt.Sprintf("custom AI endpoint at %s returned HTTP %d", customURL, resp.StatusCode),
			Action: "Check the custom AI server logs for errors"}
	}
	return StartupCheck{Name: name, Passed: true,
		Detail: fmt.Sprintf("custom AI endpoint reachable at %s (HTTP %d)", customURL, resp.StatusCode)}
}
