package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type studyPresetConfig struct {
	Name             string
	MaxSymbols       int
	BarLimit         int
	MinBarsPerSymbol int
	MaxCycles        int
	CandidateBatch   int
}

type datasetStudyStats struct {
	ConfiguredSymbolCount int       `json:"configured_symbol_count"`
	UsableSymbolCount     int       `json:"usable_symbol_count"`
	CoverageRatio         float64   `json:"coverage_ratio"`
	DataStart             time.Time `json:"data_start"`
	DataEnd               time.Time `json:"data_end"`
	StudyWindowDays       int       `json:"study_window_days"`
	MinBarsPerSymbol      int       `json:"min_bars_per_symbol"`
	MedianBarsPerSymbol   float64   `json:"median_bars_per_symbol"`
	MaxBarsPerSymbol      int       `json:"max_bars_per_symbol"`
	OverlapBars           int       `json:"overlap_bars"`
}

type tradeStudyStats struct {
	ClosedTradePnLs          []float64
	TradedSymbols            int
	TradeHHI                 float64
	Diversification          float64
	AvgTradesPerActiveSymbol float64
	DominantSymbolTradeShare float64
}

type evidenceThresholds struct {
	MinTradesForCredibility     int     `json:"min_trades_for_credibility"`
	MinActiveBarsForCredibility int     `json:"min_active_bars_for_credibility"`
	MinTestedDaysForCredibility float64 `json:"min_tested_days_for_credibility"`
	MinStudyWindowDays          int     `json:"min_study_window_days"`
	MinUsableSymbols            int     `json:"min_usable_symbols"`
	MinCoverageRatio            float64 `json:"min_coverage_ratio"`
	MaxDominantSymbolShare      float64 `json:"max_dominant_symbol_share"`
	MaxSegmentGapPct            float64 `json:"max_segment_gap_pct"`
}

type evidenceAssessment struct {
	EvidenceScore   float64
	RankingScore    float64
	CredibilityTier string
	RankingEligible bool
	QualityFlags    []string
	QualitySummary  string
}

type studyProfileHeadline struct {
	Rank            int      `json:"rank"`
	ProfileSlug     string   `json:"profile_slug"`
	RankingScore    float64  `json:"ranking_score"`
	CompositeScore  float64  `json:"composite_score"`
	EvidenceScore   float64  `json:"evidence_score"`
	CredibilityTier string   `json:"credibility_tier"`
	TotalTrades     int      `json:"total_trades"`
	TradedSymbols   int      `json:"traded_symbols"`
	ReturnPct       float64  `json:"return_pct"`
	QualityFlags    []string `json:"quality_flags"`
}

type studySummary struct {
	ReportVersion           int                    `json:"report_version"`
	RunID                   string                 `json:"run_id"`
	GeneratedAt             time.Time              `json:"generated_at"`
	StudyPreset             string                 `json:"study_preset"`
	UniversePreset          string                 `json:"universe_preset"`
	BarInterval             string                 `json:"bar_interval"`
	ConfiguredSymbolCount   int                    `json:"configured_symbol_count"`
	UsableSymbolCount       int                    `json:"usable_symbol_count"`
	CoverageRatio           float64                `json:"coverage_ratio"`
	DataStart               time.Time              `json:"data_start"`
	DataEnd                 time.Time              `json:"data_end"`
	StudyWindowDays         int                    `json:"study_window_days"`
	MinBarsPerSymbol        int                    `json:"min_bars_per_symbol"`
	MedianBarsPerSymbol     float64                `json:"median_bars_per_symbol"`
	MaxBarsPerSymbol        int                    `json:"max_bars_per_symbol"`
	OverlapBars             int                    `json:"overlap_bars"`
	MaxCycles               int                    `json:"max_cycles"`
	ReplayWarmupBars        int                    `json:"replay_warmup_bars"`
	RequestedProfiles       int                    `json:"requested_profiles"`
	CompletedProfiles       int                    `json:"completed_profiles"`
	RankingEligibleProfiles int                    `json:"ranking_eligible_profiles"`
	CredibleProfiles        int                    `json:"credible_profiles"`
	ProvisionalProfiles     int                    `json:"provisional_profiles"`
	InsufficientProfiles    int                    `json:"insufficient_profiles"`
	TopProfilesUnderSampled int                    `json:"top_profiles_under_sampled"`
	Thresholds              evidenceThresholds     `json:"thresholds"`
	Warnings                []string               `json:"warnings"`
	TopProfiles             []studyProfileHeadline `json:"top_profiles"`
}

