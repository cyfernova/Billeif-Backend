# Voice protocol v1

This document is the backend contract for the Billeif iOS and Android voice
clients. It defines one deterministic connection, interruption, handoff,
rotation, and close flow. Mobile implementations may organize their code
differently, but they must preserve the wire behavior and ordering here.

The checked-in interoperability fixtures are authoritative examples:

- [Primary mobile flow](../../internal/voice/protocol/testdata/v1/mobile-flow.json)
- [Interruption flow](../../internal/voice/protocol/testdata/v1/interruption-flow.json)
- [All v1 JSON fixtures](../../internal/voice/protocol/testdata/v1/)

## 1. Product lifetime contract

For the initial release, the backend creates a logical voice session with:

- `expires_at = created_at + 55 minutes`
- `rotate_at = created_at + 52 minutes`
- one active logical session per user
- protocol version `1`

Both timestamps returned by the server are authoritative RFC 3339 UTC values.
Clients must schedule rotation from `rotate_at`; they must not derive a later
deadline from a local clock or extend the session locally. At `expires_at`, the
client stops capture and playback and closes even if rotation has not
succeeded.

The 55-minute cap is an operational safeguard, not a claim about the maximum
AgentCore platform lifetime. Revisit it only after a continuous two-hour
endurance test covers AgentCore runtime lifecycle, TURN allocations, Cognito
token refresh, network handoff, interruptions, and supported mobile background
behavior.

There are two different identifiers:

- `session_id` is the Billeif logical conversation identifier.
- `runtime_session_id` selects one isolated AgentCore runtime session.

A Billeif runtime session ID is terminal after the backend stops it. Never send
another AgentCore invocation with that ID. If `POST .../resume` returns a new
`runtime_session_id`, the previous value becomes terminal immediately. This is
an intentional Billeif rule: AgentCore can provision new compute when a stopped
ID is invoked again, but Billeif does not reuse stopped IDs because doing so
would blur lifecycle, authorization, and stale-peer boundaries.

## 2. Transports and counters

The system uses three transport boundaries:

1. The existing `/api/v1` HTTPS API creates, reads, resumes, and ends logical
   sessions.
2. AgentCore `/invocations` carries small signaling JSON only.
3. WebRTC carries Opus RTP audio and one control DataChannel.

Audio never passes through API Gateway, Lambda, AgentCore invocation bodies, or
the DataChannel.

Create one ordered, reliable DataChannel named
`billeif.voice.control.v1`. Every DataChannel message contains:

```json
{
  "type": "heartbeat",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 17
}
```

Rules:

- `sequence` is a positive, strictly increasing, sender-scoped integer for one
  logical session.
- Client-to-runtime and runtime-to-client counters are independent.
- While the peer is connected, send `heartbeat` every 20 seconds, including
  while speech or playback is active. The runtime uses its own clock and
  ignores client timestamps when extending a lease.
- The runtime coalesces heartbeat bursts into at most one lease renewal write
  every 30 seconds. Missing heartbeats allow the two-minute lease to expire and
  the reconciler to release the abandoned session.
- AgentCore invocation responses echo the accepted request sequence.
- A new logical session resets both DataChannel counters and the invocation
  counter to `1`.
- Duplicate or out-of-order DataChannel values are not processed.
- A control JSON frame is at most 16 KiB in UTF-8.
- Unknown fields, duplicate JSON keys, unsupported events, and case-variant
  keys are invalid.
- Generation-sensitive messages contain a positive `generation_id`.
- The client drops audio for every stale generation using local state. It must
  not expect a Billeif generation value inside standard Opus RTP metadata.

## 3. Deterministic connection sequence

Perform these steps in order. Do not open the microphone before the authorized
logical session and relay-only peer are ready.

1. **Refresh Cognito credentials.** Coalesce concurrent refreshes. Refresh
   before starting if the access token is expired or has less than five minutes
   remaining. If refresh fails, do not create or attach a session.
2. **Create the logical session.** Call `POST /api/v1/voice/sessions` with one
   client-generated UUID idempotency key. Retain that same key and exact body
   until the create outcome is known.
3. **Store only the allowlisted response.** Keep `session_id`,
   `runtime_session_id`, runtime ARN and qualifier, KVS channel index, protocol
   version, `rotate_at`, `expires_at`, and spoken languages. Do not persist TURN
   credentials.
