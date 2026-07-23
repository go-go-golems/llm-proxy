package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// SupportedSchemaVersion is the newest BYOK schema this binary can open.
// Migrations are forward-only: a newer database is rejected rather than
// guessed at or silently downgraded.
const SupportedSchemaVersion = 3

type migration struct {
	version              int
	name                 string
	statements           string
	requiresLegacyIssuer bool
}

type migrationOptions struct {
	legacyOIDCIssuer string
}

var schemaMigrations = []migration{
	{
		version: 1,
		name:    "initial versioned BYOK schema and metering health probe",
		statements: `
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
  model               TEXT NOT NULL,
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
CREATE TABLE IF NOT EXISTS metering_health (
  singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
  checked_at   TIMESTAMP NOT NULL
);
INSERT OR IGNORE INTO metering_health (singleton_id, checked_at)
VALUES (1, CURRENT_TIMESTAMP);
`,
	},
	{
		version:              2,
		name:                 "issuer-aware identities, one-time auth transactions, and revocable sessions",
		requiresLegacyIssuer: true,
		statements: `
PRAGMA defer_foreign_keys = ON;
CREATE TABLE users_v2 (
  id            TEXT PRIMARY KEY,
  oidc_issuer   TEXT NOT NULL CHECK (length(oidc_issuer) > 0),
  oidc_subject  TEXT NOT NULL,
  username      TEXT NOT NULL,
  email         TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL,
  UNIQUE (oidc_issuer, oidc_subject)
);
INSERT INTO users_v2 (id, oidc_issuer, oidc_subject, username, email, created_at, updated_at)
SELECT id, ?, oidc_subject, username, email, created_at, updated_at FROM users;
DROP TABLE users;
ALTER TABLE users_v2 RENAME TO users;
CREATE UNIQUE INDEX idx_users_oidc_identity ON users(oidc_issuer, oidc_subject);
CREATE TABLE auth_transactions (
  id_hash       TEXT PRIMARY KEY,
  state_hash    TEXT NOT NULL UNIQUE,
  nonce         TEXT NOT NULL,
  pkce_verifier TEXT NOT NULL,
  return_to     TEXT NOT NULL,
  created_at    TIMESTAMP NOT NULL,
  expires_at    TIMESTAMP NOT NULL,
  consumed_at   TIMESTAMP
);
CREATE INDEX idx_auth_transactions_expires ON auth_transactions(expires_at);
CREATE TABLE sessions (
  id           TEXT NOT NULL UNIQUE,
  id_hash      TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id),
  created_at   TIMESTAMP NOT NULL,
  last_seen_at TIMESTAMP NOT NULL,
  expires_at   TIMESTAMP NOT NULL,
  revoked_at   TIMESTAMP
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
`,
	},
	{
		version: 3,
		name:    "agent grants, cumulative grant counters, and token provenance",
		statements: `
CREATE TABLE agent_grants (
  id                       TEXT PRIMARY KEY,
  user_id                  TEXT NOT NULL REFERENCES users(id),
  name                     TEXT NOT NULL,
  credential_ids           TEXT NOT NULL,
  allowed_models           TEXT NOT NULL,
  per_token_max_tokens     INTEGER,
  per_token_max_requests   INTEGER,
  rate_limit_rpm           INTEGER,
  token_ttl_seconds        INTEGER NOT NULL CHECK (token_ttl_seconds > 0),
  max_active_per_instance  INTEGER NOT NULL CHECK (max_active_per_instance > 0),
  grant_max_tokens         INTEGER,
  grant_max_requests       INTEGER,
  enabled                  INTEGER NOT NULL DEFAULT 1,
  created_at               TIMESTAMP NOT NULL,
  updated_at               TIMESTAMP NOT NULL,
  revoked_at               TIMESTAMP
);
CREATE INDEX idx_agent_grants_user ON agent_grants(user_id);
CREATE TABLE agent_grant_counters (
  grant_id       TEXT PRIMARY KEY REFERENCES agent_grants(id),
  total_tokens   INTEGER NOT NULL DEFAULT 0,
  total_requests INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE tokens ADD COLUMN agent_grant_id TEXT REFERENCES agent_grants(id);
ALTER TABLE tokens ADD COLUMN issue_channel TEXT NOT NULL DEFAULT 'web';
ALTER TABLE tokens ADD COLUMN source_client_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tokens ADD COLUMN client_instance_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_tokens_grant ON tokens(agent_grant_id);
`,
	},
}

