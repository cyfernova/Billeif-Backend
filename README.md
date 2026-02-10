# Invoice Backend

Production-Grade Golang backend for an invoice/billing platform, running on LocalStack with Terraform.

## Quick Start

```bash
# Start infrastructure
make localstack-up
make infra-apply
make migrate-up

# Initialize test resources
./scripts/init-aws-resources.sh

# Run the application
make run
```

## Prerequisites

- Go 1.21+
- Docker & Docker Compose
- Terraform & tflocal (`pip install terraform-local`)
- golang-migrate (`brew install golang-migrate`)
- awslocal (`pip install awscli-local`)

## Architecture

```
cmd/api/              # Application entry point
internal/
├── config/           # Viper configuration
├── middleware/       # Gin middleware (auth, RBAC, logging, rate-limit, sentry)
├── models/           # GORM domain models
├── handlers/         # HTTP handlers
├── services/         # Business logic
├── repositories/     # Data access layer
├── workers/          # SQS background workers
└── utils/            # Helpers (JWT, pagination, errors)
pkg/
├── awsclients/       # AWS SDK v2 with LocalStack support
├── logger/           # Zap structured logging
└── sentry/           # Sentry error tracking integration
infrastructure/
├── terraform/        # IaC: Cognito, DynamoDB, S3, SES, SNS/SQS
└── localstack/       # Init scripts
```

## Makefile Commands

```bash
make localstack-up    # Start LocalStack, Postgres, Redis
make infra-apply      # Apply Terraform via tflocal
make build            # Build binary
make run              # Run app locally
make test             # Run unit tests
make integration-test # Full integration test suite
make migrate-up       # Run database migrations
make lint             # Run golangci-lint
```

## API Endpoints

### Authentication
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register user |
| POST | `/api/v1/auth/login` | Login |
| POST | `/api/v1/auth/logout` | Logout |
| POST | `/api/v1/auth/refresh` | Refresh token |
| POST | `/api/v1/auth/forgot-password` | Request password reset |
| POST | `/api/v1/auth/reset-password` | Reset password |
| GET | `/api/v1/auth/me` | Get current user |

### Business
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/business-profiles` | List businesses |
| POST | `/api/v1/business-profiles` | Create business |
| GET | `/api/v1/business-profiles/:id` | Get business |
| PUT | `/api/v1/business-profiles/:id` | Update business |
| POST | `/api/v1/business-profiles/:id/logo` | Get logo upload URL |

### Invoices
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/invoices` | List invoices |
| POST | `/api/v1/invoices` | Create invoice |
| GET | `/api/v1/invoices/:id` | Get invoice |
| POST | `/api/v1/invoices/:id/send` | Send invoice email |
| GET | `/api/v1/invoices/:id/pdf` | Get PDF URL |

Full API docs: `openapi/openapi.yaml`

## Example Workflows

### Register and Login
```bash
# Register
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"Password123!","name":"Test User"}'

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"Password123!"}'
# Save the access_token from response
```

### Create Business and Invoice
```bash
export TOKEN="<access_token>"

# Create business
curl -X POST http://localhost:8080/api/v1/business-profiles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"My Business","email":"biz@example.com"}'

# Create customer
curl -X POST http://localhost:8080/api/v1/customers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"business_id":"<biz_id>","name":"Customer","email":"cust@example.com"}'

# Create invoice
curl -X POST http://localhost:8080/api/v1/invoices \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "business_id":"<biz_id>",
    "customer_id":"<cust_id>",
    "due_date":"2025-01-31T00:00:00Z",
    "items":[{"description":"Service","quantity":1,"unit_price":100}]
  }'
```

## Production Migration

To switch from LocalStack to real AWS:

1. **Remove LocalStack endpoint override**:
   ```bash
   # .env
   AWS_LOCALSTACK=false
   # Remove AWS_ENDPOINT
   ```

2. **Set real AWS credentials**:
   ```bash
   AWS_ACCESS_KEY_ID=<real_key>
   AWS_SECRET_ACCESS_KEY=<real_secret>
   AWS_REGION=us-east-1
   ```

3. **Update Cognito config**:
   ```bash
   COGNITO_USER_POOL_ID=<real_pool_id>
   COGNITO_CLIENT_ID=<real_client_id>
   ```

4. **Run standard Terraform** (not tflocal):
   ```bash
   cd infrastructure/terraform
   terraform init
   terraform apply
   ```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| ENVIRONMENT | dev/staging/prod | dev |
| SERVER_PORT | HTTP port | 8080 |
| DATABASE_HOST | Postgres host | localhost |
| DATABASE_PORT | Postgres port | 5432 |
| AWS_LOCALSTACK | Use LocalStack | true |
| AWS_ENDPOINT | LocalStack URL | http://localhost:4566 |
| COGNITO_USER_POOL_ID | Cognito pool ID | - |
| COGNITO_CLIENT_ID | Cognito client ID | - |
| SENTRY_DSN | Sentry Data Source Name | - |
| SENTRY_SAMPLE_RATE | Error sample rate (0.0-1.0) | 1.0 |
| SENTRY_TRACES_SAMPLE_RATE | Performance trace rate | 0.2 |
| SENTRY_ENABLE_TRACING | Enable performance tracing | false |
| SENTRY_DEBUG | Debug mode for Sentry SDK | false |

## Error Tracking (Sentry)

Sentry is integrated for production error tracking and performance monitoring.

### Configuration

Add your Sentry DSN to `.env`:
```bash
SENTRY_DSN=https://your-key@sentry.io/project-id
SENTRY_SAMPLE_RATE=1.0          # Capture 100% of errors
SENTRY_TRACES_SAMPLE_RATE=0.2   # Sample 20% of transactions
SENTRY_ENABLE_TRACING=true      # Enable performance monitoring
```

### Features

- **Automatic panic capture** with full stack traces
- **Request context** (method, path, headers, query params)
- **User context** when authenticated
- **Performance monitoring** with transaction tracing
- **Breadcrumbs** for debugging
- **Runtime enrichment** (Go version, memory stats, goroutine count)

### Usage in Handlers

```go
import "invoice-backend/internal/middleware"

// Capture errors with context
func (h *Handler) SomeEndpoint(c *gin.Context) {
    err := someOperation()
    if err != nil {
        middleware.SentryErrorHandler(c, err,
            map[string]string{"operation": "some_operation"},
            map[string]interface{}{"input": input},
        )
        c.JSON(500, gin.H{"error": "failed"})
        return
    }
}

// Add breadcrumbs for debugging
middleware.AddSentryBreadcrumb(c, "db", "queried users", sentry.LevelInfo, nil)
```

## Testing

```bash
# Unit tests with coverage
make test-coverage

# Integration tests (requires running infra)
make integration-test
```

## License

MIT
