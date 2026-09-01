# Billeif Backend Production Readiness Phase 2

Status: in progress

This is the living execution plan for the Phase 2 backend campaign. The supplied
Phase 2 specification is authoritative. Every task below must finish as
completed, already correct with evidence, intentionally deferred, externally
blocked, or externally unverified. No live deployment, Terraform apply,
production provider action, payment charge, or real outbound message is part of
this campaign.

## Verified baseline

- Source revision: `5b58a56` on `main` before implementation.
- `make test`: passed on 2026-09-01 with exit code 0, including `-race` and
  repository coverage reporting.
- Existing evidence indicates hardened tenant boundaries, single-use WebSocket
  tickets, PostgreSQL notifications, atomic quota accounting, immutable posted
  payments with compensating reversal, transactional canonical invoice issuance,
  durable outbox processing, and idempotent recurring invoice drafts. Task 0
  must verify these from concrete code and tests before marking them complete.

The test result above is controller-provided evidence for revision `5b58a56`.
Task 0 did not rerun the full suite. The inventory used source, migration, test,
Terraform, workflow, and architecture-document evidence at that same revision.
It does not establish the state of any deployed environment.

## Priority and classification

- P0: Tasks 0-4 - inventory, runtime capabilities, subscription truth,
  operational recovery, and staging verification.
- P1: Tasks 5-8 - security/privacy/recovery, accounting, durable imports, and
  remaining core backend gaps.
- P2: Tasks 9-10 - AI governance and complete release handoff/validation.
- Deferred: official GST filing, saved payment-method vaulting, automatic
  recurring issue/send, full offline POS synchronization, report PDF export,
  web voice, marketplace/A2A/AP2 breadth, and new bargaining modes.
- Externally blocked or unverified items must name the provider, configuration,
  environment, exact safe check not run, and the reason it was not run.

## Global engineering constraints

- Enforce current-business membership and branch/warehouse scope server-side.
- Include tenant identity in cache keys, queue messages, idempotency records,
  object keys, exports, provider metadata, and real-time channels.
- Use integer minor units or exact arithmetic. Posted journals balance; posted
  financial records are immutable; corrections use compensating records.
- Important multi-record events are transactional. External side effects are
  idempotent and recoverable. Unknown provider outcomes require reconciliation.
- Protected mutations authorize the resource and re-evaluate required runtime
  capability. Never expose credentials, secret identifiers, raw provider errors,
  account IDs, or infrastructure topology.
- Preserve existing architecture and data. Add paired reversible migrations.
- Add failing tests before behavior changes, then targeted and broader checks.
- Keep Swagger/OpenAPI and the frontend handoff exact for public contract changes.

## Task 0 evidence inventory

### How classifications are used

- `complete`: the audited Phase 2 slice exists with implementation and focused
  test evidence. It may still be externally unverified.
- `partial`: useful behavior exists, but the Phase 2 contract or invariant is
  incomplete.
- `missing`: no implementation evidence was found.
- `unsafe`: current behavior can present a security, tenant, data-integrity,
  financial, or truthfulness risk if treated as production-ready.
- `deferred`: the specification explicitly excludes the work from this campaign.
- `blocked`: a named dependency prevents implementation or safe verification.
- `externally unverified`: source and local tests exist, but no safe provider or
  deployed-environment check was run.

Route registration alone was never counted as implementation. A route was
classified only after its handler, service, persistence/provider behavior, and
relevant tests were inspected.

### Architecture findings

1. The HTTP composition root is `internal/app/runtime.go`. Protected `/api/v1`
   routes pass through Cognito authentication and business-membership middleware.
   Branch and permission checks remain endpoint-specific. Resource identifiers
   such as `:id` do not by themselves establish business scope; service and
   repository queries must include the effective business and branch scope.
   Evidence: `internal/middleware/business_auth.go`,
   `internal/services/business_auth_service.go`, and
   `internal/services/mutation_authorization.go`.
2. The database is the authority for business state. PostgreSQL adapters sit
   behind repository interfaces; durable side effects use outbox records and
   SQS workers where implemented. Evidence: `internal/repositories/interfaces`,
   `internal/repositories/postgres`, `internal/outbox`, `internal/workers`, and
   ADRs `docs/adr/0001-canonical-invoice-transactional-projection-outbox.md`
   and `docs/adr/0002-serverless-runtime-boundaries.md`.
3. Subscription catalog and quota enforcement are stronger than subscription
   lifecycle truth. Catalog `swipe-v1` and atomic monthly quota reservation are
   implemented, but a paid Razorpay checkout grants exactly one month; it is not
   a renewable provider subscription. Evidence: `internal/services/subscription_catalog.go`,
   `internal/services/entitlements.go`, and
   `internal/services/razorpay_payment_service.go`.
4. Invoice render and delivery, WebSocket tickets, notifications, posted journal
   invariants, and recurring draft creation have durable implementations.
   Evidence includes `internal/services/invoice_delivery_test.go`,
   `internal/repositories/postgres/invoice_delivery_repo_test.go`,
   `internal/services/websocket_ticket_service_test.go`,
   `internal/repositories/postgres/websocket_ticket_repo_test.go`,
   `internal/services/notification_service_test.go`,
   `internal/repositories/postgres/notification_repo_test.go`,
   `internal/services/journal_invariants_test.go`, and
   `cmd/lambda/recurring-invoices/main_test.go`. There is no aggregate
   operational read model or recovery API.
5. Several apparent Phase 2 surfaces are not safe contracts yet: GST silently
   falls back to a simulated provider, bulk imports have no commit worker and
   read entire multipart files into memory, upload presigns have no completion
   verification, coupon limits race at redemption, and the cart is a signed AP2
   mandate rather than an editable tenant-versioned cart.
