package session

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

func TestNewWriter(t *testing.T) {
	before := time.Now()
	sw := NewWriter(&bytes.Buffer{})
	after := time.Now()

	last := sw.LastSendTime()
	if last.Before(before) {
		t.Errorf("LastSendTime %v is before construction start %v", last, before)
	}
	if last.After(after) {
		t.Errorf("LastSendTime %v is after construction end %v", last, after)
	}
}

func TestWriteMessageSendsCorrectData(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		payload []byte
	}{
		{"ping with no payload", protocol.MsgPing, nil},
		{"stdout with text", protocol.MsgStdout, []byte("hello world")},
		{"open request", protocol.MsgOpen, []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x40, 0x01, 0xA4, 0x2F, 0x74, 0x6D, 0x70}},
		{"binary payload", protocol.MsgWrite, []byte{0x00, 0xFF, 0x80, 0x7F}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			sw := NewWriter(&buf)

			err := sw.WriteMessage(tt.msgType, tt.payload)
			if err != nil {
				t.Fatalf("WriteMessage returned error: %v", err)
			}

			msg, err := protocol.ReadMessageFrom(&buf)
			if err != nil {
				t.Fatalf("ReadMessageFrom returned error: %v", err)
			}

			if msg.Type != tt.msgType {
				t.Errorf("message type = 0x%02x, want 0x%02x", msg.Type, tt.msgType)
			}

			wantPayload := tt.payload
			if wantPayload == nil {
				wantPayload = []byte{}
			}
			if !bytes.Equal(msg.Payload, wantPayload) {
				t.Errorf("payload = %v, want %v", msg.Payload, wantPayload)
			}
		})
	}
}

func TestWriteMessageUpdatesLastSendTime(t *testing.T) {
	var buf bytes.Buffer
	sw := NewWriter(&buf)

	initial := sw.LastSendTime()

	// Small sleep to ensure the clock advances
	time.Sleep(time.Millisecond)

	err := sw.WriteMessage(protocol.MsgPing, nil)
	if err != nil {
		t.Fatalf("WriteMessage returned error: %v", err)
	}

	updated := sw.LastSendTime()
	if !updated.After(initial) {
		t.Errorf("LastSendTime was not updated: initial=%v, after write=%v", initial, updated)
	}
}

func TestWriteMessageConcurrent(t *testing.T) {
	const numGoroutines = 100

	// Use net.Pipe so we have a reader that can drain messages concurrently
	// while writers write on the other side.
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	sw := NewWriter(clientConn)

	// Reader goroutine: drain all messages and count them
	var received int
	readerDone := make(chan error, 1)
	go func() {
		for {
			_, err := protocol.ReadMessageFrom(serverConn)
			if err != nil {
				readerDone <- err
				return
			}
			received++
			if received == numGoroutines {
				readerDone <- nil
				return
			}
		}
	}()

	// Launch concurrent writers
	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := []byte{byte(idx)}
			errs[idx] = sw.WriteMessage(protocol.MsgStdout, payload)
		}(i)
	}

	wg.Wait()

	// Check that no writer returned an error
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: WriteMessage returned error: %v", i, err)
		}
	}

	// Wait for reader to finish
	select {
	case err := <-readerDone:
		if err != nil {
			t.Fatalf("reader error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reader to receive all messages")
	}

	if received != numGoroutines {
		t.Errorf("received %d messages, want %d", received, numGoroutines)
	}
}

func TestWriteMessageWithNilPayload(t *testing.T) {
	var buf bytes.Buffer
	sw := NewWriter(&buf)

	err := sw.WriteMessage(protocol.MsgPong, nil)
	if err != nil {
		t.Fatalf("WriteMessage with nil payload returned error: %v", err)
	}

	msg, err := protocol.ReadMessageFrom(&buf)
	if err != nil {
		t.Fatalf("ReadMessageFrom returned error: %v", err)
	}

	if msg.Type != protocol.MsgPong {
		t.Errorf("message type = 0x%02x, want 0x%02x", msg.Type, protocol.MsgPong)
	}
	if len(msg.Payload) != 0 {
		t.Errorf("payload length = %d, want 0", len(msg.Payload))
	}
}

func TestWriteMessageWithLargePayload(t *testing.T) {
	const size = 1024 * 1024 // 1 MB
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i % 251) // use a prime to create a non-trivial pattern
	}

	var buf bytes.Buffer
	sw := NewWriter(&buf)

	err := sw.WriteMessage(protocol.MsgWrite, payload)
	if err != nil {
		t.Fatalf("WriteMessage with 1MB payload returned error: %v", err)
	}

	msg, err := protocol.ReadMessageFrom(&buf)
	if err != nil {
		t.Fatalf("ReadMessageFrom returned error: %v", err)
	}

	if msg.Type != protocol.MsgWrite {
		t.Errorf("message type = 0x%02x, want 0x%02x", msg.Type, protocol.MsgWrite)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Errorf("payload mismatch: got %d bytes, want %d bytes", len(msg.Payload), len(payload))
	}
}

func TestWriteMessageWriteError(t *testing.T) {
	errBroken := errors.New("broken pipe")
	sw := NewWriter(&failWriter{err: errBroken})

	err := sw.WriteMessage(protocol.MsgPing, []byte("test"))
	if err == nil {
		t.Fatal("expected error from WriteMessage, got nil")
	}
	// The error should be wrapped by protocol.WriteMessageTo, but the
	// underlying cause should be our sentinel error.
	if !errors.Is(err, errBroken) {
		t.Errorf("error = %v, want it to wrap %v", err, errBroken)
	}
}

// failWriter is an io.Writer that always returns the configured error.
type failWriter struct {
	err error
}

func (f *failWriter) Write([]byte) (int, error) {
	return 0, f.err
}

// Verify failWriter implements io.Writer.
var _ io.Writer = (*failWriter)(nil)

func TestLastSendTimeMonotonicallyIncreasing(t *testing.T) {
	var buf bytes.Buffer
	sw := NewWriter(&buf)

	prev := sw.LastSendTime()
	for i := 0; i < 20; i++ {
		// Small sleep to ensure clock advances between iterations.
		time.Sleep(time.Millisecond)

		err := sw.WriteMessage(protocol.MsgPing, nil)
		if err != nil {
			t.Fatalf("iteration %d: WriteMessage returned error: %v", i, err)
		}

		current := sw.LastSendTime()
		if current.Before(prev) {
			t.Errorf("iteration %d: LastSendTime went backwards: prev=%v, current=%v", i, prev, current)
		}
		prev = current
	}
}
