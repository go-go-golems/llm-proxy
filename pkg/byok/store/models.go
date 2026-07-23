package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// User is a control-plane account, provisioned on first OIDC login or via CLI.
type User struct {
	ID          string
	OIDCIssuer  string
	OIDCSubject string
	Username    string
	Email       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AuthTransaction is a short-lived, one-time browser authorization record.
// IDHash and StateHash are SHA-256 hashes; the browser values are never stored.
type AuthTransaction struct {
	IDHash       string
	StateHash    string
	Nonce        string
	PKCEVerifier string
	ReturnTo     string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
}

// Session is a revocable server-side browser session. IDHash is the SHA-256
// hash of the opaque cookie identifier.
type Session struct {
	ID         string
	IDHash     string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

// Credential is a vault entry: a provider API key stored encrypted.
// SecretCipher is opaque to the store; encryption happens in pkg/byok/vault.
type Credential struct {
	ID           string
	UserID       string
	Provider     string // e.g. "anthropic", "openai"
	APIType      string // geppetto api-type, e.g. "claude", "openai"
	Label        string
	SecretCipher []byte
	SecretLast4  string
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AgentGrant is a browser-approved policy and cumulative budget boundary for
// device-issued capability tokens.
type AgentGrant struct {
	ID                   string
	UserID               string
	Name                 string
	CredentialIDs        []string
	AllowedModels        []string
	PerTokenMaxTokens    *int64
	PerTokenMaxRequests  *int64
	RateLimitRPM         *int64
	TokenTTL             time.Duration
	MaxActivePerInstance int
	GrantMaxTokens       *int64
	GrantMaxRequests     *int64
	Enabled              bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
	RevokedAt            *time.Time
}

// AgentGrantCounters are cumulative across every token ever issued from a
// grant. Rotation or reissuance never resets these values.
type AgentGrantCounters struct {
	GrantID       string
	TotalTokens   int64
	TotalRequests int64
}

type IssueChannel string

const (
	IssueChannelWeb    IssueChannel = "web"
	IssueChannelCLI    IssueChannel = "operator_cli"
	IssueChannelDevice IssueChannel = "device_exchange"
)

// Token is a minted bearer token. Only the SHA-256 hash of the secret is stored.
type Token struct {
	ID               string
	UserID           string
	TokenHash        string
	Name             string
	CredentialIDs    []string
	AllowedModels    []string // profile slugs or globs; empty = nothing allowed
	AgentGrantID     string
	IssueChannel     IssueChannel
	SourceClientID   string
	ClientInstanceID string
	MaxTotalTokens   *int64 // nil = unlimited
	MaxRequests      *int64 // nil = unlimited
	RateLimitRPM     *int64 // nil = unlimited
	ExpiresAt        *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	LastUsedAt       *time.Time
}

// LedgerEntry is one row of the append-only usage ledger.
type LedgerEntry struct {
	ID               string
	TokenID          string
	UserID           string
	Model            string
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	Streamed         bool
	Status           string // "ok" | "error" | "rejected"
	CreatedAt        time.Time
}

const (
	LedgerStatusOK       = "ok"
	LedgerStatusError    = "error"
	LedgerStatusRejected = "rejected"
)

// Counters are denormalized running totals per token, kept in the same
// transaction as ledger inserts so budget checks stay O(1).
type Counters struct {
	TokenID       string
	TotalTokens   int64
	TotalRequests int64
}

// AuditEvent records a control-plane or data-plane decision.
// Payload is JSON and must never contain plaintext secrets.
type AuditEvent struct {
	ID        string
	UserID    string
	TokenID   string
	EventType string
	Payload   []byte
	CreatedAt time.Time
}

type AuditFilter struct {
	UserID    string
	TokenID   string
	EventType string
	Limit     int
}

// NewID returns a 32-char random hex identifier.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b[:])
}
