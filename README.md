## Features

- Invoice creation, tracking, and PDF export
- Payment processing with Razorpay integration
- Double-entry accounting ledger
- Multi-tenant business profiles with team management
- Agent lifecycle management and discovery registry
- Agent-to-Agent (A2A) protocol v0.3 with streaming and push notifications
- Shopping agent with cart and checkout
- Bargaining and counter-offer negotiation
- Workflow automation (create, run, pause, resume)
- LLM integration (Claude, Gemini)
- WebSocket real-time notifications
- SQS-based async job processing
- Cognito JWT authentication with role-based access control

## Tech Stack

- **Language:** Go 1.24
- **Framework:** Gin
- **ORM:** GORM
- **Database:** PostgreSQL 16
- **Cache:** Redis 7
- **Auth:** AWS Cognito (JWT)
- **Cloud:** AWS (S3, SQS, SES, SNS, DynamoDB)
- **Payments:** Razorpay
- **Monitoring:** Sentry, Prometheus
- **Docs:** Swagger / OpenAPI

## Installation

**Prerequisites:** Go 1.24+, PostgreSQL 16, Redis 7, AWS credentials.

```bash
git clone <repo-url>
cd invoice-backend
cp .env.example .env    # configure your environment
make deps               # download and tidy modules
make migration-up       # apply database migrations
make run                # start the server on :8080
```

### Docker

```bash
make docker-up          # starts PostgreSQL, Redis, and the API
make docker-down        # stops and removes containers
```

## Usage

```bash
make run                # run the API server
make build              # build binary to .build/api
make test               # unit tests with race detection
make test-integration   # integration tests (requires Docker)
make lint               # run golangci-lint
make fmt                # format code (goimports + go fmt)
make swagger            # regenerate Swagger docs
```

Run a single test:

```bash
go test -v -run TestFunctionName ./internal/services/...
```

### Database Migrations

```bash
make migration-up                     # apply all pending migrations
make migration-down                   # rollback one migration
make migration-create NAME=add_xyz    # create new migration files
```

## Configuration

Viper-based configuration loaded from `.env` with environment variable overrides. Key sections:

- **Server** -- port, base URL, timeouts
- **Database** -- PostgreSQL connection, SSL mode
- **Redis** -- host, port, password
- **Cognito** -- user pool ID, client ID, JWKS refresh rate
- **AWS** -- region, credentials, S3 buckets, SQS queues
- **Razorpay** -- API key, secret, webhook secret
- **Sentry** -- DSN, sample rates
- **LLM** -- API key, model, timeout

See `internal/config/config.go` for the full configuration struct.

## API Endpoints

All routes are under `/api/v1` unless noted otherwise.

- `/auth` -- registration, login, token refresh, password reset
- `/business-profiles` -- business entity CRUD
- `/customers` -- customer management, import/export
- `/vendors` -- vendor management
- `/products` -- product catalog with inventory and images
- `/invoices` -- invoice CRUD, PDF generation, sending
- `/payments` -- payment tracking and processing
- `/ledger` -- double-entry accounting ledger
- `/teams` -- team member management
- `/agents` -- agent lifecycle, shopping, credentials, configuration
- `/discovery` -- agent registry, verification, health checks
- `/marketplace` -- product listings, merchant management, orders
- `/a2a/v0.3` -- Agent-to-Agent protocol (tasks, streaming, subscriptions)
- `/bargaining` -- negotiations and counter-offers
- `/llm` -- chat and agent-assist
- `/workflows` -- workflow automation
- `/ws` -- WebSocket connections and notifications
- `/webhooks` -- event subscriptions

Additional: `GET /health`, `GET /swagger/*`, `GET /.well-known/agent.json`.

## Project Structure

```
cmd/api/                 Entry point and router setup
internal/
  handlers/              HTTP handlers
  services/              Business logic (container-based DI)
  repositories/
    interfaces/          Repository contracts
    postgres/            PostgreSQL implementations
  models/                GORM entities
  middleware/            Auth, RBAC, CORS, rate limiting, logging
  workers/               SQS background job processors
  config/                Configuration loading and validation
pkg/
  a2a/                   Agent-to-Agent protocol
  awsclients/            AWS SDK wrappers
  nlp/                   LLM clients (Claude, Gemini)
  razorpay/              Payment gateway
  websocket/             Real-time connections
migrations/              SQL migration files (golang-migrate format)
infrastructure/          Terraform IaC
```

## Contributing

1. Fork the repository and create a feature branch.
2. Run `make fmt` and `make lint` before committing.
3. Ensure `make test` passes.
4. Open a pull request against `main`.
