# Invoice Backend Code Review Report

Review date: 2026-07-24  
Target: `main` at `6f48f61a97c986015bac01b6fc2eefa85ea4fd4a`  
Target commit: `fix security authorization and AWS boundaries`

## Scope and method

This review covers all 512 paths tracked by the target commit: Go API and
service code, Lambda entrypoints and workers, SQL migrations, Terraform,
GitHub Actions, configuration, documentation, generated sources, tests, and
tracked binary artifacts.

The review was performed against an isolated archive of the exact commit. The
live working tree's pre-existing deletions of `config.env` and `logs_temp.txt`
were preserved; those two files were inspected read-only from the archive.
Six ignored files present only in the live checkout were excluded from
exact-`main` conclusions.

Validation was static except for the two checks explicitly authorized:

| Check | Result |
|---|---|
| `make lint` | Passed |
| `make build-lambda` | Passed for HTTP, A2A stream, invoice/payment/GST/bargaining SQS, WebSocket, and voice-session Lambdas |
| Tests, race detector, vet | Not run |
| Terraform, migrations, servers, deployments, exploit/PoC execution | Not run |

Passing lint and compilation establish syntax/type/build health only. They do
not exercise the database, AWS policies, concurrency, authorization, queue
redelivery, or production configuration paths implicated below.

## Executive assessment

The codebase compiles cleanly, and several defensive foundations are sound:
Cognito token validation binds issuer/client/token use, effective-business
membership is database checked on the routes that use it, outbound A2A HTTP
has useful SSRF controls, marketplace inventory's final counter update is
conditional, S3 public access is blocked, and sensitive runtime values are
generally sourced from SSM.

It is not ready for a production deployment without remediation. The most
urgent issue is tracked Terraform plan data containing secret-bearing state.
The next tier is dominated by tenant/permission gaps, financial and inventory
race conditions, a non-functional production migration path, and AWS/runtime
wiring that prevents WebSocket, voice, storage, and notification behavior from
working reliably.

## Urgent security finding

### S-0 — Tracked Terraform plan archives contain secret-bearing state

Seven tracked files are Terraform plan ZIP archives:

- `infrastructure/terraform/google_restore_plan`
- `infrastructure/terraform/tfplan-access-logs`
- `infrastructure/terraform/tfplan-access-logs-2`
- `infrastructure/terraform/tfplan-access-logs-3`
- `infrastructure/terraform/tfplan_apply`
- `infrastructure/terraform/tfplan_codex_apply`
- `infrastructure/terraform/tfplan_codex_apply2`

Their embedded state contains populated database, Razorpay, and Google OAuth
secret fields. Secret values were not printed or copied during this review.
`.gitignore:53-59` ignores only `tfplan` and `tfplan.*`, missing these
hyphenated, underscored, and custom names.

Impact: anyone with repository or history access may obtain production-capable
credentials and infrastructure metadata. The formal scan rating is Medium/P2
because repository reachability, credential freshness, provider mode, and
network access were not established. Operational response is nevertheless
urgent because source cleanup cannot revoke copied credentials.

Remediation:

1. Treat every embedded credential as compromised and rotate/revoke it.
2. Remove the archives from the current tree and purge them from Git history.
3. Invalidate cached artifacts and audit access since the first affected commit.
4. Ignore plan/state files by artifact type and naming pattern, and store plans
   only in access-controlled, short-lived CI storage.

## High-severity correctness and deployment findings

### H-1 — Payment writes use an invalid status and lose concurrent settlements

`internal/services/payment_service.go:41-96` reads the invoice before opening
the transaction, updates the stale in-memory aggregate, and saves it without a
row lock or atomic increment. `UpdateByBusiness` repeats the pattern at
`:153-208`. Concurrent payment requests can therefore overwrite each other's
`paid_amount` and `balance_due`.

Both paths write status `partial` (`:91`, `:204`), while the canonical invoice
state machine recognizes `partially_paid`
(`internal/services/invoice_service.go:428-434`) and the database check allows
neither value (`migrations/000006_invoices.up.sql:8`). A partial payment can
fail at commit even though unit tests explicitly expect the invalid value.

