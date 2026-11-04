//go:build windows

package filehandler

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Windows ignores the read-only attribute on directories: files can still be
// created in them, so they must report W. Read-only files must not.
func TestAccessBitsReadOnlyAttribute(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fonts")
	file := filepath.Join(t.TempDir(), "a.ttf")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, file} {
		name, err := syscall.UTF16PtrFromString(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.SetFileAttributes(name, syscall.FILE_ATTRIBUTE_READONLY); err != nil {
			t.Fatal(err)
		}
		defer syscall.SetFileAttributes(name, syscall.FILE_ATTRIBUTE_NORMAL)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if accessBits(dir, di)&protocol.FioAccessW == 0 {
		t.Error("read-only directory reported as not writable")
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if accessBits(file, fi)&protocol.FioAccessW != 0 {
		t.Error("read-only file reported as writable")
	}
}
