# AgentCore voice load, failure, security, and cost report

Report date: 2026-08-06

This report is intentionally incomplete as production evidence. The repository
now contains a deterministic, provider-free validation harness and live-run
preflight gates. No AWS resource was deployed or invoked, no Sarvam request was
made, no external endpoint was contacted, and no credit was spent while
producing this report.

## Decision summary

| Area | Status | Meaning |
| --- | --- | --- |
| Deterministic offline harness | VERIFIED LOCAL | The fake model and its assertions execute without network, cloud, provider, environment, or wall-clock dependencies. |
| Selected production-code security boundaries | VERIFIED LOCAL | Local tests invoke the production protocol, tool-egress, signaling, and telemetry code listed below. |
| AWS path with fake Sarvam | NOT EXECUTED | No staging AgentCore, KVS TURN, NAT, DynamoDB, API Gateway, or Lambda load was run. |
| Real Sarvam path | NOT EXECUTED | No STT, chat, or TTS request was sent. |
| Live failure injection | NOT EXECUTED | Failure paths are modeled locally only; no service or mobile process was disrupted. |
| Production pass criteria | NOT EXECUTED | Local virtual counters are not production capacity or latency evidence. |
| Cost measurement | NOT EXECUTED | Every cost field remains blank. A numeric zero would be misleading. |
| Voice release gate | CLOSED | Production capacity, NAT compatibility, provider quota, multilingual quality, and cost remain unproven. |

`VERIFIED LOCAL` never means production capacity was verified. `NOT EXECUTED`
must remain the status until a separately approved, billable staging run
produces immutable evidence. The report envelope uses `VERIFIED LOCAL` only to
say the harness executed successfully; every invariant, failure, and security
check emitted by its deterministic model carries `MODELED LOCAL` evidence.

## What was actually exercised

The following distinction is essential when reading the generated report.

### Production components exercised by local tests

| Boundary | Production code under test | Local evidence |
| --- | --- | --- |
| DataChannel payload bound | `protocol.DecodeControlMessage` | An input larger than `protocol.MaxControlMessageBytes` is rejected. |
| Tool allowlist | `tools.Registry.Execute` | An unknown URL-fetch tool is rejected before authorization, DNS, or dialing. Poison resolver and dialer implementations fail the test if either is reached. |
| Literal SSRF targets | `tools.NewRegistry` | Metadata IPv4, metadata IPv6, localhost, loopback, and private IPv4 origins are rejected. |
| DNS answer validation and rebinding | Production tool HTTP transport | Non-public or mixed DNS answers are rejected before dial; a later private answer is rejected on a fresh connection. |
| Durable signaling fences | Production WebRTC signaling service | Ownership mismatch, logical/runtime ID mismatch, stopped runtime, expiry, platform mismatch, and app-version mismatch fail closed. |
| Signaling payload bounds | Production WebRTC signaling service | Oversized SDP and ICE candidate inputs map to a bounded payload rejection. |
| Secret and personal-data redaction | Production voice telemetry | Authorization, provider keys/bodies, TURN credentials, transcripts, answers, and recognized personal-data values are redacted under bounded output budgets. |
| Build separation | Go build constraints plus import inspection | The ordinary build excludes the live preflight file and the ordinary harness imports no HTTP client, command runner, AWS SDK, or Sarvam provider package. |

These tests are local and deterministic. Local TLS test servers used by the
production DNS-rebinding regression bind only to the test process; no external
network is contacted.

### Behavior represented only by a deterministic model

| Modeled behavior | Production evidence status |
| --- | --- |
| 25, 50, 100, and 120 concurrent fake-provider sessions | NOT EXECUTED |
| 10, 25, and 100 concurrent real-provider sessions | NOT EXECUTED |
| Bidirectional RTP/Opus media across AgentCore and KVS TURN | NOT EXECUTED |
| End-to-end barge-in and audible-silence latency | NOT EXECUTED |
| ICE restart after real mobile path change or NAT recovery | NOT EXECUTED |
| AgentCore session replacement and abandoned-capacity repair | NOT EXECUTED |
| Real Cognito token expiry and wrong-client invocation | NOT EXECUTED |
| Real NAT, Sarvam, DynamoDB, and DataChannel failure injection | NOT EXECUTED |
| Session creation rate, TURN success rate, CPU, memory, latency, and cost | NOT EXECUTED |
| Eleven-language and code-mixed quality evaluation | NOT EXECUTED |

