package sarvam

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testTTSAPIKey = "offline-tts-key-never-log"

var _ TTSOpener = (*TTSClient)(nil)

type ttsDialerFunc func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)

func (function ttsDialerFunc) DialContext(ctx context.Context, endpoint string, header http.Header) (*websocket.Conn, *http.Response, error) {
	return function(ctx, endpoint, header)
}

func TestTTSStreamExactContractFirstAudioMultipleTextAndDelayedFinal(t *testing.T) {
	type observation struct {
		path          string
		query         url.Values
		apiKey        []string
		authorization []string
		messages      [][]byte
	}
	observed := make(chan observation, 1)
	secondAudioWritten := make(chan struct{})
	releaseFinal := make(chan struct{})

	firstPCM := []byte{0x01, 0x02, 0x03, 0x04}
	secondPCM := []byte{0x05, 0x06}
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, request *http.Request) {
		messages := make([][]byte, 0, 5)
		for index := 0; index < 2; index++ {
			messageType, payload, err := conn.ReadMessage()
			if err != nil || messageType != websocket.TextMessage {
				return
			}
			messages = append(messages, bytes.Clone(payload))
		}
		if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope(firstPCM, "audio/raw", "request_1")); err != nil {
			return
		}
		for index := 0; index < 3; index++ {
			messageType, payload, err := conn.ReadMessage()
			if err != nil || messageType != websocket.TextMessage {
				return
			}
			messages = append(messages, bytes.Clone(payload))
		}
		observed <- observation{
			path: request.URL.Path, query: request.URL.Query(),
			apiKey:        append([]string(nil), request.Header.Values("Api-Subscription-Key")...),
			authorization: append([]string(nil), request.Header.Values("Authorization")...),
			messages:      messages,
		}
		if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope(secondPCM, "audio/raw", "request_1")); err != nil {
			return
		}
		close(secondAudioWritten)
		<-releaseFinal
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`))
	})
	defer server.Close()
	defer closeIfOpen(releaseFinal)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "hi-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()

	if err := stream.WriteText(ctx, "Billeif INV-42"); err != nil {
		t.Fatalf("WriteText(first) error = %v", err)
	}
	first, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next(first audio) error = %v", err)
	}
	assertTTSAudioEvent(t, first, firstPCM, "request_1")
	first.PCM16[0] = 0xff

	if err := stream.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := stream.WriteText(ctx, " दुनिया"); err != nil {
		t.Fatalf("WriteText(second) error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	second, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next(second audio) error = %v", err)
	}
	assertTTSAudioEvent(t, second, secondPCM, "request_1")

	select {
	case <-secondAudioWritten:
	case <-time.After(time.Second):
		t.Fatal("provider did not write second audio")
	}
	type nextResult struct {
		event TTSEvent
		err   error
	}
	finalResult := make(chan nextResult, 1)
	go func() {
		event, nextErr := stream.Next(ctx)
		finalResult <- nextResult{event: event, err: nextErr}
	}()
	select {
	case result := <-finalResult:
		t.Fatalf("Next(final) returned before provider final: (%#v, %v)", result.event, result.err)
	case <-time.After(40 * time.Millisecond):
	}
	close(releaseFinal)
	select {
	case result := <-finalResult:
		if result.err != nil || !result.event.Final || len(result.event.PCM16) != 0 || result.event.ContentType != "" || result.event.RequestID != "" {
			t.Fatalf("final event = (%#v, %v)", result.event, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Next(final) did not return")
	}
	if _, err := stream.Next(ctx); !errors.Is(err, ErrTTSStreamComplete) {
		t.Fatalf("Next() after final error = %v, want ErrTTSStreamComplete", err)
	}
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() remained open after final")
	}
	for name, operation := range map[string]func() error{
		"text":  func() error { return stream.WriteText(ctx, "late") },
		"flush": func() error { return stream.Flush(ctx) },
		"ping":  func() error { return stream.Ping(ctx) },
	} {
		if err := operation(); !errors.Is(err, ErrTTSStreamClosed) {
			t.Errorf("%s after final error = %v, want ErrTTSStreamClosed", name, err)
		}
	}

	got := receiveTTSWithin(t, observed)
	if got.path != "/text-to-speech/ws" {
		t.Errorf("handshake path = %q", got.path)
	}
	wantQuery := url.Values{"model": {"bulbul:v3"}, "send_completion_event": {"true"}}
	if !reflect.DeepEqual(got.query, wantQuery) {
		t.Errorf("handshake query = %#v, want %#v", got.query, wantQuery)
	}
	if !reflect.DeepEqual(got.apiKey, []string{testTTSAPIKey}) || len(got.authorization) != 0 {
		t.Errorf("credential headers = api-key %#v authorization %#v", got.apiKey, got.authorization)
	}
	wantMessages := []string{
		`{"type":"config","data":{"language_code":"hi-IN","speaker":"shubh","speech_sample_rate":16000,"output_audio_codec":"linear16","pace":1,"temperature":0.6,"min_buffer_size":50,"max_chunk_length":220}}`,
		`{"type":"text","data":{"text":"Billeif INV-42"}}`,
		`{"type":"ping"}`,
		`{"type":"text","data":{"text":" दुनिया"}}`,
		`{"type":"flush"}`,
	}
	if len(got.messages) != len(wantMessages) {
		t.Fatalf("wire message count = %d, want %d", len(got.messages), len(wantMessages))
	}
	for index, want := range wantMessages {
		if string(got.messages[index]) != want {
			t.Errorf("wire message %d = %s, want %s", index, got.messages[index], want)
		}
	}
}

func TestTTSStreamAcceptsOfficialOptionalProviderFields(t *testing.T) {
	t.Run("audio request ID omitted", func(t *testing.T) {
		server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
			if _, _, err := conn.ReadMessage(); err != nil { // config
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil { // text
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"audio","data":{"audio":"AQI=","content_type":"audio/raw"}}`))
		})
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
		if err != nil {
			t.Fatalf("OpenTTS() error = %v", err)
		}
		defer stream.Close()
		if err := stream.WriteText(ctx, "hello"); err != nil {
			t.Fatalf("WriteText() error = %v", err)
		}
		event, err := stream.Next(ctx)
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		assertTTSAudioEvent(t, event, []byte{1, 2}, "")
	})

	t.Run("final metadata", func(t *testing.T) {
		server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
			for range 3 { // config, text, flush
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final","message":"Speech generation completed","timestamp":"2026-08-07T12:34:56.123Z"}}`))
		})
		defer server.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
		if err != nil {
			t.Fatalf("OpenTTS() error = %v", err)
		}
		defer stream.Close()
		if err := stream.WriteText(ctx, "hello"); err != nil {
			t.Fatalf("WriteText() error = %v", err)
		}
		if err := stream.Flush(ctx); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
		event, err := stream.Next(ctx)
		if err != nil || !event.Final {
			t.Fatalf("Next() = (%#v, %v), want final", event, err)
		}
	})
}

