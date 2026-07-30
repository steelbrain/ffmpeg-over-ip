package main

import (
	"context"
	"errors"
	"net"
	"strings"
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

func TestDialWithFailoverNoAddresses(t *testing.T) {
	conn, addr, err := dialWithFailover(context.Background(), nil, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected error for empty address list")
	}
	if addr != "" {
		t.Errorf("addr = %q, want empty", addr)
	}
}

func TestDialWithFailoverSingle(t *testing.T) {
	want := listenTCP(t, nil)

	conn, addr, err := dialWithFailover(context.Background(), []string{want}, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}

func TestDialWithFailoverSkipsDeadEndpoints(t *testing.T) {
	live := listenTCP(t, nil)
	addrs := []string{deadAddr(t), deadAddr(t), live, deadAddr(t)}

	conn, addr, err := dialWithFailover(context.Background(), addrs, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if addr != live {
		t.Errorf("addr = %q, want the live listener %q", addr, live)
	}
}

func TestDialWithFailoverAllFail(t *testing.T) {
	a, b := deadAddr(t), deadAddr(t)

	conn, _, err := dialWithFailover(context.Background(), []string{a, b}, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected error when every endpoint is down")
	}
	for _, want := range []string{a, b} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDialWithFailoverHonorsParentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	conn, _, err := dialWithFailover(ctx, []string{"192.0.2.1:5050", "192.0.2.2:5050"}, 30*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDialWithFailoverUnixSocket(t *testing.T) {
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
	conn, addr, err := dialWithFailover(context.Background(), []string{deadAddr(t), want}, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}
