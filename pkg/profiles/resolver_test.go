package profiles

import (
	"context"
	"path/filepath"
	"testing"

	gepprofiles "github.com/go-go-golems/geppetto/pkg/engineprofiles"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/settings"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/types"
)

func TestYAMLResolverMergesSparseProfileOntoBaseSettings(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "profiles.yaml")
	store, err := gepprofiles.NewYAMLFileEngineProfileStore(path, gepprofiles.MustRegistrySlug("default"))
	if err != nil {
		t.Fatalf("NewYAMLFileEngineProfileStore error: %v", err)
	}
	apiType := types.ApiTypeGemini
	engine := "gemini-3-flash-preview"
	registry := &gepprofiles.EngineProfileRegistry{
		Slug:                     gepprofiles.MustRegistrySlug("default"),
		DefaultEngineProfileSlug: gepprofiles.MustEngineProfileSlug("gemini-smoke"),
		Profiles: map[gepprofiles.EngineProfileSlug]*gepprofiles.EngineProfile{
			gepprofiles.MustEngineProfileSlug("gemini-smoke"): {
				Slug: gepprofiles.MustEngineProfileSlug("gemini-smoke"),
				InferenceSettings: &settings.InferenceSettings{
					Chat: &settings.ChatSettings{ApiType: &apiType, Engine: &engine},
				},
			},
		},
	}
	if err := store.UpsertRegistry(ctx, registry, gepprofiles.SaveOptions{Actor: "test"}); err != nil {
		t.Fatalf("UpsertRegistry error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	resolver, err := NewYAMLResolver(path)
	if err != nil {
		t.Fatalf("NewYAMLResolver error: %v", err)
	}
	resolved, err := resolver.ResolveProfile(ctx, "gemini-smoke")
	if err != nil {
		t.Fatalf("ResolveProfile error: %v", err)
	}
	if resolved.Settings == nil || resolved.Settings.Gemini == nil {
		t.Fatalf("expected sparse profile to be merged with base Gemini settings, got %#v", resolved.Settings)
	}
	if resolved.Settings.Chat == nil || resolved.Settings.Chat.Engine == nil || *resolved.Settings.Chat.Engine != engine {
		t.Fatalf("resolved engine mismatch: %#v", resolved.Settings.Chat)
	}
}

func TestYAMLResolverListsAndResolvesProfiles(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "profiles.yaml")
	store, err := gepprofiles.NewYAMLFileEngineProfileStore(path, gepprofiles.MustRegistrySlug("default"))
	if err != nil {
		t.Fatalf("NewYAMLFileEngineProfileStore error: %v", err)
	}
	registry := &gepprofiles.EngineProfileRegistry{
		Slug:                     gepprofiles.MustRegistrySlug("default"),
		DefaultEngineProfileSlug: gepprofiles.MustEngineProfileSlug("sonnet"),
		Profiles: map[gepprofiles.EngineProfileSlug]*gepprofiles.EngineProfile{
			gepprofiles.MustEngineProfileSlug("sonnet"): {Slug: gepprofiles.MustEngineProfileSlug("sonnet"), DisplayName: "Sonnet"},
		},
	}
	if err := store.UpsertRegistry(ctx, registry, gepprofiles.SaveOptions{Actor: "test"}); err != nil {
		t.Fatalf("UpsertRegistry error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	resolver, err := NewYAMLResolver(path)
	if err != nil {
		t.Fatalf("NewYAMLResolver error: %v", err)
	}
	profiles, err := resolver.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles error: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "sonnet" {
		t.Fatalf("profiles = %#v", profiles)
	}
	resolved, err := resolver.ResolveProfile(ctx, "sonnet")
	if err != nil {
		t.Fatalf("ResolveProfile known error: %v", err)
	}
	if resolved.ProfileSlug != "sonnet" {
		t.Fatalf("resolved slug = %q", resolved.ProfileSlug)
	}
	if _, err := resolver.ResolveProfile(ctx, "missing"); err == nil {
		t.Fatalf("expected missing profile error")
	}
}
