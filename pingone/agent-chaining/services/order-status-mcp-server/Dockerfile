FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /ac-order-status-mcp-server .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /ac-order-status-mcp-server /ac-order-status-mcp-server
EXPOSE 8080
ENTRYPOINT ["/ac-order-status-mcp-server"]
