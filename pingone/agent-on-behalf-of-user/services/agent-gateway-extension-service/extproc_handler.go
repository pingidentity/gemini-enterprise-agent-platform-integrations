package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer

	toolURL        string
	idp            *idpClient
	authz          *pingoneAuthorizeClient
	userResolver   *pingoneUserResolver
	tokenValidator *delegatedTokenValidator
}

type shimConfig struct {
	toolURL           string
	idpEndpoint       string
	idpClientID       string
	idpSecret         string
	idpScope          string
	idpAudience       string
	authzEndpoint     string
	authzClientID     string
	authzClientSecret string
}

func newShim(cfg shimConfig) *shim {
	s := &shim{
		toolURL: cfg.toolURL,
		idp: &idpClient{
			endpoint:     cfg.idpEndpoint,
			clientID:     cfg.idpClientID,
			clientSecret: cfg.idpSecret,
			scope:        cfg.idpScope,
		},
	}

	if cfg.idpAudience != "" {
		ctx := context.Background()
		if v, err := newDelegatedTokenValidator(ctx, cfg.idpEndpoint, cfg.idpAudience, cfg.idpScope); err != nil {
			log.Printf("[ExtSvc] WARNING: token validator init failed: %v — inbound token validation disabled", err)
		} else {
			s.tokenValidator = v
		}
	} else {
		log.Println("[ExtSvc] WARNING: IDP_REQUIRED_AUDIENCE not set — inbound token validation disabled")
	}

	// Derive PingOne env ID and API base from the token endpoint.
	// IDP_TOKEN_ENDPOINT form: https://auth.pingone.<region>/<env-id>/as/token
	envID, apiBase, err := parsePingOneCoords(cfg.idpEndpoint)
	if err != nil {
		log.Printf("[ExtSvc] WARNING: cannot derive PingOne coords from %q: %v — user email injection disabled", cfg.idpEndpoint, err)
	}
	if envID != "" && apiBase != "" {
		s.userResolver = &pingoneUserResolver{
			envID:        envID,
			apiBase:      apiBase,
			tokenEndpoint: cfg.idpEndpoint,
			clientID:     cfg.authzClientID,
			clientSecret: cfg.authzClientSecret,
		}
		log.Printf("[ExtSvc] user email resolver enabled (envID=%s)", envID)
	}

	if cfg.authzEndpoint != "" {
		s.authz = &pingoneAuthorizeClient{
			decisionEndpoint: cfg.authzEndpoint,
			tokenEndpoint:    cfg.idpEndpoint,
			clientID:         cfg.authzClientID,
			clientSecret:     cfg.authzClientSecret,
		}
		log.Printf("[ExtSvc] PingOne Authorize enabled: %s", cfg.authzEndpoint)
	} else {
		log.Println("[ExtSvc] WARNING: AUTHZ_DECISION_ENDPOINT not set — skipping PingOne Authorize check")
	}
	if !s.configured() {
		log.Println("[ExtSvc] WARNING: TOOL_URL / IDP_TOKEN_ENDPOINT / IDP_CLIENT_ID / IDP_CLIENT_SECRET incomplete — tool requests will be denied")
	}
	return s
}

func (s *shim) configured() bool {
	return s.toolURL != "" && s.idp.endpoint != "" && s.idp.clientID != "" && s.idp.clientSecret != ""
}

// Process is the ext_proc stream handler.
//
// Per-request flow (two phases):
//  1. Header phase: validate the delegated bearer token (must carry sub + act
//     claims), exchange it for a tool-scoped delegated token, inject it, and
//     request the body (BUFFERED) for the Authorize check.
//  2. Body phase: extract the MCP method. On tools/call: call PingOne Authorize
//     with compound attributes (user sub + agent client_id + tool + amount).
func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var userSub string
	var agentClientID string
	var needsAuthz bool

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv: %v", err)
		}

		var resp *extprocv3.ProcessingResponse
		switch v := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp, userSub, agentClientID, needsAuthz = s.onRequestHeaders(stream.Context(), v.RequestHeaders)

		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onRequestBody(v, userSub, agentClientID, needsAuthz)

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp = ackResponseHeaders()
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp = echoResponseBody(v.ResponseBody)
		default:
			resp = &extprocv3.ProcessingResponse{}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// onRequestHeaders validates the incoming bearer token, exchanges it for a
