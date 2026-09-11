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
	tokenValidator *delegatedTokenValidator
}

type shimConfig struct {
	toolURL            string
	idpEndpoint        string
	idpClientID        string
	idpSecret          string
	idpScope           string
	idpAudience        string
	authzEndpoint      string
	authzClientID      string
	authzClientSecret  string
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
//  1. Header phase: validate the agent's bearer token, exchange it for a
//     tool-scoped token, inject it, and request the body (BUFFERED).
//  2. Body phase: parse the MCP method and quantity, call PingOne Authorize,
//     then either echo the body (PERMIT) or return 403 (DENY).
func (s *shim) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
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
			resp, agentClientID, needsAuthz = s.onRequestHeaders(stream.Context(), v.RequestHeaders)

		case *extprocv3.ProcessingRequest_RequestBody:
			if needsAuthz && s.authz != nil && extractMethod(v.RequestBody.Body) == "tools/call" {
				log.Printf("[ExtSvc] authorize agent=%s hour=%d", agentClientID, currentHour())
				permitted, err := s.authz.Decide(agentClientID, currentHour())
				switch {
				case err != nil:
					log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
					resp = denyForbidden("authorization service error")
				case !permitted:
					log.Printf("[ExtSvc] PingOne Authorize DENY agent=%s", agentClientID)
					resp = denyForbidden("request denied by policy")
				default:
					log.Printf("[ExtSvc] PingOne Authorize PERMIT agent=%s", agentClientID)
					resp = echoRequestBody(v.RequestBody)
				}
			} else {
				resp = echoRequestBody(v.RequestBody)
			}

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

// onRequestHeaders validates the agent's bearer token, exchanges it for a
// tool-scoped token, and returns the appropriate ProcessingResponse. It always
// returns a valid response: passthrough for non-tool requests, a deny for auth
// failures, or an inject+body-request for success.
func (s *shim) onRequestHeaders(ctx context.Context, msg *extprocv3.HttpHeaders) (resp *extprocv3.ProcessingResponse, agentClientID string, needsAuthz bool) {
	authority := headerValue(msg.Headers, ":authority")
	path := headerValue(msg.Headers, ":path")
	log.Printf("[ExtSvc] request authority=%q path=%q", authority, path)

	if !s.configured() || !s.isToolRequest(authority) {
		return passthroughHeaders(), "", false
	}

	subject := strings.TrimPrefix(headerValue(msg.Headers, "authorization"), "Bearer ")
	if subject == "" {
		log.Printf("[ExtSvc] missing bearer token — 401")
		return denyUnauthorized("bearer token required"), "", false
	}

	if s.tokenValidator != nil {
		if err := s.tokenValidator.verify(ctx, subject); err != nil {
			log.Printf("[ExtSvc] token validation failed — 401: %v", err)
			return denyUnauthorized("invalid token: " + err.Error()), "", false
		}
	}

	agentClientID = jwtClaim(subject, "client_id")

	tok, err := s.idp.exchangeForTool(subject)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return denyForbidden("token exchange failed"), "", false
	}

	log.Printf("[ExtSvc] injecting delegated token for %s", authority)
	return injectAuthAndRequestBody(tok), agentClientID, s.authz != nil
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

// jwtClaim extracts a single string claim from a JWT payload without signature
// validation. Used only to read the agent's client_id for the Authorize call.
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

// extractMethod returns the JSON-RPC method field from an MCP request body.
func extractMethod(body []byte) string {
	var rpc struct {
		Method string `json:"method"`
	}
	json.Unmarshal(body, &rpc) //nolint:errcheck — returns "" on failure, which is safe
	return rpc.Method
}

// extractQuantity returns the quantity argument from an MCP tools/call body, or 0.
// Retained for reference; the deployed policies no longer consume quantity.
// func extractQuantity(body []byte) int {
// 	var rpc struct {
// 		Params struct {
// 			Arguments map[string]any `json:"arguments"`
// 		} `json:"params"`
// 	}
// 	if err := json.Unmarshal(body, &rpc); err != nil {
// 		return 0
// 	}
// 	switch q := rpc.Params.Arguments["quantity"].(type) {
// 	case float64:
// 		return int(q)
// 	case int:
// 		return q
// 	}
// 	return 0
// }

func currentHour() int {
	// Business hours are Pacific-local. The distroless runtime ships no OS
	// tzdata, so embed the tz database via _ "time/tzdata" in main.go; on
	// zone-load failure return -1 so an unresolvable clock can never be read
	// as in-hours (fails closed in the business-hours rule).
	van, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		return -1
	}
	return time.Now().In(van).Hour()
}
