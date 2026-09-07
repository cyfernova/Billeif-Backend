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

## Fix round 1

### Scope and implementation

This correction round started from reviewed baseline
`9569767c4c1931a49abae9cbac91adc9f609cba2` and resolves every round-one
finding.

1. Scheduled plan changes now require the charged or reconciled provider period
   to start exactly at `pending_plan_effective_at`. Every renewal after a prior
   paid count also requires a stored prior period end and an exact monotonic
   start at that boundary. A mismatch remains reconciliation-required without
   changing plan, quotas, paid count, periods, or billing history.
2. Restarting an expired or cancelled renewable lifecycle resets the aggregate
   to a clean free/pending baseline before creating the new provider
   subscription. Prior plan/quota fields, provider identities, periods,
   provider event clock, paid count, grace, cancellation, pending plan, and
   reconciliation state do not carry forward. Historical billing/audit rows are
   preserved.
3. A provider-confirmed immediate cancellation now persists terminal
   subscription state, command completion, and a sanitized verification audit
   in one transaction. A command or audit write failure rolls that transaction
   back and never converts the verified cancellation path to
   `reconciliation_required`.
4. Same-payload inbox replay reconstructs the prior safe domain outcome from
   durable processing status. Reconciliation-required deliveries continue to
   return the handler's `202` outcome on replay; rejected deliveries remain
   rejected rather than becoming `200`.
5. Webhook processing validates invariant subscription, tenant, and provider
   mode identity first, performs event ordering next, and validates the
   state-dependent current/pending plan only for non-stale events. Delayed
   previous-plan events are ignored without poisoning current state.
6. Maintenance uses one total record budget across reconciliation and grace
   work. The Lambda supplies a 50-second invocation context, the service uses a
   45-second run context, and every provider fetch has a 3-second child context.
   The contexts are synchronous and cancelled directly, so no timeout goroutine
   is leaked. The schedule plus the 50-record total budget is the provider fetch
   rate ceiling.
7. Successful checkout commands persist the exact safe checkout response fields
   and replay them without reading the current aggregate or fetching the
   provider. Post-activation replay therefore remains the original
   `pending_payment` authorization response and exposes no provider IDs.
8. Billing/audit history strictly accepts an absent limit as `50` or an integer
   from `1` through `100`. Malformed and out-of-range values return `400
   subscription_invalid_limit`; repository failures return a typed sanitized
   `500 subscription_internal_error`. Handler annotations, generated Swagger,
   both OpenAPI files, runtime Swagger tests, and the frontend handoff now agree
   on limits and statuses.
9. Provider errors preserving a `4xx` HTTP status are deterministic rejections.
   Checkout, plan change, and cancellation restore the pre-command lifecycle and
   atomically mark the command `rejected`, returning `422
   subscription_provider_rejected`. Transport failures, malformed successful
   responses, and other unknown outcomes remain reconciliation-required. No raw
   provider body/error is stored, logged, or returned.

The immutable checkout response required four nullable columns on the
unreleased `000055_subscription_lifecycle.up.sql` command table:
`response_status`, `response_billing_mode`, `response_pending_plan_id`, and
`response_authorization_url`. The paired down migration already drops the
command table, so it needed no change. The manifest and all mocked Terraform
migration checksums were synchronized. No migration was applied or rolled back
against a live database.

### Strict RED/GREEN evidence

Each command below was run RED before the owning implementation and GREEN after
the minimal correction.

1. Exact plan-change boundary

   - Command: `go test ./internal/services -run TestScheduledPlanChangeRequiresExactMonotonicProviderBoundary -count=1`
   - RED: `Expected error ... but got nil`; the early provider period changed
     the plan.
   - GREEN: `ok invoice-backend/internal/services`.

2. Clean terminal restart

   - Command: `go test ./internal/services -run TestRestartAfterTerminalSubscriptionResetsLifecycleBeforeProviderCharge -count=1`
   - RED: `expected: "free"`, `actual: "biz"`.
   - GREEN: `ok invoice-backend/internal/services`.

3. Atomic terminal cancellation

   - Command: `go test ./internal/services -run 'TestImmediateProviderCancellation(CompletionFailureRollsBackWithoutReconciliation|CommitsTerminalStateCommandAndAuditTogether)' -count=1`
   - RED: the forced command completion failure produced
     `reconciliation_required` instead of the transaction's prior
     `cancellation_scheduled` state, and the successful path had no
     `provider_cancellation_verified` audit.
   - GREEN: `ok invoice-backend/internal/services`.

