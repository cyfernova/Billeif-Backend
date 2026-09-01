# Billeif Phase 2 Frontend Handoff

Status: initial evidence baseline at source revision `5b58a56`

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
  enforced separately where noted. There is no Phase 2 step-up token yet.
- UUID examples are illustrative. Timestamps are JSON RFC 3339 timestamps.
- Except where an exact stable shape is stated, current failures are
  `{ "error": "..." }`; message text is not a stable machine contract. Branch
  on HTTP status only until a later contract adds stable codes.
- Provider or signed-storage URLs are opaque and short-lived. Never log them or
  persist them as durable identifiers.

## Contract index

| ID | Current contract | Classification | Frontend use at this revision |
| --- | --- | --- | --- |
| SUB-001 | Subscription catalog | `complete` locally | May render catalog data, with the one-month purchase warning from SUB-003. |
| SUB-002 | Current subscription and legacy feature rows | `partial` | May show current stored state; do not infer renewal, provider health, or runtime capability. |
| SUB-003 | Razorpay checkout and verification | `partial`, lifecycle wording `unsafe`, provider `externally unverified` | Test-only integration until Task 2 proves provider lifecycle. Do not label auto-renewing. |
| OPS-001 | Invoice render status | `complete` for current safe projection | Pollable. |
| OPS-002 | Invoice delivery create/status | `complete` for current safe projection | Usable with UUID idempotency key; poll status. |
| OPS-003 | In-app notifications | `complete` for current non-paginated contract | Usable; refresh/poll because no versioned event contract is promised here. |
| SEC-001 | One-use WebSocket ticket | `complete` locally, deployed WebSocket `externally unverified` | Usable where the deployed WebSocket is separately verified. |
| AUTH-001 | Indian phone OTP auth | `partial`, Cognito/SMS `externally unverified` | Do not ship as hardened account-linking/session-management UX yet. |
| ASSET-001 | Business-logo and drive presigns | `partial` and `unsafe` as completed-asset workflows; S3 `externally unverified` | Do not mark an asset complete after PUT; completion verification does not exist. |
| IMP-001 | Current bulk import intake/read | `partial` and `unsafe` | Do not enable as a production import workflow; no processor was found. |
| CART-001 | AP2 signed cart mandate | `partial` and `unsafe` as the requested editable cart | Existing clients only; no update/remove/clear contract. |
| COUPON-001 | Storefront coupon list/create/update | `partial` and concurrency `unsafe` | Management UI may inspect it, but usage caps are not race-safe. |
| REPORT-001 | Report JSON/CSV export envelope | `partial` | Existing synchronous JSON/CSV only; not an XLSX or file-download contract. |

## Cross-contract controls and client lifecycle

The detailed sections below own exact schemas and examples. These matrices make
the remaining required handoff properties explicit rather than leaving an
omitted column to imply support.

| ID | Auth and business scope | Permission | Entitlement | Step-up | Idempotency |
| --- | --- | --- | --- | --- | --- |
| SUB-001 | Bearer + effective business | `subscriptions.view` | None | None implemented | Read-only |
| SUB-002 | Bearer + effective business | `subscriptions.view`; sync/direct mutations use `subscriptions.manage` | Subscription is the entitlement source | None implemented | Reads are safe to repeat; sync is convergent but has no command key; direct mutation has no idempotency key |
| SUB-003 | Bearer + effective business + all branches | `payments.manage` | None | None implemented | Required body `idempotency_key` for order; verify locks and safely re-observes a paid attempt |
| OPS-001 | Bearer + effective business + all branches | `documents.export` | None | None implemented | Read-only |
| OPS-002 | Bearer + effective business + all branches | Create `documents.manage`; read `documents.export` | None | None implemented | Required UUID `Idempotency-Key`; same actor/payload replays, changed payload conflicts |
| OPS-003 | Bearer + effective business + authenticated user | No additional permission | None | None implemented | Source ingestion is unique; read mutations have no client command key and are naturally convergent |
| SEC-001 | Bearer + effective business + authenticated user | No additional permission | None | None implemented | Ticket is one-use; issue has no command key |
| AUTH-001 | Public except logout, which requires bearer + effective business through the protected group | No business scope during public flow | None | None implemented | No command key; provider OTP/session controls apply |
| ASSET-001 | Logo: bearer + owned business; drive: bearer + effective business | Logo owner check; drive `drive.manage` | Logo none; drive `drive_storage_mb` | None implemented | No command key and no completion idempotency |
| IMP-001 | Bearer + effective business + authenticated user | Customer/vendor create permissions; product/invoice/document incomplete | None enforced | None implemented | No command/row idempotency |
| CART-001 | Bearer + effective business; user and owned shopping-agent checks | No additional permission | None | None implemented | No command key; signatures cover a created mandate but add creates a new mandate |
| COUPON-001 | Bearer + effective business | View `storefront.view`; writes `storefront.manage` | None enforced on coupon routes | None implemented | No command/version key |
| REPORT-001 | Bearer + effective business + branch/warehouse scope | `reports.export` | No report-export plan entitlement enforced | None implemented | No command key; every request creates a report run |

