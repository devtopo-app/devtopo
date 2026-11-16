//go:build !windows && !darwin && !linux

package host

// Snapshot lists no processes off Windows, macOS and Linux, so every session reads as closed.
func Snapshot() ([]Proc, error) { return nil, nil }
