package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	validator, err := newTokenValidator(context.Background())
	if err != nil {
		log.Fatalf("token validator: %v", err)
	}

	mcpServer := server.NewMCPServer("order-status-mcp-server", "1.0.0", server.WithToolCapabilities(false))
	registerOrderStatusTool(mcpServer)
	handler := server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))

	log.Println("[OrderStatusMCP] listening on :8080")
	if err := http.ListenAndServe(":8080", newRouter(handler, validator)); err != nil {
		log.Fatal(err)
	}
}
