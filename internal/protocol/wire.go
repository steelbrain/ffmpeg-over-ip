package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message represents a protocol message envelope.
type Message struct {
	Type    uint8
	Payload []byte
}

// Protocol version
const CurrentVersion = uint8(0x06)

// Control message types
const (
	MsgCommand  = uint8(0x01)
	MsgCancel   = uint8(0x02)
	MsgExitCode = uint8(0x03)
	MsgError    = uint8(0x04)
	MsgPing     = uint8(0x05)
	MsgPong     = uint8(0x06)
)

// Output piping message types
const (
	MsgStdin      = uint8(0x10)
	MsgStdinClose = uint8(0x11)
	MsgStdout     = uint8(0x12)
	MsgStderr     = uint8(0x13)
)

// File I/O request message types
const (
	MsgOpen      = uint8(0x20)
	MsgRead      = uint8(0x21)
	MsgWrite     = uint8(0x22)
	MsgSeek      = uint8(0x23)
	MsgClose     = uint8(0x24)
	MsgFstat     = uint8(0x25)
	MsgFtruncate = uint8(0x26)
	MsgUnlink    = uint8(0x27)
	MsgRename    = uint8(0x28)
	MsgMkdir     = uint8(0x29)
)

// File I/O response message types
const (
	MsgOpenOk      = uint8(0x40)
	MsgReadOk      = uint8(0x41)
	MsgWriteOk     = uint8(0x42)
	MsgSeekOk      = uint8(0x43)
	MsgCloseOk     = uint8(0x44)
	MsgFstatOk     = uint8(0x45)
	MsgFtruncateOk = uint8(0x46)
	MsgUnlinkOk    = uint8(0x47)
	MsgRenameOk    = uint8(0x48)
	MsgMkdirOk     = uint8(0x49)
	MsgIoError     = uint8(0x4F)
)

// Canonical open flags (platform-independent wire values)
const (
	FioORDONLY = uint32(0x0000)
	FioOWRONLY = uint32(0x0001)
	FioORDWR   = uint32(0x0002)
	FioOCREAT  = uint32(0x0040)
	FioOTRUNC  = uint32(0x0200)
)

// Canonical whence values
const (
	FioSeekSet = uint8(0)
	FioSeekCur = uint8(1)
	FioSeekEnd = uint8(2)
)

// Canonical errno values (matching Linux)
const (
	FioEPERM   = int32(1)
	FioENOENT  = int32(2)
	FioEIO     = int32(5)
	FioEACCES  = int32(13)
	FioEEXIST  = int32(17)
	FioENOTDIR = int32(20)
	FioEISDIR  = int32(21)
	FioEINVAL  = int32(22)
	FioENOSPC  = int32(28)
	FioEROFS   = int32(30)
	FioERANGE  = int32(34)
)

// Program type for command message
const (
	ProgramFFmpeg  = uint8(0x01)
	ProgramFFprobe = uint8(0x02)
)

// HMAC signature length (raw HMAC-SHA256 = 32 bytes)
const HMACLength = 32

// Nonce length (UUID v4 = 16 bytes)
const NonceLength = 16

// IsFileIORequest returns true for file I/O request message types (0x20–0x29).
func IsFileIORequest(msgType uint8) bool {
	return msgType >= 0x20 && msgType <= 0x29
}

// IsFileIOResponse returns true for file I/O response message types (0x40–0x4F).
func IsFileIOResponse(msgType uint8) bool {
	return msgType >= 0x40 && msgType <= 0x4F
}

// --- Message envelope ---

// ReadMessageFrom reads a protocol message from any io.Reader.
func ReadMessageFrom(r io.Reader) (*Message, error) {
	// Read message type (1 byte)
	var msgType [1]byte
	if _, err := io.ReadFull(r, msgType[:]); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("error reading message type: %w", err)
	}

	// Read payload length (4 bytes, uint32 big-endian)
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("error reading payload length: %w", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf[:])

	if payloadLen > 100*1024*1024 {
		return nil, fmt.Errorf("payload length too large: %d bytes", payloadLen)
	}

	// Read payload
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("error reading payload: %w", err)
		}
	}

	return &Message{Type: msgType[0], Payload: payload}, nil
}

// WriteMessageTo writes a protocol message to any io.Writer.
func WriteMessageTo(w io.Writer, msgType uint8, payload []byte) error {
	var header [5]byte
	header[0] = msgType
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("error writing message header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("error writing message payload: %w", err)
		}
	}
	return nil
}

