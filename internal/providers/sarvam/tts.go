package sarvam

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	ttsPath = "/text-to-speech/ws"

	ttsMaximumTextRunes     = 500
	ttsMaximumTextMessages  = 16
	ttsMaximumTextAggregate = 2_000
	ttsMaximumTextBytes     = 8_000
	ttsMaximumMessageBytes  = 1 << 20
	ttsMaximumPCMChunkBytes = 512 << 10
	ttsMaximumPCMAggregate  = 4 << 20
	ttsMaximumRequestID     = 128
	ttsMaximumErrorCode     = 64
	ttsMaximumFinalMessage  = 4 << 10
	ttsMaximumErrorMessage  = 4 << 10
	ttsMaximumErrorDetails  = 16 << 10
	ttsMaximumTimestamp     = 64
	ttsMaximumJSONDepth     = 16
	ttsMaximumJSONEntries   = 256
	ttsEventQueueCapacity   = 1

	// The launch plan treats Bulbul linear16 as raw mono PCM16LE. Sarvam's
	// current WebSocket contract does not explicitly state channel count or
	// byte order, so the offline characterization gate must remain in place.
	ttsPCMFormatAssumption = "raw-mono-pcm16le"
	ttsPCMSampleRate       = 16000
	ttsPCMChannels         = 1
	ttsPCMBytesPerSample   = 2
)

var (
	ErrTTSClientRequired   = errors.New("Sarvam TTS client is required")
	ErrTTSDialerRequired   = errors.New("Sarvam TTS dialer is required")
	ErrTTSInvalidLanguage  = errors.New("invalid Sarvam TTS language")
	ErrTTSInvalidText      = errors.New("invalid Sarvam TTS text")
	ErrTTSInvalidState     = errors.New("invalid Sarvam TTS stream state")
	ErrTTSInvalidStream    = errors.New("invalid Sarvam TTS stream")
	ErrTTSStreamClosed     = errors.New("Sarvam TTS stream is closed")
	ErrTTSStreamComplete   = errors.New("Sarvam TTS stream is complete")
	ErrTTSTransport        = errors.New("Sarvam TTS transport failed")
	ErrTTSMalformedMessage = errors.New("malformed Sarvam TTS message")
	ErrTTSMessageTooLarge  = errors.New("Sarvam TTS message exceeds size limit")
	ErrTTSRateLimited      = errors.New("Sarvam TTS rate limited")
	ErrTTSUnavailable      = errors.New("Sarvam TTS unavailable")
)

var ttsLanguages = [...]string{
	"bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN",
	"mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN",
}

// TTSOpener is the provider-neutral runtime boundary for one active speech
// generation. Each stream owns exactly one provider WebSocket.
type TTSOpener interface {
	OpenTTS(context.Context, string) (TTSStream, error)
}

// TTSStream serializes outbound messages and exposes provider audio through a
// single ordered event sequence. Close is the hard barge-in operation.
type TTSStream interface {
	WriteText(context.Context, string) error
	Flush(context.Context) error
	Ping(context.Context) error
	Next(context.Context) (TTSEvent, error)
	Done() <-chan struct{}
	Close() error
}

// TTSEvent is either an audio event or the terminal Final event. PCM16 is a
// caller-owned deep copy. By launch-plan assumption it is raw mono PCM16LE at
// 16 kHz; audio events have nonempty even PCM16, while Final has empty PCM16.
type TTSEvent struct {
	PCM16       []byte
	ContentType string
	RequestID   string
	Final       bool
}

// TTSDialer is intentionally injected. There is no network-capable fallback.
type TTSDialer interface {
	DialContext(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)
}

type TTSClient struct {
	client   *Client
	dialer   TTSDialer
	endpoint string
}

type ttsPhase uint8

const (
	ttsAcceptingText ttsPhase = iota
	ttsFlushed
	ttsFinal
	ttsClosed
)

type ttsReadResult struct {
	event TTSEvent
	err   error
}

type ttsStream struct {
	conn *websocket.Conn

	writeMu        sync.Mutex
	nextMu         sync.Mutex
	stateMu        sync.Mutex
	phase          ttsPhase
	hasText        bool
	texts          int
	textRunes      int
	textBytes      int
	pcmSize        int64
	finalPending   bool
	finalDelivered bool

	events      chan ttsReadResult
	done        chan struct{}
	stopOnce    sync.Once
	terminalErr error
}

