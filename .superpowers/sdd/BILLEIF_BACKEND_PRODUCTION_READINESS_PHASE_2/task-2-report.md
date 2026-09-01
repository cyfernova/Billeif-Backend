# Task 2 report: Subscription and Razorpay lifecycle

## Status

Status: `DONE_WITH_CONCERNS`.

The renewable subscription lifecycle is complete and locally verified. The
remaining concerns are external verification limits required by the task: no
live Razorpay call, AWS write, Terraform apply, or live PostgreSQL migration was
performed. A provider-create timeout that returns no provider subscription ID is
kept in `reconciliation_required` for explicit operator resolution because there
is no safe provider identity to fetch and blind creation retries are forbidden.

## Binding rulings implemented

- New paid monthly checkouts create genuine Razorpay subscriptions. Provider
  requests use the configured Razorpay plan ID, quantity `1`,
  `customer_notify=false`, tenant/subscription/plan/mode notes, and
  `total_count=1200`, the documented maximum.
- Existing paid plan checkouts remain `legacy_one_time` with their original
  fixed period. Their new `next_renewal_at` field is explicitly `NULL`; they are
  never relabelled renewable or silently renewed.
- Plan changes use explicit no-proration and Razorpay
  `schedule_change_at=cycle_end`. The current plan and quota remain authoritative
  until a signed matching charged event verifies the provider plan and billing
  boundary.
- Cancellation uses `cancel_at_cycle_end=true`. Access continues through the
  verified period unless a verified provider terminal result says otherwise.
- The lifecycle persists and enforces pending payment, active, renewal pending,
  past due, grace period, cancellation scheduled, cancelled, expired, suspended,
  and reconciliation required states.
- Unknown provider outcomes are never treated as payment and are not blindly
  retried. Provider fetch reconciliation requires an already persisted provider
  subscription ID and verifies mode, tenant metadata, subscription identity,
  plan, paid-count advancement, and billing period.

The provider request/response fields and event names were checked against the
official Razorpay Subscriptions API and webhook documentation before the adapter
was implemented.

## Implementation

### Domain, persistence, and transactions

- Added a renewable subscription aggregate with internal provider customer,
  subscription, plan, and mode identifiers; current and next periods; grace and
  cancellation timing; pending plan; paid count; event clock; reconciliation
  code; and lifecycle version.
- Added tenant-scoped billing history, sanitized audit history, and durable
  idempotent subscription commands.
- Added a signed durable provider-event inbox keyed by provider mode and event
  identity. It stores only the payload hash, verification result, event type,
  provider/receipt timestamps, processing state, attempts, sanitized code,
  resolved tenant/subscription, and replay counters. Raw webhook bodies and
  signatures are neither stored nor logged.
- Webhook verification precedes business lookup and effects. Duplicate delivery
  returns the prior result. Concurrent first delivery is serialized with a
  bounded in-process lock stripe and a database `ON CONFLICT` insertion point,
  so cross-instance contenders wait for or replay the durable result.
- Stale and out-of-order events cannot regress state. Same-second charged events
  are allowed only when provider paid count advances; same-second non-terminal
  regressions remain stale, while terminal events remain authoritative.
- Charged-event subscription, billing record, audit record, quota-window
  projection, inbox status, and entitlement plan transition occur in one
  transaction under a subscription row lock.
- Plan-change and cancellation commands lock the tenant subscription and use
  actor-, tenant-, action-, and idempotency-key-scoped command records.
- Provider mode scopes event identity, provider lookup, worker reconciliation,
  and grace expiry. A test-mode worker cannot fetch or mutate live-mode rows.
- Immediate provider cancellation cannot report success when the local terminal
  transition fails; the aggregate and command become reconciliation-required.

### Entitlement, quota, and storage effects

- Paid entitlement begins only after a verified captured charge with exact
  integer minor-unit amount and currency.
- Active, renewal-pending, and cancellation-scheduled states retain access only
  inside the verified period. Past-due/grace access is bounded by the grace
  deadline. Suspended, cancelled, expired, and unpaid pending states resolve to
  free access.
