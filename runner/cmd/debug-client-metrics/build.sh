#!/bin/bash

# Build the debug-client-metrics tool

echo "Building debug-client-metrics tool..."
go build -o debug-client-metrics main.go

if [ $? -eq 0 ]; then
    echo "✅ Build successful: ./debug-client-metrics"
    echo ""
    echo "Usage:"
    echo "  ./debug-client-metrics -run-id <run-id>"
    echo "  ./debug-client-metrics -run-id <run-id> -storage-config <path-to-config>"
    echo "  ./debug-client-metrics -run-id <run-id> -verbose"
else
    echo "❌ Build failed"
    exit 1
fi