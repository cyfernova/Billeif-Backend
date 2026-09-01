# Task 1 report: Runtime capability model

## Status

Task 1 is complete locally. Provider health remains externally unverified, and
the separately authorized internal diagnostics endpoint is blocked because the
repository has no operator principal distinct from business owner/admin.

## Implementation

- Added `GET /api/v1/capabilities?platform=web|ios|android` inside the existing
  authentication and business-membership middleware boundary.
- Added a fixed, ordered capability inventory for Razorpay payments, GST,
  e-invoice, e-way bill, WhatsApp, email, S3 uploads, voice, AI, storefront
  payments, report exports, bulk imports, and saved payment methods.
- Added a reusable `CapabilityService.Evaluate` seam for later sensitive
  mutations and `CapabilityService.List` for the customer endpoint.
- Kept product support, configuration presence, provider health and observation
  time, entitlement, quota, permission, business setup, platform support, and
  final state as separate response facts.
- Added all required stable states and bounded reason/setup/retry/degradation
  values. Unknown and stale provider health fail closed. Fresh degraded health
  can remain available with a customer-safe degradation object. Health never
  changes the separately evaluated entitlement fact.
- Added a tenant-and-capability-keyed, monotonic, concurrency-safe in-memory
  provider health cache with a five-minute default freshness window and a
  4096-observation default bound. No customer request performs provider I/O.
- Added a secret-safe configuration snapshot. Secret identifiers and credential
  values are reduced to booleans before entering the capability model. The
  simulated GST provider does not count as configured.
- Added non-reserving entitlement/quota inspection using the current
  subscription catalog and monthly quota usage. Existing reservation behavior
  remains authoritative for mutations.
- Added business-scoped setup reads over existing business, GST integration,
  WhatsApp, email, and storefront records. Only booleans leave the reader.
- Set saved payment methods to `unsupported`, web voice to
  `unsupported_platform`, and bulk imports to `unsupported` with
  `bulk_import_processor_unavailable` because Task 0 found no safe processor.
- Exposed the service and cache through the service container for later provider
  observers and governance work.
- Regenerated Swagger, updated the manually maintained OpenAPI document, added
  CAP-001 to the frontend handoff, and updated the Task 1 plan status/evidence.
- Added no migration. Configuration and business setup use existing state, and
  provider observations are deliberately ephemeral.

## Strict TDD evidence

Each production behavior began with a focused behavior test. Selected RED and
GREEN transcripts follow.

1. Secret-safe configuration snapshot

   - RED: `go test ./internal/config -run TestCapabilityConfigurationSnapshotReportsPresenceWithoutSensitiveValues -count=1`
   - Relevant RED: `undefined: CapabilityConfigurationSnapshot`
   - GREEN: `go test ./internal/config -run TestCapabilityConfigurationSnapshot -count=1`
   - Result: `ok invoice-backend/internal/config`

2. Tenant-isolated cached provider health and customer-safe degradation

   - RED: `go test ./internal/services -run TestCapabilityHealthCache -count=1`
   - Relevant RED: `undefined: NewCapabilityHealthCache`
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/services`

3. Non-reserving entitlement and quota inspection

   - RED: `go test ./internal/services -run TestEntitlementServiceInspectFeatureReportsCurrentQuotaWithoutReserving -count=1`
   - Relevant RED: `service.InspectFeature undefined`
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/services`

4. Authoritative evaluation, inventory, fail-closed states, and fact separation

   - RED: `go test ./internal/services -run TestCapabilityService -count=1`
   - Relevant RED: missing `NewCapabilityService` and capability types
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/services`

5. Tenant-scoped business setup reader

   - RED: `go test ./internal/services -run TestDBCapabilityBusinessSetupReaderIsTenantScopedAndSecretSafe -count=1`
   - Relevant RED: missing setup-reader constructor
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/services`

6. Handler scope, validation, and stable error codes

   - RED: `go test ./internal/handlers -run TestCapabilityHandler -count=1`
   - Relevant RED: missing `NewCapabilityHandler` and response types
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/handlers`

7. Protected route registration

   - RED: `go test ./internal/app -run TestCustomerCapabilityRoute -count=1`
   - Relevant RED: `customer capability route must be registered`
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/app`

