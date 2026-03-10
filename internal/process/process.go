package process

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const KillTimeout = 5 * time.Second

// Process manages a child process with a loopback listener for fio.
// It does not read or write protocol messages — the caller is responsible
// for all multiplexing between the loopback connection, child pipes, and
// any external connection.
type Process struct {
	programPath string
	args        []string

	cmd      *exec.Cmd
	listener net.Listener

	loopbackConn net.Conn
	loopbackDone chan struct{}

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
	}
}

// Start launches the child process with FFOIP_PORT set and starts the
// loopback listener. Returns immediately — the loopback accept and process
// wait happen in background goroutines.
func (p *Process) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to create loopback listener: %w", err)
	}
	p.listener = listener

	port := listener.Addr().(*net.TCPAddr).Port

	cmd := exec.Command(p.programPath, p.args...)
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("FFOIP_PORT=%d", port))

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

	if err := cmd.Start(); err != nil {
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

	// Accept loopback connection in background
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			p.loopbackConn = conn
		}
		close(p.loopbackDone)
	}()

	return nil
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

// Terminate sends SIGTERM, waits up to KillTimeout, then sends SIGKILL.
func (p *Process) Terminate() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.waitDone:
	case <-time.After(KillTimeout):
		_ = p.cmd.Process.Kill()
		<-p.waitDone
	}
}

// CloseLoopback closes the loopback listener and connection.
func (p *Process) CloseLoopback() {
	if p.listener != nil {
		p.listener.Close()
	}
	if p.loopbackConn != nil {
		p.loopbackConn.Close()
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