## Deterministic fake-provider result

The default command models 120 sessions for 30 virtual minutes each. It does
not sleep for 30 minutes. Counts below are deterministic virtual counters, not
measurements from AgentCore, KVS, Sarvam, a NAT instance, or a mobile client.

| Local virtual metric | Value |
| --- | ---: |
| Sessions attempted | 120 |
| Sessions admitted | 120 |
| Sessions completed | 120 |
| Virtual connected minutes | 3,600 |
| Inbound audio frames | 960 |
| Outbound audio frames | 1,200 |
| Played audio frames | 720 |
| Barge-ins | 120 |
| Stale frames dropped | 480 |
| Stale frames played | 0 |
| ICE restarts | 120 |
| Final transcripts | 120 |
| Chat requests | 120 |
| Chat requests before final transcript | 0 |
| TTS streams | 240 |
| Playback acknowledgements | 360 |
| Capacity leases held before modeled repair | 120 |
| Capacity repairs after modeled expiry/reconciliation | 120 |
| Capacity leaks after modeled repair | 0 |

Local invariant outcomes are `VERIFIED`, with evidence `MODELED LOCAL`, for
bidirectional virtual audio, generation-fenced barge-in, modeled ICE restart,
chat only after a final transcript, current-generation playback
acknowledgements, and modeled abandoned-client capacity repair. The capacity
invariant requires a nonzero held-before-repair count, matching repair count,
and zero remaining leases; the zero value cannot pass it. These outcomes do not
satisfy the matching production pass criteria.

## Planned live stages

The report schema carries these stages so absent evidence cannot be confused
with success. Empty duration cells are still operator-defined and must be
bounded to at most 30 minutes by the preflight gate.

| Stage | Provider path | Planned sessions | Planned duration | Execution | Pass criteria |
| --- | --- | ---: | --- | --- | --- |
| `aws-fake-25` | AWS path, controlled fake Sarvam | 25 | | NOT EXECUTED | NOT EXECUTED |
| `aws-fake-50` | AWS path, controlled fake Sarvam | 50 | | NOT EXECUTED | NOT EXECUTED |
| `aws-fake-100` | AWS path, controlled fake Sarvam | 100 | | NOT EXECUTED | NOT EXECUTED |
| `aws-fake-120` | AWS path, controlled fake Sarvam | 120 | 30 minutes | NOT EXECUTED | NOT EXECUTED |
| `sarvam-real-10` | Real Saaras, chat, and Bulbul | 10 | | NOT EXECUTED | NOT EXECUTED |
| `sarvam-real-25` | Real Saaras, chat, and Bulbul | 25 | | NOT EXECUTED | NOT EXECUTED |
| `sarvam-real-100` | Real Saaras, chat, and Bulbul | 100 | | NOT EXECUTED | NOT EXECUTED |

Before any real-provider stage, the operator must confirm the account's current
limits in Sarvam's dashboard and obtain explicit provider approval where
required. Sarvam documents account-wide rate-limit pools, burst-sensitive
WebSocket admission, and distinct STT, TTS, and large-model limits. None of
those limits was probed here.

## Offline failure-injection model

Each scenario executes a deterministic state transition. `RECOVERED` and
`CAPACITY REPAIRED` below describe the local model only.

