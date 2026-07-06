package engines_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/settings"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/types"
	"github.com/go-go-golems/geppetto/pkg/turns"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	byokengines "github.com/go-go-golems/llm-proxy/pkg/byok/engines"
	"github.com/go-go-golems/llm-proxy/pkg/byok/meter"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

// capturingProvider records the settings it receives instead of building a
// real provider engine.
type capturingProvider struct {
	got *profiles.ResolvedProfileRuntime
}

func (p *capturingProvider) EngineForProfile(_ context.Context, profile *profiles.ResolvedProfileRuntime) (engine.Engine, error) {
	p.got = profile
	return noopEngine{}, nil
}

type noopEngine struct{}

func (noopEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	return t, nil
}

func claudeProfile(t *testing.T, slug string) *profiles.ResolvedProfileRuntime {
	t.Helper()
	s, err := settings.NewInferenceSettings()
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	apiType := types.ApiTypeClaude
	s.Chat.ApiType = &apiType
	s.API.APIKeys["claude-api-key"] = "server-side-yaml-key"
	s.API.APIKeys["openai-api-key"] = "another-server-key"
	return &profiles.ResolvedProfileRuntime{ProfileSlug: slug, Settings: s}
}

type fixture struct {
	provider *byokengines.VaultEngineProvider
	inner    *capturingProvider
	store    store.Store
	token    store.Token
	ctx      context.Context
}

func setup(t *testing.T, allowedModels []string, credAPIType string) fixture {
	t.Helper()
	ctx := context.Background()
	st := memory.New()
	u, err := st.UpsertUser(ctx, store.User{OIDCSubject: "s", Username: "alice"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	key, err := vault.GenerateKeyBase64()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	v, err := vault.NewFromBase64(key)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	credID := store.NewID()
	cipher, err := v.Encrypt(credID, []byte("sk-ant-users-own-key"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	cred, err := st.CreateCredential(ctx, store.Credential{
		ID: credID, UserID: u.ID, Provider: "anthropic", APIType: credAPIType,
		Label: "personal", SecretCipher: cipher, SecretLast4: "-key",
	})
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	tok, err := st.MintToken(ctx, store.Token{
		UserID: u.ID, TokenHash: "h", Name: "t",
		CredentialIDs: []string{cred.ID}, AllowedModels: allowedModels,
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	inner := &capturingProvider{}
	return fixture{
		provider: &byokengines.VaultEngineProvider{Inner: inner, Vault: v, Store: st},
		inner:    inner,
		store:    st,
		token:    tok,
		ctx:      authmw.WithToken(ctx, tok),
	}
}

func TestInjectsUserKeyAndScrubsServerKeys(t *testing.T) {
	f := setup(t, []string{"sonnet"}, "claude")
	eng, err := f.provider.EngineForProfile(f.ctx, claudeProfile(t, "sonnet"))
	if err != nil || eng == nil {
		t.Fatalf("engine: %v", err)
	}
	got := f.inner.got
	if got == nil || got.Settings == nil {
		t.Fatal("inner provider not called with settings")
	}
	keys := got.Settings.API.APIKeys
	if keys["claude-api-key"] != "sk-ant-users-own-key" {
		t.Fatalf("user key not injected: %q", keys["claude-api-key"])
	}
	if _, ok := keys["openai-api-key"]; ok {
		t.Fatal("server-side key survived scrubbing")
	}
	if len(keys) != 1 {
		t.Fatalf("expected exactly one key after injection, got %v", keys)
	}
}

func TestOriginalSettingsNotMutated(t *testing.T) {
	f := setup(t, []string{"sonnet"}, "claude")
	original := claudeProfile(t, "sonnet")
	if _, err := f.provider.EngineForProfile(f.ctx, original); err != nil {
		t.Fatalf("engine: %v", err)
	}
	if original.Settings.API.APIKeys["claude-api-key"] != "server-side-yaml-key" {
		t.Fatal("resolver-owned settings were mutated in place")
	}
}

func TestModelNotAllowed(t *testing.T) {
	f := setup(t, []string{"haiku"}, "claude")
	_, err := f.provider.EngineForProfile(f.ctx, claudeProfile(t, "sonnet"))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected model_not_allowed, got %v", err)
	}
	// Rejection is ledgered with status=rejected and does not consume budget.
	entries, lerr := f.store.ListLedger(context.Background(), f.token.ID, timeZero(), 10)
	if lerr != nil || len(entries) != 1 || entries[0].Status != store.LedgerStatusRejected {
		t.Fatalf("ledger reject row: %v %+v", lerr, entries)
	}
	c, cerr := f.store.GetCounters(context.Background(), f.token.ID)
	if cerr != nil || c.TotalRequests != 0 {
		t.Fatalf("rejected request consumed budget: %+v", c)
	}
}

func TestNoCredentialForModel(t *testing.T) {
	// Credential is for openai, profile needs claude.
	f := setup(t, []string{"sonnet"}, "openai")
	_, err := f.provider.EngineForProfile(f.ctx, claudeProfile(t, "sonnet"))
	if err == nil || !strings.Contains(err.Error(), "No stored credential") {
		t.Fatalf("expected no_credential_for_model, got %v", err)
	}
}

func TestFailsClosedWithoutToken(t *testing.T) {
	f := setup(t, []string{"sonnet"}, "claude")
	_, err := f.provider.EngineForProfile(context.Background(), claudeProfile(t, "sonnet"))
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
}

func TestMeterRecorder(t *testing.T) {
	f := setup(t, []string{"sonnet"}, "claude")
	rec := recorderFor(f.store)
	usage := &turns.InferenceUsage{InputTokens: 120, OutputTokens: 30, CacheReadInputTokens: 10}
	rec.RecordInference(f.ctx, "sonnet", usage, true, nil)
	rec.RecordInference(f.ctx, "sonnet", nil, false, context.DeadlineExceeded)
	// Without a token in context, nothing is recorded.
	rec.RecordInference(context.Background(), "sonnet", usage, false, nil)

	c, err := f.store.GetCounters(context.Background(), f.token.ID)
	if err != nil || c.TotalTokens != 150 || c.TotalRequests != 2 {
		t.Fatalf("counters: %v %+v", err, c)
	}
	entries, err := f.store.ListLedger(context.Background(), f.token.ID, timeZero(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("ledger: %v (%d)", err, len(entries))
	}
	var sawStreamedOK, sawError bool
	for _, e := range entries {
		if e.Streamed && e.Status == store.LedgerStatusOK && e.CachedTokens == 10 {
			sawStreamedOK = true
		}
		if e.Status == store.LedgerStatusError && e.PromptTokens == 0 {
			sawError = true
		}
	}
	if !sawStreamedOK || !sawError {
		t.Fatalf("ledger rows missing expected shapes: %+v", entries)
	}
}

func recorderFor(st store.Store) *meter.Recorder { return &meter.Recorder{Store: st} }

func timeZero() time.Time { return time.Time{} }
