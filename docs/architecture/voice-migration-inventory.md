# Legacy voice migration inventory

## Scope and baseline

This is an evidence inventory, not an implementation plan. It was collected in
`feat/agentcore-sarvam-voice` at `d4602a7f64898fd511a936bc44aca523af724402`
(`origin/main`). Evidence came from `git grep`, `rg`, Go source inspection,
and Terraform source inspection only. No `.env` value or cloud/provider API was
read.

The repository `AGENTS.md` applies: keep changes minimal and reversible;
preserve handler/service/repository layering, tenant/auth checks, and public
compatibility; do not commit credentials; use the listed Make targets; and run
format, lint, and tests appropriate to the change. This documentation-only task
does not change an API, schema, or runtime.

The original checkout is on divergent commit `f085adc34f4c6e3387630e51174bf85fcc313092`,
whereas this worktree is at `origin/main` `d4602a7`. They share merge base
`83ba4bc`; `f085adc` is an alternate one-commit Cognito custom-domain change,
while `origin/main` contains `af5f4d3` (Sarvam TTS), `350684d3`, `112a411`, and
`d4602a7`. It is deliberately not cherry-picked: it is not a fast-forward and
would duplicate/compete with `350684d3`'s same subject while omitting the
subsequent Sarvam and NAT fixes already present on `origin/main`.

The task brief identifies `tests/TestRemovedInfrastructureIsAbsentFromBothMockedPlans`
as a pre-existing baseline failure. Preserve that fact as a baseline issue, not
a migration regression. On this exact `d4602a7` worktree, the focused command
`go test ./tests -run '^TestRemovedInfrastructureIsAbsentFromBothMockedPlans$' -count=1`
passed, so the stated failure is not reproducible here and must be reconciled
against the original checkout before treating it as fixed. `make infra-validate`
passed: Terraform formatting check, backend-free initialization, and
`terraform validate` all completed successfully.

## Legacy Deepgram realtime voice surface

### Go code, routes, protocol, and tests

The legacy stack is the realtime Deepgram relay and its Lambda-backed session
transport, not the generic notification WebSocket or synchronous Sarvam TTS.

| Area | Exact files and symbols |
| --- | --- |
| HTTP realtime relay | `internal/app/runtime.go`: protected `GET /api/v1/voice/realtime` -> `RealtimeVoiceHandler.Handle`; `internal/handlers/realtime_voice_handler.go`: `RealtimeVoiceHandler`, `NewRealtimeVoiceHandler`, `Handle`; `internal/services/realtime_voice_service.go`: `RealtimeVoiceService`, `NewRealtimeVoiceService`, `NewRealtimeVoiceServiceWithResolver`, `Serve`, `ConfigError`, `runtimeConfig`. |
| Deepgram provider/protocol | `internal/services/deepgram_voice_agent.go`: `DeepgramVoiceAgentClient`, `NewDeepgramVoiceAgentClient`, `Connect`, `SendSettings`, `SendJSON`, `SendBinary`, `SendRaw`, `ReadMessage`, `Close`; `internal/services/realtime_voice_protocol.go`: `RealtimeAppEvent`, `RealtimeDeepgramOutbound`, `DeepgramFunctionCall*`, `MapDeepgramJSONEvent`, `BuildDeepgramVoiceAgentSettings`; `internal/services/voice_mcp_bridge.go`: `VoiceMCPBridge`, function-definition and function-call bridge. |
| API Gateway WebSocket ingress | `cmd/lambda/ws/main.go`: `handleWebSocket`, `handleVoiceStart`, `handleVoiceAudio`, `handleVoiceControl`, `resolveVoiceSession`, `postVoiceEvent`; `internal/services/voice_lambda_transport.go`: `VoiceActionStart`, `VoiceActionAudio`, `VoiceActionControl`, request/session/event types, `VoiceLambdaStore`, `VoiceLambdaSessionRunner`, and `APIGatewayVoicePoster`. Actions are `voice.start`, `voice.audio`, and `voice.control`. |
| Background session relay | `cmd/lambda/voice-session/main.go`: `initRuntime`, `voiceRuntime.runner`, `handle`; `internal/services/voice_lambda_transport.go`: `NewVoiceLambdaStore`, `CreateSession`, `FindActiveSessionByConnection`, `EnqueueAudio`, `EnqueueControl`, `ListEventsAfter`, `NewVoiceLambdaSessionRunner`, `Run`, and Deepgram message/function-call handling. |
| Configuration/wiring | `internal/config/config.go`: `DeepgramConfig`, secret and environment bindings; `internal/config/voice_realtime_config.go`: `VoiceRealtimeConfig`, `WithDefaults`, `ValidateForRuntime`; `internal/config/lambda_voice_config.go`: `LambdaVoiceConfig`, `LoadLambdaVoiceConfig`, `ResolveLambdaVoiceRuntime`; `internal/services/container.go` and `internal/handlers/handler.go` wire `RealtimeVoice`; `internal/config/runtime.go`/`validation.go` enforce profile configuration. |
| Test coverage | `cmd/lambda/ws/main_test.go`; `internal/config/lambda_voice_config_test.go`, `profile_validation_test.go`; `internal/services/realtime_voice_protocol_test.go`, `voice_lambda_transport_test.go`, `voice_mcp_bridge_test.go`, `provider_boundary_test.go`; `internal/app/runtime_security_test.go`; and policy/launch tests `tests/aws_branding_policy_test.go`, `tests/launch_defaults_test.go`, `tests/runtime_bootstrap_profiles_test.go`, `tests/secret_state_safety_test.go`. |

