//go:build !linux

package admin

// systemMemory has no portable implementation off Linux; production targets
// are Linux-only and development shows process memory instead.
func systemMemory() map[string]any { return map[string]any{} }
