# Task 2b report: idempotent atomic draft creation

## Status

- Completed and committed.
- Commit: `44f4f8c3f0ad2f0641233bb31dcbe04d486910f2`
- No migration, Terraform, AWS, deployment, secret, or non-disposable database operation was performed.
- Tracked worktree was clean after commit.

## Architecture audit

Before this change, `InvoiceService.Create` performed three independent writes:

1. `InvoiceRepository.Create` committed the invoice and associated invoice items.
2. `DocumentService.MirrorLegacyInvoice` separately created or updated the Document projection, lines, withholdings, and revision. Its failure was logged and ignored.
3. `recordActivityLog` separately inserted an activity row and its failure was ignored.

The repository exposed no creation unit-of-work seam. The existing v44 schema already provided the required `api_idempotency_keys` unique scope and the invoice, Document, activity, outbox, render, and email tables, so no schema change was needed.

The production dependency is now explicit: `CanonicalInvoiceRepository` embeds the ordinary invoice repository and requires `CreateDraftAtomic`. `InvoiceService` cannot silently fall back to the legacy non-atomic `Create` method.

## Implementation

### Canonical request hashing

- Added `internal/idempotency`.
- JSON objects are recursively sorted by key.
- Arrays retain their input order.
- Exact JSON decimal meanings are normalized, so equivalent spellings such as `100`, `1e2`, and `100.00` hash identically.
- Null and absent members remain distinct.
- Unsupported values, invalid numbers, NaN, and infinities return `InvalidPayloadError`.
- Invalid payload errors expose neither request contents nor an unwrap-able parser error.
- Canonical bytes are hashed with SHA-256.
- Server-default invoice timestamps are applied only after hashing. An omitted `invoice_date` therefore remains a stable zero-value request semantic across retries.

### Idempotency and concurrency

- Invoice creation requires a non-empty UUID `Idempotency-Key`.
- Scope is `(business_id, "invoice.create", idempotency_key)`.
- The PostgreSQL transaction first runs `INSERT ... ON CONFLICT DO NOTHING` for an `in_progress` claim.
- A changed request hash returns typed `ConflictError`.
- A completed identical request replays the original invoice ID and reloads the original invoice and items.
- A visible legacy/stale identical `in_progress` row returns typed retryable `InProgressError`.
- Concurrent identical transactions rely on the PostgreSQL unique index. A contender waits at the unique insert, then either claims after a rollback or observes and replays the committed result. There is no sleep, busy loop, or process mutex.
- Claim completion, including result type, result ID, and completion time, happens in the same transaction as all command rows.

### Atomic unit of work

The transaction now writes, in order:

1. idempotency claim;
2. unnumbered v1 draft invoice;
3. invoice items;
4. same-ID Document projection;
5. same-ID source line projections;
6. activity row from the authenticated actor context;
7. explicitly supplied outbox rows;
8. explicitly supplied render jobs;
9. explicitly supplied email delivery rows;
10. completed idempotency result.

Plain draft creation supplies no outbox, render, or email rows. Commit failure is returned as failure. Every GORM operation inherits the request context.

The projection carries the same tenant, customer party, origin linkage, seller and buyer legal snapshots, project/price/render/origin references, currency, tax facts, totals, and line legal/pricing snapshots. Drafts remain `invoice_no = null`, `issued_at = null`, status `draft`, version `1`, and do not touch document sequences.

### Service and transport wiring

- Removed the post-create best-effort Document mirror and activity write from canonical `InvoiceService.Create`.
- Added the business repository to snapshot seller identity and currency.
- Existing invoice POST reads `Idempotency-Key`, preserves authenticated actor context, and maps invalid keys to 400 and conflicts/in-progress results to 409.
- Swagger documents the required invoice header and 409 response.
- Subscription generation uses the persisted run UUID as its stable idempotency key.
- Generic sales-invoice Document creation delegates through a narrow adapter to canonical `InvoiceService.CreateByBusiness` exactly once. It does not recursively call public Document creation or create a second aggregate.
- Non-sales generic documents keep their existing path.

