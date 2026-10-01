// The per-request policy: what this service does on each phase of a request.
// The gateway's authz policy routes every egress request matching an A2A or
// MCP path here: the configured targets are served, any other MCP host is
// denied.
package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	// The runtime image is distroless (no OS tzdata) and the business-hours
	// policy must be evaluated against business-local hours, not UTC — so the
	// zone database ships inside the binary.
	_ "time/tzdata"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

type processor struct {
	extprocv3.UnimplementedExternalProcessorServer

	pingOneClient          *pingOneClient
	pingOneAuthorizeClient *pingoneAuthorizeClient
	targets                []targetConfig
	googleAuth             *googleTokenSource
}

// targetConfig describes one governed hop: host + path matching, the audiences
// and scope for validation vs exchange, the protocol's body shape, and the
// per-hop JWKS validator.
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
	validator *tokenValidator
}

// requestState hands what the body phase needs from the header phase.
type requestState struct {
	target  *targetConfig // Sent to PingOne Authorize (protocol + hop identity)
	userSub string        // Sent to PingOne Authorize
	// bodyToken is set only for dualAuth targets, where the reminted token
	// rides in the request body instead of a header (a custom header added in
	// the header phase was observed not to reach the downstream agent).
	bodyToken string
}

// handleRequestHeadersPhase runs for the header phase of every request:
// match the request to a governed target, validate the caller's delegated
// bearer, remint a fresh token for the hop's final audience, and for
// Google-hosted targets also fetch a Google credential for that API's own IAM
// check. MCP-shaped traffic aimed at no configured target is denied.
func (s *processor) handleRequestHeadersPhase(ctx context.Context, msg *extprocv3.HttpHeaders, state requestState) (*extprocv3.ProcessingResponse, requestState) {
	authority, path := readHeader(msg.Headers, ":authority"), readHeader(msg.Headers, ":path")
	state.target = s.findTargetForRequest(authority, path)
	governed := state.target != nil
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

	if err := state.target.validator.verify(ctx, inboundBearerToken); err != nil {
		log.Printf("[ExtSvc] token validation failed — 401: %v", err)
		return replyUnauthorized("invalid token: " + err.Error()), requestState{}
	}

	state.userSub = readJWTClaim(inboundBearerToken, "sub")

	toolToken, err := s.pingOneClient.exchangeAgentTokenForResourceToken(inboundBearerToken, state.target.name, state.target.exchangeAudience, state.target.scope)
	if err != nil {
		log.Printf("[ExtSvc] token exchange failed — 403: %v", err)
		return replyForbidden("token exchange failed"), requestState{}
	}

	log.Printf("[ExtSvc] injecting %s token for %s", state.target.protocol, state.target.name)
	if state.target.dualAuth {
		// This hop's target enforces its own Google IAM check on
		// Authorization, so it carries a Google credential and the reminted
		// token moves to the body (see requestState.bodyToken).
		googleToken, err := s.googleAuth.Token()
		if err != nil {
			log.Printf("[ExtSvc] google credential error — 403: %v", err)
			return replyForbidden("upstream credential error"), requestState{}
		}
		state.bodyToken = toolToken
		return setHeadersAndRequestBody([]*corev3.HeaderValueOption{
			bearerAuthHeader(googleToken),
		}), state
	}
	return setHeadersAndRequestBody([]*corev3.HeaderValueOption{
		bearerAuthHeader(toolToken),
	}), state
}

