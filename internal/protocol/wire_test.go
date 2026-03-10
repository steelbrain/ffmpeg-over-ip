package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
)

// --- Envelope tests ---

func TestWriteReadMessageRoundTrip(t *testing.T) {
	payload := []byte("hello world")
	var buf bytes.Buffer

	if err := WriteMessageTo(&buf, MsgOpen, payload); err != nil {
		t.Fatalf("WriteMessageTo failed: %v", err)
	}

	msg, err := ReadMessageFrom(&buf)
	if err != nil {
		t.Fatalf("ReadMessageFrom failed: %v", err)
	}

	if msg.Type != MsgOpen {
		t.Errorf("message type: got 0x%02x, want 0x%02x", msg.Type, MsgOpen)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("payload: got %q, want %q", msg.Payload, payload)
	}
}

func TestWriteMessageEnvelopeFormat(t *testing.T) {
	payload := []byte{0xDE, 0xAD}
	var buf bytes.Buffer

	WriteMessageTo(&buf, 0x42, payload)

	data := buf.Bytes()
	if data[0] != 0x42 {
		t.Errorf("type byte: got 0x%02x, want 0x42", data[0])
	}
	length := binary.BigEndian.Uint32(data[1:5])
	if length != 2 {
		t.Errorf("payload length: got %d, want 2", length)
	}
	if !bytes.Equal(data[5:], payload) {
		t.Errorf("payload bytes: got %v, want %v", data[5:], payload)
	}
}

func TestReadMessageEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	WriteMessageTo(&buf, MsgPing, nil)

	msg, err := ReadMessageFrom(&buf)
	if err != nil {
		t.Fatalf("ReadMessageFrom failed: %v", err)
	}
	if msg.Type != MsgPing {
		t.Errorf("type: got 0x%02x, want 0x%02x", msg.Type, MsgPing)
	}
	if len(msg.Payload) != 0 {
		t.Errorf("payload length: got %d, want 0", len(msg.Payload))
	}
}

func TestReadMessageTooLargePayload(t *testing.T) {
	var buf bytes.Buffer
	// Write a header claiming 200MB payload
	buf.WriteByte(0x01)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, 200*1024*1024)
	buf.Write(lenBuf)

	_, err := ReadMessageFrom(&buf)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}
}

// --- Open request ---

func TestOpenRequestRoundTrip(t *testing.T) {
	req := &OpenRequest{
		RequestID: 1,
		FileID:    42,
		Flags:     FioOWRONLY | FioOCREAT | FioOTRUNC,
		Mode:      0o666,
		Path:      "/tmp/test/output.ts",
	}

	encoded := req.Encode()
	decoded, err := DecodeOpenRequest(encoded)
	if err != nil {
		t.Fatalf("DecodeOpenRequest failed: %v", err)
	}

	if decoded.RequestID != req.RequestID {
		t.Errorf("RequestID: got %d, want %d", decoded.RequestID, req.RequestID)
	}
	if decoded.FileID != req.FileID {
		t.Errorf("FileID: got %d, want %d", decoded.FileID, req.FileID)
	}
	if decoded.Flags != req.Flags {
		t.Errorf("Flags: got 0x%x, want 0x%x", decoded.Flags, req.Flags)
	}
	if decoded.Mode != req.Mode {
		t.Errorf("Mode: got 0o%o, want 0o%o", decoded.Mode, req.Mode)
	}
	if decoded.Path != req.Path {
		t.Errorf("Path: got %q, want %q", decoded.Path, req.Path)
	}
}

func TestOpenRequestByteLayout(t *testing.T) {
	req := &OpenRequest{
		RequestID: 0x0102,
		FileID:    0x0304,
		Flags:     0x00000241, // O_WRONLY | O_CREAT | O_TRUNC
		Mode:      0x01B6,     // 0o666
		Path:      "AB",
	}

	encoded := req.Encode()
	expected := []byte{
		0x01, 0x02, // request ID
		0x03, 0x04, // file ID
		0x00, 0x00, 0x02, 0x41, // flags
		0x01, 0xB6, // mode
		'A', 'B', // path
	}

	if !bytes.Equal(encoded, expected) {
		t.Errorf("byte layout:\n  got  %v\n  want %v", encoded, expected)
	}
}

// --- Read request ---

func TestReadRequestRoundTrip(t *testing.T) {
	req := &ReadRequest{RequestID: 5, FileID: 1, NBytes: 65536}
	decoded, err := DecodeReadRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != 5 || decoded.FileID != 1 || decoded.NBytes != 65536 {
		t.Errorf("got %+v", decoded)
	}
}

// --- Write request ---

