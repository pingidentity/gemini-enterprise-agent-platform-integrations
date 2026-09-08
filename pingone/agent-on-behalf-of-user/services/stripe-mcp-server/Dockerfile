FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOOS=linux GOARCH=amd64 go build -o stripe-mcp-tool .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/stripe-mcp-tool .
EXPOSE 8080
CMD ["./stripe-mcp-tool"]
