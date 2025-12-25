BINARY_NAME=invoice-backend
BUILD_DIR=build
DOCKER_IMAGE=invoice-backend
GO_FILES=$(shell find . -name '*.go' -type f 2>/dev/null | head -1)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: all setup clean tidy lint build run dev test test-coverage \
        infra-up infra-down infra-init infra-plan infra-apply infra-destroy \
        db-up db-down db-migrate db-migrate-create db-reset \
        docker-build docker-up docker-down docker-logs docker-ps \
        swag-init help

all: lint test build

## Setup and Dependencies
setup:
	@echo "Installing tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest

## Linting and Quality
lint:
	@echo "Running linters..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run --timeout 5m ./...; \
	else \
		echo "golangci-lint not installed. Run 'make setup'"; \
	fi

## Building
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/api

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux ./cmd/api

## Running
run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BUILD_DIR)/$(BINARY_NAME)

dev:
	@echo "Running in development mode..."
	@go run ./cmd/api/main.go

## Testing
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

test-coverage: test
	@echo "Coverage report:"
	@go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | grep total

## Infrastructure (Terraform)
infra-up:
	@echo "Starting LocalStack..."
	@docker-compose up -d localstack

infra-down:
	@echo "Stopping LocalStack..."
	@docker-compose stop localstack

infra-init:
	@echo "Initializing Terraform..."
	@cd infrastructure/terraform && terraform init

infra-plan:
	@echo "Planning Terraform..."
	@cd infrastructure/terraform && terraform plan

infra-apply:
	@echo "Applying Terraform..."
	@cd infrastructure/terraform && terraform apply -auto-approve

infra-refresh:
	@echo "Refreshing Terraform state..."
	@cd infrastructure/terraform && terraform refresh

infra-destroy:
	@echo "Destroying Terraform resources..."
	@cd infrastructure/terraform && terraform destroy -auto-approve

## Database
db-up:
	@echo "Starting database..."
	@docker-compose up -d postgres adminer

db-down:
	@echo "Stopping database..."
	@docker-compose stop postgres adminer

db-migrate:
	@echo "Running database migrations..."
	@if command -v migrate > /dev/null; then \
		migrate -path migrations -database "postgresql://invoice_user:invoice_password@localhost:5432/invoice_db?sslmode=disable" up; \
	else \
		echo "migrate tool not installed. Run 'make setup'"; \
	fi

db-migrate-create:
	@echo "Creating migration: $(name)"
	@migrate create -ext sql -dir migrations $(name)

db-reset:
	@echo "Resetting database..."
	@docker-compose exec postgres psql -U invoice_user -d invoice_db -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
	@make db-migrate

## Docker
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):latest .

docker-up:
	@echo "Starting all services..."
	@docker-compose up -d

docker-down:
	@echo "Stopping all services..."
	@docker-compose down

docker-logs:
	@docker-compose logs -f api

docker-ps:
	@docker-compose ps

## Swagger
swag-init:
	@echo "Generating Swagger documentation..."
	@if command -v swag > /dev/null; then \
		swag init -g cmd/api/main.go -o api/http; \
	else \
		echo "swag not installed. Run 'make setup'"; \
	fi

## Utilities
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@go clean

tidy:
	@echo "Tidying modules..."
	@go mod tidy
	@go mod verify

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
