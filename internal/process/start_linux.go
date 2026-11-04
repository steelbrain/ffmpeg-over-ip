//go:build linux

package process

import (
	"os/exec"
	"runtime"
	"syscall"
)

// startCmd starts cmd with Pdeathsig set, so the kernel kills the child if
// the server dies without stopping it (SIGKILL, OOM killer, crash).
//
// Pdeathsig fires when the thread that forked the child exits, not when the
// process does, and the Go runtime may retire threads. The fork therefore
// runs on a goroutine locked to its OS thread, which stays parked until the
// child has exited.
func startCmd(cmd *exec.Cmd, exited <-chan struct{}) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL

	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := cmd.Start()
		errCh <- err
		if err == nil {
			<-exited
		}
	}()
	return <-errCh
}
