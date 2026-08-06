# AgentCore voice network spike

| Gate | Status |
| --- | --- |
| Live execution | NOT EXECUTED |
| Compatibility | UNPROVEN |
| Release gate | CLOSED |

This is a future operator runbook, not a record of a completed test. No AWS,
Kinesis Video Streams (KVS), Sarvam, DNS, public-network, deployment, or secret
operation was performed while preparing it. All evidence fields below are
intentionally empty. `enable_voice` and `enable_voice_turn_udp_egress` must
remain false until the procedure is approved, funded, executed, and reviewed.

## What this spike must prove

The release gate may open only after live evidence shows all of the following:

1. AgentCore traffic from each selected private subnet follows that subnet's
   expected default route through the existing NAT instance.
2. Every required AWS, Sarvam, Billeif, and dynamic KVS host has its own DNS
   and TCP 443 observation, bounded timestamp, and redacted artifact hash. A
   summary count is not connectivity evidence.
3. `GetIceServerConfig` succeeds and the runtime returns only redacted ICE
   metadata: a stable host hash, transport, port, and credential expiry. It
   never returns or serializes the raw host, username, password, or channel ARN.
4. A test peer sends and receives nonce-bearing datagrams through KVS TURN,
   with payload, direction, and timeout checks proving bidirectional relay.
5. A TURN connection remains useful for at least 30 uninterrupted minutes and
   the credential-expiry, refresh, new-allocation, and restart behavior is
   captured in a redacted lifecycle artifact rather than inferred.
6. A NAT health artifact captures CPU, network, ENA, conntrack, packet-drop,
   status, and VPC Flow Logs evidence correlated with each run.
7. A backend regression artifact compares latency, errors, connections, and
   egress before, during, and after the run and records no regression.
8. Stable redacted path identities and signed portable envelopes prove that two
   trusted-observer evidence bundles came from distinct configured subnets.
   Caller-constructed result fields, fake public dependencies, slice positions,
   stale envelopes, and replayed signatures cannot establish identity or open a
   gate.
9. Every temporary runtime, endpoint, authorization path, rule, credential,
   session, peer, and observer permission is removed or revoked afterward.

Passing local tests only proves that the harness fails closed, validates its
inputs, redacts credentials, and behaves correctly with fakes. It cannot prove
AgentCore, KVS TURN, or NAT-instance compatibility.

## Safety model

The ordinary build cannot execute a live probe. A live-capable binary requires
the `voice_live_probe` build tag and the runtime acknowledgement defined by the
harness:

```text
I_ACKNOWLEDGE_LIVE_AGENTCORE_TURN_PROBE_MAY_INCUR_COSTS
```

Both gates are necessary and neither is evidence of approval. The tagged test
suite uses injected fakes and deterministic test-only Ed25519 keys declared in
`_test.go` files; it creates no trusted external evidence. Production code has
no live-observer constructor, setter, private key, provider adapter, or launch
target. A public `Dependencies`, `Clock`, or caller-selected `LIVE` label cannot
inject the package-sealed observer needed to mint an envelope.

One result cannot open the gate. After detailed evidence validation, the sealed
observer signs a portable `LiveEvidenceEnvelope`. Its canonical claims bind the
campaign, full source revision, immutable image digest, trusted observer
identity and key ID, change window, random run ID, stable path fingerprint,
operation timestamps, redacted ICE metadata and expiry, complete artifact
digest set, and safe pass summary. The private key stays inside the sealed
observer adapter and is never serialized, formatted, logged, or returned.

The central evaluator verifies two JSON-round-trippable envelopes against an
operator-supplied trusted-key policy. Both must be fresh, in the same approved
campaign, build, image, and change window, while having distinct run IDs, paths,
and non-overlapping artifact sets. Signature verification precedes an atomic
reservation in a required durable replay guard. A duplicate, stale, tampered,
cross-campaign, cross-build, untrusted-observer, or previously consumed envelope
fails closed. Changing the configured path order must not change the identity.
There is deliberately no Make target for launching a live probe; the future
operator change must add the reviewed package-owned adapter without exposing a
general observer-injection API.

