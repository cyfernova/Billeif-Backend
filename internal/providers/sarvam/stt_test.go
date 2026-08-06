package sarvam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testSTTAPIKey = "offline-sarvam-key-7f31"

type sttHandshake struct {
	Path          string
	RawURL        string
	Query         url.Values
	APIKey        []string
	Authorization []string
}

type sttAudioEnvelope struct {
	Audio struct {
		Data       string `json:"data"`
		SampleRate string `json:"sample_rate"`
		Encoding   string `json:"encoding"`
	} `json:"audio"`
}

type sttAudioObservation struct {
	MessageType int
	Raw         []byte
	Envelope    sttAudioEnvelope
}

type sttFlushObservation struct {
	MessageType int
	Raw         []byte
	Envelope    map[string]any
}

func TestSTTStreamUsesFinalOnlyLegacyContract(t *testing.T) {
	t.Parallel()

	pcm := make([]byte, 640)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	handshakeCh := make(chan sttHandshake, 1)
	audioCh := make(chan sttAudioObservation, 1)
	flushCh := make(chan sttFlushObservation, 1)
	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, request *http.Request) {
		handshakeCh <- sttHandshake{
			Path:          request.URL.Path,
			RawURL:        request.URL.String(),
			Query:         request.URL.Query(),
			APIKey:        append([]string(nil), request.Header.Values(APIKeyHeader)...),
			Authorization: append([]string(nil), request.Header.Values("Authorization")...),
		}

		audioType, audioJSON, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var audio sttAudioEnvelope
		if err := json.Unmarshal(audioJSON, &audio); err != nil {
			return
		}
		audioCh <- sttAudioObservation{MessageType: audioType, Raw: audioJSON, Envelope: audio}

		flushType, flushJSON, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var flush map[string]any
		if err := json.Unmarshal(flushJSON, &flush); err != nil {
			return
		}
		flushCh <- sttFlushObservation{MessageType: flushType, Raw: flushJSON, Envelope: flush}

		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"events","data":{"signal_type":"END_SPEECH"}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"data","data":{"request_id":"offline-request","transcript":"नमस्ते","language_code":"hi-IN","language_probability":"0.97","metrics":{"audio_duration":0.02,"processing_latency":0.01}}}`))
	})
	defer server.Close()

	client := newSTTTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.OpenSTT(ctx)
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()

	if err := stream.WritePCM16(ctx, pcm); err != nil {
		t.Fatalf("WritePCM16() error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	final, err := stream.AwaitFinal(ctx)
	if err != nil {
		t.Fatalf("AwaitFinal() error = %v", err)
	}

	wantProbability := 0.97
	wantFinal := FinalTranscript{
		Text:                "नमस्ते",
		DetectedLanguage:    "hi-IN",
		LanguageProbability: &wantProbability,
	}
	if !reflect.DeepEqual(final, wantFinal) {
		t.Fatalf("AwaitFinal() = %#v, want %#v", final, wantFinal)
	}

	handshake := receiveWithin(t, handshakeCh)
	wantQuery := url.Values{
		"flush_signal":      {"true"},
		"input_audio_codec": {"pcm_s16le"},
		"language-code":     {"unknown"},
		"mode":              {"transcribe"},
		"model":             {"saaras:v3"},
		"sample_rate":       {"16000"},
	}
	if handshake.Path != "/speech-to-text/ws" {
		t.Errorf("handshake path = %q, want %q", handshake.Path, "/speech-to-text/ws")
	}
	if !reflect.DeepEqual(handshake.Query, wantQuery) {
		t.Errorf("handshake query = %#v, want %#v", handshake.Query, wantQuery)
	}
	if !reflect.DeepEqual(handshake.APIKey, []string{testSTTAPIKey}) {
		t.Errorf("handshake API key headers = %#v, want one configured value", handshake.APIKey)
	}
	if len(handshake.Authorization) != 0 {
		t.Errorf("handshake unexpectedly sent Authorization: %#v", handshake.Authorization)
	}
	if strings.Contains(handshake.RawURL, testSTTAPIKey) {
		t.Errorf("handshake URL leaked the API key: %q", handshake.RawURL)
	}

	audio := receiveWithin(t, audioCh)
	if audio.MessageType != websocket.TextMessage {
		t.Errorf("audio WebSocket type = %d, want text", audio.MessageType)
	}
	assertExactJSONKeys(t, audio.Raw, "audio")
	var audioData map[string]json.RawMessage
	if err := json.Unmarshal(audio.Raw, &audioData); err != nil {
		t.Fatalf("decode audio outer object: %v", err)
	}
	assertExactJSONKeys(t, audioData["audio"], "data", "sample_rate", "encoding")
	decodedPCM, err := base64.StdEncoding.DecodeString(audio.Envelope.Audio.Data)
	if err != nil {
		t.Fatalf("audio data is not base64: %v", err)
	}
	if !reflect.DeepEqual(decodedPCM, pcm) {
		t.Errorf("decoded PCM does not match the 20 ms input frame")
	}
	if audio.Envelope.Audio.SampleRate != "16000" || audio.Envelope.Audio.Encoding != "audio/wav" {
		t.Errorf("audio metadata = (%q, %q), want (%q, %q)", audio.Envelope.Audio.SampleRate, audio.Envelope.Audio.Encoding, "16000", "audio/wav")
	}
	flush := receiveWithin(t, flushCh)
	if flush.MessageType != websocket.TextMessage {
		t.Errorf("flush WebSocket type = %d, want text", flush.MessageType)
	}
	if string(flush.Raw) != `{"type":"flush"}` {
		t.Errorf("flush JSON = %q, want exact legacy envelope", flush.Raw)
	}
	if !reflect.DeepEqual(flush.Envelope, map[string]any{"type": "flush"}) {
		t.Errorf("flush envelope = %#v, want only type=flush", flush.Envelope)
	}
}

func TestNewSTTClientRequiresExplicitNonNilDependencies(t *testing.T) {
	t.Parallel()

	providerClient, err := NewClient(Config{
		APIKey:  testSTTAPIKey,
		BaseURL: "http://127.0.0.1:1",
	}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	var typedNilDialer *websocket.Dialer
	tests := []struct {
		name    string
		client  *Client
		dialer  STTDialer
		wantErr error
	}{
		{name: "nil provider client", dialer: newLoopbackOnlyWebSocketDialer(), wantErr: ErrSTTClientRequired},
		{name: "nil dialer", client: providerClient, wantErr: ErrSTTDialerRequired},
		{name: "typed nil dialer", client: providerClient, dialer: typedNilDialer, wantErr: ErrSTTDialerRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewSTTClient(test.client, test.dialer)
			if client != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("NewSTTClient() = (%v, %v), want (nil, %v)", client, err, test.wantErr)
			}
		})
	}
}

func TestSTTStreamReusesOneSocketForSequentialTurns(t *testing.T) {
	t.Parallel()

	var connections atomic.Int32
	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		connections.Add(1)
		responses := []string{
			`{"type":"data","data":{"transcript":"first","language_code":"en-IN","language_probability":0.82}}`,
			`{"type":"data","data":{"transcript":"दूसरा","language_code":"hi-IN","language_probability":null}}`,
		}
		for _, response := range responses {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
				return
			}
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()

	client := newSTTTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.OpenSTT(ctx)
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()

	first := writeAndAwaitSTTTurn(t, ctx, stream, 0x11)
	if first.Text != "first" || first.DetectedLanguage != "en-IN" ||
		first.LanguageProbability == nil || *first.LanguageProbability != 0.82 {
		t.Fatalf("first final = %#v", first)
	}
	second := writeAndAwaitSTTTurn(t, ctx, stream, 0x22)
	if second.Text != "दूसरा" || second.DetectedLanguage != "hi-IN" || second.LanguageProbability != nil {
		t.Fatalf("second final = %#v", second)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("WebSocket connections = %d, want 1", got)
	}
}

func TestSTTStreamRejectsDuplicateFinalBeforeNewAudio(t *testing.T) {
	t.Parallel()

	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"data","data":{"transcript":"one"}}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"data","data":{"transcript":"duplicate"}}`))
		<-time.After(100 * time.Millisecond)
	})
	defer server.Close()

	client := newSTTTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.OpenSTT(ctx)
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()
	final := writeAndAwaitSTTTurn(t, ctx, stream, 0x33)
	if final.Text != "one" {
		t.Fatalf("final text = %q, want %q", final.Text, "one")
	}

	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("stream remained open after a duplicate final")
	}
	if err := stream.WritePCM16(ctx, make([]byte, 640)); !errors.Is(err, ErrSTTStreamClosed) {
		t.Fatalf("WritePCM16() after duplicate final error = %v, want %v", err, ErrSTTStreamClosed)
	}
}

