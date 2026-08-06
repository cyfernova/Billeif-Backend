# Voice AgentCore observability and incident runbook

## Purpose and activation state

This runbook covers the Billeif AgentCore voice runtime, Sarvam STT/chat/TTS,
Kinesis Video Streams (KVS) WebRTC signaling, the shared EC2 NAT instance, and
voice-session lease reconciliation.

The repository is cost-safe by default:

- `enable_voice_observability` defaults to `false`.
- `enable_voice_budgets` defaults to `false`.
- Daily and monthly budget amounts default to zero and fail closed if budgets
  are enabled before a positive amount, an alert email, and cost-tag activation
  are confirmed.
- The runtime telemetry emitter writes newline-delimited Embedded Metric Format
  (EMF) to its injected stream and makes no AWS or provider API call itself.
- No infrastructure is created or changed until an operator performs a reviewed
  Terraform apply. Do not enable these controls merely to inspect this change.

Custom CloudWatch metrics, alarms, logs, and dashboards can incur charges once
enabled. Keep both feature flags off until the limits and expected telemetry
volume have been reviewed.

Production provisioning, immutable STAGING evidence, PROD promotion, staged
admission, and exact previous-version rollback are defined in the separate
[voice cutover runbook](voice-agentcore-cutover.md). This incident runbook does
not replace those hard gates.

## Safe enablement order

1. Confirm the voice image and reconciler artifact are from the reviewed
   release. Do not invoke a live voice endpoint as part of this configuration
   review.
2. Ensure production bootstrap constructs one process-wide emitter with
   `telemetry.NewRuntimeEmitter(os.Stdout, os.LookupEnv)`. `ENVIRONMENT` is
   required, and every turn must share that emitter while using a unique opaque
   correlation ID.
3. Review the dashboard and alarm thresholds below. Set
   `enable_voice_observability=true` only for an approved environment.
4. Activate the user-defined `Workload` cost-allocation tag in AWS Billing.
   Tag activation and cost reporting are not immediate. Wait until the tag is
   available for filtering before setting
   `voice_cost_allocation_tag_activated=true`.
5. Choose positive reviewed values for `voice_daily_budget_amount` and
   `voice_monthly_budget_amount`, verify `alert_email`, and only then set
   `enable_voice_budgets=true`.
6. Review an offline Terraform plan before apply. The voice budgets use the
   exact `TagKeyValue` filter `user:Workload$voice-agentcore` and the
   `aws.us_east_1` provider alias.