The journal and document projections run after commit and only log failures
(`payment_service.go:105-113`), while deletion removes the payment without
recomputing the invoice or reversing the journal (`:220-229`).

Remediation: lock the invoice row inside one transaction, recompute settlement
from durable payment rows, use one shared status enum reflected in a migration,
and commit payment, invoice, withholding, document projection, and journal
entry atomically or through a durable outbox/reconciliation workflow.

### H-2 — The production deployment does not apply the canonical schema

There are 43 canonical `migrations/*.up.sql` files, but the deployment workflow
only packages and runs Terraform
(`.github/workflows/deploy.yml:253-276`). Terraform defines five ad-hoc
`local-exec` SQL operations instead
(`infrastructure/terraform/migrations.tf:5-131`); they neither represent the
canonical history nor fail reliably because a trailing success message masks
`psql` failures.

The database is forced private
(`infrastructure/terraform/variables.tf:66-74`,
`rds.tf:28-30`), so GitHub's hosted runner cannot reach it for those local
commands. The correct `golang-migrate` path exists only as a manual Make target
(`Makefile:188-215`).

Impact: Terraform can report a successful deployment while the application
schema is absent or partially updated.

Remediation: run the canonical migration set from a VPC-reachable,
single-writer migration job before application rollout; fail on the first SQL
error; record schema version; remove the divergent Terraform SQL resources.

### H-3 — WebSocket and voice Lambdas lack required VPC/IAM permissions

Both functions declare `vpc_config`
(`infrastructure/terraform/lambda.tf:349-373,385-408`), but only the general API
and worker roles receive `AWSLambdaVPCAccessExecutionRole`
(`iam.tf:50-73`). The WebSocket and voice roles receive only the basic logging
policy (`iam.tf:75-93`), so Lambda cannot manage network interfaces for them.

WebSocket startup also resolves many SSM-backed settings
(`cmd/lambda/ws/main.go:46-64`; `lambda.tf:30-49`), but its application policy
grants no `ssm:GetParameter` (`iam.tf:339-370`). The separate voice role grants
only the two voice-provider parameters (`:379-409`).

Impact: deployment/update can fail, and WebSocket initialization cannot load
configuration even if the function is created.

Remediation: attach the VPC execution policy to both roles and grant each
function the exact SSM parameters it loads. Add Terraform tests that instantiate
every Lambda-to-role mapping.

### H-4 — S3 uploads target nonexistent buckets and persisted URLs are private

Terraform creates account-prefixed buckets
(`infrastructure/terraform/s3.tf:1-9,54-57,87-90`) and passes their names into
Lambda environment variables (`lambda.tf:51-53`). Business and product upload
services instead hard-code `business-logos` and `product-images`
(`internal/services/business_service.go:346-355`,
`product_service.go:412-421`), so presigning targets different/nonexistent
buckets.

All application buckets block public access (`s3.tf:45-52,78-85,104-111`),
yet `S3Service.GetObjectURL` returns an unsigned public URL
(`internal/services/s3_service.go:145-146`). That URL is persisted for profile
pictures, PDFs, and GST documents, while the implemented presigned GET helper
at `:52-68` has no production caller.

Impact: uploads fail or go to the wrong bucket, and stored assets/PDF links
return access denied.

Remediation: remove literal bucket names, pass configured bucket IDs through
all services, persist object keys rather than unsigned URLs, and generate
short-lived download URLs at authorized read time.

### H-5 — API and worker notification clients receive a `wss://` management endpoint

Terraform correctly distinguishes the WebSocket client URL from the HTTPS
management API (`infrastructure/terraform/lambda.tf:1-6`) but injects the
`wss://` value into the HTTP API, A2A stream, and queue workers
(`:149-153,184-188` and the equivalent worker environment blocks).
`WebSocketConnectionService` passes it directly to the API Gateway Management
API HTTP client (`internal/services/websocket_connection_service.go:39-54`).