- Renewable quota windows use the verified provider period start rather than
  calendar month. Legacy one-time purchases use their fixed purchase period.
- A verified renewal advances the quota window atomically without deleting old
  usage. An over-quota downgrade remains scheduled; existing usage and stored
  customer data are not deleted or rewritten.
- Storage entitlement continues to use the authoritative storage meter while
  the lifecycle supplies the current verified plan limit.

### Public routes and compatibility

Added protected, tenant-scoped SUB routes:

- `POST /api/v1/subscriptions/checkout`
- `POST /api/v1/subscriptions/plan-change`
- `POST /api/v1/subscriptions/cancellation`
- `GET /api/v1/subscriptions/billing-history`
- `GET /api/v1/subscriptions/audit`

Mutation routes require all-branch context and subscription-management
permission. History routes require all-branch context and subscription-view
permission. Provider identifiers are `json:"-"` and do not appear in public
responses.

The former direct subscription create/update routes are no longer registered.
New public plan order creation through the one-time Razorpay endpoint is
rejected with a stable redirect to subscription checkout. Existing successful
plan payment attempts can still complete and are recorded truthfully as
`legacy_one_time` for compatibility.

Generated Swagger, `docs/openapi.yaml`, `openapi/openapi.yaml`, and
`docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md` document the same plan
IDs, states, billing modes, request/response envelopes, policy, and frontend
flow.

### Reconciliation runtime and infrastructure

- Added `cmd/lambda/subscription-reconciler` with a fixed maximum of 50 records
  per invocation, request identity propagation, and sanitized EMF counts for
  reconciled, suspended, and failed work.
- Added an EventBridge Scheduler five-minute invocation, reserved concurrency
  of one while background processing is enabled, VPC attachment, database and
  Razorpay-secret-only IAM access, a scheduler DLQ, and Lambda/custom failure
  alarms.
- Added build and deterministic packaging targets plus a mocked artifact.
- Terraform mocked plans validate the worker, schedule, network dependency,
  bounded concurrency, exact secret access, DLQ, and alarms. No provider call or
  apply occurred.

### Carried Task 1 prerequisites

- GST readiness now binds to the exact selected integration account/service
  path. A validated account cannot unlock a different execution account.
- Generic tax-integration internal failures log a stable operational code and
  return a generic response. Raw database/provider errors are not logged or
  returned.

## Migration

Added paired unreleased migration:

- `migrations/000055_subscription_lifecycle.up.sql`
- `migrations/000055_subscription_lifecycle.down.sql`

The up migration is expand-first and backfills existing rows without deleting
them. It adds lifecycle/provider/period fields, composite-mode event identity,
billing, audit, command, reconciliation, and quota-window schema.

The down migration removes new surfaces and maps new statuses to the legacy
three-state contract. It restores the legacy global provider-event unique
constraint only when no test/live duplicate identity exists; otherwise it
preserves all event rows and completes rollback without destructive
deduplication.

The migration manifest contains 110 paired entries through version 55 and its
checksum is synchronized with every mocked migration invocation.

## Strict TDD evidence

The following focused commands were run RED before the owning behavior and
GREEN after the minimal implementation.

1. Carried GST account binding and tax error sanitization

   - Command: `go test ./internal/handlers ./internal/services -run 'TestTaxIntegrationHandlerMapsInternalFailuresToGeneric500WithoutLeak|TestCapabilityServiceBindsGSTReadinessToSelectedIntegrationAccount' -count=1`
   - RED: the account-bound observation field/logic was missing and the handler
     log still contained the raw internal error.
   - GREEN: both packages passed.

2. Official Razorpay subscription boundary and endpoint sanitization

   - Command: `go test ./pkg/razorpay -run 'TestClientSubscriptionLifecycleUsesOfficialRazorpayContracts|TestRazorpayEndpointClassNeverExposesProviderIdentifiers|TestClientHTTPErrorPreservesStatusWithoutRawProviderBody' -count=1`
   - RED: subscription request/response types and create/fetch/update/cancel
     methods were undefined; endpoint classification exposed identifiers.
   - GREEN: `ok invoice-backend/pkg/razorpay`.

