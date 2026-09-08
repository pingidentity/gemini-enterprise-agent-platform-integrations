package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	// The runtime image is distroless (no OS tzdata) and the business-hours
	// policy must be evaluated against business-local hours, not UTC — so the
	// zone database ships inside the binary.
	_ "time/tzdata"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type targetConfig struct {
	name, host, path string
	// incomingAudience is the shared "agent-gateway" placeholder audience the
	// calling agent's own RFC 8693 exchange targets. It's never the resource
	// the downstream agent/tool actually validates — it exists so PingOne
	// resource attribute mappings only ever see one exchange touch each real
	// resource (this gateway's), matching the baatt/aobou pattern and keeping
	// each final resource's `may_act`/`act` mapping simple and terminal.
	incomingAudience string
	// exchangeAudience is the real, final audience requested in the gateway's
	// own RFC 8693 exchange — what the downstream agent or MCP server expects.
	exchangeAudience string
	scope            string
	protocol         string
	// dualAuth marks targets that are themselves Google-hosted API surfaces
	// (e.g. aiplatform.googleapis.com reasoning engines). Those enforce their
	// own Google IAM check on Authorization independent of gateway policy, so
	// the outer call needs a Google credential in addition to the PingOne
	// delegated token the downstream agent validates.
	dualAuth  bool
	validator *delegatedTokenValidator
}

type shim struct {
	extprocv3.UnimplementedExternalProcessorServer
	targets    []targetConfig
	idp        *idpClient
	authz      *pingoneAuthorizeClient
	googleAuth *googleTokenSource
}

type shimConfig struct {
	agentGatewayAudience        string
	a2aURL, a2aAudience, a2aScope string
	mcpURL, mcpAudience, mcpScope string
	idpEndpoint, idpClientID, idpSecret string
	authzEndpoint, authzClientID, authzClientSecret string
}

func newShim(cfg shimConfig) (*shim, error) {
	parseTarget := func(raw, name, protocol, exchangeAudience, scope string) (targetConfig, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return targetConfig{}, fmt.Errorf("invalid %s target URL", name)
		}
		// The validator checks the caller's bearer against the shared
		// intermediate audience, not the final one — see incomingAudience.
		validator, err := newDelegatedTokenValidator(context.Background(), cfg.idpEndpoint, cfg.agentGatewayAudience, scope)
		if err != nil {
			return targetConfig{}, err
		}
		return targetConfig{
			name: name, host: u.Host, path: u.Path,
			incomingAudience: cfg.agentGatewayAudience, exchangeAudience: exchangeAudience,
			scope: scope, protocol: protocol, validator: validator,
		}, nil
	}
	if cfg.agentGatewayAudience == "" {
		return nil, fmt.Errorf("AGENT_GATEWAY_AUDIENCE is required")
	}
	a2a, err := parseTarget(cfg.a2aURL, "A2A", "a2a", cfg.a2aAudience, cfg.a2aScope)
	if err != nil {
		return nil, err
	}
	a2a.dualAuth = true
	mcp, err := parseTarget(cfg.mcpURL, "MCP", "mcp", cfg.mcpAudience, cfg.mcpScope)
	if err != nil {
		return nil, err
	}
	// PingOne Authorize is mandatory — there is no bypass. Every tools/call
	// and A2A message:send is evaluated against the decision endpoint, and any
	// configuration gap or decision error fails closed (DENY), never open.
	if cfg.authzEndpoint == "" || cfg.authzClientID == "" || cfg.authzClientSecret == "" {
		return nil, fmt.Errorf("PingOne Authorize configuration is required (AUTHZ_DECISION_ENDPOINT, AUTHZ_CLIENT_ID, AUTHZ_CLIENT_SECRET)")
	}
	googleAuth, err := newGoogleTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("google credentials: %w", err)
	}
	authz := &pingoneAuthorizeClient{
		decisionEndpoint: cfg.authzEndpoint,
		tokenEndpoint:    cfg.idpEndpoint,
		clientID:         cfg.authzClientID,
		clientSecret:     cfg.authzClientSecret,
	}
	return &shim{
		targets:    []targetConfig{a2a, mcp},
		idp:        newIDPClient(cfg.idpEndpoint, cfg.idpClientID, cfg.idpSecret),
		authz:      authz,
		googleAuth: googleAuth,
	}, nil
}

func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var target *targetConfig
	var subject, bodyToken string
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
			resp, target, subject, bodyToken = s.onHeaders(stream.Context(), v.RequestHeaders)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.onBody(v, target, subject, bodyToken)
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

func (s *shim) targetFor(authority, path string) *targetConfig {
	for i := range s.targets {
		if s.targets[i].host == authority && strings.HasPrefix(path, s.targets[i].path) {
			return &s.targets[i]
		}
	}
	return nil
}

