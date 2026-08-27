package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/composition"
	voiceruntime "invoice-backend/internal/voice/runtime"
	voicesession "invoice-backend/internal/voice/session"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
	"invoice-backend/internal/voice/webrtc"
)

func TestLoadRuntimeEnvironmentReadsOnlyBoundedNonSecretInputs(t *testing.T) {
	values := validRuntimeEnvironmentValues()
	requested := map[string]int{}
	environment, err := loadRuntimeEnvironment(func(name string) (string, bool) {
		requested[name]++
		value, ok := values[name]
		return value, ok
	})
	if err != nil {
		t.Fatalf("loadRuntimeEnvironment() error = %v", err)
	}
	if environment.region != "ap-south-1" || environment.runtimeID != "billeif_test_voice_runtime" || len(environment.channelARNs) != 12 {
		t.Fatalf("environment = %#v", environment)
	}
	if requested["SARVAM_API_KEY"] != 0 {
		t.Fatal("bootstrap attempted to read SARVAM_API_KEY")
	}
	delete(values, "COGNITO_PHONE_CLIENT_ID")
	if _, err := loadRuntimeEnvironment(func(name string) (string, bool) { value, ok := values[name]; return value, ok }); !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("missing phone client error = %v, want ErrInvalidRuntimeEnvironment", err)
	}
}

func TestNewRuntimeApplicationRoutesInvocationsToSignalingAndClosesDependencies(t *testing.T) {
	closer := &bootstrapCloser{}
	dependencies := bootstrapRuntimeDependencies()
	dependencies.Closers = []io.Closer{closer}
	application, httpServer, err := newRuntimeApplication(validRuntimeEnvironment(), dependencies)
	if err != nil {
		t.Fatalf("newRuntimeApplication() error = %v", err)
	}

	ping := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(ping, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if ping.Code != http.StatusOK || strings.TrimSpace(ping.Body.String()) != `{"status":"Healthy"}` {
		t.Fatalf("ping = (%d, %s)", ping.Code, ping.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/invocations", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer bounded-test-token")
	request.Header.Set(voiceruntime.HeaderRuntimeSessionID, "voice-session-01K000000000000000000")
	response := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid signaling invocation status = %d, want 400 (not provider-free 503); body=%s", response.Code, response.Body.String())
	}
	if err := application.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("dependency Close() calls = %d, want 1", got)
	}
}

func TestNewRuntimeApplicationLateFailureClosesDependenciesExactlyOnce(t *testing.T) {
	closer := &bootstrapCloser{}
	dependencies := bootstrapRuntimeDependencies()
	dependencies.Closers = []io.Closer{closer}
	environment := validRuntimeEnvironment()
	environment.channelARNs[0] = "arn:aws:kinesisvideo:us-east-1:123456789012:channel/wrong-region/1"
	application, httpServer, err := newRuntimeApplication(environment, dependencies)
	if application != nil || httpServer != nil || !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("newRuntimeApplication(invalid signaling) = (%v, %v, %v)", application, httpServer, err)
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("dependency Close() calls after late failure = %d, want 1", got)
	}
}

