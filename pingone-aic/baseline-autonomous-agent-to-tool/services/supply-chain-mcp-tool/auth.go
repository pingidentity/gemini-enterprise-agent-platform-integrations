// Token validation — the security boundary of this service. The gateway
// injects a scoped PingOne access token; this middleware independently verifies
// signature, issuer, audience, expiry, and scope before any MCP handler runs.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type tokenValidator struct {
	issuer        string
	audience      string
	jwksURL       string
	requiredScope string
	keys          *jwk.Cache
}

func newTokenValidator(ctx context.Context) (*tokenValidator, error) {
	get := func(name string) (string, error) {
		if v := os.Getenv(name); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s is required", name)
	}

	issuer, err := get("IDP_ISSUER")
	if err != nil {
		return nil, err
	}
	audience, err := get("IDP_REQUIRED_AUDIENCE")
	if err != nil {
		return nil, err
	}
	requiredScope, err := get("IDP_REQUIRED_SCOPE")
	if err != nil {
		return nil, err
	}

	// Derive JWKS URL from issuer — same convention as PingOne's discovery doc.
	jwksURL := strings.TrimSuffix(issuer, "/") + "/jwks"

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS url: %w", err)
	}
	// Warm the cache so a bad issuer URL fails fast at startup rather than per-request.
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[SupplyChain] Token validation ON — issuer=%s audience=%s scope=%s", issuer, audience, requiredScope)
	return &tokenValidator{issuer: issuer, audience: audience, jwksURL: jwksURL, requiredScope: requiredScope, keys: cache}, nil
}

func (v *tokenValidator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[SupplyChain] %s %s (X-Spiffe-Id: %s)", r.Method, r.URL.Path, r.Header.Get("X-Spiffe-Id"))

		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("[SupplyChain] REJECT — no Bearer token")
			http.Error(w, "missing Bearer token", http.StatusUnauthorized)
			return
		}

		tok, err := v.verify(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			log.Printf("[SupplyChain] REJECT — %v", err)
			http.Error(w, "invalid token: "+err.Error(), http.StatusForbidden)
			return
		}

		// Log the verified identity: sub (who the call is for), act.sub (who
		// acted for it — the delegation proof), and the granted scope. Raw
		// tokens are never logged.
		var actSub string
		if act, ok := tok.Get("act"); ok {
			if m, ok := act.(map[string]interface{}); ok {
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
	set, err := v.keys.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("load JWKS: %w", err)
	}

	// PingOne's JWKS keys omit the "alg" field — infer it from the key type,
	// otherwise jwx refuses to verify the signature.
	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return nil, err
	}

	if !hasScope(tok, v.requiredScope) {
		return nil, fmt.Errorf("missing required scope %q", v.requiredScope)
	}
	return tok, nil
}

// hasScope checks both the space-delimited "scope" string claim and the "scp"
// array claim — PingOne uses the former; the latter is accepted for robustness.
func hasScope(tok jwt.Token, want string) bool {
	if raw, ok := tok.Get("scope"); ok {
		if s, ok := raw.(string); ok && slices.Contains(strings.Fields(s), want) {
			return true
		}
	}
	if raw, ok := tok.Get("scp"); ok {
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && s == want {
					return true
				}
			}
		}
	}
	return false
}