func TestSTTStreamEnforcesFrameAndTurnOrdering(t *testing.T) {
	t.Parallel()

	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()
	client := newSTTTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.OpenSTT(ctx)
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()

	for _, size := range []int{0, 639, 641, 1280} {
		if err := stream.WritePCM16(ctx, make([]byte, size)); !errors.Is(err, ErrSTTInvalidFrame) {
			t.Errorf("WritePCM16(%d bytes) error = %v, want %v", size, err, ErrSTTInvalidFrame)
		}
	}
	if err := stream.Flush(ctx); !errors.Is(err, ErrSTTInvalidState) {
		t.Fatalf("Flush() before audio error = %v, want %v", err, ErrSTTInvalidState)
	}
	if _, err := stream.AwaitFinal(ctx); !errors.Is(err, ErrSTTInvalidState) {
		t.Fatalf("AwaitFinal() before flush error = %v, want %v", err, ErrSTTInvalidState)
	}
	if err := stream.WritePCM16(ctx, make([]byte, 640)); err != nil {
		t.Fatalf("WritePCM16(640 bytes) error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := stream.Flush(ctx); !errors.Is(err, ErrSTTInvalidState) {
		t.Fatalf("duplicate Flush() error = %v, want %v", err, ErrSTTInvalidState)
	}
	if err := stream.WritePCM16(ctx, make([]byte, 640)); !errors.Is(err, ErrSTTInvalidState) {
		t.Fatalf("WritePCM16() before final error = %v, want %v", err, ErrSTTInvalidState)
	}
}

func TestSTTStreamRejectsMalformedOrUnboundedProviderMessages(t *testing.T) {
	tests := []struct {
		name        string
		messageType int
		payload     string
		wantErr     error
	}{
		{name: "binary frame", messageType: websocket.BinaryMessage, payload: `{"type":"data","data":{"transcript":"no"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "trailing JSON", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no"}} {}`, wantErr: ErrSTTMalformedMessage},
		{name: "duplicate top-level key", messageType: websocket.TextMessage, payload: `{"type":"data","type":"data","data":{"transcript":"no"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "case variant top-level key", messageType: websocket.TextMessage, payload: `{"Type":"data","data":{"transcript":"no"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "unknown top-level key", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no"},"extra":true}`, wantErr: ErrSTTMalformedMessage},
		{name: "duplicate transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"one","transcript":"two"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "case variant transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"Transcript":"no"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "unknown transcript field", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","partial":true}}`, wantErr: ErrSTTMalformedMessage},
		{name: "duplicate metric", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","metrics":{"audio_duration":1,"audio_duration":2}}}`, wantErr: ErrSTTMalformedMessage},
		{name: "unknown metric", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","metrics":{"latency":1}}}`, wantErr: ErrSTTMalformedMessage},
		{name: "invalid event", messageType: websocket.TextMessage, payload: `{"type":"events","data":{"signal_type":"SOMETHING_ELSE"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "missing transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"language_code":"hi-IN"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "empty transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":""}}`, wantErr: ErrSTTMalformedMessage},
		{name: "whitespace transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"   "}}`, wantErr: ErrSTTMalformedMessage},
		{name: "control character transcript", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"hello\nthere"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "transcript over control limit", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"` + strings.Repeat("x", (16<<10)+1) + `"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "probability above one", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","language_probability":1.01}}`, wantErr: ErrSTTMalformedMessage},
		{name: "probability below zero", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","language_probability":"-0.1"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "probability not finite", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"no","language_probability":"NaN"}}`, wantErr: ErrSTTMalformedMessage},
		{name: "oversized message", messageType: websocket.TextMessage, payload: `{"type":"data","data":{"transcript":"` + strings.Repeat("x", 65<<10) + `"}}`, wantErr: ErrSTTMessageTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				_ = conn.WriteMessage(test.messageType, []byte(test.payload))
			})
			defer server.Close()

			client := newSTTTestClient(t, server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := client.OpenSTT(ctx)
			if err != nil {
				t.Fatalf("OpenSTT() error = %v", err)
			}
			defer stream.Close()
			if err := stream.WritePCM16(ctx, make([]byte, 640)); err != nil {
				t.Fatalf("WritePCM16() error = %v", err)
			}
			if err := stream.Flush(ctx); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}
			if _, err := stream.AwaitFinal(ctx); !errors.Is(err, test.wantErr) {
				t.Fatalf("AwaitFinal() error = %v, want %v", err, test.wantErr)
			}
			select {
			case <-stream.Done():
			case <-time.After(time.Second):
				t.Fatal("stream remained open after a malformed provider message")
			}
		})
	}
}

func TestSTTStreamAcceptsTranscriptAtControlLimit(t *testing.T) {
	t.Parallel()

	want := strings.Repeat("x", 16<<10)
	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"data","data":{"transcript":"`+want+`"}}`))
	})
	defer server.Close()

	client := newSTTTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.OpenSTT(ctx)
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()
	if got := writeAndAwaitSTTTurn(t, ctx, stream, 0x44).Text; got != want {
		t.Fatalf("transcript length = %d, want %d", len(got), len(want))
	}
}

func TestSTTClientMapsHandshakeStatusesWithoutLeakingProviderData(t *testing.T) {
	tests := []struct {
		status       int
		wantSentinel error
	}{
		{status: http.StatusTooManyRequests, wantSentinel: ErrSTTRateLimited},
		{status: http.StatusServiceUnavailable, wantSentinel: ErrSTTUnavailable},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("status_%d", test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("x-request-id", testSTTAPIKey)
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte("provider body " + testSTTAPIKey))
			}))
			defer server.Close()

			client := newSTTTestClient(t, server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream, err := client.OpenSTT(ctx)
			if stream != nil || !errors.Is(err, test.wantSentinel) {
				t.Fatalf("OpenSTT() = (%v, %v), want (nil, %v)", stream, err, test.wantSentinel)
			}
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.StatusCode != test.status {
				t.Fatalf("OpenSTT() error = %v, want ProviderError status %d", err, test.status)
			}
			if providerErr.RequestID != "" {
				t.Errorf("unsafe request ID was retained: %q", providerErr.RequestID)
			}
			if strings.Contains(err.Error(), testSTTAPIKey) || strings.Contains(err.Error(), "provider body") {
				t.Errorf("OpenSTT() error leaked provider data: %q", err)
			}
		})
	}
}

func TestSTTStreamMapsProviderCloseAndDeadlineSafely(t *testing.T) {
	t.Run("provider close", func(t *testing.T) {
		server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			reason := "private provider close " + testSTTAPIKey
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, reason), time.Now().Add(time.Second))
		})
		defer server.Close()

		client := newSTTTestClient(t, server.URL)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stream, err := client.OpenSTT(ctx)
		if err != nil {
			t.Fatalf("OpenSTT() error = %v", err)
		}
		defer stream.Close()
		if err := stream.WritePCM16(ctx, make([]byte, 640)); err != nil {
			t.Fatalf("WritePCM16() error = %v", err)
		}
		if err := stream.Flush(ctx); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
		_, err = stream.AwaitFinal(ctx)
		if !errors.Is(err, ErrSTTStreamClosed) {
			t.Fatalf("AwaitFinal() error = %v, want %v", err, ErrSTTStreamClosed)
		}
		if strings.Contains(err.Error(), testSTTAPIKey) || strings.Contains(err.Error(), "private provider close") {
			t.Errorf("AwaitFinal() leaked close reason: %q", err)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			time.Sleep(150 * time.Millisecond)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"data","data":{"transcript":"late"}}`))
		})
		defer server.Close()

		client := newSTTTestClient(t, server.URL)
		openCtx, openCancel := context.WithTimeout(context.Background(), time.Second)
		defer openCancel()
		stream, err := client.OpenSTT(openCtx)
		if err != nil {
			t.Fatalf("OpenSTT() error = %v", err)
		}
		defer stream.Close()
		if err := stream.WritePCM16(openCtx, make([]byte, 640)); err != nil {
			t.Fatalf("WritePCM16() error = %v", err)
		}
		if err := stream.Flush(openCtx); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
		awaitCtx, awaitCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer awaitCancel()
		_, err = stream.AwaitFinal(awaitCtx)
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrSTTTransport) {
			t.Fatalf("AwaitFinal() error = %v, want transport deadline", err)
		}
		select {
		case <-stream.Done():
		case <-time.After(time.Second):
			t.Fatal("stream remained open after final-result deadline")
		}
	})
}

