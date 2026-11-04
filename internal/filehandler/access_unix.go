//go:build unix

package filehandler

import (
	"io/fs"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// accessBits reports what this process may do with path, as access(2) does.
func accessBits(path string, _ fs.FileInfo) uint8 {
	var bits uint8
	if syscall.Access(path, 0x4) == nil { // R_OK
		bits |= protocol.FioAccessR
	}
	if syscall.Access(path, 0x2) == nil { // W_OK
		bits |= protocol.FioAccessW
	}
	if syscall.Access(path, 0x1) == nil { // X_OK
		bits |= protocol.FioAccessX
	}
	return bits
}
