//go:build windows

package main

import (
	"log"
	"os/exec"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
)

// startFallbackChild starts the local ffmpeg and has Windows terminate it if
// this client exits first. A client that is terminated (taskkill /F, or a
// parent process killing it) runs no cleanup, so without this the local
// ffmpeg would keep running.
func startFallbackChild(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := process.KillOnExit(cmd.Process.Pid); err != nil {
		log.Printf("fallback: warning: %s (pid %d) may outlive this client: %v", cmd.Path, cmd.Process.Pid, err)
	}
	return nil
}
