package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Sign computes the HMAC-SHA256 signature for a command payload.
// The signature covers: version + nonce + program + argc + [len + arg]...
// Args are length-prefixed to avoid null-byte ambiguity.
func Sign(secret string, version uint8, nonce [protocol.NonceLength]byte, program uint8, args []string) [protocol.HMACLength]byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte{version})
	mac.Write(nonce[:])
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

// Verify checks the HMAC-SHA256 signature against the expected value.
func Verify(secret string, version uint8, nonce [protocol.NonceLength]byte, signature [protocol.HMACLength]byte, program uint8, args []string) bool {
	expected := Sign(secret, version, nonce, program, args)
	return hmac.Equal(signature[:], expected[:])
}
