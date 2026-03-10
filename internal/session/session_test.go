package session

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// readMessages reads protocol messages from r until EOF or error.
func readMessages(r io.Reader) []*protocol.Message {
	var msgs []*protocol.Message
	for {
		msg, err := protocol.ReadMessageFrom(r)
		if err != nil {
			break
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func TestSessionEchoStdout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("echo", []string{"hello session"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var gotStdout bool
	var gotExitCode bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout && string(msg.Payload) == "hello session\n" {
			gotStdout = true
		}
		if msg.Type == protocol.MsgExitCode {
			gotExitCode = true
		}
	}
	if !gotStdout {
		t.Fatal("did not receive stdout message")
	}
	if !gotExitCode {
		t.Fatal("did not receive exit code message")
	}
}

func TestSessionExitCode(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sh", []string{"-c", "exit 7"})
	proc.Start(context.Background())

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 7 {
		t.Fatalf("exit code = %d, want 7", exitCode)
	}

	// Find the exit code message
	for _, msg := range msgs {
		if msg.Type == protocol.MsgExitCode {
			code := int(binary.BigEndian.Uint32(msg.Payload))
			if code != 7 {
				t.Fatalf("MsgExitCode payload = %d, want 7", code)
			}
			return
		}
	}
	t.Fatal("no MsgExitCode received")
}

func TestSessionStdinForward(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("cat", nil)
	proc.Start(context.Background())

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send stdin data
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, []byte("piped input"))
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	<-done

	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if string(stdout) != "piped input" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "piped input")
	}
}

func TestSessionCancel(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sleep", []string{"3600"})
	proc.Start(context.Background())

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		done <- code
	}()

	// Drain messages from server in background so writes don't block
	go readMessages(clientConn)

	// Give process time to start
	time.Sleep(100 * time.Millisecond)

	// Send cancel
	protocol.WriteMessageTo(clientConn, protocol.MsgCancel, nil)

	select {
	case <-done:
		// Process was killed
	case <-time.After(10 * time.Second):
		t.Fatal("session did not terminate after cancel")
	}
}

func TestSessionPingPong(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sleep", []string{"1"})
	proc.Start(context.Background())

	go func() {
		sess := NewSession(serverConn, proc)
		sess.Run(context.Background())
		serverConn.Close()
	}()

	// Send ping, expect pong
	protocol.WriteMessageTo(clientConn, protocol.MsgPing, []byte("test"))

	msg, err := protocol.ReadMessageFrom(clientConn)
	if err != nil {
		t.Fatalf("failed to read pong: %v", err)
	}
	if msg.Type != protocol.MsgPong {
		t.Fatalf("expected MsgPong (0x%02x), got 0x%02x", protocol.MsgPong, msg.Type)
	}
	if string(msg.Payload) != "test" {
		t.Fatalf("pong payload = %q, want %q", string(msg.Payload), "test")
	}
}

func TestSessionNoLoopback(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// echo doesn't use fio — no loopback connection
	proc := process.NewProcess("echo", []string{"no fio"})
	proc.Start(context.Background())

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var gotStdout, gotExitCode bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			gotStdout = true
		}
		if msg.Type == protocol.MsgExitCode {
			gotExitCode = true
		}
	}
	if !gotStdout {
		t.Fatal("did not receive stdout")
	}
	if !gotExitCode {
		t.Fatal("did not receive exit code")
	}
}

func TestSessionStderr(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sh", []string{"-c", "echo stderr_test >&2"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	<-done

	var stderr []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStderr {
			stderr = append(stderr, msg.Payload...)
		}
	}
	if string(stderr) != "stderr_test\n" {
		t.Fatalf("stderr = %q, want %q", string(stderr), "stderr_test\n")
	}
}

func TestSessionStdoutAndStderr(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sh", []string{"-c", "echo out1; echo err1 >&2"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	<-done

	var stdout, stderr []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
		if msg.Type == protocol.MsgStderr {
			stderr = append(stderr, msg.Payload...)
		}
	}
	if string(stdout) != "out1\n" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "out1\n")
	}
	if string(stderr) != "err1\n" {
		t.Fatalf("stderr = %q, want %q", string(stderr), "err1\n")
	}
}

