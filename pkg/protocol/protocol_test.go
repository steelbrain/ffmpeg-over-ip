package protocol

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestValidateArguments(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		args      []string
		expectErr bool
	}{
		{
			name:      "valid arguments",
			toolName:  "ffmpeg",
			args:      []string{"-i", "input.mp4", "output.mp4"},
			expectErr: false,
		},
		{
			name:      "tool name with null byte",
			toolName:  "ffm\x00peg",
			args:      []string{"-i", "input.mp4"},
			expectErr: true,
		},
		{
			name:      "argument with null byte",
			toolName:  "ffmpeg",
			args:      []string{"-i", "input\x00.mp4"},
			expectErr: true,
		},
		{
			name:      "empty arguments",
			toolName:  "ffmpeg",
			args:      []string{},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateArguments(tt.toolName, tt.args)
			if tt.expectErr && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestProtobufCommandMessage(t *testing.T) {
	// Test data
	authSecret := "test-secret"
	toolName := "ffprobe"
	args := []string{"-i", "test.mp4", "-show_format"}

	// Calculate signature
	signatureArgs := append([]string{toolName}, args...)
	signature := CalculateSignature(authSecret, signatureArgs)

	// Create message
	cmdMsg := &CommandMessage{
		Version:   uint32(ProtocolVersion),
		Signature: signature,
		ToolName:  toolName,
		Arguments: args,
	}

	// Message created successfully, now test the fields

	// Verify fields
	if cmdMsg.GetVersion() != uint32(ProtocolVersion) {
		t.Errorf("version mismatch: got %d, want %d", cmdMsg.GetVersion(), ProtocolVersion)
	}
	
	if cmdMsg.GetSignature() != signature {
		t.Errorf("signature mismatch: got %q, want %q", cmdMsg.GetSignature(), signature)
	}
	
	if cmdMsg.GetToolName() != toolName {
		t.Errorf("tool name mismatch: got %q, want %q", cmdMsg.GetToolName(), toolName)
	}
	
	if len(cmdMsg.GetArguments()) != len(args) {
		t.Errorf("arguments length mismatch: got %d, want %d", len(cmdMsg.GetArguments()), len(args))
	}
	
	for i, arg := range cmdMsg.GetArguments() {
		if arg != args[i] {
			t.Errorf("argument %d mismatch: got %q, want %q", i, arg, args[i])
		}
	}
}

func TestProtobufRoundTrip(t *testing.T) {
	// Test that WriteCommandMessage -> ParseCommandMessage works
	authSecret := "test-secret-123"
	toolName := "ffmpeg"
	args := []string{"-i", "input.mkv", "-c:v", "libx264", "-preset", "medium", "output.mp4"}

	// Mock connection buffer to capture written data
	mockConn := &mockConnection{buffer: make([]byte, 0)}

	// Write command message
	err := WriteCommandMessage(mockConn, authSecret, toolName, args)
	if err != nil {
		t.Fatalf("WriteCommandMessage failed: %v", err)
	}

	// Extract the payload (skip the 5-byte header: 1 byte type + 4 bytes length)
	if len(mockConn.buffer) < 5 {
		t.Fatalf("buffer too short: %d bytes", len(mockConn.buffer))
	}
	
	payload := mockConn.buffer[5:] // Skip message header

	// Parse command message  
	version, signature, parsedToolName, parsedArgs, err := ParseCommandMessage(payload)
	if err != nil {
		t.Fatalf("ParseCommandMessage failed: %v", err)
	}

	// Verify version
	if version != ProtocolVersion {
		t.Errorf("version mismatch: got %d, want %d", version, ProtocolVersion)
	}

	// Verify tool name
	if parsedToolName != toolName {
		t.Errorf("tool name mismatch: got %q, want %q", parsedToolName, toolName)
	}

	// Verify arguments
	if len(parsedArgs) != len(args) {
		t.Errorf("arguments length mismatch: got %d, want %d", len(parsedArgs), len(args))
	}
	
	for i, arg := range parsedArgs {
		if arg != args[i] {
			t.Errorf("argument %d mismatch: got %q, want %q", i, arg, args[i])
		}
	}

	// Verify signature
	if !VerifyCommandSignature(authSecret, signature, parsedToolName, parsedArgs) {
		t.Errorf("signature verification failed")
	}
}

func TestNullByteValidation(t *testing.T) {
	authSecret := "test-secret"
	mockConn := &mockConnection{buffer: make([]byte, 0)}

	// Test null byte in tool name
	err := WriteCommandMessage(mockConn, authSecret, "ffm\x00peg", []string{"-i", "test.mp4"})
	if err == nil || !strings.Contains(err.Error(), "null bytes") {
		t.Errorf("expected null byte error for tool name, got: %v", err)
	}

	// Reset buffer
	mockConn.buffer = make([]byte, 0)

	// Test null byte in arguments
	err = WriteCommandMessage(mockConn, authSecret, "ffmpeg", []string{"-i", "test\x00.mp4"})
	if err == nil || !strings.Contains(err.Error(), "null bytes") {
		t.Errorf("expected null byte error for arguments, got: %v", err)
	}
}

// Mock connection for testing
type mockConnection struct {
	buffer []byte
}

func (m *mockConnection) Write(data []byte) (int, error) {
	m.buffer = append(m.buffer, data...)
	return len(data), nil
}

func (m *mockConnection) Read(data []byte) (int, error) {
	// Not used in these tests
	return 0, nil
}

func (m *mockConnection) Close() error {
	return nil
}

func (m *mockConnection) LocalAddr() net.Addr {
	return nil
}

func (m *mockConnection) RemoteAddr() net.Addr {
	return nil
}

func (m *mockConnection) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConnection) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConnection) SetWriteDeadline(t time.Time) error {
	return nil
}