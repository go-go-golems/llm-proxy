// Package sqlite implements the BYOK store on SQLite via mattn/go-sqlite3.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

type Store struct {
	db *sql.DB
}

// OpenOptions controls explicit, security-sensitive schema migration inputs.
type OpenOptions struct {
	// LegacyOIDCIssuer is required only when migration 2 encounters existing
	// users whose old schema had no issuer column.
	LegacyOIDCIssuer string
}

var _ store.Store = &Store{}

// Open creates or opens the BYOK database at path and ensures the schema.
func Open(path string, optionList ...OpenOptions) (*Store, error) {
	if path == "" {
		// An empty path would make the driver treat the DSN query string as
		// the filename (creating a file literally named "?_foreign_keys=…").
		return nil, errors.New("byok db path is empty")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, errors.Wrap(err, "create byok db directory")
		}
	}
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, errors.Wrap(err, "open byok sqlite db")
	}
	s := &Store{db: db}
	var options OpenOptions
	if len(optionList) > 0 {
		options = optionList[0]
	}
	if err := s.ensureSchema(options); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- helpers ---

func marshalStrings(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", errors.Wrap(err, "marshal string list")
	}
	return string(b), nil
}

func unmarshalStrings(raw string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.Wrap(err, "unmarshal string list")
	}
	return out, nil
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func nullInt(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func intPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// --- UserStore ---

func (s *Store) UpsertUser(ctx context.Context, user User) (User, error) {
	if user.OIDCIssuer == "" || user.OIDCSubject == "" {
		return User{}, errors.New("OIDC issuer and subject are required")
	}
	if user.ID == "" {
		user.ID = store.NewID()
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, oidc_issuer, oidc_subject, username, email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(oidc_issuer, oidc_subject) DO UPDATE SET
  username = excluded.username,
  email = excluded.email,
  updated_at = excluded.updated_at`,
		user.ID, user.OIDCIssuer, user.OIDCSubject, user.Username, user.Email, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return User{}, errors.Wrap(err, "upsert user")
	}
	return s.GetUserByIdentity(ctx, user.OIDCIssuer, user.OIDCSubject)
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, store.ErrNotFound
	}
	if err != nil {
		return User{}, errors.Wrap(err, "scan user")
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_issuer, oidc_subject, username, email, created_at, updated_at FROM users WHERE id = ?`, userID))
}

func (s *Store) GetUserByIdentity(ctx context.Context, issuer, subject string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_issuer, oidc_subject, username, email, created_at, updated_at FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, issuer, subject))
}

func (s *Store) GetUserBySubject(ctx context.Context, subject string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_issuer, oidc_subject, username, email, created_at, updated_at FROM users WHERE oidc_subject = ? ORDER BY created_at LIMIT 1`, subject))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_issuer, oidc_subject, username, email, created_at, updated_at FROM users WHERE username = ? ORDER BY created_at LIMIT 1`, username))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, oidc_issuer, oidc_subject, username, email, created_at, updated_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, errors.Wrap(err, "list users")
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OIDCIssuer, &u.OIDCSubject, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, errors.Wrap(err, "scan user")
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// --- AuthTransactionStore and SessionStore ---

func (s *Store) CreateAuthTransaction(ctx context.Context, transaction AuthTransaction) error {
	if transaction.IDHash == "" || transaction.StateHash == "" || transaction.Nonce == "" || transaction.PKCEVerifier == "" {
		return errors.New("auth transaction hashes, nonce, and PKCE verifier are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin create auth transaction")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_transactions WHERE expires_at <= ? OR consumed_at IS NOT NULL`, transaction.CreatedAt); err != nil {
		return errors.Wrap(err, "prune expired auth transactions")
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_transactions (id_hash, state_hash, nonce, pkce_verifier, return_to, created_at, expires_at, consumed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`, transaction.IDHash, transaction.StateHash, transaction.Nonce,
		transaction.PKCEVerifier, transaction.ReturnTo, transaction.CreatedAt, transaction.ExpiresAt); err != nil {
		return errors.Wrap(err, "create auth transaction")
	}
	if err := appendAuditEvent(ctx, tx, store.AuthTransactionEvent(store.AuditAuthTransactionCreated, transaction.CreatedAt)); err != nil {
		return err
	}
	return errors.Wrap(tx.Commit(), "commit create auth transaction")
}

