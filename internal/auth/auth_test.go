package auth

import (
	"strings"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

var testServerNonce = [protocol.NonceLength]byte{0xA0, 0xA1, 0xA2, 0xA3}

func TestSignAndVerify(t *testing.T) {
	secret := "test-secret-key"
	version := protocol.CurrentVersion
	nonce := [protocol.NonceLength]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	program := protocol.ProgramFFmpeg
	args := []string{"-i", "/media/input.mkv", "-c:v", "h264_nvenc", "output.mp4"}

	sig := Sign(secret, version, nonce, testServerNonce, program, args)

	if !Verify(secret, version, nonce, testServerNonce, sig, program, args) {
		t.Fatal("Verify should succeed with correct signature")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	nonce := [protocol.NonceLength]byte{1, 2, 3}
	args := []string{"-version"}

	sig := Sign("correct-secret", protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if Verify("wrong-secret", protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should fail with wrong secret")
	}
}

func TestVerifyWrongArgs(t *testing.T) {
	secret := "my-secret"
	nonce := [protocol.NonceLength]byte{5, 6, 7}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"-i", "input.mkv"})

	if Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, []string{"-i", "different.mkv"}) {
		t.Fatal("Verify should fail with different args")
	}
}

func TestVerifyWrongProgram(t *testing.T) {
	secret := "my-secret"
	nonce := [protocol.NonceLength]byte{}
	args := []string{"-version"}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFprobe, args) {
		t.Fatal("Verify should fail with different program")
	}
}

func TestVerifyWrongVersion(t *testing.T) {
	secret := "my-secret"
	nonce := [protocol.NonceLength]byte{}
	args := []string{"-version"}

	sig := Sign(secret, 0x05, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if Verify(secret, 0x06, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should fail with different version")
	}
}

func TestVerifyWrongNonce(t *testing.T) {
	secret := "my-secret"
	args := []string{"-version"}

	nonce1 := [protocol.NonceLength]byte{1, 2, 3}
	nonce2 := [protocol.NonceLength]byte{4, 5, 6}

	sig := Sign(secret, protocol.CurrentVersion, nonce1, testServerNonce, protocol.ProgramFFmpeg, args)

	if Verify(secret, protocol.CurrentVersion, nonce2, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should fail with different nonce")
	}
}

func TestSignDeterministic(t *testing.T) {
	secret := "deterministic"
	nonce := [protocol.NonceLength]byte{42}
	args := []string{"a", "b", "c"}

	sig1 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)
	sig2 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if sig1 != sig2 {
		t.Fatal("Sign should be deterministic")
	}
}

func TestSignEmptyArgs(t *testing.T) {
	secret := "test"
	nonce := [protocol.NonceLength]byte{}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{})

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, []string{}) {
		t.Fatal("should work with empty args")
	}
}

func TestSignManyArgs(t *testing.T) {
	secret := "many-args-secret"
	nonce := [protocol.NonceLength]byte{9, 8, 7}
	args := make([]string, 100)
	for i := range args {
		args[i] = strings.Repeat("x", i+1)
	}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed with 100 args")
	}
}

func TestSignLongArgs(t *testing.T) {
	secret := "long-arg-secret"
	nonce := [protocol.NonceLength]byte{1}
	longArg := strings.Repeat("A", 10*1024) // 10KB
	args := []string{longArg}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed with 10KB arg")
	}
}

func TestSignEmptySecret(t *testing.T) {
	secret := ""
	nonce := [protocol.NonceLength]byte{1, 2, 3}
	args := []string{"-version"}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed with empty secret")
	}
}

func TestVerifyDifferentArgCount(t *testing.T) {
	secret := "test-secret"
	nonce := [protocol.NonceLength]byte{10}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"a", "b", "c"})

	if Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, []string{"a", "b"}) {
		t.Fatal("Verify should fail when arg count differs (3 signed, 2 verified)")
	}
}

func TestVerifyExtraArg(t *testing.T) {
	secret := "test-secret"
	nonce := [protocol.NonceLength]byte{11}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"a", "b"})

	if Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, []string{"a", "b", "c"}) {
		t.Fatal("Verify should fail when extra arg added (2 signed, 3 verified)")
	}
}

