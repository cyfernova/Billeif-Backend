package sarvam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	sttPath                   = "/speech-to-text/ws"
	sttPCMFrameBytes          = 640
	sttMaximumMessageBytes    = 64 << 10
	sttMaximumTranscriptBytes = 16 << 10
	sttMaximumLanguageBytes   = 32
	sttMaximumRequestIDBytes  = 128
)

var (
	ErrSTTClientRequired   = errors.New("Sarvam STT client is required")
	ErrSTTDialerRequired   = errors.New("Sarvam STT dialer is required")
	ErrSTTInvalidFrame     = errors.New("Sarvam STT requires one 20 ms PCM16 frame")
	ErrSTTInvalidState     = errors.New("invalid Sarvam STT turn state")
	ErrSTTInvalidStream    = errors.New("invalid Sarvam STT stream")
	ErrSTTStreamClosed     = errors.New("Sarvam STT stream is closed")
	ErrSTTTransport        = errors.New("Sarvam STT transport failed")
	ErrSTTMalformedMessage = errors.New("malformed Sarvam STT message")
	ErrSTTMessageTooLarge  = errors.New("Sarvam STT message exceeds size limit")
	ErrSTTRateLimited      = errors.New("Sarvam STT rate limited")
	ErrSTTUnavailable      = errors.New("Sarvam STT unavailable")
)

// STTOpener is the provider-neutral runtime boundary for opening an STT stream.
type STTOpener interface {
	OpenSTT(context.Context) (STTStream, error)
}

// STTStream represents one warm provider socket that can process sequential turns.
type STTStream interface {
	WritePCM16(context.Context, []byte) error
	Flush(context.Context) error
	AwaitFinal(context.Context) (FinalTranscript, error)
	Done() <-chan struct{}
	Close() error
}

// FinalTranscript is the sole provider event authorized to complete a user turn.
type FinalTranscript struct {
	Text                string
	DetectedLanguage    string
	LanguageProbability *float64
}

// STTDialer is intentionally injected. There is no network-capable fallback.
type STTDialer interface {
	DialContext(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)
}

type STTClient struct {
	client   *Client
	dialer   STTDialer
	endpoint string
}

type sttTurnPhase uint8

const (
	sttAcceptingAudio sttTurnPhase = iota
	sttWaitingForFinal
	sttFinalReady
	sttTurnComplete
)

type sttStream struct {
	conn *websocket.Conn

	operationMu sync.Mutex
	stateMu     sync.Mutex
	phase       sttTurnPhase
	hasAudio    bool
	awaiting    bool
	terminalErr error

	finals   chan FinalTranscript
	done     chan struct{}
	stopOnce sync.Once
}

type sttAudioMessage struct {
	Audio sttAudioData `json:"audio"`
}

type sttAudioData struct {
	Data       []byte `json:"data"`
	SampleRate string `json:"sample_rate"`
	Encoding   string `json:"encoding"`
}

type sttFlushMessage struct {
	Type string `json:"type"`
}

type sttProviderMessageKind uint8

const (
	sttProviderEvent sttProviderMessageKind = iota
	sttProviderFinal
)

func NewSTTClient(client *Client, dialer STTDialer) (*STTClient, error) {
	if client == nil || client.apiKey == "" {
		return nil, ErrSTTClientRequired
	}
	if isNilSTTDialer(dialer) {
		return nil, ErrSTTDialerRequired
	}
	endpoint, err := client.endpoint(sttPath)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return nil, ErrInvalidRequest
	}
	parsed.RawQuery = (url.Values{
		"flush_signal":      {"true"},
		"input_audio_codec": {"pcm_s16le"},
		"language-code":     {"unknown"},
		"mode":              {"transcribe"},
		"model":             {"saaras:v3"},
		"sample_rate":       {"16000"},
	}).Encode()
	return &STTClient{client: client, dialer: dialer, endpoint: parsed.String()}, nil
}

