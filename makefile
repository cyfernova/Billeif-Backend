BINARY_NAME=invoice-backend
BUILD_DIR=build
DOCKER_IMAGE=invoice-backend

.PHONY: all build run test clean tidy lint docker-build docker-up docker-down help

all: build

build:
	@echo "Building..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/main.go

run: build
	@echo "Running..."
	@./$(BUILD_DIR)/$(BINARY_NAME)

test:
	@echo "Testing..."
	go test -v ./...

clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)

tidy:
	@echo "Tidying modules..."
	go mod tidy

lint:
	@echo "Linting..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Skip."; \
	fi

docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

docker-up:
	@echo "Starting Docker Compose..."
	docker-compose up -d

docker-down:
	@echo "Stopping Docker Compose..."
	docker-compose down

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {split($$1,A,/(^ | )/);print A[1]}' | sort | uniq | grep -v 'Makefile'
