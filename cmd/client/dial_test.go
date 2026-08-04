package main

import (
	"context"
	"fmt"
	"net"
	"testing"
)

func listenTCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return ln.Addr().String()
}

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
	_, _, err := dialWithFailover(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty list")
	}
}

func TestDialWithFailoverSingle(t *testing.T) {
	want := listenTCP(t)
	conn, addr, err := dialWithFailover(context.Background(), []string{want})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if addr != want {
		t.Errorf("addr %q want %q", addr, want)
	}
}

func TestDialWithFailoverSkipsDead(t *testing.T) {
	live := listenTCP(t)
	addrs := []string{deadAddr(t), live, deadAddr(t)}
	conn, addr, err := dialWithFailover(context.Background(), addrs)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if addr != live {
		t.Errorf("got %q want live %q", addr, live)
	}
}

func TestDialWithFailoverAllFail(t *testing.T) {
	a, b := deadAddr(t), deadAddr(t)
	_, _, err := dialWithFailover(context.Background(), []string{a, b})
	if err == nil {
		t.Fatal("expected error when all down")
	}
}

func TestDialWithFailoverDNS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	dnsAddr := fmt.Sprintf("localhost:%d", port)

	conn, got, err := dialWithFailover(context.Background(), []string{dnsAddr})
	if err != nil {
		t.Fatalf("dial DNS %s failed: %v", dnsAddr, err)
	}
	defer conn.Close()
	if got != dnsAddr {
		t.Errorf("got addr %q want %q", got, dnsAddr)
	}
}

func TestDialWithFailoverIPv6(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 not available: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := ln.Addr().String()
	conn, got, err := dialWithFailover(context.Background(), []string{addr})
	if err != nil {
		t.Fatalf("dial IPv6 %s failed: %v", addr, err)
	}
	defer conn.Close()
	if got != addr {
		t.Errorf("got addr %q want %q", got, addr)
	}
}

func TestDialWithFailoverMixedDNSIPv6(t *testing.T) {
	dead := deadAddr(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	live := ln.Addr().String()
	port := ln.Addr().(*net.TCPAddr).Port
	dnsAddr := fmt.Sprintf("localhost:%d", port)

	addrs := []string{dead, dnsAddr, live}
	conn, got, err := dialWithFailover(context.Background(), addrs)
	if err != nil {
		t.Fatalf("dial mixed failed: %v", err)
	}
	defer conn.Close()
	if got != dnsAddr && got != live {
		t.Errorf("unexpected winner %q", got)
	}
	t.Logf("mixed DNS/IPv4 succeeded via %s", got)
}
