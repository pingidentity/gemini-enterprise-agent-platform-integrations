FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /supply-chain-mcp-tool .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /supply-chain-mcp-tool /supply-chain-mcp-tool
EXPOSE 8080
ENTRYPOINT ["/supply-chain-mcp-tool"]
