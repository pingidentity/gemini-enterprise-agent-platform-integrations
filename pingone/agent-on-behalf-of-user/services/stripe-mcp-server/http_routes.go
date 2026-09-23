package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

func newRouter(mcpServer *server.StreamableHTTPServer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")

		tok, err := validateToken(authHeader)
		if err != nil {
			log.Printf("[SupplyChain] token validation failed: %v", err)
			// RFC 6750: a malformed/expired token is 401 with an
			// error="invalid_token" challenge; a valid token lacking the
			// required scope is 403 with error="insufficient_scope". The
			// validation detail goes to the log only — never echoed to the
			// caller.
			if errors.Is(err, errInsufficientScope) {
				w.Header().Set("WWW-Authenticate",
					fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, requiredScopeList()))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"error":"insufficient_scope"}`)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"invalid_token"}`)
			return
		}

		// Email is injected by the extension service; empty for tool-discovery requests.
		email := r.Header.Get("X-User-Email")

		// Log the verified delegation claims: who the call is for (sub), which
		// audience it was minted for (aud), who acted for it (act.sub — the
		// full chain, extension → agent), and the granted scope.
		sub := tok.Subject()
		aud := tok.Audience()
		scope, _ := tok.Get("scope")
		actSub := ""
		if actRaw, ok := tok.Get("act"); ok {
			if act, ok := actRaw.(map[string]interface{}); ok {
				if s, ok := act["sub"].(string); ok {
					actSub = s
				}
			}
		}
		log.Printf("[SupplyChain] Token verified — sub=%s aud=%v act.sub=%s scope=%q caller=%s", sub, aud, actSub, scope, email)

		ctx := context.WithValue(r.Context(), ctxKeyCallerEmail, email)
		r = r.WithContext(ctx)
		mcpServer.ServeHTTP(w, r)
	})
}
