//go:build !windows

package main

// attachConsole is only needed for the Windows GUI build.
func attachConsole() {}
