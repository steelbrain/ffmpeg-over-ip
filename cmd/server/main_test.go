package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/auth"
	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// buildEchoStub compiles a minimal cross-platform `echo` clone into the
// test's temp dir and returns its path. It prints argv (excluding argv[0])
// joined by spaces, then exits 0. Used in place of /bin/echo, which doesn't
// exist on Windows.
func buildEchoStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "echo-stub")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	const code = `package main
import (
	"fmt"
	"os"
	"strings"
)
func main() {
	fmt.Println(strings.Join(os.Args[1:], " "))
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatalf("write stub source: %v", err)
	}
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("go build echo stub: %v: %s", err, out)
	}
	return bin
}

func TestSendError(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errMsg := "something went wrong"

	// Write error from server side in a goroutine to avoid blocking
	done := make(chan struct{})
	go func() {
		defer close(done)
		sendError(server, errMsg)
	}()

	// Read the message from the client side
	msg, err := protocol.ReadMessageFrom(client)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	<-done

	if msg.Type != protocol.MsgError {
		t.Errorf("message type = 0x%02x, want 0x%02x (MsgError)", msg.Type, protocol.MsgError)
	}

	if string(msg.Payload) != errMsg {
		t.Errorf("payload = %q, want %q", string(msg.Payload), errMsg)
	}
}

func TestSendErrorEmptyMessage(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sendError(server, "")
	}()

	msg, err := protocol.ReadMessageFrom(client)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	<-done

	if msg.Type != protocol.MsgError {
		t.Errorf("message type = 0x%02x, want 0x%02x (MsgError)", msg.Type, protocol.MsgError)
	}

	if len(msg.Payload) != 0 {
		t.Errorf("payload length = %d, want 0", len(msg.Payload))
	}
}

// readAllMessages reads protocol messages from r until EOF or error.
func readAllMessages(r io.Reader) []*protocol.Message {
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

// makeCommandPayload creates a properly encoded CommandMessage payload.
func makeCommandPayload(secret string, program uint8, args []string) []byte {
	nonce := [protocol.NonceLength]byte{1, 2, 3}
	sig := auth.Sign(secret, protocol.CurrentVersion, nonce, program, args)
	cmd := &protocol.CommandMessage{
		Nonce:     nonce,
		Signature: sig,
		Program:   program,
		Args:      args,
	}
	return cmd.Encode()
}

func TestHandleConnectionBadFirstMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	cfg := &config.ServerConfig{AuthSecret: "test-secret"}

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

	// Send a MsgPing instead of MsgCommand
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgPing, nil); err != nil {
		t.Fatalf("failed to write ping: %v", err)
	}

	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	msg := msgs[0]
	if msg.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (0x%02x), got 0x%02x", protocol.MsgError, msg.Type)
	}
	if !strings.Contains(string(msg.Payload), "expected command") {
		t.Errorf("error message %q does not contain %q", string(msg.Payload), "expected command")
	}
}

func TestHandleConnectionInvalidCommand(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	cfg := &config.ServerConfig{AuthSecret: "test-secret"}

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

	// Send a MsgCommand with a 1-byte payload (too short to decode)
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, []byte{0x01}); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	msg := msgs[0]
	if msg.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (0x%02x), got 0x%02x", protocol.MsgError, msg.Type)
	}
	if !strings.Contains(string(msg.Payload), "invalid command") {
		t.Errorf("error message %q does not contain %q", string(msg.Payload), "invalid command")
	}
}

func TestHandleConnectionAuthFailure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	cfg := &config.ServerConfig{AuthSecret: "correct-secret"}

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

	// Sign with wrong secret
	payload := makeCommandPayload("wrong-secret", protocol.ProgramFFmpeg, []string{"-version"})
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, payload); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	msg := msgs[0]
	if msg.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (0x%02x), got 0x%02x", protocol.MsgError, msg.Type)
	}
	if !strings.Contains(string(msg.Payload), "authentication failed") {
		t.Errorf("error message %q does not contain %q", string(msg.Payload), "authentication failed")
	}
}

func TestHandleConnectionUnknownProgram(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	secret := "test-secret"
	cfg := &config.ServerConfig{AuthSecret: secret}

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

	// Sign with correct secret but unknown program 0xFF
	payload := makeCommandPayload(secret, 0xFF, []string{"-version"})
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, payload); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	msg := msgs[0]
	if msg.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (0x%02x), got 0x%02x", protocol.MsgError, msg.Type)
	}
	if !strings.Contains(string(msg.Payload), "unknown program") {
		t.Errorf("error message %q does not contain %q", string(msg.Payload), "unknown program")
	}
}

func TestHandleConnectionSuccess(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	secret := "test-secret"
	cfg := &config.ServerConfig{AuthSecret: secret}

	echoBin := buildEchoStub(t)
	go handleConnection(ctx, serverConn, cfg, echoBin, echoBin)

	// Send a valid command that runs "echo -version" (echo will just print "-version")
	payload := makeCommandPayload(secret, protocol.ProgramFFmpeg, []string{"-version"})
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, payload); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	// Read all messages with a timeout
	type result struct {
		msgs []*protocol.Message
	}
	ch := make(chan result, 1)
	go func() {
		ch <- result{msgs: readAllMessages(clientConn)}
	}()

	var msgs []*protocol.Message
	select {
	case r := <-ch:
		msgs = r.msgs
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for response messages")
	}

	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	// We should see at least stdout output and an exit code
	var gotStdout bool
	var gotExitCode bool
	var exitCode int

	for _, msg := range msgs {
		switch msg.Type {
		case protocol.MsgStdout:
			gotStdout = true
			if !strings.Contains(string(msg.Payload), "-version") {
				t.Errorf("stdout %q does not contain %q", string(msg.Payload), "-version")
			}
		case protocol.MsgExitCode:
			gotExitCode = true
			if len(msg.Payload) >= 4 {
				exitCode = int(binary.BigEndian.Uint32(msg.Payload))
			}
		}
	}

	if !gotStdout {
		t.Error("expected MsgStdout in response, got none")
	}
	if !gotExitCode {
		t.Error("expected MsgExitCode in response, got none")
	}
	if gotExitCode && exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestHandleConnectionProcessNotFound(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	secret := "test-secret"
	cfg := &config.ServerConfig{AuthSecret: secret}

	// Use a nonexistent binary path
	go handleConnection(ctx, serverConn, cfg, "/nonexistent/binary/ffmpeg", "/nonexistent/binary/ffprobe")

	payload := makeCommandPayload(secret, protocol.ProgramFFmpeg, []string{"-version"})
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, payload); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	// Read all messages with a timeout
	type result struct {
		msgs []*protocol.Message
	}
	ch := make(chan result, 1)
	go func() {
		ch <- result{msgs: readAllMessages(clientConn)}
	}()

	var msgs []*protocol.Message
	select {
	case r := <-ch:
		msgs = r.msgs
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for response messages")
	}

	if len(msgs) == 0 {
		t.Fatal("expected at least one response message, got none")
	}

	msg := msgs[0]
	if msg.Type != protocol.MsgError {
		t.Fatalf("expected MsgError (0x%02x), got 0x%02x", protocol.MsgError, msg.Type)
	}
}