// --- File I/O request types ---

type OpenRequest struct {
	RequestID uint16
	FileID    uint16
	Flags     uint32
	Mode      uint16
	Path      string
}

func (r *OpenRequest) Encode() []byte {
	pathBytes := []byte(r.Path)
	buf := make([]byte, 2+2+4+2+len(pathBytes))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	binary.BigEndian.PutUint32(buf[4:], r.Flags)
	binary.BigEndian.PutUint16(buf[8:], r.Mode)
	copy(buf[10:], pathBytes)
	return buf
}

func DecodeOpenRequest(payload []byte) (*OpenRequest, error) {
	if len(payload) < 10 {
		return nil, fmt.Errorf("OpenRequest payload too short: %d bytes", len(payload))
	}
	return &OpenRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
		Flags:     binary.BigEndian.Uint32(payload[4:]),
		Mode:      binary.BigEndian.Uint16(payload[8:]),
		Path:      string(payload[10:]),
	}, nil
}

type ReadRequest struct {
	RequestID uint16
	FileID    uint16
	NBytes    uint32
}

func (r *ReadRequest) Encode() []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	binary.BigEndian.PutUint32(buf[4:], r.NBytes)
	return buf
}

func DecodeReadRequest(payload []byte) (*ReadRequest, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("ReadRequest payload too short: %d bytes", len(payload))
	}
	return &ReadRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
		NBytes:    binary.BigEndian.Uint32(payload[4:]),
	}, nil
}

type WriteRequest struct {
	RequestID uint16
	FileID    uint16
	Data      []byte
}

func (r *WriteRequest) Encode() []byte {
	buf := make([]byte, 4+len(r.Data))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	copy(buf[4:], r.Data)
	return buf
}

func DecodeWriteRequest(payload []byte) (*WriteRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("WriteRequest payload too short: %d bytes", len(payload))
	}
	return &WriteRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
		Data:      payload[4:],
	}, nil
}

type SeekRequest struct {
	RequestID uint16
	FileID    uint16
	Offset    int64
	Whence    uint8
}

func (r *SeekRequest) Encode() []byte {
	buf := make([]byte, 13)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	binary.BigEndian.PutUint64(buf[4:], uint64(r.Offset))
	buf[12] = r.Whence
	return buf
}

func DecodeSeekRequest(payload []byte) (*SeekRequest, error) {
	if len(payload) < 13 {
		return nil, fmt.Errorf("SeekRequest payload too short: %d bytes", len(payload))
	}
	return &SeekRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
		Offset:    int64(binary.BigEndian.Uint64(payload[4:])),
		Whence:    payload[12],
	}, nil
}

type CloseRequest struct {
	RequestID uint16
	FileID    uint16
}

func (r *CloseRequest) Encode() []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	return buf
}

func DecodeCloseRequest(payload []byte) (*CloseRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("CloseRequest payload too short: %d bytes", len(payload))
	}
	return &CloseRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
	}, nil
}

type FstatRequest struct {
	RequestID uint16
	FileID    uint16
}

func (r *FstatRequest) Encode() []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	return buf
}

func DecodeFstatRequest(payload []byte) (*FstatRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("FstatRequest payload too short: %d bytes", len(payload))
	}
	return &FstatRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
	}, nil
}

type FtruncateRequest struct {
	RequestID uint16
	FileID    uint16
	Length    int64
}

func (r *FtruncateRequest) Encode() []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.FileID)
	binary.BigEndian.PutUint64(buf[4:], uint64(r.Length))
	return buf
}

func DecodeFtruncateRequest(payload []byte) (*FtruncateRequest, error) {
	if len(payload) < 12 {
		return nil, fmt.Errorf("FtruncateRequest payload too short: %d bytes", len(payload))
	}
	return &FtruncateRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileID:    binary.BigEndian.Uint16(payload[2:]),
		Length:    int64(binary.BigEndian.Uint64(payload[4:])),
	}, nil
}

type UnlinkRequest struct {
	RequestID uint16
	Path      string
}

func (r *UnlinkRequest) Encode() []byte {
	pathBytes := []byte(r.Path)
	buf := make([]byte, 2+len(pathBytes))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	copy(buf[2:], pathBytes)
	return buf
}

