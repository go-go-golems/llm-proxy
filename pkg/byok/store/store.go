// Package store defines the BYOK persistence layer: users, the credential
// vault, minted tokens, the usage ledger, and audit events. Two backends
// implement Store: sqlite (production) and memory (tests).
package store

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// ErrNotFound is returned when a lookup matches no row.
var (
	ErrNotFound       = errors.New("byok/store: not found")
	ErrGrantExhausted = errors.New("byok/store: agent grant budget exhausted")
	ErrInvalid        = errors.New("byok/store: invalid input")
)

type UserStore interface {
	// UpsertUser inserts or updates by exact (OIDCIssuer, OIDCSubject).
	UpsertUser(ctx context.Context, user User) (User, error)
	GetUserByID(ctx context.Context, userID string) (User, error)
	GetUserByIdentity(ctx context.Context, issuer, subject string) (User, error)
	// GetUserBySubject is legacy operator convenience only. OIDC authorization
	// must use GetUserByIdentity because subject is issuer-relative.
	GetUserBySubject(ctx context.Context, subject string) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
}

type AuthTransactionStore interface {
	CreateAuthTransaction(ctx context.Context, transaction AuthTransaction) error
	// ConsumeAuthTransaction atomically verifies both hashes and expiry, then
	// marks the record consumed before returning it.
	ConsumeAuthTransaction(ctx context.Context, idHash, stateHash string, now time.Time) (AuthTransaction, error)
}

type SessionStore interface {
	CreateSession(ctx context.Context, session Session) error
	// UseSession atomically rejects revoked, absolute-expired, or idle-expired
	// sessions and advances LastSeenAt for an accepted request.
	UseSession(ctx context.Context, idHash string, now, idleCutoff time.Time) (Session, error)
	ListSessionsByUser(ctx context.Context, userID string) ([]Session, error)
	RevokeSession(ctx context.Context, idHash string, at time.Time) error
	RevokeSessionByID(ctx context.Context, userID, sessionID string, at time.Time) error
}

type CredentialStore interface {
	CreateCredential(ctx context.Context, credential Credential) (Credential, error)
	GetCredential(ctx context.Context, userID, credentialID string) (Credential, error)
	ListCredentialsByUser(ctx context.Context, userID string) ([]Credential, error)
	// DeleteCredential removes the credential and revokes every token whose
	// credential bindings become empty as a result.
	DeleteCredential(ctx context.Context, userID, credentialID string) error
}

type AgentGrantStore interface {
	GetAgentGrant(ctx context.Context, userID, grantID string) (AgentGrant, error)
	ListAgentGrantsByUser(ctx context.Context, userID string) ([]AgentGrant, error)
	GetAgentGrantCounters(ctx context.Context, grantID string) (AgentGrantCounters, error)
}

type TokenStore interface {
	MintToken(ctx context.Context, token Token) (Token, error)
	GetTokenByHash(ctx context.Context, tokenHash string) (Token, error)
	ListTokensByUser(ctx context.Context, userID string) ([]Token, error)
	RevokeToken(ctx context.Context, userID, tokenID string) error
	TouchTokenUsed(ctx context.Context, tokenID string, at time.Time) error
}

type MeterStore interface {
	// RecordUsage appends a ledger row and, unless the row is rejected,
	// bumps the token counters in the same transaction.
	RecordUsage(ctx context.Context, entry LedgerEntry) error
	GetCounters(ctx context.Context, tokenID string) (Counters, error)
	ListLedger(ctx context.Context, tokenID string, since time.Time, limit int) ([]LedgerEntry, error)
	// CheckMeteringHealth performs a committed write probe. A read-only ping is
	// insufficient because metering must fail closed on disk-full/read-only
	// failures that still allow reads.
	CheckMeteringHealth(ctx context.Context) error
}

// LifecycleStore exposes the atomic security mutations used by production
// control-plane and operator paths. The domain change and its typed audit event
// either commit together or neither commits. Lower-level CRUD methods remain
// available for fixtures and internal composition, but security-sensitive
// callers must use these methods.
type LifecycleStore interface {
	CreateCredentialAudited(ctx context.Context, credential Credential) (Credential, error)
	DeleteCredentialAudited(ctx context.Context, userID, credentialID string) error
	MintTokenAudited(ctx context.Context, token Token) (Token, error)
	RevokeTokenAudited(ctx context.Context, userID, tokenID string) error
	CreateAgentGrantAudited(ctx context.Context, grant AgentGrant) (AgentGrant, error)
	UpdateAgentGrantAudited(ctx context.Context, grant AgentGrant) (AgentGrant, error)
	RevokeAgentGrantAudited(ctx context.Context, userID, grantID string) error
	// IssueAgentTokenAudited derives all capability policy from the grant,
	// rotates excess tokens for the same client instance, and audits atomically.
	IssueAgentTokenAudited(ctx context.Context, token Token) (Token, error)
}

type AuditStore interface {
	AppendEvent(ctx context.Context, event AuditEvent) error
	ListEvents(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)
}

type Store interface {
	UserStore
	AuthTransactionStore
	SessionStore
	CredentialStore
	AgentGrantStore
	TokenStore
	MeterStore
	AuditStore
	LifecycleStore
	Close() error
}
