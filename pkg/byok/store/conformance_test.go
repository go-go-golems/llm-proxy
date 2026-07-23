package store_test

import (
	"context"
	"encoding/json"
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
	u, err := s.UpsertUser(context.Background(), store.User{OIDCIssuer: "urn:test", OIDCSubject: subject, Username: username})
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
			u2, err := s.UpsertUser(ctx, store.User{OIDCIssuer: "urn:test", OIDCSubject: "kc-sub-1", Username: "alice", Email: "alice@example.com"})
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

func TestIssuerAwareIdentityIsolation(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			first, err := s.UpsertUser(ctx, store.User{OIDCIssuer: "https://issuer-a.example", OIDCSubject: "shared", Username: "alice-a"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.UpsertUser(ctx, store.User{OIDCIssuer: "https://issuer-b.example", OIDCSubject: "shared", Username: "alice-b"})
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == second.ID {
				t.Fatal("same subject under distinct issuers collapsed into one user")
			}
			got, err := s.GetUserByIdentity(ctx, "https://issuer-b.example", "shared")
			if err != nil || got.ID != second.ID {
				t.Fatalf("issuer-aware lookup = %+v, %v", got, err)
			}
		})
	}
}

func TestAuthTransactionAndSessionLifecycle(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			tx := store.AuthTransaction{IDHash: "browser-hash", StateHash: "state-hash", Nonce: "nonce", PKCEVerifier: "verifier", ReturnTo: "/app", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
			if err := s.CreateAuthTransaction(ctx, tx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ConsumeAuthTransaction(ctx, tx.IDHash, "wrong", now); err != store.ErrNotFound {
				t.Fatalf("wrong state error = %v", err)
			}
			consumed, err := s.ConsumeAuthTransaction(ctx, tx.IDHash, tx.StateHash, now)
			if err != nil || consumed.PKCEVerifier != "verifier" || consumed.ConsumedAt == nil {
				t.Fatalf("consume = %+v, %v", consumed, err)
			}
			if _, err := s.ConsumeAuthTransaction(ctx, tx.IDHash, tx.StateHash, now); err != store.ErrNotFound {
				t.Fatalf("replay error = %v", err)
			}

			expired := tx
			expired.IDHash = "expired-browser"
			expired.StateHash = "expired-state"
			expired.ExpiresAt = now
			if err := s.CreateAuthTransaction(ctx, expired); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ConsumeAuthTransaction(ctx, expired.IDHash, expired.StateHash, now); err != store.ErrNotFound {
				t.Fatalf("expired transaction error = %v", err)
			}

			user := mustUser(t, s, "session-subject", "session-user")
			session := store.Session{ID: "session-public", IDHash: "session-hash", UserID: user.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
			if err := s.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			used, err := s.UseSession(ctx, session.IDHash, now.Add(time.Minute), now.Add(-time.Minute))
			if err != nil || !used.LastSeenAt.Equal(now.Add(time.Minute)) {
				t.Fatalf("use session = %+v, %v", used, err)
			}
			listed, err := s.ListSessionsByUser(ctx, user.ID)
			if err != nil || len(listed) != 1 || listed[0].ID != session.ID {
				t.Fatalf("list sessions = %+v, %v", listed, err)
			}
			if err := s.RevokeSessionByID(ctx, user.ID, session.ID, now.Add(2*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.UseSession(ctx, session.IDHash, now.Add(3*time.Minute), now); err != store.ErrNotFound {
				t.Fatalf("revoked session error = %v", err)
			}

			idle := session
			idle.ID = "idle-session-public"
			idle.IDHash = "idle-session"
			idle.LastSeenAt = now.Add(-time.Hour)
			if err := s.CreateSession(ctx, idle); err != nil {
				t.Fatal(err)
			}
			if _, err := s.UseSession(ctx, idle.IDHash, now, now.Add(-30*time.Minute)); err != store.ErrNotFound {
				t.Fatalf("idle session error = %v", err)
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

func TestAgentGrantLifecycleCumulativeCountersAndCascades(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			user := mustUser(t, s, "grant-owner", "grant-owner")
			other := mustUser(t, s, "grant-other", "grant-other")
			credential, err := s.CreateCredential(ctx, store.Credential{UserID: user.ID, Provider: "openai", APIType: "openai", Label: "agent", SecretCipher: []byte{1}, SecretLast4: "safe"})
			if err != nil {
				t.Fatal(err)
			}
			perToken := int64(100)
			grantMax := int64(1000)
			grant, err := s.CreateAgentGrantAudited(ctx, store.AgentGrant{
				UserID: user.ID, Name: "laptop", CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model-a"},
				PerTokenMaxTokens: &perToken, TokenTTL: time.Hour, MaxActivePerInstance: 1, GrantMaxTokens: &grantMax,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetAgentGrant(ctx, other.ID, grant.ID); err != store.ErrNotFound {
				t.Fatalf("cross-owner grant lookup = %v", err)
			}
			token, err := s.MintToken(ctx, store.Token{UserID: user.ID, TokenHash: "grant-token-1", Name: "device", CredentialIDs: grant.CredentialIDs, AllowedModels: grant.AllowedModels, AgentGrantID: grant.ID, IssueChannel: store.IssueChannelDevice})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RecordUsage(ctx, store.LedgerEntry{TokenID: token.ID, UserID: user.ID, Model: "model-a", PromptTokens: 30, CompletionTokens: 20, Status: store.LedgerStatusOK}); err != nil {
				t.Fatal(err)
			}
			counters, err := s.GetAgentGrantCounters(ctx, grant.ID)
			if err != nil || counters.TotalTokens != 50 || counters.TotalRequests != 1 {
				t.Fatalf("grant counters = %+v, %v", counters, err)
			}

			grant.Name = "laptop-tightened"
			grant.PerTokenMaxTokens = int64Ptr(80)
			if _, err := s.UpdateAgentGrantAudited(ctx, grant); err != nil {
				t.Fatal(err)
			}
			storedToken, err := s.GetTokenByHash(ctx, token.TokenHash)
			if err != nil || storedToken.RevokedAt == nil {
				t.Fatalf("policy update did not revoke child token: %+v, %v", storedToken, err)
			}
			counters, _ = s.GetAgentGrantCounters(ctx, grant.ID)
			if counters.TotalTokens != 50 {
				t.Fatalf("policy update reset cumulative counters: %+v", counters)
			}

			issued1, err := s.IssueAgentTokenAudited(ctx, store.Token{UserID: user.ID, TokenHash: "issued-hash-1", AgentGrantID: grant.ID, IssueChannel: store.IssueChannelDevice, SourceClientID: "agent-client", ClientInstanceID: "0123456789abcdef"})
			if err != nil {
				t.Fatal(err)
			}
			issued2, err := s.IssueAgentTokenAudited(ctx, store.Token{UserID: user.ID, TokenHash: "issued-hash-2", AgentGrantID: grant.ID, IssueChannel: store.IssueChannelDevice, SourceClientID: "agent-client", ClientInstanceID: "0123456789abcdef"})
			if err != nil {
				t.Fatal(err)
			}
			rotated, err := s.GetTokenByHash(ctx, issued1.TokenHash)
			if err != nil || rotated.RevokedAt == nil {
				t.Fatalf("prior issued token not rotated: %+v, %v", rotated, err)
			}
			if issued2.CredentialIDs[0] != credential.ID || issued2.AllowedModels[0] != "model-a" || issued2.ExpiresAt == nil {
				t.Fatalf("issued policy not derived from grant: %+v", issued2)
			}
			if err := s.DeleteCredentialAudited(ctx, user.ID, credential.ID); err != nil {
				t.Fatal(err)
			}
			storedGrant, err := s.GetAgentGrant(ctx, user.ID, grant.ID)
			if err != nil || storedGrant.RevokedAt == nil || storedGrant.Enabled {
				t.Fatalf("credential cascade grant = %+v, %v", storedGrant, err)
			}
			storedToken, err = s.GetTokenByHash(ctx, issued2.TokenHash)
			if err != nil || storedToken.RevokedAt == nil {
				t.Fatalf("credential cascade token = %+v, %v", storedToken, err)
			}
		})
	}
}

func int64Ptr(value int64) *int64 { return &value }

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

func TestAuditedLifecycleMutations(t *testing.T) {
	for name, s := range backends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			u := mustUser(t, s, "sub-audited", "audited-user")
			credential, err := s.CreateCredentialAudited(ctx, store.Credential{
				UserID: u.ID, Provider: "test-provider", APIType: "test-api",
				Label: "audited", SecretCipher: []byte{1, 2, 3}, SecretLast4: "suffix",
			})
			if err != nil {
				t.Fatalf("create audited credential: %v", err)
			}
			token, err := s.MintTokenAudited(ctx, store.Token{
				UserID: u.ID, TokenHash: "audited-token-hash", Name: "audited-token",
				CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"},
			})
			if err != nil {
				t.Fatalf("mint audited token: %v", err)
			}
			if err := s.RevokeTokenAudited(ctx, u.ID, token.ID); err != nil {
				t.Fatalf("revoke audited token: %v", err)
			}
			cascade, err := s.MintTokenAudited(ctx, store.Token{
				UserID: u.ID, TokenHash: "audited-cascade-hash", Name: "audited-cascade",
				CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"},
			})
			if err != nil {
				t.Fatalf("mint cascade token: %v", err)
			}
			if err := s.DeleteCredentialAudited(ctx, u.ID, credential.ID); err != nil {
				t.Fatalf("delete audited credential: %v", err)
			}

			events, err := s.ListEvents(ctx, store.AuditFilter{UserID: u.ID, Limit: 10})
			if err != nil {
				t.Fatalf("list lifecycle audit: %v", err)
			}
			seen := map[string]store.AuditEvent{}
			counts := map[string]int{}
			for _, event := range events {
				seen[event.EventType] = event
				counts[event.EventType]++
				var payload map[string]any
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					t.Fatalf("audit payload for %s: %v", event.EventType, err)
				}
				for _, forbidden := range []string{"secret", "name", "provider", "label"} {
					if _, leaked := payload[forbidden]; leaked {
						t.Fatalf("audit payload for %s contains user-controlled %s field", event.EventType, forbidden)
					}
				}
			}
			for _, eventType := range []string{
				store.AuditCredentialCreated, store.AuditTokenMinted,
				store.AuditTokenRevoked, store.AuditCredentialDeleted,
			} {
				if eventType == store.AuditTokenRevoked && counts[eventType] != 2 {
					t.Errorf("token revoked audit count = %d, want 2 (explicit + credential cascade for %s)", counts[eventType], cascade.ID)
				}
				if _, ok := seen[eventType]; !ok {
					t.Errorf("missing lifecycle audit event %s", eventType)
				}
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