func TestTTSStreamBackpressuresProviderReadsBehindOneDecodedAudioEvent(t *testing.T) {
	twoEventsWritten := make(chan struct{})
	releaseProvider := make(chan struct{})
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope([]byte{1, 2}, "audio/raw", "request_1")); err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope([]byte{3, 4}, "audio/raw", "request_1")); err != nil {
			return
		}
		close(twoEventsWritten)
		<-releaseProvider
	})
	defer server.Close()
	defer closeIfOpen(releaseProvider)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()
	if err := stream.WriteText(ctx, "hello"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	providerStream := stream.(*ttsStream)
	<-twoEventsWritten
	waitForTTSCondition(t, func() bool { return len(providerStream.events) == 1 }, "first decoded event was not queued")
	// Give a reader with excess buffering time to queue the second event. The
	// bounded implementation remains blocked behind the first event.
	time.Sleep(30 * time.Millisecond)
	if queued := len(providerStream.events); queued != 1 {
		t.Fatalf("decoded event queue = %d, want hard bound of one", queued)
	}
	event, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next(first queued audio) error = %v", err)
	}
	assertTTSAudioEvent(t, event, []byte{1, 2}, "request_1")
	event, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next(second queued audio) error = %v", err)
	}
	assertTTSAudioEvent(t, event, []byte{3, 4}, "request_1")
}

func TestTTSStreamCloseIsLocalHardCancellationAndDiscardsBufferedAudio(t *testing.T) {
	audioWritten := make(chan struct{})
	peerClosed := make(chan struct{})
	unexpectedMessage := make(chan []byte, 1)
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope([]byte{1, 2, 3, 4}, "audio/raw", "request_cancel")); err != nil {
			return
		}
		close(audioWritten)
		_, payload, err := conn.ReadMessage()
		if err == nil {
			unexpectedMessage <- bytes.Clone(payload)
		}
		close(peerClosed)
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	if err := stream.WriteText(ctx, "cancel this generation"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	select {
	case <-audioWritten:
	case <-time.After(time.Second):
		t.Fatal("provider did not buffer audio before cancellation")
	}
	providerStream := stream.(*ttsStream)
	waitForTTSCondition(t, func() bool { return len(providerStream.events) == 1 }, "provider audio was not queued before cancellation")
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if event, err := stream.Next(ctx); !errors.Is(err, ErrTTSStreamClosed) || len(event.PCM16) != 0 || event.Final {
		t.Fatalf("Next() after hard close = (%#v, %v), want no stale audio and ErrTTSStreamClosed", event, err)
	}
	select {
	case payload := <-unexpectedMessage:
		t.Fatalf("Close sent an invented provider cancel message: %s", payload)
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("local Close did not close the provider socket")
	}
}