6. Terraform defines private encrypted storage, queues and DLQs, scheduled
   workers, Cognito, SES, WebSocket, payment, and observability resources, but
   Apart from the controller's read-only STS identity check recorded below,
   Task 0 performed no apply, application-provider call, charge, message, or
   staging probe.

### Milestone classification and impact ledger

| Task | Baseline classification | Evidence | Expected smallest owners and impact |
| --- | --- | --- | --- |
| 0 | `complete` for inventory; external state `externally unverified` | This plan; `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md`; evidence paths below | Documentation only. No migration, provider call, runtime, OpenAPI, Terraform, or test change. |
| 1 | `complete` locally including governed mutation preflights, production outcome recorders, and bounded asynchronous Razorpay/LLM/GST bootstrap observation; provider health `externally unverified`; internal diagnostics `blocked` | CAP-001 evaluation/cache/recorder plus guards at report, payment/storefront, GST command, drive upload, voice, business LLM, bulk-import, and saved-payment service boundaries; focused behavior/race tests are beside each owner | No migration. Customer output and typed mutation errors are secret-safe; unknown/stale fails closed. HTTP runtime startup asynchronously discovers tenant targets in rotating bounded batches, performs read-only Razorpay/LLM probes and GST credential validation when supported, and refreshes every two minutes without request-time fan-out. S3/voice/WhatsApp/email remain unknown without a safe producer. Internal diagnostics require a future operator principal distinct from business owner/admin. |
| 2 | `partial`, with an `unsafe` truth gap; Razorpay `externally unverified` | `internal/models/subscription.go`, `internal/models/payment_attempt.go`, `internal/services/subscription_service.go`, `internal/services/razorpay_payment_service.go`, `migrations/000041_add_razorpay_payment_attempts.up.sql`, `internal/services/razorpay_payment_service_test.go`, `internal/handlers/payment_handler_idempotency_test.go`, `infrastructure/terraform/tests/razorpay.tftest.hcl` | Subscription/payment models, repositories, services, handlers, webhook inbox/reconciliation worker, paired migrations, provider fixtures, OpenAPI and race/replay tests. Expand-first state conversion must not activate entitlements from stale or ambiguous events. |
| 3 | `partial`; deployed queues/providers `externally unverified` | Render/delivery status in `internal/services/invoice_service.go` and `internal/services/invoice_delivery.go`; outbox/workers; `infrastructure/terraform/monitoring.tf` and `sns_sqs.tf` | Aggregate read service/repositories, customer and operator handlers, recovery commands, audit, metrics/alarms, permissions/step-up, OpenAPI, state/retry tests. Recovery must remain tenant-bound and idempotent. |
| 4 | `missing`; every staging dependency `externally unverified` | No bounded environment-selecting verification command found after inspecting every entry point under `cmd`, `internal/config`, `Makefile`, `infrastructure/terraform`, `.github/workflows`, and `docs` | New verification command and an adjacent refusal/redaction/classification/cleanup test package; documentation and safe adapters only unless a real gap is found. No schema expected by default. Must refuse production, redact secrets, bound writes, and classify cleanup. |
| 5 | `partial`, with `unsafe` upload/session/privacy gaps; AWS/Cognito restore state `externally unverified` | `internal/middleware/business_auth.go`, `internal/services/websocket_ticket_service.go`, `infrastructure/terraform/s3.tf`, `internal/services/report_service.go`, `internal/services/auth_service.go`, `internal/services/auth_upload_test.go`, `internal/services/auth_phone_test.go`, `.github/workflows/deploy.yml` | Step-up/session/MFA/privacy/upload/recovery owners across middleware, services, handlers, repositories, `cmd`, Terraform/CI and paired migrations. Security and data-retention impact is high; no live Cognito or paid scanner action. |
| 6 | `partial`; banking, lock date, Trial Balance and Balance Sheet `missing` | `internal/services/journal_service.go`, `internal/services/payment_service.go`, `internal/reporting`, `internal/services/journal_invariants_test.go`, `internal/services/payment_invariants_test.go`, `internal/services/payment_postgres_integration_test.go`, `tests/unit/journal_handler_test.go`, `tests/unit/ledger_service_test.go` | Journal/payment/inventory/reporting services and repositories, new accounting/bank models, paired migrations, permissions/step-up, OpenAPI, concurrency/invariant/export tests. Posted records remain immutable and balanced by currency. |
| 7 | `unsafe` and `partial` | `internal/handlers/billing_ops_handler.go`, `internal/services/billing_ops_service.go`, `internal/models/swipe_ops.go`, `tests/unit/billing_ops_handler_test.go`, `migrations/000030_add_swipe_billing_ops.up.sql`; no bulk-import worker found in `internal/workers`, `cmd`, or `infrastructure/terraform` | Narrow to customer/vendor/product validation and commit handlers/services/repository, S3 metadata, worker/queue/alarm, paired migrations if state is insufficient, OpenAPI and restart/idempotency/tenant/formula tests. Existing queued jobs require a compatibility and cleanup decision. |
| 8 | `partial`, with `unsafe` asset/cart/coupon behavior; phone/S3/provider paths `externally unverified` | `internal/services/auth_phone_test.go`, `internal/services/s3_service_test.go`, `internal/services/commerce_service_test.go`, `internal/services/shopping_agent_service_test.go`, `internal/services/shopping_agent_signature_test.go`, `internal/services/report_service_test.go`, `tests/unit/commerce_handler_test.go`, `tests/unit/report_handler_test.go` | Existing owning services/handlers/models plus paired migrations for upload/cart/coupon state, report export implementation, permissions/entitlements, OpenAPI and concurrency/security tests. Saved methods remain unavailable. |
| 9 | `partial`; capability management `unsafe`; AI providers `externally unverified` | `internal/models/agent.go`, `internal/handlers/agent_handler.go`, `internal/services/agent_service.go`, `tests/unit/agent_handler_test.go`, `tests/integration/agent_test.go`, `internal/voice/tools/authorization_test.go` | Agent/voice runtime authorization, governance models/repositories, audit/budget/approval/kill-switch services, paired migrations, provider-safe tests and AI contracts. Default deny unclassified tools; never persist secrets or unnecessary prompts. |
| 10 | `pending`; GST filing is `deferred` | Existing OpenAPI and this initial handoff are evidence inputs, not final release proof | OpenAPI regeneration, exact handoff completion, full validation, final diff and the required non-filing GST reconciliation follow-up plan. No frontend files or deployment actions. |

