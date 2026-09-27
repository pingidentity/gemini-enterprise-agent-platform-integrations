package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
	stripe "github.com/stripe/stripe-go/v79"
)

func main() {
	log.SetFlags(0)

	stripe.Key = requireEnv("STRIPE_SECRET_KEY")
	validator, err := newTokenValidator(context.Background())
	if err != nil {
		log.Fatalf("token validator: %v", err)
	}
	mcpSrv := server.NewMCPServer("stripe-mcp-server", "1.0.0",
		server.WithToolCapabilities(false),
	)
	registerStripeMcpTools(mcpSrv)
	mcpRouter := newRouter(server.NewStreamableHTTPServer(mcpSrv), validator)
	if err := http.ListenAndServe(":8080", mcpRouter); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
