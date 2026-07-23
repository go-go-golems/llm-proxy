package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

const legacySchemaDDL = `
CREATE TABLE users (
  id TEXT PRIMARY KEY, oidc_subject TEXT NOT NULL UNIQUE, username TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL
);
CREATE TABLE credentials (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), provider TEXT NOT NULL,
  api_type TEXT NOT NULL, label TEXT NOT NULL, secret_cipher BLOB NOT NULL,
  secret_last4 TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_credentials_user ON credentials(user_id);
CREATE TABLE tokens (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), token_hash TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL, credential_ids TEXT NOT NULL, allowed_models TEXT NOT NULL,
  max_total_tokens INTEGER, max_requests INTEGER, rate_limit_rpm INTEGER,
  expires_at TIMESTAMP, revoked_at TIMESTAMP, created_at TIMESTAMP NOT NULL, last_used_at TIMESTAMP
);
CREATE INDEX idx_tokens_user ON tokens(user_id);
CREATE TABLE usage_ledger (
  id TEXT PRIMARY KEY, token_id TEXT NOT NULL REFERENCES tokens(id), user_id TEXT NOT NULL,
  model TEXT NOT NULL, prompt_tokens INTEGER NOT NULL, completion_tokens INTEGER NOT NULL,
  cached_tokens INTEGER NOT NULL DEFAULT 0, streamed INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL, created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_ledger_token_time ON usage_ledger(token_id, created_at);
CREATE TABLE token_counters (
  token_id TEXT PRIMARY KEY REFERENCES tokens(id), total_tokens INTEGER NOT NULL DEFAULT 0,
  total_requests INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE audit_events (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL DEFAULT '', token_id TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL, payload TEXT NOT NULL, created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_audit_user ON audit_events(user_id);
`

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func schemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	version, err := readSchemaVersion(context.Background(), db)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return version
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	return count == 1
}

func TestOpenMigratesEmptyDatabaseToCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := schemaVersion(t, store.db); got != SupportedSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, SupportedSchemaVersion)
	}
	if err := validateCurrentSchema(context.Background(), store.db); err != nil {
		t.Fatalf("validate current schema: %v", err)
	}
}

func TestOpenCurrentDatabaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	now := time.Now().UTC()
	if _, err := first.db.Exec(`INSERT INTO users (id, oidc_issuer, oidc_subject, username, email, created_at, updated_at) VALUES ('u1','https://issuer.example','s1','alice','',?,?)`, now, now); err != nil {
		t.Fatalf("seed current db: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = second.Close() }()
	var username string
	if err := second.db.QueryRow(`SELECT username FROM users WHERE id = 'u1'`).Scan(&username); err != nil || username != "alice" {
		t.Fatalf("current data changed: username=%q err=%v", username, err)
	}
}

func TestOpenMigratesLegacyUnversionedDatabaseAndPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := rawDB(t, path)
	if _, err := db.Exec(legacySchemaDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, email, created_at, updated_at) VALUES ('legacy-user','legacy-sub','legacy-name','',?,?)`, now, now); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if got := schemaVersion(t, db); got != 0 {
		t.Fatalf("legacy schema version = %d, want 0", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	store, err := Open(path, OpenOptions{LegacyOIDCIssuer: "https://legacy.example"})
	if err != nil {
		t.Fatalf("migrate legacy db: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := schemaVersion(t, store.db); got != SupportedSchemaVersion {
		t.Fatalf("migrated schema version = %d, want %d", got, SupportedSchemaVersion)
	}
	var username, issuer string
	if err := store.db.QueryRow(`SELECT username, oidc_issuer FROM users WHERE id = 'legacy-user'`).Scan(&username, &issuer); err != nil || username != "legacy-name" || issuer != "https://legacy.example" {
		t.Fatalf("legacy data changed: username=%q issuer=%q err=%v", username, issuer, err)
	}
	if !tableExists(t, store.db, "metering_health") {
		t.Fatal("metering health table was not added")
	}
}

func TestVersionTwoMigrationAddsGrantSchemaAndPreservesTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-two.db")
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaMigrations[0].statements); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaMigrations[1].statements, "https://issuer.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO users (id,oidc_issuer,oidc_subject,username,email,created_at,updated_at) VALUES ('u','https://issuer.example','s','alice','',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tokens (id,user_id,token_hash,name,credential_ids,allowed_models,created_at) VALUES ('t','u','hash','legacy','[]','[]',?)`, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = migrated.Close() }()
	if got := schemaVersion(t, migrated.db); got != SupportedSchemaVersion {
		t.Fatalf("schema version = %d", got)
	}
	token, err := migrated.GetTokenByHash(context.Background(), "hash")
	if err != nil || token.IssueChannel != store.IssueChannelWeb || token.AgentGrantID != "" {
		t.Fatalf("migrated token = %+v, %v", token, err)
	}
	var tables int
	if err := migrated.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('agent_grants','agent_grant_counters')`).Scan(&tables); err != nil || tables != 2 {
		t.Fatalf("grant tables = %d, %v", tables, err)
	}
}

func TestVersionOneMigrationPreservesUsersAndForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version-one.db")
	db := rawDB(t, path)
	if _, err := db.Exec(schemaMigrations[0].statements + `PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, email, created_at, updated_at) VALUES ('u1','sub','alice','',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO credentials (id,user_id,provider,api_type,label,secret_cipher,secret_last4,disabled,created_at,updated_at) VALUES ('c1','u1','openai','openai','label',X'01','…test',0,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "requires --byok-legacy-oidc-issuer") {
		t.Fatalf("missing issuer error = %v", err)
	}
	check := rawDB(t, path)
	if got := schemaVersion(t, check); got != 1 {
		t.Fatalf("failed v2 migration recorded version %d", got)
	}
	_ = check.Close()

	migrated, err := Open(path, OpenOptions{LegacyOIDCIssuer: "https://legacy.example"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = migrated.Close() }()
	user, err := migrated.GetUserByIdentity(context.Background(), "https://legacy.example", "sub")
	if err != nil || user.ID != "u1" {
		t.Fatalf("migrated identity = %+v, %v", user, err)
	}
	if _, err := migrated.GetCredential(context.Background(), "u1", "c1"); err != nil {
		t.Fatalf("credential FK/data lost: %v", err)
	}
	var foreignKeys int
	if err := migrated.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign key enforcement after migration = %d, %v", foreignKeys, err)
	}
	if _, err := migrated.db.Exec(`INSERT INTO sessions (id,id_hash,user_id,created_at,last_seen_at,expires_at) VALUES ('bad-public','bad','missing',?,?,?)`, now, now, now.Add(time.Hour)); err == nil {
		t.Fatal("post-migration session foreign key was not enforced")
	}
}

func TestPopulatedLegacyMigrationRequiresExplicitIssuerAndRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-needs-issuer.db")
	db := rawDB(t, path)
	if _, err := db.Exec(legacySchemaDDL); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, email, created_at, updated_at) VALUES ('u1','shared-subject','alice','',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "requires --byok-legacy-oidc-issuer") {
		t.Fatalf("missing legacy issuer error = %v", err)
	}
	check := rawDB(t, path)
	if got := schemaVersion(t, check); got != 0 {
		t.Fatalf("failed identity migration recorded version %d", got)
	}
	if tableExists(t, check, "sessions") {
		t.Fatal("failed identity migration left Phase 2 tables")
	}
}

func TestOpenRejectsFutureSchemaWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db := rawDB(t, path)
	future := SupportedSchemaVersion + 1
	if _, err := db.Exec(`PRAGMA user_version = ` + strconv.Itoa(future)); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("future schema error = %v", err)
	}
	check := rawDB(t, path)
	if got := schemaVersion(t, check); got != future {
		t.Fatalf("future schema was modified: got %d want %d", got, future)
	}
}

func TestMalformedLegacySchemaRollsBackMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed-legacy.db")
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, oidc_subject TEXT NOT NULL UNIQUE, username TEXT NOT NULL)`); err != nil {
		t.Fatalf("create malformed schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("malformed legacy schema migration succeeded")
	}
	check := rawDB(t, path)
	if got := schemaVersion(t, check); got != 0 {
		t.Fatalf("failed migration recorded version %d", got)
	}
	if tableExists(t, check, "tokens") {
		t.Fatal("failed migration left newly created tables behind")
	}
}

func TestLegacySchemaWithoutSecurityConstraintsIsRejected(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"missing unique token hash": func(ddl string) string {
			return strings.Replace(ddl, "token_hash TEXT NOT NULL UNIQUE", "token_hash TEXT NOT NULL", 1)
		},
		"missing credential owner foreign key": func(ddl string) string {
			return strings.Replace(ddl, "user_id TEXT NOT NULL REFERENCES users(id), provider", "user_id TEXT NOT NULL, provider", 1)
		},
		"nullable token hash": func(ddl string) string {
			return strings.Replace(ddl, "token_hash TEXT NOT NULL UNIQUE", "token_hash TEXT UNIQUE", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "insecure-legacy.db")
			db := rawDB(t, path)
			if _, err := db.Exec(mutate(legacySchemaDDL)); err != nil {
				t.Fatalf("create insecure legacy schema: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close raw db: %v", err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("insecure legacy schema was stamped as current")
			}
			check := rawDB(t, path)
			if got := schemaVersion(t, check); got != 0 {
				t.Fatalf("rejected legacy schema recorded version %d", got)
			}
		})
	}
}

func TestInvalidMigrationPlanFailsBeforeFirstCommit(t *testing.T) {
	for name, plan := range map[string][]migration{
		"missing first migration": {{version: 2, name: "second", statements: `CREATE TABLE second (id INTEGER);`}},
		"duplicate migration": {
			{version: 1, name: "first", statements: `CREATE TABLE first (id INTEGER);`},
			{version: 1, name: "duplicate", statements: `CREATE TABLE duplicate (id INTEGER);`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid-plan.db")
			db := rawDB(t, path)
			if err := migrateSchema(context.Background(), db, plan, 2); err == nil {
				t.Fatal("invalid migration plan succeeded")
			}
			if got := schemaVersion(t, db); got != 0 {
				t.Fatalf("invalid plan recorded version %d", got)
			}
			if tableExists(t, db, "first") || tableExists(t, db, "second") || tableExists(t, db, "duplicate") {
				t.Fatal("invalid plan committed DDL before plan validation")
			}
		})
	}
}

func TestMigrationStatementFailureRollsBackDDLAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-failure.db")
	db := rawDB(t, path)
	broken := []migration{{
		version:    1,
		name:       "deliberate failure",
		statements: `CREATE TABLE should_rollback (id INTEGER); THIS IS NOT SQL;`,
	}}
	if err := migrateSchema(context.Background(), db, broken, 1); err == nil {
		t.Fatal("broken migration succeeded")
	}
	if got := schemaVersion(t, db); got != 0 {
		t.Fatalf("broken migration recorded version %d", got)
	}
	if tableExists(t, db, "should_rollback") {
		t.Fatal("broken migration left DDL behind")
	}
}

func TestMeteringHealthProbeRequiresCommittedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meter-probe.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.CheckMeteringHealth(context.Background()); err != nil {
		t.Fatalf("healthy write probe: %v", err)
	}
	if _, err := store.db.Exec(`
CREATE TRIGGER reject_meter_probe
BEFORE UPDATE ON metering_health
BEGIN
  SELECT RAISE(ABORT, 'metering writes blocked');
END;`); err != nil {
		t.Fatalf("create probe rejection trigger: %v", err)
	}
	if err := store.CheckMeteringHealth(context.Background()); err == nil || !strings.Contains(err.Error(), "write metering health probe") {
		t.Fatalf("blocked write probe error = %v", err)
	}
}

func TestCurrentVersionWithMalformedSchemaFailsValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed-current.db")
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY); PRAGMA user_version = ` + strconv.Itoa(SupportedSchemaVersion)); err != nil {
		t.Fatalf("create malformed current schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "missing required columns") {
		t.Fatalf("malformed current schema error = %v", err)
	}
}
