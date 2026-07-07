// Package policy holds the pure decision helpers shared by the HTTP
// middleware, the scoped model lister, and the engine provider.
package policy

import (
	"path"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/apierr"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

// ModelAllowed reports whether a profile slug matches the token's allowlist.
// Entries are exact slugs or path.Match globs (e.g. "gpt-*").
func ModelAllowed(allowed []string, slug string) bool {
	for _, pattern := range allowed {
		if pattern == slug {
			return true
		}
		if ok, err := path.Match(pattern, slug); err == nil && ok {
			return true
		}
	}
	return false
}

// CheckTokenUsable validates liveness (revocation, expiry) of a token.
func CheckTokenUsable(tok store.Token, now time.Time) *apierr.APIError {
	if tok.RevokedAt != nil {
		return apierr.NewTokenRevoked()
	}
	if tok.ExpiresAt != nil && now.After(*tok.ExpiresAt) {
		return apierr.NewTokenExpired()
	}
	return nil
}

// CheckBudgets compares running counters against the token's budgets.
func CheckBudgets(tok store.Token, counters store.Counters) *apierr.APIError {
	if tok.MaxTotalTokens != nil && counters.TotalTokens >= *tok.MaxTotalTokens {
		return apierr.NewBudgetExhausted(*tok.MaxTotalTokens)
	}
	if tok.MaxRequests != nil && counters.TotalRequests >= *tok.MaxRequests {
		return apierr.NewRequestBudgetExhausted(*tok.MaxRequests)
	}
	return nil
}
