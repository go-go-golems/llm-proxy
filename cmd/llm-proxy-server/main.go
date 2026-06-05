package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/go-go-golems/llm-proxy/pkg/server"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	profiles := flag.String("profiles", "", "path to Geppetto profile YAML (used in later phases)")
	flag.Parse()
	_ = profiles

	srv := server.New(server.Options{})
	log.Printf("llm-proxy-server listening on %s", *listen)
	if err := http.ListenAndServe(*listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