func resolveStudyPreset(name string) (studyPresetConfig, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "quick":
		return studyPresetConfig{
			Name:             "quick",
			MaxSymbols:       20,
			BarLimit:         1000,
			MinBarsPerSymbol: 160,
			MaxCycles:        240,
			CandidateBatch:   20,
		}, nil
	case "standard":
		return studyPresetConfig{
			Name:             "standard",
			MaxSymbols:       40,
			BarLimit:         1600,
			MinBarsPerSymbol: 240,
			MaxCycles:        360,
			CandidateBatch:   24,
		}, nil
	case "broad":
		return studyPresetConfig{
			Name:             "broad",
			MaxSymbols:       80,
			BarLimit:         2400,
			MinBarsPerSymbol: 320,
			MaxCycles:        520,
			CandidateBatch:   32,
		}, nil
	case "extended":
		return studyPresetConfig{
			Name:             "extended",
			MaxSymbols:       0,
			BarLimit:         3600,
			MinBarsPerSymbol: 480,
			MaxCycles:        720,
			CandidateBatch:   40,
		}, nil
	case "custom":
		return studyPresetConfig{Name: "custom"}, nil
	default:
		return studyPresetConfig{}, fmt.Errorf("unknown study-preset %q", name)
	}
}

func previewStringFlag(args []string, name, fallback string) string {
	target := "-" + strings.TrimLeft(name, "-")
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == target && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
		if strings.HasPrefix(arg, target+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, target+"="))
		}
	}
	return fallback
}

func resolveUniverseSymbolsFile(preset, current string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "", "core":
		return "data/universe/us_canada_tradable_core.txt"
	case "broad":
		return "data/universe/us_companies.txt"
	case "custom":
		return current
	default:
		return current
	}
}

func inspectDatasetCoverage(dataDir string, configuredSymbols, usableSymbols []string) (datasetStudyStats, error) {
	configured := dedupeSymbols(configuredSymbols)
	usable := dedupeSymbols(usableSymbols)
	stats := datasetStudyStats{
		ConfiguredSymbolCount: len(configured),
		UsableSymbolCount:     len(usable),
	}
	if stats.ConfiguredSymbolCount > 0 {
		stats.CoverageRatio = float64(stats.UsableSymbolCount) / float64(stats.ConfiguredSymbolCount)
	}
	if len(usable) == 0 {
		return stats, fmt.Errorf("no usable symbols to inspect")
	}

	barCounts := make([]int, 0, len(usable))
	var start time.Time
	var end time.Time

	for _, symbol := range usable {
		path := filepath.Join(dataDir, strings.ToUpper(symbol)+".csv")
		bars, fileStart, fileEnd, err := inspectBarsCSV(path)
		if err != nil {
			return stats, err
		}
		barCounts = append(barCounts, bars)
		if start.IsZero() || (!fileStart.IsZero() && fileStart.Before(start)) {
			start = fileStart
		}
		if end.IsZero() || (!fileEnd.IsZero() && fileEnd.After(end)) {
			end = fileEnd
		}
	}

	sort.Ints(barCounts)
	stats.MinBarsPerSymbol = barCounts[0]
	stats.MaxBarsPerSymbol = barCounts[len(barCounts)-1]
	stats.OverlapBars = stats.MinBarsPerSymbol
	stats.MedianBarsPerSymbol = medianInt(barCounts)
	stats.DataStart = start
	stats.DataEnd = end
	if !start.IsZero() && !end.IsZero() && !end.Before(start) {
		stats.StudyWindowDays = int(end.Sub(start).Hours()/24.0) + 1
	}
	return stats, nil
}

func inspectBarsCSV(path string) (int, time.Time, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}
	if len(rows) <= 1 {
		return 0, time.Time{}, time.Time{}, fmt.Errorf("%s has no data rows", path)
	}

	tsIdx := 0
	for i, header := range rows[0] {
		if strings.EqualFold(strings.TrimSpace(header), "timestamp") {
			tsIdx = i
			break
		}
	}

	bars := 0
	var start time.Time
	var end time.Time
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= tsIdx {
			continue
		}
		ts, err := parseDatasetTimestamp(row[tsIdx])
		if err != nil {
			continue
		}
		if start.IsZero() {
			start = ts
		}
		end = ts
		bars++
	}
	if bars == 0 {
		return 0, time.Time{}, time.Time{}, fmt.Errorf("%s has no valid timestamp rows", path)
	}
	return bars, start, end, nil
}

func parseDatasetTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		switch {
		case n > 1_000_000_000_000:
			return time.UnixMilli(n), nil
		case n > 1_000_000_000:
			return time.Unix(n, 0), nil
		}
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
}

func medianInt(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return float64(values[mid])
	}
	return float64(values[mid-1]+values[mid]) / 2.0
}

func parseBarIntervalDuration(interval string) time.Duration {
	v := strings.TrimSpace(strings.ToLower(interval))
	if v == "" {
		return 0
	}
	unit := v[len(v)-1]
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || n <= 0 {
		return 0
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute
	case 'h':
		return time.Duration(n) * time.Hour
	case 'd':
		return time.Duration(n) * 24 * time.Hour
	default:
		return 0
	}
}

func estimateActiveDays(activeBars int, interval string) float64 {
	if activeBars <= 0 {
		return 0
	}
	dur := parseBarIntervalDuration(interval)
	if dur <= 0 {
		return 0
	}
	return (float64(activeBars) * dur.Hours()) / 24.0
}

func readTradeStudyStats(path string) (tradeStudyStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return tradeStudyStats{}, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return tradeStudyStats{}, err
	}
	if len(rows) <= 1 {
		return tradeStudyStats{}, fmt.Errorf("trades file has no rows")
	}

	header := rows[0]
	actionIdx := -1
	symbolIdx := -1
	pnlIdx := -1
	for i, h := range header {
		col := strings.ToLower(strings.TrimSpace(h))
		switch col {
		case "action":
			actionIdx = i
		case "symbol":
			symbolIdx = i
		case "realized_pnl":
			pnlIdx = i
		}
	}
	if actionIdx < 0 || symbolIdx < 0 {
		return tradeStudyStats{}, fmt.Errorf("trades file missing action/symbol columns")
	}

	closeCounts := make(map[string]int)
	anyCounts := make(map[string]int)
	pnls := make([]float64, 0, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= actionIdx || len(row) <= symbolIdx {
			continue
		}
		action := strings.ToUpper(strings.TrimSpace(row[actionIdx]))
		symbol := strings.ToUpper(strings.TrimSpace(row[symbolIdx]))
		if action == "" || symbol == "" {
			continue
		}
		anyCounts[symbol]++
		if strings.HasPrefix(action, "CLOSE_") {
			closeCounts[symbol]++
			if pnlIdx >= 0 && len(row) > pnlIdx {
				if pnl, err := strconv.ParseFloat(strings.TrimSpace(row[pnlIdx]), 64); err == nil {
					pnls = append(pnls, pnl)
				}
			}
		}
	}

	counts := closeCounts
	if len(counts) == 0 {
		counts = anyCounts
	}
	total := 0
	maxCount := 0
	for _, count := range counts {
		total += count
		if count > maxCount {
			maxCount = count
		}
	}
	if total == 0 {
		return tradeStudyStats{}, fmt.Errorf("no trade rows parsed")
	}

	hhi := 0.0
	for _, count := range counts {
		share := float64(count) / float64(total)
		hhi += share * share
	}
	tradedSymbols := len(counts)
	diversification := 0.0
	if tradedSymbols > 1 {
		minHHI := 1.0 / float64(tradedSymbols)
		diversification = clamp((1.0-hhi)/(1.0-minHHI), 0.0, 1.0)
	}

	stats := tradeStudyStats{
		ClosedTradePnLs:          pnls,
		TradedSymbols:            tradedSymbols,
		TradeHHI:                 hhi,
		Diversification:          diversification,
		AvgTradesPerActiveSymbol: float64(total) / float64(tradedSymbols),
		DominantSymbolTradeShare: float64(maxCount) / float64(total),
	}
	return stats, nil
}

