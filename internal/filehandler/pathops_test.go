package filehandler

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

func statPath(t *testing.T, h *Handler, path string) (uint8, []byte) {
	t.Helper()
	return dispatch(t, h, protocol.MsgStat, (&protocol.PathRequest{RequestID: 9, Path: path}).Encode())
}

func listDir(t *testing.T, h *Handler, path string) (uint8, []byte) {
	t.Helper()
	return dispatch(t, h, protocol.MsgListDir, (&protocol.PathRequest{RequestID: 10, Path: path}).Encode())
}

func TestStatFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subs.ass")
	if err := os.WriteFile(path, []byte("[Script Info]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	rt, rp := statPath(t, h, path)
	if rt != protocol.MsgStatOk {
		t.Fatalf("expected MsgStatOk, got 0x%02x", rt)
	}
	r, err := protocol.DecodeStatOkResponse(rp)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestID != 9 || r.Size != 14 || r.Mtime != 1700000000 {
		t.Fatalf("unexpected stat reply: %+v", r)
	}
	if r.Mode&protocol.FioSIFMT != protocol.FioSIFREG {
		t.Fatalf("expected a regular file, got mode 0%o", r.Mode)
	}
	if r.Access&(protocol.FioAccessR|protocol.FioAccessW) != protocol.FioAccessR|protocol.FioAccessW {
		t.Fatalf("expected read and write access, got 0x%x", r.Access)
	}
}

func TestStatDirectory(t *testing.T) {
	h := NewHandler()
	rt, rp := statPath(t, h, t.TempDir())
	if rt != protocol.MsgStatOk {
		t.Fatalf("expected MsgStatOk, got 0x%02x", rt)
	}
	r, _ := protocol.DecodeStatOkResponse(rp)
	if r.Mode&protocol.FioSIFMT != protocol.FioSIFDIR {
		t.Fatalf("expected a directory, got mode 0%o", r.Mode)
	}
}

func TestStatMissing(t *testing.T) {
	h := NewHandler()
	rt, rp := statPath(t, h, filepath.Join(t.TempDir(), "nope.png"))
	if rt != protocol.MsgIoError {
		t.Fatalf("expected MsgIoError, got 0x%02x", rt)
	}
	if e := decodeIoError(t, rp); e.RequestID != 9 || e.Errno != protocol.FioENOENT {
		t.Fatalf("expected ENOENT for request 9, got %+v", e)
	}
}

func TestStatReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("access bits come from file attributes on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	path := filepath.Join(t.TempDir(), "ro.txt")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	_, rp := statPath(t, h, path)
	r, err := protocol.DecodeStatOkResponse(rp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Access != protocol.FioAccessR {
		t.Fatalf("expected read-only access, got 0x%x", r.Access)
	}
}

func TestStatFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.ttf")
	os.WriteFile(target, []byte("12345"), 0o644)
	link := filepath.Join(dir, "link.ttf")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	_, rp := statPath(t, h, link)
	r, err := protocol.DecodeStatOkResponse(rp)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode&protocol.FioSIFMT != protocol.FioSIFREG || r.Size != 5 {
		t.Fatalf("stat should describe the target: %+v", r)
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.ttf"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.otf"), []byte("bbbbbb"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	if runtime.GOOS != "windows" {
		if err := os.Symlink("a.ttf", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
	}

	h := NewHandler()
	rt, rp := listDir(t, h, dir)
	if rt != protocol.MsgListDirOk {
		t.Fatalf("expected MsgListDirOk, got 0x%02x", rt)
	}
	r, err := protocol.DecodeListDirOkResponse(rp)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestID != 10 {
		t.Fatalf("request id: got %d", r.RequestID)
	}

	got := map[string]protocol.DirEntry{}
	for _, e := range r.Entries {
		if e.Name == "." || e.Name == ".." {
			t.Fatalf("listing must not contain %q", e.Name)
		}
		got[e.Name] = e
	}
	want := map[string]struct {
		typ  uint32
		size int64
	}{
		"a.ttf":   {protocol.FioSIFREG, 3},
		"b.otf":   {protocol.FioSIFREG, 6},
		".hidden": {protocol.FioSIFREG, 1},
		"sub":     {protocol.FioSIFDIR, -1},
	}
	if runtime.GOOS != "windows" {
		want["link"] = struct {
			typ  uint32
			size int64
		}{protocol.FioSIFLNK, -1} // lstat: the link itself
	}
	if len(got) != len(want) {
		t.Fatalf("entries: got %d (%v), want %d", len(got), r.Entries, len(want))
	}
	for name, w := range want {
		e, ok := got[name]
		if !ok {
			t.Fatalf("missing entry %q", name)
		}
		if e.Mode&protocol.FioSIFMT != w.typ {
			t.Errorf("%s: mode 0%o, want type 0%o", name, e.Mode, w.typ)
		}
		if w.size >= 0 && e.Size != w.size {
			t.Errorf("%s: size %d, want %d", name, e.Size, w.size)
		}
		if e.Mtime == 0 {
			t.Errorf("%s: missing mtime", name)
		}
	}
}

func TestListDirErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	os.WriteFile(file, []byte("x"), 0o644)

	h := NewHandler()
	rt, rp := listDir(t, h, filepath.Join(dir, "missing"))
	if rt != protocol.MsgIoError || decodeIoError(t, rp).Errno != protocol.FioENOENT {
		t.Fatalf("missing dir: got 0x%02x %v", rt, rp)
	}
	rt, rp = listDir(t, h, file)
	if rt != protocol.MsgIoError || decodeIoError(t, rp).Errno != protocol.FioENOTDIR {
		t.Fatalf("listing a file: got 0x%02x %+v", rt, decodeIoError(t, rp))
	}
}

func TestListDirEmpty(t *testing.T) {
	h := NewHandler()
	rt, rp := listDir(t, h, t.TempDir())
	if rt != protocol.MsgListDirOk {
		t.Fatalf("expected MsgListDirOk, got 0x%02x", rt)
	}
	r, err := protocol.DecodeListDirOkResponse(rp)
	if err != nil || len(r.Entries) != 0 {
		t.Fatalf("expected no entries: %+v %v", r, err)
	}
}

func TestReadRequestFillsRegularFiles(t *testing.T) {
	// A reader that returns one byte per call stands in for storage that
	// returns short reads mid-file.
	buf := make([]byte, 11)
	n, err := readRequest(iotest.OneByteReader(strings.NewReader("hello world!")), true, buf)
	if err != nil || n != 11 || string(buf) != "hello world" {
		t.Fatalf("regular: n=%d err=%v buf=%q", n, err, buf[:n])
	}

	// Pipes and devices get one read, so a reply never waits for more data.
	n, err = readRequest(iotest.OneByteReader(strings.NewReader("hello")), false, buf)
	if err != nil || n != 1 {
		t.Fatalf("non-regular: n=%d err=%v", n, err)
	}
}

func TestReadRequestStopsAtEOF(t *testing.T) {
	buf := make([]byte, 64)
	n, err := readRequest(iotest.OneByteReader(strings.NewReader("tail")), true, buf)
	if n != 4 || err != io.EOF {
		t.Fatalf("n=%d err=%v, want 4, EOF", n, err)
	}
}

func TestReadRequestKeepsBytesBeforeError(t *testing.T) {
	// Bytes read before a failure have moved the file offset and must be
	// returned (handleRead sends them as ReadOk) rather than dropped.
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(boom))
	buf := make([]byte, 10)
	n, err := readRequest(r, true, buf)
	if n != 3 || !errors.Is(err, boom) || string(buf[:3]) != "abc" {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
