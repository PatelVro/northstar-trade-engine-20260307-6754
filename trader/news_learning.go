package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type newsLearningState struct {
	UpdatedAt   string                        `json:"updated_at"`
	TraderID    string                        `json:"trader_id"`
	GlobalScore float64                       `json:"global_score"`
	Symbols     map[string]newsLearningSymbol `json:"symbols"`
}

type newsLearningSymbol struct {
	Score   float64 `json:"score"`
	Samples int     `json:"samples"`
}

func (at *AutoTrader) loadNewsLearningState() error {
	if at.newsMemoryPath == "" {
		at.newsMemoryPath = filepath.Join("runtime", "news_learning", at.id+".json")
	}
	if err := os.MkdirAll(filepath.Dir(at.newsMemoryPath), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(at.newsMemoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var state newsLearningState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode news memory: %w", err)
	}

	if state.GlobalScore > 0 {
		at.newsCredibilityGlobal = clampFloat(state.GlobalScore, 0.35, 1.40)
	}
	for symbol, mem := range state.Symbols {
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		if normalized == "" {
			continue
		}
		at.newsCredibility[normalized] = clampFloat(mem.Score, 0.20, 1.80)
		at.newsSampleCount[normalized] = mem.Samples
	}
	return nil
}

func (at *AutoTrader) persistNewsLearningState() error {
	if at.newsMemoryPath == "" {
		at.newsMemoryPath = filepath.Join("runtime", "news_learning", at.id+".json")
	}
	if err := os.MkdirAll(filepath.Dir(at.newsMemoryPath), 0755); err != nil {
		return err
	}
	payload := newsLearningState{
		UpdatedAt:   time.Now().Format(time.RFC3339),
		TraderID:    at.id,
		GlobalScore: clampFloat(at.newsCredibilityGlobal, 0.35, 1.40),
		Symbols:     make(map[string]newsLearningSymbol, len(at.newsCredibility)),
	}
	for symbol, score := range at.newsCredibility {
		payload.Symbols[symbol] = newsLearningSymbol{
			Score:   clampFloat(score, 0.20, 1.80),
			Samples: at.newsSampleCount[symbol],
		}
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(at.newsMemoryPath, b, 0644)
}

func (at *AutoTrader) effectiveNewsCredibility(symbol string) float64 {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	global := at.newsCredibilityGlobal
	if global <= 0 {
		global = 1.0
	}
	if symbol == "" {
		return clampFloat(global, 0.20, 1.80)
	}
	local := at.newsCredibility[symbol]
	if local <= 0 {
		local = 1.0
	}
	samples := at.newsSampleCount[symbol]
	weight := clampFloat(float64(samples)/20.0, 0, 0.75)
	return clampFloat((1.0-weight)*global+weight*local, 0.20, 1.80)
}

func (at *AutoTrader) newsBiasKey(symbol, side string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	side = strings.ToLower(strings.TrimSpace(side))
	if symbol == "" || (side != "long" && side != "short") {
		return ""
	}
	return symbol + "_" + side
}

func (at *AutoTrader) trackPlannedNewsBias(symbol, side string, bias float64) {
	if !at.config.UseNewsRisk {
		return
	}
	key := at.newsBiasKey(symbol, side)
	if key == "" {
		return
	}
	if bias > 1 {
		bias = 1
	}
	if bias < -1 {
		bias = -1
	}
	at.plannedNewsBias[key] = bias
}

func (at *AutoTrader) promotePlannedNewsBias(symbol, side string) {
	key := at.newsBiasKey(symbol, side)
	if key == "" {
		return
	}
	if bias, ok := at.plannedNewsBias[key]; ok {
		at.positionNewsBias[key] = bias
		delete(at.plannedNewsBias, key)
	}
}

func (at *AutoTrader) clearNewsBias(symbol, side string) {
	key := at.newsBiasKey(symbol, side)
	if key == "" {
		return
	}
	delete(at.plannedNewsBias, key)
	delete(at.positionNewsBias, key)
}

func (at *AutoTrader) learnNewsOutcome(symbol, side string, pnlPct float64) {
	if !at.config.UseNewsRisk {
		return
	}
	key := at.newsBiasKey(symbol, side)
	if key == "" {
		return
	}
	bias, ok := at.positionNewsBias[key]
	delete(at.positionNewsBias, key)
	if !ok || bias == 0 {
		return
	}
	if pnlPct == 0 {
		return
	}

	pred := signFloatNonZero(bias, 0)
	outcome := signFloatNonZero(pnlPct, 0)
	if pred == 0 || outcome == 0 {
		return
	}

	magnitude := clampFloat(absNewsFloat(bias)*(0.35+0.65*clampFloat(absNewsFloat(pnlPct)/3.0, 0, 1)), 0.02, 0.30)
	delta := 0.10 * magnitude
	if pred != outcome {
		delta = -delta
	}

	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	cur := at.newsCredibility[symbol]
	if cur <= 0 {
		cur = 1.0
	}
	at.newsCredibility[symbol] = clampFloat(cur+delta, 0.20, 1.80)
	at.newsSampleCount[symbol] = at.newsSampleCount[symbol] + 1

	global := at.newsCredibilityGlobal
	if global <= 0 {
		global = 1.0
	}
	at.newsCredibilityGlobal = clampFloat(global+delta*0.45, 0.35, 1.40)
	at.lastNewsLearnDelta = delta
	at.lastNewsLearnSymbol = symbol

	if at.newsSampleCount[symbol]%2 == 0 {
		_ = at.persistNewsLearningState()
	}
}

func absNewsFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
