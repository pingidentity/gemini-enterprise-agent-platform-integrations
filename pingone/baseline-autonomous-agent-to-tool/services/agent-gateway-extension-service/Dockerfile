FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /agent-gateway-extension-service .

FROM gcr.io/distroless/static-debian12
COPY --from=builder /agent-gateway-extension-service /agent-gateway-extension-service
EXPOSE 50051
ENTRYPOINT ["/agent-gateway-extension-service"]
