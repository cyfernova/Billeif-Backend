# ADR 0002: Separate HTTP, streaming, worker, WebSocket, and voice runtimes

- Status: Accepted
- Date: 2026-07-30
- Scope: Billeif serverless request and event entrypoints
- Implementation: Target prelaunch architecture; acceptance does not assert deployment

## Context

Billeif serves ordinary request-response APIs, response-streaming A2A
operations, WebSocket events, asynchronous jobs, migrations, and realtime
voice sessions. These workloads have different protocol, timeout,
authorization, network, dependency, and cost characteristics.

A single API Gateway mode cannot provide the most economical ordinary request
path and Lambda response streaming. A single all-purpose runtime also loads
repositories, clients, secrets, and VPC networking that many entrypoints do
not need.

## Decision

Each entrypoint is an explicit runtime boundary and initializes only the
repositories, AWS clients, configuration, and secrets required for that
workload.

| Runtime | Ingress and responsibility | Boundary |
| --- | --- | --- |
| Ordinary HTTP | HTTP API using payload format 1.0 for normal Billeif routes | 28-second Lambda timeout and a 25-second request deadline; application middleware retains dual-Cognito authentication and exhaustive route classification |
| A2A streaming | REST API for `/api/v1/a2a/message:stream` and `/api/v1/a2a/tasks/{taskId}/subscribe` | Dedicated streaming runtime; ordinary routes do not use REST |
| Invoice/render worker | Its own queue and versioned render jobs | No unrelated service graph; idempotent claims and partial-batch failure behavior |
| GST worker | Its own queue and GST work | No unrelated provider or repository initialization |
| Bargaining worker | Its own queue and one database-claimed round | 60-second invocation and a 45-second provider deadline |
| Delivery and SES-event workers | Delivery queue and provider feedback queue | Delivery state only; no settlement side effects |
| WebSocket | WebSocket API events | Uses database networking only while tenant authorization requires PostgreSQL |
| Voice | Feature-gated voice API and session transport | Disabled by default and outside the VPC when PostgreSQL is not required |
| Outbox publisher | Immediate post-commit optimization and periodic recovery | Publishes only recognized event contracts |
| Migration | Checksummed schema migration bundles | Reserved concurrency of one and no application request handling |

HTTP API and REST API expose distinct Billeif base URLs. Clients use the HTTP
base for ordinary traffic and the streaming base only for the two streaming
operations. REST wildcard detailed method metrics are disabled; structured
access logs are retained for 14 days and standard service metrics remain
enabled.

Runtime configuration fails closed when required configuration is missing.
Compiled Lambda artifacts target ARM and omit local paths and unnecessary
symbol data. Memory remains a measured deployment setting rather than an
architecture assumption.

Voice pilot limits are one session per user, five sessions globally, 900
seconds per session, and 250 ms polling. Before raising the global cap above
five or forecasting more than USD 10 per month of voice-related AWS spend,
voice moves to an ARM Fargate Go gateway that carries audio in memory and
retains only session metadata and expiry state.

## Consequences

- Ordinary requests avoid REST API cost and remain below the HTTP integration
  ceiling with explicit timeout headroom.
- Streaming protocol needs do not force ordinary traffic onto REST.
- Function permissions, secrets, startup work, database pools, and VPC
  attachment can match the entrypoint rather than the entire application.
- Route ownership and client base-URL selection become explicit contracts.
- The HTTP application remains responsible for correctly classifying public,
  user-pool, and business-pool routes.
- A capability moved between runtimes requires an explicit review of
  authentication, timeout, VPC, permission, logging, and client URL effects.
- WebSocket remains VPC-attached while its authorization depends on
  PostgreSQL; it may leave the VPC only after that dependency is removed.

## Alternatives rejected

### Keep every HTTP route on REST API

Rejected because only two operations need response streaming, while ordinary
REST traffic and detailed per-method metrics raise the cost floor.

### Put streaming routes on HTTP API

Rejected because the selected Lambda response-streaming integration is a REST
API capability.

### Initialize the full application for every Lambda

Rejected because it expands cold-start work, secret access, permissions,
database connections, and failure surface without benefiting narrow workers.

### Enable voice as an ordinary launch feature

Rejected because the relay model has a materially different duration and cost
profile. It remains a capped pilot until its soak and budget gates pass.

## References

- [Stream proxy integration responses in API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/response-transfer-mode.html)
- [HTTP API quotas](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-quotas.html)
- [Configure Amazon SQS to trigger Lambda](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-configure-lambda-function-trigger.html)