### Implemented contract and invariant evidence

| Area | Classification | Concrete evidence and limitation |
| --- | --- | --- |
| Business and branch authorization | `partial` | Protected routes use `middleware.Auth` and `middleware.BusinessAuth`; scoped services/repositories generally recheck `business_id`. Endpoint permissions and branch requirements are not uniform, so each mutation remains an audit target. Evidence: `internal/app/runtime.go`, `internal/middleware/business_auth.go`, `internal/services/business_auth_service.go`. |
| Subscription catalog | `complete` locally | `swipe-v1` defines free/pro/rise/biz amounts in integer paise, features and quotas. Evidence: `internal/services/subscription_catalog.go`, `internal/services/subscription_catalog_test.go`, `internal/handlers/subscription_catalog_handler_test.go`. |
| Entitlement resolution and quota reservation | `complete` for the implemented feature subset | Missing/inactive/expired subscription falls back to free; e-invoice/e-way bill usage is reserved atomically per month. It is not the Phase 2 runtime capability model. Evidence: `internal/services/entitlements.go` and `internal/services/entitlements_test.go`. |
| Subscription lifecycle | `partial` and `unsafe` if described as recurring | Subscription state lacks provider mode/IDs, grace/cancel-at-period-end and billing history. Direct plan mutations produce unstable server errors. Paid checkout sets one one-month period. Evidence: `internal/models/subscription.go`, `internal/services/subscription_service.go`, `internal/services/razorpay_payment_service.go`. |
| Razorpay event handling | `partial`, `externally unverified` | Raw-body HMAC and unique event ID exist. Inbox rows lack mode, payload hash, verification state, attempts, sanitized error, resolved IDs and replay audit; no reconciliation worker/read model exists. Evidence: `migrations/000041_add_razorpay_payment_attempts.up.sql`, `internal/services/razorpay_payment_service.go`, `internal/services/razorpay_payment_service_test.go`, `internal/handlers/payment_handler_idempotency_test.go`, `pkg/razorpay`. |
| Invoice render status | `complete` for customer-safe status | Tenant/invoice/job scoped projection exposes only kind, source version and timestamps. Evidence: `internal/services/invoice_service.go`, `internal/handlers/invoice_handler.go`, `internal/models/invoice_foundations.go`, `tests/unit/invoice_service_test.go`, `internal/handlers/invoice_download_handler_test.go`, `internal/workers/pdf_renderer_snapshot_test.go`. |
| Invoice delivery | `complete` for create/status | UUID idempotency key and canonical request hash are claimed atomically with activity/outbox state; status projection omits provider and lease details. Evidence: `internal/services/invoice_delivery.go`, `internal/repositories/postgres/invoice_delivery.go`, `internal/services/invoice_delivery_test.go`, `internal/handlers/invoice_delivery_handler_test.go`, `internal/repositories/postgres/invoice_delivery_repo_test.go`. |
| Notifications | `complete` for current list/read contract | Business+user-scoped list, read and read-all; source-event idempotency exists. List is capped but not cursor-paginated. Evidence: `internal/models/notification.go`, `internal/handlers/notification_handler_test.go`, `internal/services/notification_service_test.go`, `internal/repositories/postgres/notification_repo_test.go`, `migrations/000051_notifications.up.sql`. |
| WebSocket tickets | `complete` locally | One-use, business+subject-bound, at most 60-second ticket; only SHA-256 digest is stored. Evidence: `internal/services/websocket_ticket_service.go`, `internal/services/websocket_ticket_service_test.go`, `internal/repositories/postgres/websocket_ticket_repo_test.go`, `internal/handlers/websocket_ticket_handler_test.go`, `tests/unit/websocket_handler_test.go`, `migrations/000050_websocket_tickets.up.sql`. |
| Recurring invoice drafts | `complete` within deferral boundary; scheduler `externally unverified` | Durable runs create idempotent drafts; auto-send is rejected. Automatic issue/send remains deferred. Evidence: `internal/services/billing_ops_invoice_subscription_test.go`, `internal/services/billing_ops_invoice_subscription_postgres_integration_test.go`, `cmd/lambda/recurring-invoices/main_test.go`, `infrastructure/terraform/recurring_invoices.tf`. |
| Journals and ledger | `complete` for existing invariant subset; Phase 2 accounting `partial` | ACC-001/002 cover list/get/create/update/delete/post/reverse plus ledger list/balance. Posted journals balance per currency, are immutable, and reverse through compensation. Mutations have no idempotency/version/step-up/fiscal lock; ledger uses floating-point response fields. Trial Balance, Balance Sheet, opening balances and banking were not found after inspecting `internal/handlers`, `internal/services`, `internal/models`, `internal/repositories`, `internal/reporting`, and `migrations`. Evidence: `internal/services/journal_service.go`, `internal/models/journal.go`, `internal/models/ledger.go`, `internal/services/journal_invariants_test.go`, `tests/unit/journal_handler_test.go`, `tests/unit/ledger_service_test.go`, `migrations/000008_ledger_entries.up.sql`, `migrations/000025_add_document_platform.up.sql`, `migrations/000029_add_projects_and_reporting.up.sql`. |
| Payment accounting | `complete` for existing invariant subset | Payment reversal routes through compensating journal behavior. Evidence: `internal/services/payment_service.go`, `internal/services/payment_invariants_test.go`, `internal/services/payment_postgres_integration_test.go`, `migrations/payment_reversal_schema_test.go`. |
| GST document operations | `unsafe`, `partial`, provider `externally unverified` | OPS-004 exposes durable job/retry states, but its status nests idempotency keys, queue IDs, raw payloads, provider references/errors and URLs; unknown documents may read as `idle`. Business-global idempotency keys are not request-hash bound. Missing provider configuration selects `simulatedGSTProvider`, which can fabricate success. Evidence: `internal/services/tax_compliance_execution.go`, `internal/services/gst_provider.go`, `internal/models/gst_compliance.go`, `internal/services/tax_compliance_service_test.go`, `tests/unit/document_handler_test.go`. |
| GST setup, lookup and reconciliation | `partial`, some output `unsafe`; provider `externally unverified`; official filing `deferred` | OPS-005 hides encrypted credentials but can return hints/raw metadata/simulated status. OPS-006 provides synchronous GSTR-2B matching and report runs, not filing. Evidence: `internal/handlers/tax_handler.go`, `internal/services/tax_compliance_service.go`, `internal/models/tax.go`, `internal/services/tax_compliance_service_test.go`, `tests/unit/tax_handler_test.go`, `migrations/000027_add_gst_compliance.up.sql`, `migrations/000031_add_gst_execution_and_pos.up.sql`. |
| Bulk import | `unsafe`, `partial` | Multipart content is read fully into memory and rows are persisted as queued, but no processing worker was found after inspecting `internal/workers`, `cmd`, and `infrastructure/terraform`. Product/invoice/document imports also lack the complete permission model. Evidence: `internal/handlers/billing_ops_handler.go`, `internal/services/billing_ops_service.go`, `internal/models/swipe_ops.go`, `tests/unit/billing_ops_handler_test.go`, `migrations/000030_add_swipe_billing_ops.up.sql`. |
| Uploads and assets | `unsafe`, `partial`, S3 `externally unverified` | Logo and drive presigns bind declared size/type; Terraform blocks public access and encrypts/version-controls buckets. No pending-upload completion, checksum, quarantine/scan or object-metadata verification exists. Drive creates the asset before upload and returns internal coordinates. Evidence: `internal/services/s3_service_test.go`, `tests/unit/business_service_test.go`, `internal/services/commerce_service_test.go`, `tests/unit/commerce_handler_test.go`, `tests/integration/s3_test.go`, `infrastructure/terraform/s3.tf`. |
| Phone authentication | `partial`, Cognito/SMS `externally unverified` | Indian E.164 normalization, register/confirm/resend/login/verify/refresh/global logout exist. Login reveals local registration; explicit provider linking, durable device/session revocation, MFA and audit are absent. Evidence: `internal/services/auth_service.go`, `internal/services/auth_phone_test.go`, `tests/unit/auth_service_test.go`, `tests/integration/auth_test.go`, `migrations/000023_add_phone_auth_fields.up.sql`. |
| Cart | `partial` and `unsafe` for Phase 2 UI | Existing cart is a signed user/agent AP2 mandate with JSON-string items and float amount. Add-item creates a new one-item mandate; retries/concurrent checkout can duplicate because there is no serialized command claim. Update/remove/clear, tenant/version checks and authoritative price/stock revalidation are absent. Evidence: `internal/models/ap2_mandate.go`, `internal/services/shopping_agent_service.go`, `internal/services/shopping_agent_service_test.go`, `internal/services/shopping_agent_signature_test.go`, `tests/unit/shopping_handler_test.go`. |
| Coupons | `partial` and `unsafe` under concurrency | Create/list/full update/public validation exist, but there is no delete policy and usage limits are counted before redemption without serializing the coupon row. Evidence: `internal/services/commerce_service.go`, `internal/models/commerce.go`, `migrations/000032_add_storefront_enterprise_features.up.sql`, `internal/services/commerce_service_test.go`, `internal/services/commerce_checkout_postgres_integration_test.go`, `internal/services/storefront_tenant_scope_test.go`. |
| Report export | `partial` | JSON and formula-neutralized CSV are returned inside a JSON envelope. Native typed XLSX, file response/disposition and timezone-aware cells are absent. Evidence: `internal/services/report_service.go`, `internal/services/report_service_test.go`, `tests/unit/report_handler_test.go`. |
| AI capability inventory | `partial` and `unsafe` as governance; providers `externally unverified` | AI-001 capability list/add/remove stores descriptive JSON-string config. The access helper admits owner or same-business users and no route permission is enforced. `validate-permissions` only checks that access and can double-write a response after failure; it does not authorize a capability/tool/action. There is no risk classification, scoped approval, budget reservation, kill switch or governance audit. Evidence: `internal/handlers/agent_handler.go`, `internal/services/agent_service.go`, `internal/models/agent.go`, `tests/unit/agent_handler_test.go`, `tests/integration/agent_test.go`, `migrations/000013_add_ap2_agent_marketplace.up.sql`. |
| Infrastructure and CI | `partial`, `externally unverified` | Terraform defines private buckets, SQS/DLQs, workers, schedules, Cognito, SES, WebSocket and alarms; deployment CI exists. No apply, restore drill or staging verification was performed. Evidence: `infrastructure/terraform/s3.tf`, `infrastructure/terraform/sns_sqs.tf`, `infrastructure/terraform/recurring_invoices.tf`, `infrastructure/terraform/cognito.tf`, `infrastructure/terraform/cognito_phone.tf`, `infrastructure/terraform/ses.tf`, `infrastructure/terraform/api_gateway_websocket.tf`, `infrastructure/terraform/monitoring.tf`, `infrastructure/terraform/tests/http_serverless.tftest.hcl`, `infrastructure/terraform/tests/outbox.tftest.hcl`, `.github/workflows/deploy.yml`. |

