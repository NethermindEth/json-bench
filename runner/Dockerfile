# Multi-stage build for Go runner
FROM golang:1.24.1-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY runner/ ./runner/
COPY config/ ./config/

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jsonrpc-bench-runner ./runner/main.go

# Final stage - minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl postgresql-client

WORKDIR /app

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Copy binary from builder
COPY --from=builder /app/jsonrpc-bench-runner /app/jsonrpc-bench-runner
COPY --from=builder /app/config /app/config

# Create necessary directories and set permissions
RUN mkdir -p /app/results /app/historic && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Health check with longer start period and more frequent checks
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=5 \
    CMD curl -f http://localhost:8081/api/health || exit 1

# Default command - start API server
CMD ["/app/jsonrpc-bench-runner", "-api", "-storage-config", "/app/config/storage-docker.yaml"]