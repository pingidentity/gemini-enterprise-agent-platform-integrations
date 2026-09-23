FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /baatt-supply-chain-mcp-tool .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /baatt-supply-chain-mcp-tool /baatt-supply-chain-mcp-tool
EXPOSE 8080
ENTRYPOINT ["/baatt-supply-chain-mcp-tool"]