func TestWriteRequestRoundTrip(t *testing.T) {
	data := []byte("hello video data")
	req := &WriteRequest{RequestID: 10, FileID: 2, Data: data}
	decoded, err := DecodeWriteRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != 10 || decoded.FileID != 2 {
		t.Errorf("header: got req=%d file=%d", decoded.RequestID, decoded.FileID)
	}
	if !bytes.Equal(decoded.Data, data) {
		t.Errorf("data: got %q, want %q", decoded.Data, data)
	}
}

// --- Seek request ---

func TestSeekRequestRoundTrip(t *testing.T) {
	req := &SeekRequest{RequestID: 7, FileID: 1, Offset: -1024, Whence: FioSeekEnd}
	decoded, err := DecodeSeekRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Offset != -1024 {
		t.Errorf("Offset: got %d, want -1024", decoded.Offset)
	}
	if decoded.Whence != FioSeekEnd {
		t.Errorf("Whence: got %d, want %d", decoded.Whence, FioSeekEnd)
	}
}

func TestSeekRequestNegativeOffset(t *testing.T) {
	// Verify signed int64 survives encoding
	req := &SeekRequest{RequestID: 1, FileID: 1, Offset: -9999999999, Whence: FioSeekCur}
	decoded, err := DecodeSeekRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Offset != -9999999999 {
		t.Errorf("Offset: got %d, want -9999999999", decoded.Offset)
	}
}

// --- Close request ---

func TestCloseRequestRoundTrip(t *testing.T) {
	req := &CloseRequest{RequestID: 100, FileID: 50}
	decoded, err := DecodeCloseRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != 100 || decoded.FileID != 50 {
		t.Errorf("got %+v", decoded)
	}
}

// --- Fstat request ---

func TestFstatRequestRoundTrip(t *testing.T) {
	req := &FstatRequest{RequestID: 3, FileID: 7}
	decoded, err := DecodeFstatRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != 3 || decoded.FileID != 7 {
		t.Errorf("got %+v", decoded)
	}
}

// --- Ftruncate request ---

func TestFtruncateRequestRoundTrip(t *testing.T) {
	req := &FtruncateRequest{RequestID: 20, FileID: 5, Length: 1048576}
	decoded, err := DecodeFtruncateRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Length != 1048576 {
		t.Errorf("Length: got %d, want 1048576", decoded.Length)
	}
}

// --- Unlink request ---

func TestUnlinkRequestRoundTrip(t *testing.T) {
	req := &UnlinkRequest{RequestID: 15, Path: "/tmp/old-segment.ts"}
	decoded, err := DecodeUnlinkRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Path != req.Path {
		t.Errorf("Path: got %q, want %q", decoded.Path, req.Path)
	}
}

// --- Rename request ---

func TestRenameRequestRoundTrip(t *testing.T) {
	req := &RenameRequest{
		RequestID: 30,
		OldPath:   "/tmp/stream.m3u8.tmp",
		NewPath:   "/tmp/stream.m3u8",
	}
	decoded, err := DecodeRenameRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.OldPath != req.OldPath {
		t.Errorf("OldPath: got %q, want %q", decoded.OldPath, req.OldPath)
	}
	if decoded.NewPath != req.NewPath {
		t.Errorf("NewPath: got %q, want %q", decoded.NewPath, req.NewPath)
	}
}

func TestRenameRequestByteLayout(t *testing.T) {
	req := &RenameRequest{RequestID: 0x0001, OldPath: "AB", NewPath: "CD"}
	encoded := req.Encode()
	expected := []byte{
		0x00, 0x01, // request ID
		0x00, 0x02, // old path length = 2
		'A', 'B', // old path
		'C', 'D', // new path
	}
	if !bytes.Equal(encoded, expected) {
		t.Errorf("byte layout:\n  got  %v\n  want %v", encoded, expected)
	}
}

// --- Mkdir request ---

func TestMkdirRequestRoundTrip(t *testing.T) {
	req := &MkdirRequest{RequestID: 8, Mode: 0o755, Path: "/tmp/2024/01/15"}
	decoded, err := DecodeMkdirRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Mode != 0o755 {
		t.Errorf("Mode: got 0o%o, want 0o755", decoded.Mode)
	}
	if decoded.Path != req.Path {
		t.Errorf("Path: got %q, want %q", decoded.Path, req.Path)
	}
}

// --- OpenOk response ---

func TestOpenOkResponseRoundTrip(t *testing.T) {
	resp := &OpenOkResponse{RequestID: 1, FileSize: 524288000}
	decoded, err := DecodeOpenOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.FileSize != 524288000 {
		t.Errorf("FileSize: got %d, want 524288000", decoded.FileSize)
	}
}

