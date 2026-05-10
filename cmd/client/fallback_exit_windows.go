//go:build windows

package main

import "os/exec"

// exitCodeFromError returns the child's exit code on Windows. Windows has no
// POSIX-style signal concept for child processes, so signal mapping (128+sig)
// doesn't apply — ExitCode() carries the truth.
func exitCodeFromError(err error) int {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return 1
	}
	return exitErr.ExitCode()
}