func assessProfileEvidence(r profileResult, thresholds evidenceThresholds, minTradedSymbols int) evidenceAssessment {
	flags := make([]string, 0, 6)
	critical := false

	if thresholds.MinTradesForCredibility > 0 && r.TotalTrades < thresholds.MinTradesForCredibility {
		flags = append(flags, "insufficient_trades")
		critical = true
	}
	if thresholds.MinActiveBarsForCredibility > 0 && r.ActiveBarsTested < thresholds.MinActiveBarsForCredibility {
		flags = append(flags, "insufficient_active_bars")
		critical = true
	}
	if thresholds.MinTestedDaysForCredibility > 0 && r.ActiveDaysEstimate < thresholds.MinTestedDaysForCredibility {
		flags = append(flags, "insufficient_active_days")
		critical = true
	}
	if thresholds.MinStudyWindowDays > 0 && r.StudyWindowDays < thresholds.MinStudyWindowDays {
		flags = append(flags, "narrow_window")
		critical = true
	}
	if thresholds.MinUsableSymbols > 0 && r.UsableSymbolCount < thresholds.MinUsableSymbols {
		flags = append(flags, "insufficient_symbol_coverage")
		critical = true
	} else if thresholds.MinCoverageRatio > 0 && r.CoverageRatio < thresholds.MinCoverageRatio {
		flags = append(flags, "insufficient_symbol_coverage")
		critical = true
	}
	if minTradedSymbols > 0 && r.TradedSymbols > 0 && r.TradedSymbols < minTradedSymbols {
		flags = append(flags, "insufficient_traded_symbols")
	}
	if thresholds.MaxDominantSymbolShare > 0 && r.DominantSymbolTradeShare > thresholds.MaxDominantSymbolShare {
		flags = append(flags, "concentrated_symbol_dependence")
	}
	if thresholds.MaxSegmentGapPct > 0 && math.Abs(r.FirstHalfReturnPct-r.SecondHalfReturnPct) > thresholds.MaxSegmentGapPct {
		flags = append(flags, "unstable_segments")
	} else if r.SegmentStability > 0 && r.SegmentStability < 0.45 {
		flags = append(flags, "unstable_segments")
	}
	flags = dedupeStrings(flags)

	tradeComponent := evidenceProgress(float64(r.TotalTrades), float64(maxInt(1, thresholds.MinTradesForCredibility)))
	barsComponent := evidenceProgress(float64(r.ActiveBarsTested), float64(maxInt(1, thresholds.MinActiveBarsForCredibility)))
	testedDaysComponent := evidenceProgress(r.ActiveDaysEstimate, math.Max(thresholds.MinTestedDaysForCredibility, 1))
	windowComponent := evidenceProgress(float64(r.StudyWindowDays), float64(maxInt(1, thresholds.MinStudyWindowDays)))
	usableSymbolsComponent := evidenceProgress(float64(r.UsableSymbolCount), float64(maxInt(1, thresholds.MinUsableSymbols)))
	coverageComponent := evidenceProgress(r.CoverageRatio, math.Max(thresholds.MinCoverageRatio, 0.01))
	tradedSymbolsComponent := 1.0
	if minTradedSymbols > 0 {
		tradedSymbolsComponent = evidenceProgress(float64(r.TradedSymbols), float64(minTradedSymbols))
	}
	diversificationComponent := clamp(r.Diversification, 0.0, 1.0)
	evidenceScore := 0.28*tradeComponent +
		0.18*barsComponent +
		0.12*testedDaysComponent +
		0.12*windowComponent +
		0.12*usableSymbolsComponent +
		0.08*coverageComponent +
		0.05*tradedSymbolsComponent +
		0.05*diversificationComponent

	if containsString(flags, "concentrated_symbol_dependence") {
		evidenceScore -= 0.10
	}
	if containsString(flags, "unstable_segments") {
		evidenceScore -= 0.10
	}
	evidenceScore = clamp(evidenceScore, 0.0, 1.0)

	credibilityTier := "credible"
	rankingEligible := !critical
	switch {
	case !rankingEligible:
		credibilityTier = "insufficient"
	case len(flags) > 0 || evidenceScore < 0.75:
		credibilityTier = "provisional"
	}

	rankingScore := r.CompositeScore * (0.30 + 0.70*evidenceScore)
	if !rankingEligible {
		rankingScore -= 0.35 + 0.20*(1.0-evidenceScore)
	} else if credibilityTier == "provisional" {
		rankingScore -= 0.05
	}

	return evidenceAssessment{
		EvidenceScore:   evidenceScore,
		RankingScore:    rankingScore,
		CredibilityTier: credibilityTier,
		RankingEligible: rankingEligible,
		QualityFlags:    flags,
		QualitySummary:  qualitySummary(flags, credibilityTier),
	}
}

func evidenceProgress(actual, threshold float64) float64 {
	if threshold <= 0 {
		return 1.0
	}
	return clamp(actual/threshold, 0.0, 1.0)
}