func (c *STTClient) OpenSTT(ctx context.Context) (STTStream, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if c == nil || c.client == nil || c.client.apiKey == "" || isNilSTTDialer(c.dialer) || c.endpoint == "" {
		return nil, ErrSTTInvalidStream
	}
	if err := ctx.Err(); err != nil {
		return nil, sttContextError(err)
	}
	header := make(http.Header, 1)
	header.Set(APIKeyHeader, c.client.apiKey)
	conn, response, err := c.dialer.DialContext(ctx, c.endpoint, header)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, sttDialError(ctx, response, c.client.apiKey)
	}
	if conn == nil {
		return nil, ErrSTTTransport
	}
	conn.SetReadLimit(sttMaximumMessageBytes)
	stream := &sttStream{
		conn:   conn,
		phase:  sttAcceptingAudio,
		finals: make(chan FinalTranscript, 1),
		done:   make(chan struct{}),
	}
	go stream.readLoop()
	return stream, nil
}

func (c *STTClient) String() string {
	return "SarvamSTTClient"
}

func (s *sttStream) WritePCM16(ctx context.Context, pcm []byte) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if len(pcm) != sttPCMFrameBytes {
		return ErrSTTInvalidFrame
	}
	if s == nil || s.conn == nil || s.done == nil {
		return ErrSTTInvalidStream
	}
	if err := ctx.Err(); err != nil {
		return sttContextError(err)
	}

	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.isDone() {
		return ErrSTTStreamClosed
	}
	s.stateMu.Lock()
	if s.phase == sttTurnComplete {
		s.phase = sttAcceptingAudio
		s.hasAudio = false
	}
	if s.phase != sttAcceptingAudio {
		s.stateMu.Unlock()
		return ErrSTTInvalidState
	}
	s.stateMu.Unlock()

	payload, err := json.Marshal(sttAudioMessage{Audio: sttAudioData{
		Data:       pcm,
		SampleRate: "16000",
		Encoding:   "audio/wav",
	}})
	if err != nil {
		return ErrSTTInvalidStream
	}
	if err := s.writePayload(ctx, payload); err != nil {
		return err
	}
	s.stateMu.Lock()
	s.hasAudio = true
	s.stateMu.Unlock()
	return nil
}

func (s *sttStream) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if s == nil || s.conn == nil || s.done == nil {
		return ErrSTTInvalidStream
	}
	if err := ctx.Err(); err != nil {
		return sttContextError(err)
	}

	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if s.isDone() {
		return ErrSTTStreamClosed
	}
	s.stateMu.Lock()
	if s.phase != sttAcceptingAudio || !s.hasAudio {
		s.stateMu.Unlock()
		return ErrSTTInvalidState
	}
	// Publish the waiting phase before writing. A loopback provider can respond
	// before WriteMessage returns to this goroutine.
	s.phase = sttWaitingForFinal
	s.stateMu.Unlock()

	payload, err := json.Marshal(sttFlushMessage{Type: "flush"})
	if err != nil {
		s.terminate(ErrSTTInvalidStream)
		return ErrSTTInvalidStream
	}
	return s.writePayload(ctx, payload)
}

func (s *sttStream) AwaitFinal(ctx context.Context) (FinalTranscript, error) {
	if ctx == nil {
		return FinalTranscript{}, ErrContextRequired
	}
	if s == nil || s.conn == nil || s.done == nil || s.finals == nil {
		return FinalTranscript{}, ErrSTTInvalidStream
	}
	if err := ctx.Err(); err != nil {
		err = sttContextError(err)
		s.terminate(err)
		return FinalTranscript{}, err
	}

	s.stateMu.Lock()
	if s.awaiting || (s.phase != sttWaitingForFinal && s.phase != sttFinalReady) {
		s.stateMu.Unlock()
		if s.isDone() {
			return FinalTranscript{}, ErrSTTStreamClosed
		}
		return FinalTranscript{}, ErrSTTInvalidState
	}
	s.awaiting = true
	s.stateMu.Unlock()

	select {
	case final := <-s.finals:
		return s.finishAwait(final)
	case <-s.done:
		select {
		case final := <-s.finals:
			return s.finishAwait(final)
		default:
		}
		s.stateMu.Lock()
		err := s.terminalErr
		s.awaiting = false
		s.stateMu.Unlock()
		if err == nil {
			err = ErrSTTStreamClosed
		}
		return FinalTranscript{}, err
	case <-ctx.Done():
		err := sttContextError(ctx.Err())
		s.terminate(err)
		return FinalTranscript{}, err
	}
}

func (s *sttStream) finishAwait(final FinalTranscript) (FinalTranscript, error) {
	s.stateMu.Lock()
	s.awaiting = false
	if s.phase == sttFinalReady {
		s.phase = sttTurnComplete
	}
	s.stateMu.Unlock()
	return final, nil
}

