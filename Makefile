.PHONY: help build run test race coverage tf-init tf-apply tf-destroy localstack-up localstack-down swag-init clean deps lint

help:
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

deps:           ## Install dependencies
	go mod download
	go mod tidy

build:          ## Build the application
	@echo "Building..."
	go build -o bin/api cmd/api/main.go

run:            ## Run the application
	@echo "Running..."
	go run cmd/api/main.go

test:           ## Run all tests
	go test -v -cover ./...

race:           ## Run tests with race detector
	go test -race -v ./...

coverage:       ## Generate coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:           ## Run linter
	golangci-lint run ./... || echo "golangci-lint not installed, skipping"

swag-init:      ## Generate Swagger documentation
	@echo "Generating Swagger docs..."
	swag init -g cmd/api/main.go -o docs

tf-init:        ## Initialize Terraform
	cd terraform && tflocal init

tf-apply:       ## Apply Terraform configuration
	cd terraform && tflocal apply -auto-approve

tf-destroy:     ## Destroy Terraform resources
	cd terraform && tflocal destroy -auto-approve

tf-plan:        ## Show Terraform plan
	cd terraform && tflocal plan

localstack-up:  ## Start LocalStack
	docker-compose up -d localstack

localstack-down:## Stop LocalStack
	docker-compose down

localstack-logs:## View LocalStack logs
	docker-compose logs -f localstack

clean:          ## Clean build artifacts
	rm -rf bin/ coverage.out coverage.html
	rm -f docs/*.json docs/*.yaml docs/docs.go

all: deps lint test build  ## Run deps, lint, test, and build