4. **Attach.** Invoke `session.attach` using the returned
   `runtime_session_id`, the fresh bearer token, and invocation sequence `1`.
5. **Configure ICE.** Apply the returned ICE servers with an ICE transport
   policy of `relay`. Use only UDP relay candidates. TURN URLs use UDP port 443;
   the allocated relay port may be ephemeral.
6. **Create and send the offer.** Create the local audio transceiver and the
   ordered reliable DataChannel, set the local description, then invoke
   `webrtc.offer` with sequence `2`.
7. **Apply the answer.** The runtime uses half-trickle ICE and returns a fully
   gathered relay answer. Set it as the remote description.
8. **Send client candidates.** Send each bounded UDP `typ relay`, component 1
   candidate with successive invocation sequences. The v1 golden sends its
   first candidate with sequence `3`. A successful candidate returns HTTP 204
   and no JSON body.
9. **Open media.** Wait for the peer connection and DataChannel to become
   connected. Configure the platform audio session, acquire audio focus, start
   capture, and send `client.ready`.
10. **Send local VAD boundaries.** Send `speech.started` when local VAD opens and
    `speech.ended` when it closes. Include `turn_id` and
    `client_monotonic_ms`. Stream only the bounded Opus audio that belongs
    between those boundaries.
11. **Acknowledge playback.** For the current `turn_id` and `generation_id`,
    send `playback.started` when the first generation-approved frame reaches
    the device output and `playback.completed` only after the final frame has
    drained. Include the local monotonic timestamp.
12. **Maintain the session.** Send `heartbeat` every 20 seconds while connected;
    do not pause the cadence during speech or playback. The runtime coalesces
    durable lease renewals to at most one write every 30 seconds. Refresh
    authorization through a new `session.attach` before an authenticated
    operation needs a near-expiry token.
13. **Rotate at `rotate_at`.** Follow section 9. Do not wait for `expires_at`.
14. **Close explicitly.** Send `session.close` if the DataChannel is available,
    call `DELETE /api/v1/voice/sessions/{session_id}`, close WebRTC and platform
    audio, clear route overrides, and discard the terminal runtime ID.

## 4. Logical session API

### 4.1 Create

`POST /api/v1/voice/sessions`

```json
{
  "branch_id": "optional-validated-branch-id",
  "idempotency_key": "client-generated-uuid",
  "preferred_language": "en-IN",
  "fallback_language": "en-IN",
  "consent": {
    "transcript_storage": true,
    "audio_recording": false,
    "policy_version": "2026-08-01"
  },
  "client": {
    "platform": "ios",
    "app_version": "1.0.0",
    "protocol_version": 1
  }
}
```

The 201 response is the shape in
[`session-created.json`](../../internal/voice/protocol/testdata/v1/session-created.json).
In particular, it includes both `rotate_at` and `expires_at`. It never includes
AWS keys, Sarvam keys, KVS IAM credentials, bearer tokens, raw DynamoDB keys, or
transcript text.

### 4.2 Get

`GET /api/v1/voice/sessions/{session_id}` returns owned, tenant-checked metadata
only. Use it after an ambiguous HTTP result or process restoration. A terminal,
foreign, or unavailable session is not a reason to invoke its old AgentCore
runtime ID.

### 4.3 Resume before rotation

`POST /api/v1/voice/sessions/{session_id}/resume` is for a process restart or
recoverable network change before `rotate_at`:

- If the runtime is still resumable, the response retains the logical and
  runtime IDs and conditionally extends the lease, never past `expires_at`.
- If the old runtime was stopped, the response retains the logical session but
  contains a fresh `runtime_session_id`.
- Once a fresh runtime ID is returned, the old value is terminal and must never
  be invoked again.
- Terminal, expired, or rotated logical sessions are not resumable.
- Resume never increments admission capacity a second time.

After resume, run attach, ICE configuration, and offer/answer again. A new
runtime starts with invocation sequence `1`; a still-running runtime continues
the logical session's next invocation sequence.

### 4.4 End

`DELETE /api/v1/voice/sessions/{session_id}` is idempotent. The backend releases
capacity once, calls `StopRuntimeSession`, and marks the logical session closed.
HTTP 204 also means the session was already closed. The client may retry DELETE
after a timeout, but it must never reuse the stopped runtime ID.