func TestNewTTSClientRequiresExplicitDependenciesAndCanonicalLanguage(t *testing.T) {
	providerClient, err := NewClient(Config{APIKey: testTTSAPIKey, BaseURL: "http://127.0.0.1:1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	var typedNil *websocket.Dialer
	for _, test := range []struct {
		name    string
		client  *Client
		dialer  TTSDialer
		wantErr error
	}{
		{name: "nil provider client", dialer: newTTSLoopbackOnlyDialer(), wantErr: ErrTTSClientRequired},
		{name: "nil dialer", client: providerClient, wantErr: ErrTTSDialerRequired},
		{name: "typed nil dialer", client: providerClient, dialer: typedNil, wantErr: ErrTTSDialerRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewTTSClient(test.client, test.dialer)
			if client != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("NewTTSClient() = (%v, %v), want nil, %v", client, err, test.wantErr)
			}
		})
	}

	var dials atomic.Int32
	dialer := ttsDialerFunc(func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		dials.Add(1)
		return nil, nil, errors.New("offline dial sentinel")
	})
	client, err := NewTTSClient(providerClient, dialer)
	if err != nil {
		t.Fatalf("NewTTSClient() error = %v", err)
	}
	for _, language := range []string{"", "or-IN", "unknown", "en-US", " en-IN", "en-IN "} {
		stream, err := client.OpenTTS(context.Background(), language)
		if stream != nil || !errors.Is(err, ErrTTSInvalidLanguage) {
			t.Errorf("OpenTTS(%q) = (%v, %v), want ErrTTSInvalidLanguage", language, stream, err)
		}
	}
	if got := dials.Load(); got != 0 {
		t.Fatalf("invalid languages caused %d network dials", got)
	}
	wantLanguages := []string{"bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN", "mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN"}
	if !reflect.DeepEqual(ttsLanguages[:], wantLanguages) {
		t.Fatalf("canonical TTS languages = %v, want %v", ttsLanguages, wantLanguages)
	}
}

