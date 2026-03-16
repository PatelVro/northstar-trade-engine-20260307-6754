package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestApplyRuntimeSettingsUsesVCSMetadataWhenUnset(t *testing.T) {
	info := applyRuntimeSettings(Info{}, &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.time", Value: "2026-03-15T13:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	})

	normalized := normalize(info)
	if normalized.Commit != "abcdef1234567890" {
		t.Fatalf("expected runtime commit, got %q", normalized.Commit)
	}
	if normalized.BuildTime != "2026-03-15T13:00:00Z" {
		t.Fatalf("expected runtime build time, got %q", normalized.BuildTime)
	}
	if normalized.Dirty != "dirty" {
		t.Fatalf("expected dirty build state, got %q", normalized.Dirty)
	}
	if normalized.Channel != "local" {
		t.Fatalf("expected local channel fallback, got %q", normalized.Channel)
	}
	if normalized.Version != "dev" {
		t.Fatalf("expected dev version fallback, got %q", normalized.Version)
	}
}

func TestApplyRuntimeSettingsPreservesExplicitLdflags(t *testing.T) {
	info := applyRuntimeSettings(Info{
		Version:   "v1.2.3",
		Commit:    "release-commit",
		BuildTime: "2026-03-15T15:00:00Z",
		Channel:   "release",
		Dirty:     "clean",
	}, &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.time", Value: "2026-03-15T13:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	})

	if info.Version != "v1.2.3" {
		t.Fatalf("expected ldflags version to win, got %q", info.Version)
	}
	if info.Commit != "release-commit" {
		t.Fatalf("expected ldflags commit to win, got %q", info.Commit)
	}
	if info.BuildTime != "2026-03-15T15:00:00Z" {
		t.Fatalf("expected ldflags build time to win, got %q", info.BuildTime)
	}
	if info.Channel != "release" {
		t.Fatalf("expected ldflags channel to win, got %q", info.Channel)
	}
	if info.Dirty != "clean" {
		t.Fatalf("expected ldflags dirty state to win, got %q", info.Dirty)
	}
}
