package protocol

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestHelloRoundTrip(t *testing.T) {
	m := &HelloMessage{Nonce: [NonceLength]byte{1, 2, 3, 15: 16}}
	p := m.Encode()
	if len(p) != 1+NonceLength || p[0] != CurrentVersion {
		t.Fatalf("encoded hello = %x", p)
	}
	d, err := DecodeHelloMessage(p)
	if err != nil || d.Nonce != m.Nonce {
		t.Fatalf("decoded %+v, %v", d, err)
	}
}

func TestHelloVersionCheckedFirst(t *testing.T) {
	// Another release's hello may have another length; the version error
	// must win so the user learns what is actually wrong.
	_, err := DecodeHelloMessage([]byte{CurrentVersion + 1, 0xAA})
	if err == nil || !strings.Contains(err.Error(), "protocol version mismatch") {
		t.Fatalf("err = %v", err)
	}
	if _, err := DecodeHelloMessage([]byte{CurrentVersion, 1, 2}); err == nil {
		t.Fatal("short hello accepted")
	}
	if _, err := DecodeHelloMessage(nil); err == nil {
		t.Fatal("empty hello accepted")
	}
}

func TestChallengeRoundTrip(t *testing.T) {
	m := &ChallengeMessage{Nonce: [NonceLength]byte{9, 15: 1}}
	p := m.Encode()
	if len(p) != NonceLength {
		t.Fatalf("challenge is %d bytes; it must carry only the nonce", len(p))
	}
	d, err := DecodeChallengeMessage(p)
	if err != nil || d.Nonce != m.Nonce {
		t.Fatalf("decoded %+v, %v", d, err)
	}
	if _, err := DecodeChallengeMessage(make([]byte, NonceLength+1)); err == nil {
		t.Fatal("long challenge accepted")
	}
}

func TestAcceptedRoundTrip(t *testing.T) {
	m := &AcceptedMessage{Proof: [HMACLength]byte{31: 7}}
	d, err := DecodeAcceptedMessage(m.Encode())
	if err != nil || d.Proof != m.Proof {
		t.Fatalf("decoded %+v, %v", d, err)
	}
	if _, err := DecodeAcceptedMessage(make([]byte, HMACLength-1)); err == nil {
		t.Fatal("short accepted accepted")
	}
}

func TestStdinAckRoundTrip(t *testing.T) {
	n, err := DecodeStdinAck(EncodeStdinAck(0xDEADBEEF))
	if err != nil || n != 0xDEADBEEF {
		t.Fatalf("got %x, %v", n, err)
	}
	if _, err := DecodeStdinAck([]byte{1, 2, 3}); err == nil {
		t.Fatal("short ack accepted")
	}
}

