# Task 3 Report: Operational visibility and recovery

Status: complete (implementation owned end-to-end; prior partial implementation inspected, verified, corrected, and completed)

Repository head at completion: `99591a2` (`main`)

## 1. Scope and ownership

Task 3 delivers a secure aggregate operational read model, separately authorized
operator detail, domain-proven safe recovery commands, fail-closed step-up, and
metrics/alarms backed by truthful producer emission, with exact OPS contracts.

Commits `d63b501`, `a8dad0a`, `2ceb835`, and `fa9f8ef` (a partial implementation
left by a previous agent) were inspected line by line against the brief, not
trusted. My verification and corrections are in commit `99591a2`. Findings:

### Verified as correct (prior agent work, retained)

- Aggregate projection over focused adapters per domain owner
  (`internal/repositories/postgres/operation_repository.go`): render jobs,
  email delivery (split into `invoice_delivery` / `email_delivery`), outbox,
  Razorpay webhooks, GST e-invoice/e-waybill, recurring runs, WhatsApp +
  notifications, imports. `voice_reconciliation` is reported in
  `unavailable_types` because voice sessions live in DynamoDB with no coherent
  durable PostgreSQL aggregate source; it is never presented as success.
- `unknown`, `reconciliation_required`, and `failed` remain distinct everywhere
  (`normalizeOperationStatus`, `applyStoredOperationStatusFilter`); no collapse.
- Stable, bounded, tenant-scoped pagination with a filter-hash-bound opaque
  cursor and a `SnapshotAt` watermark; per-adapter composite cursors match the
  global `(updated_at DESC, type ASC, id ASC)` order.
- Business projection exposes only sanitized fields; safe-correlation UUID and
  error codes are sanitized (`safeOperationCode`, `safeStoredOperationCode`);
  no provider references, queue/message IDs, payloads, accounts, or topology.
- Business render retry (`RecoverBusinessOperation`): permission
  (`documents.manage`) plus live capability re-evaluation (invoice queue,
  invoice bucket, SQS/S3 clients re-read per command, fail-closed on missing
  configuration), domain-proven (exact failed revision, invoice/version/kind,
  issued facts, deterministic object key), idempotent (idempotency key +
  request hash + operation revision), race-safe (row lock + unique indexes +
  unique-violation resolution), transactionally audited with exactly one
  compatible outbox event.
- Operator boundary (`middleware.RequirePlatformOperator`): accepts only the
  separately configured verified `cognito:groups` value; owner/admin/business
  roles never satisfy it; missing configuration fails closed with
  `operator_not_configured`. No live Cognito changes were made.
- Operator high-risk recovery is fail-closed: `reconcile`, `reprocess_webhook`,
  `redrive_dead_letter`, `resolve` return `428 step_up_required` until Task 5
  delivers the scoped one-time step-up token; every operator decision is
  durably audited (`operation_recovery_commands`) with subject, operation,
  action, tenant/resource, reason, idempotency/correlation identity, and safe
  result code — never raw provider or queue payloads.
- Paired expand-first migration `000056_operation_recovery` (up/down),
  preserving existing data; audit-only table with effect and idempotency
  unique indexes.
- Generated Swagger, `openapi/openapi.yaml`, `docs/openapi.yaml`, and the
  frontend handoff (OPS-007/OPS-008) match runtime behavior; `make swagger`
  produces zero drift at the final head.

### Confirmed defects and gaps found, corrected in `99591a2`

1. **False readiness claim (critical):** all eight `Billeif/Operations` alarms
   referenced metrics with **no producer anywhere in the codebase**. Every
   alarm was structurally unable to fire.
2. **Fabricated DLQ metric:** `DLQGrowth` is unobservable from application
   code (SQS redrive is consumer-invisible). The alarm was removed; DLQ growth
   remains covered by the pre-existing `worker_dlq_messages` alarms on native
   `ApproximateNumberOfMessagesVisible` (already asserted in
   `tests/outbox.tftest.hcl`).
