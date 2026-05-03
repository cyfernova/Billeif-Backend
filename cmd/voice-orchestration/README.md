# Voice Orchestration Service

A Go middleware API that handles the following pipeline:

1. Accepts a `.wav` file upload via `POST /api/v1/voice/transcribe`
2. Sends the file to a Speech-to-Text (STT) service via HTTP multipart upload
3. Sends the resulting transcript to the MIN MAX LLM REST API
4. Returns a JSON response containing the filename, transcript, and LLM response

## Endpoints

### `GET /health`

Returns `{"status": "ok"}`. Use for liveness checks.

### `POST /api/v1/voice/transcribe`

**Content-Type:** `multipart/form-data`

**Form field:** `file` — the `.wav` audio file to process.

**Success response (200):**

```json
{
  "filename": "recording.wav",
  "transcript": "Hello world",
  "llm_response": "Processed response from MIN MAX"
}
```

**Error responses (502):**

STT failure:

```json
{
  "error": "STT transcription failed",
  "details": "<reason>"
}
```

MIN MAX failure:

```json
{
  "error": "MIN MAX LLM query failed",
  "details": "<reason>"
}
```

## Configuration

Edit the top-level `var` block in `cmd/voice-orchestration/main.go` to set the service URLs and API key:

```go
var (
    STT_URL        = "http://localhost:8081/api/v1/stt/transcribe"
    MINMAX_URL     = "http://localhost:8082/api/v1/llm/chat"
    MINMAX_API_KEY = "your-minmax-api-key-here"
)
```

## Running

```bash
# From the repo root
go run ./cmd/voice-orchestration/main.go
```

The service listens on port 8080.

## Calling the endpoint with curl

```bash
curl -X POST http://localhost:8080/api/v1/voice/transcribe \
  -F "file=@/path/to/recording.wav"
```

## Requirements

- Go 1.21+
- STT service reachable at `STT_URL`
- MIN MAX LLM service reachable at `MINMAX_URL`