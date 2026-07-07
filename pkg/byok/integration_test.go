// Package byok_test assembles the full BYOK stack in-process — middleware,
// scoped models, vault engine provider, metering — against a real Geppetto
// OpenAI-compatible engine pointed at a local httptest provider. This is the
// CI-runnable equivalent of the tmux smoke test and exercises Geppetto's
// provider packaging directly.
package byok_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-go-golems/geppetto/pkg/steps/ai/settings"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/types"

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

type fakeOpenAIProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	auths  []string
	bodies []string
}

func newFakeOpenAIProvider(t *testing.T) *fakeOpenAIProvider {
	t.Helper()
	p := &fakeOpenAIProvider{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.auths = append(p.auths, r.Header.Get("Authorization"))
		p.bodies = append(p.bodies, string(body))
		p.mu.Unlock()

		if got := r.Header.Get("Authorization"); got != "Bearer "+userProviderKey {
			http.Error(w, "wrong provider key: "+got, http.StatusUnauthorized)
			return
		}
		var req struct {
			Stream        bool `json:"stream"`
			StreamOptions *struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if !req.Stream {
			http.Error(w, "expected streaming Geppetto request", http.StatusBadRequest)
			return
		}
		if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
			http.Error(w, "expected stream_options.include_usage=true", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_byok","object":"chat.completion.chunk","created":1,"model":"fake-model","choices":[{"index":0,"delta":{"role":"assistant","content":"provider_key_remained_server_side=true"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"chatcmpl_byok","object":"chat.completion.chunk","created":1,"model":"fake-model","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":7}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeOpenAIProvider) baseURL() string { return p.server.URL + "/v1" }

func (p *fakeOpenAIProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.auths)
}

func (p *fakeOpenAIProvider) lastAuth() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.auths) == 0 {
		return ""
	}
	return p.auths[len(p.auths)-1]
}

// testResolver resolves any slug to an OpenAI-compatible Geppetto profile. The
// profile intentionally carries a server-side YAML key that must never reach the
// provider; VaultEngineProvider must scrub it and inject the user's key.
type testResolver struct{ providerBaseURL string }

func (r testResolver) ResolveProfile(_ context.Context, slug string) (*profiles.ResolvedProfileRuntime, error) {
	s, err := settings.NewInferenceSettings()
	if err != nil {
		return nil, err
	}
	apiType := types.ApiTypeOpenAI
	model := "fake-model"
	s.Chat.ApiType = &apiType
	s.Chat.Engine = &model
	s.API.APIKeys["openai-api-key"] = "server-side-yaml-key"
	s.API.BaseUrls["openai-base-url"] = r.providerBaseURL
	s.API.AllowHTTP["openai"] = true
	s.API.AllowLocalNetworks["openai"] = true
	return &profiles.ResolvedProfileRuntime{ProfileSlug: slug, Settings: s}, nil
}

func (testResolver) ListProfiles(context.Context) ([]profiles.ProfileDescriptor, error) {
	return []profiles.ProfileDescriptor{{ID: "fake"}, {ID: "forbidden"}}, nil
}

type stack struct {
	handler  http.Handler
	store    store.Store
	provider *fakeOpenAIProvider
	token    store.Token
	raw      string
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
		ID: credID, UserID: u.ID, Provider: "openai-compatible", APIType: "openai",
		Label: "personal", SecretCipher: cipherBlob, SecretLast4: "-key",
	})
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	budget := int64(30) // each fake provider call burns 19 tokens: call 2 crosses, call 3 rejected
	tok, err := st.MintToken(ctx, store.Token{
		UserID: u.ID, TokenHash: hash, Name: "e2e",
		CredentialIDs: []string{cred.ID}, AllowedModels: []string{"fake"},
		MaxTotalTokens: &budget,
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	provider := newFakeOpenAIProvider(t)
	engineProvider := &byokengines.VaultEngineProvider{Vault: v, Store: st}
	recorder := &meter.Recorder{Store: st}
	chatSvc := &runtime.GeppettoChatCompletionService{
		Profiles: testResolver{providerBaseURL: provider.baseURL()}, Engines: engineProvider, Usage: recorder,
	}
	srv := server.New(server.Options{ChatCompletionService: chatSvc})
	return stack{
		handler: authmw.TokenAuth(st, srv.Handler()),
		store:   st, provider: provider, token: tok, raw: raw,
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

func TestEndToEndGeppettoKeyInjectionMeteringAndBudget(t *testing.T) {
	s := buildStack(t)

	// Call 1: succeeds through real Geppetto provider packaging with the USER's
	// decrypted key; the server-side profile key must be scrubbed before the
	// OpenAI-compatible request reaches the fake provider.
	w := chatCall(t, s, "fake")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "provider_key_remained_server_side=true") {
		t.Fatalf("call 1: %d %s", w.Code, w.Body.String())
	}
	if got := s.provider.lastAuth(); got != "Bearer "+userProviderKey {
		t.Fatalf("provider got wrong key: %q", got)
	}
	// The wire response surfaces provider usage reported by Geppetto.
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Usage == nil || resp.Usage.PromptTokens != 12 {
		t.Fatalf("usage not surfaced: %v %s", err, w.Body.String())
	}

	// Disallowed model → 403 with param=model and no provider call.
	callsBeforeForbidden := s.provider.calls()
	if w := chatCall(t, s, "forbidden"); w.Code != 403 || !strings.Contains(w.Body.String(), "model_not_allowed") {
		t.Fatalf("forbidden model: %d %s", w.Code, w.Body.String())
	}
	if callsAfterForbidden := s.provider.calls(); callsAfterForbidden != callsBeforeForbidden {
		t.Fatalf("forbidden model reached provider: before=%d after=%d", callsBeforeForbidden, callsAfterForbidden)
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
