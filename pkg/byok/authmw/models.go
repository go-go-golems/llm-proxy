package authmw

import (
	"context"

	"github.com/go-go-golems/llm-proxy/pkg/byok/policy"
	"github.com/go-go-golems/llm-proxy/pkg/server"
)

// ScopedModelLister filters the inner lister down to the models the
// request's token is allowed to use. Without a token in context it returns
// nothing: the data plane fails closed.
type ScopedModelLister struct {
	Inner server.ModelLister
}

var _ server.ModelLister = &ScopedModelLister{}

func (l *ScopedModelLister) ListModels(ctx context.Context) ([]server.ModelDescriptor, error) {
	tok, ok := TokenFrom(ctx)
	if !ok || l.Inner == nil {
		return []server.ModelDescriptor{}, nil
	}
	all, err := l.Inner.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]server.ModelDescriptor, 0, len(all))
	for _, m := range all {
		if policy.ModelAllowed(tok.AllowedModels, m.ID) {
			out = append(out, m)
		}
	}
	return out, nil
}
