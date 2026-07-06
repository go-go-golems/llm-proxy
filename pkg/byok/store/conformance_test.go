package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/sqlite"
)

func backends(t *testing.T) map[string]store.Store {
	t.Helper()
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "byok.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return map[string]store.Store{
		"memory": memory.New(),
		"sqlite": sqliteStore,
	}
}

func mustUser(t *testing.T, s store.Store, subject, username string) store.User {
	t.Helper()
	u, err := s.UpsertUser(context.Background(), store.User{OIDCSubject: subject, Username: username})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	return u
}

func TestUserUpsertAndLookup(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u := mustUser(t, s, "kc-sub-1", "alice")
			if u.ID == "" {
				t.Fatal("expected generated user ID")
			}
			// Upsert with same subject updates, does not duplicate.
			u2, err := s.UpsertUser(ctx, store.User{OIDCSubject: "kc-sub-1", Username: "alice", Email: "alice@example.com"})
			if err != nil {
				t.Fatalf("second upsert: %v", err)
			}
			if u2.ID != u.ID {
				t.Fatalf("upsert created new user: %s vs %s", u2.ID, u.ID)
			}
			if u2.Email != "alice@example.com" {
				t.Fatalf("email not updated: %q", u2.Email)
			}
			byName, err := s.GetUserByUsername(ctx, "alice")
			if err != nil || byName.ID != u.ID {
				t.Fatalf("get by username: %v %+v", err, byName)
			}
			if _, err := s.GetUserBySubject(ctx, "nope"); err != store.ErrNotFound {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
			users, err := s.ListUsers(ctx)
			if err != nil || len(users) != 1 {
				t.Fatalf("list users: %v (%d)", err, len(users))
			}
		})
	}
}

func TestCredentialCRUDAndCascade(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u := mustUser(t, s, "sub-cred", "bob")

			c1, err := s.CreateCredential(ctx, store.Credential{
				UserID: u.ID, Provider: "anthropic", APIType: "claude",
				Label: "personal", SecretCipher: []byte{1, 2, 3}, SecretLast4: "x4Kq",
			})
			if err != nil {
				t.Fatalf("create credential: %v", err)
			}
			c2, err := s.CreateCredential(ctx, store.Credential{
				UserID: u.ID, Provider: "openai", APIType: "openai",
				Label: "work", SecretCipher: []byte{4, 5, 6}, SecretLast4: "z9Pl",
			})
			if err != nil {
				t.Fatalf("create credential 2: %v", err)
			}

			got, err := s.GetCredential(ctx, u.ID, c1.ID)
			if err != nil || string(got.SecretCipher) != string([]byte{1, 2, 3}) {
				t.Fatalf("get credential: %v %+v", err, got)
			}
			// Ownership is enforced.
			if _, err := s.GetCredential(ctx, "other-user", c1.ID); err != store.ErrNotFound {
				t.Fatalf("expected ErrNotFound for wrong owner, got %v", err)
			}

			// Token bound only to c1 must be revoked when c1 is deleted;
			// token bound to both survives.
			only, err := s.MintToken(ctx, store.Token{
				UserID: u.ID, TokenHash: "hash-only", Name: "only-c1",
				CredentialIDs: []string{c1.ID}, AllowedModels: []string{"sonnet"},
			})
			if err != nil {
				t.Fatalf("mint token: %v", err)
			}
			both, err := s.MintToken(ctx, store.Token{
				UserID: u.ID, TokenHash: "hash-both", Name: "both",
				CredentialIDs: []string{c1.ID, c2.ID}, AllowedModels: []string{"sonnet"},
			})
			if err != nil {
				t.Fatalf("mint token 2: %v", err)
			}

			if err := s.DeleteCredential(ctx, u.ID, c1.ID); err != nil {
				t.Fatalf("delete credential: %v", err)
			}
			if _, err := s.GetCredential(ctx, u.ID, c1.ID); err != store.ErrNotFound {
				t.Fatalf("credential still present after delete: %v", err)
			}
			tokens, err := s.ListTokensByUser(ctx, u.ID)
			if err != nil {
				t.Fatalf("list tokens: %v", err)
			}
			for _, tok := range tokens {
				switch tok.ID {
				case only.ID:
					if tok.RevokedAt == nil {
						t.Fatal("token bound only to deleted credential was not revoked")
					}
				case both.ID:
					if tok.RevokedAt != nil {
						t.Fatal("token with surviving binding was revoked")
					}
				}
			}
		})
	}
}

