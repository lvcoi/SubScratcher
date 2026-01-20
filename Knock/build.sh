#!/bin/bash

# Set GOBIN directory
GOBIN="/Users/anthony/go/bin"

# Create GOBIN directory if it doesn't exist
mkdir -p $GOBIN

# Build the application
go build -o $GOBIN/knock ./cmd/main.go

# Make the binary executable
chmod +x $GOBIN/knock

echo "Built and installed knock to $GOBIN/knock"
