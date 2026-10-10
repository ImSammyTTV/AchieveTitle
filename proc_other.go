//go:build !windows

package main

import "os/exec"

// hideWindow only matters on Windows.
func hideWindow(cmd *exec.Cmd) *exec.Cmd { return cmd }
