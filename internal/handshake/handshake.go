// Package handshake implements connection setup between client and server:
// a challenge-response exchange in which each side proves it knows the
// shared secret, followed by the signed command. See protocol.HelloMessage
// for the message layout.
package handshake

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/auth"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Timeout bounds the whole handshake on either side, so a silent peer can't
// hold a connection (and its goroutine) for long.
const Timeout = 10 * time.Second

// ServerError is an error message the server sent instead of completing the
// handshake.
type ServerError struct {
	Msg string
}

func (e *ServerError) Error() string {
	msg := "server error: " + e.Msg
	if strings.HasPrefix(e.Msg, "expected command message") {
		// A server from an older release expects the command first.
		msg += " (the server runs a different ffmpeg-over-ip release; client and server must run the same release)"
	}
	return msg
}

// ErrServerAuth means the server accepted the command but could not prove it
// knows the secret. A real server with a different secret rejects the
// command instead ("authentication failed"), so this points at something
// other than the configured server answering at its address.
var ErrServerAuth = errors.New("authentication failed: the server accepted the command but did not prove it knows the auth secret; check that the address points at your ffmpeg-over-ip server")

// Client performs the client side of the handshake on conn and sends the
// signed command. When it returns nil, the server has accepted the command
// and proved it knows the secret.
func Client(conn net.Conn, secret string, program uint8, args []string) error {
	cmd := &protocol.CommandMessage{Program: program, Args: args}
	if err := checkCommandSize(cmd); err != nil {
		return err
	}

	conn.SetDeadline(time.Now().Add(Timeout))
	defer conn.SetDeadline(time.Time{})

	var hello protocol.HelloMessage
	if _, err := rand.Read(hello.Nonce[:]); err != nil {
		return fmt.Errorf("generating nonce: %w", err)
	}
	if err := protocol.WriteMessageTo(conn, protocol.MsgHello, hello.Encode()); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}

	msg, err := protocol.ReadMessageUntrusted(conn, protocol.MaxHandshakePayload, nil)
	if err != nil {
		return fmt.Errorf("reading challenge: %w", err)
	}
	if err := expect(msg, protocol.MsgChallenge); err != nil {
		return err
	}
	challenge, err := protocol.DecodeChallengeMessage(msg.Payload)
	if err != nil {
		return err
	}

	cmd.Nonce = challenge.Nonce
	cmd.Signature = auth.Sign(secret, protocol.CurrentVersion, hello.Nonce, challenge.Nonce, program, args)
	if err := protocol.WriteMessageTo(conn, protocol.MsgCommand, cmd.Encode()); err != nil {
		return fmt.Errorf("sending command: %w", err)
	}

	msg, err = protocol.ReadMessageUntrusted(conn, protocol.MaxHandshakePayload, nil)
	if err != nil {
		return fmt.Errorf("reading the server's answer to the command: %w", err)
	}
	if err := expect(msg, protocol.MsgAccepted); err != nil {
		return err
	}
	accepted, err := protocol.DecodeAcceptedMessage(msg.Payload)
	if err != nil {
		return err
	}
	if !auth.VerifyServerProof(secret, protocol.CurrentVersion, hello.Nonce, challenge.Nonce, accepted.Proof) {
		return ErrServerAuth
	}
	return nil
}

// expect checks that msg has type want, turning an MsgError into a
// *ServerError.
func expect(msg *protocol.Message, want uint8) error {
	switch msg.Type {
	case want:
		return nil
	case protocol.MsgError:
		return &ServerError{Msg: string(msg.Payload)}
	default:
		return fmt.Errorf("unexpected message 0x%02x from server during handshake (wanted 0x%02x)", msg.Type, want)
	}
}

// checkCommandSize rejects commands the server won't accept, before
// connecting, with an error that says why. The wire format also limits
// argc and each argument to 65535 (uint16 lengths).
func checkCommandSize(cmd *protocol.CommandMessage) error {
	if len(cmd.Args) > 0xFFFF {
		return fmt.Errorf("too many arguments: %d (limit %d)", len(cmd.Args), 0xFFFF)
	}
	for i, a := range cmd.Args {
		if len(a) > 0xFFFF {
			return fmt.Errorf("argument %d is %d bytes long (limit %d)", i+1, len(a), 0xFFFF)
		}
	}
	if n := len(cmd.Encode()); n > protocol.MaxHandshakePayload {
		return fmt.Errorf("command line is %d bytes encoded (limit %d)", n, protocol.MaxHandshakePayload)
	}
	return nil
}

// MaxPreAuthBytes bounds the message bytes that all connections still in the
// handshake may hold together, and MaxPreAuthBytesPerHost what connections
// from one IP address (one /64 for IPv6) may hold, so a single host can't
// fill the budget for everyone. A connection whose message doesn't fit is
// dropped. Buffer is taken only as bytes arrive, so idle connections hold
// none of it.
const (
	MaxPreAuthBytes        = 64 * 1024 * 1024
	MaxPreAuthBytesPerHost = MaxPreAuthBytes / 4
)

