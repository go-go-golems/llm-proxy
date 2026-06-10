package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	llmproxydoc "github.com/go-go-golems/llm-proxy/pkg/doc"
	profilespkg "github.com/go-go-golems/llm-proxy/pkg/profiles"
	runtimepkg "github.com/go-go-golems/llm-proxy/pkg/runtime"
	"github.com/go-go-golems/llm-proxy/pkg/server"
	"github.com/spf13/cobra"
)

type serverOptions struct {
	listen   string
	profiles string
}

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCommand() *cobra.Command {
	opts := &serverOptions{}
	rootCmd := &cobra.Command{
		Use:   "llm-proxy-server",
		Short: "Serve an OpenAI-compatible HTTP API backed by Geppetto profiles",
		Long: `llm-proxy-server exposes OpenAI-compatible model, completion, and chat-completion endpoints.

Provider setup lives in Geppetto profile YAML. Without a profiles file the server still exposes health checks and static stub responses, which is useful for smoke tests and local wiring.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context(), opts)
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}
	rootCmd.Flags().StringVar(&opts.listen, "listen", "127.0.0.1:8080", "address to listen on")
	rootCmd.Flags().StringVar(&opts.profiles, "profiles", "", "path to Geppetto profile YAML")
	cobra.CheckErr(logging.AddLoggingSectionToRootCommand(rootCmd, "llm-proxy"))

	helpSystem := help.NewHelpSystem()
	cobra.CheckErr(llmproxydoc.AddDocToHelpSystem(helpSystem))
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	return rootCmd
}

func runServer(ctx context.Context, opts *serverOptions) error {
	var modelLister server.ModelLister
	var completionService server.CompletionService
	var chatCompletionService server.ChatCompletionService
	if opts.profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(opts.profiles)
		if err != nil {
			return err
		}
		modelLister = profileModelLister{resolver: resolver}
		completionService = &runtimepkg.GeppettoCompletionService{Profiles: resolver}
		chatCompletionService = &runtimepkg.GeppettoChatCompletionService{Profiles: resolver}
	}

	srv := server.New(server.Options{CompletionService: completionService, ChatCompletionService: chatCompletionService, ModelLister: modelLister})
	httpServer := &http.Server{
		Addr:              opts.listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("llm-proxy-server listening on %s", opts.listen)
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