// --- ReadOk response ---

func TestReadOkResponseRoundTrip(t *testing.T) {
	data := make([]byte, 65536)
	for i := range data {
		data[i] = byte(i % 256)
	}
	resp := &ReadOkResponse{RequestID: 5, Data: data}
	decoded, err := DecodeReadOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(decoded.Data, data) {
		t.Errorf("data mismatch: got %d bytes, want %d bytes", len(decoded.Data), len(data))
	}
}

func TestReadOkResponseEOF(t *testing.T) {
	resp := &ReadOkResponse{RequestID: 5, Data: nil}
	decoded, err := DecodeReadOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Data) != 0 {
		t.Errorf("expected empty data for EOF, got %d bytes", len(decoded.Data))
	}
}

// --- WriteOk response ---

func TestWriteOkResponseRoundTrip(t *testing.T) {
	resp := &WriteOkResponse{RequestID: 10, BytesWritten: 65536}
	decoded, err := DecodeWriteOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.BytesWritten != 65536 {
		t.Errorf("BytesWritten: got %d, want 65536", decoded.BytesWritten)
	}
}

// --- SeekOk response ---

func TestSeekOkResponseRoundTrip(t *testing.T) {
	resp := &SeekOkResponse{RequestID: 7, Offset: 1048576}
	decoded, err := DecodeSeekOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Offset != 1048576 {
		t.Errorf("Offset: got %d, want 1048576", decoded.Offset)
	}
}

// --- RequestID-only responses (CloseOk, FtruncateOk, UnlinkOk, RenameOk, MkdirOk) ---

func TestRequestIDResponseRoundTrip(t *testing.T) {
	resp := &RequestIDResponse{RequestID: 42}
	decoded, err := DecodeRequestIDResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != 42 {
		t.Errorf("RequestID: got %d, want 42", decoded.RequestID)
	}
}

// --- FstatOk response ---

func TestFstatOkResponseRoundTrip(t *testing.T) {
	resp := &FstatOkResponse{RequestID: 3, FileSize: 999999, Mode: 0o100644}
	decoded, err := DecodeFstatOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.FileSize != 999999 {
		t.Errorf("FileSize: got %d, want 999999", decoded.FileSize)
	}
	if decoded.Mode != 0o100644 {
		t.Errorf("Mode: got 0o%o, want 0o100644", decoded.Mode)
	}
}

// --- IoError response ---

func TestIoErrorResponseRoundTrip(t *testing.T) {
	resp := &IoErrorResponse{RequestID: 1, Errno: FioENOENT}
	decoded, err := DecodeIoErrorResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Errno != FioENOENT {
		t.Errorf("Errno: got %d, want %d", decoded.Errno, FioENOENT)
	}
}

// --- Command message ---

func TestCommandMessageRoundTrip(t *testing.T) {
	msg := &CommandMessage{
		Program: ProgramFFmpeg,
		Args:    []string{"-i", "/media/input.mkv", "-c:v", "h264_nvenc", "output.mp4"},
	}
	// Fill nonce with test data
	for i := range msg.Nonce {
		msg.Nonce[i] = byte(i)
	}
	// Fill signature with test data
	for i := range msg.Signature {
		msg.Signature[i] = byte(i + 100)
	}

	encoded := msg.Encode()
	decoded, err := DecodeCommandMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Program != ProgramFFmpeg {
		t.Errorf("Program: got %d, want %d", decoded.Program, ProgramFFmpeg)
	}
	if decoded.Nonce != msg.Nonce {
		t.Errorf("Nonce mismatch")
	}
	if decoded.Signature != msg.Signature {
		t.Errorf("Signature mismatch")
	}
	if len(decoded.Args) != len(msg.Args) {
		t.Fatalf("Args length: got %d, want %d", len(decoded.Args), len(msg.Args))
	}
	for i, arg := range decoded.Args {
		if arg != msg.Args[i] {
			t.Errorf("Args[%d]: got %q, want %q", i, arg, msg.Args[i])
		}
	}
}