4. Durable duplicate outcome

   - Command: `go test ./internal/services -run TestSubscriptionWebhookSamePayloadReplayPreservesReconciliationOutcome -count=1`
   - RED: the second delivery returned no domain error.
   - GREEN: `ok invoice-backend/internal/services`.

5. Stale event before current-plan validation

   - Command: `go test ./internal/services -run TestDelayedPreviousPlanEventIsIgnoredBeforeCurrentPlanValidation -count=1`
   - RED: delayed previous-plan event returned reconciliation-required.
   - GREEN: `ok invoice-backend/internal/services` with
     `stale_event_ignored` and unchanged active plan.

6. One maintenance budget and explicit deadlines

   - Command: `go test ./internal/services -run TestSubscriptionMaintenanceUsesOneTotalRunBudgetAcrossReconciliationAndGrace -count=1`
   - RED: expected one suspension, actual two, for three total operations under
     limit two.
   - GREEN: `ok invoice-backend/internal/services`.
   - Command: `go test ./internal/services -run TestSubscriptionMaintenanceBoundsEachProviderFetchByDeadline -count=1`
   - RED: `unknown field MaintenanceRunTimeout` and `unknown field
     ProviderFetchTimeout`.
   - GREEN: blocking provider boundary was cancelled within the test budget and
     returned `Failed: 1`.
   - Command: `go test ./cmd/lambda/subscription-reconciler -run TestHandlerRunsBoundedMaintenanceAndEmitsMetrics -count=1`
   - RED: `unknown field runTimeout`.
   - GREEN: `ok invoice-backend/cmd/lambda/subscription-reconciler`; runner
     observed a context deadline and limit 50.

7. Immutable checkout replay

   - Command: `go test ./internal/services -run TestCheckoutIdempotencyReplayReturnsImmutableOriginalResponseAfterActivation -count=1`
   - RED: expected pending/original authorization response; actual was the
     active aggregate with changed URL.
   - GREEN: exact response equality, one create, and zero replay fetches passed.

8. Strict history runtime and documentation contracts

   - Command: `go test ./internal/handlers -run 'TestSubscriptionHistory(RejectsMalformedAndOutOfRangeLimits|MapsInternalFailuresToSanitizedServerError)' -count=1`
   - RED: invalid limits returned `200`; repository failures returned `400`.
   - GREEN: both endpoints return stable `400`/`500` outcomes without raw
     database detail.
   - Command: `go test ./internal/handlers -run TestSubscriptionProviderRejectionMapsToStableUnprocessableResponse -count=1`
   - RED: expected `422`, actual `400`.
   - GREEN: stable `subscription_provider_rejected` response passed.
   - Command: `go test ./internal/app -run TestSwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses -count=1`
   - RED: `/subscriptions/checkout response 422 is undocumented`.
   - GREEN after `make swagger`: runtime Swagger includes mutation
     `200/400/409/422/503`, history `200/400/500/503`, and limit `1..100`
     default `50`.

9. Deterministic rejection versus ambiguous provider outcome

   - Command: `go test ./internal/services -run 'TestDeterministicProvider(CheckoutRejectionRestoresPriorLifecycle|PlanChangeRejectionRestoresActivePlan|CancellationRejectionRestoresActiveAccess)' -count=1`
   - RED: `undefined: ErrSubscriptionProviderRejected` for all mutation paths.
   - GREEN: `ok invoice-backend/internal/services`; all three restore prior
     state and avoid reconciliation.
   - Control command:
     `go test ./internal/services -run TestSubscriptionProviderTimeoutIsRecordedForReconciliationAndNeverBlindlyRetried -count=1`
   - GREEN: transport timeout remains reconciliation-required and create count
     remains one.

10. Reconciliation and self-review boundary completeness

    - Command: `go test ./internal/services -run TestSubscriptionMaintenanceDoesNotApplyPendingPlanBeforeVerifiedBoundary -count=1`
    - RED: maintenance returned success and applied the early pending plan.
    - GREEN: result is `Failed: 1`; incumbent plan/quota/period/count remain
      unchanged in reconciliation.
    - Command: `go test ./internal/services -run TestRenewalChargeRequiresStoredPriorPeriodBoundary -count=1`
    - RED: a paid-count renewal with no stored prior period returned success.
    - GREEN: `provider_period_not_monotonic`, no billing row, no paid-count or
      period mutation.

### Files and contracts

Material implementation changes:

- `internal/services/subscription_lifecycle_service.go`
- `internal/services/subscription_lifecycle_test.go`
- `internal/models/subscription.go`
- `internal/handlers/subscription_handler.go`
- `internal/handlers/subscription_handler_test.go`
- `cmd/lambda/subscription-reconciler/main.go`
- `cmd/lambda/subscription-reconciler/main_test.go`
- `migrations/000055_subscription_lifecycle.up.sql`
- `migrations/subscription_lifecycle_schema_test.go`
- `migrations/manifest.sha256`
- mocked migration results under `infrastructure/terraform/tests/`

Contract changes:

- `docs/docs.go` regenerated from handler annotations
- `docs/openapi.yaml`
- `openapi/openapi.yaml`
- `internal/app/runtime_swagger_test.go`
- `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`

Public provider/customer/subscription/plan/payment/invoice identifiers remain
hidden. Billing amounts remain exact integer minor units. Every command, event,
audit, billing, and maintenance query remains business and provider-mode scoped.

### Final validation

- Focused race command across amended packages passed:
  `go test -race ./internal/services ./internal/handlers ./cmd/lambda/subscription-reconciler ./pkg/razorpay -run 'Test(Subscription|StartRenewable|RestartAfterTerminal|CheckoutIdempotency|SignedSubscription|ConcurrentFirstDelivery|ConcurrentPlanChange|ScheduledPlanChange|ImmediateProviderCancellation|DelayedPreviousPlan|EqualTimestamp|DeterministicProvider|HandlerRunsBoundedMaintenance|ClientHTTPError)' -count=1`.
- `make fmt`: passed.
- `make lint`: passed with `golangci-lint run --timeout=5m`.
- `make test`: passed with race detection and coverage across `./...`.
- `make migration-manifest-verify`: every pair through `000055` passed and the
  embedded bundle test passed.
- `make swagger`: passed with only the existing non-fatal repository-root
  package-name warning.
- `jq empty docs/swagger.json`: passed.
- Ruby safe YAML parsing passed for `docs/swagger.yaml`, `docs/openapi.yaml`,
  and `openapi/openapi.yaml`.
- `make build-lambda-subscription-reconciler`: passed for Linux ARM64.
- `terraform fmt -check -recursive`: passed.
- `terraform validate`: passed; only the pre-existing DynamoDB
  `hash_key/range_key` deprecation warnings remain.
- `terraform test -filter=tests/migrator.tftest.hcl`: `6 passed, 0 failed`.
- `terraform test -filter=tests/razorpay.tftest.hcl`: `20 passed, 0 failed`.
- `git diff --check`: passed.
- Final changed-file secret scan found no private key, cloud access key, raw
  credential, webhook body/signature, provider account ID, or embedded secret.

No live Razorpay request, payment/charge, AWS/provider write, Terraform apply,
message, production action, or live database migration was run.

### Commits

- `00b64b6 fix: preserve subscription lifecycle boundaries`
- `abc458d fix: harden subscription lifecycle recovery`
- `dffaaa6 docs: align subscription lifecycle outcomes`
- `580700c fix: require prior renewal boundary`
- `docs: record subscription fix round evidence` (this report commit; final
  handoff contains its hash)

### Self-review

The repository code-review skill was applied directly because this task
explicitly prohibited its normal standards/spec subagents. The standards axis
reviewed `9569767...HEAD` against repository `AGENTS.md` and the handler-service-
repository conventions. The spec axis reviewed the same diff against
`task-2-brief.md` and all nine round-one findings.

No remaining material standards or spec finding was identified after adding the
missing-prior-boundary correction. The review confirmed current membership and
business scope remain handler/middleware enforced; provider mode and tenant
identity remain in event/command/audit ownership; provider mutations remain
outside database transactions; local multi-record effects are transactional;
unknown outcomes are never retried as payment; deterministic rejections do not
enter reconciliation; and generated/handwritten contracts match runtime status
behavior. The carried Task 1 GST binding and sanitized tax error prerequisites
were untouched and remained green in the full suite.

### External gaps and concerns

- Live Razorpay subscription create/update/cancel, webhook delivery, and fetch
  behavior remain externally unverified because provider calls and charges were
  prohibited.
- Migration 55 remains unapplied to live PostgreSQL. It is unreleased,
  expand-first, reversible through its paired down migration, manifest-verified,
  and locally exercised only.
- Terraform validation/tests used local mocked providers only. No real-account
  plan or apply was run. Pre-existing DynamoDB deprecation warnings remain.
- A transport timeout before provider subscription identity is returned still
  requires operator/provider reconciliation and intentionally cannot be blindly
  retried.

## Fix round 2

### Finding and implementation