func qualitySummary(flags []string, tier string) string {
	if len(flags) == 0 {
		switch tier {
		case "credible":
			return "meets configured evidence thresholds"
		case "provisional":
			return "usable but not yet strong enough for high confidence"
		default:
			return "insufficient evidence"
		}
	}
	parts := make([]string, 0, len(flags))
	for _, flag := range flags {
		switch flag {
		case "insufficient_trades":
			parts = append(parts, "trade count below credibility threshold")
		case "insufficient_active_bars":
			parts = append(parts, "tested bars below credibility threshold")
		case "insufficient_active_days":
			parts = append(parts, "tested days below credibility threshold")
		case "narrow_window":
			parts = append(parts, "dataset window is too short")
		case "insufficient_symbol_coverage":
			parts = append(parts, "usable symbol coverage is too narrow")
		case "insufficient_traded_symbols":
			parts = append(parts, "too few symbols actually traded")
		case "concentrated_symbol_dependence":
			parts = append(parts, "results rely too heavily on one symbol")
		case "unstable_segments":
			parts = append(parts, "segment returns are unstable")
		default:
			parts = append(parts, strings.ReplaceAll(flag, "_", " "))
		}
	}
	return strings.Join(parts, "; ")
}

func sortProfileResults(results []profileResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].RankingEligible != results[j].RankingEligible {
			return results[i].RankingEligible
		}
		if results[i].RankingScore == results[j].RankingScore {
			if results[i].EvidenceScore == results[j].EvidenceScore {
				if results[i].CompositeScore == results[j].CompositeScore {
					if results[i].ReturnPct == results[j].ReturnPct {
						return results[i].MaxDrawdownPct < results[j].MaxDrawdownPct
					}
					return results[i].ReturnPct > results[j].ReturnPct
				}
				return results[i].CompositeScore > results[j].CompositeScore
			}
			return results[i].EvidenceScore > results[j].EvidenceScore
		}
		return results[i].RankingScore > results[j].RankingScore
	})
}

func buildStudySummary(runID, studyPreset, universePreset, barInterval string, dataset datasetStudyStats, thresholds evidenceThresholds, maxCycles, warmupBars, requestedProfiles int, results []profileResult) studySummary {
	summary := studySummary{
		ReportVersion:         2,
		RunID:                 runID,
		GeneratedAt:           time.Now(),
		StudyPreset:           studyPreset,
		UniversePreset:        universePreset,
		BarInterval:           barInterval,
		ConfiguredSymbolCount: dataset.ConfiguredSymbolCount,
		UsableSymbolCount:     dataset.UsableSymbolCount,
		CoverageRatio:         dataset.CoverageRatio,
		DataStart:             dataset.DataStart,
		DataEnd:               dataset.DataEnd,
		StudyWindowDays:       dataset.StudyWindowDays,
		MinBarsPerSymbol:      dataset.MinBarsPerSymbol,
		MedianBarsPerSymbol:   dataset.MedianBarsPerSymbol,
		MaxBarsPerSymbol:      dataset.MaxBarsPerSymbol,
		OverlapBars:           dataset.OverlapBars,
		MaxCycles:             maxCycles,
		ReplayWarmupBars:      warmupBars,
		RequestedProfiles:     requestedProfiles,
		CompletedProfiles:     len(results),
		Thresholds:            thresholds,
		Warnings:              []string{},
		TopProfiles:           []studyProfileHeadline{},
	}

	for i, result := range results {
		switch result.CredibilityTier {
		case "credible":
			summary.CredibleProfiles++
		case "provisional":
			summary.ProvisionalProfiles++
		default:
			summary.InsufficientProfiles++
		}
		if result.RankingEligible {
			summary.RankingEligibleProfiles++
		}
		if i < 5 && !result.RankingEligible {
			summary.TopProfilesUnderSampled++
		}
		if i < 10 {
			summary.TopProfiles = append(summary.TopProfiles, studyProfileHeadline{
				Rank:            i + 1,
				ProfileSlug:     result.ProfileSlug,
				RankingScore:    result.RankingScore,
				CompositeScore:  result.CompositeScore,
				EvidenceScore:   result.EvidenceScore,
				CredibilityTier: result.CredibilityTier,
				TotalTrades:     result.TotalTrades,
				TradedSymbols:   result.TradedSymbols,
				ReturnPct:       result.ReturnPct,
				QualityFlags:    append([]string(nil), result.QualityFlags...),
			})
		}
	}

	if summary.RankingEligibleProfiles == 0 {
		summary.Warnings = append(summary.Warnings, "No profile met the configured credibility thresholds; treat the leaderboard as exploratory only.")
	}
	if dataset.StudyWindowDays > 0 && thresholds.MinStudyWindowDays > 0 && dataset.StudyWindowDays < thresholds.MinStudyWindowDays {
		summary.Warnings = append(summary.Warnings, fmt.Sprintf("Study window is only %d days; configured minimum is %d days.", dataset.StudyWindowDays, thresholds.MinStudyWindowDays))
	}
	if thresholds.MinCoverageRatio > 0 && dataset.CoverageRatio > 0 && dataset.CoverageRatio < thresholds.MinCoverageRatio {
		summary.Warnings = append(summary.Warnings, fmt.Sprintf("Usable symbol coverage is %.0f%%; configured minimum is %.0f%%.", dataset.CoverageRatio*100.0, thresholds.MinCoverageRatio*100.0))
	}
	if dataset.UsableSymbolCount > 0 && thresholds.MinUsableSymbols > 0 && dataset.UsableSymbolCount < thresholds.MinUsableSymbols {
		summary.Warnings = append(summary.Warnings, fmt.Sprintf("Only %d usable symbols were available; configured minimum is %d.", dataset.UsableSymbolCount, thresholds.MinUsableSymbols))
	}
	return summary
}