type ttsConfigEnvelope struct {
	Type string        `json:"type"`
	Data ttsConfigData `json:"data"`
}

type ttsConfigData struct {
	LanguageCode     string  `json:"language_code"`
	Speaker          string  `json:"speaker"`
	SpeechSampleRate int     `json:"speech_sample_rate"`
	OutputAudioCodec string  `json:"output_audio_codec"`
	Pace             float64 `json:"pace"`
	Temperature      float64 `json:"temperature"`
	MinBufferSize    int     `json:"min_buffer_size"`
	MaxChunkLength   int     `json:"max_chunk_length"`
}

type ttsTextEnvelope struct {
	Type string      `json:"type"`
	Data ttsTextData `json:"data"`
}

type ttsTextData struct {
	Text string `json:"text"`
}

type ttsControlEnvelope struct {
	Type string `json:"type"`
}

func NewTTSClient(client *Client, dialer TTSDialer) (*TTSClient, error) {
	if client == nil || client.apiKey == "" {
		return nil, ErrTTSClientRequired
	}
	if isNilTTSDialer(dialer) {
		return nil, ErrTTSDialerRequired
	}
	endpoint, err := client.endpoint(ttsPath)
	if err != nil {
		return nil, ErrTTSInvalidStream
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, ErrTTSInvalidStream
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return nil, ErrTTSInvalidStream
	}
	parsed.RawQuery = (url.Values{
		"model":                 {DefaultTTSModel},
		"send_completion_event": {"true"},
	}).Encode()
	return &TTSClient{client: client, dialer: dialer, endpoint: parsed.String()}, nil
}

func (client *TTSClient) OpenTTS(ctx context.Context, languageCode string) (TTSStream, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if client == nil || client.client == nil || client.client.apiKey == "" || isNilTTSDialer(client.dialer) || client.endpoint == "" {
		return nil, ErrTTSInvalidStream
	}
	if !validTTSLanguage(languageCode) {
		return nil, ErrTTSInvalidLanguage
	}
	if err := ctx.Err(); err != nil {
		return nil, ttsContextError(err)
	}
	header := make(http.Header, 1)
	header.Set(APIKeyHeader, client.client.apiKey)
	conn, response, err := client.dialer.DialContext(ctx, client.endpoint, header)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, ttsDialError(ctx, response, client.client.apiKey)
	}
	if conn == nil {
		return nil, ErrTTSTransport
	}
	conn.SetReadLimit(ttsMaximumMessageBytes)
	stream := &ttsStream{
		conn: conn, phase: ttsAcceptingText,
		events: make(chan ttsReadResult, ttsEventQueueCapacity),
		done:   make(chan struct{}),
	}
	config := ttsConfigEnvelope{Type: "config", Data: ttsConfigData{
		LanguageCode: languageCode, Speaker: DefaultTTSSpeaker,
		SpeechSampleRate: ttsPCMSampleRate, OutputAudioCodec: "linear16",
		Pace: 1, Temperature: 0.6, MinBufferSize: 50, MaxChunkLength: 220,
	}}
	payload, marshalErr := json.Marshal(config)
	if marshalErr != nil {
		stream.terminate(ErrTTSInvalidStream)
		return nil, ErrTTSInvalidStream
	}
	if writeErr := stream.writePayload(ctx, payload); writeErr != nil {
		stream.terminate(writeErr)
		return nil, writeErr
	}
	go stream.readLoop()
	return stream, nil
}

func (client *TTSClient) String() string { return "SarvamTTSClient" }

func (stream *ttsStream) WriteText(ctx context.Context, text string) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if !validTTSText(text) {
		return ErrTTSInvalidText
	}
	if !validTTSStream(stream) {
		return ErrTTSInvalidStream
	}
	if err := ctx.Err(); err != nil {
		stream.terminate(ttsContextError(err))
		return ttsContextError(err)
	}
	payload, err := json.Marshal(ttsTextEnvelope{Type: "text", Data: ttsTextData{Text: text}})
	if err != nil || int64(len(payload)) > MaxRequestBytes {
		return ErrTTSInvalidText
	}

	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if stream.isDone() {
		return ErrTTSStreamClosed
	}
	stream.stateMu.Lock()
	if stream.phase != ttsAcceptingText {
		stream.stateMu.Unlock()
		return ErrTTSInvalidState
	}
	runeCount := utf8.RuneCountInString(text)
	if stream.texts+1 > ttsMaximumTextMessages || stream.textRunes+runeCount > ttsMaximumTextAggregate || stream.textBytes+len(text) > ttsMaximumTextBytes {
		stream.stateMu.Unlock()
		return ErrTTSInvalidText
	}
	// Publish before the wire write because an eager provider can answer before
	// WriteMessage returns to this goroutine.
	stream.hasText = true
	stream.texts++
	stream.textRunes += runeCount
	stream.textBytes += len(text)
	stream.stateMu.Unlock()
	return stream.writePayloadLocked(ctx, payload)
}

