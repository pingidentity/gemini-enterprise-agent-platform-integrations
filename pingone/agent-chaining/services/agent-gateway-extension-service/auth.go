// Token validation — the security boundary of this service. The gateway
// forwards the caller agent's PingOne delegated access token; this validator
// independently verifies signature, issuer, audience, expiry, and the hop's
// scope before the token is exchanged (reminted) for the target-audienced one.
// One validator instance per governed target (A2A and MCP).
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type tokenValidator struct {
	jwksURL          string
	jwksCache        *jwk.Cache
	requiredIssuer   string
	requiredAudience string
	requiredScope    string
}

func newTokenValidator(ctx context.Context, requiredIssuer, requiredAudience, requiredScope string) (*tokenValidator, error) {
	jwksURL := strings.TrimSuffix(requiredIssuer, "/") + "/jwks"

	jwksCache := jwk.NewCache(ctx)
	if err := jwksCache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS url: %w", err)
	}
	// Warm the cache so a bad issuer URL fails fast at startup rather than per-request.
	if _, err := jwksCache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[ExtSvc] Token validation ON — issuer=%s audience=%s scope=%s", requiredIssuer, requiredAudience, requiredScope)
	return &tokenValidator{jwksURL: jwksURL, jwksCache: jwksCache, requiredIssuer: requiredIssuer, requiredAudience: requiredAudience, requiredScope: requiredScope}, nil
}

func (v *tokenValidator) verify(ctx context.Context, raw string) error {
	set, err := v.jwksCache.Get(ctx, v.jwksURL)
	if err != nil {
		return fmt.Errorf("load JWKS: %w", err)
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
		return err
	}

	if !hasScope(tok, v.requiredScope) {
		return fmt.Errorf("missing required scope %q", v.requiredScope)
	}
	return nil
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