func TestRuntimeDependencyValidationRejectsTypedNilInterfaces(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runtimeDependencies)
	}{
		{name: "authorization", mutate: func(value *runtimeDependencies) { value.Authorization = (*bootstrapAuthorization)(nil) }},
		{name: "current session authorization", mutate: func(value *runtimeDependencies) { value.CurrentSessions = (*bootstrapCurrentSessions)(nil) }},
		{name: "sessions", mutate: func(value *runtimeDependencies) { value.Sessions = (*bootstrapSessions)(nil) }},
		{name: "lease renewer", mutate: func(value *runtimeDependencies) { value.LeaseRenewer = (*bootstrapSessions)(nil) }},
		{name: "ice", mutate: func(value *runtimeDependencies) { value.ICE = (*bootstrapICE)(nil) }},
		{name: "stt", mutate: func(value *runtimeDependencies) { value.STT = (*bootstrapSTT)(nil) }},
		{name: "chat", mutate: func(value *runtimeDependencies) { value.Chat = (*bootstrapChat)(nil) }},
		{name: "metrics", mutate: func(value *runtimeDependencies) { value.Metrics = (*bootstrapSignalingMetrics)(nil) }},
		{name: "output factory", mutate: func(value *runtimeDependencies) {
			var factory composition.OutputFactoryFunc
			value.Outputs = factory
		}},
		{name: "decoder factory", mutate: func(value *runtimeDependencies) {
			var factory composition.DecoderFactoryFunc
			value.Decoders = factory
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := bootstrapRuntimeDependencies()
			test.mutate(&dependencies)
			if validRuntimeDependencies(dependencies) {
				t.Fatal("typed-nil dependency was accepted")
			}
		})
	}
}

func TestRuntimeOrderedCloserIgnoresTypedNilCloser(t *testing.T) {
	closer := &runtimeOrderedCloser{
		slot:         &invocationHandlerSlot{},
		dependencies: []io.Closer{(*bootstrapCloser)(nil)},
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSarvamProductionTransportsDisableProxiesAndEnforceTLS(t *testing.T) {
	httpClient, websocketDialer, transport := newSarvamTransports()
	defer transport.CloseIdleConnections()
	if httpClient.Transport != transport || transport.Proxy != nil || websocketDialer.Proxy != nil {
		t.Fatal("Sarvam transports did not pin the proxy-free transport")
	}
	if transport.TLSClientConfig == nil || websocketDialer.TLSClientConfig == nil ||
		transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || websocketDialer.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("Sarvam transports did not enforce TLS 1.2+")
	}
	if transport.TLSClientConfig.ServerName != "api.sarvam.ai" || websocketDialer.TLSClientConfig.ServerName != "api.sarvam.ai" {
		t.Fatal("Sarvam transports did not pin the official TLS server name")
	}
	if httpClient.Timeout != sarvamTransportTimeout || websocketDialer.HandshakeTimeout <= 0 || transport.ResponseHeaderTimeout <= 0 {
		t.Fatal("Sarvam transport deadlines are not bounded")
	}
	if err := httpClient.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v, want http.ErrUseLastResponse", err)
	}
}

func TestLazySarvamProvidersResolveCredentialForEveryProviderOperation(t *testing.T) {
	resolver := &rotatingProviderResolver{}
	httpClient, websocketDialer, transport := newSarvamTransports()
	providers, err := newLazySarvamProviders(&config.Config{
		Secrets: config.SecretIdentifiers{Sarvam: "arn:aws:secretsmanager:ap-south-1:123456789012:secret:test"},
		Sarvam:  config.SarvamConfig{BaseURL: sarvam.DefaultBaseURL},
	}, resolver, httpClient, websocketDialer, transport)
	if err != nil {
		t.Fatalf("newLazySarvamProviders() error = %v", err)
	}
	for index := 0; index < 2; index++ {
		if _, err := providers.resolveClient(context.Background()); err != nil {
			t.Fatalf("resolveClient(%d) error = %v", index, err)
		}
	}
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("ResolveProvider() calls = %d, want one per operation", got)
	}
	encoded, err := json.Marshal(providers)
	if err != nil || string(encoded) != "{}" || strings.Contains(string(encoded), "rotating-provider-key") {
		t.Fatalf("provider serialization = (%s, %v)", encoded, err)
	}
	if err := providers.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := providers.resolveClient(context.Background()); !errors.Is(err, ErrInvalidRuntimeEnvironment) {
		t.Fatalf("resolveClient(after close) error = %v, want ErrInvalidRuntimeEnvironment", err)
	}
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("ResolveProvider() calls after close = %d, want 2", got)
	}
}

