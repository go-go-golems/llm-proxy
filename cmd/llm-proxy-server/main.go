package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	profilespkg "github.com/go-go-golems/llm-proxy/pkg/profiles"
	"github.com/go-go-golems/llm-proxy/pkg/server"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	profiles := flag.String("profiles", "", "path to Geppetto profile YAML (used in later phases)")
	flag.Parse()
	var modelLister server.ModelLister
	if *profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(*profiles)
		if err != nil {
			log.Fatalf("load profiles: %v", err)
		}
		modelLister = profileModelLister{resolver: resolver}
	}

	srv := server.New(server.Options{ModelLister: modelLister})
	log.Printf("llm-proxy-server listening on %s", *listen)
	if err := http.ListenAndServe(*listen, srv.Handler()); err != nil {
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
