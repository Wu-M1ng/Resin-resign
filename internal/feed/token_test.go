package feed

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	plain, hash, prefix, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil {
		t.Fatalf("token is not raw URL-safe base64: %v", err)
	}
	if len(decoded) != tokenBytes {
		t.Fatalf("decoded token length = %d, want %d", len(decoded), tokenBytes)
	}
	if hash != HashToken(plain) {
		t.Fatalf("hash does not match HashToken")
	}
	if prefix == "" || !strings.HasPrefix(plain, prefix) || len(prefix) > 8 {
		t.Fatalf("unexpected prefix %q for token %q", prefix, plain)
	}
	plain2, hash2, _, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if plain == plain2 || hash == hash2 {
		t.Fatal("two generated tokens unexpectedly matched")
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	const token = "a-token"
	if got, want := HashToken(token), HashToken(token); got != want {
		t.Fatalf("hash changed: got %q want %q", got, want)
	}
	if HashToken(token) == token {
		t.Fatal("hash must not equal plaintext token")
	}
}