3. **Stale Terraform mock manifest checksum:** all eight `mock_provider`
   blocks hardcoded a migration manifest checksum (`7885c669…`) that no longer
   matched `migrations/manifest.sha256` (`7b89918b…`), so every mocked
   Terraform test failed its migration postcondition and downstream alarm
   assertions were skipped. All mocks now use the real checksum.
4. **Branding allowlist drift:** `tests/aws_branding_policy_test.go` approved
   resource list still contained the removed alarm; updated.

## 2. Metric and alarm backing (final state)

| Alarm | Metric (namespace `Billeif/Operations`) | Truthful producer |
| --- | --- | --- |
| `operations_repeated_failures` | `RepeatedFailures` (category `recovery`) | `cmd/lambda/sqs-invoice`, `cmd/lambda/sqs-email-delivery`, and `cmd/lambda/sqs-gst` consumers emit 1 per failed record that SQS has already redelivered (`ApproximateReceiveCount >= 2`); GST records emit only the recovery sample, never an invented render or delivery rate |
| `operations_reconciliation_backlog` | `ReconciliationBacklog` (category `reconciliation`, Maximum) | `cmd/lambda/subscription-reconciler` periodically counts `razorpay_webhook_events.processing_status='reconciliation_required'` plus `gst_submission_jobs.status='needs_attention'` via `OperationRepository.CountReconciliationBacklog` with the bounded run context (telemetry-only aggregate; no tenant detail leaves the database); fully fail-open — counter, cancellation, and encode failures never change the maintenance outcome |
| `operations_provider_latency` | `ProviderLatencyMilliseconds` (category `provider`, p99) | `configuredGSTProvider.doJSON` around the complete provider round trip — request, response headers, body read, and decode, including error returns — forwarded through the lazy provider to every resolved configuration |
| `operations_webhook_failures` | `WebhookFailures` (category `webhook`) | `RazorpayPaymentService.HandleWebhook` on signature rejection, payload rejection, durable transaction failure, replay mismatch, and apply-failure reconciliation; ordinary successful duplicates are never counted |
| `operations_recurring_schedule_failures` | `RecurringScheduleFailures` (category `schedule`) | `cmd/lambda/recurring-invoices` dispatcher result |
| `operations_render_failure_rate` | `RenderFailureRate` 0/100 (category `render`) | `cmd/lambda/sqs-invoice` consumer per processed record |
| `operations_delivery_failure_rate` | `DeliveryFailureRate` 0/100 (category `delivery`) | `cmd/lambda/sqs-email-delivery` consumer per processed record |
| Queue age | `QueueAgeSeconds` emitted per record from `SentTimestamp`; alarm remains the pre-existing native `worker_queue_age` set on `ApproximateAgeOfOldestMessage` | SQS native + consumer emission |
| DLQ growth | **No application metric** (unobservable) | Pre-existing native `worker_dlq_messages` alarms on `ApproximateNumberOfMessagesVisible` |

Error-type alarms use `treat_missing_data = "notBreaching"`; latency uses
`extended_statistic = "p99"`; all use 3 evaluation periods with
`datapoints_to_alarm = 2` (M-of-N). All producers are low-cardinality
(dimensions `Environment` + bounded `Category` only), fail-open (telemetry
errors never fail records or runs), and never fabricate values: missing
attributes emit nothing, and nil emitters disable emission.

## 3. RED → GREEN evidence (exact)

Every behavior change was written test-first.

