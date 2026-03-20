# Load .env file if it exists
-include .env
export

.PHONY: help infra-backend-init infra-init infra-apply infra-plan infra-destroy infra-output build-lambda build-lambda-http build-lambda-a2a-stream build-lambda-sqs-invoice build-lambda-sqs-payment build-lambda-ws package-lambda run-local test test-integration migrate-up migrate-down migrate-create fmt lint clean deps test-coverage swagger

LAMBDA_BUILD_DIR := .build/lambda

help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Infrastructure targets
infra-backend-init: ## Create S3 bucket and DynamoDB table for Terraform backend
	@echo "Creating S3 bucket for Terraform state..."
	aws s3 mb s3://invoice-backend-tfstate-20251229 --region us-east-1 || true
	aws s3api put-bucket-versioning --bucket invoice-backend-tfstate-20251229 --versioning-configuration Status=Enabled
	@echo "Creating DynamoDB table for state locking..."
	aws dynamodb create-table \
		--table-name terraform-state-lock \
		--attribute-definitions AttributeName=LockID,AttributeType=S \
		--key-schema AttributeName=LockID,KeyType=HASH \
		--billing-mode PAY_PER_REQUEST \
		--region us-east-1 || true
	@echo "Backend resources created!"

infra-init: ## Initialize Terraform
	cd infrastructure/terraform && terraform init

infra-apply: package-lambda ## Package Lambda artifacts and apply Terraform configuration
	cd infrastructure/terraform && terraform apply -auto-approve

infra-plan: package-lambda ## Package Lambda artifacts and plan Terraform configuration
	cd infrastructure/terraform && terraform plan

infra-destroy: ## Destroy Terraform infrastructure
	cd infrastructure/terraform && terraform destroy -auto-approve

infra-output: ## Save Terraform output to file
	cd infrastructure/terraform && terraform output -json > terraform_output.json
	cd infrastructure/terraform && terraform output > terraform_output.txt
	@echo "Terraform output saved to infrastructure/terraform/terraform_output.txt"

# Build targets
build-lambda: build-lambda-http build-lambda-a2a-stream build-lambda-sqs-invoice build-lambda-sqs-payment build-lambda-ws ## Build all Lambda binaries

build-lambda-http: ## Build HTTP API Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/http
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(LAMBDA_BUILD_DIR)/http/bootstrap ./cmd/lambda/http

build-lambda-a2a-stream: ## Build A2A stream Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/a2a-stream
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(LAMBDA_BUILD_DIR)/a2a-stream/bootstrap ./cmd/lambda/a2a-stream

build-lambda-sqs-invoice: ## Build invoice SQS Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-invoice
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(LAMBDA_BUILD_DIR)/sqs-invoice/bootstrap ./cmd/lambda/sqs-invoice

build-lambda-sqs-payment: ## Build payment SQS Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/sqs-payment
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(LAMBDA_BUILD_DIR)/sqs-payment/bootstrap ./cmd/lambda/sqs-payment

build-lambda-ws: ## Build WebSocket Lambda bootstrap binary
	mkdir -p $(LAMBDA_BUILD_DIR)/ws
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(LAMBDA_BUILD_DIR)/ws/bootstrap ./cmd/lambda/ws

package-lambda: build-lambda ## Package Lambda artifacts into zip files
	cd $(LAMBDA_BUILD_DIR)/http && zip -q -r ../http.zip bootstrap
	cd $(LAMBDA_BUILD_DIR)/a2a-stream && zip -q -r ../a2a-stream.zip bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-invoice && zip -q -r ../sqs-invoice.zip bootstrap
	cd $(LAMBDA_BUILD_DIR)/sqs-payment && zip -q -r ../sqs-payment.zip bootstrap
	cd $(LAMBDA_BUILD_DIR)/ws && zip -q -r ../ws.zip bootstrap

run-local: ## Run the HTTP server locally
	go run ./cmd/server

# Test targets
test: ## Run unit tests
	go test -v -race -cover ./...

test-integration: ## Run integration tests (requires local dependencies running)
	go test -v -tags=integration ./tests/integration/...

# Migration targets (using golang-migrate CLI)
migrate-up: ## Run database migrations up (using migrate CLI)
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@127.0.0.1:5432/invoice_db?sslmode=disable" up

migrate-down: ## Run database migrations down (using migrate CLI)
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@127.0.0.1:5432/invoice_db?sslmode=disable" down

migrate-create: ## Create a new migration (usage: make migrate-create NAME=migration_name)
	migrate create -ext sql -dir ./migrations -seq $(NAME)

# Code quality targets
fmt: ## Format Go code
	go fmt ./...
	go run golang.org/x/tools/cmd/goimports@latest -w .

lint: ## Run linter
	golangci-lint run --timeout=5m

# Utility targets
clean: ## Clean build artifacts
	rm -rf .build/
	go clean

deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

test-coverage: ## Generate test coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

swagger: ## Generate Swagger documentation
	swag init -g internal/app/runtime.go -o docs/
