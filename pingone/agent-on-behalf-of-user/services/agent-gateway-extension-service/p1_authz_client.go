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

type pingoneAuthorizeClient struct {
	clientID         string
	clientSecret     string
	tokenEndpoint    string
	decisionEndpoint string

	mu    sync.Mutex
	token cachedAccessToken
}

// getPingOneAuthorizeDecision sends the request parameters to PingOne Authorize
// and returns true for PERMIT, false for DENY or INDETERMINATE. Map keys are the
// wire contract: each must byte-match a Trust Framework attribute name exactly.
func (c *pingoneAuthorizeClient) getPingOneAuthorizeDecision(params map[string]any) (bool, error) {
	token, err := c.getPingOneAuthorizeAccessToken()
	if err != nil {
		return false, fmt.Errorf("authorize token: %w", err)
	}

	requestBody, _ := json.Marshal(struct {
		Parameters map[string]any `json:"parameters"`
	}{Parameters: params})

	req, err := http.NewRequest(http.MethodPost, c.decisionEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := pingoneHTTPClient.Do(req)
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

func (c *pingoneAuthorizeClient) getPingOneAuthorizeAccessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.token.token != "" && now.Before(c.token.expires) {
		return c.token.token, nil
	}
	tok, expiresIn, err := fetchOAuthToken(c.tokenEndpoint, c.clientID, c.clientSecret,
		url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	c.token = cachedAccessToken{token: tok, expires: now.Add(getTokenCacheLifetime(expiresIn))}
	return tok, nil
}
