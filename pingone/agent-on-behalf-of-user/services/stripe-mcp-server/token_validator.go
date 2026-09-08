package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

var (
	idpIssuer         string
	mcpTokenAudience  string
	idpJwksURL        string
	rawRequiredScopes string

	jwksMu      sync.Mutex
	jwksCache   jwk.Set
	jwksExpires time.Time
)

const jwksTTL = 5 * time.Minute

// validateToken verifies the bearer token's signature, iss, aud, and required
// scopes. On success it returns the parsed token so callers can log its claims.
func validateToken(bearerHeader string) (jwt.Token, error) {
	raw := strings.TrimPrefix(bearerHeader, "Bearer ")
	raw = strings.TrimPrefix(raw, "bearer ")
	if raw == "" {
		return nil, fmt.Errorf("missing bearer token")
	}

	ks, err := getJWKS()
	if err != nil {
		return nil, fmt.Errorf("jwks unavailable: %w", err)
	}

	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(ks, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
	)
	if err != nil {
		return nil, fmt.Errorf("token signature or expiry invalid: %w", err)
	}

	if tok.Issuer() != idpIssuer {
		return nil, fmt.Errorf("unexpected issuer %q (want %q)", tok.Issuer(), idpIssuer)
	}

	if mcpTokenAudience != "" {
		found := false
		for _, a := range tok.Audience() {
			if a == mcpTokenAudience {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("token audience %v does not include %q", tok.Audience(), mcpTokenAudience)
		}
	}

	scopeVal, _ := tok.Get("scope")
	scopeStr, _ := scopeVal.(string)
	grantedSet := make(map[string]struct{})
	for _, s := range strings.Fields(scopeStr) {
		grantedSet[s] = struct{}{}
	}
	for _, required := range requiredScopeList() {
		if _, ok := grantedSet[required]; !ok {
			return nil, fmt.Errorf("token is missing required scope %q", required)
		}
	}

	return tok, nil
}

func requiredScopeList() []string {
	return strings.Fields(rawRequiredScopes)
}

// initIdpJwksURL derives idpJwksURL from idpIssuer. Must be called after idpIssuer is set.
func initIdpJwksURL() error {
	if idpIssuer == "" {
		return fmt.Errorf("IDP_ISSUER is required")
	}
	idpJwksURL = idpIssuer + "/jwks"
	return nil
}

// getJWKS returns the cached key set, refreshing it if the TTL has elapsed.
func getJWKS() (jwk.Set, error) {
	jwksMu.Lock()
	defer jwksMu.Unlock()

	if jwksCache != nil && time.Now().Before(jwksExpires) {
		return jwksCache, nil
	}

	ks, err := jwk.Fetch(context.Background(), idpJwksURL)
	if err != nil {
		return nil, err
	}
	jwksCache = ks
	jwksExpires = time.Now().Add(jwksTTL)
	return ks, nil
}