func TestSTTContextAndCloseBehaviorIsBounded(t *testing.T) {
	t.Parallel()

	providerClient, err := NewClient(Config{APIKey: testSTTAPIKey, BaseURL: "http://127.0.0.1:1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	dialer := sttDialerFunc(func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		t.Fatal("dialer called for an invalid context")
		return nil, nil, nil
	})
	client, err := NewSTTClient(providerClient, dialer)
	if err != nil {
		t.Fatalf("NewSTTClient() error = %v", err)
	}
	if stream, err := client.OpenSTT(nilSTTContext()); stream != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("OpenSTT(nil) = (%v, %v), want context error", stream, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if stream, err := client.OpenSTT(canceled); stream != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenSTT(canceled) = (%v, %v), want cancellation", stream, err)
	}

	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()
	stream, err := newSTTTestClient(t, server.URL).OpenSTT(context.Background())
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	if err := stream.WritePCM16(nilSTTContext(), make([]byte, 640)); !errors.Is(err, ErrContextRequired) {
		t.Errorf("WritePCM16(nil) error = %v, want context error", err)
	}
	if err := stream.Flush(nilSTTContext()); !errors.Is(err, ErrContextRequired) {
		t.Errorf("Flush(nil) error = %v, want context error", err)
	}
	if _, err := stream.AwaitFinal(nilSTTContext()); !errors.Is(err, ErrContextRequired) {
		t.Errorf("AwaitFinal(nil) error = %v, want context error", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close")
	}
	if err := stream.WritePCM16(context.Background(), make([]byte, 640)); !errors.Is(err, ErrSTTStreamClosed) {
		t.Errorf("WritePCM16() after close error = %v, want stream closed", err)
	}
}

func TestSTTWriteCancellationRaceRetiresStream(t *testing.T) {
	t.Parallel()

	server := newSTTLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer server.Close()

	dialer, wrappedConnCh := newSTTCancellationRaceDialer()
	providerClient, err := NewClient(Config{APIKey: testSTTAPIKey, BaseURL: server.URL}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewSTTClient(providerClient, dialer)
	if err != nil {
		t.Fatalf("NewSTTClient() error = %v", err)
	}
	stream, err := client.OpenSTT(context.Background())
	if err != nil {
		t.Fatalf("OpenSTT() error = %v", err)
	}
	defer stream.Close()
	wrappedConn := receiveWithin(t, wrappedConnCh)
	wrappedConn.armCancellationRace()

	ctx, cancel := context.WithCancel(context.Background())
	writeErrCh := make(chan error, 1)
	go func() {
		writeErrCh <- stream.WritePCM16(ctx, make([]byte, 640))
	}()
	receiveSignalWithin(t, wrappedConn.writeStarted)
	cancel()
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("stream remained reusable after an ambiguous canceled write")
	}
	close(wrappedConn.releaseWrite)
	if err := receiveWithin(t, writeErrCh); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrSTTTransport) {
		t.Fatalf("WritePCM16() cancellation-race error = %v, want transport cancellation", err)
	}
	if err := stream.WritePCM16(context.Background(), make([]byte, 640)); !errors.Is(err, ErrSTTStreamClosed) {
		t.Fatalf("second WritePCM16() error = %v, want stream closed", err)
	}
}

