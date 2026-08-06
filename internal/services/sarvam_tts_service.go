package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/pkg/logger"
)

const (
	SarvamTTSModel          = sarvam.DefaultTTSModel
	SarvamTTSMaxCharacters  = sarvam.TTSMaxCharacters
	SarvamTTSMaxAudioBytes  = sarvam.MaxResponseBytes
	defaultSarvamTTSSpeaker = sarvam.DefaultTTSSpeaker
)

var (
	ErrSarvamTTSUnavailable = errors.New("sarvam text-to-speech is not configured")
	ErrInvalidSarvamTTS     = errors.New("invalid sarvam text-to-speech request")
)

type SarvamLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var SarvamLanguages = []SarvamLanguage{
	{Code: "en-IN", Name: "English (India)"},
	{Code: "hi-IN", Name: "Hindi"},
	{Code: "bn-IN", Name: "Bengali"},
	{Code: "ta-IN", Name: "Tamil"},
	{Code: "te-IN", Name: "Telugu"},
	{Code: "kn-IN", Name: "Kannada"},
	{Code: "ml-IN", Name: "Malayalam"},
	{Code: "mr-IN", Name: "Marathi"},
	{Code: "gu-IN", Name: "Gujarati"},
	{Code: "pa-IN", Name: "Punjabi"},
	{Code: "od-IN", Name: "Odia"},
}

type SarvamTTSRequest struct {
	Text                 string  `json:"text"`
	LanguageCode         string  `json:"language_code"`
	Speaker              string  `json:"speaker,omitempty"`
	Model                string  `json:"model,omitempty"`
	OutputAudioCodec     string  `json:"output_audio_codec,omitempty"`
	OutputAudioBitrate   string  `json:"output_audio_bitrate,omitempty"`
	Pace                 float64 `json:"pace,omitempty"`
	SpeechSampleRate     int     `json:"speech_sample_rate,omitempty"`
	Temperature          float64 `json:"temperature,omitempty"`
	DictionaryID         string  `json:"dict_id,omitempty"`
	EnablePreprocessing  bool    `json:"enable_preprocessing,omitempty"`
	EnableCachedResponse bool    `json:"enable_cached_responses,omitempty"`
}

type SarvamTTSResult struct {
	Audio       []byte
	ContentType string
	RequestID   string
}

type SarvamTTSService struct {
	appCfg   *config.Config
	resolver ProviderConfigResolver
	client   *http.Client
	log      *logger.Logger
}