| Scenario | Modeled transition | Local result | Live result |
| --- | --- | --- | --- |
| NAT stop and recovery | `ACTIVE -> NAT UNHEALTHY -> RELAY ICE RESTARTED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Sarvam STT close | `ACTIVE -> STT CLOSED -> NEXT TURN STREAM -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Chat HTTP 429 | `ACTIVE -> CHAT RATE LIMITED -> BOUNDED RETRY -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Chat HTTP 503 | `ACTIVE -> CHAT UNAVAILABLE -> BOUNDED RETRY -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Chat timeout | `ACTIVE -> CHAT TIMED OUT -> BOUNDED RETRY -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Sarvam TTS close | `ACTIVE -> TTS CLOSED -> GENERATION PURGED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Cognito token expiry | `ACTIVE -> MEDIA PAUSED -> REATTACH REQUIRED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Mobile network switch | `ACTIVE -> PATH CHANGED -> ICE RESTARTED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| DataChannel loss with media alive | `ACTIVE -> CONTROL LOST -> PEER REBUILT -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| AgentCore session stop | `ACTIVE -> TERMINAL -> REPLACEMENT REQUIRED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| DynamoDB throttling | `ACTIVE -> THROTTLED -> LEASE RECONCILED -> RECOVERED` | MODELED LOCAL | NOT EXECUTED |
| Client death without `DELETE` | `ACTIVE -> ABANDONED -> LEASE EXPIRED -> CAPACITY REPAIRED` | MODELED LOCAL | NOT EXECUTED |

## Security matrix

The harness produces sanitized check IDs and outcomes only. Canary values are
asserted absent from evidence strings.

| Attack | Local result | Production boundary exercised? | Live result |
| --- | --- | --- | --- |
| Cross-tenant attach | BLOCKED, MODELED LOCAL | Yes, production signaling ownership test; the matrix also has a local binding model | NOT EXECUTED |
| Runtime session ID reuse | BLOCKED, MODELED LOCAL | Yes, production signaling mismatch/stopped-runtime test; the matrix also has a local terminal binding model | NOT EXECUTED |
| Wrong Cognito client | BLOCKED, MODELED LOCAL | No, matrix model only | NOT EXECUTED |
| Unallowlisted tool URL | BLOCKED, MODELED LOCAL | Yes, production tool registry before DNS/dial | NOT EXECUTED |
| EC2 metadata IPv4 SSRF | BLOCKED, MODELED LOCAL | Yes, production tool origin validation | NOT EXECUTED |
| EC2 metadata IPv6 SSRF | BLOCKED, MODELED LOCAL | Yes, production tool origin validation | NOT EXECUTED |
| Localhost SSRF | BLOCKED, MODELED LOCAL | Yes, production tool origin validation | NOT EXECUTED |
| Private IPv4 SSRF | BLOCKED, MODELED LOCAL | Yes, production tool origin and DNS validation | NOT EXECUTED |
| DNS rebinding | BLOCKED, MODELED LOCAL | Yes, production resolver revalidation test | NOT EXECUTED |
| Oversized DataChannel payload | BLOCKED, MODELED LOCAL | Yes, production protocol decoder | NOT EXECUTED |
| Oversized signaling payload | BLOCKED, MODELED LOCAL | Yes, production signaling test; matrix result uses a local bound | NOT EXECUTED |
| Authorization log leak | BLOCKED, MODELED LOCAL | Yes, production telemetry test; matrix also uses a local redactor | NOT EXECUTED |
| Provider/API-key log leak | BLOCKED, MODELED LOCAL | Yes, production telemetry test; matrix also uses a local redactor | NOT EXECUTED |

Production SSRF tests follow the deny-by-default principles in the OWASP SSRF
guidance: validate the configured origin, resolve for every new connection,
reject any non-public or mixed answer before dial, disable redirects, and keep
tool names allowlisted. AWS documents the link-local EC2 metadata address used
by the metadata regression.

## Production pass criteria

Local model assertions are shown only to expose what is modeled. They are not
substitutes for live observations.