The probe runtime role must stay limited to its workload calls. Route, ENI,
NAT, metrics, and VPC Flow Logs inspection belongs to a separate, short-lived,
read-only operator or observer role. Never grant those observer permissions to
the runtime. Never place AWS credentials, Cognito tokens, Sarvam keys, TURN
usernames, TURN passwords, or raw ICE responses in command lines, source,
environment snapshots, logs, or evidence documents.

## Offline evidence contracts

The harness models the evidence required for a future run without performing
it. These contracts are exercised only with fakes in ordinary and
`voice_live_probe` tagged tests:

- `ConnectivityObservation` records one approved host hash, DNS result, TCP
  443 result, bounded observation time, and artifact hash per required host.
  AWS control/signaling, Sarvam, Billeif, and dynamically returned KVS hosts
  must all be present; aggregate success booleans are insufficient.
- `RedactedICE` carries only a host hash, `udp` transport, port 443, and expiry.
  Raw ICE hosts and credentials remain inside the narrow relay boundary and
  must be non-serializable and safe under default formatting.
- `NATHealthObservation` references redacted NAT metrics, ENA/network,
  conntrack, packet-drop, status, and flow-log artifacts for the exact run
  window.
- `EnduranceObservation` proves continuous relay operation for at least 30
  minutes using injected times in offline tests.
- `CredentialLifecycleObservation` references evidence for established media
  across credential expiry, a post-expiry allocation, credential refresh, and
  the observed restart decision.
- `BackendRegressionObservation` references before/during/after evidence and
  must explicitly report no existing-backend regression.
- `LiveEvidenceEnvelope` is a portable signed DTO containing hashes and safe
  metadata only. The sealed observer receives claims only after topology,
  connectivity, ICE, relay, NAT, endurance, credential-lifecycle, backend,
  timestamp, and artifact checks pass. A central `VerifyLiveRelease` call
  requires a trusted observer policy and replay guard; public or synthetic
  `ProbeResult` values cannot satisfy it.

Artifact hashes identify access-controlled evidence without embedding raw
artifacts in a runtime response. The signed envelope may carry those hashes but
never credentials, channel ARNs, subnet IDs, raw endpoints, payloads, or private
keys. A hash and signature do not replace review: the independent reviewer must
retrieve each private artifact, verify every digest and time window, and approve
the trusted observer key before central release evaluation.

## Known design contradictions to resolve first

These are blockers, not implementation details to paper over:

### UDP proof flag has a chicken-and-egg dependency

The production runtime currently requires
`enable_voice_turn_udp_egress=true`, while that value semantically asserts that
the live TURN proof has already passed. The live proof itself needs UDP egress
and an AgentCore runtime. Do not reinterpret or set the production flag merely
to make the proof possible. The approved change must create an isolated,
temporary probe deployment with a narrowly scoped UDP rule, then remove it.
Production promotion remains closed until its evidence is independently
reviewed.

### A normal synchronous invocation cannot prove 30-minute endurance

AgentCore synchronous invocations are bounded below the required 30-minute
window, and the current HTTP runtime has a 15-second response write timeout.
Each endurance probe therefore needs a reviewed streaming or background
execution design with bounded startup and control calls. A successful short
`POST /invocations` response is not endurance evidence.

### An invocation cannot select its subnet

AgentCore does not expose invocation-level subnet selection. Create two
separate temporary operator-only probe deployments, each configured with
exactly one of the two candidate private subnets. Do not claim a subnet was
tested based on an inferred ENI or a runtime configured with both subnets.

### Observer access is not workload access

The runtime can report its own redacted probe result, but it cannot establish
the route-table, ENI, NAT host, conntrack, CloudWatch, or flow-log facts needed
for the gate. A separate observer must capture those facts and correlate them
by UTC time window and redacted connection identifiers.

### ICE credential lifetime is an experimental result

The lifetime returned with KVS credentials does not determine what happens to
an already established allocation. The run must separately observe established
media across expiry, a new allocation after expiry, credential refresh, and any
required peer restart. Do not infer one result from another.

