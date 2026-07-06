// Package buildinfo holds version metadata stamped at build time.
package buildinfo

// These are set via -ldflags at release build time.
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return Version + " (" + Commit + ", built " + Date + ")"
}