1. **Emitter surface + SQS derivation + per-record emission**
   (`pkg/operationsmetrics`):
   - RED: `go test ./pkg/operationsmetrics/` → build failed,
     `undefined: SQSSentTimestampAttribute / SQSQueueAgeSeconds /
     SQSReceiveCount / SQSReceiveCountAttribute`; additionally new assertions
     required `Emit` to reject category `queue` and metric `DLQGrowth`
     (fabrication guard) and defined `EmitRecordOutcome` behavior.
   - GREEN: `ok invoice-backend/pkg/operationsmetrics 0.837s`. New tests:
     `TestSQSAttributeDerivationIsBoundedAndOptional`,
     `TestEmitRecordOutcomeEmitsPipelineOutcomeAndRepeatedFailures`,
     `TestEmitRecordOutcomeSuccessEmitsOneSampleWithoutRepeatedFailures`,
     `TestEmitRecordOutcomeRejectsMismatchedPipelineAndNilEmitter`,
     `TestNewRuntimeEmitterFailsOpenWithoutSafeEnvironment`,
     `TestEmitterQueueAgeSecondsUsesEmitterClock`.
2. **Webhook failure metric** (`internal/services/razorpay_payment_service.go`):
   - RED: `svc.WithOperationsMetrics undefined (type *RazorpayPaymentService…)`
     (build failure = RED).
   - GREEN: `go test ./internal/services/` full package `ok 1.215s`. New tests:
     `TestRazorpayWebhookFailuresEmitBoundedWebhookMetric`,
     `TestRazorpayWebhookReconciliationEmitsBoundedWebhookMetric`.
3. **Provider latency metric** (`internal/services/gst_provider.go`):
   - RED: same build failure for the missing emitter seam.
   - GREEN (same run): `TestConfiguredGSTProviderEmitsBoundedProviderLatency`,
     `TestConfiguredGSTProviderEmitsLatencyForFailedProviderCalls`,
     `TestConfiguredGSTProviderWithoutEmitterStillSucceeds`.
4. **Reconciliation backlog counter**
   (`internal/repositories/postgres/operation_repository.go` + interface):
   - RED: `NewOperationRepository(database).CountReconciliationBacklog
     undefined` (build failure = RED).
   - GREEN: `ok invoice-backend/internal/repositories/postgres 0.510s`. New
     test: `TestOperationRepositoryCountsReconciliationBacklogAcrossAllTenants`
     (counts exactly 2 Razorpay `reconciliation_required` + 1 GST
     `needs_attention` = 3). The count is exposed on a narrow
     `OperationBacklogCounter` interface so business reads never depend on it.
5. **Consumer metrics (render, delivery, GST queue age)**:
   - RED: `go vet`/`go test` build failures — `too many arguments in call to
     processSQSEvent`, `undefined: emitGSTRecordMetrics`, `unknown field
     metrics in struct literal of type emailDeliveryHandler`.
   - GREEN: `ok` for `cmd/lambda/sqs-invoice`, `cmd/lambda/sqs-email-delivery`,
     `cmd/lambda/sqs-gst`. New tests: `TestProcessSQSEventEmitsBoundedRenderOutcomeMetrics`
     (2 render samples + 1 repeated-failure sample),
     `TestProcessSQSEventNilEmitterKeepsProcessing`,
     `TestHandleEmitsBoundedDeliveryOutcomeMetrics`,
     `TestHandleNilEmitterKeepsDeliveryBehavior`,
     `TestEmitGSTRecordMetricsEmitsBoundedQueueAge`,
     `TestEmitGSTRecordMetricsSkipsUnobservableRecords`.
6. **Recurring schedule failures metric** (`cmd/lambda/recurring-invoices`):
   - RED: `TestHandlerEmitsRecurringScheduleFailuresMetric` failing against
     single-sample behavior; existing single-JSON assertion also updated
     (line-split) as part of the intended contract change.
   - GREEN: `ok invoice-backend/cmd/lambda/recurring-invoices 0.620s`.
7. **Reconciliation backlog emission** (`cmd/lambda/subscription-reconciler`):
   - RED: `unknown field backlog in struct literal of type lambdaHandler`
     (build failure = RED).
   - GREEN: `ok invoice-backend/cmd/lambda/subscription-reconciler 0.658s`.
     New tests: `TestHandlerEmitsReconciliationBacklogMetric`,
     `TestHandlerNilBacklogCounterKeepsMaintenanceBehavior`.
