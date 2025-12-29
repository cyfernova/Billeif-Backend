# Invoice Backend

Production-grade invoice and billing management system built with Go, designed for scalability with clean architecture principles.

## Features

- Invoice CRUD operations
- Client management
- PDF invoice generation
- Email notifications via SES
- Async processing with SQS
- Event publishing via SNS
- File storage with S3
- Complete Swagger documentation
- Health check endpoints
- Rate limiting
- Structured logging with Zerolog

## Architecture

Monolithic but scalable architecture designed with clean architecture patterns for future microservice extraction.

### Project Structure

```
invoice-backend/
├── cmd/api/              # Application entry point
├── internal/
│   ├── config/           # Configuration management
│   ├── domain/           # Domain models (bounded contexts)
│   ├── infrastructure/   # External dependencies (AWS)
│   ├── application/      # Business logic services
│   ├── interfaces/       # HTTP handlers & middleware
│   └── utils/            # Shared utilities
├── pkg/                 # Public packages
├── terraform/           # Infrastructure as code
├── scripts/             # Helper scripts
└── tests/               # Test files
```

## Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Terraform 1.x
- tflocal (LocalStack wrapper)
- AWS CLI (optional)

## Quick Start

### 1. Install Dependencies

```bash
make deps
```

### 2. Start LocalStack

```bash
make localstack-up
```

### 3. Initialize Terraform

```bash
make tf-init
```

### 4. Apply Terraform

```bash
make tf-apply
```

### 5. Build and Run

```bash
make build
make run
```

The API will be available at `http://localhost:8080`

## API Documentation

Swagger UI is available at: `http://localhost:8080/swagger/index.html`

## Development

### Available Commands

```bash
make help           # Show all available commands
make build          # Build the application
make run            # Run the application
make test           # Run all tests
make race           # Run tests with race detector
make coverage       # Generate coverage report
make lint           # Run linter
make swag-init     # Generate Swagger documentation
make localstack-up  # Start LocalStack
make localstack-down # Stop LocalStack
make tf-init        # Initialize Terraform
make tf-apply       # Apply Terraform
make tf-destroy     # Destroy Terraform resources
```

### Environment Variables

Create a `.env` file based on `.env.example`:

```bash
cp .env.example .env
```

## Testing

Run all tests:

```bash
make test
```

Run with race detection:

```bash
make race
```

Generate coverage report:

```bash
make coverage
```

## Infrastructure

Terraform is used to provision AWS resources via LocalStack:

- **DynamoDB**: Invoices and Clients tables with GSIs
- **S3**: Buckets for invoices, logos, and documents
- **SES**: Email sending
- **SNS**: Notification topics
- **SQS**: Async processing queues

## Design Principles

- **Clean Architecture**: Clear separation of concerns
- **Domain-Driven Design**: Bounded contexts for Invoice and Client
- **Testability**: Interface-based dependency injection
- **Scalability**: Stateless design, ready for horizontal scaling
- **Self-Documenting Code**: Clear naming, minimal comments
- **Comprehensive Testing**: 80%+ coverage target

## License

Apache 2.0
