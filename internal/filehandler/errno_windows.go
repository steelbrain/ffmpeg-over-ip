//go:build windows

package filehandler

import (
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Windows error codes that have no fs sentinel (the sentinels cover not
// found, exists and access denied). Sharing and lock violations map to
// EACCES, as the C runtime maps them for ffmpeg's own file calls.
const (
	errorPathNotFound       = syscall.Errno(3)
	errorInvalidHandle      = syscall.Errno(6)
	errorWriteProtect       = syscall.Errno(19)
	errorNotSameDevice      = syscall.Errno(17)
	errorSharingViolation   = syscall.Errno(32)
	errorLockViolation      = syscall.Errno(33)
	errorHandleDiskFull     = syscall.Errno(39)
	errorBrokenPipe         = syscall.Errno(109)
	errorDiskFull           = syscall.Errno(112)
	errorNegativeSeek       = syscall.Errno(131)
	errorSeekOnDevice       = syscall.Errno(132)
	errorDirNotEmpty        = syscall.Errno(145)
	errorBusy               = syscall.Errno(170)
	errorFilenameExcedRange = syscall.Errno(206)
	errorFileTooLarge       = syscall.Errno(223)
	errorNoData             = syscall.Errno(232)
	errorDirectory          = syscall.Errno(267)
	errorDiskQuotaExceeded  = syscall.Errno(1295)
	errorCantResolveName    = syscall.Errno(1921)
)

func mapPlatformErrno(errno syscall.Errno) (int32, bool) {
	switch errno {
	case errorPathNotFound:
		// A directory on the way to the file doesn't exist: ENOENT, as on
		// POSIX (Go names this code syscall.ENOTDIR on Windows).
		return protocol.FioENOENT, true
	case errorDiskFull, errorHandleDiskFull:
		return protocol.FioENOSPC, true
	case errorSharingViolation, errorLockViolation:
		return protocol.FioEACCES, true
	case errorDirNotEmpty:
		return protocol.FioENOTEMPTY, true
	case errorNotSameDevice:
		return protocol.FioEXDEV, true
	case errorFilenameExcedRange:
		return protocol.FioENAMETOOLONG, true
	case errorCantResolveName:
		return protocol.FioELOOP, true
	case errorDirectory:
		return protocol.FioENOTDIR, true
	case errorWriteProtect:
		return protocol.FioEROFS, true
	case errorBusy:
		return protocol.FioEBUSY, true
	case errorInvalidHandle:
		return protocol.FioEBADF, true
	case errorBrokenPipe, errorNoData:
		return protocol.FioEPIPE, true
	case errorNegativeSeek:
		return protocol.FioEINVAL, true
	case errorSeekOnDevice:
		return protocol.FioESPIPE, true
	case errorFileTooLarge:
		return protocol.FioEFBIG, true
	case errorDiskQuotaExceeded:
		return protocol.FioEDQUOT, true
	}
	return 0, false
}