var preAuth = &byteBudget{max: MaxPreAuthBytes, hostMax: MaxPreAuthBytesPerHost}

type byteBudget struct {
	mu        sync.Mutex
	used, max int
	hostMax   int
	hosts     map[string]int // bytes held per hostKey; "" isn't limited
}

func (b *byteBudget) take(host string, n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+n > b.max || host != "" && b.hosts[host]+n > b.hostMax {
		return false
	}
	b.used += n
	if host != "" {
		if b.hosts == nil {
			b.hosts = make(map[string]int)
		}
		b.hosts[host] += n
	}
	return true
}

func (b *byteBudget) give(host string, n int) {
	if n == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= n
	if host != "" {
		if b.hosts[host] -= n; b.hosts[host] <= 0 {
			delete(b.hosts, host)
		}
	}
}

// hostKey names the host a connection comes from, for the per-host share of
// the budget: its IP address, or its /64 for IPv6, where one host can easily
// use many addresses. Connections that aren't over IP (Unix sockets) share
// only the overall budget.
func hostKey(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp.IP == nil {
		return ""
	}
	if ip4 := tcp.IP.To4(); ip4 != nil {
		return ip4.String()
	}
	return tcp.IP.Mask(net.CIDRMask(64, 128)).String()
}

// busyMessage is sent to a client whose handshake message didn't fit in
// MaxPreAuthBytes.
const busyMessage = "server busy: too much data from connections that haven't authenticated yet; try again later"

// Rejected is returned by Server when the client failed the handshake. Msg
// has already been sent to the client.
type Rejected struct {
	Msg string
}

func (e *Rejected) Error() string { return e.Msg }

// Server performs the server side of the handshake on conn and returns the
// verified command. If the client fails, Server sends it an MsgError and
// returns a *Rejected; other errors (I/O) mean the connection is unusable.
func Server(conn net.Conn, secret string) (*protocol.CommandMessage, error) {
	conn.SetDeadline(time.Now().Add(Timeout))
	defer conn.SetDeadline(time.Time{})

	host := hostKey(conn.RemoteAddr())
	held := 0
	reserve := func(n int) bool {
		if !preAuth.take(host, n) {
			return false
		}
		held += n
		return true
	}
	defer func() { preAuth.give(host, held) }()

	reject := func(format string, a ...any) (*protocol.CommandMessage, error) {
		msg := fmt.Sprintf(format, a...)
		protocol.WriteMessageTo(conn, protocol.MsgError, []byte(msg))
		return nil, &Rejected{Msg: msg}
	}

	read := func(what string) (*protocol.Message, error) {
		msg, err := protocol.ReadMessageUntrusted(conn, protocol.MaxHandshakePayload, reserve)
		if errors.Is(err, protocol.ErrBusy) {
			// Tell the client why before dropping it.
			protocol.WriteMessageTo(conn, protocol.MsgError, []byte(busyMessage))
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", what, err)
		}
		return msg, nil
	}

	msg, err := read("hello")
	if err != nil {
		return nil, err
	}
	switch msg.Type {
	case protocol.MsgHello:
	case protocol.MsgCommand:
		// Clients before protocol 0x07 send the command first. Decoding it
		// reports the version mismatch in the same words as always.
		if _, err := protocol.DecodeCommandMessage(msg.Payload); err != nil {
			return reject("invalid command: %v", err)
		}
		return reject("expected hello message (0x%02x), got command", protocol.MsgHello)
	default:
		return reject("expected hello message (0x%02x), got 0x%02x", protocol.MsgHello, msg.Type)
	}
	hello, err := protocol.DecodeHelloMessage(msg.Payload)
	if err != nil {
		return reject("invalid hello: %v", err)
	}

	var challenge protocol.ChallengeMessage
	if _, err := rand.Read(challenge.Nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	if err := protocol.WriteMessageTo(conn, protocol.MsgChallenge, challenge.Encode()); err != nil {
		return nil, fmt.Errorf("sending challenge: %w", err)
	}

	msg, err = read("command")
	if err != nil {
		return nil, err
	}
	if msg.Type != protocol.MsgCommand {
		return reject("expected command message (0x%02x), got 0x%02x", protocol.MsgCommand, msg.Type)
	}
	cmd, err := protocol.DecodeCommandMessage(msg.Payload)
	if err != nil {
		return reject("invalid command: %v", err)
	}
	if cmd.Nonce != challenge.Nonce ||
		!auth.Verify(secret, protocol.CurrentVersion, hello.Nonce, challenge.Nonce, cmd.Signature, cmd.Program, cmd.Args) {
		return reject("authentication failed")
	}

	// Only now, to a client that has shown it knows the secret, prove that
	// the server knows it too.
	accepted := protocol.AcceptedMessage{
		Proof: auth.ServerProof(secret, protocol.CurrentVersion, hello.Nonce, challenge.Nonce),
	}
	if err := protocol.WriteMessageTo(conn, protocol.MsgAccepted, accepted.Encode()); err != nil {
		return nil, fmt.Errorf("sending accepted: %w", err)
	}
	return cmd, nil
}