// handleRequestBodyPhase runs for the body phase: each target's one known
// action gets a PingOne Authorize check; anything else passes through
// unchanged.
func (s *processor) handleRequestBodyPhase(b *extprocv3.ProcessingRequest_RequestBody, state requestState) *extprocv3.ProcessingResponse {
	if state.target == nil {
		return passRequestBodyThrough(b.RequestBody)
	}
	body := b.RequestBody.Body
	action, orderID, ok := parseRequestActionAndOrderID(state.target.protocol, body)
	if !ok {
		log.Printf("[ExtSvc] target=%s unrecognized body — 403", state.target.name)
		return replyForbidden("unsupported request")
	}
	requestHour := currentRequestHourPacific()
	log.Printf("[ExtSvc] authorize user=%s action=%s order=%s hour=%d", state.userSub, action, orderID, requestHour)
	// The decision-request parameters: keys must byte-match the Trust
	// Framework attribute Names the policies consume (see README). Change
	// this map to change what the policy sees.
	permitted, err := s.pingOneAuthorizeClient.getPingOneAuthorizeDecision(buildDecisionRequestParams(state.userSub, requestHour))
	switch {
	case err != nil:
		log.Printf("[ExtSvc] PingOne Authorize error: %v", err)
		return replyForbidden("authorization service error")
	case !permitted:
		log.Printf("[ExtSvc] PingOne Authorize DENY user=%s", state.userSub)
		return replyForbidden("request denied by policy")
	}
	log.Printf("[ExtSvc] PingOne Authorize PERMIT user=%s", state.userSub)
	if state.bodyToken != "" {
		mutatedBody, err := setDelegatedAuthorizationInBody(body, state.bodyToken)
		if err != nil {
			log.Printf("[ExtSvc] target=%s body mutation error — 403: %v", state.target.name, err)
			return replyForbidden("request body error")
		}
		return replaceRequestBody(mutatedBody, b.RequestBody.EndOfStream)
	}
	return passRequestBodyThrough(b.RequestBody)
}

// findTargetForRequest decides which outbound traffic this service governs: a
// request matches a target when its :authority equals the target's host AND
// its path prefix-matches the target's path (the A2A target's path encodes the
// engine ID; the MCP target's path is /mcp).
func (s *processor) findTargetForRequest(authority, path string) *targetConfig {
	for i := range s.targets {
		if s.targets[i].host == authority && strings.HasPrefix(path, s.targets[i].path) {
			return &s.targets[i]
		}
	}
	return nil
}

// buildDecisionRequestParams assembles the decision-request parameters: keys
// must byte-match the Trust Framework attribute Names the policies consume
// (see README). Change this map to change what the policy sees.
func buildDecisionRequestParams(userSub string, requestHour int) map[string]any {
	return map[string]any{
		"User Sub":     userSub,
		"Request Hour": requestHour,
	}
}

// parseRequestActionAndOrderID reads each hop's one known action and its
// order-ID argument out of the request body. A2A carries it as a message part
// prefixed "get_order_status:"; MCP as a tools/call params.name +
// arguments.order_id. ok=false for anything else — callers treat that as
// fail-closed.
func parseRequestActionAndOrderID(protocol string, body []byte) (string, string, bool) {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return "", "", false
	}
	if protocol == "a2a" {
		message, _ := request["message"].(map[string]any)
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
	if protocol == "mcp" && request["method"] == "tools/call" {
		params, _ := request["params"].(map[string]any)
		if params["name"] == "get_order_status" {
			arguments, _ := params["arguments"].(map[string]any)
			orderID, _ := arguments["order_id"].(string)
			return "get_order_status", orderID, orderID != ""
		}
	}
	return "", "", false
}

// setDelegatedAuthorizationInBody replaces the message's delegatedAuthorization
// metadata with the gateway-reminted token, so the downstream agent validates
// the token the gateway just exchanged rather than the caller's original one.
func setDelegatedAuthorizationInBody(body []byte, token string) ([]byte, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	metadata, _ := request["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["delegatedAuthorization"] = "Bearer " + token
	request["metadata"] = metadata
	return json.Marshal(request)
}

func currentRequestHourPacific() int {
	pacificTimeZone, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		log.Printf("[ExtSvc] timezone load failed: %v — sending hour -1 (policy will deny)", err)
		return -1
	}
	return time.Now().In(pacificTimeZone).Hour()
}