## 5. AgentCore signaling

Every request is a short JSON object sent to AgentCore `/invocations` with the
current bearer token and `X-Amzn-Bedrock-AgentCore-Runtime-Session-Id`. Media is
never included.

### 5.1 Attach

```json
{
  "type": "session.attach",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 1,
  "client": {"platform":"android","app_version":"1.0.0"}
}
```

```json
{
  "type": "session.attached",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 1,
  "ice_servers": [{
    "urls": ["turn:discovered-kvs-host:443?transport=udp"],
    "username": "short-lived-username",
    "credential": "short-lived-credential"
  }],
  "ice_expires_at": "2026-08-06T07:05:00Z",
  "ice_transport_policy": "relay"
}
```

TURN credentials are memory-only and scoped to the active peer. Do not log,
cache to disk, synchronize, or include them in diagnostics.

### 5.2 Offer and answer

```json
{
  "type": "webrtc.offer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 2,
  "sdp": "..."
}
```

```json
{
  "type": "webrtc.answer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 2,
  "sdp": "...complete relay answer..."
}
```

### 5.3 Candidate

```json
{
  "type": "webrtc.candidate",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 3,
  "candidate": {
    "candidate": "candidate:... typ relay ...",
    "sdp_mid": "0",
    "sdp_mline_index": 0,
    "username_fragment": "clientUfrag"
  }
}
```

Only UDP `typ relay` component 1 candidates are valid. A successful request
returns 204. Do not expect a synthetic candidate-ack event.

### 5.4 ICE restart

After refreshing TURN configuration, create a fresh ICE-restart offer. Both the
effective ICE username fragment and password must differ from the active offer.

```json
{
  "type": "webrtc.restart",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 5,
  "sdp": "...fresh ICE-restart offer..."
}
```

The response is `webrtc.answer` with sequence `5` and newly gathered relay
candidates. Never restart by replaying an old stable description.

## 6. DataChannel events

Client to runtime:

- `client.ready`
- `speech.started`
- `speech.ended`
- `interrupt`
- `playback.started`
- `playback.completed`
- `heartbeat`
- `session.close`
- `network.changed`

Runtime to client:

- `session.ready`
- `agent.state`
- `transcript.final`
- `language.selected`
- `turn.started`
- `answer.final`
- `turn.completed`
- `turn.cancelled`
- `error`
- `session.rotate`
- `ice.refresh`

Payload rules in addition to the common envelope:

| Event | Required v1 payload |
|---|---|
| `speech.started`, `speech.ended` | `turn_id`; mobile also sends `client_monotonic_ms` |
| `interrupt` | `turn_id`, `generation_id`, `client_monotonic_ms` |
| `playback.started`, `playback.completed` | `turn_id`, `generation_id`, `client_monotonic_ms` |
| `agent.state` | `generation_id`, stable lowercase `state` |
| `transcript.final` | `turn_id`, final `text`; optional safe `detected_language`; optional probability from 0 through 1 only when detected language is present |
| `language.selected` | supported `language` |
| `answer.final` | `turn_id`, `generation_id`, and final visible `text` of at most 8 KiB |
| turn lifecycle events | `generation_id`; include `turn_id` when known |
| `error` | stable lowercase `error_code`; optional bounded public `message` |
| `session.rotate` | canonical RFC 3339 UTC `rotate_at` |

Client partial transcripts are never authoritative. Only
`transcript.final` may be displayed or retained as the final user turn.
When Sarvam omits detection metadata, the runtime omits both detection fields
instead of inventing a language or confidence. A detected language without a
confidence value is valid. `language.selected` always carries the supported
language actually used for the spoken response.

`answer.final` is the only authoritative assistant-text event. It is emitted
once, after the complete chat result has passed validation, and before waiting
for TTS finalization or client playback acknowledgement. Clients may therefore
show the completed answer immediately when TTS closes or stalls. Incremental
model deltas, hidden reasoning, tool payloads, and provider errors are never
placed in this event. A healthy generation still emits `turn.completed` only
after playback drains; a text-fallback generation emits it after the failed
speech path has been retired.

## 7. Interruption and barge-in

The checked-in interruption golden uses generation 41 for the canceled output
and generation 42 for its replacement.

