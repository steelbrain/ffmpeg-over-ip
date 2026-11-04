package handshake

import (
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

type serverResult struct {
	cmd *protocol.CommandMessage
	err error
}

func runPair(t *testing.T, clientSecret, serverSecret string, args []string) (error, serverResult) {
	t.Helper()
	c, s := net.Pipe()
	defer c.Close()
	ch := make(chan serverResult, 1)
	go func() {
		defer s.Close()
		cmd, err := Server(s, serverSecret)
		ch <- serverResult{cmd, err}
	}()
	cerr := Client(c, clientSecret, protocol.ProgramFFprobe, args)
	c.Close()
	return cerr, <-ch
}

func TestHandshakeSuccess(t *testing.T) {
	args := []string{"-i", "a b.mkv", "", "x"}
	cerr, sr := runPair(t, "secret", "secret", args)
	if cerr != nil || sr.err != nil {
		t.Fatalf("client err %v, server err %v", cerr, sr.err)
	}
	if sr.cmd.Program != protocol.ProgramFFprobe || !reflect.DeepEqual(sr.cmd.Args, args) {
		t.Fatalf("server got %+v", sr.cmd)
	}
}

// A server with a different secret rejects the command, and says so.
func TestHandshakeWrongSecret(t *testing.T) {
	cerr, sr := runPair(t, "secret", "other", []string{"-version"})
	var se *ServerError
	if !errors.As(cerr, &se) || se.Msg != "authentication failed" {
		t.Fatalf("client err = %v, want server error \"authentication failed\"", cerr)
	}
	var rej *Rejected
	if !errors.As(sr.err, &rej) || sr.cmd != nil {
		t.Fatalf("server: %+v, %v; want a rejection", sr.cmd, sr.err)
	}
}

// Nothing derived from the secret goes to a peer that hasn't proved it
// knows the secret: otherwise anyone who can connect could guess the
// secret offline.
func TestHandshakeNoProofBeforeClientProves(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	go func() {
		defer s.Close()
		Server(s, "secret")
	}()
	protocol.WriteMessageTo(c, protocol.MsgHello, (&protocol.HelloMessage{}).Encode())
	msg, err := protocol.ReadMessageFrom(c)
	if err != nil || msg.Type != protocol.MsgChallenge || len(msg.Payload) != protocol.NonceLength {
		t.Fatalf("challenge = %v, %v; want the bare %d-byte nonce", msg, err, protocol.NonceLength)
	}
	bad := &protocol.CommandMessage{Program: protocol.ProgramFFmpeg}
	chal, _ := protocol.DecodeChallengeMessage(msg.Payload)
	bad.Nonce = chal.Nonce
	protocol.WriteMessageTo(c, protocol.MsgCommand, bad.Encode())
	for {
		msg, err := protocol.ReadMessageFrom(c)
		if err != nil {
			break
		}
		if msg.Type == protocol.MsgAccepted {
			t.Fatal("server sent its proof to a client that failed authentication")
		}
	}
}

// Something at the server's address that doesn't know the secret can't
// pass as the server: the client stops before acting on anything from it.
func TestHandshakeImpostorServer(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	go func() {
		defer s.Close()
		protocol.ReadMessageFrom(s) // hello
		ch := &protocol.ChallengeMessage{Nonce: [16]byte{1}}
		protocol.WriteMessageTo(s, protocol.MsgChallenge, ch.Encode())
		protocol.ReadMessageFrom(s) // command
		acc := &protocol.AcceptedMessage{Proof: [32]byte{2}}
		protocol.WriteMessageTo(s, protocol.MsgAccepted, acc.Encode())
	}()
	if err := Client(c, "secret", protocol.ProgramFFmpeg, nil); !errors.Is(err, ErrServerAuth) {
		t.Fatalf("err = %v, want ErrServerAuth", err)
	}
}

// A server from an older release answers a hello with "expected command
// message"; the client says the releases differ.
func TestHandshakeOldServerHint(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	go func() {
		defer s.Close()
		protocol.ReadMessageFrom(s)
		protocol.WriteMessageTo(s, protocol.MsgError, []byte("expected command message (0x01), got 0x07"))
	}()
	err := Client(c, "secret", protocol.ProgramFFmpeg, nil)
	var se *ServerError
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "same release") {
		t.Fatalf("err = %v, want a ServerError naming the release mismatch", err)
	}
}

