package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// pingoneAuthorizeClient calls PingOne Authorize for a PERMIT/DENY decision on
// each tools/call (and a2a message:send) body. Fails closed: any error,
// non-200, or non-PERMIT decision is treated as DENY by the caller.
type pingoneAuthorizeClient struct {
	decisionEndpoint string
	tokenEndpoint    string
	clientID         string
	clientSecret     string

	mu    sync.Mutex
	token cachedToken
}

// Decide sends exactly the attributes this journey's policies consume and
// returns true for PERMIT, false for DENY or INDETERMINATE. (aobou's fork
// additionally sends agent_client_id/tool_name/amount_cents for its
// amount-limit policy; nothing here reads those.)
func (c *pingoneAuthorizeClient) Decide(userSub string, requestHour int) (bool, error) {
	tok, err := c.accessToken()
	if err != nil {
		return false, fmt.Errorf("authorize token: %w", err)
	}

	body, _ := json.Marshal(struct {
		Parameters map[string]any `json:"parameters"`
	}{
		Parameters: map[string]any{
			// Keys must byte-match the Trust Framework attribute Names
			// (User Sub, Request Hour) — the request-parameter resolver
			// matches on the attribute name, not the "Resolver Name" label.
			"User Sub":     userSub,
			"Request Hour": requestHour,
		},
	})

	req, err := http.NewRequest(http.MethodPost, c.decisionEndpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("decision request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("decision endpoint HTTP %d: %s", resp.StatusCode, raw)
	}

	var out struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("parse decision response: %w", err)
	}
	return out.Decision == "PERMIT", nil
}

func (c *pingoneAuthorizeClient) accessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.token.token != "" && now.Before(c.token.expires) {
		return c.token.token, nil
	}
	tok, expiresIn, err := fetchToken(c.tokenEndpoint, c.clientID, c.clientSecret,
		url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	c.token = cachedToken{token: tok, expires: now.Add(tokenTTL(expiresIn))}
	return tok, nil
}
