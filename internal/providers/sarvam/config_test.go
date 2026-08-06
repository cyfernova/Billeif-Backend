package sarvam

import "testing"

func TestConfigDefaultsMatchApprovedSarvamModels(t *testing.T) {
	got := (Config{}).withDefaults()

	if got.BaseURL != "https://api.sarvam.ai" {
		t.Fatalf("BaseURL = %q, want %q", got.BaseURL, "https://api.sarvam.ai")
	}
	if got.STTModel != "saaras:v3" {
		t.Fatalf("STTModel = %q, want %q", got.STTModel, "saaras:v3")
	}
	if got.LLMModel != "sarvam-105b" {
		t.Fatalf("LLMModel = %q, want %q", got.LLMModel, "sarvam-105b")
	}
	if got.TTSModel != "bulbul:v3" {
		t.Fatalf("TTSModel = %q, want %q", got.TTSModel, "bulbul:v3")
	}
	if got.TTSSpeaker != "shubh" {
		t.Fatalf("TTSSpeaker = %q, want %q", got.TTSSpeaker, "shubh")
	}
}

func TestConfigDefaultsPreserveExplicitValues(t *testing.T) {
	got := (Config{
		APIKey:     "test-key",
		BaseURL:    "https://sarvam.example.test/base",
		STTModel:   "stt-model",
		LLMModel:   "llm-model",
		TTSModel:   "tts-model",
		TTSSpeaker: "speaker",
	}).withDefaults()

	if got.APIKey != "test-key" || got.BaseURL != "https://sarvam.example.test/base" ||
		got.STTModel != "stt-model" || got.LLMModel != "llm-model" ||
		got.TTSModel != "tts-model" || got.TTSSpeaker != "speaker" {
		t.Fatalf("explicit config was not preserved: %#v", got)
	}
}
