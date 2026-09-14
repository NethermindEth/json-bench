FROM golang:1.25.14-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY runner/ ./runner/

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jsonrpc-bench-runner ./runner

FROM alpine:3.22

RUN apk --no-cache upgrade && \
    apk --no-cache add ca-certificates tzdata curl postgresql-client

WORKDIR /app

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

COPY --from=builder /app/jsonrpc-bench-runner /app/jsonrpc-bench-runner

RUN mkdir -p /app/results /app/historic && \
    chown -R appuser:appgroup /app

USER appuser

ENTRYPOINT ["/app/jsonrpc-bench-runner"]