func TestCommandMessageByteLayout(t *testing.T) {
	msg := &CommandMessage{
		Program: ProgramFFprobe,
		Args:    []string{"a", "b"},
	}
	// Zero nonce and signature for predictable output
	encoded := msg.Encode()

	// Check version byte
	if encoded[0] != CurrentVersion {
		t.Errorf("version: got 0x%02x, want 0x%02x", encoded[0], CurrentVersion)
	}

	// Check program byte position
	programOffset := 1 + NonceLength + HMACLength
	if encoded[programOffset] != ProgramFFprobe {
		t.Errorf("program byte at offset %d: got 0x%02x, want 0x%02x",
			programOffset, encoded[programOffset], ProgramFFprobe)
	}

	// Check args: [argc 2B][len0 2B][arg0][len1 2B][arg1]
	argsOffset := programOffset + 1
	expectedArgs := []byte{
		0x00, 0x02, // argc = 2
		0x00, 0x01, // len("a") = 1
		'a',
		0x00, 0x01, // len("b") = 1
		'b',
	}
	if !bytes.Equal(encoded[argsOffset:], expectedArgs) {
		t.Errorf("args bytes:\n  got  %v\n  want %v", encoded[argsOffset:], expectedArgs)
	}

	// Check total length: 1 + 16 + 32 + 1 + 2 + (2+1) + (2+1) = 58
	if len(encoded) != 58 {
		t.Errorf("total length: got %d, want 58", len(encoded))
	}
}

func TestCommandMessageWrongVersion(t *testing.T) {
	msg := &CommandMessage{Program: ProgramFFmpeg, Args: []string{"test"}}
	encoded := msg.Encode()
	encoded[0] = 0x05 // wrong version

	_, err := DecodeCommandMessage(encoded)
	if err == nil {
		t.Fatal("expected error for wrong version, got nil")
	}
}

func TestCommandMessageSingleArg(t *testing.T) {
	msg := &CommandMessage{Program: ProgramFFprobe, Args: []string{"-version"}}
	decoded, err := DecodeCommandMessage(msg.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Args) != 1 || decoded.Args[0] != "-version" {
		t.Errorf("Args: got %v, want [-version]", decoded.Args)
	}
}

// --- Full message round-trips (envelope + payload) ---

