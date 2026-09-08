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

// pingoneAuthorizeClient calls PingOne Authorize for a compound PERMIT/DENY
// decision using both user and agent attributes.
type pingoneAuthorizeClient struct {
	decisionEndpoint string
	tokenEndpoint    string
	clientID         string
	clientSecret     string

	mu    sync.Mutex
	token cachedToken
}

// Decide sends compound attributes to PingOne Authorize and returns true for
// PERMIT, false for DENY or INDETERMINATE.
func (c *pingoneAuthorizeClient) Decide(userSub, agentClientID, toolName string, amountCents, requestHour int) (bool, error) {
	tok, err := c.accessToken()
	if err != nil {
		return false, fmt.Errorf("authorize token: %w", err)
	}

	body, _ := json.Marshal(struct {
		Parameters map[string]any `json:"parameters"`
	}{
		Parameters: map[string]any{
			// Keys must byte-match the Trust Framework attribute Names
			// (User Sub, Agent Client ID, Tool Name, Amount Cents,
			// Request Hour) — the request-parameter resolver matches on
			// the attribute name, not the "Resolver Name" label.
			"User Sub":        userSub,
			"Agent Client ID": agentClientID,
			"Tool Name":       toolName,
			"Amount Cents":    amountCents,
			"Request Hour":    requestHour,
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
