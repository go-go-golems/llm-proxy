package profiles

import (
	"context"
	"fmt"

	gepprofiles "github.com/go-go-golems/geppetto/pkg/engineprofiles"
	"github.com/go-go-golems/geppetto/pkg/steps/ai/settings"
)

type ProfileDescriptor struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
}

type ResolvedProfileRuntime struct {
	RegistrySlug string
	ProfileSlug  string
	Settings     *settings.InferenceSettings
	Metadata     map[string]any
}

type ProfileResolver interface {
	ResolveProfile(ctx context.Context, slug string) (*ResolvedProfileRuntime, error)
	ListProfiles(ctx context.Context) ([]ProfileDescriptor, error)
}

type GeppettoResolver struct {
	registry gepprofiles.Registry
}

func NewGeppettoResolver(registry gepprofiles.Registry) (*GeppettoResolver, error) {
	if registry == nil {
		return nil, fmt.Errorf("profile registry is required")
	}
	return &GeppettoResolver{registry: registry}, nil
}

func NewYAMLResolver(path string) (*GeppettoResolver, error) {
	store, err := gepprofiles.NewYAMLFileEngineProfileStore(path, "")
	if err != nil {
		return nil, err
	}
	registries, err := store.ListRegistries(context.Background())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if len(registries) == 0 {
		_ = store.Close()
		return nil, fmt.Errorf("profile file %q did not contain any registries", path)
	}
	registry, err := gepprofiles.NewStoreRegistry(store, registries[0].Slug)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return NewGeppettoResolver(registry)
}

func (r *GeppettoResolver) ResolveProfile(ctx context.Context, slug string) (*ResolvedProfileRuntime, error) {
	profileSlug, err := gepprofiles.ParseEngineProfileSlug(slug)
	if err != nil {
		return nil, err
	}
	resolved, err := r.registry.ResolveEngineProfile(ctx, gepprofiles.ResolveInput{EngineProfileSlug: profileSlug})
	if err != nil {
		return nil, err
	}
	return &ResolvedProfileRuntime{
		RegistrySlug: resolved.RegistrySlug.String(),
		ProfileSlug:  resolved.EngineProfileSlug.String(),
		Settings:     resolved.InferenceSettings,
		Metadata:     resolved.Metadata,
	}, nil
}

func (r *GeppettoResolver) ListProfiles(ctx context.Context) ([]ProfileDescriptor, error) {
	registries, err := r.registry.ListRegistries(ctx)
	if err != nil {
		return nil, err
	}
	ret := []ProfileDescriptor{}
	for _, reg := range registries {
		profiles_, err := r.registry.ListEngineProfiles(ctx, reg.Slug)
		if err != nil {
			return nil, err
		}
		for _, p := range profiles_ {
			if p == nil {
				continue
			}
			ret = append(ret, ProfileDescriptor{ID: p.Slug.String(), DisplayName: p.DisplayName, Description: p.Description})
		}
	}
	return ret, nil
}