### NAT gateway guidance is not NAT-instance proof

AWS AgentCore VPC guidance describes managed NAT gateway patterns. That does
not establish compatibility with this customer-managed NAT instance. Only the
live, subnet-specific route, TURN, metrics, and flow evidence below can resolve
that risk.

## Required approvals and credentials

Before any live action, record and review:

- a change ticket, named primary operator, independent reviewer, spend owner,
  maintenance window, abort owner, and maximum approved cost;
- immutable source revision and image digest for a temporary probe image that
  is never promoted to `PROD`;
- two temporary AgentCore runtimes or equivalent isolated probe deployments,
  one pinned to each selected private subnet;
- temporary non-production endpoints using streaming or background execution;
- a temporary Cognito app client and operator-only principal that cannot be
  used by production mobile clients;
- short-lived operator AWS credentials obtained through the approved identity
  flow, with a separate read-only observer role;
- runtime access to the existing Sarvam secret by ARN only, resolved at
  runtime, without retrieving or copying its value into the change record;
- KVS permissions limited to the assigned test signaling channel and ICE
  configuration calls; and
- a test peer outside the VPC that echoes nonce-bearing UDP payloads without
  storing TURN credentials or audio.

Revoke the Cognito client/tokens and operator sessions at the end of the window.
TURN credentials and Cognito tokens must be treated as ephemeral credentials
even when their remaining lifetime is short.

## Preflight evidence

The operator and observer must capture these facts before starting either
probe:

1. Confirm the two private subnet IDs and supported Mumbai AZ IDs are distinct.
2. Resolve each subnet to its associated route table and confirm the
   `0.0.0.0/0` target is the expected existing NAT instance.
3. Confirm the NAT instance is in the expected public subnet, has the expected
   Elastic IP, has source/destination checks disabled, and follows a public
   route to the Internet Gateway.
4. Confirm IPv4 forwarding and persistent NAT/PAT rules, instance recovery or
   one-instance Auto Scaling behavior, no Spot capacity, SSM-only
   administration, and the intended forwarding security-group scope.
5. Confirm CPU, status checks, network packets/bytes, conntrack utilization,
   packet drops, CPU credit surplus, and VPC Flow Logs can be observed for the
   full window.
6. Freeze an explicit hostname allowlist for KVS control/signaling, returned
   KVS STUN/TURN hosts, Secrets Manager, logging, and Sarvam. The harness must
   reject endpoints outside that list.
7. Capture backend request latency, error rate, NAT egress, and connection
   baseline for a comparable period before the spike.
8. Verify all probe deadlines are bounded and shorter than the approved change
   window. A missing timeout is a stop condition.

## Future execution procedure

### 1. Prepare two subnet-pinned deployments

Create temporary probe A with only private subnet A and temporary probe B with
only private subnet B. Use separate non-production runtime/endpoint identifiers
and temporary operator-only authorization. Do not update `PROD`, the mobile
attach response, or the production voice release variables.

For each deployment, the observer must verify the actual AgentCore ENI is in
the intended subnet and that its route-table association resolves to the
expected NAT instance before UDP testing begins. An unexpected subnet, ENI,
route, source address, or NAT target aborts the run. Configure the harness
runtime-subnet expectation for that deployment and require the returned
stable redacted path identity to match. Probe A and probe B must cover distinct
identities; a duplicate identity is a failed release gate. Reordering the two
configured paths must not create a second identity.

### 2. Run bounded control checks

For probe A and then probe B:

1. Start the reviewed streaming/background probe with the live build tag,
   exact runtime acknowledgement, approved endpoint allowlist, expected subnet,
   route-table, NAT-instance, source/destination-check expectation, and bounded
   deadlines. Pin the approved campaign ID, full source revision, immutable
   image digest, observer identity and key ID, and UTC change-window bounds in
   the probe configuration.
2. Resolve every required hostname and establish TCP 443 only to an approved
   endpoint. Produce one `ConnectivityObservation` per AWS control/signaling,
   Sarvam, Billeif, and dynamic KVS host. Record stable host/result hashes,
   bounded timestamps, and private artifact digests; do not record raw hosts,
   tokens, request bodies, or resolved credentials.
