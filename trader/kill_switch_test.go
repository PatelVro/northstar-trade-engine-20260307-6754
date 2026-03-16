package trader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type killSwitchTestTrader struct {
	positions    []map[string]interface{}
	liveOrders   []map[string]interface{}
	cancelledFor []string
}

func (t *killSwitchTestTrader) GetBalance() (map[string]interface{}, error) { return nil, nil }
func (t *killSwitchTestTrader) GetPositions() ([]map[string]interface{}, error) {
	return append([]map[string]interface{}(nil), t.positions...), nil
}
func (t *killSwitchTestTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (t *killSwitchTestTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return nil, nil
}
func (t *killSwitchTestTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (t *killSwitchTestTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return nil, nil
}
func (t *killSwitchTestTrader) SetLeverage(symbol string, leverage int) error { return nil }
func (t *killSwitchTestTrader) GetMarketPrice(symbol string) (float64, error) { return 100, nil }
func (t *killSwitchTestTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return nil
}
func (t *killSwitchTestTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return nil
}
func (t *killSwitchTestTrader) CancelAllOrders(symbol string) error {
	t.cancelledFor = append(t.cancelledFor, symbol)
	return nil
}
func (t *killSwitchTestTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}
func (t *killSwitchTestTrader) GetLiveOrders() ([]map[string]interface{}, error) {
	return append([]map[string]interface{}(nil), t.liveOrders...), nil
}

func TestKillSwitchEnvBlocksTradingAndCancelsOrders(t *testing.T) {
	traderID := "paper_trader"
	t.Setenv(killSwitchEnvVarName+"_"+killSwitchEnvSuffix(traderID), "operator manual stop")

	mockTrader := &killSwitchTestTrader{
		liveOrders: []map[string]interface{}{
			{"symbol": "AAPL"},
		},
	}

	at := &AutoTrader{
		id:             traderID,
		name:           "Paper Trader",
		exchange:       "alpaca",
		trader:         mockTrader,
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-5 * time.Minute),
		config: AutoTraderConfig{
			ID:           traderID,
			Name:         "Paper Trader",
			Mode:         "paper",
			Broker:       "sim",
			StrategyMode: "multi_factor",
			ScanInterval: 3 * time.Second,
		},
	}
	at.initializeBrokerRuntimeState()
	at.initializeKillSwitchState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now(),
		TradingAllowed: true,
		PassCount:      4,
	})

	summary := at.runKillSwitchCheck("test")
	if !summary.Active {
		t.Fatalf("expected env kill switch to be active")
	}
	if summary.Source != "env" {
		t.Fatalf("expected env source, got %q", summary.Source)
	}
	if !summary.OrdersCancelled {
		t.Fatalf("expected kill switch to cancel orders")
	}
	if len(mockTrader.cancelledFor) != 1 || mockTrader.cancelledFor[0] != "AAPL" {
		t.Fatalf("expected cancel call for AAPL, got %+v", mockTrader.cancelledFor)
	}
	if err := at.ensureKillSwitchClear(); err == nil {
		t.Fatalf("expected kill switch gate to block trading")
	}

	status := at.GetOperatorStatus()
	if status.TradingAllowed {
		t.Fatalf("expected operator status to block trading")
	}
	if status.TradingBlockReason != "emergency kill switch active" {
		t.Fatalf("expected kill switch block reason, got %q", status.TradingBlockReason)
	}
	if !status.KillSwitch.Active {
		t.Fatalf("expected nested kill switch summary to be active")
	}
}

func TestKillSwitchFileActivatesAndClears(t *testing.T) {
	tempDir := t.TempDir()
	switchPath := filepath.Join(tempDir, "paper_trader.switch")

	mockTrader := &killSwitchTestTrader{
		liveOrders: []map[string]interface{}{
			{"symbol": "MSFT"},
		},
	}

	at := &AutoTrader{
		id:             "paper_trader",
		name:           "Paper Trader",
		exchange:       "alpaca",
		trader:         mockTrader,
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-5 * time.Minute),
		config: AutoTraderConfig{
			ID:             "paper_trader",
			Name:           "Paper Trader",
			Mode:           "paper",
			Broker:         "sim",
			StrategyMode:   "multi_factor",
			KillSwitchFile: switchPath,
			ScanInterval:   3 * time.Second,
		},
	}
	at.initializeBrokerRuntimeState()
	at.initializeKillSwitchState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now(),
		TradingAllowed: true,
		PassCount:      4,
	})

	if err := os.WriteFile(switchPath, []byte("operator requested halt\n"), 0o644); err != nil {
		t.Fatalf("failed to create kill switch file: %v", err)
	}

	summary := at.runKillSwitchCheck("test")
	if !summary.Active {
		t.Fatalf("expected file kill switch to be active")
	}
	if summary.Source != "file" {
		t.Fatalf("expected file source, got %q", summary.Source)
	}
	if summary.FilePath == "" {
		t.Fatalf("expected file path to be recorded")
	}

	if err := os.Remove(switchPath); err != nil {
		t.Fatalf("failed to remove kill switch file: %v", err)
	}

	summary = at.runKillSwitchCheck("test")
	if summary.Active {
		t.Fatalf("expected kill switch to clear after file removal")
	}
	if err := at.ensureKillSwitchClear(); err != nil {
		t.Fatalf("expected trading to be allowed after kill switch cleared, got %v", err)
	}
}

func TestKillSwitchConfigFlagActivates(t *testing.T) {
	at := &AutoTrader{
		id:             "paper_trader",
		name:           "Paper Trader",
		exchange:       "alpaca",
		trader:         &killSwitchTestTrader{},
		initialBalance: 100000,
		isRunning:      true,
		startTime:      time.Now().Add(-5 * time.Minute),
		config: AutoTraderConfig{
			ID:                  "paper_trader",
			Name:                "Paper Trader",
			Mode:                "paper",
			Broker:              "sim",
			StrategyMode:        "multi_factor",
			EmergencyKillSwitch: true,
			ScanInterval:        3 * time.Second,
		},
	}
	at.initializeBrokerRuntimeState()
	at.initializeKillSwitchState()
	at.setReadinessSummary(ReadinessSummary{
		Status:         ReadinessPass,
		Message:        "startup readiness passed",
		CheckedAt:      time.Now(),
		TradingAllowed: true,
		PassCount:      4,
	})

	summary := at.runKillSwitchCheck("test")
	if !summary.Active {
		t.Fatalf("expected config kill switch to be active")
	}
	if summary.Source != "config" {
		t.Fatalf("expected config source, got %q", summary.Source)
	}
	if !strings.Contains(summary.Message, "emergency_kill_switch=true") {
		t.Fatalf("expected config kill switch message, got %q", summary.Message)
	}
}
