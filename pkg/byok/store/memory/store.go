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
	"github.com/pkg/errors"
)

type Store struct {
	mu               sync.RWMutex
	users            map[string]store.User            // by ID
	authTransactions map[string]store.AuthTransaction // by ID hash
	sessions         map[string]store.Session         // by ID hash
	credentials      map[string]store.Credential      // by ID
	agentGrants      map[string]store.AgentGrant      // by ID
	grantCounters    map[string]store.AgentGrantCounters
	tokens           map[string]store.Token // by ID
	ledger           []store.LedgerEntry
	counters         map[string]store.Counters // by token ID
	audit            []store.AuditEvent
}

var _ store.Store = &Store{}

func New() *Store {
	return &Store{
		users:            map[string]store.User{},
		authTransactions: map[string]store.AuthTransaction{},
		sessions:         map[string]store.Session{},
		credentials:      map[string]store.Credential{},
		agentGrants:      map[string]store.AgentGrant{},
		grantCounters:    map[string]store.AgentGrantCounters{},
		tokens:           map[string]store.Token{},
		counters:         map[string]store.Counters{},
	}
}

func (s *Store) Close() error { return nil }

// --- UserStore ---

func (s *Store) UpsertUser(_ context.Context, user store.User) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, existing := range s.users {
		if existing.OIDCIssuer == user.OIDCIssuer && existing.OIDCSubject == user.OIDCSubject {
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

func (s *Store) GetUserByID(_ context.Context, userID string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[userID]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

func (s *Store) GetUserByIdentity(_ context.Context, issuer, subject string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.OIDCIssuer == issuer && u.OIDCSubject == subject {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
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

// --- AuthTransactionStore and SessionStore ---

func (s *Store) CreateAuthTransaction(_ context.Context, transaction store.AuthTransaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idHash, existing := range s.authTransactions {
		if !transaction.CreatedAt.Before(existing.ExpiresAt) || existing.ConsumedAt != nil {
			delete(s.authTransactions, idHash)
		}
	}
	if transaction.IDHash == "" || transaction.StateHash == "" {
		return errors.New("auth transaction hashes are required")
	}
	if _, exists := s.authTransactions[transaction.IDHash]; exists {
		return errors.New("auth transaction already exists")
	}
	for _, existing := range s.authTransactions {
		if existing.StateHash == transaction.StateHash {
			return errors.New("auth transaction state already exists")
		}
	}
	s.authTransactions[transaction.IDHash] = transaction
	s.appendEventLocked(store.AuthTransactionEvent(store.AuditAuthTransactionCreated, transaction.CreatedAt))
	return nil
}

func (s *Store) ConsumeAuthTransaction(_ context.Context, idHash, stateHash string, now time.Time) (store.AuthTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	transaction, ok := s.authTransactions[idHash]
	if !ok || transaction.StateHash != stateHash || transaction.ConsumedAt != nil || !now.Before(transaction.ExpiresAt) {
		return store.AuthTransaction{}, store.ErrNotFound
	}
	transaction.ConsumedAt = &now
	s.appendEventLocked(store.AuthTransactionEvent(store.AuditAuthTransactionConsumed, now))
	delete(s.authTransactions, idHash)
	return transaction, nil
}

func (s *Store) CreateSession(_ context.Context, session store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idHash, existing := range s.sessions {
		if !session.CreatedAt.Before(existing.ExpiresAt) {
			delete(s.sessions, idHash)
		}
	}
	if session.ID == "" {
		session.ID = store.NewID()
	}
	if session.IDHash == "" {
		return errors.New("session ID hash is required")
	}
	if _, exists := s.sessions[session.IDHash]; exists {
		return errors.New("session already exists")
	}
	s.sessions[session.IDHash] = session
	s.appendEventLocked(store.SessionEvent(session.UserID, store.AuditSessionCreated, session.CreatedAt))
	return nil
}

func (s *Store) UseSession(_ context.Context, idHash string, now, idleCutoff time.Time) (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[idHash]
	if !ok || session.RevokedAt != nil || !now.Before(session.ExpiresAt) || session.LastSeenAt.Before(idleCutoff) {
		return store.Session{}, store.ErrNotFound
	}
	session.LastSeenAt = now
	s.sessions[idHash] = session
	return session, nil
}

func (s *Store) ListSessionsByUser(_ context.Context, userID string) ([]store.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]store.Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) RevokeSession(_ context.Context, idHash string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[idHash]
	if !ok {
		return store.ErrNotFound
	}
	session.RevokedAt = &at
	s.sessions[idHash] = session
	s.appendEventLocked(store.SessionEvent(session.UserID, store.AuditSessionRevoked, at))
	return nil
}

func (s *Store) RevokeSessionByID(_ context.Context, userID, sessionID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idHash, session := range s.sessions {
		if session.ID == sessionID && session.UserID == userID && session.RevokedAt == nil {
			session.RevokedAt = &at
			s.sessions[idHash] = session
			s.appendEventLocked(store.SessionEvent(userID, store.AuditSessionRevoked, at))
			return nil
		}
	}
	return store.ErrNotFound
}

// --- CredentialStore ---

func (s *Store) CreateCredential(_ context.Context, credential store.Credential) (store.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createCredentialLocked(credential), nil
}

func (s *Store) CreateCredentialAudited(_ context.Context, credential store.Credential) (store.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	created := s.createCredentialLocked(credential)
	s.appendEventLocked(store.CredentialCreatedEvent(created))
	return created, nil
}

func (s *Store) createCredentialLocked(credential store.Credential) store.Credential {
	if credential.ID == "" {
		credential.ID = store.NewID()
	}
	now := time.Now().UTC()
	credential.CreatedAt = now
	credential.UpdatedAt = now
	credential.SecretCipher = append([]byte(nil), credential.SecretCipher...)
	s.credentials[credential.ID] = credential
	return credential
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
	_, _, err := s.deleteCredentialLocked(userID, credentialID)
	return err
}

func (s *Store) DeleteCredentialAudited(_ context.Context, userID, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	revokedTokens, revokedGrants, err := s.deleteCredentialLocked(userID, credentialID)
	if err != nil {
		return err
	}
	for _, grantID := range revokedGrants {
		s.appendEventLocked(store.AgentGrantEvent(s.agentGrants[grantID], store.AuditAgentGrantRevoked))
	}
	for _, tokenID := range revokedTokens {
		s.appendEventLocked(store.TokenRevokedEvent(userID, tokenID))
	}
	s.appendEventLocked(store.CredentialDeletedEvent(userID, credentialID))
	return nil
}

func (s *Store) deleteCredentialLocked(userID, credentialID string) ([]string, []string, error) {
	c, ok := s.credentials[credentialID]
	if !ok || c.UserID != userID {
		return nil, nil, store.ErrNotFound
	}
	delete(s.credentials, credentialID)
	now := time.Now().UTC()
	revokedTokenSet := map[string]struct{}{}
	var revokedGrants []string
	for grantID, grant := range s.agentGrants {
		if grant.UserID != userID || grant.RevokedAt != nil {
			continue
		}
		for _, id := range grant.CredentialIDs {
			if id == credentialID {
				grant.Enabled, grant.RevokedAt, grant.UpdatedAt = false, &now, now
				s.agentGrants[grantID] = grant
				revokedGrants = append(revokedGrants, grantID)
				break
			}
		}
	}
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
		grant, grantExists := s.agentGrants[t.AgentGrantID]
		grantRevoked := t.AgentGrantID != "" && grantExists && grant.RevokedAt != nil
		if (bound && remaining == 0) || grantRevoked {
			t.RevokedAt = &now
			s.tokens[id] = t
			revokedTokenSet[id] = struct{}{}
		}
	}
	revokedTokens := make([]string, 0, len(revokedTokenSet))
	for id := range revokedTokenSet {
		revokedTokens = append(revokedTokens, id)
	}
	return revokedTokens, revokedGrants, nil
}

// --- AgentGrantStore ---

func (s *Store) CreateAgentGrantAudited(_ context.Context, grant store.AgentGrant) (store.AgentGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateAgentGrantLocked(grant); err != nil {
		return store.AgentGrant{}, err
	}
	if grant.ID == "" {
		grant.ID = store.NewID()
	}
	if _, exists := s.agentGrants[grant.ID]; exists {
		return store.AgentGrant{}, errors.New("agent grant already exists")
	}
	now := time.Now().UTC()
	grant.CreatedAt, grant.UpdatedAt = now, now
	grant.Enabled = true
	grant.CredentialIDs = append([]string(nil), grant.CredentialIDs...)
	grant.AllowedModels = append([]string(nil), grant.AllowedModels...)
	s.agentGrants[grant.ID] = grant
	s.grantCounters[grant.ID] = store.AgentGrantCounters{GrantID: grant.ID}
	s.appendEventLocked(store.AgentGrantEvent(grant, store.AuditAgentGrantCreated))
	return grant, nil
}

func (s *Store) UpdateAgentGrantAudited(_ context.Context, grant store.AgentGrant) (store.AgentGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.agentGrants[grant.ID]
	if !ok || existing.UserID != grant.UserID || existing.RevokedAt != nil {
		return store.AgentGrant{}, store.ErrNotFound
	}
	if err := s.validateAgentGrantLocked(grant); err != nil {
		return store.AgentGrant{}, err
	}
	grant.CreatedAt = existing.CreatedAt
	grant.UpdatedAt = time.Now().UTC()
	grant.Enabled = true
	grant.CredentialIDs = append([]string(nil), grant.CredentialIDs...)
	grant.AllowedModels = append([]string(nil), grant.AllowedModels...)
	s.agentGrants[grant.ID] = grant
	s.appendEventLocked(store.AgentGrantEvent(grant, store.AuditAgentGrantUpdated))
	for id, token := range s.tokens {
		if token.AgentGrantID == grant.ID && token.RevokedAt == nil {
			now := grant.UpdatedAt
			token.RevokedAt = &now
			s.tokens[id] = token
			s.appendEventLocked(store.TokenRevokedEvent(grant.UserID, id))
		}
	}
	return grant, nil
}

func (s *Store) validateAgentGrantLocked(grant store.AgentGrant) error {
	if err := store.ValidateAgentGrantPolicy(grant); err != nil {
		return err
	}
	for _, id := range grant.CredentialIDs {
		credential, ok := s.credentials[id]
		if !ok || credential.UserID != grant.UserID || credential.Disabled {
			return errors.New("agent grant credential is unavailable")
		}
	}
	return nil
}

func (s *Store) GetAgentGrant(_ context.Context, userID, grantID string) (store.AgentGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	grant, ok := s.agentGrants[grantID]
	if !ok || grant.UserID != userID {
		return store.AgentGrant{}, store.ErrNotFound
	}
	return grant, nil
}

func (s *Store) ListAgentGrantsByUser(_ context.Context, userID string) ([]store.AgentGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.AgentGrant
	for _, grant := range s.agentGrants {
		if grant.UserID == userID {
			out = append(out, grant)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) GetAgentGrantCounters(_ context.Context, grantID string) (store.AgentGrantCounters, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.agentGrants[grantID]; !ok {
		return store.AgentGrantCounters{}, store.ErrNotFound
	}
	return s.grantCounters[grantID], nil
}

func (s *Store) RevokeAgentGrantAudited(_ context.Context, userID, grantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.agentGrants[grantID]
	if !ok || grant.UserID != userID || grant.RevokedAt != nil {
		return store.ErrNotFound
	}
	now := time.Now().UTC()
	grant.Enabled, grant.RevokedAt, grant.UpdatedAt = false, &now, now
	s.agentGrants[grantID] = grant
	s.appendEventLocked(store.AgentGrantEvent(grant, store.AuditAgentGrantRevoked))
	for id, token := range s.tokens {
		if token.AgentGrantID == grantID && token.RevokedAt == nil {
			token.RevokedAt = &now
			s.tokens[id] = token
			s.appendEventLocked(store.TokenRevokedEvent(userID, id))
		}
	}
	return nil
}

func (s *Store) IssueAgentTokenAudited(_ context.Context, token store.Token) (store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := store.ValidateAgentTokenProvenance(token); err != nil {
		return store.Token{}, err
	}
	grant, ok := s.agentGrants[token.AgentGrantID]
	if !ok || grant.UserID != token.UserID || !grant.Enabled || grant.RevokedAt != nil {
		return store.Token{}, store.ErrNotFound
	}
	counters := s.grantCounters[grant.ID]
	if grant.GrantMaxTokens != nil && counters.TotalTokens >= *grant.GrantMaxTokens || grant.GrantMaxRequests != nil && counters.TotalRequests >= *grant.GrantMaxRequests {
		return store.Token{}, store.ErrGrantExhausted
	}
	now := time.Now().UTC()
	token.ID, token.Name, token.CreatedAt = store.NewID(), grant.Name, now
	expires := now.Add(grant.TokenTTL)
	token.ExpiresAt = &expires
	token.CredentialIDs = append([]string(nil), grant.CredentialIDs...)
	token.AllowedModels = append([]string(nil), grant.AllowedModels...)
	token.MaxTotalTokens, token.MaxRequests, token.RateLimitRPM = grant.PerTokenMaxTokens, grant.PerTokenMaxRequests, grant.RateLimitRPM
	var active []store.Token
	for _, existing := range s.tokens {
		if existing.UserID == token.UserID && existing.AgentGrantID == grant.ID && existing.SourceClientID == token.SourceClientID && existing.ClientInstanceID == token.ClientInstanceID && existing.RevokedAt == nil && (existing.ExpiresAt == nil || existing.ExpiresAt.After(now)) {
			active = append(active, existing)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].CreatedAt.Before(active[j].CreatedAt) })
	rotateCount := max(0, len(active)-grant.MaxActivePerInstance+1)
	for i := 0; i < rotateCount; i++ {
		existing := active[i]
		existing.RevokedAt = &now
		s.tokens[existing.ID] = existing
		s.appendEventLocked(store.TokenEvent(existing, store.AuditTokenRotated))
	}
	s.tokens[token.ID] = token
	s.appendEventLocked(store.TokenEvent(token, store.AuditDeviceTokenIssued))
	return token, nil
}

// --- TokenStore ---

func (s *Store) MintToken(_ context.Context, token store.Token) (store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mintTokenLocked(token), nil
}

func (s *Store) MintTokenAudited(_ context.Context, token store.Token) (store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	minted := s.mintTokenLocked(token)
	s.appendEventLocked(store.TokenMintedEvent(minted))
	return minted, nil
}

func (s *Store) mintTokenLocked(token store.Token) store.Token {
	if token.ID == "" {
		token.ID = store.NewID()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	if token.IssueChannel == "" {
		token.IssueChannel = store.IssueChannelWeb
	}
	token.CredentialIDs = append([]string(nil), token.CredentialIDs...)
	token.AllowedModels = append([]string(nil), token.AllowedModels...)
	s.tokens[token.ID] = token
	return token
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
	return s.revokeTokenLocked(userID, tokenID)
}

func (s *Store) RevokeTokenAudited(_ context.Context, userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.revokeTokenLocked(userID, tokenID); err != nil {
		return err
	}
	s.appendEventLocked(store.TokenRevokedEvent(userID, tokenID))
	return nil
}

func (s *Store) revokeTokenLocked(userID, tokenID string) error {
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
		if token, ok := s.tokens[entry.TokenID]; ok && token.AgentGrantID != "" {
			grantCounters := s.grantCounters[token.AgentGrantID]
			grantCounters.GrantID = token.AgentGrantID
			grantCounters.TotalTokens += entry.PromptTokens + entry.CompletionTokens
			grantCounters.TotalRequests++
			s.grantCounters[token.AgentGrantID] = grantCounters
		}
	}
	return nil
}

func (s *Store) CheckMeteringHealth(context.Context) error { return nil }

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
	s.appendEventLocked(event)
	return nil
}

func (s *Store) appendEventLocked(event store.AuditEvent) {
	if event.ID == "" {
		event.ID = store.NewID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if len(event.Payload) == 0 {
		event.Payload = []byte("{}")
	}
	event.Payload = append([]byte(nil), event.Payload...)
	s.audit = append(s.audit, event)
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
