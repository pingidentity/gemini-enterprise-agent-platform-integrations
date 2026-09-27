package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

var pingoneHTTPClient = &http.Client{Timeout: 10 * time.Second}

// requireEnv returns the value of the given environment variable or fatally exits if unset.
func requireEnv(name string) string {
	val := os.Getenv(name)
	if val == "" {
		log.Fatalf("required environment variable %s is not set", name)
	}
	return val
}

// readHeader reads one header out of an ext_proc HeaderMap.
// The protobuf spec allows a value to arrive in either Value (UTF-8 string)
// or RawValue (bytes), which one depends on the sender.
func readHeader(extProcHeaderMap *corev3.HeaderMap, key string) string {
	if extProcHeaderMap == nil {
		return ""
	}
	for _, h := range extProcHeaderMap.Headers {
		if strings.EqualFold(h.Key, key) {
			if h.Value != "" {
				return h.Value
			}
			return string(h.RawValue)
		}
	}
	return ""
}

// readJWTClaim reads one string claim from a JWT's payload.
// It does NOT verify the signature.
func readJWTClaim(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	v, _ := claims[claim].(string)
	return v
}

// readMCPMethod returns the JSON-RPC "method" field from an MCP request body
// (e.g. "tools/call"). A body that isn't JSON parses to "", which callers
// treat as "not a method that needs special handling".
func readMCPMethod(mcpRequestBody []byte) string {
	var jsonrpcRequest struct {
		Method string `json:"method"`
	}
	json.Unmarshal(mcpRequestBody, &jsonrpcRequest) //nolint:errcheck — returns "" on failure, which is safe
	return jsonrpcRequest.Method
}

// fetchOAuthToken POSTs an OAuth request to a token endpoint (client id/secret as
// Basic auth, the given form fields as the body) and returns the access token
// and its expires_in value.
func fetchOAuthToken(tokenEndpoint, clientID, clientSecret string, form url.Values) (string, int, error) {
	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := pingoneHTTPClient.Do(req)
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
		return "", 0, fmt.Errorf("empty access_token from %s", tokenEndpoint)
	}
	return out.AccessToken, out.ExpiresIn, nil
}

// getTokenCacheLifetime turns a token's expires_in (seconds) into how long to cache it:
// the token's lifetime minus 30s, so a cached token is always refreshed
// before it actually expires. Never returns less than 10s.
func getTokenCacheLifetime(expiresIn int) time.Duration {
	ttl := time.Duration(expiresIn)*time.Second - 30*time.Second
	if ttl < 10*time.Second {
		return 10 * time.Second
	}
	return ttl
}

// cachedAccessToken is an access token paired with the moment it stops being usable.
type cachedAccessToken struct {
	token   string
	expires time.Time
}
