package process

import "os/exec"

// StartTied starts cmd so that the operating system ends it if this process
// dies without stopping it: Pdeathsig on Linux, a kill-on-close job object on
// Windows, a plain start elsewhere. exited must be closed once cmd has exited
// (on Linux the starting OS thread stays parked until then); a nil exited
// keeps that thread parked for the life of this process, which suits a
// process that exits right after its child.
func StartTied(cmd *exec.Cmd, exited <-chan struct{}) error {
	return startCmd(cmd, exited)
}
