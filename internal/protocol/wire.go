package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// Message represents a protocol message envelope.
type Message struct {
	Type    uint8
	Payload []byte
}

// Protocol version. A client and server must speak the same version; the
// server rejects a hello or command with any other. 0x07 added the
// hello/challenge handshake, MsgStdinAck, MsgStat and MsgListDir.
const CurrentVersion = uint8(0x07)

// Control message types
const (
	MsgCommand  = uint8(0x01)
	MsgCancel   = uint8(0x02)
	MsgExitCode = uint8(0x03)
	MsgError    = uint8(0x04)
	MsgPing     = uint8(0x05)
	MsgPong     = uint8(0x06)

	// Handshake (see HelloMessage): MsgHello from the client, MsgChallenge
	// from the server, MsgCommand from the client, MsgAccepted from the
	// server.
	MsgHello     = uint8(0x07)
	MsgChallenge = uint8(0x08)
	MsgAccepted  = uint8(0x09)
)

// Output piping message types
const (
	MsgStdin      = uint8(0x10)
	MsgStdinClose = uint8(0x11)
	MsgStdout     = uint8(0x12)
	MsgStderr     = uint8(0x13)
	MsgStdinAck   = uint8(0x14)
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
	MsgStat      = uint8(0x2A)
	MsgListDir   = uint8(0x2B)
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
	MsgStatOk      = uint8(0x4A)
	MsgListDirOk   = uint8(0x4B)
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
	FioEPERM        = int32(1)
	FioENOENT       = int32(2)
	FioEIO          = int32(5)
	FioEBADF        = int32(9)
	FioEACCES       = int32(13)
	FioEBUSY        = int32(16)
	FioEEXIST       = int32(17)
	FioEXDEV        = int32(18)
	FioENOTDIR      = int32(20)
	FioEISDIR       = int32(21)
	FioEINVAL       = int32(22)
	FioEFBIG        = int32(27)
	FioENOSPC       = int32(28)
	FioESPIPE       = int32(29)
	FioEROFS        = int32(30)
	FioEPIPE        = int32(32)
	FioERANGE       = int32(34)
	FioENAMETOOLONG = int32(36)
	FioENOTEMPTY    = int32(39)
	FioELOOP        = int32(40)
	FioEOVERFLOW    = int32(75)
	FioEDQUOT       = int32(122)
)

// Canonical file mode bits (POSIX st_mode values as on Linux), used in
// FstatOk, StatOk and ListDirOk.
const (
	FioSIFMT   = uint32(0o170000)
	FioSIFSOCK = uint32(0o140000)
	FioSIFLNK  = uint32(0o120000)
	FioSIFREG  = uint32(0o100000)
	FioSIFBLK  = uint32(0o060000)
	FioSIFDIR  = uint32(0o040000)
	FioSIFCHR  = uint32(0o020000)
	FioSIFIFO  = uint32(0o010000)
)

// Access bits in StatOk: what the client process may do with the path.
const (
	FioAccessR = uint8(0x01)
	FioAccessW = uint8(0x02)
	FioAccessX = uint8(0x04)
)

// ModeToWire converts a Go file mode to canonical POSIX st_mode bits.
func ModeToWire(m fs.FileMode) uint32 {
	mode := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		mode |= 0o1000
	}
	switch {
	case m&fs.ModeDir != 0:
		return mode | FioSIFDIR
	case m&fs.ModeSymlink != 0:
		return mode | FioSIFLNK
	case m&fs.ModeNamedPipe != 0:
		return mode | FioSIFIFO
	case m&fs.ModeSocket != 0:
		return mode | FioSIFSOCK
	case m&fs.ModeCharDevice != 0:
		return mode | FioSIFCHR
	case m&fs.ModeDevice != 0:
		return mode | FioSIFBLK
	case m&fs.ModeIrregular != 0:
		return mode // type unknown
	default:
		return mode | FioSIFREG
	}
}

// Program type for command message
const (
	ProgramFFmpeg  = uint8(0x01)
	ProgramFFprobe = uint8(0x02)
)

// HMAC signature length (raw HMAC-SHA256 = 32 bytes)
const HMACLength = 32

// Nonce length (UUID v4 = 16 bytes)
const NonceLength = 16

// IsFileIORequest returns true for file I/O request message types (0x20–0x2F).
// Every one of them carries the request ID in its first two payload bytes.
func IsFileIORequest(msgType uint8) bool {
	return msgType >= 0x20 && msgType <= 0x2F
}