1. On local speech during agent playback, stop local playback immediately.
2. Send `interrupt` for the current `turn_id` and `generation_id`.
3. Purge queued PCM and Opus for that generation.
4. Ignore late control callbacks and RTP associated with the stale local
   generation state.
5. Expect `turn.cancelled` for the interrupted generation. Never synthesize
   `turn.completed` for it.
6. Accept output again only after `turn.started` identifies a higher
   `generation_id`.

If the DataChannel is unavailable, perform steps 1 and 3 locally, reconnect as
described below, and do not replay stale audio after reconnection.

## 8. Network handoff and reconnect

Use the same algorithm for Wi-Fi/cellular handoff, path loss, ICE failure, and
return from a suspended process:

1. Stop capture and playback; purge the current generation's queued audio.
2. If the DataChannel still works, send `network.changed` with the next client
   sequence and `client_monotonic_ms`.
3. Refresh Cognito if needed. Never put a bearer token on the DataChannel.
4. Call GET to reconcile an ambiguous logical-session state. Call resume if the
   process or runtime must be restored.
5. If resume returns a new runtime ID, permanently discard the old one and reset
   the AgentCore invocation sequence to `1` for the new runtime.
6. Invoke `session.attach` to rotate authorization and obtain fresh TURN
   credentials.
7. Choose signaling from the reconciled runtime and peer state:
   - If resume retained the same runtime and the surviving peer has a prior
     stable remote description, apply the relay-only ICE configuration, create
     a fresh ICE-restart offer, invoke `webrtc.restart`, and apply the answer.
   - If resume returned a new runtime ID, permanently close the old peer,
     create a fresh `RTCPeerConnection`, apply the new relay-only ICE
     configuration, create an ordinary offer, and invoke `webrtc.offer`. A new
     runtime has no prior description to restart. Use the same fresh-offer path
     whenever the local peer itself had to be replaced.
8. Wait for ICE, DataChannel, and the selected audio route to be ready. Send
   `client.ready`, then resume capture. Never replay old microphone or TTS
   buffers.

An `ice.refresh` event starts steps 3 and 6 before the current credentials
expire. If the DataChannel is already lost, the client begins from step 3.

## 9. Rotation and hard expiry

The client schedules this flow immediately when it receives the create or
resume response. `session.rotate` is a reminder, not the only trigger.

At `rotate_at`:

1. Stop accepting a new user turn.
2. If a generation is active, interrupt it and purge stale output.
3. Send `session.close` when possible.
4. Call DELETE for the old logical session and retry that idempotent request
   until its outcome is known or `expires_at` is reached.
5. Close WebRTC and platform audio and mark both old identifiers terminal.
6. Create a successor logical session with a new UUID idempotency key. Because
   admission permits one active session per user, close the old session before
   this create. Retry the create with the same new key and exact body.
7. Verify the successor response contains a new logical ID and a new runtime
   ID, then run the deterministic connection sequence from attach.

Do not use resume to extend a logical session past its rotation window. Do not
invoke the old runtime ID after StopRuntimeSession, even if a platform call with
that ID could create fresh compute. At `expires_at`, close locally without
waiting for another server event.

## 10. Android lifecycle

### Audio focus and phone calls

- Use voice-communication audio attributes with speech content and request
  audio focus immediately before opening playback/capture.
- Treat transient focus loss and duck requests as a pause, not as ducked agent
  speech. Stop capture and playback, interrupt the active generation when the
  channel is available, and purge its buffers.
- A phone call can lock focus. If focus is delayed or denied, do not start
  audio. When focus returns, revalidate the logical session and route, then
  restart through `client.ready`; never auto-play buffered agent audio.
- Abandon audio focus on close.

### Bluetooth and route changes

- Register `AudioDeviceCallback` and observe the active communication device.
- On supported Android versions, use `AudioManager.setCommunicationDevice()`
  for wired, HFP, and BLE communication devices. Wait for the device-change
  callback before marking audio ready. Use the platform-compatible fallback on
  older versions.
- If a selected headset disconnects, pause, purge the current generation,
  select an available communication route, and resume only after the route is
  confirmed. Do not silently force speaker if the user selected a private
  route.
- Call `clearCommunicationDevice()` on close.

### Foreground and background

- Foreground operation is the default v1 behavior.
- If the product supports continuing capture while backgrounded, start a
  correctly declared microphone foreground service while the app is visible,
  after `RECORD_AUDIO` is granted, and show the required notification. Do not
  attempt to start microphone capture from a prohibited background state.
