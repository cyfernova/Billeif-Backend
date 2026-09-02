# GST Reconciliation Non-Filing Follow-Up

Status: planned; implementation and provider validation not started

## Boundary

Build a tenant-safe accounting workflow that imports GST source data, reconciles
it with Billeif records, explains mismatches, and exports review evidence. It
must not prepare, submit, amend, cancel, or claim acceptance of any GST return,
e-invoice, or e-way bill. Provider acknowledgements remain authoritative and a
simulated response is never government evidence.

Official filing, filing credentials, portal automation, and autonomous tax
decisions are excluded. Any future filing work requires a separate specification,
security review, approval model, provider certification, and release gate.

## Required outcome

- Replace synchronous GSTR-2B matching with a durable, period-scoped run.
- Bind every run, source object, match, exception, export, and command to
  `business_id`; preserve branch scope where ledger records carry it.
- Accept only verified pending uploads or revision-bound provider snapshots.
- Normalize GSTIN, invoice number/date, place of supply, tax components, currency,
  and integer minor-unit amounts before matching.
- Produce explainable match states: exact, tolerance match, missing in books,
  missing in source, value mismatch, tax mismatch, identity mismatch, duplicate,
  and manual review.
- Keep source facts immutable. Corrections create new source revisions or
  compensating accounting records; reconciliation never edits posted journals.
- Export review evidence only. Every response and file must state `not_filed` and
  contain no government acknowledgement unless copied from a verified provider
  response with its provenance.

## Delivery sequence

1. Harden GST account projections so customer APIs expose no credentials,
   provider metadata, raw errors, internal object coordinates, or simulator
   claims. Remove simulated success from production capability health.
2. Add paired expand-first migrations for reconciliation runs, immutable source
   revisions, normalized lines, matches, exceptions, command claims, and exports.
3. Implement bounded streaming ingestion with checksum, content-type, row-count,
   formula, duplicate, tenant, period, and GSTIN validation. Record row errors
   without mutating accounting data.
4. Implement a deterministic matching engine with versioned rules and explicit
   tolerance policy. Store the rule version and reason codes for every result.
5. Add idempotent start/cancel/retry/export commands. A timeout after provider or
   storage side effects must become `reconciliation_required`, never success.
6. Add reviewer assignment, notes, and exception resolution with accounting
   permissions and audit. Resolution may link or annotate records; it cannot file
   or silently change posted financial data.
7. Add customer-safe status, summary, exception, and export contracts to the
   frontend handoff and generated OpenAPI only after handlers and tests exist.
8. Validate in a non-production provider sandbox with a least-privilege identity;
   retain `externally unverified` until source retrieval, rotation, timeout,
   replay, and acknowledgement provenance are observed.

## Controls and acceptance

- Require bearer authentication, effective-business membership, `reports.view`
  for reads, `documents.manage` for imports, and `reports.export` for exports.
- Use UUID command identities plus tenant, actor, action, period, source revision,
  and normalized request hash. Same input converges; changed reuse conflicts.
- Serialize competing runs for the same business, GSTIN, period, source, and rule
  version. Concurrent retries cannot duplicate matches, exceptions, or exports.
- Audit actor, tenant, source revision, rule version, counts, reason codes,
  transitions, export reference, and safe failure code. Never persist plaintext
  credentials, authorization headers, signed URLs, raw provider bodies, or
  unnecessary uploaded-document text.
- Test cross-tenant reads/mutations, wrong-period and wrong-GSTIN uploads,
  duplicate rows/files, formula payloads, rounding boundaries, concurrent starts,
  replay with changed input, cancellation, provider timeout after effect,
  unknown outcome, export isolation, and sensitive-log redaction.
- Run migration up/down tests, focused service/repository/handler tests, live
  PostgreSQL concurrency tests, generated-contract checks, Terraform format,
  validate, and mocked tests. Do not run Terraform apply or a GST filing action.

## Rollout and rollback

Deploy migrations before workers and handlers. Keep capability unavailable until
storage, queues, scanners, provider credentials, and alarms are verified. Enable
read-only internal review first, then bounded imports and exports. Roll back the
application and workers before down migrations; retain reconciliation evidence
according to the accounting retention policy. A rollback never deletes source
evidence or converts an unknown outcome into failure or success.
