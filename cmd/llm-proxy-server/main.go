package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	profilespkg "github.com/go-go-golems/llm-proxy/pkg/profiles"
	runtimepkg "github.com/go-go-golems/llm-proxy/pkg/runtime"
	"github.com/go-go-golems/llm-proxy/pkg/server"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	profiles := flag.String("profiles", "", "path to Geppetto profile YAML (used in later phases)")
	flag.Parse()
	var modelLister server.ModelLister
	var completionService server.CompletionService
	var chatCompletionService server.ChatCompletionService
	if *profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(*profiles)
		if err != nil {
			log.Fatalf("load profiles: %v", err)
		}
		modelLister = profileModelLister{resolver: resolver}
		completionService = &runtimepkg.GeppettoCompletionService{Profiles: resolver}
		chatCompletionService = &runtimepkg.GeppettoChatCompletionService{Profiles: resolver}
	}

	srv := server.New(server.Options{CompletionService: completionService, ChatCompletionService: chatCompletionService, ModelLister: modelLister})
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("llm-proxy-server listening on %s", *listen)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
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
