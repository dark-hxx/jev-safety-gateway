package main

import (
	"runtime"
	"runtime/debug"

	"jev-safety-gateway/internal/admin"
)

// Set by -ldflags at build time (see Dockerfile and scripts/build-local-test.ps1).
// The empty defaults are load bearing: a plain `go build` from a checkout then
// falls back to the VCS stamp Go embeds on its own, so a local build still
// reports a real revision instead of nothing.
var (
	version   = "dev"
	commit    = ""
	buildTime = ""
)

// buildInfo describes this binary to the admin console (GET /api/version).
// Everything reported here is a property of the build, never of the traffic:
// the endpoint that serves it is unauthenticated.
func buildInfo() admin.Info {
	info := admin.Info{
		Version: version,
		Commit:  commit,
		BuiltAt: buildTime,
		Go:      runtime.Version(),
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	// A stamped build wins; the VCS stamp only fills what ldflags left blank.
	// Docker has no .git in its context, so there the flags are the only source.
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = shortRev(s.Value)
			}
		case "vcs.time":
			if info.BuiltAt == "" {
				info.BuiltAt = s.Value
			}
		}
	}
	return info
}

// shortRev trims a full git object name to the seven characters people read.
func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
