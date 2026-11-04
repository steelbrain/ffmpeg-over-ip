package process

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const KillTimeout = 5 * time.Second

// The loopback listener accepts any local connection, so fio proves it is
// the child this process started: the child gets a random token in
// FFOIP_TOKEN, and fio's first bytes on the connection are
// LoopbackHelloMagic followed by that token (see fio_init_tunnel in
// fio/fio.c). Connections that don't send it within loopbackHelloTimeout are
// dropped; the first that does is the loopback, and the rest are closed.
const (
	LoopbackHelloMagic   = "FIO1"
	loopbackTokenBytes   = 32 // hex-encoded: 64 characters, FIO_TOKEN_LEN
	loopbackHelloTimeout = 5 * time.Second
)

// LoopbackHello returns what fio sends first on the loopback connection when
// started with FFOIP_TOKEN=token.
func LoopbackHello(token string) []byte {
	return append([]byte(LoopbackHelloMagic), token...)
}

// Process manages a child process with a loopback listener for fio.
// It does not read or write protocol messages — the caller is responsible
// for all multiplexing between the loopback connection, child pipes, and
// any external connection.
type Process struct {
	programPath string
	args        []string

	cmd      *exec.Cmd
	listener net.Listener
	token    string

	// loopbackMu guards loopbackConn, loopbackClosed and pendingConns.
	loopbackMu     sync.Mutex
	loopbackConn   net.Conn
	loopbackClosed bool
	pendingConns   map[net.Conn]struct{}
	loopbackOnce   sync.Once
	loopbackDone   chan struct{}

	stdinPipe  io.WriteCloser
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser

	waitDone chan struct{}
	waitErr  error
}

func NewProcess(programPath string, args []string) *Process {
	return &Process{
		programPath:  programPath,
		args:         args,
		loopbackDone: make(chan struct{}),
		waitDone:     make(chan struct{}),
		pendingConns: make(map[net.Conn]struct{}),
	}
}

// Start launches the child process with FFOIP_PORT and FFOIP_TOKEN set and
// starts the loopback listener. Returns immediately — the loopback accept and
// process wait happen in background goroutines.
func (p *Process) Start(ctx context.Context) error {
	tokenBytes := make([]byte, loopbackTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("failed to generate loopback token: %w", err)
	}
	p.token = hex.EncodeToString(tokenBytes)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to create loopback listener: %w", err)
	}
	p.listener = listener

	port := listener.Addr().(*net.TCPAddr).Port

	cmd := exec.Command(p.programPath, p.args...)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("FFOIP_PORT=%d", port), "FFOIP_TOKEN="+p.token)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		listener.Close()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	p.stdinPipe = stdinPipe

	// Use os.Pipe instead of cmd.StdoutPipe/StderrPipe. cmd.Wait()
	// closes pipes it creates, racing with concurrent reads. With
	// os.Pipe we own the read end so it stays open until we close it.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		listener.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutW

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		listener.Close()
		stdoutR.Close()
		stdoutW.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	cmd.Stderr = stderrW

	if err := startCmd(cmd, p.waitDone); err != nil {
		listener.Close()
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}
	p.cmd = cmd

	// Close write ends in parent — only the child writes to these.
	stdoutW.Close()
	stderrW.Close()
	p.stdoutPipe = stdoutR
	p.stderrPipe = stderrR

	// Wait for process in background (ensures cmd.Wait is called exactly once)
	go func() {
		p.waitErr = cmd.Wait()
		close(p.waitDone)
		// Close listener to unblock Accept if process exits without connecting
		listener.Close()
	}()

	go p.acceptLoopback(listener)

	return nil
}

// acceptLoopback accepts connections until one sends the hello, the listener
// is closed (the child exited, or CloseLoopback), or accepting fails. Each
// connection is checked on its own goroutine, so a local process that
// connects first and sends nothing can't hold up fio.
func (p *Process) acceptLoopback(listener net.Listener) {
	want := LoopbackHello(p.token)
	var wg sync.WaitGroup
	var delay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			// A transient error (EMFILE, ENFILE, ECONNABORTED) must not end
			// accepting: fio would wait forever on a connection nobody
			// accepts. Retry with backoff until the listener is closed.
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			time.Sleep(delay)
			continue
		}
		delay = 0
		if !p.trackPending(conn) {
			conn.Close()
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := readHello(conn, want)
			if !ok || !p.publishLoopback(conn) {
				p.dropPending(conn)
			}
		}()
	}
	wg.Wait()
	p.loopbackOnce.Do(func() { close(p.loopbackDone) })
}

