package filehandler

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// helper: send an encoded request through HandleMessage and return response type + payload
func dispatch(t *testing.T, h *Handler, msgType uint8, payload []byte) (uint8, []byte) {
	t.Helper()
	rt, rp, err := h.HandleMessage(msgType, payload)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	return rt, rp
}

// decode helpers that fail the test on error instead of silently ignoring it
func decodeOpenOk(t *testing.T, rp []byte) *protocol.OpenOkResponse {
	t.Helper()
	r, err := protocol.DecodeOpenOkResponse(rp)
	if err != nil {
		t.Fatalf("DecodeOpenOkResponse: %v", err)
	}
	return r
}

func decodeReadOk(t *testing.T, rp []byte) *protocol.ReadOkResponse {
	t.Helper()
	r, err := protocol.DecodeReadOkResponse(rp)
	if err != nil {
		t.Fatalf("DecodeReadOkResponse: %v", err)
	}
	return r
}

func decodeWriteOk(t *testing.T, rp []byte) *protocol.WriteOkResponse {
	t.Helper()
	r, err := protocol.DecodeWriteOkResponse(rp)
	if err != nil {
		t.Fatalf("DecodeWriteOkResponse: %v", err)
	}
	return r
}

func decodeSeekOk(t *testing.T, rp []byte) *protocol.SeekOkResponse {
	t.Helper()
	r, err := protocol.DecodeSeekOkResponse(rp)
	if err != nil {
		t.Fatalf("DecodeSeekOkResponse: %v", err)
	}
	return r
}

func decodeFstatOk(t *testing.T, rp []byte) *protocol.FstatOkResponse {
	t.Helper()
	r, err := protocol.DecodeFstatOkResponse(rp)
	if err != nil {
		t.Fatalf("DecodeFstatOkResponse: %v", err)
	}
	return r
}

func decodeIoError(t *testing.T, rp []byte) *protocol.IoErrorResponse {
	t.Helper()
	r, err := protocol.DecodeIoErrorResponse(rp)
	if err != nil {
		t.Fatalf("DecodeIoErrorResponse: %v", err)
	}
	return r
}

func TestOpenReadClose(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, content, 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Open
	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 10, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("expected MsgOpenOk, got 0x%02x", rt)
	}
	resp := decodeOpenOk(t, rp)
	if resp.FileSize != int64(len(content)) {
		t.Fatalf("expected file size %d, got %d", len(content), resp.FileSize)
	}

	// Read
	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 2, FileID: 10, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != string(content) {
		t.Fatalf("expected %q, got %q", content, rr.Data)
	}

	// Close
	rt, _ = dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 3, FileID: 10,
	}).Encode())
	if rt != protocol.MsgCloseOk {
		t.Fatalf("expected MsgCloseOk, got 0x%02x", rt)
	}
}

func TestOpenWriteReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	h := NewHandler()
	defer h.CloseAll()

	// Open for write+create
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	// Write
	data := []byte("test data 123")
	rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: data,
	}).Encode())
	if rt != protocol.MsgWriteOk {
		t.Fatalf("expected MsgWriteOk, got 0x%02x", rt)
	}
	wr := decodeWriteOk(t, rp)
	if wr.BytesWritten != uint32(len(data)) {
		t.Fatalf("expected %d bytes written, got %d", len(data), wr.BytesWritten)
	}

	// Close
	dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{RequestID: 3, FileID: 1}).Encode())

	// Reopen for read
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 4, FileID: 2, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Read back
	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 5, FileID: 2, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != string(data) {
		t.Fatalf("expected %q, got %q", data, rr.Data)
	}
}

func TestOpenCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.txt")

	h := NewHandler()
	defer h.CloseAll()

	rt, _ := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioOWRONLY | protocol.FioOCREAT,
		Mode: 0o644, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("expected MsgOpenOk, got 0x%02x", rt)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}
}

func TestReadEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte("hi"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// First read gets content
	dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 2, FileID: 1, NBytes: 1024,
	}).Encode())

	// Second read at EOF returns empty
	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 3, FileID: 1, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if len(rr.Data) != 0 {
		t.Fatalf("expected empty data at EOF, got %d bytes", len(rr.Data))
	}
}

func TestSeekSetCurEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seek.txt")
	os.WriteFile(path, []byte("0123456789"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// SeekSet to position 5
	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 5, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	sr := decodeSeekOk(t, rp)
	if sr.Offset != 5 {
		t.Fatalf("expected offset 5, got %d", sr.Offset)
	}

	// SeekCur +2 → 7
	rt, rp = dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 3, FileID: 1, Offset: 2, Whence: protocol.FioSeekCur,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	sr = decodeSeekOk(t, rp)
	if sr.Offset != 7 {
		t.Fatalf("expected offset 7, got %d", sr.Offset)
	}

	// SeekEnd -3 → 7
	rt, rp = dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 4, FileID: 1, Offset: -3, Whence: protocol.FioSeekEnd,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	sr = decodeSeekOk(t, rp)
	if sr.Offset != 7 {
		t.Fatalf("expected offset 7, got %d", sr.Offset)
	}
}

func TestFstat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 2, FileID: 1,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fr := decodeFstatOk(t, rp)
	if fr.FileSize != 5 {
		t.Fatalf("expected size 5, got %d", fr.FileSize)
	}
	if fr.Mode&0o777 != 0o644 {
		t.Fatalf("expected mode 0644, got 0%o", fr.Mode&0o777)
	}
}

func TestFtruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR, Path: path,
	}).Encode())

	rt, _ := dispatch(t, h, protocol.MsgFtruncate, (&protocol.FtruncateRequest{
		RequestID: 2, FileID: 1, Length: 5,
	}).Encode())
	if rt != protocol.MsgFtruncateOk {
		t.Fatalf("expected MsgFtruncateOk, got 0x%02x", rt)
	}

	// Verify via fstat
	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 3, FileID: 1,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fr := decodeFstatOk(t, rp)
	if fr.FileSize != 5 {
		t.Fatalf("expected size 5 after truncate, got %d", fr.FileSize)
	}
}

func TestUnlinkFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.txt")
	os.WriteFile(path, []byte("bye"), 0o644)

	h := NewHandler()

	rt, _ := dispatch(t, h, protocol.MsgUnlink, (&protocol.UnlinkRequest{
		RequestID: 1, Path: path,
	}).Encode())
	if rt != protocol.MsgUnlinkOk {
		t.Fatalf("expected MsgUnlinkOk, got 0x%02x", rt)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be gone after unlink")
	}
}

func TestUnlinkDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "emptydir")
	os.Mkdir(subdir, 0o755)

	h := NewHandler()

	rt, _ := dispatch(t, h, protocol.MsgUnlink, (&protocol.UnlinkRequest{
		RequestID: 1, Path: subdir,
	}).Encode())
	if rt != protocol.MsgUnlinkOk {
		t.Fatalf("expected MsgUnlinkOk, got 0x%02x", rt)
	}

	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Fatal("directory should be gone after unlink")
	}
}

func TestRename(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	os.WriteFile(oldPath, []byte("data"), 0o644)

	h := NewHandler()

	rt, _ := dispatch(t, h, protocol.MsgRename, (&protocol.RenameRequest{
		RequestID: 1, OldPath: oldPath, NewPath: newPath,
	}).Encode())
	if rt != protocol.MsgRenameOk {
		t.Fatalf("expected MsgRenameOk, got 0x%02x", rt)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old path should not exist")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatal("new path should exist")
	}
}

func TestMkdir(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "subdir")

	h := NewHandler()

	rt, _ := dispatch(t, h, protocol.MsgMkdir, (&protocol.MkdirRequest{
		RequestID: 1, Mode: 0o755, Path: newDir,
	}).Encode())
	if rt != protocol.MsgMkdirOk {
		t.Fatalf("expected MsgMkdirOk, got 0x%02x", rt)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestMkdirExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	os.Mkdir(existing, 0o755)

	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgMkdir, (&protocol.MkdirRequest{
		RequestID: 1, Mode: 0o755, Path: existing,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for existing dir, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEEXIST {
		t.Fatalf("expected EEXIST, got %d", er.Errno)
	}
}

func TestMkdirNested(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")

	h := NewHandler()

	// Mkdir should fail for nested paths when parent doesn't exist (POSIX mkdir semantics)
	rt, _ := dispatch(t, h, protocol.MsgMkdir, (&protocol.MkdirRequest{
		RequestID: 1, Mode: 0o755, Path: nested,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for nested mkdir, got 0x%02x", rt)
	}
}

// --- Error cases ---

func TestOpenNonExistent(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: "/nonexistent/path/xyz",
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioENOENT {
		t.Fatalf("expected ENOENT (%d), got %d", protocol.FioENOENT, er.Errno)
	}
}

func TestReadInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 1, FileID: 999, NBytes: 10,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

func TestCloseRemovesFromMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("x"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 2, FileID: 1,
	}).Encode())

	// Read with closed fileID should fail
	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 3, FileID: 1, NBytes: 10,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after close, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestUnknownMessageType(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(0xFF, []byte{})
	if err == nil {
		t.Fatal("expected error for unknown message type")
	}
}

func TestOpenDuplicateFileID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	os.WriteFile(path, []byte("x"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Second open with same fileID
	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 2, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for duplicate fileID, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestReadZeroBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zero.txt")
	os.WriteFile(path, []byte("content"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 2, FileID: 1, NBytes: 0,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if len(rr.Data) != 0 {
		t.Fatalf("expected empty data, got %d bytes", len(rr.Data))
	}
}

func TestWriteEmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioOWRONLY | protocol.FioOCREAT,
		Mode: 0o644, Path: path,
	}).Encode())

	rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: []byte{},
	}).Encode())
	if rt != protocol.MsgWriteOk {
		t.Fatalf("expected MsgWriteOk, got 0x%02x", rt)
	}
	wr := decodeWriteOk(t, rp)
	if wr.BytesWritten != 0 {
		t.Fatalf("expected 0 bytes written, got %d", wr.BytesWritten)
	}
}

