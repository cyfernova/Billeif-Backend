# ADR 0004: SES delivery idempotency boundary

- Status: Accepted
- Date: 2026-07-30
- Scope: Billeif invoice PDF delivery through Amazon SES
- Implementation: Target prelaunch architecture; acceptance does not assert deployment

## Context

Billeif delivers a completed, private invoice PDF from S3 by calling Amazon
SES `SendRawEmail`. SQS and the transactional outbox provide at-least-once
work delivery. PostgreSQL owns delivery status, attempts, and a bounded
processing lease.

SES `SendRawEmail` does not accept an idempotency token and SES does not expose
an API that can look up whether a caller-defined delivery identifier was
accepted. SES also replaces the MIME `Message-ID` and `Date` headers. There is
therefore an unavoidable crash window:

1. SES accepts the message.
2. The worker exits, times out, or loses database connectivity before the
   `sent` transition commits.
3. The processing lease expires and SQS retries the delivery.
4. The retry can send the same invoice again.

The database lease prevents concurrent ordinary duplicates. It cannot make
the SES acceptance and PostgreSQL transition atomic.

## Decision

Billeif retains at-least-once delivery and makes this provider boundary
explicit.

- A worker claims one delivery with an exact two-minute database lease.
- The worker verifies the live lease immediately before calling SES.
- SES receives both `delivery_id` and `business_id` message tags.
- A successful SES response must contain a provider message identifier.
- The `sent` transition requires the same live lease owner. Failure to commit
  it returns a retryable error and does not falsely record a failed send.
- Terminal delivery states are successful no-ops for duplicate SQS messages.
- Task3d3 feedback processing correlates bounce, complaint, and delivery
  events with both tags and the provider message identifier where available.

The worker does not mark `sent` before SES. That would create at-most-once
behavior and could permanently lose an invoice email when the provider call
fails after the database commit. The worker also does not claim exactly-once
delivery, because current SES APIs cannot provide it.

## Operational mitigation

- Lambda errors, queue age, and dead-letter queue depth are alarmed.
- An error after SES acceptance is investigated using `delivery_id`,
  `business_id`, invocation logs, and Task3d3 SES feedback correlation.
- Support treats a possible duplicate email as preferable to silently losing
  a customer invoice.
- A future exactly-once requirement needs a provider or owned mail transport
  with a durable caller-supplied idempotency key and acceptance lookup. It
  cannot be met by rearranging the current database and SES calls.

## Consequences

- Normal concurrent SQS deliveries cannot send under the same live lease.
- Retried terminal messages do not send again.
- A crash after SES acceptance but before `MarkSent` can produce a duplicate
  after lease expiry.
- Monitoring and feedback correlation can diagnose this window, but cannot
  close it.
- Availability is not traded for a false no-duplicate guarantee.

## Alternatives rejected

### Mark sent before calling SES

Rejected because a crash or provider error after the database transition
would permanently suppress a message that SES never accepted.

### Use a deterministic MIME Message-ID

Rejected because SES replaces that header, and `SendRawEmail` has no
idempotency contract based on MIME headers.

### Hold the database transaction open across the SES call

Rejected because it extends database locks across a network request and still
cannot atomically commit with SES acceptance.

## References

- [Amazon SES SendRawEmail API](https://docs.aws.amazon.com/ses/latest/APIReference/API_SendRawEmail.html)
- [Amazon SES raw email requirements](https://docs.aws.amazon.com/ses/latest/dg/send-email-raw.html)
