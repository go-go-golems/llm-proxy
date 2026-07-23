package store

import (
	"strings"

	"github.com/pkg/errors"
)

// ValidateAgentGrantPolicy validates backend-independent structural policy.
// Ownership and credential availability must additionally be checked inside
// the backend's atomic mutation boundary.
func ValidateAgentGrantPolicy(grant AgentGrant) error {
	if grant.UserID == "" || strings.TrimSpace(grant.Name) == "" {
		return errors.New("agent grant owner and name are required")
	}
	if len(grant.CredentialIDs) == 0 || len(grant.AllowedModels) == 0 {
		return errors.New("agent grant credentials and models are required")
	}
	if grant.TokenTTL <= 0 || grant.MaxActivePerInstance <= 0 {
		return errors.New("agent grant token TTL and active-token limit must be positive")
	}
	for name, value := range map[string]*int64{
		"per-token token limit":   grant.PerTokenMaxTokens,
		"per-token request limit": grant.PerTokenMaxRequests,
		"rate limit":              grant.RateLimitRPM,
		"grant token limit":       grant.GrantMaxTokens,
		"grant request limit":     grant.GrantMaxRequests,
	} {
		if value != nil && *value <= 0 {
			return errors.Errorf("agent grant %s must be positive", name)
		}
	}
	if hasBlankOrDuplicate(grant.CredentialIDs) {
		return errors.New("agent grant credential IDs must be non-empty and unique")
	}
	if hasBlankOrDuplicate(grant.AllowedModels) {
		return errors.New("agent grant models must be non-empty and unique")
	}
	return nil
}

func ValidateAgentTokenProvenance(token Token) error {
	if token.UserID == "" || token.AgentGrantID == "" || token.IssueChannel != IssueChannelDevice || strings.TrimSpace(token.SourceClientID) == "" {
		return errors.Wrap(ErrInvalid, "device token owner, grant, channel, and source client are required")
	}
	if len(token.SourceClientID) > 128 || len(token.ClientInstanceID) < 16 || len(token.ClientInstanceID) > 128 {
		return errors.Wrap(ErrInvalid, "device token provenance length is invalid")
	}
	for _, r := range token.ClientInstanceID {
		if !isClientInstanceRune(r) {
			return errors.Wrap(ErrInvalid, "client instance ID contains unsupported characters")
		}
	}
	if token.TokenHash == "" {
		return errors.Wrap(ErrInvalid, "device token hash is required")
	}
	return nil
}

func isClientInstanceRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
}

func hasBlankOrDuplicate(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
