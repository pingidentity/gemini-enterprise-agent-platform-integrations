FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o aobou-stripe-mcp-server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/aobou-stripe-mcp-server .
EXPOSE 8080
CMD ["./aobou-stripe-mcp-server"]