### Provider and environment truth

| Provider/dependency | Source-level state | Task 0 verification state |
| --- | --- | --- |
| Razorpay | Order, verify and webhook adapters exist; purchase grants one month | `externally unverified`: no test-mode order, payment or webhook was sent because external writes/charges are outside scope. |
| GST/e-invoice/e-way bill | HTTP adapter plus unsafe simulated fallback | `externally unverified`: no sandbox submission or reconciliation check; simulated results are not provider evidence. |
| Cognito email/phone and SMS | SDK flows and separate phone pool configuration exist | `externally unverified`: no account, OTP, MFA or session mutation was performed. |
| S3 | Presign clients and private Terraform buckets exist | `externally unverified`: no upload, HEAD, download, lifecycle or cleanup check was performed. |
| SQS/EventBridge/Lambda | Worker, queue, DLQ and schedule Terraform exists | `externally unverified`: no deployed message or schedule was inspected. |
| SES, WhatsApp, Sarvam, Claude, Gemini, AgentCore | Adapters or infrastructure exist in varying depth | `externally unverified`: no outbound message/model call was made. Runtime capability state must remain unknown or unavailable until Task 1 evidence exists. |
| PostgreSQL and Redis/Valkey | Local adapters have test evidence in `internal/app/database_runtime_test.go`, `internal/app/database_pool_test.go`, `internal/migrator/postgres_test.go`, `internal/migrator/postgres_integration_test.go`, `internal/ratelimit/redis_limiter_test.go`, and `internal/ratelimit/redis_limiter_integration_test.go` | `externally unverified` for staging/production. Task 4 owns bounded environment checks. |
| AWS control plane | Controller evidence records AWS CLI `2.36.7` and a successful read-only STS identity check for profile `default` in `ap-south-1` | Further live inspection was intentionally not run because the credentials resolved to the account root principal. No account identifier is recorded here; aside from that identity check, no resource mutation or application-provider call occurred. Task 4 requires a least-privilege non-production identity. |

