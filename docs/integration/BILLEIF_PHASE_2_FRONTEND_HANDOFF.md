# Billeif Phase 2 Frontend Handoff

Status: complete for the audited local backend; deployed providers and environment checks remain externally unverified

This document describes only Phase 2-relevant HTTP behavior that is implemented
in the backend at the audited revision. It is not a future API design. `partial`
and `unsafe` contracts are included so clients can recognize existing traffic,
but they are not approval to ship the incomplete workflow. Missing, deferred,
blocked, and externally unverified work is listed without invented methods,
paths, fields, or status codes.

The source of truth for every claim is the cited handler/service/model/test, not
route registration or current generated OpenAPI alone.

## Shared request rules

- Paths below are relative to `/api/v1`.
- Unless marked public, send `Authorization: Bearer <access-token>` and an
  effective business scope. Current middleware accepts `business_id` as a query
  parameter, `business_id` or `X-Business-ID` as a header, a
  `:business_id` path parameter, or a JSON body field. New frontend work should
  use one consistent carrier and never trust a client-side business identifier
  as authorization. Evidence: `internal/middleware/business_auth.go`.
- Permissions shown are enforced by `internal/app/runtime.go`. Branch scope is
  enforced separately where noted. Accounting locked-period overrides use a
  one-time scoped step-up token plus a command identity and reason.
- UUID examples are illustrative. Timestamps are JSON RFC 3339 timestamps.
- Except where an exact stable shape is stated, current failures are
  `{ "error": "..." }`; message text is not a stable machine contract. Branch
  on HTTP status only until a later contract adds stable codes.
- Provider or signed-storage URLs are opaque and short-lived. Never log them or
  persist them as durable identifiers.

## Contract index

| ID | Current contract | Classification | Frontend use at this revision |
| --- | --- | --- | --- |
| CAP-001 | Runtime capability evaluation and mutation preflight | `complete` locally; provider health `externally unverified` | Authoritative source for whether the active user/business/platform may expose or attempt covered functionality; governed service mutations fail closed independently of frontend gating. |
| SUB-001 | Subscription catalog | `complete` locally | Authoritative renewable monthly plan catalog. |
| SUB-002 | Current subscription, lifecycle and feature rows | `complete` locally | Show server lifecycle/period/pending state; runtime capability remains CAP-001. |
| SUB-003 | Razorpay renewable checkout, plan change, cancellation and billing/audit | `complete` locally; provider `externally unverified` | Use for new monthly subscriptions; never infer payment from checkout success. |
| OPS-001 | Invoice render status | `complete` for current safe projection | Pollable. |
| OPS-002 | Invoice delivery create/status | `complete` for current safe projection | Usable with UUID idempotency key; poll status. |
| OPS-003 | In-app notifications | `complete` for current non-paginated contract | Usable; refresh/poll because no versioned event contract is promised here. |
| OPS-004 | Document GST compliance status and commands | `partial`, response `unsafe`, provider `externally unverified` | Keep customer success UI disabled; status can include simulated or provider-internal data. |
| OPS-005 | GST integration accounts and GSTIN lookup | `partial` and `unsafe`, provider `externally unverified` | Internal setup only; never expose credentials, provider metadata, or simulated validation as government proof. |
| OPS-006 | GSTR-2B reconciliation and GST report runs | `partial`; official filing `deferred` | Internal accounting operations only; exports are generated data, not filed returns. |
| OPS-007 | Aggregate business operation list/detail/timeline and safe render retry | `complete` locally; workers/providers `externally unverified` | Use as the business-safe cross-domain status surface. Only exact failed versioned invoice renders advertise and accept customer retry. |
| OPS-008 | Platform operator operation detail/timeline/recovery | Detail/timeline `complete` locally; high-risk recovery `blocked` by fail-closed step-up | Operator UI must require the separately configured operator group. Show high-risk actions as unavailable with `step_up_required`; never substitute owner/admin. |
| SEC-001 | One-use WebSocket ticket | `complete` locally, deployed WebSocket `externally unverified` | Usable where the deployed WebSocket is separately verified. |
| ACC-001 | Fiscal policy, chart of accounts, journal/opening posting and banking reconciliation | `complete` locally | Use authoritative account classes/hierarchy and locked-period override contract; bank statement upload/storage is externally unverified. |
| ACC-002 | Trial Balance, Balance Sheet, account drilldown and read-only reconciliation diagnostics | `complete` locally | Use required currency and posted-ledger minor-unit results; exports/shares reuse REPORT-001. |
| AUTH-001 | Indian phone OTP auth | `implemented`, Cognito/SMS `externally unverified` | Ship only after staging OTP delivery, expiry, replay, refresh and global-logout verification. |
| ASSET-001 | Verified business-logo upload and drive presigns | Logo `complete` locally; drive remains `partial` and `unsafe`; S3 `externally unverified` | Mark a logo ready only after its completion response. A drive PUT still is not completion proof. |
| IMP-001 | Durable customer/vendor/product import | `complete` locally; S3/SQS deployment `externally unverified` | Enable after migration `000059` and worker deployment verification. |
| CART-001 | Tenant-versioned editable AP2 cart mandate | `complete` locally | Mutate only `pending` carts with the current version; refetch on `409`. |
| COUPON-001 | Storefront coupon controls and serialized redemption | `complete` locally | Hard usage caps require migration `000060`; redeemed coupons remain immutable to deletion. |
| REPORT-001 | Report JSON/CSV compatibility envelope and native XLSX download | `complete` locally | XLSX is bounded, formula-safe, typed, business-timezone aware and returned as an attachment; runtime capability and permission checks remain mandatory. |
| AI-001 | Agent capability list/add/remove and permission probe | `partial` and `unsafe` as governance | Capability inventory only; do not treat it as execution authorization. Governed runtime authority is internal and default-disabled. |

## Cross-contract controls and client lifecycle

The detailed sections below own exact schemas and examples. These matrices make
the remaining required handoff properties explicit rather than leaving an
omitted column to imply support.

| ID | Auth and business scope | Permission | Entitlement | Step-up | Idempotency |
| --- | --- | --- | --- | --- | --- |
| CAP-001 | Bearer + effective business; setup, entitlement, quota, permission, and GST snapshot reads are business-scoped, while global provider facts contain no tenant data | No endpoint-wide permission; each result evaluates its listed operation permission | Each result evaluates the current subscription catalog feature where applicable | None; read-only | Read-only and safe to repeat |
| SUB-001 | Bearer + effective business | `subscriptions.view` | None | None implemented | Read-only |
| SUB-002 | Bearer + effective business | `subscriptions.view`; legacy feature sync uses `subscriptions.manage` | Verified current subscription period is the entitlement/quota source | None implemented | Reads are safe; direct subscription mutation routes are disabled |
| SUB-003 | Bearer + effective business + all branches | Mutations `subscriptions.manage`; history/audit `subscriptions.view` | Paid access changes only on signed verified boundary events | None implemented | Required body `idempotency_key` per actor/business/action; changed reuse conflicts |
| OPS-001 | Bearer + effective business + all branches | `documents.export` | None | None implemented | Read-only |
| OPS-002 | Bearer + effective business + all branches | Create `documents.manage`; read `documents.export` | None | None implemented | Required UUID `Idempotency-Key`; same actor/payload replays, changed payload conflicts |
| OPS-003 | Bearer + effective business + authenticated user | No additional permission | None | None implemented | Source ingestion is unique; read mutations have no client command key and are naturally convergent |
| OPS-004 | Bearer + effective business + all branches | Reads/PDF `documents.export`; mutations `documents.manage` | Generate e-invoice/e-way bill enforces its feature and monthly quota; other commands do not | None implemented | Required `Idempotency-Key`; key is business-global and a changed operation/payload can replay the first job |
| OPS-005 | Bearer + effective business | GSTIN lookup `reports.view`; account CRUD/validate `tax.integrations.manage` + all branches | None | None implemented | Reads safe; account writes/validate have no command key |
| OPS-006 | Bearer + effective business + all branches | Import `documents.manage`; reports/read `reports.view`; export `reports.export` | None | None implemented | Import deduplicates identical payload/period/source but has no client key; every export creates a run |
| OPS-007 | Bearer + effective business + all branches | Reads need no additional permission; safe render retry requires `documents.manage` in the service | Runtime invoice queue, invoice bucket, SQS and S3 clients are re-evaluated before accepting retry | None for exact failed render retry | Body UUID `idempotency_key` is scoped to actor/business/action; changed reuse and a second command for the same operation revision return `unsafe_replay` |
| OPS-008 | Bearer + separately configured verified JWT group; original `business_id` is mandatory | Business owner/admin roles are ignored | No provider capability is exercised while step-up is blocked | High-risk reconcile/webhook/DLQ/resolve commands require Task 5 one-time scoped step-up and currently return `step_up_required` | Every operator decision is durably audited with tenant, actor, operation, action, reason, command identity, correlation and safe result code |
| SEC-001 | Bearer + effective business + authenticated user | No additional permission | None | None implemented | Ticket is one-use; issue has no command key |
| ACC-001 | Bearer + effective business + all branches | Reads `reports.view`; fiscal/account writes `accounting.manage`; bank writes `banking.manage`; posting document/payment routes also retain their domain permission | None | Locked-date postings require `X-Step-Up-Token`, `X-Lock-Override-Reason`, and a scoped `Idempotency-Key`; `428` code `accounting_period_locked` otherwise | Opening and statement imports bind payload hashes to tenant command keys; an override is one-time and scoped to resource/date/subject |
| ACC-002 | Bearer + effective business + branch authorization | `reports.view`; export `reports.export`; share `reports.share` | Existing report capability checks apply to export/share | None; read-only | Query/read safe; every export/share creates its existing report record |
| AUTH-001 | Register/confirm/resend/login/verify/refresh are public; link/link-confirm/logout require bearer only | No business scope; linking binds the resolved authenticated database user | None | Durable hashed-subject security audit | Provider OTP expiry/replay controls apply; explicit linking never mutates before OTP confirmation |
| ASSET-001 | Logo: bearer + effective business + owned path business + uploader-bound upload; drive: bearer + effective business | Logo owner check; drive `drive.manage` | Logo none; drive `drive_storage_mb`, measured from tenant drive assets in MB | None implemented | Logo upload ID is idempotent and never reattaches after supersession; drive has no command key |
| IMP-001 | Bearer + effective business + authenticated user; jobs are uploader-bound for validation/commit/cancel | Customer `customers.create`; vendor `vendors.create`; product `products.manage` | CAP-001 `bulk_imports` plus business setup and the matching permission | None | Validation replays by tenant/uploader/upload; commit requires a UUID `Idempotency-Key`; identical retry republishes safely |
| CART-001 | Bearer + effective business; cart mutations require matching user, business and owned shopping-agent scope | No additional permission | None | None implemented | Required body `version` is an optimistic command precondition; stale/state conflicts return `409` |
| COUPON-001 | Management: bearer + effective business; validation: public slug route with rate limits | View `storefront.view`; writes/delete `storefront.manage`; validation public | None enforced on coupon routes | None implemented | Update accepts optional `version`; redemption and management changes serialize on the coupon row |
| REPORT-001 | Bearer + effective business + branch/warehouse scope | `reports.export` | `export_documents` through CAP-001 | None implemented | No command key; every accepted request creates a report run |
| AI-001 | Bearer + effective business; same-business access passes the current ownership helper | No additional permission | None | None implemented | Add/remove have no command/version key; reads are safe |

