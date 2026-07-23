package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("  from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveSecretFile("", path, "--secret")
	if err != nil || got != "from-file" {
		t.Fatalf("resolve file = %q, %v", got, err)
	}
	if _, err := resolveSecretFile("inline", path, "--secret"); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("inline plus file error = %v", err)
	}
	if _, err := resolveSecretFile("", t.TempDir(), "--secret"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
	if _, err := resolveSecretFile("", filepath.Join(t.TempDir(), "missing"), "--secret"); err == nil || !strings.Contains(err.Error(), "read --secret file") {
		t.Fatalf("missing file error = %v", err)
	}
	symlink := filepath.Join(t.TempDir(), "symlink")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSecretFile("", symlink, "--secret"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
	large := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(large, make([]byte, maxDeploymentSecretFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSecretFile("", large, "--secret"); err == nil || !strings.Contains(err.Error(), "large") {
		t.Fatalf("large error = %v", err)
	}
}

func TestRunServerRejectsBYOKWithoutProfiles(t *testing.T) {
	err := runServer(context.Background(), &ServeSettings{ByokDB: t.TempDir() + "/byok.sqlite"})
	if err == nil {
		t.Fatal("expected BYOK without profiles to be rejected")
	}
	if !strings.Contains(err.Error(), "--byok-db requires --profiles") {
		t.Fatalf("unexpected error: %v", err)
	}
}