This scoped correction started from clean reviewed baseline
`74c5b9c4352b022205dc906ee9dd39aacd07a52b`. A deterministic Razorpay
rejection is known not to have applied at the provider, but checkout,
plan-change, and cancellation each need a local compensation transaction to
restore the prior aggregate and mark the command rejected. If that local
transaction failed, all three paths returned the history-read sentinel. The
shared handler consequently emitted the history-specific message
`subscription history could not be loaded` for a lifecycle mutation.

The three compensation-failure branches now return the distinct sanitized
`ErrSubscriptionMutationInternal` sentinel. The handler maps it to the exact
generic response below and never includes provider or database details:

```json
{"error":{"code":"subscription_mutation_internal_error","message":"subscription request could not be completed"}}
```

Billing and audit repository failures retain their prior, distinct safe
response:

```json
{"error":{"code":"subscription_internal_error","message":"subscription history could not be loaded"}}
```

Checkout, plan-change, and cancellation now document `500` in generated
Swagger. Both hand-authored OpenAPI files reference a dedicated
`SubscriptionMutationInternal` response with the exact code/message example,
and the frontend handoff distinguishes mutation recovery from history-read
failure. No raw provider response, raw database error, signature, credential,
provider identifier, or infrastructure detail is returned or newly logged.

### Strict RED/GREEN evidence

1. Service, handler, and generated Swagger behavior

   - RED command:

     `go test ./internal/services ./internal/handlers ./internal/app -run 'Test(DeterministicProviderRejectionCompensationFailuresReturnMutationInternalError|SubscriptionMutationInternalFailureMapsToStableGenericServerError|SubscriptionHistoryMapsInternalFailuresToSanitizedServerError|SwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses)' -count=1`

   - Expected RED output:

     ```text
     internal/handlers/subscription_handler_test.go:95:52: undefined: services.ErrSubscriptionMutationInternal
     internal/services/subscription_lifecycle_test.go:1042:27: undefined: ErrSubscriptionMutationInternal
     internal/services/subscription_lifecycle_test.go:1069:27: undefined: ErrSubscriptionMutationInternal
     internal/services/subscription_lifecycle_test.go:1094:27: undefined: ErrSubscriptionMutationInternal
     runtime_swagger_test.go:112: /subscriptions/checkout response 500 is undocumented
     FAIL
     ```

   - GREEN command:

     `make swagger && go test ./internal/services ./internal/handlers ./internal/app -run 'Test(DeterministicProviderRejectionCompensationFailuresReturnMutationInternalError|SubscriptionMutationInternalFailureMapsToStableGenericServerError|SubscriptionHistoryMapsInternalFailuresToSanitizedServerError|SwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses)' -count=1`

   - Relevant GREEN output:

     ```text
     ok invoice-backend/internal/services
     ok invoice-backend/internal/handlers
     ok invoice-backend/internal/app
     ```

   The service test forces the compensation transaction to fail after a mocked
   provider `4xx` rejection for each public mutation. Every path returns only
   the new sentinel, never the history sentinel or raw fixture detail. The
   handler tests assert the exact mutation and history HTTP `500` responses.

2. Hand-authored OpenAPI and frontend handoff contracts

   - RED command:

     `go test ./internal/app -run TestStaticSubscriptionContractsDocumentDistinctInternalFailures -count=1`

   - Expected RED output:

     ```text
     /subscriptions/checkout mutation 500 ref = ""
     /subscriptions/checkout mutation 500 ref = ""
     frontend handoff is missing exact response {"error":{"code":"subscription_mutation_internal_error","message":"subscription request could not be completed"}}
     FAIL
     ```

   - GREEN command:

     `go test ./internal/app -run TestStaticSubscriptionContractsDocumentDistinctInternalFailures -count=1`

   - GREEN output:

     ```text
     ok invoice-backend/internal/app
     ```

   The test parses `docs/openapi.yaml` and `openapi/openapi.yaml`, checks all
   three mutation `500` references, checks the exact mutation and history
   response examples, and checks both literal safe responses in the frontend
   handoff.

3. Final combined focused GREEN

   - Command:

     `go test ./internal/services ./internal/handlers ./internal/app -run 'Test(DeterministicProviderRejectionCompensationFailuresReturnMutationInternalError|SubscriptionMutationInternalFailureMapsToStableGenericServerError|SubscriptionHistoryMapsInternalFailuresToSanitizedServerError|SwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses|StaticSubscriptionContractsDocumentDistinctInternalFailures)' -count=1`

   - Output:

     ```text
     ok invoice-backend/internal/services 0.369s
     ok invoice-backend/internal/handlers 0.469s
     ok invoice-backend/internal/app 0.548s
     ```

