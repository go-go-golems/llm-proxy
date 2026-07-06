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

var _ store.Store = &Store{}

// Open creates or opens the BYOK database at path and ensures the schema.
func Open(path string) (*Store, error) {
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
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ensureSchema() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  oidc_subject  TEXT NOT NULL UNIQUE,
  username      TEXT NOT NULL,
  email         TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS credentials (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id),
  provider      TEXT NOT NULL,
  api_type      TEXT NOT NULL,
  label         TEXT NOT NULL,
  secret_cipher BLOB NOT NULL,
  secret_last4  TEXT NOT NULL,
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_credentials_user ON credentials(user_id);
CREATE TABLE IF NOT EXISTS tokens (
  id                TEXT PRIMARY KEY,
  user_id           TEXT NOT NULL REFERENCES users(id),
  token_hash        TEXT NOT NULL UNIQUE,
  name              TEXT NOT NULL,
  credential_ids    TEXT NOT NULL,
  allowed_models    TEXT NOT NULL,
  max_total_tokens  INTEGER,
  max_requests      INTEGER,
  rate_limit_rpm    INTEGER,
  expires_at        TIMESTAMP,
  revoked_at        TIMESTAMP,
  created_at        TIMESTAMP NOT NULL,
  last_used_at      TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_tokens_user ON tokens(user_id);
CREATE TABLE IF NOT EXISTS usage_ledger (
  id                 TEXT PRIMARY KEY,
  token_id           TEXT NOT NULL REFERENCES tokens(id),
  user_id            TEXT NOT NULL,
  model              TEXT NOT NULL,
  prompt_tokens      INTEGER NOT NULL,
  completion_tokens  INTEGER NOT NULL,
  cached_tokens      INTEGER NOT NULL DEFAULT 0,
  streamed           INTEGER NOT NULL DEFAULT 0,
  status             TEXT NOT NULL,
  created_at         TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_token_time ON usage_ledger(token_id, created_at);
CREATE TABLE IF NOT EXISTS token_counters (
  token_id       TEXT PRIMARY KEY REFERENCES tokens(id),
  total_tokens   INTEGER NOT NULL DEFAULT 0,
  total_requests INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS audit_events (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL DEFAULT '',
  token_id    TEXT NOT NULL DEFAULT '',
  event_type  TEXT NOT NULL,
  payload     TEXT NOT NULL,
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_events(user_id);
`
	_, err := s.db.Exec(ddl)
	return errors.Wrap(err, "ensure byok schema")
}

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

func intPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// --- UserStore ---

func (s *Store) UpsertUser(ctx context.Context, user User) (User, error) {
	if user.ID == "" {
		user.ID = store.NewID()
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(oidc_subject) DO UPDATE SET
  username = excluded.username,
  email = excluded.email,
  updated_at = excluded.updated_at`,
		user.ID, user.OIDCSubject, user.Username, user.Email, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return User{}, errors.Wrap(err, "upsert user")
	}
	return s.GetUserBySubject(ctx, user.OIDCSubject)
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.OIDCSubject, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, store.ErrNotFound
	}
	if err != nil {
		return User{}, errors.Wrap(err, "scan user")
	}
	return u, nil
}

func (s *Store) GetUserBySubject(ctx context.Context, subject string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_subject, username, email, created_at, updated_at FROM users WHERE oidc_subject = ?`, subject))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, oidc_subject, username, email, created_at, updated_at FROM users WHERE username = ? ORDER BY created_at LIMIT 1`, username))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, oidc_subject, username, email, created_at, updated_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, errors.Wrap(err, "list users")
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.OIDCSubject, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, errors.Wrap(err, "scan user")
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// --- CredentialStore ---

func (s *Store) CreateCredential(ctx context.Context, credential Credential) (Credential, error) {
	if credential.ID == "" {
		credential.ID = store.NewID()
	}
	now := time.Now().UTC()
	credential.CreatedAt = now
	credential.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO credentials (id, user_id, provider, api_type, label, secret_cipher, secret_last4, disabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		credential.ID, credential.UserID, credential.Provider, credential.APIType, credential.Label,
		credential.SecretCipher, credential.SecretLast4, credential.Disabled, credential.CreatedAt, credential.UpdatedAt)
	if err != nil {
		return Credential{}, errors.Wrap(err, "create credential")
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

	now := time.Now().UTC()
	for _, id := range revoke {
		if _, err := tx.ExecContext(ctx, `UPDATE tokens SET revoked_at = ? WHERE id = ?`, now, id); err != nil {
			return errors.Wrap(err, "revoke token on credential delete")
		}
	}
	return errors.Wrap(tx.Commit(), "commit delete credential")
}

// --- TokenStore ---

func (s *Store) MintToken(ctx context.Context, token Token) (Token, error) {
	if token.ID == "" {
		token.ID = store.NewID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	credIDs, err := marshalStrings(token.CredentialIDs)
	if err != nil {
		return Token{}, err
	}
	models, err := marshalStrings(token.AllowedModels)
	if err != nil {
		return Token{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO tokens (id, user_id, token_hash, name, credential_ids, allowed_models,
                    max_total_tokens, max_requests, rate_limit_rpm, expires_at, revoked_at, created_at, last_used_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID, token.UserID, token.TokenHash, token.Name, credIDs, models,
		nullInt(token.MaxTotalTokens), nullInt(token.MaxRequests), nullInt(token.RateLimitRPM),
		nullTime(token.ExpiresAt), nullTime(token.RevokedAt), token.CreatedAt, nullTime(token.LastUsedAt))
	if err != nil {
		return Token{}, errors.Wrap(err, "mint token")
	}
	return token, nil
}

const tokenColumns = `id, user_id, token_hash, name, credential_ids, allowed_models,
max_total_tokens, max_requests, rate_limit_rpm, expires_at, revoked_at, created_at, last_used_at`

func scanToken(scan func(dest ...any) error) (Token, error) {
	var t Token
	var credIDs, models string
	var maxTokens, maxRequests, rateLimit sql.NullInt64
	var expiresAt, revokedAt, lastUsedAt sql.NullTime
	err := scan(&t.ID, &t.UserID, &t.TokenHash, &t.Name, &credIDs, &models,
		&maxTokens, &maxRequests, &rateLimit, &expiresAt, &revokedAt, &t.CreatedAt, &lastUsedAt)
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
	res, err := s.db.ExecContext(ctx,
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
	return nil
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
	}
	return errors.Wrap(tx.Commit(), "commit record usage")
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
	_, err := s.db.ExecContext(ctx, `
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
	User        = store.User
	Credential  = store.Credential
	Token       = store.Token
	LedgerEntry = store.LedgerEntry
	Counters    = store.Counters
	AuditEvent  = store.AuditEvent
	AuditFilter = store.AuditFilter
)