8. Monotonic provider observations

   - RED: `go test ./internal/services -run TestCapabilityHealthCacheDoesNotReplaceNewerObservation -count=1`
   - Relevant RED: `expected healthy, actual unavailable`
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/services`

9. Simulated GST rejection

   - RED: `go test ./internal/config -run TestCapabilityConfigurationSnapshotRejectsSimulatedGSTProvider -count=1`
   - Relevant RED: expected `false`, received `true`
   - GREEN: same focused command
   - Result: `ok invoice-backend/internal/config`

10. Stable zero-valued quota JSON and saved-method permission fact

    - RED: `go test ./internal/services -run 'TestCapabilityQuotaSerializesZeroValuesExplicitly|TestCapabilityServiceReturnsStableProductPlatformEntitlementQuotaAndPermissionStates' -count=1`
    - Relevant RED: zero fields omitted; saved-method permission expected `true`
    - GREEN: same focused command
    - Result: `ok invoice-backend/internal/services`

11. Bounded cache eviction

    - RED: `go test ./internal/services -run TestCapabilityHealthCacheEvictsOldestObservationAtCapacity -count=1`
    - Relevant RED: `unknown field MaxEntries in struct literal of type CapabilityHealthCacheOptions`
    - GREEN: same focused command
    - Result: `ok invoice-backend/internal/services 0.899s`

## Final validation

- `go test -race -count=1 ./internal/services -run 'TestCapability|TestDBCapability|TestEntitlementServiceInspectFeature'`
  - `ok invoice-backend/internal/services 1.823s`
- `go test -count=1 ./internal/config ./internal/services ./internal/handlers ./internal/app`
  - all four packages passed
- `make swagger`
  - passed and regenerated `docs/docs.go`; the existing root `go list` warning
    reports that the repository root has no Go files and does not fail generation
- `make fmt`
  - passed (`go fmt ./...` and `goimports`)
- `make lint`
  - passed (`golangci-lint run --timeout=5m`)
- `ruby -e 'require "yaml"; YAML.load_file("docs/openapi.yaml"); puts "docs/openapi.yaml: valid"'`
  - `docs/openapi.yaml: valid`
- `git diff --check` and staged `git diff --cached --check`
  - passed with no whitespace errors
- Customer JSON tag scan for secret, credential, account ID, operator detail,
  topology, and raw-error fields
  - no matches
- Per campaign instruction, the full `make test` suite was not rerun; the
  controller owns the full campaign gate.

## Files

### Added

- `internal/config/capabilities.go`
- `internal/config/capabilities_test.go`
- `internal/handlers/capability_handler.go`
- `internal/handlers/capability_handler_test.go`
- `internal/services/capability_health_cache.go`
- `internal/services/capability_health_cache_test.go`
- `internal/services/capability_service.go`
- `internal/services/capability_service_test.go`
- `internal/services/capability_setup_reader.go`
- `internal/services/capability_setup_reader_test.go`

### Updated

- `internal/services/entitlements.go`
- `internal/services/entitlements_test.go`
- `internal/services/container.go`
- `internal/handlers/handler.go`
- `internal/app/runtime.go`
- `internal/app/runtime_routes_test.go`
- `docs/docs.go`
- `docs/openapi.yaml`
- `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`
- `docs/plans/BILLEIF_BACKEND_PRODUCTION_READINESS_PHASE_2.md`

## Commits

- `14b1bd3 feat: add runtime capability evaluation`
- `docs: record runtime capability evidence` (this report commit; hash is in the
  parent handoff because a commit cannot contain its own final hash)

## Self-review

- Confirmed the endpoint is under both authentication and business-access
  middleware and ignores caller-supplied business IDs in favor of the validated
  active business context.
- Confirmed all setup queries include `business_id` and existing soft-delete
  constraints, and tenant-cache keys contain both business and capability.
- Confirmed configuration presence does not imply health and cached health does
  not imply configuration, entitlement, permission, quota, or setup.
- Confirmed provider health observations carry explicit UTC observation time and
  staleness, older observations cannot replace newer ones, and cache growth is
  bounded.
- Confirmed unknown capabilities, missing observations, and stale observations
  fail closed with stable reason codes.
- Confirmed customer JSON cannot serialize the cache's operator detail and no
  credential, account, secret identifier, raw error, or topology field appears
  in the public schema.
- Confirmed public `401`/`403` OpenAPI responses reflect the shared middleware's
  legacy error envelope, while handler-owned errors use the stable nested error
  envelope documented in CAP-001.
- Confirmed there are no migrations, synchronous provider calls, new generic
  framework abstractions, or unrelated staged files.

## Concerns and follow-up

- The health cache is process-local. Lambda cold starts and parallel instances
  begin with unknown health and do not share observations. This is intentionally
  truthful and fail-closed, but a later task must attach bounded provider
  observers or choose a durable tenant-scoped snapshot if cross-instance
  continuity becomes required.
- No live provider was probed, so provider health is externally unverified.
- Internal diagnostics remain blocked until a genuine operator identity and
  policy distinct from business owner/admin exist. No weaker authorization was
  invented.
- The current legacy bulk-import paths use inconsistent per-type permissions.
  The capability therefore exposes no misleading aggregate permission and
  remains unsupported until Task 7 supplies a safe processor and authoritative
  permission model.

## Fix round 1

### Status

Reviewer findings are resolved locally in commit `3c98b91` (`fix: enforce
runtime capability boundaries`). CAP-001 is no longer advisory-only: the same
authoritative evaluation now rejects governed service mutations before effects.
Provider health remains externally unverified, and internal diagnostics remain
blocked on a distinct operator principal.

### Implementation

- Added `CapabilityService.Require` and `CapabilityUnavailableError`, a reusable
  service-layer preflight and typed customer-safe error. Evaluation failures and
  every unavailable state fail closed with stable capability, state, reason,
  setup-action, and retry fields. Existing transactional reservations remain
  authoritative and are not replaced by preflight.
- Enforced preflight at the actual service mutation/provider boundaries for:
  report export; Razorpay plan and storefront order creation; e-invoice generate
  and cancel; e-way generate, Part B update, and multi-vehicle commands; drive
  upload initiation; mobile voice session creation; business-scoped LLM chat
  and agent-assist; direct and queued bulk-import intake; and saved-payment
  add/default/delete/token mutations.
- Added no-effect tests at those boundaries. Rejected calls do not query or
  mutate repositories, reserve quota, enqueue work, encrypt credentials,
  allocate voice capacity, create drive assets, presign uploads, or call
  providers.
- Added a production `CapabilityHealthRecorder`. It classifies success, HTTP
  429, provider unavailable, and timeout outcomes into sanitized observations.
  Concrete Razorpay order, business LLM, and GST e-invoice/e-way execution paths
  record outcomes without adding provider fan-out to `GET /capabilities`.
  Observations remain tenant/capability scoped and monotonic.
- Removed raw LLM/Exa provider response bodies from returned errors. Handler
  capability errors expose only the stable safe projection.
- Made drive capability evaluation and upload enforcement use the same
  `EntitlementService.InspectDriveStorage` reader. Public quota values use MB;
  enforcement retains exact byte precision. Exact-limit and cross-business
  behavior is tested, and the read endpoint does not reserve storage.
- Deep-copied `RetryAt` on health-cache record and read. Added mutation-isolation
  and concurrent race coverage.
- Added stable transport mapping: `403` for setup/upgrade/permission, `422` for
  unsupported product/platform, `429` for quota, and `503` for unknown or
  temporary unavailability. Regenerated Swagger and updated manual OpenAPI,
  CAP-001 handoff, affected contract classifications, and Task 1 plan evidence.
- Added no migration. The shared health cache remains bounded and process-local.

### Strict TDD RED/GREEN evidence

Each production behavior below started with a minimal behavior test and an
expected failure caused by the missing behavior.

1. Reusable typed capability preflight

   - RED: `go test ./internal/services -run TestCapabilityServiceRequireRejectsUnsupportedCapabilityWithTypedError -count=1`
   - RED evidence: `service.Require undefined` and
     `undefined: CapabilityUnavailableError`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`

