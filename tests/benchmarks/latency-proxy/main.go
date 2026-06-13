// Command latency-proxy is a dumb bidirectional TCP proxy that injects a fixed
// delay before forwarding each chunk in each direction. It is used by the
// seek benchmark to emulate slow backing media / a high-latency link, so the
// cost of speculative prefetch reads that get discarded on a seek becomes
// visible in wall-clock time.
//
// The delay is per forwarded chunk per direction, so a single request/response
// round trip incurs roughly 2x the configured latency. This is an
// amplification knob, not a precise network model.
package main

import (
	"flag"
	"log"
	"net"
	"sync"
	"time"
)

func main() {
	listenAddr := flag.String("listen", "127.0.0.1:0", "address to listen on")
	targetAddr := flag.String("target", "", "address to forward connections to")
	latency := flag.Duration("latency", 0, "delay injected before forwarding each chunk (per direction)")
	flag.Parse()

	if *targetAddr == "" {
		log.Fatal("latency-proxy: --target is required")
	}

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("latency-proxy: listen: %v", err)
	}
	defer ln.Close()

	log.Printf("latency-proxy: listening on %s -> %s (latency %s/chunk/direction)",
		ln.Addr(), *targetAddr, *latency)

	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(client, *targetAddr, *latency)
	}
}

func handle(client net.Conn, target string, latency time.Duration) {
	defer client.Close()

	upstream, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("latency-proxy: dial %s: %v", target, err)
		return
	}
	defer upstream.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go pipe(&wg, upstream, client, latency) // client -> upstream
	go pipe(&wg, client, upstream, latency) // upstream -> client
	wg.Wait()
}

func pipe(wg *sync.WaitGroup, dst, src net.Conn, latency time.Duration) {
	defer wg.Done()
	buf := make([]byte, 64*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if latency > 0 {
				time.Sleep(latency)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	// Unblock the peer direction by half-closing where possible.
	if c, ok := dst.(*net.TCPConn); ok {
		c.CloseWrite()
	}
}
