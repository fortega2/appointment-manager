// Package token mints the opaque bearer tokens the session cookie and the
// password reset link carry, and derives the digest each of them is stored as.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrZeroBytes is returned when a token of no length is asked for.
var ErrZeroBytes = errors.New("bytes must be greater than zero")

// Generate returns size bytes of crypto/rand, base64-URL encoded so the result
// is safe to put in a cookie or a query string.
func Generate(size uint) (string, error) {
	if size == 0 {
		return "", fmt.Errorf("generate token: %w", ErrZeroBytes)
	}

	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

// Hash derives the value a token is stored as, so a leaked dump cannot be
// replayed. Unsalted SHA-256 is the right primitive here and a password KDF
// would be the wrong one: see ADR 0006.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