func TestLazySarvamProvidersResolveAndCloseAreRaceSafe(t *testing.T) {
	resolver := &rotatingProviderResolver{}
	httpClient, websocketDialer, transport := newSarvamTransports()
	providers, err := newLazySarvamProviders(&config.Config{
		Secrets: config.SecretIdentifiers{Sarvam: "arn:aws:secretsmanager:ap-south-1:123456789012:secret:test"},
		Sarvam:  config.SarvamConfig{BaseURL: sarvam.DefaultBaseURL},
	}, resolver, httpClient, websocketDialer, transport)
	if err != nil {
		t.Fatalf("newLazySarvamProviders() error = %v", err)
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < 64; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, resolveErr := providers.resolveClient(context.Background())
			if resolveErr != nil && !errors.Is(resolveErr, ErrInvalidRuntimeEnvironment) {
				t.Errorf("resolveClient() error = %v", resolveErr)
			}
		}()
	}
	close(start)
	if err := providers.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	group.Wait()
	if err := providers.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestProductionSpeechOutputFactoryIsLazyAndOwnsConsentWorker(t *testing.T) {
	tts := &bootstrapTTS{}
	writer := &bootstrapFinalTurnWriter{}
	emitter, err := voicetelemetry.NewEmitter(io.Discard, voicetelemetry.EmitterConfig{Environment: "test", Service: voicetelemetry.RuntimeService})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	factory, err := newProductionSpeechOutputFactory(t.Context(), tts, writer, emitter)
	if err != nil {
		t.Fatalf("newProductionSpeechOutputFactory() error = %v", err)
	}

	withoutConsent := voicesession.Session{ID: "voice_01K000000000000000000000001"}
	output, err := factory.NewOutput(withoutConsent)
	if err != nil {
		t.Fatalf("NewOutput(no consent) error = %v", err)
	}
	if err := output.(io.Closer).Close(); err != nil {
		t.Fatalf("Close(no consent) error = %v", err)
	}

	withConsent := withoutConsent
	withConsent.ConsentTranscriptStorage = true
	withConsent.TurnSequence = 3
	withConsent.GenerationID = 7
	withConsent.ExpiresAt = time.Now().UTC().Add(30 * time.Minute)
	output, err = factory.NewOutput(withConsent)
	if err != nil {
		t.Fatalf("NewOutput(consent) error = %v", err)
	}
	if err := output.(io.Closer).Close(); err != nil {
		t.Fatalf("Close(consent) error = %v", err)
	}
	if got := tts.openCalls.Load(); got != 0 {
		t.Fatalf("TTS opens during bootstrap/output construction = %d, want 0", got)
	}
	if got := writer.calls.Load(); got != 0 {
		t.Fatalf("durable writes without a completed turn = %d, want 0", got)
	}
}