### Absence-search boundaries

Negative findings are based on the owning surfaces below, not route-registration
absence alone:

| Finding | Inspected owners/directories |
| --- | --- |
| No distinct operator authorization for internal capability diagnostics | `internal/app`, `internal/middleware`, `internal/handlers/admin_handler.go`, `internal/services/business_auth_service.go`, Cognito role claims, and permissions; business owner/admin is not an operator principal |
| No bounded staging verifier | every entry point under `cmd`, plus `internal/config`, `Makefile`, `infrastructure/terraform`, `.github/workflows`, and `docs` |
| No fiscal lock, Trial Balance, Balance Sheet, opening-balance or banking workflow | `internal/handlers`, `internal/services`, `internal/models`, `internal/repositories`, `internal/reporting`, `migrations`, `openapi` |
| No bulk-import processor/queue/commit workflow | `internal/handlers/billing_ops_handler.go`, `internal/services/billing_ops_service.go`, `internal/models/swipe_ops.go`, all of `internal/workers`, every entry point under `cmd`, `infrastructure/terraform`, and `migrations` |
| No AI risk/approval/budget/kill-switch governance | `internal/handlers/agent_handler.go`, agent-prefixed owners under `internal/services` and `internal/models`, `internal/voice`, `internal/repositories`, `migrations`, and AI/provider Terraform under `infrastructure/terraform` |
| No verified upload completion/privacy/device/MFA workflows | `internal/handlers`, `internal/services`, `internal/models`, `internal/repositories`, `internal/middleware`, `cmd`, `migrations`, `infrastructure/terraform`, and `.github/workflows` |

## Task 0: Baseline and contract inventory

Status: completed (inventory and living-contract baseline; fix round 1 expanded)

Audit Phase 2 routes, handlers, services, repositories, models, migrations,
workers, queues, providers, states, idempotency, permissions, entitlements,
tests, Terraform, and CI. Classify every area as complete, partial, missing,
unsafe, deferred, blocked, or externally unverified and cite evidence paths.
Verify provider and subscription semantics from implementation, not route
presence. Create the initial exact frontend handoff and update this plan with
architecture findings, expected files, migration/provider/test/rollout/security/
data-integrity impacts, and milestone status.

Task 0 changed only the two requested documentation artifacts. No migration,
provider, rollout, infrastructure, production-code, OpenAPI, or test impact was
introduced. The campaign remains in progress because Tasks 1-10 are pending.
The fix-round audit added current ACC-001/002, OPS-004/005/006, and AI-001
contracts and completed endpoint-specific ASSET-001, IMP-001, CART-001, and
COUPON-001 examples/status/retry evidence. These are baseline truth, not release
approval for surfaces marked unsafe or internal-only.

## Task 1: Runtime capability model

