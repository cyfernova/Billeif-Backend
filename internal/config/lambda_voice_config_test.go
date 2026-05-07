package config

import (
	"testing"
)

func TestLoadLambdaVoiceConfigRequiresVoiceEnv(t *testing.T) {
	t.Setenv("ENVIRONMENT", "dev")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("COGNITO_USER_POOL_ID", "pool")
	t.Setenv("COGNITO_CLIENT_ID", "client")
	t.Setenv("COGNITO_REGION", "us-east-1")
	t.Setenv("WEBSOCKET_API_ENDPOINT", "https://example.execute-api.us-east-1.amazonaws.com/dev")
	t.Setenv("WEBSOCKET_CONNECTIONS_TABLE", "connections")
	t.Setenv("VOICE_SESSIONS_TABLE", "voice-sessions")

	_, err := LoadLambdaVoiceConfig(true)
	if err == nil {
		t.Fatal("expected missing voice provider config to fail")
	}
}

func TestLoadLambdaVoiceConfigAcceptsExplicitVoiceEnv(t *testing.T) {
	t.Setenv("ENVIRONMENT", "dev")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("COGNITO_USER_POOL_ID", "pool")
	t.Setenv("COGNITO_CLIENT_ID", "client")
	t.Setenv("COGNITO_REGION", "us-east-1")
	t.Setenv("WEBSOCKET_API_ENDPOINT", "https://example.execute-api.us-east-1.amazonaws.com/dev")
	t.Setenv("WEBSOCKET_CONNECTIONS_TABLE", "connections")
	t.Setenv("VOICE_SESSIONS_TABLE", "voice-sessions")
	t.Setenv("VOICE_SESSION_WORKER_FUNCTION_NAME", "voice-worker")
	t.Setenv("DEEPGRAM_API_KEY", "dg")
	t.Setenv("DEEPGRAM_VOICE_AGENT_URL", "wss://voice.example.test")
	t.Setenv("DEEPGRAM_VOICE_INPUT_ENCODING", "linear16")
	t.Setenv("DEEPGRAM_VOICE_INPUT_SAMPLE_RATE", "24000")
	t.Setenv("DEEPGRAM_VOICE_OUTPUT_ENCODING", "linear16")
	t.Setenv("DEEPGRAM_VOICE_OUTPUT_SAMPLE_RATE", "24000")
	t.Setenv("DEEPGRAM_VOICE_LISTEN_MODEL", "listen")
	t.Setenv("DEEPGRAM_VOICE_SPEAK_MODEL", "speak")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek")
	t.Setenv("DEEPSEEK_BASE_URL", "https://deepseek.example.test")
	t.Setenv("DEEPSEEK_MODEL", "model")
	t.Setenv("VOICE_WS_MAX_SESSION_SECONDS", "840")
	t.Setenv("VOICE_WS_PING_INTERVAL_SECONDS", "20")
	t.Setenv("VOICE_WS_WRITE_TIMEOUT_SECONDS", "5")
	t.Setenv("VOICE_WS_MAX_FRAME_BYTES", "16000")
	t.Setenv("VOICE_WS_MAX_CONCURRENT_SESSIONS_PER_USER", "1")
	t.Setenv("VOICE_WS_EVENT_POLL_INTERVAL_MS", "25")
	t.Setenv("VOICE_WS_EVENT_TTL_SECONDS", "900")
	t.Setenv("VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES", "12000")
	t.Setenv("VOICE_WS_PROVIDER_READY_TIMEOUT_SECONDS", "10")

	cfg, err := LoadLambdaVoiceConfig(true)
	if err != nil {
		t.Fatalf("expected explicit voice config to load: %v", err)
	}
	if cfg.VoiceRealtime.InputSampleRate != 24000 {
		t.Fatalf("unexpected input sample rate: %d", cfg.VoiceRealtime.InputSampleRate)
	}
	if cfg.SessionWorkerFunctionName != "voice-worker" {
		t.Fatalf("unexpected worker function name: %s", cfg.SessionWorkerFunctionName)
	}
}