| ID | Pagination / file behavior | State and retry | Event / invalidation | Existing migration and rollout | Proof and governing limitation |
| --- | --- | --- | --- | --- | --- |
| CAP-001 | Fixed 13-item array; no pagination or file | Synchronous observed-state evaluation; reads are retry-safe; honor `retry_at` for temporary provider failures | A fixed global observer refreshes AI and shared Razorpay/storefront health; explicit GST validation and real GST outcomes update the business GST snapshot; refetch after readiness, subscription, permission, credential, or setup changes | Expand first with `migrations/000054_capability_provider_health_snapshots.up.sql`, then deploy the application | Capability, fixed-global-observer, durable-GST-repository, guarded-mutation, quota, handler, migration-bundle, and route tests beside their owners; no tenant census or rotating cache exists |
| SUB-001 | No pagination or file | Synchronous retry-safe catalog read | Invalidate on catalog-version/deployment change | Code-defined catalog | `internal/services/subscription_catalog_test.go` |
| SUB-002 | No pagination or file | Ten lifecycle states; free/legacy/renewable billing modes; refetch after commands/provider boundaries | No versioned client event; poll with bounded backoff | Expand-first `000055_subscription_lifecycle`; legacy rows preserved | lifecycle/catalog/entitlement/quota tests beside owners |
| SUB-003 | History/audit `limit` 1-100, default 50; no file | Idempotent mutations; ambiguous outcomes stay reconciliation-required | No client event; invalidate SUB-002/history after verified changes | Deploy `000055`, application, then enable five-minute worker; provider rollout unverified | lifecycle/provider/race/worker tests and mocked `razorpay.tftest.hcl` |
| OPS-001 | No pagination or file; PDF download is separate | Poll `queued`/`processing`; stop on `completed`/`failed`/`obsolete` | No versioned client event; invalidate invoice PDF/download state on terminal state | Existing render/outbox migrations; deployed worker unverified | `tests/unit/invoice_service_test.go`, `internal/handlers/invoice_download_handler_test.go`, `internal/workers/pdf_renderer_snapshot_test.go`, `infrastructure/terraform/tests/outbox.tftest.hcl`; no safe retry API |
| OPS-002 | No pagination or file | Reuse same key after timeout; poll until terminal | No versioned client event promised; invalidate invoice and delivery queries on change | Existing canonical delivery/outbox schema; SES/deployed worker unverified | `internal/services/invoice_delivery_test.go`, `internal/handlers/invoice_delivery_handler_test.go`, `internal/repositories/postgres/invoice_delivery_repo_test.go`; no customer retry action or provider detail |
| OPS-003 | Limit only, maximum 200; no cursor/file | Read operations are retry-safe | No versioned notification-event name promised; invalidate list after mark-read | `migrations/000051_notifications.up.sql`; deployed delivery unverified | `internal/handlers/notification_handler_test.go`, `internal/services/notification_service_test.go`, `internal/repositories/postgres/notification_repo_test.go`; capped non-cursor list |
| OPS-004 | No pagination; PDF fetch returns `{"pdf_url":"..."}` JSON rather than file bytes | Poll job states; reuse the same key only for the identical command | No versioned event; invalidate compliance status while polling | `migrations/000027_add_gst_compliance.up.sql`, `migrations/000031_add_gst_execution_and_pos.up.sql`; provider/queue unverified | `internal/services/tax_compliance_service_test.go`, `tests/unit/document_handler_test.go`; unsafe projection and simulator fallback |
| OPS-005 | Account list is an unpaginated array; GSTIN lookup has no file | Reads safe; account mutation result is synchronous | No event; refetch account list after write/validate | `migrations/000027_add_gst_compliance.up.sql`, `migrations/000031_add_gst_execution_and_pos.up.sql`; provider unverified | `internal/services/tax_compliance_service_test.go`, `tests/unit/tax_handler_test.go`; simulated validation and raw metadata possible |
| OPS-006 | No pagination/file contract; payloads are JSON values/encoded strings | Import is synchronous `processed`; report run is immediately `completed` or `failed` | No event; refetch selected run | `migrations/000027_add_gst_compliance.up.sql`; official filing deferred | `internal/services/tax_compliance_service_test.go`, `tests/unit/tax_handler_test.go`; no filing acknowledgement |
| OPS-007 | List `limit` 1-100, default 50; filter-bound opaque cursor; timeline limit 1-100, default 50; no file | `queued`, `in_progress`, `succeeded`, `failed`, `reconciliation_required`, `unknown`; source failures are reported in `unavailable_types` | No client event is promised; use bounded polling and retain the same cursor filters | Expand-first paired `000056_operation_recovery`; deploy migration before application | Service/repository/handler recovery, tenant, sanitization, pagination and race tests plus generated Swagger and both OpenAPI files |
| OPS-008 | Detail and timeline are single-resource reads; timeline limit 1-100; no file | Detail remains sanitized. High-risk commands are audited rejections until Task 5; no provider call, message, or DLQ redrive occurs | No operator event is promised; refetch timeline after each definitive response | `PLATFORM_OPERATOR_GROUP` has no default and fails closed; no Cognito mutation was performed | `internal/middleware/operator_auth_test.go`, operator service/handler tests, migration audit tests, and mocked Terraform alarm tests |
| SEC-001 | No pagination/file | Never reuse; issue another after expiry/failure | Successful connect owns later channel behavior; no issuance event | `migrations/000050_websocket_tickets.up.sql`; deployed WebSocket unverified | `internal/services/websocket_ticket_service_test.go`, `internal/repositories/postgres/websocket_ticket_repo_test.go`, `internal/handlers/websocket_ticket_handler_test.go`, `tests/unit/websocket_handler_test.go`; at most 60-second TTL |
| ACC-001 | Account/bank/audit lists are bounded unpaginated responses; bank import parses at most 10,000 CSV rows from verified pending upload metadata | Journal states remain draft/posted/reversed; reversal follows configured blocked/next-open policy; statement pending/reconciled | No event; invalidate policy/accounts/journal/ledger/bank state after confirmed mutation | Expand-first paired `000058_accounting_completeness`; deploy migration before application | Journal lock/override/reversal/branch tests, accounting migration tests, payment invariants, route-permission tests; S3 upload path externally unverified |
| ACC-002 | Report query uses standard page/limit and CSV export/share behavior; `currency` is required for accounting statements/diagnostics | Posted-ledger read-only projection; safe to repeat; diagnostics never repair | No event; invalidate after any posting/reversal/opening/bank adjustment | `000058` backfills branch and authoritative account metadata before enabling reports | PostgreSQL reporting query tests, service reconciliation checks, generated Swagger and both OpenAPI files |
| AUTH-001 | No pagination/file | OTP starts use IP/target rate limits plus durable provider-purpose cooldown; public resend and login do not expose account existence | Refetch profile after link; replace tokens after refresh; clear every local token after logout | Existing phone uniqueness migration; Cognito/SMS rollout unverified | Focused service/handler tests cover E.164, collision, explicit linking, provider error normalization and expiry/replay classifications |
| ASSET-001 | Logo uses opaque pending upload ID and exact signed headers; drive remains unchanged | Logo create may be retried with a fresh upload; completion is idempotent for the same upload ID and reports `cleanup_pending` without rolling back the new reference | No asset-ready event; invalidate business profile only after logo completion says `ready` | Existing security pending-upload schema and `infrastructure/terraform/s3.tf`; S3 externally unverified | `internal/services/business_logo_service_test.go`, pending-upload/S3 tests, scoped IAM test; drive still lacks completion and version control |
| IMP-001 | Upload uses the verified pending-upload flow; validate body is JSON; list page/limit; result is an authorized job artifact | `validating`, `validated`, `commit_queued`, `committing`, `completed`, `failed`, `canceled`, `expired`; retry same commands after ambiguity | Completion creates an idempotent in-app notification; poll job detail with bounded backoff | Expand-first paired `000059`; deploy application and bulk-import worker before enabling | Service restart/idempotency/formula/tenant tests, route-permission tests, migration tests, worker partial-batch tests, mocked Terraform topology |
| CART-001 | Cart list uses page/limit; no file | Only `pending`, unexpired, unpaid carts are editable; every mutation increments `version` and refreshes availability, stock, price and tax | No event; replace the cached cart with the mutation response or refetch after conflict | Expand-first `000060` after existing AP2/signature migrations | Signature/service/repository tests cover tenant, stale version and authoritative totals |
| COUPON-001 | List is an array without pagination; no file | Activation/date/minimum/discount/total/per-customer limits are validated; checkout locks through redemption | No event; invalidate coupon list after management writes or redemption-visible order changes | Expand-first `000060` changes redemption FK to restrict deletion and backfills counts | Service tests plus PostgreSQL concurrent checkout prove one redemption at a hard cap of one |
| REPORT-001 | Input page/limit; JSON/CSV retain their envelope; XLSX is a native attachment capped at 500 rows, 64 columns, 32,767 characters per cell and 8 MiB uncompressed worksheet XML | Synchronous completed run; repeat creates another run | No export event; `X-Report-Run-ID` identifies the durable audit record | Existing `migrations/000029_add_projects_and_reporting.up.sql`; no schema rollout | Spreadsheet/service/handler/route tests cover typing, timezone, formulas, bounds, headers, permission and entitlement; no export idempotency |
| AI-001 | Capability list is an unpaginated array; no file | Descriptive rows have no execution lifecycle; writes unsafe to retry | No event; refetch capability list after a confirmed write | `migrations/000013_add_ap2_agent_marketplace.up.sql`; AI providers unverified | `tests/unit/agent_handler_test.go`, `tests/integration/agent_test.go`; permission probe is not authorization |

## CAP-001: Runtime capability evaluation

`GET /capabilities?platform=web`

| Property | Contract |
| --- | --- |
| Status | Implemented locally; live provider observations are externally unverified |
| Purpose | Backend-authoritative availability for the active business, user, subscription and client platform |
| Authentication and business scope | Bearer token plus effective business; cross-business selection is rejected by business-membership middleware |
| Endpoint permission | None beyond current membership because the response applies per-capability permissions and does not reveal provider internals |
| Entitlement | Per capability; see mapping below |
| Step-up | None; read-only |
| Request | No body; optional `platform` is `web` (default), `ios`, or `android` |
| Response | `200` `CapabilityList` below |
| Pagination and file behavior | Fixed 13-item array; no pagination or file |
| Idempotency and retry | Read-only and safe to repeat; for `temporarily_unavailable`, wait until `retry_at` when supplied |
| State transitions | This GET performs no transition. Re-evaluation may change after a global observation, explicit GST credential validation, real GST operation, subscription/quota, permission, setup, or platform change |
| Event and invalidation | No event in Task 1. Invalidate after subscription, permission, business setup or provider-readiness changes |
| Migration and rollout | Apply expand-first migration `000054_capability_provider_health_snapshots`, deploy the backend, then enable frontend gating. Roll back application code before the optional down migration, which removes only derived GST health snapshots |

Capability keys are `razorpay_payments`, `gst_provider`, `e_invoice`,
`e_way_bill`, `whatsapp_messaging`, `email_delivery`, `s3_uploads`, `voice`,
`ai`, `storefront_payments`, `report_exports`, `bulk_imports`, and
`saved_payment_methods`. The response order is stable in that order.

Each item contains these exact fields: `key`; `product_support.supported`;
`configuration.required` and `configuration.configured`;
`provider_health.status`, optional `provider_health.observed_at`, and
`provider_health.stale`; `entitlement.required` and `entitlement.entitled`;
optional `quota.unit`, `quota.limited`, `quota.limit`, `quota.used`,
`quota.remaining`, and `quota.available`; `permission.required`, optional `permission.key`, and
`permission.granted`; `business_setup.required` and `business_setup.complete`;
`platform.requested`, `platform.supported`, and `platform.supported_platforms`;
`available`; `state`; `reason_code`; optional `setup_action`; optional
`retry_at`; optional customer-safe `degradation.code` and
`degradation.message`; and `evaluated_at`. The envelope contains
`business_id`, `platform`, `evaluated_at`, and `capabilities`.

```json
{
  "business_id": "22222222-2222-4222-8222-222222222222",
  "platform": "web",
  "evaluated_at": "2026-09-01T12:00:00Z",
  "capabilities": [
    {
      "key": "e_invoice",
      "product_support": {"supported": true},
      "configuration": {"required": true, "configured": true},
      "provider_health": {
        "status": "unavailable",
        "observed_at": "2026-09-01T11:59:45Z",
        "stale": false
      },
      "entitlement": {"required": true, "entitled": true},
      "quota": {"limited": true, "limit": 100, "used": 20, "remaining": 80, "available": true},
      "permission": {"required": true, "key": "documents.manage", "granted": true},
      "business_setup": {"required": true, "complete": true},
      "platform": {"requested": "web", "supported": true, "supported_platforms": ["web", "ios", "android"]},
      "available": false,
      "state": "temporarily_unavailable",
      "reason_code": "provider_temporarily_unavailable",
      "retry_at": "2026-09-01T12:01:00Z",
      "degradation": {"code": "provider_unavailable", "message": "Provider service is temporarily unavailable."},
      "evaluated_at": "2026-09-01T12:00:00Z"
    }
  ]
}
```

States are `available`, `setup_required`, `upgrade_required`,
`quota_exhausted`, `permission_denied`, `temporarily_unavailable`,
`unsupported_platform`, `unsupported`, and `unknown`. Stable reason codes are
`available`, `capability_unknown`, `provider_not_configured`,
`provider_health_unknown`, `provider_health_stale`,
`provider_temporarily_unavailable`, `provider_degraded`,
`entitlement_required`, `quota_exhausted`, `permission_required`,
`business_setup_required`, `platform_unsupported`,
`saved_payment_methods_unsupported`, and `bulk_import_processor_unavailable`.
Governed mutations may additionally return `capability_evaluation_failed` with
state `unknown` when the evaluator itself cannot read an authoritative fact.
Stable setup actions are `contact_support`, `configure_gst`,
`configure_whatsapp`, `configure_email`, `enable_voice`,
`enable_storefront_payments`, `complete_business_setup`,
`upgrade_subscription`, `request_permission`, and
`validate_gst_integration`. Missing, stale, or unavailable GST health uses
`validate_gst_integration`; missing GST credentials still uses `configure_gst`.

Permission mapping is: Razorpay and saved methods `payments.manage`; GST setup
`tax.integrations.manage`; e-invoice/e-way bill `documents.manage`; WhatsApp
and email `notifications.manage`; S3 `drive.manage`; voice `voice:use`; AI
`agents.view`; storefront payment `storefront.manage`; report export
`reports.export`. Bulk import reports no aggregate permission because the
legacy import paths use inconsistent per-type permissions; it remains
unavailable before any permission could make it usable. Entitlement mapping is:
GST provider `gst_api`; e-invoice `einvoice`;
e-way bill `ewaybill`; WhatsApp `whatsapp_notifications`; S3
`drive_storage_mb`; storefront payments `online_store`; report exports
`export_documents`. Other listed capabilities have no current plan gate.

Evaluation precedence is product support, platform, backend configuration,
entitlement, quota, permission, business setup, then observed provider health.
All underlying facts remain present even when an earlier fact determines the
final state. A health failure therefore never changes `entitlement.entitled`.
Configured means only that a binding exists; it never means healthy. Missing,
stale, or unknown provider health fails closed. A fresh `degraded` observation
may remain `available` with `reason_code: "provider_degraded"` and a safe
degradation object.

For `s3_uploads`, `quota.unit` is `MB`; `limit` is the plan's storage MB,
`used` rounds tenant drive-asset bytes up to the next MB, and `remaining`
rounds remaining bytes down to whole MB. `available` becomes false at the exact
byte limit. The read endpoint never reserves storage. Drive presign enforcement
uses this same tenant-scoped reader and rejects an upload that would exceed the
byte limit before creating a drive asset or calling S3.

Web voice is always `unsupported_platform`; saved payment methods are always
`unsupported`. Bulk imports are available only when business setup, entitlement,
quota, permission, and deployed worker dependencies pass CAP-001. The legacy
simulated GST provider never satisfies configuration.
Customer JSON cannot contain provider credentials, provider account IDs, secret
identifiers, raw provider errors, or infrastructure topology.

The same evaluation is a service-layer preflight, not merely frontend advice.
Report export; Razorpay plan/storefront order creation; e-invoice and e-way
generation/cancellation/update commands; drive upload initiation; mobile voice
session creation; business LLM chat/agent-assist; bulk-import intake; and saved
payment-method add/default/delete/token mutations are rejected before their
database, queue, reservation, encryption, or provider effect when the relevant
capability is unavailable. The preflight does not replace transactional
e-invoice/e-way quota reservation or upload quota enforcement.

Governed mutations return a stable customer-safe body:

```json
{
  "code": "capability_unavailable",
  "error": "capability e_invoice is unavailable: provider_health_unknown",
  "capability": "e_invoice",
  "state": "unknown",
  "reason_code": "provider_health_unknown",
  "setup_action": "validate_gst_integration",
  "retry_at": "2026-09-01T12:01:00Z"
}
```

`setup_action` and `retry_at` are optional. Status is `403` for setup, upgrade,
or permission states; `422` for unsupported product/platform; `429` for quota;
and `503` for unknown or temporarily unavailable. Clients must branch on
`state`/`reason_code`, never parse `error`.