8. **Runtime wiring** (`internal/app/runtime.go`):
   - RED: `undefined: initOperationsMetricsEmitter` and
     `invoiceRT.Metrics undefined` (build failures = RED).
   - GREEN: `ok invoice-backend/internal/app 1.002s`. New test:
     `TestInitOperationsMetricsEmitterFailsOpen`.
9. **Terraform contracts**:
   - RED (pre-fix, clean HEAD): `terraform test -filter=tests/outbox.tftest.hcl`
     → `run "outbox_dispatcher_is_private_serial_and_fail_closed"... fail`,
     `Failure! 0 passed, 1 failed, 13 skipped` (stale mock manifest checksum;
     re-verified identical on stashed HEAD before fixing).
   - GREEN: `Success! 14 passed, 0 failed` for outbox.tftest.hcl, including
     `aggregate_operations_alarms_are_low_cardinality_and_fail_safe`, whose
     DLQ-coverage assertion now requires the five native
     `worker_dlq_messages` alarms on `ApproximateNumberOfMessagesVisible`.

## 3b. Review-correction wave (parent review round 2)

The parent review found five defects; all were fixed test-first and verified.

1. **Backlog telemetry could fail the maintenance run and ignored Lambda
   cancellation** (`cmd/lambda/subscription-reconciler/main.go`): the backlog
   gauge now receives the bounded run context (`runCtx`) instead of
   `context.Background()` and is fully fail-open — nil counter, unusable
   environment, cancellation, count failures, and encode failures are all
   swallowed so the maintenance outcome and its retry behavior never change.
   The pre-existing lifecycle metric error behavior is preserved.
   RED: `TestHandlerCancellationReachesBacklogCounter`,
   `TestHandlerFailingBacklogCounterKeepsMaintenanceOutcome`,
   `TestHandlerBacklogEncodeFailureKeepsMaintenanceOutcome` all failed
   (cancellation never reached the counter; failures surfaced from `Handle`).
   GREEN: `ok invoice-backend/cmd/lambda/subscription-reconciler 0.698s`.
2. **Webhook database transaction failures were not counted**
   (`internal/services/razorpay_payment_service.go`): a valid signed webhook
   whose durable transaction fails now emits exactly one fail-open
   `WebhookFailures` sample before the error surfaces. Ordinary successful
   duplicates remain uncounted.
   RED: `TestRazorpayWebhookTransactionFailureEmitsWebhookFailureMetric`
   (deterministic `no such table` transaction failure emitted nothing).
   GREEN: passes; `TestRazorpayWebhookSuccessfulDuplicateEmitsNoFailureMetric`
   proves duplicates stay uncounted.
3. **Provider latency excluded response-body read and decode time**
   (`internal/services/gst_provider.go`): the emission point moved to a
   deferred closure that measures the complete round trip — request, response
   headers, body read, and decode — on every return path, success or error.
   RED: `TestConfiguredGSTProviderLatencyIncludesResponseBodyReadAndDecode`
   measured 0.000083 ms against a 60 ms delayed response body. GREEN: latency
   covers the delayed body (>= 40 ms asserted).
4. **Failed redelivered GST records emitted no recovery sample**
   (`cmd/lambda/sqs-gst/main.go`): the metrics helper now receives the real
   processing outcome and emits one `RepeatedFailures` recovery sample via the
   new bounded `Emitter.EmitRepeatedFailure()` — without inventing a render or
   delivery rate, which GST records never emit.
   RED: `TestEmitGSTRecordMetricsCoversFailureOutcomesWithoutInventingRates`
   plus `TestEmitRepeatedFailureIsBoundedAndFailSafe` (build failure = RED).
   GREEN: success (queue age only), first failure (no recovery sample), and
   redelivered failure (exactly one recovery sample) all asserted.