2. Report export mutation boundary

   - RED: `go test ./internal/services -run TestReportServiceExportRejectsUnavailableCapabilityBeforeQueryOrRun -count=1`
   - RED evidence: `WithCapabilityGuard undefined`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; repository query/run counts
     remained zero.

3. Razorpay and storefront payment order boundary

   - RED: `go test ./internal/services -run TestRazorpayCreateOrderRejectsUnavailablePaymentCapabilityBeforeDatabaseOrProvider -count=1`
   - RED evidence: `WithCapabilityGuard undefined`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; both plan and `store_order`
     selected the correct capability and reached neither database nor provider.

4. GST/e-invoice/e-way command boundaries

   - RED: `go test ./internal/services -run TestTaxComplianceCommandsRejectUnavailableCapabilitiesBeforeDatabaseOrQueue -count=1`
   - RED evidence: `WithCapabilityGuard undefined`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; all five commands rejected
     before database or queue effects.

5. Bulk-import intake boundaries

   - RED: `go test ./internal/services -run TestBulkImportIntakeRejectsUnsupportedCapabilityBeforeAnyEffect -count=1`
   - RED evidence: missing customer/billing capability setters
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; direct and queued intake
     reached neither permission checks nor repositories.

6. Saved-payment mutation boundaries

   - RED: `go test ./internal/services -run TestCredentialProviderAddPaymentMethodRejectsUnsupportedCapabilityBeforeEncryptionOrRepository -count=1`
   - RED evidence: missing capability setter and business-scoped request field
   - GREEN: same command
   - RED: `go test ./internal/services -run TestCredentialProviderOtherPaymentMutationsRejectUnsupportedCapabilityBeforeRepository -count=1`
   - RED evidence: existing mutation signatures had no business scope
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; add/default/delete/token
     operations were rejected before encryption or repository calls.