func (stream *ttsStream) Flush(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if !validTTSStream(stream) {
		return ErrTTSInvalidStream
	}
	if err := ctx.Err(); err != nil {
		stream.terminate(ttsContextError(err))
		return ttsContextError(err)
	}
	payload, _ := json.Marshal(ttsControlEnvelope{Type: "flush"})
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if stream.isDone() {
		return ErrTTSStreamClosed
	}
	stream.stateMu.Lock()
	if stream.phase != ttsAcceptingText || !stream.hasText {
		stream.stateMu.Unlock()
		return ErrTTSInvalidState
	}
	stream.phase = ttsFlushed
	stream.stateMu.Unlock()
	return stream.writePayloadLocked(ctx, payload)
}

func (stream *ttsStream) Ping(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if !validTTSStream(stream) {
		return ErrTTSInvalidStream
	}
	if err := ctx.Err(); err != nil {
		stream.terminate(ttsContextError(err))
		return ttsContextError(err)
	}
	payload, _ := json.Marshal(ttsControlEnvelope{Type: "ping"})
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if stream.isDone() {
		return ErrTTSStreamClosed
	}
	stream.stateMu.Lock()
	valid := stream.phase == ttsAcceptingText || stream.phase == ttsFlushed
	stream.stateMu.Unlock()
	if !valid {
		return ErrTTSInvalidState
	}
	return stream.writePayloadLocked(ctx, payload)
}

func (stream *ttsStream) Next(ctx context.Context) (TTSEvent, error) {
	if ctx == nil {
		return TTSEvent{}, ErrContextRequired
	}
	if !validTTSStream(stream) {
		return TTSEvent{}, ErrTTSInvalidStream
	}
	stream.nextMu.Lock()
	defer stream.nextMu.Unlock()
	for {
		if stream.isDone() {
			terminalErr := stream.terminalError()
			if !errors.Is(terminalErr, ErrTTSStreamComplete) {
				return TTSEvent{}, terminalErr
			}
		}
		select {
		case result := <-stream.events:
			return cloneTTSEvent(result.event), result.err
		default:
		}
		select {
		case result := <-stream.events:
			return cloneTTSEvent(result.event), result.err
		case <-stream.done:
			select {
			case result := <-stream.events:
				return cloneTTSEvent(result.event), result.err
			default:
			}
			return stream.nextTerminal()
		case <-ctx.Done():
			err := ttsContextError(ctx.Err())
			stream.terminate(err)
			return TTSEvent{}, err
		}
	}
}

func (stream *ttsStream) Done() <-chan struct{} {
	if stream == nil || stream.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return stream.done
}

func (stream *ttsStream) Close() error {
	if stream != nil {
		stream.terminate(ErrTTSStreamClosed)
		stream.stateMu.Lock()
		if errors.Is(stream.terminalErr, ErrTTSStreamComplete) && !stream.finalDelivered {
			stream.terminalErr = ErrTTSStreamClosed
			stream.phase = ttsClosed
			stream.finalPending = false
		}
		stream.stateMu.Unlock()
	}
	return nil
}

func (stream *ttsStream) String() string { return "SarvamTTSStream" }

func (stream *ttsStream) writePayload(ctx context.Context, payload []byte) error {
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	return stream.writePayloadLocked(ctx, payload)
}

func (stream *ttsStream) writePayloadLocked(ctx context.Context, payload []byte) error {
	if stream.isDone() {
		return ErrTTSStreamClosed
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.conn.SetWriteDeadline(deadline)
	} else {
		_ = stream.conn.SetWriteDeadline(time.Time{})
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		stream.terminate(ttsContextError(ctx.Err()))
	})
	err := stream.conn.WriteMessage(websocket.TextMessage, payload)
	if !stop() {
		<-callbackDone
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ttsContextError(ctxErr)
	}
	if err != nil {
		if stream.isDone() {
			return ErrTTSStreamClosed
		}
		stream.terminate(ErrTTSTransport)
		return ErrTTSTransport
	}
	_ = stream.conn.SetWriteDeadline(time.Time{})
	return nil
}

