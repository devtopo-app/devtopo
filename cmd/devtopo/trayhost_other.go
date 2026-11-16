//go:build windows || (darwin && cgo)

package main

// trayHost reports whether the tray icon can run: on Windows and macOS it always can.
func trayHost() (ok bool, hint string) { return true, "" }