func NewSarvamTTSService(appCfg *config.Config, resolver ProviderConfigResolver, client *http.Client, log *logger.Logger) *SarvamTTSService {
	cfg := config.SarvamConfig{}
	if appCfg != nil {
		cfg = appCfg.Sarvam
	}
	if client == nil {
		timeout := time.Duration(cfg.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &SarvamTTSService{appCfg: appCfg, resolver: resolver, client: client, log: log.Named("sarvam_tts_service")}
}

func (s *SarvamTTSService) Synthesize(ctx context.Context, input SarvamTTSRequest) (*SarvamTTSResult, error) {
	if s == nil || s.appCfg == nil {
		return nil, ErrSarvamTTSUnavailable
	}
	cfg := s.appCfg.Sarvam
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(s.appCfg.Secrets.Sarvam) != "" {
		if s.resolver == nil {
			return nil, ErrSarvamTTSUnavailable
		}
		resolved, err := s.resolver.ResolveProvider(ctx, s.appCfg, config.SecretSarvam)
		if err != nil {
			return nil, fmt.Errorf("resolve Sarvam credentials: %w", err)
		}
		cfg = resolved.Sarvam
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrSarvamTTSUnavailable
	}
	input = normalizeSarvamTTSRequest(input)
	if err := validateSarvamTTSRequest(input); err != nil {
		return nil, err
	}
	providerClient, err := sarvam.NewClient(sarvam.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		TTSModel:   SarvamTTSModel,
		TTSSpeaker: defaultSarvamTTSSpeaker,
	}, s.client)
	if err != nil {
		return nil, fmt.Errorf("configure Sarvam TTS client: %w", err)
	}
	response, err := providerClient.PostJSON(ctx, sarvam.JSONRequest{
		Path:          "/text-to-speech/stream",
		Payload:       input,
		Accept:        sarvamCodecContentType(input.OutputAudioCodec),
		ResponseLimit: SarvamTTSMaxAudioBytes,
	})
	if err != nil {
		if errors.Is(err, sarvam.ErrResponseTooLarge) {
			return nil, fmt.Errorf("Sarvam TTS response exceeds %d bytes: %w", SarvamTTSMaxAudioBytes, err)
		}
		return nil, fmt.Errorf("call Sarvam TTS: %w", err)
	}
	contentType := strings.TrimSpace(response.ContentType)
	if contentType == "" {
		contentType = sarvamCodecContentType(input.OutputAudioCodec)
	}
	return &SarvamTTSResult{Audio: response.Body, ContentType: contentType, RequestID: response.RequestID}, nil
}

type SarvamProviderError = sarvam.ProviderError

func normalizeSarvamTTSRequest(input SarvamTTSRequest) SarvamTTSRequest {
	input.Text = strings.TrimSpace(input.Text)
	input.LanguageCode = strings.TrimSpace(input.LanguageCode)
	if input.LanguageCode == "or-IN" {
		input.LanguageCode = "od-IN"
	}
	input.Speaker = strings.TrimSpace(input.Speaker)
	input.Model = strings.TrimSpace(input.Model)
	input.OutputAudioCodec = strings.ToLower(strings.TrimSpace(input.OutputAudioCodec))
	input.OutputAudioBitrate = strings.ToLower(strings.TrimSpace(input.OutputAudioBitrate))
	input.DictionaryID = strings.TrimSpace(input.DictionaryID)
	if input.Speaker == "" {
		input.Speaker = defaultSarvamTTSSpeaker
	}
	if input.Model == "" {
		input.Model = SarvamTTSModel
	}
	if input.OutputAudioCodec == "" {
		input.OutputAudioCodec = "mp3"
	}
	if input.OutputAudioBitrate == "" {
		input.OutputAudioBitrate = "128k"
	}
	if input.Pace == 0 {
		input.Pace = 1
	}
	if input.SpeechSampleRate == 0 {
		input.SpeechSampleRate = 24000
	}
	if input.Temperature == 0 {
		input.Temperature = 0.6
	}
	return input
}

func validateSarvamTTSRequest(input SarvamTTSRequest) error {
	if input.Text == "" || len([]rune(input.Text)) > SarvamTTSMaxCharacters {
		return fmt.Errorf("%w: text is required and must not exceed %d characters", ErrInvalidSarvamTTS, SarvamTTSMaxCharacters)
	}
	if !isSarvamLanguage(input.LanguageCode) {
		return fmt.Errorf("%w: unsupported language_code %q", ErrInvalidSarvamTTS, input.LanguageCode)
	}
	if input.Model != SarvamTTSModel {
		return fmt.Errorf("%w: model must be %q", ErrInvalidSarvamTTS, SarvamTTSModel)
	}
	if !sarvamContainsString([]string{"mp3", "wav", "aac", "opus", "flac", "linear16", "mulaw", "alaw"}, input.OutputAudioCodec) {
		return fmt.Errorf("%w: unsupported output_audio_codec", ErrInvalidSarvamTTS)
	}
	if !sarvamContainsString([]string{"32k", "64k", "128k", "192k", "256k"}, input.OutputAudioBitrate) {
		return fmt.Errorf("%w: unsupported output_audio_bitrate", ErrInvalidSarvamTTS)
	}
	if input.Pace < 0.5 || input.Pace > 2 {
		return fmt.Errorf("%w: pace must be between 0.5 and 2.0", ErrInvalidSarvamTTS)
	}
	if input.Temperature < 0.01 || input.Temperature > 1 {
		return fmt.Errorf("%w: temperature must be between 0.01 and 1.0", ErrInvalidSarvamTTS)
	}
	if !containsInt([]int{8000, 16000, 22050, 24000}, input.SpeechSampleRate) {
		return fmt.Errorf("%w: speech_sample_rate must be 8000, 16000, 22050, or 24000", ErrInvalidSarvamTTS)
	}
	return nil
}

func isSarvamLanguage(code string) bool {
	for _, language := range SarvamLanguages {
		if language.Code == code {
			return true
		}
	}
	return false
}

func sarvamContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sarvamCodecContentType(codec string) string {
	switch codec {
	case "mp3":
		return "audio/mpeg"
	case "wav", "linear16":
		return "audio/wav"
	case "aac":
		return "audio/aac"
	case "opus":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "mulaw":
		return "audio/basic"
	case "alaw":
		return "audio/x-alaw-basic"
	default:
		return "application/octet-stream"
	}
}
