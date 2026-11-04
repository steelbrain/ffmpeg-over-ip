package filehandler

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// A write that fails after writing part of the data reports the part, as
// write(2) does; only a write that wrote nothing is an error.
func TestWriteReplyPartialWrite(t *testing.T) {
	rt, rp, err := writeReply(7, 4, &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC})
	if err != nil || rt != protocol.MsgWriteOk {
		t.Fatalf("partial write: got type 0x%02x, err %v; want WriteOk", rt, err)
	}
	r, derr := protocol.DecodeWriteOkResponse(rp)
	if derr != nil || r.RequestID != 7 || r.BytesWritten != 4 {
		t.Fatalf("partial write reply %+v, %v; want 4 bytes for request 7", r, derr)
	}

	rt, rp, _ = writeReply(8, 0, &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC})
	if rt != protocol.MsgIoError {
		t.Fatalf("failed write: got type 0x%02x, want IoError", rt)
	}
	e, _ := protocol.DecodeIoErrorResponse(rp)
	if e == nil || e.Errno != protocol.FioENOSPC {
		t.Fatalf("failed write reply %+v; want ENOSPC", e)
	}

	rt, rp, _ = writeReply(9, 10, nil)
	r, _ = protocol.DecodeWriteOkResponse(rp)
	if rt != protocol.MsgWriteOk || r.BytesWritten != 10 {
		t.Fatalf("full write: got 0x%02x %+v", rt, r)
	}
}

// OPEN_OK carries the file type, so fio's fstat can tell a pipe from a file.
func TestOpenReportsFileType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.ts")
	if err := os.WriteFile(path, []byte("12345"), 0o640); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	defer h.CloseAll()
	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("open: got 0x%02x", rt)
	}
	r := decodeOpenOk(t, rp)
	if r.FileSize != 5 || r.Mode&protocol.FioSIFMT != protocol.FioSIFREG {
		t.Fatalf("open reply %+v (mode 0%o); want size 5, regular file", r, r.Mode)
	}
	if fi, err := os.Stat(path); err == nil && uint32(fi.Mode().Perm()) != r.Mode&0o777 {
		t.Fatalf("mode 0%o, file has 0%o", r.Mode&0o777, fi.Mode().Perm())
	}
}

// ListDir on something that is not a directory fails with ENOTDIR without
// opening it.
func TestListDirNotADirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	rt, rp := listDir(t, h, path)
	if rt != protocol.MsgIoError {
		t.Fatalf("got 0x%02x, want IoError", rt)
	}
	if e, _ := protocol.DecodeIoErrorResponse(rp); e == nil || e.Errno != protocol.FioENOTDIR {
		t.Fatalf("got %+v, want ENOTDIR", e)
	}
}

// Directories bigger than one ReadDir batch are listed completely.
func TestListDirManyEntries(t *testing.T) {
	dir := t.TempDir()
	const n = listDirBatch*2 + 17
	for i := 0; i < n; i++ {
		f, err := os.Create(filepath.Join(dir, "f"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	h := NewHandler()
	rt, rp := listDir(t, h, dir)
	if rt != protocol.MsgListDirOk {
		t.Fatalf("got 0x%02x", rt)
	}
	r, err := protocol.DecodeListDirOkResponse(rp)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) != n {
		t.Fatalf("listed %d entries, want %d", len(r.Entries), n)
	}
}