Stable endpoint errors introduced by this handler are
`400 {"error":{"code":"invalid_platform","message":"Platform must be web, ios, or android."}}`,
`403 {"error":{"code":"business_scope_required","message":"An active business is required."}}`,
`503 {"error":{"code":"capability_service_unavailable","message":"Capability evaluation is unavailable."}}`,
and `500 {"error":{"code":"capability_evaluation_failed","message":"Capability evaluation failed."}}`.
Authentication and cross-business membership may be rejected earlier by shared
middleware with HTTP `401` or `403` and the shared legacy error envelope.

Provider-health scope is explicit. Razorpay and AI are configured, truly global
provider-network observations held in a fixed two-key process-local cache. The
global cache API accepts no business identifier and contains no setup,
entitlement, permission, quota, credential, account, or raw-error field. HTTP
runtime startup launches a non-overlapping asynchronous observer that probes
each configured global provider once per cycle, with at most two workers,
five-second probe timeouts, two attempts, a 30-second cycle deadline, and a
two-minute maximum refresh interval. It performs no tenant discovery, census,
table count, rotation, or business-keyed cache write.

GST health is business-specific and durable in
`capability_provider_health_snapshots`, keyed by `(business_id, gst_provider)`.
The row contains only internal account/revision consistency keys, a bounded
status, observation/freshness/retry timestamps, and a customer-safe code; the
primary key and provider constraint bound it to one row per business GST key.
Credential upsert atomically advances active account revisions and invalidates
the earlier observation without validating or marking the provider healthy.
The explicit `POST /tax/integrations/{id}/validate` action validates the stored
tenant credentials and records account status plus the sanitized result in one
revision-bound transaction. Real e-invoice/e-way calls use that same tenant
credential revision and persist provider outcomes using a detached bounded
context. Every accepted completion advances a database-ordered observation
revision; an old credential revision is rejected explicitly. Each observation
is fresh for 24 hours. A missing or stale row stays fail-closed and directs the
user to `validate_gst_integration`.
`GET /capabilities` performs only exact business-keyed snapshot reads and never
contacts a provider.

The AI probe recognizes only explicitly supported official OpenAI/DeepSeek
hosts and their exact configured `/chat/completions` or
`/v1/chat/completions` shapes, then uses the provider's read-only `GET /models`
or `GET /v1/models` complete-list contract. Custom endpoints are not probed. A
response proves health or model absence only after a terminal JSON HTTP `200`
and a complete recognized `object: "list"` and
`data` array streamed within the 1 MiB and 10,000-entry bounds. Only complete
presence is healthy and only complete absence is unavailable.
Empty/unrecognized objects, malformed JSON or entries, pagination/partial
or unrecognized response headers, other successful statuses, oversized bodies,
scan-cap exhaustion, unknown URL shapes, and `404`/`405` routes record nothing
and remain unknown. Completeness is never inferred from the absence of a known
pagination header. The probe never decodes an unbounded array, logs or returns the body,
or sends a chat mutation.

Configuration presence alone never writes healthy. A Lambda cold start may
briefly lack global Razorpay/AI observations, and instances may temporarily
disagree about those two global facts; durable GST observations survive process
turnover. No safe producer was added for S3 because presign success does not
prove object-store health; voice admission does not prove the downstream
media/runtime path; and WhatsApp/email lack a safe probe here. Those providers
remain unknown until a future producer exists. No live provider is claimed
healthy by local tests. No internal diagnostics endpoint was added because the
repository has no operator principal distinct from business owner/admin.

Observer shutdown cancels and joins in-flight global probes before
database/provider dependencies close; repeated start/stop generations cannot
overlap. A failed recurring cycle produces one bounded stable issue code and
waits for the normal interval. Cycle reporting never includes raw
database/provider errors, credentials, identifiers, or response bodies.

## SUB-001: Subscription catalog

`GET /subscriptions/catalog`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token plus effective business |
| Permission | `subscriptions.view` |
| Entitlement / step-up / idempotency | None |
| Success | `200` `SubscriptionCatalogResponse` |
| Pagination | None |
| Invalidation | Refetch after deployment/catalog version change; no event contract |
| Evidence | `internal/app/runtime.go`, `internal/handlers/commerce_handler.go`, `internal/services/subscription_catalog.go`, `internal/services/subscription_catalog_test.go` |

Exact top-level fields are `version` and `plans`. Every plan has `id`,
`display_name`, `legacy_plan`, `plan_code`, `currency`, `amount`, `interval`,
`features`, and `quotas`. `amount` is integer paise and `currency` is `INR`.

| id | display_name | legacy_plan | plan_code | amount | interval | invoices / customers / users / storage_mb |
| --- | --- | --- | --- | ---: | --- | --- |
| `free` | `Free` | `free` | `free` | 0 | `month` | 10 / 10 / 3 / 100 |
| `pro_monthly` | `Pro` | `starter` | `pro` | 29900 | `month` | 100 / 100 / 3 / 512 |
| `rise_monthly` | `Rise` | `professional` | `rise` | 99900 | `month` | 1000 / 1000 / 10 / 2048 |
| `biz_monthly` | `Biz` | `enterprise` | `biz` | 299900 | `month` | 100000 / 100000 / 50 / 10240 |

All plans also contain `einvoice_monthly` and `ewaybill_monthly` quotas equal to
the invoice quota. Feature keys are `einvoice`, `ewaybill`, `bulk_gst`,
`gst_api`, `pos`, `online_store`, `multi_currency`, `export_documents`,
`sez_documents`, `deemed_export_documents`, `multi_user`, `custom_roles`,
`multi_business`, `branches`, `priority_support`, `drive_storage_mb`, and
`whatsapp_notifications`. All are false on `free` and true on the paid plans.

Example excerpt:

```json
{
  "version": "swipe-v1",
  "plans": [
    {
      "id": "pro_monthly",
      "display_name": "Pro",
      "legacy_plan": "starter",
      "plan_code": "pro",
      "currency": "INR",
      "amount": 29900,
      "interval": "month",
      "features": { "einvoice": true, "ewaybill": true },
      "quotas": {
        "invoices": 100,
        "customers": 100,
        "users": 3,
        "storage_mb": 512,
        "einvoice_monthly": 100,
        "ewaybill_monthly": 100
      }
    }
  ]
}
```

`interval: "month"` is a renewable interval for new checkouts through SUB-003.
Previously successful plan checkouts remain fixed-period `legacy_one_time`
purchases and are never silently relabelled or renewed.

## SUB-002: Current subscription and feature rows

`GET /subscriptions`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token plus effective business |
| Permission | `subscriptions.view` |
| Success | `200` subscription object; `404 {"error":"subscription not found"}` |
| Evidence | `internal/handlers/subscription_handler.go`, `internal/models/subscription.go`, `tests/unit/subscription_service_test.go`, `internal/services/entitlements_test.go` |

Exact fields are `id`, `business_id`, `plan`, optional `plan_code`, optional
`catalog_version`, `status`, `billing_mode`, quota maxima, `start_date`, optional
`end_date`, optional `next_billing_date`, optional `period_start`, optional
`period_end`, optional `next_renewal_at`, optional `grace_deadline`,
`cancel_at_period_end`, optional `cancellation_effective_at`, optional
`cancelled_at`, optional `pending_plan_id`, optional
`pending_plan_effective_at`, optional sanitized `reconciliation_code`,
`lifecycle_version`, `created_at`, and `updated_at`. Provider customer,
subscription, plan, mode, event, payment and invoice identifiers are internal.

Lifecycle states are `pending_payment`, `active`, `renewal_pending`, `past_due`,
`grace_period`, `cancellation_scheduled`, `cancelled`, `expired`, `suspended`,
and `reconciliation_required`. Billing modes are `free`, `legacy_one_time`, and
`renewable`. Paid access is projected only from a verified current period;
past-due/grace access ends at `grace_deadline`, and suspended, cancelled,
expired, or unpaid pending subscriptions project the free plan.

```json
{
  "id": "11111111-1111-4111-8111-111111111111",
  "business_id": "22222222-2222-4222-8222-222222222222",
  "plan": "starter",
  "plan_code": "pro",
  "catalog_version": "swipe-v1",
  "status": "active",
  "billing_mode": "renewable",
  "max_invoices": 100,
  "max_customers": 100,
  "max_users": 3,
  "max_storage_mb": 512,
  "start_date": "2026-09-01T12:00:00Z",
  "end_date": "2026-10-01T12:00:00Z",
  "next_billing_date": "2026-10-01T12:00:00Z",
  "period_start": "2026-09-01T12:00:00Z",
  "period_end": "2026-10-01T12:00:00Z",
  "next_renewal_at": "2026-10-01T12:00:00Z",
  "cancel_at_period_end": false,
  "lifecycle_version": 4,
  "created_at": "2026-09-01T12:00:00Z",
  "updated_at": "2026-09-01T12:00:00Z"
}
```

`GET /subscriptions/entitlements` and
`POST /subscriptions/entitlements/sync` return an array of legacy feature rows.
The GET requires `subscriptions.view`; sync requires `subscriptions.manage`.
Each row has `id`, `business_id`, `feature_key`, `enabled`, optional
`limit_value`, optional `metadata` (a JSON-encoded string), `created_at`, and
`updated_at`. These rows are not the Task 1 runtime-capability response.

The internal entitlement resolver separately has this exact projection:

```json
{
  "plan_id": "pro_monthly",
  "einvoice_enabled": true,
  "einvoice_limit": 100,
  "ewaybill_enabled": true,
  "ewaybill_limit": 100,
  "bulk_gst_enabled": true,
  "gst_api_enabled": true,
  "pos_enabled": true
}
```

It is enforced by relevant operations but is not returned by the two legacy
feature-row endpoints. Stable enforcement failures, where used, are:

```json
{ "code": "feature_disabled", "error": "...", "feature": "einvoice", "plan_id": "free" }
```

```json
{ "code": "quota_exceeded", "error": "...", "feature": "einvoice", "limit": 100, "used": 100, "plan_id": "pro_monthly" }
```

Evidence: `internal/models/commerce.go`, `internal/services/commerce_service.go`,
`internal/services/entitlements.go`, `internal/services/entitlements_test.go`,
and `internal/services/commerce_service_test.go`.

Direct `POST /subscriptions` and `PUT /subscriptions` mutations are not
registered. Use the idempotent lifecycle commands in SUB-003.

## SUB-003: Razorpay renewable subscription lifecycle

New monthly plans use these endpoints:

- `POST /subscriptions/checkout`
- `POST /subscriptions/plan-change`
- `POST /subscriptions/cancellation`
- `GET /subscriptions/billing-history?limit=50`
- `GET /subscriptions/audit?limit=50`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token, effective business, all-branches scope |
| Permission | Mutations `subscriptions.manage`; history/audit `subscriptions.view` |
| Step-up / entitlement | None |
| Idempotency | Every mutation requires body `idempotency_key`; scoped to actor, business and action; changed reuse conflicts; successful checkout replay returns the immutable original pending response even after later activation |
| Success | `200` |
| Provider state | Locally verified against the official adapter; live provider deployment remains `externally unverified` |
| Evidence | `internal/handlers/subscription_handler.go`, `internal/services/subscription_lifecycle_service.go`, `internal/repositories/postgres/subscription_lifecycle_repo.go`, `pkg/razorpay/client.go`, `internal/services/subscription_lifecycle_test.go`, migration `000055_subscription_lifecycle` |

Checkout request and response:

```json
{
  "plan_id": "pro_monthly",
  "idempotency_key": "33333333-3333-4333-8333-333333333333"
}
```

```json
{
  "subscription_id": "44444444-4444-4444-8444-444444444444",
  "status": "pending_payment",
  "billing_mode": "renewable",
  "pending_plan_id": "pro_monthly",
  "authorization_url": "https://opaque-provider-authorization.example"
}
```

The authorization URL is opaque. Pending checkout does not grant paid
entitlements. Restarting an expired or cancelled lifecycle first resets the
aggregate to a clean free/pending baseline; no prior plan, quota, period,
provider clock, grace, or cancellation field carries into the new checkout.
`POST /payments/razorpay/order` now rejects `target_type: "plan"`;
it remains available only for authenticated store-order payments.

Plan-change request:

```json
{
  "plan_id": "rise_monthly",
  "idempotency_key": "55555555-5555-4555-8555-555555555555"
}
```

Cancellation request is `{ "idempotency_key": "..." }`. Both return:

```json
{
  "subscription_id": "44444444-4444-4444-8444-444444444444",
  "status": "renewal_pending",
  "plan_id": "pro_monthly",
  "pending_plan_id": "rise_monthly",
  "effective_at": "2026-10-01T12:00:00Z",
  "cancel_at_period_end": false,
  "reconciliation_required": false
}
```

There is no proration. Plan changes take effect only when the matching signed
provider charge starts exactly at the stored verified boundary and advances the
period monotonically. Cancellation is scheduled at
the current paid period end. Concurrent plan change/cancellation conflicts are
serialized. A downgrade above the target quota keeps existing rows and becomes
restricted after the boundary rather than deleting data. A provider-confirmed
immediate cancellation persists terminal state, command completion, and audit
in one transaction.

Billing history returns `{ "records": [...] }`. Each record exposes `id`,
`business_id`, `subscription_id`, integer `amount_minor`, `currency`, `status`,
`receipt_reference`, optional period/quota-window timestamps, `occurred_at`, and
`created_at`. Audit has the same envelope and exposes sanitized lifecycle
action/status/plan/code/timestamps. Neither surface returns provider IDs.
The optional history `limit` defaults to `50` and must be an integer from `1`
through `100`; malformed or out-of-range values return `400 subscription_invalid_limit`.

Stable lifecycle errors are:

```json
{"error":{"code":"subscription_invalid_request","message":"subscription request could not be processed"}}
```

Mutation-internal and history-internal failures have distinct safe responses:

```json
{"error":{"code":"subscription_mutation_internal_error","message":"subscription request could not be completed"}}
{"error":{"code":"subscription_internal_error","message":"subscription history could not be loaded"}}
```

- `400 subscription_invalid_request`: invalid request or unsupported plan.
- `400 subscription_invalid_limit`: malformed or out-of-range history limit.
- `409 subscription_conflict`: current-state or changed-idempotency conflict.
- `422 subscription_provider_rejected`: the provider deterministically rejected a mutation and the local lifecycle was restored.
- `500 subscription_mutation_internal_error`: a known non-applied provider rejection could not be compensated locally; do not infer success, and refetch SUB-002 before retrying.
- `503 subscription_reconciliation_required`: provider outcome is ambiguous.
- `503 subscription_unavailable`: provider configuration cannot be resolved safely.
- `500 subscription_internal_error`: a history read failed without exposing database details.

