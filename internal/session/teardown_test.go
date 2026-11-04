package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("uses sleep and POSIX signals")
	}
}

// A client that vanishes without sending MsgCancel (SIGKILLed, crashed, or
// its machine dropped the connection) must not leave the child running until
// the 150s keepalive timeout.
func TestSessionClientDisconnectStopsChild(t *testing.T) {
	requireUnix(t)

	clientConn, serverConn := net.Pipe()
	proc := process.NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		code, _ := NewSession(serverConn, proc).Run(context.Background())
		done <- code
	}()

	time.Sleep(100 * time.Millisecond)
	clientConn.Close()

	select {
	case code := <-done:
		if code != 128+15 {
			t.Fatalf("exit code = %d, want 143 (SIGTERM)", code)
		}
	case <-time.After(process.KillTimeout + 2*time.Second):
		proc.Terminate()
		t.Fatal("child still running after the client disconnected")
	}
}

// Server shutdown cancels the session context: the child is stopped, and the
// still-connected client is told its exit code.
func TestSessionServerShutdownStopsChild(t *testing.T) {
	requireUnix(t)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	proc := process.NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _ := NewSession(serverConn, proc).Run(ctx)
		serverConn.Close()
		done <- code
	}()

	msgsCh := make(chan []*protocol.Message, 1)
	go func() { msgsCh <- readMessages(clientConn) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(process.KillTimeout + 2*time.Second):
		proc.Terminate()
		t.Fatal("child still running after server shutdown")
	}
	var exitCode = -1
	for _, m := range <-msgsCh {
		if m.Type == protocol.MsgExitCode {
			exitCode = int(binary.BigEndian.Uint32(m.Payload))
		}
	}
	if exitCode != 128+15 {
		t.Fatalf("client got exit code %d, want 143", exitCode)
	}
}

// fioRequestScript connects to the loopback like fio does, sends one request
// of the given type with request id 7 and the given path, and prints the
// response type and errno it gets back.
const fioRequestScript = `
import socket, struct, os, sys
s = socket.create_connection(('127.0.0.1', int(os.environ['FFOIP_PORT'])))
s.sendall(b'FIO1' + os.environ['FFOIP_TOKEN'].encode())  # loopback hello (process.LoopbackHello)
payload = struct.pack('>H', 7) + sys.argv[2].encode()
s.sendall(struct.pack('>BI', int(sys.argv[1], 0), len(payload)) + payload)
def read(n):
    b = b''
    while len(b) < n:
        c = s.recv(n - len(b))
        if not c:
            sys.exit('eof')
        b += c
    return b
t, n = struct.unpack('>BI', read(5))
body = read(n)
errno = struct.unpack('>i', body[2:6])[0] if t == 0x4F else 0
sys.stdout.write('type=0x%02x errno=%d\n' % (t, errno))
`

func runFioRequest(t *testing.T, msgType uint8, answer func(net.Conn)) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	proc := process.NewProcess("python3", []string{"-c", fioRequestScript, "0x" + strings.ToUpper(hex2(msgType)), "/fonts"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	go func() {
		NewSession(serverConn, proc).Run(context.Background())
		serverConn.Close()
	}()

	if answer != nil {
		answer(clientConn)
	}
	var stdout bytes.Buffer
	for _, m := range readMessages(clientConn) {
		if m.Type == protocol.MsgStdout {
			stdout.Write(m.Payload)
		}
	}
	return strings.TrimSpace(stdout.String())
}

func hex2(b uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&0xF]})
}

// fio's path queries reach the client and its reply reaches fio.
func TestSessionForwardsPathOps(t *testing.T) {
	got := runFioRequest(t, protocol.MsgListDir, func(c net.Conn) {
		msg, err := protocol.ReadMessageFrom(c)
		if err != nil || msg.Type != protocol.MsgListDir {
			t.Errorf("expected forwarded MsgListDir, got %+v %v", msg, err)
			return
		}
		req, _ := protocol.DecodePathRequest(msg.Payload)
		if req.RequestID != 7 || req.Path != "/fonts" {
			t.Errorf("forwarded request: %+v", req)
		}
		resp := &protocol.ListDirOkResponse{RequestID: 7}
		protocol.WriteMessageTo(c, protocol.MsgListDirOk, resp.Encode())
	})
	if got != "type=0x4b errno=0" {
		t.Fatalf("fio got %q, want ListDirOk", got)
	}
}

// A request the server can't parse ends forwarding. The loopback must be
// closed then, so fio sees EOF and fails its requests instead of waiting for
// replies forever.
func TestSessionClosesLoopbackOnBadRequest(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	const script = `
import socket, struct, os
s = socket.create_connection(('127.0.0.1', int(os.environ['FFOIP_PORT'])))
s.sendall(b'FIO1' + os.environ['FFOIP_TOKEN'].encode())  # loopback hello (process.LoopbackHello)
s.sendall(struct.pack('>BI', 0x21, 200 * 1024 * 1024))  # READ, oversized length
s.settimeout(5)
try:
    print('eof' if not s.recv(1) else 'data')
except socket.timeout:
    print('timeout')
`
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	proc := process.NewProcess("python3", []string{"-c", script})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	go func() {
		NewSession(serverConn, proc).Run(context.Background())
		serverConn.Close()
	}()
	var stdout bytes.Buffer
	for _, m := range readMessages(clientConn) {
		if m.Type == protocol.MsgStdout {
			stdout.Write(m.Payload)
		}
	}
	if got := strings.TrimSpace(stdout.String()); got != "eof" {
		t.Fatalf("fio side got %q, want eof", got)
	}
}

type failingWriter struct{ writes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, errors.New("loopback closed")
}

func TestRelayExactDrainsWhenLoopbackFails(t *testing.T) {
	// A reply that arrives after the child exited (loopback closed) is read
	// to its end so the next message from the client parses correctly; the
	// client is not treated as gone, so it still gets its exit code.
	src := bytes.NewReader([]byte("PAYLOADnext"))
	dst := &failingWriter{}
	if err := relayExact(dst, []byte("HDR"), src, 7, make([]byte, 3)); err != nil {
		t.Fatalf("relayExact: %v", err)
	}
	rest, _ := io.ReadAll(src)
	if string(rest) != "next" {
		t.Fatalf("stream out of sync, remaining %q", rest)
	}
	if dst.writes != 1 {
		t.Fatalf("expected no writes after the first failure, got %d", dst.writes)
	}
}

func TestRelayExactCopiesAndReportsShortSource(t *testing.T) {
	var dst bytes.Buffer
	if err := relayExact(&dst, []byte("H"), strings.NewReader("abcdef"), 6, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "Habcdef" {
		t.Fatalf("got %q", dst.String())
	}
	if err := relayExact(io.Discard, nil, strings.NewReader("ab"), 5, make([]byte, 4)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short source: got %v, want ErrUnexpectedEOF", err)
	}
}