5. **Razorpay operations stayed timestamped at their old processing time after
   a replay-to-reconciliation transition**
   (`internal/repositories/postgres/operation_repository.go`): the safe latest
   lifecycle expression
   `CASE WHEN COALESCE(last_replayed_at, received_at) > COALESCE(processed_at,
   received_at) THEN COALESCE(last_replayed_at, received_at) ELSE
   COALESCE(processed_at, received_at) END` (portable across PostgreSQL and
   the SQLite fixtures) now drives snapshot filtering, cursoring, and ordering,
   with a Go mirror (`latestRazorpayLifecycleTime`) for the projection and the
   direct-read snapshot, which previously used the host wall clock. The urgent
   operation is surfaced, filtered, and ordered at the replay time.
   RED: `TestOperationRepositorySurfacesRazorpayReplayReconciliationAtReplayTime`
   (UpdatedAt stayed at the old processing time). GREEN: UpdatedAt equals the
   replay time; a pre-replay snapshot surfaces nothing; the post-replay list
   orders the webhook operation first; the cursor after the replay time
   excludes it.

## 4. Validation (final gate, all on commit `99591a2` and the review-correction wave)

- `make fmt` — clean.
- `make lint` — clean (exit 0).
- `make test` — **exit 0; 52 packages ok; zero failures** (includes race and
  coverage targets as configured).
- Focused race tests: `go test -race` over operationsmetrics, services,
  repositories/postgres, handlers, middleware, app, and all `cmd/lambda/...`
  — all ok.
- `go build ./cmd/lambda/sqs-invoice ./cmd/lambda/sqs-email-delivery
  ./cmd/lambda/sqs-gst ./cmd/lambda/recurring-invoices
  ./cmd/lambda/subscription-reconciler ./cmd/server ./cmd/lambda/http` — all
  changed commands build.
- `make test-integration` not run: requires local PostgreSQL/Redis
  dependencies not available in this environment (unchanged integration
  boundaries; recovery behavior covered by focused service/repository tests).
- `make swagger` — regenerated; zero drift in `docs/docs.go`,
  `docs/openapi.yaml`, `openapi/openapi.yaml` (no API contract change).
- `terraform fmt -check` clean; `terraform validate` success; `terraform test`
  full suite — **85 passed, 0 failed**.
- `git diff --check` — clean. Changed-line secret scan — clean (only prose
  mentions and pre-existing secret-ARN indirections; no values).

### Review-correction wave gate (round 2)

- Focused suites for every touched package: `ok` for operationsmetrics,
  services, repositories/postgres, app, handlers, middleware, all
  `cmd/lambda/...`, and migrations, plus `-race` on services,
  repositories/postgres, operationsmetrics, subscription-reconciler, and
  sqs-gst.
- `make fmt` clean; `make lint` clean; `make test` exit 0 with 52 packages ok
  and zero failures; `terraform fmt -check` and `terraform validate` clean
  (no Terraform files changed in the round, so the mocked Terraform tests were
  not re-run).
- Migrations package tests (`migrations/embedded_test.go`,
  `manifest.sha256` validation, `operation_recovery_schema_test.go`) pass as
  part of `make test`.

## 5. Contracts

- Generated Swagger, both OpenAPI files, and the frontend handoff
  (`docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`, OPS-007/OPS-008)
  describe exactly the runtime surface: business list/get/timeline/recovery,
  operator detail/timeline/recovery with mandatory original `business_id`,
  operator-only tags, `step_up_required` marked for high-risk operator
  commands, and `unavailable_types` semantics. No contract drift after the
  metric work (`make swagger` clean).
- Statuses `queued`, `in_progress`, `succeeded`, `failed`,
  `reconciliation_required`, `unknown` are distinct in projection, filters,
  and timeline. `unknown` and `reconciliation_required` never collapse into
  `failed`.

## 6. Migrations

- `000056_operation_recovery.up.sql` / `.down.sql` — paired, expand-first,
  reversible, preserves existing data. Adds the durable audit table with
  idempotency uniqueness `(business_id, actor_subject, action,
  idempotency_key)`, effect uniqueness on accepted commands
  `(business_id, operation_type, operation_id, action, operation_version)
  WHERE result_code = 'accepted'`, and a timeline index. Down migration drops
  only the new objects.