Status: completed locally; provider health externally unverified; internal
diagnostics blocked pending a distinct operator authorization mechanism

Implement business-scoped, backend-authoritative capability evaluation covering
Razorpay, GST/e-invoice/e-way bill, WhatsApp, email, S3 uploads, voice, AI,
storefront payments, report exports, bulk imports, and saved payment methods.
Separate product support, configuration, cached provider health and timestamp,
entitlement, quota, permission, business setup, platform support, final state,
stable reason/setup/retry/degradation fields. Supported states are `available`,
`setup_required`, `upgrade_required`, `quota_exhausted`, `permission_denied`,
`temporarily_unavailable`, `unsupported_platform`, `unsupported`, and `unknown`.
Add customer and separately authorized internal diagnostics endpoints, cache
tenant-isolation and degradation tests, config-state/service/handler/permission
tests, OpenAPI, and CAP handoff contracts. Customer output must be secret-safe.

Inventory note: no equivalent runtime model was found. Existing plan
entitlements must be an input, not a replacement. Expected owners are
`internal/config`, capability models/repositories/services/handlers,
`internal/app/runtime.go`, permissions, OpenAPI, and planned capability service,
handler, tenant-cache, permission, and degradation test files beside their
owners. Add a paired
migration only if health/config snapshots are durable. Roll out customer-safe
evaluation before any UI depends on it; keep unknown providers unavailable.

Implemented CAP-001 at `GET /api/v1/capabilities?platform=web|ios|android`.
The response separately reports product support, configuration presence,
cached health and observation time, subscription entitlement, quota,
permission, business setup, platform support, final availability, stable state
and reason, setup action, retry time and customer-safe degradation. The bounded
inventory covers Razorpay, GST provider, e-invoice, e-way bill, WhatsApp,
email, S3 uploads, voice, AI, storefront payments, report exports, bulk
imports and saved payment methods. Web voice is `unsupported_platform`; saved
payment methods are `unsupported`; bulk import is `unsupported` with
`bulk_import_processor_unavailable`; simulated GST never counts as configured.

`CapabilityService.Evaluate` and its typed `Require` preflight are reusable at
sensitive service mutation boundaries. Report exports, Razorpay plan/storefront
orders, GST commands, drive presigns, voice admission, business LLM execution,
bulk-import intake, and saved-payment mutations now fail before effects when
the capability is unavailable. Preflight observes current authoritative
entitlements/quotas and permissions without reserving quota or calling a
provider; existing transactional GST quota reservation and authoritative drive
byte enforcement remain decisive.

Actual Razorpay order, business-scoped LLM, and GST e-invoice/e-way provider
outcomes record sanitized tenant/provider-health observations. Storefront shares
the Razorpay provider fact; e-invoice and e-way bill share the GST provider fact.
In addition, the HTTP runtime starts a recurring asynchronous observer that
discovers tenants in rotating bounded batches, shares one read-only Razorpay
order-list and LLM model-lookup probe across each batch, and validates stored GST
integration credentials per tenant when `GST_VALIDATE_PATH` is configured. It
uses four workers, five-second probe deadlines, at most two attempts, a maximum
of 256 targets per cycle, and a two-minute non-overlapping refresh. Success,
rate-limit, timeout and unavailable classifications are monotonic by observation
time and become stale after five minutes; configuration alone never records
healthy. Missing and stale health fail closed and provider failure never changes
the separately returned entitlement fact. Unsupported probe shapes record
nothing and remain unknown. S3 presign, voice admission,
WhatsApp, and email have no safe business-scoped probe in this task and remain
unknown until a future asynchronous producer exists. The cache is deliberately
process-local: each Lambda instance begins unknown briefly and establishes its
own observations asynchronously, so Task 1 makes no deployed-provider health
claim and instances can temporarily disagree.

No migration was added because configuration and setup use existing rows and
provider health is ephemeral. A separately authorized internal diagnostics
endpoint was not added: current `admin` is a business role, not a distinct
operator principal, and exposing provider internals through it would violate
the authorization requirement. This remains blocked until a genuine operator
identity and policy exist.

Drive capability quota and upload enforcement share one tenant-scoped
`InspectDriveStorage` reader. It reports MB limit/used/remaining while enforcing
at byte precision, rejects the exact limit before asset/presign effects, and the
read path never reserves quota.

Evidence: `internal/config/capabilities_test.go`,
`internal/services/capability_health_cache_test.go`,
`internal/services/capability_health_recorder_test.go`,
`internal/services/capability_health_observer_test.go`,
`internal/services/capability_service_test.go`,
`internal/services/capability_setup_reader_test.go`,
`internal/services/entitlements_test.go`,
`internal/services/report_service_test.go`,
`internal/services/razorpay_payment_service_test.go`,
`internal/services/tax_compliance_service_test.go`,
`internal/services/commerce_service_test.go`,
`internal/services/llm_service_test.go`,
`internal/services/credential_provider_capability_test.go`,
`internal/services/mutation_authorization_test.go`,
`internal/voice/session/service_test.go`,
`internal/handlers/capability_handler_test.go`, and
`internal/app/runtime_routes_test.go`, and
`internal/app/runtime_swagger_test.go`. Swagger was regenerated in
`docs/docs.go`; `docs/openapi.yaml` and CAP-001 in
`docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md` contain the exact public
contract. Frontend rollout must follow the backend deployment and must keep
provider-backed actions unavailable while health is unknown.

## Task 2: Subscription and Razorpay lifecycle

Status: pending

