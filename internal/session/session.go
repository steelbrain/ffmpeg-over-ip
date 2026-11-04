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

const maxPayloadLen = 100 * 1024 * 1024

// exitDrainTimeout bounds how long a session waits, after sending the exit
// code, for the client to hang up.
const exitDrainTimeout = 30 * time.Second

// Variables so tests can shorten them; each session copies them when it is
// created.
var (
	keepaliveSendInterval = 30 * time.Second
	keepaliveRecvTimeout  = 150 * time.Second
	keepaliveTick         = 5 * time.Second
)

// Session manages one client connection: multiplexes between the TCP
// connection, the child process pipes, and the fio loopback connection.
type Session struct {
	conn net.Conn
	proc *process.Process
	w    *Writer

	lastRecv atomic.Int64
	stdin    stdinQueue

	keepaliveSend, keepaliveRecv, keepaliveEvery time.Duration

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
		stdin:         stdinQueue{wake: make(chan struct{}, 1)},

		keepaliveSend:  keepaliveSendInterval,
		keepaliveRecv:  keepaliveRecvTimeout,
		keepaliveEvery: keepaliveTick,
	}
	s.lastRecv.Store(MonoNow())
	return s
}

// Run blocks until the child process exits. Returns the exit code.
//
// The child is stopped early when the client goes away (its connection
// fails or stops answering keepalives) or when shutdown is canceled (server
// shutdown).
func (s *Session) Run(shutdown context.Context) (int, error) {
	// ctx is the relay's lifetime: it ends when the client is gone or the
	// session is over, not at server shutdown, so a child asked to stop can
	// still finish its outputs through the client.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// clientGone tears down a connection that failed: closing it unblocks
	// any goroutine stuck writing to it, and canceling ctx stops the child.
	clientGone := func() {
		s.conn.Close()
		cancel()
	}

	go func() {
		select {
		case <-ctx.Done():
			// The client is gone. Without this, a client that disconnects
			// (killed, crashed, network gone) leaves ffmpeg running on the
			// server, blocked on file requests nobody will answer.
			s.stopChild()
		case <-shutdown.Done():
			s.stopChildForShutdown(ctx)
		case <-s.proc.Done():
		}
	}()

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
		s.pipeOutput(s.proc.Stdout(), protocol.MsgStdout, clientGone)
	}()

	// Pipe stderr → TCP
	pipeWg.Add(1)
	go func() {
		defer pipeWg.Done()
		s.pipeOutput(s.proc.Stderr(), protocol.MsgStderr, clientGone)
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
		if ctx.Err() != nil {
			// The child is being stopped; stopChild may have run before
			// the loopback was set, so close it here.
			conn.Close()
			return
		}

		// Forward fio requests from loopback → TCP
		s.forwardLoopbackToTCP(ctx, conn, clientGone)
	}()

	// Stdin is written to the child by its own goroutine (see stdinQueue).
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		s.writeStdin(ctx)
	}()

	// TCP → dispatch: runs immediately (handles stdin, cancel, ping, and
	// fio responses once loopback is ready)
	dispatchDone := make(chan struct{})
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		defer close(dispatchDone)
		s.dispatchTCPMessages(ctx, clientGone)
	}()

	// Keepalive
	otherWg.Add(1)
	go func() {
		defer otherWg.Done()
		s.keepalive(ctx, clientGone)
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

	// Let the client read the exit code before the connection goes away
	// (see CloseGracefully): half-close, and let the dispatcher read and
	// drop what the client still sends until it hangs up, which it does as
	// soon as it has the exit code.
	if ctx.Err() == nil && canHalfClose(s.conn) {
		if s.conn.(interface{ CloseWrite() error }).CloseWrite() == nil {
			select {
			case <-dispatchDone:
			case <-time.After(exitDrainTimeout):
			}
		}
	}
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

// stopChild closes the loopback first, so every fio request the child is
// waiting on fails at once instead of blocking its exit, then terminates it.
// It is for a client that is gone: nobody would answer those requests.
func (s *Session) stopChild() {
	s.closeLoopback()
	s.proc.Terminate()
}

// stopChildForShutdown stops the child for a server shutdown while the
// client is still connected. The loopback stays open, so the child can
// finish its outputs through the client after SIGTERM (ffmpeg writes an
// MP4's index then, for example); it is closed only if the client goes away
// in the meantime.
func (s *Session) stopChildForShutdown(clientCtx context.Context) {
	done := make(chan struct{})
	go func() {
		s.proc.Terminate()
		close(done)
	}()
	select {
	case <-done:
	case <-clientCtx.Done():
		s.closeLoopback()
		<-done
	}
}

func (s *Session) closeLoopback() {
	s.loopbackMu.Lock()
	if s.loopback != nil {
		s.loopback.Close()
	}
	s.loopbackMu.Unlock()
}

func (s *Session) pipeOutput(r io.ReadCloser, msgType uint8, clientGone func()) {
	// Read to EOF (the child and everything it started have exited), then
	// close our end now rather than whenever the GC finalizes it.
	defer r.Close()
	buf := make([]byte, 32*1024)
	failed := false
	for {
		n, err := r.Read(buf)
		if n > 0 && !failed {
			if werr := s.w.WriteMessage(msgType, buf[:n]); werr != nil {
				// Keep draining so the child never blocks on a full pipe.
				failed = true
				clientGone()
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) forwardLoopbackToTCP(ctx context.Context, loopback net.Conn, clientGone func()) {
	// Once nothing forwards fio's requests (a malformed message, a dead
	// client), close the loopback so fio fails them with EIO instead of
	// waiting for replies that will never come.
	defer loopback.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := protocol.ReadMessageFrom(loopback)
		if err != nil {
			return
		}
		if !protocol.IsFileIORequest(msg.Type) {
			continue
		}
		if err := s.w.WriteMessage(msg.Type, msg.Payload); err != nil {
			clientGone()
			return
		}
	}
}

func (s *Session) dispatchTCPMessages(ctx context.Context, clientGone func()) {
	fileResponseCopyBuf := make([]byte, 256*1024)

	for {
		if ctx.Err() != nil {
			return
		}
		msgType, payloadLen, header, err := readMessageHeader(s.conn)
		if err != nil {
			// The client disconnected or sent garbage.
			clientGone()
			return
		}

		s.lastRecv.Store(MonoNow())

		if protocol.IsFileIOResponse(msgType) {
			select {
			case <-s.loopbackReady:
				if err := relayExact(s.loopback, header[:], s.conn, int64(payloadLen), fileResponseCopyBuf); err != nil {
					clientGone()
					return
				}
			case <-ctx.Done():
				return
			}
			continue
		}

		payload, err := readMessagePayload(s.conn, payloadLen)
		if err != nil {
			clientGone()
			return
		}

		switch {
		case msgType == protocol.MsgStdin:
			if err := s.stdin.push(payload); err != nil {
				log.Printf("session: %v", err)
				clientGone()
				return
			}
		case msgType == protocol.MsgStdinClose:
			s.stdin.closeInput()
		case msgType == protocol.MsgCancel:
			go s.proc.Terminate()
		case msgType == protocol.MsgPing:
			// Never wait behind another write here: this goroutine also
			// relays the fio replies the child is waiting for.
			s.w.TryWriteMessage(protocol.MsgPong, payload)
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

// relayExact writes header and then n bytes read from src to dst. Only an
// error reading src is returned. If dst fails (the child exited and its
// loopback is closed while a reply was in flight), the rest of the payload is
// still read and dropped, so the client stream stays in sync and the exit
// code can still be delivered.
func relayExact(dst io.Writer, header []byte, src io.Reader, n int64, buf []byte) error {
	_, dstErr := dst.Write(header)
	for n > 0 {
		chunk := buf
		if int64(len(chunk)) > n {
			chunk = chunk[:n]
		}
		m, err := src.Read(chunk)
		if m > 0 {
			n -= int64(m)
			if dstErr == nil {
				_, dstErr = dst.Write(chunk[:m])
			}
		}
		if err != nil {
			if n == 0 {
				return nil
			}
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}
	return nil
}

func (s *Session) keepalive(ctx context.Context, clientGone func()) {
	ticker := time.NewTicker(s.keepaliveEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check for a silent client before pinging, and never wait
			// behind a write: a write stuck on a dead connection would
			// otherwise stop this loop from ever noticing.
			lastRecv := MonoTime(s.lastRecv.Load())
			if time.Since(lastRecv) >= s.keepaliveRecv {
				log.Printf("session: client keepalive timeout")
				clientGone()
				return
			}
			if time.Since(s.w.LastSendTime()) >= s.keepaliveSend {
				s.w.TryWriteMessage(protocol.MsgPing, nil)
			}
		}
	}
}

// stdinQueue hands stdin from the dispatcher to writeStdin. The dispatcher
// must never block on the child's stdin: it also relays the fio replies, and
// a child waiting for a reply isn't reading stdin. The client keeps at most
// protocol.StdinWindow bytes unacknowledged, which bounds the queue.
type stdinQueue struct {
	mu      sync.Mutex
	chunks  [][]byte
	closed  bool // MsgStdinClose received
	unacked int  // bytes received and not yet acknowledged
	wake    chan struct{}
}

func (q *stdinQueue) push(p []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(p) == 0 {
		return nil
	}
	if q.unacked+len(p) > protocol.StdinWindow {
		return fmt.Errorf("client sent more than %d unacknowledged stdin bytes", protocol.StdinWindow)
	}
	q.unacked += len(p)
	q.chunks = append(q.chunks, p)
	q.signal()
	return nil
}

func (q *stdinQueue) closeInput() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *stdinQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// writeStdin writes queued stdin to the child in order and acknowledges each
// chunk once written (or dropped, if the child no longer reads stdin), then
// closes the child's stdin after MsgStdinClose.
func (s *Session) writeStdin(ctx context.Context) {
	q := &s.stdin
	stdin := s.proc.Stdin()
	for {
		q.mu.Lock()
		if len(q.chunks) == 0 {
			closed := q.closed
			q.mu.Unlock()
			if closed {
				stdin.Close()
				return
			}
			select {
			case <-q.wake:
			case <-ctx.Done():
				return
			case <-s.proc.Done():
				return
			}
			continue
		}
		chunk := q.chunks[0]
		q.chunks[0] = nil
		q.chunks = q.chunks[1:]
		q.mu.Unlock()

		// An error means the child closed stdin or exited; the data has
		// nowhere to go, but the client still gets its window back.
		stdin.Write(chunk)

		q.mu.Lock()
		q.unacked -= len(chunk)
		q.mu.Unlock()
		s.w.WriteMessage(protocol.MsgStdinAck, protocol.EncodeStdinAck(uint32(len(chunk))))
	}
}
