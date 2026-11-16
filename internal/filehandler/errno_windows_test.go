//go:build windows

package filehandler

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// A missing parent directory is ENOENT, as on POSIX, although Windows
// reports ERROR_PATH_NOT_FOUND, which Go calls syscall.ENOTDIR.
func TestMapErrnoMissingParentIsENOENT(t *testing.T) {
	if got := mapErrno(syscall.Errno(3)); got != protocol.FioENOENT {
		t.Fatalf("ERROR_PATH_NOT_FOUND -> %d, want ENOENT", got)
	}
	_, err := os.Open(filepath.Join(t.TempDir(), "missing-dir", "file"))
	if got := mapErrno(err); got != protocol.FioENOENT {
		t.Fatalf("open under a missing directory: %v -> %d, want ENOENT", err, got)
	}
}

// ERROR_DIRECTORY (a file where a directory was expected) is ENOTDIR.
func TestMapErrnoErrorDirectoryIsENOTDIR(t *testing.T) {
	if got := mapErrno(syscall.Errno(267)); got != protocol.FioENOTDIR {
		t.Fatalf("ERROR_DIRECTORY -> %d, want ENOTDIR", got)
	}
}