Legacy control/data semantics are: the WebSocket Lambda authorizes the business
scope, writes a session plus connection pointer and per-user counter to DynamoDB,
invokes the session Lambda, polls session events, connects to Deepgram, relays
audio/control frames, and posts Deepgram-mapped application events over the API
Gateway management API. `DeepSeek` and the MCP bridge are part of the configured
realtime voice agent settings and therefore inside this migration boundary.

### Build, configuration, documentation, and CI

`Makefile` exposes Deepgram/voice Terraform variable pass-throughs and the
`build-lambda-voice-session` plus `package-lambda` `voice-session.zip` path;
the aggregate `build-lambda` includes that worker. `.env.example` documents the
Deepgram, DeepSeek, realtime-WebSocket, session-table, and session-Lambda
variable names (its placeholder values were not used). Terraform variables are
`enable_voice`, `deepgram_voice_*`, `deepseek_*`, `mcp_server_url`,
`voice_ws_*`, `voice_sessions_table_name`, and `voice_session_lambda_*`.

Generated API documentation consists of `docs/docs.go` and `docs/openapi.yaml`:
they retain the Sarvam endpoints and the retired `/voice/agent` contract;
realtime WebSocket routing is code-only. `docs/adr/0002-serverless-runtime-boundaries.md`
and `docs/adr/0003-cost-first-mumbai-beta-profile.md` document the five-session,
900-second pilot and the intended Fargate replacement threshold. GitHub Actions
in `.github/workflows/deploy.yml` validates and tests Terraform, builds/deploys
the artifacts, and gates Terraform `enable_voice` from repository configuration.

### Terraform resources, names, outputs, secrets, IAM, and observability

The legacy Terraform addresses are:

- `aws_dynamodb_table.voice_sessions` (`pk`/`sk`, PAY_PER_REQUEST, TTL
  `expires_at`, production PITR), with local name `voice_sessions_table_name`
  and output `voice_sessions_table`.
- `aws_lambda_function.voice_session`,
  `aws_cloudwatch_log_group.lambda_voice_session`, and
  `aws_cloudwatch_metric_alarm.voice_active_sessions`; local name
  `voice_session_lambda_name`; output `lambda_voice_session_arn`. The function
  is ARM64, 1024 MiB, 900 seconds, and feature-gated reserved concurrency five.
