package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunServerRejectsBYOKWithoutProfiles(t *testing.T) {
	err := runServer(context.Background(), &ServeSettings{ByokDB: t.TempDir() + "/byok.sqlite"})
	if err == nil {
		t.Fatal("expected BYOK without profiles to be rejected")
	}
	if !strings.Contains(err.Error(), "--byok-db requires --profiles") {
		t.Fatalf("unexpected error: %v", err)
	}
}
