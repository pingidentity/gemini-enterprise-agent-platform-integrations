// Supply Chain MCP Tool — MCP server on Cloud Run exposing the `restock` tool.
// Every request passes through token validation (auth.go) before the MCP
// handler runs.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	_ = godotenv.Load()
	port := "8080"

	validator, err := newTokenValidator(context.Background())
	if err != nil {
		log.Fatalf("token validator: %v", err)
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "supply-chain-mcp-tool", Version: "1.0.0"},
		nil,
	)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "restock",
		Description: "Place a restock order for a product in a given region.",
	}, handleRestock)

	mux := http.NewServeMux()
	mux.Handle("/mcp", validator.middleware(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server }, nil,
	)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("[SupplyChain] listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
