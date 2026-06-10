package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	llmproxydoc "github.com/go-go-golems/llm-proxy/pkg/doc"
	profilespkg "github.com/go-go-golems/llm-proxy/pkg/profiles"
	runtimepkg "github.com/go-go-golems/llm-proxy/pkg/runtime"
	"github.com/go-go-golems/llm-proxy/pkg/server"
	"github.com/spf13/cobra"
)

type ServeCommand struct {
	*cmds.CommandDescription
}

type ServeSettings struct {
	Listen   string `glazed:"listen"`
	Profiles string `glazed:"profiles"`
}

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "llm-proxy-server",
		Short: "Serve an OpenAI-compatible HTTP API backed by Geppetto profiles",
		Long: `llm-proxy-server exposes OpenAI-compatible model, completion, and chat-completion endpoints.

Provider setup lives in Geppetto profile YAML. Without a profiles file the server still exposes health checks and static stub responses, which is useful for smoke tests and local wiring.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}
	cobra.CheckErr(logging.AddLoggingSectionToRootCommand(rootCmd, "llm-proxy"))

	helpSystem := help.NewHelpSystem()
	cobra.CheckErr(llmproxydoc.AddDocToHelpSystem(helpSystem))
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	serveCmd, err := NewServeCommand()
	cobra.CheckErr(err)
	serveCobraCmd, err := cli.BuildCobraCommandFromCommand(serveCmd,
		cli.WithParserConfig(cli.CobraParserConfig{
			ShortHelpSections: []string{schema.DefaultSlug},
		}),
	)
	cobra.CheckErr(err)
	rootCmd.AddCommand(serveCobraCmd)

	return rootCmd
}

func NewServeCommand() (*ServeCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}

	cmdDesc := cmds.NewCommandDescription(
		"serve",
		cmds.WithShort("Serve the OpenAI-compatible LLM proxy HTTP API"),
		cmds.WithLong(`Serve the OpenAI-compatible LLM proxy HTTP API.

The server exposes /healthz, /v1/models, /v1/completions, and /v1/chat/completions. Provider setup lives in Geppetto profile YAML; without a profiles file the server still exposes health checks and static stub responses.

Examples:

  llm-proxy-server serve
  llm-proxy-server serve --listen 127.0.0.1:8080
  llm-proxy-server serve --profiles examples/profiles.yaml --listen :8080
`),
		cmds.WithFlags(
			fields.New(
				"listen",
				fields.TypeString,
				fields.WithDefault("127.0.0.1:8080"),
				fields.WithHelp("Address to listen on"),
			),
			fields.New(
				"profiles",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Path to Geppetto profile YAML"),
			),
		),
		cmds.WithSections(commandSettingsSection),
	)

	return &ServeCommand{CommandDescription: cmdDesc}, nil
}

func (c *ServeCommand) Run(ctx context.Context, parsedValues *values.Values) error {
	settings := &ServeSettings{}
	if err := parsedValues.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	return runServer(ctx, settings)
}

func runServer(ctx context.Context, opts *ServeSettings) error {
	var modelLister server.ModelLister
	var completionService server.CompletionService
	var chatCompletionService server.ChatCompletionService
	if opts.Profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(opts.Profiles)
		if err != nil {
			return err
		}
		modelLister = profileModelLister{resolver: resolver}
		completionService = &runtimepkg.GeppettoCompletionService{Profiles: resolver}
		chatCompletionService = &runtimepkg.GeppettoChatCompletionService{Profiles: resolver}
	}

	srv := server.New(server.Options{CompletionService: completionService, ChatCompletionService: chatCompletionService, ModelLister: modelLister})
	httpServer := &http.Server{
		Addr:              opts.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("llm-proxy-server listening on %s", opts.Listen)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

type profileModelLister struct {
	resolver profilespkg.ProfileResolver
}

func (l profileModelLister) ListModels(ctx context.Context) ([]server.ModelDescriptor, error) {
	profiles, err := l.resolver.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]server.ModelDescriptor, 0, len(profiles))
	for _, p := range profiles {
		models = append(models, server.ModelDescriptor{ID: p.ID, Object: "model", OwnedBy: "geppetto-profile"})
	}
	return models, nil
}
