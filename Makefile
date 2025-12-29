.PHONY: help localstack-up localstack-down localstack-logs infra-init infra-apply infra-destroy build run test test-integration migrate-up migrate-down migrate-create fmt lint docker-build docker-up docker-down clean deps test-coverage swagger setup-test-user

help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

localstack-up:
	docker-compose up -d localstack postgres redis adminer
	@echo "Waiting for LocalStack to be ready..."
	@timeout 60 bash -c 'until curl -f http://localhost:4566/_localstack/health; do sleep 2; done'
	@echo "LocalStack is ready!"

localstack-down:
	docker-compose down

localstack-logs:
	docker-compose logs -f localstack

infra-init:
	cd infrastructure/terraform && tflocal init

infra-apply:
	cd infrastructure/terraform && tflocal apply -auto-approve

infra-destroy:
	cd infrastructure/terraform && tflocal destroy -auto-approve

build:
	go build -o bin/api ./cmd/api

run:
	go run ./cmd/api

test:
	go test -v -race -cover ./...

test-integration:
	@echo "Starting infrastructure..."
	$(MAKE) localstack-up
	@sleep 10
	@echo "Applying Terraform..."
	$(MAKE) infra-apply
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...
	@echo "Cleaning up..."
	$(MAKE) infra-destroy
	$(MAKE) localstack-down

migrate-up:
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@localhost:5432/invoice_db?sslmode=disable" up

migrate-down:
	migrate -path ./migrations -database "postgres://invoice_user:invoice_pass@localhost:5432/invoice_db?sslmode=disable" down

migrate-create:
	migrate create -ext sql -dir ./migrations -seq $(NAME)

fmt:
	go fmt ./...
	go run golang.org/x/tools/cmd/goimports@latest -w .

lint:
	golangci-lint run --timeout=5m

docker-build:
	docker-compose build

docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down

clean:
	rm -rf bin/
	go clean

deps:
	go mod download
	go mod tidy

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

swagger:
	swag init -g cmd/api/main.go -o docs/

setup-test-user:
	./infrastructure/localstack/create-test-user.sh
