package authmw

import (
	"context"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

type ctxKey struct{}

// WithToken attaches the validated token to the request context.
func WithToken(ctx context.Context, tok store.Token) context.Context {
	return context.WithValue(ctx, ctxKey{}, tok)
}

// TokenFrom returns the validated token, if the request went through TokenAuth.
func TokenFrom(ctx context.Context) (store.Token, bool) {
	tok, ok := ctx.Value(ctxKey{}).(store.Token)
	return tok, ok
}
