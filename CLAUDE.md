# CLAUDE.md

This file provides Claude Code guidance for this repository.

@AGENTS.md

## Memory/Scope Rules

- This file is project memory for Claude Code.
- If a deeper `CLAUDE.md` exists in a subdirectory, it overrides this file for files in that subtree.
- Keep instructions here concise and task-enabling; put shared cross-agent rules in `AGENTS.md`.

## Claude-Specific Priorities

1. Read `AGENTS.md` first, then apply the task.
2. Keep edits small and explain assumptions when repo behavior is ambiguous.
3. Prefer existing project commands and workflows over ad hoc scripts.
4. When changing behavior, add or update tests instead of relying on manual claims.

## Quick Start Context

- Primary runtime entrypoints: `cmd/lambda/http/main.go` and `cmd/lambda/*`
- Health endpoint: `GET /health`
- Swagger endpoint: `GET /swagger/index.html`

Bootstrapping:

```bash
cp .env.example .env
make deps
make migrate-up
make run-local
```

## What Claude Should Verify Before Handoff

Run these unless the task is docs-only or explicitly says otherwise:

```bash
make fmt
make lint
make test
```

Also run when relevant:

```bash
make test-integration
make swagger
```

## Operational Notes

- Configuration is loaded via Viper from `.env` + env vars (`internal/config/config.go`).
- Shared runtime wiring lives in `internal/app/runtime.go`.
- Auth and tenant scoping depend on middleware context helpers:
  - `middleware.GetUserID(c)`
  - `middleware.GetBusinessID(c)`
  - `middleware.GetRole(c)`
- Route groups live under `/api/v1` in `internal/app/runtime.go`.

## Safe Defaults for Claude

- Prefer `rg` for code discovery.
- Do not commit `.env` values, secrets, or generated credentials.
- Keep business scoping intact on all protected resource queries.
- If Node.js tools are ever required in this repo, use `pnpm`.
- Do NOT create any extra markdown files (e.g., README, CHANGELOG, docs, summaries) after completing a task. Only modify existing `.md` files when explicitly requested.

## Coding Style Contract (DRY + KISS + Paradigms)

Use these defaults for all implementation tasks:

1. DRY: Keep one source of truth for each business rule, validation rule, and query rule. If duplicated logic appears in 2+ places, extract it into the correct layer.
2. KISS: Choose the simplest implementation that satisfies requirements and tests; avoid speculative abstractions (YAGNI).
3. Layering: keep `handlers` thin, enforce use-case rules in `services`, and isolate storage concerns in `repositories`.
4. Dependency inversion: services depend on repository interfaces, not concrete DB implementations.
5. Context and errors: propagate `context.Context`; wrap errors with `%w` and add operation context.
6. Testability: prefer designs that can be unit-tested without DB/network. For bug fixes, reproduce with a failing test first when practical.

## Production Structure Defaults

When adding new backend features, follow this file placement pattern:

1. HTTP contract and parsing: `internal/handlers/<domain>_handler.go`
2. Business workflow: `internal/services/<domain>_service.go`
3. Data contracts: `internal/repositories/interfaces/<domain>_repository.go`
4. PostgreSQL adapter: `internal/repositories/postgres/<domain>_repository.go`
5. Domain/persistence model: `internal/models/<domain>.go`
6. Integration coverage: `tests/integration/...`

Additional placement rules:

1. Keep flow vertical (handler -> service -> repository).
2. Avoid putting business logic into `internal/utils`.
3. Prefer adding to existing packages before creating new top-level directories.

## References

- DRY origin and definition: https://media.pragprog.com/articles/may_04_improve_code1.pdf
- YAGNI: https://martinfowler.com/bliki/Yagni.html
- Simple design rules: https://martinfowler.com/bliki/BeckDesignRules.html
- Dependency Injection: https://martinfowler.com/articles/injection.html
- Go module layout: https://go.dev/doc/modules/layout
- Effective Go: https://go.dev/doc/effective_go
- Go Code Review Comments: https://go.dev/wiki/CodeReviewComments
- Twelve-Factor App: https://12factor.net/
