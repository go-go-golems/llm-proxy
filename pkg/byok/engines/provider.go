// Package engines provides the BYOK EngineProvider: it swaps the server-side
// profile API keys for the calling token's stored credential before engine
// creation. This is the enforcement point where the user's own provider key
// gets used — and the only place plaintext secrets exist, briefly, per request.
package engines

import (
	"context"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/go-go-golems/llm-proxy/pkg/byok/apierr"
	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	"github.com/go-go-golems/llm-proxy/pkg/byok/policy"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
	"github.com/go-go-golems/llm-proxy/pkg/runtime"
)

// VaultEngineProvider enforces the token's model allowlist and injects the
// decrypted per-user credential into a cloned copy of the resolved profile
// settings. It fails closed: without a token in context, no engine.
type VaultEngineProvider struct {
	Inner runtime.EngineProvider
	Vault *vault.Vault
	Store store.Store
}

var _ runtime.EngineProvider = &VaultEngineProvider{}

func (p *VaultEngineProvider) EngineForProfile(ctx context.Context, profile *profiles.ResolvedProfileRuntime) (engine.Engine, error) {
	if profile == nil || profile.Settings == nil {
		return nil, errors.New("byok: resolved profile settings are required")
	}
	tok, ok := authmw.TokenFrom(ctx)
	if !ok {
		return nil, errors.New("byok: no token in request context; refusing inference")
	}

	if !policy.ModelAllowed(tok.AllowedModels, profile.ProfileSlug) {
		apiErr := apierr.NewModelNotAllowed(profile.ProfileSlug)
		p.ledgerReject(ctx, tok, profile.ProfileSlug)
		return nil, apiErr
	}

	apiType := profileAPIType(profile)
	if apiType == "" {
		return nil, errors.Errorf("byok: profile %q has no api_type; cannot select a credential", profile.ProfileSlug)
	}
	cred, err := p.pickCredential(ctx, tok, apiType)
	if err != nil {
		p.ledgerReject(ctx, tok, profile.ProfileSlug)
		return nil, err
	}

	key, err := p.Vault.Decrypt(cred.ID, cred.SecretCipher)
	if err != nil {
		return nil, errors.Wrapf(err, "byok: decrypt credential %s", cred.ID)
	}

	settings := profile.Settings.Clone()
	if settings.API == nil {
		return nil, errors.New("byok: cloned settings have no API section")
	}
	// Replace, never merge: server-side YAML/env keys must not subsidize
	// BYOK callers, so every other key is scrubbed.
	settings.API.APIKeys = map[string]string{apiType + "-api-key": string(key)}
	if settings.Chat != nil && settings.Chat.APIKeys != nil {
		settings.Chat.APIKeys = map[string]string{}
	}

	inner := p.Inner
	if inner == nil {
		inner = &runtime.FactoryEngineProvider{}
	}
	return inner.EngineForProfile(ctx, &profiles.ResolvedProfileRuntime{
		RegistrySlug: profile.RegistrySlug,
		ProfileSlug:  profile.ProfileSlug,
		Settings:     settings,
		Metadata:     profile.Metadata,
	})
}

// pickCredential returns the first enabled bound credential matching the
// profile's api_type.
func (p *VaultEngineProvider) pickCredential(ctx context.Context, tok store.Token, apiType string) (store.Credential, error) {
	for _, id := range tok.CredentialIDs {
		cred, err := p.Store.GetCredential(ctx, tok.UserID, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Credential{}, errors.Wrapf(err, "byok: load credential %s", id)
		}
		if cred.Disabled || cred.APIType != apiType {
			continue
		}
		return cred, nil
	}
	return store.Credential{}, apierr.NewNoCredentialForModel(apiType)
}

// ledgerReject records a policy rejection for a known model in the usage
// ledger (status=rejected: audited, but not counted against budgets).
func (p *VaultEngineProvider) ledgerReject(ctx context.Context, tok store.Token, model string) {
	if err := p.Store.RecordUsage(ctx, store.LedgerEntry{
		TokenID: tok.ID, UserID: tok.UserID, Model: model,
		Status: store.LedgerStatusRejected,
	}); err != nil {
		log.Warn().Err(err).Str("token_id", tok.ID).Msg("byok: ledger reject write failed")
	}
}

func profileAPIType(p *profiles.ResolvedProfileRuntime) string {
	if p.Settings != nil && p.Settings.Chat != nil && p.Settings.Chat.ApiType != nil {
		return string(*p.Settings.Chat.ApiType)
	}
	return ""
}
