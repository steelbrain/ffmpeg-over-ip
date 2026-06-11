package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

type stats struct {
	mu sync.Mutex

	openRequests  uint64
	readRequests  uint64
	readReqBytes  uint64
	readReqMin    uint32
	readReqMax    uint32
	readReq32K    uint64
	readReqLt32K  uint64
	readReqGt32K  uint64
	seekRequests  uint64
	fstatRequests uint64
	closeRequests uint64

	readResponses uint64
	readRespBytes uint64
	readRespZero  uint64
	ioErrors      uint64
}

func (s *stats) observeServerToClient(msg *protocol.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case protocol.MsgOpen:
		s.openRequests++
	case protocol.MsgRead:
		s.readRequests++
		if len(msg.Payload) >= 8 {
			n := binary.BigEndian.Uint32(msg.Payload[4:])
			s.readReqBytes += uint64(n)
			if s.readReqMin == 0 || n < s.readReqMin {
				s.readReqMin = n
			}
			if n > s.readReqMax {
				s.readReqMax = n
			}
			switch {
			case n == 32768:
				s.readReq32K++
			case n < 32768:
				s.readReqLt32K++
			default:
				s.readReqGt32K++
			}
		}
	case protocol.MsgSeek:
		s.seekRequests++
	case protocol.MsgFstat:
		s.fstatRequests++
	case protocol.MsgClose:
		s.closeRequests++
	}
}

func (s *stats) observeClientToServer(msg *protocol.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case protocol.MsgReadOk:
		s.readResponses++
		if len(msg.Payload) >= 2 {
			n := uint64(len(msg.Payload) - 2)
			s.readRespBytes += n
			if n == 0 {
				s.readRespZero++
			}
		}
	case protocol.MsgIoError:
		s.ioErrors++
	}
}

func (s *stats) write(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out bytes.Buffer
	fmt.Fprintf(&out, "open_requests=%d\n", s.openRequests)
	fmt.Fprintf(&out, "read_requests=%d\n", s.readRequests)
	fmt.Fprintf(&out, "read_request_bytes=%d\n", s.readReqBytes)
	fmt.Fprintf(&out, "read_request_min=%d\n", s.readReqMin)
	fmt.Fprintf(&out, "read_request_max=%d\n", s.readReqMax)
	fmt.Fprintf(&out, "read_request_32768=%d\n", s.readReq32K)
	fmt.Fprintf(&out, "read_request_lt_32768=%d\n", s.readReqLt32K)
	fmt.Fprintf(&out, "read_request_gt_32768=%d\n", s.readReqGt32K)
	fmt.Fprintf(&out, "seek_requests=%d\n", s.seekRequests)
	fmt.Fprintf(&out, "fstat_requests=%d\n", s.fstatRequests)
	fmt.Fprintf(&out, "close_requests=%d\n", s.closeRequests)
	fmt.Fprintf(&out, "read_responses=%d\n", s.readResponses)
	fmt.Fprintf(&out, "read_response_bytes=%d\n", s.readRespBytes)
	fmt.Fprintf(&out, "read_response_zero=%d\n", s.readRespZero)
	fmt.Fprintf(&out, "io_errors=%d\n", s.ioErrors)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		log.Printf("failed to write stats: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("failed to publish stats: %v", err)
	}
}

func forward(src net.Conn, dst net.Conn, observe func(*protocol.Message)) {
	for {
		msg, err := protocol.ReadMessageFrom(src)
		if err != nil {
			if err != io.EOF {
				log.Printf("proxy read error: %v", err)
			}
			return
		}

		observe(msg)
		if err := protocol.WriteMessageTo(dst, msg.Type, msg.Payload); err != nil {
			log.Printf("proxy write error: %v", err)
			return
		}
	}
}

func handle(client net.Conn, target string, s *stats, statsPath string) {
	defer client.Close()

	server, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("failed to connect to target %s: %v", target, err)
		return
	}
	defer server.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		forward(client, server, s.observeClientToServer)
		server.Close()
	}()
	go func() {
		defer wg.Done()
		forward(server, client, s.observeServerToClient)
		client.Close()
	}()
	wg.Wait()
	s.write(statsPath)
}

func main() {
	listenAddr := flag.String("listen", "", "TCP listen address")
	targetAddr := flag.String("target", "", "TCP target address")
	statsPath := flag.String("stats", "", "path to write shell-compatible stats")
	flag.Parse()

	if *listenAddr == "" || *targetAddr == "" || *statsPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", *listenAddr, err)
	}
	defer listener.Close()

	var s stats
	s.write(*statsPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		s.write(*statsPath)
		os.Exit(0)
	}()

	log.Printf("proxy listening on %s and forwarding to %s", listener.Addr(), *targetAddr)

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handle(client, *targetAddr, &s, *statsPath)
	}
}
