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
	"github.com/steelbrain/ffmpeg-over-ip/internal/handshake"
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

// readAllMessages reads protocol messages from r until EOF or error,
// leaving out the handshake's MsgAccepted.
func readAllMessages(r io.Reader) []*protocol.Message {
	var msgs []*protocol.Message
	for {
		msg, err := protocol.ReadMessageFrom(r)
		if err != nil {
			break
		}
		if msg.Type == protocol.MsgAccepted {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// startHandshake plays the client side of the handshake by hand, so tests
// can sign commands with any secret: it sends a hello and returns both
// nonces from the server's challenge.
func startHandshake(t *testing.T, conn net.Conn) (clientNonce, serverNonce [protocol.NonceLength]byte) {
	t.Helper()
	clientNonce = [protocol.NonceLength]byte{1, 2, 3}
	hello := &protocol.HelloMessage{Nonce: clientNonce}
	if err := protocol.WriteMessageTo(conn, protocol.MsgHello, hello.Encode()); err != nil {
		t.Fatalf("failed to write hello: %v", err)
	}
	msg, err := protocol.ReadMessageFrom(conn)
	if err != nil {
		t.Fatalf("failed to read challenge: %v", err)
	}
	if msg.Type != protocol.MsgChallenge {
		t.Fatalf("expected MsgChallenge, got 0x%02x (%q)", msg.Type, msg.Payload)
	}
	ch, err := protocol.DecodeChallengeMessage(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return clientNonce, ch.Nonce
}

// makeCommandPayload creates a CommandMessage payload for one connection.
func makeCommandPayload(secret string, clientNonce, serverNonce [protocol.NonceLength]byte, program uint8, args []string) []byte {
	cmd := &protocol.CommandMessage{
		Nonce:     serverNonce,
		Signature: auth.Sign(secret, protocol.CurrentVersion, clientNonce, serverNonce, program, args),
		Program:   program,
		Args:      args,
	}
	return cmd.Encode()
}

// sendCommand runs the handshake and sends a command signed with secret.
func sendCommand(t *testing.T, conn net.Conn, secret string, program uint8, args []string) {
	t.Helper()
	cn, sn := startHandshake(t, conn)
	if err := protocol.WriteMessageTo(conn, protocol.MsgCommand, makeCommandPayload(secret, cn, sn, program, args)); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}
}

func TestHandleConnectionBadFirstMessage(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	ctx := context.Background()
	cfg := &config.ServerConfig{AuthSecret: "test-secret"}

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

	// Send a MsgPing instead of MsgHello
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
	if !strings.Contains(string(msg.Payload), "expected hello") {
		t.Errorf("error message %q does not contain %q", string(msg.Payload), "expected hello")
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
	sendCommand(t, clientConn, "wrong-secret", protocol.ProgramFFmpeg, []string{"-version"})

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
	sendCommand(t, clientConn, secret, 0xFF, []string{"-version"})

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
	sendCommand(t, clientConn, secret, protocol.ProgramFFmpeg, []string{"-version"})

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

	sendCommand(t, clientConn, secret, protocol.ProgramFFmpeg, []string{"-version"})

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

// A command recorded from one connection must not run on another: the
// signature covers the server's nonce, which is new for every connection.
func TestHandleConnectionReplayRejected(t *testing.T) {
	secret := "test-secret"
	cfg := &config.ServerConfig{AuthSecret: secret}
	args := []string{"-version"}

	// Connection 1: record a valid command (the server would run it).
	c1, s1 := net.Pipe()
	go handleConnection(context.Background(), s1, cfg, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	cn, sn := startHandshake(t, c1)
	recorded := makeCommandPayload(secret, cn, sn, protocol.ProgramFFmpeg, args)
	c1.Close()

	// Connection 2: replay it after a fresh hello with the same client nonce.
	c2, s2 := net.Pipe()
	defer c2.Close()
	go handleConnection(context.Background(), s2, cfg, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	startHandshake(t, c2)
	if err := protocol.WriteMessageTo(c2, protocol.MsgCommand, recorded); err != nil {
		t.Fatal(err)
	}
	msgs := readAllMessages(c2)
	if len(msgs) == 0 || msgs[0].Type != protocol.MsgError || string(msgs[0].Payload) != "authentication failed" {
		t.Fatalf("replayed command: got %v, want MsgError \"authentication failed\"", msgs)
	}
}

// Before the handshake completes, a peer can't make the server allocate
// more than protocol.MaxHandshakePayload.
func TestHandleConnectionOversizedHelloRejected(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		handleConnection(context.Background(), serverConn, &config.ServerConfig{AuthSecret: "s"}, "/bin/echo", "/bin/echo")
		close(done)
	}()
	// Header only: type + a 100 MiB length. The server must give up without
	// waiting for (or allocating) the payload.
	hdr := []byte{protocol.MsgHello, 0x06, 0x40, 0x00, 0x00}
	if _, err := clientConn.Write(hdr); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server kept reading an oversized pre-auth message")
	}
}

// A client of an older release sends its command first; it gets the version
// mismatch it always got.
func TestHandleConnectionPreviousReleaseClient(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go handleConnection(context.Background(), serverConn, &config.ServerConfig{AuthSecret: "s"}, "/bin/echo", "/bin/echo")
	old := (&protocol.CommandMessage{Program: protocol.ProgramFFmpeg, Args: []string{"-version"}}).Encode()
	old[0] = protocol.CurrentVersion - 1
	if err := protocol.WriteMessageTo(clientConn, protocol.MsgCommand, old); err != nil {
		t.Fatal(err)
	}
	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 || msgs[0].Type != protocol.MsgError || !strings.Contains(string(msgs[0].Payload), "protocol version mismatch") {
		t.Fatalf("got %v, want a protocol version mismatch error", msgs)
	}
}

// Connections that never send anything must not keep a real client from
// completing its handshake.
func TestIdleConnectionsDontLockOutClients(t *testing.T) {
	cfg := &config.ServerConfig{AuthSecret: "secret"}
	var idle []net.Conn
	for i := 0; i < 300; i++ {
		c, s := net.Pipe()
		idle = append(idle, c)
		go handleConnection(context.Background(), s, cfg, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	}
	defer func() {
		for _, c := range idle {
			c.Close()
		}
	}()
	c, s := net.Pipe()
	defer c.Close()
	go handleConnection(context.Background(), s, cfg, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	done := make(chan error, 1)
	go func() { done <- handshake.Client(c, "secret", protocol.ProgramFFmpeg, []string{"-version"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handshake failed behind idle connections: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handshake blocked behind idle connections")
	}
}

// A command that arrives while the server is shutting down doesn't start
// ffmpeg (which would only be stopped again, or outlive the server where
// nothing ties it to the server's lifetime).
func TestHandleConnectionDuringShutdown(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go handleConnection(ctx, serverConn, &config.ServerConfig{AuthSecret: "s"}, "/nonexistent/ffmpeg", "/nonexistent/ffprobe")
	sendCommand(t, clientConn, "s", protocol.ProgramFFmpeg, []string{"-version"})
	msgs := readAllMessages(clientConn)
	if len(msgs) == 0 || msgs[0].Type != protocol.MsgError || string(msgs[0].Payload) != "server is shutting down" {
		t.Fatalf("got %v, want MsgError \"server is shutting down\"", msgs)
	}
}