func TestSTTErrorsAndFormattingDoNotExposeCredentials(t *testing.T) {
	t.Parallel()

	providerClient, err := NewClient(Config{APIKey: testSTTAPIKey, BaseURL: "http://127.0.0.1:1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	dialer := sttDialerFunc(func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("dial failure containing " + testSTTAPIKey)
	})
	client, err := NewSTTClient(providerClient, dialer)
	if err != nil {
		t.Fatalf("NewSTTClient() error = %v", err)
	}
	stream, openErr := client.OpenSTT(context.Background())
	if stream != nil || !errors.Is(openErr, ErrSTTTransport) {
		t.Fatalf("OpenSTT() = (%v, %v), want sanitized transport error", stream, openErr)
	}
	surfaces := []string{
		openErr.Error(),
		fmt.Sprint(client),
		fmt.Sprintf("%+v", client),
		fmt.Sprintf("%#v", client),
		fmt.Sprintf("%+v", *client),
		fmt.Sprintf("%#v", *client),
	}
	for _, surface := range surfaces {
		if strings.Contains(surface, testSTTAPIKey) {
			t.Fatalf("credential leaked through public surface: %q", surface)
		}
	}
}

type sttDialerFunc func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)

func (function sttDialerFunc) DialContext(ctx context.Context, endpoint string, header http.Header) (*websocket.Conn, *http.Response, error) {
	return function(ctx, endpoint, header)
}

