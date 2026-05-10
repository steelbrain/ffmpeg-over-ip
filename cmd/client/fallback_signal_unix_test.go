//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// TestHelperSelfKill is the helper variant for signal-death tests. Like
// TestHelperProcess it gates on GO_WANT_HELPER_PROCESS=1 and reads its
// subcommand from argv after "--". Subcommands:
//
//	self-kill <SIGNAL>  : signal own pid with SIGINT or SIGTERM
func TestHelperSelfKill(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) < 2 || args[0] != "self-kill" {
		fmt.Fprintln(os.Stderr, "self-kill helper: bad args")
		os.Exit(127)
	}
	var sig syscall.Signal
	switch args[1] {
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGTERM":
		sig = syscall.SIGTERM
	default:
		fmt.Fprintf(os.Stderr, "self-kill helper: unknown signal %q\n", args[1])
		os.Exit(127)
	}
	// Send the signal to ourselves and pause so the kernel actually delivers
	// signal-death rather than us racing to exit normally.
	_ = syscall.Kill(os.Getpid(), sig)
	select {} // block forever; signal will tear us down
}

func TestExecChildMapsSIGINTTo130(t *testing.T) {
	withHelperEnv(t)
	code := execChild(os.Args[0], []string{"-test.run=^TestHelperSelfKill$", "--", "self-kill", "SIGINT"})
	if code != 128+int(syscall.SIGINT) {
		t.Errorf("exit code = %d, want %d (128 + SIGINT)", code, 128+int(syscall.SIGINT))
	}
}

func TestExecChildMapsSIGTERMTo143(t *testing.T) {
	withHelperEnv(t)
	code := execChild(os.Args[0], []string{"-test.run=^TestHelperSelfKill$", "--", "self-kill", "SIGTERM"})
	if code != 128+int(syscall.SIGTERM) {
		t.Errorf("exit code = %d, want %d (128 + SIGTERM)", code, 128+int(syscall.SIGTERM))
	}
}