Delivery errors are logged and swallowed for each connection (`:216-239`), so
handlers can return success while no notification was delivered.

Remediation: inject the HTTPS management endpoint for server-side posting,
retain `wss://` only for clients, and return/aggregate delivery errors with a
retry or dead-letter path.

### H-6 — Inventory and reservation updates are not concurrency-safe

Inventory balance mutation reads a balance, checks availability, and writes a
new total without a row lock or conditional update
(`internal/services/inventory_domain_service.go:1635-1674,2037-2051`).
Concurrent stock movements can oversell or lose updates.

Reservation service methods appear transactional but call a repository backed
by the root database handle
(`internal/services/inventory_service.go:92-105`;
`internal/repositories/postgres/inventory_repo.go:59-60`), so reservation
writes escape the caller's transaction and can survive rollback or make
reserved stock negative.

Remediation: perform all balance/reservation reads and writes through the same
transaction, lock the exact balance row or use a guarded atomic update, and
enforce non-negative/quantity invariants in the database.

### H-7 — GST retries ignore backoff and replay terminal provider actions

The GST service records `NextAttemptAt`
(`internal/services/tax_compliance_execution.go:821-826`) and immediately sends
the replacement message at `:836`; the SQS dispatch at `:449-468` sets no
delay. `ProcessJobByID` at `:407-415` does not reject early, terminal, duplicate,
or already-processing deliveries before invoking the external provider.

Impact: duplicate/redelivered messages can repeat tax-provider side effects,
and retries occur immediately instead of respecting the saved schedule.

Remediation: atomically claim only due, retryable states; include an external
idempotency key; set SQS delay or schedule delivery; reject terminal/stale
messages; persist provider response before acknowledging.

### H-8 — Recurring invoice automation has no deployed producer or consumer

`DispatchDueInvoiceSubscriptions`
(`internal/services/billing_ops_service.go:1502-1547`) only inserts a queued run
and advances schedule dates. It does not create an invoice or enqueue work, has
no call site, and no EventBridge/scheduler resource exists. The invoice worker
supports only PDF message types (`internal/workers/worker.go:123-160`), not
subscription runs.

Impact: recurring schedules advance only if manually dispatched, and queued
runs never generate invoices. Manual “generate now” is a separate working path.

Remediation: add a deployed scheduler, transactional due-run claim, stable
logical idempotency key, and a worker that creates the invoice before advancing
the schedule.

### H-9 — Document, journal, and payment projections can partially commit

Document creation can commit stock and ledger effects before later compliance
work fails (`internal/services/document_service.go:208` onward), and concurrent
conversion can consume the same source quantity more than once (`:800`
onward). Journal posting/reversal performs check-then-write transitions without
locking, allowing duplicate ledger effects
(`internal/services/journal_service.go:161` onward).

Remediation: define one authoritative transaction boundary for each business
operation, lock source/state rows, make transition predicates atomic, and move
external work behind an outbox with idempotent consumers.

### H-10 — Creating a stocked product passes an empty required warehouse ID

Product and variant creation send opening-stock mutations with
`WarehouseID: ""`
(`internal/services/product_service.go:155-184,778-788`). The inventory
mutation persists that value (`inventory_domain_service.go:1412-1438`), while
`inventory_balances.warehouse_id` is a non-null UUID foreign key
(`migrations/000028_inventory_parity.up.sql:188-196`).

Impact: creating a non-service product with opening stock fails and rolls back;
tests miss the real database constraint.

Remediation: require/resolve a valid default warehouse before opening stock,
validate tenant ownership, and add a database-backed creation test.

## Medium-severity correctness and maintainability findings

### M-1 — A2A streaming can precommit an incorrect `200` response

`cmd/lambda/a2a-stream/main.go:408-418` waits only 150 ms, snapshots response
metadata, and posts it before the router necessarily writes headers/status.
Unset status defaults to 200 at `:231-251`; later authentication or handler
errors cannot change the already-posted metadata.

