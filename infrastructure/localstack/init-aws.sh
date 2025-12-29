#!/bin/bash
set -e

echo "Waiting for LocalStack to be ready..."
until curl -f http://localhost:4566/_localstack/health > /dev/null 2>&1; do
  echo "LocalStack not ready yet..."
  sleep 2
done

echo "LocalStack is ready!"
