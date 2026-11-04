//go:build windows

package process

import (
	"log"
	"os/exec"
)

// startCmd starts cmd and ties its lifetime to this process's (see
// KillOnExit), so ffmpeg can't outlive a server that dies without stopping
// it. If that fails, ffmpeg still runs; it just loses the safety net.
func startCmd(cmd *exec.Cmd, _ <-chan struct{}) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := KillOnExit(cmd.Process.Pid); err != nil {
		log.Printf("warning: %s (pid %d) may outlive this process: %v", cmd.Path, cmd.Process.Pid, err)
	}
	return nil
}
