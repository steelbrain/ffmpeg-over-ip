package protocol

import (
	"bytes"
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
	ProtocolVersion = uint8(1)
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

// VerifySignature verifies the HMAC signature for authentication
func VerifySignature(authSecret, signature string, commandArgs []string) bool {
	expected := CalculateSignature(authSecret, commandArgs)
	return hmac.Equal([]byte(signature), []byte(expected))
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

// WriteCommandMessage writes a command message to the connection
// It formats the payload according to the protocol specification:
// - 1 byte protocol version
// - 64 byte signature (HMAC of the command args)
// - Command arguments separated by null bytes
// Returns an error if the connection is nil or if writing to the connection fails
func WriteCommandMessage(conn net.Conn, authSecret string, args []string) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}

	if len(args) == 0 {
		return fmt.Errorf("no arguments provided for command")
	}

	// Calculate signature
	signature := CalculateSignature(authSecret, args)

	// Pre-allocate the payload buffer with a reasonable size to avoid reallocations
	// Initial capacity: version (1) + signature (64) + estimated args size
	estimatedSize := 1 + SignatureLength + 64*len(args)
	payload := make([]byte, 0, estimatedSize)

	// Add version
	payload = append(payload, ProtocolVersion)

	// Add signature
	payload = append(payload, []byte(signature)...)

	// Add arguments with null byte separators
	for i, arg := range args {
		payload = append(payload, []byte(arg)...)
		if i < len(args)-1 {
			payload = append(payload, 0) // Null byte separator
		}
	}

	return WriteMessage(conn, MessageTypeCommand, payload)
}

// ParseCommandMessage parses a command message payload into its components:
// - protocol version (1 byte)
// - signature (64 bytes)
// - command arguments (separated by null bytes)
// Returns an error if the payload is invalid or too short
func ParseCommandMessage(payload []byte) (version uint8, signature string, args []string, err error) {
	// Validate payload length
	if payload == nil {
		return 0, "", nil, fmt.Errorf("payload is nil")
	}
	if len(payload) < 1+SignatureLength {
		return 0, "", nil, fmt.Errorf("invalid command message: payload length %d is too short (minimum required: %d)",
			len(payload), 1+SignatureLength)
	}

	// Extract version
	version = payload[0]
	if version != ProtocolVersion {
		// We still proceed but warn via an error that versions don't match
		// This allows for future backwards compatibility
		err = fmt.Errorf("protocol version mismatch: got %d, expected %d", version, ProtocolVersion)
	}

	// Extract signature
	signature = string(payload[1 : 1+SignatureLength])

	// Extract arguments
	argsPart := payload[1+SignatureLength:]
	if len(argsPart) > 0 {
		// Split arguments by null byte
		for _, arg := range bytes.Split(argsPart, []byte{0}) {
			args = append(args, string(arg))
		}
	}

	return version, signature, args, err
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
