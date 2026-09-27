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
	// Warm the cache so a bad issuer URL fails fast at startup rather than per-request.
	if _, err := jwksCache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[SupplyChain] Token validation ON — issuer=%s audience=%s scope=%s", requiredIssuer, requiredAudience, requiredScope)
	return &tokenValidator{jwksURL: jwksURL, jwksCache: jwksCache, requiredIssuer: requiredIssuer, requiredAudience: requiredAudience, requiredScope: requiredScope}, nil
}

// middleware wraps an MCP handler with bearer validation: reject 401/403
// before the handler runs, log the verified identity, then forward.
func (v *tokenValidator) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("[SupplyChain] REJECT — no Bearer token")
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"invalid_token"}`)
			return
		}

		tok, err := v.verify(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))
		if err != nil {
			log.Printf("[SupplyChain] token validation failed: %v", err)
			// RFC 6750: a malformed/expired token is 401 with an
			// error="invalid_token" challenge; a valid token lacking the
			// required scope is 403 with error="insufficient_scope". The
			// validation detail goes to the log only — never echoed to the
			// caller.
			if errors.Is(err, errInsufficientScope) {
				w.Header().Set("WWW-Authenticate",
					fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, v.requiredScope))
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

		// Log the verified identity: sub (who the call is for), act.sub (who
		// acted for it — the delegation proof), and the granted scope. Raw
		// tokens are never logged.
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
		// Email is injected by the extension service; empty for tool-discovery requests.
		email := r.Header.Get("X-User-Email")
		log.Printf("[SupplyChain] Token verified — sub=%v aud=%v act.sub=%s scope=%q caller=%s", sub, aud, actSub, scope, email)

		ctx := context.WithValue(r.Context(), ctxKeyCallerEmail, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (v *tokenValidator) verify(ctx context.Context, raw string) (jwt.Token, error) {
	set, err := v.jwksCache.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("load JWKS: %w", err)
	}

	// PingOne's JWKS keys omit the "alg" field — infer it from the key type,
	// otherwise jwx refuses to verify the signature.
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
