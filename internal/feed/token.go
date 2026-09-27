package feed

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const tokenBytes = 32

// GenerateToken returns a URL-safe bearer token, its SHA-256 digest, and a
// short display prefix. Only the digest and prefix should be persisted.
func GenerateToken() (plain string, hash string, prefix string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("generate feed token: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	hash = HashToken(plain)
	prefix = plain
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return plain, hash, prefix, nil
}

// HashToken computes the canonical lowercase hexadecimal SHA-256 digest used
// for database lookup. It deliberately does not accept a prefix as a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
