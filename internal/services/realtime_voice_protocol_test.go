package services

import (
	"encoding/json"
	"testing"

	"invoice-backend/internal/config"

	"github.com/stretchr/testify/require"
)

func TestBuildDeepgramVoiceAgentSettingsUsesConfiguredLLMEndpoint(t *testing.T) {
	cfg := config.VoiceRealtimeConfig{
		DeepgramAPIKey:        "dg",
		DeepgramVoiceAgentURL: "wss://agent.deepgram.test/v1/agent/converse",
		InputEncoding:         "linear16",
		InputSampleRate:       24000,
		OutputEncoding:        "linear16",
		OutputSampleRate:      24000,
		ListenModel:           "nova-3",
		SpeakModel:            "aura-2-thalia-en",
		DeepSeekAPIKey:        "voice-llm-key",
		DeepSeekBaseURL:       "https://voice-llm.example.test",
		DeepSeekModel:         "voice-test-model",
	}

	settings := BuildDeepgramVoiceAgentSettings(cfg, DeepgramVoiceAgentSettingsOptions{
		BusinessID:     "biz-123",
		ConversationID: "conv-123",
		Language:       "en",
	})

	require.Equal(t, "Settings", settings["type"])
	audio := settings["audio"].(map[string]interface{})
	input := audio["input"].(map[string]interface{})
	output := audio["output"].(map[string]interface{})
	require.Equal(t, "linear16", input["encoding"])
	require.Equal(t, 24000, input["sample_rate"])
	require.Equal(t, "linear16", output["encoding"])
	require.Equal(t, "none", output["container"])

	agent := settings["agent"].(map[string]interface{})
	think := agent["think"].(map[string]interface{})
	provider := think["provider"].(map[string]interface{})
	endpoint := think["endpoint"].(map[string]interface{})
	headers := endpoint["headers"].(map[string]interface{})
	require.Equal(t, "open_ai", provider["type"])
	require.Equal(t, "voice-test-model", provider["model"])
	require.Equal(t, "https://voice-llm.example.test/chat/completions", endpoint["url"])
	require.Equal(t, "Bearer voice-llm-key", headers["authorization"])
	require.Contains(t, think["prompt"], "biz-123")
}

func TestParseRealtimeVoiceControl(t *testing.T) {
	event, err := ParseRealtimeVoiceControl([]byte(`{"type":"client_context","conversation_id":"c1","business_id":"b1","visible_messages":[]}`))
	require.NoError(t, err)
	require.Equal(t, "client_context", event.Type)
	require.Equal(t, "c1", event.ConversationID)
	require.Equal(t, "b1", event.BusinessID)

	_, err = ParseRealtimeVoiceControl([]byte(`{"type":"record_file"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

func TestMapDeepgramJSONEvent(t *testing.T) {
	event, ok, err := MapDeepgramJSONEvent([]byte(`{"type":"ConversationText","role":"assistant","content":"Hello"}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, AppEventConversationText, event.Type)
	require.Equal(t, "assistant", event.Role)
	require.Equal(t, "Hello", event.Content)

	event, ok, err = MapDeepgramJSONEvent([]byte(`{"type":"UserStartedSpeaking"}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, AppEventUserStartedSpeaking, event.Type)

	event, ok, err = MapDeepgramJSONEvent([]byte(`{"type":"Error","code":"FAILED_TO_THINK","message":"LLM failed"}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, AppEventError, event.Type)
	require.Equal(t, "FAILED_TO_THINK", event.Code)
	require.Equal(t, "LLM failed", event.Message)
}

func TestFunctionCallRequestMapsSafely(t *testing.T) {
	payload := []byte(`{"type":"FunctionCallRequest","functions":[{"id":"f1","name":"do_secret","arguments":"{}","client_side":true}]}`)
	event, ok, err := MapDeepgramJSONEvent(payload)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "function_call_request", event.Type)

	raw, err := json.Marshal(event.Data)
	require.NoError(t, err)
	require.Contains(t, string(raw), "do_secret")
	require.NotContains(t, string(raw), "arguments")
}