| ID | Pagination / file behavior | State and retry | Event / invalidation | Existing migration and rollout | Proof and governing limitation |
| --- | --- | --- | --- | --- | --- |
| SUB-001 | No pagination or file | Synchronous read; retry safe | No event; invalidate on catalog-version/deployment change | No Task 0 migration; catalog is code-defined | Catalog service tests; one-month checkout is not renewal |
| SUB-002 | No pagination or file | Stored `active`/`canceled`/`expired` model; refetch after payment | No versioned event; invalidate subscription and entitlement queries after verify/sync | Existing subscription and feature-entitlement schema; keep direct writes disabled | Subscription/commerce/entitlement tests; runtime capability and lifecycle are incomplete |
| SUB-003 | No pagination or file | Attempt `created`/`pending`/`paid`/`failed`; do not blindly retry unknown provider outcomes | No client event; invalidate SUB-002 after verified paid response | `migrations/000041_add_razorpay_payment_attempts.up.sql`; test-mode/provider rollout unverified | Razorpay service/handler/IaC tests; no recurring lifecycle or reconciliation |
| OPS-001 | No pagination or file; PDF download is separate | Poll `queued`/`processing`; stop on `completed`/`failed`/`obsolete` | No versioned client event; invalidate invoice PDF/download state on terminal state | Existing render/outbox migrations; deployed worker unverified | Render service/handler/repository/Lambda tests; no safe retry API |
| OPS-002 | No pagination or file | Reuse same key after timeout; poll until terminal | No versioned client event promised; invalidate invoice and delivery queries on change | Existing canonical delivery/outbox schema; SES/deployed worker unverified | Delivery unit/integration tests; no customer retry action or provider detail |
| OPS-003 | Limit only, maximum 200; no cursor/file | Read operations are retry-safe | No versioned notification-event name promised; invalidate list after mark-read | `migrations/000051_notifications.up.sql`; deployed delivery unverified | Handler/service/repository tests; capped non-cursor list |
| SEC-001 | No pagination/file | Never reuse; issue another after expiry/failure | Successful connect owns later channel behavior; no issuance event | `migrations/000050_websocket_tickets.up.sql`; deployed WebSocket unverified | Ticket service/repository/handler/Lambda tests; at most 60-second TTL |
| AUTH-001 | No pagination/file | OTP/session retry follows current rate/cooldown behavior; errors are not stable | No auth event contract; invalidate local session/profile after verify/refresh/logout | `migrations/000023_add_phone_auth_fields.up.sql`; Cognito/SMS rollout unverified | Auth tests; enumeration, linking, device/session, MFA and audit gaps |
| ASSET-001 | PUT bytes to opaque signed URL; no download/completion contract here | Request a new URL after expiry; PUT success is not completion proof | No asset-ready event; do not invalidate/show final asset as complete | Existing asset schema and private-bucket Terraform; S3 externally unverified | S3/business/commerce tests; missing checksum/HEAD/scan/reference transaction |
| IMP-001 | Job list uses page/limit; upload is multipart; job payloads are JSON strings | Queued rows have no processor/restart contract | No import event; do not depend on progress invalidation | `migrations/000030_add_swipe_billing_ops.up.sql`; keep UI disabled | Billing-ops tests prove intake/read only; no durable validation/commit worker |
| CART-001 | Cart list uses page/limit; no file | Mandate states `pending`/`signed`/`rejected`/`expired`; do not retry checkout after ambiguous side effect | No versioned cart event; invalidate/refetch created mandate only | Existing AP2 mandate schema; keep out of new editable-cart UI | Shopping/AP2/signature tests; no mutation concurrency or authoritative pricing/stock |
| COUPON-001 | List is an array without pagination; no file | Active/date rules exist; retrying writes can duplicate without a client key | No versioned coupon event; invalidate storefront coupon list after writes | `migrations/000032_add_storefront_enterprise_features.up.sql`; avoid claiming hard caps | Commerce/storefront tests; redemption/update concurrency is unsafe |
| REPORT-001 | Input page/limit; response embeds JSON or CSV string, not a file response | Synchronous `completed`/`failed` run model; repeat creates another run | No export event; invalidate report-run/history views after success | Existing report-run schema; XLSX rollout missing | Report service/handler tests; no XLSX, typed cells, disposition or export idempotency |

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

