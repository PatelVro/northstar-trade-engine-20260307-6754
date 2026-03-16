package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDedupeSymbolsFiltersInvalidAndHeaders(t *testing.T) {
	in := []string{
		" aapl ",
		"AAPL",
		"msft",
		"SYMBOL",
		"bad/symbol",
		"TSLA",
		"cqssymbol",
		"BRK.B",
	}

	out := dedupeSymbols(in)
	want := []string{"AAPL", "MSFT", "TSLA", "BRK.B"}
	if len(out) != len(want) {
		t.Fatalf("expected %d symbols, got %d (%v)", len(want), len(out), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("unexpected symbol at %d: want=%s got=%s", i, want[i], out[i])
		}
	}
}

func TestCountCSVDataRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.csv")
	data := "timestamp,open,high,low,close,volume\n1,1,1,1,1,10\n2,1,1,1,1,11\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rows, err := countCSVDataRows(path)
	if err != nil {
		t.Fatalf("countCSVDataRows failed: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}
}