func nilSTTContext() context.Context {
	return nil
}

func writeAndAwaitSTTTurn(t *testing.T, ctx context.Context, stream STTStream, fill byte) FinalTranscript {
	t.Helper()
	pcm := make([]byte, 640)
	for i := range pcm {
		pcm[i] = fill
	}
	if err := stream.WritePCM16(ctx, pcm); err != nil {
		t.Fatalf("WritePCM16() error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	final, err := stream.AwaitFinal(ctx)
	if err != nil {
		t.Fatalf("AwaitFinal() error = %v", err)
	}
	return final
}

func newSTTTestClient(t *testing.T, baseURL string) *STTClient {
	t.Helper()
	providerClient, err := NewClient(Config{APIKey: testSTTAPIKey, BaseURL: baseURL}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewSTTClient(providerClient, newLoopbackOnlyWebSocketDialer())
	if err != nil {
		t.Fatalf("NewSTTClient() error = %v", err)
	}
	return client
}

func newSTTLoopbackServer(t *testing.T, handler func(*websocket.Conn, *http.Request)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn, request)
	}))
}

func newLoopbackOnlyWebSocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Proxy:            nil,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("test WebSocket address is invalid")
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return nil, errors.New("test WebSocket dial blocked: address is not loopback")
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
}