func TestHandshakeHelloVersionMismatch(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	ch := make(chan serverResult, 1)
	go func() {
		defer s.Close()
		cmd, err := Server(s, "secret")
		ch <- serverResult{cmd, err}
	}()
	hello := (&protocol.HelloMessage{}).Encode()
	hello[0] = protocol.CurrentVersion + 1
	protocol.WriteMessageTo(c, protocol.MsgHello, hello)
	msg, err := protocol.ReadMessageFrom(c)
	if err != nil || msg.Type != protocol.MsgError || !strings.Contains(string(msg.Payload), "protocol version mismatch") {
		t.Fatalf("got %v %v, want a version mismatch error", msg, err)
	}
	if r := <-ch; r.cmd != nil {
		t.Fatal("server accepted a mismatched hello")
	}
}

// A command signed for the right nonces but sent with another nonce echoed
// is rejected (the echo must match the challenge).
func TestHandshakeNonceEchoMismatch(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	ch := make(chan serverResult, 1)
	go func() {
		defer s.Close()
		cmd, err := Server(s, "secret")
		ch <- serverResult{cmd, err}
	}()
	protocol.WriteMessageTo(c, protocol.MsgHello, (&protocol.HelloMessage{}).Encode())
	msg, _ := protocol.ReadMessageFrom(c)
	chal, _ := protocol.DecodeChallengeMessage(msg.Payload)
	cmd := &protocol.CommandMessage{Program: protocol.ProgramFFmpeg}
	cmd.Nonce = chal.Nonce
	cmd.Nonce[0] ^= 1
	protocol.WriteMessageTo(c, protocol.MsgCommand, cmd.Encode())
	msg, _ = protocol.ReadMessageFrom(c)
	if msg == nil || string(msg.Payload) != "authentication failed" {
		t.Fatalf("got %v, want authentication failed", msg)
	}
	if r := <-ch; r.cmd != nil {
		t.Fatal("server accepted the command")
	}
}

func TestHandshakeCommandTooLarge(t *testing.T) {
	big := strings.Repeat("x", 60000)
	args := make([]string, 20) // 1.2 MB encoded, over the 1 MiB limit
	for i := range args {
		args[i] = big
	}
	c, s := net.Pipe()
	defer s.Close()
	defer c.Close()
	err := Client(c, "secret", protocol.ProgramFFmpeg, args)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want a size error before anything is sent", err)
	}

	if err := Client(c, "s", protocol.ProgramFFmpeg, []string{strings.Repeat("y", 70000)}); err == nil || !strings.Contains(err.Error(), "argument 1") {
		t.Fatalf("err = %v, want an argument length error", err)
	}
}

// Every message a peer can send before the handshake completes is capped,
// on both sides.
func TestHandshakeOversizedMessagesRejected(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	go func() {
		defer s.Close()
		protocol.ReadMessageFrom(s)
		s.Write([]byte{protocol.MsgChallenge, 0x7F, 0xFF, 0xFF, 0xFF})
	}()
	err := Client(c, "secret", protocol.ProgramFFmpeg, nil)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want payload too large", err)
	}
}

