# AGENTS.md

Repository-level instructions for coding agents working in `invoice-backend`.

Applies to the whole repository unless a deeper `AGENTS.md` overrides it.
If both `AGENTS.md` and tool-specific files exist, follow all non-conflicting instructions, with deeper files winning inside their subtree.

## 1) Project Snapshot

- Language: Go `1.24`
- HTTP framework: Gin
- ORM: GORM
- Database: PostgreSQL
- Cache: Redis
- Auth: AWS Cognito JWT
- Entry point: `cmd/api/main.go`
- API base path: `/api/v1`

## 2) Canonical Commands

Use these first; avoid inventing custom alternatives when a Make target exists.

```bash
make run                  # Start API server
make build                # Build .build/api
make test                 # Unit tests (race + coverage)
make test-integration     # Integration tests (Docker-backed)
make lint                 # golangci-lint
make fmt                  # go fmt + goimports
make swagger              # Regenerate Swagger docs
make deps                 # go mod download + tidy
```

Single test examples:

```bash
go test -v ./internal/services/... -run TestName
go test -v ./tests/integration/... -run TestName -tags=integration
```

Database migrations:

```bash
make migration-up
make migration-down
make migration-create NAME=add_example_table
make migration-status
```

Docker/local infra:

```bash
make docker-up
make docker-down
```

## 3) Agent Working Agreement

1. Keep changes minimal, targeted, and reversible.
2. Follow existing layering:
   - `handlers` for HTTP/parsing/response
   - `services` for business logic
   - `repositories` for persistence
3. Do not bypass auth/tenant checks in protected endpoints.
4. Never commit secrets from `.env` or credentials from local setup.
5. Run `make fmt`, `make lint`, and `make test` after meaningful code changes.
6. If API contracts change, run `make swagger`.
7. If schema changes, add migration files and validate up/down paths.
8. Use `pnpm` (not `npm`/`yarn`) for any Node.js tooling that may be introduced.

## 4) High-Value Workflows

### Add or modify an endpoint

1. Update handler in `internal/handlers`.
2. Update service logic in `internal/services`.
3. Update repository interface/implementation if persistence changes.
4. Add/update tests (unit first, then integration if behavior spans boundaries).
5. Regenerate Swagger if request/response/route annotations changed.

### Add a database-backed feature

1. Create migration: `make migration-create NAME=...`
2. Apply locally: `make migration-up`
3. Add/adjust GORM models and repository logic.
4. Add tests that validate migration-backed behavior.

### Fix a production bug

1. Add or identify a failing test reproducing the issue.
2. Implement smallest fix in correct layer.
3. Verify with `make test` and impacted integration tests.
4. Note any follow-up hardening work (metrics, validation, retry logic, etc.).

## 5) Architecture Notes

- `cmd/api/main.go`: wiring, router setup, graceful shutdown, workers startup.
- `internal/config`: env-driven config via Viper.
- `internal/middleware`: auth, RBAC, CORS, request ID, logging, recovery.
- `internal/handlers`: route handlers and HTTP contracts.
- `internal/services/container.go`: dependency injection container.
- `internal/repositories/interfaces`: repository contracts.
- `internal/repositories/postgres`: PostgreSQL implementations.
- `internal/workers/worker.go`: async processing (SQS-backed jobs).
- `.well-known/agent.json` + `.well-known/agents.json`: agent discovery metadata.

## 6) Domain Terms

- Business scoping: Multi-tenant access control by `business_id`.
- A2A: Agent-to-agent task/stream protocol under `/api/v1/a2a/v0.3`.
- AP2: Additional protocol/repository surface used by handler/service layers.
- Marketplace/Bargaining: commerce and negotiation flows with dedicated services.

## 7) Code Quality Checklist

Before finishing a task, confirm:

1. Code is formatted (`make fmt`).
2. Lint is clean (`make lint`).
3. Tests pass (`make test` and any relevant integration tests).
4. Swagger updated when API changed.
5. Migrations included when schema changed.
6. No credentials or environment secrets were added to tracked files.

## 8) Go Style Conventions

1. Keep handlers thin; put business logic in services.
2. Accept `context.Context` where long-running/service calls are made.
3. Wrap errors with context (`fmt.Errorf("...: %w", err)`).
4. Prefer table-driven tests for service/repository logic.
5. Reuse existing logger and middleware patterns instead of new frameworks.
6. Preserve backward compatibility for public API fields unless change is explicitly requested.

## 9) PR/Commit Guidance

- Prefer small focused PRs.
- Title format recommendation: `<area>: <what changed>`
  - Example: `invoice: enforce business scoping in list endpoint`
- Branch naming recommendation: `feature/<area>-<short-desc>` or `fix/<area>-<short-desc>`
- Include validation evidence (tests run, lint status, migration notes).
- Mention rollout or backward-compatibility concerns when relevant.

## 10) Keep This File Current

- Treat this as a living playbook: update it when build/test/migration workflows change.
- If a repeated agent mistake happens twice, add a short preventive rule here.
