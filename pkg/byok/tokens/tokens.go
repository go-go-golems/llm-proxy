// Package tokens mints and hashes BYOK bearer tokens. The plaintext is
// visible exactly once at mint time; only its SHA-256 hex digest is stored,
// so validation is a hash-then-index-lookup and never a secret comparison.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/pkg/errors"
)

// Prefix makes minted tokens recognizable in logs and secret scanners.
const Prefix = "llmp_"

// Mint returns a fresh plaintext token and its storage hash.
func Mint() (string, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", errors.Wrap(err, "generate token")
	}
	raw := Prefix + base64.RawURLEncoding.EncodeToString(b[:])
	return raw, Hash(raw), nil
}

// Hash returns the hex SHA-256 digest of a plaintext token.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// LooksLikeToken reports whether s has the minted-token shape. Used to give
// clearer errors when a caller pastes a provider key instead of a BYOK token.
func LooksLikeToken(s string) bool {
	return strings.HasPrefix(s, Prefix) && len(s) > len(Prefix)
}