func DecodeUnlinkRequest(payload []byte) (*UnlinkRequest, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("UnlinkRequest payload too short: %d bytes", len(payload))
	}
	return &UnlinkRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Path:      string(payload[2:]),
	}, nil
}

type RenameRequest struct {
	RequestID uint16
	OldPath   string
	NewPath   string
}

func (r *RenameRequest) Encode() []byte {
	oldBytes := []byte(r.OldPath)
	newBytes := []byte(r.NewPath)
	buf := make([]byte, 2+2+len(oldBytes)+len(newBytes))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], uint16(len(oldBytes)))
	copy(buf[4:], oldBytes)
	copy(buf[4+len(oldBytes):], newBytes)
	return buf
}

func DecodeRenameRequest(payload []byte) (*RenameRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("RenameRequest payload too short: %d bytes", len(payload))
	}
	oldLen := int(binary.BigEndian.Uint16(payload[2:]))
	if len(payload) < 4+oldLen {
		return nil, fmt.Errorf("RenameRequest payload too short for old path: need %d, have %d", 4+oldLen, len(payload))
	}
	return &RenameRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		OldPath:   string(payload[4 : 4+oldLen]),
		NewPath:   string(payload[4+oldLen:]),
	}, nil
}

type MkdirRequest struct {
	RequestID uint16
	Mode      uint16
	Path      string
}

func (r *MkdirRequest) Encode() []byte {
	pathBytes := []byte(r.Path)
	buf := make([]byte, 4+len(pathBytes))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint16(buf[2:], r.Mode)
	copy(buf[4:], pathBytes)
	return buf
}

func DecodeMkdirRequest(payload []byte) (*MkdirRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("MkdirRequest payload too short: %d bytes", len(payload))
	}
	return &MkdirRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Mode:      binary.BigEndian.Uint16(payload[2:]),
		Path:      string(payload[4:]),
	}, nil
}

// --- File I/O response types ---

type OpenOkResponse struct {
	RequestID uint16
	FileSize  int64
}

func (r *OpenOkResponse) Encode() []byte {
	buf := make([]byte, 10)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint64(buf[2:], uint64(r.FileSize))
	return buf
}

func DecodeOpenOkResponse(payload []byte) (*OpenOkResponse, error) {
	if len(payload) < 10 {
		return nil, fmt.Errorf("OpenOkResponse payload too short: %d bytes", len(payload))
	}
	return &OpenOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileSize:  int64(binary.BigEndian.Uint64(payload[2:])),
	}, nil
}

type ReadOkResponse struct {
	RequestID uint16
	Data      []byte
}

func (r *ReadOkResponse) Encode() []byte {
	buf := make([]byte, 2+len(r.Data))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	copy(buf[2:], r.Data)
	return buf
}

func DecodeReadOkResponse(payload []byte) (*ReadOkResponse, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("ReadOkResponse payload too short: %d bytes", len(payload))
	}
	return &ReadOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Data:      payload[2:],
	}, nil
}

type WriteOkResponse struct {
	RequestID    uint16
	BytesWritten uint32
}

func (r *WriteOkResponse) Encode() []byte {
	buf := make([]byte, 6)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint32(buf[2:], r.BytesWritten)
	return buf
}

func DecodeWriteOkResponse(payload []byte) (*WriteOkResponse, error) {
	if len(payload) < 6 {
		return nil, fmt.Errorf("WriteOkResponse payload too short: %d bytes", len(payload))
	}
	return &WriteOkResponse{
		RequestID:    binary.BigEndian.Uint16(payload[0:]),
		BytesWritten: binary.BigEndian.Uint32(payload[2:]),
	}, nil
}

type SeekOkResponse struct {
	RequestID uint16
	Offset    int64
}

func (r *SeekOkResponse) Encode() []byte {
	buf := make([]byte, 10)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint64(buf[2:], uint64(r.Offset))
	return buf
}

func DecodeSeekOkResponse(payload []byte) (*SeekOkResponse, error) {
	if len(payload) < 10 {
		return nil, fmt.Errorf("SeekOkResponse payload too short: %d bytes", len(payload))
	}
	return &SeekOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Offset:    int64(binary.BigEndian.Uint64(payload[2:])),
	}, nil
}

// RequestIDResponse is used for CloseOk, FtruncateOk, UnlinkOk, RenameOk, MkdirOk
type RequestIDResponse struct {
	RequestID uint16
}

