package main

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
)

// pingOneClient performs the PingOne RFC 8693 token exchange for both governed
// hops (A2A and MCP) — the step that turns the calling agent's inbound token
// into one audienced to this hop's real final audience, presenting this
// service's own PingOne token as the actor. This is the only party in the
// journey that ever exchanges onto the real final resources.
//
// PingOne picks the audience from the requested scope's resource mapping,
// so the exchange itself names no resource — but both the actor fetch and
// the exchange send the scope explicitly.
//
// Both the actor tokens and the exchanged tokens are cached. Exchanged tokens
// are keyed by the exact subject token — same subject in, same result out —
// so one identity's token is never handed to another identity's request.
type pingOneClient struct {
	clientID      string
	clientSecret  string
	tokenEndpoint string

	mu sync.Mutex
	// actorTokensByScope caches actor tokens per requested scope. PingOne's
	// client_credentials grant rejects an unscoped request once the client
	// has scopes assigned across more than one resource ("May not request
	// scopes for multiple resources"), so each actor fetch must name exactly
	// the scope it needs for the target it's about to be presented to.
	actorTokensByScope map[string]cachedAccessToken
	exchangedTokens    map[string]cachedAccessToken // keyed by target + subject token
}

// exchangeAgentTokenForResourceToken returns a token audienced to the target's
// final audience, delegated from the subject token, using the cache when
// possible.
func (c *pingOneClient) exchangeAgentTokenForResourceToken(subjectToken, targetName, audience, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.actorTokensByScope == nil {
		c.actorTokensByScope = make(map[string]cachedAccessToken)
	}
	if c.exchangedTokens == nil {
		c.exchangedTokens = make(map[string]cachedAccessToken)
	}

	now := time.Now()
	for k, v := range c.exchangedTokens {
		if now.After(v.expires) {
			delete(c.exchangedTokens, k)
		}
	}
	cacheKey := targetName + ":" + subjectToken
	if cached, ok := c.exchangedTokens[cacheKey]; ok && now.Before(cached.expires) {
		return cached.token, nil
	}

	actor, err := c.refreshActorToken(now, scope)
	if err != nil {
		return "", fmt.Errorf("actor client_credentials: %w", err)
	}

	tok, ttl, err := c.performDelegationTokenExchange(subjectToken, actor, audience, scope)
	if err != nil {
		return "", err
	}
	c.exchangedTokens[cacheKey] = cachedAccessToken{token: tok, expires: now.Add(ttl)}
	log.Printf("[ExtSvc] delegated token minted target=%s ttl=%v", targetName, ttl)
	return tok, nil
}

// refreshActorToken returns the cached actor token for the given scope,
// fetching a new one if expired or not yet cached. Must be called with c.mu
// held.
func (c *pingOneClient) refreshActorToken(now time.Time, scope string) (string, error) {
	if cached, ok := c.actorTokensByScope[scope]; ok && now.Before(cached.expires) {
		return cached.token, nil
	}
	tok, expiresIn, err := fetchOAuthToken(c.tokenEndpoint, c.clientID, c.clientSecret,
		url.Values{"grant_type": {"client_credentials"}, "scope": {scope}})
	if err != nil {
		return "", err
	}
	c.actorTokensByScope[scope] = cachedAccessToken{token: tok, expires: now.Add(getTokenCacheLifetime(expiresIn))}
	return tok, nil
}

// performDelegationTokenExchange performs the RFC 8693 token exchange: the
// subject token plus this service's actor token, minted into a token
// audienced to the target resource.
func (c *pingOneClient) performDelegationTokenExchange(subjectToken, actorToken, audience, scope string) (string, time.Duration, error) {
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {subjectToken},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:access_token"},
		"actor_token":          {actorToken},
		"actor_token_type":     {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	if audience != "" {
		form.Set("audience", audience)
	}
	tok, expiresIn, err := fetchOAuthToken(c.tokenEndpoint, c.clientID, c.clientSecret, form)
	if err != nil {
		return "", 0, err
	}
	return tok, getTokenCacheLifetime(expiresIn), nil
}