// IsFileIOResponse returns true for file I/O response message types (0x40–0x4F).
func IsFileIOResponse(msgType uint8) bool {
	return msgType >= 0x40 && msgType <= 0x4F
}

// --- Message envelope ---

// ReadMessageFrom reads a protocol message from any io.Reader.
func ReadMessageFrom(r io.Reader) (*Message, error) {
	return ReadMessageLimit(r, MaxPayload)
}

// MaxPayload is the largest message ReadMessageFrom accepts. It is meant for
// peers that have already proved they know the secret: the buffer for a
// message is allocated in full before its payload arrives.
const MaxPayload = 100 * 1024 * 1024

// MaxHandshakePayload bounds every message read before the other side has
// proved it knows the secret (hello, challenge, command).
const MaxHandshakePayload = 1024 * 1024

// ErrBusy is returned by ReadMessageUntrusted when reserve refuses more
// buffer space.
var ErrBusy = errors.New("too much data buffered for unauthenticated connections")

// ReadMessageUntrusted reads one message from a peer that hasn't proved it
// knows the secret. Payloads over maxPayload are rejected from the header.
// The payload is read into pieces, each allocated once its first byte has
// arrived and as large as everything received so far (at least 256 bytes,
// at most 16 KiB). A peer that announces
// a large message and goes silent holds nothing, and any peer holds at most
// 16 KiB more than it has sent. The pieces are joined once the message is
// complete. If reserve is not nil, it is asked for each piece before it is
// allocated; when it refuses, the read fails with ErrBusy.
func ReadMessageUntrusted(r io.Reader, maxPayload uint32, reserve func(n int) bool) (*Message, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("error reading message header: %w", err)
	}
	if l := binary.BigEndian.Uint32(header[1:]); l > maxPayload {
		return nil, fmt.Errorf("payload length too large: %d bytes (limit %d)", l, maxPayload)
	}
	payloadLen := int(binary.BigEndian.Uint32(header[1:]))
	const minChunk, maxChunk = 256, 16 * 1024
	var pieces [][]byte
	got := 0
	var next [1]byte
	for got < payloadLen {
		if _, err := io.ReadFull(r, next[:]); err != nil {
			return nil, fmt.Errorf("error reading payload: %w", err)
		}
		n := min(payloadLen-got, max(minChunk, min(got, maxChunk)))
		if reserve != nil && !reserve(n) {
			return nil, ErrBusy
		}
		piece := make([]byte, n)
		piece[0] = next[0]
		if _, err := io.ReadFull(r, piece[1:]); err != nil {
			return nil, fmt.Errorf("error reading payload: %w", err)
		}
		pieces = append(pieces, piece)
		got += n
	}
	if len(pieces) == 1 {
		return &Message{Type: header[0], Payload: pieces[0]}, nil
	}
	// Growing one buffer instead would copy it for every piece, or (with
	// append) hold more than was reserved.
	payload := make([]byte, 0, payloadLen)
	for _, p := range pieces {
		payload = append(payload, p...)
	}
	return &Message{Type: header[0], Payload: payload}, nil
}

