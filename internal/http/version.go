package httpapp

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

// buildVersion is stamped at build time with -ldflags "-X ...buildVersion=...".
// Left empty it falls back to VCS data the Go toolchain embeds automatically.
var buildVersion string

// BuildInfo identifies the running binary, so a deployment can be checked
// against the repository without guessing.
type BuildInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Modified bool   `json:"modified"`
	Built    string `json:"built"`
	Go       string `json:"go"`
}

func currentBuildInfo() BuildInfo {
	info := BuildInfo{Version: buildVersion}

	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	info.Go = build.GoVersion
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Commit = setting.Value
		case "vcs.time":
			info.Built = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}

	if info.Version == "" {
		if info.Commit != "" {
			info.Version = info.Commit
			if len(info.Version) > 12 {
				info.Version = info.Version[:12]
			}
		} else {
			info.Version = "unknown"
		}
	}

	return info
}

// VersionHTMX reports what build is running. Deliberately outside the admin
// gate so a deployment can be identified without unlocking Settings.
func (h *Handler) VersionHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(currentBuildInfo()); err != nil {
		h.Logger.Error("Failed to encode version", "error", err)
	}
}
