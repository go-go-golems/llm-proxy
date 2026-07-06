// Package memory implements the BYOK store in process memory, for tests
// and ephemeral development runs.
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

type Store struct {
	mu          sync.RWMutex
	users       map[string]store.User       // by ID
	credentials map[string]store.Credential // by ID
	tokens      map[string]store.Token      // by ID
	ledger      []store.LedgerEntry
	counters    map[string]store.Counters // by token ID
	audit       []store.AuditEvent
}

var _ store.Store = &Store{}

func New() *Store {
	return &Store{
		users:       map[string]store.User{},
		credentials: map[string]store.Credential{},
		tokens:      map[string]store.Token{},
		counters:    map[string]store.Counters{},
	}
}

func (s *Store) Close() error { return nil }

// --- UserStore ---

func (s *Store) UpsertUser(_ context.Context, user store.User) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, existing := range s.users {
		if existing.OIDCSubject == user.OIDCSubject {
			existing.Username = user.Username
			existing.Email = user.Email
			existing.UpdatedAt = now
			s.users[id] = existing
			return existing, nil
		}
	}
	if user.ID == "" {
		user.ID = store.NewID()
	}
	user.CreatedAt = now
	user.UpdatedAt = now
	s.users[user.ID] = user
	return user, nil
}

func (s *Store) GetUserBySubject(_ context.Context, subject string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.OIDCSubject == subject {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (s *Store) GetUserByUsername(_ context.Context, username string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var found *store.User
	for _, u := range s.users {
		u := u
		if u.Username == username && (found == nil || u.CreatedAt.Before(found.CreatedAt)) {
			found = &u
		}
	}
	if found == nil {
		return store.User{}, store.ErrNotFound
	}
	return *found, nil
}

func (s *Store) ListUsers(_ context.Context) ([]store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]store.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// --- CredentialStore ---

func (s *Store) CreateCredential(_ context.Context, credential store.Credential) (store.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if credential.ID == "" {
		credential.ID = store.NewID()
	}
	now := time.Now().UTC()
	credential.CreatedAt = now
	credential.UpdatedAt = now
	credential.SecretCipher = append([]byte(nil), credential.SecretCipher...)
	s.credentials[credential.ID] = credential
	return credential, nil
}

func (s *Store) GetCredential(_ context.Context, userID, credentialID string) (store.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.credentials[credentialID]
	if !ok || c.UserID != userID {
		return store.Credential{}, store.ErrNotFound
	}
	return c, nil
}

func (s *Store) ListCredentialsByUser(_ context.Context, userID string) ([]store.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Credential
	for _, c := range s.credentials {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) DeleteCredential(_ context.Context, userID, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.credentials[credentialID]
	if !ok || c.UserID != userID {
		return store.ErrNotFound
	}
	delete(s.credentials, credentialID)
	now := time.Now().UTC()
	for id, t := range s.tokens {
		if t.UserID != userID || t.RevokedAt != nil {
			continue
		}
		bound, remaining := false, 0
		for _, cid := range t.CredentialIDs {
			if cid == credentialID {
				bound = true
			} else {
				remaining++
			}
		}
		if bound && remaining == 0 {
			t.RevokedAt = &now
			s.tokens[id] = t
		}
	}
	return nil
}

// --- TokenStore ---

func (s *Store) MintToken(_ context.Context, token store.Token) (store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token.ID == "" {
		token.ID = store.NewID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	token.CredentialIDs = append([]string(nil), token.CredentialIDs...)
	token.AllowedModels = append([]string(nil), token.AllowedModels...)
	s.tokens[token.ID] = token
	return token, nil
}

func (s *Store) GetTokenByHash(_ context.Context, tokenHash string) (store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tokens {
		if t.TokenHash == tokenHash {
			return t, nil
		}
	}
	return store.Token{}, store.ErrNotFound
}

func (s *Store) ListTokensByUser(_ context.Context, userID string) ([]store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Token
	for _, t := range s.tokens {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) RevokeToken(_ context.Context, userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[tokenID]
	if !ok || t.UserID != userID || t.RevokedAt != nil {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	s.tokens[tokenID] = t
	return nil
}

func (s *Store) TouchTokenUsed(_ context.Context, tokenID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tokens[tokenID]; ok {
		at := at.UTC()
		t.LastUsedAt = &at
		s.tokens[tokenID] = t
	}
	return nil
}

// --- MeterStore ---

func (s *Store) RecordUsage(_ context.Context, entry store.LedgerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ID == "" {
		entry.ID = store.NewID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	s.ledger = append(s.ledger, entry)
	if entry.Status != store.LedgerStatusRejected {
		c := s.counters[entry.TokenID]
		c.TokenID = entry.TokenID
		c.TotalTokens += entry.PromptTokens + entry.CompletionTokens
		c.TotalRequests++
		s.counters[entry.TokenID] = c
	}
	return nil
}

func (s *Store) GetCounters(_ context.Context, tokenID string) (store.Counters, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.counters[tokenID]; ok {
		return c, nil
	}
	return store.Counters{TokenID: tokenID}, nil
}

func (s *Store) ListLedger(_ context.Context, tokenID string, since time.Time, limit int) ([]store.LedgerEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	var out []store.LedgerEntry
	for _, e := range s.ledger {
		if e.TokenID == tokenID && !e.CreatedAt.Before(since) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- AuditStore ---

func (s *Store) AppendEvent(_ context.Context, event store.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.ID == "" {
		event.ID = store.NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if len(event.Payload) == 0 {
		event.Payload = []byte("{}")
	}
	s.audit = append(s.audit, event)
	return nil
}

func (s *Store) ListEvents(_ context.Context, filter store.AuditFilter) ([]store.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	var out []store.AuditEvent
	for _, e := range s.audit {
		if filter.UserID != "" && e.UserID != filter.UserID {
			continue
		}
		if filter.TokenID != "" && e.TokenID != filter.TokenID {
			continue
		}
		if filter.EventType != "" && !strings.EqualFold(e.EventType, filter.EventType) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
