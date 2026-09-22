FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /aobou-agent-gateway-extension-service .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /aobou-agent-gateway-extension-service /aobou-agent-gateway-extension-service
EXPOSE 50051
ENTRYPOINT ["/aobou-agent-gateway-extension-service"]