After a successful authorization, poll/refetch SUB-002 until a signed
`subscription.charged` event activates the verified period. A `subscription.pending`
event starts past-due/grace handling; grace expiry suspends access. Duplicate
events replay their prior result, stale events cannot regress state, and
wrong-plan/test-live/signature/race ambiguity becomes reconciliation-required.
Deterministic provider HTTP rejections do not become reconciliation; transport
and otherwise unknown outcomes do. The five-minute worker has one total budget
of 50 reconciliation-plus-grace records, a 50-second invocation ceiling, and
bounded provider fetch deadlines. It re-observes known provider subscriptions without
creating charges or blindly retrying unknown mutations. Client event delivery
is not promised; refetch after commands and on bounded polling/backoff.

## OPS-007: Aggregate business operational visibility and safe recovery

All aggregate business routes require bearer authentication, the effective
business, and all-branches scope:

- `GET /operations`
- `GET /operations/{operation_id}`
- `GET /operations/{operation_id}/timeline?limit=50`
- `POST /operations/{operation_id}/recovery`

`operation_id` is the composite `{type}:{UUID}` identity returned by list.
List accepts repeated or comma-separated `type` and `status` filters, `limit`
from 1 through 100 (default 50), and an opaque `cursor`. A cursor is bound to
its original normalized filters and snapshot. A malformed cursor, changed
filter, or out-of-range limit is `400 invalid_operation_query`.

The complete type enum is `invoice_render`, `invoice_delivery`, `outbox`,
`razorpay_webhook`, `gst_einvoice`, `gst_ewaybill`, `recurring_invoice`,
`email_delivery`, `whatsapp_delivery`, `notification`, `import`, and
`voice_reconciliation`. Voice currently has no coherent durable aggregate
source and is explicitly returned in `unavailable_types`; it is never presented
as success. A failed source adapter similarly adds its bounded type names to
that array without forcing another domain to fail or inventing state.

Normalized status is exactly `queued`, `in_progress`, `succeeded`, `failed`,
`reconciliation_required`, or `unknown`. The last two never collapse to
`failed`. Business projections may contain only the composite operation ID,
type, safe resource type/ID, normalized status, attempts, last/next attempt,
retryable/reconciliation/dead-letter flags, sanitized error code, UUID
correlation ID, and lifecycle timestamps. They never contain source status,
provider references, queue/message IDs, raw payload/error, account/secret IDs,
lease details, object keys, recipients, or topology.

```json
{
  "operations": [
    {
      "operation_id": "invoice_render:55555555-5555-4555-8555-555555555555",
      "type": "invoice_render",
      "resource": {"type": "document", "id": "66666666-6666-4666-8666-666666666666"},
      "status": "failed",
      "attempts": 2,
      "retryable": true,
      "reconciliation_required": false,
      "dead_letter": false,
      "error_code": "render_failed",
      "created_at": "2026-09-02T08:00:00Z",
      "updated_at": "2026-09-02T08:01:00Z"
    }
  ],
  "next_cursor": "opaque-filter-bound-cursor",
  "unavailable_types": ["voice_reconciliation"]
}
```

Timeline returns `{ "operation_id": "...", "events": [...] }`; each event has
only normalized `status`, optional safe `code`, and `occurred_at`. Recovery
audit events are included without actor, reason, provider data, or raw errors.

The only accepted business recovery in this revision is `retry` for the exact
failed, retryable, versioned `invoice_render`. It requires `documents.manage`
and re-evaluates the invoice queue/bucket and live SQS/S3 clients before the
transaction. The original failed render remains the command target; the
transaction verifies its tenant, failure state, immutable update revision,
invoice/version/kind, issued facts for final PDFs, and deterministic object key,
then inserts exactly one compatible outbox event and one sanitized command
audit. It does not create a second render job or regenerate finance, tax,
inventory, delivery, or provider outcomes.

```json
{
  "action": "retry",
  "reason": "retry deterministic final render",
  "idempotency_key": "77777777-7777-4777-8777-777777777777",
  "correlation_id": "88888888-8888-4888-8888-888888888888"
}
```

Accepted response is `202`:

```json
{
  "command_id": "99999999-9999-4999-8999-999999999999",
  "operation_id": "invoice_render:55555555-5555-4555-8555-555555555555",
  "action": "retry",
  "result_code": "accepted",
  "correlation_id": "88888888-8888-4888-8888-888888888888",
  "replayed": false,
  "accepted_at": "2026-09-02T08:02:00Z"
}
```

Reuse the same UUID key and identical command after timeout. It returns the
same command with `replayed: true` and no second outbox event. Changed reuse or
a different key against the same source revision is `409 unsafe_replay`.
Other type/action combinations are `422 unsupported_recovery`. Other stable
errors are `400 invalid_recovery_request`, `403 operation_access_denied`, and
`404 operation_not_found`. Do not retry a recovery after `409` or `422`.

## OPS-008: Separately authorized operator detail and recovery boundary

Operator routes are:

- `GET /operator/operations/{operation_id}?business_id={UUID}`
- `GET /operator/operations/{operation_id}/timeline?business_id={UUID}&limit=50`
- `POST /operator/operations/{operation_id}/recovery?business_id={UUID}`

They use bearer authentication but not business-role authorization. Access is
granted only when the verified JWT groups contain the exact configured
`PLATFORM_OPERATOR_GROUP`. The variable has no default. Missing configuration
returns `403 operator_not_configured`; missing group returns
`403 operator_access_denied`. Owner, admin, and business roles are ignored.
The original owning `business_id` is mandatory and every lookup and audit is
tenant-bound.

Operator detail adds only sanitized `source_status` and bounded
`recovery_actions` to OPS-007. It still never exposes provider identifiers,
queue/message IDs, raw provider or queue payloads, raw errors, recipients,
accounts/secrets, object keys, leases, or infrastructure topology. Each action
has `action`, `available`, and optional `requirement_code`.

High-risk `reconcile`, `reprocess_webhook`, `redrive_dead_letter`, and
`resolve` requests cross a narrow one-time scoped step-up boundary. Task 5 has
not yet supplied issuance/verification, so the verifier fails closed. The
backend writes a durable rejected audit with operator subject, tenant,
operation/resource identity, action, reason, idempotency/correlation,
operation revision and safe `step_up_required` result, then returns:

```json
{"code":"step_up_required","error":"operation recovery step-up is required"}
```

HTTP status is `428`. If the audit cannot be stored, the response is
`503 recovery_audit_unavailable`; the blocked action is still not executed.
No live Cognito mutation, provider recovery, queue message, DLQ redrive, or
production action is part of this revision. Operator UI must render these
actions disabled and must not treat possession of an `X-Step-Up-Token` as
sufficient until Task 5 changes the backend boundary.

## OPS-001: Invoice render status

`GET /invoices/{invoice_id}/renders/{render_job_id}`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token, effective business, all-branches scope |
| Permission | `documents.export` |
| Success | `200` |
| Stable states | `queued`, `processing`, `completed`, `failed`, `obsolete` |
| Render kinds | `preview`, `final` |
| Retry/event | No customer retry or event contract; poll with bounded backoff |
| Evidence | `internal/handlers/invoice_handler.go`, `internal/services/invoice_service.go`, `internal/models/invoice_foundations.go`, `tests/unit/invoice_service_test.go`, `internal/handlers/invoice_download_handler_test.go`, `internal/workers/pdf_renderer_snapshot_test.go` |

```json
{
  "id": "55555555-5555-4555-8555-555555555555",
  "invoice_id": "66666666-6666-4666-8666-666666666666",
  "kind": "final",
  "source_invoice_version": 4,
  "status": "completed",
  "created_at": "2026-09-01T12:00:00Z",
  "updated_at": "2026-09-01T12:00:03Z",
  "completed_at": "2026-09-01T12:00:03Z"
}
```

`completed_at` is present and may be `null`. The response deliberately omits
object keys, lease/attempt details, provider details, and raw errors. A completed
final render can be downloaded through the separately implemented
`GET /invoices/{invoice_id}/pdf` presigned-download contract.

## OPS-002: Invoice email delivery

`POST /invoices/{invoice_id}/deliveries`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token, effective business, all-branches scope |
| Permission | `documents.manage` |
| Idempotency | Required `Idempotency-Key` header containing a UUID |
| Request | `{"recipient":"buyer@example.com"}` |
| Success | `202` |
| Retry/event | Reuse the same key and payload to recover the same result; poll status; no versioned client event is promised |
| Evidence | `internal/handlers/invoice_handler.go`, `internal/services/invoice_delivery.go`, `internal/repositories/postgres/invoice_delivery.go`, `internal/services/invoice_delivery_test.go`, `internal/handlers/invoice_delivery_handler_test.go`, `internal/repositories/postgres/invoice_delivery_repo_test.go` |

```json
{
  "delivery": {
    "id": "77777777-7777-4777-8777-777777777777",
    "invoice_id": "66666666-6666-4666-8666-666666666666",
    "render_job_id": "55555555-5555-4555-8555-555555555555",
    "recipient": "buyer@example.com",
    "status": "waiting_for_render",
    "created_at": "2026-09-01T12:00:00Z",
    "updated_at": "2026-09-01T12:00:00Z"
  },
  "replayed": false
}
```

`GET /invoices/{invoice_id}/deliveries/{delivery_id}` requires
`documents.export` and returns only the nested delivery object fields shown
above. Optional timestamps are `sent_at`, `delivered_at`, and `failed_at`.
Stable states are `waiting_for_render`, `queued`, `processing`, `sent`,
`delivered`, `failed`, `bounced`, and `complained`.

Exact current errors are `400 invalid invoice delivery request`, `404 invoice
not found` or `invoice delivery not found`, `409 invoice delivery conflicts with
current state`, and sanitized `500` availability failures.
Changing recipient or actor while reusing a key is a conflict.

## OPS-003: In-app notifications

| Method and path | Success | Result |
| --- | --- | --- |
| `GET /notifications?limit=100` | `200` | JSON array of notifications |
| `POST /notifications/{id}/read` | `200` | The updated notification |
| `POST /notifications/read-all` | `200` | `{"updated": 3}` |

All three require bearer auth and effective business scope and are additionally
scoped to the authenticated user. No extra permission, entitlement, step-up, or
idempotency header is required. List defaults to 100, rejects a non-positive or
non-numeric value, and caps the service result at 200. It has no cursor or total.

```json
{
  "id": "88888888-8888-4888-8888-888888888888",
  "business_id": "22222222-2222-4222-8222-222222222222",
  "type": "invoice_delivery",
  "title": "Invoice sent",
  "body": "Invoice INV-42 was sent",
  "resource_type": "invoice",
  "resource_id": "66666666-6666-4666-8666-666666666666",
  "read_at": null,
  "created_at": "2026-09-01T12:00:00Z"
}
```

Optional fields are omitted rather than guaranteed `null`. Exact current
client-relevant errors include `400 invalid notification limit` and
`404 notification not found`. Evidence: `internal/models/notification.go`,
`internal/handlers/notification_handler.go`,
`internal/services/notification_service.go`,
`internal/repositories/postgres/notification_repo.go`,
`migrations/000051_notifications.up.sql`,
`internal/handlers/notification_handler_test.go`,
`internal/services/notification_service_test.go`, and
`internal/repositories/postgres/notification_repo_test.go`.

## SEC-001: WebSocket ticket

`POST /websocket/tickets`

Bearer auth and effective business scope are required; no additional permission,
entitlement, step-up, body, or idempotency key is required. Success is:

```json
{
  "ticket": "opaque-base64url-value",
  "expires_at": "2026-09-01T12:01:00Z"
}
```

Use it once as `GET /api/v1/ws?ticket=<url-encoded-ticket>`. The ticket is bound
to the authenticated subject and business, has at most a 60-second TTL, and is
atomically consumed once. Only its SHA-256 digest is persisted. Request another
ticket after expiry or failed connection; do not retry the same ticket. There is
no public refresh or revoke endpoint. Deployed WebSocket connectivity remains
externally unverified.

Evidence: `internal/services/websocket_ticket_service.go`,
`internal/handlers/websocket_ticket_handler.go`,
`internal/repositories/postgres/websocket_ticket_repo.go`,
`internal/services/websocket_ticket_service_test.go`,
`internal/handlers/websocket_ticket_handler_test.go`,
`internal/repositories/postgres/websocket_ticket_repo_test.go`,
`tests/unit/websocket_handler_test.go`, and
`migrations/000050_websocket_tickets.up.sql`.

## AUTH-001: Indian phone OTP authentication

Registration/session-start endpoints are public. Link, link-confirm and logout
require a bearer access token without requiring business selection. They are rate limited in
`internal/app/runtime.go`; Cognito and SMS are externally unverified.

| Method and path | Exact request | Current success |
| --- | --- | --- |
| `POST /auth/phone/register` | `{"phone_number":"9876543210","name":"Asha"}` | `201 {"phone_number":"+919876543210","message":"OTP sent to your phone number"}` |
| `POST /auth/phone/confirm` | `{"phone_number":"+919876543210","code":"123456"}` | `200 {"message":"phone number verified successfully"}` |
| `POST /auth/phone/resend-confirmation` | `{"phone_number":"+919876543210"}` | `200 {"message":"verification code resent"}` |
| `POST /auth/phone/login` | `{"phone_number":"+919876543210"}` | `200 {"challenge_name":"SMS_OTP","session":"opaque","message":"OTP sent to your phone number"}` |
| `POST /auth/phone/verify-login` | `{"phone_number":"+919876543210","code":"123456","session":"opaque"}` | `200` token object |
| `POST /auth/phone/refresh` | `{"refresh_token":"opaque"}` | `200` token object |
| `POST /auth/phone/link` | bearer plus `{"phone_number":"+919876543210"}` | `202 {"phone_number":"+919876543210","message":"OTP sent to your phone number"}` |
| `POST /auth/phone/link/confirm` | bearer plus `{"phone_number":"+919876543210","code":"123456"}` | `200 {"message":"phone number linked successfully"}` |
| `POST /auth/phone/logout` | no body; bearer access token | `200 {"message":"logged out successfully"}` |

The token object fields are `access_token`, `refresh_token`, `expires_in`, and
`token_type`. Registration rejects email prebinding. Phone input is normalized
to Indian `+91` E.164. Profile updates cannot silently set or replace a phone;
clients must use the explicit link and confirm routes. Collision checks run both
before provider confirmation and at the database uniqueness boundary. Cognito is
the authority for OTP expiry and replay rejection. Public login/resend normalize
unknown-account behavior for register/login/resend, refresh/logout hide provider details, and security
events store hashed phone subjects rather than raw numbers. A configured durable
cooldown fails closed if its DynamoDB client is unavailable.