7. Voice admission boundary

   - RED: `go test ./internal/voice/session -run TestServiceCreateRejectsCapabilityGuardBeforeAdmission -count=1`
   - RED evidence: `unknown field CreateGuard in struct literal of type ServiceOptions`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/voice/session`; capacity store was not
     touched.

8. Business LLM execution boundaries

   - RED: `go test ./internal/services -run TestLLMServiceBusinessChatRejectsUnavailableAICapabilityBeforeProvider -count=1`
   - RED evidence: missing business-scoped method/setter
   - GREEN: same command
   - RED: `go test ./internal/services -run TestLLMServiceBusinessAgentAssistRejectsUnavailableAICapabilityBeforeProvider -count=1`
   - RED evidence: `ProcessAgentIntentForBusiness undefined`
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; provider call count remained
     zero for both entry points.

9. Drive upload guard and authoritative storage quota

   - RED: `go test ./internal/services -run TestCreateDriveUploadRejectsUnavailableCapabilityBeforeAssetOrPresign -count=1`
   - RED evidence: missing capability-controls setter
   - GREEN: same command
   - RED: `go test ./internal/services -run TestEntitlementServiceInspectDriveStorageUsesTenantScopedAssetUsageInMB -count=1`
   - RED evidence: quota unit/drive reader missing
   - GREEN: same command
   - RED: `go test ./internal/services -run TestCreateDriveUploadRejectsExactAuthoritativeStorageLimitBeforeAsset -count=1`
   - RED evidence: upload path ignored the shared authoritative reader
   - GREEN: same command
   - Result: `ok invoice-backend/internal/services`; below-limit, exact-limit,
     MB reporting, and cross-business isolation passed.

10. Production provider-outcome recording

    - RED: `go test ./internal/services -run TestCapabilityHealthRecorderClassifiesSanitizedProviderOutcomesPerTenant -count=1`
    - RED evidence: missing recorder/outcome types and constructor
    - GREEN: same command
    - RED: `go test ./internal/services -run TestLLMServiceBusinessChatRecordsTenantScopedProviderOutcome -count=1`
    - RED evidence: missing LLM recorder seam
    - GREEN: same command
    - RED: `go test ./internal/services -run TestRazorpayCreateOrderRecordsTenantScopedProviderSuccess -count=1`
    - RED evidence: missing Razorpay recorder seam
    - GREEN: same command
    - Result: `ok invoice-backend/internal/services`; success, 429 degradation,
      unavailable, timeout, sanitization, observation time, and tenant isolation
      passed. GST operation outcome hooks use the same recorder.

11. `RetryAt` ownership and concurrency

    - RED: `go test ./internal/services -run TestCapabilityHealthCacheRetryAtIsMutationIsolatedOnRecordAndRead -count=1`
    - RED evidence: mutating the caller's input changed the cached fact
    - GREEN: same command
    - Race verification:
      `go test -race ./internal/services -run 'TestCapabilityHealthCache(RetryAt|ConcurrentRecord)' -count=1`
    - Result: `ok invoice-backend/internal/services 1.827s`

12. Stable mutation HTTP contract

    - RED: `go test ./internal/handlers -run TestWriteSubscriptionControlErrorReturnsMachineReadableCapabilityPayload -count=1`
    - RED evidence: every state returned `handled=false`
    - GREEN: `go test ./internal/handlers -run TestWriteSubscriptionControlError -count=1`
    - Result: `ok invoice-backend/internal/handlers`

### Validation after fix round 1

- `go test ./internal/services ./internal/voice/session ./internal/handlers ./internal/app -count=1`
  - all four packages passed.
- `go test -race ./internal/services ./internal/voice/session ./internal/handlers ./internal/app -count=1`
  - `ok invoice-backend/internal/services 4.411s`
  - `ok invoice-backend/internal/voice/session 2.029s`
  - `ok invoice-backend/internal/handlers 2.263s`
  - `ok invoice-backend/internal/app 2.468s`
- `go test ./tests/unit -count=1`
  - `ok invoice-backend/tests/unit 0.618s`
- `go test ./cmd/... -run '^$' -count=1`
  - all command packages compiled successfully.
- `make swagger`
  - passed and regenerated tracked `docs/docs.go`; the existing non-fatal root
    package warning remains because the repository root has no Go files.
- `make fmt`
  - passed (`go fmt ./...` and `goimports`).
- `make lint`
  - passed (`golangci-lint run --timeout=5m`).
- `ruby -e 'require "yaml"; YAML.load_file("docs/openapi.yaml"); puts "docs/openapi.yaml: valid"'`
  - `docs/openapi.yaml: valid`.
- `git diff --check` and `git diff --cached --check`
  - passed.
- Diff secret scan found no bearer token, provider account token, or private-key
  material. Every changed path belongs to capability enforcement, its tests, or
  the required contract/evidence updates.
- Per campaign instruction, full `make test` was not rerun; the controller owns
  the campaign-wide gate.

### Files changed in fix round 1

- Capability core: `internal/services/capability_service.go`,
  `internal/services/capability_health_cache.go`, and new
  `internal/services/capability_health_recorder.go`, with adjacent tests.
- Service boundaries: report, Razorpay payment, tax compliance, commerce/drive,
  customer, billing ops, credential provider, LLM, service container, and voice
  session service, with adjacent behavior tests.
- Transport: governed handlers plus shared capability error mapping/tests.
- Shared quota: `internal/services/entitlements.go` and tests.
- Existing unit fixture: `tests/unit/customer_service_test.go` explicitly injects
  an allow guard so legacy import mechanics remain isolated while production
  intake remains fail-closed.
- Contracts/evidence: generated Swagger `docs/docs.go`, manual
  `docs/openapi.yaml`, frontend CAP handoff, and Phase 2 Task 1 plan evidence.

### Self-review and concerns

- Confirmed guarded service boundaries cannot be bypassed by calling a handler's
  service directly, and handlers also expose the stable typed error projection.
- Confirmed health-cache keys contain business plus capability, observations do
  not move backward in time, `RetryAt` pointers cannot be mutated through cache
  inputs/outputs, and race tests pass.
- Confirmed configuration presence never writes a healthy observation and a
  failed observation never revokes entitlement.
- Confirmed drive quota reads and upload enforcement share one tenant-filtered
  source. Capability reads do not reserve; upload rejection happens before
  asset creation and presigning.
- Confirmed raw LLM/Exa provider response bodies are no longer returned to
  customers and recorder details are classifications rather than raw errors.
- The cache remains process-local. Cold starts and concurrent Lambda instances
  begin unknown and can disagree until they observe outcomes. A durable shared
  cache was intentionally not introduced without a migration/operational need.
- There is no safe synchronous bootstrap probe for S3, voice, WhatsApp, or email.
  S3 presign success would not prove object delivery, and voice admission would
  not prove the downstream runtime/media path. These remain unknown until a
  future bounded asynchronous producer is implemented.
- Razorpay, LLM, and GST outcome producers are wired to real production paths,
  but no live provider was called; all provider health remains externally
  unverified.
- Internal diagnostics remain blocked until a true operator principal and
  policy distinct from business owner/admin exist. No weak endpoint was added.
