package main

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a helper program (like tasklist) flashing a console
// window, since AchieveTitle itself has none.
func hideWindow(cmd *exec.Cmd) *exec.Cmd {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}
