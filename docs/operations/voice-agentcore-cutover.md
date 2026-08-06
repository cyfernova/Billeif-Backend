# AgentCore and Sarvam realtime voice cutover

## Current release state

This repository prepares the cutover gates. It does not perform the cutover.
Production voice admission remains hard-disabled in the deployment workflow with
`TF_VAR_enable_voice="false"`, and Terraform defaults both
`provision_voice_infrastructure`, `promote_voice_agentcore_prod`, and
`enable_voice` to `false`.

The following operator actions are blocking and **NOT EXECUTED** as part of
this implementation:

- provision or apply any AWS infrastructure;
- build or publish a production ECR image;
- deploy or invoke a STAGING or PROD AgentCore endpoint;
- verify or increase Sarvam Saaras STT concurrency to at least 150;
- verify or increase Bulbul concurrency to at least 150;
- verify or increase Sarvam-105B quota to at least 300 requests per minute;
- verify AgentCore and KVS account quotas for at least 120 sessions;
- run the Task 7 live AgentCore-to-KVS TURN proof through the NAT instance;
- run the Task 16 live 120-session load and actual-cost measurement;
- verify the immutable runtime version in staging;
- verify generic non-voice WebSocket notifications against staging;
- promote a PROD endpoint, enable a mobile remote feature, or admit any user;
- execute the internal, 5%, 25%, 50%, or 100% rollout stages; or
- apply destruction of obsolete provider infrastructure in an AWS account.

No provider endpoint was called and no external mobile configuration was
changed while preparing this runbook.

## Gate model

Infrastructure provisioning, PROD endpoint promotion, and user admission are
three independent steps:

1. `provision_voice_infrastructure=true` creates the ECR repository, runtime,
   STAGING endpoint, 12-channel KVS pool, reconciler, and any separately enabled
   observability. `enable_voice` can and should remain `false`.
2. `promote_voice_agentcore_prod=true` creates or updates PROD only when an
   explicit numeric `voice_agentcore_prod_version` is supplied. In production,
   every evidence acknowledgement below must also be true. Forward promotion
   additionally requires the staging evidence to name the exact selected image
   digest, release identifier, and numeric runtime version.
3. `enable_voice=true` additionally requires a promoted PROD endpoint and a
   non-disabled `voice_rollout_stage`. The checked-in deployment workflow keeps
   this value false; enabling it requires a separate reviewed repository change.

The runtime container URI is constructed as `repository@sha256:<64 lowercase
hex>`. `voice_agentcore_image_tag` is only a bounded publishing input. The
workflow records the pushed manifest digest, verifies that ECR resolves the tag
to the same digest, and supplies that digest to Terraform. AgentCore never
receives a mutable tag URI. `PUBLISH_VOICE_AGENTCORE_IMAGE` is independent from
infrastructure provisioning. It is true only for the STAGING artifact publish;
PROD promotion rejects publishing and must reuse the verified digest and
release.

Production promotion requires all of these booleans:

| Evidence | Terraform acknowledgement | Current state |
| --- | --- | --- |
| Saaras STT concurrency >= 150 | `voice_sarvam_stt_concurrency_150_acknowledged` | NOT EXECUTED / `false` |
| Bulbul concurrency >= 150 | `voice_bulbul_concurrency_150_acknowledged` | NOT EXECUTED / `false` |
| Sarvam-105B >= 300 RPM | `voice_sarvam_llm_rpm_300_acknowledged` | NOT EXECUTED / `false` |
| AgentCore and KVS >= 120 sessions | `voice_agentcore_kvs_sessions_120_acknowledged` | NOT EXECUTED / `false` |
| Task 7 live TURN proof | `voice_turn_live_proof_acknowledged` | NOT EXECUTED / `false` |
| Task 16 live load and cost evidence | `voice_load_cost_live_evidence_acknowledged` | NOT EXECUTED / `false` |
| Exact staging artifact verified | `voice_staging_verified` plus `voice_staging_verified_image_digest`, `voice_staging_verified_release`, and `voice_staging_verified_version` | NOT EXECUTED / `false`; bindings empty |
| Generic WebSocket notifications verified | `voice_generic_websocket_verified` | NOT EXECUTED / `false` |

An acknowledgement means a dated evidence artifact was reviewed. Published
defaults or an unexecuted harness are not evidence about the Billeif account.

## Staging procedure

These are operator instructions for a later approved change window. They were
not run for this implementation.

1. Keep `TF_VAR_enable_voice=false` (the checked-in workflow enforces this),
   `PROMOTE_VOICE_AGENTCORE_PROD=false`, and `VOICE_ROLLOUT_STAGE=disabled`.
   Keep every evidence acknowledgement false.
2. Set `PROVISION_VOICE_INFRASTRUCTURE=true` and
   `PUBLISH_VOICE_AGENTCORE_IMAGE=true`. Select a bounded, versioned
   `VOICE_AGENTCORE_IMAGE_TAG` and immutable `VOICE_AGENTCORE_RELEASE`; leave
   `VOICE_AGENTCORE_IMAGE_DIGEST` empty so the workflow must derive it from the
   pushed manifest.
3. Manually dispatch the deployment only after its ordinary AWS approval. The
   workflow bootstraps the gated ECR repository, publishes the ARM64 image,
   verifies the tag-to-digest lookup, records the digest in the job summary, and
   provisions STAGING. Do not copy a tag into the runtime container URI.
4. Record the exact digest from the job summary, the unchanged release
   identifier, and the positive numeric AgentCore version exposed by STAGING.
   These three values are one evidence tuple; do not mix values from different
   runs.
5. Confirm STAGING is `READY`, uses that numeric runtime version, has MMDSv2
   required, and uses the intended VPC subnets and NAT-instance route.