func (s *Store) ConsumeAuthTransaction(ctx context.Context, idHash, stateHash string, now time.Time) (AuthTransaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthTransaction{}, errors.Wrap(err, "begin consume auth transaction")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE auth_transactions SET consumed_at = ?
WHERE id_hash = ? AND state_hash = ? AND consumed_at IS NULL AND expires_at > ?`, now, idHash, stateHash, now)
	if err != nil {
		return AuthTransaction{}, errors.Wrap(err, "consume auth transaction")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return AuthTransaction{}, store.ErrNotFound
	}
	var transaction AuthTransaction
	var consumed sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT id_hash, state_hash, nonce, pkce_verifier, return_to, created_at, expires_at, consumed_at
FROM auth_transactions WHERE id_hash = ?`, idHash).Scan(&transaction.IDHash, &transaction.StateHash,
		&transaction.Nonce, &transaction.PKCEVerifier, &transaction.ReturnTo, &transaction.CreatedAt,
		&transaction.ExpiresAt, &consumed)
	if err != nil {
		return AuthTransaction{}, errors.Wrap(err, "load consumed auth transaction")
	}
	transaction.ConsumedAt = timePtr(consumed)
	if err := appendAuditEvent(ctx, tx, store.AuthTransactionEvent(store.AuditAuthTransactionConsumed, now)); err != nil {
		return AuthTransaction{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_transactions WHERE id_hash = ?`, idHash); err != nil {
		return AuthTransaction{}, errors.Wrap(err, "delete consumed auth transaction")
	}
	if err := tx.Commit(); err != nil {
		return AuthTransaction{}, errors.Wrap(err, "commit consume auth transaction")
	}
	return transaction, nil
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	if session.ID == "" || session.IDHash == "" || session.UserID == "" {
		return errors.New("session ID, ID hash, and user ID are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin create session")
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, session.CreatedAt); err != nil {
		return errors.Wrap(err, "prune expired sessions")
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sessions (id, id_hash, user_id, created_at, last_seen_at, expires_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, NULL)`, session.ID, session.IDHash, session.UserID, session.CreatedAt, session.LastSeenAt, session.ExpiresAt); err != nil {
		return errors.Wrap(err, "create session")
	}
	if err := appendAuditEvent(ctx, tx, store.SessionEvent(session.UserID, store.AuditSessionCreated, session.CreatedAt)); err != nil {
		return err
	}
	return errors.Wrap(tx.Commit(), "commit create session")
}

func (s *Store) UseSession(ctx context.Context, idHash string, now, idleCutoff time.Time) (Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, errors.Wrap(err, "begin use session")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE sessions SET last_seen_at = ?
WHERE id_hash = ? AND revoked_at IS NULL AND expires_at > ? AND last_seen_at >= ?`, now, idHash, now, idleCutoff)
	if err != nil {
		return Session{}, errors.Wrap(err, "use session")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return Session{}, store.ErrNotFound
	}
	var session Session
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT id, id_hash, user_id, created_at, last_seen_at, expires_at, revoked_at
FROM sessions WHERE id_hash = ?`, idHash).Scan(&session.ID, &session.IDHash, &session.UserID, &session.CreatedAt,
		&session.LastSeenAt, &session.ExpiresAt, &revoked)
	if err != nil {
		return Session{}, errors.Wrap(err, "load used session")
	}
	session.RevokedAt = timePtr(revoked)
	if err := tx.Commit(); err != nil {
		return Session{}, errors.Wrap(err, "commit use session")
	}
	return session, nil
}

func (s *Store) ListSessionsByUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, id_hash, user_id, created_at, last_seen_at, expires_at, revoked_at FROM sessions WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "list sessions")
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var session Session
		var revoked sql.NullTime
		if err := rows.Scan(&session.ID, &session.IDHash, &session.UserID, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt, &revoked); err != nil {
			return nil, errors.Wrap(err, "scan session")
		}
		session.RevokedAt = timePtr(revoked)
		out = append(out, session)
	}
	return out, rows.Err()
}

func (s *Store) RevokeSession(ctx context.Context, idHash string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin revoke session")
	}
	defer func() { _ = tx.Rollback() }()
	var userID string
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE id_hash = ? AND revoked_at IS NULL`, idHash).Scan(&userID); errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	} else if err != nil {
		return errors.Wrap(err, "load session for revocation")
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash = ? AND revoked_at IS NULL`, at, idHash)
	if err != nil {
		return errors.Wrap(err, "revoke session")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return store.ErrNotFound
	}
	if err := appendAuditEvent(ctx, tx, store.SessionEvent(userID, store.AuditSessionRevoked, at)); err != nil {
		return err
	}
	return errors.Wrap(tx.Commit(), "commit revoke session")
}

func (s *Store) RevokeSessionByID(ctx context.Context, userID, sessionID string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin revoke session by ID")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, at, sessionID, userID)
	if err != nil {
		return errors.Wrap(err, "revoke session by ID")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return store.ErrNotFound
	}
	if err := appendAuditEvent(ctx, tx, store.SessionEvent(userID, store.AuditSessionRevoked, at)); err != nil {
		return err
	}
	return errors.Wrap(tx.Commit(), "commit revoke session by ID")
}

// --- CredentialStore ---

func (s *Store) CreateCredential(ctx context.Context, credential Credential) (Credential, error) {
	return s.createCredential(ctx, credential, false)
}

func (s *Store) CreateCredentialAudited(ctx context.Context, credential Credential) (Credential, error) {
	return s.createCredential(ctx, credential, true)
}