func writeStudySummaryJSON(path string, summary studySummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeStudySummaryMarkdown(path string, summary studySummary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Northstar Backtest Study Summary\n\n")
	b.WriteString(fmt.Sprintf("- Generated: %s\n", summary.GeneratedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Study preset: `%s`\n", summary.StudyPreset))
	b.WriteString(fmt.Sprintf("- Universe preset: `%s`\n", summary.UniversePreset))
	b.WriteString(fmt.Sprintf("- Bar interval: `%s`\n", summary.BarInterval))
	b.WriteString(fmt.Sprintf("- Configured symbols: `%d`\n", summary.ConfiguredSymbolCount))
	b.WriteString(fmt.Sprintf("- Usable symbols: `%d` (%.1f%% coverage)\n", summary.UsableSymbolCount, summary.CoverageRatio*100.0))
	if !summary.DataStart.IsZero() && !summary.DataEnd.IsZero() {
		b.WriteString(fmt.Sprintf("- Study window: `%s` to `%s` (%d days)\n", summary.DataStart.Format("2006-01-02"), summary.DataEnd.Format("2006-01-02"), summary.StudyWindowDays))
	}
	b.WriteString(fmt.Sprintf("- Bars per usable symbol: min `%d`, median `%.1f`, max `%d`\n", summary.MinBarsPerSymbol, summary.MedianBarsPerSymbol, summary.MaxBarsPerSymbol))
	b.WriteString(fmt.Sprintf("- Profiles completed: `%d/%d`\n", summary.CompletedProfiles, summary.RequestedProfiles))
	b.WriteString(fmt.Sprintf("- Ranking eligible: `%d`\n", summary.RankingEligibleProfiles))
	b.WriteString(fmt.Sprintf("- Credibility tiers: credible `%d`, provisional `%d`, insufficient `%d`\n", summary.CredibleProfiles, summary.ProvisionalProfiles, summary.InsufficientProfiles))

	if len(summary.Warnings) > 0 {
		b.WriteString("\n## Warnings\n\n")
		for _, warning := range summary.Warnings {
			b.WriteString(fmt.Sprintf("- %s\n", warning))
		}
	}

	if len(summary.TopProfiles) > 0 {
		b.WriteString("\n## Top Profiles\n\n")
		b.WriteString("| Rank | Profile | Tier | Rank Score | Evidence | Return % | Trades | Traded Symbols | Flags |\n")
		b.WriteString("| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- |\n")
		for _, item := range summary.TopProfiles {
			flags := strings.Join(item.QualityFlags, ", ")
			if flags == "" {
				flags = "none"
			}
			b.WriteString(fmt.Sprintf("| %d | %s | %s | %.3f | %.2f | %.2f | %d | %d | %s |\n",
				item.Rank, item.ProfileSlug, item.CredibilityTier, item.RankingScore, item.EvidenceScore, item.ReturnPct, item.TotalTrades, item.TradedSymbols, flags))
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
