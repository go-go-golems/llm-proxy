package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// requireSession wraps API handlers with session-cookie auth plus a
// same-origin check on mutating requests (CSRF defense: SameSite=Lax
// already blocks cross-site POSTs in modern browsers; the Origin check
// covers the rest).
func (s *Server) requireSession(next func(http.ResponseWriter, *http.Request, SessionClaims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.sessions.ClaimsFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if r.Method != http.MethodGet && !s.sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross-origin request rejected")
			return
		}
		next(w, r, claims)
	}
}

func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client (curl, tests)
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if s.publicURL != nil && strings.EqualFold(parsed.Host, s.publicURL.Host) {
		return true
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// --- credentials ---

type credentialOut struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	APIType     string    `json:"api_type"`
	Label       string    `json:"label"`
	SecretLast4 string    `json:"secret_last4"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func credentialToOut(c store.Credential) credentialOut {
	return credentialOut{
		ID: c.ID, Provider: c.Provider, APIType: c.APIType, Label: c.Label,
		SecretLast4: c.SecretLast4, Disabled: c.Disabled, CreatedAt: c.CreatedAt,
	}
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	creds, err := s.store.ListCredentialsByUser(r.Context(), claims.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list credentials failed")
		return
	}
	out := make([]credentialOut, 0, len(creds))
	for _, c := range creds {
		out = append(out, credentialToOut(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	var req struct {
		Provider string `json:"provider"`
		APIType  string `json:"api_type"`
		Label    string `json:"label"`
		Secret   string `json:"secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Provider == "" || req.APIType == "" || req.Secret == "" {
		writeErr(w, http.StatusBadRequest, "provider, api_type, and secret are required")
		return
	}
	if req.Label == "" {
		req.Label = req.Provider
	}
	credID := store.NewID()
	cipherBlob, err := s.vault.Encrypt(credID, []byte(req.Secret))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encryption failed")
		return
	}
	cred, err := s.store.CreateCredential(r.Context(), store.Credential{
		ID: credID, UserID: claims.UserID, Provider: req.Provider, APIType: req.APIType,
		Label: req.Label, SecretCipher: cipherBlob, SecretLast4: vault.Last4(req.Secret),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store credential failed")
		return
	}
	s.audit(r, claims, "credential.created", fmt.Sprintf(`{"credential_id":%q,"provider":%q}`, cred.ID, cred.Provider))
	writeJSON(w, http.StatusCreated, credentialToOut(cred))
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	id := r.PathValue("id")
	if err := s.store.DeleteCredential(r.Context(), claims.UserID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "credential not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.audit(r, claims, "credential.deleted", fmt.Sprintf(`{"credential_id":%q}`, id))
	w.WriteHeader(http.StatusNoContent)
}

// --- tokens ---

type tokenOut struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	CredentialIDs []string   `json:"credential_ids"`
	AllowedModels []string   `json:"allowed_models"`
	MaxTokens     *int64     `json:"max_total_tokens,omitempty"`
	MaxRequests   *int64     `json:"max_requests,omitempty"`
	RateLimitRPM  *int64     `json:"rate_limit_rpm,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UsedTokens    int64      `json:"used_tokens"`
	UsedRequests  int64      `json:"used_requests"`
	Token         string     `json:"token,omitempty"` // set ONLY in the mint response
}

func (s *Server) tokenToOut(r *http.Request, t store.Token) tokenOut {
	counters, err := s.store.GetCounters(r.Context(), t.ID)
	if err != nil {
		counters = store.Counters{}
	}
	return tokenOut{
		ID: t.ID, Name: t.Name, CredentialIDs: t.CredentialIDs, AllowedModels: t.AllowedModels,
		MaxTokens: t.MaxTotalTokens, MaxRequests: t.MaxRequests, RateLimitRPM: t.RateLimitRPM,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt,
		UsedTokens: counters.TotalTokens, UsedRequests: counters.TotalRequests,
	}
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	toks, err := s.store.ListTokensByUser(r.Context(), claims.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list tokens failed")
		return
	}
	out := make([]tokenOut, 0, len(toks))
	for _, t := range toks {
		out = append(out, s.tokenToOut(r, t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMintToken(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	var req struct {
		Name          string   `json:"name"`
		CredentialIDs []string `json:"credential_ids"`
		AllowedModels []string `json:"allowed_models"`
		MaxTokens     int64    `json:"max_total_tokens"`
		MaxRequests   int64    `json:"max_requests"`
		RateLimitRPM  int64    `json:"rate_limit_rpm"`
		ExpiresDays   int      `json:"expires_in_days"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || len(req.AllowedModels) == 0 || len(req.CredentialIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "name, allowed_models, and credential_ids are required")
		return
	}
	for _, id := range req.CredentialIDs {
		cred, err := s.store.GetCredential(r.Context(), claims.UserID, id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("credential %s not found", id))
			return
		}
		if cred.Disabled {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("credential %s is disabled", id))
			return
		}
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mint failed")
		return
	}
	tok := store.Token{
		UserID: claims.UserID, TokenHash: hash, Name: req.Name,
		CredentialIDs: req.CredentialIDs, AllowedModels: req.AllowedModels,
	}
	if req.MaxTokens > 0 {
		tok.MaxTotalTokens = &req.MaxTokens
	}
	if req.MaxRequests > 0 {
		tok.MaxRequests = &req.MaxRequests
	}
	if req.RateLimitRPM > 0 {
		tok.RateLimitRPM = &req.RateLimitRPM
	}
	if req.ExpiresDays > 0 {
		exp := time.Now().UTC().Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
		tok.ExpiresAt = &exp
	}
	minted, err := s.store.MintToken(r.Context(), tok)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mint failed")
		return
	}
	s.audit(r, claims, "token.minted", fmt.Sprintf(`{"token_id":%q,"name":%q}`, minted.ID, req.Name))
	out := s.tokenToOut(r, minted)
	out.Token = raw // the only place the plaintext ever appears
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	id := r.PathValue("id")
	if err := s.store.RevokeToken(r.Context(), claims.UserID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "token not found or already revoked")
			return
		}
		writeErr(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	s.audit(r, claims, "token.revoked", fmt.Sprintf(`{"token_id":%q}`, id))
	w.WriteHeader(http.StatusNoContent)
}

