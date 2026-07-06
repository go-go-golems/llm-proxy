// Package byok_test assembles the full BYOK stack in-process — middleware,
// scoped models, vault engine provider, metering — against a fake engine
// that reports usage, and drives it through real HTTP handlers. This is the
// CI-runnable equivalent of the tmux smoke test.
package byok_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/settings"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/types"
	"github.com/go-go-golems/geppetto/pkg/turns"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	byokengines "github.com/go-go-golems/llm-proxy/pkg/byok/engines"
	"github.com/go-go-golems/llm-proxy/pkg/byok/meter"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
	"github.com/go-go-golems/llm-proxy/pkg/runtime"
	"github.com/go-go-golems/llm-proxy/pkg/server"
)

const userProviderKey = "sk-fake-users-own-key"

// usageEngine implements EngineWithResult: fixed 12+7 token usage per call.
type usageEngine struct{}

func (usageEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	turns.AppendBlock(t, turns.NewAssistantTextBlock("provider_key_remained_server_side=true"))
	return t, nil
}

func (e usageEngine) RunInferenceWithResult(ctx context.Context, t *turns.Turn) (*turns.Turn, *geppettoengine.InferenceResult, error) {
	out, err := e.RunInference(ctx, t)
	return out, &geppettoengine.InferenceResult{
		Usage: &turns.InferenceUsage{InputTokens: 12, OutputTokens: 7},
	}, err
}

// keyAssertingProvider stands in for geppetto's factory: it hands out
// usageEngine and records which API keys reached engine creation.
type keyAssertingProvider struct {
	lastKeys map[string]string
}

func (p *keyAssertingProvider) EngineForProfile(_ context.Context, profile *profiles.ResolvedProfileRuntime) (geppettoengine.Engine, error) {
	p.lastKeys = profile.Settings.API.APIKeys
	return usageEngine{}, nil
}

// testResolver resolves any slug to a claude-typed profile carrying a
// server-side YAML key that must never reach the engine.
type testResolver struct{}

func (testResolver) ResolveProfile(_ context.Context, slug string) (*profiles.ResolvedProfileRuntime, error) {
	s, err := settings.NewInferenceSettings()
	if err != nil {
		return nil, err
	}
	apiType := types.ApiTypeClaude
	s.Chat.ApiType = &apiType
	s.API.APIKeys["claude-api-key"] = "server-side-yaml-key"
	return &profiles.ResolvedProfileRuntime{ProfileSlug: slug, Settings: s}, nil
}

func (testResolver) ListProfiles(context.Context) ([]profiles.ProfileDescriptor, error) {
	return []profiles.ProfileDescriptor{{ID: "fake"}, {ID: "forbidden"}}, nil
}

type stack struct {
	handler http.Handler
	store   store.Store
	inner   *keyAssertingProvider
	token   store.Token
	raw     string
}

func buildStack(t *testing.T) stack {
	t.Helper()
	ctx := context.Background()
	st := memory.New()
	u, err := st.UpsertUser(ctx, store.User{OIDCSubject: "s", Username: "alice"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	keyB64, err := vault.GenerateKeyBase64()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	v, err := vault.NewFromBase64(keyB64)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	credID := store.NewID()
	cipherBlob, err := v.Encrypt(credID, []byte(userProviderKey))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	cred, err := st.CreateCredential(ctx, store.Credential{
		ID: credID, UserID: u.ID, Provider: "anthropic", APIType: "claude",
		Label: "personal", SecretCipher: cipherBlob, SecretLast4: "-key",
	})
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	budget := int64(30) // each fake call burns 19 tokens: call 2 crosses, call 3 rejected
	tok, err := st.MintToken(ctx, store.Token{
		UserID: u.ID, TokenHash: hash, Name: "e2e",
		CredentialIDs: []string{cred.ID}, AllowedModels: []string{"fake"},
		MaxTotalTokens: &budget,
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	inner := &keyAssertingProvider{}
	engineProvider := &byokengines.VaultEngineProvider{Inner: inner, Vault: v, Store: st}
	recorder := &meter.Recorder{Store: st}
	chatSvc := &runtime.GeppettoChatCompletionService{
		Profiles: testResolver{}, Engines: engineProvider, Usage: recorder,
	}
	srv := server.New(server.Options{ChatCompletionService: chatSvc})
	return stack{
		handler: authmw.TokenAuth(st, srv.Handler()),
		store:   st, inner: inner, token: tok, raw: raw,
	}
}

func chatCall(t *testing.T, s stack, model string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.raw)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, req)
	return w
}

func TestEndToEndKeyInjectionMeteringAndBudget(t *testing.T) {
	s := buildStack(t)

	// Call 1: succeeds with the USER's decrypted key, server key scrubbed.
	w := chatCall(t, s, "fake")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "provider_key_remained_server_side=true") {
		t.Fatalf("call 1: %d %s", w.Code, w.Body.String())
	}
	if s.inner.lastKeys["claude-api-key"] != userProviderKey {
		t.Fatalf("engine got wrong key: %v", s.inner.lastKeys)
	}
	if len(s.inner.lastKeys) != 1 {
		t.Fatalf("server-side keys leaked into engine: %v", s.inner.lastKeys)
	}
	// The wire response surfaces the provider usage.
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Usage == nil || resp.Usage.PromptTokens != 12 {
		t.Fatalf("usage not surfaced: %v %s", err, w.Body.String())
	}

	// Disallowed model → 403 with param=model.
	if w := chatCall(t, s, "forbidden"); w.Code != 403 || !strings.Contains(w.Body.String(), "model_not_allowed") {
		t.Fatalf("forbidden model: %d %s", w.Code, w.Body.String())
	}

	// Call 2: still under budget pre-check (19 < 30), crosses it post-hoc.
	if w := chatCall(t, s, "fake"); w.Code != 200 {
		t.Fatalf("call 2: %d %s", w.Code, w.Body.String())
	}
	// Call 3: budget pre-check now sees 38 >= 30 → 429.
	if w := chatCall(t, s, "fake"); w.Code != 429 || !strings.Contains(w.Body.String(), "budget_exhausted") {
		t.Fatalf("call 3 should be over budget: %d %s", w.Code, w.Body.String())
	}

	// Ledger: 2 ok rows + 1 rejected (model_not_allowed); counters exclude rejected.
	c, err := s.store.GetCounters(context.Background(), s.token.ID)
	if err != nil || c.TotalTokens != 38 || c.TotalRequests != 2 {
		t.Fatalf("counters: %v %+v", err, c)
	}
	entries, err := s.store.ListLedger(context.Background(), s.token.ID, time.Time{}, 10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("ledger: %v (%d rows)", err, len(entries))
	}

	// Revocation is immediate.
	if err := s.store.RevokeToken(context.Background(), s.token.UserID, s.token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if w := chatCall(t, s, "fake"); w.Code != 401 || !strings.Contains(w.Body.String(), "token_revoked") {
		t.Fatalf("post-revoke call: %d %s", w.Code, w.Body.String())
	}
}
