// Package agentapi exposes the tiny-idp-authenticated coding-agent acquisition
// plane. It never accepts broker llmp capabilities as identity tokens.
package agentapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/oidcauth"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/pkg/errors"
)

const issueScope = "llm.tokens.issue"

type Authenticator interface {
	Authenticate(ctx context.Context, authorizationHeaders []string, requiredScopes []string) (oidcauth.Principal, *oidcauth.Failure)
}

type Server struct {
	store   store.Store
	auth    Authenticator
	issueMu sync.Mutex
}

func New(st store.Store, auth Authenticator) (*Server, error) {
	if st == nil || auth == nil {
		return nil, errors.New("agent API store and authenticator are required")
	}
	return &Server{store: st, auth: auth}, nil
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent/v1/grants", s.handleListGrants)
	mux.HandleFunc("POST /agent/v1/tokens", s.handleIssueToken)
}

type grantOut struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	AllowedModels        []string `json:"allowed_models"`
	TokenTTLSeconds      int64    `json:"token_ttl_seconds"`
	MaxActivePerInstance int      `json:"max_active_per_instance"`
	PerTokenMaxTokens    *int64   `json:"per_token_max_total_tokens,omitempty"`
	PerTokenMaxRequests  *int64   `json:"per_token_max_requests,omitempty"`
	RateLimitRPM         *int64   `json:"rate_limit_rpm,omitempty"`
	GrantMaxTokens       *int64   `json:"grant_max_total_tokens,omitempty"`
	GrantMaxRequests     *int64   `json:"grant_max_requests,omitempty"`
	UsedTokens           int64    `json:"used_tokens"`
	UsedRequests         int64    `json:"used_requests"`
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	principal, failure := s.auth.Authenticate(r.Context(), r.Header.Values("Authorization"), []string{issueScope})
	if failure != nil {
		writeFailure(w, failure)
		return
	}
	user, err := s.store.GetUserByIdentity(r.Context(), principal.Issuer, principal.Subject)
	if errors.Is(err, store.ErrNotFound) {
		writeFailure(w, &oidcauth.Failure{Status: http.StatusUnauthorized, Code: "invalid_token"})
		return
	}
	if err != nil {
		writeFailure(w, &oidcauth.Failure{Status: http.StatusServiceUnavailable, Code: "identity_mapping_unavailable"})
		return
	}
	grants, err := s.store.ListAgentGrantsByUser(r.Context(), user.ID)
	if err != nil {
		writeFailure(w, &oidcauth.Failure{Status: http.StatusServiceUnavailable, Code: "grant_store_unavailable"})
		return
	}
	out := make([]grantOut, 0, len(grants))
	for _, grant := range grants {
		if !grant.Enabled || grant.RevokedAt != nil {
			continue
		}
		counters, err := s.store.GetAgentGrantCounters(r.Context(), grant.ID)
		if err != nil {
			writeFailure(w, &oidcauth.Failure{Status: http.StatusServiceUnavailable, Code: "grant_store_unavailable"})
			return
		}
		if (grant.GrantMaxTokens != nil && counters.TotalTokens >= *grant.GrantMaxTokens) || (grant.GrantMaxRequests != nil && counters.TotalRequests >= *grant.GrantMaxRequests) {
			continue
		}
		out = append(out, grantOut{
			ID: grant.ID, Name: grant.Name, AllowedModels: append([]string(nil), grant.AllowedModels...),
			TokenTTLSeconds: int64(grant.TokenTTL / time.Second), MaxActivePerInstance: grant.MaxActivePerInstance,
			PerTokenMaxTokens: grant.PerTokenMaxTokens, PerTokenMaxRequests: grant.PerTokenMaxRequests,
			RateLimitRPM: grant.RateLimitRPM, GrantMaxTokens: grant.GrantMaxTokens, GrantMaxRequests: grant.GrantMaxRequests,
			UsedTokens: counters.TotalTokens, UsedRequests: counters.TotalRequests,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	principal, failure := s.auth.Authenticate(r.Context(), r.Header.Values("Authorization"), []string{issueScope})
	if failure != nil {
		writeFailure(w, failure)
		return
	}
	user, err := s.store.GetUserByIdentity(r.Context(), principal.Issuer, principal.Subject)
	if errors.Is(err, store.ErrNotFound) {
		writeFailure(w, &oidcauth.Failure{Status: http.StatusUnauthorized, Code: "invalid_token"})
		return
	}
	if err != nil {
		writeFailure(w, &oidcauth.Failure{Status: http.StatusServiceUnavailable, Code: "identity_mapping_unavailable"})
		return
	}
	var request struct {
		GrantID          string `json:"grant_id"`
		ClientInstanceID string `json:"client_instance_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.GrantID) == "" || strings.TrimSpace(request.ClientInstanceID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "token_issuance_unavailable"})
		return
	}
	s.issueMu.Lock()
	issued, err := s.store.IssueAgentTokenAudited(r.Context(), store.Token{
		UserID: user.ID, TokenHash: hash, AgentGrantID: request.GrantID,
		IssueChannel: store.IssueChannelDevice, SourceClientID: principal.ClientID,
		ClientInstanceID: request.ClientInstanceID,
	})
	s.issueMu.Unlock()
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "grant_not_found"})
		return
	}
	if errors.Is(err, store.ErrGrantExhausted) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "grant_budget_exhausted"})
		return
	}
	if errors.Is(err, store.ErrInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "token_issuance_unavailable"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": raw, "token_type": "Bearer", "token_id": issued.ID,
		"grant_id": issued.AgentGrantID, "allowed_models": issued.AllowedModels,
		"expires_at": issued.ExpiresAt, "max_total_tokens": issued.MaxTotalTokens,
		"max_requests": issued.MaxRequests, "rate_limit_rpm": issued.RateLimitRPM,
	})
}

func writeFailure(w http.ResponseWriter, failure *oidcauth.Failure) {
	w.Header().Set("Content-Type", "application/json")
	switch failure.Status {
	case http.StatusUnauthorized:
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	case http.StatusForbidden:
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
	}
	writeJSON(w, failure.Status, map[string]any{"error": failure.Code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