3. Paired schema and legacy compatibility

   - Command: `go test ./migrations -run TestSubscriptionLifecycle -count=1`
   - RED: migration 55 and its lifecycle/inbox/billing/audit contracts were
     absent.
   - GREEN: `ok invoice-backend/migrations`.

4. Pending checkout without early entitlement

   - Command: `go test ./internal/services -run TestStartRenewableSubscriptionCreatesPendingProviderSubscriptionWithoutEarlyEntitlement -count=1`
   - RED: lifecycle constructor/input and `StartRenewable` were missing.
   - GREEN: pending checkout, provider notes, 1200-cycle cap, and hidden provider
     IDs passed.

5. Signed charged event and replay

   - Command: `go test ./internal/services -run TestSignedSubscriptionChargedActivatesOnceAndReplayReturnsPriorOutcome -count=1`
   - RED: `HandleWebhook` and durable lifecycle inbox behavior were missing.
   - GREEN: one entitlement transition, billing row, event row, and replay
     result passed.

6. No-proration plan-change/cancellation race

   - Command: `go test ./internal/services -run TestConcurrentPlanChangeAndCancellationSerializesAtNoProrationBoundary -count=1`
   - RED: scheduled plan change and cancellation methods were undefined.
   - GREEN: only the serialized scheduled change won and the higher entitlement
     remained unavailable before the charged boundary.

7. Invalid signature, test/live mismatch, wrong plan, stale/out-of-order, and
   cancellation/charge race

   - Command: `go test ./internal/services -run 'TestSubscriptionWebhookRejectsInvalidSignatureWrongModeAndWrongPlanWithoutEntitlement|TestSubscriptionWebhookStaleFailureCannotRegressAndCancellationChargeRaceReconciles' -count=1`
   - RED: unsafe events were not durably rejected or reconciled.
   - GREEN: all cases passed with stable codes and no unverified entitlement.

8. Missing webhook repair, failed renewal, grace expiry, and bounded worker

   - Command: `go test ./internal/services ./cmd/lambda/subscription-reconciler -run 'TestSubscriptionMaintenanceRepairsMissingChargeAndExpiresGraceWithoutDeletingData|TestFailedRenewalEntersPastDueWithBoundedGrace|TestHandlerRunsBoundedMaintenanceAndEmitsMetrics' -count=1`
   - RED: maintenance runner and worker handler were absent.
   - GREEN: trusted provider fetch repaired the missing charged observation,
     grace expired to suspended, and bounded metrics passed.

9. Unknown create outcome, second actor, and over-quota downgrade

   - Command: `go test ./internal/services -run 'TestSubscriptionProviderTimeoutIsRecordedForReconciliationAndNeverBlindlyRetried|TestPendingRenewableCheckoutRejectsSecondActorBeforeAnotherProviderMutation|TestOverQuotaDowngradeSchedulesWithoutDeletingUsage' -count=1`
   - RED: provider timeout retry/state and race policies were absent.
   - GREEN: create was called once, the second actor was rejected before another
     provider mutation, and downgrade preserved usage.

10. Terraform reconciliation schedule

    - Command: `terraform fmt -check -recursive`
    - RED: the new Terraform files required formatting.
    - GREEN: format check passed.
    - Command: `terraform test -filter=tests/razorpay.tftest.hcl`
    - First RED: generated subscription reconciler IAM role name exceeded the
      AWS length boundary.
    - GREEN after the bounded name: `Success! 20 passed, 0 failed.`

11. Runtime catalog plan-ID composition review test

    - Command: `go test ./internal/services -run TestSubscriptionProviderSettingsUsePublicCatalogPlanIDs -count=1`
    - RED: expected `pro_monthly/rise_monthly/biz_monthly`; runtime returned
      `pro/rise/biz` keys.
    - GREEN: `ok invoice-backend/internal/services`.

