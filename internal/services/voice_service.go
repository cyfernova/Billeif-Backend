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

// Transcribe processes audio data and returns transcription
func (s *VoiceService) Transcribe(ctx context.Context, audioData []byte, filename string) (*TranscriptionResult, error) {
	s.log.Debug("transcribing audio", "filename", filename, "size", len(audioData))

	text, err := s.transcriber.TranscribeAudio(ctx, audioData, filename)
	if err != nil {
		s.log.Error("transcription failed", "error", err)
		return nil, err
	}

	s.log.Info("transcription completed", "text_length", len(text))

	return &TranscriptionResult{Text: text}, nil
}
