.PHONY: help infra-backend-init infra-init infra-apply infra-destroy infra-output build run test test-integration migrate-up migrate-down migrate-create fmt lint docker-build docker-up docker-down clean deps test-coverage swagger

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

infra-apply: ## Apply Terraform configuration
	cd infrastructure/terraform && terraform apply -auto-approve

infra-plan: ## Plan Terraform configuration
	cd infrastructure/terraform && terraform plan

infra-destroy: ## Destroy Terraform infrastructure
	cd infrastructure/terraform && terraform destroy -auto-approve

infra-output: ## Save Terraform output to file
	cd infrastructure/terraform && terraform output -json > terraform_output.json
	cd infrastructure/terraform && terraform output > terraform_output.txt
	@echo "Terraform output saved to infrastructure/terraform/terraform_output.txt"

# Build targets
build: ## Build the application
	go build -o bin/api ./cmd/api

run: ## Run the application
	go run ./cmd/api

# Test targets
test: ## Run unit tests
	go test -v -race -cover ./...

test-integration: ## Run integration tests
	@echo "Starting local infrastructure..."
	$(MAKE) docker-up
	@sleep 10
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...
	@echo "Cleaning up..."
	$(MAKE) docker-down

# Migration targets
migrate-up: ## Run database migrations up
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@localhost:5432/invoice_db?sslmode=disable" up

migrate-down: ## Run database migrations down
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@localhost:5432/invoice_db?sslmode=disable" down

migrate-create: ## Create a new migration (usage: make migrate-create NAME=migration_name)
	migrate create -ext sql -dir ./migrations -seq $(NAME)

# Code quality targets
fmt: ## Format Go code
	go fmt ./...
	go run golang.org/x/tools/cmd/goimports@latest -w .

lint: ## Run linter
	golangci-lint run --timeout=5m

# Docker targets
docker-build: ## Build Docker images
	docker-compose build

docker-up: ## Start Docker containers
	docker-compose up --build -d

docker-down: ## Stop Docker containers
	docker-compose down

# Utility targets
clean: ## Clean build artifacts
	rm -rf bin/
	go clean

deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

test-coverage: ## Generate test coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

swagger: ## Generate Swagger documentation
	swag init -g cmd/api/main.go -o docs/