func TestSeekInvalidWhence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seek.txt")
	os.WriteFile(path, []byte("x"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 0, Whence: 99,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for invalid whence, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

// --- Helper tests ---

func TestFlagTranslation(t *testing.T) {
	tests := []struct {
		wire     uint32
		expected int
	}{
		{protocol.FioORDONLY, os.O_RDONLY},
		{protocol.FioOWRONLY, os.O_WRONLY},
		{protocol.FioORDWR, os.O_RDWR},
		{protocol.FioOWRONLY | protocol.FioOCREAT, os.O_WRONLY | os.O_CREATE},
		{protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC, os.O_WRONLY | os.O_CREATE | os.O_TRUNC},
		{protocol.FioORDWR | protocol.FioOTRUNC, os.O_RDWR | os.O_TRUNC},
	}
	for _, tc := range tests {
		got := wireToOSFlags(tc.wire)
		if got != tc.expected {
			t.Errorf("wireToOSFlags(0x%04x) = 0x%x, want 0x%x", tc.wire, got, tc.expected)
		}
	}
}

func TestErrnoMapping(t *testing.T) {
	tests := []struct {
		err      error
		expected int32
	}{
		{syscall.EPERM, protocol.FioEPERM},
		{syscall.ENOENT, protocol.FioENOENT},
		{syscall.EIO, protocol.FioEIO},
		{syscall.EACCES, protocol.FioEACCES},
		{syscall.EEXIST, protocol.FioEEXIST},
		{syscall.ENOTDIR, protocol.FioENOTDIR},
		{syscall.EISDIR, protocol.FioEISDIR},
		{syscall.EINVAL, protocol.FioEINVAL},
		{syscall.ENOSPC, protocol.FioENOSPC},
		{syscall.EROFS, protocol.FioEROFS},
		{syscall.ERANGE, protocol.FioERANGE},
		// PathError wrapping
		{&os.PathError{Op: "open", Path: "/x", Err: syscall.ENOENT}, protocol.FioENOENT},
		// Unknown → EIO fallback
		{io.ErrUnexpectedEOF, protocol.FioEIO},
	}
	for _, tc := range tests {
		got := mapErrno(tc.err)
		if got != tc.expected {
			t.Errorf("mapErrno(%v) = %d, want %d", tc.err, got, tc.expected)
		}
	}
}

func TestCloseAll(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()

	for i := uint16(0); i < 5; i++ {
		path := filepath.Join(dir, string(rune('a'+i))+".txt")
		os.WriteFile(path, []byte("x"), 0o644)
		dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
			RequestID: uint16(i), FileID: i, Flags: protocol.FioORDONLY, Path: path,
		}).Encode())
	}

	h.CloseAll()

	// All fileIDs should now be invalid
	for i := uint16(0); i < 5; i++ {
		rt, _ := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
			RequestID: 100, FileID: i, NBytes: 1,
		}).Encode())
		if rt != protocol.MsgIoError {
			t.Fatalf("expected MsgIoError for fileID %d after CloseAll, got 0x%02x", i, rt)
		}
	}
}

func TestFullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.txt")
	content := []byte("round trip test content")
	os.WriteFile(path, content, 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Encode request
	openReq := &protocol.OpenRequest{
		RequestID: 42, FileID: 7, Flags: protocol.FioORDONLY, Path: path,
	}
	encoded := openReq.Encode()

	// HandleMessage
	rt, rp := dispatch(t, h, protocol.MsgOpen, encoded)
	if rt != protocol.MsgOpenOk {
		t.Fatalf("expected MsgOpenOk, got 0x%02x", rt)
	}

	// Decode response
	resp := decodeOpenOk(t, rp)
	if resp.RequestID != 42 {
		t.Fatalf("expected requestID 42, got %d", resp.RequestID)
	}
	if resp.FileSize != int64(len(content)) {
		t.Fatalf("expected file size %d, got %d", len(content), resp.FileSize)
	}
}

// --- Invalid file ID tests ---

func TestWriteInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 1, FileID: 999, Data: []byte("data"),
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

func TestSeekInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 1, FileID: 999, Offset: 0, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

func TestCloseInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 1, FileID: 999,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

func TestFstatInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 1, FileID: 999,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

