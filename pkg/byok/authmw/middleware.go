// Package authmw enforces BYOK bearer tokens on the data plane. It wraps the
// llm-proxy handler: /v1/* requests must present a minted token, which is
// validated, budget-checked, rate-limited, and attached to the context for
// the scoped model lister and the vault engine provider downstream.
package authmw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/go-go-golems/llm-proxy/pkg/byok/apierr"
	"github.com/go-go-golems/llm-proxy/pkg/byok/policy"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/pkg/errors"
)

// TokenAuth returns middleware enforcing minted tokens on /v1/* paths.
// Other paths (e.g. /healthz, control plane) pass through untouched.
func TokenAuth(st store.Store, next http.Handler) http.Handler {
	limiter := NewRateLimiter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now().UTC()

		raw, ok := bearerToken(r)
		if !ok {
			writeAPIError(w, apierr.NewMissingAPIKey())
			return
		}
		tok, err := st.GetTokenByHash(r.Context(), tokens.Hash(raw))
		if errors.Is(err, store.ErrNotFound) {
			writeAPIError(w, apierr.NewInvalidAPIKey())
			return
		}
		if err != nil {
			log.Error().Err(err).Msg("byok: token lookup failed")
			writeAPIError(w, &apierr.APIError{Status: 500, Type: "api_error", Code: "internal_error", Message: "token validation failed"})
			return
		}
		if apiErr := policy.CheckTokenUsable(tok, now); apiErr != nil {
			rejected(r.Context(), st, tok, apiErr)
			writeAPIError(w, apiErr)
			return
		}
		if !limiter.Allow(tok.ID, tok.RateLimitRPM, now) {
			apiErr := apierr.NewRateLimited(*tok.RateLimitRPM)
			rejected(r.Context(), st, tok, apiErr)
			writeAPIError(w, apiErr)
			return
		}
		counters, err := st.GetCounters(r.Context(), tok.ID)
		if err != nil {
			log.Error().Err(err).Str("token_id", tok.ID).Msg("byok: counter lookup failed")
			writeAPIError(w, &apierr.APIError{Status: 500, Type: "api_error", Code: "internal_error", Message: "budget check failed"})
			return
		}
		if apiErr := policy.CheckBudgets(tok, counters); apiErr != nil {
			rejected(r.Context(), st, tok, apiErr)
			writeAPIError(w, apiErr)
			return
		}
		if err := st.TouchTokenUsed(r.Context(), tok.ID, now); err != nil {
			log.Warn().Err(err).Str("token_id", tok.ID).Msg("byok: touch last_used_at failed")
		}
		next.ServeHTTP(w, r.WithContext(WithToken(r.Context(), tok)))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	authz := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(authz, prefix))
	return raw, raw != ""
}

func rejected(ctx context.Context, st store.Store, tok store.Token, apiErr *apierr.APIError) {
	payload := fmt.Sprintf(`{"code":%q,"path":"data-plane"}`, apiErr.Code)
	if err := st.AppendEvent(ctx, store.AuditEvent{
		UserID: tok.UserID, TokenID: tok.ID,
		EventType: "inference.rejected", Payload: []byte(payload),
	}); err != nil {
		log.Warn().Err(err).Str("token_id", tok.ID).Msg("byok: audit append failed")
	}
}

// writeAPIError mirrors pkg/server's OpenAI error envelope; the middleware
// runs outside the server package so it carries its own writer.
func writeAPIError(w http.ResponseWriter, e *apierr.APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": e.Message,
			"type":    e.Type,
			"param":   e.Param,
			"code":    e.Code,
		},
	})
}
