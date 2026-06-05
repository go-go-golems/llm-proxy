package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-go-golems/geppetto/pkg/engineprofiles"
	"github.com/go-go-golems/geppetto/pkg/events"
	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/inference/engine/factory"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

type reasoningSink struct {
	EventTypes                map[string]int      `json:"event_types"`
	ReasoningDeltaCount       int                 `json:"reasoning_delta_count"`
	ReasoningDeltaText        string              `json:"reasoning_delta_text,omitempty"`
	ReasoningDeltaSources     map[string]int      `json:"reasoning_delta_sources,omitempty"`
	ReasoningFinishedCount    int                 `json:"reasoning_finished_count"`
	ReasoningFinishedTexts    []string            `json:"reasoning_finished_texts,omitempty"`
	ReasoningFinishedSources  map[string]int      `json:"reasoning_finished_sources,omitempty"`
	InfoMessages              map[string]int      `json:"info_messages,omitempty"`
	ProviderFinishedStop      string              `json:"provider_finished_stop,omitempty"`
	ProviderFinishedClass     string              `json:"provider_finished_class,omitempty"`
	ProviderFinishedToolCalls bool                `json:"provider_finished_tool_calls,omitempty"`
	FinalMetadataExtra        map[string]any      `json:"final_metadata_extra,omitempty"`
	Errors                    []string            `json:"errors,omitempty"`
	Samples                   []map[string]string `json:"samples,omitempty"`
}

func newReasoningSink() *reasoningSink {
	return &reasoningSink{
		EventTypes:               map[string]int{},
		ReasoningDeltaSources:    map[string]int{},
		ReasoningFinishedSources: map[string]int{},
		InfoMessages:             map[string]int{},
	}
}

func (s *reasoningSink) PublishEvent(ev events.Event) error {
	if ev == nil {
		return nil
	}
	s.EventTypes[string(ev.Type())]++
	if len(s.Samples) < 20 {
		sample := map[string]string{"type": string(ev.Type())}
		s.Samples = append(s.Samples, sample)
	}
	s.FinalMetadataExtra = ev.Metadata().Extra
	switch e := ev.(type) {
	case *events.EventReasoningDelta:
		s.ReasoningDeltaCount++
		s.ReasoningDeltaText = e.Text
		if e.Source != "" {
			s.ReasoningDeltaSources[e.Source]++
		}
		if len(s.Samples) <= 20 {
			s.Samples[len(s.Samples)-1]["delta"] = truncate(e.Delta, 120)
			s.Samples[len(s.Samples)-1]["source"] = e.Source
		}
	case *events.EventReasoningSegmentFinished:
		s.ReasoningFinishedCount++
		if e.Text != "" {
			s.ReasoningFinishedTexts = append(s.ReasoningFinishedTexts, e.Text)
		}
		if e.Source != "" {
			s.ReasoningFinishedSources[e.Source]++
		}
	case *events.EventInfo:
		s.InfoMessages[e.Message]++
	case *events.EventProviderCallFinished:
		s.ProviderFinishedStop = e.StopReason
		s.ProviderFinishedClass = e.FinishClass
		s.ProviderFinishedToolCalls = e.HasToolCalls
	case *events.EventError:
		if e.ErrorString != "" {
			s.Errors = append(s.Errors, e.ErrorString)
		}
	}
	return nil
}

type runSummary struct {
	Profile           string                 `json:"profile"`
	OK                bool                   `json:"ok"`
	Error             string                 `json:"error,omitempty"`
	DurationMS        int64                  `json:"duration_ms"`
	Result            *turns.InferenceResult `json:"result,omitempty"`
	ReasoningBlocks   []blockSummary         `json:"reasoning_blocks,omitempty"`
	AssistantText     string                 `json:"assistant_text,omitempty"`
	AssistantTextSize int                    `json:"assistant_text_size"`
	Sink              *reasoningSink         `json:"sink"`
}