// tool-scoped token, resolves the user's email, and requests the body for
// the downstream Authorize check.
func (s *shim) onRequestHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (resp *extprocv3.ProcessingResponse, userSub, agentClientID string, needsAuthz bool) {
	authority := headerValue(msg.Headers, ":authority")
	path := headerValue(msg.Headers, ":path")

	if !s.configured() || !s.isToolRequest(authority) {
		return passthroughHeaders(), "", "", false
	}

	log.Printf("[ExtSvc] request authority=%q path=%q", authority, path)

	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		log.Printf("[ExtSvc] missing bearer token — 401")
		return denyUnauthorized("bearer token required"), "", "", false
	}

	if s.tokenValidator != nil {
		if err := s.tokenValidator.verify(ctx, bearer); err != nil {
			log.Printf("[ExtSvc] token validation failed — 401: %v", err)
			return denyUnauthorized("invalid token: " + err.Error()), "", "", false
		}
	}

	userSub = jwtClaim(bearer, "sub")
	agentClientID = jwtActClaim(bearer)
	if agentClientID == "" {
		agentClientID = jwtClaim(bearer, "client_id")
	}

	tok, err := s.idp.exchangeForTool(bearer)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return denyForbidden("token exchange failed"), "", "", false
	}

	// Resolve the user's email from their sub and inject it as X-User-Email so
	// the MCP server can identify the caller without needing PingOne access.
	userEmail := ""
	if s.userResolver != nil && userSub != "" {
		if email, err := s.userResolver.emailForSub(userSub); err != nil {
			log.Printf("[ExtSvc] WARNING: email lookup failed for sub=%s: %v — proceeding without email", userSub, err)
		} else {
			userEmail = email
		}
	}

	serviceName := strings.SplitN(authority, ".", 2)[0]
	log.Printf("[ExtSvc] %s %s — user=%s agent=%s email=%s", serviceName, path, userSub, agentClientID, userEmail)
	log.Printf("[ExtSvc] injecting tool token for %s", authority)
	return injectAuthAndEmailAndRequestBody(tok, userEmail), userSub, agentClientID, s.authz != nil
}

// onRequestBody handles the body phase. For tools/call it calls PingOne
// Authorize with compound attributes (user sub + agent client_id + tool + amount).
func (s *shim) onRequestBody(b *extprocv3.ProcessingRequest_RequestBody, userSub, agentClientID string, needsAuthz bool) *extprocv3.ProcessingResponse {
	body := b.RequestBody.Body
	if !needsAuthz || extractMethod(body) != "tools/call" {
		return echoRequestBody(b.RequestBody)
	}

	amountCents := extractTotalPriceCents(body)
	toolName := extractToolName(body)
	log.Printf("[ExtSvc] authorize user=%s agent=%s tool=%s amount_cents=%d hour=%d",
		userSub, agentClientID, toolName, amountCents, currentHour())

	permitted, err := s.authz.Decide(userSub, agentClientID, toolName, amountCents, currentHour())
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return denyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY user=%s agent=%s", userSub, agentClientID)
		return denyForbidden("request denied by policy")
	default:
		log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s agent=%s", userSub, agentClientID)
		return echoRequestBody(b.RequestBody)
	}
}

func (s *shim) isToolRequest(authority string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(s.toolURL, "https://"), "http://")
	return strings.HasPrefix(authority, host)
}

func headerValue(headers *corev3.HeaderMap, key string) string {
	if headers == nil {
		return ""
	}
	for _, h := range headers.Headers {
		if strings.EqualFold(h.Key, key) {
			if h.Value != "" {
				return h.Value
			}
			return string(h.RawValue)
		}
	}
	return ""
}

// jwtClaim extracts a top-level string claim from a JWT without signature
// validation. Used only for logging and Authorize attributes.
func jwtClaim(token, claim string) string {
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

// jwtActClaim extracts the actor identity from the JWT's act object.
// PingOne RFC 8693 tokens use act.sub for the actor; falls back to act.client_id.
func jwtActClaim(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Act struct {
			Sub      string `json:"sub"`
			ClientID string `json:"client_id"`
		} `json:"act"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	if claims.Act.Sub != "" {
		return claims.Act.Sub
	}
	return claims.Act.ClientID
}

func extractMethod(body []byte) string {
	var rpc struct {
		Method string `json:"method"`
	}
	json.Unmarshal(body, &rpc) //nolint:errcheck
	return rpc.Method
}

func extractToolName(body []byte) string {
	var rpc struct {
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	json.Unmarshal(body, &rpc) //nolint:errcheck
	return rpc.Params.Name
}

// extractTotalPriceCents extracts the total_price argument (USD) from a
// create_stripe_payment_intent call and converts to cents for comparison.
func extractTotalPriceCents(body []byte) int {
	var rpc struct {
		Params struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return 0
	}
	switch v := rpc.Params.Arguments["total_price"].(type) {
	case float64:
		return int(v * 100)
	}
	return 0
}

func currentHour() int {
	return time.Now().UTC().Hour()
}