// onHeaders validates the caller's delegated bearer, remints a fresh
// PingOne-delegated token audienced to this hop's target (adding the
// gateway's own actor, mirroring the MCP hop's existing exchange), and for
// Google-hosted targets also injects a Google credential to satisfy that
// API's own IAM check. It returns the reminted token separately (bodyToken)
// so onBody can place it in the request body for dualAuth targets, since a
// custom header added here was observed not to reach the downstream agent.
func (s *shim) onHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (*extprocv3.ProcessingResponse, *targetConfig, string, string) {
	authority, path := headerValue(msg.Headers, ":authority"), headerValue(msg.Headers, ":path")
	target := s.targetFor(authority, path)
	log.Printf("[ExtSvc] onHeaders authority=%q path=%q matched=%v", authority, path, target != nil)
	if target == nil {
		return passthroughHeaders(), nil, "", ""
	}
	bearer := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if bearer == "" {
		return denyUnauthorized("bearer token required"), target, "", ""
	}
	if err := target.validator.verify(ctx, bearer); err != nil {
		return denyUnauthorized("invalid delegated token"), target, "", ""
	}
	subject := jwtClaim(bearer, "sub")

	reminted, err := s.idp.exchangeForTarget(bearer, target.name, target.exchangeAudience, target.scope)
	if err != nil {
		log.Printf("[ExtSvc] target=%s token exchange failed: %v", target.name, err)
		return denyForbidden("token exchange failed"), target, "", ""
	}

	if target.dualAuth {
		googleTok, err := s.googleAuth.Token()
		if err != nil {
			log.Printf("[ExtSvc] target=%s google credential error: %v", target.name, err)
			return denyForbidden("upstream credential error"), target, "", ""
		}
		log.Printf("[ExtSvc] validated delegated token — sub=%s aud=%s scope=%q", subject, target.incomingAudience, target.scope)
		log.Printf("[ExtSvc] injecting Google credential + reminted token (aud=%s scope=%s) for %s", target.exchangeAudience, target.scope, target.name)
		return injectGoogleAuth(googleTok), target, subject, reminted
	}
	log.Printf("[ExtSvc] validated delegated token — sub=%s aud=%s scope=%q", subject, target.incomingAudience, target.scope)
	log.Printf("[ExtSvc] injecting reminted token (aud=%s scope=%s) for %s", target.exchangeAudience, target.scope, target.name)
	return injectAuthAndRequestBody(reminted), target, subject, ""
}

func (s *shim) onBody(b *extprocv3.ProcessingRequest_RequestBody, target *targetConfig, subject, bodyToken string) *extprocv3.ProcessingResponse {
	if target == nil {
		return echoRequestBody(b.RequestBody)
	}
	body := b.RequestBody.Body
	action, orderID, ok := parseRequest(target.protocol, body)
	if !ok {
		return denyForbidden("unsupported request")
	}
	hour := currentHour()
	permitted, err := s.authz.Decide(subject, hour)
	if err != nil || !permitted {
		log.Printf("[ExtSvc] DENY target=%s action=%s err=%v permitted=%v", target.name, action, err, permitted)
		return denyForbidden("request denied by policy")
	}
	log.Printf("[ExtSvc] authorize user=%s action=%s hour=%d", subject, action, hour)
	log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s", subject)
	log.Printf("[ExtSvc] PERMIT target=%s action=%s order=%s (authz=pingone-authorize)", target.name, action, orderID)
	if bodyToken != "" {
		mutated, err := setDelegatedAuthorizationInBody(body, bodyToken)
		if err != nil {
			log.Printf("[ExtSvc] target=%s body mutation error: %v", target.name, err)
			return denyForbidden("request body error")
		}
		return replaceRequestBody(mutated, b.RequestBody.EndOfStream)
	}
	return echoRequestBody(b.RequestBody)
}

// setDelegatedAuthorizationInBody replaces the message's delegatedAuthorization
// metadata with the gateway-reminted token, so the downstream agent validates
// the token the gateway just exchanged rather than the caller's original one.
func setDelegatedAuthorizationInBody(body []byte, token string) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	metadata, _ := req["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["delegatedAuthorization"] = "Bearer " + token
	req["metadata"] = metadata
	return json.Marshal(req)
}

func parseRequest(protocol string, body []byte) (string, string, bool) {
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return "", "", false
	}
	if protocol == "a2a" {
		message, _ := req["message"].(map[string]any)
		parts, _ := message["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			text, _ := part["text"].(string)
			if strings.HasPrefix(text, "get_order_status:") {
				orderID := strings.TrimPrefix(text, "get_order_status:")
				if strings.HasPrefix(orderID, "ORD-") && len(orderID) > 4 {
					return "get_order_status", orderID, true
				}
			}
		}
		return "", "", false
	}
	if protocol == "mcp" && req["method"] == "tools/call" {
		params, _ := req["params"].(map[string]any)
		if params["name"] == "get_order_status" {
			arguments, _ := params["arguments"].(map[string]any)
			orderID, _ := arguments["order_id"].(string)
			return "get_order_status", orderID, orderID != ""
		}
	}
	return "", "", false
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
func jwtClaim(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var c map[string]any
	if json.Unmarshal(payload, &c) != nil {
		return ""
	}
	v, _ := c[claim].(string)
	return v
}
// currentHour returns the current hour in the business timezone (America/
// Vancouver here, matching the PingOne Authorize business-hours rule). The
// distroless runtime has no OS tzdata, hence the embedded database above.
func currentHour() int {
	loc, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		// Fail closed: an unresolvable zone is not a reason to send the policy
		// an hour it will read as in-hours. -1 can never fall inside 8..17.
		log.Printf("[ExtSvc] timezone load failed: %v — sending hour -1 (policy will deny)", err)
		return -1
	}
	return time.Now().In(loc).Hour()
}
