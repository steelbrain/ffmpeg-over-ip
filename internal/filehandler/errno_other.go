//go:build !windows

package filehandler

import "syscall"

// On Unix the errno switch in mapErrno covers everything.
func mapPlatformErrno(syscall.Errno) (int32, bool) { return 0, false }