func (stream *ttsStream) readLoop() {
	for {
		messageType, payload, err := stream.conn.ReadMessage()
		if err != nil {
			if stream.isDone() {
				return
			}
			stream.terminate(ttsReadError(err))
			return
		}
		if messageType != websocket.TextMessage {
			stream.terminate(ErrTTSMalformedMessage)
			return
		}
		event, err := stream.decodeProviderMessage(payload)
		if err != nil {
			stream.terminate(err)
			return
		}
		if event == nil {
			continue
		}
		if event.Final {
			stream.terminate(ErrTTSStreamComplete)
			return
		}
		select {
		case stream.events <- ttsReadResult{event: cloneTTSEvent(*event)}:
		case <-stream.done:
			return
		}
	}
}

func (stream *ttsStream) decodeProviderMessage(payload []byte) (*TTSEvent, error) {
	if len(payload) > ttsMaximumMessageBytes {
		return nil, ErrTTSMessageTooLarge
	}
	if !utf8.Valid(payload) {
		return nil, ErrTTSMalformedMessage
	}
	top, err := ttsExactObject(payload, map[string]struct{}{"type": {}, "data": {}}, []string{"type", "data"})
	if err != nil {
		return nil, err
	}
	messageType, err := ttsRequiredString(top["type"])
	if err != nil {
		return nil, err
	}
	switch messageType {
	case "audio":
		return stream.decodeAudio(top["data"])
	case "event":
		return stream.decodeFinal(top["data"])
	case "error":
		return nil, decodeTTSError(top["data"])
	default:
		return nil, ErrTTSMalformedMessage
	}
}

func (stream *ttsStream) decodeAudio(raw json.RawMessage) (*TTSEvent, error) {
	object, err := ttsExactObject(raw, map[string]struct{}{
		"audio": {}, "content_type": {}, "request_id": {},
	}, []string{"audio", "content_type"})
	if err != nil {
		return nil, err
	}
	audio, err := ttsRequiredString(object["audio"])
	if err != nil || audio == "" {
		return nil, ErrTTSMalformedMessage
	}
	pcm, err := base64.StdEncoding.Strict().DecodeString(audio)
	if err != nil || len(pcm) == 0 || len(pcm)%ttsPCMBytesPerSample != 0 {
		clear(pcm)
		return nil, ErrTTSMalformedMessage
	}
	if len(pcm) > ttsMaximumPCMChunkBytes {
		clear(pcm)
		return nil, ErrTTSMessageTooLarge
	}
	contentType, err := ttsRequiredString(object["content_type"])
	if err != nil || contentType != "audio/raw" {
		clear(pcm)
		return nil, ErrTTSMalformedMessage
	}
	var requestID string
	if requestRaw, ok := object["request_id"]; ok {
		requestID, err = ttsRequiredString(requestRaw)
		if err != nil || len(requestID) > ttsMaximumRequestID || safeRequestID(requestID, "") != requestID {
			clear(pcm)
			return nil, ErrTTSMalformedMessage
		}
	}

	stream.stateMu.Lock()
	if !stream.hasText || (stream.phase != ttsAcceptingText && stream.phase != ttsFlushed) {
		stream.stateMu.Unlock()
		clear(pcm)
		return nil, ErrTTSMalformedMessage
	}
	stream.pcmSize += int64(len(pcm))
	tooLarge := stream.pcmSize > ttsMaximumPCMAggregate
	stream.stateMu.Unlock()
	if tooLarge {
		clear(pcm)
		return nil, ErrTTSMessageTooLarge
	}
	return &TTSEvent{PCM16: bytes.Clone(pcm), ContentType: contentType, RequestID: requestID}, nil
}