func TestTTSStreamValidatesTextAndExactStateMachine(t *testing.T) {
	messages := make(chan []string, 1)
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		observed := make([]string, 0, 5)
		for index := 0; index < 5; index++ {
			messageType, payload, err := conn.ReadMessage()
			if err != nil || messageType != websocket.TextMessage {
				return
			}
			observed = append(observed, string(payload))
		}
		messages <- observed
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`))
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "kn-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()
	if err := stream.Flush(ctx); !errors.Is(err, ErrTTSInvalidState) {
		t.Fatalf("Flush() before text error = %v, want ErrTTSInvalidState", err)
	}
	invalidText := []string{
		"", "   ", "line\nbreak", string([]byte{0xff}), strings.Repeat("ಕ", 501),
	}
	for _, text := range invalidText {
		if err := stream.WriteText(ctx, text); !errors.Is(err, ErrTTSInvalidText) {
			t.Errorf("WriteText(%q) error = %v, want ErrTTSInvalidText", text, err)
		}
	}
	maxText := strings.Repeat("ಕ", 500)
	if err := stream.WriteText(ctx, maxText); err != nil {
		t.Fatalf("WriteText(500 native characters) error = %v", err)
	}
	if err := stream.WriteText(ctx, " ಪಾವತಿ ಪೂರ್ಣವಾಗಿದೆ."); err != nil {
		t.Fatalf("WriteText(second) error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := stream.Flush(ctx); !errors.Is(err, ErrTTSInvalidState) {
		t.Fatalf("duplicate Flush() error = %v, want ErrTTSInvalidState", err)
	}
	if err := stream.WriteText(ctx, "late"); !errors.Is(err, ErrTTSInvalidState) {
		t.Fatalf("WriteText() after Flush error = %v, want ErrTTSInvalidState", err)
	}
	if err := stream.Ping(ctx); err != nil {
		t.Fatalf("Ping() while awaiting final error = %v", err)
	}
	final, err := stream.Next(ctx)
	if err != nil || !final.Final {
		t.Fatalf("Next(final) = (%#v, %v)", final, err)
	}

	got := receiveTTSWithin(t, messages)
	if len(got) != 5 || !strings.Contains(got[1], maxText) || got[3] != `{"type":"flush"}` || got[4] != `{"type":"ping"}` {
		t.Fatalf("valid wire messages = %#v", got)
	}
}

func TestTTSStreamBoundsGenerationTextBeforeWire(t *testing.T) {
	for _, test := range []struct {
		name       string
		allowed    []string
		rejected   string
		wantWrites int
	}{
		{name: "message count", allowed: repeatTTSText("a", 16), rejected: "b", wantWrites: 16},
		{name: "rune aggregate", allowed: repeatTTSText(strings.Repeat("क", 500), 4), rejected: "क", wantWrites: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			textWrites := make(chan int, 1)
			server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
				if _, _, err := conn.ReadMessage(); err != nil { // config
					return
				}
				count := 0
				for {
					_, payload, err := conn.ReadMessage()
					if err != nil {
						return
					}
					if string(payload) == `{"type":"flush"}` {
						break
					}
					count++
				}
				textWrites <- count
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`))
			})
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "hi-IN")
			if err != nil {
				t.Fatalf("OpenTTS() error = %v", err)
			}
			defer stream.Close()
			for index, text := range test.allowed {
				if err := stream.WriteText(ctx, text); err != nil {
					t.Fatalf("WriteText(allowed %d) error = %v", index, err)
				}
			}
			if err := stream.WriteText(ctx, test.rejected); !errors.Is(err, ErrTTSInvalidText) {
				t.Fatalf("over-budget WriteText() error = %v, want ErrTTSInvalidText", err)
			}
			if err := stream.Flush(ctx); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}
			if event, err := stream.Next(ctx); err != nil || !event.Final {
				t.Fatalf("Next(final) = (%#v, %v)", event, err)
			}
			if got := receiveTTSWithin(t, textWrites); got != test.wantWrites {
				t.Fatalf("provider text messages = %d, want %d; rejected text reached the wire", got, test.wantWrites)
			}
		})
	}
}

func TestTTSStreamContextCancellationClosesBlockedRead(t *testing.T) {
	peerClosed := make(chan struct{})
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			close(peerClosed)
		}
	})
	defer server.Close()

	openCtx, cancelOpen := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelOpen()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(openCtx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	if err := stream.WriteText(openCtx, "wait for audio"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	readCtx, cancelRead := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelRead()
	if _, err := stream.Next(readCtx); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrTTSTransport) {
		t.Fatalf("Next() deadline error = %v, want transport + context deadline", err)
	}
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("stream remained open after read context deadline")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("provider socket remained open after read context deadline")
	}
}

func TestTTSClientMapsHandshakeStatusesWithoutLeaksOrRetries(t *testing.T) {
	for _, test := range []struct {
		status  int
		wantErr error
	}{
		{status: http.StatusTooManyRequests, wantErr: ErrTTSRateLimited},
		{status: http.StatusServiceUnavailable, wantErr: ErrTTSUnavailable},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("x-request-id", testTTSAPIKey)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, "provider-sensitive-body "+testTTSAPIKey)
			}))
			defer server.Close()

			stream, err := newTTSTestClient(t, server.URL).OpenTTS(context.Background(), "en-IN")
			if stream != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("OpenTTS() = (%v, %v), want nil, %v", stream, err, test.wantErr)
			}
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.StatusCode != test.status || providerErr.RequestID != "" {
				t.Fatalf("provider error = %#v (%v)", providerErr, err)
			}
			if strings.Contains(err.Error(), "provider-sensitive") || strings.Contains(err.Error(), testTTSAPIKey) {
				t.Fatalf("handshake error leaked provider data: %q", err)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("handshake calls = %d, want no retry", got)
			}
		})
	}
}

func TestTTSClientRejectsRedirectWithoutForwardingCredential(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		destinationCalls.Add(1)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL+"/credential-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	stream, err := newTTSTestClient(t, origin.URL).OpenTTS(context.Background(), "en-IN")
	if stream != nil || !errors.Is(err, ErrTTSTransport) {
		t.Fatalf("redirect OpenTTS() = (%v, %v), want transport failure", stream, err)
	}
	if got := destinationCalls.Load(); got != 0 {
		t.Fatalf("redirect destination received %d credential-bearing requests", got)
	}
}