Limitation: `interval: "month"` describes the purchased access period. The
audited Razorpay implementation does not create an auto-renewing provider
subscription.

## SUB-002: Current subscription and feature rows

`GET /subscriptions`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token plus effective business |
| Permission | `subscriptions.view` |
| Success | `200` subscription object; `404 {"error":"subscription not found"}` |
| Evidence | `internal/handlers/subscription_handler.go`, `internal/models/subscription.go`, subscription service/repository tests |

Exact fields are `id`, `business_id`, `plan`, optional `plan_code`, optional
`catalog_version`, `status`, `max_invoices`, `max_customers`, `max_users`,
`max_storage_mb`, `start_date`, optional `end_date`, optional
`next_billing_date`, `created_at`, and `updated_at`. Model-declared status values
are `active`, `canceled`, and `expired`; legacy plan values are `free`,
`starter`, `professional`, and `enterprise`. The current update service does not
validate a non-empty status against that set, which is another reason clients
must not use the direct update route.

```json
{
  "id": "11111111-1111-4111-8111-111111111111",
  "business_id": "22222222-2222-4222-8222-222222222222",
  "plan": "starter",
  "plan_code": "pro",
  "catalog_version": "swipe-v1",
  "status": "active",
  "max_invoices": 100,
  "max_customers": 100,
  "max_users": 3,
  "max_storage_mb": 512,
  "start_date": "2026-09-01T12:00:00Z",
  "end_date": "2026-10-01T12:00:00Z",
  "next_billing_date": "2026-10-01T12:00:00Z",
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
`internal/services/entitlements.go`, and entitlement/commerce tests.

Do not use current `POST /subscriptions` or `PUT /subscriptions` to buy or switch
paid plans. The service rejects paid creation and plan changes, while the handler
currently maps those errors to unstable `500` responses.

## SUB-003: Razorpay one-month checkout

`POST /payments/razorpay/order`

| Property | Current value |
| --- | --- |
| Auth and scope | Bearer token, effective business, all-branches scope |
| Permission | `payments.manage` |
| Step-up / entitlement | None |
| Idempotency | Body `idempotency_key`, required; scoped to user and business |
| Success | `200` |
| Provider state | `externally unverified`; returns `503` when unconfigured |
| Evidence | `internal/handlers/razorpay_payment_handler.go`, `internal/services/razorpay_payment_service.go`, `internal/models/payment_attempt.go`, payment tests, `migrations/000041_add_razorpay_payment_attempts.up.sql` |

Order request and response:

```json
{
  "target_type": "plan",
  "plan_id": "pro_monthly",
  "idempotency_key": "33333333-3333-4333-8333-333333333333"
}
```

```json
{
  "payment_attempt_id": "44444444-4444-4444-8444-444444444444",
  "razorpay_key_id": "rzp_test_example",
  "razorpay_order_id": "order_example",
  "amount": 29900,
  "currency": "INR"
}
```

For a store order, use `target_type: "store_order"` with
`store_order_id`; omit `plan_id`. The server calculates amount and currency.
Reusing the idempotency key with a different target/amount/currency conflicts.

`POST /payments/razorpay/verify` accepts:

```json
{
  "payment_attempt_id": "44444444-4444-4444-8444-444444444444",
  "razorpay_order_id": "order_example",
  "razorpay_payment_id": "pay_example",
  "razorpay_signature": "opaque-signature"
}
```

Success is `200 {"status":"paid"}`. The backend verifies the checkout
signature and fetches trusted provider payment/order state before applying the
result. Current sanitized errors include `400 invalid request`, `400 invalid
payment verification`, `404 payment target not found`, `409 payment request
conflicts with an existing attempt`, `503 payment provider is not configured`,
and `502 payment provider request failed`.

After a plan payment, refetch SUB-002. The stored period starts at payment time
and ends one month later. There is no renewal, grace, cancel-at-period-end,
proration, billing-history, provider-subscription ID, reconciliation endpoint,
or client event contract. A timeout/unknown outcome must not be shown as failed
or retried blindly.

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
| Evidence | `internal/handlers/invoice_handler.go`, `internal/services/invoice_service.go`, render model/repository and focused tests |

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
| Evidence | `internal/handlers/invoice_handler.go`, `internal/services/invoice_delivery.go`, PostgreSQL atomic-delivery adapter and unit/integration tests |

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
`404 notification not found`. Evidence: notification model, handler, service,
repository, `migrations/000051_notifications.up.sql`, and focused tests.

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
`internal/handlers/websocket_ticket_handler.go`, WebSocket handler/Lambda and
PostgreSQL atomic-consume tests, `migrations/000050_websocket_tickets.up.sql`.

## AUTH-001: Indian phone OTP authentication

These endpoints are public except logout. Logout is in the bearer- and
business-protected route group. They are rate limited in
`internal/app/runtime.go`; Cognito and SMS are externally unverified.

| Method and path | Exact request | Current success |
| --- | --- | --- |
| `POST /auth/phone/register` | `{"phone_number":"9876543210","name":"Asha"}` | `201 {"user_id":"...","phone_number":"+919876543210","message":"OTP sent to your phone number"}` |
| `POST /auth/phone/confirm` | `{"phone_number":"+919876543210","code":"123456"}` | `200 {"message":"phone number verified successfully"}` |
| `POST /auth/phone/resend-confirmation` | `{"phone_number":"+919876543210"}` | `200 {"message":"verification code resent"}` |
| `POST /auth/phone/login` | `{"phone_number":"+919876543210"}` | `200 {"challenge_name":"SMS_OTP","session":"opaque","message":"OTP sent to your phone number"}` |
| `POST /auth/phone/verify-login` | `{"phone_number":"+919876543210","code":"123456","session":"opaque"}` | `200` token object |
| `POST /auth/phone/refresh` | `{"refresh_token":"opaque"}` | `200` token object |
| `POST /auth/phone/logout` | no body; bearer access token | `200 {"message":"logged out successfully"}` |

The token object fields are `access_token`, `refresh_token`, `expires_in`, and
`token_type`. Registration accepts an optional `email` field syntactically but
the service deliberately rejects non-empty email; explicit cross-provider
linking does not exist. Phone input is normalized to an Indian `+91` E.164
number. The login precheck currently distinguishes an unregistered number, so
errors are not enumeration-resistant. The OTP cooldown returns without enforcing
a durable cooldown when its DynamoDB table/client is unavailable. The app stores
a local user before phone confirmation and has no durable device/session registry,
TOTP MFA, privacy audit, or one-time step-up. Treat error messages as unstable.

Evidence: `internal/handlers/auth_handler.go`, `internal/services/auth_service.go`,
auth handler/service tests, and `migrations/000023_add_phone_auth_fields.up.sql`.

## ASSET-001: Current upload presigns

### Business logo

`POST /business-profiles/{business_id}/logo?size_bytes=2048` requires bearer auth
and business ownership. Send the intended MIME type as `Content-Type`; if absent,
the handler defaults to `image/png`. Allowed image types are GIF, JPEG, PNG, SVG,
and WebP; size must be 1 byte through 5 MiB. Success is:

```json
{
  "upload_url": "https://opaque-signed-storage-url.example",
  "required_headers": {
    "Content-Length": "2048",
    "Content-Type": "image/png"
  }
}
```

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

For both flows, upload with exactly the returned headers. Critically, neither
flow has a completion endpoint, checksum, HEAD/metadata verification,
quarantine/scan, or transactional reference update. Drive creates the database
asset before bytes arrive; a failed PUT can leave a ghost record. Logo presign
does not update `logo_url`. Therefore a successful PUT is not a proven completed
asset at this revision.

Evidence: business and commerce handlers/services/models, `internal/services/s3_service.go`,
upload tests, and private-bucket Terraform.

## IMP-001: Current bulk import intake is not production-ready

The registered multipart routes are `POST /imports/customers`, `/vendors`,
`/products`, `/invoices`, and `/documents`. Customer/vendor have explicit create
permissions; the other three do not have the complete Phase 2 permission model.
Each accepts `multipart/form-data` with a required file field named `file`. The
current handler reads the full uploaded file into memory, optionally stores it
in S3, parses CSV rows synchronously, and creates a queued `BulkJob`. No worker
that commits those rows was found, so a successful intake can remain queued
forever. Invoice/document import is outside Task 7's intended scope.

`GET /bulk-jobs?page=1&limit=20` returns `data`, `total`, `page`, and `limit`.
`GET /bulk-jobs/{id}` returns a business-scoped job with optional rows and
artifacts. Job fields are `id`, `business_id`, `created_by`, `job_type`, optional
`action`, `status`, optional `file_name`, optional `file_key`, optional
`content_type`, `total_rows`, `processed_rows`, `succeeded_rows`, `failed_rows`,
optional `request_payload`, optional `result_payload`, optional `last_error`,
optional `queued_at`, `started_at`, `completed_at`, `created_at`, `updated_at`,
optional `rows`, and optional `artifacts`. JSON payload fields are encoded
strings; internal file keys are exposed.

Do not enable imports in a production frontend until Task 7 replaces this with
preview/validation, explicit UUID commit, worker progress, restart-safe row
idempotency, artifacts, cancellation, retention, stable errors and notifications.
Evidence: billing-ops handler/service/model,
`migrations/000030_add_swipe_billing_ops.up.sql`, and absence of
a bulk-import processor under `internal/workers` and `cmd`.

## CART-001: Existing AP2 cart mandate

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
`id`, optional `intent_mandate_id`, `user_id`, `agent_id`, optional
`merchant_id`, `items` (JSON-encoded string), `total_amount` (floating-point),
`currency`, `signature`, optional `merchant_signature`, `status`, `expires_at`,
`created_at`, and optional `payment_mandates`. Public verification keys are not
returned. States are `pending`, `signed`, `rejected`, and `expired`.

Existing related endpoints are POST `/agents/shopping/cart/add` with
`{"product_id":"..."}`, GET `/agents/shopping/cart/{id}`, GET
`/agents/shopping/carts`, and checkout. They require bearer auth; access is
user/owned-agent scoped. The add endpoint does not mutate `{id}`: it creates a
new one-item mandate. There are no update/remove/clear, business/branch/version,
or authoritative current price/availability/stock checks. Do not model this as
the requested editable commerce cart.

Evidence: `internal/handlers/shopping_agent_handler.go`,
`internal/services/shopping_agent_service.go`, `internal/models/ap2_mandate.go`,
AP2 repository and signature/checkout tests.

## COUPON-001: Storefront coupon management

| Method and path | Permission | Success |
| --- | --- | --- |
| `GET /storefronts/{storefront_id}/coupons` | `storefront.view` | `200` array |
| `POST /storefronts/{storefront_id}/coupons` | `storefront.manage` | `201` coupon |
| `PUT /storefronts/{storefront_id}/coupons/{coupon_id}` | `storefront.manage` | `200` coupon |

All require bearer auth and effective business scope. Create and update use the
same full input: required `code`, required `discount_type` (`percentage` or
`fixed`), required positive `discount_value`, `minimum_order_value`,
`max_discount_amount`, `usage_limit`, `usage_limit_per_customer`, optional
`starts_at`, optional `ends_at`, optional `is_active`, and optional `metadata`.

The response fields are `id`, `storefront_id`, `code`, `discount_type`,
`discount_value`, `minimum_order_value`, `max_discount_amount`, `usage_limit`,
`usage_limit_per_customer`, optional `starts_at`, optional `ends_at`,
`is_active`, optional `metadata` (JSON-encoded string), `created_at`, and
`updated_at`.

There is no delete route or version field. Update is a full replacement for the
required values and overwrites omitted numeric limits with zero. Checkout checks
dates/minimum/usage, but usage counting and redemption insertion are not
serialized on the coupon; concurrent orders can exceed caps. Do not promise a
hard usage ceiling until Task 8 fixes this race.

Evidence: commerce routes/handler/service/model,
`migrations/000032_add_storefront_enterprise_features.up.sql`, and
storefront checkout tests.

## REPORT-001: Current synchronous JSON/CSV export

`POST /reports/{report_key}/export` requires bearer auth, effective business and
`reports.export`. Branch/warehouse scope is constrained by the handler; a report
that cannot be safely scoped returns `403`.

Exact request fields are optional `page`, `limit`, `columns`, `filters`, and
`format`. `format` is `json` by default and accepts only `json` or `csv`.
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

Success is `201` JSON, not a streamed file:

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

`filters`, `visible_columns`, `payload`, and `summary` inside `run` are
JSON-encoded strings. CSV cells beginning, after leading whitespace, with
`=`, `+`, `-`, `@`, or tab are prefixed with an apostrophe; this is covered by
tests. There is no XLSX, native file response, `Content-Disposition`, typed
money/date cells, timezone-aware spreadsheet output, export idempotency key, or
export event. Report PDF export is explicitly deferred.

Evidence: report handler/service/model/reporting registry and focused tests.

## No frontend contract yet

| Requested area | Current classification | Frontend instruction |
| --- | --- | --- |
| CAP runtime capability endpoint and diagnostics | `missing` | Do not infer availability from route presence, environment variables, plan rows, or UI feature flags. |
| Renewable subscription lifecycle, billing history, cancellation/grace/proration and reconciliation | `missing` around a `partial` one-month flow | Do not show auto-renewal or authoritative next charge. |
| Aggregate operation status, operator detail and safe recovery actions | `missing` around complete individual statuses | Poll OPS-001/002 only; do not invent retries or DLQ actions. |
| GST/e-invoice/e-way bill provider truth and reconciliation | `unsafe`, `partial`, provider `externally unverified` | Keep provider-backed success UI disabled: absent provider configuration selects a simulator that can fabricate IRN/ack/e-way bill values. A local succeeded state is not government-system evidence. Evidence: `internal/services/gst_provider.go`. |
| Staging verification evidence | `missing` and `externally unverified` | Do not label a provider operational from local tests. |
| Step-up, TOTP, durable devices/sessions and privacy workflows | `missing` | Do not expose placeholder controls. |
| Trial Balance, Balance Sheet, fiscal lock/opening balance and bank reconciliation | `missing` around existing journal invariants | Existing reports/journals do not prove these Phase 2 contracts. |
| Durable two-phase customer/vendor/product import | `missing` around unsafe intake | Keep imports disabled. |
| Verified upload completion, editable cart, race-safe coupons and typed XLSX | `missing` around partial surfaces | Do not simulate completion client-side. |
| AI risk/approval/budget/kill-switch governance | `missing` around descriptive capabilities and read-only voice tools | Do not enable governed high-risk agent actions. |
| Saved payment methods | `deferred` | Keep unavailable. |
| Official GST return filing | `deferred` | Do not expose filing. Existing simulated provider output is not filing evidence. |
| Automatic recurring issue/send | `deferred`; recurring draft creation exists | Do not label recurring drafts as auto-sent invoices. |
| Full offline POS sync, report PDF export, web voice, marketplace/A2A/AP2 breadth, new bargaining modes | `deferred` | Keep outside Phase 2 release claims. |

## Rollout and validation truth

- No frontend repository or frontend file was changed for this baseline.
- No live deployment, Terraform apply, provider account mutation, Razorpay charge,
  OTP, object upload, queue message, outbound email/WhatsApp, GST submission, or
  AI call was made. All such behavior is externally unverified.
- Controller evidence records AWS CLI `2.36.7` and a successful read-only STS
  identity check for profile `default` in `ap-south-1`. The identity was the
  account root principal, so all further live AWS inspection was intentionally
  stopped for safety. No account identifier is included; aside from that
  identity check, no AWS resource was mutated and no application-provider call
  was made. Task 4 needs a least-privilege non-production identity.
- The controller-recorded baseline at `5b58a56` is `make test` passing on
  2026-09-01, including race and coverage. Task 0 did not rerun it.
- Task 10 must regenerate OpenAPI and replace these limitations only after the
  corresponding behavior, migrations, permissions, tests, rollout gates and
  provider verification are complete.
