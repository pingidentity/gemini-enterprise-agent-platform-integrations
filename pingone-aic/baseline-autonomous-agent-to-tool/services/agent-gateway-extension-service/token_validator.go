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

type delegatedTokenValidator struct {
	issuer   string
	audience string
	scope    string
	jwksURL  string
	keys     *jwk.Cache
}

func newDelegatedTokenValidator(ctx context.Context, idpTokenEndpoint, audience, scope string) (*delegatedTokenValidator, error) {
	// Derive issuer and JWKS URL from the token endpoint.
	// IDP_TOKEN_ENDPOINT form: https://auth.pingone.<region>/<env-id>/as/token
	issuer := strings.TrimSuffix(idpTokenEndpoint, "/token")
	jwksURL := issuer + "/jwks"

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS url: %w", err)
	}
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS from %s: %w", jwksURL, err)
	}

	log.Printf("[ExtSvc] token validation ON — issuer=%s audience=%s scope=%s", issuer, audience, scope)
	return &delegatedTokenValidator{
		issuer:   issuer,
		audience: audience,
		scope:    scope,
		jwksURL:  jwksURL,
		keys:     cache,
	}, nil
}

func (v *delegatedTokenValidator) verify(ctx context.Context, raw string) error {
	set, err := v.keys.Get(ctx, v.jwksURL)
	if err != nil {
		return fmt.Errorf("load JWKS: %w", err)
	}

	// PingOne JWKS keys omit the "alg" field — infer from key type.
	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return err
	}

	if !hasScope(tok, v.scope) {
		return fmt.Errorf("missing required scope %q", v.scope)
	}
	return nil
}

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
