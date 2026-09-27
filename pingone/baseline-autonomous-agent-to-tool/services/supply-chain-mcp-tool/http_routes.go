package main

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newRouter(mcpServer *mcp.StreamableHTTPHandler, validator *tokenValidator) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", validator.middleware(mcpServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