AWS Budgets alerts are notifications, not a spending kill switch. Tagged cost
data is delayed, and shared or untaggable costs such as portions of the NAT
instance, dashboard, and alarms may not appear in the voice-only filter. Use the
AWS bill and Cost Explorer as the source of truth; do not infer a bill from
AgentCore usage metrics. See [activating user-defined cost-allocation
tags](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/activating-tags.html),
[budget filters](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-create-filters.html),
and the [AWS Budgets API model](https://docs.aws.amazon.com/aws-cost-management/latest/APIReference/API_budgets_Budget.html).

## Telemetry privacy contract

The `Billeif/Voice` event is one bounded aggregate per terminal turn. It must
never contain audio frames, encoded audio, a full transcript, an answer, a
prompt, or a raw provider response body.

- Metric dimensions are exactly `Environment`, `Service`, and `Outcome`.
  `Outcome` is one of `success`, `error`, or `cancelled`.
- The opaque correlation ID is log metadata only. It is never a metric
  dimension and must not contain a phone number, email, account identifier, or
  other user data.
- Authorization values, Sarvam secrets, TURN usernames and credentials,
  provider bodies, transcripts, answers, phone/email/payment/GST/government
  identifiers, bearer/basic credentials, JWTs, subscription keys, AWS access
  keys, and credential-bearing TURN URLs are redacted.
- Unknown detail value types fail closed to `[REDACTED]` instead of invoking a
  string conversion method.
- Detail maps and lists have depth, entry, node, key, string, and total-string
  budgets. The complete encoded event cannot exceed 64 KiB.
- Successful detailed traces use one deterministic hash bucket out of 100.
  Error and cancelled turns retain their bounded, redacted traces. Aggregate
  metrics are emitted for every terminal turn.
- Application request/response payload capture remains disabled. AgentCore
  application logs can contain payloads, so do not enable payload logging to
  troubleshoot voice content. See [AgentCore observability
  configuration](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/observability-configure.html).

If any forbidden content is found, disable voice observability, restrict access
to the affected log group, preserve only the minimum evidence required by the
incident process, and treat the event as a privacy incident. Do not paste the
content into tickets or chat.

## Turn boundaries and metrics

All boundary timestamps must be non-zero and strictly monotonic for a turn:

1. `speech_started`
2. `speech_ended`
3. `stt_final_received`
4. `llm_request_started`
5. `llm_first_token`
6. `tts_request_started`
7. `tts_first_audio`
8. `client_first_audio_played`
9. `turn_completed`

`turn_cancelled` is the terminal alternative after `speech_started`.
`tts_request_started` means the first LLM text delta was submitted to TTS after
`llm_first_token`; opening/configuring a TTS socket is not that boundary.
`client_first_audio_played` must come from the authenticated client playback
acknowledgement, not merely from a server send. One optional interrupt interval
records interruption request to confirmed playback stop.

| Namespace | Signal | Unit / statistic | Initial alarm |
| --- | --- | --- | --- |
| `Billeif/Voice` | `EndOfSpeechToFirstAudioMilliseconds` | ms, p95/p99 dashboard | p95 > 2,000 ms |
| `Billeif/Voice` | `STTFinalLatencyMilliseconds` | ms, p95/p99 dashboard | p95 > 1,000 ms |
| `Billeif/Voice` | `LLMFirstTokenLatencyMilliseconds` | ms, p95/p99 dashboard | p95 > 1,500 ms |
| `Billeif/Voice` | `TTSFirstAudioLatencyMilliseconds` | ms, p95/p99 dashboard | p95 > 1,000 ms |
| `Billeif/Voice` | `InterruptToPlaybackStopMilliseconds` | ms, p95/p99 dashboard | p95 > 150 ms |
| `Billeif/Voice` | `SessionLeaks` | count, sum across outcomes | > 0 |
| `Billeif/Voice` | `KVSAllocationErrors` | count, sum across outcomes | > 0 |
| `Billeif/Voice` | `ICERestarts` | count, sum across outcomes | > 5 |
| `Billeif/Voice` | `Sarvam429` | count, sum across outcomes | > 2 |
| `Billeif/Voice` | `Sarvam503` | count, sum across outcomes | > 0 |
| `Billeif/Voice` | `DurabilityFailures` | count, sum across outcomes | > 0 |
| `Billeif/Voice` | `InputTokens`, `OutputTokens`, `TTSCharacters`, `STTSeconds` | per-turn count/seconds, sum dashboard | dashboard only |
| `AWS/Bedrock-AgentCore` | `SystemErrors`, `Throttles` | sum | > 0 |
| `AWS/Bedrock-AgentCore` | `ActiveSessionCount` | maximum, account-wide `AgentCore.Runtime` scope | >= 80 |
| `AWS/Bedrock-AgentCore` | `Latency` | p99 | > 5,000 ms |
| `AWS/Bedrock-AgentCore` | `CPUUsed-vCPUHours`, `MemoryUsed-GBHours` | sum dashboard | dashboard only |
| `AWS/EC2` | NAT `CPUUtilization` | average | >= 70% |
| `AWS/EC2` | NAT `CPUCreditBalance` | minimum | <= 20 |
| `AWS/EC2` | NAT `CPUSurplusCreditsCharged` | sum | > 0 |
| `Billeif/NAT` | ENA allowance-exceeded counters (conntrack, bandwidth, link-local, PPS) | per-second rate of cumulative maximum series | conntrack rate > 0; others dashboard only |
| `Billeif/NAT` | `net_drop_in`, `net_drop_out` | sum | > 0 |

Custom signal queries use the emitting component's exact `Service` dimension:
`SessionLeaks` selects `voice-reconciler`; KVS, ICE, Sarvam, and durability
signals select `voice-runtime`. AgentCore `SystemErrors`, `Throttles`, and
`Latency` select both `Service=AgentCore.Runtime` and the exact runtime
`Resource` ARN. `ActiveSessionCount` has only the documented
`Service=AgentCore.Runtime` identity and is account-wide, so it must not be
presented as a Billeif-only session count.

All Task 15 alarms use explicit two-of-three evaluation. Missing data is
`notBreaching` except AgentCore `ActiveSessionCount`, where missing data is
`breaching` so loss of the capacity signal is visible. These are launch
guardrails, not validated service-level objectives; tune them only after the
offline load and failure report is reviewed.

AgentCore publishes built-in runtime metrics under
`AWS/Bedrock-AgentCore`; the application uses EMF for its bounded custom
metrics. See [AgentCore runtime metrics](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/observability-runtime-metrics.html)
and the [CloudWatch EMF specification](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html).

## Log locations and retention

The Terraform configuration pre-creates the standard STAGING AgentCore log
group after infrastructure provisioning and before the STAGING endpoint. It
creates the PROD log group only when explicit PROD promotion is enabled:

- `/aws/bedrock-agentcore/runtimes/<runtime-id>-STAGING`
- `/aws/bedrock-agentcore/runtimes/<runtime-id>-PROD` (only after promotion)
- `/aws/lambda/<project>-<environment>-voice-reconciler`

Retention is exactly seven days outside production and fourteen days in
production. Do not create an unbounded parallel log group. The AgentCore log
path and viewing workflow are documented in [viewing AgentCore
observability](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/observability-view.html).

## Incident triage

For every incident, record the alarm name, UTC window, environment, deployed
release/image identifier, and opaque correlation IDs. Never record user audio,
transcripts, answers, credentials, authorization headers, TURN material, or raw
provider bodies.

### Sarvam outage or throttling

1. Compare `Sarvam429` and `Sarvam503` with STT, LLM, and TTS latency and the
   AgentCore error metrics. A 429 can mean rate limit or exhausted quota; a 503
   can mean transient provider load.
2. Check [Sarvam API Status](https://status.sarvam.ai/) and the [official error
   guidance](https://docs.sarvam.ai/api/getting-started/errors-troubleshooting).
   The guidance permits bounded backoff for 429, 500, and 503; do not retry
   validation or authentication failures blindly.
3. Verify only the secret ARN, secret version metadata, and runtime permission.
   Never retrieve, print, or copy the secret value during triage. A persistent
   403 is an authentication/rotation incident, not a retry storm.
4. Stop accepting new voice sessions if the bounded retry policy cannot recover.
   Cancel affected turns with a safe user-visible error and allow established
   sessions to drain where possible.
5. If rollback is required, roll the voice image/runtime endpoint back to the
   last known-good Sarvam release. If none is healthy, disable voice and retain
   text service. The retired provider is not a rollback target and must not be
   reintroduced through configuration, secrets, images, or dynamic failover.

Recovery requires normal 429/503 counts, recovered latency, successful new test
sessions in the approved non-production environment, and no privacy violations.

### KVS allocation or ICE failure

1. Correlate `KVSAllocationErrors`, `ICERestarts`, active sessions, and NAT
   signals. Determine whether failure occurs during channel allocation,
   signaling, ICE credential acquisition, or media establishment.
2. Verify the fixed 12-channel pool is tagged and present, allocation state is
   not stale, the session owns the selected channel, and the runtime role can
   call only the intended KVS operations.
3. Check KVS service quotas and recent quota events. Do not increase quotas or
   pool size without capacity and cost review.
4. Confirm the client received fresh KVS-managed ICE credentials and the
   expected bounded expiry. Never log TURN usernames or credentials, and do not
   introduce a custom TURN server during the incident.
5. If allocation remains unsafe, stop new voice sessions, drain active sessions,
   reconcile stale channel ownership, and roll back to the last known-good
   Sarvam voice release or disable voice.

Recovery requires successful allocation and media establishment without new
ICE restarts across the two-of-three alarm window.

### NAT failure or saturation

1. Check EC2 system status, `CPUUtilization`, `CPUCreditBalance`,
   `CPUSurplusCreditsCharged`, ENA conntrack/allowance-exceeded metrics, and
   ingress/egress drops together. CPU alone is not proof of a network fault.
2. Confirm the instance is running and SSM-managed, source/destination checking
   remains disabled, private routes still target the NAT network interface, and
   IPv4 forwarding/iptables masquerade rules are intact.
3. In SSM, verify `amazon-cloudwatch-agent` is running with
   `/opt/aws/amazon-cloudwatch-agent/etc/billeif-nat.json`. Its metric policy is
   limited to `cloudwatch:PutMetricData` in `Billeif/NAT`.
4. If custom NAT metrics disappear, verify the ENA interface is still `eth0`
   before changing configuration. Missing metrics do not prove that no drops
   occurred.
5. Stop new sessions and drain active work if connection tracking or drops
   continue. Do not replace the NAT architecture or resize the instance during
   an incident without a reviewed infrastructure and cost decision.

The ENA allowance counters are cumulative. Confirm recovery from their
CloudWatch `RATE(...)` series returning to zero across the two-of-three alarm
window; a non-zero historical counter value is not evidence of a continuing
fault.

CloudWatch Agent's ENA metrics and Linux network-drop metrics are described in
[network performance metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-Agent-network-performance.html)
and [metrics collected by CloudWatch Agent](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/metrics-collected-by-CloudWatch-agent.html).
EC2 CPU-credit semantics are documented in [EC2 CloudWatch
metrics](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/viewing_metrics_with_cloudwatch.html).

### Quota exhaustion

1. Treat the `ActiveSessionCount >= 80` alarm as the 20-session operational
   headroom guardrail below the configured 100-session product limit.
2. Compare AgentCore active sessions with the voice table's active leases and
   KVS allocations. Large divergence indicates leakage rather than legitimate
   demand.
3. Check AgentCore, KVS signaling-channel, Sarvam STT/chat/TTS, and account-level
   quotas separately. Do not assume one service's count proves another
   service's capacity.
4. Reject new sessions cleanly while preserving existing sessions. Do not raise
   the 100-session limit or any external quota until load-test evidence,
   concurrency confirmation, security review, and cost approval are complete.
5. Use [AgentCore runtime troubleshooting](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-troubleshooting.html)
   for platform-specific failures without enabling payload logs.

Recovery requires active sessions below the guardrail, successful lease/KVS
allocation, and no AgentCore throttles for the evaluation window.

### Session leaks or durability failure

1. On `SessionLeaks` or `DurabilityFailures`, compare AgentCore active sessions,
   DynamoDB active leases, KVS allocations, and reconciler executions.
2. Verify the one-minute reconciler schedule is enabled, its reserved concurrency
   remains one, and the `gsi2` expired-lease query is succeeding.
3. Inspect only session IDs, lease timestamps, generation/sequence counters, and
   product-terminal status. Do not inspect or log final-turn content.
4. Confirm the reconciler conditionally marks the lease terminal before calling
   `StopRuntimeSession`. Product-terminal sessions must never be revived.
5. A post-response final-turn write failure increments `DurabilityFailures` but
   must not turn a successfully delivered live response into a failed call.
   Repair the persistence path and reconcile the affected lease; do not replay
   an unverified answer.
6. If leakage continues, stop new sessions, let healthy sessions drain, and
   disable voice. Resume only after stale leases and channels are reconciled and
   the active counts converge.

## Rollback and closure

Rollback is voice-image/runtime-endpoint rollback to the last known-good Sarvam
release, or voice disablement with text service retained. It is never a provider
swap. Preserve the last unhealthy release identifier for investigation, but do
not preserve credentials or customer content in the incident record.

Before closing an incident:

- All triggering alarms have returned to OK for at least one full evaluation
  window.
- New sessions can allocate KVS, establish media, complete a turn, persist the
  terminal state, and close without a leak in an approved non-production test.
- `ActiveSessionCount`, DynamoDB leases, and channel allocations converge after
  drain.
- No secret or private content was logged.
- Any threshold or capacity change has a reviewed follow-up with measured load
  and cost evidence.