12. Immediate cancellation persistence failure review test

    - Command: `go test ./internal/services -run TestImmediateProviderCancellationPersistenceFailureRequiresReconciliation -count=1`
    - RED: the service returned success after a forced local cancellation write
      failure.
    - GREEN: it returned `ErrSubscriptionProviderUnknown` and stored
      reconciliation-required.

13. Provider-mode maintenance isolation review test

    - Command: `go test ./internal/services -run TestSubscriptionMaintenanceRepairsMissingChargeAndExpiresGraceWithoutDeletingData -count=1`
    - RED: a test-mode pass selected a live-mode reconciliation row and returned
      `subscription maintenance incomplete`.
    - GREEN: only test-mode rows were fetched/expired; live rows were unchanged.

14. Concurrent first provider-event delivery review test

    - Command: `go test ./internal/services -run TestConcurrentFirstDeliveryAppliesSubscriptionEventExactlyOnce -count=1`
    - RED: simultaneous inserts returned database lock/unique-race errors.
    - GREEN: eight concurrent deliveries produced one first result, seven
      duplicates, one billing record, and replay count seven.

15. Truthful legacy renewal and reversible event identity review tests

    - Command: `go test ./migrations -run TestSubscriptionLifecycleMigrationExpandsAndClassifiesLegacyRows -count=1`
    - RED: migration lacked the `next_renewal_at = NULL` legacy invariant.
    - GREEN: passed after the backfill correction.
    - Command: `go test ./migrations -run TestSubscriptionLifecycleDownMigrationRestoresLegacyCompatibility -count=1`
    - RED: rollback lacked a duplicate-identity guard.
    - GREEN: passed after conditional legacy unique-constraint restoration.

16. Same-second provider ordering review test

    - Command: `go test ./internal/services -run TestEqualTimestampChargedEventAdvancesPaidCountWithoutAllowingRegression -count=1`
    - RED: result code was `stale_event_ignored`.
    - GREEN: paid count advanced and the subscription became active while
      same-time regressive events remained stale.

17. Mock migration checksum diagnosis

    - Command: `go test -race ./tests -run TestRemovedInfrastructureIsAbsentFromBothMockedPlans -count=1`
    - RED: mocked Terraform migration result returned the prior manifest
      checksum.
    - GREEN after synchronizing deterministic mocks: `ok invoice-backend/tests`.

## Final validation

- `make fmt`: passed; the final tree is gofmt/goimports clean.
- `make lint`: passed with `golangci-lint run --timeout=5m`.
- `make test`: passed on the final implementation with race detection and
  coverage across `./...`.
- `make migration-manifest-verify`: all migrations `000001` through `000055`
  passed checksum verification; embedded bundle test passed.
- `make swagger`: passed and regenerated Swagger. The existing non-fatal root
  warning says the repository root contains no Go files.
- `jq empty docs/swagger.json`: passed.
- Ruby safe YAML parsing of `docs/swagger.yaml`, `docs/openapi.yaml`, and
  `openapi/openapi.yaml`: passed.
- `make build-lambda-subscription-reconciler`: produced the stripped Linux ARM64
  worker binary successfully.
- `terraform fmt -check -recursive`: passed.
- `terraform validate`: passed. It reports only pre-existing DynamoDB
  `hash_key/range_key` deprecation warnings.
- `terraform test -filter=tests/migrator.tftest.hcl`: `6 passed, 0 failed`.
- `terraform test -filter=tests/razorpay.tftest.hcl`: `20 passed, 0 failed`.
- `git diff --check` and final staged/working-tree checks: passed.
- Changed-file scan found no private key, cloud access key, raw credential, or
  newly embedded secret value. Dynamic secret references and mock-only fixture
  strings remain non-secret.

## Files and contracts

### Added

- `cmd/lambda/subscription-reconciler/main.go`
- `cmd/lambda/subscription-reconciler/main_test.go`
- `infrastructure/terraform/subscription_reconciliation.tf`
- `internal/repositories/interfaces/subscription_lifecycle_repository.go`
- `internal/repositories/postgres/subscription_lifecycle_repo.go`
- `internal/services/subscription_lifecycle_service.go`
- `internal/services/subscription_lifecycle_test.go`
- `migrations/000055_subscription_lifecycle.up.sql`
- `migrations/000055_subscription_lifecycle.down.sql`
- `migrations/subscription_lifecycle_schema_test.go`

