package session

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// stdinHOLScript stands in for ffmpeg: it sends one fio request and waits
// for the reply before it reads stdin (like a muxer blocked on a fio write
// while the demuxer reading stdin waits).
const stdinHOLScript = `
import socket, struct, os, sys
s = socket.create_connection(('127.0.0.1', int(os.environ['FFOIP_PORT'])))
s.sendall(b'FIO1' + os.environ['FFOIP_TOKEN'].encode())  # loopback hello (process.LoopbackHello)
payload = struct.pack('>H', 7) + b'/x'
s.sendall(struct.pack('>BI', 0x2A, len(payload)) + payload)
def read(n):
    b = b''
    while len(b) < n:
        c = s.recv(n - len(b))
        if not c:
            sys.exit('eof')
        b += c
    return b
t, n = struct.unpack('>BI', read(5)); read(n)
data = sys.stdin.buffer.read()
sys.stdout.write('got reply, stdin=%d\n' % len(data))
`

// Stdin the child isn't reading yet must not hold up the fio reply it is
// waiting for: both arrive on the same connection.
func TestSessionStdinDoesNotBlockFioReplies(t *testing.T) {
	requireUnix(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("needs python3")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	serverConn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}

	proc := process.NewProcess("python3", []string{"-c", stdinHOLScript})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		NewSession(serverConn, proc).Run(context.Background())
		serverConn.Close()
		close(done)
	}()

	const total = 1024 * 1024 // more than a pipe buffer, within the window
	w := NewWriter(clientConn)
	go func() {
		chunk := bytes.Repeat([]byte{'a'}, 32*1024)
		for sent := 0; sent < total; sent += len(chunk) {
			if w.WriteMessage(protocol.MsgStdin, chunk) != nil {
				return
			}
		}
		w.WriteMessage(protocol.MsgStdinClose, nil)
	}()

	var out strings.Builder
	var acked atomic.Int64
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			msg, err := protocol.ReadMessageFrom(clientConn)
			if err != nil {
				return
			}
			switch msg.Type {
			case protocol.MsgStat:
				time.Sleep(50 * time.Millisecond) // the reply arrives after some stdin
				w.WriteMessage(protocol.MsgStatOk, (&protocol.StatOkResponse{RequestID: 7}).Encode())
			case protocol.MsgStdout:
				out.Write(msg.Payload)
			case protocol.MsgStdinAck:
				n, err := protocol.DecodeStdinAck(msg.Payload)
				if err != nil {
					t.Error(err)
				}
				acked.Add(int64(n))
			case protocol.MsgExitCode:
				clientConn.Close() // as the real client exits
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		proc.Terminate()
		t.Fatal("fio reply stuck behind stdin the child wasn't reading")
	}
	clientConn.Close()
	<-readerDone
	if !strings.Contains(out.String(), "stdin=1048576") {
		t.Fatalf("child output %q, want all stdin delivered", out.String())
	}
	if acked.Load() != total {
		t.Fatalf("acked %d stdin bytes, want %d", acked.Load(), total)
	}
}

// A client that ignores the window is cut off instead of growing the
// server's queue without bound.
func TestSessionStdinWindowEnforced(t *testing.T) {
	requireUnix(t)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	proc := process.NewProcess("sleep", []string{"30"}) // never reads stdin
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		NewSession(serverConn, proc).Run(context.Background())
		close(done)
	}()
	go readMessages(clientConn)

	chunk := make([]byte, 64*1024)
	for sent := 0; sent <= 2*protocol.StdinWindow; sent += len(chunk) {
		if protocol.WriteMessageTo(clientConn, protocol.MsgStdin, chunk) != nil {
			break
		}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		proc.Terminate()
		t.Fatal("session kept accepting stdin past the window")
	}
}

// A client that stops reading (its machine lost power: no FIN, no RST)
// leaves the server's writes stuck. The keepalive must still notice the
// silence and stop the child.
func TestSessionKeepaliveNotStuckBehindWrite(t *testing.T) {
	requireUnix(t)
	defer func(send, recv, tick time.Duration) {
		keepaliveSendInterval, keepaliveRecvTimeout, keepaliveTick = send, recv, tick
	}(keepaliveSendInterval, keepaliveRecvTimeout, keepaliveTick)
	keepaliveSendInterval, keepaliveRecvTimeout, keepaliveTick = 200*time.Millisecond, time.Second, 100*time.Millisecond

	clientConn, serverConn := net.Pipe() // the client never reads
	defer clientConn.Close()
	proc := process.NewProcess("yes", nil) // fills stdout; pipeOutput blocks writing it
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		NewSession(serverConn, proc).Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		proc.Terminate()
		t.Fatal("keepalive never noticed the silent client")
	}
}