func TestSessionExitCodeZero(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("true", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	for _, msg := range msgs {
		if msg.Type == protocol.MsgExitCode {
			code := int(binary.BigEndian.Uint32(msg.Payload))
			if code != 0 {
				t.Fatalf("MsgExitCode payload = %d, want 0", code)
			}
			return
		}
	}
	t.Fatal("no MsgExitCode received")
}

func TestSessionExitCodeNonZero(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sh", []string{"-c", "exit 99"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 99 {
		t.Fatalf("exit code = %d, want 99", exitCode)
	}

	for _, msg := range msgs {
		if msg.Type == protocol.MsgExitCode {
			code := int(binary.BigEndian.Uint32(msg.Payload))
			if code != 99 {
				t.Fatalf("MsgExitCode payload = %d, want 99", code)
			}
			return
		}
	}
	t.Fatal("no MsgExitCode received")
}

func TestSessionStdinClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Close stdin without sending any data — cat should exit immediately
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	// Verify no stdout data was produced
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout && len(msg.Payload) > 0 {
			t.Fatalf("unexpected stdout data: %q", string(msg.Payload))
		}
	}
}

func TestSessionContextCancel(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Drain messages so writes don't block
	go readMessages(clientConn)

	// Give process time to start
	time.Sleep(100 * time.Millisecond)

	// Send cancel to kill the child, then close connection to unblock IO
	protocol.WriteMessageTo(clientConn, protocol.MsgCancel, nil)
	clientConn.Close()

	select {
	case <-done:
		// Session exited after cancel + disconnect
	case <-time.After(10 * time.Second):
		t.Fatal("session did not terminate after cancel and disconnect")
	}
}

func TestSessionLargeStdout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Use cat with stdin so we control when the process exits.
	// Send 100KB via stdin, close stdin, cat outputs it and exits.
	// This avoids races where fast-exiting processes (head, dd) close
	// before pipeOutput finishes flushing through the synchronous net.Pipe.
	proc := process.NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send 100KB via stdin
	data := make([]byte, 102400)
	for i := range data {
		data[i] = byte(i % 251)
	}
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, data)
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	<-done

	var totalBytes int
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			totalBytes += len(msg.Payload)
		}
	}
	if totalBytes != 102400 {
		t.Fatalf("total stdout bytes = %d, want 102400", totalBytes)
	}
}

func TestSessionMultiplePingPong(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sleep", []string{"2"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	go func() {
		sess := NewSession(serverConn, proc)
		sess.Run(context.Background())
		serverConn.Close()
	}()

	for i := 0; i < 3; i++ {
		payload := []byte{byte(i)}
		if err := protocol.WriteMessageTo(clientConn, protocol.MsgPing, payload); err != nil {
			t.Fatalf("ping %d: write error: %v", i, err)
		}

		msg, err := protocol.ReadMessageFrom(clientConn)
		if err != nil {
			t.Fatalf("ping %d: read error: %v", i, err)
		}
		if msg.Type != protocol.MsgPong {
			t.Fatalf("ping %d: expected MsgPong (0x%02x), got 0x%02x", i, protocol.MsgPong, msg.Type)
		}
		if len(msg.Payload) != 1 || msg.Payload[0] != byte(i) {
			t.Fatalf("ping %d: pong payload = %v, want [%d]", i, msg.Payload, i)
		}
	}
}

func TestSessionUnknownMessageDropped(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Use a process that stays alive long enough for the unknown message to be processed
	proc := process.NewProcess("sh", []string{"-c", "sleep 0.5; echo alive"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send unknown message type 0xFF while process is still running
	protocol.WriteMessageTo(clientConn, 0xFF, []byte("garbage"))

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	// Verify the session still processed stdout normally
	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if string(stdout) != "alive\n" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "alive\n")
	}
}

func TestSessionStdinData(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send 3 separate MsgStdin messages with different payloads
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, []byte("alpha "))
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, []byte("beta "))
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, []byte("gamma"))
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if string(stdout) != "alpha beta gamma" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "alpha beta gamma")
	}
}