func TestTTSClientCredentialDoesNotLeakThroughPublicSurfaces(t *testing.T) {
	providerClient, err := NewClient(Config{APIKey: testTTSAPIKey, BaseURL: "http://127.0.0.1:1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewTTSClient(providerClient, ttsDialerFunc(func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errors.New("transport-sensitive-detail " + testTTSAPIKey)
	}))
	if err != nil {
		t.Fatalf("NewTTSClient() error = %v", err)
	}
	_, openErr := client.OpenTTS(context.Background(), "en-IN")
	if !errors.Is(openErr, ErrTTSTransport) || strings.Contains(openErr.Error(), testTTSAPIKey) || strings.Contains(openErr.Error(), "transport-sensitive") {
		t.Fatalf("OpenTTS() leaked dial error: %v", openErr)
	}
	for _, surface := range []string{
		client.String(), fmt.Sprint(client), fmt.Sprintf("%+v", client), fmt.Sprintf("%#v", client),
		fmt.Sprintf("%+v", *client), fmt.Sprintf("%#v", *client),
	} {
		if strings.Contains(surface, testTTSAPIKey) {
			t.Fatalf("credential leaked through public surface: %q", surface)
		}
	}
}

func TestTTSStreamMapsProviderErrorsAndCloseCodesWithoutLeaks(t *testing.T) {
	for _, test := range []struct {
		name      string
		message   string
		closeCode int
		wantErr   error
	}{
		{name: "numeric rate limit", message: `{"type":"error","data":{"code":429,"message":"provider-sensitive-detail","request_id":"safe_request"}}`, wantErr: ErrTTSRateLimited},
		{name: "current rate limit code", message: `{"type":"error","data":{"code":"rate_limit_exceeded_error","message":"provider-sensitive-detail"}}`, wantErr: ErrTTSRateLimited},
		{name: "numeric unavailable", message: `{"type":"error","data":{"code":503,"message":"provider-sensitive-detail"}}`, wantErr: ErrTTSUnavailable},
		{name: "internal server code", message: `{"type":"error","data":{"code":"internal_server_error","message":"provider-sensitive-detail"}}`, wantErr: ErrTTSUnavailable},
		{name: "unknown sanitized error", message: `{"type":"error","data":{"code":"bad_request","message":"provider-sensitive-detail"}}`, wantErr: ErrTTSTransport},
		{name: "optional code omitted with details", message: `{"type":"error","data":{"message":"provider-sensitive-detail","details":{"retryable":false,"attempt":1,"nested":{"reason":null}},"request_id":"safe_request"}}`, wantErr: ErrTTSTransport},
		{name: "try again close", closeCode: websocket.CloseTryAgainLater, wantErr: ErrTTSUnavailable},
		{name: "normal close before final", closeCode: websocket.CloseNormalClosure, wantErr: ErrTTSStreamClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
				if _, _, err := conn.ReadMessage(); err != nil { // config
					return
				}
				if _, _, err := conn.ReadMessage(); err != nil { // text
					return
				}
				if test.closeCode != 0 {
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(test.closeCode, "provider-sensitive-detail"), time.Now().Add(time.Second))
					return
				}
				_ = conn.WriteMessage(websocket.TextMessage, []byte(test.message))
			})
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
			if err != nil {
				t.Fatalf("OpenTTS() error = %v", err)
			}
			defer stream.Close()
			if err := stream.WriteText(ctx, "trigger provider response"); err != nil {
				t.Fatalf("WriteText() error = %v", err)
			}
			_, err = stream.Next(ctx)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Next() error = %v, want %v", err, test.wantErr)
			}
			if strings.Contains(err.Error(), "provider-sensitive") || strings.Contains(err.Error(), testTTSAPIKey) {
				t.Fatalf("provider stream error leaked data: %q", err)
			}
		})
	}
}

