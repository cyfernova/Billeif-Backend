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

- Primary runtime entrypoint: `cmd/api/main.go`
- Default API port: `:8080`
- Metrics endpoint: `:9090`
- Health endpoint: `GET /health`
- Swagger endpoint: `GET /swagger/index.html`

Bootstrapping:

```bash
cp .env.example .env
make deps
make migration-up
make run
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
- Worker processing starts in `main.go` via `internal/workers`.
- Auth and tenant scoping depend on middleware context helpers:
  - `middleware.GetUserID(c)`
  - `middleware.GetBusinessID(c)`
  - `middleware.GetRole(c)`
- Route groups live under `/api/v1` in `cmd/api/main.go`.

## Safe Defaults for Claude

- Prefer `rg` for code discovery.
- Do not commit `.env` values, secrets, or generated credentials.
- Keep business scoping intact on all protected resource queries.
- If Node.js tools are ever required in this repo, use `pnpm`.