var requiredSchemaColumns = map[string][]string{
	"users":                {"id", "oidc_issuer", "oidc_subject", "username", "email", "created_at", "updated_at"},
	"auth_transactions":    {"id_hash", "state_hash", "nonce", "pkce_verifier", "return_to", "created_at", "expires_at", "consumed_at"},
	"sessions":             {"id", "id_hash", "user_id", "created_at", "last_seen_at", "expires_at", "revoked_at"},
	"credentials":          {"id", "user_id", "provider", "api_type", "label", "secret_cipher", "secret_last4", "disabled", "created_at", "updated_at"},
	"agent_grants":         {"id", "user_id", "name", "credential_ids", "allowed_models", "per_token_max_tokens", "per_token_max_requests", "rate_limit_rpm", "token_ttl_seconds", "max_active_per_instance", "grant_max_tokens", "grant_max_requests", "enabled", "created_at", "updated_at", "revoked_at"},
	"agent_grant_counters": {"grant_id", "total_tokens", "total_requests"},
	"tokens":               {"id", "user_id", "token_hash", "name", "credential_ids", "allowed_models", "max_total_tokens", "max_requests", "rate_limit_rpm", "expires_at", "revoked_at", "created_at", "last_used_at", "agent_grant_id", "issue_channel", "source_client_id", "client_instance_id"},
	"usage_ledger":         {"id", "token_id", "user_id", "model", "prompt_tokens", "completion_tokens", "cached_tokens", "streamed", "status", "created_at"},
	"token_counters":       {"token_id", "total_tokens", "total_requests"},
	"audit_events":         {"id", "user_id", "token_id", "event_type", "payload", "created_at"},
	"metering_health":      {"singleton_id", "checked_at"},
}

var requiredNotNullColumns = map[string][]string{
	"users":                {"oidc_issuer", "oidc_subject", "username", "email", "created_at", "updated_at"},
	"auth_transactions":    {"state_hash", "nonce", "pkce_verifier", "return_to", "created_at", "expires_at"},
	"sessions":             {"id", "user_id", "created_at", "last_seen_at", "expires_at"},
	"credentials":          {"user_id", "provider", "api_type", "label", "secret_cipher", "secret_last4", "disabled", "created_at", "updated_at"},
	"agent_grants":         {"user_id", "name", "credential_ids", "allowed_models", "token_ttl_seconds", "max_active_per_instance", "enabled", "created_at", "updated_at"},
	"agent_grant_counters": {"total_tokens", "total_requests"},
	"tokens":               {"user_id", "token_hash", "name", "credential_ids", "allowed_models", "created_at", "issue_channel", "source_client_id", "client_instance_id"},
	"usage_ledger":         {"token_id", "user_id", "model", "prompt_tokens", "completion_tokens", "cached_tokens", "streamed", "status", "created_at"},
	"token_counters":       {"total_tokens", "total_requests"},
	"audit_events":         {"user_id", "token_id", "event_type", "payload", "created_at"},
	"metering_health":      {"checked_at"},
}

var requiredPrimaryKeys = map[string]string{
	"users": "id", "auth_transactions": "id_hash", "sessions": "id_hash",
	"credentials": "id", "agent_grants": "id", "tokens": "id", "usage_ledger": "id",
	"agent_grant_counters": "grant_id", "token_counters": "token_id", "audit_events": "id", "metering_health": "singleton_id",
}

type indexRequirement struct {
	table   string
	columns []string
	unique  bool
}

var requiredSchemaIndexes = []indexRequirement{
	{table: "users", columns: []string{"oidc_issuer", "oidc_subject"}, unique: true},
	{table: "auth_transactions", columns: []string{"state_hash"}, unique: true},
	{table: "auth_transactions", columns: []string{"expires_at"}},
	{table: "sessions", columns: []string{"id"}, unique: true},
	{table: "sessions", columns: []string{"user_id"}},
	{table: "sessions", columns: []string{"expires_at"}},
	{table: "credentials", columns: []string{"user_id"}},
	{table: "agent_grants", columns: []string{"user_id"}},
	{table: "tokens", columns: []string{"token_hash"}, unique: true},
	{table: "tokens", columns: []string{"agent_grant_id"}},
	{table: "tokens", columns: []string{"user_id"}},
	{table: "usage_ledger", columns: []string{"token_id", "created_at"}},
	{table: "audit_events", columns: []string{"user_id"}},
}

type foreignKeyRequirement struct {
	table     string
	column    string
	refTable  string
	refColumn string
}

var requiredSchemaForeignKeys = []foreignKeyRequirement{
	{table: "sessions", column: "user_id", refTable: "users", refColumn: "id"},
	{table: "credentials", column: "user_id", refTable: "users", refColumn: "id"},
	{table: "agent_grants", column: "user_id", refTable: "users", refColumn: "id"},
	{table: "agent_grant_counters", column: "grant_id", refTable: "agent_grants", refColumn: "id"},
	{table: "tokens", column: "user_id", refTable: "users", refColumn: "id"},
	{table: "tokens", column: "agent_grant_id", refTable: "agent_grants", refColumn: "id"},
	{table: "usage_ledger", column: "token_id", refTable: "tokens", refColumn: "id"},
	{table: "token_counters", column: "token_id", refTable: "tokens", refColumn: "id"},
}

