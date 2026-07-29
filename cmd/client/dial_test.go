package main

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// listenTCP starts a listener that accepts connections and hands them to
// onAccept (if non-nil). It returns the address and stops on cleanup.
func listenTCP(t *testing.T, onAccept func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if onAccept != nil {
				go onAccept(conn)
			}
		}
	}()
	return ln.Addr().String()
}

// deadAddr returns an address nothing is listening on. Binding then closing
// makes the port very likely to refuse rather than blackhole, so tests fail
// fast instead of waiting out a timeout.
func deadAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestDialFirstAvailableNoAddresses(t *testing.T) {
	conn, addr, err := dialFirstAvailable(context.Background(), nil, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected error for empty address list")
	}
	if addr != "" {
		t.Errorf("addr = %q, want empty", addr)
	}
}

func TestDialFirstAvailableSingle(t *testing.T) {
	want := listenTCP(t, nil)

	conn, addr, err := dialFirstAvailable(context.Background(), []string{want}, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}

func TestDialFirstAvailableSkipsDeadEndpoints(t *testing.T) {
	live := listenTCP(t, nil)
	addrs := []string{deadAddr(t), deadAddr(t), live, deadAddr(t)}

	conn, addr, err := dialFirstAvailable(context.Background(), addrs, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if addr != live {
		t.Errorf("addr = %q, want the live listener %q", addr, live)
	}
}

func TestDialFirstAvailableAllFail(t *testing.T) {
	a, b := deadAddr(t), deadAddr(t)

	conn, _, err := dialFirstAvailable(context.Background(), []string{a, b}, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected error when every endpoint is down")
	}
	// errors.Join keeps every attempt's failure, each tagged with its address,
	// so the log says which endpoints were tried and why each one failed.
	for _, want := range []string{a, b} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A loser whose handshake completes after the winner is picked must have its
// connection closed, not leaked. The losing server sees EOF as proof.
func TestDialFirstAvailableClosesLosingConnections(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	closed := make(chan error, 1)
	var once sync.Once
	loser := listenTCP(t, func(conn net.Conn) {
		defer conn.Close()
		wg.Wait() // hold the accept until the winner has been returned
		once.Do(func() {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, err := conn.Read(make([]byte, 1))
			closed <- err
		})
	})
	winner := listenTCP(t, nil)

	conn, addr, err := dialFirstAvailable(context.Background(), []string{winner, loser}, 2*time.Second)
	if err != nil {
		wg.Done()
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if addr != winner {
		// The loser's accept handler blocks, but its TCP handshake still
		// completes, so either address can win. Only the winner assertion is
		// unsafe to make; skip rather than flake.
		wg.Done()
		t.Skipf("loser %q won the race, cannot assert on the winner's cleanup", addr)
	}
	wg.Done()

	select {
	case err := <-closed:
		if !errors.Is(err, io.EOF) {
			t.Errorf("losing connection: got %v, want EOF from client-side close", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("losing connection was never closed — leaked socket")
	}
}

func TestDialFirstAvailableHonorsParentContext(t *testing.T) {
	// A listener that never accepts still completes the TCP handshake via the
	// backlog, so use an unroutable address to keep the dial pending.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	conn, _, err := dialFirstAvailable(ctx, []string{"192.0.2.1:5050", "192.0.2.2:5050"}, 30*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDialFirstAvailableUnixSocket(t *testing.T) {
	sock := t.TempDir() + "/test.sock"
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	want := "unix:" + sock
	conn, addr, err := dialFirstAvailable(context.Background(), []string{deadAddr(t), want}, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}
