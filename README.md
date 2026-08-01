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
make migrate-up         # apply database migrations
make run-local          # run the HTTP Lambda handler locally
```

### Local PostgreSQL (optional)

```bash
docker compose up -d postgres
docker compose down
```

## Usage

```bash
make run-local          # run the HTTP Lambda handler locally
make build-lambda       # build all Lambda bootstrap binaries
make package-lambda     # package Lambda zip artifacts
make test               # unit tests with race detection
make test-integration   # integration tests (requires local deps running)
make lint               # run golangci-lint
make fmt                # format code (goimports + go fmt)
make swagger            # regenerate Swagger docs
```

Run a single test:

```bash
go test -v -run TestFunctionName ./internal/services/...
```

### Infrastructure Deployment

```bash
make infra-plan          # package Lambda artifacts and preview Terraform changes
make infra-apply         # package Lambda artifacts and apply Terraform changes
```

Worker queues use a shared SQS visibility timeout sized for the 60 second worker Lambda timeout plus batching window. If you change worker Lambda timeouts, update the queue timeout together.

Before enabling the HTTP API after Terraform creates the Billeif invoice cursor
secret metadata, seed its raw scalar `SecretString` with at least 32 bytes
through the approved out-of-band `asm-exec` workflow. Terraform intentionally
does not create or retain this value.

### India SMS OTP Setup

India phone authentication requires AWS End User Messaging SMS registration and DLT-approved values before Terraform apply. Set these Terraform variables through `terraform.tfvars`, `*.auto.tfvars`, or `TF_VAR_*` environment variables:

```hcl
india_sms_sender_id          = "ABCDEF"
india_dlt_entity_id          = "YOUR_PEID"
india_signup_template_id     = "YOUR_SIGNUP_TEMPLATE_ID"
india_auth_template_id       = "YOUR_AUTH_TEMPLATE_ID"
india_signup_message_template = "Your Invoice Backend verification code is {####}."
india_auth_message_template   = "Your Invoice Backend login code is {####}."
```

Use the exact DLT-approved message text, including case, spaces, and punctuation. The `{####}` placeholder is replaced with the Cognito OTP at send time.

### Cognito Custom Domain Rollout

The custom hostname is fixed to `auth.billeif.com` and uses a certificate in `us-east-1`, as required by Cognito. Roll it out in three applies so runtime consumers never move before the required DNS targets are available:

1. Apply with both custom-domain flags `false` (the default). Terraform creates the ACM certificate, keeps runtime consumers on the Cognito prefix domain, and outputs `cognito_custom_domain_acm_validation`.
2. Add that validation CNAME at the authoritative DNS provider and wait for the ACM certificate to become issued.
3. Set `ENABLE_COGNITO_CUSTOM_DOMAIN_PROVISIONING=true` for `make infra-plan` / `make infra-apply`, or set the matching GitHub repository variable. This apply validates ACM, creates the Cognito custom domain, and outputs its CloudFront target while runtime stays on the prefix domain.
4. Create the `auth.billeif.com` CNAME to that CloudFront target, configure the Google OAuth origin and redirect output by Terraform, and verify both.
5. Set `ENABLE_COGNITO_CUSTOM_DOMAIN_CUTOVER=true`. The final apply promotes runtime consumers to `auth.billeif.com`; enabling cutover also keeps provisioning enabled.

Keep both inputs set after cutover. For a non-destructive routing rollback, keep provisioning `true` and set only cutover to `false`; runtime returns to the preserved AWS prefix while the custom-domain attachment remains ready. Set both flags to `false` only when intentionally removing the custom-domain attachment after rollback.

### Terraform Recovery After Partial Apply

If `terraform apply` is interrupted while creating Lambdas, repair state before rerunning:

```bash
cd infrastructure/terraform
terraform state show aws_lambda_function.api_http
terraform state show aws_lambda_function.a2a_stream
terraform state show aws_lambda_function.ws_handler
terraform untaint aws_lambda_function.api_http
terraform untaint aws_lambda_function.a2a_stream
terraform untaint aws_lambda_function.ws_handler
```

If a Lambda already exists in AWS but is missing from Terraform state, import it instead of rerunning `apply` blindly:

```bash
cd infrastructure/terraform
terraform import aws_lambda_function.api_http invoice-backend-api-http
terraform import aws_lambda_function.a2a_stream invoice-backend-a2a-stream
terraform import aws_lambda_function.ws_handler invoice-backend-ws-handler
```

### Database Migrations

```bash
make migrate-up                     # apply all pending migrations
make migrate-down                   # rollback one migration
make migrate-create NAME=add_xyz    # create new migration files
```

## Configuration

Viper-based configuration loaded from `.env` with environment variable overrides. Key sections:

- **Server** -- base URL and protocol-facing endpoints
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
cmd/
  lambda/
    http/                API Gateway HTTP Lambda entrypoint
    a2a-stream/          Streaming Lambda runtime entrypoint
    sqs-invoice/         Invoice queue processor
    ws/                  WebSocket Lambda handler
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