func (s *Store) ensureSchema(options OpenOptions) error {
	return migrateSchema(context.Background(), s.db, schemaMigrations, SupportedSchemaVersion, migrationOptions{
		legacyOIDCIssuer: options.LegacyOIDCIssuer,
	})
}

// SchemaVersion reports the SQLite user_version after validating that it is a
// version this binary understands.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	version, err := readSchemaVersion(ctx, s.db)
	if err != nil {
		return 0, err
	}
	if version > SupportedSchemaVersion {
		return 0, errors.Errorf("BYOK database schema version %d is newer than supported version %d", version, SupportedSchemaVersion)
	}
	return version, nil
}

func migrateSchema(ctx context.Context, db *sql.DB, migrations []migration, supported int, optionList ...migrationOptions) error {
	if ctx == nil {
		return errors.New("schema migration context is required")
	}
	if db == nil {
		return errors.New("schema migration database is required")
	}
	current, err := readSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if current > supported {
		return errors.Errorf("BYOK database schema version %d is newer than supported version %d", current, supported)
	}
	if current == supported {
		return validateCurrentSchema(ctx, db)
	}

	ordered := append([]migration(nil), migrations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	pending := make([]migration, 0, supported-current)
	expected := current + 1
	for _, m := range ordered {
		if m.version <= current {
			continue
		}
		if m.version > supported {
			break
		}
		if m.version != expected {
			return errors.Errorf("missing or duplicate BYOK schema migration %d before migration %d", expected, m.version)
		}
		pending = append(pending, m)
		expected++
	}
	if expected-1 != supported {
		return errors.Errorf("BYOK schema migrations ended at version %d, want %d", expected-1, supported)
	}
	// Validate and apply the complete pending plan in one transaction so a
	// later failure cannot leave a partially advanced schema.
	var options migrationOptions
	if len(optionList) > 0 {
		options = optionList[0]
	}
	return applyMigrations(ctx, db, pending, options, supported)
}

func applyMigrations(ctx context.Context, db *sql.DB, pending []migration, options migrationOptions, supported int) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.Wrap(err, "reserve BYOK schema migration connection")
	}
	defer func() { _ = conn.Close() }()
	// SQLite cannot rebuild a referenced parent table while FK enforcement is
	// active. Disable enforcement only on this reserved startup connection,
	// validate the final graph explicitly, and always restore it before return.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return errors.Wrap(err, "disable foreign keys for BYOK schema migration")
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`) }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin BYOK schema migrations")
	}
	defer func() { _ = tx.Rollback() }()
	for _, m := range pending {
		var args []any
		if m.requiresLegacyIssuer {
			var users int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
				return errors.Wrap(err, "count legacy OIDC users")
			}
			if users > 0 && strings.TrimSpace(options.legacyOIDCIssuer) == "" {
				return errors.New("BYOK schema migration 2 requires --byok-legacy-oidc-issuer for existing users")
			}
			args = []any{strings.TrimSpace(options.legacyOIDCIssuer)}
		}
		if _, err := tx.ExecContext(ctx, m.statements, args...); err != nil {
			return errors.Wrapf(err, "apply BYOK schema migration %d (%s)", m.version, m.name)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			return errors.Wrapf(err, "record BYOK schema migration %d", m.version)
		}
	}
	if err := validateSchema(ctx, tx); err != nil {
		return errors.Wrapf(err, "validate BYOK schema version %d", supported)
	}
	if err := validateForeignKeyIntegrity(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "commit BYOK schema migrations")
	}
	return nil
}

func readSchemaVersion(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var version int
	if err := queryer.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, errors.Wrap(err, "read BYOK schema version")
	}
	if version < 0 {
		return 0, errors.Errorf("invalid negative BYOK schema version %d", version)
	}
	return version, nil
}

func validateCurrentSchema(ctx context.Context, db *sql.DB) error {
	version, err := readSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if version != SupportedSchemaVersion {
		return errors.Errorf("BYOK database schema version is %d, want %d", version, SupportedSchemaVersion)
	}
	return validateSchema(ctx, db)
}

type schemaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateSchema(ctx context.Context, queryer schemaQueryer) error {
	for table, required := range requiredSchemaColumns {
		rows, err := queryer.QueryContext(ctx, `SELECT cid, name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
		if err != nil {
			return errors.Wrapf(err, "inspect BYOK table %s", table)
		}
		type columnConstraint struct {
			notNull    bool
			primaryKey bool
		}
		found := make(map[string]columnConstraint, len(required))
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				_ = rows.Close()
				return errors.Wrapf(err, "scan BYOK table %s", table)
			}
			found[name] = columnConstraint{notNull: notNull == 1, primaryKey: primaryKey > 0}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return errors.Wrapf(err, "iterate BYOK table %s", table)
		}
		_ = rows.Close()
		var missing []string
		for _, column := range required {
			if _, ok := found[column]; !ok {
				missing = append(missing, column)
			}
		}
		if len(missing) != 0 {
			return errors.Errorf("BYOK table %s is missing required columns: %s", table, strings.Join(missing, ", "))
		}
		for _, column := range requiredNotNullColumns[table] {
			if !found[column].notNull {
				return errors.Errorf("BYOK table %s column %s must be NOT NULL", table, column)
			}
		}
		primaryKey := requiredPrimaryKeys[table]
		if primaryKey == "" || !found[primaryKey].primaryKey {
			return errors.Errorf("BYOK table %s is missing required primary key on %s", table, primaryKey)
		}
	}
	if err := validateRequiredIndexes(ctx, queryer); err != nil {
		return err
	}
	if err := validateRequiredForeignKeys(ctx, queryer); err != nil {
		return err
	}
	var singletonCount int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM metering_health WHERE singleton_id = 1`).Scan(&singletonCount); err != nil {
		return errors.Wrap(err, "validate metering health probe row")
	}
	if singletonCount != 1 {
		return errors.New("metering health probe row is missing")
	}
	var emptyIssuers int
	if err := queryer.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE oidc_issuer = ''`).Scan(&emptyIssuers); err != nil {
		return errors.Wrap(err, "validate OIDC identity issuers")
	}
	if emptyIssuers != 0 {
		return errors.New("BYOK users contain an empty OIDC issuer")
	}
	return nil
}

