package main

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
)

// pingOneClient performs the PingOne RFC 8693 token exchange — the step that
// turns the agent's inbound token into a tool-audienced one. It swaps the
// agent's token (the subject) for one audienced to the MCP tool, presenting
// this service's own PingOne token as the actor.
//
// Both the actor token and the exchanged tokens are cached. Exchanged tokens
// are keyed by the exact subject token — same subject in, same result out —
// so one identity's token is never handed to another identity's request.
type pingOneClient struct {
	clientID      string
	clientSecret  string
	tokenEndpoint string
	targetScope   string // requested on the exchanged token; maps to the tool resource's audience

	mu              sync.Mutex
	actorToken      cachedAccessToken            // the service's own client_credentials token
	exchangedTokens map[string]cachedAccessToken // tool tokens keyed by subject token
}

// exchangeAgentTokenForResourceToken returns a tool-audienced token delegated
// from the agent's subject token, using the cache when possible.
func (c *pingOneClient) exchangeAgentTokenForResourceToken(subjectToken string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exchangedTokens == nil {
		c.exchangedTokens = make(map[string]cachedAccessToken)
	}

	now := time.Now()
	for k, v := range c.exchangedTokens {
		if now.After(v.expires) {
			delete(c.exchangedTokens, k)
		}
	}
	if cached, ok := c.exchangedTokens[subjectToken]; ok && now.Before(cached.expires) {
		return cached.token, nil
	}

	actor, err := c.refreshActorToken(now)
	if err != nil {
		return "", fmt.Errorf("actor client_credentials: %w", err)
	}

	tok, ttl, err := c.performDelegationTokenExchange(subjectToken, actor)
	if err != nil {
		return "", err
	}
	c.exchangedTokens[subjectToken] = cachedAccessToken{token: tok, expires: now.Add(ttl)}
	log.Printf("[ExtSvc] delegated tool token minted (ttl %v)", ttl)
	return tok, nil
}

// refreshActorToken returns the cached actor token, fetching a new one if
// expired. Must be called with c.mu held.
func (c *pingOneClient) refreshActorToken(now time.Time) (string, error) {
	if c.actorToken.token != "" && now.Before(c.actorToken.expires) {
		return c.actorToken.token, nil
	}
	tok, expiresIn, err := fetchOAuthToken(c.tokenEndpoint, c.clientID, c.clientSecret,
		url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	c.actorToken = cachedAccessToken{token: tok, expires: now.Add(getTokenCacheLifetime(expiresIn))}
	return tok, nil
}

// performDelegationTokenExchange performs the RFC 8693 token exchange: the
// agent's subject token plus this service's actor token, minted into a token
// audienced to the target resource.
func (c *pingOneClient) performDelegationTokenExchange(subjectToken, actorToken string) (string, time.Duration, error) {
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {subjectToken},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:access_token"},
		"actor_token":          {actorToken},
		"actor_token_type":     {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
	}
	if c.targetScope != "" {
		form.Set("scope", c.targetScope)
	}
	tok, expiresIn, err := fetchOAuthToken(c.tokenEndpoint, c.clientID, c.clientSecret, form)
	if err != nil {
		return "", 0, err
	}
	return tok, getTokenCacheLifetime(expiresIn), nil
}
