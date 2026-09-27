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

// pingoneUserResolver resolves a PingOne user's email from their sub via the
// management API, using the Authorize worker's client_credentials token (that
// app holds the Identity Data Read Only role). Caches the token and resolved
// emails; a lookup miss is handled by the caller (best-effort feature).
type pingoneUserResolver struct {
	envID         string
	apiBase       string
	tokenEndpoint string
	clientID      string
	clientSecret  string

	mu          sync.Mutex
	mgmtToken   string
	mgmtExpires time.Time
	emailCache  map[string]string // sub → email
}

// emailForSub returns the email for the given PingOne sub (the user's id).
func (r *pingoneUserResolver) emailForSub(sub string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

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

	resp, err := pingoneHTTPClient.Do(req)
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
	if result.Email == "" {
		return "", fmt.Errorf("user %q has no email", sub)
	}

	if r.emailCache == nil {
		r.emailCache = make(map[string]string)
	}
	r.emailCache[sub] = result.Email
	return result.Email, nil
}

// refreshMgmtToken returns a valid management token, fetching a new one when expired.
// Must be called with r.mu held.
func (r *pingoneUserResolver) refreshMgmtToken() (string, error) {
	if r.mgmtToken != "" && time.Now().Before(r.mgmtExpires) {
		return r.mgmtToken, nil
	}
	tok, expiresIn, err := fetchOAuthToken(r.tokenEndpoint, r.clientID, r.clientSecret,
		url.Values{
			"grant_type": {"client_credentials"},
			"scope":      {pingOneMgmtScope},
		})
	if err != nil {
		return "", err
	}
	r.mgmtToken = tok
	r.mgmtExpires = time.Now().Add(getTokenCacheLifetime(expiresIn))
	return tok, nil
}

// parsePingOneCoords derives the PingOne env ID and management API base URL
// from the issuer, which encodes both:
//
//	https://auth.pingone.<region>/<env-id>/as → https://api.pingone.<region>/v1
func parsePingOneCoords(issuer string) (envID, apiBase string, err error) {
	withoutScheme := strings.TrimPrefix(issuer, "https://")
	parts := strings.SplitN(withoutScheme, "/", 3)
	if len(parts) < 2 || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse env ID from %q", issuer)
	}
	envID = parts[1]
	apiBase = "https://" + strings.Replace(parts[0], "auth.", "api.", 1) + "/v1"
	return envID, apiBase, nil
}
