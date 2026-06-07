package main

import (
	"context"
	"encoding/json"
	"fmt"
	gepprofiles "github.com/go-go-golems/geppetto/pkg/engineprofiles"
	"github.com/go-go-golems/geppetto/pkg/inference/engine/factory"
	claudeengine "github.com/go-go-golems/geppetto/pkg/steps/ai/claude"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

func main() {
	store, err := gepprofiles.NewYAMLFileEngineProfileStore("/tmp/llm-proxy-backend-smoke-profiles.yaml", "")
	if err != nil {
		panic(err)
	}
	reg, err := gepprofiles.NewStoreRegistry(store, "default")
	if err != nil {
		panic(err)
	}
	resolved, err := reg.ResolveEngineProfile(context.Background(), gepprofiles.ResolveInput{EngineProfileSlug: gepprofiles.EngineProfileSlug("anthropic-haiku-smoke")})
	if err != nil {
		panic(err)
	}
	b, _ := json.MarshalIndent(map[string]any{
		"chat":          resolved.InferenceSettings.Chat,
		"client_nil":    resolved.InferenceSettings.Client == nil,
		"claude_nil":    resolved.InferenceSettings.Claude == nil,
		"api_base_urls": resolved.InferenceSettings.API.BaseUrls,
	}, "", "  ")
	fmt.Println(string(b))
	eng, err := factory.NewStandardEngineFactory().CreateEngine(resolved.InferenceSettings)
	if err != nil {
		panic(err)
	}
	ce, ok := eng.(*claudeengine.ClaudeEngine)
	if !ok {
		fmt.Printf("engine type %T\n", eng)
		return
	}
	t := &turns.Turn{}
	turns.AppendBlock(t, turns.NewUserTextBlock("Reply with exactly: anthropic proxy simple ok"))
	req, err := ce.MakeMessageRequestFromTurn(t)
	if err != nil {
		panic(err)
	}
	rb, _ := json.MarshalIndent(req, "", "  ")
	fmt.Println(string(rb))
}