func TestProductionFinalTurnSinkReportsExhaustedAsyncWrite(t *testing.T) {
	recorder := &runtimeSignalRecorder{}
	writer := &bootstrapFinalTurnWriter{err: errors.New("offline durable write failure")}
	factory := newProductionFinalTurnSinkFactory(t.Context(), writer, recorder)
	session := voicesession.Session{
		ID: "voice_01K000000000000000000000001", ConsentTranscriptStorage: true,
		ExpiresAt: time.Date(2026, 8, 8, 9, 30, 0, 0, time.UTC),
	}
	lease, err := factory.NewFinalTurnSink(session)
	if err != nil {
		t.Fatalf("NewFinalTurnSink() error = %v", err)
	}
	base := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	if err := lease.Sink.EnqueueAfterPlayback(voicesession.FinalTurn{
		SessionID: session.ID, Sequence: 1, GenerationID: 1,
		Transcript: "show invoice status", ProviderLanguage: "en-IN", SelectedLanguage: "en-IN",
		Response: "The invoice is paid.",
		Timings: voicesession.FinalTurnTimings{
			SpeechEndedAt: base, STTFinalAt: base.Add(time.Millisecond),
			LLMFirstTokenAt: base.Add(2 * time.Millisecond), TTSFirstAudioAt: base.Add(3 * time.Millisecond),
			ClientFirstAudioAt: base.Add(4 * time.Millisecond),
		},
		CompletedAt: base.Add(5 * time.Millisecond), ExpiresAt: session.ExpiresAt,
	}); err != nil {
		t.Fatalf("EnqueueAfterPlayback() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for len(recorder.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := recorder.snapshot(); !equalSignals(got, []voicetelemetry.Signal{voicetelemetry.SignalDurabilityFailures}) {
		t.Fatalf("async durability signals = %v, want one DurabilityFailures", got)
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := lease.Close(closeContext); !errors.Is(err, voicesession.ErrFinalTurnWrite) {
		t.Fatalf("Close() error = %v, want ErrFinalTurnWrite", err)
	}
}

func TestProductionSpeechOutputFactoryRejectsTypedNilDependencies(t *testing.T) {
	validTTS := &bootstrapTTS{}
	validWriter := &bootstrapFinalTurnWriter{}
	validEmitter, err := voicetelemetry.NewEmitter(io.Discard, voicetelemetry.EmitterConfig{Environment: "test", Service: voicetelemetry.RuntimeService})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v", err)
	}
	var nilTTS *bootstrapTTS
	var nilWriter *bootstrapFinalTurnWriter
	for name, test := range map[string]struct {
		tts     sarvam.TTSOpener
		writer  voicesession.FinalTurnWriter
		emitter *voicetelemetry.Emitter
	}{
		"tts":       {tts: nilTTS, writer: validWriter, emitter: validEmitter},
		"writer":    {tts: validTTS, writer: nilWriter, emitter: validEmitter},
		"telemetry": {tts: validTTS, writer: validWriter, emitter: nil},
	} {
		t.Run(name, func(t *testing.T) {
			factory, err := newProductionSpeechOutputFactory(t.Context(), test.tts, test.writer, test.emitter)
			if factory != nil || !errors.Is(err, ErrInvalidRuntimeEnvironment) {
				t.Fatalf("newProductionSpeechOutputFactory(typed nil) = (%v, %v)", factory, err)
			}
		})
	}
}

func bootstrapRuntimeDependencies() runtimeDependencies {
	return runtimeDependencies{
		Authorization:   bootstrapAuthorization{},
		CurrentSessions: bootstrapCurrentSessions{},
		Sessions:        bootstrapSessions{},
		LeaseRenewer:    bootstrapSessions{},
		ICE:             bootstrapICE{},
		STT:             bootstrapSTT{},
		Chat:            bootstrapChat{},
		Outputs:         composition.ControlOutputFactory{},
		Metrics:         bootstrapSignalingMetrics{},
		Decoders: composition.DecoderFactoryFunc(func() (audio.Decoder, error) {
			return bootstrapDecoder{}, nil
		}),
	}
}

func validRuntimeEnvironmentValues() map[string]string {
	arns := make([]string, 12)
	for index := range arns {
		arns[index] = "arn:aws:kinesisvideo:ap-south-1:123456789012:channel/voice-" + string(rune('a'+index)) + "/1234567890"
	}
	return map[string]string{
		"AWS_REGION":                    "ap-south-1",
		"BILLEIF_API_ORIGIN":            "https://api123.execute-api.ap-south-1.amazonaws.com/test",
		"COGNITO_PHONE_CLIENT_ID":       "voice-phone-client",
		"COGNITO_PHONE_REGION":          "ap-south-1",
		"COGNITO_PHONE_USER_POOL_ID":    "ap-south-1_voicephone",
		"RUNTIME_ID":                    "billeif_test_voice_runtime",
		"SARVAM_SECRET_ARN":             "arn:aws:secretsmanager:ap-south-1:123456789012:secret:billeif-test-sarvam",
		"VOICE_GLOBAL_CAPACITY_LIMIT":   "100",
		"VOICE_KVS_CHANNEL_ARNS":        strings.Join(arns, ","),
		"VOICE_KVS_CHANNEL_COUNT":       "12",
		"VOICE_PER_USER_CAPACITY_LIMIT": "1",
		"VOICE_PROTOCOL_VERSION":        "1",
		"VOICE_SESSION_LEASE_DURATION":  "2m",
		"VOICE_SESSION_MAX_DURATION":    "55m",
		"VOICE_SESSION_ROTATE_AFTER":    "52m",
		"VOICE_SESSIONS_TABLE_NAME":     "billeif-test-voice-sessions",
	}
}

func validRuntimeEnvironment() runtimeEnvironment {
	value, err := loadRuntimeEnvironment(func(name string) (string, bool) {
		entry, ok := validRuntimeEnvironmentValues()[name]
		return entry, ok
	})
	if err != nil {
		panic(err)
	}
	return value
}

type bootstrapAuthorization struct{}

func (bootstrapAuthorization) Resolve(context.Context, string) (webrtc.TrustedIdentity, error) {
	return webrtc.TrustedIdentity{}, webrtc.ErrSignalingUnauthorized
}

type bootstrapCurrentSessions struct{}

func (bootstrapCurrentSessions) Authorize(context.Context, string, string) (webrtc.CurrentSessionAuthorization, error) {
	return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnauthorized
}

type bootstrapSessions struct{}

func (bootstrapSessions) Get(context.Context, voicesession.Scope, string) (*voicesession.Session, error) {
	return nil, voicesession.ErrNotFound
}

func (bootstrapSessions) RenewLease(context.Context, voicesession.LeaseRenewal) (*voicesession.Session, error) {
	return nil, voicesession.ErrLeaseNotRenewable
}

type bootstrapICE struct{}

func (bootstrapICE) GetTURN(context.Context, string) (webrtc.TURNCredentials, error) {
	return webrtc.TURNCredentials{}, errors.New("offline ICE")
}

type bootstrapSTT struct{}

func (bootstrapSTT) OpenSTT(context.Context) (sarvam.STTStream, error) {
	return nil, errors.New("offline STT")
}

type bootstrapChat struct{}

func (bootstrapChat) StreamChat(context.Context, sarvam.ChatRequest, sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	return sarvam.ChatResult{}, errors.New("offline chat")
}

type bootstrapTTS struct{ openCalls atomic.Int32 }

func (tts *bootstrapTTS) OpenTTS(context.Context, string) (sarvam.TTSStream, error) {
	tts.openCalls.Add(1)
	return nil, errors.New("offline TTS")
}

type bootstrapSignalingMetrics struct{}

func (bootstrapSignalingMetrics) KVSAllocationError() {}
func (bootstrapSignalingMetrics) ICERestart()         {}

type bootstrapFinalTurnWriter struct {
	calls atomic.Int32
	err   error
}

func (writer *bootstrapFinalTurnWriter) PersistFinalTurn(context.Context, voicesession.FinalTurn) error {
	writer.calls.Add(1)
	return writer.err
}

type bootstrapDecoder struct{}

func (bootstrapDecoder) Decode([]byte, []int16) (int, error) { return audio.SamplesPerFrame, nil }

type bootstrapCloser struct{ calls atomic.Int32 }

func (closer *bootstrapCloser) Close() error {
	closer.calls.Add(1)
	return nil
}

type rotatingProviderResolver struct{ calls atomic.Int32 }

func (resolver *rotatingProviderResolver) ResolveProvider(_ context.Context, source *config.Config, kind config.SecretKind) (*config.Config, error) {
	if kind != config.SecretSarvam || source == nil {
		return nil, errors.New("invalid provider request")
	}
	resolved := *source
	resolved.Sarvam.APIKey = "rotating-provider-key-" + string(rune('0'+resolver.calls.Add(1)))
	return &resolved, nil
}
