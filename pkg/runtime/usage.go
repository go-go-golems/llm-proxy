package runtime

import (
	"context"

	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

// UsageRecorder observes finished inference calls with their authoritative
// token usage. Streaming responses carry no usage on the wire, so this hook
// is the only reliable metering point.
type UsageRecorder interface {
	RecordInference(ctx context.Context, model string, usage *turns.InferenceUsage, streamed bool, inferenceErr error)
}

func usageFrom(result *geppettoengine.InferenceResult) *turns.InferenceUsage {
	if result == nil {
		return nil
	}
	return result.Usage
}