type sttCancellationRaceConn struct {
	net.Conn
	blockWrite   atomic.Bool
	writeStarted chan struct{}
	releaseWrite chan struct{}
}

func (conn *sttCancellationRaceConn) armCancellationRace() {
	conn.blockWrite.Store(true)
}

func (conn *sttCancellationRaceConn) Write(payload []byte) (int, error) {
	if conn.blockWrite.CompareAndSwap(true, false) {
		close(conn.writeStarted)
		<-conn.releaseWrite
	}
	return conn.Conn.Write(payload)
}
func newSTTCancellationRaceDialer() (*websocket.Dialer, <-chan *sttCancellationRaceConn) {
	dialer := newLoopbackOnlyWebSocketDialer()
	baseDial := dialer.NetDialContext
	connCh := make(chan *sttCancellationRaceConn, 1)
	dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := baseDial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		wrapped := &sttCancellationRaceConn{
			Conn:         conn,
			writeStarted: make(chan struct{}),
			releaseWrite: make(chan struct{}),
		}
		connCh <- wrapped
		return wrapped, nil
	}
	return dialer, connCh
}

func receiveWithin[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fake STT server observation")
		var zero T
		return zero
	}
}

func receiveSignalWithin(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fake transport signal")
	}
}

func assertExactJSONKeys(t *testing.T, raw []byte, want ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	if len(object) != len(want) {
		t.Fatalf("JSON keys = %v, want %v", reflect.ValueOf(object).MapKeys(), want)
	}
	for _, key := range want {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON object is missing exact key %q", key)
		}
	}
}