func TestTTSStreamRejectsMalformedAndOversizedProviderMessages(t *testing.T) {
	validAudio := string(ttsTestAudioEnvelope([]byte{1, 2}, "audio/raw", "request_ok"))
	tests := []struct {
		name        string
		messageType int
		message     string
		writeText   bool
		flush       bool
		wantErr     error
	}{
		{name: "malformed JSON", messageType: websocket.TextMessage, message: `{`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "binary message", messageType: websocket.BinaryMessage, message: validAudio, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "audio before text", messageType: websocket.TextMessage, message: validAudio, wantErr: ErrTTSMalformedMessage},
		{name: "invalid base64", messageType: websocket.TextMessage, message: `{"type":"audio","data":{"audio":"%%%","content_type":"audio/raw","request_id":"request_1"}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "empty audio", messageType: websocket.TextMessage, message: `{"type":"audio","data":{"audio":"","content_type":"audio/raw","request_id":"request_1"}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "odd PCM16", messageType: websocket.TextMessage, message: string(ttsTestAudioEnvelope([]byte{1, 2, 3}, "audio/raw", "request_1")), writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "wrong content type", messageType: websocket.TextMessage, message: string(ttsTestAudioEnvelope([]byte{1, 2}, "audio/wav", "request_1")), writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "unsafe request ID", messageType: websocket.TextMessage, message: string(ttsTestAudioEnvelope([]byte{1, 2}, "audio/raw", "unsafe request")), writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "unknown audio field", messageType: websocket.TextMessage, message: `{"type":"audio","data":{"audio":"AQI=","content_type":"audio/raw","request_id":"request_1","extra":true}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "final before flush", messageType: websocket.TextMessage, message: `{"type":"event","data":{"event_type":"final"}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "wrong final event", messageType: websocket.TextMessage, message: `{"type":"event","data":{"event_type":"done"}}`, writeText: true, flush: true, wantErr: ErrTTSMalformedMessage},
		{name: "invalid final message", messageType: websocket.TextMessage, message: `{"type":"event","data":{"event_type":"final","message":7}}`, writeText: true, flush: true, wantErr: ErrTTSMalformedMessage},
		{name: "invalid final timestamp", messageType: websocket.TextMessage, message: `{"type":"event","data":{"event_type":"final","timestamp":"not-a-date"}}`, writeText: true, flush: true, wantErr: ErrTTSMalformedMessage},
		{name: "unknown final field", messageType: websocket.TextMessage, message: `{"type":"event","data":{"event_type":"final","extra":true}}`, writeText: true, flush: true, wantErr: ErrTTSMalformedMessage},
		{name: "error missing message", messageType: websocket.TextMessage, message: `{"type":"error","data":{"code":429}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "error details is not object", messageType: websocket.TextMessage, message: `{"type":"error","data":{"message":"failed","details":[]}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "unknown error field", messageType: websocket.TextMessage, message: `{"type":"error","data":{"message":"failed","extra":true}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "undocumented pong", messageType: websocket.TextMessage, message: `{"type":"pong","data":{}}`, writeText: true, wantErr: ErrTTSMalformedMessage},
		{name: "oversized frame", messageType: websocket.TextMessage, message: strings.Repeat("x", ttsMaximumMessageBytes+1), writeText: true, wantErr: ErrTTSMessageTooLarge},
		{name: "oversized decoded audio", messageType: websocket.TextMessage, message: string(ttsTestAudioEnvelope(make([]byte, ttsMaximumPCMChunkBytes+2), "audio/raw", "request_1")), writeText: true, wantErr: ErrTTSMessageTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
				if _, _, err := conn.ReadMessage(); err != nil { // config
					return
				}
				if test.writeText {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
				if test.flush {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
				_ = conn.WriteMessage(test.messageType, []byte(test.message))
			})
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
			if err != nil {
				t.Fatalf("OpenTTS() error = %v", err)
			}
			defer stream.Close()
			if test.writeText {
				if err := stream.WriteText(ctx, "hello"); err != nil {
					t.Fatalf("WriteText() error = %v", err)
				}
			}
			if test.flush {
				if err := stream.Flush(ctx); err != nil {
					t.Fatalf("Flush() error = %v", err)
				}
			}
			if _, err := stream.Next(ctx); !errors.Is(err, test.wantErr) {
				t.Fatalf("Next() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestTTSLinear16OfflineCharacterizationPolicy(t *testing.T) {
	if ttsPCMFormatAssumption != "raw-mono-pcm16le" || ttsPCMSampleRate != 16000 || ttsPCMChannels != 1 || ttsPCMBytesPerSample != 2 {
		t.Fatalf("launch PCM assumption changed without characterization: format=%q rate=%d channels=%d bytes/sample=%d", ttsPCMFormatAssumption, ttsPCMSampleRate, ttsPCMChannels, ttsPCMBytesPerSample)
	}
	if ttsMaximumPCMAggregate != 4<<20 {
		t.Fatalf("TTS generation PCM budget = %d, want offline-gated 4 MiB", ttsMaximumPCMAggregate)
	}
}

func TestTTSProviderTestsAndImplementationStayOffline(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return this test file")
	}
	directory := filepath.Dir(currentFile)
	for _, name := range []string{"tts.go", "tts_test.go"} {
		source, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range []string{
			"api." + "sarvam.ai",
			"Default" + "BaseURL",
			"websocket." + "DefaultDialer",
			"SARVAM" + "_API_KEY",
			"." + "env",
		} {
			if strings.Contains(string(source), forbidden) {
				t.Errorf("%s contains forbidden live-provider token %q", name, forbidden)
			}
		}
	}
}

func TestTTSStreamBoundsAggregatePCM16(t *testing.T) {
	chunk := make([]byte, ttsMaximumPCMChunkBytes)
	for index := range chunk {
		chunk[index] = byte(index)
	}
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		for sent := int64(0); sent < ttsMaximumPCMAggregate; sent += int64(len(chunk)) {
			if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope(chunk, "audio/raw", "request_large")); err != nil {
				return
			}
		}
		_ = conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope([]byte{1, 2}, "audio/raw", "request_large"))
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()
	if err := stream.WriteText(ctx, "bounded audio"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	audioBytes := int64(0)
	for {
		event, err := stream.Next(ctx)
		if err != nil {
			if !errors.Is(err, ErrTTSMessageTooLarge) {
				t.Fatalf("aggregate Next() error = %v, want ErrTTSMessageTooLarge", err)
			}
			break
		}
		audioBytes += int64(len(event.PCM16))
		if audioBytes > ttsMaximumPCMAggregate {
			t.Fatalf("delivered PCM exceeded aggregate budget: %d", audioBytes)
		}
	}
	if audioBytes != ttsMaximumPCMAggregate {
		t.Fatalf("PCM accepted before aggregate rejection = %d, want exact budget %d", audioBytes, ttsMaximumPCMAggregate)
	}
}

func TestTTSStreamFinalClosesSocketWhenAudioQueueIsFull(t *testing.T) {
	finalWritten := make(chan struct{})
	peerClosed := make(chan struct{})
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // flush
			return
		}
		for index := 0; index < ttsEventQueueCapacity; index++ {
			pcm := []byte{byte(index), 0}
			if err := conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope(pcm, "audio/raw", "request_queue")); err != nil {
				return
			}
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`)); err != nil {
			return
		}
		close(finalWritten)
		if _, _, err := conn.ReadMessage(); err != nil {
			close(peerClosed)
		}
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()
	if err := stream.WriteText(ctx, "fill bounded audio queue"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	select {
	case <-finalWritten:
	case <-time.After(time.Second):
		t.Fatal("provider did not write final")
	}
	select {
	case <-stream.Done():
	case <-time.After(300 * time.Millisecond):
		t.Fatal("full audio queue prevented final from closing the stream")
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("final did not close the provider socket")
	}
	for index := 0; index < ttsEventQueueCapacity; index++ {
		event, err := stream.Next(ctx)
		if err != nil || event.Final || !bytes.Equal(event.PCM16, []byte{byte(index), 0}) {
			t.Fatalf("queued audio %d = (%#v, %v)", index, event, err)
		}
	}
	if event, err := stream.Next(ctx); err != nil || !event.Final || len(event.PCM16) != 0 {
		t.Fatalf("terminal event after queued audio = (%#v, %v)", event, err)
	}
	if _, err := stream.Next(ctx); !errors.Is(err, ErrTTSStreamComplete) {
		t.Fatalf("Next() after terminal error = %v, want ErrTTSStreamComplete", err)
	}
}

func TestTTSStreamCloseOverridesUndeliveredCompletionAndDropsQueuedAudio(t *testing.T) {
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // text
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // flush
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, ttsTestAudioEnvelope([]byte{1, 2}, "audio/raw", "request_completed"))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`))
		<-time.After(100 * time.Millisecond)
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	if err := stream.WriteText(ctx, "completed but interrupted"); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("provider completion did not close stream")
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if event, err := stream.Next(ctx); !errors.Is(err, ErrTTSStreamClosed) || len(event.PCM16) != 0 || event.Final {
		t.Fatalf("Next() after close-over-completion = (%#v, %v), want stale completion discarded", event, err)
	}
}

func TestTTSStreamSerializesConcurrentWriters(t *testing.T) {
	const writes = 16
	textMessages := make(chan []string, 1)
	server := newTTSLoopbackServer(t, func(conn *websocket.Conn, _ *http.Request) {
		if _, _, err := conn.ReadMessage(); err != nil { // config
			return
		}
		texts := make([]string, 0, writes)
		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil || messageType != websocket.TextMessage {
				return
			}
			if string(payload) == `{"type":"flush"}` {
				break
			}
			var envelope struct {
				Type string `json:"type"`
				Data struct {
					Text string `json:"text"`
				} `json:"data"`
			}
			if json.Unmarshal(payload, &envelope) != nil || envelope.Type != "text" || envelope.Data.Text == "" {
				return
			}
			texts = append(texts, envelope.Data.Text)
		}
		textMessages <- texts
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"event","data":{"event_type":"final"}}`))
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := newTTSTestClient(t, server.URL).OpenTTS(ctx, "en-IN")
	if err != nil {
		t.Fatalf("OpenTTS() error = %v", err)
	}
	defer stream.Close()
	var wait sync.WaitGroup
	errs := make(chan error, writes)
	for index := 0; index < writes; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errs <- stream.WriteText(ctx, fmt.Sprintf("chunk-%02d", index))
		}(index)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent WriteText() error = %v", err)
		}
	}
	if err := stream.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if event, err := stream.Next(ctx); err != nil || !event.Final {
		t.Fatalf("Next(final) = (%#v, %v)", event, err)
	}
	if got := receiveTTSWithin(t, textMessages); len(got) != writes {
		t.Fatalf("serialized provider text writes = %d, want %d", len(got), writes)
	}
}