func TestFtruncateInvalidFileID(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgFtruncate, (&protocol.FtruncateRequest{
		RequestID: 1, FileID: 999, Length: 10,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL (%d), got %d", protocol.FioEINVAL, er.Errno)
	}
}

// --- Non-existent path tests ---

func TestUnlinkNonExistent(t *testing.T) {
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgUnlink, (&protocol.UnlinkRequest{
		RequestID: 1, Path: "/nonexistent/path/xyz.txt",
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioENOENT {
		t.Fatalf("expected ENOENT (%d), got %d", protocol.FioENOENT, er.Errno)
	}
}

func TestRenameNonExistent(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()

	rt, rp := dispatch(t, h, protocol.MsgRename, (&protocol.RenameRequest{
		RequestID: 1, OldPath: filepath.Join(dir, "nonexistent.txt"), NewPath: filepath.Join(dir, "dest.txt"),
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioENOENT {
		t.Fatalf("expected ENOENT (%d), got %d", protocol.FioENOENT, er.Errno)
	}
}

// --- Data integrity tests ---

func TestLargeFileWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")

	h := NewHandler()
	defer h.CloseAll()

	// Open for write
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	// Write 1MB of data
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 251) // use a prime to avoid trivial patterns
	}
	rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: data,
	}).Encode())
	if rt != protocol.MsgWriteOk {
		t.Fatalf("expected MsgWriteOk, got 0x%02x", rt)
	}
	wr := decodeWriteOk(t, rp)
	if wr.BytesWritten != uint32(len(data)) {
		t.Fatalf("expected %d bytes written, got %d", len(data), wr.BytesWritten)
	}

	// Close
	dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{RequestID: 3, FileID: 1}).Encode())

	// Reopen for read
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 4, FileID: 2, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Read back entire file
	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 5, FileID: 2, NBytes: uint32(len(data) + 1),
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if len(rr.Data) != len(data) {
		t.Fatalf("expected %d bytes, got %d", len(data), len(rr.Data))
	}
	for i := range data {
		if rr.Data[i] != data[i] {
			t.Fatalf("data mismatch at byte %d: expected 0x%02x, got 0x%02x", i, data[i], rr.Data[i])
		}
	}
}

func TestMultipleFilesSimultaneous(t *testing.T) {
	dir := t.TempDir()

	h := NewHandler()
	defer h.CloseAll()

	contents := make([]string, 5)
	paths := make([]string, 5)
	for i := 0; i < 5; i++ {
		contents[i] = fmt.Sprintf("content-for-file-%d", i)
		paths[i] = filepath.Join(dir, fmt.Sprintf("file%d.txt", i))

		// Open each file for writing
		dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
			RequestID: uint16(i*10 + 1), FileID: uint16(i + 1),
			Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
			Mode: 0o644, Path: paths[i],
		}).Encode())

		// Write content
		dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
			RequestID: uint16(i*10 + 2), FileID: uint16(i + 1), Data: []byte(contents[i]),
		}).Encode())
	}

	// Close all files
	for i := 0; i < 5; i++ {
		dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
			RequestID: uint16(i*10 + 3), FileID: uint16(i + 1),
		}).Encode())
	}

	// Reopen all for reading with new fileIDs
	for i := 0; i < 5; i++ {
		dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
			RequestID: uint16(i*10 + 4), FileID: uint16(i + 100),
			Flags: protocol.FioORDONLY, Path: paths[i],
		}).Encode())
	}

	// Read back from each and verify
	for i := 0; i < 5; i++ {
		rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
			RequestID: uint16(i*10 + 5), FileID: uint16(i + 100), NBytes: 1024,
		}).Encode())
		if rt != protocol.MsgReadOk {
			t.Fatalf("file %d: expected MsgReadOk, got 0x%02x", i, rt)
		}
		rr := decodeReadOk(t, rp)
		if string(rr.Data) != contents[i] {
			t.Fatalf("file %d: expected %q, got %q", i, contents[i], string(rr.Data))
		}
	}
}

func TestSeekAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seekread.txt")
	os.WriteFile(path, []byte("0123456789"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Seek to position 5
	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 5, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	sr := decodeSeekOk(t, rp)
	if sr.Offset != 5 {
		t.Fatalf("expected offset 5, got %d", sr.Offset)
	}

	// Read 3 bytes → "567"
	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 3, FileID: 1, NBytes: 3,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != "567" {
		t.Fatalf("expected %q, got %q", "567", string(rr.Data))
	}
}

func TestFstatAfterWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "statwrite.txt")

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	data := []byte("some test data for fstat")
	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: data,
	}).Encode())

	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 3, FileID: 1,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fr := decodeFstatOk(t, rp)
	if fr.FileSize != int64(len(data)) {
		t.Fatalf("expected size %d, got %d", len(data), fr.FileSize)
	}
}

func TestFtruncateExtend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extend.txt")
	os.WriteFile(path, []byte("short"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR, Path: path,
	}).Encode())

	// Truncate to larger size
	rt, _ := dispatch(t, h, protocol.MsgFtruncate, (&protocol.FtruncateRequest{
		RequestID: 2, FileID: 1, Length: 1024,
	}).Encode())
	if rt != protocol.MsgFtruncateOk {
		t.Fatalf("expected MsgFtruncateOk, got 0x%02x", rt)
	}

	// Verify via fstat
	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 3, FileID: 1,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fr := decodeFstatOk(t, rp)
	if fr.FileSize != 1024 {
		t.Fatalf("expected size 1024 after truncate extend, got %d", fr.FileSize)
	}
}

