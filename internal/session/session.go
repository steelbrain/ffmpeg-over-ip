package session

import (
	"context"
	"encoding/binary"
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
)

// Session manages one client connection: multiplexes between the TCP
// connection, the child process pipes, and the fio loopback connection.
type Session struct {
	conn net.Conn
	proc *process.Process
	w    *Writer

	lastRecv atomic.Int64

	// loopback is set when fio connects, protected by loopbackMu
	loopbackMu   sync.Mutex
	loopback     net.Conn
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
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := protocol.ReadMessageFrom(s.conn)
		if err != nil {
			return
		}

		s.lastRecv.Store(time.Now().UnixNano())

		switch {
		case protocol.IsFileIOResponse(msg.Type):
			// Wait for loopback to be ready before forwarding
			select {
			case <-s.loopbackReady:
				protocol.WriteMessageTo(s.loopback, msg.Type, msg.Payload)
			case <-ctx.Done():
				return
			}
		case msg.Type == protocol.MsgStdin:
			s.proc.Stdin().Write(msg.Payload)
		case msg.Type == protocol.MsgStdinClose:
			s.proc.Stdin().Close()
		case msg.Type == protocol.MsgCancel:
			go s.proc.Terminate()
		case msg.Type == protocol.MsgPing:
			s.w.WriteMessage(protocol.MsgPong, msg.Payload)
		default:
			log.Printf("session: unknown message type 0x%02x from client, dropping", msg.Type)
		}
	}
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
