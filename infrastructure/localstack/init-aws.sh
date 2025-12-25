#!/bin/bash

# LocalStack initialization script for Invoice Backend
# This script waits for LocalStack to be ready and applies Terraform configuration

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Waiting for LocalStack to be ready...${NC}"

# Wait for LocalStack to be ready
MAX_RETRIES=30
RETRY_COUNT=0
until curl -f -s http://localhost:4566/_localstack/health > /dev/null 2>&1; do
  RETRY_COUNT=$((RETRY_COUNT+1))
  if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
    echo -e "${RED}LocalStack failed to start after ${MAX_RETRIES} attempts${NC}"
    exit 1
  fi
  echo -e "${YELLOW}Waiting for LocalStack... (${RETRY_COUNT}/${MAX_RETRIES})${NC}"
  sleep 2
done

echo -e "${GREEN}LocalStack is ready!${NC}"

# Change to terraform directory
cd "$(dirname "$0")/../terraform" || exit 1

echo -e "${GREEN}Initializing Terraform...${NC}"
terraform init

echo -e "${GREEN}Applying Terraform configuration...${NC}"
terraform apply -var-file="terraform.tfvars" -auto-approve

echo -e "${GREEN}Infrastructure setup complete!${NC}"
echo -e "${GREEN}Run 'terraform output' to see the created resources${NC}"
