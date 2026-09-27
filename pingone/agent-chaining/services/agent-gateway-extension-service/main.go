// Agent Gateway extension service — an Envoy ext_proc gRPC handler.
//
// The Agent Gateway calls this service for every request on the governed path.
// For requests bound to the MCP tool it delegates the incoming Agent Chaining token
// (arriving as the Authorization Bearer, sub=user, act=agent) into a
// tool-audienced token via an RFC 8693 exchange (see idp.go), then injects
// that token as the Authorization header. tools/call requests are evaluated
// against PingOne Authorize before reaching the tool.
// It fails closed: unauthorized requests get an immediate error and never reach
// the tool. Every other request passes through untouched.
//
// Cloud Run terminates TLS, so we serve plain h2c on GRPC_PORT.
package main

import (
	"log"
	"net"
	"os"

	"cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

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

	shim, err := newShim(shimConfig{
		projectID:          projectID,
		region:             os.Getenv("GC_REGION"),
		agentEngineID:      os.Getenv("AGENT_ENGINE_ID"),
		requiredAudience:   os.Getenv("IDP_REQUIRED_AUDIENCE"),
		agentRequiredScope: os.Getenv("IDP_REQUIRED_SCOPE_AGENT"), toolRequiredScope: os.Getenv("IDP_REQUIRED_SCOPE_TOOL"),
		agentAudience: os.Getenv("AGENT_AUDIENCE"), agentScope: os.Getenv("AGENT_SCOPE"),
		toolURL:       os.Getenv("TOOL_URL"), toolAudience: os.Getenv("TOOL_AUDIENCE"), toolScope: os.Getenv("TOOL_SCOPE"),
		idpEndpoint:   os.Getenv("IDP_ISSUER") + "/token", idpClientID: os.Getenv("EXCHANGE_CLIENT_ID"), idpSecret: os.Getenv("EXCHANGE_CLIENT_SECRET"),
		authzEndpoint: os.Getenv("AUTHZ_DECISION_ENDPOINT"), authzClientID: os.Getenv("AUTHZ_CLIENT_ID"), authzClientSecret: os.Getenv("AUTHZ_CLIENT_SECRET"),
	})
	if err != nil {
		log.Fatalf("initialize extension service: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(grpcServer, shim)
	reflection.Register(grpcServer)

	log.Printf("[ExtSvc] ext_proc listening on :%s", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