Remediation: commit metadata only when the handler first writes headers/status,
or block until those values are known.

### M-2 — Ledger payment rows ignore report filters

Customer/vendor document rows receive date/project/party predicates
(`internal/repositories/postgres/reporting_repo.go:823-845`), but the payment
UNION at `:846-858` applies only business/deleted filters and always joins
customers. Project ledger payments at `:776-788` similarly omit project/date
constraints. Totals and running balances therefore include unrelated payments.

Remediation: construct one typed filter set and apply it consistently to every
UNION arm, with separate customer/vendor joins.

### M-3 — Invoice/document numbers use a one-million-value time suffix

Invoice and document numbers use `UnixNano()%1000000`
(`internal/services/invoice_service.go:271-275`;
`document_service.go:1355-1374`) under unique constraints, with no collision
retry. Collisions become avoidable request failures.

Remediation: use a database-backed per-business sequence or a collision-safe
identifier allocated in the insert transaction.

### M-4 — The rule-based NLP fallback does not implement its rules

The parser creates a pattern/category map that is never read
(`pkg/nlp/intent_parser.go:196-207`), returns no keywords or price range
(`:241-253`), and its `contains` helper returns true for any two non-empty
strings (`:275-278`). Every non-empty fallback input becomes urgent and then
falls through to wildcard product search
(`internal/services/product_matching_service.go:139-158`).

Remediation: implement real normalized matching/extraction and table-driven
tests for fallback behavior, including negative cases.

### M-5 — Required configuration defaults are applied after validation

`internal/config/config.go:424-432` validates before `setDefaults`. Validation
requires environment, AWS region, and positive JWT expiries
(`internal/config/validation.go:11-13,41-50`), while their defaults are assigned
later (`config.go:536-539,574-585`). Those defaults cannot rescue omitted
values.

Remediation: apply defaults before validation and distinguish truly required
settings from documented defaults.

### M-6 — Deployment configuration checks fail open and applies are cancelable

The workflow's `require` helper never increments `missing`; any single
configured backend value or credential sets `configured=1`, so incomplete
configuration enables deployment
(`.github/workflows/deploy.yml:192-234`). Workflow-wide
`cancel-in-progress: true` (`:18-20`) can terminate an active Terraform apply
when a new run starts on the same ref.

Remediation: require every backend value plus one complete authentication mode,
fail closed, and use a non-cancelable concurrency group for apply.

### M-7 — Published API specifications materially diverge

The runtime-generated Swagger document describes 226 paths/296 operations;
`docs/openapi.yaml` describes 15/17 and `openapi/openapi.yaml` 42/60. CI has no
generation/diff gate. Generated metadata also applies `/api/v1` to root routes
and marks the public well-known route as authenticated
(`internal/app/runtime.go:294-301`;
`internal/handlers/well_known_handler.go:31`; `docs/docs.go`).

Remediation: choose one generated source of truth, correct root/public security
annotations, and fail CI when committed output differs.

## Test and tooling gaps

- CI runs `go test ./...` but omits the Makefile's `-race` mode, tagged
  integration tests, and `terraform test`.
- Payment tests codify status `partial` and do not use the real database or
  concurrent payments.
- Inventory tests do not exercise concurrent `ReserveStock` or transaction
  rollback through the production repository.
- S3 tests assert the obsolete literal bucket names.
- The sole Terraform test is Razorpay-specific and does not cover Lambda
  VPC/SSM role attachments, bucket wiring, migrations, or WebSocket schemes.
- Twenty-eight test files define copied `*Testable` handler/service
  implementations (234 methods). Those tests can pass while production code
  diverges; WebSocket tests already miss the confirmed production failures.
- Dependabot points Terraform at `/infrastructure` instead of
  `/infrastructure/terraform` and has no pnpm entry for the custom SMS Lambda
  (`.github/dependabot.yml:49-50`).

## Security findings

