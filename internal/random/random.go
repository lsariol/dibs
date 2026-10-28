// Package random gives unpredictable values from crypto/rand: lobby codes,
// session tokens, IDs and the numbers behind shuffles.
package random

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
)

// CodeAlphabet has no look-alike characters (0/O, 1/I/L), so codes are easy to read aloud.
const CodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// Code returns n characters from CodeAlphabet.
func Code(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = CodeAlphabet[Intn(len(CodeAlphabet))]
	}
	return string(b)
}

// Token returns 32 random bytes as URL-safe base64: a session credential.
func Token() string {
	b := make([]byte, 32)
	fill(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ID returns a short opaque identifier with the given prefix, e.g. "p_k3J9…".
func ID(prefix string) string {
	b := make([]byte, 9)
	fill(b)
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Intn returns a uniform number in [0, n); it panics if n <= 0.
func Intn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("random: crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}

func fill(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("random: crypto/rand failed: " + err.Error())
	}
}
