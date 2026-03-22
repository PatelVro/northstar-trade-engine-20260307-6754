package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"northstar/logger"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pipelineBacktestVersion = 1

type pipelineBucketSummary struct {
	Key                    string  `json:"key"`
	Observations           int     `json:"observations"`
	AllowTradeObservations int     `json:"allow_trade_observations"`
	NoTradeObservations    int     `json:"no_trade_observations"`
	EntryDecisions         int     `json:"entry_decisions"`
	BlockedEntries         int     `json:"blocked_entries"`
	ReducedSizeEntries     int     `json:"reduced_size_entries"`
	ClosedTrades           int     `json:"closed_trades"`
	WinRatePct             float64 `json:"win_rate_pct"`
	AvgTradeUSD            float64 `json:"avg_trade_usd"`
	ProfitFactor           float64 `json:"profit_factor"`
	TotalRealizedPnLUSD    float64 `json:"total_realized_pnl_usd"`
}

type pipelineBacktestSummary struct {
	ReportVersion           int                     `json:"report_version"`
	GeneratedAt             time.Time               `json:"generated_at"`
	DecisionLogCount        int                     `json:"decision_log_count"`
	ObservationCount        int                     `json:"observation_count"`
	ValidFeatureCount       int                     `json:"valid_feature_count"`
	InvalidFeatureCount     int                     `json:"invalid_feature_count"`
	AllowTradeCount         int                     `json:"allow_trade_count"`
	NoTradeCount            int                     `json:"no_trade_count"`
	EntryDecisionCount      int                     `json:"entry_decision_count"`
	BlockedEntryCount       int                     `json:"blocked_entry_count"`
	SelectorBlockedCount    int                     `json:"selector_blocked_count"`
	AllocatorBlockedCount   int                     `json:"allocator_blocked_count"`
	ReducedSizeEntryCount   int                     `json:"reduced_size_entry_count"`
	ClosedTradeCount        int                     `json:"closed_trade_count"`
	WinRatePct              float64                 `json:"win_rate_pct"`
	AvgTradeUSD             float64                 `json:"avg_trade_usd"`
	ProfitFactor            float64                 `json:"profit_factor"`
	TotalRealizedPnLUSD     float64                 `json:"total_realized_pnl_usd"`
	ByRegime                []pipelineBucketSummary `json:"by_regime"`
	ByStrategyFamily        []pipelineBucketSummary `json:"by_strategy_family"`
	ByTradingRecommendation []pipelineBucketSummary `json:"by_trading_recommendation"`
}

type pipelineBucketAccumulator struct {
	observations           int
	allowTradeObservations int
	noTradeObservations    int
	entryDecisions         int
	blockedEntries         int
	reducedSizeEntries     int
	closedTrades           int
	winCount               int
	totalPnL               float64
	grossProfit            float64
	grossLoss              float64
}

type pipelineTradeLineage struct {
	regime     string
	family     string
	allowTrade bool
	quantity   float64
}

