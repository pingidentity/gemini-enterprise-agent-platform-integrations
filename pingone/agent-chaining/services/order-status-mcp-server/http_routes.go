package main

import (
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func newRouter(mcpServer *server.StreamableHTTPServer, validator *tokenValidator) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", validator.middleware(mcpServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