- `aws_apigatewayv2_route.ws_voice_start`, `.ws_voice_audio`, and
  `.ws_voice_control` on the existing WebSocket API. They all target
  `aws_apigatewayv2_integration.websocket_lambda`.
- `aws_iam_role.lambda_voice_exec`,
  `data.aws_iam_policy_document.lambda_voice_app`, and
  `aws_iam_role_policy.lambda_voice_app`; the WebSocket role policy also has
  `WebSocketTables` and `InvokeVoiceSessionWorker` statements. These grant the
  voice table, API Gateway `ManageConnections`, session-worker invocation, and
  Deepgram/DeepSeek secret access.
- `aws_secretsmanager_secret.deepgram` and `.deepseek` plus KMS decrypt policy;
  environment wiring in `local.voice_secret_env` and the WebSocket/session
  Lambda environment includes `DEEPGRAM_SECRET_ARN`, `DEEPSEEK_SECRET_ARN`,
  `VOICE_SESSIONS_TABLE`, `VOICE_SESSION_WORKER_FUNCTION_NAME`, and the
  Deepgram protocol variables. Do not destroy or inspect secret values.
- Outputs `voice_realtime_input_sample_rate` and
  `voice_realtime_output_sample_rate`, sourced from the Deepgram variables.
- Terraform checks in `infrastructure/terraform/tests/migrator.tftest.hcl` and
  `razorpay.tftest.hcl`, plus string/address assertions in
  `tests/aws_branding_policy_test.go` and secret-state policy in
  `tests/secret_state_safety_test.go`.

`aws_secretsmanager_secret.sarvam` is intentionally not legacy realtime voice:
it belongs to the preserved synchronous TTS surface below.

## Explicitly preserved surfaces

### Generic WebSocket notifications

Do not destroy `aws_apigatewayv2_api.websocket`,
`aws_apigatewayv2_integration.websocket_lambda`,
`aws_apigatewayv2_route.ws_connect`, `.ws_disconnect`, or `.ws_default`,
`aws_apigatewayv2_deployment.websocket`, `aws_apigatewayv2_stage.websocket_default`,
`aws_cloudwatch_log_group.websocket_api_access`,
`aws_lambda_permission.allow_websocket_lambda`, `aws_lambda_function.ws_handler`,
`aws_cloudwatch_log_group.lambda_ws_handler`, or `aws_dynamodb_table.ws_connections`.
They are the generic connection/notification platform, even though the current
WebSocket handler also dispatches the three legacy `voice.*` actions.

The application equivalents that remain are `pkg/websocket/{hub.go,client.go}`;
`internal/services/websocket_connection_service.go`; `internal/handlers/websocket_handler.go`
and its notification methods; the protected `/api/v1/ws` upgrade, stats, status,
users, health, `/notify/:userID`, and `/notify-all` routes in
`internal/app/runtime.go`; and `tests/unit/websocket_handler_test.go`.

### Synchronous Sarvam TTS

Keep `internal/services/sarvam_tts_service.go` (`SarvamTTSService`,
`Synthesize`, `SarvamTTSRequest`, `SarvamLanguages`) and
`internal/handlers/sarvam_tts_handler.go` (`SarvamTTSHandler`, `Synthesize`,
`ListLanguages`), plus their tests and container/handler wiring. Keep protected
`POST /api/v1/voice/text-to-speech` and
`GET /api/v1/voice/text-to-speech/languages`, generated OpenAPI entries, and
`aws_secretsmanager_secret.sarvam` with its HTTP-Lambda secret/IAM wiring. This
surface synchronously returns audio from Sarvam; it does not use the Deepgram
session worker, `voice_sessions`, or `voice.*` API Gateway actions.

## `voice_sessions` reuse assessment

The existing table may be reused only as a backwards-compatible physical table:
its general `pk`/`sk` schema and `expires_at` TTL can store additional typed
records without a migration. It already implements limited legacy admission:
the `CreateSession` transaction conditionally creates a connection pointer and
increments `USER#<id>/COUNTER` only while `active_count < max`; it also uses
conditional writes for unique events and state completion.