Evidence: `internal/handlers/auth_handler.go`, `internal/services/auth_service.go`,
`internal/services/auth_phone_test.go`, `tests/unit/auth_service_test.go`,
`tests/integration/auth_test.go`, and
`migrations/000023_add_phone_auth_fields.up.sql`.

## ACC-001: Fiscal controls, posting and bank reconciliation

All routes require bearer auth, effective business, and all-branches scope.
Reads use `reports.view`. Fiscal/account mutations use `accounting.manage`; bank
setup, import, matching and close use `banking.manage`. A journal write uses
`accounting.manage`. Document and payment origination retains its domain write
permission and additionally requires `accounting.manage` when it can post.

| Method and path | Input | Success |
| --- | --- | --- |
| `GET/PUT /accounting/policy` | PUT: `lock_date`, `reversal_policy` (`next_open_period` or `blocked`) | `200` policy |
| `GET /accounting/accounts` | none | `200` authoritative account array |
| `PUT /accounting/accounts/{code}` | `name`, `account_class`, optional `parent_code` | `200` account; cycles and cross-class parents are rejected |
| `POST /accounting/opening-balances` | body command below | `201` source-linked posted journal |
| `GET /accounting/audit` | none | `200` latest bounded audit array |
| `GET/POST /accounting/bank-accounts` | POST: `name`, `currency`, `masked_account`, `ledger_account`, `opening_minor` | `200` array / `201` account |
| `POST /accounting/bank-statements` | tenant/uploader-bound clean pending `upload_id`, account, command key and period | `201` pending statement |
| `GET /accounting/bank-statements/{id}/transactions?unreconciled=true` | none | `200` transaction and active-match states |
| `GET /accounting/bank-transactions/{id}/suggestions` | none | `200` bounded exact/fuzzy candidates; no mutation |
| `POST/DELETE /accounting/bank-transactions/{id}/match` | ledger ID/reason or unmatch reason | `201` match / `204` |
| `POST /accounting/bank-transactions/{id}/adjustment` | `kind` (`fee` or `interest`), reason, idempotency key | `201` posted journal and match atomically |
| `POST /accounting/bank-statements/{id}/reconcile` | `reconciliation_date` | `200` only when every transaction is matched |

The bank CSV is read server-side only after the pending upload is tenant-bound,
uploader-bound, scan-clean, `bank_statement`, and `text/csv`. Required headings
are `external_id,transaction_at,amount_minor,currency`; optional headings are
`reference,description`. Dates use `YYYY-MM-DD`; maximum 10,000 rows. Currency,
period, account, direction, amount, active statement and tenant scope are
revalidated transactionally.

Opening body example:

```json
{"idempotency_key":"open-2026","as_of_date":"2026-04-01T00:00:00Z","lines":[{"account_code":"CASH","account_name":"Cash","account_class":"asset","entry_type":"debit","amount_minor":10000,"currency":"INR"},{"account_code":"OPENING_EQUITY","account_name":"Opening equity","account_class":"equity","entry_type":"credit","amount_minor":10000,"currency":"INR"}],"inventory":[]}
```

Posted journals, canonical invoice issue, issued document conversion/origination,
payment posting, bank adjustment, and inventory adjustment/transfer/reset/
assembly all enforce the fiscal lock. When posting date is locked, send a UUID
`Idempotency-Key`, one-use `X-Step-Up-Token`, and 8-500 character
`X-Lock-Override-Reason`. Missing or invalid authorization returns exact
`428 {"error":"scoped step-up and override reason are required","code":"accounting_period_locked"}`.
An override is bound to business, subject, action, resource, command identity,
posting date and reason, then consumed in the same transaction as the financial
effect. Reversals either fail under `blocked` or post on the first day after the
lock under `next_open_period`. Issued documents cannot use draft cancellation;
they must use the financial reversal workflow.

Opening balance and bank statement keys replay only the exact tenant-bound
request hash; changed reuse fails. Bank adjustment unmatch atomically reverses
its journal. No bank or reconciliation endpoint auto-repairs data. Bank file
storage and deployed scanning remain externally unverified.

Evidence: `internal/services/accounting_service.go`, journal/payment/document/
inventory services, `internal/services/journal_invariants_test.go`,
`internal/services/payment_invariants_test.go`,
`migrations/000058_accounting_completeness.up.sql`, and route security tests.

## ACC-002: Accounting statements and diagnostics

Use the standard report endpoint with keys `trial_balance`, `balance_sheet`, and
`account_drilldown`:

```http
POST /reports/trial_balance/query
Content-Type: application/json

{"page":1,"limit":50,"filters":{"date_from":"2026-04-01T00:00:00Z","date_to":"2027-03-31T23:59:59Z","currency":"INR","branch_id":"optional-uuid","compare_to":"2026-03-31T23:59:59Z"}}
```

Trial Balance rows provide account code/name/class/parent plus opening, period,
closing and comparison debit/credit minor-unit totals. Balance Sheet returns
asset, liability and equity hierarchy rows plus `assets_minor`,
`liabilities_minor`, and `equity_minor` totals. Account drilldown requires
`filters.account_code`. Only posted journals contribute. Currency is required;
branch scope is permission-filtered. Standard `/export` and `/share` variants
provide the existing CSV/export-record and share contracts.

`GET /accounting/reconciliation-diagnostics?currency=INR&through=<RFC3339>`
returns `journal_ledger_differences`, `inventory_gl_difference_minor`,
`tax_gl_difference_minor`, `currency`, and constant `read_only:true`. Missing or
invalid currency returns `400`. The endpoint compares canonical journal/ledger
tuples, opening inventory plus stock movement against inventory GL, and signed
document tax against tax GL. It never mutates or repairs.

Results are synchronous and retry-safe. Invalidate after journal/document/
payment/inventory/opening/bank adjustment or reversal. Evidence:
`internal/repositories/postgres/reporting_repo.go`, `internal/reporting/registry.go`,
`internal/services/accounting_service.go`,
`internal/repositories/postgres/accounting_reporting_test.go`, and migration
`000058`.

## OPS-004: Document GST compliance commands and status

Routes are business-scoped and all-branches. Reads and the PDF-job request use
`documents.export`; mutations use `documents.manage`.

| Method and path | Exact body | Success |
| --- | --- | --- |
| `GET /documents/{id}/compliance` | none | `200` compliance status |
| `POST /documents/{id}/einvoice` | `{"source":"web"}` (`source` optional) | `202` job |
| `GET /documents/{id}/einvoice` | none | `200` e-invoice record; `404 {"error":"e-invoice not found"}` |
| `POST /documents/{id}/einvoice/cancel` | `{"reason":"duplicate","source":"web"}` | `202` job |
| `POST /documents/{id}/ewaybill` | `{"source":"web","dispatch_from":{},"dispatch_to":{},"distance_km":12.5,"transporter":{},"vehicle":{}}` | `202` job |
| `GET /documents/{id}/ewaybill` | none | `200` e-way-bill record; `404 {"error":"e-way bill not found"}` |
| `GET /documents/{id}/ewaybill/pdf` | none | `200 {"pdf_url":"https://opaque-signed-url.example"}`; service errors are unstable `400` text |
| `PATCH /documents/{id}/ewaybill/part-b` | `{"source":"web","transporter":{},"vehicle":{"number":"MH12AB1234"},"reason_code":"1"}` | `202` job |
| `POST /documents/{id}/ewaybill/multi-vehicle` | `{"source":"web","movement_type":"add","vehicle_no":"MH12AB1234","transport_doc_no":"LR-42","from_place":"Pune","from_state":"27","reason_code":"1","payload":{}}` | `202` job |

The invoice alias `POST /invoices/{id}/einvoice` has the same mutation
permission and job semantics. Every mutation command in the table requires
`Idempotency-Key`; reads and PDF retrieval do not. The
current key is unique at business scope but is not bound to operation/payload;
reusing it for changed input can return the first job. Use a fresh UUID for a
new command and reuse a key only for the exact same command after timeout.
Missing headers return exact `400 {"error":"Idempotency-Key header is
required"}`; the helper does not require UUID syntax.

The job response fields are `id`, `business_id`, `document_id`, `operation`,
`status`, `idempotency_key`, optional `queue_message_id`, `attempt_count`,
optional `next_attempt_at`, `last_attempt_at`, `succeeded_at`, `last_error`,
`error_class`, optional `request_payload`, `result_payload`, `source`,
`created_at`, and `updated_at`. Operations are `generate_einvoice`,
`cancel_einvoice`, `generate_ewaybill`, `update_eway_part_b`, `multi_vehicle`,
and `fetch_eway_pdf`. States are `queued`, `processing`, `succeeded`,
`retrying`, `failed`, and `needs_attention`; error classes are `retriable`,
`validation`, `credentials`, `duplicate`, `rule`, `unavailable`, and `unknown`.

```json
{
  "id": "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
  "business_id": "22222222-2222-4222-8222-222222222222",
  "document_id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
  "operation": "generate_einvoice",
  "status": "queued",
  "idempotency_key": "ffffffff-ffff-4fff-8fff-ffffffffffff",
  "attempt_count": 0,
  "source": "web",
  "created_at": "2026-09-01T12:00:00Z",
  "updated_at": "2026-09-01T12:00:00Z"
}
```

The status projection has `compliance_status`, optional `portal_status`,
`retry_count`, optional `irn`, `ack_number`, `ack_date`, `qr_code_url`,
`eway_bill_number`, `eway_bill_valid_until`, `last_error`, and optional nested
`job`, `einvoice`, and `ewaybill`. It begins as `idle`; the current read can
return `idle` even for an unknown document ID, so `idle` is not existence proof.

```json
{"compliance_status":"idle","retry_count":0}
```

The exact e-invoice record fields are `id`, `business_id`, `document_id`,
optional `integration_account_id`, `status`, optional `irn`, `ack_number`,
`ack_date`, `signed_qr_code_payload`, `qr_code_url`, `provider_reference_id`,
`provider_name`, `request_payload`, `response_payload`, `error_class`,
`last_error`, `generated_at`, `cancelled_at`, `created_at`, and `updated_at`.
States are `pending`, `generated`, `cancelled`, and `failed`. The exact e-way
bill record additionally exposes `eway_bill_number`, `eway_bill_date`,
`eway_bill_valid_until`, `supply_type`, `part_a_status`, `part_b_status`,
`distance_km`, `distance_source`, JSON-encoded `transporter`, `vehicle`,
`dispatch_from`, and `dispatch_to`, `pdf_url`, `updated_part_b_at`, plus the
same provider/payload/error/timestamp fields. States are `pending`, `generated`,
`part_b_updated`, `multi_vehicle`, `cancelled`, and `failed`.

```json
{"id":"12121212-aaaa-4aaa-8aaa-121212121212","business_id":"22222222-2222-4222-8222-222222222222","document_id":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","status":"generated","irn":"provider-or-simulated-value","ack_number":"provider-or-simulated-value","signed_qr_code_payload":"unsafe-provider-payload","qr_code_url":"https://unsafe-current-url.example","provider_reference_id":"unsafe-reference","provider_name":"simulated","request_payload":"{}","response_payload":"{}","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}
```

```json
{"id":"13131313-aaaa-4aaa-8aaa-131313131313","business_id":"22222222-2222-4222-8222-222222222222","document_id":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","status":"generated","eway_bill_number":"provider-or-simulated-value","distance_km":12.5,"distance_source":"manual","transporter":"{}","vehicle":"{\"number\":\"MH12AB1234\"}","dispatch_from":"{}","dispatch_to":"{}","pdf_url":"https://unsafe-current-url.example","provider_name":"simulated","request_payload":"{}","response_payload":"{}","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}
```

> ⛔ **UNSAFE - DO NOT DISPLAY, LOG, OR PERSIST:** the exact current response
> can expose `idempotency_key`, `queue_message_id`, provider names/references,
> signed QR/PDF URLs, request/result/response payloads, and raw provider errors
> through those nested objects. Task 3 must replace this with a customer-safe
> projection and opaque operation/artifact identifiers. Until then, keep this
> surface internal and discard all unsafe nested fields.

Generate commands can return stable entitlement errors `403` with code
`feature_disabled` and `429` with code `quota_exceeded`. Binding errors are
`400`; other domain/provider text is unstable. Queue/provider execution is
externally unverified. Missing provider configuration selects a simulator that
can create provider-looking success, so even `succeeded`, IRN, ack, QR, or
e-way-bill fields are not government proof.

Evidence: `internal/app/runtime.go`, `internal/handlers/document_handler.go`,
`internal/services/tax_compliance_execution.go`,
`internal/services/gst_provider.go`, `internal/models/gst_compliance.go`,
`internal/services/tax_compliance_service_test.go`,
`tests/unit/document_handler_test.go`,
`migrations/000027_add_gst_compliance.up.sql`, and
`migrations/000031_add_gst_execution_and_pos.up.sql`.

## OPS-005: GST integration accounts and GSTIN lookup

| Method and path | Permission | Body / success |
| --- | --- | --- |
| `GET /tax/integrations` | `tax.integrations.manage`, all branches | `200 {"data":[]}` |
| `POST /tax/integrations` | same | body below; `200` account |
| `PUT /tax/integrations/{id}` | same | body below; `200` account |
| `POST /tax/integrations/{id}/validate` | same | no body; `200` account; stable safe errors below |
| `POST /utils/gstin/{gstin}/fetch` | `reports.view` | no body; `200` lookup result |

The account body has optional `provider`, required `service_type`, optional
`gsp_name`, optional `portal_username`, `credentials`, and optional `metadata`.
Credentials may contain `portal_username`, `portal_password`, `api_username`,
`api_password`, `api_key`, `api_secret`, and string-map `metadata`.

```json
{"provider":"configured-provider","service_type":"einvoice","gsp_name":"Example GSP","portal_username":"tenant-user","credentials":{"api_username":"runtime-secret","api_password":"runtime-secret"},"metadata":{"environment":"sandbox"}}
```

Never echo or persist request credentials client-side. The exact safe-listed
account response fields are `id`, `business_id`, `provider`, `service_type`,
optional `gsp_name`, optional `portal_username`, optional `credential_hint`,
`status`, optional `last_validated_at`, optional `last_error`, optional
`metadata` as a JSON-encoded string, `created_at`, and `updated_at`. Encrypted
credentials and internal credential/observation revisions are not serialized.
Account writes and validate are not idempotent.

