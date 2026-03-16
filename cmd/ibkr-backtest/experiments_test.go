package main

import (
	"flag"
	"path/filepath"
	"testing"
)

func TestCaptureEffectiveFlagValuesRedactsSensitiveInputs(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	accountID := fs.String("account-id", "", "")
	sessionCookie := fs.String("session-cookie", "", "")
	deepseekKey := fs.String("deepseek-key", "", "")
	studyPreset := fs.String("study-preset", "quick", "")

	*accountID = "DU1234567"
	*sessionCookie = "x-sess-uuid=secret"
	*deepseekKey = "sk-secret"
	*studyPreset = "broad"

	values := captureEffectiveFlagValues(fs)
	if values["account-id"] == "DU1234567" {
		t.Fatalf("expected account-id to be masked")
	}
	if values["session-cookie"] != "[redacted]" {
		t.Fatalf("expected session-cookie to be redacted, got %q", values["session-cookie"])
	}
	if values["deepseek-key"] != "[redacted]" {
		t.Fatalf("expected deepseek-key to be redacted, got %q", values["deepseek-key"])
	}
	if values["study-preset"] != "broad" {
		t.Fatalf("expected non-sensitive flag value to be preserved, got %q", values["study-preset"])
	}
}

func TestBuildExperimentDatasetFilesIncludesUsedCSVsAndSymbolsFile(t *testing.T) {
	dataDir := filepath.Join("C:\\data", "csv")
	files := buildExperimentDatasetFiles(dataDir, []string{"aapl", "MSFT", "AAPL"}, filepath.Join("data", "universe", "us_companies.txt"))
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d (%v)", len(files), files)
	}
	if filepath.Base(files[0]) != "AAPL.csv" && filepath.Base(files[1]) != "AAPL.csv" {
		t.Fatalf("expected AAPL.csv to be included, got %v", files)
	}
}