6. Execute the Task 7 live TURN proof. Record relay establishment, UDP/TCP
   behavior, NAT counters, cleanup, and billed TURN evidence.
7. Execute the Task 16 staged live load profile. Record p95 latency,
   interruption, provider errors, capacity repair, resident memory, and actual
   AWS/Sarvam cost per connected minute. Do not convert modeled results into
   live evidence.
8. Verify the account-specific Sarvam, AgentCore, and KVS quotas listed above.
9. Exercise generic WebSocket `$connect`, `$disconnect`, and `$default` routes
   plus targeted and broadcast non-voice notifications. Confirm the connection
   table and ordinary notification delivery remain healthy.
10. Set only the acknowledgement whose evidence has actually passed. Keep a
    failed or missing gate false. When staging passes, set
    `VOICE_STAGING_VERIFIED=true` and bind
    `VOICE_STAGING_VERIFIED_IMAGE_DIGEST`,
    `VOICE_STAGING_VERIFIED_RELEASE`, and
    `VOICE_STAGING_VERIFIED_VERSION` to the exact tuple recorded in step 4.

## PROD promotion and rollout

After every blocking gate passes, keep
`PROVISION_VOICE_INFRASTRUCTURE=true`, set
`PUBLISH_VOICE_AGENTCORE_IMAGE=false`, and copy the recorded digest and release
into `VOICE_AGENTCORE_IMAGE_DIGEST` and `VOICE_AGENTCORE_RELEASE`. Set
`PROMOTE_VOICE_AGENTCORE_PROD=true` and `VOICE_AGENTCORE_PROD_VERSION` to the
exact numeric version in the bound staging evidence tuple. The workflow rejects
publish-and-promote in one dispatch, and Terraform rejects a tuple that differs
from the selected STAGING artifact. Record the former numeric PROD version in
`VOICE_AGENTCORE_PREVIOUS_PROD_VERSION` before an upgrade. Keep
`VOICE_ROLLOUT_STAGE=disabled`; promotion still leaves backend admission false.

Promotion still does not admit users. In a separate reviewed change, remove the
workflow's repository lock only after confirming `enable_voice` will be paired
with a non-disabled rollout stage. The backend hashes its canonical
authenticated identity for the `internal` stage. For the phone user pool this
identity is `<user-pool-id>:<sub>`, matching authentication middleware, not the
raw token `sub`; for the primary pool it is the token `sub`. Compute hashes in
an approved local tool that neither logs nor persists the raw identity.
`voice_rollout_internal_sub_hashes` must contain only `sha256:<64 lowercase
hex>` values and never raw identities. Percentage stages use a deterministic
user-and-business bucket.

Progress through `internal`, `5`, `25`, `50`, and `100` in separate reviewed
changes. For `internal`, populate the canonical identity hashes before setting
`voice_rollout_stage=internal`; percentage stages do not use the allowlist.
Do not skip a stage. At each stage, record:

- end-of-speech-to-first-audio and interruption latency;
- Sarvam and AgentCore errors, throttles, and quota headroom;
- NAT CPU, conntrack, packet drops, and TURN health;
- capacity leases and abandoned-session repair;
- actual cost per connected minute; and
- mobile crash-free sessions and text fallback behavior.

Stop progression and disable admission when a launch SLO or safety gate fails.

## Rollback

Rollback is text-first and never restores the removed legacy voice provider.

1. Set `enable_voice=false` and disable the external mobile voice feature. The
   backend continues to permit session lookup and cleanup while rejecting new
   create/resume admission.
2. Stop active sessions using their exact runtime session IDs, allow the
   reconciler to repair leases, and verify global/user capacity returns to zero.
3. Preserve ordinary text chat, generic WebSocket notifications, and
   synchronous non-realtime Sarvam TTS where each remains healthy.
4. If the fault is runtime code, keep `PUBLISH_VOICE_AGENTCORE_IMAGE=false`,
   reuse the current pinned digest and release, keep
   `PROMOTE_VOICE_AGENTCORE_PROD=true`, and set
   `VOICE_AGENTCORE_PROD_VERSION` equal to the recorded
   `VOICE_AGENTCORE_PREVIOUS_PROD_VERSION`. Review the plan and require that the
   PROD endpoint version is the only voice-runtime selection changing.
   Terraform permits this exact previous-version rollback only while admission
   is false; it does not require mutating or republishing the runtime artifact.
5. If Sarvam has a provider-wide failure, present a localized text-only message.
   Do not silently switch providers.
6. Do not destroy the runtime, KVS pool, table, or reconciler until every active
   session is stopped and capacity cleanup is verified.

## Offline repository verification

Run commands directly rather than `make`, because the repository Makefile
includes `.env`:

```bash
go test ./...
go vet ./...
terraform -chdir=infrastructure/terraform fmt -check -recursive
terraform -chdir=infrastructure/terraform validate
terraform -chdir=infrastructure/terraform test
git diff --check
go test ./tests -run TestNoLegacyRealtimeProviderInActiveProjectFiles -count=1
```

The final policy test must pass with no active-project references outside its
historical allowlist. These offline checks do not satisfy any live
acknowledgement.

## Authoritative references

- [AgentCore runtime versions and endpoints](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/agent-runtime-versioning.html)
- [AgentCore Runtime quotas](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/bedrock-agentcore-limits.html)
- [Stop an AgentCore runtime session](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-stop-session.html)
- [AgentCore WebRTC with KVS TURN](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-webrtc-get-started-kvs.html)
- [Kinesis Video Streams WebRTC quotas](https://docs.aws.amazon.com/en_us/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-limits.html)
- [Amazon ECR image URI and digest format](https://docs.aws.amazon.com/codepipeline/latest/userguide/file-reference.html)