// --- usage ---

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request, claims SessionClaims) {
	tokenID := r.URL.Query().Get("token_id")
	if tokenID == "" {
		writeErr(w, http.StatusBadRequest, "token_id is required")
		return
	}
	// Ownership check: the ledger is per token; only the owner may read it.
	owned := false
	toks, err := s.store.ListTokensByUser(r.Context(), claims.UserID)
	if err == nil {
		for _, t := range toks {
			if t.ID == tokenID {
				owned = true
				break
			}
		}
	}
	if !owned {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	since := time.Time{}
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "since must be RFC3339")
			return
		}
		since = parsed
	}
	entries, err := s.store.ListLedger(r.Context(), tokenID, since, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ledger read failed")
		return
	}
	type entryOut struct {
		Model            string    `json:"model"`
		PromptTokens     int64     `json:"prompt_tokens"`
		CompletionTokens int64     `json:"completion_tokens"`
		CachedTokens     int64     `json:"cached_tokens"`
		Streamed         bool      `json:"streamed"`
		Status           string    `json:"status"`
		CreatedAt        time.Time `json:"created_at"`
	}
	out := make([]entryOut, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryOut{
			Model: e.Model, PromptTokens: e.PromptTokens, CompletionTokens: e.CompletionTokens,
			CachedTokens: e.CachedTokens, Streamed: e.Streamed, Status: e.Status, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) audit(r *http.Request, claims SessionClaims, eventType, payload string) {
	if err := s.store.AppendEvent(r.Context(), store.AuditEvent{
		UserID: claims.UserID, EventType: eventType, Payload: []byte(payload),
	}); err != nil {
		log.Warn().Err(err).Str("event", eventType).Msg("byok/web: audit append failed")
	}
}