func TestFullOpenRequestRoundTrip(t *testing.T) {
	req := &OpenRequest{
		RequestID: 1,
		FileID:    1,
		Flags:     FioORDONLY,
		Mode:      0,
		Path:      "/media/movies/input.mkv",
	}

	var buf bytes.Buffer
	if err := WriteMessageTo(&buf, MsgOpen, req.Encode()); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	msg, err := ReadMessageFrom(&buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if msg.Type != MsgOpen {
		t.Errorf("type: got 0x%02x, want 0x%02x", msg.Type, MsgOpen)
	}

	decoded, err := DecodeOpenRequest(msg.Payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Path != "/media/movies/input.mkv" {
		t.Errorf("Path: got %q, want %q", decoded.Path, "/media/movies/input.mkv")
	}
}

func TestFullIoErrorRoundTrip(t *testing.T) {
	resp := &IoErrorResponse{RequestID: 42, Errno: FioEACCES}

	var buf bytes.Buffer
	WriteMessageTo(&buf, MsgIoError, resp.Encode())

	msg, _ := ReadMessageFrom(&buf)
	decoded, _ := DecodeIoErrorResponse(msg.Payload)

	if decoded.RequestID != 42 || decoded.Errno != FioEACCES {
		t.Errorf("got req=%d errno=%d, want req=42 errno=%d", decoded.RequestID, decoded.Errno, FioEACCES)
	}
}

// --- Decode error cases ---

func TestDecodeShortPayloads(t *testing.T) {
	tests := []struct {
		name   string
		decode func([]byte) error
	}{
		{"OpenRequest", func(b []byte) error { _, e := DecodeOpenRequest(b); return e }},
		{"ReadRequest", func(b []byte) error { _, e := DecodeReadRequest(b); return e }},
		{"WriteRequest", func(b []byte) error { _, e := DecodeWriteRequest(b); return e }},
		{"SeekRequest", func(b []byte) error { _, e := DecodeSeekRequest(b); return e }},
		{"CloseRequest", func(b []byte) error { _, e := DecodeCloseRequest(b); return e }},
		{"FstatRequest", func(b []byte) error { _, e := DecodeFstatRequest(b); return e }},
		{"FtruncateRequest", func(b []byte) error { _, e := DecodeFtruncateRequest(b); return e }},
		{"UnlinkRequest", func(b []byte) error { _, e := DecodeUnlinkRequest(b); return e }},
		{"RenameRequest", func(b []byte) error { _, e := DecodeRenameRequest(b); return e }},
		{"MkdirRequest", func(b []byte) error { _, e := DecodeMkdirRequest(b); return e }},
		{"OpenOkResponse", func(b []byte) error { _, e := DecodeOpenOkResponse(b); return e }},
		{"ReadOkResponse", func(b []byte) error { _, e := DecodeReadOkResponse(b); return e }},
		{"WriteOkResponse", func(b []byte) error { _, e := DecodeWriteOkResponse(b); return e }},
		{"SeekOkResponse", func(b []byte) error { _, e := DecodeSeekOkResponse(b); return e }},
		{"RequestIDResponse", func(b []byte) error { _, e := DecodeRequestIDResponse(b); return e }},
		{"FstatOkResponse", func(b []byte) error { _, e := DecodeFstatOkResponse(b); return e }},
		{"IoErrorResponse", func(b []byte) error { _, e := DecodeIoErrorResponse(b); return e }},
		{"CommandMessage", func(b []byte) error { _, e := DecodeCommandMessage(b); return e }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.decode([]byte{})
			if err == nil {
				t.Errorf("expected error for empty payload, got nil")
			}
			err = tc.decode([]byte{0x00})
			if err == nil {
				t.Errorf("expected error for 1-byte payload, got nil")
			}
			if tc.name == "CommandMessage" {
				// 50 bytes = version + nonce + sig + program, but no argc (needs 52 minimum)
				shortPayload := make([]byte, 50)
				shortPayload[0] = CurrentVersion
				err = tc.decode(shortPayload)
				if err == nil {
					t.Errorf("expected error for 50-byte CommandMessage (no argc), got nil")
				}
			}
		})
	}
}

// --- UTF-8 paths ---

func TestUTF8Paths(t *testing.T) {
	path := "/media/映画/テスト.mkv"
	req := &OpenRequest{RequestID: 1, FileID: 1, Flags: FioORDONLY, Mode: 0, Path: path}
	decoded, err := DecodeOpenRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Path != path {
		t.Errorf("Path: got %q, want %q", decoded.Path, path)
	}
}

// --- Message type constants ---

func TestMessageTypeValues(t *testing.T) {
	// Verify no collisions and correct values per spec
	types := map[uint8]string{
		0x01: "Command", 0x02: "Cancel", 0x03: "ExitCode", 0x04: "Error",
		0x05: "Ping", 0x06: "Pong",
		0x10: "Stdin", 0x11: "StdinClose", 0x12: "Stdout", 0x13: "Stderr",
		0x20: "Open", 0x21: "Read", 0x22: "Write", 0x23: "Seek",
		0x24: "Close", 0x25: "Fstat", 0x26: "Ftruncate",
		0x27: "Unlink", 0x28: "Rename", 0x29: "Mkdir",
		0x40: "OpenOk", 0x41: "ReadOk", 0x42: "WriteOk", 0x43: "SeekOk",
		0x44: "CloseOk", 0x45: "FstatOk", 0x46: "FtruncateOk",
		0x47: "UnlinkOk", 0x48: "RenameOk", 0x49: "MkdirOk",
		0x4F: "IoError",
	}

	consts := map[string]uint8{
		"Command": MsgCommand, "Cancel": MsgCancel, "ExitCode": MsgExitCode, "Error": MsgError,
		"Ping": MsgPing, "Pong": MsgPong,
		"Stdin": MsgStdin, "StdinClose": MsgStdinClose, "Stdout": MsgStdout, "Stderr": MsgStderr,
		"Open": MsgOpen, "Read": MsgRead, "Write": MsgWrite, "Seek": MsgSeek,
		"Close": MsgClose, "Fstat": MsgFstat, "Ftruncate": MsgFtruncate,
		"Unlink": MsgUnlink, "Rename": MsgRename, "Mkdir": MsgMkdir,
		"OpenOk": MsgOpenOk, "ReadOk": MsgReadOk, "WriteOk": MsgWriteOk, "SeekOk": MsgSeekOk,
		"CloseOk": MsgCloseOk, "FstatOk": MsgFstatOk, "FtruncateOk": MsgFtruncateOk,
		"UnlinkOk": MsgUnlinkOk, "RenameOk": MsgRenameOk, "MkdirOk": MsgMkdirOk,
		"IoError": MsgIoError,
	}

	for name, val := range consts {
		expectedName, ok := types[val]
		if !ok {
			t.Errorf("constant %s (0x%02x) not in expected types map", name, val)
			continue
		}
		if expectedName != name {
			t.Errorf("constant %s has value 0x%02x which maps to %s", name, val, expectedName)
		}
	}
}

// --- IsFileIORequest ---

func TestIsFileIORequest(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		want    bool
	}{
		{"below range 0x1F", 0x1F, false},
		{"lower bound MsgOpen", 0x20, true},
		{"upper bound MsgMkdir", 0x29, true},
		{"above range 0x2A", 0x2A, false},
		{"zero", 0x00, false},
		{"max uint8", 0xFF, false},
		{"MsgOpen", MsgOpen, true},
		{"MsgRead", MsgRead, true},
		{"MsgWrite", MsgWrite, true},
		{"MsgSeek", MsgSeek, true},
		{"MsgClose", MsgClose, true},
		{"MsgFstat", MsgFstat, true},
		{"MsgFtruncate", MsgFtruncate, true},
		{"MsgUnlink", MsgUnlink, true},
		{"MsgRename", MsgRename, true},
		{"MsgMkdir", MsgMkdir, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsFileIORequest(tc.msgType)
			if got != tc.want {
				t.Errorf("IsFileIORequest(0x%02x) = %v, want %v", tc.msgType, got, tc.want)
			}
		})
	}
}

