package services

import (
	"context"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/pipecat"
)

// VoiceService handles voice transcription using Deepgram
type VoiceService struct {
	transcriber *pipecat.DeepgramClient
	log         *logger.Logger
}

// NewVoiceService creates a new voice service
func NewVoiceService(cfg config.DeepgramConfig, log *logger.Logger) *VoiceService {
	return &VoiceService{
		transcriber: pipecat.NewDeepgramClient(cfg.APIKey),
		log:         log,
	}
}

// TranscriptionResult represents the result of voice transcription
type TranscriptionResult struct {
	Text string `json:"text"`
}

type SpeechResult struct {
	Audio       []byte
	ContentType string
}

// Transcribe processes audio data and returns transcription.
func (s *VoiceService) Transcribe(ctx context.Context, audioData []byte, filename string, contentType string) (*TranscriptionResult, error) {
	s.log.Debug("transcribing audio", "filename", filename, "size", len(audioData), "content_type", contentType)

	text, err := s.transcriber.TranscribeAudio(ctx, audioData, filename, contentType)
	if err != nil {
		s.log.Error("transcription failed", "error", err)
		return nil, err
	}

	s.log.Info("transcription completed", "text_length", len(text))

	return &TranscriptionResult{Text: text}, nil
}

// Speak synthesizes assistant text into speech audio.
func (s *VoiceService) Speak(ctx context.Context, text string) (*SpeechResult, error) {
	s.log.Debug("synthesizing voice response", "text_length", len(text))

	audio, contentType, err := s.transcriber.SpeakText(ctx, text)
	if err != nil {
		s.log.Error("speech synthesis failed", "error", err)
		return nil, err
	}

	s.log.Info("speech synthesis completed", "audio_size", len(audio), "content_type", contentType)

	return &SpeechResult{Audio: audio, ContentType: contentType}, nil
}
