package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeVoiceService struct {
	transcript  string
	err         error
	called      bool
	speakCalled bool
	filename    string
	contentType string
	audioSize   int
	speechAudio []byte
	speechType  string
}

func (f *fakeVoiceService) Transcribe(ctx context.Context, audioData []byte, filename string, contentType string) (*services.TranscriptionResult, error) {
	f.called = true
	f.filename = filename
	f.contentType = contentType
	f.audioSize = len(audioData)
	if f.err != nil {
		return nil, f.err
	}
	return &services.TranscriptionResult{Text: f.transcript}, nil
}

func (f *fakeVoiceService) Speak(ctx context.Context, text string) (*services.SpeechResult, error) {
	f.speakCalled = true
	if f.err != nil {
		return nil, f.err
	}
	audio := f.speechAudio
	if len(audio) == 0 {
		audio = []byte("mp3-bytes")
	}
	contentType := f.speechType
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	return &services.SpeechResult{Audio: audio, ContentType: contentType}, nil
}

type fakeLLMService struct {
	response string
	err      error
	called   bool
	messages []services.ChatMessage
	options  services.LLMChatOptions
}

func (f *fakeLLMService) Chat(ctx context.Context, messages []services.ChatMessage) (string, error) {
	f.called = true
	f.messages = append([]services.ChatMessage(nil), messages...)
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeLLMService) ChatWithOptions(ctx context.Context, messages []services.ChatMessage, options services.LLMChatOptions) (string, error) {
	f.options = options
	return f.Chat(ctx, messages)
}

func newVoiceAgentRouter(voice *fakeVoiceService, llm *fakeLLMService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewVoiceHandler(voice, llm, logger.New())
	router.POST("/voice/agent", handler.Agent)
	return router
}

func newVoiceAgentRequest(t *testing.T, messages *string, filename string, fileContentType string, fileBody []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if fileBody != nil {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		if fileContentType != "" {
			header.Set("Content-Type", fileContentType)
		}
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(fileBody)
		require.NoError(t, err)
	}

	if messages != nil {
		require.NoError(t, writer.WriteField("messages", *messages))
	}

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/voice/agent", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestVoiceAgentSuccess(t *testing.T) {
	messages := `[{"role":"user","content":"Who owes me money?"},{"role":"assistant","content":"I can check receivables."}]`
	voice := &fakeVoiceService{transcript: "Show overdue invoices"}
	llm := &fakeLLMService{response: "Here are the overdue invoices."}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, &messages, "turn.m4a", "audio/m4a", []byte("audio-bytes")))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, voice.called)
	require.Equal(t, "turn.m4a", voice.filename)
	require.Equal(t, "audio/m4a", voice.contentType)
	require.Equal(t, len("audio-bytes"), voice.audioSize)
	require.True(t, llm.called)
	require.Len(t, llm.messages, 3)
	require.Equal(t, "Show overdue invoices", llm.messages[2].Content)
	require.Equal(t, voiceAgentMaxTokens, llm.options.MaxTokens)
	require.NotEmpty(t, llm.options.System)
	require.True(t, voice.speakCalled)

	var response VoiceAgentResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "Show overdue invoices", response.Transcript)
	require.Equal(t, "Here are the overdue invoices.", response.Response)
	require.NotEmpty(t, response.ResponseAudioBase64)
	require.Equal(t, "audio/mpeg", response.ResponseAudioContentType)
	require.Equal(t, "billeif-response.mp3", response.ResponseAudioFilename)
	require.Equal(t, "turn.m4a", response.Filename)
	require.Equal(t, len("audio-bytes"), response.Size)
}

func TestVoiceAgentMissingFile(t *testing.T) {
	voice := &fakeVoiceService{}
	llm := &fakeLLMService{}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, nil, "", "", nil))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.False(t, voice.called)
	require.False(t, llm.called)
}

func TestVoiceAgentEmptyTranscript(t *testing.T) {
	voice := &fakeVoiceService{transcript: "   "}
	llm := &fakeLLMService{response: "unused"}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, nil, "turn.wav", "", []byte("audio-bytes")))

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.Equal(t, "audio/wav", voice.contentType)
	require.False(t, llm.called)
}

func TestVoiceAgentMalformedMessages(t *testing.T) {
	messages := `{"role":"user","content":"not an array"}`
	voice := &fakeVoiceService{transcript: "unused"}
	llm := &fakeLLMService{response: "unused"}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, &messages, "turn.wav", "audio/wav", []byte("audio-bytes")))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.False(t, voice.called)
	require.False(t, llm.called)
}

func TestVoiceAgentTranscriptionFailure(t *testing.T) {
	voice := &fakeVoiceService{err: errors.New("deepgram unavailable")}
	llm := &fakeLLMService{response: "unused"}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, nil, "turn.wav", "audio/wav", []byte("audio-bytes")))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.True(t, voice.called)
	require.False(t, llm.called)
}

func TestVoiceAgentLLMFailure(t *testing.T) {
	voice := &fakeVoiceService{transcript: "Summarize sales"}
	llm := &fakeLLMService{err: errors.New("minimax unavailable")}
	router := newVoiceAgentRouter(voice, llm)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newVoiceAgentRequest(t, nil, "turn.wav", "audio/wav", []byte("audio-bytes")))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.True(t, voice.called)
	require.True(t, llm.called)
}
