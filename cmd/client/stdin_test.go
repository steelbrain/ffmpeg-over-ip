package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
	"github.com/steelbrain/ffmpeg-over-ip/internal/session"
)

// The client stops sending stdin at the window until the server acks, then
// carries on, and sends everything followed by MsgStdinClose.
func TestPumpStdinRespectsWindow(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	total := 2*protocol.StdinWindow + 12345
	credit := newStdinCredit()
	go pumpStdin(bytes.NewReader(make([]byte, total)), session.NewWriter(client), credit)

	got := 0
	for got < protocol.StdinWindow {
		msg, err := protocol.ReadMessageFrom(server)
		if err != nil || msg.Type != protocol.MsgStdin {
			t.Fatalf("got %v, %v", msg, err)
		}
		got += len(msg.Payload)
	}
	if got != protocol.StdinWindow {
		t.Fatalf("sent %d bytes before the first ack, want exactly the window %d", got, protocol.StdinWindow)
	}

	// Nothing more without an ack.
	server.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if msg, err := protocol.ReadMessageFrom(server); err == nil {
		t.Fatalf("client sent 0x%02x past the window", msg.Type)
	}
	server.SetReadDeadline(time.Time{})

	// Ack as the server would; everything else arrives, then the close.
	credit.release(got)
	closed := false
	for !closed {
		msg, err := protocol.ReadMessageFrom(server)
		if err != nil {
			t.Fatal(err)
		}
		switch msg.Type {
		case protocol.MsgStdin:
			got += len(msg.Payload)
			credit.release(len(msg.Payload))
		case protocol.MsgStdinClose:
			closed = true
		}
	}
	if got != total {
		t.Fatalf("forwarded %d bytes, want %d", got, total)
	}
}

func TestPumpStdinClosesOnEmptyInput(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go pumpStdin(io.LimitReader(nil, 0), session.NewWriter(client), newStdinCredit())
	msg, err := protocol.ReadMessageFrom(server)
	if err != nil || msg.Type != protocol.MsgStdinClose {
		t.Fatalf("got %v, %v; want MsgStdinClose", msg, err)
	}
}