// ReadMessageLimit reads one message, rejecting payloads over maxPayload
// bytes before allocating anything. The payload buffer is allocated in full
// up front, so use it only for peers that have proved they know the secret.
func ReadMessageLimit(r io.Reader, maxPayload uint32) (*Message, error) {
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

	if payloadLen > maxPayload {
		return nil, fmt.Errorf("payload length too large: %d bytes (limit %d)", payloadLen, maxPayload)
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

// PathRequest is the payload of MsgStat and MsgListDir.
type PathRequest struct {
	RequestID uint16
	Path      string
}

func (r *PathRequest) Encode() []byte {
	buf := make([]byte, 2+len(r.Path))
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	copy(buf[2:], r.Path)
	return buf
}

func DecodePathRequest(payload []byte) (*PathRequest, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("PathRequest payload too short: %d bytes", len(payload))
	}
	return &PathRequest{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Path:      string(payload[2:]),
	}, nil
}

// --- File I/O response types ---

// OpenOkResponse: req_id(2) + file_size(8) + mode(4) = 14. Mode is POSIX
// st_mode bits of the opened file (ModeToWire), so fio can report pipes and
// devices as such from fstat without another round trip.
type OpenOkResponse struct {
	RequestID uint16
	FileSize  int64
	Mode      uint32
}

func (r *OpenOkResponse) Encode() []byte {
	buf := make([]byte, 14)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint64(buf[2:], uint64(r.FileSize))
	binary.BigEndian.PutUint32(buf[10:], r.Mode)
	return buf
}

func DecodeOpenOkResponse(payload []byte) (*OpenOkResponse, error) {
	if len(payload) < 14 {
		return nil, fmt.Errorf("OpenOkResponse payload too short: %d bytes", len(payload))
	}
	return &OpenOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		FileSize:  int64(binary.BigEndian.Uint64(payload[2:])),
		Mode:      binary.BigEndian.Uint32(payload[10:]),
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

// StatOkResponse answers MsgStat. Mode is canonical (ModeToWire), Mtime is
// Unix seconds, and Access holds FioAccess* bits for the client process.
type StatOkResponse struct {
	RequestID uint16
	Size      int64
	Mode      uint32
	Mtime     int64
	Access    uint8
}

func (r *StatOkResponse) Encode() []byte {
	buf := make([]byte, 23)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint64(buf[2:], uint64(r.Size))
	binary.BigEndian.PutUint32(buf[10:], r.Mode)
	binary.BigEndian.PutUint64(buf[14:], uint64(r.Mtime))
	buf[22] = r.Access
	return buf
}

func DecodeStatOkResponse(payload []byte) (*StatOkResponse, error) {
	if len(payload) < 23 {
		return nil, fmt.Errorf("StatOkResponse payload too short: %d bytes", len(payload))
	}
	return &StatOkResponse{
		RequestID: binary.BigEndian.Uint16(payload[0:]),
		Size:      int64(binary.BigEndian.Uint64(payload[2:])),
		Mode:      binary.BigEndian.Uint32(payload[10:]),
		Mtime:     int64(binary.BigEndian.Uint64(payload[14:])),
		Access:    payload[22],
	}, nil
}

// DirEntry is one entry of a ListDirOk response, described as lstat(2) sees
// it. Mode is canonical (ModeToWire), Mtime is Unix seconds.
type DirEntry struct {
	Name  string
	Mode  uint32
	Size  int64
	Mtime int64
}

// ListDirOkResponse answers MsgListDir: req_id(2) + count(4) + entries of
// name_len(2) + name + mode(4) + size(8) + mtime(8).
type ListDirOkResponse struct {
	RequestID uint16
	Entries   []DirEntry
}

// ListDirEntrySize is the encoded size of a DirEntry with the given name.
func ListDirEntrySize(name string) int { return 2 + len(name) + 20 }

func (r *ListDirOkResponse) Encode() []byte {
	size := 6
	for _, e := range r.Entries {
		size += ListDirEntrySize(e.Name)
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint16(buf[0:], r.RequestID)
	binary.BigEndian.PutUint32(buf[2:], uint32(len(r.Entries)))
	off := 6
	for _, e := range r.Entries {
		binary.BigEndian.PutUint16(buf[off:], uint16(len(e.Name)))
		off += 2
		off += copy(buf[off:], e.Name)
		binary.BigEndian.PutUint32(buf[off:], e.Mode)
		binary.BigEndian.PutUint64(buf[off+4:], uint64(e.Size))
		binary.BigEndian.PutUint64(buf[off+12:], uint64(e.Mtime))
		off += 20
	}
	return buf
}

func DecodeListDirOkResponse(payload []byte) (*ListDirOkResponse, error) {
	if len(payload) < 6 {
		return nil, fmt.Errorf("ListDirOkResponse payload too short: %d bytes", len(payload))
	}
	r := &ListDirOkResponse{RequestID: binary.BigEndian.Uint16(payload[0:])}
	count := binary.BigEndian.Uint32(payload[2:])
	off := 6
	for i := uint32(0); i < count; i++ {
		if len(payload)-off < 2 {
			return nil, fmt.Errorf("ListDirOkResponse truncated at entry %d", i)
		}
		nameLen := int(binary.BigEndian.Uint16(payload[off:]))
		off += 2
		if len(payload)-off < nameLen+20 {
			return nil, fmt.Errorf("ListDirOkResponse truncated at entry %d", i)
		}
		e := DirEntry{Name: string(payload[off : off+nameLen])}
		off += nameLen
		e.Mode = binary.BigEndian.Uint32(payload[off:])
		e.Size = int64(binary.BigEndian.Uint64(payload[off+4:]))
		e.Mtime = int64(binary.BigEndian.Uint64(payload[off+12:]))
		off += 20
		r.Entries = append(r.Entries, e)
	}
	return r, nil
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

// --- Handshake ---
//
// Each side proves it knows the shared secret before the other acts on
// anything it sends:
//
//	client -> MsgHello:     version(1) + client nonce(16)
//	server -> MsgChallenge: server nonce(16)
//	client -> MsgCommand:   version(1) + server nonce(16) + signature(32) + program + args
//	server -> MsgAccepted:  proof(32)
//
// The command signature is auth.Sign over both nonces and the command; the
// server's nonce is new for every connection, so a recorded command can't be
// replayed. The server answers with auth.ServerProof over both nonces only
// after verifying the signature, so nobody gets a value derived from the
// secret without first showing they know it (which would let them guess
// the secret offline). The client checks the proof before it acts on any
// file request. The secret itself never crosses the wire, but nothing is
// encrypted.

func versionMismatch(got uint8) error {
	return fmt.Errorf("protocol version mismatch: client sent 0x%02x, server expects 0x%02x; client and server must run the same ffmpeg-over-ip release", got, CurrentVersion)
}

// HelloMessage opens a connection (client -> server).
type HelloMessage struct {
	Nonce [NonceLength]byte // client nonce
}

func (m *HelloMessage) Encode() []byte {
	buf := make([]byte, 1+NonceLength)
	buf[0] = CurrentVersion
	copy(buf[1:], m.Nonce[:])
	return buf
}

// DecodeHelloMessage checks the version byte first, so a client of another
// release gets a version error rather than a length error.
func DecodeHelloMessage(payload []byte) (*HelloMessage, error) {
	if len(payload) >= 1 && payload[0] != CurrentVersion {
		return nil, versionMismatch(payload[0])
	}
	if len(payload) != 1+NonceLength {
		return nil, fmt.Errorf("hello payload must be %d bytes, got %d", 1+NonceLength, len(payload))
	}
	var m HelloMessage
	copy(m.Nonce[:], payload[1:])
	return &m, nil
}

// ChallengeMessage answers a hello (server -> client).
type ChallengeMessage struct {
	Nonce [NonceLength]byte // server nonce
}

func (m *ChallengeMessage) Encode() []byte {
	return append([]byte(nil), m.Nonce[:]...)
}

func DecodeChallengeMessage(payload []byte) (*ChallengeMessage, error) {
	if len(payload) != NonceLength {
		return nil, fmt.Errorf("challenge payload must be %d bytes, got %d", NonceLength, len(payload))
	}
	var m ChallengeMessage
	copy(m.Nonce[:], payload)
	return &m, nil
}

// AcceptedMessage accepts a verified command (server -> client).
type AcceptedMessage struct {
	Proof [HMACLength]byte // auth.ServerProof
}

func (m *AcceptedMessage) Encode() []byte {
	return append([]byte(nil), m.Proof[:]...)
}

func DecodeAcceptedMessage(payload []byte) (*AcceptedMessage, error) {
	if len(payload) != HMACLength {
		return nil, fmt.Errorf("accepted payload must be %d bytes, got %d", HMACLength, len(payload))
	}
	var m AcceptedMessage
	copy(m.Proof[:], payload)
	return &m, nil
}

// --- Stdin flow control ---

// StdinWindow is how many stdin bytes the client may send before the server
// acknowledges them with MsgStdinAck. Without it, a client could queue stdin
// faster than ffmpeg reads it, and the server's single reader would block on
// the stdin pipe while fio responses wait behind it on the same connection.
const StdinWindow = 4 * 1024 * 1024

// EncodeStdinAck encodes how many stdin bytes the server has written to the
// child since the last ack.
func EncodeStdinAck(n uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], n)
	return buf[:]
}

func DecodeStdinAck(payload []byte) (uint32, error) {
	if len(payload) != 4 {
		return 0, fmt.Errorf("stdin ack payload must be 4 bytes, got %d", len(payload))
	}
	return binary.BigEndian.Uint32(payload), nil
}

// --- Command message ---

type CommandMessage struct {
	Nonce     [NonceLength]byte // the server nonce from MsgChallenge, echoed
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
		return nil, versionMismatch(version)
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
