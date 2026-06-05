package profiles

import (
	"context"
	"path/filepath"
	"testing"

	gepprofiles "github.com/go-go-golems/geppetto/pkg/engineprofiles"
)

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