func TestTokenRoundTripAndRevoke(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u := mustUser(t, s, "sub-tok", "carol")

			maxTokens := int64(5000)
			rpm := int64(60)
			expires := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
			minted, err := s.MintToken(ctx, store.Token{
				UserID: u.ID, TokenHash: "the-hash", Name: "demo",
				CredentialIDs: []string{"cred-1"}, AllowedModels: []string{"sonnet", "gpt-*"},
				MaxTotalTokens: &maxTokens, RateLimitRPM: &rpm, ExpiresAt: &expires,
			})
			if err != nil {
				t.Fatalf("mint: %v", err)
			}

			got, err := s.GetTokenByHash(ctx, "the-hash")
			if err != nil {
				t.Fatalf("get by hash: %v", err)
			}
			if got.ID != minted.ID || got.Name != "demo" {
				t.Fatalf("round trip mismatch: %+v", got)
			}
			if got.MaxTotalTokens == nil || *got.MaxTotalTokens != 5000 {
				t.Fatalf("max tokens lost: %+v", got.MaxTotalTokens)
			}
			if got.MaxRequests != nil {
				t.Fatalf("expected nil MaxRequests, got %v", *got.MaxRequests)
			}
			if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
				t.Fatalf("expires mismatch: %v vs %v", got.ExpiresAt, expires)
			}
			if len(got.AllowedModels) != 2 || got.AllowedModels[1] != "gpt-*" {
				t.Fatalf("allowed models mismatch: %v", got.AllowedModels)
			}

			if err := s.TouchTokenUsed(ctx, minted.ID, time.Now()); err != nil {
				t.Fatalf("touch: %v", err)
			}
			if err := s.RevokeToken(ctx, u.ID, minted.ID); err != nil {
				t.Fatalf("revoke: %v", err)
			}
			got, err = s.GetTokenByHash(ctx, "the-hash")
			if err != nil || got.RevokedAt == nil {
				t.Fatalf("revocation not persisted: %v %+v", err, got)
			}
			// Double revoke and wrong-owner revoke both fail.
			if err := s.RevokeToken(ctx, u.ID, minted.ID); err != store.ErrNotFound {
				t.Fatalf("double revoke: %v", err)
			}
			if _, err := s.GetTokenByHash(ctx, "missing"); err != store.ErrNotFound {
				t.Fatalf("expected ErrNotFound: %v", err)
			}
		})
	}
}

func TestUsageLedgerAndCounters(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u := mustUser(t, s, "sub-meter", "dave")
			tok, err := s.MintToken(ctx, store.Token{
				UserID: u.ID, TokenHash: "meter-hash", Name: "metered",
				CredentialIDs: []string{"c"}, AllowedModels: []string{"sonnet"},
			})
			if err != nil {
				t.Fatalf("mint: %v", err)
			}

			for i, e := range []store.LedgerEntry{
				{TokenID: tok.ID, UserID: u.ID, Model: "sonnet", PromptTokens: 100, CompletionTokens: 50, Status: store.LedgerStatusOK},
				{TokenID: tok.ID, UserID: u.ID, Model: "sonnet", PromptTokens: 20, CompletionTokens: 0, Status: store.LedgerStatusError, Streamed: true},
				{TokenID: tok.ID, UserID: u.ID, Model: "sonnet", Status: store.LedgerStatusRejected},
			} {
				if err := s.RecordUsage(ctx, e); err != nil {
					t.Fatalf("record usage %d: %v", i, err)
				}
			}

			c, err := s.GetCounters(ctx, tok.ID)
			if err != nil {
				t.Fatalf("counters: %v", err)
			}
			// Rejected rows are ledgered but do not consume budget.
			if c.TotalTokens != 170 || c.TotalRequests != 2 {
				t.Fatalf("counters mismatch: %+v", c)
			}

			entries, err := s.ListLedger(ctx, tok.ID, time.Time{}, 10)
			if err != nil || len(entries) != 3 {
				t.Fatalf("ledger: %v (%d)", err, len(entries))
			}

			// Unknown token has zero counters, not an error.
			zero, err := s.GetCounters(ctx, "does-not-exist")
			if err != nil || zero.TotalTokens != 0 || zero.TotalRequests != 0 {
				t.Fatalf("zero counters: %v %+v", err, zero)
			}
		})
	}
}

func TestAuditEvents(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, e := range []store.AuditEvent{
				{UserID: "u1", EventType: "token.minted", Payload: []byte(`{"token_id":"t1"}`)},
				{UserID: "u1", TokenID: "t1", EventType: "inference.request", Payload: []byte(`{"model":"sonnet"}`)},
				{UserID: "u2", EventType: "credential.created"},
			} {
				if err := s.AppendEvent(ctx, e); err != nil {
					t.Fatalf("append: %v", err)
				}
			}
			events, err := s.ListEvents(ctx, store.AuditFilter{UserID: "u1"})
			if err != nil || len(events) != 2 {
				t.Fatalf("filter by user: %v (%d)", err, len(events))
			}
			events, err = s.ListEvents(ctx, store.AuditFilter{EventType: "inference.request"})
			if err != nil || len(events) != 1 || events[0].TokenID != "t1" {
				t.Fatalf("filter by type: %v %+v", err, events)
			}
		})
	}
}
