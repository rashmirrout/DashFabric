# Multi-stage build for Weaver Gateway
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o weaver ./cmd/weaver

# Runtime stage
FROM alpine:3.18

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/weaver /app/weaver

# Copy config templates
COPY deploy/kubernetes/config.yaml /etc/weaver/config.yaml

# Create non-root user
RUN addgroup -g 1000 weaver && \
    adduser -D -u 1000 -G weaver weaver

USER weaver

EXPOSE 5000 9090

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/app/weaver", "health"]

ENTRYPOINT ["/app/weaver"]
