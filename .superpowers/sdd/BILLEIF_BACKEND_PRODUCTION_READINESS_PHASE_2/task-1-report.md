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
