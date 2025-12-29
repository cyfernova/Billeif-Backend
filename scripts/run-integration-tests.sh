#!/bin/bash
set -e

echo "=== Running Integration Tests ==="

cleanup() {
    echo "Cleaning up..."
    make infra-destroy || true
    make localstack-down || true
}

trap cleanup EXIT

echo "Starting infrastructure..."
make localstack-up
sleep 10

echo "Applying Terraform..."
make infra-apply

echo "Running database migrations..."
make migrate-up || true

echo "Initializing AWS resources..."
./scripts/init-aws-resources.sh

echo "Running integration tests..."
go test -v -tags=integration ./tests/integration/...

echo "=== Integration Tests Complete ==="