// --- IsFileIOResponse ---

func TestIsFileIOResponse(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		want    bool
	}{
		{"below range 0x3F", 0x3F, false},
		{"lower bound MsgOpenOk", 0x40, true},
		{"upper bound MsgIoError", 0x4F, true},
		{"above range 0x50", 0x50, false},
		{"MsgOpenOk", MsgOpenOk, true},
		{"MsgReadOk", MsgReadOk, true},
		{"MsgWriteOk", MsgWriteOk, true},
		{"MsgSeekOk", MsgSeekOk, true},
		{"MsgCloseOk", MsgCloseOk, true},
		{"MsgFstatOk", MsgFstatOk, true},
		{"MsgFtruncateOk", MsgFtruncateOk, true},
		{"MsgUnlinkOk", MsgUnlinkOk, true},
		{"MsgRenameOk", MsgRenameOk, true},
		{"MsgMkdirOk", MsgMkdirOk, true},
		{"MsgIoError", MsgIoError, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsFileIOResponse(tc.msgType)
			if got != tc.want {
				t.Errorf("IsFileIOResponse(0x%02x) = %v, want %v", tc.msgType, got, tc.want)
			}
		})
	}
}

// --- Command message edge cases ---

func TestCommandMessageNoArgs(t *testing.T) {
	msg := &CommandMessage{Program: ProgramFFmpeg, Args: []string{}}
	encoded := msg.Encode()
	decoded, err := DecodeCommandMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Program != ProgramFFmpeg {
		t.Errorf("Program: got %d, want %d", decoded.Program, ProgramFFmpeg)
	}
	if len(decoded.Args) != 0 {
		t.Errorf("Args: got %v (len %d), want empty", decoded.Args, len(decoded.Args))
	}
}

func TestCommandMessageManyArgs(t *testing.T) {
	args := make([]string, 100)
	for i := range args {
		args[i] = fmt.Sprintf("arg-%d", i)
	}
	msg := &CommandMessage{Program: ProgramFFprobe, Args: args}
	decoded, err := DecodeCommandMessage(msg.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Args) != 100 {
		t.Fatalf("Args length: got %d, want 100", len(decoded.Args))
	}
	for i, arg := range decoded.Args {
		expected := fmt.Sprintf("arg-%d", i)
		if arg != expected {
			t.Errorf("Args[%d]: got %q, want %q", i, arg, expected)
		}
	}
}

func TestCommandMessageArgWithNullByte(t *testing.T) {
	// With length-prefixed args, null bytes in args are preserved correctly.
	msg := &CommandMessage{Program: ProgramFFmpeg, Args: []string{"before\x00after"}}
	decoded, err := DecodeCommandMessage(msg.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Args) != 1 {
		t.Fatalf("Args length: got %d, want 1", len(decoded.Args))
	}
	if decoded.Args[0] != "before\x00after" {
		t.Errorf("Args[0]: got %q, want %q", decoded.Args[0], "before\x00after")
	}
}

func TestCommandMessageDecodeArgCountTruncated(t *testing.T) {
	// Payload has version+nonce+sig+program but only 1 byte for argc (needs 2).
	payload := make([]byte, 1+NonceLength+HMACLength+1+1)
	payload[0] = CurrentVersion
	_, err := DecodeCommandMessage(payload)
	if err == nil {
		t.Fatal("expected error for truncated arg count, got nil")
	}
}

func TestCommandMessageDecodeArgTruncated(t *testing.T) {
	// Build a valid header, then argc=1 but truncate the arg data.
	headerLen := 1 + NonceLength + HMACLength + 1
	// argc=1, argLen=10, but only provide 3 bytes of arg data
	payload := make([]byte, headerLen+2+2+3)
	payload[0] = CurrentVersion
	binary.BigEndian.PutUint16(payload[headerLen:], 1)    // argc = 1
	binary.BigEndian.PutUint16(payload[headerLen+2:], 10) // arg len = 10, but only 3 bytes follow
	_, err := DecodeCommandMessage(payload)
	if err == nil {
		t.Fatal("expected error for truncated arg data, got nil")
	}
}

