package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
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

	files        map[uint16]*fileStats
	pendingOpens map[uint16]pendingOpen
	pendingReads map[uint16]pendingRead
	pendingSeeks map[uint16]pendingSeek
}

type fileStats struct {
	offset    int64
	size      int64
	intervals []readInterval
}

type readInterval struct {
	start int64
	end   int64
}

type pendingOpen struct {
	fileID uint16
}

type pendingRead struct {
	fileID    uint16
	start     int64
	requested uint32
}

type pendingSeek struct {
	fileID uint16
}

func newStats() *stats {
	return &stats{
		files:        make(map[uint16]*fileStats),
		pendingOpens: make(map[uint16]pendingOpen),
		pendingReads: make(map[uint16]pendingRead),
		pendingSeeks: make(map[uint16]pendingSeek),
	}
}

func (s *stats) file(fileID uint16) *fileStats {
	file := s.files[fileID]
	if file == nil {
		file = &fileStats{size: -1}
		s.files[fileID] = file
	}
	return file
}

func (s *stats) observeServerToClient(msg *protocol.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case protocol.MsgOpen:
		s.openRequests++
		if req, err := protocol.DecodeOpenRequest(msg.Payload); err == nil {
			s.pendingOpens[req.RequestID] = pendingOpen{fileID: req.FileID}
			file := s.file(req.FileID)
			file.offset = 0
			file.size = -1
		}
	case protocol.MsgRead:
		s.readRequests++
		if req, err := protocol.DecodeReadRequest(msg.Payload); err == nil {
			file := s.file(req.FileID)
			start := file.offset
			file.offset += int64(req.NBytes)
			s.pendingReads[req.RequestID] = pendingRead{
				fileID:    req.FileID,
				start:     start,
				requested: req.NBytes,
			}

			n := req.NBytes
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
		if req, err := protocol.DecodeSeekRequest(msg.Payload); err == nil {
			s.pendingSeeks[req.RequestID] = pendingSeek{fileID: req.FileID}
		}
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
	case protocol.MsgOpenOk:
		if resp, err := protocol.DecodeOpenOkResponse(msg.Payload); err == nil {
			if pending, ok := s.pendingOpens[resp.RequestID]; ok {
				file := s.file(pending.fileID)
				file.size = resp.FileSize
				delete(s.pendingOpens, resp.RequestID)
			}
		}
	case protocol.MsgReadOk:
		s.readResponses++
		if len(msg.Payload) >= 2 {
			n := uint64(len(msg.Payload) - 2)
			s.readRespBytes += n
			if n == 0 {
				s.readRespZero++
			}
			resp, err := protocol.DecodeReadOkResponse(msg.Payload)
			if err == nil {
				if pending, ok := s.pendingReads[resp.RequestID]; ok {
					file := s.file(pending.fileID)
					dataLen := int64(len(resp.Data))
					if dataLen > 0 {
						file.intervals = append(file.intervals, readInterval{
							start: pending.start,
							end:   pending.start + dataLen,
						})
					}
					if file.offset == pending.start+int64(pending.requested) {
						file.offset = pending.start + dataLen
					}
					delete(s.pendingReads, resp.RequestID)
				}
			}
		}
	case protocol.MsgSeekOk:
		if resp, err := protocol.DecodeSeekOkResponse(msg.Payload); err == nil {
			if pending, ok := s.pendingSeeks[resp.RequestID]; ok {
				s.file(pending.fileID).offset = resp.Offset
				delete(s.pendingSeeks, resp.RequestID)
			}
		}
	case protocol.MsgIoError:
		s.ioErrors++
	}
}

func (s *stats) write(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	uniqueReadBytes := uniqueBytes(s.files)
	redundantReadBytes := uint64(0)
	if s.readRespBytes > uniqueReadBytes {
		redundantReadBytes = s.readRespBytes - uniqueReadBytes
	}

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
	fmt.Fprintf(&out, "read_unique_bytes=%d\n", uniqueReadBytes)
	fmt.Fprintf(&out, "read_redundant_bytes=%d\n", redundantReadBytes)
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

func uniqueBytes(files map[uint16]*fileStats) uint64 {
	var intervals []readInterval
	for _, file := range files {
		intervals = append(intervals, file.intervals...)
	}
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start == intervals[j].start {
			return intervals[i].end < intervals[j].end
		}
		return intervals[i].start < intervals[j].start
	})

	var total uint64
	current := intervals[0]
	for _, interval := range intervals[1:] {
		if interval.end <= interval.start {
			continue
		}
		if interval.start <= current.end {
			if interval.end > current.end {
				current.end = interval.end
			}
			continue
		}
		total += uint64(current.end - current.start)
		current = interval
	}
	if current.end > current.start {
		total += uint64(current.end - current.start)
	}
	return total
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

	s := newStats()
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
		go handle(client, *targetAddr, s, *statsPath)
	}
}