It does **not** have any GSI. In particular, there is no indexed lookup by
business, user, provider/AgentCore session ID, status, or expiry. It also has
no durable lease-owner, lease-version/fencing token, renewable lease expiry, or
conditional lease-acquire/release protocol. Its TTL is cleanup only, not a
lease/admission guarantee, and the counter cleanup is best effort after session
completion. A replacement needs an explicit admission key and conditional
lease design (and the required GSI(s)) before it can safely use this table for
new multi-worker AgentCore scheduling.

## NAT egress evidence to preserve or prove

The NAT instance is shared egress, not a Deepgram-specific resource, and must
be preserved/proven through the migration. In `infrastructure/terraform/nat_egress.tf`
the exact resources are `data.aws_ami.billeif_nat_instance`,
`aws_iam_role.nat_instance`, `.nat_instance_ssm`,
`aws_iam_instance_profile.nat_instance`, `aws_security_group.nat_instance`,
`aws_instance.nat`, `aws_eip.nat_instance`,
`aws_eip_association.nat_instance`, `aws_route.private_default_egress`,
`aws_ssm_association.nat_bootstrap_ready`, `.nat_activation_ready`,
`aws_ec2_instance_state.nat_running`, and `.nat_stopped`.

The instance is an AL2023 ARM64 `t4g.micro` in `aws_subnet.public[1]`, with
source/destination checking disabled, no associated public IP, IMDSv2 required,
an encrypted 8-GiB gp3 root disk, standard CPU credits, an EIP, and a private
route `0.0.0.0/0` via its primary network interface (or the managed NAT gateway
when that mode is selected). Gateway endpoints
`aws_vpc_endpoint.s3` and `.dynamodb` attach to `aws_route_table.private`.
CloudWatch alarms are `aws_cloudwatch_metric_alarm.nat_system_status` (EC2
system-status failure with EC2 recover plus SNS), `.nat_cpu_high` (70% average),
and `.nat_cpu_credits_low` (20 minimum credits), all with SNS actions.

Operational gaps to prove before cutover: this is one small NAT instance and
one EIP, therefore a single-AZ/instance egress dependency; the AMI and
user-data are ignored after creation, so forwarding convergence relies on SSM
associations; there is no explicit NAT network-throughput/packet-drop alarm,
synthetic provider-egress probe, or automatic capacity failover. Removing the
Deepgram relay must not remove the NAT route, endpoints, bootstrap ordering, or
alarms until replacement egress requirements are demonstrated.

## Planned destruction boundary and risks

Destroy only the Deepgram realtime relay: the realtime HTTP route/handler/service,
Deepgram client/protocol/MCP bridge, `cmd/lambda/voice-session`, `voice.*`
dispatch from `cmd/lambda/ws`, Deepgram/DeepSeek configuration and placeholder
documentation, the three WebSocket voice routes, `voice_session` Lambda/log/
alarm/IAM, Deepgram and DeepSeek secrets/IAM references, Deepgram Terraform
variables/outputs, voice-session build/package wiring, and the corresponding
tests and policy assertions. Do not destroy the generic WebSocket platform,
Sarvam TTS, NAT egress, S3/DynamoDB endpoints, shared API Gateway, or the
`voice_sessions` table until a decided migration either reuses it or proves it
has no retained data/rollback role.

Implementation risks are: deleting the three actions changes the WebSocket
deployment hash; the HTTP `/voice/realtime` route is a second legacy ingress
independent of API Gateway WebSocket actions; generated OpenAPI/docs and
branding-policy assertions must be updated with source changes; secret removal
must retain Sarvam while removing Deepgram/DeepSeek only; VPC/NAT dependencies
currently name the voice Lambda and need re-evaluation; and a table destroy
would eliminate the legacy admission/session state before a safe AgentCore
lease/admission replacement exists.
