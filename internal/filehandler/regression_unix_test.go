//go:build unix

package filehandler

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// ListDir on a FIFO must not block in open(2): the handler serves one
// request at a time, so the client's whole message loop would stall.
func TestListDirOnFifoDoesNotBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	done := make(chan uint8, 1)
	go func() {
		typ, _, _ := h.HandleMessage(protocol.MsgListDir, (&protocol.PathRequest{RequestID: 1, Path: p}).Encode())
		done <- typ
	}()
	select {
	case typ := <-done:
		if typ != protocol.MsgIoError {
			t.Fatalf("got 0x%02x, want IoError (ENOTDIR)", typ)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListDir on a FIFO blocked")
	}
}

// Opening a FIFO reports it as one, so fio's fstat marks it streamed.
func TestOpenFifoReportsFifo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	// O_RDWR on a FIFO doesn't wait for a peer on Linux and macOS.
	h := NewHandler()
	defer h.CloseAll()
	rt, rp := dispatch(t, h, protocol.MsgOpen, (&protocol.OpenRequest{
		RequestID: 1, FileID: 1, Flags: protocol.FioORDWR, Path: p,
	}).Encode())
	if rt != protocol.MsgOpenOk {
		t.Fatalf("open: got 0x%02x", rt)
	}
	if r := decodeOpenOk(t, rp); r.Mode&protocol.FioSIFMT != protocol.FioSIFIFO {
		t.Fatalf("mode 0%o: want a FIFO", r.Mode)
	}
}
