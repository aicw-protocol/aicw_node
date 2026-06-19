# AICW Node Dockerfile
#
# AICW-FORK: Builds the MPC node with Phase A dynamic peer support.

FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download || true

# Copy source code
COPY . .

# Build the node binary
RUN go build -o aicw-node ./cmd/aicw-node

# Runtime image
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates curl

# Copy binary from builder
COPY --from=builder /app/aicw-node /app/aicw-node

# Copy config template
COPY config/config.yaml.template /app/config/config.yaml.template

# Create identity directory
RUN mkdir -p /app/identity

# Expose health check port
EXPOSE 8080

# Health check endpoint
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1

# Run the node
ENTRYPOINT ["/app/aicw-node"]
CMD ["start", "--config", "/app/config/config.yaml"]
