package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// pingAuthorizeClient calls PingAuthorize's governance-engine decision endpoint
// for a PERMIT/DENY decision. Unlike the PingOne SaaS variant of this service
// (which fetched a client-credentials token per client), PingAuthorize
// authenticates callers with a static shared secret in the CLIENT-TOKEN header.
type pingAuthorizeClient struct {
	decisionEndpoint string
	sharedSecret     string
}

// Decide sends the request attributes to PingAuthorize and returns true for
// authorised, false for anything else (NOT_APPLICABLE, DENY, errors — fail
// closed). The deployed policies consume the agent's identity and the request
// hour (business hours + agent allow-list).
func (c *pingAuthorizeClient) Decide(agentClientID string, requestHour int) (bool, error) {
	body, _ := json.Marshal(struct {
		Domain     string         `json:"domain"`
		Action     string         `json:"action"`
		Service    string         `json:"service"`
		Attributes map[string]any `json:"attributes"`
	}{
		Domain:  "baatt",
		Action:  "restock",
		Service: "Supply Chain MCP Tool",
		Attributes: map[string]any{
			// Keys must match what the deployed policies' conditions read.
			"Agent Client ID": agentClientID,
			"Request Hour":    requestHour,
		},
	})

	req, err := http.NewRequest(http.MethodPost, c.decisionEndpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("CLIENT-TOKEN", c.sharedSecret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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
		Decision   string `json:"decision"`
		Authorised bool   `json:"authorised"`
		Status     struct {
			Code string `json:"code"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return false, fmt.Errorf("parse decision response: %w", err)
	}
	// Fail closed: only explicit authorised=true counts. NOT_APPLICABLE,
	// DENY, and anything unexpected all deny.
	return out.Status.Code == "OKAY" && out.Authorised, nil
}