func TestCommandMessageNoArgsSerialization(t *testing.T) {
	msg := &CommandMessage{Program: ProgramFFmpeg, Args: []string{}}
	encoded := msg.Encode()

	// Expected: [version 1B][nonce 16B][sig 32B][program 1B][argc 0x00 0x00]
	expectedLen := 1 + NonceLength + HMACLength + 1 + 2
	if len(encoded) != expectedLen {
		t.Fatalf("length: got %d, want %d", len(encoded), expectedLen)
	}

	// Check argc bytes are 0x00, 0x00
	argcOffset := 1 + NonceLength + HMACLength + 1
	if encoded[argcOffset] != 0x00 || encoded[argcOffset+1] != 0x00 {
		t.Errorf("argc bytes: got [0x%02x, 0x%02x], want [0x00, 0x00]",
			encoded[argcOffset], encoded[argcOffset+1])
	}
}

// --- Multiple messages in sequence ---

func TestMultipleMessagesInSequence(t *testing.T) {
	var buf bytes.Buffer

	// Write 5 different messages
	messages := []struct {
		msgType uint8
		payload []byte
	}{
		{MsgOpen, (&OpenRequest{RequestID: 1, FileID: 1, Flags: FioORDONLY, Mode: 0, Path: "/tmp/a"}).Encode()},
		{MsgRead, (&ReadRequest{RequestID: 2, FileID: 1, NBytes: 4096}).Encode()},
		{MsgWrite, (&WriteRequest{RequestID: 3, FileID: 2, Data: []byte("data")}).Encode()},
		{MsgClose, (&CloseRequest{RequestID: 4, FileID: 1}).Encode()},
		{MsgIoError, (&IoErrorResponse{RequestID: 5, Errno: FioENOENT}).Encode()},
	}

	for _, m := range messages {
		if err := WriteMessageTo(&buf, m.msgType, m.payload); err != nil {
			t.Fatalf("WriteMessageTo failed for 0x%02x: %v", m.msgType, err)
		}
	}

	// Read all 5 back and verify
	for i, expected := range messages {
		msg, err := ReadMessageFrom(&buf)
		if err != nil {
			t.Fatalf("ReadMessageFrom #%d failed: %v", i, err)
		}
		if msg.Type != expected.msgType {
			t.Errorf("message #%d type: got 0x%02x, want 0x%02x", i, msg.Type, expected.msgType)
		}
		if !bytes.Equal(msg.Payload, expected.payload) {
			t.Errorf("message #%d payload mismatch", i)
		}
	}
}

// --- ReadMessage error cases ---

func TestReadMessageTruncatedHeader(t *testing.T) {
	// Only 3 bytes: 1 type byte + 2 of the 4-byte length field
	buf := bytes.NewReader([]byte{0x01, 0x00, 0x00})
	_, err := ReadMessageFrom(buf)
	if err == nil {
		t.Fatal("expected error for truncated header, got nil")
	}
	if !strings.Contains(err.Error(), "payload length") {
		t.Errorf("expected error about payload length, got: %v", err)
	}
}

func TestReadMessageTruncatedPayload(t *testing.T) {
	// Header says 100 bytes but only 10 bytes of payload provided
	var buf bytes.Buffer
	buf.WriteByte(MsgRead)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, 100)
	buf.Write(lenBuf)
	buf.Write(make([]byte, 10)) // only 10 of 100 bytes

	_, err := ReadMessageFrom(&buf)
	if err == nil {
		t.Fatal("expected error for truncated payload, got nil")
	}
	if !strings.Contains(err.Error(), "payload") {
		t.Errorf("expected error about payload, got: %v", err)
	}
}

// --- Edge case requests ---

func TestOpenRequestEmptyPath(t *testing.T) {
	req := &OpenRequest{RequestID: 1, FileID: 1, Flags: FioORDONLY, Mode: 0, Path: ""}
	decoded, err := DecodeOpenRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Path != "" {
		t.Errorf("Path: got %q, want empty string", decoded.Path)
	}
}

func TestFtruncateRequestZeroLength(t *testing.T) {
	req := &FtruncateRequest{RequestID: 1, FileID: 1, Length: 0}
	decoded, err := DecodeFtruncateRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Length != 0 {
		t.Errorf("Length: got %d, want 0", decoded.Length)
	}
}

