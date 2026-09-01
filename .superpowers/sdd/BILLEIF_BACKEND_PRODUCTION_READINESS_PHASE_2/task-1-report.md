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

## Fix round 2

### Status

Fix round 2 is complete locally in `8475c74` (`fix: bootstrap governed provider
capabilities`). The round closes the voice nil-guard bypass, cold-start health
bootstrap deadlock, provider HTTP-status loss, storefront/GST provider-health
identity mismatch, and report Swagger classification findings. No live provider
was called, so deployed-provider health remains externally unverified. Internal
diagnostics remain blocked pending a distinct operator principal.

### Implementation

- Voice creation now fails closed with `ErrCreateGuardUnavailable` whenever
  `ServiceOptions.CreateGuard` is omitted. `NewService` installs the deny guard,
  `Create` always invokes the guard before rollout or admission, the production
  container injects `CapabilityService.Require`, and unrelated voice tests inject
  an explicit allow guard.
- Added a production `CapabilityHealthObserver` with explicit target discovery,
  provider probers, retry/time/concurrency limits, recurring non-overlapping
  cycles, tenant-scoped recording, and process lifecycle start/stop hooks.
  HTTP runtime initialization starts it asynchronously; runtime shutdown cancels
  it before closing the database.
- Target discovery reads existing business and GST integration rows in rotating
  bounded batches. It emits global Razorpay/AI groups and tenant-specific GST
  groups only for configured capabilities. Configuration presence never writes
  a health fact. A later cycle discovers newly created tenants without requiring
  a customer capability request or a sensitive mutation.
- Observer defaults are at most 256 tenant/capability targets per cycle, four
  workers, five-second probe deadlines, two attempts, and a two-minute refresh.
  Cycles do not overlap. Typed HTTP responses are not retried; transient transport
  failures receive the bounded retry. Unsupported probes record nothing, so the
  capability remains truthfully unknown.
- Razorpay now has a bounded authenticated read-only `GET /orders?count=1`
  probe. LLM health uses an authenticated read-only model lookup derived from
  the configured OpenAI-compatible chat endpoint. GST uses the configured
  credential-validation endpoint with the tenant's encrypted integration
  credentials, without updating the account row.
- Added sanitized typed Razorpay and GST HTTP errors implementing
  `HTTPStatusCode()`. Neither adapter includes provider response bodies,
  credentials, account identifiers, or topology in returned errors. The common
  recorder now preserves real 429, timeout, unavailable, and success
  classifications and produces only stable customer-safe degradation codes.
- Storefront evaluation and concrete Razorpay outcomes now use the same shared
  `razorpay_payments` provider-health key. E-invoice and e-way-bill evaluation and
  operation outcomes similarly share `gst_provider`. Cache keys remain
  business-scoped, so a provider result cannot cross tenants.
- Moved capability-error annotations from unguarded report Query to guarded
  report Export and regenerated Swagger. A runtime Swagger behavior test checks
  `403`, `422`, `429`, and `503` on Export and rejects capability-error contracts
  on Query.
- Updated CAP-001 and the Task 1 plan evidence to document asynchronous
  bootstrap, shared provider-health identities, bounds, process-local behavior,
  and the exact unsupported-producer gaps. No migration was added.

### Strict RED/GREEN evidence

1. Voice constructor fail-closed behavior

   - RED: `go test ./internal/voice/session -run TestServiceCreateFailsClosedWithoutGuardBeforeAdmission -count=1`
   - RED output: build failed with `undefined: ErrCreateGuardUnavailable`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/voice/session`.
   - Broader GREEN: `go test ./internal/voice/session -count=1`.

2. Storefront provider-health identity

   - RED: `go test ./internal/services -run TestRazorpayStorefrontOrderRecordsSharedProviderHealthKey -count=1`
   - RED output: the successful storefront order left the shared Razorpay fact
     absent.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`.
   - The GREEN test also proves no write under the storefront feature key and no
     cross-business fact.

3. Sanitized Razorpay HTTP status

   - RED: `go test ./pkg/razorpay -run TestClientHTTPErrorPreservesStatusWithoutRawProviderBody -count=1`
   - RED output: the returned ordinary error did not implement
     `HTTPStatusCode()`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/pkg/razorpay`.
   - Adapter-to-recorder GREEN:
     `go test ./internal/services -run TestRazorpayAdapterStatusFeedsRateLimitHealthWithoutRawBody -count=1`.
   - Result: `ok invoice-backend/internal/services`; real adapter 429 became
     `degraded/provider_rate_limited` with retry time and no raw body.

4. Sanitized GST HTTP status and timeout

   - RED: `go test ./internal/services -run TestGSTAdapterHTTPStatusFeedsRateLimitHealthWithoutRawBody -count=1`
   - RED output: the GST adapter returned its raw provider message and no typed
     HTTP status.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`.
   - Timeout GREEN:
     `go test ./internal/services -run TestGSTAdapterTimeoutFeedsUnavailableHealth -count=1`.
   - Result: `ok invoice-backend/internal/services`; timeout became sanitized
     unavailable health with a bounded retry time.