```json
{"id":"14141414-aaaa-4aaa-8aaa-141414141414","business_id":"22222222-2222-4222-8222-222222222222","provider":"configured-provider","service_type":"einvoice","gsp_name":"Example GSP","portal_username":"tenant-user","credential_hint":"unsafe-hint","status":"pending","metadata":"{\"environment\":\"sandbox\"}","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}
```

Integration errors use the stable envelope
`{"error":{"code":"...","message":"..."}}`. Exact public errors are:

- `400 invalid_tax_integration_request`: `Tax integration request is invalid.`
- `404 gst_integration_account_not_found`: `GST integration account was not found.`
- `409 gst_credential_revision_conflict`: `GST integration credentials changed. Refresh and retry.`
- `422 gst_credential_validation_not_configured`: `GST credential validation is not configured.`
- `422 gst_credential_validation_failed`: `GST credential validation failed.`
- `422 gst_integration_account_required`: `Configure a GST integration account before using this operation.`
- `500 tax_integration_internal_error`: `Tax integration request failed.`

List uses the generic `500`; create/update uses `400`, `404`, `409`, and
`500`; explicit validation uses `404`, `409`, both validation `422` codes, and
`500`. Error responses never include the account or raw database/provider text.
A missing validation path returns the not-configured `422`, performs no
provider I/O, and leaves provider readiness unknown.

Credential writes atomically advance an internal business-wide credential
revision and invalidate the prior GST health snapshot. A validation or real
operation outcome is accepted only for the exact tenant account revision it
loaded. E-invoice/e-way execution uses that same decrypted tenant credential
source; it no longer falls back to unrelated global GST credentials when the
tenant account is missing. Best-effort real-operation health persistence is
detached from request cancellation and bounded; an auxiliary persistence issue
does not change the provider/business result and is emitted only as a sanitized
operational issue.

GSTIN lookup returns `gstin`, optional `pan`, `legal_name`, `trade_name`,
`address`, `state_code`, `status`, `registration_date`, `constitution`,
`nature_of_business`, `provider_message`, `source`, `is_valid`, and optional
`raw_metadata`. If the provider URL is absent, current code returns a local
format-derived fallback. `raw_metadata`, provider names, validation status, and
fallback success are internal-only and are not government registry proof.

```json
{"gstin":"27ABCDE1234F1Z5","pan":"ABCDE1234F","state_code":"27","source":"local_validation","is_valid":true}
```

> ⛔ **UNSAFE - DO NOT DISPLAY, LOG, OR PERSIST:** do not expose request
> credentials, `credential_hint`, provider error text, `raw_metadata`, or
> simulated provider status. Task 3 must add safe projections; Task 1 must make
> provider readiness authoritative.

Evidence: `internal/app/runtime.go`, `internal/handlers/tax_handler.go`,
`internal/services/tax_compliance_execution.go`,
`internal/services/tax_compliance_service.go`, `internal/models/gst_compliance.go`,
`internal/services/tax_compliance_service_test.go`, and
`tests/unit/tax_handler_test.go`.

## OPS-006: GSTR-2B reconciliation and GST report runs

`POST /tax/gstr-2b/import` requires `documents.manage` and all branches. The
body requires `period_start`, `period_end`, and `lines`; optional fields are
`source` and `notes`. A line can contain `supplier_gstin`, `supplier_name`,
`document_number`, `document_date`, `document_type`, `taxable_amount`,
`tax_amount`, `cgst_amount`, `sgst_amount`, `igst_amount`, `cess_amount`,
`place_of_supply`, and map `raw_payload`.

```json
{"period_start":"2026-08-01T00:00:00Z","period_end":"2026-08-31T23:59:59Z","source":"api","lines":[{"supplier_gstin":"27ABCDE1234F1Z5","document_number":"INV-42","taxable_amount":1000,"tax_amount":180,"igst_amount":180}]}
```

Success is `201 {"import":{...},"results":[...]}`. Import fields are `id`,
`business_id`, `period_start`, `period_end`, `source`, `status`, optional
`notes`, optional JSON-encoded `raw_payload`, `created_at`, and `updated_at`.
Status is synchronously `processed`. Result status values are `matched`,
`value_mismatch`, `tax_mismatch`, `missing_in_books`, and `missing_in_portal`;
result fields also include import/document IDs, mismatch text, book/import
taxable/tax amounts, optional JSON-encoded metadata, and timestamps. Identical
raw input/period/source is deduplicated, but there is no client idempotency key;
service failures are unstable `500` text.

```json
{"import":{"id":"15151515-aaaa-4aaa-8aaa-151515151515","business_id":"22222222-2222-4222-8222-222222222222","period_start":"2026-08-01T00:00:00Z","period_end":"2026-08-31T23:59:59Z","source":"api","status":"processed","raw_payload":"{}","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"},"results":[{"id":"16161616-aaaa-4aaa-8aaa-161616161616","import_id":"15151515-aaaa-4aaa-8aaa-151515151515","business_id":"22222222-2222-4222-8222-222222222222","status":"missing_in_books","books_taxable_amount":0,"import_taxable_amount":1000,"books_tax_amount":0,"import_tax_amount":180,"created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}]}
```

GST reports use `GET /tax/reports/{report_type}` with required YYYY-MM-DD
`period_start` and `period_end`, plus optional `filing_frequency` and
`export_format`; success is `200` a report-specific JSON map. `POST` to
`/tax/reports/{report_type}/export` accepts those same option fields as JSON and
also syntactically accepts `created_by`, which the handler overwrites from the
authenticated user; it returns `201` a `GSTReportRun`.
`GET /tax/report-runs/{id}` returns `200` or
`404 {"error":"report run not found"}`. Types are `gstr1`, `gstr2b`, `cmp08`,
`gstr4`, `gstr7`, and `hsn_summary`. A run has `id`, `business_id`,
`report_type`, `period_start`, `period_end`, `filing_frequency`,
`export_format`, `status`, optional JSON-encoded `warnings` and `payload`,
optional `created_by`, `created_at`, and `updated_at`; states are `queued`,
`completed`, and `failed`.

```json
{"id":"17171717-aaaa-4aaa-8aaa-171717171717","business_id":"22222222-2222-4222-8222-222222222222","report_type":"gstr1","period_start":"2026-08-01T00:00:00Z","period_end":"2026-08-31T23:59:59Z","filing_frequency":"monthly","export_format":"json","status":"completed","warnings":"[]","payload":"{}","created_by":"user-id","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}
```

These are reconciliation/generated-report surfaces only. They do not file a
return and have no government acknowledgement. Official GST filing remains
deferred. Evidence: `internal/app/runtime.go`, `internal/handlers/tax_handler.go`,
`internal/services/tax_compliance_service.go`, `internal/models/tax.go`,
`internal/services/tax_compliance_service_test.go`,
`tests/unit/tax_handler_test.go`, and
`migrations/000027_add_gst_compliance.up.sql`.

## AI-001: Descriptive agent capability CRUD

| Method and path | Exact body | Current success |
| --- | --- | --- |
| `GET /agents/{id}/capabilities` | none | `200` array |
| `POST /agents/{id}/capabilities` | `{"capability_type":"product_search","description":"Read catalog","config":{"limit":10}}` | `201 {"message":"capability added successfully"}` |
| `DELETE /agents/{id}/capabilities/{capability_id}` | none | `200 {"message":"capability removed successfully"}` |
| `POST /agents/validate-permissions/{id}` | none | nominally `200 {"has_permission":true}` |

The add body requires `capability_type`; `description` and map `config` are
optional. List entries have `id`, `agent_id`, `capability_type`, optional
`description`, `config` as a JSON-encoded string, and `created_at`.

```json
[{"id":"11111111-aaaa-4aaa-8aaa-111111111111","agent_id":"22222222-aaaa-4aaa-8aaa-222222222222","capability_type":"product_search","description":"Read catalog","config":"{\"limit\":10}","created_at":"2026-09-01T12:00:00Z"}]
```

Binding errors are unstable `400`; an inaccessible agent/capability is `404`
with current error text; service failures are unstable `500`. The current
ownership helper admits the agent owner **or any effective user in the agent's
business**; these routes add no explicit permission, entitlement, step-up,
version, or command idempotency. Duplicate `(agent_id, capability_type)` is
database-rejected but returned as unstable `500` text.

> ⛔ **UNSAFE - NOT AN AUTHORIZATION CONTRACT:** capability rows are
> descriptive configuration. `validate-permissions` only reuses the
> owner/same-business lookup; it does not accept a tool, risk, arguments, or
> requested action and can attempt a second response after a lookup failure.
> Do not use it to authorize execution or show a governance approval.

These CRUD rows still provide no execution authority. Runtime execution now has
a separate code-owned, default-deny governance contract with exact risk,
permission, approval, budget, audit, kill-switch and breaker checks. Evidence:
`internal/services/agent_governance_service.go`,
`internal/repositories/interfaces/agent_governance_repository.go`,
`internal/repositories/postgres/agent_governance_repo.go`,
`migrations/000061_ai_agent_governance.up.sql`, and `internal/app/runtime.go`.
The descriptive CRUD evidence remains:
`internal/handlers/agent_handler.go`, `internal/services/agent_service.go`,
`internal/models/agent.go`, `tests/unit/agent_handler_test.go`,
`tests/integration/agent_test.go`, and
`migrations/000013_add_ap2_agent_marketplace.up.sql`.

### Internal governed AI execution contract

There is no public approval or kill-switch API in this release. The independent
`AI_AGENT_EXECUTION_ENABLED` hard gate defaults false, and the durable global
gate also defaults disabled. Missing catalog, permission, repository, gate,
budget, breaker, or audit state denies execution. Both agent and business spend
ceilings are explicit UTC-day windows in integer micros; provider and model are
immutable for a run, so an open circuit cannot trigger a costlier fallback.

| Property | Internal contract |
| --- | --- |
| Exposure | Internal service/repository seam only; no HTTP method/path, frontend request, or frontend response exists |
| State | `initializing`, `running`, `completed`, `failed`, `cancelled`, `timed_out`, `blocked`, or `reconciliation_required` |
| Retry | A scoped idempotency replay never invokes the tool twice; terminal success converges through a bounded safe result reference, while in-progress, cancelled, timed-out, failed, unknown, or missing-reference replays fail closed |
| Limits | Per-run tokens, daily per-agent spend, per-business spend, steps, tool calls, duration, retries, cancellation, global/business/agent gates, and provider circuit/probe admission |
| Stable internal errors | governance unavailable/invalid scope/conflict; invalid arguments; tool denied/in progress/replay unavailable/replay cancelled/replay timed out/reconciliation required/execution failed; approval required/invalid; gate disabled/run cancelled/run expired; step/tool/token/retry/spend limit; unsafe transition; circuit open/probe busy |
| Frontend rule | Show AI and voice as unavailable through CAP-001. Do not invent approval, budget, audit, cancellation, or retry endpoints from this internal vocabulary |

The exact risk classes are `read-only`, `internal draft`, `reversible write`,
`external communication`, `financial commitment`, `tax or compliance`,
`credential or security`, and `irreversible or legally significant`. Missing or
different classifications deny execution. The last five classes require exact
one-use approval; no high-risk effect adapter is registered.

The code-owned catalog classifies the four existing voice reads as `read-only`.
Only `list_invoices` and `get_invoice` are eligible for production advertising;
customer reads remain disabled. The voice runtime requires a governed-executor
factory and advertises zero tools when it is absent or unready. Tool arguments
are duplicate-key-rejected canonical JSON, hashed with SHA-256, bounded, and
rejected when sensitive key names are present. Stored audit data is sanitized
metadata, hashes, bounded configuration, usage, integer-micro cost, safe result
references, retry/failure codes, and dispositions, never prompts, credentials,
Bearer values, or provider bodies.

AI and voice capabilities remain explicitly unavailable even when durable gates
are enabled because no production adapter yet governs the model call and tool
call as one run. Voice session creation checks both unavailable capabilities,
and production runtime composition has no governance factory. Enabling database
gates alone therefore cannot advertise or dispatch ungoverned model inference.

External communication, financial, tax, security, and legal classes require an
exact one-use approval plus an effect adapter with idempotency and reconciliation.
No such high-risk adapter is registered. Production A2A task execution,
autonomous bargaining, and procurement start/auto-buy therefore fail closed;
unknown A2A task types are denied, and `payment.process` can no longer return a
false success. A timeout or ambiguous outcome after dispatch is
`reconciliation_required`, never success or an automatic release/retry.

## ASSET-001: Verified logo upload and current drive presigns

### Business logo

`POST /business-profiles/{business_id}/logo` requires bearer auth, the matching
effective business, and business ownership. Send JSON with the exact intended
metadata. JPEG, PNG, and WebP are allowed from 1 byte through 5 MiB.

```json
{
  "content_type": "image/png",
  "size_bytes": 2048,
  "checksum_sha256": "base64-encoded-32-byte-sha256"
}
```

Success is `201`. The upload ID is opaque. PUT bytes to `upload_url` using every
returned header exactly; the signature binds checksum, length, type, business,
uploader, and upload ID.

```json
{
  "upload": {
    "id": "33333333-aaaa-4aaa-8aaa-333333333333",
    "business_id": "22222222-2222-4222-8222-222222222222",
    "uploader_id": "user-id",
    "kind": "business_logo",
    "content_type": "image/png",
    "size_bytes": 2048,
    "checksum_sha256": "base64-encoded-32-byte-sha256",
    "status": "pending"
  },
  "upload_url": "https://opaque-signed-storage-url.example",
  "required_headers": {
    "Content-Length": "2048",
    "Content-Type": "image/png",
    "x-amz-checksum-sha256": "base64-encoded-32-byte-sha256",
    "x-amz-meta-business-id": "22222222-2222-4222-8222-222222222222",
    "x-amz-meta-uploader-id": "user-id",
    "x-amz-meta-upload-id": "33333333-aaaa-4aaa-8aaa-333333333333"
  }
}
```

After PUT, call `POST /business-profiles/{business_id}/logo/complete` with
`{"upload_id":"33333333-aaaa-4aaa-8aaa-333333333333"}`. Success is:

```json
{
  "business_id": "22222222-2222-4222-8222-222222222222",
  "upload_id": "33333333-aaaa-4aaa-8aaa-333333333333",
  "status": "ready",
  "replayed": false,
  "cleanup_pending": false
}
```