### Materially updated

- Runtime/config/composition: `internal/app/runtime.go`,
  `internal/config/{config,runtime,validation}.go`,
  `internal/services/container.go`, `internal/handlers/handler.go`.
- Provider/payment compatibility: `pkg/razorpay/{client,webhook}.go`,
  `internal/services/razorpay_payment_service.go`,
  `internal/handlers/razorpay_payment_handler.go`, payment models/tests.
- Subscription projection: `internal/models/subscription.go`,
  `internal/services/{subscription_catalog,entitlements}.go`, tests.
- Task 1 prerequisites: capability/GST execution and tax integration handler
  files/tests.
- Infrastructure/build: `Makefile`, Terraform Lambda/network files, mocked plan
  fixtures and tests.
- Contracts: `docs/docs.go`, `docs/openapi.yaml`, `openapi/openapi.yaml`, and
  `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`.
- Migration bundle: `migrations/identities.txt`, `migrations/manifest.sha256`,
  embedded/deployment safety tests.

## Commits

- `7cc382c fix: bind GST readiness and sanitize integration failures`
- `3e85239 feat: add renewable subscription lifecycle`
- `3d35b3e infra: schedule subscription reconciliation`
- `1ae098e docs: define subscription lifecycle contracts`
- `8c4a51b fix: serialize subscription lifecycle races`
- `7f5db3f fix: close subscription lifecycle review gaps`
- `00b16e3 fix: order same-second subscription events safely`
- `1ce52bf test: align mocked migration checksums`
- `500c48a fix: preserve event rows during lifecycle rollback`
- `docs: record subscription lifecycle validation` (this report commit; the
  parent handoff contains its hash)

## Self-review

Standards review against repository `AGENTS.md` found no remaining hard
violations. The implementation keeps handler/service/repository/PostgreSQL
ownership, propagates context, uses typed branchable errors, scopes all public
reads and mutations to authenticated business membership, and keeps external
provider calls behind the provider boundary.

Spec review against `task-2-brief.md` confirmed every named scenario has focused
coverage: duplicate/replay, concurrent first delivery, stale and out-of-order,
invalid signature, missing webhook repair, wrong plan, mode mismatch,
concurrent plan-change/cancellation, over-quota downgrade, grace expiry, failed
renewal, same-second ordering, provider timeout, and rollback compatibility.

The review also confirmed:

- integer minor-unit comparison is exact;
- no raw webhook body, signature, credential, provider/database error, or
  provider identifier enters logs or public contracts;
- provider notes, event keys, command keys, audit, and queue/schedule ownership
  retain tenant identity;
- provider mutations happen outside local database transactions, while every
  local multi-record lifecycle effect is atomic;
- worker selection is bounded and provider-mode scoped;
- legacy data is preserved and truthfully classified;
- no frontend source file, unrelated Markdown, live database, cloud resource,
  provider account, or production system was changed.

The code-review skill normally delegates independent standards and spec axes,
but task instructions explicitly prohibited subagents. Both axes were therefore
performed directly against `dbeaf67...HEAD`.

## External gaps and concerns

- Live Razorpay subscription creation, update, cancellation, signature delivery,
  and reconciliation fetch remain externally unverified because live provider
  calls and charges were prohibited.
- Migration 55 was not applied to or rolled back against a live PostgreSQL
  database. SQL contracts, manifest integrity, repository behavior, and local
  database tests passed; deployment remains an operator-controlled step.
- Terraform was formatted, validated, and tested only with mocked providers. No
  plan against a real account, apply, AWS CLI write, schedule invocation,
  message, or production action occurred.
- A create timeout before a provider subscription ID is returned cannot be
  safely fetched by ID. It intentionally remains reconciliation-required for
  manual/provider-console resolution, and idempotent replay does not issue a
  second create.