func analyzePipelineBacktest(decisionLogDir string) (pipelineBacktestSummary, error) {
	records, err := readDecisionLogRecords(decisionLogDir)
	if err != nil {
		return pipelineBacktestSummary{}, err
	}
	summary := pipelineBacktestSummary{
		ReportVersion:           pipelineBacktestVersion,
		GeneratedAt:             time.Now().UTC(),
		ByRegime:                []pipelineBucketSummary{},
		ByStrategyFamily:        []pipelineBucketSummary{},
		ByTradingRecommendation: []pipelineBucketSummary{},
	}
	if len(records) == 0 {
		return summary, nil
	}

	regimeBuckets := make(map[string]*pipelineBucketAccumulator)
	familyBuckets := make(map[string]*pipelineBucketAccumulator)
	recommendationBuckets := make(map[string]*pipelineBucketAccumulator)
	openTrades := make(map[string]pipelineTradeLineage)

	for _, record := range records {
		summary.DecisionLogCount++
		for _, observation := range record.Pipeline {
			summary.ObservationCount++
			if observation.FeatureValid {
				summary.ValidFeatureCount++
			} else {
				summary.InvalidFeatureCount++
			}
			if observation.SelectionAllowTrade {
				summary.AllowTradeCount++
			} else {
				summary.NoTradeCount++
			}

			updateObservationBucket(regimeBuckets, normalizedBucketKey(observation.Regime, "unknown"), observation.SelectionAllowTrade)
			updateObservationBucket(familyBuckets, normalizedBucketKey(observation.SelectedFamily, "unknown"), observation.SelectionAllowTrade)
			recommendationKey := "no_trade"
			if observation.SelectionAllowTrade {
				recommendationKey = "allow_trade"
			}
			updateObservationBucket(recommendationBuckets, recommendationKey, observation.SelectionAllowTrade)
		}

		for _, action := range record.Decisions {
			pipeline := action.Pipeline
			actionType := strings.ToLower(strings.TrimSpace(action.Action))
			if pipeline != nil {
				actionType = strings.ToLower(strings.TrimSpace(pipeline.DecisionAction))
			}
			if actionType == "" {
				actionType = strings.ToLower(strings.TrimSpace(action.Action))
			}
			isEntry := actionType == "open_long" || actionType == "open_short"
			if pipeline != nil && isEntry {
				summary.EntryDecisionCount++
				if !pipeline.DecisionAllowed {
					summary.BlockedEntryCount++
					updateEntryBucket(regimeBuckets, normalizedBucketKey(pipeline.Regime, "unknown"), false, pipeline.AllocationReducedSize)
					updateEntryBucket(familyBuckets, normalizedBucketKey(pipeline.SelectedFamily, "unknown"), false, pipeline.AllocationReducedSize)
					if selectorBlocked(pipeline) {
						summary.SelectorBlockedCount++
					}
					if allocatorBlocked(pipeline) {
						summary.AllocatorBlockedCount++
					}
				} else {
					updateEntryBucket(regimeBuckets, normalizedBucketKey(pipeline.Regime, "unknown"), true, pipeline.AllocationReducedSize)
					updateEntryBucket(familyBuckets, normalizedBucketKey(pipeline.SelectedFamily, "unknown"), true, pipeline.AllocationReducedSize)
				}
				if pipeline.AllocationReducedSize {
					summary.ReducedSizeEntryCount++
				}
			}

			if !actionHasImmediateEffect(action.OrderStatus, action.Success) {
				continue
			}

			switch actionType {
			case "open_long", "open_short":
				if pipeline == nil {
					continue
				}
				key := actionPositionKey(action.Symbol, actionType)
				openTrades[key] = pipelineTradeLineage{
					regime:     normalizedBucketKey(pipeline.Regime, "unknown"),
					family:     normalizedBucketKey(pipeline.SelectedFamily, "unknown"),
					allowTrade: pipeline.SelectionAllowTrade,
					quantity:   action.Quantity,
				}
			case "close_long", "close_short":
				key := actionPositionKey(action.Symbol, actionType)
				lineage, ok := openTrades[key]
				if !ok {
					if pipeline == nil {
						continue
					}
					lineage = pipelineTradeLineage{
						regime:     normalizedBucketKey(pipeline.Regime, "unknown"),
						family:     normalizedBucketKey(pipeline.SelectedFamily, "unknown"),
						allowTrade: pipeline.SelectionAllowTrade,
						quantity:   action.Quantity,
					}
				}
				registerClosedTrade(regimeBuckets, lineage.regime, action.RealizedPnL)
				registerClosedTrade(familyBuckets, lineage.family, action.RealizedPnL)
				recommendationKey := "no_trade"
				if lineage.allowTrade {
					recommendationKey = "allow_trade"
				}
				registerClosedTrade(recommendationBuckets, recommendationKey, action.RealizedPnL)
				summary.ClosedTradeCount++
				summary.TotalRealizedPnLUSD += action.RealizedPnL
				if action.RealizedPnL > 0 {
					summary.WinRatePct += 1
				}
				if action.Quantity > 0 && lineage.quantity > action.Quantity {
					lineage.quantity -= action.Quantity
					openTrades[key] = lineage
				} else {
					delete(openTrades, key)
				}
			}
		}
	}

	if summary.ClosedTradeCount > 0 {
		summary.WinRatePct = (summary.WinRatePct / float64(summary.ClosedTradeCount)) * 100.0
		summary.AvgTradeUSD = summary.TotalRealizedPnLUSD / float64(summary.ClosedTradeCount)
	}
	summary.ProfitFactor = profitFactorFromTotals(bucketGrossProfit(regimeBuckets), bucketGrossLoss(regimeBuckets))
	summary.ByRegime = finalizeBucketSummaries(regimeBuckets)
	summary.ByStrategyFamily = finalizeBucketSummaries(familyBuckets)
	summary.ByTradingRecommendation = finalizeBucketSummaries(recommendationBuckets)

	return summary, nil
}