## Files changed

- Canonical hashing and typed errors:
  - `internal/idempotency/canonical.go`
  - `internal/idempotency/errors.go`
- Atomic repository contract and PostgreSQL implementation:
  - `internal/repositories/interfaces/repositories.go`
  - `internal/repositories/postgres/invoice_repo.go`
  - repository assembly/runtime wiring
- Canonical service and projection:
  - `internal/services/invoice_service.go`
  - `internal/services/invoice_atomic_create.go`
  - `internal/services/document_service.go`
  - `internal/services/container.go`
  - `internal/services/billing_ops_service.go`
- Existing handler integration and generated Swagger:
  - `internal/handlers/invoice_handler.go`
  - `internal/handlers/document_handler.go`
  - `docs/docs.go`
- Tests and SQL mock dependency:
  - canonical hash, service concurrency/projection/delegation, handler mapping, PostgreSQL transaction fault injection, and optional disposable PostgreSQL integration tests
  - `go.mod`
  - `go.sum`

## TDD evidence

### RED

1. `go test ./internal/idempotency -count=1`
   - Failed on undefined `CanonicalHash` and `InvalidPayloadError`.
2. `go test ./internal/services -run 'TestInvoiceServiceCreate' -count=1`
   - Failed on undefined atomic command/result contracts, typed idempotency errors, required key, and explicit repository dependency.
3. `go test ./internal/handlers -run TestInvoiceCreateErrorStatusMapsIdempotencyErrors -count=1`
   - Failed on undefined transport error mapping.
4. `go test ./internal/services -run TestDocumentServiceSalesInvoiceDelegationCallsCanonicalCreatorOnceWithoutRecursion -count=1`
   - Failed on the missing narrow delegation boundary.
5. Canonical invalid-payload leak regression
   - Failed because the first implementation exposed its underlying parser error through `Unwrap`.

### GREEN

- `go test -race ./internal/idempotency ./internal/services ./internal/repositories/postgres ./internal/handlers -count=1`
  - Passed.
- SQL fault-injection suite
  - Passed for idempotency claim, invoice, invoice items, Document, Document lines, activity, outbox, render, email, idempotency completion, and commit.
- Service concurrency suite
  - Sixteen identical concurrent calls returned one invoice ID and one atomic execution.
- Projection suite
  - Passed same-ID, tenant, party, legal snapshot, origin, totals, tax, currency, and line snapshot assertions.
- Delegation suite
  - Passed exactly-one canonical creator invocation.

## Final verification

Fresh combined verification completed with exit code 0:

```text
make fmt
make lint
go test ./... -count=1
make test
make migration-manifest-verify
git diff --check
```

Additional verification:

- `make swagger` completed and regenerated `docs/docs.go`.
- Searches found no goroutine launch, direct SQS send, legacy `repo.Create(ctx, invoice)`, post-create mirror, or best-effort create activity call in the canonical create implementation.
- `git diff --name-only -- migrations infrastructure/terraform` returned no paths.
- PostgreSQL integration test deterministically skipped because `MIGRATION_TEST_DATABASE_URL` was not configured.

## Disposable PostgreSQL harness

When `MIGRATION_TEST_DATABASE_URL` is explicitly set, the repository integration test:

- validates that the DSN is PostgreSQL;
- creates an isolated randomized schema in the disposable database;
- creates only its isolated test tables;
- runs two real concurrent idempotency contenders;
- proves one aggregate and one completed claim;
- proves changed-hash conflict;
- proves a failed command rolls its claim back and can be retried;
- drops the isolated schema during cleanup.

SQLite is not used by any Task 2b test.

## Self-review and concerns

