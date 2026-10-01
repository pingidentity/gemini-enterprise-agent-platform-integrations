// Agent Gateway extension service — the Envoy ext_proc processor that the Agent Gateway calls out to.
//   - The gateway's authz policy routes here every egress request whose path starts with /mcp
//     or an A2A reasoningEngines path — two governed hops, matched by host+path:
//     Support Agent → native A2A → Order Status Agent (dual-auth), and
//     Order Status Agent → MCP → Order Status MCP Server.
//
// Request flow (per matched hop):
//
//	 header phase  1. Validate the caller's delegated token against the hop's scope: fail → 401
//		             2. Delegation exchange, reminted for the hop's real final audience: fail → 403
//		             3. Inject the reminted token as Authorization: Bearer and request the body;
//		                on the A2A hop, Authorization instead carries a Google credential (the
//		                aiplatform API's own IAM check) and the reminted token rides in the
//		                body's metadata
//	 body phase    4. if the hop's one known action: ask PingOne Authorize for PERMIT/DENY:
//		                deny, error, or unknown body → 403; on PERMIT the A2A body gets the
//		                reminted token in metadata.delegatedAuthorization
//
// When everything passes, the request continues to the hop's target carrying
// the reminted token (in the header, or in the body on the dual-auth hop), and
// the target's reply flows back to the caller.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

type processorConfig struct {
	// project/region/engine of the A2A target — the mTLS URL is derived, not
	// configured (see newProcessor for why)
	projectID, region, agentEngineID string

	// what every inbound token must carry
	idpRequiredAudience string
	agentRequiredScope  string
	toolRequiredScope   string

	// A2A hop's exchange target
	agentAudience, agentScope string

	// MCP hop's URL and exchange target
	toolURL, toolAudience, toolScope string

	// PingOne issuer and this service's exchange app (the RFC 8693 actor)
	idpIssuer, idpClientID, idpSecret string

	// PingOne Authorize worker app + decision endpoint
	authzEndpoint, authzClientID, authzClientSecret string
}

func newProcessor(cfg processorConfig) (*processor, error) {
	// requiredScope is what the inbound bearer must carry; scope is what the
	// outbound exchange mints. Equal by design today, but they're separate
	// roles — the required scope protects this hop, the exchange scope defines
	// the downstream token.
	parseTarget := func(raw, name, protocol, requiredScope, exchangeAudience, scope string) (targetConfig, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return targetConfig{}, fmt.Errorf("invalid %s target URL", name)
		}
		// The validator checks the caller's bearer against the shared
		// intermediate audience, not the final one — see incomingAudience.
		validator, err := newTokenValidator(context.Background(), cfg.idpIssuer, cfg.idpRequiredAudience, requiredScope)
		if err != nil {
			return targetConfig{}, err
		}
		return targetConfig{
			name: name, host: u.Host, path: u.Path,
			incomingAudience: cfg.idpRequiredAudience, exchangeAudience: exchangeAudience,
			scope: scope, protocol: protocol, validator: validator,
		}, nil
	}
	// The A2A target is derived, not configured: the Agent-to-Anywhere mesh is
	// TLS-inspection-based and only intercepts the mTLS host, and the authz
	// policy only matches project-ID-string reasoningEngine paths — deriving
	// the URL here enforces both by construction.
	agentURL := fmt.Sprintf(
		"https://%s-aiplatform.mtls.googleapis.com/v1beta1/projects/%s/locations/%s/reasoningEngines/%s/a2a",
		cfg.region, cfg.projectID, cfg.region, cfg.agentEngineID,
	)
	a2a, err := parseTarget(agentURL, "A2A", "a2a", cfg.agentRequiredScope, cfg.agentAudience, cfg.agentScope)
	if err != nil {
		return nil, err
	}
	a2a.dualAuth = true
	mcp, err := parseTarget(cfg.toolURL, "MCP", "mcp", cfg.toolRequiredScope, cfg.toolAudience, cfg.toolScope)
	if err != nil {
		return nil, err
	}
	googleAuth, err := newGoogleTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("google credentials: %w", err)
	}
	return &processor{
		targets: []targetConfig{a2a, mcp},
		pingOneClient: &pingOneClient{
			tokenEndpoint: cfg.idpIssuer + "/token",
			clientID:      cfg.idpClientID,
			clientSecret:  cfg.idpSecret,
		},
		pingOneAuthorizeClient: &pingoneAuthorizeClient{
			clientID:         cfg.authzClientID,
			clientSecret:     cfg.authzClientSecret,
			tokenEndpoint:    cfg.idpIssuer + "/token",
			decisionEndpoint: cfg.authzEndpoint,
		},
		googleAuth: googleAuth,
	}, nil
}

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	// Project ID comes from the Cloud Run metadata server when not set — the
	// service always runs inside its own project, so the derived value is the
	// canonical project-ID-string form by construction. The env var remains as
	// an override for local debugging (no metadata server off Cloud Run).
	projectID := os.Getenv("GC_PROJECT_ID")
	if projectID == "" {
		p, err := metadata.ProjectID()
		if err != nil {
			log.Fatalf("resolve project id: %v (set GC_PROJECT_ID to run locally)", err)
		}
		projectID = p
	}

	processor, err := newProcessor(processorConfig{
		projectID:           projectID,
		region:              requireEnv("GC_REGION"),
		agentEngineID:       requireEnv("AGENT_ENGINE_ID"),
		idpRequiredAudience: requireEnv("IDP_REQUIRED_AUDIENCE"),
		agentRequiredScope:  requireEnv("IDP_REQUIRED_SCOPE_AGENT"),
		toolRequiredScope:   requireEnv("IDP_REQUIRED_SCOPE_TOOL"),
		agentAudience:       requireEnv("AGENT_AUDIENCE"),
		agentScope:          requireEnv("AGENT_SCOPE"),
		toolURL:             requireEnv("TOOL_URL"),
		toolAudience:        requireEnv("TOOL_AUDIENCE"),
		toolScope:           requireEnv("TOOL_SCOPE"),
		idpIssuer:           requireEnv("IDP_ISSUER"),
		idpClientID:         requireEnv("EXCHANGE_CLIENT_ID"),
		idpSecret:           requireEnv("EXCHANGE_CLIENT_SECRET"),
		authzEndpoint:       requireEnv("AUTHZ_DECISION_ENDPOINT"),
		authzClientID:       requireEnv("AUTHZ_CLIENT_ID"),
		authzClientSecret:   requireEnv("AUTHZ_CLIENT_SECRET"),
	})
	if err != nil {
		log.Fatalf("initialize extension service: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(grpcServer, processor)
	reflection.Register(grpcServer)

	log.Printf("[ExtSvc] ext_proc listening on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
