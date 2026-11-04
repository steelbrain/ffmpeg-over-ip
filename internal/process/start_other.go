//go:build !linux && !windows

package process

import "os/exec"

// startCmd starts cmd. Linux (Pdeathsig) and Windows (job objects) have the
// OS kill the child when the server dies; elsewhere the server's loopback
// closing makes the child's next file operation fail instead.
func startCmd(cmd *exec.Cmd, _ <-chan struct{}) error {
	return cmd.Start()
}
