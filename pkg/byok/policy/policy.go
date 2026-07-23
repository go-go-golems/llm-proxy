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

// CheckAgentGrant validates grant liveness, cumulative budgets, and that a
// child token has not escaped the currently approved policy.
func CheckAgentGrant(grant store.AgentGrant, counters store.AgentGrantCounters, tok store.Token) *apierr.APIError {
	if grant.RevokedAt != nil || !grant.Enabled {
		return apierr.NewTokenRevoked()
	}
	if grant.GrantMaxTokens != nil && counters.TotalTokens >= *grant.GrantMaxTokens {
		return apierr.NewBudgetExhausted(*grant.GrantMaxTokens)
	}
	if grant.GrantMaxRequests != nil && counters.TotalRequests >= *grant.GrantMaxRequests {
		return apierr.NewRequestBudgetExhausted(*grant.GrantMaxRequests)
	}
	if !subset(tok.CredentialIDs, grant.CredentialIDs) || !subset(tok.AllowedModels, grant.AllowedModels) {
		return apierr.NewTokenRevoked()
	}
	if exceeds(tok.MaxTotalTokens, grant.PerTokenMaxTokens) || exceeds(tok.MaxRequests, grant.PerTokenMaxRequests) || exceeds(tok.RateLimitRPM, grant.RateLimitRPM) {
		return apierr.NewTokenRevoked()
	}
	return nil
}

func subset(values, allowed []string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		set[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

func exceeds(value, maximum *int64) bool {
	if maximum == nil {
		return false
	}
	return value == nil || *value > *maximum
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