func TestCloseAndReopenSameFileID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.txt")
	os.WriteFile(path, []byte("reopen test"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Open with fileID 1
	rt, _ := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("first open: expected MsgOpenOk, got 0x%02x", rt)
	}

	// Close fileID 1
	rt, _ = dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 2, FileID: 1,
	}).Encode())
	if rt != protocol.MsgCloseOk {
		t.Fatalf("close: expected MsgCloseOk, got 0x%02x", rt)
	}

	// Reopen with same fileID 1
	rt, _ = dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 3, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("reopen: expected MsgOpenOk, got 0x%02x", rt)
	}

	// Read should work
	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 4, FileID: 1, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("read after reopen: expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != "reopen test" {
		t.Fatalf("expected %q, got %q", "reopen test", string(rr.Data))
	}
}

func TestRenameOverwrite(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	os.WriteFile(pathA, []byte("content-a"), 0o644)
	os.WriteFile(pathB, []byte("content-b"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Rename a.txt over b.txt
	rt, _ := dispatch(t, h, protocol.MsgRename, (&protocol.RenameRequest{
		RequestID: 1, OldPath: pathA, NewPath: pathB,
	}).Encode())
	if rt != protocol.MsgRenameOk {
		t.Fatalf("expected MsgRenameOk, got 0x%02x", rt)
	}

	// a.txt should be gone
	if _, err := os.Stat(pathA); !os.IsNotExist(err) {
		t.Fatal("old path should not exist after rename")
	}

	// b.txt should have a's content
	got, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if string(got) != "content-a" {
		t.Fatalf("expected dest to have %q, got %q", "content-a", string(got))
	}
}

func TestOpenRDWR(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rdwr.txt")

	h := NewHandler()
	defer h.CloseAll()

	// Open with O_RDWR | O_CREAT | O_TRUNC
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	// Write data
	data := []byte("read-write test")
	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: data,
	}).Encode())

	// Seek back to start
	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 3, FileID: 1, Offset: 0, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	sr := decodeSeekOk(t, rp)
	if sr.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", sr.Offset)
	}

	// Read back
	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 4, FileID: 1, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != string(data) {
		t.Fatalf("expected %q, got %q", data, rr.Data)
	}
}

func TestHandleMessageConcurrent(t *testing.T) {
	dir := t.TempDir()

	h := NewHandler()
	defer h.CloseAll()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fileID := uint16(id + 1)
			reqBase := uint16(id * 100)
			path := filepath.Join(dir, fmt.Sprintf("concurrent-%d.txt", id))
			content := fmt.Sprintf("data-from-goroutine-%d", id)

			// Open
			rt, _ := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
				RequestID: reqBase + 1, FileID: fileID,
				Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
				Mode: 0o644, Path: path,
			}).Encode())
			if rt != protocol.MsgOpenOk {
				t.Errorf("goroutine %d: open expected MsgOpenOk, got 0x%02x", id, rt)
				return
			}

			// Write
			rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
				RequestID: reqBase + 2, FileID: fileID, Data: []byte(content),
			}).Encode())
			if rt != protocol.MsgWriteOk {
				t.Errorf("goroutine %d: write expected MsgWriteOk, got 0x%02x", id, rt)
				return
			}
			wr := decodeWriteOk(t, rp)
			if wr.BytesWritten != uint32(len(content)) {
				t.Errorf("goroutine %d: expected %d bytes written, got %d", id, len(content), wr.BytesWritten)
				return
			}

			// Close
			rt, _ = dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
				RequestID: reqBase + 3, FileID: fileID,
			}).Encode())
			if rt != protocol.MsgCloseOk {
				t.Errorf("goroutine %d: close expected MsgCloseOk, got 0x%02x", id, rt)
				return
			}

			// Reopen for read with a different fileID
			readFileID := uint16(id + 100)
			rt, _ = dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
				RequestID: reqBase + 4, FileID: readFileID,
				Flags: protocol.FioORDONLY, Path: path,
			}).Encode())
			if rt != protocol.MsgOpenOk {
				t.Errorf("goroutine %d: reopen expected MsgOpenOk, got 0x%02x", id, rt)
				return
			}

			// Read back
			rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
				RequestID: reqBase + 5, FileID: readFileID, NBytes: 1024,
			}).Encode())
			if rt != protocol.MsgReadOk {
				t.Errorf("goroutine %d: read expected MsgReadOk, got 0x%02x", id, rt)
				return
			}
			rr := decodeReadOk(t, rp)
			if string(rr.Data) != content {
				t.Errorf("goroutine %d: expected %q, got %q", id, content, string(rr.Data))
				return
			}

			// Close read handle
			dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
				RequestID: reqBase + 6, FileID: readFileID,
			}).Encode())
		}(i)
	}
	wg.Wait()
}

// --- Malformed payload tests ---

func TestMalformedOpenPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgOpen, []byte{0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedReadPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgRead, []byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedWritePayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgWrite, []byte{0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedSeekPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgSeek, []byte{0x00, 0x01, 0x02, 0x03, 0x04})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedClosePayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgClose, []byte{0x00})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedFstatPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgFstat, []byte{0x00})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedFtruncatePayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgFtruncate, []byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedUnlinkPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgUnlink, []byte{})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedRenamePayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgRename, []byte{0x00})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestMalformedMkdirPayload(t *testing.T) {
	h := NewHandler()
	_, _, err := h.HandleMessage(protocol.MsgMkdir, []byte{0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

// --- Filesystem edge-case tests ---

func TestWriteToReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Open read-only
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Write should fail with MsgIoError
	rt, _, err := h.HandleMessage(protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: []byte("attempt"),
	}).Encode())
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
}

func TestFstatOnDeletedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todelete.txt")
	os.WriteFile(path, []byte("ephemeral"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Open the file
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Delete the file from disk while it's still open
	os.Remove(path)

	// Fstat on the still-open fd should succeed on Unix (open fd keeps inode alive)
	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 2, FileID: 1,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk on deleted-but-open file, got 0x%02x", rt)
	}
	fr := decodeFstatOk(t, rp)
	if fr.FileSize != int64(len("ephemeral")) {
		t.Fatalf("expected size %d, got %d", len("ephemeral"), fr.FileSize)
	}
}

func TestCloseAllThenWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closeall-write.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR, Path: path,
	}).Encode())

	h.CloseAll()

	rt, rp := dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: []byte("nope"),
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after CloseAll, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestCloseAllThenSeek(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closeall-seek.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	h.CloseAll()

	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 0, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after CloseAll, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestCloseAllThenFstat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closeall-fstat.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	h.CloseAll()

	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 2, FileID: 1,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after CloseAll, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestCloseAllThenFtruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closeall-ftrunc.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR, Path: path,
	}).Encode())

	h.CloseAll()

	rt, rp := dispatch(t, h, protocol.MsgFtruncate, (&protocol.FtruncateRequest{
		RequestID: 2, FileID: 1, Length: 2,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after CloseAll, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestCloseAllThenClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closeall-close.txt")
	os.WriteFile(path, []byte("data"), 0o644)

	h := NewHandler()

	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	h.CloseAll()

	rt, rp := dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 2, FileID: 1,
	}).Encode())
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError after CloseAll, got 0x%02x", rt)
	}
	er := decodeIoError(t, rp)
	if er.Errno != protocol.FioEINVAL {
		t.Fatalf("expected EINVAL, got %d", er.Errno)
	}
}

func TestReadBeyondEOFReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beyondeof.txt")

	h := NewHandler()
	defer h.CloseAll()

	// Open for write+create, write 5 bytes
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: []byte("hello"),
	}).Encode())

	// Seek to end (offset 5)
	dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 3, FileID: 1, Offset: 0, Whence: protocol.FioSeekEnd,
	}).Encode())

	// Read at EOF should return empty data, not an error
	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 4, FileID: 1, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if len(rr.Data) != 0 {
		t.Fatalf("expected empty data beyond EOF, got %d bytes", len(rr.Data))
	}
}

func TestMultipleSequentialWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seqwrite.txt")

	h := NewHandler()
	defer h.CloseAll()

	// Open for write
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
		Mode: 0o644, Path: path,
	}).Encode())

	// Write "aaa"
	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 1, Data: []byte("aaa"),
	}).Encode())

	// Write "bbb"
	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 3, FileID: 1, Data: []byte("bbb"),
	}).Encode())

	// Close
	dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 4, FileID: 1,
	}).Encode())

	// Reopen for read
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 5, FileID: 2, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Read back → "aaabbb"
	rt, rp := dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 6, FileID: 2, NBytes: 1024,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	rr := decodeReadOk(t, rp)
	if string(rr.Data) != "aaabbb" {
		t.Fatalf("expected %q, got %q", "aaabbb", string(rr.Data))
	}
}

func TestOpenReadOnlyWithTrunc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rdonly-trunc.txt")
	os.WriteFile(path, []byte("original"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	// Open with O_RDONLY | O_TRUNC — should not crash regardless of outcome
	rt, _, err := h.HandleMessage(protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY | protocol.FioOTRUNC, Path: path,
	}).Encode())
	// We accept either success (MsgOpenOk) or an IO error (MsgIoError), but never a crash or Go error
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if rt != protocol.MsgOpenOk && rt != protocol.MsgIoError {
		t.Fatalf("expected MsgOpenOk or MsgIoError, got 0x%02x", rt)
	}
}

func TestWireToOSFlagsUnknownBits(t *testing.T) {
	// 0x8000 has no known flag bits set for access mode (bits 0-1 are 0 = RDONLY).
	// wireToOSFlags should ignore unknown bits and return O_RDONLY.
	flags := wireToOSFlags(0x8000)
	if flags != os.O_RDONLY {
		t.Fatalf("expected os.O_RDONLY (%d), got %d", os.O_RDONLY, flags)
	}
}