func (stream *ttsStream) decodeFinal(raw json.RawMessage) (*TTSEvent, error) {
	object, err := ttsExactObject(raw, map[string]struct{}{
		"event_type": {}, "message": {}, "timestamp": {},
	}, []string{"event_type"})
	if err != nil {
		return nil, err
	}
	eventType, err := ttsRequiredString(object["event_type"])
	if err != nil || eventType != "final" {
		return nil, ErrTTSMalformedMessage
	}
	if messageRaw, ok := object["message"]; ok {
		message, decodeErr := ttsRequiredString(messageRaw)
		if decodeErr != nil || !validTTSMetadataString(message, ttsMaximumFinalMessage) {
			return nil, ErrTTSMalformedMessage
		}
	}
	if timestampRaw, ok := object["timestamp"]; ok {
		timestamp, decodeErr := ttsRequiredString(timestampRaw)
		if decodeErr != nil || len(timestamp) == 0 || len(timestamp) > ttsMaximumTimestamp {
			return nil, ErrTTSMalformedMessage
		}
		if _, parseErr := time.Parse(time.RFC3339Nano, timestamp); parseErr != nil {
			return nil, ErrTTSMalformedMessage
		}
	}
	stream.stateMu.Lock()
	if stream.phase != ttsFlushed || !stream.hasText {
		stream.stateMu.Unlock()
		return nil, ErrTTSMalformedMessage
	}
	stream.phase = ttsFinal
	stream.finalPending = true
	stream.stateMu.Unlock()
	return &TTSEvent{Final: true}, nil
}

func decodeTTSError(raw json.RawMessage) error {
	object, err := ttsExactObject(raw, map[string]struct{}{
		"code": {}, "message": {}, "details": {}, "request_id": {},
	}, []string{"message"})
	if err != nil {
		return err
	}
	message, decodeErr := ttsRequiredString(object["message"])
	if decodeErr != nil || !validTTSMetadataString(message, ttsMaximumErrorMessage) {
		return ErrTTSMalformedMessage
	}
	if detailsRaw, ok := object["details"]; ok {
		if len(detailsRaw) > ttsMaximumErrorDetails || !validTTSDetailsObject(detailsRaw) {
			return ErrTTSMalformedMessage
		}
	}
	if requestRaw, ok := object["request_id"]; ok {
		requestID, decodeErr := ttsRequiredString(requestRaw)
		if decodeErr != nil || len(requestID) > ttsMaximumRequestID || safeRequestID(requestID, "") != requestID {
			return ErrTTSMalformedMessage
		}
	}
	codeValue, ok := object["code"]
	if !ok {
		return ErrTTSTransport
	}
	codeRaw := bytes.TrimSpace(codeValue)
	if len(codeRaw) == 0 || len(codeRaw) > ttsMaximumErrorCode {
		return ErrTTSMalformedMessage
	}
	var stringCode string
	if json.Unmarshal(codeRaw, &stringCode) == nil {
		switch stringCode {
		case "429", "rate_limit_exceeded", "rate_limit_exceeded_error":
			return ErrTTSRateLimited
		case "500", "503", "internal_server_error", "service_unavailable":
			return ErrTTSUnavailable
		default:
			return ErrTTSTransport
		}
	}
	var numericCode int
	if json.Unmarshal(codeRaw, &numericCode) != nil {
		return ErrTTSMalformedMessage
	}
	switch numericCode {
	case http.StatusTooManyRequests:
		return ErrTTSRateLimited
	case http.StatusInternalServerError, http.StatusServiceUnavailable:
		return ErrTTSUnavailable
	default:
		return ErrTTSTransport
	}
}

func (stream *ttsStream) terminate(err error) {
	if err == nil {
		err = ErrTTSStreamClosed
	}
	stream.stopOnce.Do(func() {
		stream.stateMu.Lock()
		if !errors.Is(err, ErrTTSStreamComplete) {
			stream.phase = ttsClosed
		}
		stream.terminalErr = err
		stream.stateMu.Unlock()
		if stream.conn != nil {
			_ = stream.conn.Close()
		}
		if stream.done != nil {
			close(stream.done)
		}
	})
}

func (stream *ttsStream) terminalError() error {
	stream.stateMu.Lock()
	defer stream.stateMu.Unlock()
	if stream.terminalErr == nil {
		return ErrTTSStreamClosed
	}
	return stream.terminalErr
}

func (stream *ttsStream) nextTerminal() (TTSEvent, error) {
	stream.stateMu.Lock()
	defer stream.stateMu.Unlock()
	if errors.Is(stream.terminalErr, ErrTTSStreamComplete) && stream.finalPending && !stream.finalDelivered {
		stream.finalDelivered = true
		return TTSEvent{Final: true}, nil
	}
	if stream.terminalErr == nil {
		return TTSEvent{}, ErrTTSStreamClosed
	}
	return TTSEvent{}, stream.terminalErr
}

