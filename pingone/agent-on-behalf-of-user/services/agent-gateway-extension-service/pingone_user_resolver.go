package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const pingOneMgmtScope = "p1:read:user"

// pingoneUserResolver resolves a PingOne user's email address from their sub
// claim using the PingOne management API. It uses the extension service's own
// client_credentials token (same credentials as the IDP exchange) and caches
// both the management token and resolved emails to minimise latency.
//
// The extension service's PingOne worker app must have the "Identity Data Read
// Only" role assigned (scoped to the environment) for this to work.
type pingoneUserResolver struct {
	envID         string
	apiBase       string
	tokenEndpoint string
	clientID      string
	clientSecret  string

	mu           sync.Mutex
	mgmtToken    string
	mgmtExpires  time.Time
	emailCache   map[string]string // sub → email
}

// emailForSub returns the email for the given PingOne sub, using cache when possible.
func (r *pingoneUserResolver) emailForSub(sub string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.emailCache == nil {
		r.emailCache = make(map[string]string)
	}
	if email, ok := r.emailCache[sub]; ok {
		return email, nil
	}

	tok, err := r.refreshMgmtToken()
	if err != nil {
		return "", fmt.Errorf("management token: %w", err)
	}

	// In PingOne the user's sub IS their id — use id eq for the filter.
	apiURL := fmt.Sprintf("%s/environments/%s/users/%s", r.apiBase, r.envID, sub)

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build user lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("user lookup request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("user lookup returned HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode user lookup response: %w", err)
	}
	email := result.Email
	if email == "" {
		return "", fmt.Errorf("user %q has no email", sub)
	}

	r.emailCache[sub] = email
	return email, nil
}

// refreshMgmtToken returns a valid management token, fetching a new one when expired.
// Must be called with r.mu held.
func (r *pingoneUserResolver) refreshMgmtToken() (string, error) {
	if r.mgmtToken != "" && time.Now().Before(r.mgmtExpires) {
		return r.mgmtToken, nil
	}
	tok, expiresIn, err := fetchToken(r.tokenEndpoint, r.clientID, r.clientSecret,
		url.Values{
			"grant_type": {"client_credentials"},
			"scope":      {pingOneMgmtScope},
		})
	if err != nil {
		return "", err
	}
	r.mgmtToken = tok
	r.mgmtExpires = time.Now().Add(tokenTTL(expiresIn))
	return tok, nil
}

// parsePingOneCoords derives the PingOne env ID and management API base URL
// from a token endpoint URL of the form:
//
//	https://auth.pingone.<region>/<env-id>/as/token
func parsePingOneCoords(tokenEndpoint string) (envID, apiBase string, err error) {
	withoutScheme := strings.TrimPrefix(tokenEndpoint, "https://")
	parts := strings.SplitN(withoutScheme, "/", 4)
	if len(parts) < 2 || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse env ID from %q", tokenEndpoint)
	}
	envID = parts[1]
	apiHost := strings.Replace(parts[0], "auth.", "api.", 1)
	apiBase = "https://" + apiHost + "/v1"
	return envID, apiBase, nil
}