- No issue transition, sequence allocation, cursor endpoint, dispatcher, SQS publishing, mobile work, Terraform, or migration was added.
- The old `MirrorLegacyInvoice` calls remain only in pre-existing update/send compatibility paths. Canonical creation no longer uses them. Making draft updates and later lifecycle transitions atomic with their projections is follow-up work outside this slice.
- The real PostgreSQL integration path was not executed locally because the explicit disposable DSN was absent; SQL transaction tests and all default suites passed.

## Review fix round

### Status and architecture

- Addressed all six Important review findings without changing the PostgreSQL `INSERT ... ON CONFLICT DO NOTHING` claim algorithm or adding a migration.
- Canonical hashing now uses a dedicated command payload that includes trusted subscription and run origins while excluding only the idempotency key.
- Document tax projection keeps GST and cess distinct, reconciles component totals, and projects CGST/SGST or IGST using seller state names or GSTIN state codes.
- Supported terms, custom fields, and additional charges now survive the same-ID Document projection.
- Generic sales-document fields that the canonical invoice schema cannot faithfully represent are rejected as a sanitized invalid payload before delegation; the handler maps that error to 400.
- Completed replay loads soft-deleted invoices unscoped and falls back to the immutable tenant-scoped result ID when the aggregate row is no longer visible, without re-executing.
- Atomic commands are tenant/aggregate validated before the idempotency claim, including optional outbox, render, and email rows.
- The optional PostgreSQL harness no longer creates a global extension or disables FKs. It requires an empty disposable database, creates and drops a randomized schema, aligns UUID types, installs production-relevant FKs, seeds tenant parents, and asserts optional rows.
- Swagger now documents the conditional sales-document idempotency header and 409 response.

### Files changed

- `internal/services/invoice_service.go`
- `internal/services/invoice_atomic_create.go`
- `internal/services/invoice_atomic_create_test.go`
- `internal/repositories/postgres/invoice_repo.go`
- `internal/repositories/postgres/invoice_atomic_repo_test.go`
- `internal/repositories/postgres/invoice_atomic_repo_integration_test.go`
- `internal/handlers/invoice_handler.go`
- `internal/handlers/invoice_handler_idempotency_test.go`
- `internal/handlers/document_handler.go`
- `docs/docs.go`

### TDD evidence

Focused RED failures were recorded for:

1. GST and cess double-counting and missing component splits.
2. Changed trusted subscription run origin replaying instead of conflicting.
3. Generic sales fields being silently discarded and supported editor fields missing from the projection.
4. Completed replay failing after the invoice became invisible.
5. Cross-tenant optional rows reaching SQL before validation.
6. The missing empty-disposable-database guard.
7. State-code place-of-supply being misclassified as interstate.
8. Invalid canonical payloads mapping to HTTP 500.

Focused GREEN:

```text
go test -race ./internal/idempotency ./internal/services ./internal/repositories/postgres ./internal/handlers -count=1
```

Final verification completed with exit code 0:

```text
make fmt
make lint
go test ./... -count=1
make test
make swagger
make migration-manifest-verify
git diff --check
```

### Concerns

- `MIGRATION_TEST_DATABASE_URL` was not configured, so the real PostgreSQL path deterministically skipped; its safety guard has deterministic SQL-mock coverage.
- No AWS, deployment, secret, Terraform/state, migration, or non-disposable database operation was performed.

## Review fix round 2

### Status and architecture

- Preserved the strict generic sales-document validator from round 1.
- Added a package-private POS-to-canonical entry point. It is not reachable through the generic document HTTP contract.
- POS checkout now maps its server cart and session into a trusted canonical invoice command with:
  - the required checkout idempotency key;
  - `pos` invoice origin;
  - customer identity when present, or an explicit `Counter sale` buyer snapshot for anonymous/manual checkout;
  - session currency;
  - tax, dispatch, transport, and counterparty facts;
  - POS source and session linkage;
  - canonical line snapshots.