// When unauthenticated connections already hold the whole budget, a new
// message that needs buffer is refused instead of growing memory.
func TestHandshakePreAuthBudget(t *testing.T) {
	saved := preAuth
	preAuth = &byteBudget{max: 8192, hostMax: 8192}
	defer func() { preAuth = saved }()

	if !preAuth.take("", 8192-100) { // someone else holds most of it
		t.Fatal("take")
	}
	c, s := net.Pipe()
	defer c.Close()
	ch := make(chan serverResult, 1)
	go func() {
		defer s.Close()
		cmd, err := Server(s, "secret")
		ch <- serverResult{cmd, err}
	}()
	// A command-first client with a 5000-byte payload needs more than is
	// left. It is told why.
	go protocol.WriteMessageTo(c, protocol.MsgCommand, make([]byte, 5000))
	msg, err := protocol.ReadMessageFrom(c)
	if err != nil || msg.Type != protocol.MsgError || !strings.HasPrefix(string(msg.Payload), "server busy") {
		t.Fatalf("client got %v, %v; want a server busy error", msg, err)
	}
	r := <-ch
	if !errors.Is(r.err, protocol.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", r.err)
	}
	preAuth.give("", 8192-100)
	if preAuth.used != 0 {
		t.Fatalf("budget leaked: %d bytes still held", preAuth.used)
	}

	// With the budget free again, a real handshake goes through and gives
	// back everything it held.
	if cerr, sr := runPair(t, "secret", "secret", []string{"-version"}); cerr != nil || sr.err != nil {
		t.Fatalf("client %v, server %v", cerr, sr.err)
	}
	if preAuth.used != 0 {
		t.Fatalf("budget leaked after a handshake: %d bytes", preAuth.used)
	}
}

// Connections that sent a header announcing a large message and then went
// quiet hold none of the budget, so they can't lock out real clients.
func TestHandshakeHeaderOnlyConnectionsHoldNothing(t *testing.T) {
	saved := preAuth
	preAuth = &byteBudget{max: 8192, hostMax: 8192}
	defer func() { preAuth = saved }()

	var wg sync.WaitGroup
	defer wg.Wait() // before preAuth is restored
	for i := 0; i < 100; i++ {
		c, s := net.Pipe()
		defer c.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer s.Close()
			Server(s, "secret")
		}()
		// net.Pipe: returns once the server has read the header.
		c.Write([]byte{protocol.MsgHello, 0x00, 0x10, 0x00, 0x00})
	}
	time.Sleep(50 * time.Millisecond)
	preAuth.mu.Lock()
	used := preAuth.used
	preAuth.mu.Unlock()
	if used != 0 {
		t.Fatalf("%d header-only connections hold %d bytes of budget", 100, used)
	}
	if cerr, sr := runPair(t, "secret", "secret", []string{"-version"}); cerr != nil || sr.err != nil {
		t.Fatalf("client %v, server %v", cerr, sr.err)
	}
}

// One host can hold only its share of the budget; other hosts still get in.
// IPv6 addresses in one /64 count as one host.
func TestPreAuthBudgetPerHost(t *testing.T) {
	b := &byteBudget{max: 8192, hostMax: 2048}
	if !b.take("10.0.0.1", 2048) || b.take("10.0.0.1", 1) {
		t.Fatal("host share not enforced")
	}
	if !b.take("10.0.0.2", 2048) || !b.take("", 4096) {
		t.Fatal("other hosts refused")
	}
	if b.take("10.0.0.3", 1) {
		t.Fatal("overall budget not enforced")
	}
	b.give("10.0.0.1", 2048)
	if !b.take("10.0.0.1", 2048) {
		t.Fatal("share not given back")
	}
	b.give("10.0.0.1", 2048)
	b.give("10.0.0.2", 2048)
	b.give("", 4096)
	if b.used != 0 || len(b.hosts) != 0 {
		t.Fatalf("leaked: used %d, hosts %v", b.used, b.hosts)
	}

	key := func(s string) string { return hostKey(&net.TCPAddr{IP: net.ParseIP(s), Port: 1}) }
	if key("2001:db8::1") != key("2001:db8::ffff:2") || key("2001:db8::1") == key("2001:db8:0:1::1") {
		t.Fatal("IPv6 hosts not keyed by /64")
	}
	if key("::ffff:10.0.0.1") != "10.0.0.1" || key("10.0.0.1") != "10.0.0.1" {
		t.Fatal("IPv4 hosts not keyed by address")
	}
	if hostKey(&net.UnixAddr{Name: "@", Net: "unix"}) != "" || hostKey(nil) != "" {
		t.Fatal("non-IP connections should share only the overall budget")
	}
}