func TestSignSpecialCharArgs(t *testing.T) {
	secret := "special-chars"
	nonce := [protocol.NonceLength]byte{20}
	args := []string{
		"unicode-\u00e9\u00e8\u00ea",
		"null-byte-\x00-middle",
		"newline-\n-tab-\t-cr-\r",
		"\U0001f600\U0001f525\U0001f4a5", // emoji
	}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed with special character args")
	}
}

func TestVerifyZeroNonce(t *testing.T) {
	secret := "zero-nonce-secret"
	nonce := [protocol.NonceLength]byte{} // all zeros
	args := []string{"-i", "input.mp4"}

	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed with all-zero nonce")
	}
}

func TestSignNullByteArgDistinct(t *testing.T) {
	// With length-prefixed signing, args with null bytes produce different
	// signatures than the split equivalent.
	secret := "null-test"
	nonce := [protocol.NonceLength]byte{1}

	sig1 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"a\x00b"})
	sig2 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"a", "b"})

	if sig1 == sig2 {
		t.Fatal("args with embedded null byte must produce different signature from split args")
	}
}

func TestSignArgCountMatters(t *testing.T) {
	// With length-prefixed signing, ["ab"] and ["a","b"] produce different
	// signatures even though the concatenated content is the same.
	secret := "count-test"
	nonce := [protocol.NonceLength]byte{2}

	sig1 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"ab"})
	sig2 := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, []string{"a", "b"})

	if sig1 == sig2 {
		t.Fatal("different arg counts must produce different signatures")
	}
}

func TestSignAllProgramTypes(t *testing.T) {
	secret := "program-types"
	nonce := [protocol.NonceLength]byte{30}
	args := []string{"-version"}

	sigFFmpeg := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)
	sigFFprobe := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFprobe, args)

	if sigFFmpeg == sigFFprobe {
		t.Fatal("Signatures for ProgramFFmpeg and ProgramFFprobe should differ")
	}

	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sigFFmpeg, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should succeed for ProgramFFmpeg")
	}
	if !Verify(secret, protocol.CurrentVersion, nonce, testServerNonce, sigFFprobe, protocol.ProgramFFprobe, args) {
		t.Fatal("Verify should succeed for ProgramFFprobe")
	}
}

func TestVerifyWrongServerNonce(t *testing.T) {
	// A command signed for one connection's server nonce is rejected on any
	// other connection: replaying a captured command fails.
	secret := "test-secret"
	nonce := [protocol.NonceLength]byte{1}
	args := []string{"-version"}
	sig := Sign(secret, protocol.CurrentVersion, nonce, testServerNonce, protocol.ProgramFFmpeg, args)
	other := testServerNonce
	other[15] ^= 1
	if Verify(secret, protocol.CurrentVersion, nonce, other, sig, protocol.ProgramFFmpeg, args) {
		t.Fatal("Verify should fail for a different server nonce")
	}
}

func TestServerProof(t *testing.T) {
	cn := [protocol.NonceLength]byte{1, 2, 3}
	proof := ServerProof("s", protocol.CurrentVersion, cn, testServerNonce)
	if !VerifyServerProof("s", protocol.CurrentVersion, cn, testServerNonce, proof) {
		t.Fatal("VerifyServerProof should succeed")
	}
	if VerifyServerProof("other", protocol.CurrentVersion, cn, testServerNonce, proof) {
		t.Fatal("a server without the secret must fail")
	}
	cn2 := cn
	cn2[0] ^= 1
	if VerifyServerProof("s", protocol.CurrentVersion, cn2, testServerNonce, proof) {
		t.Fatal("a proof for another client nonce must fail")
	}
}

func TestServerProofIsNotACommandSignature(t *testing.T) {
	// The server computes proofs over nonces the client picks. Those must
	// never verify as a command signature, whatever the client picked.
	cn := [protocol.NonceLength]byte{9}
	proof := ServerProof("s", protocol.CurrentVersion, cn, testServerNonce)
	if Verify("s", protocol.CurrentVersion, cn, testServerNonce, proof, protocol.ProgramFFmpeg, nil) {
		t.Fatal("a server proof verified as a command signature")
	}
}
