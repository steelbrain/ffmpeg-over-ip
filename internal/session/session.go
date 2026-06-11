package session

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

const (
	keepaliveSendInterval = 30 * time.Second
	keepaliveRecvTimeout  = 150 * time.Second
	maxPayloadLen         = 100 * 1024 * 1024
)

// Session manages one client connection: multiplexes between the TCP
// connection, the child process pipes, and the fio loopback connection.
type Session struct {
	conn net.Conn
	proc *process.Process
	w    *Writer

	lastRecv atomic.Int64

	// loopback is set when fio connects, protected by loopbackMu
	loopbackMu    sync.Mutex
	loopback      net.Conn
	loopbackReady chan struct{}
}

func NewSession(conn net.Conn, proc *process.Process) *Session {
	s := &Session{
		conn:          conn,
		proc:          proc,
		w:             NewWriter(conn),
		loopbackReady: make(chan struct{}),
	}
	s.lastRecv.Store(time.Now().UnixNano())
	return s
}

// Run blocks until the child process exits. Returns the exit code.
func (s *Session) Run(ctx context.Context) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// pipeWg tracks stdout/stderr goroutines — they must finish draining
	// before we send the exit code or close the connection.
	var pipeWg sync.WaitGroup
	// otherWg tracks loopback, dispatch, keepalive goroutines — these
	// depend on the connection and are cleaned up with a timeout.
	var otherWg sync.WaitGroup

	// Pipe stdout → TCP
	pipeWg.Add(1)
	go func() {
		defer pipeWg.Done()
		s.pipeOutput(s.proc.Stdout(), protocol.MsgStdout)
	}()

	// Pipe stderr → TCP
	pipeWg.Add(1)
	go func() {
		defer pipeWg.Done()
		s.pipeOutput(s.proc.Stderr(), protocol.MsgStderr)
	}()

	// Wait for loopback in background, start forwarding when ready
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		conn := s.proc.Loopback()
		if conn == nil {
			return
		}
		s.loopbackMu.Lock()
		s.loopback = conn
		s.loopbackMu.Unlock()
		close(s.loopbackReady)

		// Forward fio requests from loopback → TCP
		s.forwardLoopbackToTCP(ctx, conn)
	}()

	// TCP → dispatch: runs immediately (handles stdin, cancel, ping, and
	// fio responses once loopback is ready)
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		s.dispatchTCPMessages(ctx, cancel)
	}()

	// Keepalive
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		s.keepalive(ctx, cancel)
	}()

	// Wait for child to exit
	exitCode, _ := s.proc.Wait()

	// Close loopback to flush forwarder
	s.loopbackMu.Lock()
	if s.loopback != nil {
		s.loopback.Close()
	}
	s.loopbackMu.Unlock()

	// Wait for stdout/stderr to finish draining before sending exit code.
	// pipeOutput reads from os.Pipe fds we own, so it will get EOF once
	// the child exits and the OS pipe buffer is drained — no timeout needed.
	pipeWg.Wait()

	// Send exit code after all output has been sent
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(exitCode))
	s.w.WriteMessage(protocol.MsgExitCode, payload)

	// Close conn to unblock TCP reader goroutine
	s.conn.Close()

	// Give remaining goroutines time to drain
	done := make(chan struct{})
	go func() {
		otherWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	return exitCode, nil
}

func (s *Session) pipeOutput(r io.Reader, msgType uint8) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s.w.WriteMessage(msgType, buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) forwardLoopbackToTCP(ctx context.Context, loopback net.Conn) {
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := protocol.ReadMessageFrom(loopback)
		if err != nil {
			return
		}
		if protocol.IsFileIORequest(msg.Type) {
			s.w.WriteMessage(msg.Type, msg.Payload)
		}
	}
}

func (s *Session) dispatchTCPMessages(ctx context.Context, cancel context.CancelFunc) {
	fileResponseCopyBuf := make([]byte, 256*1024)

	for {
		if ctx.Err() != nil {
			return
		}
		msgType, payloadLen, header, err := readMessageHeader(s.conn)
		if err != nil {
			return
		}

		s.lastRecv.Store(time.Now().UnixNano())

		if protocol.IsFileIOResponse(msgType) {
			select {
			case <-s.loopbackReady:
				if _, err := s.loopback.Write(header[:]); err != nil {
					return
				}
				if err := copyExact(s.loopback, s.conn, int64(payloadLen), fileResponseCopyBuf); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
			continue
		}

		payload, err := readMessagePayload(s.conn, payloadLen)
		if err != nil {
			return
		}

		switch {
		case msgType == protocol.MsgStdin:
			s.proc.Stdin().Write(payload)
		case msgType == protocol.MsgStdinClose:
			s.proc.Stdin().Close()
		case msgType == protocol.MsgCancel:
			go s.proc.Terminate()
		case msgType == protocol.MsgPing:
			s.w.WriteMessage(protocol.MsgPong, payload)
		default:
			log.Printf("session: unknown message type 0x%02x from client, dropping", msgType)
		}
	}
}

func readMessageHeader(r io.Reader) (uint8, uint32, [5]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if err == io.EOF {
			return 0, 0, header, err
		}
		return 0, 0, header, fmt.Errorf("error reading message header: %w", err)
	}

	payloadLen := binary.BigEndian.Uint32(header[1:])
	if payloadLen > maxPayloadLen {
		return 0, 0, header, fmt.Errorf("payload length too large: %d bytes", payloadLen)
	}

	return header[0], payloadLen, header, nil
}

func readMessagePayload(r io.Reader, payloadLen uint32) ([]byte, error) {
	payload := make([]byte, payloadLen)
	if payloadLen == 0 {
		return payload, nil
	}
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("error reading payload: %w", err)
	}
	return payload, nil
}

func copyExact(dst io.Writer, src io.Reader, n int64, buf []byte) error {
	if n == 0 {
		return nil
	}
	limited := &io.LimitedReader{R: src, N: n}
	if _, err := io.CopyBuffer(dst, limited, buf); err != nil {
		return err
	}
	if limited.N != 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (s *Session) keepalive(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(s.w.LastSendTime()) >= keepaliveSendInterval {
				s.w.WriteMessage(protocol.MsgPing, nil)
			}
			lastRecv := time.Unix(0, s.lastRecv.Load())
			if time.Since(lastRecv) >= keepaliveRecvTimeout {
				log.Printf("session: client keepalive timeout")
				s.proc.Terminate()
				cancel()
				return
			}
		}
	}
}
