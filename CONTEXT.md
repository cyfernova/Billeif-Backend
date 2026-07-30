# Billeif Sales Invoice Context

This document defines the durable language of Billeif sales invoicing. It
describes business meaning and invariants, not database tables, services,
queues, or deployment details.

## Core terms

### Invoice

The authoritative sales record for a business. The same aggregate represents
tax invoices and bills of supply created manually or through POS, storefront,
subscription, or conversion flows.

An Invoice owns its legal and commercial content, including the seller and
buyer snapshots, line snapshots, taxes, totals, currency, invoice date, due
date, origin, version, and legal identity after issuance.

### Draft Invoice

An Invoice that is still being prepared.

- It has no Invoice Number and no issuance timestamp.
- Its legal and commercial content can be edited.
- Every accepted edit advances its version.
- A client must identify the version it intends to edit so a stale edit cannot
  overwrite newer work.
- Creating a draft never consumes an Invoice Number.
- A preview may be requested for a particular draft version, but a preview
  does not issue or freeze the Invoice.

### Issued Invoice

An Invoice whose legal identity and legal content have been finalized.

Issuance is a single business transition. It locks the current draft, allocates
the Invoice Number, records the issuance time, freezes the seller, buyer,
line, tax, total, date, and document-type facts, creates the Final Render work,
and records the event needed to continue that work.

An Issued Invoice is not made editable by a failed render, a failed delivery,
or an unpaid balance. Cancellation, settlement, rendering, and delivery are
separate facts. Corrections that change legal content require an explicit
legal correction process rather than reopening the issued record.

### Sales Invoice Origin

The immutable classification of the business flow that first requested the
Invoice:

- `manual`: entered directly by a person.
- `pos`: created by a point-of-sale transaction.
- `storefront`: created by a storefront order.
- `subscription`: created by a subscription run.
- `conversion`: created by converting another business document.

Origin records provenance; it does not select a different invoice model or
weaken invoice invariants. Every origin produces the same canonical Invoice,
Document Projection, numbering behavior, and issuance behavior.

Manual and POS origins may represent an anonymous or ad hoc buyer without a
customer record. In that case, the Invoice still requires an immutable buyer
party snapshot. Storefront, subscription, and conversion origins require the
customer relationship defined by their source flow.

### Document Projection

A presentation and reporting view derived from an Invoice. It has the same
identifier and tenant ownership as its Invoice and expresses the invoice in
the generic Billeif document language.

The Invoice remains authoritative. The Document Projection cannot independently
change legal, party, line, tax, pricing, settlement, or origin facts. It is
created and updated in the same business transaction as the Invoice facts it
reflects, and it can be validated or rebuilt from the Invoice.

Generic document workflows delegate sales-invoice creation and issuance to the
Invoice context. They do not create a second sales-invoice aggregate.

### Invoice Number

The legal serial number allocated only when a Draft Invoice is issued.

Its sequence is scoped by business, document type, invoice-date financial
year, and series. The financial year is calculated in the business timezone
and begins on 1 April. If the business has no configured timezone,
`Asia/Kolkata` is used.

Series contain one to three uppercase letters. The default series are `INV`
for tax invoices and `BOS` for bills of supply, producing numbers such as
`INV/26-27/000001` and `BOS/26-27/000001`. A sequence cannot exceed `999999`.
Allocation is atomic and unique; rolled-back or unsuccessful issuance must not
leave the Invoice partially issued.

### Preview Render

A non-legal PDF rendering of one specific Draft Invoice version.

- It is evidence of how that version would be presented, not evidence of
  issuance.
- A later draft edit makes an older preview obsolete.
- Duplicate work for the same request is safe and does not create a second
  business outcome.
- Preview files are private artifacts retained for no more than seven days.
- Preview failure does not change the Draft Invoice.

### Final Render

The deterministic PDF representation of one specific Issued Invoice version.

- It is requested as part of issuance.
- Exactly one logical Final Render exists for an issued version.
- Retries produce the same bytes and canonical private object identity.
- It cannot be generated from a draft or from a different invoice version.
- It must complete before a Delivery Attempt can send the invoice attachment.
- Final Render failure does not undo issuance; it remains recoverable work.

### Delivery Attempt

One idempotent request to deliver an Issued Invoice's completed Final Render
to a recipient.

A Delivery Attempt owns its recipient, sender identity, Final Render reference,
provider message identity, attempts, lease, timestamps, and outcome. It may
wait for the Final Render, be queued or processed, and then be sent, delivered,
failed, bounced, or complained.

Delivery is not part of the Invoice's legal or settlement lifecycle. A sent
message does not prove provider delivery or payment. A bounce or complaint
does not unissue the Invoice. Repeating the same delivery command with the
same idempotency key replays the original request; changing the command under
that key is a conflict.

### Settlement lifecycle

Settlement records how much value has been applied to an Issued Invoice. It is
derived from durable payment and withholding facts, never from rendering or
delivery.

The settlement conditions are:

- **Unsettled:** no value has been applied; balance due equals the invoice
  total.
- **Partially settled:** applied value is greater than zero and less than the
  invoice total.
- **Settled:** applied value covers the invoice total; balance due is zero.
- **Past due:** the invoice has an outstanding balance after its due date.
  This is a time-based collection condition and can coexist with unsettled or
  partially settled.

Payment corrections or reversals change the applied amount and may move the
settlement condition in either direction. They do not change the Invoice
Number, issuance facts, Final Render identity, or prior Delivery Attempts.
Cancellation or voiding is a separate legal lifecycle and preserves the
settlement and delivery history required for audit.

## Lifecycle independence

The following questions have separate answers:

1. Is the Invoice a draft, issued, or legally canceled/voided?
2. Is the requested Preview Render or Final Render queued, processing,
   complete, failed, or obsolete?
3. Is a Delivery Attempt waiting, queued, processing, sent, delivered, failed,
   bounced, or complained?
4. Is the Invoice unsettled, partially settled, settled, or past due?

No transition on one axis implies a transition on another unless an explicit
business command coordinates them. In particular:

- previewing does not issue;
- issuing does not mean rendered, delivered, or paid;
- rendering does not mean delivered;
- sending does not mean delivered or paid;
- payment does not mean delivered;
- failure of asynchronous work does not roll back issuance.

## Command identity

Create, preview, issue, and delivery commands require stable idempotency
identity. An identical retry returns the original business result. Reusing an
identity for different command content is rejected. Idempotency identifies a
business command; it does not weaken optimistic version checks or tenant
boundaries.
