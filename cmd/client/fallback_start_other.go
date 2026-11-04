//go:build !windows

package main

import (
	"os/exec"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
)

// startFallbackChild starts the local ffmpeg. On Linux the kernel kills it
// (Pdeathsig) if this client is killed without forwarding a signal to it
// (SIGKILL, OOM killer, a crash). The client exits right after its child, so
// the thread that started it can stay parked until then (nil exited).
func startFallbackChild(cmd *exec.Cmd) error {
	return process.StartTied(cmd, nil)
}
