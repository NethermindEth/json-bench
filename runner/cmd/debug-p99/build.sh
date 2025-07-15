#!/bin/bash

# Build the p99 debugging tool
echo "Building p99 debugging tool..."

# Navigate to the runner directory
cd ../../

# Build the tool
go build -o cmd/debug-p99/debug-p99 ./cmd/debug-p99

if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo "Usage: ./debug-p99 -run-id <RUN_ID> [-storage-config <CONFIG_FILE>] [-verbose]"
else
    echo "Build failed!"
    exit 1
fi