5. Cold-start observer core

   - RED: `go test ./internal/services -run TestCapabilityHealthObserverMovesColdTenantsFromUnknownWithoutMutation -count=1`
   - RED output: observer targets, prober, options, and constructor were
     undefined.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`.
   - The test proves one global probe records independent healthy facts for two
     tenants while a third tenant remains absent.

6. Tenant/capability discovery

   - RED: `go test ./internal/services -run TestDBCapabilityProbeTargetSourceDiscoversConfiguredTenantsAndGSTAccounts -count=1`
   - RED output: `undefined: NewDBCapabilityProbeTargetSource`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`.
   - Follow-up RED:
     `go test ./internal/services -run TestDBCapabilityProbeTargetSourceRotatesBoundedTenantBatches -count=1`.
   - RED output: the second bounded batch repeated `biz-1` through `biz-3` and
     omitted `biz-4`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`; cursor rotation now
     gives every later tenant an asynchronous observation path.

7. Concrete Razorpay read-only probe

   - RED: `go test ./pkg/razorpay -run TestClientProbeUsesBoundedReadOnlyOrderList -count=1`
   - RED output: `client.Probe undefined`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/pkg/razorpay`.
   - Service seam RED:
     `go test ./internal/services -run TestRazorpayCapabilityProbeDoesNotCreateOrder -count=1`.
   - RED output: `service.ProbeCapability undefined`.
   - GREEN output: `ok invoice-backend/internal/services`; provider create count
     remained zero.