func (s *Store) createCredential(ctx context.Context, credential Credential, audited bool) (Credential, error) {
	if credential.ID == "" {
		credential.ID = store.NewID()
	}
	now := time.Now().UTC()
	credential.CreatedAt = now
	credential.UpdatedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Credential{}, errors.Wrap(err, "begin create credential")
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO credentials (id, user_id, provider, api_type, label, secret_cipher, secret_last4, disabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		credential.ID, credential.UserID, credential.Provider, credential.APIType, credential.Label,
		credential.SecretCipher, credential.SecretLast4, credential.Disabled, credential.CreatedAt, credential.UpdatedAt)
	if err != nil {
		return Credential{}, errors.Wrap(err, "create credential")
	}
	if audited {
		if err := appendAuditEvent(ctx, tx, store.CredentialCreatedEvent(credential)); err != nil {
			return Credential{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Credential{}, errors.Wrap(err, "commit create credential")
	}
	return credential, nil
}

const credentialColumns = `id, user_id, provider, api_type, label, secret_cipher, secret_last4, disabled, created_at, updated_at`

func scanCredential(scan func(dest ...any) error) (Credential, error) {
	var c Credential
	err := scan(&c.ID, &c.UserID, &c.Provider, &c.APIType, &c.Label, &c.SecretCipher, &c.SecretLast4, &c.Disabled, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, store.ErrNotFound
	}
	if err != nil {
		return Credential{}, errors.Wrap(err, "scan credential")
	}
	return c, nil
}

func (s *Store) GetCredential(ctx context.Context, userID, credentialID string) (Credential, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+credentialColumns+` FROM credentials WHERE id = ? AND user_id = ?`, credentialID, userID)
	return scanCredential(row.Scan)
}

func (s *Store) ListCredentialsByUser(ctx context.Context, userID string) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+credentialColumns+` FROM credentials WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "list credentials")
	}
	defer func() { _ = rows.Close() }()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCredential(ctx context.Context, userID, credentialID string) error {
	return s.deleteCredential(ctx, userID, credentialID, false)
}

func (s *Store) DeleteCredentialAudited(ctx context.Context, userID, credentialID string) error {
	return s.deleteCredential(ctx, userID, credentialID, true)
}

func (s *Store) deleteCredential(ctx context.Context, userID, credentialID string, audited bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin delete credential")
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM credentials WHERE id = ? AND user_id = ?`, credentialID, userID)
	if err != nil {
		return errors.Wrap(err, "delete credential")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "delete credential rows affected")
	}
	if n == 0 {
		return store.ErrNotFound
	}

	// A grant must never silently continue after one of its approved credential
	// bindings disappears. Revoke every affected grant and all child tokens in
	// this same transaction.
	grantRows, err := tx.QueryContext(ctx, `SELECT id, credential_ids FROM agent_grants WHERE user_id = ? AND revoked_at IS NULL`, userID)
	if err != nil {
		return errors.Wrap(err, "list agent grants for credential cascade")
	}
	var revokeGrants []string
	for grantRows.Next() {
		var grantID, rawIDs string
		if err := grantRows.Scan(&grantID, &rawIDs); err != nil {
			_ = grantRows.Close()
			return errors.Wrap(err, "scan agent grant for credential cascade")
		}
		ids, err := unmarshalStrings(rawIDs)
		if err != nil {
			_ = grantRows.Close()
			return err
		}
		for _, id := range ids {
			if id == credentialID {
				revokeGrants = append(revokeGrants, grantID)
				break
			}
		}
	}
	if err := grantRows.Close(); err != nil {
		return errors.Wrap(err, "close agent grant cascade rows")
	}
	now := time.Now().UTC()
	for _, grantID := range revokeGrants {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_grants SET enabled=0, revoked_at=?, updated_at=? WHERE id=?`, now, now, grantID); err != nil {
			return errors.Wrap(err, "revoke agent grant for credential cascade")
		}
		if audited {
			grant := AgentGrant{ID: grantID, UserID: userID, RevokedAt: &now, UpdatedAt: now}
			if err := appendAuditEvent(ctx, tx, store.AgentGrantEvent(grant, store.AuditAgentGrantRevoked)); err != nil {
				return err
			}
		}
		if err := revokeGrantTokens(ctx, tx, userID, grantID, now, audited); err != nil {
			return err
		}
	}

	// Revoke tokens whose only remaining credential binding was this credential.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, credential_ids FROM tokens WHERE user_id = ? AND revoked_at IS NULL`, userID)
	if err != nil {
		return errors.Wrap(err, "list tokens for cascade")
	}
	var revoke []string
	for rows.Next() {
		var id, rawIDs string
		if err := rows.Scan(&id, &rawIDs); err != nil {
			_ = rows.Close()
			return errors.Wrap(err, "scan token for cascade")
		}
		ids, err := unmarshalStrings(rawIDs)
		if err != nil {
			_ = rows.Close()
			return err
		}
		remaining := 0
		bound := false
		for _, cid := range ids {
			if cid == credentialID {
				bound = true
			} else {
				remaining++
			}
		}
		if bound && remaining == 0 {
			revoke = append(revoke, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return errors.Wrap(err, "iterate tokens for cascade")
	}
	_ = rows.Close()

	for _, id := range revoke {
		if _, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at = ? WHERE id = ?`, now, id); err != nil {
			return errors.Wrap(err, "revoke token on credential delete")
		}
		if audited {
			if err := appendAuditEvent(ctx, tx, store.TokenRevokedEvent(userID, id)); err != nil {
				return err
			}
		}
	}
	if audited {
		if err := appendAuditEvent(ctx, tx, store.CredentialDeletedEvent(userID, credentialID)); err != nil {
			return err
		}
	}
	return errors.Wrap(tx.Commit(), "commit delete credential")
}

// --- AgentGrantStore ---

const agentGrantColumns = `id, user_id, name, credential_ids, allowed_models,
per_token_max_tokens, per_token_max_requests, rate_limit_rpm, token_ttl_seconds,
max_active_per_instance, grant_max_tokens, grant_max_requests, enabled, created_at, updated_at, revoked_at`

func scanAgentGrant(scan func(dest ...any) error) (AgentGrant, error) {
	var grant AgentGrant
	var credentialIDs, models string
	var perTokenTokens, perTokenRequests, rateLimit, grantTokens, grantRequests sql.NullInt64
	var ttlSeconds int64
	var revoked sql.NullTime
	err := scan(&grant.ID, &grant.UserID, &grant.Name, &credentialIDs, &models,
		&perTokenTokens, &perTokenRequests, &rateLimit, &ttlSeconds,
		&grant.MaxActivePerInstance, &grantTokens, &grantRequests, &grant.Enabled,
		&grant.CreatedAt, &grant.UpdatedAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentGrant{}, store.ErrNotFound
	}
	if err != nil {
		return AgentGrant{}, errors.Wrap(err, "scan agent grant")
	}
	if grant.CredentialIDs, err = unmarshalStrings(credentialIDs); err != nil {
		return AgentGrant{}, err
	}
	if grant.AllowedModels, err = unmarshalStrings(models); err != nil {
		return AgentGrant{}, err
	}
	grant.PerTokenMaxTokens = intPtr(perTokenTokens)
	grant.PerTokenMaxRequests = intPtr(perTokenRequests)
	grant.RateLimitRPM = intPtr(rateLimit)
	grant.TokenTTL = time.Duration(ttlSeconds) * time.Second
	grant.GrantMaxTokens = intPtr(grantTokens)
	grant.GrantMaxRequests = intPtr(grantRequests)
	grant.RevokedAt = timePtr(revoked)
	return grant, nil
}

func (s *Store) validateAgentGrantCredentials(ctx context.Context, tx *sql.Tx, grant AgentGrant) error {
	if err := store.ValidateAgentGrantPolicy(grant); err != nil {
		return err
	}
	for _, credentialID := range grant.CredentialIDs {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM credentials WHERE id = ? AND user_id = ? AND disabled = 0`, credentialID, grant.UserID).Scan(&count); err != nil {
			return errors.Wrap(err, "validate agent grant credential")
		}
		if count != 1 {
			return errors.New("agent grant credential is unavailable")
		}
	}
	return nil
}

