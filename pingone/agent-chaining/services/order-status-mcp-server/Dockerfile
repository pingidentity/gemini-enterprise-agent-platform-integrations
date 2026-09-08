FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /order-status-mcp-server .

FROM alpine:3.22
COPY --from=builder /order-status-mcp-server /order-status-mcp-server
EXPOSE 8080
ENTRYPOINT ["/order-status-mcp-server"]