Determine whether monthly plans are renewable subscriptions or one-time
checkout and make wording and behavior truthful. Implement the durable state
machine, provider IDs/mode separation, periods/renewal/grace/cancellation,
plan changes and proration policy, billing history, entitlement/quota/storage
effects, and audit. Add a signed provider-event inbox with unique provider event
identity, mode, type, payload hash, verification, timestamps, processing state,
attempts, sanitized error, resolved business/subscription, and replay audit.
Prevent duplicate/stale/out-of-order/wrong-plan/test-live events from regressing
or activating entitlement; serialize races; reconcile missing/ambiguous events
with bounded provider fetches. Test every named race, replay, invalid signature,
timeout, grace, failure, mismatch, and reconciliation scenario. Add SUB contracts.

Inventory note: the current paid flow is a one-time checkout that sets one month
of access, not a renewable subscription. The webhook record is too small for the
specified inbox and reconciliation semantics. Expected owners are subscription,
payment-attempt and webhook models/repositories/services/handlers, a
reconciliation worker, paired migrations, OpenAPI and provider/race tests. Use
expand-first compatibility for existing rows and never infer payment from an
unknown provider outcome.

## Task 3: Operational visibility and recovery

Status: pending

Build a secure aggregate operational read model across render, delivery, outbox,
Razorpay, GST/e-invoice/e-way bill, recurring, email/SES, WhatsApp,
notifications, imports, and voice reconciliation without forcing one storage
model. Expose business-safe status and separately authorized operator detail.
Distinguish unknown/reconciliation-required from failed. Implement only safe,
audited, idempotent retry/reconcile/re-render/delivery/webhook/DLQ/resolution/
timeline operations bound to original tenant/resource. Add step-up where needed,
metrics/alarms for queue age, failures, DLQ growth, reconciliation, latency,
webhooks, schedules, render and delivery, plus OPS contracts and tests.

Inventory note: individual render, delivery, GST, recurring and notification
states exist, plus queues/DLQs and some alarms; the aggregate projection and safe
recovery commands do not. Expected owners span a new aggregate service over
existing repositories, customer/operator handlers, audit, permissions/step-up,
Terraform alarms, OpenAPI and recovery/idempotency tests. Do not collapse
`unknown` into `failed` or reveal provider payloads.

## Task 4: Staging verification harness

Status: pending

Create a bounded JSON-emitting verification command that explicitly selects an
environment, refuses production by default, separates read-only from visible
writes, redacts secrets/provider responses, classifies passed/failed/skipped/
blocked/not-configured, records timestamps/evidence, bounds concurrency/retries,
and safely cleans synthetic resources. Cover configured Cognito/Google/phone,
PostgreSQL, Redis, S3, SQS, EventBridge, WebSocket tickets, Razorpay test, SES,
WhatsApp/GST sandboxes, Claude/Gemini/Sarvam/AgentCore and controlled invoice,
report, upload, recurring, WebSocket and checkout journeys. Unit-test refusal,
redaction, classification, cleanup, schema; document non-production commands.

Inventory note: no command with these refusal, redaction and classification
properties was found. Expected owners are a focused `cmd` package and tests plus
non-production documentation; reuse safe adapters rather than adding product
state. Every provider and deployed resource is externally unverified until this
harness records evidence.

## Task 5: Security, privacy, and recovery

Status: pending

Implement scoped one-time step-up authorization bound to user, business, action,
resource, time, assurance, expiry and identifier for high-risk actions. Implement
truthful Cognito-compatible session/device registration and revocation semantics,
TOTP MFA flows/configuration, safe tenant-bound pending uploads with metadata,
checksum, quarantine/scan abstraction/download/cleanup/audit, spreadsheet formula
neutralization preserving typed cells, durable privacy export/deletion/retention/
purge/provider/S3 cleanup, backup/restore runbook/scripts/RPO/RTO and safe drill
classification, and focused CI secret/dependency/static/migration/Terraform/
OpenAPI/logging/tenant checks. No paid scanner or live Cognito changes.

Inventory note: tenant middleware, permissions, secret resolution, private S3,
single-use WebSocket tickets and CSV formula neutralization are useful foundations.
Step-up, TOTP, durable session/device state, verified uploads, privacy workflows
and restore drills are absent. Expected owners span auth/middleware, upload and
privacy services/repositories, paired migrations, safe `cmd` utilities,
Terraform/CI, OpenAPI and adversarial tests. Existing unverified presigns must
not be presented as completed assets.

## Task 6: Accounting completeness

Status: pending

First map existing fiscal year, lock date, Trial Balance, Balance Sheet, opening
balances, bank and subledger/inventory/tax reconciliation. Implement only gaps.
Enforce a business lock date across every posting/inventory/origination/background
path with permission, scoped step-up override, reason, audit and explicit reversal
period policy. Add posted-ledger Trial Balance and Balance Sheet with hierarchy,
opening/period/closing totals, branch/currency/comparison/drilldown/export/share.
Add read-only reconciliation diagnostics with no auto-repair. Add validated,
idempotent, audited opening-balance postings and inventory facts. Add bank account,
statement import/transaction, exact and bounded fuzzy suggestions, manual match/
unmatch, fee/interest adjustment, reconciliation date, unreconciled/audit state.
Include paired migrations, concurrency/invariant/permission tests and ACC contracts.

Inventory note: journal and payment posting/reversal invariants exist, but fiscal
lock, Trial Balance, Balance Sheet, opening balances and banking/reconciliation
were not found. Expected owners are journal/payment/inventory/reporting services,
new accounting/bank repositories and models, paired migrations, permissions and
scoped step-up, OpenAPI and concurrency/invariant/export tests. Backfill and
rounding policy must preserve existing posted data.

## Task 7: Durable bulk import

Status: pending