### Files and public contracts

Implementation and focused tests:

- `internal/services/subscription_lifecycle_service.go`
- `internal/services/subscription_lifecycle_test.go`
- `internal/handlers/subscription_handler.go`
- `internal/handlers/subscription_handler_test.go`
- `internal/app/runtime_swagger_test.go`

Generated and hand-authored contracts:

- `docs/docs.go`
- `docs/openapi.yaml`
- `openapi/openapi.yaml`
- `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`

No model, repository, migration, migration manifest, provider adapter, worker,
or Terraform file changed. Tenant/business and provider-mode isolation, integer
minor-unit billing, signed webhook processing, and the carried Task 1 GST
prerequisites are untouched.

### Validation

- Focused amended-package race command passed:

  `go test -race ./internal/services ./internal/handlers ./internal/app -run 'Test(DeterministicProviderRejectionCompensationFailuresReturnMutationInternalError|SubscriptionMutationInternalFailureMapsToStableGenericServerError|SubscriptionHistoryMapsInternalFailuresToSanitizedServerError|SwaggerDocumentsExactSubscriptionMutationAndHistoryStatuses|StaticSubscriptionContractsDocumentDistinctInternalFailures)' -count=1`

  Output: services `1.712s`, handlers `1.735s`, app `2.019s`, all `ok`.
- `make fmt`: passed (`go fmt ./...` and repository `goimports`).
- `git diff --exit-code` immediately after formatting: passed; formatting was
  reproducible and did not alter the implementation commit.
- `make lint`: passed with `golangci-lint run --timeout=5m`.
- `make test`: passed with `go test -v -race -cover ./...`.
- `make swagger`: passed and regenerated the same tracked `docs/docs.go`. It
  emitted only the pre-existing non-fatal repository-root `no Go files`
  package-name warning.
- `jq empty docs/swagger.json`: passed.
- The first Ruby parse command used `YAML.safe_load_file`, which this host's
  Psych version does not provide, and failed with `NoMethodError`. The corrected
  non-mutating command
  `ruby -e 'require "yaml"; ARGV.each { |path| YAML.safe_load(File.read(path), permitted_classes: [], permitted_symbols: [], aliases: true); puts "parsed #{path}" }' docs/swagger.yaml docs/openapi.yaml openapi/openapi.yaml`
  parsed all three files successfully.
- `git diff --check 74c5b9c...HEAD`: passed.
- Final added-line secret scan reported `changed-line secret scan: clean` for
  private-key markers, cloud access keys, and credential assignments.
- Migration manifest verification and Terraform mocked tests were not rerun in
  this round because no migration, manifest, infrastructure, or Terraform file
  changed, as required by the scoped validation instruction.

No live Razorpay request, payment, charge, webhook, AWS/provider write,
Terraform apply, message, production action, or live database migration was
performed.

### Commits

- `45b3e8d fix: distinguish subscription mutation failures`
- `docs: record subscription fix round two evidence` (this report commit; final
  handoff contains its hash)

### Self-review

The repository code-review skill was applied directly because this task again
prohibited its normal standards/spec subagents. The fixed point resolves to
`74c5b9c4352b022205dc906ee9dd39aacd07a52b`; the reviewed implementation diff
contains the single commit `45b3e8d fix: distinguish subscription mutation
failures` before this evidence commit.

Standards axis: no finding. The change remains in the owning service and thin
HTTP mapper, uses branchable sentinels, propagates context through the existing
repository transaction, uses the external-provider fake only at the Razorpay
boundary, and exercises real observable SQLite repository behavior. No raw
error is wrapped into the public sentinel or response. Generated Swagger and
hand-authored contracts were updated together. No speculative abstraction,
cross-tenant data access, provider retry, schema change, or unrelated edit was
introduced.

Spec axis: no finding. Exactly the three reviewed compensation failures now
produce the mutation-specific safe `500`; history and audit keep their exact
history-specific safe `500`; generated Swagger includes mutation `500` for all
three routes; both OpenAPI files and the frontend handoff contain the exact
public code/message. Focused tests cover all requested paths and the full race
suite remains green.

### External gaps and concerns

- Live Razorpay behavior remains externally unverified because this correction
  deliberately made no provider call or charge. The change only classifies and
  safely exposes a local persistence failure after an already deterministic
  provider rejection.
- The existing non-fatal Swagger generator repository-root package-name warning
  remains unchanged.
- No new migration or infrastructure concern was introduced.