func TestWireToOSFlagsAllCombinations(t *testing.T) {
	type testCase struct {
		name     string
		wire     uint32
		expected int
	}

	tests := []testCase{}
	accModes := []struct {
		name string
		wire uint32
		flag int
	}{
		{"RDONLY", protocol.FioORDONLY, os.O_RDONLY},
		{"WRONLY", protocol.FioOWRONLY, os.O_WRONLY},
		{"RDWR", protocol.FioORDWR, os.O_RDWR},
	}

	for _, acc := range accModes {
		for _, creat := range []bool{false, true} {
			for _, trunc := range []bool{false, true} {
				wire := acc.wire
				expected := acc.flag
				name := acc.name

				if creat {
					wire |= protocol.FioOCREAT
					expected |= os.O_CREATE
					name += "+CREAT"
				}
				if trunc {
					wire |= protocol.FioOTRUNC
					expected |= os.O_TRUNC
					name += "+TRUNC"
				}

				tests = append(tests, testCase{name: name, wire: wire, expected: expected})
			}
		}
	}

	if len(tests) != 12 {
		t.Fatalf("expected 12 combinations, got %d", len(tests))
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wireToOSFlags(tc.wire)
			if got != tc.expected {
				t.Errorf("wireToOSFlags(0x%04x) = %d, want %d", tc.wire, got, tc.expected)
			}
		})
	}
}

func TestSeekBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	os.WriteFile(path, []byte("0123456789"), 0o644) // 10 bytes

	h := NewHandler()
	defer h.CloseAll()

	// Open
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())

	// Seek to position 100, well beyond EOF
	rt, rp := dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 100, Whence: protocol.FioSeekSet,
	}).Encode())
	if rt != protocol.MsgSeekOk {
		t.Fatalf("expected MsgSeekOk, got 0x%02x", rt)
	}
	seekResp := decodeSeekOk(t, rp)
	if seekResp.Offset != 100 {
		t.Fatalf("expected seek offset 100, got %d", seekResp.Offset)
	}
}

func TestWriteAfterSeekBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.txt")
	os.WriteFile(path, []byte{}, 0o644) // empty file

	h := NewHandler()
	defer h.CloseAll()

	// Open RDWR | CREAT
	dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR | protocol.FioOCREAT, Path: path,
	}).Encode())

	// Seek to position 100
	dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 2, FileID: 1, Offset: 100, Whence: protocol.FioSeekSet,
	}).Encode())

	// Write "x" at position 100
	dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 3, FileID: 1, Data: []byte("x"),
	}).Encode())

	// Close
	dispatch(t, h, protocol.MsgClose, (&protocol.CloseRequest{
		RequestID: 4, FileID: 1,
	}).Encode())

	// Reopen and fstat — size should be 101
	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 5, FileID: 2, Flags: protocol.FioORDONLY, Path: path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("expected MsgOpenOk, got 0x%02x", rt)
	}
	openResp := decodeOpenOk(t, rp)
	if openResp.FileSize != 101 {
		t.Fatalf("expected file size 101, got %d", openResp.FileSize)
	}

	// Seek to position 100 and read 1 byte
	dispatch(t, h, protocol.MsgSeek, (&protocol.SeekRequest{
		RequestID: 6, FileID: 2, Offset: 100, Whence: protocol.FioSeekSet,
	}).Encode())

	rt, rp = dispatch(t, h, protocol.MsgRead, (&protocol.ReadRequest{
		RequestID: 7, FileID: 2, NBytes: 1,
	}).Encode())
	if rt != protocol.MsgReadOk {
		t.Fatalf("expected MsgReadOk, got 0x%02x", rt)
	}
	readResp := decodeReadOk(t, rp)
	if len(readResp.Data) != 1 || readResp.Data[0] != 'x' {
		t.Fatalf("expected byte 'x' at position 100, got %v", readResp.Data)
	}
}

func TestMapErrnoUnmapped(t *testing.T) {
	// syscall.ENOMEM (errno 12) is not in the mapErrno switch — should fall through to FioEIO.
	got := mapErrno(syscall.ENOMEM)
	if got != protocol.FioEIO {
		t.Fatalf("expected FioEIO (%d) for unmapped errno, got %d", protocol.FioEIO, got)
	}
}