- Legacy POS `issued` and `final` intent is deliberately normalized at the boundary to the canonical unnumbered draft lifecycle. The checkout path does not allocate a sequence, number an invoice, set `issued_at`, or dispatch work.
- Legacy POS non-GST mode maps to a canonical non-GST treatment rather than being silently discarded.
- Trusted origin, currency, and anonymous buyer snapshot fields are excluded from public JSON input and included in the canonical idempotency hash.
- Intra-state GST projection now rounds CGST to decimal storage precision and assigns the rounded tax remainder to SGST, so odd-paise GST reconciles exactly.

### TDD evidence

Focused RED:

```text
go test ./internal/services -run 'TestPOSCheckoutMapsActivePayloadToCanonicalUnnumberedDraft|TestInvoiceDocumentProjectionSplitsOddPaiseGSTWithoutLosingRemainder' -count=1
```

The POS regression failed with `invalid canonical payload`. The odd-paise regression projected `0.05` as `0.03 + 0.03` instead of `0.03 + 0.02`.

Focused GREEN:

```text
go test ./internal/services -run 'TestPOSCheckoutMapsActivePayloadToCanonicalUnnumberedDraft|TestInvoiceDocumentProjectionSplitsOddPaiseGSTWithoutLosingRemainder' -count=1
go test ./internal/services -count=1
go test -race ./internal/idempotency ./internal/services ./internal/repositories/postgres ./internal/handlers -count=1
```

Full verification completed with exit code 0:

```text
make fmt
make lint
go test ./... -count=1
make test
git diff --check
```

### Concerns

- The POS regression uses an isolated in-memory SQLite session table and no external or persistent database.
- No migration, issue transition, AWS, deployment, dispatcher, mobile, cloud, secret, Terraform/state, or non-disposable database operation was performed.

## Review fix round 3

### Status and architecture

- The production POS checkout handler now copies the authenticated user, role, request ID, and client IP into the service request context before invoking checkout.
- The canonical invoice actor requirement remains unchanged and fail-closed.
- `POSHandler` now depends on an internal service interface, allowing the production handler context boundary to be tested without copying handler behavior.
- POS mapping leaves `InvoiceDate` zero. Canonical creation hashes the stable command first and then applies its existing current-time default, so identical retry and concurrent commands retain the same hash.
- Party mapping now enforces exactly two supported combinations:
  - manual or omitted party type with no party ID produces an anonymous `Counter sale` snapshot;
  - customer party type with a party ID resolves the identified customer.
- Customer without an ID, manual with an ID, and omitted/default-manual type with an ID are rejected as invalid canonical payloads.

### TDD evidence

Focused RED:

```text
go test ./internal/services -run 'TestCanonicalPOSCheckoutInputReplaysSameSessionAndKey|TestCanonicalPOSCheckoutInputValidatesPartyTypeAndIDPairing' -count=1
```

The retry conflicted with a changed request hash, and all three invalid party pairings were accepted.

After introducing the minimal handler test seam, the production handler regression failed behaviorally with:

```text
checkout status = 400, want 201: {"error":"invoice create actor is required"}
```

Focused GREEN:

```text
go test ./internal/handlers -run TestPOSCheckoutPropagatesAuthenticatedActorToServiceContext -count=1
go test ./internal/services -run 'TestCanonicalPOSCheckoutInputReplaysSameSessionAndKey|TestCanonicalPOSCheckoutInputValidatesPartyTypeAndIDPairing' -count=1
go test ./internal/handlers ./internal/services -count=1
go test -race ./internal/idempotency ./internal/services ./internal/repositories/postgres ./internal/handlers -count=1
```

Full verification completed with exit code 0:

```text
make fmt
make lint
go test ./... -count=1
make test
```

### Concerns

- No canonical actor or idempotency requirement was weakened.
- No external database, migration, issue transition, AWS, deployment, dispatcher, mobile, cloud, secret, or Terraform/state operation was performed.
