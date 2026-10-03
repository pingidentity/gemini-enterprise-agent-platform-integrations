// Token validation — the security boundary of this service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

var errInsufficientScope = errors.New("insufficient scope")

type tokenValidator struct {
	jwksURL          string
	jwksCache        *jwk.Cache
	requiredIssuer   string
	requiredAudience string
	requiredScope    string
}

func newTokenValidator(ctx context.Context) (*tokenValidator, error) {
	requiredIssuer := requireEnv("IDP_ISSUER")
	requiredAudience := requireEnv("IDP_REQUIRED_AUDIENCE")
	requiredScope := requireEnv("IDP_REQUIRED_SCOPE")
	jwksURL := strings.TrimSuffix(requiredIssuer, "/") + "/jwks"

	jwksCache := jwk.NewCache(ctx)
	if err := jwksCache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS url: %w", err)
	}
	if _, err := jwksCache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[SupplyChain] Token validation ON — issuer=%s audience=%s scope=%s", requiredIssuer, requiredAudience, requiredScope)
	return &tokenValidator{jwksURL: jwksURL, jwksCache: jwksCache, requiredIssuer: requiredIssuer, requiredAudience: requiredAudience, requiredScope: requiredScope}, nil
}

func (v *tokenValidator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("[SupplyChain] REJECT — no Bearer token")
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "missing Bearer token", http.StatusUnauthorized)
			return
		}

		tok, err := v.verify(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			log.Printf("[SupplyChain] token validation failed: %v", err)
			if errors.Is(err, errInsufficientScope) {
				w.Header().Set("WWW-Authenticate",
					fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, v.requiredScope))
				http.Error(w, "insufficient scope", http.StatusForbidden)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		var actSub string
		if act, ok := tok.Get("act"); ok {
			if m, ok := act.(map[string]any); ok {
				if s, ok := m["sub"].(string); ok {
					actSub = s
				}
			}
		}
		sub, _ := tok.Get("sub")
		aud, _ := tok.Get("aud")
		scope, _ := tok.Get("scope")
		log.Printf("[SupplyChain] Token verified — sub=%v aud=%v act.sub=%s scope=%q forwarding to MCP handler", sub, aud, actSub, scope)
		next.ServeHTTP(w, r)
	})
}

func (v *tokenValidator) verify(ctx context.Context, raw string) (jwt.Token, error) {
	set, err := v.jwksCache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("load JWKS: %w", err)
	}

	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.requiredIssuer),
		jwt.WithAudience(v.requiredAudience),
	)
	if err != nil {
		return nil, err
	}

	if !hasScope(tok, v.requiredScope) {
		return nil, fmt.Errorf("%w: missing required scope %q", errInsufficientScope, v.requiredScope)
	}
	return tok, nil
}

func hasScope(tok jwt.Token, want string) bool {
	if raw, ok := tok.Get("scope"); ok {
		if s, ok := raw.(string); ok && slices.Contains(strings.Fields(s), want) {
			return true
		}
	}
	return false
}
