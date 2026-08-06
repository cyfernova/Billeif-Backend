# ADR: AgentCore Sarvam realtime voice

## Status

Accepted for the AgentCore voice migration.

## Context

The legacy realtime relay couples client media to API Gateway and Lambda. The
replacement must preserve the existing authenticated HTTP API and its tenant
controls while keeping realtime media outside that control plane.

## Decision

- Amazon Bedrock AgentCore Runtime is the realtime host. Its ARM64 container
  listens on `0.0.0.0:8080`, exposes `GET /ping`, and accepts signaling at
  `POST /invocations`.
- AgentCore creates one isolated runtime session for each logical voice call.
  The existing HTTP API remains the only business-tool boundary. Runtime tools
  forward the caller's JWT to that API, which revalidates authorization and
  tenant scope. The runtime has no direct RDS connection.
- Mobile invokes the JWT-authorized runtime directly with its Cognito JWT. The
  runtime accepts only the forwarded, allowlisted `Authorization` header and
  verifies ownership against the logical DynamoDB session. Tokens are neither
  logged nor persisted.
- WebRTC carries all microphone and speaker media through Kinesis Video Streams
  managed TURN. API Gateway and Lambda never carry audio frames. The ordered,
  reliable control channel is `billeif.voice.control.v1`.
- The existing NAT instance is an unproven compatibility exception. It remains
  only behind a mandatory live UDP/TURN gate covering route behavior,
  conntrack, packet loss, reconnects, and 120 synthetic sessions. A failed gate
  requires measured remediation before launch; it is not a reason to add a
  legacy-provider fallback.
- DynamoDB is the terminal-state authority. Stopped AgentCore runtime session
  IDs can be invoked again, so the runtime must reject closed, expired, or
  otherwise terminal logical sessions before extending a lease or starting
  signaling.
- No audio recordings are stored. The product may persist only consented final
  text and bounded operational metadata under its retention policy.

## Consequences

The old Deepgram realtime stack is removed rather than retained as fallback.
Failure rollback is text-only: disable voice admission and retain the ordinary
authenticated text/API experience. This avoids silently reintroducing audio
through Lambda or an unverified provider path.

The runtime depends on direct Cognito-authorized invocation, KVS TURN, a
healthy NAT proof gate, and the existing API's availability. These dependencies
are deliberately explicit so deployment and operational gates can test them.