func (s *Store) CreateAgentGrantAudited(ctx context.Context, grant AgentGrant) (AgentGrant, error) {
	if grant.ID == "" {
		grant.ID = store.NewID()
	}
	now := time.Now().UTC()
	grant.CreatedAt, grant.UpdatedAt, grant.Enabled = now, now, true
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentGrant{}, errors.Wrap(err, "begin create agent grant")
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.validateAgentGrantCredentials(ctx, tx, grant); err != nil {
		return AgentGrant{}, err
	}
	credentialIDs, err := marshalStrings(grant.CredentialIDs)
	if err != nil {
		return AgentGrant{}, err
	}
	models, err := marshalStrings(grant.AllowedModels)
	if err != nil {
		return AgentGrant{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_grants (`+agentGrantColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		grant.ID, grant.UserID, grant.Name, credentialIDs, models,
		nullInt(grant.PerTokenMaxTokens), nullInt(grant.PerTokenMaxRequests), nullInt(grant.RateLimitRPM),
		int64(grant.TokenTTL/time.Second), grant.MaxActivePerInstance, nullInt(grant.GrantMaxTokens),
		nullInt(grant.GrantMaxRequests), grant.Enabled, grant.CreatedAt, grant.UpdatedAt)
	if err != nil {
		return AgentGrant{}, errors.Wrap(err, "create agent grant")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_grant_counters (grant_id, total_tokens, total_requests) VALUES (?, 0, 0)`, grant.ID); err != nil {
		return AgentGrant{}, errors.Wrap(err, "create agent grant counters")
	}
	if err := appendAuditEvent(ctx, tx, store.AgentGrantEvent(grant, store.AuditAgentGrantCreated)); err != nil {
		return AgentGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentGrant{}, errors.Wrap(err, "commit create agent grant")
	}
	return grant, nil
}

func (s *Store) UpdateAgentGrantAudited(ctx context.Context, grant AgentGrant) (AgentGrant, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentGrant{}, errors.Wrap(err, "begin update agent grant")
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := scanAgentGrant(tx.QueryRowContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, grant.ID, grant.UserID).Scan)
	if err != nil {
		return AgentGrant{}, err
	}
	if err := s.validateAgentGrantCredentials(ctx, tx, grant); err != nil {
		return AgentGrant{}, err
	}
	credentialIDs, err := marshalStrings(grant.CredentialIDs)
	if err != nil {
		return AgentGrant{}, err
	}
	models, err := marshalStrings(grant.AllowedModels)
	if err != nil {
		return AgentGrant{}, err
	}
	grant.CreatedAt, grant.UpdatedAt, grant.Enabled, grant.RevokedAt = existing.CreatedAt, time.Now().UTC(), true, nil
	_, err = tx.ExecContext(ctx, `UPDATE agent_grants SET name=?, credential_ids=?, allowed_models=?, per_token_max_tokens=?, per_token_max_requests=?, rate_limit_rpm=?, token_ttl_seconds=?, max_active_per_instance=?, grant_max_tokens=?, grant_max_requests=?, enabled=1, updated_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`,
		grant.Name, credentialIDs, models, nullInt(grant.PerTokenMaxTokens), nullInt(grant.PerTokenMaxRequests), nullInt(grant.RateLimitRPM),
		int64(grant.TokenTTL/time.Second), grant.MaxActivePerInstance, nullInt(grant.GrantMaxTokens), nullInt(grant.GrantMaxRequests), grant.UpdatedAt, grant.ID, grant.UserID)
	if err != nil {
		return AgentGrant{}, errors.Wrap(err, "update agent grant")
	}
	if err := appendAuditEvent(ctx, tx, store.AgentGrantEvent(grant, store.AuditAgentGrantUpdated)); err != nil {
		return AgentGrant{}, err
	}
	if err := revokeGrantTokens(ctx, tx, grant.UserID, grant.ID, grant.UpdatedAt, true); err != nil {
		return AgentGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentGrant{}, errors.Wrap(err, "commit update agent grant")
	}
	return grant, nil
}

func (s *Store) GetAgentGrant(ctx context.Context, userID, grantID string) (AgentGrant, error) {
	return scanAgentGrant(s.db.QueryRowContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE id = ? AND user_id = ?`, grantID, userID).Scan)
}

func (s *Store) ListAgentGrantsByUser(ctx context.Context, userID string) ([]AgentGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "list agent grants")
	}
	defer func() { _ = rows.Close() }()
	var out []AgentGrant
	for rows.Next() {
		grant, err := scanAgentGrant(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

func (s *Store) GetAgentGrantCounters(ctx context.Context, grantID string) (AgentGrantCounters, error) {
	var counters AgentGrantCounters
	err := s.db.QueryRowContext(ctx, `SELECT grant_id, total_tokens, total_requests FROM agent_grant_counters WHERE grant_id = ?`, grantID).Scan(&counters.GrantID, &counters.TotalTokens, &counters.TotalRequests)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentGrantCounters{}, store.ErrNotFound
	}
	return counters, errors.Wrap(err, "get agent grant counters")
}

func (s *Store) RevokeAgentGrantAudited(ctx context.Context, userID, grantID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin revoke agent grant")
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE agent_grants SET enabled=0, revoked_at=?, updated_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, now, now, grantID, userID)
	if err != nil {
		return errors.Wrap(err, "revoke agent grant")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return store.ErrNotFound
	}
	grant := AgentGrant{ID: grantID, UserID: userID, UpdatedAt: now, RevokedAt: &now}
	if err := appendAuditEvent(ctx, tx, store.AgentGrantEvent(grant, store.AuditAgentGrantRevoked)); err != nil {
		return err
	}
	if err := revokeGrantTokens(ctx, tx, userID, grantID, now, true); err != nil {
		return err
	}
	return errors.Wrap(tx.Commit(), "commit revoke agent grant")
}

func revokeGrantTokens(ctx context.Context, tx *sql.Tx, userID, grantID string, at time.Time, audited bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tokens WHERE user_id=? AND agent_grant_id=? AND revoked_at IS NULL`, userID, grantID)
	if err != nil {
		return errors.Wrap(err, "list agent grant tokens")
	}
	var tokenIDs []string
	for rows.Next() {
		var tokenID string
		if err := rows.Scan(&tokenID); err != nil {
			_ = rows.Close()
			return errors.Wrap(err, "scan agent grant token")
		}
		tokenIDs = append(tokenIDs, tokenID)
	}
	if err := rows.Close(); err != nil {
		return errors.Wrap(err, "close agent grant token rows")
	}
	for _, tokenID := range tokenIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, at, tokenID); err != nil {
			return errors.Wrap(err, "revoke agent grant token")
		}
		if audited {
			if err := appendAuditEvent(ctx, tx, store.TokenRevokedEvent(userID, tokenID)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) IssueAgentTokenAudited(ctx context.Context, token Token) (Token, error) {
	if err := store.ValidateAgentTokenProvenance(token); err != nil {
		return Token{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Token{}, errors.Wrap(err, "begin issue agent token")
	}
	defer func() { _ = tx.Rollback() }()
	grant, err := scanAgentGrant(tx.QueryRowContext(ctx, `SELECT `+agentGrantColumns+` FROM agent_grants WHERE id=? AND user_id=? AND enabled=1 AND revoked_at IS NULL`, token.AgentGrantID, token.UserID).Scan)
	if err != nil {
		return Token{}, err
	}
	if err := s.validateAgentGrantCredentials(ctx, tx, grant); err != nil {
		return Token{}, err
	}
	var counters AgentGrantCounters
	if err := tx.QueryRowContext(ctx, `SELECT grant_id,total_tokens,total_requests FROM agent_grant_counters WHERE grant_id=?`, grant.ID).Scan(&counters.GrantID, &counters.TotalTokens, &counters.TotalRequests); err != nil {
		return Token{}, errors.Wrap(err, "load agent grant budget")
	}
	if (grant.GrantMaxTokens != nil && counters.TotalTokens >= *grant.GrantMaxTokens) || (grant.GrantMaxRequests != nil && counters.TotalRequests >= *grant.GrantMaxRequests) {
		return Token{}, store.ErrGrantExhausted
	}
	now := time.Now().UTC()
	token.ID, token.Name, token.CreatedAt = store.NewID(), grant.Name, now
	expires := now.Add(grant.TokenTTL)
	token.ExpiresAt = &expires
	token.CredentialIDs = append([]string(nil), grant.CredentialIDs...)
	token.AllowedModels = append([]string(nil), grant.AllowedModels...)
	token.MaxTotalTokens, token.MaxRequests, token.RateLimitRPM = grant.PerTokenMaxTokens, grant.PerTokenMaxRequests, grant.RateLimitRPM

	rows, err := tx.QueryContext(ctx, `SELECT id FROM tokens WHERE user_id=? AND agent_grant_id=? AND source_client_id=? AND client_instance_id=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>?) ORDER BY created_at`, token.UserID, grant.ID, token.SourceClientID, token.ClientInstanceID, now)
	if err != nil {
		return Token{}, errors.Wrap(err, "list active client-instance tokens")
	}
	var activeIDs []string
	for rows.Next() {
		var tokenID string
		if err := rows.Scan(&tokenID); err != nil {
			_ = rows.Close()
			return Token{}, errors.Wrap(err, "scan active client-instance token")
		}
		activeIDs = append(activeIDs, tokenID)
	}
	if err := rows.Close(); err != nil {
		return Token{}, errors.Wrap(err, "close active client-instance tokens")
	}
	rotateCount := max(0, len(activeIDs)-grant.MaxActivePerInstance+1)
	for _, tokenID := range activeIDs[:rotateCount] {
		if _, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, tokenID); err != nil {
			return Token{}, errors.Wrap(err, "rotate agent token")
		}
		rotated := Token{ID: tokenID, UserID: token.UserID}
		if err := appendAuditEvent(ctx, tx, store.TokenEvent(rotated, store.AuditTokenRotated)); err != nil {
			return Token{}, err
		}
	}
	credentialIDs, err := marshalStrings(token.CredentialIDs)
	if err != nil {
		return Token{}, err
	}
	models, err := marshalStrings(token.AllowedModels)
	if err != nil {
		return Token{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tokens (id,user_id,token_hash,name,credential_ids,allowed_models,max_total_tokens,max_requests,rate_limit_rpm,expires_at,revoked_at,created_at,last_used_at,agent_grant_id,issue_channel,source_client_id,client_instance_id) VALUES (?,?,?,?,?,?,?,?,?,?,NULL,?,NULL,?,?,?,?)`,
		token.ID, token.UserID, token.TokenHash, token.Name, credentialIDs, models,
		nullInt(token.MaxTotalTokens), nullInt(token.MaxRequests), nullInt(token.RateLimitRPM), token.ExpiresAt,
		token.CreatedAt, token.AgentGrantID, token.IssueChannel, token.SourceClientID, token.ClientInstanceID)
	if err != nil {
		return Token{}, errors.Wrap(err, "insert agent token")
	}
	if err := appendAuditEvent(ctx, tx, store.TokenEvent(token, store.AuditDeviceTokenIssued)); err != nil {
		return Token{}, err
	}
	if err := tx.Commit(); err != nil {
		return Token{}, errors.Wrap(err, "commit issue agent token")
	}
	return token, nil
}

// --- TokenStore ---

func (s *Store) MintToken(ctx context.Context, token Token) (Token, error) {
	return s.mintToken(ctx, token, false)
}

func (s *Store) MintTokenAudited(ctx context.Context, token Token) (Token, error) {
	return s.mintToken(ctx, token, true)
}

func (s *Store) mintToken(ctx context.Context, token Token, audited bool) (Token, error) {
	if token.ID == "" {
		token.ID = store.NewID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	if token.IssueChannel == "" {
		token.IssueChannel = store.IssueChannelWeb
	}
	credIDs, err := marshalStrings(token.CredentialIDs)
	if err != nil {
		return Token{}, err
	}
	models, err := marshalStrings(token.AllowedModels)
	if err != nil {
		return Token{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Token{}, errors.Wrap(err, "begin mint token")
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
INSERT INTO tokens (id, user_id, token_hash, name, credential_ids, allowed_models,
                    max_total_tokens, max_requests, rate_limit_rpm, expires_at, revoked_at, created_at, last_used_at,
                    agent_grant_id, issue_channel, source_client_id, client_instance_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID, token.UserID, token.TokenHash, token.Name, credIDs, models,
		nullInt(token.MaxTotalTokens), nullInt(token.MaxRequests), nullInt(token.RateLimitRPM),
		nullTime(token.ExpiresAt), nullTime(token.RevokedAt), token.CreatedAt, nullTime(token.LastUsedAt),
		nullString(token.AgentGrantID), token.IssueChannel, token.SourceClientID, token.ClientInstanceID)
	if err != nil {
		return Token{}, errors.Wrap(err, "mint token")
	}
	if audited {
		if err := appendAuditEvent(ctx, tx, store.TokenMintedEvent(token)); err != nil {
			return Token{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Token{}, errors.Wrap(err, "commit mint token")
	}
	return token, nil
}

const tokenColumns = `id, user_id, token_hash, name, credential_ids, allowed_models,
max_total_tokens, max_requests, rate_limit_rpm, expires_at, revoked_at, created_at, last_used_at,
agent_grant_id, issue_channel, source_client_id, client_instance_id`

func scanToken(scan func(dest ...any) error) (Token, error) {
	var t Token
	var credIDs, models string
	var maxTokens, maxRequests, rateLimit sql.NullInt64
	var expiresAt, revokedAt, lastUsedAt sql.NullTime
	var agentGrantID sql.NullString
	err := scan(&t.ID, &t.UserID, &t.TokenHash, &t.Name, &credIDs, &models,
		&maxTokens, &maxRequests, &rateLimit, &expiresAt, &revokedAt, &t.CreatedAt, &lastUsedAt,
		&agentGrantID, &t.IssueChannel, &t.SourceClientID, &t.ClientInstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, store.ErrNotFound
	}
	if err != nil {
		return Token{}, errors.Wrap(err, "scan token")
	}
	if t.CredentialIDs, err = unmarshalStrings(credIDs); err != nil {
		return Token{}, err
	}
	if t.AllowedModels, err = unmarshalStrings(models); err != nil {
		return Token{}, err
	}
	t.MaxTotalTokens = intPtr(maxTokens)
	t.MaxRequests = intPtr(maxRequests)
	t.RateLimitRPM = intPtr(rateLimit)
	t.ExpiresAt = timePtr(expiresAt)
	t.RevokedAt = timePtr(revokedAt)
	t.LastUsedAt = timePtr(lastUsedAt)
	if agentGrantID.Valid {
		t.AgentGrantID = agentGrantID.String
	}
	return t, nil
}

func (s *Store) GetTokenByHash(ctx context.Context, tokenHash string) (Token, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM tokens WHERE token_hash = ?`, tokenHash)
	return scanToken(row.Scan)
}

func (s *Store) ListTokensByUser(ctx context.Context, userID string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tokenColumns+` FROM tokens WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, errors.Wrap(err, "list tokens")
	}
	defer func() { _ = rows.Close() }()
	var out []Token
	for rows.Next() {
		t, err := scanToken(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeToken(ctx context.Context, userID, tokenID string) error {
	return s.revokeToken(ctx, userID, tokenID, false)
}

func (s *Store) RevokeTokenAudited(ctx context.Context, userID, tokenID string) error {
	return s.revokeToken(ctx, userID, tokenID, true)
}

func (s *Store) revokeToken(ctx context.Context, userID, tokenID string, audited bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin revoke token")
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), tokenID, userID)
	if err != nil {
		return errors.Wrap(err, "revoke token")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "revoke token rows affected")
	}
	if n == 0 {
		return store.ErrNotFound
	}
	if audited {
		if err := appendAuditEvent(ctx, tx, store.TokenRevokedEvent(userID, tokenID)); err != nil {
			return err
		}
	}
	return errors.Wrap(tx.Commit(), "commit revoke token")
}

func (s *Store) TouchTokenUsed(ctx context.Context, tokenID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, at.UTC(), tokenID)
	return errors.Wrap(err, "touch token")
}

// --- MeterStore ---

func (s *Store) RecordUsage(ctx context.Context, entry LedgerEntry) error {
	if entry.ID == "" {
		entry.ID = store.NewID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin record usage")
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
INSERT INTO usage_ledger (id, token_id, user_id, model, prompt_tokens, completion_tokens, cached_tokens, streamed, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.TokenID, entry.UserID, entry.Model,
		entry.PromptTokens, entry.CompletionTokens, entry.CachedTokens, entry.Streamed, entry.Status, entry.CreatedAt)
	if err != nil {
		return errors.Wrap(err, "insert ledger entry")
	}
	if entry.Status != store.LedgerStatusRejected {
		total := entry.PromptTokens + entry.CompletionTokens
		_, err = tx.ExecContext(ctx, `
INSERT INTO token_counters (token_id, total_tokens, total_requests) VALUES (?, ?, 1)
ON CONFLICT(token_id) DO UPDATE SET
  total_tokens = total_tokens + excluded.total_tokens,
  total_requests = total_requests + 1`,
			entry.TokenID, total)
		if err != nil {
			return errors.Wrap(err, "update token counters")
		}
		var grantID sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT agent_grant_id FROM tokens WHERE id = ?`, entry.TokenID).Scan(&grantID); err != nil {
			return errors.Wrap(err, "load token grant for counters")
		}
		if grantID.Valid {
			_, err = tx.ExecContext(ctx, `
INSERT INTO agent_grant_counters (grant_id, total_tokens, total_requests) VALUES (?, ?, 1)
ON CONFLICT(grant_id) DO UPDATE SET
  total_tokens = total_tokens + excluded.total_tokens,
  total_requests = total_requests + 1`, grantID.String, total)
			if err != nil {
				return errors.Wrap(err, "update agent grant counters")
			}
		}
	}
	return errors.Wrap(tx.Commit(), "commit record usage")
}

func (s *Store) CheckMeteringHealth(ctx context.Context) error {
	res, err := s.db.ExecContext(ctx, `UPDATE metering_health SET checked_at = ? WHERE singleton_id = 1`, time.Now().UTC())
	if err != nil {
		return errors.Wrap(err, "write metering health probe")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "metering health probe rows affected")
	}
	if rows != 1 {
		return errors.Errorf("metering health probe updated %d rows, want 1", rows)
	}
	return nil
}