3. Call KVS `GetIceServerConfig` on only the assigned test channel. Record a
   `RedactedICE` response containing the stable approved-host hash, UDP
   transport, port 443, and expiry time. Raw host, username, password, and
   channel fields must be absent, not masked copies. Validate the configured
   expiry margin immediately after credential acquisition and again after TURN
   relay setup; either failure aborts before attestation.
4. Start the external test peer and allocate KVS TURN. Send a fresh random nonce
   in each direction, require the exact peer echo, enforce send/receive
   deadlines, and record byte counts, loss, setup latency, and reconnect result.
5. Stop immediately on an allowlist violation, credential expiry before the
   test starts, redaction failure, context cancellation, deadline violation,
   route mismatch, or partial TURN echo.

### 3. Run the 30-minute endurance checks

Keep each subnet-pinned TURN connection active for at least 30 uninterrupted
minutes using the approved streaming/background mechanism. Run the agreed
synthetic cadence and, when capacity approval includes it, hold 120 aggregate
synthetic sessions for 30 minutes. Do not send real voice or customer data.

During each run:

- sample TURN establishment, bidirectional payload success, packet loss,
  reconnect success, and added latency;
- mark the issued ICE credential expiry instant, keep the established
  connection active across it, then separately test a new allocation;
- fetch fresh ICE credentials through the approved path and record whether an
  ICE restart, peer restart, or neither is actually required;
- sample NAT CPU, network packets/bytes, status, conntrack use, packet drops,
  CPU credit surplus, and available ENA network-performance metrics, then bind
  them to one `NATHealthObservation` artifact for the exact run window;
- query VPC Flow Logs for the bounded UTC window and correlate the AgentCore
  ENI, NAT path, destination, protocol, port, accepts/rejects, packets, and
  bytes without copying credentials or payloads; and
- run the existing backend regression check concurrently, comparing API error
  rate, latency, egress, and connection behavior with the preflight baseline,
  then bind before/during/after artifacts to one
  `BackendRegressionObservation`.

Abort both probes and begin cleanup if UDP does not traverse the NAT instance,
an ENI bypasses the expected route, TURN allocation is intermittent, packet
drops are sustained, a credential is exposed, or existing backend traffic
regresses.

### 4. Apply pass/fail gates

All subnet-specific checks must pass. For any approved 120-session capacity
run, the minimum gates are:

- NAT CPU p95 below 60 percent;
- conntrack utilization below 70 percent;
- no sustained packet drops;
- no CPU-credit throttling or surplus charges;
- TURN establishment success at least 99 percent;
- reconnect success at least 99 percent;
- added network latency p95 below the separately agreed voice budget; and
- no regression in existing backend egress, latency, or error rate.

The voice latency budget must be numeric and approved before execution. Missing,
partial, contradictory, uncorrelated, non-private, or independently unvalidated
evidence is a failed gate. A result from one subnet cannot be used for the
other subnet. Public DTOs, synthetic results, and user-selected `LIVE` labels
must never open the release gate.

### 5. Clean up and regress

Whether the spike passes or fails:

1. Stop the streaming/background operations, test peer, and all sessions.
2. Revoke temporary Cognito tokens, delete/disable the temporary app client,
   expire operator sessions, and remove temporary observer grants.
3. Delete both temporary runtime endpoints and runtimes, their temporary image,
   probe-only channel, and any probe-only logs after preserving approved,
   redacted evidence according to retention policy.
4. Remove the temporary UDP/security-group rule and restore every route,
   security group, release variable, and feature flag to its preflight value.
5. Confirm no probe ENIs, allocations, sessions, background operations, or
   billable temporary resources remain.
6. Rerun the backend regression check and compare it with both the preflight
   and in-test snapshots.
7. Search retained logs and evidence for credential fields; quarantine the
   evidence and rotate affected credentials if any are found.
8. Have the independent reviewer sign off on cleanup before closing the change.

## Evidence record

These fields must remain empty until an approved live run. Links must point to
access-controlled, redacted artifacts; do not paste raw logs or credentials.

