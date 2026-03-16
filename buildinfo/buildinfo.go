package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var (
	Version   string
	Commit    string
	BuildTime string
	Channel   string
	Dirty     string
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Channel   string `json:"channel"`
	Dirty     string `json:"dirty"`
}

func Current() Info {
	info := Info{
		Version:   strings.TrimSpace(Version),
		Commit:    strings.TrimSpace(Commit),
		BuildTime: strings.TrimSpace(BuildTime),
		Channel:   strings.TrimSpace(Channel),
		Dirty:     strings.TrimSpace(Dirty),
	}

	if runtimeInfo, ok := debug.ReadBuildInfo(); ok {
		info = applyRuntimeSettings(info, runtimeInfo)
	}

	return normalize(info)
}

func applyRuntimeSettings(info Info, runtimeInfo *debug.BuildInfo) Info {
	if runtimeInfo == nil {
		return info
	}

	if shouldFillVersion(info.Version) {
		if runtimeVersion := strings.TrimSpace(runtimeInfo.Main.Version); runtimeVersion != "" && runtimeVersion != "(devel)" {
			info.Version = runtimeVersion
		}
	}

	for _, setting := range runtimeInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			if shouldFillCommit(info.Commit) {
				info.Commit = strings.TrimSpace(setting.Value)
			}
		case "vcs.time":
			if shouldFillBuildTime(info.BuildTime) {
				info.BuildTime = strings.TrimSpace(setting.Value)
			}
		case "vcs.modified":
			if shouldFillDirty(info.Dirty) {
				switch strings.TrimSpace(setting.Value) {
				case "true":
					info.Dirty = "dirty"
				case "false":
					info.Dirty = "clean"
				}
			}
		}
	}

	if strings.TrimSpace(info.Channel) == "" {
		if info.Version != "" && info.Version != "dev" {
			info.Channel = "release"
		} else {
			info.Channel = "local"
		}
	}

	return info
}

func normalize(info Info) Info {
	if shouldFillVersion(info.Version) {
		info.Version = "dev"
	}
	if shouldFillCommit(info.Commit) {
		info.Commit = "unknown"
	}
	if shouldFillBuildTime(info.BuildTime) {
		info.BuildTime = "unknown"
	}
	if strings.TrimSpace(info.Channel) == "" {
		info.Channel = "local"
	}
	if shouldFillDirty(info.Dirty) {
		info.Dirty = "unknown"
	}

	return info
}

func shouldFillVersion(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "" || value == "dev" || value == "(devel)"
}

func shouldFillCommit(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "" || value == "unknown"
}

func shouldFillBuildTime(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "" || value == "unknown"
}

func shouldFillDirty(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "" || value == "unknown"
}

func (i Info) ShortCommit() string {
	if len(i.Commit) > 12 {
		return i.Commit[:12]
	}
	return i.Commit
}

func (i Info) Summary() string {
	return fmt.Sprintf(
		"version=%s commit=%s dirty=%s built=%s channel=%s",
		i.Version,
		i.ShortCommit(),
		i.Dirty,
		i.BuildTime,
		i.Channel,
	)
}

func (i Info) Map() map[string]string {
	return map[string]string{
		"version":      i.Version,
		"commit":       i.Commit,
		"commit_short": i.ShortCommit(),
		"build_time":   i.BuildTime,
		"channel":      i.Channel,
		"dirty":        i.Dirty,
		"summary":      i.Summary(),
	}
}
