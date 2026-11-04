package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os/exec"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// earlyExitScript reads a little stdin, writes a lot of stdout and exits 3
// while the client is still sending stdin (like `ffmpeg -i - -t 5`).
const earlyExitScript = `
import sys
sys.stdin.buffer.read(65536)
sys.stdout.buffer.write(b'x' * 600000)
sys.stdout.flush()
sys.exit(3)
`

// The exit code must reach the client even though stdin it sent is still
// unread on the server: closing with unread data resets the connection,
// which can destroy the exit code before the client reads it.
func TestSessionExitCodeSurvivesUnreadStdin(t *testing.T) {
	requireUnix(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("needs python3")
	}
	for i := 0; i < 15; i++ {
		if code := runEarlyExit(t); code != 3 {
			t.Fatalf("run %d: client saw %d, want exit code 3", i, code)
		}
	}
}

func runEarlyExit(t *testing.T) int {
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

	proc := process.NewProcess("python3", []string{"-c", earlyExitScript})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	go NewSession(serverConn, proc).Run(context.Background())

	// Client: send stdin up to the window, then read like cmd/client does.
	w := NewWriter(clientConn)
	go func() {
		chunk := bytes.Repeat([]byte{'a'}, 32*1024)
		for sent := 0; sent+len(chunk) <= protocol.StdinWindow; sent += len(chunk) {
			if w.WriteMessage(protocol.MsgStdin, chunk) != nil {
				return
			}
		}
	}()
	clientConn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		msg, err := protocol.ReadMessageFrom(clientConn)
		if err != nil {
			return -1 // the connection ended without an exit code
		}
		if msg.Type == protocol.MsgExitCode && len(msg.Payload) == 4 {
			return int(binary.BigEndian.Uint32(msg.Payload))
		}
	}
}

// trailerScript stands in for ffmpeg writing its trailer on SIGTERM: it
// connects to fio's loopback, waits for SIGTERM, then sends one request and
// exits 0 if it gets the reply.
const trailerScript = `
import socket, struct, os, sys, signal, threading
s = socket.create_connection(('127.0.0.1', int(os.environ['FFOIP_PORT'])))
s.sendall(b'FIO1' + os.environ['FFOIP_TOKEN'].encode())
term = threading.Event()
signal.signal(signal.SIGTERM, lambda *a: term.set())
sys.stdout.write('ready\n'); sys.stdout.flush()
term.wait(30)
payload = struct.pack('>H', 9) + b'/out.mp4'
s.sendall(struct.pack('>BI', 0x2A, len(payload)) + payload)
def read(n):
    b = b''
    while len(b) < n:
        c = s.recv(n - len(b))
        if not c:
            sys.stdout.write('trailer failed\n'); sys.exit(1)
        b += c
    return b
t, n = struct.unpack('>BI', read(5)); read(n)
sys.stdout.write('trailer written\n')
sys.exit(0)
`

// At server shutdown with the client still connected, ffmpeg gets SIGTERM
// with its file access intact, so it can finish its outputs.
func TestSessionShutdownLetsChildFinishOutputs(t *testing.T) {
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

	proc := process.NewProcess("python3", []string{"-c", trailerScript})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	shutdown, cancel := context.WithCancel(context.Background())
	defer cancel()
	go NewSession(serverConn, proc).Run(shutdown)

	w := NewWriter(clientConn)
	var stdout bytes.Buffer
	code := -1
	clientConn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for code < 0 {
		msg, err := protocol.ReadMessageFrom(clientConn)
		if err != nil {
			t.Fatalf("connection ended: %v (stdout %q)", err, stdout.String())
		}
		switch msg.Type {
		case protocol.MsgStdout:
			stdout.Write(msg.Payload)
			if bytes.Contains(stdout.Bytes(), []byte("ready\n")) && shutdown.Err() == nil {
				cancel() // the server is shutting down
			}
		case protocol.MsgStat:
			w.WriteMessage(protocol.MsgStatOk, (&protocol.StatOkResponse{RequestID: 9}).Encode())
		case protocol.MsgExitCode:
			code = int(binary.BigEndian.Uint32(msg.Payload))
		}
	}
	if code != 0 || !bytes.Contains(stdout.Bytes(), []byte("trailer written")) {
		t.Fatalf("exit code %d, stdout %q; want the child to finish its output after SIGTERM", code, stdout.String())
	}
}
