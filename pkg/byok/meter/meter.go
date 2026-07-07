// Package meter records per-request token usage into the BYOK ledger.
// It implements runtime.UsageRecorder and reads the token from the request
// context, so the runtime services stay BYOK-agnostic.
package meter

import (
	"context"

	"github.com/go-go-golems/geppetto/pkg/turns"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/runtime"
)

type Recorder struct {
	Store store.Store
}

var _ runtime.UsageRecorder = &Recorder{}

// RecordInference writes one ledger row for a finished upstream call.
// A nil usage (provider omitted it) is recorded as zeros so the request
// still counts against max_requests.
func (r *Recorder) RecordInference(ctx context.Context, model string, usage *turns.InferenceUsage, streamed bool, inferenceErr error) {
	tok, ok := authmw.TokenFrom(ctx)
	if !ok {
		return
	}
	entry := store.LedgerEntry{
		TokenID: tok.ID, UserID: tok.UserID, Model: model,
		Streamed: streamed, Status: store.LedgerStatusOK,
	}
	if inferenceErr != nil {
		entry.Status = store.LedgerStatusError
	}
	if usage != nil {
		entry.PromptTokens = int64(usage.InputTokens)
		entry.CompletionTokens = int64(usage.OutputTokens)
		entry.CachedTokens = int64(usage.CachedTokens + usage.CacheReadInputTokens)
	}
	// The request context may be canceled (client disconnected mid-stream);
	// the usage still happened upstream, so record it detached.
	if err := r.Store.RecordUsage(context.WithoutCancel(ctx), entry); err != nil {
		log.Error().Err(err).Str("token_id", tok.ID).Str("model", model).Msg("byok: usage recording failed")
	}
}