Completion rechecks the exact object key and HEAD metadata, then updates the
business reference transactionally. Repeating the same upload ID is safe and
returns `replayed:true`; an older completed upload never replaces a newer logo.
`cleanup_pending:true` means the new reference is committed but deletion of the
old object still needs storage cleanup. `400` means invalid
create input, `404` hides business/upload scope mismatches, and `409` means the
uploaded object failed completion verification. A successful PUT alone is not
asset completion.

### Drive asset

`POST /drive/presign` requires `drive.manage` and the plan's drive-storage
entitlement. Request fields are `name`, optional `folder_path`, `content_type`,
`size_bytes`, optional `category`, and optional `metadata`. Allowed types are
JPEG, PNG, WebP, and PDF; maximum size is 25 MiB.

```json
{
  "name": "receipt.pdf",
  "folder_path": "receipts/2026",
  "content_type": "application/pdf",
  "size_bytes": 2048,
  "category": "receipt",
  "metadata": { "source": "web" }
}
```

Success is `201` with `asset`, `upload_url`, and `required_headers`. The exact
asset fields are `id`, `business_id`, optional `uploaded_by`, `name`, optional
`folder_path`, `bucket`, `object_key`, optional `content_type`, `size_bytes`,
optional `category`, optional `metadata` (JSON-encoded string), `created_at`, and
`updated_at`. `bucket` and `object_key` are currently leaked implementation
coordinates and must not become frontend dependencies.

```json
{
  "asset": {
    "id": "33333333-aaaa-4aaa-8aaa-333333333333",
    "business_id": "22222222-2222-4222-8222-222222222222",
    "uploaded_by": "user-id",
    "name": "receipt.pdf",
    "folder_path": "receipts/2026",
    "bucket": "current-private-bucket",
    "object_key": "22222222-2222-4222-8222-222222222222/20260901/opaque-current-key",
    "content_type": "application/pdf",
    "size_bytes": 2048,
    "category": "receipt",
    "metadata": "{\"source\":\"web\"}",
    "created_at": "2026-09-01T12:00:00Z",
    "updated_at": "2026-09-01T12:00:00Z"
  },
  "upload_url": "https://opaque-signed-storage-url.example",
  "required_headers": {"Content-Length":"2048","Content-Type":"application/pdf"}
}
```

> ⛔ **DRIVE ONLY - UNSAFE - DO NOT DISPLAY, LOG, OR PERSIST:** `bucket`,
> `object_key`, and `upload_url` below expose storage topology or temporary
> credentials. Use `asset.id` only in memory and discard unsafe fields after PUT.

The complete drive route behavior is:

| Method and path | Request | Success | Endpoint-specific failures |
| --- | --- | --- | --- |
| `GET /drive` | none | `200 {"items":[DriveAsset],"usage_bytes":2048}` | unstable `500` text |
| `POST /drive/presign` | create body above | `201` session above | bind, unsupported type, quota, feature, storage/config errors are unstable `400` text |
| `PATCH /drive/{id}` | `{"name":"receipt-final.pdf","folder_path":"receipts/final","category":"receipt","metadata":{"reviewed":true}}` | `200` asset | bind/service `400`; `404 {"error":"drive asset not found"}` |
| `DELETE /drive/{id}` | none | `204` no body | `404` with unstable database text; otherwise `400` text |

List wraps the same exact `DriveAsset` shape shown above:

```json
{"items":[{"id":"33333333-aaaa-4aaa-8aaa-333333333333","business_id":"22222222-2222-4222-8222-222222222222","name":"receipt.pdf","bucket":"current-private-bucket","object_key":"current/internal/object-key","content_type":"application/pdf","size_bytes":2048,"created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}],"usage_bytes":2048}
```

All drive routes use bearer/effective-business scope. List requires `drive.view`;
the other routes require `drive.manage`. PATCH is partial for name/category/
metadata, but omission of `folder_path` clears it. DELETE soft-deletes the row
even when object deletion fails; a retry after success normally becomes `404`.
There is no idempotency or version control, so do not blindly retry PATCH/DELETE
after an ambiguous result.

For both flows, upload with exactly the returned headers. Logo is complete only
after its completion endpoint returns `ready`. Drive still has no checksum,
HEAD/metadata verification, completion transaction, or version control; a failed
drive PUT can leave a ghost row and must not be shown as a completed asset.

Evidence: `internal/handlers/business_handler.go`,
`internal/services/business_service.go`, `internal/services/business_logo_service_test.go`, `internal/handlers/commerce_handler.go`,
`internal/services/commerce_service.go`, `internal/models/commerce.go`,
`internal/services/s3_service.go`, `internal/services/s3_service_test.go`,
`tests/unit/business_service_test.go`, `internal/services/commerce_service_test.go`,
`tests/unit/commerce_handler_test.go`, `tests/integration/s3_test.go`, and
`infrastructure/terraform/s3.tf`.

## IMP-001: Durable two-phase bulk import

Supported intake routes are `POST /imports/customers`, `/imports/vendors`, and
`/imports/products`. Each accepts JSON with an opaque clean pending-upload UUID,
optional `delimiter` (`comma`, `semicolon`, or `tab`), and optional source-header
to canonical-field `mapping`:

```json
{"upload_id":"44444444-aaaa-4aaa-8aaa-444444444444","delimiter":"comma","mapping":{"customer_name":"name"}}
```

Validation returns `201` and never creates customers, vendors, or products. It
persists normalized row previews and stable row errors, rejects invalid UTF-8,
checksum/size mismatch, malformed or oversized CSV, formula-prefixed values,
invalid mappings and domain values, and tenant or uploader mismatch. A job with
any invalid row cannot commit.

Commit is `POST /imports/{job_id}/commit` with a UUID `Idempotency-Key`; success
is `202`. Cancellation is `DELETE /imports/{job_id}` and returns `200`. Reuse the
same upload and validation payload or commit key after an ambiguous response.
Changing a replayed validation payload or commit key returns `409` code
`bulk_import_conflict`. Other stable codes are `bulk_import_invalid` (`400`),
`bulk_import_forbidden` (`403`), `bulk_import_not_found` (`404`), and
`bulk_import_retryable` (`503`).

Poll `GET /bulk-jobs/{id}` until terminal. Result artifacts never expose object
keys; completion writes a bounded CSV result artifact and an idempotent in-app
notification. Job list/detail reads are uploader-bound. Due queue commands,
expired worker leases, and 30-day artifact/job cleanup are recovered by the
five-minute maintenance schedule; completion-delivery retries keep notifications
idempotent. Invoice and document imports remain unsupported.

### Replaced legacy intake

Before migration `000059`, the routes used multipart upload, buffered the file,
and created queued rows without a consumer. Those routes and the generic legacy
import service path are disabled. Existing queued imports are marked failed with
`legacy_import_requires_revalidation`; clients must create a verified pending
upload and start a new validation.

Successful legacy intake was `202` with the job object, not `201`:

```http
Content-Type: multipart/form-data; boundary=...

--...
Content-Disposition: form-data; name="file"; filename="customers.csv"
Content-Type: text/csv

name,email
Asha,asha@example.test
--...--
```

```json
{
  "id": "44444444-aaaa-4aaa-8aaa-444444444444",
  "business_id": "22222222-2222-4222-8222-222222222222",
  "created_by": "user-id",
  "job_type": "import_customers",
  "status": "queued",
  "file_name": "customers.csv",
  "file_key": "current/internal/import/key",
  "content_type": "text/csv",
  "total_rows": 1,
  "processed_rows": 0,
  "succeeded_rows": 0,
  "failed_rows": 0,
  "request_payload": "{\"source\":\"imports_api\"}",
  "result_payload": "{}",
  "queued_at": "2026-09-01T12:00:00Z",
  "created_at": "2026-09-01T12:00:00Z",
  "updated_at": "2026-09-01T12:00:00Z"
}
```

> **Legacy warning:** `file_key` exposed internal storage topology. It is hidden
> from current job and artifact JSON. Frontend code must never model this field.

Legacy intake failures were `400 {"error":"file is required"}` for a missing part,
unstable `400` file-open/read text, `403` unstable permission text where the
service checks customer/vendor permissions, and otherwise unstable `500` text.
There is no file-size limit, encoding/content validation, command key, or safe
retry guarantee; a timeout may have created another queued job.

`GET /bulk-jobs?page=1&limit=20` returns `data`, `total`, `page`, and `limit`.
Defaults are page 1/limit 10 and the maximum limit is 100. Success example:

```json
{"data":[{"id":"44444444-aaaa-4aaa-8aaa-444444444444","business_id":"22222222-2222-4222-8222-222222222222","created_by":"user-id","job_type":"import_customers","status":"queued","total_rows":1,"processed_rows":0,"succeeded_rows":0,"failed_rows":0,"created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}],"total":1,"page":1,"limit":20}
```

`GET /bulk-jobs/{id}` returns a business-and-uploader-scoped job with optional rows and
artifacts. Job fields are `id`, `business_id`, `created_by`, `job_type`, optional
`action`, `status`, optional `file_name`, optional
`content_type`, `total_rows`, `processed_rows`, `succeeded_rows`, `failed_rows`,
optional `request_payload`, optional `result_payload`, optional `last_error`,
optional `queued_at`, `started_at`, `completed_at`, `created_at`, `updated_at`,
optional `rows`, and optional `artifacts`. JSON payload fields are encoded
strings; internal file keys are not serialized.

A row has `id`, `bulk_job_id`, `row_number`, `status`, optional `entity_id`,
optional `entity_type`, optional JSON-encoded `input` and `result`, optional
`error`, `created_at`, and `updated_at`. An artifact has `id`, `bulk_job_id`,
`artifact_type`, `file_name`, `status`, optional `expires_at`,
`created_at`, and `updated_at`. GET-list errors are unstable `500`; GET-one is
`404 {"error":"bulk job not found"}` for any lookup error. Reads are safe to
retry. Model-declared job states are `pending`, `queued`, `processing`,
`completed`, and `failed`, but this intake only establishes `queued` and has no
progress/restart event or invalidation contract.

This legacy request/response material is historical only. New clients must use
the two-phase JSON contract above. Evidence: `internal/app/runtime.go`,
`internal/handlers/billing_ops_handler.go`,
`internal/services/bulk_import_service.go`, `internal/models/swipe_ops.go`,
`migrations/000059_durable_bulk_imports.up.sql`, the bulk-import worker tests,
and `infrastructure/terraform/bulk_import.tf`.

## CART-001: Tenant-versioned editable AP2 cart mandate

`POST /agents/shopping/cart?agent_id={owned_shopping_agent_id}` accepts:

```json
{
  "product_ids": ["99999999-9999-4999-8999-999999999999"],
  "max_amount": 5000,
  "query": "office supplies",
  "expiration": 24
}
```

`expiration` defaults to 24 when zero. The result is a `CartMandate` with fields
`id`, `business_id`, optional `intent_mandate_id`, `user_id`, `agent_id`, optional
`merchant_id`, `items` (JSON-encoded string), `subtotal_amount`, `tax_amount`, `total_amount` (floating-point),
`currency`, `signature`, optional `merchant_signature`, `status`, `expires_at`,
`version`, `created_at`, `updated_at`, and optional `payment_mandates`. Public verification keys are not
returned. States are `pending`, `signed`, `rejected`, and `expired`.

```json
{"id":"55555555-aaaa-4aaa-8aaa-555555555555","user_id":"user-id","agent_id":"66666666-aaaa-4aaa-8aaa-666666666666","items":"[{\"product_id\":\"99999999-9999-4999-8999-999999999999\",\"quantity\":1}]","total_amount":299,"currency":"INR","signature":"opaque-signature","status":"pending","expires_at":"2026-09-02T12:00:00Z","created_at":"2026-09-01T12:00:00Z"}
```

| Method and path | Exact request | Success | Endpoint-specific failures |
| --- | --- | --- | --- |
| `POST /agents/shopping/cart?agent_id={id}` | body above | `201` `CartMandate` | missing query `400 {"error":"agent_id parameter is required"}`; inaccessible/non-shopping agent `404 {"error":"agent not found"}`; bind `400`; service `500` unstable text |
| `POST /agents/shopping/cart/add?agent_id={id}` | `{"product_id":"99999999-9999-4999-8999-999999999999"}` | `201` a **new** one-item `CartMandate` | same missing-agent/bind/status mapping; service `500` unstable text |
| `POST /agents/shopping/cart/{id}/items/{product_id}` | `{"version":1,"quantity":2}` | `200` updated cart, version incremented | bind/domain `400`; scope `404`; stale/state/stock/expiry `409` |
| `PATCH /agents/shopping/cart/{id}/items/{product_id}` | `{"version":2,"quantity":3}` | `200` updated cart, version incremented | bind/domain `400`; scope `404`; stale/state/stock/expiry `409` |
| `DELETE /agents/shopping/cart/{id}/items/{product_id}` | `{"version":3}` | `200` updated cart, version incremented | bind/domain `400`; scope `404`; stale/state/stock/expiry `409` |
| `DELETE /agents/shopping/cart/{id}/items` | `{"version":4}` | `200` empty pending cart, version incremented | bind `400`; scope `404`; stale/state/expiry `409` |
| `POST /agents/shopping/checkout` | `{"cart_mandate_id":"55555555-aaaa-4aaa-8aaa-555555555555","payment_method_id":"77777777-aaaa-4aaa-8aaa-777777777777"}` (`payment_method_id` optional) | `201` `PaymentMandate` | bind `400`; domain/signature/already-processed errors are unstable `500` text |
| `GET /agents/shopping/cart/{id}` | none | `200` `CartMandate` | `404 {"error":"cart not found"}` for any lookup error |
| `GET /agents/shopping/carts?page=1&limit=10` | none | `200 {"data":[],"total":0,"page":1,"limit":10}` | unstable `500` text |

A checkout response has `id`, `cart_mandate_id`, `user_id`, optional
`payment_method_id`, `amount`, `currency`, `signature`, optional
`razorpay_order_id`, optional `razorpay_payment_id`, `status`, optional
`processed_at`, and `created_at`.

```json
{"id":"88888888-aaaa-4aaa-8aaa-888888888888","cart_mandate_id":"55555555-aaaa-4aaa-8aaa-555555555555","user_id":"user-id","amount":299,"currency":"INR","signature":"opaque-signature","status":"pending","created_at":"2026-09-01T12:00:00Z"}
```

