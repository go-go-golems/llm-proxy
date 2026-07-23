package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	byokstore "github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

func lifecycleStore(t *testing.T) (*Store, byokstore.User) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "lifecycle.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user, err := st.UpsertUser(context.Background(), byokstore.User{OIDCIssuer: "urn:test", OIDCSubject: "subject", Username: "user"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return st, user
}

func blockAuditInserts(t *testing.T, st *Store) {
	t.Helper()
	_, err := st.db.Exec(`
CREATE TRIGGER reject_audit_insert
BEFORE INSERT ON audit_events
BEGIN
  SELECT RAISE(ABORT, 'audit delivery blocked for atomicity test');
END;`)
	if err != nil {
		t.Fatalf("create audit rejection trigger: %v", err)
	}
}

func requireAuditFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "append audit event") {
		t.Fatalf("error = %v, want audit append failure", err)
	}
}

func TestAuthAndSessionMutationsRollBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("auth transaction create", func(t *testing.T) {
		st, _ := lifecycleStore(t)
		blockAuditInserts(t, st)
		err := st.CreateAuthTransaction(ctx, byokstore.AuthTransaction{IDHash: "id", StateHash: "state", Nonce: "nonce", PKCEVerifier: "verifier", ReturnTo: "/app", CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
		requireAuditFailure(t, err)
		var count int
		_ = st.db.QueryRow(`SELECT COUNT(*) FROM auth_transactions WHERE id_hash='id'`).Scan(&count)
		if count != 0 {
			t.Fatal("auth transaction survived failed audit")
		}
	})

	t.Run("auth transaction consume", func(t *testing.T) {
		st, _ := lifecycleStore(t)
		transaction := byokstore.AuthTransaction{IDHash: "consume-id", StateHash: "consume-state", Nonce: "nonce", PKCEVerifier: "verifier", ReturnTo: "/app", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
		if err := st.CreateAuthTransaction(ctx, transaction); err != nil {
			t.Fatal(err)
		}
		blockAuditInserts(t, st)
		_, err := st.ConsumeAuthTransaction(ctx, transaction.IDHash, transaction.StateHash, now)
		requireAuditFailure(t, err)
		if _, err := st.db.Exec(`DROP TRIGGER reject_audit_insert`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ConsumeAuthTransaction(ctx, transaction.IDHash, transaction.StateHash, now); err != nil {
			t.Fatalf("consume was not rolled back: %v", err)
		}
	})

	t.Run("session create and revoke", func(t *testing.T) {
		st, user := lifecycleStore(t)
		blockAuditInserts(t, st)
		session := byokstore.Session{ID: "session-public", IDHash: "session", UserID: user.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}
		err := st.CreateSession(ctx, session)
		requireAuditFailure(t, err)
		var count int
		_ = st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id_hash='session'`).Scan(&count)
		if count != 0 {
			t.Fatal("session survived failed create audit")
		}
		if _, err := st.db.Exec(`DROP TRIGGER reject_audit_insert`); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		blockAuditInserts(t, st)
		err = st.RevokeSession(ctx, session.IDHash, now.Add(time.Minute))
		requireAuditFailure(t, err)
		if _, err := st.UseSession(ctx, session.IDHash, now.Add(2*time.Minute), now); err != nil {
			t.Fatalf("revocation survived failed audit: %v", err)
		}
	})
}

func TestAgentGrantMutationsRollBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	newGrant := func(user byokstore.User, credentialID string) byokstore.AgentGrant {
		return byokstore.AgentGrant{UserID: user.ID, Name: "agent", CredentialIDs: []string{credentialID}, AllowedModels: []string{"model"}, TokenTTL: time.Hour, MaxActivePerInstance: 1}
	}

	t.Run("create", func(t *testing.T) {
		st, user := lifecycleStore(t)
		credential, err := st.CreateCredential(ctx, byokstore.Credential{UserID: user.ID, Provider: "provider", APIType: "api", Label: "label", SecretCipher: []byte{1}, SecretLast4: "safe"})
		if err != nil {
			t.Fatal(err)
		}
		blockAuditInserts(t, st)
		grant := newGrant(user, credential.ID)
		grant.ID = "grant-create"
		_, err = st.CreateAgentGrantAudited(ctx, grant)
		requireAuditFailure(t, err)
		if _, err := st.GetAgentGrant(ctx, user.ID, grant.ID); err != byokstore.ErrNotFound {
			t.Fatalf("grant survived failed audit: %v", err)
		}
	})

	t.Run("update and revoke", func(t *testing.T) {
		st, user := lifecycleStore(t)
		credential, err := st.CreateCredential(ctx, byokstore.Credential{UserID: user.ID, Provider: "provider", APIType: "api", Label: "label", SecretCipher: []byte{1}, SecretLast4: "safe"})
		if err != nil {
			t.Fatal(err)
		}
		grant, err := st.CreateAgentGrantAudited(ctx, newGrant(user, credential.ID))
		if err != nil {
			t.Fatal(err)
		}
		token, err := st.MintToken(ctx, byokstore.Token{UserID: user.ID, TokenHash: "grant-rollback-token", Name: "token", CredentialIDs: grant.CredentialIDs, AllowedModels: grant.AllowedModels, AgentGrantID: grant.ID})
		if err != nil {
			t.Fatal(err)
		}
		blockAuditInserts(t, st)
		grant.Name = "changed"
		_, err = st.UpdateAgentGrantAudited(ctx, grant)
		requireAuditFailure(t, err)
		stored, err := st.GetAgentGrant(ctx, user.ID, grant.ID)
		if err != nil || stored.Name != "agent" {
			t.Fatalf("grant update survived failed audit: %+v, %v", stored, err)
		}
		storedToken, err := st.GetTokenByHash(ctx, token.TokenHash)
		if err != nil || storedToken.RevokedAt != nil {
			t.Fatalf("child revocation survived failed audit: %+v, %v", storedToken, err)
		}
		_, err = st.IssueAgentTokenAudited(ctx, byokstore.Token{UserID: user.ID, TokenHash: "failed-issue-hash", AgentGrantID: grant.ID, IssueChannel: byokstore.IssueChannelDevice, SourceClientID: "agent", ClientInstanceID: "0123456789abcdef"})
		requireAuditFailure(t, err)
		if _, err := st.GetTokenByHash(ctx, "failed-issue-hash"); err != byokstore.ErrNotFound {
			t.Fatalf("issued token survived failed audit: %v", err)
		}
		err = st.RevokeAgentGrantAudited(ctx, user.ID, grant.ID)
		requireAuditFailure(t, err)
		stored, _ = st.GetAgentGrant(ctx, user.ID, grant.ID)
		if stored.RevokedAt != nil {
			t.Fatal("grant revocation survived failed audit")
		}
	})
}

func TestAuditedCredentialCreateRollsBackWhenAuditFails(t *testing.T) {
	st, user := lifecycleStore(t)
	blockAuditInserts(t, st)
	credential := byokstore.Credential{
		ID: "credential-create", UserID: user.ID, Provider: "provider", APIType: "api",
		Label: "label", SecretCipher: []byte{1, 2, 3}, SecretLast4: "suffix",
	}
	_, err := st.CreateCredentialAudited(context.Background(), credential)
	requireAuditFailure(t, err)
	if _, err := st.GetCredential(context.Background(), user.ID, credential.ID); err != byokstore.ErrNotFound {
		t.Fatalf("credential survived failed audit: %v", err)
	}
}

func TestAuditedCredentialDeleteAndCascadeRollBackWhenAuditFails(t *testing.T) {
	st, user := lifecycleStore(t)
	ctx := context.Background()
	credential, err := st.CreateCredential(ctx, byokstore.Credential{
		ID: "credential-delete", UserID: user.ID, Provider: "provider", APIType: "api",
		Label: "label", SecretCipher: []byte{1, 2, 3}, SecretLast4: "suffix",
	})
	if err != nil {
		t.Fatalf("create setup credential: %v", err)
	}
	token, err := st.MintToken(ctx, byokstore.Token{
		ID: "cascade-token", UserID: user.ID, TokenHash: "cascade-hash", Name: "cascade",
		CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"},
	})
	if err != nil {
		t.Fatalf("create setup token: %v", err)
	}
	blockAuditInserts(t, st)
	err = st.DeleteCredentialAudited(ctx, user.ID, credential.ID)
	requireAuditFailure(t, err)
	if _, err := st.GetCredential(ctx, user.ID, credential.ID); err != nil {
		t.Fatalf("credential deletion was not rolled back: %v", err)
	}
	stored, err := st.GetTokenByHash(ctx, token.TokenHash)
	if err != nil || stored.RevokedAt != nil {
		t.Fatalf("cascade revocation was not rolled back: token=%+v err=%v", stored, err)
	}
}

func TestAuditedTokenMintRollsBackWhenAuditFails(t *testing.T) {
	st, user := lifecycleStore(t)
	blockAuditInserts(t, st)
	token := byokstore.Token{
		ID: "token-mint", UserID: user.ID, TokenHash: "mint-hash", Name: "mint",
		CredentialIDs: []string{"credential"}, AllowedModels: []string{"model"},
	}
	_, err := st.MintTokenAudited(context.Background(), token)
	requireAuditFailure(t, err)
	if _, err := st.GetTokenByHash(context.Background(), token.TokenHash); err != byokstore.ErrNotFound {
		t.Fatalf("token survived failed audit: %v", err)
	}
}

func TestAuditedTokenRevokeRollsBackWhenAuditFails(t *testing.T) {
	st, user := lifecycleStore(t)
	ctx := context.Background()
	token, err := st.MintToken(ctx, byokstore.Token{
		ID: "token-revoke", UserID: user.ID, TokenHash: "revoke-hash", Name: "revoke",
		CredentialIDs: []string{"credential"}, AllowedModels: []string{"model"},
	})
	if err != nil {
		t.Fatalf("create setup token: %v", err)
	}
	blockAuditInserts(t, st)
	err = st.RevokeTokenAudited(ctx, user.ID, token.ID)
	requireAuditFailure(t, err)
	stored, err := st.GetTokenByHash(ctx, token.TokenHash)
	if err != nil || stored.RevokedAt != nil {
		t.Fatalf("token revocation was not rolled back: token=%+v err=%v", stored, err)
	}
}