func validateForeignKeyIntegrity(ctx context.Context, queryer schemaQueryer) error {
	rows, err := queryer.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return errors.Wrap(err, "check BYOK foreign key integrity")
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return errors.New("BYOK schema migration left a foreign key violation")
	}
	return rows.Err()
}

func validateRequiredIndexes(ctx context.Context, queryer schemaQueryer) error {
	for _, required := range requiredSchemaIndexes {
		rows, err := queryer.QueryContext(ctx, `SELECT seq, name, "unique", origin, partial FROM pragma_index_list(?)`, required.table)
		if err != nil {
			return errors.Wrapf(err, "inspect BYOK indexes for %s", required.table)
		}
		matched := false
		for rows.Next() {
			var sequence, unique, partial int
			var name, origin string
			if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
				_ = rows.Close()
				return errors.Wrapf(err, "scan BYOK index for %s", required.table)
			}
			if required.unique && unique != 1 {
				continue
			}
			columns, err := indexColumns(ctx, queryer, name)
			if err != nil {
				_ = rows.Close()
				return err
			}
			if equalColumnList(columns, required.columns) {
				matched = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return errors.Wrapf(err, "iterate BYOK indexes for %s", required.table)
		}
		_ = rows.Close()
		if !matched {
			kind := "index"
			if required.unique {
				kind = "unique index"
			}
			return errors.Errorf("BYOK table %s is missing required %s on (%s)", required.table, kind, strings.Join(required.columns, ", "))
		}
	}
	return nil
}

func indexColumns(ctx context.Context, queryer schemaQueryer, index string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT seqno, cid, name FROM pragma_index_info(?)`, index)
	if err != nil {
		return nil, errors.Wrapf(err, "inspect BYOK index %s", index)
	}
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var sequence, columnID int
		var name string
		if err := rows.Scan(&sequence, &columnID, &name); err != nil {
			return nil, errors.Wrapf(err, "scan BYOK index %s", index)
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func validateRequiredForeignKeys(ctx context.Context, queryer schemaQueryer) error {
	for _, required := range requiredSchemaForeignKeys {
		rows, err := queryer.QueryContext(ctx, `SELECT id, seq, "table", "from", "to", on_update, on_delete, match FROM pragma_foreign_key_list(?)`, required.table)
		if err != nil {
			return errors.Wrapf(err, "inspect BYOK foreign keys for %s", required.table)
		}
		matched := false
		for rows.Next() {
			var id, sequence int
			var refTable, column, refColumn, onUpdate, onDelete, match string
			if err := rows.Scan(&id, &sequence, &refTable, &column, &refColumn, &onUpdate, &onDelete, &match); err != nil {
				_ = rows.Close()
				return errors.Wrapf(err, "scan BYOK foreign key for %s", required.table)
			}
			if column == required.column && refTable == required.refTable && refColumn == required.refColumn {
				matched = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return errors.Wrapf(err, "iterate BYOK foreign keys for %s", required.table)
		}
		_ = rows.Close()
		if !matched {
			return errors.Errorf("BYOK table %s is missing required foreign key %s -> %s(%s)", required.table, required.column, required.refTable, required.refColumn)
		}
	}
	return nil
}

func equalColumnList(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
