package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
)

func TestSarvamTTSServiceSupportsEveryDocumentedLanguage(t *testing.T) {
	for _, language := range SarvamLanguages {
		t.Run(language.Code, func(t *testing.T) {
			var received SarvamTTSRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/text-to-speech/stream", r.URL.Path)
				require.Equal(t, "secret", r.Header.Get("api-subscription-key"))
				require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				w.Header().Set("Content-Type", "audio/mpeg")
				w.Header().Set("x-request-id", "request-1")
				_, _ = io.WriteString(w, "audio")
			}))
			defer server.Close()

			svc := NewSarvamTTSService(&config.Config{Sarvam: config.SarvamConfig{APIKey: "secret", BaseURL: server.URL}}, nil, server.Client(), logger.New())
			result, err := svc.Synthesize(context.Background(), SarvamTTSRequest{Text: "hello", LanguageCode: language.Code})

			require.NoError(t, err)
			require.Equal(t, language.Code, received.LanguageCode)
			require.Equal(t, SarvamTTSModel, received.Model)
			require.Equal(t, "shubh", received.Speaker)
			require.Equal(t, "audio/mpeg", result.ContentType)
			require.Equal(t, []byte("audio"), result.Audio)
			require.Equal(t, "request-1", result.RequestID)
		})
	}
}

func TestSarvamTTSServiceRejectsInvalidInputBeforeProviderCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	svc := NewSarvamTTSService(&config.Config{Sarvam: config.SarvamConfig{APIKey: "secret", BaseURL: server.URL}}, nil, server.Client(), logger.New())

	_, err := svc.Synthesize(context.Background(), SarvamTTSRequest{Text: "hello", LanguageCode: "fr-FR"})

	require.ErrorIs(t, err, ErrInvalidSarvamTTS)
	require.False(t, called)
}
