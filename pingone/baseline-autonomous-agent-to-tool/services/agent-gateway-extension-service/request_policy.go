// The per-request policy: what this service does on each phase of a request.
// The gateway's authz policy routes every egress request whose path starts
// with /mcp here: the governed tool host is served, any other host is denied.
package main

import (
	"context"
	"log"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

type processor struct {
	extprocv3.UnimplementedExternalProcessorServer

	tokenValidator         *tokenValidator
	pingOneClient          *pingOneClient
	pingOneAuthorizeClient *pingoneAuthorizeClient
	toolURL                string
}

// requestState hands what the body phase needs from the header phase.
type requestState struct {
	agentClientID string // Sent to PingOne Authorize
	// governed is true when the header phase validated the bearer and swapped
	// in the tool token; the body phase runs the Authorize check only then.
	governed bool
}

// handleRequestHeadersPhase runs for the header phase of every request:
// validate the agent's bearer, exchange it for a tool-scoped token, and ask
// for the body. MCP-shaped traffic aimed anywhere but the governed tool host
// is denied.
func (s *processor) handleRequestHeadersPhase(ctx context.Context, msg *extprocv3.HttpHeaders, state requestState) (*extprocv3.ProcessingResponse, requestState) {
	authority := readHeader(msg.Headers, ":authority")
	path := readHeader(msg.Headers, ":path")
	governed := s.isGovernedToolHost(authority)
	log.Printf("[ExtSvc] request authority=%q path=%q governed=%v", authority, path, governed)
	if !governed {
		if strings.HasPrefix(path, "/mcp") {
			log.Printf("[ExtSvc] unregistered MCP host — 403")
			return replyForbidden("unregistered MCP server"), requestState{}
		}
		return passthroughHeaders(), requestState{}
	}

	inboundBearerToken := strings.TrimPrefix(readHeader(msg.Headers, "authorization"), "Bearer ")
	if inboundBearerToken == "" {
		log.Printf("[ExtSvc] missing bearer token — 401")
		return replyUnauthorized("bearer token required"), requestState{}
	}

	if err := s.tokenValidator.verify(ctx, inboundBearerToken); err != nil {
		log.Printf("[ExtSvc] token validation failed — 401: %v", err)
		return replyUnauthorized("invalid token: " + err.Error()), requestState{}
	}

	state.agentClientID = readJWTClaim(inboundBearerToken, "client_id")

	toolToken, err := s.pingOneClient.exchangeAgentTokenForResourceToken(inboundBearerToken)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return replyForbidden("token exchange failed"), requestState{}
	}

	log.Printf("[ExtSvc] injecting tool token for %s", authority)
	state.governed = true
	return setHeadersAndRequestBody([]*corev3.HeaderValueOption{
		bearerAuthHeader(toolToken),
	}), state
}

// handleRequestBodyPhase runs for the body phase: tools/call requests get a
// PingOne Authorize check; every other body (initialize, tools/list) passes
// through unchanged.
func (s *processor) handleRequestBodyPhase(b *extprocv3.ProcessingRequest_RequestBody, state requestState) *extprocv3.ProcessingResponse {
	if state.governed && readMCPMethod(b.RequestBody.Body) == "tools/call" {
		return s.askPingOneAuthorizeToPermitToolsCall(b, state.agentClientID)
	}
	return passRequestBodyThrough(b.RequestBody)
}

// askPingOneAuthorizeToPermitToolsCall asks PingOne Authorize whether this
// tools/call may proceed. Any error or non-PERMIT fails closed with 403 — the
// request never reaches the tool.
func (s *processor) askPingOneAuthorizeToPermitToolsCall(b *extprocv3.ProcessingRequest_RequestBody, agentClientID string) *extprocv3.ProcessingResponse {
	requestHour := currentRequestHourPacific()
	log.Printf("[ExtSvc] authorize agent=%s hour=%d", agentClientID, requestHour)
	// The decision-request parameters: keys must byte-match the Trust
	// Framework attribute Names the policies consume (see README). Change
	// this map to change what the policy sees.
	decisionRequestParams := map[string]any{
		"Agent Client ID": agentClientID,
		"Request Hour":    requestHour,
	}
	permitted, err := s.pingOneAuthorizeClient.getPingOneAuthorizeDecision(decisionRequestParams)
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return replyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY agent=%s", agentClientID)
		return replyForbidden("request denied by policy")
	default:
		log.Printf("[ExtSvc] PingOne Authorize PERMIT agent=%s", agentClientID)
		return passRequestBodyThrough(b.RequestBody)
	}
}

func (s *processor) isGovernedToolHost(authority string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(s.toolURL, "https://"), "http://")
	return strings.HasPrefix(authority, host)
}

func currentRequestHourPacific() int {
	pacificTimeZone, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		return -1
	}
	return time.Now().In(pacificTimeZone).Hour()
}