Payment states are `pending`, `authorized`, `captured`, `failed`, and
`refunded`. Cart-list pagination defaults to 1/10 and caps at 100. All routes
require bearer/effective-business context; cart creation additionally checks an
owner-or-same-business shopping agent, and reads are user-scoped. No explicit
permission, entitlement, step-up, command key, or event exists.

The legacy `/cart/add` endpoint still creates a new one-item mandate. New item
routes mutate only an owned, same-business, unexpired `pending` cart without a
payment. Every accepted mutation re-reads authoritative marketplace availability,
unreserved stock and price, resolves backing-product tax, recalculates totals,
clears merchant signature state, re-signs the buyer snapshot and atomically
increments `version`. A `409` is not success: refetch before another mutation.

Evidence: `internal/handlers/shopping_agent_handler.go`,
`internal/services/shopping_agent_service.go`, `internal/models/ap2_mandate.go`,
`internal/repositories/interfaces/ap2_repository.go`,
`internal/repositories/postgres/ap2_repo.go`,
`internal/services/shopping_agent_service_test.go`,
`internal/services/shopping_agent_signature_test.go`,
`internal/handlers/shopping_agent_track_order_test.go`,
`tests/unit/shopping_handler_test.go`, and `tests/integration/agent_test.go`.

## COUPON-001: Storefront coupon management

| Method and path | Permission | Success | Endpoint-specific failures |
| --- | --- | --- | --- |
| `GET /storefronts/{storefront_id}/coupons` | `storefront.view` | `200` array | storefront `404` with unstable text; other unstable `500` text |
| `POST /storefronts/{storefront_id}/coupons` | `storefront.manage` | `201` coupon | bind/service `400` unstable text; storefront `404` |
| `PUT /storefronts/{storefront_id}/coupons/{coupon_id}` | `storefront.manage` | `200` coupon | bind/service `400`; coupon/storefront `404`, all with unstable text |
| `DELETE /storefronts/{storefront_id}/coupons/{coupon_id}` | `storefront.manage` | `204` | coupon/storefront `404`; redeemed coupon `409` |
| `POST /public/store/{slug}/coupons/validate` | public, rate-limited | `200` validation result | bind `400`; missing/unpublished/not-accepting storefront `404`; other `500` text |

All require bearer auth and effective business scope. Create and update use the
same full input: required `code`, required `discount_type` (`percentage` or
`fixed`), required positive `discount_value`, `minimum_order_value`,
`max_discount_amount`, `usage_limit`, `usage_limit_per_customer`, optional
`starts_at`, optional `ends_at`, optional `is_active`, and optional `metadata`.

```json
{"code":"WELCOME10","discount_type":"percentage","discount_value":10,"minimum_order_value":500,"max_discount_amount":200,"usage_limit":100,"usage_limit_per_customer":1,"starts_at":"2026-09-01T00:00:00Z","ends_at":"2026-09-30T23:59:59Z","is_active":true,"metadata":{"campaign":"launch"}}
```

The response fields are `id`, `storefront_id`, `code`, `discount_type`,
`discount_value`, `minimum_order_value`, `max_discount_amount`, `usage_limit`,
`usage_limit_per_customer`, optional `starts_at`, optional `ends_at`,
`is_active`, optional `metadata` (JSON-encoded string), `created_at`, and
`updated_at`, `redemption_count`, and `version`.

```json
{"id":"99999999-aaaa-4aaa-8aaa-999999999999","storefront_id":"aaaaaaaa-bbbb-4aaa-8aaa-aaaaaaaaaaaa","code":"WELCOME10","discount_type":"percentage","discount_value":10,"minimum_order_value":500,"max_discount_amount":200,"usage_limit":100,"usage_limit_per_customer":1,"starts_at":"2026-09-01T00:00:00Z","ends_at":"2026-09-30T23:59:59Z","is_active":true,"metadata":"{\"campaign\":\"launch\"}","created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}
```

The GET list response is an array of that same shape, for example
`[{"id":"99999999-aaaa-4aaa-8aaa-999999999999","storefront_id":"aaaaaaaa-bbbb-4aaa-8aaa-aaaaaaaaaaaa","code":"WELCOME10","discount_type":"percentage","discount_value":10,"minimum_order_value":500,"max_discount_amount":200,"usage_limit":100,"usage_limit_per_customer":1,"is_active":true,"created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-01T12:00:00Z"}]`.

Validation accepts exact body `{"code":"WELCOME10","customer_email":"asha@example.test","subtotal":1000}`. A valid coupon returns
`200 {"valid":true,"discount_total":100}`. Rule failures such as inactive,
not-yet-active, expired, minimum order, or usage limit also return HTTP `200`,
for example `{"valid":false,"discount_total":0,"message":"coupon has
expired"}`. That message is legacy text and is not a stable error code.

Update remains full replacement for backward compatibility and accepts optional
`version`; a stale version returns `409`. The service validates activation
windows, non-negative minimum/maximum/usage controls, percentage bounds and
prevents lowering caps below redeemed usage. Checkout holds a coupon row lock
while rechecking all rules, writing the order/redemption and incrementing
`redemption_count`, so concurrent orders cannot exceed total or per-customer
caps. Delete returns `409` after any redemption. Create still has no command key;
refetch after ambiguous writes. There is no event contract.

Evidence: `internal/app/runtime.go`, `internal/handlers/commerce_handler.go`,
`internal/services/commerce_service.go`, `internal/models/commerce.go`,
`migrations/000032_add_storefront_enterprise_features.up.sql`,
`migrations/000060_cart_coupon_controls.up.sql`, and
`internal/services/commerce_service_test.go`,
`internal/services/commerce_checkout_postgres_integration_test.go`,
`internal/services/storefront_tenant_scope_test.go`,
`tests/unit/commerce_handler_test.go`, and
`tests/unit/commerce_public_handler_test.go`.

## REPORT-001: Synchronous JSON/CSV and native XLSX export

`POST /reports/{report_key}/export` requires bearer auth, effective business and
`reports.export`. Branch/warehouse scope is constrained by the handler; a report
that cannot be safely scoped returns `403`.

Exact request fields are optional `page`, `limit`, `columns`, `filters`, and
`format`. `format` is `json` by default and accepts `json`, `csv`, or `xlsx`.
Filter fields are `date_from`, `date_to`, `project_id`, `warehouse_id`,
`party_id`, `product_id`, `variant_id`, `category_id`, `search`, and
`include_cancelled`.

```json
{
  "page": 1,
  "limit": 100,
  "columns": ["serial_number", "total"],
  "filters": {
    "date_from": "2026-04-01T00:00:00Z",
    "date_to": "2026-09-01T23:59:59Z",
    "include_cancelled": false
  },
  "format": "csv"
}
```

JSON and CSV success remains the backward-compatible `201` JSON envelope:

```json
{
  "run": {
    "id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    "business_id": "22222222-2222-4222-8222-222222222222",
    "report_key": "sales_register",
    "run_kind": "export",
    "filters": "{\"date_from\":\"2026-04-01T00:00:00Z\"}",
    "visible_columns": "[\"serial_number\",\"total\"]",
    "export_format": "csv",
    "status": "completed",
    "payload": "{...}",
    "summary": "{...}",
    "generated_by": "user-id",
    "created_at": "2026-09-01T12:00:00Z",
    "updated_at": "2026-09-01T12:00:00Z"
  },
  "filename": "sales_register-20260901-120000.csv",
  "content_type": "text/csv",
  "data": "Number,Total\nINV-42,299.00\n"
}
```

CAP-001 failures use the stable capability mutation error documented above and
occur before report lookup/query/run creation. Other binding failures are
unstable `400` text. Exact current classified failures are
`400 {"error":"unsupported export format"}`, `400 {"error":"at least one
valid column is required"}`, `403 {"error":"report scope unavailable"}` or
`403` for an unsafe branch/warehouse projection, `404 {"error":"report not
found"}`, `413 {"error":"report export exceeds safe bounds"}`, and otherwise
unstable `500` text. The repository defaults to page 1 and 50 rows and caps a
page at 500 rows. A repeated request creates
another run/filename; there is no command idempotency, event, or safe mutation
retry contract. Refetch report history only after a confirmed `201`.

`filters`, `visible_columns`, `payload`, and `summary` inside `run` are
JSON-encoded strings. CSV cells beginning, after leading whitespace, with
`=`, `+`, `-`, `@`, or tab are prefixed with an apostrophe; this is covered by
tests.

For `format: "xlsx"`, success is `201` binary XLSX with:

```http
Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
Content-Disposition: attachment; filename="sales_register-20260902-120000.xlsx"
Cache-Control: no-store
X-Content-Type-Options: nosniff
X-Report-Run-ID: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa
Access-Control-Expose-Headers: Content-Disposition, X-Report-Run-ID
```

The Lambda adapter base64-encodes non-UTF-8 response bytes for API Gateway, and
direct HTTP clients receive the same XLSX bytes. Number/integer columns are
native numeric cells. Date/timestamp columns are native Excel date cells after
conversion to the business profile timezone. String headers and cells use the
same formula neutralization as CSV. Workbook generation rejects more than 500
rows, 64 columns, 32,767 characters in one cell, or 8 MiB of uncompressed
worksheet XML. Each accepted export creates a completed `report_runs` audit
record with business, actor, report, format, visible columns, filters, output
metadata, byte size and timezone; XLSX bytes are not persisted in that record.
There is no export idempotency key or export event. Report PDF remains deferred.

Evidence: `internal/app/runtime.go`, `internal/handlers/report_handler.go`,
`internal/services/report_service.go`, `internal/models/report.go`,
the registry under `internal/reporting`, `internal/services/report_service_test.go`,
`pkg/spreadsheet/xlsx_test.go`, `internal/handlers/report_handler_xlsx_test.go`,
and `internal/app/runtime_security_test.go`.

## Remaining frontend contracts

| Requested area | Current classification | Frontend instruction |
| --- | --- | --- |
| Internal capability diagnostics | `blocked` | Use customer-safe CAP-001 only. Provider internals require a future operator principal distinct from business owner/admin. |
| Renewable subscription lifecycle, billing history, cancellation/grace/proration and reconciliation | SUB-001/002/003 are `complete` locally; Razorpay remains `externally unverified` | Use the documented renewable lifecycle, but never infer provider payment or renewal from checkout success. |
| Aggregate operation status, operator detail and safe recovery actions | OPS-007 business projection and exact failed-render retry are `complete` locally; OPS-008 high-risk actions remain step-up-blocked | Use the aggregate projection and exact retry contract. Keep webhook/reconcile/DLQ/resolve disabled with `step_up_required`; never substitute admin/owner for operator. |
| Customer-safe aggregate GST/provider truth and recovery | `missing` around OPS-004/005/006 | Keep provider-backed success UI disabled: absent provider configuration selects a simulator that can fabricate IRN/ack/e-way bill values. A local succeeded state is not government-system evidence. Evidence: `internal/services/gst_provider.go`. |
| Staging verification evidence | `missing` and `externally unverified` | Do not label a provider operational from local tests. |
| Step-up, TOTP, durable devices/sessions and privacy workflows | Task 5 is `complete` locally; Cognito, S3, and restore drills remain `externally unverified` | Use only the generated contracts after deployment verification. OPS-008 provider-effect recovery remains intentionally blocked despite the separate scoped step-up surface. |
| Trial Balance, Balance Sheet, fiscal lock/opening balance and bank reconciliation | ACC-001/002 `complete` locally; bank object/scanner deployment externally unverified | Enable from the ACC contracts after migration `000058`; do not label uploaded bank files operational until storage/scanning is verified. |
| Durable two-phase customer/vendor/product import | IMP-001 `complete` locally; S3/SQS deployment externally unverified | Enable after migration `000059`, worker deployment, queue alarm, upload, artifact, and notification smoke verification. |
| Verified upload completion, editable cart, race-safe coupons and typed XLSX | ASSET-001 logo, CART-001, COUPON-001, and REPORT-001 are `complete` locally; drive completion remains `partial`; S3 is `externally unverified` | Use the completed contracts after their migrations. Keep drive completion unavailable and never simulate upload completion client-side. |
| Governed high-risk AI effect adapters and public operator controls | Core runtime governance exists; no high-risk adapter or public approval/kill-switch API is registered | Keep `AI_AGENT_EXECUTION_ENABLED=false`; do not enable high-risk agent actions. |
| Saved payment methods | `deferred`; every current mutation is fail-closed by CAP-001 | Keep unavailable; read-only legacy views do not authorize creation or token use. |
| Official GST return filing | `deferred` | Do not expose filing. Existing simulated provider output is not filing evidence. |
| Automatic recurring issue/send | `deferred`; recurring draft creation exists | Do not label recurring drafts as auto-sent invoices. |
| Full offline POS sync, report PDF export, web voice, marketplace/A2A/AP2 breadth, new bargaining modes | `deferred` | Keep outside Phase 2 release claims. |

## Rollout and validation truth

- No frontend repository or frontend file was changed for this baseline.
- No live deployment, Terraform apply, provider account mutation, Razorpay charge,
  OTP, object upload, queue message, outbound email/WhatsApp, GST submission, or
  AI call was made. All such behavior is externally unverified.
- Swagger was regenerated from `internal/app/runtime.go` during Task 10; the
  tracked generated files were already current and produced no diff. The two
  manually curated OpenAPI contracts remain covered by route/contract tests.
- At `bc0c138` on 2026-09-02, `make fmt`, `make lint`, and `make test` passed;
  `make test` ran the full Go race/coverage suite. A focused `go test` over
  `internal/app`, `internal/config`, `internal/handlers`, `internal/middleware`,
  `internal/repositories/postgres`, `internal/services`, `internal/verify`,
  `migrations`, and `tests/...` also passed route, Swagger/OpenAPI, migration,
  tenant, sensitive-data, idempotency, reconciliation, inventory, and payment
  name filters with `-count=1`.
- `terraform -chdir=infrastructure/terraform fmt -check -recursive`,
  `terraform -chdir=infrastructure/terraform validate`, and
  `terraform -chdir=infrastructure/terraform test` passed without apply; mocked
  Terraform tests reported 88 passed and 0 failed.
  Validate/test retained existing DynamoDB key deprecation warnings. Live
  PostgreSQL AI governance cases skipped because `MIGRATION_TEST_DATABASE_URL`
  was not configured.
- The non-filing GST reconciliation follow-up is
  `docs/plans/BILLEIF_GST_RECONCILIATION_FOLLOWUP.md`. It does not authorize
  filing, provider mutation, or exposure of current unsafe GST projections.
