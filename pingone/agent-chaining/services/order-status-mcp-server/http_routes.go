package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/mark3labs/mcp-go/server"
)

type ctxKeyCallerSub struct{}

func newRouter(mcpServer *server.StreamableHTTPServer, validator *tokenValidator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tok, err := validator.verify(r.Context(), strings.TrimPrefix(auth, "Bearer "))
		if err != nil {
			log.Printf("[OrderStatusMCP] auth rejected: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// Log the verified delegation claims: who the call is for (sub, the
		// user throughout), which audience it was minted for (aud), who acted
		// for it (act.sub — the gateway extension's remint, the delegation
		// proof), and the granted scope.
		log.Printf("[OrderStatusMCP] Token verified — sub=%s aud=%v act.sub=%s scope=%q",
			tok.Subject(), tok.Audience(), actSub(tok), scopeOf(tok))
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyCallerSub{}, tok.Subject()))
		mcpServer.ServeHTTP(w, r)
	})
}

// actSub flattens the token's nested act claim (extension -> agent -> ...) into
// a single " -> " string for logging.
func actSub(tok jwt.Token) string {
	actRaw, ok := tok.Get("act")
	if !ok {
		return "<none>"
	}
	parts := []string{}
	act, ok := actRaw.(map[string]interface{})
	for ok && act != nil {
		if s, ok := act["sub"].(string); ok && s != "" {
			parts = append(parts, s)
		}
		act, ok = act["act"].(map[string]interface{})
	}
	if len(parts) == 0 {
		return "<none>"
	}
	return strings.Join(parts, " -> ")
}

func scopeOf(tok jwt.Token) string {
	if s, ok := tok.Get("scope"); ok {
		if str, ok := s.(string); ok {
			return str
		}
	}
	return ""
}