- If those requirements are not met, backgrounding stops capture/playback and
  follows the reconnect or close flow. Process restoration always uses GET and
  resume before AgentCore attach.
- Android 15 audio focus requests require the app to be topmost or running an
  eligible foreground service. Failure to obtain focus is a non-playing state,
  not permission to continue silently.

## 11. iOS lifecycle

### Audio session and routes

- Configure `AVAudioSession` for `.playAndRecord` with `.voiceChat`. Use a
  two-way voice-capable Bluetooth HFP route; A2DP alone is output-only and is
  not a microphone route.
- Activate the audio session only after the authorized peer is ready. Deactivate
  it on close and notify other audio sessions as appropriate.
- Observe route changes. On an old-device-unavailable change, stop capture and
  playback, purge the generation, inspect the new input/output route, and
  resume only after a usable voice route is active. Respect the user's route
  choice rather than forcing speaker without an explicit product action.

### Interruptions and background

- Observe `AVAudioSession.interruptionNotification` (or the current replacement
  APIs on the deployment target). On interruption begin, stop capture and
  playback, interrupt the active generation when possible, and purge buffers.
- On interruption end, use the system resumption recommendation, but resume
  only after token, logical session, peer, and route checks succeed. A
  `shouldResume` recommendation never permits replay of stale generation audio.
- iOS may deactivate an audio session when it suspends the process, and the
  notification can arrive after the app runs again. Treat that as restoration:
  GET, resume if eligible, attach, re-establish WebRTC, reactivate audio, then
  send `client.ready`.
- Continue in the background only when the app's approved Audio background mode
  and current product policy permit an active two-way voice session. Otherwise
  backgrounding follows the interruption and close/reconnect flow.

## 12. Retry and idempotency rules

| Situation | Required client action |
|---|---|
| Create request times out | Retry the exact body with the same idempotency key. |
| Create returns 409 | Do not mutate and retry that key; reconcile or create with a new key for a genuinely new session. |
| GET or DELETE times out | Retry; both are safe for state reconciliation. |
| Resume times out | GET first, then retry resume only if the session remains resumable. |
| AgentCore 401 | Refresh once, then reattach. Never log or send the token over WebRTC. |
| 403 or 404 | Treat the supplied logical/runtime binding as unusable; do not probe other IDs. |
| 409 signaling conflict | Reconcile with attach and a fresh ICE restart; do not replay an old offer. |
| 413 or protocol 400 | Treat as a client compatibility bug; do not retry the same payload. |
| 429 or 503 | Honor `Retry-After` when present, otherwise use bounded exponential backoff with full jitter, capped by `rotate_at` and `expires_at`. |
| Invocation response is lost | Do not blindly replay an offer with the same sequence. GET/resume if needed, attach with the next known-safe sequence, and use a fresh ICE restart. |
| DataChannel closes | Do not application-retry old sequence numbers. Reconnect and continue with fresh transport state. |
| Playback acknowledgement is uncertain | Never send `playback.completed` unless the current generation actually drained on the device. |

Only accepted signaling requests consume a server sequence. A response that
explicitly says an expiring TURN configuration caused a conflict does not
authorize replaying stale SDP; refresh attach and create new ICE credentials.

## 13. Official platform references

- [Android: manage audio focus](https://developer.android.com/media/optimize/audio-focus)
- [Android: Audio Manager self-managed call routing](https://developer.android.com/develop/connectivity/bluetooth/ble-audio/audio-manager)
- [Android: foreground service types and microphone restrictions](https://developer.android.com/develop/background-work/services/fgs/service-types)
- [Apple: handling audio interruptions](https://developer.apple.com/documentation/AVFAudio/handling-audio-interruptions)
- [Apple: responding to audio route changes](https://developer.apple.com/documentation/avfaudio/responding-to-audio-route-changes)
- [Apple: `voiceChat` audio-session mode](https://developer.apple.com/documentation/avfaudio/avaudiosession/mode-swift.struct/voicechat)
- [AWS: AgentCore isolated runtime sessions](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-sessions.html)
- [AWS: stop a running AgentCore session](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-stop-session.html)
- [AWS: AgentCore lifecycle settings](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-lifecycle-settings.html)