| Criterion | Required threshold | Local model | Observed production value | Status |
| --- | --- | --- | --- | --- |
| Concurrent admission | 100 sessions admitted | 120 virtual admissions | | NOT EXECUTED |
| Synthetic completion | 120 sessions complete | 120 virtual completions | | NOT EXECUTED |
| Session creation success | At least 99% | Not modeled as a measured rate | | NOT EXECUTED |
| TURN connection success | At least 99% | Not modeled as a measured rate | | NOT EXECUTED |
| End of speech to first audible audio p95 | At most 2,000 ms under the agreed India mobile profile | Not measured | | NOT EXECUTED |
| Healthy-device interruption to silence p95 | At most 150 ms | Generation fence only, no latency | | NOT EXECUTED |
| LLM request ordering | No request before final transcript | Verified in model | | NOT EXECUTED |
| Generation fence | No stale audio after generation change | Verified in model | | NOT EXECUTED |
| Abandoned clients | No leaked capacity after five minutes | Immediate modeled repair only | | NOT EXECUTED |
| NAT CPU | p95 below 60% | Not measured | | NOT EXECUTED |
| NAT conntrack | Below 70% utilization | Not measured | | NOT EXECUTED |
| NAT packet health | No sustained packet drops | Not measured | | NOT EXECUTED |
| NAT CPU credits | No throttling or surplus charges | Not measured | | NOT EXECUTED |
| NAT reconnect | At least 99% | Not measured | | NOT EXECUTED |
| NAT latency/backend health | Approved numeric latency budget and no backend regression | Not measured | | NOT EXECUTED |
| AgentCore resident memory | p95 below 256 MiB, or revise cost model using measurements | Not measured | | NOT EXECUTED |
| AWS cost | Actual cost per connected minute recorded by component | Not measured | | NOT EXECUTED |
| Real-provider quality | All 11 planned languages and code-mixed samples reviewed | Not modeled | | NOT EXECUTED |
| Real-provider LLM load | Request rate confirmed under realistic turn frequency | Not modeled | | NOT EXECUTED |

The NAT criteria are inherited from
[`voice-network-spike.md`](voice-network-spike.md). That report also remains
explicit that NAT-instance compatibility is unproven without correlated live
artifacts.

## Cost record

Cost collection was not executed. Recorded values are deliberately blank,
including total cost and cost per connected minute.

| Component | Recorded USD |
| --- | ---: |
| AgentCore Runtime | |
| Kinesis Video Streams TURN | |
| NAT instance incremental cost | |
| DynamoDB | |
| API Gateway and Lambda | |
| Sarvam | |
| Total | |
| USD per connected minute | |

Future evidence must include the exact run window, resource tags, usage units,
billing-export or Cost Explorer query, pricing snapshot, connected-minute
denominator, currency conversion if applicable, and calculation artifact. Do
not replace a missing observation with zero.

## Live-mode safety boundary

The default command is safe and deterministic:

```sh
go run ./cmd/loadtest/voice
```

It defaults to `-mode=fake -dry-run=true -sessions=120 -duration=30m`. Fake mode
rejects `-dry-run=false` and accepts no provider endpoint or credential.

The ordinary binary has no live implementation. `-mode=live` returns
`ErrLiveBuildDisabled`. A separate `voice_live_load` build tag exposes only a
preflight contract. Even after every gate passes, the tagged command returns
`ErrLiveExecutorUnavailable`; no network transport is linked.

Any future executor must retain all of these independent gates:

- the `voice_live_load` build tag;
- an exact planned stage ID and matching session count, with a hard ceiling of
  120 sessions;
- a positive duration no longer than 30 minutes, with exactly 30 minutes for
  `aws-fake-120`;
- explicit `-dry-run=false`;
- `-environment=staging`;
- an exact acknowledgement in `BILLEIF_VOICE_LIVE_LOAD_ACK`;
- immutable 40-character source revision and `sha256:` image digest;
- explicit remote HTTPS API, AgentCore, and chat endpoints;
- explicit remote WSS STT and TTS endpoints;
- rejection of loopback, link-local, private, special-use, and local DNS names;
- separate spend, quota, staging, DNS-revalidation, and provider-approval
  confirmations.

