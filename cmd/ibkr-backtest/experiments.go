package main

import (
	"flag"
	"fmt"
	"northstar/allocator"
	"northstar/features"
	"northstar/regime"
	"northstar/research/experiments"
	"northstar/selector"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func captureEffectiveFlagValues(fs *flag.FlagSet) map[string]string {
	values := make(map[string]string)
	if fs == nil {
		return values
	}
	fs.VisitAll(func(f *flag.Flag) {
		values[f.Name] = redactExperimentFlagValue(f.Name, f.Value.String())
	})
	return values
}

func redactExperimentFlagValue(name, value string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch {
	case name == "account-id":
		return maskValue(value, 4)
	case strings.Contains(name, "key"), strings.Contains(name, "secret"), strings.Contains(name, "cookie"), strings.Contains(name, "password"):
		return "[redacted]"
	default:
		return value
	}
}

func maskValue(value string, keep int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if keep <= 0 || len(value) <= keep {
		return "[redacted]"
	}
	return strings.Repeat("*", len(value)-keep) + value[len(value)-keep:]
}

func buildExperimentDatasetFiles(dataDir string, usableSymbols []string, symbolsFile string) []string {
	files := make([]string, 0, len(usableSymbols)+1)
	for _, symbol := range dedupeSymbols(usableSymbols) {
		if symbol == "" {
			continue
		}
		files = append(files, filepath.Join(dataDir, strings.ToUpper(symbol)+".csv"))
	}
	if strings.TrimSpace(symbolsFile) != "" {
		files = append(files, symbolsFile)
	}
	sort.Strings(files)
	return dedupeStringPaths(files)
}

func buildExperimentDatasetMetadata(dataset datasetStudyStats, configuredSymbols, usableSymbols []string, barInterval string, minBarsPerSymbol int) map[string]interface{} {
	metadata := map[string]interface{}{
		"configured_symbol_count": dataset.ConfiguredSymbolCount,
		"usable_symbol_count":     dataset.UsableSymbolCount,
		"coverage_ratio":          dataset.CoverageRatio,
		"study_window_days":       dataset.StudyWindowDays,
		"bar_interval":            barInterval,
		"min_bars_per_symbol":     minBarsPerSymbol,
		"configured_symbols":      append([]string(nil), configuredSymbols...),
		"usable_symbols":          append([]string(nil), usableSymbols...),
	}
	if !dataset.DataStart.IsZero() {
		metadata["data_start"] = dataset.DataStart.Format("2006-01-02T15:04:05Z07:00")
	}
	if !dataset.DataEnd.IsZero() {
		metadata["data_end"] = dataset.DataEnd.Format("2006-01-02T15:04:05Z07:00")
	}
	return metadata
}

func buildExperimentResultMetadata(summary studySummary, results []profileResult) map[string]interface{} {
	metadata := map[string]interface{}{
		"completed_profiles":         summary.CompletedProfiles,
		"credible_profiles":          summary.CredibleProfiles,
		"ranking_eligible_profiles":  summary.RankingEligibleProfiles,
		"requested_profiles":         summary.RequestedProfiles,
		"top_profiles_under_sampled": summary.TopProfilesUnderSampled,
		"feature_schema_version":     features.SchemaVersion,
		"regime_detector_version":    regime.DetectorVersion,
		"strategy_selector_version":  selector.SelectorVersion,
		"allocator_version":          allocator.AllocatorVersion,
	}
	if len(results) > 0 {
		metadata["top_profile_slug"] = results[0].ProfileSlug
		metadata["top_profile_tier"] = results[0].CredibilityTier
		metadata["top_profile_return_pct"] = results[0].ReturnPct
		metadata["leaderboard_profiles"] = len(results)
	}
	return metadata
}

func registerBacktestExperiment(runID, runRoot, workspaceRoot, dataDir, symbolsFile string, configuredSymbols, usableSymbols []string, dataset datasetStudyStats, summary studySummary, results []profileResult, params map[string]string) error {
	resultFiles, err := experiments.CollectBacktestResultFiles(runRoot)
	if err != nil {
		return err
	}
	_, err = experiments.Register(experiments.RegisterRequest{
		ExperimentID:  runID,
		Kind:          "ibkr_backtest",
		RunRoot:       runRoot,
		WorkspaceRoot: workspaceRoot,
		Command:       append([]string{"go", "run", "./cmd/ibkr-backtest"}, os.Args[1:]...),
		Parameters:    params,
		DatasetRoot:   dataDir,
		DatasetFiles:  buildExperimentDatasetFiles(dataDir, usableSymbols, symbolsFile),
		DatasetMetadata: buildExperimentDatasetMetadata(
			dataset,
			configuredSymbols,
			usableSymbols,
			params["bar-interval"],
			intFromString(params["min-bars-per-symbol"]),
		),
		ResultFiles:    resultFiles,
		ResultMetadata: buildExperimentResultMetadata(summary, results),
		Notes: map[string]interface{}{
			"reproducibility":           "manifest includes redacted parameters, code fingerprint, dataset fingerprints, result artifact hashes, and feature/regime/selector/allocator versions",
			"feature_schema_version":    features.SchemaVersion,
			"regime_detector_version":   regime.DetectorVersion,
			"strategy_selector_version": selector.SelectorVersion,
			"allocator_version":         allocator.AllocatorVersion,
		},
	})
	return err
}

func dedupeStringPaths(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func intFromString(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var value int
	_, _ = fmt.Sscanf(raw, "%d", &value)
	return value
}