func readDecisionLogRecords(logDir string) ([]logger.DecisionRecord, error) {
	pattern := filepath.Join(logDir, "decision_*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		err := filepath.Walk(logDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info == nil || info.IsDir() {
				return nil
			}
			if matched, matchErr := filepath.Match("decision_*.json", filepath.Base(path)); matchErr == nil && matched {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	records := make([]logger.DecisionRecord, 0, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record logger.DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CycleNumber == records[j].CycleNumber {
			return records[i].Timestamp.Before(records[j].Timestamp)
		}
		return records[i].CycleNumber < records[j].CycleNumber
	})
	return records, nil
}

func writePipelineSummaryJSON(path string, summary pipelineBacktestSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writePipelineAttributionCSV(path string, summary pipelineBacktestSummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		"group_type",
		"key",
		"observations",
		"allow_trade_observations",
		"no_trade_observations",
		"entry_decisions",
		"blocked_entries",
		"reduced_size_entries",
		"closed_trades",
		"win_rate_pct",
		"avg_trade_usd",
		"profit_factor",
		"total_realized_pnl_usd",
	}); err != nil {
		return err
	}

	writeGroup := func(groupType string, buckets []pipelineBucketSummary) error {
		for _, bucket := range buckets {
			row := []string{
				groupType,
				bucket.Key,
				strconv.Itoa(bucket.Observations),
				strconv.Itoa(bucket.AllowTradeObservations),
				strconv.Itoa(bucket.NoTradeObservations),
				strconv.Itoa(bucket.EntryDecisions),
				strconv.Itoa(bucket.BlockedEntries),
				strconv.Itoa(bucket.ReducedSizeEntries),
				strconv.Itoa(bucket.ClosedTrades),
				fmt.Sprintf("%.2f", bucket.WinRatePct),
				fmt.Sprintf("%.2f", bucket.AvgTradeUSD),
				fmt.Sprintf("%.4f", bucket.ProfitFactor),
				fmt.Sprintf("%.2f", bucket.TotalRealizedPnLUSD),
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
		return nil
	}

	if err := writeGroup("regime", summary.ByRegime); err != nil {
		return err
	}
	if err := writeGroup("strategy_family", summary.ByStrategyFamily); err != nil {
		return err
	}
	return writeGroup("trading_recommendation", summary.ByTradingRecommendation)
}

func updateObservationBucket(buckets map[string]*pipelineBucketAccumulator, key string, allowTrade bool) {
	bucket := ensureBucket(buckets, key)
	bucket.observations++
	if allowTrade {
		bucket.allowTradeObservations++
	} else {
		bucket.noTradeObservations++
	}
}

func updateEntryBucket(buckets map[string]*pipelineBucketAccumulator, key string, allowed bool, reduced bool) {
	bucket := ensureBucket(buckets, key)
	bucket.entryDecisions++
	if !allowed {
		bucket.blockedEntries++
	}
	if reduced {
		bucket.reducedSizeEntries++
	}
}

func registerClosedTrade(buckets map[string]*pipelineBucketAccumulator, key string, pnl float64) {
	bucket := ensureBucket(buckets, key)
	bucket.closedTrades++
	bucket.totalPnL += pnl
	if pnl > 0 {
		bucket.winCount++
		bucket.grossProfit += pnl
	} else if pnl < 0 {
		bucket.grossLoss += -pnl
	}
}

func ensureBucket(buckets map[string]*pipelineBucketAccumulator, key string) *pipelineBucketAccumulator {
	if bucket, ok := buckets[key]; ok {
		return bucket
	}
	bucket := &pipelineBucketAccumulator{}
	buckets[key] = bucket
	return bucket
}

func finalizeBucketSummaries(buckets map[string]*pipelineBucketAccumulator) []pipelineBucketSummary {
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]pipelineBucketSummary, 0, len(keys))
	for _, key := range keys {
		bucket := buckets[key]
		summary := pipelineBucketSummary{
			Key:                    key,
			Observations:           bucket.observations,
			AllowTradeObservations: bucket.allowTradeObservations,
			NoTradeObservations:    bucket.noTradeObservations,
			EntryDecisions:         bucket.entryDecisions,
			BlockedEntries:         bucket.blockedEntries,
			ReducedSizeEntries:     bucket.reducedSizeEntries,
			ClosedTrades:           bucket.closedTrades,
			TotalRealizedPnLUSD:    bucket.totalPnL,
			ProfitFactor:           profitFactorFromTotals(bucket.grossProfit, bucket.grossLoss),
		}
		if bucket.closedTrades > 0 {
			summary.WinRatePct = (float64(bucket.winCount) / float64(bucket.closedTrades)) * 100.0
			summary.AvgTradeUSD = bucket.totalPnL / float64(bucket.closedTrades)
		}
		out = append(out, summary)
	}
	return out
}

