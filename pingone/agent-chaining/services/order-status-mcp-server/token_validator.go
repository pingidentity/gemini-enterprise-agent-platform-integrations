package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type tokenValidator struct {
	issuer        string
	audience      string
	requiredScope string
	keys          *jwk.Cache
	jwksURL       string
}

func newTokenValidator(ctx context.Context) (*tokenValidator, error) {
	issuer, err := requireEnv("IDP_ISSUER")
	if err != nil {
		return nil, err
	}
	audience, err := requireEnv("IDP_REQUIRED_AUDIENCE")
	if err != nil {
		return nil, err
	}
	scope, err := requireEnv("IDP_REQUIRED_SCOPE")
	if err != nil {
		return nil, err
	}

	jwksURL := strings.TrimSuffix(issuer, "/") + "/jwks"
	keys := jwk.NewCache(ctx)
	if err := keys.Register(jwksURL); err != nil {
		return nil, fmt.Errorf("register JWKS: %w", err)
	}
	if _, err := keys.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	return &tokenValidator{issuer: issuer, audience: audience, requiredScope: scope, keys: keys, jwksURL: jwksURL}, nil
}

func (v *tokenValidator) verify(ctx context.Context, raw string) (jwt.Token, error) {
	set, err := v.keys.Get(ctx, v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("load JWKS: %w", err)
	}
	tok, err := jwt.Parse([]byte(raw), jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience))
	if err != nil {
		return nil, err
	}
	if !hasScope(tok, v.requiredScope) {
		return nil, fmt.Errorf("missing required scope %q", v.requiredScope)
	}
	if subject, ok := tok.Get("sub"); !ok || subject == "" {
		return nil, fmt.Errorf("token missing sub claim")
	}
	return tok, nil
}

func hasScope(tok jwt.Token, wanted string) bool {
	raw, ok := tok.Get("scope")
	if !ok {
		return false
	}
	scope, ok := raw.(string)
	return ok && strings.Contains(" "+scope+" ", " "+wanted+" ")
}
