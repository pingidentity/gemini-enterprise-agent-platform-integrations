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

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func main() {
	_ = godotenv.Load()

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	shim, err := newShim(shimConfig{
		agentGatewayAudience: os.Getenv("AGENT_GATEWAY_AUDIENCE"),
		a2aURL:               os.Getenv("A2A_TARGET_URL"), a2aAudience: os.Getenv("A2A_REQUIRED_AUDIENCE"), a2aScope: os.Getenv("A2A_REQUIRED_SCOPE"),
		mcpURL: os.Getenv("MCP_TARGET_URL"), mcpAudience: os.Getenv("MCP_REQUIRED_AUDIENCE"), mcpScope: os.Getenv("MCP_REQUIRED_SCOPE"),
		idpEndpoint: os.Getenv("IDP_TOKEN_ENDPOINT"), idpClientID: os.Getenv("IDP_CLIENT_ID"), idpSecret: os.Getenv("IDP_CLIENT_SECRET"),
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
