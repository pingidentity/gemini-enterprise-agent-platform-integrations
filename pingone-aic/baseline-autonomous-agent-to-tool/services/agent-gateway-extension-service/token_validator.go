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

func newDelegatedTokenValidator(ctx context.Context, issuer, audience, scope string) (*delegatedTokenValidator, error) {
	// IDP_ISSUER must be the exact `iss` claim of the AIC token, port included:
	// https://<tenant>:443/am/oauth2/realms/root/realms/<realm>
	// AIC exposes keys at <issuer>/connect/jwk_uri (PingOne SaaS used <issuer>/jwks).
	jwksURL := strings.TrimSuffix(issuer, "/") + "/connect/jwk_uri"

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

	// AIC's JWKS keys omit the "alg" field — infer from key type.
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

// hasScope handles both serializations: PingOne SaaS sent "scope" as a
// space-delimited string; AIC sends it as a JSON array.
func hasScope(tok jwt.Token, want string) bool {
	if raw, ok := tok.Get("scope"); ok {
		switch s := raw.(type) {
		case string:
			if slices.Contains(strings.Fields(s), want) {
				return true
			}
		case []any:
			for _, item := range s {
				if str, ok := item.(string); ok && str == want {
					return true
				}
			}
		}
	}
	return false
}