func TestRenameAcrossDirectories(t *testing.T) {
	dir := t.TempDir()
	dirA := filepath.Join(dir, "a")
	dirB := filepath.Join(dir, "b")
	os.MkdirAll(dirA, 0o755)
	os.MkdirAll(dirB, 0o755)

	oldPath := filepath.Join(dirA, "file.txt")
	newPath := filepath.Join(dirB, "file.txt")
	os.WriteFile(oldPath, []byte("cross-dir"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	rt, _ := dispatch(t, h, protocol.MsgRename, (&protocol.RenameRequest{
		RequestID: 1, OldPath: oldPath, NewPath: newPath,
	}).Encode())
	if rt != protocol.MsgRenameOk {
		t.Fatalf("expected MsgRenameOk, got 0x%02x", rt)
	}

	// Verify old path is gone and new path exists with correct content
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old path should not exist after rename")
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("failed to read new path: %v", err)
	}
	if string(data) != "cross-dir" {
		t.Fatalf("expected %q, got %q", "cross-dir", string(data))
	}
}

func TestUnlinkNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "notempty")
	os.MkdirAll(subdir, 0o755)
	os.WriteFile(filepath.Join(subdir, "child.txt"), []byte("hi"), 0o644)

	h := NewHandler()
	defer h.CloseAll()

	rt, _, err := h.HandleMessage(protocol.MsgUnlink, (&protocol.UnlinkRequest{
		RequestID: 1, Path: subdir,
	}).Encode())
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	// Unlinking a non-empty directory should fail with an IO error
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for non-empty directory unlink, got 0x%02x", rt)
	}
}

func TestMapErrnoEBUSY(t *testing.T) {
	got := mapErrno(syscall.EBUSY)
	if got != protocol.FioEIO {
		t.Fatalf("expected FioEIO (%d) for EBUSY (unmapped), got %d", protocol.FioEIO, got)
	}
}

func TestMapErrnoEPIPE(t *testing.T) {
	got := mapErrno(syscall.EPIPE)
	if got != protocol.FioEIO {
		t.Fatalf("expected FioEIO (%d) for EPIPE (unmapped), got %d", protocol.FioEIO, got)
	}
}

func TestMapErrnoWrappedInSyscallError(t *testing.T) {
	err := &os.SyscallError{Syscall: "write", Err: syscall.ENOSPC}
	got := mapErrno(err)
	if got != protocol.FioENOSPC {
		t.Fatalf("expected FioENOSPC (%d) for wrapped ENOSPC, got %d", protocol.FioENOSPC, got)
	}
}

func TestOpenEmptyPath(t *testing.T) {
	h := NewHandler()
	defer h.CloseAll()

	rt, _, err := h.HandleMessage(protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 10, Flags: protocol.FioORDONLY, Path: "",
	}).Encode())
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError for empty path, got 0x%02x", rt)
	}
}

func TestWriteThenFstatReflectsSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "growing.bin")

	h := NewHandler()
	defer h.CloseAll()

	// Open for write+create+truncate
	rt, _ := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 20,
		Flags: protocol.FioOWRONLY | protocol.FioOCREAT | protocol.FioOTRUNC,
		Path:  path,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("expected MsgOpenOk, got 0x%02x", rt)
	}

	// Write 100 bytes
	data100 := make([]byte, 100)
	for i := range data100 {
		data100[i] = 'A'
	}
	rt, _ = dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 2, FileID: 20, Data: data100,
	}).Encode())
	if rt != protocol.MsgWriteOk {
		t.Fatalf("expected MsgWriteOk after first write, got 0x%02x", rt)
	}

	// Fstat — should show 100 bytes
	rt, rp := dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 3, FileID: 20,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fstatResp := decodeFstatOk(t, rp)
	if fstatResp.FileSize != 100 {
		t.Fatalf("expected file size 100 after first write, got %d", fstatResp.FileSize)
	}

	// Write 50 more bytes
	data50 := make([]byte, 50)
	for i := range data50 {
		data50[i] = 'B'
	}
	rt, _ = dispatch(t, h, protocol.MsgWrite, (&protocol.WriteRequest{
		RequestID: 4, FileID: 20, Data: data50,
	}).Encode())
	if rt != protocol.MsgWriteOk {
		t.Fatalf("expected MsgWriteOk after second write, got 0x%02x", rt)
	}

	// Fstat — should show 150 bytes
	rt, rp = dispatch(t, h, protocol.MsgFstat, (&protocol.FstatRequest{
		RequestID: 5, FileID: 20,
	}).Encode())
	if rt != protocol.MsgFstatOk {
		t.Fatalf("expected MsgFstatOk, got 0x%02x", rt)
	}
	fstatResp = decodeFstatOk(t, rp)
	if fstatResp.FileSize != 150 {
		t.Fatalf("expected file size 150 after second write, got %d", fstatResp.FileSize)
	}
}

func TestMkdirWithMode(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "modedir")

	h := NewHandler()
	defer h.CloseAll()

	rt, _ := dispatch(t, h, protocol.MsgMkdir, (&protocol.MkdirRequest{
		RequestID: 1, Mode: 0o700, Path: subdir,
	}).Encode())
	if rt != protocol.MsgMkdirOk {
		t.Fatalf("expected MsgMkdirOk, got 0x%02x", rt)
	}

	info, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("failed to stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected a directory, got non-directory")
	}
	// Check mode within umask tolerance: owner bits must match
	actualMode := info.Mode().Perm()
	if actualMode&0o700 != 0o700 {
		t.Fatalf("expected owner rwx (0700) in mode, got %04o", actualMode)
	}
}