func TestReadMessageLimit(t *testing.T) {
	var buf bytes.Buffer
	WriteMessageTo(&buf, MsgHello, make([]byte, 100))
	if _, err := ReadMessageLimit(bytes.NewReader(buf.Bytes()), 99); err == nil {
		t.Fatal("message over the limit accepted")
	}
	msg, err := ReadMessageLimit(bytes.NewReader(buf.Bytes()), 100)
	if err != nil || msg.Type != MsgHello || len(msg.Payload) != 100 {
		t.Fatalf("got %+v, %v", msg, err)
	}
	// The limit is checked from the header alone: no payload needs to follow.
	hdr := []byte{MsgHello, 0x7F, 0xFF, 0xFF, 0xFF}
	if _, err := ReadMessageLimit(bytes.NewReader(hdr), MaxHandshakePayload); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadMessageUntrusted(t *testing.T) {
	var buf bytes.Buffer
	payload := bytes.Repeat([]byte{0xAB}, 10000) // several growth steps
	WriteMessageTo(&buf, MsgCommand, payload)
	msg, err := ReadMessageUntrusted(bytes.NewReader(buf.Bytes()), MaxHandshakePayload, nil)
	if err != nil || msg.Type != MsgCommand || !bytes.Equal(msg.Payload, payload) {
		t.Fatalf("got %v, %v", msg, err)
	}
	if _, err := ReadMessageUntrusted(bytes.NewReader(buf.Bytes()), 9999, nil); err == nil {
		t.Fatal("message over the limit accepted")
	}
	msg, err = ReadMessageUntrusted(bytes.NewReader([]byte{MsgPing, 0, 0, 0, 0}), 10, nil)
	if err != nil || len(msg.Payload) != 0 {
		t.Fatalf("empty payload: %v, %v", msg, err)
	}
}

// A peer that announces a large message and then sends little must not
// make the reader allocate the announced size.
func TestReadMessageUntrustedAllocatesAsDataArrives(t *testing.T) {
	hdr := []byte{MsgHello, 0x00, 0x10, 0x00, 0x00} // announces 1 MiB
	r := io.MultiReader(bytes.NewReader(hdr), bytes.NewReader(make([]byte, 100)))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := ReadMessageUntrusted(r, MaxHandshakePayload, nil)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("truncated message accepted")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 64*1024 {
		t.Fatalf("allocated %d bytes for 100 bytes of payload", grew)
	}
}

// Buffer is reserved only for bytes that have arrived: nothing for a bare
// header, and otherwise at most twice what was sent and at most 16 KiB more.
func TestReadMessageUntrustedReservesForArrivedBytes(t *testing.T) {
	hdr := []byte{MsgHello, 0x00, 0x10, 0x00, 0x00} // announces 1 MiB
	for _, sent := range []int{0, 1, 300, 5000, 70000, 600000} {
		granted := 0
		r := io.MultiReader(bytes.NewReader(hdr), bytes.NewReader(make([]byte, sent)))
		_, err := ReadMessageUntrusted(r, MaxHandshakePayload, func(n int) bool {
			granted += n
			return true
		})
		if err == nil {
			t.Fatal("truncated message accepted")
		}
		if sent == 0 && granted != 0 || granted > min(max(256, 2*sent), sent+16*1024) {
			t.Fatalf("reserved %d bytes for %d bytes of payload", granted, sent)
		}
	}
}

func TestReadMessageUntrustedReserve(t *testing.T) {
	var buf bytes.Buffer
	WriteMessageTo(&buf, MsgCommand, make([]byte, 10000))
	granted := 0
	_, err := ReadMessageUntrusted(bytes.NewReader(buf.Bytes()), MaxHandshakePayload, func(n int) bool {
		if granted+n > 6000 {
			return false
		}
		granted += n
		return true
	})
	if err != ErrBusy || granted > 6000 {
		t.Fatalf("err = %v after granting %d bytes, want ErrBusy within 6000", err, granted)
	}
}

// A length of 2^31 or more is too large on 32-bit builds too, where it
// would not fit in an int.
func TestReadMessageUntrustedHugeLength(t *testing.T) {
	for _, hdr := range [][]byte{
		{MsgHello, 0xFF, 0xFF, 0xFF, 0xFF},
		{MsgHello, 0x80, 0x00, 0x00, 0x00},
	} {
		msg, err := ReadMessageUntrusted(bytes.NewReader(hdr), MaxHandshakePayload, nil)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("header %x: got %v, %v; want payload too large", hdr, msg, err)
		}
	}
}

// The buffer is exactly as large as what was reserved for it.
func TestReadMessageUntrustedChargesWhatItAllocates(t *testing.T) {
	var buf bytes.Buffer
	payload := bytes.Repeat([]byte{0x5A}, 900000)
	WriteMessageTo(&buf, MsgCommand, payload)
	granted := 0
	msg, err := ReadMessageUntrusted(&buf, MaxHandshakePayload, func(n int) bool {
		granted += n
		return true
	})
	if err != nil || !bytes.Equal(msg.Payload, payload) {
		t.Fatalf("got %v", err)
	}
	if cap(msg.Payload) != granted || granted != len(payload) {
		t.Fatalf("buffer capacity %d, reserved %d, payload %d", cap(msg.Payload), granted, len(payload))
	}
}

// Reading a large message allocates about twice its size (the pieces, then
// the joined payload), not a copy of the buffer for every piece.
func TestReadMessageUntrustedAllocatesLinearly(t *testing.T) {
	var buf bytes.Buffer
	WriteMessageTo(&buf, MsgCommand, make([]byte, MaxHandshakePayload))
	data := buf.Bytes()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := ReadMessageUntrusted(bytes.NewReader(data), MaxHandshakePayload, nil); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 3*MaxHandshakePayload {
		t.Fatalf("allocated %d bytes to read a %d-byte message", grew, MaxHandshakePayload)
	}
}
