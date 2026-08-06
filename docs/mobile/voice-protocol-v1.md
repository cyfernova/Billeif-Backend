# Voice protocol v1

## Transport and invariants

Mobile invokes the AgentCore Runtime over HTTPS for signaling and sends media
over WebRTC. Audio must never pass through API Gateway or Lambda. The runtime
accepts only a Cognito-authorized invocation and checks the logical session
before processing a request.

Create one ordered, reliable WebRTC DataChannel named
`billeif.voice.control.v1`. Every DataChannel control message has:

```json
{
  "type": "heartbeat",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 17
}
```

`sequence` is a positive, strictly increasing integer for one sender and one
logical session. Receivers ignore duplicate and out-of-order values. Control
JSON is limited to 16 KiB, including its UTF-8 encoding. Unknown JSON fields
and unsupported event names are rejected.

Messages about an active generated response include a positive
`generation_id`. A new generation supersedes all older generations. The client
discards stale audio by local generation state; it must not assume that an
arbitrary application value is embedded in standard Opus RTP. This is required
for interruption safety.

## Invocation requests

`POST /invocations` transports short JSON signaling requests. It does not
transport media.

### Attach

```json
{
  "type": "session.attach",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "client": {"platform": "ios", "app_version": "1.0.0"}
}
```

The response is:

```json
{
  "type": "session.attached",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "ice_servers": [{"urls": ["turn:host:port"]}],
  "ice_expires_at": "2026-08-06T06:05:00Z"
}
```

TURN credentials are returned only through the authorized attach response and
are never embedded in source, logs, or persistent mobile configuration.

### Offer

```json
{
  "type": "webrtc.offer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sdp": "..."
}
```

The response is:

```json
{
  "type": "webrtc.answer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sdp": "..."
}
```

### ICE candidate

```json
{
  "type": "webrtc.candidate",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "candidate": {
    "candidate": "...",
    "sdp_mid": "0",
    "sdp_mline_index": 0
  }
}
```

The runtime accepts the candidate for the attached session. It emits no new
typed signaling response unless it needs to provide the next SDP answer.

### Restart

```json
{
  "type": "webrtc.restart",
  "protocol_version": 1,
  "session_id": "voice_01K..."
}
```

The runtime responds with `webrtc.answer` after it accepts the ICE restart.
`ice.refresh` on the DataChannel tells the client to attach again for renewed
TURN configuration before performing the restart.

## DataChannel event names

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
- `turn.completed`
- `turn.cancelled`
- `error`
- `session.rotate`
- `ice.refresh`

`interrupt`, playback lifecycle events, `agent.state`, and turn lifecycle
events are generation-sensitive. For example:

```json
{
  "type": "interrupt",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 18,
  "turn_id": 18,
  "generation_id": 42,
  "client_monotonic_ms": 8723341
}
```

The runtime stops the matching generation, emits `turn.cancelled` when
appropriate, and never resumes audio for it. Client partial transcripts are
not authoritative; `transcript.final` is the runtime's final transcription
event.