func TestSessionEmptyStdout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("true", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var gotExitCode bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout && len(msg.Payload) > 0 {
			t.Fatalf("unexpected stdout data: %q", string(msg.Payload))
		}
		if msg.Type == protocol.MsgExitCode {
			gotExitCode = true
		}
	}
	if !gotExitCode {
		t.Fatal("no MsgExitCode received")
	}
}

func TestSessionBinaryStdout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("dd", []string{"if=/dev/urandom", "bs=256", "count=1"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if len(stdout) != 256 {
		t.Fatalf("stdout length = %d, want 256", len(stdout))
	}

	// Verify it contains at least some non-zero bytes (random data)
	allZero := true
	for _, b := range stdout {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("binary stdout was all zeros, expected random data")
	}
}

func TestSessionPongPayloadEcho(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sleep", []string{"2"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	go func() {
		sess := NewSession(serverConn, proc)
		sess.Run(context.Background())
		serverConn.Close()
	}()

	// Send ping with a 1KB payload
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	if err := protocol.WriteMessageTo(clientConn, protocol.MsgPing, payload); err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}

	msg, err := protocol.ReadMessageFrom(clientConn)
	if err != nil {
		t.Fatalf("failed to read pong: %v", err)
	}
	if msg.Type != protocol.MsgPong {
		t.Fatalf("expected MsgPong (0x%02x), got 0x%02x", protocol.MsgPong, msg.Type)
	}
	if len(msg.Payload) != 1024 {
		t.Fatalf("pong payload length = %d, want 1024", len(msg.Payload))
	}
	for i := range payload {
		if msg.Payload[i] != payload[i] {
			t.Fatalf("pong payload mismatch at byte %d: got 0x%02x, want 0x%02x", i, msg.Payload[i], payload[i])
		}
	}
}

func TestSessionStdinCloseIdempotent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send stdin data, then close twice
	protocol.WriteMessageTo(clientConn, protocol.MsgStdin, []byte("data"))
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if string(stdout) != "data" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "data")
	}
}

func TestSessionCancelAlreadyExited(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("true", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		done <- code
	}()

	// Read messages until we get MsgExitCode
	for {
		msg, err := protocol.ReadMessageFrom(clientConn)
		if err != nil {
			t.Fatalf("unexpected read error before MsgExitCode: %v", err)
		}
		if msg.Type == protocol.MsgExitCode {
			code := int(binary.BigEndian.Uint32(msg.Payload))
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			break
		}
	}

	// Process has exited, now send cancel — should not crash or hang
	protocol.WriteMessageTo(clientConn, protocol.MsgCancel, nil)

	// Close client side to let session's dispatchTCPMessages unblock and finish
	clientConn.Close()

	select {
	case <-done:
		// Session finished gracefully
	case <-time.After(10 * time.Second):
		t.Fatal("session hung after cancel on already-exited process")
	}
}

func TestSessionExitCodeMax(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("sh", []string{"-c", "exit 255"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 255 {
		t.Fatalf("exit code = %d, want 255", exitCode)
	}

	for _, msg := range msgs {
		if msg.Type == protocol.MsgExitCode {
			code := int(binary.BigEndian.Uint32(msg.Payload))
			if code != 255 {
				t.Fatalf("MsgExitCode payload = %d, want 255", code)
			}
			return
		}
	}
	t.Fatal("no MsgExitCode received")
}

func TestSessionMultipleStdinWrites(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	proc := process.NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Send 100 small MsgStdin messages
	var expected []byte
	for i := 0; i < 100; i++ {
		chunk := []byte{byte(i)}
		expected = append(expected, chunk...)
		if err := protocol.WriteMessageTo(clientConn, protocol.MsgStdin, chunk); err != nil {
			t.Fatalf("stdin write %d: %v", i, err)
		}
	}
	protocol.WriteMessageTo(clientConn, protocol.MsgStdinClose, nil)

	msgs := readMessages(clientConn)
	exitCode := <-done

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var stdout []byte
	for _, msg := range msgs {
		if msg.Type == protocol.MsgStdout {
			stdout = append(stdout, msg.Payload...)
		}
	}
	if len(stdout) != len(expected) {
		t.Fatalf("stdout length = %d, want %d", len(stdout), len(expected))
	}
	for i := range expected {
		if stdout[i] != expected[i] {
			t.Fatalf("stdout mismatch at byte %d: got 0x%02x, want 0x%02x", i, stdout[i], expected[i])
		}
	}
}

// readAsyncReader starts reading all data from r in a background goroutine.
// Returns a function that blocks until the read completes and returns the data.
func readAsyncReader(r io.Reader) func() ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(r)
		ch <- result{data, err}
	}()
	return func() ([]byte, error) { res := <-ch; return res.data, res.err }
}

