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
var ErrNotFound = errors.New("byok/store: not found")

type UserStore interface {
	// UpsertUser inserts or updates by OIDCSubject and returns the stored row.
	UpsertUser(ctx context.Context, user User) (User, error)
	GetUserBySubject(ctx context.Context, subject string) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
}

type CredentialStore interface {
	CreateCredential(ctx context.Context, credential Credential) (Credential, error)
	GetCredential(ctx context.Context, userID, credentialID string) (Credential, error)
	ListCredentialsByUser(ctx context.Context, userID string) ([]Credential, error)
	// DeleteCredential removes the credential and revokes every token whose
	// credential bindings become empty as a result.
	DeleteCredential(ctx context.Context, userID, credentialID string) error
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
}

type AuditStore interface {
	AppendEvent(ctx context.Context, event AuditEvent) error
	ListEvents(ctx context.Context, filter AuditFilter) ([]AuditEvent, error)
}

type Store interface {
	UserStore
	CredentialStore
	TokenStore
	MeterStore
	AuditStore
	Close() error
}