func (s *sttStream) Done() <-chan struct{} {
	if s == nil || s.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.done
}

func (s *sttStream) Close() error {
	if s != nil {
		s.terminate(ErrSTTStreamClosed)
	}
	return nil
}

func (s *sttStream) String() string {
	return "SarvamSTTStream"
}

func (s *sttStream) writePayload(ctx context.Context, payload []byte) error {
	if s.isDone() {
		return ErrSTTStreamClosed
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(deadline)
	} else {
		_ = s.conn.SetWriteDeadline(time.Time{})
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		s.terminate(sttContextError(ctx.Err()))
	})
	err := s.conn.WriteMessage(websocket.TextMessage, payload)
	if !stop() {
		<-callbackDone
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return sttContextError(ctxErr)
	}
	if err != nil {
		if s.isDone() {
			return ErrSTTStreamClosed
		}
		s.terminate(ErrSTTTransport)
		return ErrSTTTransport
	}
	_ = s.conn.SetWriteDeadline(time.Time{})
	return nil
}

func (s *sttStream) readLoop() {
	for {
		messageType, payload, err := s.conn.ReadMessage()
		if err != nil {
			s.terminate(sttReadError(err))
			return
		}
		if messageType != websocket.TextMessage {
			s.terminate(ErrSTTMalformedMessage)
			return
		}
		kind, final, err := decodeSTTProviderMessage(payload)
		if err != nil {
			s.terminate(err)
			return
		}
		if kind == sttProviderEvent {
			continue
		}

		s.stateMu.Lock()
		if s.phase != sttWaitingForFinal {
			s.stateMu.Unlock()
			s.terminate(ErrSTTMalformedMessage)
			return
		}
		s.phase = sttFinalReady
		s.stateMu.Unlock()
		select {
		case s.finals <- final:
		default:
			s.terminate(ErrSTTMalformedMessage)
			return
		}
	}
}

func (s *sttStream) terminate(err error) {
	if err == nil {
		err = ErrSTTStreamClosed
	}
	s.stopOnce.Do(func() {
		s.stateMu.Lock()
		s.terminalErr = err
		s.stateMu.Unlock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		if s.done != nil {
			close(s.done)
		}
	})
}

func (s *sttStream) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func decodeSTTProviderMessage(payload []byte) (sttProviderMessageKind, FinalTranscript, error) {
	if len(payload) > sttMaximumMessageBytes {
		return 0, FinalTranscript{}, ErrSTTMessageTooLarge
	}
	if !utf8.Valid(payload) {
		return 0, FinalTranscript{}, ErrSTTMalformedMessage
	}
	top, err := decodeExactJSONObject(payload,
		map[string]struct{}{"type": {}, "data": {}},
		[]string{"type", "data"},
	)
	if err != nil {
		return 0, FinalTranscript{}, err
	}
	messageType, err := decodeRequiredString(top["type"])
	if err != nil {
		return 0, FinalTranscript{}, ErrSTTMalformedMessage
	}
	switch messageType {
	case "events":
		if err := decodeSTTEvent(top["data"]); err != nil {
			return 0, FinalTranscript{}, err
		}
		return sttProviderEvent, FinalTranscript{}, nil
	case "data":
		final, err := decodeSTTFinal(top["data"])
		return sttProviderFinal, final, err
	default:
		return 0, FinalTranscript{}, ErrSTTMalformedMessage
	}
}

func decodeSTTEvent(raw json.RawMessage) error {
	object, err := decodeExactJSONObject(raw,
		map[string]struct{}{"signal_type": {}},
		[]string{"signal_type"},
	)
	if err != nil {
		return err
	}
	signal, err := decodeRequiredString(object["signal_type"])
	if err != nil || (signal != "START_SPEECH" && signal != "END_SPEECH") {
		return ErrSTTMalformedMessage
	}
	return nil
}

