package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
)

const (
	// Protocol message types
	MessageTypeCommand    = uint8(1)
	MessageTypeStdout     = uint8(2)
	MessageTypeStderr     = uint8(3)
	MessageTypeExitCode   = uint8(4)
	MessageTypeError      = uint8(5)
	MessageTypeCancel     = uint8(6)
	MessageTypeStdin      = uint8(7)
	MessageTypeStdinClose = uint8(8)

	// Protocol constants
	ProtocolVersion = uint8(2)
	SignatureLength = 64
)

// ConnectionInfo contains data about the connection type and address
type ConnectionInfo struct {
	Network string // "tcp" or "unix"
	Address string // host:port or socket path
}

// Message represents a protocol message
type Message struct {
	Type    uint8
	Payload []byte
}

// ParseAddress parses an address string into connection information
func ParseAddress(address string) (*ConnectionInfo, error) {
	if strings.Contains(address, ":") {
		// TCP address (host:port)
		return &ConnectionInfo{
			Network: "tcp",
			Address: address,
		}, nil
	} else if filepath.IsAbs(address) {
		// Unix socket path
		return &ConnectionInfo{
			Network: "unix",
			Address: address,
		}, nil
	}

	return nil, fmt.Errorf("invalid address format: %s (must be host:port or /path/to/socket)", address)
}

// CalculateSignature calculates the HMAC signature for authentication
func CalculateSignature(authSecret string, commandArgs []string) string {
	mac := hmac.New(sha256.New, []byte(authSecret))
	message := strings.Join(commandArgs, "\x00")
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCommandSignature verifies the HMAC signature for a command message with tool name
func VerifyCommandSignature(authSecret, signature, toolName string, commandArgs []string) bool {
	signatureArgs := append([]string{toolName}, commandArgs...)
	expected := CalculateSignature(authSecret, signatureArgs)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// validateArguments checks for null bytes in tool name and arguments
func validateArguments(toolName string, arguments []string) error {
	if strings.Contains(toolName, "\x00") {
		return fmt.Errorf("tool name contains null bytes: %q", toolName)
	}

	for i, arg := range arguments {
		if strings.Contains(arg, "\x00") {
			return fmt.Errorf("argument %d contains null bytes: %q", i, arg)
		}
	}

	return nil
}

// ReadMessage reads a protocol message from the connection
// Returns a Message struct containing the message type and payload, or an error if reading fails
func ReadMessage(conn net.Conn) (*Message, error) {
	if conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	// Read message type (1 byte)
	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, typeBuf); err != nil {
		if err == io.EOF {
			// Return EOF directly so caller can distinguish between a closed connection and other errors
			return nil, err
		}
		return nil, fmt.Errorf("error reading message type: %w", err)
	}

	// Read payload length (4 bytes, uint32)
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, fmt.Errorf("error reading payload length: %w", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	// Sanity check on payload length to avoid memory issues
	if payloadLen > 100*1024*1024 { // 100MB max payload size
		return nil, fmt.Errorf("payload length too large: %d bytes", payloadLen)
	}

	// Read payload
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return nil, fmt.Errorf("error reading payload: %w", err)
		}
	}

	return &Message{
		Type:    typeBuf[0],
		Payload: payload,
	}, nil
}

// WriteMessage writes a protocol message to the connection
// Handles serializing the message type and payload length before writing to the connection
func WriteMessage(conn net.Conn, msgType uint8, payload []byte) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	// Prepare the header: 1 byte for type + 4 bytes for payload length
	header := make([]byte, 5)
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))

	// Write the header
	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("error writing message header: %w", err)
	}

	// Write the payload if any
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return fmt.Errorf("error writing message payload: %w", err)
		}
	}

	return nil
}

// WriteCommandMessage writes a command message to the connection using protobuf
// It validates arguments for null bytes and creates a protobuf-serialized command message
// Returns an error if validation fails, serialization fails, or writing to connection fails
func WriteCommandMessage(conn net.Conn, authSecret, toolName string, args []string) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	if len(args) == 0 {
		return fmt.Errorf("no arguments provided for command")
	}

	if toolName == "" {
		return fmt.Errorf("no tool name provided")
	}

	// Validate arguments for null bytes
	if err := validateArguments(toolName, args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	// Calculate signature including tool name
	signatureArgs := append([]string{toolName}, args...)
	signature := CalculateSignature(authSecret, signatureArgs)

	// Create protobuf message
	cmdMsg := &CommandMessage{
		Version:   uint32(ProtocolVersion),
		Signature: signature,
		ToolName:  toolName,
		Arguments: args,
	}

	// Serialize to protobuf
	payload, err := proto.Marshal(cmdMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal command message: %w", err)
	}

	return WriteMessage(conn, MessageTypeCommand, payload)
}

// ParseCommandMessage parses a protobuf command message payload
// Returns the parsed command message components and validates for null bytes
// Returns an error if deserialization fails, version mismatch, or validation fails
func ParseCommandMessage(payload []byte) (version uint8, signature string, toolName string, args []string, err error) {
	if payload == nil {
		return 0, "", "", nil, fmt.Errorf("payload is nil")
	}

	// Deserialize protobuf message
	var cmdMsg CommandMessage
	if err := proto.Unmarshal(payload, &cmdMsg); err != nil {
		return 0, "", "", nil, fmt.Errorf("failed to unmarshal command message: %w", err)
	}

	// Extract fields
	version = uint8(cmdMsg.Version)
	signature = cmdMsg.Signature
	toolName = cmdMsg.ToolName
	args = cmdMsg.Arguments

	// Check protocol version
	if version != ProtocolVersion {
		err = fmt.Errorf("protocol version mismatch: got %d, expected %d", version, ProtocolVersion)
	}

	// Validate arguments for null bytes (server-side protection)
	if validateErr := validateArguments(toolName, args); validateErr != nil {
		return 0, "", "", nil, fmt.Errorf("malformed command message: %w", validateErr)
	}

	return version, signature, toolName, args, err
}

// ConnectWithTimeout attempts to connect with a timeout
// Handles both TCP and Unix socket connections appropriately
// For Unix sockets, it first checks if the socket file exists
// For TCP connections, it uses DialTimeout to enforce the timeout
func ConnectWithTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	if network == "" || address == "" {
		return nil, fmt.Errorf("invalid network or address")
	}

	// For Unix sockets, we need to handle timeouts differently
	if network == "unix" {
		// Check if the socket exists first
		_, err := os.Stat(address)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("unix socket %s does not exist", address)
			}
			return nil, fmt.Errorf("error checking unix socket %s: %w", address, err)
		}

		// Use the standard net.Dial for Unix sockets
		conn, err := net.Dial(network, address)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to unix socket %s: %w", address, err)
		}
		return conn, nil
	}

	// For TCP, use timeout
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s address %s: %w", network, address, err)
	}
	return conn, nil
}
