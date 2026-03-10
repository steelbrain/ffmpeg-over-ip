package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/auth"
	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

func TestApplyRewrites(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		rewrites [][2]string
		want     []string
		sameRef  bool // if true, expect returned slice is the same reference as input
	}{
		{
			name:     "empty rewrites returns same slice",
			args:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			rewrites: nil,
			want:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			sameRef:  true,
		},
		{
			name:     "empty rewrites with explicit empty slice returns same slice",
			args:     []string{"-c:v", "h264_nvenc"},
			rewrites: [][2]string{},
			want:     []string{"-c:v", "h264_nvenc"},
			sameRef:  true,
		},
		{
			name:     "single rewrite replaces codec",
			args:     []string{"-c:v", "h264_nvenc", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "h264_qsv", "-i", "input.mp4"},
		},
		{
			name:     "multiple rewrites applied in order",
			args:     []string{"-c:v", "h264_nvenc", "-preset", "fast"},
			rewrites: [][2]string{
				{"h264_nvenc", "h264_qsv"},
				{"fast", "medium"},
			},
			want: []string{"-c:v", "h264_qsv", "-preset", "medium"},
		},
		{
			name:     "rewrite that does not match leaves args unchanged",
			args:     []string{"-c:v", "libx264", "-i", "input.mp4"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v", "libx264", "-i", "input.mp4"},
		},
		{
			name:     "rewrite applied to all args not just first match",
			args:     []string{"h264_nvenc", "foo", "h264_nvenc"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"h264_qsv", "foo", "h264_qsv"},
		},
		{
			name:     "rewrite with empty replacement deletes pattern",
			args:     []string{"-nostdin", "-c:v", "libx264"},
			rewrites: [][2]string{{"-nostdin", ""}},
			want:     []string{"", "-c:v", "libx264"},
		},
		{
			name:     "multiple occurrences of pattern in one arg",
			args:     []string{"aa-bb-aa", "cc"},
			rewrites: [][2]string{{"aa", "xx"}},
			want:     []string{"xx-bb-xx", "cc"},
		},
		{
			name: "chained rewrites where first produces text matched by second",
			args: []string{"alpha"},
			rewrites: [][2]string{
				{"alpha", "beta"},
				{"beta", "gamma"},
			},
			want: []string{"gamma"},
		},
		{
			name:     "empty args with non-empty rewrites returns empty result",
			args:     []string{},
			rewrites: [][2]string{{"a", "b"}},
			want:     []string{},
		},
		{
			name:     "rewrite matching part of arg",
			args:     []string{"-c:v h264_nvenc", "-preset fast"},
			rewrites: [][2]string{{"h264_nvenc", "h264_qsv"}},
			want:     []string{"-c:v h264_qsv", "-preset fast"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRewrites(tt.args, tt.rewrites)

			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}

			if tt.sameRef {
				if len(tt.args) > 0 && &got[0] != &tt.args[0] {
					t.Error("expected returned slice to be the same reference as input, but got a copy")
				}
			}
		})
	}
}

func TestApplyRewritesDoesNotMutateInput(t *testing.T) {
	original := []string{"h264_nvenc", "fast"}
	argsCopy := make([]string, len(original))
	copy(argsCopy, original)

	rewrites := [][2]string{{"h264_nvenc", "h264_qsv"}}
	_ = applyRewrites(original, rewrites)

	for i := range original {
		if original[i] != argsCopy[i] {
			t.Errorf("input was mutated: arg[%d] = %q, want %q", i, original[i], argsCopy[i])
		}
	}
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

	go handleConnection(ctx, serverConn, cfg, "/bin/echo", "/bin/echo")

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
