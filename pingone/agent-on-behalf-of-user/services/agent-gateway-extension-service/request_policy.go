// The per-request policy: what this service does on each phase of a request.
// The gateway's authz policy routes every egress request whose path starts
// with /mcp here: the governed tool host is served, any other host is denied.
package main

import (
	"context"
	"encoding/json"
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
	userResolver           *pingoneUserResolver
	toolURL                string
}

// requestState hands what the body phase needs from the header phase.
type requestState struct {
	userSub       string // Sent to PingOne Authorize
	agentClientID string // Sent to PingOne Authorize
	// governed is true when the header phase validated the bearer and swapped
	// in the tool token; the body phase runs the Authorize check only then.
	governed bool
}

// handleRequestHeadersPhase runs for the header phase of every request:
// validate the delegated bearer, exchange it for a tool-scoped token, resolve
// the user's email, and ask for the body. MCP-shaped traffic aimed anywhere
// but the governed tool host is denied.
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

	state.userSub = readJWTClaim(inboundBearerToken, "sub")
	// The agent's identity rides in act.sub on delegated tokens; tool-discovery
	// client_credentials tokens carry it top-level in client_id.
	state.agentClientID = readJWTActClaim(inboundBearerToken)
	if state.agentClientID == "" {
		state.agentClientID = readJWTClaim(inboundBearerToken, "client_id")
	}

	toolToken, err := s.pingOneClient.exchangeAgentTokenForResourceToken(inboundBearerToken)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return replyForbidden("token exchange failed"), requestState{}
	}

	// This journey adds user-email resolution: the MCP server identifies the
	// caller from X-User-Email without needing PingOne access. Best-effort by
	// design — a failed lookup proceeds without the header.
	userEmail := ""
	if s.userResolver != nil && state.userSub != "" {
		if email, err := s.userResolver.emailForSub(state.userSub); err != nil {
			log.Printf("[ExtSvc] WARNING: email lookup failed for sub=%s: %v — proceeding without email", state.userSub, err)
		} else {
			userEmail = email
		}
	}

	log.Printf("[ExtSvc] injecting tool token for %s (user=%s agent=%s email=%s)", authority, state.userSub, state.agentClientID, userEmail)
	state.governed = true
	headers := []*corev3.HeaderValueOption{bearerAuthHeader(toolToken)}
	if userEmail != "" {
		headers = append(headers, plainHeader("X-User-Email", userEmail))
	}
	return setHeadersAndRequestBody(headers), state
}

// handleRequestBodyPhase runs for the body phase: tools/call requests get a
// PingOne Authorize check; every other body (initialize, tools/list) passes
// through unchanged.
func (s *processor) handleRequestBodyPhase(b *extprocv3.ProcessingRequest_RequestBody, state requestState) *extprocv3.ProcessingResponse {
	if state.governed && readMCPMethod(b.RequestBody.Body) == "tools/call" {
		return s.askPingOneAuthorizeToPermitToolsCall(b, state.userSub, state.agentClientID)
	}
	return passRequestBodyThrough(b.RequestBody)
}

// askPingOneAuthorizeToPermitToolsCall asks PingOne Authorize whether this
// tools/call may proceed. Any error or non-PERMIT fails closed with 403 — the
// request never reaches the tool. This journey's decision is compound: user +
// agent + tool + amount.
func (s *processor) askPingOneAuthorizeToPermitToolsCall(b *extprocv3.ProcessingRequest_RequestBody, userSub, agentClientID string) *extprocv3.ProcessingResponse {
	amountCents := readStripeTotalPriceCents(b.RequestBody.Body)
	toolName := readMCPToolName(b.RequestBody.Body)
	requestHour := currentRequestHourPacific()
	log.Printf("[ExtSvc] authorize user=%s agent=%s tool=%s amount_cents=%d hour=%d",
		userSub, agentClientID, toolName, amountCents, requestHour)
	// The decision-request parameters: keys must byte-match the Trust
	// Framework attribute Names the policies consume (see README). Change
	// this map to change what the policy sees.
	decisionRequestParams := map[string]any{
		"User Sub":        userSub,
		"Agent Client ID": agentClientID,
		"Tool Name":       toolName,
		"Amount Cents":    amountCents,
		"Request Hour":    requestHour,
	}
	permitted, err := s.pingOneAuthorizeClient.getPingOneAuthorizeDecision(decisionRequestParams)
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return replyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY user=%s agent=%s", userSub, agentClientID)
		return replyForbidden("request denied by policy")
	default:
		log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s agent=%s", userSub, agentClientID)
		return passRequestBodyThrough(b.RequestBody)
	}
}

func (s *processor) isGovernedToolHost(authority string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(s.toolURL, "https://"), "http://")
	return strings.HasPrefix(authority, host)
}

// readStripeTotalPriceCents extracts the total_price argument (USD) from a
// create_stripe_payment_intent call and converts it to cents for the
// amount-limit policy. Non-parsable or absent → 0 (nothing to compare against
// the limit; the policy simply won't deny on amount).
func readStripeTotalPriceCents(mcpRequestBody []byte) int {
	var jsonrpcRequest struct {
		Params struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(mcpRequestBody, &jsonrpcRequest); err != nil {
		return 0
	}
	switch v := jsonrpcRequest.Params.Arguments["total_price"].(type) {
	case float64:
		return int(v * 100)
	}
	return 0
}

func currentRequestHourPacific() int {
	pacificTimeZone, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		return -1
	}
	return time.Now().In(pacificTimeZone).Hour()
}
