package process

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// startTokenChild starts a child that prints its FFOIP_PORT and FFOIP_TOKEN
// and then waits, standing in for an ffmpeg whose fio connects late.
func startTokenChild(t *testing.T) (*Process, string, string) {
	t.Helper()
	proc := NewProcess("sh", []string{"-c", `echo "$FFOIP_PORT $FFOIP_TOKEN"; sleep 10`})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		proc.CloseLoopback()
		proc.Terminate()
	})
	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatalf("reading child env: %v", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		t.Fatalf("child printed %q, want port and token", line)
	}
	if len(fields[1]) != 64 {
		t.Fatalf("FFOIP_TOKEN %q: want 64 hex characters", fields[1])
	}
	return proc, fields[0], fields[1]
}

func dialLoopback(t *testing.T, port string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func loopbackWithin(t *testing.T, proc *Process, d time.Duration) net.Conn {
	t.Helper()
	got := make(chan net.Conn, 1)
	go func() { got <- proc.Loopback() }()
	select {
	case conn := <-got:
		return conn
	case <-time.After(d):
		t.Fatalf("Loopback did not return within %v", d)
		return nil
	}
}

// expectClosed checks that the server closed conn (read returns EOF/reset).
func expectClosed(t *testing.T, conn net.Conn, within time.Duration) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(within))
	var b [1]byte
	_, err := conn.Read(b[:])
	if err == nil {
		t.Fatal("connection still open: server sent data")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("connection still open after %v", within)
	}
}

// A local process that connects before fio must not become the loopback,
// whether it sends nothing, a wrong token, or plain fio requests.
func TestLoopbackRequiresToken(t *testing.T) {
	proc, port, token := startTokenChild(t)

	silent := dialLoopback(t, port)
	wrong := dialLoopback(t, port)
	if _, err := wrong.Write(LoopbackHello(strings.Repeat("0", 64))); err != nil {
		t.Fatal(err)
	}
	expectClosed(t, wrong, 2*time.Second)
	raw := dialLoopback(t, port)
	// a ListDir request instead of a hello (the audit's hijack attempt)
	raw.Write([]byte{0x2B, 0, 0, 0, 7, 0, 1, '/', 'U', 's', 'e', 'r'})
	raw.Write(make([]byte, 64))
	expectClosed(t, raw, 2*time.Second)

	fio := dialLoopback(t, port)
	if _, err := fio.Write(LoopbackHello(token)); err != nil {
		t.Fatal(err)
	}
	conn := loopbackWithin(t, proc, 2*time.Second)
	if conn == nil {
		t.Fatal("Loopback returned nil for a connection with the right token")
	}

	// The returned connection is fio's: bytes fio sends arrive on it.
	if _, err := fio.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("read %q, %v from the loopback; want fio's bytes", buf, err)
	}

	// The silent connection is dropped once fio is in.
	expectClosed(t, silent, 2*time.Second)
	// And no one else gets in afterwards.
	if late, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
		late.Write(LoopbackHello(token))
		expectClosed(t, late, 2*time.Second)
		late.Close()
	}
}

func TestLoopbackTokenDiffersPerProcess(t *testing.T) {
	_, _, a := startTokenChild(t)
	_, _, b := startTokenChild(t)
	if a == b {
		t.Fatal("two processes got the same FFOIP_TOKEN")
	}
}

// CloseLoopback while a connection is still being checked: Loopback returns
// nil and the connection is closed.
func TestCloseLoopbackDuringHello(t *testing.T) {
	proc, port, _ := startTokenChild(t)
	pending := dialLoopback(t, port)
	pending.Write([]byte("FIO")) // part of a hello
	time.Sleep(50 * time.Millisecond)
	proc.CloseLoopback()
	if conn := loopbackWithin(t, proc, 2*time.Second); conn != nil {
		t.Fatal("Loopback returned a connection after CloseLoopback")
	}
	expectClosed(t, pending, 2*time.Second)
}