8. Concrete LLM read-only probe

   - RED: `go test ./internal/services -run TestLLMCapabilityProbeUsesReadOnlyModelLookup -count=1`
   - RED output: `service.ProbeCapability undefined`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`; request was an
     authenticated GET to the model endpoint, not chat execution.

9. Concrete GST tenant probe

   - RED: `go test ./internal/services -run TestGSTCapabilityProbeValidatesStoredCredentialsWithoutMutatingAccount -count=1`
   - RED output: `service.ProbeCapability undefined`.
   - GREEN: same focused command.
   - GREEN output: `ok invoice-backend/internal/services`; validation ran once
     and status, last validation time, and last error were unchanged.

10. Shared GST provider-health fact

    - RED: `go test ./internal/services -run TestGSTFeaturesShareTenantScopedProviderHealth -count=1`
    - RED output: `e_invoice should use the shared GST provider fact`.
    - GREEN: same focused command.
    - GREEN output: `ok invoice-backend/internal/services`; e-invoice and e-way
      bill used the shared fact and the other tenant stayed unknown.

11. Recurring discovery and observer lifecycle

    - RED: `go test ./internal/services -run TestCapabilityHealthObserverRunDiscoversNewTenantOnLaterCycle -count=1`
    - RED output: `observer.Run undefined`.
    - GREEN: same focused command.
    - GREEN output: `ok invoice-backend/internal/services`.
    - Container RED:
      `go test ./internal/services -run TestContainerStartsAndStopsCapabilityHealthObserver -count=1`.
    - RED output: missing container observer field and start/stop methods.
    - GREEN output: `ok invoice-backend/internal/services`.

12. Typed rate-limit retry bound

    - RED: `go test ./internal/services -run TestCapabilityHealthObserverDoesNotRetryTypedRateLimit -count=1`
    - RED output: expected one provider call, received two.
    - GREEN: same focused command.
    - GREEN output: `ok invoice-backend/internal/services`; typed 429 is recorded
      immediately as degraded and is not retried.
    - Bound/unsupported GREEN:
      `go test -race ./internal/services -run 'TestCapabilityHealthObserver(Bounds|DoesNotRetry|Run|Moves)' -count=1`.
    - Result: `ok invoice-backend/internal/services 1.924s`; maximum active probes
      stayed at or below two in the fixture and unsupported remained unknown.

13. Report Swagger ownership

    - RED: `go test ./internal/app -run TestSwaggerDocumentsCapabilityErrorsOnlyForGuardedReportExport -count=1`
    - RED output: `export response 403 is undocumented`.
    - GREEN: `make swagger && go test ./internal/app -run TestSwaggerDocumentsCapabilityErrorsOnlyForGuardedReportExport -count=1`.
    - GREEN output: Swagger generation completed with the existing non-fatal
      root-package warning; test result `ok invoice-backend/internal/app`.

### Validation

- `go test -race -count=1 ./internal/services ./internal/voice/session ./pkg/razorpay ./internal/app`
  - `ok invoice-backend/internal/services 4.223s`
  - `ok invoice-backend/internal/voice/session 1.818s`
  - `ok invoice-backend/pkg/razorpay 1.758s`
  - `ok invoice-backend/internal/app 2.252s`
- `go test -count=1 ./cmd/server ./cmd/lambda/http`
  - server compiled with no tests; HTTP Lambda passed.
- Final post-format focused packages:
  `go test -count=1 ./internal/services ./internal/voice/session ./pkg/razorpay ./internal/app ./cmd/server ./cmd/lambda/http`
  - all passed.
- `make swagger`
  - passed; regenerated `docs/docs.go`.
- `make fmt`
  - passed (`go fmt ./...` and `goimports`).
- `make lint`
  - passed (`golangci-lint run --timeout=5m`).
- `ruby -e 'require "yaml"; YAML.load_file("docs/openapi.yaml"); puts "docs/openapi.yaml: valid"'`
  - `docs/openapi.yaml: valid`.
- `git diff --check` and staged diff check
  - passed.
- Secret-marker scan found new fake credentials/account identifiers only in
  test fixtures that assert redaction. No credential, secret identifier, raw
  provider response, or topology was added to customer output or production
  constants.
- Per campaign instruction, the full `make test` suite was not rerun; the
  controller owns that gate.

### Files added in fix round 2

- `internal/services/capability_health_observer.go`
- `internal/services/capability_health_observer_test.go`
- `internal/services/container_capability_observer_test.go`
- `internal/services/gst_provider_health_test.go`
- `pkg/razorpay/client_test.go`

### Principal files updated in fix round 2

- Capability/cache/container/runtime: `internal/services/capability_service.go`,
  `internal/services/capability_health_recorder.go`,
  `internal/services/container.go`, and `internal/app/runtime.go`.
- Provider adapters/services: `pkg/razorpay/client.go`,
  `internal/services/razorpay_payment_service.go`,
  `internal/services/gst_provider.go`,
  `internal/services/tax_compliance_service.go`,
  `internal/services/tax_compliance_execution.go`, and
  `internal/services/llm_service.go`, with adjacent behavior tests.
- Voice: `internal/voice/session/service.go`, `store.go`, and explicit guard
  injection in session tests.
- Contract/evidence: `internal/handlers/report_handler.go`,
  `internal/app/runtime_swagger_test.go`, `docs/docs.go`, CAP-001 frontend
  handoff, and the Task 1 plan section.

### Commits

- `8475c74 fix: bootstrap governed provider capabilities`
- `docs: record capability bootstrap evidence` (report commit; final hash is
  returned to the parent because a commit cannot contain its own hash)

### Self-review and concerns

- Confirmed every observer cache write contains both business ID and the shared
  provider-health capability key; global provider calls share only the network
  outcome, never a cache key or customer identity.
- Confirmed observer discovery is bounded, rotates batches, does not execute in
  `GET /capabilities`, does not overlap cycles, and does not call a mutation
  provider method. GST validation reads/decrypts existing tenant credentials but
  never persists them or includes them in an error/log/customer result.
- Confirmed configuration and health remain separate: configured tenants begin
  unknown, only a concrete successful probe records healthy, and unsupported
  probes stay unknown. Failure never revokes entitlement or consumes quota.
- Confirmed Storefront/Razorpay and EInvoice/EWayBill/GST use deliberate shared
  provider facts without weakening their distinct feature entitlement,
  permission, setup, platform, or quota facts.
- Confirmed voice nil-guard behavior is a typed fail-closed error before store
  admission. Production and every test that creates sessions now make its guard
  policy explicit.
- Confirmed provider status types omit raw bodies and the real adapter-to-recorder
  tests cover 429 and timeout/unavailable classifications.
- The cache remains process-local and non-durable. Every instance starts unknown
  briefly and bootstraps independently, so concurrent instances may disagree
  until their observers run; observations are not claimed as cross-instance
  truth.
- LLM probing requires an OpenAI-compatible `/chat/completions` URL from which a
  read-only model endpoint can be derived. Other shapes stay unknown instead of
  guessing or executing a chat mutation.
- GST probing requires `GST_VALIDATE_PATH` and at least one tenant integration
  account. Providers without that safe validation path stay unknown with no
  fabricated healthy state.
- S3, voice, WhatsApp, and email still lack safe provider probes. Their existing
  operation paths or future bounded observers must supply observations before
  those provider-backed facts can become healthy.
- No live provider was called, no migration was added, and internal diagnostics
  remain blocked until a real operator authorization mechanism exists.

## Fix round 3

### Implementation

- Replaced the guessed LLM `GET /models/{model}` probe with the declared
  OpenAI-compatible/DeepSeek read-only model-list contract. Only exact
  `/chat/completions` and `/v1/chat/completions` configurations map to
  `GET /models` and `GET /v1/models`. The response is bounded to 1 MiB, scanned
  for at most 10,000 model entries, and never returned or logged. A present
  configured model records success; an absent configured model records a
  sanitized unavailable outcome. Unrecognized shapes and `404`/`405` record
  nothing and remain unknown. No probe sends a chat request.
- Added a tenant-target census to the rotating database source. It reports the
  exact configured Razorpay/AI target count, the distinct GST-account tenant
  count, and the worst-case business-page sweep count. Each observer fact
  declares a freshness window large enough for a complete serial rotation:
  `sweep cycles * (refresh interval + maximum cycle duration) + maximum cycle
  duration`.
- Made the production cycle budget explicit: at most 20 target groups per page,
  four workers, two five-second attempts, and a one-minute cycle deadline. Five
  worst-case waves take 50 seconds, leaving ten seconds for census, discovery,
  recording, and cancellation. Provider calls, database queries/pages, retries,
  concurrency, and cycle duration remain bounded.
- Aligned cache capacity with active census targets. The cache retains its
  ordinary 4096 outcome budget plus one reserved slot per active target
  high-water mark. Fresh scheduled observations are protected; expired
  scheduled facts and ordinary outcomes are evicted first. If churn fills the
  bounded cache entirely with current reservations, recording returns
  `ErrCapabilityHealthCapacity` and leaves all active facts intact instead of
  silently evicting one. The observer exposes only the stable
  `observation_record_failed` issue code.
- Preserved operation outcomes and observer refreshes through the same recorder
  while carrying the observer's declared freshness. Monotonic observation time,
  tenant+capability keys, retry-time cloning, and the distinct setup/health
  facts remain unchanged.
- Added sanitized recurring-cycle reporting. A production logger receives at
  most one stable code per failed cycle (`target_census_failed`,
  `target_discovery_failed`, `cycle_deadline_exceeded`, or
  `observation_record_failed`) and the observer waits for its normal interval;
  raw database/provider errors, credentials, response bodies, and topology are
  not part of the callback.
- Changed container lifecycle management to cancel and join the observer before
  returning from Stop. Start/Stop generations serialize under the lifecycle
  lock, so a new generation cannot overlap an in-flight prior discovery or
  probe. Runtime Close already invokes Stop before closing the rate limiter,
  worker, workflow, and database.
- Updated CAP-001 and the Task 1 plan with exact safe-probe, target-page,
  freshness, cache, error-reporting, and shutdown semantics. The public HTTP
  schema and status contract did not change, so Swagger regeneration was not
  required in this round.

### Strict RED/GREEN evidence

1. Declared DeepSeek/OpenAI-compatible model-list probe

   - RED: `go test ./internal/services -run TestLLMCapabilityProbeUsesDeepSeekReadOnlyModelList -count=1`
   - RED output: the probe requested `GET /models/deepseek-chat`; the test
     required the documented list request `GET /models`.
   - GREEN: the same focused command returned
     `ok invoice-backend/internal/services`.
   - Additional GREEN:
     `go test ./internal/services -run 'TestLLM(CapabilityProbeMarksMissingConfiguredModelUnavailableWithoutRawBody|UnsupportedProbeRouteLeavesHealthUnknownWithoutChatMutation|UnrecognizedChatEndpointShapeDoesNotCallProvider)' -count=1`.
     The configured-model absence is sanitized and unavailable; `404`, `405`,
     and unrecognized shapes remain unknown; no chat mutation occurs.

2. Active-target census and declared sweep freshness

   - RED: `go test ./internal/services -run TestDBCapabilityProbeCensusDeclaresWorstCaseBusinessSweep -count=1`
   - RED output: `CapabilityProbeCensus` and its source method were undefined.
   - GREEN: the same focused command returned
     `ok invoice-backend/internal/services`; 240 businesses with two global
     targets and possible GST produce 480 active facts and 40 worst-case
     20-target cycles.
   - RED: `go test ./internal/services -run TestCapabilityHealthCacheUsesDeclaredRefreshSLA -count=1`
   - RED output: `CapabilityHealthObservation` had no `FreshFor` field.
   - GREEN: the same command passed; the customer fact remains fresh inside the
     declared full-sweep window.

3. Scale, rotation, and cache pressure beyond the 170-business review case

   - RED: `go test ./internal/services -run TestCapabilityHealthObserverDeclaresSweepFreshnessBeyond170Businesses -count=1`
   - RED output: observer options had no refresh/cycle SLA and a first-page fact
     became stale under the 600-target rotation fixture.
   - GREEN: the same command passed; the first fact remains fresh and present
     after additional cache pressure.
   - GREEN full-rotation proofs:
     `go test ./internal/services -run 'TestCapabilityHealthObserverRefreshes(EveryTenantAcrossBoundedGlobalProviderPages|GSTTenantsAcrossBoundedProbePages)' -count=1`.
     All 240 global-provider tenants and all 180 GST tenants were refreshed;
     global provider calls stayed one per page, GST calls stayed page-bounded,
     and an outside tenant received no fact.
   - RED: `go test ./internal/services -run TestCapabilityHealthObserverRunCannotExceedDeclaredRefreshInterval -count=1`
   - RED output: a caller-supplied one-hour interval prevented a second cycle
     inside the declared ten-millisecond test refresh window.
   - GREEN: the same command passed after Run clamps slower caller values to the
     declared refresh interval.

4. Active-fact capacity protection

   - RED: `go test ./internal/services -run TestCapabilityHealthCacheCapacityTracksActiveTargetsBeyondDefaultPressure -count=1`
   - RED output: `EnsureActiveCapacity` was undefined.
   - GREEN: the same command passed with 600 active tenant facts.
   - RED: `go test ./internal/services -run TestCapabilityHealthCacheExpiredSweepReservationCanBeEvicted -count=1`
   - RED output: the expired scheduled fact remained protected during pressure.
   - GREEN: the same command passed; expired reservations are eligible first.
   - RED: `go test ./internal/services -run TestCapabilityHealthCacheNeverSilentlyEvictsCurrentActiveObservations -count=1`
   - RED output: build failed with `undefined: ErrCapabilityHealthCapacity`.
   - GREEN: the same command passed; two current facts remain and a third write
     receives the stable capacity error.

5. Mathematically conservative production page bound

   - RED: `go test ./internal/services -run TestCapabilityHealthObserverDefaultTargetBoundLeavesCycleDeadlineMargin -count=1`
   - RED output: expected 20 targets, received 24; 24 worst-case probes consumed
     the entire one-minute deadline.
   - GREEN: the same command passed. Twenty targets require five four-worker
     waves and at most 50 seconds for two five-second attempts.

6. Shutdown join and generation serialization

   - RED: `go test ./internal/services -run TestContainerStopCancelsAndJoinsObserverBeforeReturning -count=1`
   - RED output: Stop returned while the canceled runner was still blocked.
   - GREEN: the same command passed after Stop waits for the observer goroutine.
   - RED: `go test ./internal/services -run TestContainerRepeatedStartStopNeverOverlapsObserverGenerations -count=1`
   - RED output: the previous lifecycle state did not track/join a running
     generation.
   - GREEN: the same command passed across three start/stop generations with
     maximum active generation count one.
   - RED follow-up:
     `go test ./internal/services -run TestContainerStopJoinsConcreteObserverDiscoveryAndProbe -count=1`.
   - RED output: cancellation of an in-flight concrete probe was recorded as a
     provider-unavailable fact.
   - GREEN: the same command passed for both blocking discovery and blocking
     probe; Stop joined both and shutdown cancellation recorded no health fact.

7. Sanitized recurring-cycle reporting

   - RED: `go test ./internal/services -run TestCapabilityHealthObserverReportsSanitizedCycleErrorsWithoutBusyLoop -count=1`
   - RED output: cycle issue/options types were undefined and recurring errors
     had no production reporting seam.
   - GREEN: the same command passed. Two failures were separated by the refresh
     wait and only `target_discovery_failed` was emitted; the fixture's raw
     database secret/error text was absent.

### Validation

- `go test -race -count=1 ./internal/services ./internal/voice/session ./pkg/razorpay ./internal/app ./internal/handlers ./tests/unit`
  - all six packages passed; services `4.037s`, voice `1.541s`, Razorpay
    `1.483s`, app `2.024s`, handlers `1.727s`, unit `1.825s`.
- `go test -count=1 ./cmd/server ./cmd/lambda/http`
  - server compiled with no tests; HTTP Lambda passed in `0.637s`.
- `make fmt`
  - passed (`go fmt ./...` and `goimports`).
- `make lint`
  - passed (`golangci-lint run --timeout=5m`).
- `ruby -ryaml -rdate -e 'YAML.safe_load(File.read("docs/openapi.yaml"), permitted_classes: [Date, Time], aliases: true); puts "openapi yaml ok"'`
  - `openapi yaml ok`.
- `git diff --check`
  - passed before the implementation commit and after the report update.
- Secret marker scan over the implementation diff found no private-key marker,
  AWS access-key pattern, or token-like `sk-` value. Test-only strings named
  `raw-secret` prove redaction and are not credentials.
- Swagger was not regenerated because fix round 3 changed no public route,
  schema, annotation, or error contract. The prior generated/manual contract
  remains covered by the focused app/runtime tests.
- Per campaign instruction, the full `make test` suite was not rerun; the
  controller owns that campaign gate. No live provider call was made.

### Files updated in fix round 3

- `internal/services/llm_service.go` and `llm_service_test.go`.
- `internal/services/capability_health_observer.go` and
  `capability_health_observer_test.go`.
- `internal/services/capability_health_cache.go` and
  `capability_health_cache_test.go`.
- `internal/services/capability_health_recorder.go`.
- `internal/services/container.go` and
  `container_capability_observer_test.go`.
- `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md` and
  `docs/plans/BILLEIF_BACKEND_PRODUCTION_READINESS_PHASE_2.md`.

### Commits

- `5e1e29e fix: harden capability observation lifecycle`
- `docs: record capability observer hardening` (report commit; final hash is
  returned to the parent because a commit cannot contain its own hash)

### Self-review and concerns

- Confirmed the LLM observer uses only a recognized read-only list-models
  contract and a bounded response; unsupported provider shapes stay unknown
  instead of being marked unavailable. No response body, API key, model list,
  provider topology, or tenant/account identifier reaches the callback or
  customer fact.
- Confirmed global network outcomes are applied only to explicitly discovered
  business targets and cached under business+provider-health keys. GST remains
  per-tenant. Setup discovery and health remain distinct, and no outside tenant
  inherits configuration or health.
- Confirmed the database work per cycle is bounded to two census counts, one
  rotating business page, and at most one GST-account lookup for that page. The
  process-local cache is bounded by the ordinary budget plus the active-target
  high-water census; it does not silently evict a current scheduled fact.
- Confirmed cancellation is checked before recording provider outcomes, Stop
  joins the actual observer generation, and Runtime closes the database only
  afterward.
- Remaining limitation: health is deliberately process-local and not durable,
  so concurrent Lambda instances may disagree until each completes its bounded
  census/rotation. Very large active populations produce a proportionally
  longer declared freshness window; this is truthful and prevents rotation-only
  staleness but is less responsive than a future durable/shared observation
  store.
- Razorpay and AI are configured globally, yet their health facts remain
  business-keyed because CAP-001 combines global provider readiness with
  tenant-specific setup, entitlement, permission, and quota. Network calls are
  grouped once per page without sharing a customer cache identity.
- S3, voice, WhatsApp, and email still lack a safe provider probe and remain
  unknown until a future asynchronous producer exists. GST remains unknown if
  no validation path is configured. No weak operator diagnostics were added.
- No migration was added, no provider was called live, and no public capability
  contract changed.

## Fix round 4

Status: complete locally. The rejected tenant census, rotating target pages,
high-water cache reservation, and business-keyed global-provider observations
were removed. Razorpay/AI health is now explicitly global and fixed-size; GST
health is business-scoped and durable. Provider health remains externally
unverified, and internal diagnostics remain blocked pending a distinct operator
authorization mechanism.

### Design and implementation

1. Global Razorpay and AI health

   - `CapabilityGlobalHealthCache` accepts only `razorpay_payments` and `ai`.
     Its API has no business identifier and its fixed two-key observations have
     only a validated status, observation/retry timestamps, and a bounded
     customer-safe classification. It has no credential, account, setup,
     entitlement, permission, quota, raw-error, eviction, reservation, or map
     scan surface.
   - `CapabilityGlobalHealthObserver` receives only a fixed map of configured
     global probers. A cycle probes each at most once with at most two workers,
     five-second probe deadlines, two attempts, a 30-second cycle deadline, and
     a two-minute maximum refresh interval. It performs no database discovery,
     census, `COUNT`, rotation, or tenant write.
   - Razorpay and LLM implement `ProbeGlobalCapability`. Business Razorpay and
     LLM operations no longer write tenant-keyed copies of global network
     health. Capability evaluation combines the global fact with the requesting
     business's setup, entitlement, quota, and permission facts.
   - Lifecycle cancellation and join behavior is preserved. Recurring issue
     callbacks contain only `cycle_deadline_exceeded` or
     `observation_record_failed`; source errors never enter the callback.

2. Complete-list LLM proof

   - The LLM probe recognizes only configured `/chat/completions` and
     `/v1/chat/completions` endpoints and only pagination-free HTTP `200`
     responses with an exact top-level `object: "list"` plus `data` envelope.
   - A streaming decoder caps the body at 1 MiB, the array at 10,000 entries,
     and nested skipped values at depth 64. It parses through the closing array,
     object, and EOF before classifying either model presence or absence.
   - Empty/unrecognized objects, malformed JSON or model entries, unknown
     top-level/pagination fields, partial or other successful HTTP statuses,
     pagination headers, oversized bodies, scan-cap exhaustion, unknown URL
     shapes, and `404`/`405` record nothing and remain unknown. The response
     body, model list, pagination cursor, and credentials are never returned or
     logged. No chat mutation is used.

3. Durable business GST health

   - Paired migration `000054_capability_provider_health_snapshots` adds one row
     keyed by `(business_id, provider_key)`, constrains the only provider key to
     `gst_provider`, constrains status/customer-code combinations, and stores
     only observation/freshness/retry timestamps plus the safe classification.
     The down migration drops only this derived table.
   - The repository performs an exact business/provider read, exact clear, and
     `ON CONFLICT` update only when the incoming `observed_at` is newer. It
     rejects free-form classifications before SQL. Migration identity,
     manifest, latest-version, pair-count, and schema-contract tests were
     updated for version 54.
   - `CapabilityBusinessHealthReader` reads only the requesting business's GST
     row. Missing or stale state fails closed. Observations are fresh for 24
     hours and are never stored in an unbounded process map.
   - Credential upsert clears the prior GST observation and performs no
     provider call, so the sensitive mutation cannot bootstrap itself. Existing
     `POST /tax/integrations/{id}/validate` is the explicit path from unknown to
     observed; validation failures return/store a stable safe message and still
     persist the sanitized unavailable fact. Real e-invoice/e-way provider
     outcomes refresh the same durable row after capability admission.
   - Unknown, stale, or unavailable GST health now returns setup action
     `validate_gst_integration`; missing GST setup still returns
     `configure_gst`. Other providers without a safe producer remain explicitly
     `unobserved` and unknown.

4. Runtime and contracts

   - Runtime repository composition injects the new durable repository into the
     service container. `GET /capabilities` performs no provider I/O; its
     database work is a constant number of exact business-keyed GST reads, not
     a cycle-wide table scan.
   - CAP-001 handoff, Task 1 plan evidence/limitations, manual OpenAPI, handler
     annotation, and generated Swagger were updated for the scope model,
     migration order, `validate_gst_integration`, 24-hour GST freshness, and
     strict LLM completeness behavior.
   - Obsolete census/rotation/high-water production code and tests were
     deleted rather than retained behind new constants.

### Strict TDD evidence

1. LLM completeness

   - RED:
     `go test ./internal/services -run 'TestLLMCapabilityProbeLeavesUnprovenModelListsUnknown' -count=1`
     failed because `{}`, missing markers/data, malformed JSON/entries,
     oversized input, and a configured model before scan-cap exhaustion were
     classified unavailable or healthy instead of unsupported/unknown.
   - Additional RED after self-review:
     `go test ./internal/services -run TestLLMCapabilityProbeRejectsHTTPResponsesThatDoNotProveACompleteList -count=1`
     failed all five fixtures because HTTP `201`, HTTP `206`, `Link`,
     `Content-Range`, and `X-Next-Cursor` responses returned
     `configured LLM model is unavailable`.
   - GREEN:
     `go test ./internal/services -run 'TestLLMCapabilityProbe(UsesDeepSeekReadOnlyModelList|MarksMissingConfiguredModelUnavailableWithoutRawBody|LeavesUnprovenModelListsUnknown|RejectsHTTPResponsesThatDoNotProveACompleteList|UnsupportedProbeRouteLeavesHealthUnknownWithoutChatMutation|UnrecognizedChatEndpointShapeDoesNotCallProvider)' -count=1`
     returned `ok invoice-backend/internal/services`.

2. Explicit global scope and bounded observer

   - RED focused tests initially failed to compile with undefined
     `CapabilityGlobalHealthCache`, `CapabilityGlobalProviderProber`,
     `CapabilityGlobalHealthObserver`, global recorder, and options types.
   - RED:
     `go test ./internal/services -run TestCapabilityGlobalHealthCacheRejectsFreeformProviderDetail -count=1`
     failed with `An error is expected but got nil`.
   - RED:
     `go test ./internal/services -run TestEveryCapabilityDefinitionDeclaresItsProviderHealthScope -count=1`
     reported expected `unobserved`, actual empty scope for `report_exports`.
   - GREEN global cache/observer/scope/lifecycle command:
     `go test -race ./internal/services -run 'TestCapabilityGlobalHealth(Cache|Observer|Recorder)|TestContainer.*Capability|TestContainer.*Observer' -count=1`
     returned `ok invoice-backend/internal/services`.

3. Durable GST schema and repository

   - RED:
     `go test ./migrations -run TestCapabilityProviderHealth -count=1`
     initially failed because both `000054` files were missing.
   - RED repository tests initially failed to compile because the snapshot
     model, repository interface/errors, and PostgreSQL constructor did not
     exist.
   - RED:
     `go test ./internal/repositories/postgres -run TestCapabilityProviderHealthUpsertRejectsFreeformCustomerCodeBeforeSQL -count=1`
     showed an unexpected attempted `INSERT ... ON CONFLICT` containing the
     free-form fixture instead of rejecting before SQL.
   - RED schema classification test reported missing healthy/degraded/
     unavailable status-to-customer-code constraint fragments.
   - GREEN:
     `go test -count=1 ./migrations ./internal/migrator ./internal/repositories/postgres`
     passed all three packages, and `make migration-manifest-verify` verified
     both version-54 directions and the embedded bundle.

4. GST service and runtime wiring

   - RED business-health tests initially failed with undefined business reader,
     GST recorder, and `BusinessHealth` capability option.
   - RED explicit-validation test failed with undefined
     `WithGSTProviderHealthRecorder`; after the durable writer existed, the
     safe-error follow-up failed to compile with undefined
     `ErrGSTCredentialValidationFailed`.
   - RED runtime composition test failed because `Repositories` had no
     `CapabilityProviderHealth` member.
   - GREEN focused service/runtime/repository/migration command:
     `go test ./internal/services ./internal/repositories/postgres ./internal/app ./migrations -count=1`
     passed all four packages.

### Files and migration

- Global observation and evaluation:
  `internal/services/capability_health_cache.go`,
  `capability_health_recorder.go`, `capability_global_health_observer.go`,
  `capability_service.go`, `container.go`, `llm_service.go`, and
  `razorpay_payment_service.go`, with focused tests beside their owners.
- Durable GST state:
  `internal/models/capability_provider_health.go`,
  `internal/repositories/interfaces/capability_provider_health_repository.go`,
  `internal/repositories/postgres/capability_provider_health_repo.go`,
  `internal/services/capability_business_health.go`,
  `tax_compliance_service.go`, `tax_compliance_execution.go`, and runtime
  composition/tests.
- Migration:
  `migrations/000054_capability_provider_health_snapshots.up.sql`, paired down,
  schema test, identity, manifest, embedded counts, and latest-version test.
- Contracts/evidence:
  `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`,
  `docs/plans/BILLEIF_BACKEND_PRODUCTION_READINESS_PHASE_2.md`,
  `docs/openapi.yaml`, `internal/handlers/capability_handler.go`, and generated
  `docs/docs.go`.
- Removed:
  `internal/services/capability_health_observer.go`, its obsolete rotating/
  census test, and the obsolete business-keyed capacity-cache test.

### Validation

- Final focused race gate:
  `go test -race -count=1 ./internal/services ./internal/voice/session ./pkg/razorpay ./internal/app ./internal/handlers ./internal/repositories/postgres ./tests/unit`
  passed: services `4.252s`, voice session `1.493s`, Razorpay `1.439s`, app
  `2.226s`, handlers `1.908s`, PostgreSQL repositories `1.915s`, and unit tests
  `2.002s`.
- Migration/repository gate:
  `make migration-manifest-verify` passed through both `000054` files and the
  embedded-bundle test;
  `go test -count=1 ./migrations ./internal/migrator ./internal/repositories/postgres`
  passed.
- Command compile:
  `go test -count=1 ./cmd/server ./cmd/lambda/http` passed; server has no test
  files and HTTP Lambda passed in `0.633s`.
- `make swagger` completed and regenerated `docs/docs.go`; the generator emitted
  its existing root-package `go list` warning but exited successfully.
- `make fmt` passed (`go fmt ./...` and `goimports`).
- `make lint` passed (`golangci-lint run --timeout=5m`).
- Ruby safe-load validation returned `openapi and swagger yaml ok`; JSON parsing
  returned `swagger json ok`; focused runtime Swagger/routes tests passed.
- `git diff --check` passed. The tracked diff and all new files were scanned for
  private-key markers, AWS access-key patterns, and token-shaped `sk-` values;
  no secret pattern was found. Deliberate `raw-secret` test fixtures prove
  redaction and are not credentials.
- No live provider call, migration apply/down, Terraform plan/apply, external
  write, message, or charge was performed.

### Commit

- `d164278 fix: scope capability provider health`
- `docs: record scoped capability health evidence` (this report commit; its
  final hash is returned to the parent because a commit cannot contain itself)

### Self-review and concerns

- Confirmed a global observation cannot carry a tenant identifier by type or
  API, and only the two explicit global keys enter the global map. All business
  setup, entitlement, quota, permission, and GST facts are evaluated separately
  for the requesting business.
- Confirmed the global cache can never follow historical tenant growth: it has
  two accepted keys, no dynamic capacity, no eviction reservation, and no map
  scan on write. The observer has no database dependency and therefore cannot
  execute the former whole-table census/count work.
- Confirmed LLM health/unavailability is recorded only after complete bounded
  proof. Every incomplete, paginated, malformed, oversized, capped, or
  unrecognized response path returns the unsupported sentinel, which the
  observer intentionally does not record.
- Confirmed the GST table is sanitized, exact-tenant keyed, one row per allowed
  provider key, monotonic, and durable across process turnover. Credential
  mutation clears prior health; explicit validation is the only setup action
  that can move a new/changed integration from unknown to observed before a
  guarded GST mutation.
- Global Razorpay/AI facts remain process-local by design. A cold instance starts
  those two facts unknown, and instances can disagree until their next bounded
  observer cycle. GST does not have that limitation.
- Real GST operation health writes are best-effort so a completed provider/
  document side effect is not converted into a customer failure by an auxiliary
  observation write. A failed write leaves the prior row to become stale and
  should receive dedicated metrics in future observability work; explicit
  validation propagates persistence failure.
- S3, voice, WhatsApp, and email still have no safe producer and remain unknown.
  Internal provider diagnostics remain blocked rather than being exposed to a
  business admin. Provider health remains externally unverified because local
  tests used only fakes and `httptest` servers.