Endpoint preflight is string and literal-address validation only. The future
executor must still re-resolve and validate every DNS answer on every new
connection to prevent rebinding.

## Offline verification commands

The focused harness and build-boundary checks are:

```sh
go test ./internal/voice/loadtest -count=1
go test ./cmd/loadtest/voice -count=1
go test -tags=voice_live_load ./cmd/loadtest/voice -count=1
go test ./tests -run '^TestVoiceLoad' -count=1
go test -race ./internal/voice/loadtest ./cmd/loadtest/voice -count=1
go test -race ./tests -run '^TestVoiceLoad' -count=1
go test -race -tags=voice_live_load ./cmd/loadtest/voice -count=1
```

The selected production-component regressions are:

```sh
go test ./internal/voice/protocol -run '^TestDecodeControlMessageRejectsOversizedControlFrames$' -count=1
go test ./internal/voice/tools -run '^(TestResolverRejectsEveryNonPublicOrMixedAnswerBeforeDial|TestResolverRevalidatesEachNewConnectionAndStopsRebinding|TestExecuteRejectsInvalidArgumentsBeforeAuthorizationOrNetwork)$' -count=1
go test ./internal/voice/webrtc -run '^(TestSignalingDurableSessionAndTrustedRuntimeFences|TestSignalingMapsBoundedSDPAndCandidateViolationsToPayloadTooLarge)$' -count=1
go test ./internal/voice/telemetry -run '^(TestRedactDetailsRemovesSecretsProviderContentAndPersonalData|TestRedactDetailsDetectsCredentialValuesUnderInnocuousKeys|TestRedactDetailsIsBoundedAndFailsClosedForUnsupportedValues)$' -count=1
```

Repository-wide offline verification is:

```sh
go test ./... -count=1
```

Ordinary tests must remain free of provider, AWS, and external-network calls.
A failing test may not be bypassed by adding credentials or enabling live mode.

## Evidence required to reopen the release gate

For each authorized live stage, retain an immutable, sanitized evidence bundle
containing:

- source revision, image digest, stage ID, session count, cadence, timestamps,
  region, subnet, and exact endpoint identities;
- session-create, WebRTC/TURN, bidirectional-audio, interruption, ICE-restart,
  and completion outcomes tied to stable test IDs;
- AgentCore CPU and resident-memory measurements;
- KVS allocation and TURN establishment measurements;
- NAT CPU, packets/bytes, conntrack, packet-drop, CPU-credit, flow-log, and
  backend-regression observations;
- DynamoDB throttling and consumed-capacity metrics plus API Gateway and Lambda
  usage;
- Sarvam STT/TTS concurrency, chat request rate, latency, closure/error, and
  multilingual-quality observations;
- abandoned-client cleanup observations at least five minutes after failure;
- secret scans of logs and crash output;
- component cost calculation and reviewer sign-off;
- cleanup proof showing sessions, temporary endpoints, credentials, and
  resources were retired.

Only independently reviewed live evidence can change a stage or pass criterion
from `NOT EXECUTED`.

## Official references

- [Amazon Bedrock AgentCore Runtime sessions](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-sessions.html)
- [Amazon Bedrock AgentCore Runtime lifecycle](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-lifecycle-settings.html)
- [Amazon Bedrock AgentCore quotas](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/bedrock-agentcore-limits.html)
- [Kinesis Video Streams WebRTC quotas](https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-limits.html)
- [Amazon VPC NAT gateway metrics](https://docs.aws.amazon.com/vpc/latest/userguide/viewing-metrics.html)
- [DynamoDB throttling diagnosis with CloudWatch](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TroubleshootingThrottling-cloudwatch.html)
- [EC2 instance metadata retrieval and link-local endpoints](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html)
- [Sarvam credits and rate limits](https://docs.sarvam.ai/api/getting-started/ratelimits)
- [Sarvam pricing](https://docs.sarvam.ai/api/getting-started/pricing)
- [Sarvam chat completions](https://docs.sarvam.ai/api-reference/chat/chat-completions)
- [OWASP SSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- [Go build constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