func TestTTSClientAndStreamRejectNilOrCanceledContexts(t *testing.T) {
	var dials atomic.Int32
	dialer := ttsDialerFunc(func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		dials.Add(1)
		return nil, nil, errors.New("must not dial")
	})
	providerClient, err := NewClient(Config{APIKey: testTTSAPIKey, BaseURL: "http://127.0.0.1:1"}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewTTSClient(providerClient, dialer)
	if err != nil {
		t.Fatalf("NewTTSClient() error = %v", err)
	}
	var nilContext context.Context
	if stream, err := client.OpenTTS(nilContext, "en-IN"); stream != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("OpenTTS(nil) = (%v, %v)", stream, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if stream, err := client.OpenTTS(canceled, "en-IN"); stream != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenTTS(canceled) = (%v, %v)", stream, err)
	}
	if got := dials.Load(); got != 0 {
		t.Fatalf("nil/canceled context caused %d dials", got)
	}

	var nilStream *ttsStream
	if err := nilStream.WriteText(context.Background(), "hello"); !errors.Is(err, ErrTTSInvalidStream) {
		t.Errorf("nil WriteText() error = %v", err)
	}
	if err := nilStream.Flush(context.Background()); !errors.Is(err, ErrTTSInvalidStream) {
		t.Errorf("nil Flush() error = %v", err)
	}
	if err := nilStream.Ping(context.Background()); !errors.Is(err, ErrTTSInvalidStream) {
		t.Errorf("nil Ping() error = %v", err)
	}
	if _, err := nilStream.Next(context.Background()); !errors.Is(err, ErrTTSInvalidStream) {
		t.Errorf("nil Next() error = %v", err)
	}
	if err := nilStream.Close(); err != nil {
		t.Errorf("nil Close() error = %v", err)
	}
}