func TestSeekRequestMaxValues(t *testing.T) {
	req := &SeekRequest{
		RequestID: math.MaxUint16,
		FileID:    math.MaxUint16,
		Offset:    math.MaxInt64,
		Whence:    FioSeekEnd,
	}
	decoded, err := DecodeSeekRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != math.MaxUint16 {
		t.Errorf("RequestID: got %d, want %d", decoded.RequestID, uint16(math.MaxUint16))
	}
	if decoded.FileID != math.MaxUint16 {
		t.Errorf("FileID: got %d, want %d", decoded.FileID, uint16(math.MaxUint16))
	}
	if decoded.Offset != math.MaxInt64 {
		t.Errorf("Offset: got %d, want %d", decoded.Offset, int64(math.MaxInt64))
	}
}

func TestRenameRequestEmptyPaths(t *testing.T) {
	req := &RenameRequest{RequestID: 1, OldPath: "", NewPath: ""}
	decoded, err := DecodeRenameRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.OldPath != "" {
		t.Errorf("OldPath: got %q, want empty string", decoded.OldPath)
	}
	if decoded.NewPath != "" {
		t.Errorf("NewPath: got %q, want empty string", decoded.NewPath)
	}
}

func TestWriteRequestLargeData(t *testing.T) {
	// 1MB data payload
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 251) // use a prime to avoid patterns
	}
	req := &WriteRequest{RequestID: 1, FileID: 1, Data: data}
	decoded, err := DecodeWriteRequest(req.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !bytes.Equal(decoded.Data, data) {
		t.Errorf("data mismatch: got %d bytes, want %d bytes", len(decoded.Data), len(data))
	}
}

func TestOpenOkResponseNegativeFileSize(t *testing.T) {
	resp := &OpenOkResponse{RequestID: 1, FileSize: -1}
	decoded, err := DecodeOpenOkResponse(resp.Encode())
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.FileSize != -1 {
		t.Errorf("FileSize: got %d, want -1", decoded.FileSize)
	}
}

func TestReadMessagePayloadBoundary(t *testing.T) {
	// Exactly 100MB should be accepted (not rejected by size check)
	var buf bytes.Buffer
	buf.WriteByte(0x01)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, 100*1024*1024) // exactly 100MB
	buf.Write(lenBuf)
	// ReadMessageFrom will accept the size but fail on ReadFull (no payload data)
	_, err := ReadMessageFrom(&buf)
	if err == nil {
		t.Fatal("expected error (no payload data), got nil")
	}
	// The error should be about reading payload, NOT about size
	if strings.Contains(err.Error(), "too large") {
		t.Fatalf("100MB should be accepted, got: %v", err)
	}

	// 100MB + 1 should be rejected by size check
	buf.Reset()
	buf.WriteByte(0x01)
	binary.BigEndian.PutUint32(lenBuf, 100*1024*1024+1) // 100MB + 1
	buf.Write(lenBuf)
	_, err = ReadMessageFrom(&buf)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("100MB+1 should be rejected as too large, got: %v", err)
	}
}

func TestReadMessageFromEOFOnType(t *testing.T) {
	// Empty reader should return io.EOF directly
	r := bytes.NewReader([]byte{})
	_, err := ReadMessageFrom(r)
	if err == nil {
		t.Fatal("expected error for empty reader, got nil")
	}
	if err != io.EOF && err != io.ErrUnexpectedEOF {
		// Accept either io.EOF or io.ErrUnexpectedEOF depending on implementation
		// but it should be an EOF-related error, not something else
		if !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("expected EOF-related error, got: %v", err)
		}
	}
}

func TestWriteMessageToNilPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessageTo(&buf, 0x05, nil); err != nil {
		t.Fatalf("WriteMessageTo failed: %v", err)
	}
	data := buf.Bytes()
	// Should be exactly 5 bytes: 1 type + 4 length
	if len(data) != 5 {
		t.Fatalf("expected 5 bytes, got %d", len(data))
	}
	if data[0] != 0x05 {
		t.Errorf("type byte: got 0x%02x, want 0x05", data[0])
	}
	length := binary.BigEndian.Uint32(data[1:5])
	if length != 0 {
		t.Errorf("payload length: got %d, want 0", length)
	}
}

func TestWriteMessageToEmptyVsNil(t *testing.T) {
	var bufNil bytes.Buffer
	var bufEmpty bytes.Buffer

	if err := WriteMessageTo(&bufNil, 0x10, nil); err != nil {
		t.Fatalf("WriteMessageTo(nil) failed: %v", err)
	}
	if err := WriteMessageTo(&bufEmpty, 0x10, []byte{}); err != nil {
		t.Fatalf("WriteMessageTo(empty) failed: %v", err)
	}

	if !bytes.Equal(bufNil.Bytes(), bufEmpty.Bytes()) {
		t.Fatalf("nil and empty payloads produced different wire output:\n  nil:   %v\n  empty: %v",
			bufNil.Bytes(), bufEmpty.Bytes())
	}
}