The static security review closed 72 validated candidates across 38 threat
surfaces. Attack-path calibration retained 53 findings (1 Critical, 3 High,
28 Medium, 21 Low), suppressed 13 defeated or duplicate paths, and deferred 6
paths that require deployment, policy, model-behavior, or downstream-consumer
evidence. The scan is partial only for those six explicit open questions; none
is silently treated as safe.

### Critical

- **Entitlement sync grants every plan all features and unlimited quotas.**
  The subscription is loaded and discarded; reads and feature checks then
  persist all-enabled entitlements and `-1` quota sentinels. Drive storage
  translates that sentinel into no aggregate limit. The introducing commit and
  current test suggest possible deliberate rollout policy, but the paid catalog
  and live gating code contradict that interpretation and require owner
  confirmation.

### High

- **Pull-request verification exposes production secrets to
  branch-controlled code.** Workflow-level production variables reach
  same-repository pull-request tests and Make targets before merge.
- **Category discovery dumps raw private agents from every tenant.** A global
  match-all query serializes private/inactive agent configuration and unscoped
  product records without target identifiers.
- **Unrelated tenants can reserve marketplace seller inventory.** The A2A
  seller-negotiation dispatcher drops the authenticated principal and can
  create repeatable orphan reservations against another merchant's stock.

### Medium inventory

- Cloud/developer boundary: tracked agent-tool cloud-command approvals;
  tracked secret-bearing Terraform plan archives.
- Agent/A2A boundary: bargaining counterparty impersonation; private agent
  discovery; agent CRUD permission bypass; cross-user order tracking;
  arbitrary agent capability disclosure; forged merchant cart approval; buyer
  budget bypass; reservation ownership bypass.
- Tenant/role boundary: render-profile password disclosure and management;
  shipment management; vendor financial records; customer master data; billing
  administration; warehouse stock ACL bypass; cross-business global role
  claims.
- Realtime/edge boundary: cross-tenant WebSocket injection; global WebSocket
  broadcast; spoofable forwarding-header rate-limit bypass; report-share
  passcode brute force.
- State-machine boundary: bargaining idempotency race; pre-OTP registration
  denial; fake cryptographic signature state; recurring-invoice replay; coupon
  cap race; unbounded presigned upload; payment-deletion ledger desync.

### Low inventory

- Cross-tenant negotiation agents; autonomous-negotiation identity spoofing;
  session and negotiation progress disclosure; stale JWT business claim reuse.
- Agent registry hijacking; duplicate ratings; global capability deletion;
  barcode permission bypass; unauthorized LLM decisions and summaries.
- WebSocket presence enumeration and token leakage to Sentry; unenforced
  storefront block policy.
- Negative-inventory, reservation-overconsumption, and duplicate-journal races;
  cross-tenant category-relation disclosure; CSV formula injection; global GST
  idempotency collision; bounded A2A response resource exhaustion.

The six deferred candidates concern voice/MCP model-to-tool authority, shipping
price policy, downstream use of single-use AP2 tokens, intended paid-feature
policy, finite GST quota configuration, and voice/MCP indirect tool invocation.
They need environment or product evidence before a security rating.

## Recommended remediation order

1. Rotate and purge secrets from Terraform plan history.
2. Isolate CI verification from production secrets/OIDC and protect deploy.
3. Close cross-tenant/object and permission gaps at shared middleware/service
   boundaries.
4. Repair production migrations and WebSocket/voice IAM before the next deploy.
5. Make payment, inventory, document, journal, checkout, GST, and recurring-job
   transitions atomic and idempotent.
6. Correct S3 and WebSocket endpoint wiring.
7. Add database-backed concurrency/invariant tests and Terraform policy tests.
8. Reconcile documentation, dependency automation, and copied test doubles.

## Limitations

No runtime server, database, AWS account, queue, Terraform plan/apply,
migration, exploit, or dynamic authorization test was used. Security findings
are therefore validated from exact source/control/sink traces, configuration,
infrastructure definitions, and existing tests. No external dependency
advisory scanner was run.