// readHello reports whether conn starts with want, read within
// loopbackHelloTimeout. The comparison takes the same time wherever the
// first difference is.
func readHello(conn net.Conn, want []byte) bool {
	got := make([]byte, len(want))
	conn.SetReadDeadline(time.Now().Add(loopbackHelloTimeout))
	_, err := io.ReadFull(conn, got)
	conn.SetReadDeadline(time.Time{})
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func (p *Process) trackPending(conn net.Conn) bool {
	p.loopbackMu.Lock()
	defer p.loopbackMu.Unlock()
	if p.loopbackConn != nil || p.loopbackClosed {
		return false
	}
	p.pendingConns[conn] = struct{}{}
	return true
}

func (p *Process) dropPending(conn net.Conn) {
	p.loopbackMu.Lock()
	delete(p.pendingConns, conn)
	p.loopbackMu.Unlock()
	conn.Close()
}

// publishLoopback makes conn the loopback unless there already is one or
// CloseLoopback ran. It stops accepting and closes the other connections.
func (p *Process) publishLoopback(conn net.Conn) bool {
	p.loopbackMu.Lock()
	defer p.loopbackMu.Unlock()
	if p.loopbackConn != nil || p.loopbackClosed {
		return false
	}
	delete(p.pendingConns, conn)
	p.loopbackConn = conn
	p.listener.Close()
	for other := range p.pendingConns {
		other.Close()
	}
	p.loopbackOnce.Do(func() { close(p.loopbackDone) })
	return true
}

// Loopback blocks until either fio connects or the child exits.
// Returns the fio connection, or nil if the child exited without connecting.
func (p *Process) Loopback() net.Conn {
	select {
	case <-p.loopbackDone:
		return p.loopbackConn
	case <-p.waitDone:
		// Child exited — give accept a moment to finish in case it raced
		select {
		case <-p.loopbackDone:
			return p.loopbackConn
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	}
}

// Stdout returns the child's stdout pipe.
func (p *Process) Stdout() io.ReadCloser { return p.stdoutPipe }

// Stderr returns the child's stderr pipe.
func (p *Process) Stderr() io.ReadCloser { return p.stderrPipe }

// Stdin returns the child's stdin pipe.
func (p *Process) Stdin() io.WriteCloser { return p.stdinPipe }

// Done is closed once the child has exited and been waited for.
func (p *Process) Done() <-chan struct{} { return p.waitDone }

// Wait blocks until the child exits and returns the exit code.
func (p *Process) Wait() (int, error) {
	<-p.waitDone
	return getExitCode(p.waitErr), nil
}

// Signal sends a signal to the child process.
func (p *Process) Signal(sig os.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return p.cmd.Process.Signal(sig)
}

// Terminate sends SIGTERM, waits up to KillTimeout, then sends SIGKILL. It
// returns once the child has exited.
func (p *Process) Terminate() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	select {
	case <-p.waitDone:
		return
	default:
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Windows can't deliver SIGTERM, so waiting would only delay the
		// kill by KillTimeout.
		_ = p.cmd.Process.Kill()
		<-p.waitDone
		return
	}
	select {
	case <-p.waitDone:
	case <-time.After(KillTimeout):
		_ = p.cmd.Process.Kill()
		<-p.waitDone
	}
}

// CloseLoopback closes the loopback listener and connection, and any
// connection still being checked.
func (p *Process) CloseLoopback() {
	p.loopbackMu.Lock()
	defer p.loopbackMu.Unlock()
	p.loopbackClosed = true
	if p.listener != nil {
		p.listener.Close()
	}
	if p.loopbackConn != nil {
		p.loopbackConn.Close()
	}
	for conn := range p.pendingConns {
		conn.Close()
	}
}

func getExitCode(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal())
			}
			return status.ExitStatus()
		}
	}
	return -1
}
