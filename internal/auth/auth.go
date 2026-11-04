package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Both sides prove they know the shared secret, bound to two fresh nonces:
// the client's (sent in MsgHello) and the server's (sent in MsgChallenge).
// The client proves it first (the command signature); the server's proof
// follows in MsgAccepted.
// The labels keep the server's proof and the client's command signature
// from ever being valid for each other.
const (
	commandLabel     = "ffoip-command"
	serverProofLabel = "ffoip-server-proof"
)

// Sign computes the HMAC-SHA256 signature for a command. It covers the
// label, version, both nonces, program, argc and each length-prefixed arg
// (length prefixes avoid null-byte ambiguity). The server's nonce is new on
// every connection, so a captured command can't be replayed.
func Sign(secret string, version uint8, clientNonce, serverNonce [protocol.NonceLength]byte, program uint8, args []string) [protocol.HMACLength]byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(commandLabel))
	mac.Write([]byte{version})
	mac.Write(clientNonce[:])
	mac.Write(serverNonce[:])
	mac.Write([]byte{program})
	var countBuf [2]byte
	binary.BigEndian.PutUint16(countBuf[:], uint16(len(args)))
	mac.Write(countBuf[:])
	for _, arg := range args {
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(arg)))
		mac.Write(lenBuf[:])
		mac.Write([]byte(arg))
	}

	var sig [protocol.HMACLength]byte
	copy(sig[:], mac.Sum(nil))
	return sig
}

// Verify checks a command signature in constant time.
func Verify(secret string, version uint8, clientNonce, serverNonce [protocol.NonceLength]byte, signature [protocol.HMACLength]byte, program uint8, args []string) bool {
	expected := Sign(secret, version, clientNonce, serverNonce, program, args)
	return hmac.Equal(signature[:], expected[:])
}

// ServerProof is what the server sends in MsgAccepted, after verifying the
// command, to show the client it knows the secret before the client acts on
// any request from it.
func ServerProof(secret string, version uint8, clientNonce, serverNonce [protocol.NonceLength]byte) [protocol.HMACLength]byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(serverProofLabel))
	mac.Write([]byte{version})
	mac.Write(clientNonce[:])
	mac.Write(serverNonce[:])
	var proof [protocol.HMACLength]byte
	copy(proof[:], mac.Sum(nil))
	return proof
}

// VerifyServerProof checks a server proof in constant time.
func VerifyServerProof(secret string, version uint8, clientNonce, serverNonce [protocol.NonceLength]byte, proof [protocol.HMACLength]byte) bool {
	expected := ServerProof(secret, version, clientNonce, serverNonce)
	return hmac.Equal(proof[:], expected[:])
}