func TestSessionLoopbackForwarding(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Python script that:
	// 1. Connects to FFOIP_PORT
	// 2. Sends an MsgOpen request (fio request) over loopback
	// 3. Reads the MsgOpenOk response (fio response forwarded by session)
	// 4. Prints "ok" to stdout if response type matches
	pyScript := `
import socket, struct, os, sys
port = int(os.environ['FFOIP_PORT'])
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.connect(('127.0.0.1', port))

# Send MsgOpen (0x20) request: reqID=1, fileID=1, flags=0, mode=0, path="/test"
path = b'/test'
payload = struct.pack('>HHIH', 1, 1, 0, 0) + path
header = struct.pack('>BI', 0x20, len(payload))
s.sendall(header + payload)

# Read response envelope (5 bytes: type + length)
hdr = b''
while len(hdr) < 5:
    hdr += s.recv(5 - len(hdr))
msg_type, length = struct.unpack('>BI', hdr)

# Read response payload
data = b''
while len(data) < length:
    data += s.recv(length - len(data))

# Verify it's MsgOpenOk (0x40)
if msg_type == 0x40:
    sys.stdout.write('ok\n')
else:
    sys.stdout.write('fail:0x{:02x}\n'.format(msg_type))
sys.stdout.flush()
s.close()
`

	proc := process.NewProcess("python3", []string{"-c", pyScript})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		sess := NewSession(serverConn, proc)
		code, _ := sess.Run(context.Background())
		serverConn.Close()
		done <- code
	}()

	// Session should forward the MsgOpen from loopback to us via TCP
	msg, err := protocol.ReadMessageFrom(clientConn)
	if err != nil {
		t.Fatalf("failed to read forwarded fio request: %v", err)
	}
	if msg.Type != protocol.MsgOpen {
		t.Fatalf("expected MsgOpen (0x20), got 0x%02x", msg.Type)
	}

	// Decode the open request to verify payload was forwarded correctly
	openReq, err := protocol.DecodeOpenRequest(msg.Payload)
	if err != nil {
		t.Fatalf("failed to decode open request: %v", err)
	}
	if openReq.RequestID != 1 || openReq.FileID != 1 || openReq.Path != "/test" {
		t.Fatalf("open request mismatch: %+v", openReq)
	}

	// Send back MsgOpenOk response — session should forward it to the loopback
	openOkPayload := (&protocol.OpenOkResponse{RequestID: 1, FileSize: 42}).Encode()
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgOpenOk, openOkPayload); err != nil {
		t.Fatalf("failed to send open ok: %v", err)
	}

	// Read remaining messages — stdout comes through as MsgStdout (the session
	// pipes process stdout to TCP, so we must NOT also read proc.Stdout() directly)
	msgs := readMessages(clientConn)
	exitCode := <-done

	// Collect stdout and stderr from TCP messages
	var stdout, stderr []byte
	for _, m := range msgs {
		if m.Type == protocol.MsgStdout {
			stdout = append(stdout, m.Payload...)
		}
		if m.Type == protocol.MsgStderr {
			stderr = append(stderr, m.Payload...)
		}
	}

	result := strings.TrimSpace(string(stdout))
	if result != "ok" {
		t.Fatalf("python script reported: stdout=%q stderr=%q (expected stdout='ok')", result, string(stderr))
	}

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", exitCode, string(stderr))
	}
}