func assertTTSAudioEvent(t *testing.T, event TTSEvent, wantPCM []byte, wantRequestID string) {
	t.Helper()
	if event.Final || event.ContentType != "audio/raw" || event.RequestID != wantRequestID || !bytes.Equal(event.PCM16, wantPCM) {
		t.Fatalf("audio event = %#v, want PCM %v content_type audio/raw request_id %q", event, wantPCM, wantRequestID)
	}
}

func ttsTestAudioEnvelope(pcm []byte, contentType, requestID string) []byte {
	return []byte(`{"type":"audio","data":{"audio":"` + base64.StdEncoding.EncodeToString(pcm) + `","content_type":"` + contentType + `","request_id":"` + requestID + `"}}`)
}

func newTTSTestClient(t *testing.T, baseURL string) *TTSClient {
	t.Helper()
	providerClient, err := NewClient(Config{APIKey: testTTSAPIKey, BaseURL: baseURL}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client, err := NewTTSClient(providerClient, newTTSLoopbackOnlyDialer())
	if err != nil {
		t.Fatalf("NewTTSClient() error = %v", err)
	}
	return client
}

func newTTSLoopbackServer(t *testing.T, handler func(*websocket.Conn, *http.Request)) *httptest.Server {
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

func newTTSLoopbackOnlyDialer() *websocket.Dialer {
	return &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Proxy:            nil,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("test TTS WebSocket address is invalid")
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return nil, errors.New("test TTS WebSocket dial blocked: address is not loopback")
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
}

func receiveTTSWithin[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for TTS observation")
		return zero
	}
}

func waitForTTSCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}

func repeatTTSText(text string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = text
	}
	return values
}