func (r *RequestIDResponse) Encode() []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	return buf
}

func DecodeRequestIDResponse(payload []byte) (*RequestIDResponse, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("RequestIDResponse payload too short: %d bytes", len(payload))
	}
	return &RequestIDResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
	}, nil
}

type FstatOkResponse struct {
	RequestID uint16
	FileSize  int64
	Mode      uint32
}

func (r *FstatOkResponse) Encode() []byte {
	buf := make([]byte, 14)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint64(buf[2:], uint64(r.FileSize))
	binary.BigEndian.PutUint32(buf[10:], r.Mode)
	return buf
}

func DecodeFstatOkResponse(payload []byte) (*FstatOkResponse, error) {
	if len(payload) < 14 {
		return nil, fmt.Errorf("FstatOkResponse payload too short: %d bytes", len(payload))
	}
	return &FstatOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileSize:  int64(binary.BigEndian.Uint64(payload[2:])),
		Mode:      binary.BigEndian.Uint32(payload[10:]),
	}, nil
}

type IoErrorResponse struct {
	RequestID uint16
	Errno     int32
}

func (r *IoErrorResponse) Encode() []byte {
	buf := make([]byte, 6)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint32(buf[2:], uint32(r.Errno))
	return buf
}

func DecodeIoErrorResponse(payload []byte) (*IoErrorResponse, error) {
	if len(payload) < 6 {
		return nil, fmt.Errorf("IoErrorResponse payload too short: %d bytes", len(payload))
	}
	return &IoErrorResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Errno:     int32(binary.BigEndian.Uint32(payload[2:])),
	}, nil
}

// --- Command message ---

type CommandMessage struct {
	Nonce     [NonceLength]byte
	Signature [HMACLength]byte
	Program   uint8
	Args      []string
}

func (m *CommandMessage) Encode() []byte {
	// Args are length-prefixed: [argc 2B][len 2B][arg bytes]...
	// This avoids the null-byte ambiguity of the old null-separated format.
	argsSize := 2 // argc
	for _, arg := range m.Args {
		argsSize += 2 + len(arg) // len + arg bytes
	}

	buf := make([]byte, 1+NonceLength+HMACLength+1+argsSize)
	buf[0] = CurrentVersion
	copy(buf[1:], m.Nonce[:])
	copy(buf[1+NonceLength:], m.Signature[:])
	buf[1+NonceLength+HMACLength] = m.Program

	offset := 1 + NonceLength + HMACLength + 1
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(m.Args)))
	offset += 2
	for _, arg := range m.Args {
		binary.BigEndian.PutUint16(buf[offset:], uint16(len(arg)))
		offset += 2
		copy(buf[offset:], []byte(arg))
		offset += len(arg)
	}

	return buf
}

func DecodeCommandMessage(payload []byte) (*CommandMessage, error) {
	minLen := 1 + NonceLength + HMACLength + 1
	if len(payload) < minLen {
		return nil, fmt.Errorf("command payload too short: %d bytes (minimum %d)", len(payload), minLen)
	}

	version := payload[0]
	if version != CurrentVersion {
		return nil, fmt.Errorf("unsupported protocol version: 0x%02x (expected 0x%02x)", version, CurrentVersion)
	}

	msg := &CommandMessage{
		Program: payload[1+NonceLength+HMACLength],
	}
	copy(msg.Nonce[:], payload[1:1+NonceLength])
	copy(msg.Signature[:], payload[1+NonceLength:1+NonceLength+HMACLength])

	// Args are length-prefixed: [argc 2B][len 2B][arg bytes]...
	argsData := payload[minLen:]
	if len(argsData) < 2 {
		return nil, fmt.Errorf("command payload too short for arg count")
	}
	argc := int(binary.BigEndian.Uint16(argsData[:2]))
	offset := 2
	for i := 0; i < argc; i++ {
		if offset+2 > len(argsData) {
			return nil, fmt.Errorf("command payload truncated at arg %d length", i)
		}
		argLen := int(binary.BigEndian.Uint16(argsData[offset:]))
		offset += 2
		if offset+argLen > len(argsData) {
			return nil, fmt.Errorf("command payload truncated at arg %d data", i)
		}
		msg.Args = append(msg.Args, string(argsData[offset:offset+argLen]))
		offset += argLen
	}

	return msg, nil
}
