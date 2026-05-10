//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// exitCodeFromError translates a *exec.ExitError into a shell-style exit code:
// a signal-killed child becomes 128+signum (e.g., 130 for SIGINT, 143 for
// SIGTERM), matching what bash/zsh report. Otherwise the child's own exit
// status is returned. Non-ExitError failures map to 1.
func exitCodeFromError(err error) int {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return 1
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return exitErr.ExitCode()
}