func (stream *ttsStream) isDone() bool {
	select {
	case <-stream.done:
		return true
	default:
		return false
	}
}

func validTTSStream(stream *ttsStream) bool {
	return stream != nil && stream.conn != nil && stream.events != nil && stream.done != nil
}

func validTTSLanguage(languageCode string) bool {
	for _, language := range ttsLanguages {
		if languageCode == language {
			return true
		}
	}
	return false
}

func validTTSText(text string) bool {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
		return false
	}
	runes := 0
	for _, character := range text {
		runes++
		if unicode.IsControl(character) {
			return false
		}
	}
	return runes >= 1 && runes <= ttsMaximumTextRunes
}

func cloneTTSEvent(event TTSEvent) TTSEvent {
	event.PCM16 = bytes.Clone(event.PCM16)
	return event
}

func ttsExactObject(raw []byte, allowed map[string]struct{}, required []string) (map[string]json.RawMessage, error) {
	object, err := decodeExactJSONObject(raw, allowed, required)
	if err != nil {
		return nil, ErrTTSMalformedMessage
	}
	return object, nil
}

func ttsRequiredString(raw json.RawMessage) (string, error) {
	value, err := decodeRequiredString(raw)
	if err != nil {
		return "", ErrTTSMalformedMessage
	}
	return value, nil
}

func validTTSMetadataString(value string, maximumBytes int) bool {
	if !utf8.ValidString(value) || len(value) > maximumBytes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// validTTSDetailsObject validates the provider's arbitrary error-details
// object without retaining it. The frame has a tighter details-specific byte
// limit, and nested containers are bounded to avoid parser resource abuse.
func validTTSDetailsObject(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > ttsMaximumErrorDetails {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') || !consumeTTSJSONObject(decoder, 1) {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func consumeTTSJSONObject(decoder *json.Decoder, depth int) bool {
	if depth > ttsMaximumJSONDepth {
		return false
	}
	seen := make(map[string]struct{})
	entries := 0
	for decoder.More() {
		entries++
		if entries > ttsMaximumJSONEntries {
			return false
		}
		keyToken, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := keyToken.(string)
		if !ok {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		if !consumeTTSJSONValue(decoder, depth) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim('}')
}

func consumeTTSJSONArray(decoder *json.Decoder, depth int) bool {
	if depth > ttsMaximumJSONDepth {
		return false
	}
	entries := 0
	for decoder.More() {
		entries++
		if entries > ttsMaximumJSONEntries || !consumeTTSJSONValue(decoder, depth) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim(']')
}

func consumeTTSJSONValue(decoder *json.Decoder, depth int) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		switch token.(type) {
		case nil, bool, string, json.Number:
			return true
		default:
			return false
		}
	}
	switch delimiter {
	case '{':
		return consumeTTSJSONObject(decoder, depth+1)
	case '[':
		return consumeTTSJSONArray(decoder, depth+1)
	default:
		return false
	}
}

func ttsDialError(ctx context.Context, response *http.Response, apiKey string) error {
	if response != nil && response.Body != nil {
		_, _ = io.CopyN(io.Discard, response.Body, maxResponseDrainBytes)
		_ = response.Body.Close()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ttsContextError(ctxErr)
	}
	if response == nil {
		return ErrTTSTransport
	}
	requestID := safeRequestID(response.Header.Get("x-request-id"), apiKey)
	providerErr := &ProviderError{StatusCode: response.StatusCode, RequestID: requestID}
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %w", ErrTTSRateLimited, providerErr)
	case http.StatusInternalServerError, http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %w", ErrTTSUnavailable, providerErr)
	default:
		return fmt.Errorf("%w: %w", ErrTTSTransport, providerErr)
	}
}

func ttsReadError(err error) error {
	if errors.Is(err, websocket.ErrReadLimit) {
		return ErrTTSMessageTooLarge
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) && (closeErr.Code == websocket.CloseInternalServerErr || closeErr.Code == websocket.CloseTryAgainLater) {
		return ErrTTSUnavailable
	}
	return ErrTTSStreamClosed
}

func ttsContextError(err error) error {
	return fmt.Errorf("%w: %w", ErrTTSTransport, err)
}

func isNilTTSDialer(dialer TTSDialer) bool {
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

var _ TTSOpener = (*TTSClient)(nil)
var _ TTSStream = (*ttsStream)(nil)