- No other schema changes were made in this task.

## 7. Security and authorization review

- Platform operator access depends only on the verified `cognito:groups`
  claim value matching the separately configured `PLATFORM_OPERATOR_GROUP`;
  the group arrives exclusively from JWT verification (set at
  `auth.go:331`), never from body/query input. Missing configuration fails
  closed; owner/admin roles are explicitly rejected by test.
- High-risk operator recovery (`reconcile`, `reprocess_webhook`,
  `redrive_dead_letter`, `resolve`) fails closed with `428 step_up_required`
  and is durably audited even when rejected; no provider call, message send,
  or redrive occurs. Safe business render retry keeps permission
  (`documents.manage`), capability re-evaluation, idempotency, revision
  binding, and transactional audit.
- Business reads are tenant-scoped in every adapter; the only cross-tenant
  surface is the aggregate reconciliation backlog count used for telemetry,
  which returns a bare count with no tenant/provider/payload detail and is
  never exposed through business or operator reads.
- EMF payloads carry only `Environment`, `Category`, metric names and values;
  tests assert no business, provider, queue, payload, credential, or error
  fields leak.

## 8. External gaps and unverified behavior

- No live AWS, Razorpay, GST provider, SES, Cognito, or database writes were
  performed; no Terraform apply; no message or redrive action.
- `voice_reconciliation` remains an explicitly unavailable aggregate type
  (DynamoDB-owned state). It is never silently presented as succeeded.
- Alarm thresholds (repeated failures ≥ 5/5min, reconciliation backlog > 10,
  provider p99 > 2000ms, failure rates > 10%) are launch defaults chosen from
  the existing monitoring conventions; they require staging traffic (Task 4)
  for empirical tuning. Missing data is notBreaching for all error metrics.
- SES feedback-driven bounces/complaints update delivery states and surface
  in the projection; they are not double-counted into `DeliveryFailureRate`,
  which is emitted per send attempt from the durable queue consumer.
- Local `cmd/server` worker-mode render processing does not emit the consumer
  metrics (production emission lives in the SQS Lambda boundaries, matching
  the existing outbox/recurring EMF pattern).

## 9. Commits

- `1322e9e` — fix: harden operation telemetry and razorpay ordering
  (parent-review round 2: fail-open backlog telemetry with bounded context,
  webhook transaction-failure metric, complete provider round-trip latency,
  GST repeated-failure recovery sample, Razorpay replay-time lifecycle
  ordering; all with RED→GREEN tests)
- `99591a2` — ops: back operations alarms with truthful producers
  (metric producers at boundaries, fabricated-alarm removal, stale Terraform
  mock checksums, branding allowlist, all with RED→GREEN tests)
- `d63b501`, `a8dad0a`, `2ceb835`, `fa9f8ef` — inherited partial
  implementation (verified, retained)

## 10. Concerns for review

1. `RenderFailureRate`/`DeliveryFailureRate` use 0/100 per-record samples
   aggregated with the `Average` statistic. This is a deliberate low-cardinality
   pattern; per-batch weighting follows message counts, not job counts.
2. The reconciler backlog count is a cross-tenant `COUNT` on indexed columns
   (`idx_razorpay_webhook_events_processing`, `idx_gst_submission_jobs_status`);
   acceptable for a scheduled Lambda, but worth revisiting if either table
   grows into the tens of millions.
3. `RepeatedFailures` fires on the third delivery attempt of an SQS message
   (receive count ≥ 2 at failure) — an early DLQ-growth precursor. The
   threshold (5 per 5 minutes) is a default, not SLA-derived.
4. Operator recovery remains a fail-closed audit boundary by design until
   Task 5 ships the scoped one-time step-up verifier; the
   `FailClosedOperationStepUpVerifier` default must be replaced, not bypassed.
5. The outbox adapter exposes outbox events as operations for
   observability; it does not and must not become a retry command surface
   (no DLQ redrive exists at the application layer).