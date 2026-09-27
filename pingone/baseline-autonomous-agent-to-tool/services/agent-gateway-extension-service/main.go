// Agent Gateway extension service — the Envoy ext_proc processor that the Agent Gateway calls out to.
//   - The gateway's authz policy routes here every egress request whose path starts with /mcp.
//
// Request flow (traffic aimed at the MCP tool's host):
//
//	 header phase  1. Validate the agent's token: fail → 401
//		             2. Delegation exchange: fail → 403
//		             3. Inject exchanged token as Authorization: Bearer and request the body
//	 body phase    4. if tools/call: ask PingOne Authorize for PERMIT/DENY: deny or error → 403
//		                other methods: (initialize, tools/list) echo through
//
// When everything passes, the request continues to the tool carrying the
// exchanged token, and the tool's reply flows back to the agent.
package main

import (
	"context"
	"fmt"
	"log"
	"net"

	_ "time/tzdata"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

type processorConfig struct {
	// which tool is governed
	toolURL   string
	toolScope string

	// what the inbound token must carry
	idpRequiredAudience string
	idpRequiredScope    string

	// PingOne issuer and this service's exchange app (the RFC 8693 actor)
	idpIssuer   string
	idpClientID string
	idpSecret   string

	// PingOne Authorize worker app + decision endpoint
	authzEndpoint     string
	authzClientID     string
	authzClientSecret string
}

func newProcessor(cfg processorConfig) (*processor, error) {
	validator, err := newTokenValidator(context.Background(),
		cfg.idpIssuer, cfg.idpRequiredAudience, cfg.idpRequiredScope)
	if err != nil {
		return nil, fmt.Errorf("token validator init: %w", err)
	}

	return &processor{
		toolURL: cfg.toolURL,
		pingOneClient: &pingOneClient{
			tokenEndpoint: cfg.idpIssuer + "/token",
			clientID:      cfg.idpClientID,
			clientSecret:  cfg.idpSecret,
			targetScope:   cfg.toolScope,
		},
		pingOneAuthorizeClient: &pingoneAuthorizeClient{
			decisionEndpoint: cfg.authzEndpoint,
			tokenEndpoint:    cfg.idpIssuer + "/token",
			clientID:         cfg.authzClientID,
			clientSecret:     cfg.authzClientSecret,
		},
		tokenValidator: validator,
	}, nil
}

func main() {
	port := "50051"

	processor, err := newProcessor(processorConfig{
		toolURL:             requireEnv("TOOL_URL"),
		idpIssuer:           requireEnv("IDP_ISSUER"),
		idpClientID:         requireEnv("EXCHANGE_CLIENT_ID"),
		idpSecret:           requireEnv("EXCHANGE_CLIENT_SECRET"),
		toolScope:           requireEnv("TOOL_SCOPE"),
		idpRequiredAudience: requireEnv("IDP_REQUIRED_AUDIENCE"),
		idpRequiredScope:    requireEnv("IDP_REQUIRED_SCOPE"),
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
