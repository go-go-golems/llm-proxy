package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

type fakeProfileResolver struct{}

func (fakeProfileResolver) ResolveProfile(_ context.Context, slug string) (*profiles.ResolvedProfileRuntime, error) {
	return &profiles.ResolvedProfileRuntime{ProfileSlug: slug}, nil
}

func (fakeProfileResolver) ListProfiles(_ context.Context) ([]profiles.ProfileDescriptor, error) {
	return nil, nil
}

type fakeEngineProvider struct{ eng engine.Engine }

func (p fakeEngineProvider) EngineForProfile(_ context.Context, _ *profiles.ResolvedProfileRuntime) (engine.Engine, error) {
	return p.eng, nil
}

type appendEngine struct{}

func (appendEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	turns.AppendBlock(t, turns.NewAssistantTextBlock("hello from geppetto"))
	return t, nil
}

func TestGeppettoCompletionServiceComplete(t *testing.T) {
	raw, _ := json.Marshal("prompt")
	svc := &GeppettoCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: appendEngine{}}}
	resp, err := svc.Complete(context.Background(), &openaicompletions.CompletionRequest{Model: "sonnet", Prompt: raw})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Model != "sonnet" {
		t.Fatalf("model = %q", resp.Model)
	}
	if resp.Choices[0].Text != "hello from geppetto" {
		t.Fatalf("text = %q", resp.Choices[0].Text)
	}
}

type errorEngine struct{}

func (errorEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	return t, assertErr("boom")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestGeppettoCompletionServiceEngineError(t *testing.T) {
	raw, _ := json.Marshal("prompt")
	svc := &GeppettoCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: errorEngine{}}}
	_, err := svc.Complete(context.Background(), &openaicompletions.CompletionRequest{Model: "sonnet", Prompt: raw})
	if err == nil {
		t.Fatalf("expected engine error")
	}
}
