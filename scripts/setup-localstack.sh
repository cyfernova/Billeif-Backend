#!/bin/bash

set -e

echo "Setting up LocalStack..."

docker run -d \
  --name localstack \
  -p 4566:4566 \
  -p 4510-4559:4510-4559 \
  -e SERVICES=s3,dynamodb,ses,sns,sqs,iam,sts \
  -e DEBUG=1 \
  -e AWS_DEFAULT_REGION=us-east-1 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  localstack/localstack:4.0

echo "Waiting for LocalStack to be ready..."
sleep 10

echo "LocalStack is ready!"
