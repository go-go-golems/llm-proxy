package vault_test

import (
	"strings"
	"testing"

	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	key, err := vault.GenerateKeyBase64()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	v, err := vault.NewFromBase64(key)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return v
}

func TestRoundTrip(t *testing.T) {
	v := newVault(t)
	secret := []byte("sk-ant-api03-verysecret")
	blob, err := v.Encrypt("cred-1", secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if strings.Contains(string(blob), "verysecret") {
		t.Fatal("plaintext visible in cipher blob")
	}
	got, err := v.Decrypt("cred-1", blob)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("decrypt: %v %q", err, got)
	}
}

func TestAADBindsCredentialID(t *testing.T) {
	v := newVault(t)
	blob, err := v.Encrypt("cred-1", []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := v.Decrypt("cred-2", blob); err == nil {
		t.Fatal("blob decrypted under a different credential ID")
	}
}

func TestTamperedBlobFails(t *testing.T) {
	v := newVault(t)
	blob, err := v.Encrypt("cred-1", []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	blob[len(blob)-1] ^= 0xFF
	if _, err := v.Decrypt("cred-1", blob); err == nil {
		t.Fatal("tampered blob decrypted")
	}
}

func TestWrongKeyFails(t *testing.T) {
	v1, v2 := newVault(t), newVault(t)
	blob, err := v1.Encrypt("cred-1", []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := v2.Decrypt("cred-1", blob); err == nil {
		t.Fatal("blob decrypted with a different master key")
	}
}

func TestLast4(t *testing.T) {
	if got := vault.Last4("sk-ant-abcdef"); got != "…cdef" {
		t.Fatalf("last4: %q", got)
	}
	if got := vault.Last4("ab"); got != "…ab" {
		t.Fatalf("short last4: %q", got)
	}
}
