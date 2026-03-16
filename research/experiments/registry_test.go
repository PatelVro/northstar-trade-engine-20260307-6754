package experiments

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterWritesManifestAndRegistry(t *testing.T) {
	workspace := t.TempDir()
	runRoot := filepath.Join(workspace, "output", "ibkr_backtests", "run_1")
	registryRoot := filepath.Join(workspace, "output", "research", "experiments")
	datasetRoot := filepath.Join(workspace, "dataset")

	mustWriteFile(t, filepath.Join(workspace, "go.mod"), "module test\n\ngo 1.25.0\n")
	mustWriteFile(t, filepath.Join(workspace, "trader", "sample.go"), "package trader\n")
	mustWriteFile(t, filepath.Join(datasetRoot, "AAPL.csv"), "timestamp,close\n1,100\n2,101\n")
	mustWriteFile(t, filepath.Join(runRoot, "leaderboard.json"), `{"ok":true}`)
	mustWriteFile(t, filepath.Join(runRoot, "study_summary.json"), `{"completed_profiles":1}`)

	manifest, err := Register(RegisterRequest{
		ExperimentID:  "exp_1",
		Kind:          "ibkr_backtest",
		RunRoot:       runRoot,
		WorkspaceRoot: workspace,
		RegistryRoot:  registryRoot,
		Command:       []string{"go", "run", "./cmd/ibkr-backtest"},
		Parameters: map[string]string{
			"study-preset": "quick",
			"max-cycles":   "10",
		},
		DatasetRoot:  datasetRoot,
		DatasetFiles: []string{filepath.Join(datasetRoot, "AAPL.csv")},
		DatasetMetadata: map[string]interface{}{
			"configured_symbol_count": 1,
			"usable_symbol_count":     1,
		},
		ResultFiles: []string{
			filepath.Join(runRoot, "leaderboard.json"),
			filepath.Join(runRoot, "study_summary.json"),
		},
		ResultMetadata: map[string]interface{}{
			"top_profile_slug":   "multi_factor_s0p35_p0p08",
			"completed_profiles": 1,
			"credible_profiles":  0,
		},
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if manifest.CodeVersion.SourceFingerprint == "" {
		t.Fatalf("expected code fingerprint")
	}
	if manifest.Dataset.Fingerprint == "" {
		t.Fatalf("expected dataset fingerprint")
	}
	if manifest.Results.Fingerprint == "" {
		t.Fatalf("expected result fingerprint")
	}

	if _, err := os.Stat(filepath.Join(runRoot, "experiment_manifest.json")); err != nil {
		t.Fatalf("expected run manifest file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(registryRoot, "exp_1.json")); err != nil {
		t.Fatalf("expected registry manifest file: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(registryRoot, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	if len(registry.Experiments) != 1 {
		t.Fatalf("expected one registry entry, got %d", len(registry.Experiments))
	}
	if registry.Experiments[0].TopProfileSlug != "multi_factor_s0p35_p0p08" {
		t.Fatalf("unexpected registry top profile %q", registry.Experiments[0].TopProfileSlug)
	}
}

func TestCollectBacktestResultFilesFiltersDecisionLogs(t *testing.T) {
	runRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(runRoot, "leaderboard.json"), "{}")
	mustWriteFile(t, filepath.Join(runRoot, "profiles", "p1", "output", "replay_summary.json"), "{}")
	mustWriteFile(t, filepath.Join(runRoot, "profiles", "p1", "decision_logs", "decision_1.json"), "{}")

	files, err := CollectBacktestResultFiles(runRoot)
	if err != nil {
		t.Fatalf("CollectBacktestResultFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 result files, got %d (%v)", len(files), files)
	}
	for _, file := range files {
		if filepath.Base(filepath.Dir(file)) == "decision_logs" {
			t.Fatalf("decision logs should not be included: %s", file)
		}
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
