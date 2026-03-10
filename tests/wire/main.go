package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// messageSpec defines a message type and its test vector encoder/decoder.
type messageSpec struct {
	msgType uint8
	encode  func() []byte
	verify  func(payload []byte) error
}

func testMessages() []messageSpec {
	return []messageSpec{
		// --- Requests ---
		{
			msgType: protocol.MsgOpen,
			encode: func() []byte {
				return (&protocol.OpenRequest{
					RequestID: 0x0102, FileID: 0x0304,
					Flags: 0x0241, Mode: 0x01B6, Path: "/tmp/test.ts",
				}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeOpenRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 0x0102 || m.FileID != 0x0304 || m.Flags != 0x0241 || m.Mode != 0x01B6 || m.Path != "/tmp/test.ts" {
					return fmt.Errorf("Open mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgRead,
			encode: func() []byte {
				return (&protocol.ReadRequest{RequestID: 5, FileID: 1, NBytes: 65536}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeReadRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 5 || m.FileID != 1 || m.NBytes != 65536 {
					return fmt.Errorf("Read mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgWrite,
			encode: func() []byte {
				return (&protocol.WriteRequest{RequestID: 10, FileID: 2, Data: []byte("hello video data")}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeWriteRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 10 || m.FileID != 2 || !bytes.Equal(m.Data, []byte("hello video data")) {
					return fmt.Errorf("Write mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgSeek,
			encode: func() []byte {
				return (&protocol.SeekRequest{RequestID: 7, FileID: 1, Offset: -9999999999, Whence: 2}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeSeekRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 7 || m.FileID != 1 || m.Offset != -9999999999 || m.Whence != 2 {
					return fmt.Errorf("Seek mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgClose,
			encode: func() []byte {
				return (&protocol.CloseRequest{RequestID: 100, FileID: 50}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeCloseRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 100 || m.FileID != 50 {
					return fmt.Errorf("Close mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgFstat,
			encode: func() []byte {
				return (&protocol.FstatRequest{RequestID: 3, FileID: 7}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeFstatRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 3 || m.FileID != 7 {
					return fmt.Errorf("Fstat mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgFtruncate,
			encode: func() []byte {
				return (&protocol.FtruncateRequest{RequestID: 20, FileID: 5, Length: 1048576}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeFtruncateRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 20 || m.FileID != 5 || m.Length != 1048576 {
					return fmt.Errorf("Ftruncate mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgUnlink,
			encode: func() []byte {
				return (&protocol.UnlinkRequest{RequestID: 15, Path: "/tmp/old.ts"}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeUnlinkRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 15 || m.Path != "/tmp/old.ts" {
					return fmt.Errorf("Unlink mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgRename,
			encode: func() []byte {
				return (&protocol.RenameRequest{RequestID: 30, OldPath: "/tmp/a.tmp", NewPath: "/tmp/a.m3u8"}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRenameRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 30 || m.OldPath != "/tmp/a.tmp" || m.NewPath != "/tmp/a.m3u8" {
					return fmt.Errorf("Rename mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgMkdir,
			encode: func() []byte {
				return (&protocol.MkdirRequest{RequestID: 8, Mode: 0755, Path: "/tmp/2024"}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeMkdirRequest(p)
				if err != nil {
					return err
				}
				if m.RequestID != 8 || m.Mode != 0755 || m.Path != "/tmp/2024" {
					return fmt.Errorf("Mkdir mismatch: %+v", m)
				}
				return nil
			},
		},
		// --- Responses ---
		{
			msgType: protocol.MsgOpenOk,
			encode: func() []byte {
				return (&protocol.OpenOkResponse{RequestID: 1, FileSize: 524288000}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeOpenOkResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 1 || m.FileSize != 524288000 {
					return fmt.Errorf("OpenOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgReadOk,
			encode: func() []byte {
				data := make([]byte, 16)
				for i := range data {
					data[i] = byte(i)
				}
				return (&protocol.ReadOkResponse{RequestID: 5, Data: data}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeReadOkResponse(p)
				if err != nil {
					return err
				}
				expected := make([]byte, 16)
				for i := range expected {
					expected[i] = byte(i)
				}
				if m.RequestID != 5 || !bytes.Equal(m.Data, expected) {
					return fmt.Errorf("ReadOk mismatch: req=%d datalen=%d", m.RequestID, len(m.Data))
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgWriteOk,
			encode: func() []byte {
				return (&protocol.WriteOkResponse{RequestID: 10, BytesWritten: 65536}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeWriteOkResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 10 || m.BytesWritten != 65536 {
					return fmt.Errorf("WriteOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgSeekOk,
			encode: func() []byte {
				return (&protocol.SeekOkResponse{RequestID: 7, Offset: 1048576}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeSeekOkResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 7 || m.Offset != 1048576 {
					return fmt.Errorf("SeekOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgCloseOk,
			encode: func() []byte {
				return (&protocol.RequestIDResponse{RequestID: 100}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRequestIDResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 100 {
					return fmt.Errorf("CloseOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgFstatOk,
			encode: func() []byte {
				return (&protocol.FstatOkResponse{RequestID: 3, FileSize: 999999, Mode: 0100644}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeFstatOkResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 3 || m.FileSize != 999999 || m.Mode != 0100644 {
					return fmt.Errorf("FstatOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgFtruncateOk,
			encode: func() []byte {
				return (&protocol.RequestIDResponse{RequestID: 20}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRequestIDResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 20 {
					return fmt.Errorf("FtruncateOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgUnlinkOk,
			encode: func() []byte {
				return (&protocol.RequestIDResponse{RequestID: 15}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRequestIDResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 15 {
					return fmt.Errorf("UnlinkOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgRenameOk,
			encode: func() []byte {
				return (&protocol.RequestIDResponse{RequestID: 30}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRequestIDResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 30 {
					return fmt.Errorf("RenameOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgMkdirOk,
			encode: func() []byte {
				return (&protocol.RequestIDResponse{RequestID: 8}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeRequestIDResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 8 {
					return fmt.Errorf("MkdirOk mismatch: %+v", m)
				}
				return nil
			},
		},
		{
			msgType: protocol.MsgIoError,
			encode: func() []byte {
				return (&protocol.IoErrorResponse{RequestID: 1, Errno: 2}).Encode()
			},
			verify: func(p []byte) error {
				m, err := protocol.DecodeIoErrorResponse(p)
				if err != nil {
					return err
				}
				if m.RequestID != 1 || m.Errno != 2 {
					return fmt.Errorf("IoError mismatch: %+v", m)
				}
				return nil
			},
		},
	}
}

func msgTypeName(t uint8) string {
	names := map[uint8]string{
		0x20: "Open", 0x21: "Read", 0x22: "Write", 0x23: "Seek",
		0x24: "Close", 0x25: "Fstat", 0x26: "Ftruncate",
		0x27: "Unlink", 0x28: "Rename", 0x29: "Mkdir",
		0x40: "OpenOk", 0x41: "ReadOk", 0x42: "WriteOk", 0x43: "SeekOk",
		0x44: "CloseOk", 0x45: "FstatOk", 0x46: "FtruncateOk",
		0x47: "UnlinkOk", 0x48: "RenameOk", 0x49: "MkdirOk",
		0x4F: "IoError",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return fmt.Sprintf("0x%02x", t)
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Println(port)
	os.Stdout.Sync()

	conn, err := ln.Accept()
	if err != nil {
		log.Fatalf("accept: %v", err)
	}
	defer conn.Close()

	msgs := testMessages()

	// Phase 1: Echo — read from C, decode, re-encode, write back
	for _, spec := range msgs {
		msg, err := protocol.ReadMessageFrom(conn)
		if err != nil {
			log.Fatalf("phase1 read %s: %v", msgTypeName(spec.msgType), err)
		}
		if msg.Type != spec.msgType {
			log.Fatalf("phase1 %s: got type 0x%02x, want 0x%02x", msgTypeName(spec.msgType), msg.Type, spec.msgType)
		}

		// Verify we can decode what C sent
		if err := spec.verify(msg.Payload); err != nil {
			log.Fatalf("phase1 verify %s: %v", msgTypeName(spec.msgType), err)
		}

		// Echo back: write the same message type with the raw payload
		// (decode + re-encode would also work, but raw echo is simpler and
		//  the verify above already proved the bytes are correct)
		if err := protocol.WriteMessageTo(conn, msg.Type, msg.Payload); err != nil {
			log.Fatalf("phase1 write %s: %v", msgTypeName(spec.msgType), err)
		}
	}

	// Phase 2: Go-originated — send each message with known test values
	for _, spec := range msgs {
		payload := spec.encode()
		if err := protocol.WriteMessageTo(conn, spec.msgType, payload); err != nil {
			log.Fatalf("phase2 write %s: %v", msgTypeName(spec.msgType), err)
		}
	}

	fmt.Fprintln(os.Stderr, "go wire-test: PASS")
}
