package main

import (
	"flag"
	"log"
	"net/http"

	"bap-system/bap-mcp-pep/internal/mcppep"
)

func main() {
	configPath := flag.String("config", "bap-mcp-pep/mcp-pep.example.json", "protected MCP PEP configuration")
	flag.Parse()
	config, err := mcppep.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	server, err := mcppep.New(config)
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{Addr: config.ListenAddress, Handler: server.Handler(), ReadHeaderTimeout: 5e9, ReadTimeout: 15e9, WriteTimeout: 15e9, IdleTimeout: 60e9}
	log.Printf("BAP MCP PEP listening on %s", config.ListenAddress)
	if config.TLSCertPath != "" {
		log.Fatal(httpServer.ListenAndServeTLS(config.TLSCertPath, config.TLSKeyPath))
	}
	log.Fatal(httpServer.ListenAndServe())
}
