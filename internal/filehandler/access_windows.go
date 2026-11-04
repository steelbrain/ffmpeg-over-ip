//go:build windows

package filehandler

import (
	"io/fs"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// accessBits approximates access(2) on Windows from the mode Go derives from
// the file attributes: everything is readable, and the read-only attribute
// clears the write bits of files. Windows ignores that attribute on
// directories (files can still be created in them; Explorer uses it to mark
// customized folders), so directories are always writable here.
func accessBits(_ string, info fs.FileInfo) uint8 {
	perm := info.Mode().Perm()
	var bits uint8
	if perm&0o444 != 0 {
		bits |= protocol.FioAccessR
	}
	if perm&0o222 != 0 || info.IsDir() {
		bits |= protocol.FioAccessW
	}
	if perm&0o111 != 0 {
		bits |= protocol.FioAccessX
	}
	return bits
}
