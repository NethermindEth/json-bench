# Multi-stage build for Go runner
FROM golang:1.25.4-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY runner/ ./runner/

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jsonrpc-bench-runner ./runner/main.go

# Final stage - minimal runtime image with k6 (base is alpine)
FROM grafana/k6:1.4.0

# Use root user for dependencies installation
USER root

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl postgresql-client

WORKDIR /app

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Copy binary from builder
COPY --from=builder /app/jsonrpc-bench-runner /app/jsonrpc-bench-runner

# Create necessary directories and set permissions
RUN mkdir -p /app/results /app/historic && \
    chown -R appuser:appgroup /app

# Copy config files
COPY config/ ./config/

# Switch to non-root user
USER appuser

# Default entrypoint
ENTRYPOINT ["/app/jsonrpc-bench-runner"]
