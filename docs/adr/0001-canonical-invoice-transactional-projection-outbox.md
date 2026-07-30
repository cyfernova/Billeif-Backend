# ADR 0001: Canonical Invoice with transactional projection and outbox

- Status: Accepted
- Date: 2026-07-30
- Scope: Billeif sales invoices and bills of supply
- Implementation: Target prelaunch architecture; acceptance does not assert deployment

## Context

Billeif can originate sales invoices from manual entry, POS, storefront
orders, subscription runs, and document conversion. Letting each path create a
different invoice shape, or letting the generic document model act as a second
aggregate, creates inconsistent numbering, tax, party, and settlement rules.

Invoice changes also initiate rendering and delivery work. Updating
PostgreSQL and publishing work as unrelated operations creates a dual-write
gap: the business change may commit without its work, or work may become
visible for a business change that later rolls back.

## Decision

`Invoice` is the sole authoritative aggregate for every Billeif sales invoice
and bill of supply.

- Origin is immutable provenance, not a choice of invoice model.
- A Draft Invoice is editable, versioned, and unnumbered.
- Issuance is the only allocation point for an Invoice Number.
- Issuance locks the draft, allocates the number, freezes legal facts, creates
  the Final Render work, and records its event as one transaction.
- Payments, balance, cancellation, renders, and Delivery Attempts remain
  separately auditable lifecycle facts.

The generic Document is a same-identifier projection of the Invoice. It
supports presentation and cross-document reporting, but it cannot originate
or independently modify sales-invoice truth. Sales-invoice commands entering
through a generic document surface delegate to Sales Invoicing. The Invoice
and its Document Projection are written in the same transaction.

Asynchronous work uses a transactional outbox:

1. The accepted business transition, idempotency result, job, and outbox event
   commit together.
2. A post-commit publication attempt may reduce latency.
3. A periodic publisher recovers any unpublished event.
4. Consumers assume at-least-once delivery and claim versioned work with
   bounded leases.
5. Duplicate work is harmless; obsolete previews are suppressed; final output
   is deterministic.

The outbox event is the durable publication intent. A successful immediate
attempt is an optimization, never an alternative source of truth.

## Consequences

- All sales origins share one numbering, issuance, tax, idempotency, and
  projection path.
- Draft creation does not consume legal serial numbers.
- Queue or provider outages do not make an issued Invoice ambiguous; pending
  work remains recoverable.
- The transaction contains more coordinated records and therefore requires
  explicit rollback, concurrency, and bounded-query tests.
- Projection drift is repaired from the Invoice, not by choosing between two
  peers.
- Workers require idempotent completion, lease expiry recovery, and
  version-aware stale-work handling.
- Delivery and settlement reporting cannot infer state from Invoice issuance
  or one another.

## Alternatives rejected

### Keep Invoice and Document as peer aggregates

Rejected because legal, pricing, party, and settlement facts could diverge and
each origin would need to choose which aggregate governs.

### Allocate numbers when a draft is created

Rejected because abandoned drafts would consume legal serials and draft work
would be confused with issuance.

### Publish directly after a database transaction

Rejected because a crash between commit and publication loses required work.
Retried direct publication can also duplicate work without a durable command
record.

### Generate PDFs or send email inside the invoice transaction

Rejected because network and provider latency would extend locks and make an
external failure part of legal issuance.

## References

- [Transactional outbox pattern - AWS Prescriptive Guidance](https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html)
