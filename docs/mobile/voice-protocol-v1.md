# Voice protocol v1

## Transport and invariants

Mobile invokes the AgentCore Runtime over HTTPS for signaling and sends media
over WebRTC. Audio must never pass through API Gateway or Lambda. The runtime
accepts only a Cognito-authorized invocation and checks the logical session
before processing a request. JSON signaling requests and responses carry the
same `protocol_version`, `session_id`, and positive sender-scoped monotonic
`sequence` fields as DataChannel control messages.

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
logical session. Client-to-runtime and runtime-to-client counters are
independent. Receivers reject duplicate and out-of-order values and do not
process them. Control JSON is limited to 16 KiB, including its UTF-8 encoding.
Unknown JSON fields and unsupported event names are rejected.

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
  "sequence": 1,
  "client": {"platform": "ios", "app_version": "1.0.0"}
}
```

The response is:

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
  "ice_expires_at": "2026-08-06T06:05:00Z",
  "ice_transport_policy": "relay"
}
```

TURN credentials are returned only through the authorized attach response and
are never embedded in source, logs, or persistent mobile configuration. The
client must set its peer connection ICE transport policy to `relay`. TURN
server URLs must use UDP port 443. Exchanged and selected ICE candidates must
be UDP `typ relay` component 1 candidates; the relay allocation port itself may
be ephemeral. The client must not fall back to host, server-reflexive, or TCP
candidates.

### Offer

```json
{
  "type": "webrtc.offer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 2,
  "sdp": "..."
}
```

The response is:

```json
{
  "type": "webrtc.answer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 2,
  "sdp": "..."
}
```

### ICE candidate

```json
{
  "type": "webrtc.candidate",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 3,
  "candidate": {
    "candidate": "...",
    "sdp_mid": "0",
    "sdp_mline_index": 0,
    "username_fragment": "freshClientUfrag"
  }
}
```

The runtime returns **HTTP 204 No Content** when it accepts a candidate for the
attached session. That acknowledgement is not a JSON event and therefore has
no sequence. It does not invent a typed signaling response.

### Restart

```json
{
  "type": "webrtc.restart",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 4,
  "sdp": "...fresh client ICE-restart offer..."
}
```

The restart request must contain a newly created client ICE-restart offer whose
effective ICE username fragment **and** ICE password both differ from the active
offer. The offer must provide one unambiguous credential pair for every bundled
media section. The runtime never attempts to restart from an old stable
description. It returns `webrtc.answer` with the same request sequence and a
newly gathered SDP after accepting the offer:

```json
{
  "type": "webrtc.answer",
  "protocol_version": 1,
  "session_id": "voice_01K...",
  "sequence": 4,
  "sdp": "..."
}
```

`ice.refresh` on the DataChannel tells the client to attach again for renewed
TURN configuration before performing the restart.

If TURN credentials expire while an offer or restart is being processed, the
runtime returns an HTTP conflict without consuming that request sequence. The
client must repeat `session.attach` with the same sequence to refresh its TURN
configuration, then retry signaling with the next sequence. It must not replay
the expired offer under that consumed attach sequence.

The signaling exchange deliberately uses half-trickle ICE. The client may send
bounded candidates before or after its offer. Because a synchronous AgentCore
invocation cannot push later runtime candidates to the client, the runtime waits
for its own ICE gathering to complete and embeds its complete relay candidate
set in each SDP answer.

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