func decodeSTTFinal(raw json.RawMessage) (FinalTranscript, error) {
	object, err := decodeExactJSONObject(raw, map[string]struct{}{
		"request_id":           {},
		"transcript":           {},
		"language_code":        {},
		"language_probability": {},
		"metrics":              {},
	}, []string{"transcript"})
	if err != nil {
		return FinalTranscript{}, err
	}
	transcript, err := decodeRequiredString(object["transcript"])
	if err != nil || strings.TrimSpace(transcript) == "" || len(transcript) > sttMaximumTranscriptBytes || containsControl(transcript) {
		return FinalTranscript{}, ErrSTTMalformedMessage
	}

	language, err := decodeOptionalString(object["language_code"])
	if err != nil || len(language) > sttMaximumLanguageBytes || !safeSTTLanguage(language) {
		return FinalTranscript{}, ErrSTTMalformedMessage
	}
	probability, err := decodeSTTProbability(object["language_probability"])
	if err != nil {
		return FinalTranscript{}, err
	}
	if requestIDRaw, ok := object["request_id"]; ok {
		requestID, err := decodeOptionalString(requestIDRaw)
		if err != nil || len(requestID) > sttMaximumRequestIDBytes || containsControl(requestID) {
			return FinalTranscript{}, ErrSTTMalformedMessage
		}
	}
	if metrics, ok := object["metrics"]; ok {
		if err := decodeSTTMetrics(metrics); err != nil {
			return FinalTranscript{}, err
		}
	}
	return FinalTranscript{
		Text:                transcript,
		DetectedLanguage:    language,
		LanguageProbability: probability,
	}, nil
}

func decodeSTTMetrics(raw json.RawMessage) error {
	metrics, err := decodeExactJSONObject(raw, map[string]struct{}{
		"audio_duration":     {},
		"processing_latency": {},
	}, nil)
	if err != nil {
		return err
	}
	for _, rawValue := range metrics {
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(rawValue))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return ErrSTTMalformedMessage
		}
		value, err := number.Float64()
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrSTTMalformedMessage
		}
	}
	return nil
}

func decodeExactJSONObject(raw []byte, allowed map[string]struct{}, required []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrSTTMalformedMessage
	}
	object := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, ErrSTTMalformedMessage
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, ErrSTTMalformedMessage
		}
		if _, ok := allowed[key]; !ok {
			return nil, ErrSTTMalformedMessage
		}
		if _, duplicate := object[key]; duplicate {
			return nil, ErrSTTMalformedMessage
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrSTTMalformedMessage
		}
		object[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrSTTMalformedMessage
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrSTTMalformedMessage
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			return nil, ErrSTTMalformedMessage
		}
	}
	return object, nil
}

func decodeRequiredString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", ErrSTTMalformedMessage
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", ErrSTTMalformedMessage
	}
	return value, nil
}

func decodeOptionalString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	return decodeRequiredString(raw)
}

func decodeSTTProbability(raw json.RawMessage) (*float64, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > 32 {
		return nil, ErrSTTMalformedMessage
	}
	text := string(trimmed)
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil || strings.TrimSpace(value) != value || value == "" {
			return nil, ErrSTTMalformedMessage
		}
		text = value
	}
	probability, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
		return nil, ErrSTTMalformedMessage
	}
	return &probability, nil
}

func safeSTTLanguage(value string) bool {
	if value == "" {
		return true
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func sttDialError(ctx context.Context, response *http.Response, apiKey string) error {
	if response != nil && response.Body != nil {
		_, _ = io.CopyN(io.Discard, response.Body, maxResponseDrainBytes)
		_ = response.Body.Close()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return sttContextError(ctxErr)
	}
	if response == nil {
		return ErrSTTTransport
	}
	requestID := safeRequestID(response.Header.Get("x-request-id"), apiKey)
	providerErr := &ProviderError{StatusCode: response.StatusCode, RequestID: requestID}
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrSTTRateLimited, providerErr)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %w", ErrSTTUnavailable, providerErr)
	default:
		return providerErr
	}
}

func sttReadError(err error) error {
	if errors.Is(err, websocket.ErrReadLimit) {
		return ErrSTTMessageTooLarge
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) && (closeErr.Code == websocket.CloseInternalServerErr || closeErr.Code == websocket.CloseTryAgainLater) {
		return ErrSTTUnavailable
	}
	return ErrSTTStreamClosed
}

func sttContextError(err error) error {
	return fmt.Errorf("%w: %w", ErrSTTTransport, err)
}

func isNilSTTDialer(dialer STTDialer) bool {
	if dialer == nil {
		return true
	}
	value := reflect.ValueOf(dialer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ STTOpener = (*STTClient)(nil)
var _ STTStream = (*sttStream)(nil)
