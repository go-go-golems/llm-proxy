// Package vault encrypts provider API keys at rest with AES-256-GCM.
// Each secret is sealed with a fresh nonce and the owning credential ID as
// AAD, so a ciphertext copied onto another credential row fails to decrypt.
// Blob layout: version byte (0x01) ‖ 12-byte nonce ‖ ciphertext+tag.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"

	"github.com/pkg/errors"
)

const blobVersion = 0x01

type Vault struct {
	aead cipher.AEAD
}

// New builds a vault from a 32-byte master key.
func New(masterKey []byte) (*Vault, error) {
	if len(masterKey) != 32 {
		return nil, errors.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, errors.Wrap(err, "init cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "init GCM")
	}
	return &Vault{aead: aead}, nil
}

// NewFromBase64 builds a vault from a base64(std or url)-encoded 32-byte key.
func NewFromBase64(encoded string) (*Vault, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, errors.Wrap(err, "decode master key")
		}
	}
	return New(key)
}

// GenerateKeyBase64 returns a fresh random master key, base64-encoded.
func GenerateKeyBase64() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", errors.Wrap(err, "generate master key")
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

// Encrypt seals plaintext for the given credential ID.
func (v *Vault) Encrypt(credentialID string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.Wrap(err, "generate nonce")
	}
	blob := make([]byte, 0, 1+len(nonce)+len(plaintext)+v.aead.Overhead())
	blob = append(blob, blobVersion)
	blob = append(blob, nonce...)
	return v.aead.Seal(blob, nonce, plaintext, []byte(credentialID)), nil
}

// Decrypt opens a blob sealed by Encrypt for the same credential ID.
func (v *Vault) Decrypt(credentialID string, blob []byte) ([]byte, error) {
	if len(blob) < 1+v.aead.NonceSize() {
		return nil, errors.New("cipher blob too short")
	}
	if blob[0] != blobVersion {
		return nil, errors.Errorf("unsupported cipher blob version %d", blob[0])
	}
	nonce := blob[1 : 1+v.aead.NonceSize()]
	ciphertext := blob[1+v.aead.NonceSize():]
	plaintext, err := v.aead.Open(nil, nonce, ciphertext, []byte(credentialID))
	if err != nil {
		return nil, errors.Wrap(err, "decrypt credential secret")
	}
	return plaintext, nil
}

// Last4 returns a display-only suffix of a secret ("…abcd").
func Last4(secret string) string {
	if len(secret) <= 4 {
		return "…" + secret
	}
	return "…" + secret[len(secret)-4:]
}
