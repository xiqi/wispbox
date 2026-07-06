package lowmem

import (
	"os"
	"runtime/debug"
)

const (
	defaultMemoryLimit = 64 << 20
	defaultGCPercent   = 30
)

// ApplyDefaults keeps wispbox small even when it is launched outside the
// packaged systemd unit. Explicit GOMEMLIMIT/GOGC values still win.
func ApplyDefaults() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(defaultGCPercent)
	}
}