Implement two-phase durable CSV import for customers, vendors, and products only.
Validation/preview uses tenant-bound upload metadata, streaming parser, encoding/
delimiter/columns/mapping/normalization/GSTIN/contact/unit/tax/price/currency and
duplicate checks with durable row errors and no business mutation. Commit uses a
UUID command bound to business/uploader, locked state transition, bounded batches,
durable progress/results, worker retry, row idempotency, artifacts, notification,
retention/cleanup/cancellation. Prevent cross-tenant access, replay after restart,
partial invalid mutation, unscoped object keys, and formula injection. Reuse the
existing bulk-job/SQS architecture; add migrations, worker/service/handler tests,
alarms and IMP contracts.

Inventory note: the current import intake is unsafe to expose as a finished
workflow: it buffers the entire multipart file and creates queued rows without a
worker. Expected owners are the existing bulk-job model/service/handler plus S3
metadata, a dedicated worker/queue/alarm and repository transitions. Use paired
migrations if current states cannot be expanded safely. Decide how existing
queued jobs are failed, migrated or retried before enabling the frontend.

## Task 8: Remaining core backend gaps

Status: pending

Strengthen phone authentication/account linking with E.164, resend/expiry/replay,
refresh/logout, collision detection, explicit link (never silent merge), audit,
enumeration-resistant errors/rates. Complete business-logo pending upload and
tenant key/checksum/metadata/type/size verification with transactional idempotent
reference update and post-commit old-object cleanup. Add cart update/remove/clear
with owner/tenant/version/state checks and authoritative availability/pricing/tax.
Add coupon update/activation/date/minimum/maximum/usage controls safe with
redemption and no deletion of redeemed coupons. Add real bounded XLSX export with
typed money/dates/timezone, formula safety, MIME/disposition, auth/entitlement/
audit and native/web compatibility. Keep saved methods unavailable. Add AUTH,
ASSET, CART, COUPON and REPORT contracts and concurrency/security tests.

Inventory note: phone flows, presigns, AP2 cart mandates, coupons and JSON/CSV
exports exist only partially. The upload completion gap, non-editable cart,
non-serialized coupon usage and JSON-wrapped CSV are the main correctness risks.
Expected owners stay within current auth/commerce/shopping/report handlers,
services, models and repositories, with paired migrations where state/versioning
is added, OpenAPI, and provider/concurrency/security tests. Saved payment methods
remain explicitly unavailable.

## Task 9: AI agent governance

Status: pending

Classify every existing tool into the specified eight risk classes and default
deny missing classifications/permissions. Bind high-risk approvals to agent,
business, user, tool, normalized arguments/hash, resource, impact, time, expiry,
one-time ID and idempotency; argument changes invalidate approval. Enforce run,
daily, business-spend, step, call, duration, retry, cancellation, kill-switch,
circuit-breaker and concurrency-safe spend reservations without silent expensive
fallback. Audit provider/model/config/template/run/tool/risk/approval/sanitized
args/result/usage/cost/retries/failure/disposition without secrets or unnecessary
prompts. Add adversarial tenant, prompt-injection, approval replay/change, budget,
timeout-after-effect, false-success, bargaining, kill-switch and credential tests.
Disable capabilities governance cannot cover and document AI contracts.

Inventory note: current agent capability CRUD is descriptive configuration, not
the specified runtime authorization model. Voice tools are a useful read-only
bounded subset. Expected owners include agent and voice runtime services,
approval/budget/audit models and repositories, paired migrations, kill-switch
configuration, provider-safe tests and AI contracts. Default deny all tools not
classified and disable high-risk execution until governance is proven.

## Task 10: Frontend handoff and release validation

Status: pending

Regenerate Swagger/OpenAPI. Complete every CAP/SUB/OPS/SEC/ACC/IMP/AUTH/ASSET/
CART/COUPON/REPORT/AI contract with exact status, method/path, auth, business,
permission, entitlement, step-up, idempotency, schemas/examples, pagination/file
behavior, stable errors/statuses, states/retries/events/invalidation, rollout,
tests and limitations. Mark internal, blocked, deferred and externally unverified
items. Run route/contract, migration, tenant, sensitive logging, finance/inventory
idempotency, provider unknown-outcome and Terraform format/validate/test reviews.
Create the non-filing GST reconciliation follow-up plan. Run full verification,
inspect the final diff, and ensure no frontend or secret files changed.

Inventory note: `docs/integration/BILLEIF_PHASE_2_FRONTEND_HANDOFF.md` is the
initial evidence-backed handoff, not the final release contract. Task 10 must
update it after behavior changes, regenerate OpenAPI, run the complete required
validation, record external gaps truthfully, and produce the non-filing GST
reconciliation follow-up plan without implementing deferred filing.

## Expected change surface

Expected areas include `internal/app`, `internal/config`, `internal/handlers`,
`internal/middleware`, `internal/models`, `internal/repositories`,
`internal/services`, `internal/workers`, `cmd`, `migrations`,
`infrastructure/terraform`, `.github/workflows`, `openapi`, `docs`, and focused
tests. Each task must narrow this list to the smallest existing owner.

Task 0 touched documentation only. Future tasks must not treat the broad list as
authorization to edit every area; the milestone ledger above names the expected
smallest owners and each implementation task must narrow them again from evidence.

## Rollout, security, and data integrity

All schema work is expand-first and reversible. New infrastructure remains
feature-gated and is validated without apply. Provider functionality remains
unavailable or degraded until configuration and health evidence exist. Public
contracts roll out behind backend authorization, tenant checks, entitlements,
idempotency and stable errors. Recovery never duplicates financial, inventory,
tax, file, notification, or external-message outcomes.
