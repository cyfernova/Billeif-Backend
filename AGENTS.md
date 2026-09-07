# AGENTS.md

Repository instructions for coding agents working in `invoice-backend`.

## 1) Scope and Precedence

- These instructions apply repository-wide unless closer instructions override them.
- In each directory, `AGENTS.override.md` takes precedence over `AGENTS.md`; Codex loads at most one instruction file per directory.
- Codex merges instructions from the repository root toward the working directory. Put subsystem-only rules near the code they govern.

## 2) Project Snapshot

- Go `1.25.0`, Gin, GORM, PostgreSQL, Redis, and AWS Cognito JWT
- Production HTTP Lambda: `cmd/lambda/http/main.go`
- Local HTTP server: `cmd/server`
- Other Lambda entry points: `cmd/lambda/`
- API base path: `/api/v1`

## 3) Authorization and Boundaries

- For answers, reviews, diagnoses, audits, or plans: inspect and report; do not edit unless asked.
- For change, build, fix, or refactor requests: make in-scope local changes and run relevant non-destructive checks.
- Ask before external writes, destructive actions, costs, production operations, or material scope expansion.
- Keep changes minimal and backward compatible unless a breaking change is explicitly requested.
- Preserve unrelated user changes; never discard or overwrite them.
- Do not create Markdown files unless explicitly requested. Modify existing Markdown only when the task requires it or the user asks.

## 4) Canonical Commands

Use Make targets when an appropriate target exists.

```bash
make run-local
make build-lambda
make package-lambda
make test                 # race and coverage
make test-integration     # requires local dependencies
make lint
make fmt
make swagger
make deps
```

Focused tests:

```bash
go test -v ./internal/services/... -run TestName
go test -v ./tests/integration/... -run TestName -tags=integration
```

Migrations:

```bash
make migrate-up
make migrate-down
make migrate-create NAME=add_example_table
```

Treat rollbacks and infrastructure targets as destructive. Confirm the exact environment before running them against shared or remote resources.

## 5) Architecture and Coding Rules

Follow the existing layers:

- `internal/handlers`: HTTP transport; `internal/services`: use cases and invariants
- `internal/repositories/interfaces`: contracts; `internal/repositories/postgres`: PostgreSQL adapters
- `internal/models`: models; `internal/config`: configuration; `internal/middleware`: HTTP cross-cutting behavior
- `internal/workers`: asynchronous work; `pkg`: reusable non-domain libraries

Rules:

1. Keep handlers thin; keep persistence details in repository adapters.
2. Services depend on repository interfaces, not infrastructure implementations.
3. Pass `context.Context` through request-scoped service and repository calls.
4. Wrap errors with context using `%w`; add typed errors only when callers must branch.
5. Preserve auth, RBAC, and `business_id` tenant checks on protected endpoints.
6. Prefer explicit composition, focused functions, and early returns.
7. Keep each business, validation, and query rule authoritative in one layer. Extract repetition only after a real second use.
8. Keep domain logic out of `internal/utils`; utilities must be generic and side-effect free.
9. Reuse existing types, logging, middleware, and dependency-injection patterns.
10. Keep business logic testable without network or database dependencies; prefer table-driven tests where useful.
11. Use `pnpm` for new Node.js tooling. Never track credentials, tokens, `.env` contents, or local secrets.

## 6) Change Workflows

### Endpoint changes

- Update the handler and transport contract, put business behavior in the service, and change repositories only when persistence changes.
- Add focused tests and integration coverage for external boundaries. Run `make swagger` for route or API contract changes.

### Database-backed changes

- Create a paired migration, validate both paths locally, update models and repositories in their existing layers, and test the resulting behavior.

### Production bug fixes

- Reproduce the defect with a test when practical, apply the smallest fix in the owning layer, then run focused and relevant broader checks.
- Report out-of-scope hardening such as metrics, validation, or retries.

## 7) Validation

- Run the smallest relevant checks while iterating.
- After meaningful Go changes, run `make fmt`, `make lint`, and `make test`.
- Run `make test-integration` for changed integration boundaries when dependencies are available.
- Run `make swagger` for API changes; include and validate migrations for schema changes.
- If a required command cannot run, report the command, blocker, and substitute validation.
- Inspect the final diff and confirm no secrets or unrelated changes were added.

## 8) Domain and Runtime Notes

- Business scoping: tenant isolation by `business_id`
- A2A: agent-to-agent tasks and streams under `/api/v1/a2a/v0.3`
- AP2: protocol and repository surface used by handlers and services
- Marketplace/Bargaining: commerce and negotiation workflows
- Agent discovery: `.well-known/agent.json` and `.well-known/agents.json`
- Dependency-injection container: `internal/services/container.go`

New domain work should follow:

```text
internal/handlers/<domain>_handler.go
    -> internal/services/<domain>_service.go
    -> internal/repositories/interfaces/<domain>_repository.go
    -> internal/repositories/postgres/<domain>_repository.go
```

Keep transport DTOs near handlers and persistence models near repositories or `internal/models`. Never expose database-only fields through public contracts.

## 9) Code Review Rules

- Prioritize correctness, security, tenant isolation, data integrity, concurrency, compatibility, and missing tests.
- Flag protected endpoints missing auth, authorization, or `business_id` scoping; identify the safe existing pattern.
- Flag misplaced business or persistence logic when it creates concrete risk.
- Flag schema changes lacking paired migrations, reversibility, or compatibility handling.
- Cite the smallest relevant lines, explain impact, and recommend a correction.
- Skip automated formatting or lint issues unless they reveal behavioral risk.
- Reviews are read-only unless fixes are also requested.

## 10) PR and Commit Guidance

- Prefer small, focused changes.
- Recommended title: `<area>: <what changed>`.
- Recommended branch: `feature/<area>-<short-desc>` or `fix/<area>-<short-desc>`.
- Include validation evidence and migration, rollout, or compatibility notes.
- Do not mention Codex in branch names or commit messages.

## 11) Maintenance

- Keep this file as a concise operational contract, not general engineering documentation.
- Propose updates when guidance is stale or missing; edit it only when explicitly requested.

## Agent skills

### Issue tracker

Issues and PRDs are tracked in this repository's GitHub Issues. See `docs/agents/issue-tracker.md`.

### Triage labels

Triage uses the five default canonical label names. See `docs/agents/triage-labels.md`.

### Domain docs

This repository uses a single-context layout with root domain documents and system-wide ADRs. See `docs/agents/domain.md`.