type blockSummary struct {
	Text                string `json:"text,omitempty"`
	SummaryJSON         string `json:"summary_json,omitempty"`
	EncryptedContentSet bool   `json:"encrypted_content_set,omitempty"`
}

func main() {
	profilesPath := flag.String("profiles", "/tmp/llm-proxy-thinking-smoke-profiles.yaml", "Geppetto profile YAML")
	profileList := flag.String("profiles-to-run", "claude-thinking-sonnet-smoke,openai-responses-thinking-smoke", "comma-separated profile slugs")
	prompt := flag.String("prompt", "Solve this internally: if a train leaves at 3pm and travels 45 miles per hour for 2 hours, what distance did it travel? Reply with only the final number and unit.", "prompt")
	output := flag.String("output", "/tmp/llm-proxy-thinking-smoke-summary.json", "summary JSON output path")
	flag.Parse()

	ctx := context.Background()
	store, err := engineprofiles.NewYAMLFileEngineProfileStore(*profilesPath, "")
	if err != nil {
		fatal(err)
	}
	registry, err := engineprofiles.NewStoreRegistry(store, "default")
	if err != nil {
		fatal(err)
	}
	engineFactory := factory.NewStandardEngineFactory()

	var summaries []runSummary
	for _, raw := range strings.Split(*profileList, ",") {
		profile := strings.TrimSpace(raw)
		if profile == "" {
			continue
		}
		summary := runOne(ctx, registry, engineFactory, profile, *prompt)
		summaries = append(summaries, summary)
	}

	b, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, append(b, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Println(string(b))
}

func runOne(ctx context.Context, registry *engineprofiles.StoreRegistry, engineFactory *factory.StandardEngineFactory, profile string, prompt string) runSummary {
	start := time.Now()
	sink := newReasoningSink()
	summary := runSummary{Profile: profile, Sink: sink}
	resolved, err := registry.ResolveEngineProfile(ctx, engineprofiles.ResolveInput{EngineProfileSlug: engineprofiles.EngineProfileSlug(profile)})
	if err != nil {
		summary.Error = err.Error()
		summary.DurationMS = time.Since(start).Milliseconds()
		return summary
	}
	eng, err := engineFactory.CreateEngine(resolved.InferenceSettings)
	if err != nil {
		summary.Error = err.Error()
		summary.DurationMS = time.Since(start).Milliseconds()
		return summary
	}
	turn := &turns.Turn{ID: "thinking_smoke_" + profile}
	turns.AppendBlock(turn, turns.NewUserTextBlock(prompt))
	runCtx := events.WithEventSinks(ctx, sink)
	out, result, err := geppettoengine.RunInferenceWithResult(runCtx, eng, turn)
	summary.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	summary.OK = true
	summary.Result = result
	for _, block := range out.Blocks {
		switch block.Kind {
		case turns.BlockKindReasoning:
			summary.ReasoningBlocks = append(summary.ReasoningBlocks, summarizeReasoningBlock(block))
		case turns.BlockKindLLMText:
			if s, ok := block.Payload[turns.PayloadKeyText].(string); ok {
				summary.AssistantText += s
			}
		case turns.BlockKindUser, turns.BlockKindToolCall, turns.BlockKindToolUse, turns.BlockKindSystem, turns.BlockKindOther:
			continue
		}
	}
	summary.AssistantTextSize = len(summary.AssistantText)
	summary.AssistantText = truncate(summary.AssistantText, 500)
	return summary
}

func summarizeReasoningBlock(block turns.Block) blockSummary {
	var ret blockSummary
	if text, ok := block.Payload[turns.PayloadKeyText].(string); ok {
		ret.Text = truncate(text, 500)
	}
	if enc, ok := block.Payload[turns.PayloadKeyEncryptedContent].(string); ok && enc != "" {
		ret.EncryptedContentSet = true
	}
	if summary, ok := block.Payload[turns.PayloadKeySummary]; ok && summary != nil {
		if b, err := json.Marshal(summary); err == nil {
			ret.SummaryJSON = truncate(string(b), 500)
		}
	}
	return ret
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
