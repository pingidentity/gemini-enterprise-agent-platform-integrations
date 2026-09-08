// Agent Gateway extension service — an Envoy ext_proc gRPC handler.
//
// The Agent Gateway calls this service for every request on the governed path.
// For requests bound to the MCP tool it delegates the agent's own PingOne token
// (arriving as the Authorization Bearer) into a tool-audienced token via an
// RFC 8693 exchange (see idp.go), then injects that token as the Authorization
// header. It fails closed: unauthorized requests get an immediate error and
// never reach the tool. Every other request passes through untouched.
//
// Cloud Run terminates TLS, so we serve plain h2c on GRPC_PORT.
package main

import (
	"log"
	"net"
	"os"
	// Embedded tz database — the distroless runtime ships no OS tzdata, and
	// currentHour() resolves America/Vancouver (business-hours clock).
	_ "time/tzdata"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func main() {
	_ = godotenv.Load() // local dev convenience; absent on Cloud Run

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	shim := newShim(shimConfig{
		toolURL:           os.Getenv("TOOL_URL"),
		idpEndpoint:       os.Getenv("IDP_TOKEN_ENDPOINT"),
		idpClientID:       os.Getenv("IDP_CLIENT_ID"),
		idpSecret:         os.Getenv("IDP_CLIENT_SECRET"),
		idpScope:          os.Getenv("IDP_SCOPE"),
		idpAudience:       os.Getenv("IDP_REQUIRED_AUDIENCE"),
		authzEndpoint:     os.Getenv("AUTHZ_DECISION_ENDPOINT"),
		authzClientID:     os.Getenv("AUTHZ_CLIENT_ID"),
		authzClientSecret: os.Getenv("AUTHZ_CLIENT_SECRET"),
	})

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
