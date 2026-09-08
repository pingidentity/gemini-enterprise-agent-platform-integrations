package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// httpClient is used for all outbound PingOne calls. The timeout prevents a
// hung IdP from blocking the ext_proc stream indefinitely.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// fetchToken POSTs form-encoded credentials to a token endpoint and returns
// the access token and its expires_in value. Both idpClient and
// pingoneAuthorizeClient use this so the HTTP/parse logic lives in one place.
func fetchToken(endpoint, clientID, clientSecret string, form url.Values) (string, int, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, body)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", 0, fmt.Errorf("parse token response: %w", err)
	}
	if out.AccessToken == "" {
		return "", 0, fmt.Errorf("empty access_token from %s", endpoint)
	}
	return out.AccessToken, out.ExpiresIn, nil
}

// tokenTTL converts expires_in to a cache duration, refreshing 30s early and
// clamping to a 10s minimum so we never cache a token that's about to expire.
func tokenTTL(expiresIn int) time.Duration {
	ttl := time.Duration(expiresIn)*time.Second - 30*time.Second
	if ttl < 10*time.Second {
		return 10 * time.Second
	}
	return ttl
}

// cachedToken is a token with its expiry, used by both clients.
type cachedToken struct {
	token   string
	expires time.Time
}

// idpClient performs a PingOne RFC 8693 delegation exchange. It swaps the
// *agent's* PingOne token (the subject) for one audienced to the MCP tool,
// presenting the extension service's own PingOne token as the actor. Because
// the subject token was issued by PingOne (the agent obtained it via
// client_credentials), no external-issuer trust is required.
//
// PingOne picks the tool audience from the requested scope's resource mapping,
// so there is no explicit audience/resource parameter. Both the actor token
// and the exchanged tokens are cached independently until near expiry.
type idpClient struct {
	endpoint     string
	clientID     string
	clientSecret string
	mu           sync.Mutex
	// actorByScope caches actor tokens per requested scope. PingOne's
	// client_credentials grant rejects an unscoped request once the client
	// has scopes assigned across more than one resource ("May not request
	// scopes for multiple resources"), so each actor fetch must name exactly
	// the scope it needs for the target it's about to be presented to.
	actorByScope map[string]cachedToken
	cache        map[string]cachedToken
}

func newIDPClient(endpoint, clientID, clientSecret string) *idpClient {
	return &idpClient{
		endpoint:     endpoint,
		clientID:     clientID,
		clientSecret: clientSecret,
		actorByScope: make(map[string]cachedToken),
		cache:        make(map[string]cachedToken),
	}
}

// exchangeForTarget returns a target-audienced delegated token.
func (c *idpClient) exchangeForTarget(subjectToken, targetName, audience, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.cache {
		if now.After(v.expires) {
			delete(c.cache, k)
		}
	}
	cacheKey := targetName + ":" + subjectToken
	if ct, ok := c.cache[cacheKey]; ok && now.Before(ct.expires) {
		return ct.token, nil
	}

	actor, err := c.refreshActor(now, scope)
	if err != nil {
		return "", fmt.Errorf("actor client_credentials: %w", err)
	}

	tok, ttl, err := c.exchange(subjectToken, actor, audience, scope)
	if err != nil {
		return "", err
	}
	c.cache[cacheKey] = cachedToken{token: tok, expires: now.Add(ttl)}
	log.Printf("[ExtSvc] delegated token minted target=%s ttl=%v", targetName, ttl)
	return tok, nil
}

// refreshActor returns the cached actor token for the given scope, fetching
// a new one if expired or not yet cached. Must be called with c.mu held.
func (c *idpClient) refreshActor(now time.Time, scope string) (string, error) {
	if cached, ok := c.actorByScope[scope]; ok && now.Before(cached.expires) {
		return cached.token, nil
	}
	tok, expiresIn, err := fetchToken(c.endpoint, c.clientID, c.clientSecret,
		url.Values{"grant_type": {"client_credentials"}, "scope": {scope}})
	if err != nil {
		return "", err
	}
	c.actorByScope[scope] = cachedToken{token: tok, expires: now.Add(tokenTTL(expiresIn))}
	return tok, nil
}

// exchange performs the RFC 8693 token exchange.
func (c *idpClient) exchange(subjectToken, actorToken, audience, scope string) (string, time.Duration, error) {
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
	tok, expiresIn, err := fetchToken(c.endpoint, c.clientID, c.clientSecret, form)
	if err != nil {
		return "", 0, err
	}
	return tok, tokenTTL(expiresIn), nil
}