func (s *Store) GetCounters(ctx context.Context, tokenID string) (Counters, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT token_id, total_tokens, total_requests FROM token_counters WHERE token_id = ?`, tokenID)
	var c Counters
	err := row.Scan(&c.TokenID, &c.TotalTokens, &c.TotalRequests)
	if errors.Is(err, sql.ErrNoRows) {
		return Counters{TokenID: tokenID}, nil
	}
	if err != nil {
		return Counters{}, errors.Wrap(err, "scan counters")
	}
	return c, nil
}

func (s *Store) ListLedger(ctx context.Context, tokenID string, since time.Time, limit int) ([]LedgerEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, token_id, user_id, model, prompt_tokens, completion_tokens, cached_tokens, streamed, status, created_at
FROM usage_ledger WHERE token_id = ? AND created_at >= ? ORDER BY created_at DESC LIMIT ?`,
		tokenID, since.UTC(), limit)
	if err != nil {
		return nil, errors.Wrap(err, "list ledger")
	}
	defer func() { _ = rows.Close() }()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.TokenID, &e.UserID, &e.Model,
			&e.PromptTokens, &e.CompletionTokens, &e.CachedTokens, &e.Streamed, &e.Status, &e.CreatedAt); err != nil {
			return nil, errors.Wrap(err, "scan ledger entry")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- AuditStore ---

func (s *Store) AppendEvent(ctx context.Context, event AuditEvent) error {
	return appendAuditEvent(ctx, s.db, event)
}

type auditExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func appendAuditEvent(ctx context.Context, exec auditExecer, event AuditEvent) error {
	if event.ID == "" {
		event.ID = store.NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	payload := event.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	} else if !json.Valid(payload) {
		payload = []byte(`{"warning":"invalid audit payload dropped"}`)
	}
	_, err := exec.ExecContext(ctx, `
INSERT INTO audit_events (id, user_id, token_id, event_type, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID, event.UserID, event.TokenID, event.EventType, string(payload), event.CreatedAt)
	return errors.Wrap(err, "append audit event")
}

func (s *Store) ListEvents(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	query := `SELECT id, user_id, token_id, event_type, payload, created_at FROM audit_events WHERE 1=1`
	var args []any
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.TokenID != "" {
		query += ` AND token_id = ?`
		args = append(args, filter.TokenID)
	}
	if filter.EventType != "" {
		query += ` AND event_type = ?`
		args = append(args, filter.EventType)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "list audit events")
	}
	defer func() { _ = rows.Close() }()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var payload string
		if err := rows.Scan(&e.ID, &e.UserID, &e.TokenID, &e.EventType, &payload, &e.CreatedAt); err != nil {
			return nil, errors.Wrap(err, "scan audit event")
		}
		e.Payload = []byte(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Type aliases so backend code reads naturally.
type (
	User               = store.User
	AuthTransaction    = store.AuthTransaction
	Session            = store.Session
	Credential         = store.Credential
	AgentGrant         = store.AgentGrant
	AgentGrantCounters = store.AgentGrantCounters
	Token              = store.Token
	LedgerEntry        = store.LedgerEntry
	Counters           = store.Counters
	AuditEvent         = store.AuditEvent
	AuditFilter        = store.AuditFilter
)
