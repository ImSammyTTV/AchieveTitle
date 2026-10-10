//go:build darwin && !cgo

package main

// Builds for macOS without cgo (e.g. cross-compiled) have no menu bar icon.
func trayAvailable() bool { return false }

func runTray(a *app) { a.wait() }