### Change and build

| Field | Recorded value |
| --- | --- |
| Change ticket | |
| Primary operator | |
| Independent reviewer | |
| Spend owner and ceiling | |
| Window start UTC | |
| Window end UTC | |
| Campaign ID | |
| Source revision | |
| Temporary image digest | |
| Trusted observer identity and key ID | |
| Harness acknowledgement reviewed | |

### Topology and authorization

| Field | Recorded value |
| --- | --- |
| Probe A runtime and endpoint | |
| Probe A subnet and AZ ID | |
| Probe A route table | |
| Probe A AgentCore ENI | |
| Probe A stable redacted path identity | |
| Probe B runtime and endpoint | |
| Probe B subnet and AZ ID | |
| Probe B route table | |
| Probe B AgentCore ENI | |
| Probe B stable redacted path identity | |
| NAT instance and public subnet | |
| NAT source/destination check | |
| Observed NAT Elastic IP | |
| Temporary operator auth client | |
| Observer role session | |

### Control and TURN results

| Field | Recorded value |
| --- | --- |
| Endpoint allowlist revision | |
| Probe A per-host DNS/TCP 443 artifact hashes | |
| Probe B per-host DNS/TCP 443 artifact hashes | |
| Probe A connectivity observation window | |
| Probe B connectivity observation window | |
| Probe A run/network/credential/relay/completion instants | |
| Probe B run/network/credential/relay/completion instants | |
| Probe A redacted ICE host hash/transport/port/expiry | |
| Probe B redacted ICE host hash/transport/port/expiry | |
| Probe A TURN send/receive artifact | |
| Probe B TURN send/receive artifact | |
| ICE issued and expiry instants | |
| Established connection at expiry | |
| New allocation after expiry | |
| Credential refresh result | |
| Required restart behavior | |
| Probe A credential-lifecycle artifact hash | |
| Probe B credential-lifecycle artifact hash | |

### Endurance, NAT, and regression results

| Field | Recorded value |
| --- | --- |
| Probe A at-least-30-minute endurance window | |
| Probe B at-least-30-minute endurance window | |
| Probe A endurance artifact hash | |
| Probe B endurance artifact hash | |
| Session count and cadence | |
| TURN establishment rate | |
| Reconnect rate | |
| Added network latency p95 | |
| NAT CPU p95 | |
| NAT network packets and bytes | |
| Conntrack maximum and capacity | |
| Packet-drop result | |
| CPU-credit result | |
| Probe A NAT health artifact hash | |
| Probe B NAT health artifact hash | |
| Flow-log query and artifact hashes | |
| Backend baseline artifact | |
| Backend in-test artifact | |
| Backend post-cleanup artifact | |
| Probe A backend-regression artifact hash | |
| Probe B backend-regression artifact hash | |
| Backend regression verdict | |
| Probe A signed envelope digest and signature | |
| Probe B signed envelope digest and signature | |
| Durable replay reservation | |
| Independent validator identity and central decision | |

### Cleanup and decision

| Field | Recorded value |
| --- | --- |
| Sessions and peers stopped | |
| Temporary auth revoked | |
| Temporary runtimes/endpoints removed | |
| Temporary network changes reverted | |
| Remaining ENIs/resources check | |
| Credential-log scan | |
| Cleanup reviewer sign-off | |
| Compatibility decision | |
| Release-gate decision | |

Until every applicable field is populated with independently reviewed live
evidence, NAT-instance compatibility remains **UNPROVEN** and the voice release
gate remains **CLOSED**.

## Official references

- [AgentCore Runtime WebRTC protocol and TURN requirements](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-webrtc.html)
- [AgentCore Runtime VPC connectivity](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/agentcore-vpc.html)
- [Kinesis Video Streams WebRTC requirements](https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-requirements.html)
- [Kinesis Video Streams WebRTC quotas and limits](https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-limits.html)
- [AWS VPC NAT instance setup and source/destination checks](https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html)
- [ENA network performance metrics](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-network-performance-ena.html)
- [VPC Flow Logs fundamentals](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-basics.html)
