package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	stripe "github.com/stripe/stripe-go/v79"
)

func main() {
	log.SetFlags(0)

	stripe.Key = requireEnv("STRIPE_SECRET_KEY")
	idpIssuer = strings.TrimSuffix(requireEnv("IDP_ISSUER"), "/")
	rawRequiredScopes = requireEnv("IDP_REQUIRED_SCOPE")
	mcpTokenAudience = requireEnv("IDP_REQUIRED_AUDIENCE")
	if err := initIdpJwksURL(); err != nil {
		log.Fatalf("invalid IDP_ISSUER: %v", err)
	}
	mcpSrv := server.NewMCPServer("stripe-mcp-server", "1.0.0",
		server.WithToolCapabilities(false),
	)
	registerStripeMcpTools(mcpSrv)
	mcpRouter := newRouter(server.NewStreamableHTTPServer(mcpSrv))
	if err := http.ListenAndServe(":8080", mcpRouter); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