func profitFactorFromTotals(grossProfit, grossLoss float64) float64 {
	if grossLoss > 0 {
		return grossProfit / grossLoss
	}
	if grossProfit > 0 {
		return 999
	}
	return 0
}

func normalizedBucketKey(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func selectorBlocked(pipeline *logger.PipelineDecision) bool {
	if pipeline == nil {
		return false
	}
	if !pipeline.SelectionAllowTrade || strings.EqualFold(pipeline.SelectedFamily, "no_trade") || strings.EqualFold(pipeline.SelectionRiskMode, "no_trade") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(pipeline.BlockingReason)), "selector")
}

func allocatorBlocked(pipeline *logger.PipelineDecision) bool {
	if pipeline == nil {
		return false
	}
	if !pipeline.DecisionAllowed && pipeline.SelectionAllowTrade && !pipeline.AllocationAllowTrade {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(pipeline.BlockingReason)), "allocator")
}

func actionHasImmediateEffect(status string, success bool) bool {
	if !success {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "filled", "partially_filled":
		return true
	default:
		return false
	}
}

func actionPositionKey(symbol, action string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	action = strings.ToLower(strings.TrimSpace(action))
	side := ""
	switch action {
	case "open_long", "close_long":
		side = "long"
	case "open_short", "close_short":
		side = "short"
	}
	return symbol + "_" + side
}

func bucketGrossProfit(buckets map[string]*pipelineBucketAccumulator) float64 {
	total := 0.0
	for _, bucket := range buckets {
		total += bucket.grossProfit
	}
	return total
}

func bucketGrossLoss(buckets map[string]*pipelineBucketAccumulator) float64 {
	total := 0.0
	for _, bucket := range buckets {
		total += bucket.grossLoss
	}
	return total
}
