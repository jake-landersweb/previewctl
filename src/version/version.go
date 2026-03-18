package version

import "runtime/debug"

// Version is set via ldflags at build time.
var Version = "dev"

// Get returns the build version. If Version was not set via ldflags,
// it falls back to the module version from runtime/debug.ReadBuildInfo
// (populated automatically by go install ...@vX.Y.Z).
func Get() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}
